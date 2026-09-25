package organisation

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// lifecycleFixture is a verified platform owner that verifiably owns an
// organisation holding a verified PLATFORM_GROUP_AFFILIATE relationship.
type lifecycleFixture struct {
	platform, owner, sub, ownsSub, affiliate string
	resolver                                 *EligibilityResolver
}

func newLifecycleFixture(t *testing.T, e *env, sub string) lifecycleFixture {
	t.Helper()
	f := lifecycleFixture{platform: "test-platform-" + token(), owner: e.canonicalOrganisation(t), sub: sub}
	f.resolver = &EligibilityResolver{Orgs: e.repo, PlatformID: f.platform}
	ownerRel, err := e.repo.EnsurePlatformRelationship(e.ctx, domain.PlatformRelationship{PlatformID: f.platform, OrganisationID: f.owner,
		RelationshipType: domain.PlatformRelOwner, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "platform-governance"}, actor())
	if err != nil {
		t.Fatal(err)
	}
	if f.ownsSub, err = e.repo.EnsureCorporateRelationship(e.ctx, domain.CorporateRelationship{
		SourceOrganisationID: f.owner, TargetOrganisationID: sub, RelationshipType: domain.CorpRelOwns,
		DirectOrDerived: domain.CorporateFactDirect, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "shared-governance"}, actor()); err != nil {
		t.Fatal(err)
	}
	if f.affiliate, err = e.repo.EnsurePlatformRelationship(e.ctx, domain.PlatformRelationship{PlatformID: f.platform, OrganisationID: sub,
		RelationshipType: domain.PlatformRelGroupAffiliate, BasisRelationshipID: f.ownsSub, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "platform-governance"}, actor()); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.VerifyCorporateRelationship(e.ctx, f.ownsSub, e.evidence(), actor()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{ownerRel, f.affiliate} {
		if err := e.repo.VerifyPlatformRelationship(e.ctx, id, e.evidence(), actor()); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f lifecycleFixture) eligible(t *testing.T, e *env, at time.Time) bool {
	t.Helper()
	got, err := f.resolver.ResolveInternalEligibility(e.ctx, f.sub, at)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestDivestitureReview is ADR-BCP-018's required divestiture scenario
// (section 167): a corporate relationship ending causes a
// PLATFORM_GROUP_AFFILIATE review, INTERNAL eligibility review and a
// commercial reclassification, without changing the tenant, the canonical
// organisation or history.
func TestDivestitureReview(t *testing.T) {
	e := newEnv(t)
	sub, provisioned, req := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	f := newLifecycleFixture(t, e, sub)
	before, divested, after := e.at.Add(time.Hour), e.at.Add(2*time.Hour), e.at.Add(3*time.Hour)
	if !f.eligible(t, e, before) {
		t.Fatal("a verified affiliate of the platform owner must be INTERNAL-eligible before the divestiture")
	}

	reviewer := &CorporateChangeReviewer{Orgs: e.repo, Eligibility: f.resolver}
	ending := actor()
	review, err := reviewer.EndCorporateRelationship(e.ctx, f.ownsSub, divested, "shares sold to a third party", ending)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Affiliates) != 1 || review.Affiliates[0].PlatformRelationshipID != f.affiliate ||
		review.Affiliates[0].OrganisationID != sub || review.Affiliates[0].InternalEligible {
		t.Fatalf("review = %+v; want the affiliate flagged and no longer INTERNAL-eligible", review)
	}
	if got := e.outboxTypes(t, ending.CorrelationID); !slices.Equal(got, []string{events.CorporateRelationshipEnded}) {
		t.Fatalf("ending published %v", got)
	}
	// Ending never cascades: the affiliate stays live until governance decides.
	if pr, err := e.repo.GetPlatformRelationship(e.ctx, f.affiliate); err != nil || pr.Status != domain.RelationshipStatusActive {
		t.Fatalf("affiliate after the corporate change: %+v %v; want it untouched pending review", pr, err)
	}

	deciding := actor()
	reclassified, err := reviewer.TerminateAffiliate(e.ctx, AffiliateTermination{PlatformRelationshipID: f.affiliate, At: divested,
		Reason: "no longer a group company", DecisionReference: "gov_reclass_" + token(),
		ReclassifyAs: domain.PlatformRelExternalClient}, deciding)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.outboxTypes(t, deciding.CorrelationID); !slices.Equal(got, []string{events.PlatformRelationshipEnded}) {
		t.Fatalf("termination published %v; the reclassification is pending and publishes nothing yet", got)
	}
	// The tenant's own EXTERNAL_CLIENT relationship is on another platform;
	// on this platform the reclassification is a new, unverified record.
	replacement, err := e.repo.GetPlatformRelationship(e.ctx, reclassified)
	if err != nil || replacement == nil || replacement.RelationshipType != domain.PlatformRelExternalClient ||
		replacement.Status != domain.RelationshipStatusPending || replacement.VerificationState != domain.VerificationPendingReview ||
		replacement.OrganisationID != sub || replacement.PlatformID != f.platform {
		t.Fatalf("reclassification = %+v %v", replacement, err)
	}
	if f.eligible(t, e, after) {
		t.Fatal("a divested organisation must not remain INTERNAL-eligible")
	}

	// Identity is untouched: same organisation, same tenant mapping.
	mappings, err := e.repo.ListTenantOrganisationMappings(e.ctx, req.TenantID, after)
	if err != nil || len(mappings) != 1 || mappings[0].OrganisationID != sub || mappings[0].ID != provisioned.TenantOrganisationMapping {
		t.Fatalf("tenant mappings after divestiture: %+v %v", mappings, err)
	}
	var kind string
	if err := e.admin.QueryRow(e.ctx, `SELECT entity_type FROM registry.canonical_entity WHERE canonical_entity_id=$1::uuid`, sub).Scan(&kind); err != nil || kind != "ORGANISATION" {
		t.Fatalf("canonical organisation after divestiture: %q %v", kind, err)
	}
	// History stays queryable: as of before the divestiture the owner still
	// owned the organisation and it was INTERNAL-eligible.
	ancestry, err := e.repo.ListCorporateControlAncestry(e.ctx, sub, before)
	if err != nil || len(ancestry) != 1 || ancestry[0].ID != f.ownsSub {
		t.Fatalf("ancestry as of before the divestiture: %+v %v", ancestry, err)
	}
	if !f.eligible(t, e, before) {
		t.Fatal("eligibility as of before the divestiture must be unchanged")
	}
}

// TestConflictingOwnershipFailsClosed proves section 73: conflicting
// authoritative evidence moves the fact to CONFLICTED instead of choosing a
// value, consequential use fails closed, and review can restore it.
func TestConflictingOwnershipFailsClosed(t *testing.T) {
	e := newEnv(t)
	f := newLifecycleFixture(t, e, e.canonicalOrganisation(t))
	at := e.at.Add(time.Hour)
	if !f.eligible(t, e, at) {
		t.Fatal("precondition: eligible")
	}

	first := actor()
	if err := e.repo.MarkCorporateRelationshipConflicted(e.ctx, f.ownsSub, repository.Conflict{References: []string{"evd_registry_extract_40pct"},
		DetectedAt: at, Reason: "registry extract shows 40%, filing shows 100%"}, first); err != nil {
		t.Fatal(err)
	}
	if got := e.outboxTypes(t, first.CorrelationID); !slices.Equal(got, []string{events.CorporateRelationshipConflicted}) {
		t.Fatalf("conflict published %v", got)
	}
	if f.eligible(t, e, at) {
		t.Fatal("a CONFLICTED ownership fact must not support INTERNAL eligibility")
	}

	again := actor()
	if err := e.repo.MarkCorporateRelationshipConflicted(e.ctx, f.ownsSub, repository.Conflict{References: []string{"evd_second_source"},
		DetectedAt: at, Reason: "a second source also disagrees"}, again); err != nil {
		t.Fatal(err)
	}
	if got := e.outboxTypes(t, again.CorrelationID); len(got) != 0 {
		t.Fatalf("a further conflict on a CONFLICTED fact published %v", got)
	}
	row := mustRow(t, f.ownsSub)
	var state string
	var meta []byte
	if err := e.admin.QueryRow(e.ctx, `SELECT verification_state, metadata FROM registry.corporate_relationship WHERE corporate_relationship_id=$1::uuid`, row).Scan(&state, &meta); err != nil {
		t.Fatal(err)
	}
	var m struct {
		Conflicting []string `json:"conflicting_evidence_references"`
	}
	if err := json.Unmarshal(meta, &m); err != nil || state != "CONFLICTED" ||
		!slices.Equal(m.Conflicting, []string{"evd_registry_extract_40pct", "evd_second_source"}) {
		t.Fatalf("state=%s conflicting=%v err=%v", state, m.Conflicting, err)
	}
	var audited int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM audit_events WHERE correlation_id=$1::uuid AND action='corporate_relationship.conflicted'`,
		again.CorrelationID).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("second conflict audit rows = %d, %v", audited, err)
	}

	// Review resolves the conflict by re-verifying the fact (CONFLICTED → ACTIVE).
	if err := e.repo.VerifyCorporateRelationship(e.ctx, f.ownsSub, e.evidence(), actor()); err != nil {
		t.Fatal(err)
	}
	if !f.eligible(t, e, at) {
		t.Fatal("re-verified ownership must support eligibility again")
	}
}

// TestRelationshipTransitionGuards covers the rules the transitions enforce.
func TestRelationshipTransitionGuards(t *testing.T) {
	e := newEnv(t)
	f := newLifecycleFixture(t, e, e.canonicalOrganisation(t))
	reviewer := &CorporateChangeReviewer{Orgs: e.repo, Eligibility: f.resolver}
	later := e.at.Add(time.Hour)

	expectErr := func(name string, err error, fragment string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), fragment) {
			t.Errorf("%s: err=%v; want it to mention %q", name, err, fragment)
		}
	}
	expectErr("end before effect", e.repo.EndCorporateRelationship(e.ctx, f.ownsSub, e.at.Add(-time.Hour), "backdated", actor()), "before it took effect")
	expectErr("end without reason", e.repo.EndCorporateRelationship(e.ctx, f.ownsSub, later, " ", actor()), "requires an effective time and a reason")
	expectErr("conflict without evidence", e.repo.MarkCorporateRelationshipConflicted(e.ctx, f.ownsSub,
		repository.Conflict{DetectedAt: later, Reason: "hearsay"}, actor()), "conflicting evidence references")
	expectErr("unknown relationship", e.repo.EndPlatformRelationship(e.ctx, domain.NewResourceID(domain.PlatformRelationshipIDPrefix), later, "x", actor()), "not found")

	_, err := reviewer.TerminateAffiliate(e.ctx, AffiliateTermination{PlatformRelationshipID: f.affiliate, At: later, Reason: "x"}, actor())
	expectErr("termination without decision", err, "decision reference")
	_, err = reviewer.TerminateAffiliate(e.ctx, AffiliateTermination{PlatformRelationshipID: f.affiliate, At: later, Reason: "x",
		DecisionReference: "gov_1", ReclassifyAs: domain.PlatformRelOwner}, actor())
	expectErr("privileged reclassification", err, "cannot be reclassified as PLATFORM_OWNER")
	owners, err := e.repo.ListPlatformRelationships(e.ctx, f.owner, later)
	if err != nil || len(owners) != 1 {
		t.Fatalf("owner relationships: %+v %v", owners, err)
	}
	_, err = reviewer.TerminateAffiliate(e.ctx, AffiliateTermination{PlatformRelationshipID: owners[0].ID, At: later, Reason: "x", DecisionReference: "gov_2"}, actor())
	expectErr("terminating a non-affiliate", err, "not PLATFORM_GROUP_AFFILIATE")

	// Ending a relationship that never took effect is audited, not published.
	pending, err := e.repo.EnsureCorporateRelationship(e.ctx, domain.CorporateRelationship{
		SourceOrganisationID: f.owner, TargetOrganisationID: e.canonicalOrganisation(t), RelationshipType: domain.CorpRelControls,
		DirectOrDerived: domain.CorporateFactDirect, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "admission-review"}, actor())
	if err != nil {
		t.Fatal(err)
	}
	withdrawing := actor()
	if err := e.repo.EndCorporateRelationship(e.ctx, pending, later, "claim withdrawn", withdrawing); err != nil {
		t.Fatal(err)
	}
	var audited int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM audit_events WHERE correlation_id=$1::uuid AND action='corporate_relationship.ended'`,
		withdrawing.CorrelationID).Scan(&audited); err != nil || audited != 1 || len(e.outboxTypes(t, withdrawing.CorrelationID)) != 0 {
		t.Fatalf("withdrawing a pending claim: audit=%d err=%v events=%v", audited, err, e.outboxTypes(t, withdrawing.CorrelationID))
	}
	expectErr("ending twice", e.repo.EndCorporateRelationship(e.ctx, pending, later, "again", actor()), "not live")
	expectErr("conflict on an ended fact", e.repo.MarkCorporateRelationshipConflicted(e.ctx, pending,
		repository.Conflict{References: []string{"evd"}, DetectedAt: later, Reason: "late"}, actor()), "not live")
}
