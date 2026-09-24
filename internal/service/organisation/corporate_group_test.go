package organisation

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
)

// TestCorporateGroupDerivation drives ADR-BCP-018 gate ORG-05 against real
// PostgreSQL: policy-based derivation with lineage, silent re-derivation,
// divestiture ending (not deleting) a membership, manual memberships left to
// governance, and groups that cannot be derived.
func TestCorporateGroupDerivation(t *testing.T) {
	e := newEnv(t)
	at := e.at.Add(time.Hour)
	later := at.Add(time.Hour)
	root, a, b, minority, unknownStake, parent, manual := e.canonicalOrganisation(t), e.canonicalOrganisation(t),
		e.canonicalOrganisation(t), e.canonicalOrganisation(t), e.canonicalOrganisation(t), e.canonicalOrganisation(t), e.canonicalOrganisation(t)

	edge := func(source, target string, typ domain.CorporateRelationshipType, pct *float64) string {
		t.Helper()
		id, err := e.repo.EnsureCorporateRelationship(e.ctx, domain.CorporateRelationship{
			SourceOrganisationID: source, TargetOrganisationID: target, RelationshipType: typ, OwnershipPercentage: pct,
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
	p := func(v float64) *float64 { return &v }
	rootOwnsA := edge(root, a, domain.CorpRelOwns, p(100))
	aControlsB := edge(a, b, domain.CorpRelControls, nil)
	rootOwnsMinority := edge(root, minority, domain.CorpRelOwns, p(30))
	rootOwnsUnknown := edge(root, unknownStake, domain.CorpRelOwns, nil)
	edge(parent, root, domain.CorpRelControls, nil)

	groupID := domain.NewResourceID(domain.CorporateGroupIDPrefix)
	if err := e.repo.CreateCorporateGroup(e.ctx, domain.CorporateGroup{ID: groupID, DisplayName: "Test Group", RootOrganisationID: root,
		Status: "ACTIVE", GroupingPolicy: domain.GroupPolicyVerifiedControlMajority, EffectiveFrom: e.at}, actor()); err != nil {
		t.Fatal(err)
	}
	// A governed manual membership (e.g. a board resolution) for an
	// organisation with no qualifying edge.
	if _, err := e.repo.EnsureCorporateGroupMembership(e.ctx, domain.CorporateGroupMembership{CorporateGroupID: groupID, OrganisationID: manual,
		GroupRole: "OTHER", ManualBasisReference: "evd_board_resolution_2026_01", Status: "ACTIVE", EffectiveFrom: e.at,
		DerivedAt: e.at, DerivationVersion: "governed-manual"}, actor()); err != nil {
		t.Fatal(err)
	}

	deriver := &CorporateGroupDeriver{Orgs: e.repo, Now: func() time.Time { return at }}
	first := actor()
	report, err := deriver.Derive(e.ctx, groupID, first)
	if err != nil {
		t.Fatal(err)
	}
	if want := sortedCopy([]string{root, a, b}); !slices.Equal(sortedCopy(report.Added), want) {
		t.Fatalf("added %v, want %v", report.Added, want)
	}
	if !slices.Equal(report.Manual, []string{manual}) || len(report.Ended) != 0 {
		t.Fatalf("manual=%v ended=%v", report.Manual, report.Ended)
	}
	excluded := map[string]string{}
	for _, x := range report.Excluded {
		excluded[x.RelationshipID] = x.Reason
	}
	if !strings.Contains(excluded[rootOwnsMinority], "controlling majority") || !strings.Contains(excluded[rootOwnsUnknown], "percentage") {
		t.Fatalf("excluded = %v", excluded)
	}
	if !strings.Contains(strings.Join(report.Ambiguities, ";"), "may not be the ultimate parent") {
		t.Fatalf("a root controlled from outside must be reported: %v", report.Ambiguities)
	}
	lineage := map[string][]string{}
	members, err := e.repo.ListCorporateGroupMembers(e.ctx, groupID, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		lineage[m.OrganisationID] = m.BasisRelationshipIDs
		if m.OrganisationID != manual && m.DerivationVersion != domain.GroupPolicyVerifiedControlMajority {
			t.Errorf("%s derivation_version = %q", m.OrganisationID, m.DerivationVersion)
		}
	}
	if !slices.Equal(lineage[a], []string{rootOwnsA}) || !slices.Equal(lineage[b], []string{aControlsB}) || !slices.Equal(lineage[root], []string{rootOwnsA}) {
		t.Fatalf("lineage = %v", lineage)
	}
	if _, in := lineage[minority]; in {
		t.Fatal("a 30% shareholding must not make an organisation a group member")
	}

	// Re-deriving an unchanged graph changes and records nothing.
	again := actor()
	report, err = deriver.Derive(e.ctx, groupID, again)
	if err != nil {
		t.Fatal(err)
	}
	var audits int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM audit_events WHERE correlation_id=$1::uuid`, again.CorrelationID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if len(report.Added) != 0 || len(report.Ended) != 0 || len(report.Unchanged) != 3 || audits != 0 {
		t.Fatalf("re-derivation: %+v audits=%d", report, audits)
	}

	// Divestiture: a no longer controls b. b's membership ends; history stays.
	if _, err := e.admin.Exec(e.ctx, `UPDATE registry.corporate_relationship SET status='ENDED', effective_to=$2 WHERE corporate_relationship_id=$1::uuid`,
		mustRow(t, aControlsB), later.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	deriver.Now = func() time.Time { return later }
	report, err = deriver.Derive(e.ctx, groupID, actor())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(report.Ended, []string{b}) || len(report.Added) != 0 {
		t.Fatalf("divestiture: %+v", report)
	}
	before, _ := e.repo.ListCorporateGroupMembers(e.ctx, groupID, at)
	after, _ := e.repo.ListCorporateGroupMembers(e.ctx, groupID, later.Add(time.Minute))
	if !hasMember(before, b) || hasMember(after, b) || !hasMember(after, manual) {
		t.Fatalf("effective-dated membership: before=%v after=%v", orgIDs(before), orgIDs(after))
	}

	// A root that starts alone rests on the group definition; when it later
	// gains a member, its lineage is updated rather than frozen as "manual".
	solo, joiner := e.canonicalOrganisation(t), e.canonicalOrganisation(t)
	soloGroup := domain.NewResourceID(domain.CorporateGroupIDPrefix)
	if err := e.repo.CreateCorporateGroup(e.ctx, domain.CorporateGroup{ID: soloGroup, DisplayName: "Solo", RootOrganisationID: solo,
		Status: "ACTIVE", GroupingPolicy: domain.GroupPolicyVerifiedControlMajority, EffectiveFrom: e.at}, actor()); err != nil {
		t.Fatal(err)
	}
	if report, err := deriver.Derive(e.ctx, soloGroup, actor()); err != nil || !slices.Equal(report.Added, []string{solo}) {
		t.Fatalf("solo root: %+v %v", report, err)
	}
	soloOwnsJoiner := edge(solo, joiner, domain.CorpRelOwns, p(51))
	deriver.Now = func() time.Time { return later.Add(time.Hour) }
	report, err = deriver.Derive(e.ctx, soloGroup, actor())
	if err != nil || !slices.Equal(report.Ended, []string{solo}) || !slices.Equal(sortedCopy(report.Added), sortedCopy([]string{solo, joiner})) || len(report.Manual) != 0 {
		t.Fatalf("root gaining a member: %+v %v", report, err)
	}
	soloMembers, _ := e.repo.ListCorporateGroupMembers(e.ctx, soloGroup, later.Add(2*time.Hour))
	for _, m := range soloMembers {
		if m.OrganisationID == solo && (!slices.Equal(m.BasisRelationshipIDs, []string{soloOwnsJoiner}) || m.ManualBasisReference != "") {
			t.Fatalf("root lineage not updated: %+v", m)
		}
	}

	// Groups that cannot be derived are skipped, not guessed.
	for name, g := range map[string]domain.CorporateGroup{
		"no root":       {DisplayName: "Rootless", Status: "ACTIVE", GroupingPolicy: domain.GroupPolicyVerifiedControlMajority},
		"manual policy": {DisplayName: "Manual", RootOrganisationID: root, Status: "ACTIVE", GroupingPolicy: "governed-manual/v1"},
	} {
		g.ID, g.EffectiveFrom = domain.NewResourceID(domain.CorporateGroupIDPrefix), e.at
		if err := e.repo.CreateCorporateGroup(e.ctx, g, actor()); err != nil {
			t.Fatal(err)
		}
		report, err := deriver.Derive(e.ctx, g.ID, actor())
		if err != nil || report.Skipped == "" || len(report.Added) != 0 {
			t.Errorf("%s: %+v %v", name, report, err)
		}
	}
}

func sortedCopy(v []string) []string { out := slices.Clone(v); slices.Sort(out); return out }

func mustRow(t *testing.T, id string) string {
	t.Helper()
	row, err := domain.ParseResourceID(domain.CorporateRelationshipIDPrefix, id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func hasMember(ms []domain.CorporateGroupMembership, org string) bool {
	for _, m := range ms {
		if m.OrganisationID == org {
			return true
		}
	}
	return false
}

func orgIDs(ms []domain.CorporateGroupMembership) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.OrganisationID)
	}
	return out
}
