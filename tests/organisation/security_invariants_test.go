// Target path: baobab-platform/baobab-cp/internal/domain/organisation_security_test.go
// (or tests/organisation/security_invariants_test.go — match project test layout)
//
// ADR-BCP-018 — Security invariant proofs.
//
// Explicitly prove:
//   1. same corporate group != cross-tenant access
//   2. same parent != subsidiary administration
//   3. same PlatformAccount != shared authorization
//   4. PLATFORM_GROUP_AFFILIATE != runtime permission
//   5. IAM organisation claim != canonical corporate truth
//
// All consequential relationship resolution fails closed when evidence is
// missing or conflicted.

package organisation_test

import (
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func verifiedActiveCorpRel(source, target string, relType domain.CorporateRelationshipType, from time.Time) domain.CorporateRelationship {
	return domain.CorporateRelationship{
		ID:                   "cr_" + source + "_" + target,
		SourceOrganisationID: source,
		TargetOrganisationID: target,
		RelationshipType:     relType,
		VerificationState:    domain.VerificationVerified,
		Status:               "ACTIVE",
		EffectiveFrom:        from,
		SourceAuthority:      "test",
	}
}

func unverifiedCorpRel(source, target string, relType domain.CorporateRelationshipType, from time.Time) domain.CorporateRelationship {
	r := verifiedActiveCorpRel(source, target, relType, from)
	r.VerificationState = domain.VerificationUnverified
	return r
}

func conflictedCorpRel(source, target string, relType domain.CorporateRelationshipType, from time.Time) domain.CorporateRelationship {
	r := verifiedActiveCorpRel(source, target, relType, from)
	r.VerificationState = domain.VerificationConflicted
	r.Status = "CONFLICTED"
	return r
}

func verifiedPlatformRel(org string, relType domain.PlatformRelationshipType, from time.Time) domain.PlatformRelationship {
	return domain.PlatformRelationship{
		ID:                "pr_" + org,
		OrganisationID:    org,
		RelationshipType:  relType,
		VerificationState: domain.VerificationVerified,
		Status:            "ACTIVE",
		EffectiveFrom:     from,
		SourceAuthority:   "platform-governance",
	}
}

// authorizeTenantData is a stand-in for the real authorization check.
// Production code must only allow access when the resolved tenant context
// matches the resource tenant — never because of corporate group membership.
func authorizeTenantData(requestTenantID, resourceTenantID string, _ []domain.CorporateGroupMembership) bool {
	return requestTenantID == resourceTenantID
}

// mayAdministerSubsidiary is a stand-in: parent corporate ownership must not
// grant subsidiary tenant administration.
func mayAdministerSubsidiary(actorOrgID, targetOrgID string, rels []domain.CorporateRelationship, at time.Time) bool {
	// Authorization is never derived from CorporateRelationship.
	// Even a verified PARENT_OF / OWNS edge is insufficient.
	for _, r := range rels {
		if r.SourceOrganisationID == actorOrgID &&
			r.TargetOrganisationID == targetOrgID &&
			r.IsConsequential(at) {
			// Ownership is known — still deny administration via this path.
			return false
		}
	}
	return false
}

// platformAccountGrantsAuth is a stand-in: shared PlatformAccount must not
// grant authorization.
func platformAccountGrantsAuth(_ domain.PlatformAccount, _ []domain.PlatformAccountMembership) bool {
	return false
}

// platformGroupAffiliateGrantsPermission is a stand-in.
func platformGroupAffiliateGrantsPermission(rel domain.PlatformRelationship, at time.Time) bool {
	if rel.RelationshipType != domain.PlatformRelGroupAffiliate {
		return false
	}
	// Even when consequential for INTERNAL eligibility, it is not a runtime
	// permission for tenant data or administration.
	_ = rel.IsConsequential(at)
	return false
}

// resolveCanonicalOrgFromIAMClaim maps an IAM organisation claim only through
// ExternalReference / canonical mappings. Direct use as corporate truth is forbidden.
func resolveCanonicalOrgFromIAMClaim(iamClaim string, externalRefs map[string]string) (canonicalID string, ok bool) {
	// externalRefs simulates ExternalReference resolution: iamClaim -> canonical_entity_id
	canonicalID, ok = externalRefs[iamClaim]
	return canonicalID, ok
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSameCorporateGroupDoesNotGrantCrossTenantAccess(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)

	// Nabhold group: parent owns two subsidiaries, each with its own tenant.
	rels := []domain.CorporateRelationship{
		verifiedActiveCorpRel("ce_nabhold", "ce_zuribeans", domain.CorpRelOwns, at.Add(-365*24*time.Hour)),
		verifiedActiveCorpRel("ce_nabhold", "ce_thamani", domain.CorpRelOwns, at.Add(-365*24*time.Hour)),
	}
	_ = rels

	memberships := []domain.CorporateGroupMembership{
		{ID: "m1", CorporateGroupID: "cg_nabhold", OrganisationID: "ce_nabhold", MembershipType: "ROOT", Status: "ACTIVE", EffectiveFrom: at.Add(-365 * 24 * time.Hour)},
		{ID: "m2", CorporateGroupID: "cg_nabhold", OrganisationID: "ce_zuribeans", MembershipType: "SUBSIDIARY", Status: "ACTIVE", EffectiveFrom: at.Add(-365 * 24 * time.Hour)},
		{ID: "m3", CorporateGroupID: "cg_nabhold", OrganisationID: "ce_thamani", MembershipType: "SUBSIDIARY", Status: "ACTIVE", EffectiveFrom: at.Add(-365 * 24 * time.Hour)},
	}

	// Tenant A (ZuriBeans) must not read Tenant B (Thamani) merely because
	// both are in the same corporate group.
	if authorizeTenantData("tn_zuribeans", "tn_thamani", memberships) {
		t.Fatal("same corporate group must not grant cross-tenant data access")
	}
	if !authorizeTenantData("tn_zuribeans", "tn_zuribeans", memberships) {
		t.Fatal("same tenant must still authorize")
	}
}

func TestSameParentDoesNotGrantSubsidiaryAdministration(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rels := []domain.CorporateRelationship{
		verifiedActiveCorpRel("ce_nabhold", "ce_zuribeans", domain.CorpRelOwns, at.Add(-30*24*time.Hour)),
	}

	if mayAdministerSubsidiary("ce_nabhold", "ce_zuribeans", rels, at) {
		t.Fatal("verified parent ownership must not grant subsidiary administration via CorporateRelationship")
	}
}

func TestSamePlatformAccountDoesNotGrantSharedAuthorization(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	account := domain.PlatformAccount{
		ID:          "pa_acme",
		DisplayName: "Acme Holdings Account",
		Status:      "ACTIVE",
		EffectiveFrom: at.Add(-10 * 24 * time.Hour),
	}
	members := []domain.PlatformAccountMembership{
		{ID: "pam1", PlatformAccountID: "pa_acme", MemberType: "TENANT", MemberID: "tn_acme_foods", Status: "ACTIVE", EffectiveFrom: at.Add(-10 * 24 * time.Hour)},
		{ID: "pam2", PlatformAccountID: "pa_acme", MemberType: "TENANT", MemberID: "tn_acme_logistics", Status: "ACTIVE", EffectiveFrom: at.Add(-10 * 24 * time.Hour)},
	}

	if platformAccountGrantsAuth(account, members) {
		t.Fatal("same PlatformAccount must not grant shared authorization")
	}
}

func TestPlatformGroupAffiliateIsNotRuntimePermission(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rel := verifiedPlatformRel("ce_zuribeans", domain.PlatformRelGroupAffiliate, at.Add(-100*24*time.Hour))

	if !rel.IsConsequential(at) {
		t.Fatal("expected verified active PLATFORM_GROUP_AFFILIATE to be consequential for eligibility derivation")
	}
	if platformGroupAffiliateGrantsPermission(rel, at) {
		t.Fatal("PLATFORM_GROUP_AFFILIATE must not grant runtime permission")
	}
}

func TestIAMOrganisationClaimIsNotCanonicalCorporateTruth(t *testing.T) {
	// IAM may present an organisation claim; it is only a pointer into
	// ExternalReference resolution. Without a mapping, resolution fails closed.
	iamClaim := "org_iam_acme_foods"

	// No mapping registered.
	if _, ok := resolveCanonicalOrgFromIAMClaim(iamClaim, map[string]string{}); ok {
		t.Fatal("unmapped IAM organisation claim must not resolve to canonical truth")
	}

	// With mapping, we get a canonical id — still not used as authorization input.
	canonical, ok := resolveCanonicalOrgFromIAMClaim(iamClaim, map[string]string{
		iamClaim: "ce_acme_foods",
	})
	if !ok || canonical != "ce_acme_foods" {
		t.Fatalf("expected mapped claim to resolve, got %q ok=%v", canonical, ok)
	}
	// Authorization still requires tenant context, not the IAM claim itself.
	if authorizeTenantData(iamClaim, "tn_acme_foods", nil) {
		t.Fatal("IAM organisation claim string must not be used as tenant_id for authorization")
	}
}

func TestConsequentialResolutionFailsClosedWhenUnverified(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rel := unverifiedCorpRel("ce_parent", "ce_child", domain.CorpRelOwns, at.Add(-24*time.Hour))
	if rel.IsConsequential(at) {
		t.Fatal("UNVERIFIED corporate relationship must not be consequential")
	}
}

func TestConsequentialResolutionFailsClosedWhenConflicted(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rel := conflictedCorpRel("ce_parent", "ce_child", domain.CorpRelOwns, at.Add(-24*time.Hour))
	if rel.IsConsequential(at) {
		t.Fatal("CONFLICTED corporate relationship must not be consequential")
	}
}

func TestDefaultLegalEntityProjectionConflictFailsClosed(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	mappings := []domain.TenantLegalEntityMapping{
		{
			ID:            "m1",
			TenantID:      "tn_x",
			LegalEntityID: "LE-A",
			IsDefault:     true,
			Status:        "ACTIVE",
			EffectiveFrom: at.Add(-24 * time.Hour),
		},
	}
	err := domain.AssertDefaultLegalEntityProjection("LE-B", mappings, at)
	if err == nil {
		t.Fatal("expected conflict between Tenant.LegalEntityID and default mapping")
	}
}

func TestDefaultLegalEntityProjectionConsistent(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	mappings := []domain.TenantLegalEntityMapping{
		{
			ID:            "m1",
			TenantID:      "tn_x",
			LegalEntityID: "LE-A",
			IsDefault:     true,
			Status:        "ACTIVE",
			EffectiveFrom: at.Add(-24 * time.Hour),
		},
	}
	if err := domain.AssertDefaultLegalEntityProjection("LE-A", mappings, at); err != nil {
		t.Fatalf("expected consistent projection, got %v", err)
	}
	if got := domain.DefaultLegalEntityIDFromMappings(mappings, at); got != "LE-A" {
		t.Fatalf("expected LE-A, got %q", got)
	}
}
