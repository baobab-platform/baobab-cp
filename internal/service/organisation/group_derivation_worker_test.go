package organisation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// failingGraph makes the corporate graph unreadable, standing in for any
// derivation failure.
type failingGraph struct {
	repository.OrganisationRepository
}

func (failingGraph) ListCorporateControlDescendants(context.Context, string, time.Time) ([]domain.CorporateRelationship, error) {
	return nil, errors.New("graph unavailable")
}

// TestGroupDerivationWorker drives ADR-BCP-018 gate ORG-05's derivation as
// derived-state maintenance against real PostgreSQL: every corporate graph
// change requests a derivation in its own transaction, the worker converges
// membership asynchronously, a failure is recorded and retried without
// touching the authoritative relationship, a request arriving mid-derivation
// is not lost, and the scheduled sweep re-derives everything.
func TestGroupDerivationWorker(t *testing.T) {
	e := newEnv(t)
	root, a, c := e.canonicalOrganisation(t), e.canonicalOrganisation(t), e.canonicalOrganisation(t)
	p := func(v float64) *float64 { return &v }
	edge := func(source, target string) string {
		t.Helper()
		id, err := e.repo.EnsureCorporateRelationship(e.ctx, domain.CorporateRelationship{
			SourceOrganisationID: source, TargetOrganisationID: target, RelationshipType: domain.CorpRelOwns, OwnershipPercentage: p(100),
			DirectOrDerived: domain.CorporateFactDirect, VerificationState: domain.VerificationPendingReview,
			Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "admission-review"}, actor())
		if err != nil {
			t.Fatal(err)
		}
		if err := e.repo.VerifyCorporateRelationship(e.ctx, id, e.evidence(), actor()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	rootOwnsA := edge(root, a)

	groupID := domain.NewResourceID(domain.CorporateGroupIDPrefix)
	if err := e.repo.CreateCorporateGroup(e.ctx, domain.CorporateGroup{ID: groupID, DisplayName: "Worker Group", RootOrganisationID: root,
		Status: "ACTIVE", GroupingPolicy: domain.GroupPolicyVerifiedControlMajority, EffectiveFrom: e.at}, actor()); err != nil {
		t.Fatal(err)
	}
	state := func() repository.GroupDerivationState {
		t.Helper()
		s, err := e.repo.GetCorporateGroupDerivation(e.ctx, groupID)
		if err != nil || s == nil {
			t.Fatalf("derivation record: %v %v", s, err)
		}
		return *s
	}
	members := func() []string {
		t.Helper()
		live, err := e.repo.ListLiveCorporateGroupMembers(e.ctx, groupID)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, m := range live {
			ids = append(ids, m.OrganisationID)
		}
		slices.Sort(ids)
		return ids
	}
	want := func(ids ...string) []string { slices.Sort(ids); return ids }

	// The deriver evaluates the graph at a fixed instant; the worker's own
	// clock is real time, as the queue's timestamps are. Tests in other
	// packages share this database and may request derivations at any
	// moment, so "settled" below means not RETRYING rather than CURRENT.
	derivedAt := e.at.Add(time.Hour)
	deriver := &CorporateGroupDeriver{Orgs: e.repo, Now: func() time.Time { return derivedAt }}
	worker := &GroupDerivationWorker{Deriver: deriver, Queue: e.repo, Batch: 1000}
	// drain runs passes until a derivation of this group has succeeded since
	// it was called. A pass can miss the group: the claim skips rows another
	// transaction holds (FOR UPDATE SKIP LOCKED), and every corporate
	// relationship change in any test package sharing this database requests
	// every group's derivation, locking their rows until it commits. A group
	// still RETRYING after such a pass has not been derived, so "settled"
	// cannot mean merely "not PENDING".
	drain := func() {
		t.Helper()
		since := time.Now()
		for i := 0; i < 100; i++ {
			if _, err := worker.RunOnce(e.ctx); err != nil {
				t.Fatal(err)
			}
			if s := state(); s.State != repository.GroupDerivationRetrying && s.LastSucceededAt != nil && !s.LastSucceededAt.Before(since) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("derivation never ran")
	}

	// Creating the group requested its derivation; nothing is derived until
	// the worker runs.
	if s := state(); s.State != repository.GroupDerivationPending || len(members()) != 0 {
		t.Fatalf("a new group should be PENDING with no members: %+v %v", s, members())
	}
	drain()
	// Membership is asserted rather than LastAdded: a concurrent graph change
	// from another test package may run a further, empty derivation.
	if s := state(); s.State == repository.GroupDerivationRetrying || s.LastSucceededAt == nil {
		t.Fatalf("after the first derivation: %+v", s)
	}
	if got := members(); !slices.Equal(got, want(root, a)) {
		t.Fatalf("members = %v, want root and a", got)
	}

	// A new verified relationship requests a derivation in its own
	// transaction, and the worker converges on it.
	edge(a, c)
	if s := state(); s.State != repository.GroupDerivationPending {
		t.Fatalf("a relationship change should request a derivation: %+v", s)
	}
	drain()
	if got := members(); !slices.Equal(got, want(root, a, c)) {
		t.Fatalf("members = %v, want root, a and c", got)
	}

	// A failing derivation is recorded for retry; the relationship change
	// that requested it stays committed and membership is untouched. (Retry
	// timing is covered without a database in TestGroupDerivationWorkerRetries:
	// here, tests in other packages change the shared graph concurrently,
	// which by design makes every backed-off derivation due at once.)
	worker.Deriver = &CorporateGroupDeriver{Orgs: failingGraph{e.repo}, Now: func() time.Time { return derivedAt }}
	if err := e.repo.EndCorporateRelationship(e.ctx, rootOwnsA, derivedAt.Add(-time.Minute), "divested", actor()); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	if failed := state(); failed.State != repository.GroupDerivationRetrying || failed.Attempts < 1 || failed.LastError != "graph unavailable" || failed.NextAttemptAt == nil {
		t.Fatalf("a failed derivation should be RETRYING with its error: %+v", failed)
	}
	if got := members(); !slices.Equal(got, want(root, a, c)) {
		t.Fatalf("a failed derivation changed membership: %v", got)
	}
	// A further graph change makes the backed-off derivation due at once.
	d := e.canonicalOrganisation(t)
	edge(c, d)
	if s := state(); s.NextAttemptAt == nil || s.NextAttemptAt.After(time.Now()) {
		t.Fatalf("a graph change should make a backed-off derivation due now: %+v", s)
	}
	worker.Deriver = deriver
	drain()
	// Ending root→a cuts a, c and d off from the root.
	if s := state(); s.State == repository.GroupDerivationRetrying || s.Attempts != 0 || s.LastError != "" {
		t.Fatalf("after recovery: %+v", s)
	}
	if got := members(); !slices.Equal(got, want(root)) {
		t.Fatalf("members = %v, want only root after the divestiture", got)
	}

	// A request arriving while a derivation runs is not cleared by it.
	if _, err := e.repo.RequestCorporateGroupDerivations(e.ctx, "test"); err != nil {
		t.Fatal(err)
	}
	// The claim skips rows another transaction holds (FOR UPDATE SKIP
	// LOCKED), and a relationship change in any package sharing the test
	// database locks every group's row while it requests their derivation,
	// so claim until that transaction has let go.
	var claim *repository.GroupDerivationClaim
	for deadline := time.Now().Add(10 * time.Second); claim == nil && time.Now().Before(deadline); {
		claims, err := e.repo.ClaimCorporateGroupDerivations(e.ctx, time.Now(), time.Minute, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for i := range claims {
			if claims[i].GroupID == groupID {
				claim = &claims[i]
			}
		}
		if claim == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if claim == nil {
		t.Fatal("the requested group was not claimed")
	}
	// A request arriving mid-derivation never shortens the lease: no other
	// worker derives the group concurrently.
	if _, err := e.repo.RequestCorporateGroupDerivations(e.ctx, "arrived mid-derivation"); err != nil {
		t.Fatal(err)
	}
	if again, err := e.repo.ClaimCorporateGroupDerivations(e.ctx, time.Now(), time.Minute, 1000); err != nil || slices.ContainsFunc(again, func(c repository.GroupDerivationClaim) bool { return c.GroupID == groupID }) {
		t.Fatalf("a leased derivation was claimed again: %v %v", again, err)
	}
	if err := e.repo.RecordCorporateGroupDerived(e.ctx, groupID, claim.RequestSeq, time.Now(), 0, 0); err != nil {
		t.Fatal(err)
	}
	if s := state(); s.State != repository.GroupDerivationPending {
		t.Fatalf("a request that arrived mid-derivation was lost: %+v", s)
	}
	drain()

	// The scheduled sweep re-derives every derivable group; with nothing
	// changed it converges without changes.
	if err := worker.Sweep(e.ctx); err != nil {
		t.Fatal(err)
	}
	if s := state(); s.State != repository.GroupDerivationPending {
		t.Fatalf("the sweep should request a derivation: %+v", s)
	}
	drain()
	if s := state(); s.State == repository.GroupDerivationRetrying || s.LastAdded != 0 || s.LastEnded != 0 {
		t.Fatalf("an unchanged graph should re-derive without changes: %+v", s)
	}
}

// TestGroupDerivationSkipsGroupsWithoutDerivation: governed-manual groups
// and retired groups are never queued, so derivation never overrides them.
func TestGroupDerivationSkipsGroupsWithoutDerivation(t *testing.T) {
	e := newEnv(t)
	root := e.canonicalOrganisation(t)
	for _, g := range []domain.CorporateGroup{
		{DisplayName: "Manual Group", RootOrganisationID: root, Status: "ACTIVE", GroupingPolicy: "governed-manual"},
		{DisplayName: "Retired Group", RootOrganisationID: root, Status: "RETIRED", GroupingPolicy: domain.GroupPolicyVerifiedControlMajority},
	} {
		g.ID, g.EffectiveFrom = domain.NewResourceID(domain.CorporateGroupIDPrefix), e.at
		if err := e.repo.CreateCorporateGroup(e.ctx, g, actor()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.repo.RequestCorporateGroupDerivations(e.ctx, "test"); err != nil {
			t.Fatal(err)
		}
		if s, err := e.repo.GetCorporateGroupDerivation(e.ctx, g.ID); err != nil || s != nil {
			t.Fatalf("%s was queued for derivation: %+v %v", g.DisplayName, s, err)
		}
	}
}

// TestCorporateGroupIsNeverAnAccessPath: group membership confers no access
// (section 29). Outside the derivation itself, nothing that resolves context,
// authorization, capabilities, products, INTERNAL eligibility or any API
// route reads corporate groups, so a stale or failed derivation cannot
// change who may do what.
func TestCorporateGroupIsNeverAnAccessPath(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	derivation := map[string]bool{
		filepath.Join(root, "internal/service/organisation/corporate_group.go"):         true,
		filepath.Join(root, "internal/service/organisation/group_derivation_worker.go"): true,
	}
	for _, dir := range []string{"api", "internal/resolver", "internal/auth", "internal/service", "internal/capability", "internal/product", "internal/billing"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || derivation[path] {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(raw), "corporate_group") || strings.Contains(string(raw), "CorporateGroup") {
				t.Errorf("%s reads corporate groups; group membership is never an access path", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
