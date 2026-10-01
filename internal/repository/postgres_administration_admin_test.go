package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAdministrativeGrantAdministration covers migration 000089 and the
// ADA-05 repository: issuing is replay-safe, transitions follow the
// lifecycle at the grant's version, revoking ends every delegation, and the
// platform activates and expires grants itself. Each change is audited.
// digest makes a 64-character request hash, as the handler does.
func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestAdministrativeGrantAdministration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	tail := domain.NewUUIDv7()[24:]
	jane, bob, ops, other := "prn_jane"+tail, "prn_bob"+tail, "prn_ops"+tail, "prn_other"+tail
	t.Cleanup(func() {
		who := []string{jane, bob, other}
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant_command WHERE actor_id = $1`, ops)
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1) AND delegated_from IS NOT NULL`, who)
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1)`, who)
	})
	actor := func() AuditActor {
		return AuditActor{ActorID: ops, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}
	catalogue := administration.MustDefaultCatalogue()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenant := administration.Scope{Level: administration.LevelTenant, TenantID: "tn_acmeug"}
	plan := func(principal string, depth int) administration.Grant {
		g, err := administration.PlanIssue(catalogue, ops, administration.IssueRequest{PrincipalID: principal, Permission: "tenant.view",
			Scope: tenant, GrantType: administration.TypeStanding, DelegableDepth: depth, Reason: "test"}, now)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}

	// Issuing: replay with the same request returns the same grant, a
	// different request under the same key is refused, and it is audited.
	g := plan(jane, 1)
	issued, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-0000001", digest("hash-a"), g)
	if err != nil {
		t.Fatal(err)
	}
	if issued.GrantID == "" || issued.Version != 1 || issued.Status != administration.StatusActive {
		t.Fatalf("issued %+v", issued)
	}
	again, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-0000001", digest("hash-a"), plan(jane, 1))
	if err != nil || again.GrantID != issued.GrantID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-0000001", digest("hash-b"), plan(jane, 1)); !errors.Is(err, ErrGrantCommandKeyReused) {
		t.Fatalf("a reused key must be refused, got %v", err)
	}
	var count int
	admin.QueryRow(ctx, `SELECT count(*) FROM policy.administrative_grant WHERE principal_id = $1`, jane).Scan(&count)
	if count != 1 {
		t.Fatalf("a replay created %d grants", count)
	}
	admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'administrative_grant.created' AND target = $1`, issued.GrantID).Scan(&count)
	if count != 1 {
		t.Fatalf("issuing wrote %d audit events", count)
	}

	// Inspecting and paging.
	for i := 0; i < 2; i++ {
		if _, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-extra"+string(rune('a'+i))+"000000", digest("h"+string(rune('a'+i))), plan(other, 0)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.GetAdministrativeGrant(ctx, issued.GrantID)
	if err != nil || got.PrincipalID != jane {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := repo.GetAdministrativeGrant(ctx, "agr_doesnotexist"); !errors.Is(err, ErrGrantNotFound) {
		t.Fatalf("missing grant: %v", err)
	}
	page, next, err := repo.ListAdministrativeGrants(ctx, GrantFilter{PrincipalID: other}, 1, "")
	if err != nil || len(page) != 1 || next == "" {
		t.Fatalf("first page: %d %q %v", len(page), next, err)
	}
	page2, next2, err := repo.ListAdministrativeGrants(ctx, GrantFilter{PrincipalID: other}, 1, next)
	if err != nil || len(page2) != 1 || next2 != "" || page2[0].GrantID == page[0].GrantID {
		t.Fatalf("second page: %+v %q %v", page2, next2, err)
	}
	if _, _, err := repo.ListAdministrativeGrants(ctx, GrantFilter{}, 10, "not-a-cursor"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("bad cursor: %v", err)
	}

	// Delegation from the source Jane holds.
	delegated, err := repo.DelegateAdministrativeGrant(ctx, AuditActor{ActorID: jane, ActorType: "human", CorrelationID: domain.NewUUIDv7()},
		"key-delegate-000001", "hash-d", issued.GrantID,
		func(source administration.Grant, sources map[string]administration.Grant) (administration.Grant, error) {
			return administration.PlanDelegation(catalogue, jane, source, sources, administration.DelegationRequest{
				PrincipalID: bob, Permission: "tenant.view", Scope: tenant, ValidUntil: now.Add(time.Hour), Reason: "cover"}, now)
		})
	if err != nil {
		t.Fatal(err)
	}
	if delegated.Source != administration.SourceDelegation || delegated.DelegatedFromGrantID != issued.GrantID {
		t.Fatalf("delegated %+v", delegated)
	}
	// Someone who does not hold the source cannot delegate it.
	_, err = repo.DelegateAdministrativeGrant(ctx, AuditActor{ActorID: other, ActorType: "human", CorrelationID: domain.NewUUIDv7()},
		"key-delegate-000002", "hash-e", issued.GrantID,
		func(source administration.Grant, sources map[string]administration.Grant) (administration.Grant, error) {
			return administration.PlanDelegation(catalogue, other, source, sources, administration.DelegationRequest{
				PrincipalID: bob, Permission: "tenant.view", Scope: tenant, ValidUntil: now.Add(time.Hour), Reason: "x"}, now)
		})
	var refusal *administration.Refusal
	if !errors.As(err, &refusal) || refusal.Code != administration.CodeDelegationInvalid {
		t.Fatalf("delegating someone else's grant: %v", err)
	}

	// Transitions: stale version, invalid command, then the lifecycle.
	if _, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-suspend-000001", digest("t1"), issued.GrantID,
		administration.CommandSuspend, "review", 7, now); !errors.Is(err, ErrGrantVersionMismatch) {
		t.Fatalf("stale version: %v", err)
	}
	if _, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-withdraw-00001", digest("t2"), issued.GrantID,
		administration.CommandWithdraw, "no", 1, now); !errors.As(err, &refusal) || refusal.Code != administration.CodeTransitionInvalid {
		t.Fatalf("withdrawing an active grant: %v", err)
	}
	suspended, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-suspend-000002", digest("t3"), issued.GrantID,
		administration.CommandSuspend, "review", 1, now)
	if err != nil || suspended.Status != administration.StatusSuspended || suspended.Version != 2 {
		t.Fatalf("suspend: %+v %v", suspended, err)
	}
	replay, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-suspend-000002", digest("t3"), issued.GrantID,
		administration.CommandSuspend, "review", 1, now)
	if err != nil || replay.Version != 2 {
		t.Fatalf("a replayed transition must not apply twice: %+v %v", replay, err)
	}
	resumed, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-resume-0000001", digest("t4"), issued.GrantID,
		administration.CommandResume, "cleared", 2, now)
	if err != nil || resumed.Status != administration.StatusActive || resumed.Version != 3 {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	revoked, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-revoke-0000001", digest("t5"), issued.GrantID,
		administration.CommandRevoke, "left the team", 3, now)
	if err != nil || revoked.Status != administration.StatusRevoked || revoked.RevokedBy != ops || revoked.RevocationReason != "left the team" {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	cascaded, err := repo.GetAdministrativeGrant(ctx, delegated.GrantID)
	if err != nil || cascaded.Status != administration.StatusRevoked || cascaded.RevokedBy != ops {
		t.Fatalf("revoking a source must revoke its delegations: %+v %v", cascaded, err)
	}
	admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'administrative_grant.REVOKED' AND target = ANY($1)`,
		[]string{issued.GrantID, delegated.GrantID}).Scan(&count)
	if count != 2 {
		t.Fatalf("revocation and its cascade wrote %d audit events, want 2", count)
	}

	// The platform activates and expires grants itself.
	future := plan(bob, 0)
	future.ValidFrom, future.Status = now.Add(time.Hour), administration.StatusPending
	pending, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-pending01", digest("hp"), future)
	if err != nil {
		t.Fatal(err)
	}
	lapsing := plan(bob, 0)
	until := now.Add(2 * time.Hour)
	lapsing.GrantType, lapsing.ValidUntil = administration.TypeTimeBound, &until
	timed, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-timed0001", digest("ht"), lapsing)
	if err != nil {
		t.Fatal(err)
	}
	if a, e, err := repo.SweepAdministrativeGrants(ctx, now.Add(10*time.Minute)); err != nil || a != 0 {
		t.Fatalf("early sweep activated %d (expired %d): %v", a, e, err)
	}
	if _, _, err := repo.SweepAdministrativeGrants(ctx, now.Add(90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if p, _ := repo.GetAdministrativeGrant(ctx, pending.GrantID); p.Status != administration.StatusActive {
		t.Fatalf("the sweep must activate a PENDING grant whose start has come, got %s", p.Status)
	}
	if _, _, err := repo.SweepAdministrativeGrants(ctx, now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if e, _ := repo.GetAdministrativeGrant(ctx, timed.GrantID); e.Status != administration.StatusExpired {
		t.Fatalf("the sweep must expire a grant past its end, got %s", e.Status)
	}
}
