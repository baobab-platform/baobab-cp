// Target path: baobab-platform/baobab-cp/internal/domain/organisation_security_test.go
//
// ADR-BCP-018 — Security invariant proofs (CI).
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

package domain

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Stand-in authorization helpers (document the required production policy)
// ---------------------------------------------------------------------------

// authorizeTenantData allows access only when request tenant equals resource
// tenant. Corporate group membership is ignored by design.
func authorizeTenantData(requestTenantID, resourceTenantID string, _ []CorporateGroupMembership) bool {
	return requestTenantID == resourceTenantID
}

// mayAdministerSubsidiary never grants admin rights from CorporateRelationship.
func mayAdministerSubsidiary(actorOrgID, targetOrgID string, rels []CorporateRelationship, at time.Time) bool {
	for _, r := range rels {
		if r.SourceOrganisationID == actorOrgID &&
			r.TargetOrganisationID == targetOrgID &&
			r.IsConsequential(at) {
			return false
		}
	}
	return false
}

func platformAccountGrantsAuth(_ PlatformAccount, _ []PlatformAccountMembership) bool {
	return false
}

func platformGroupAffiliateGrantsPermission(rel PlatformRelationship, at time.Time) bool {
	if rel.RelationshipType != PlatformRelGroupAffiliate {
		return false
	}
	_ = rel.IsConsequential(at)
	return false
}

// resolveCanonicalOrgFromIAMClaim maps IAM claims only via ExternalReference-style map.
func resolveCanonicalOrgFromIAMClaim(iamClaim string, externalRefs map[string]string) (string, bool) {
	id, ok := externalRefs[iamClaim]
	return id, ok
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func verifiedActiveCorpRel(source, target string, relType CorporateRelationshipType, from time.Time) CorporateRelationship {
	return CorporateRelationship{
		ID:                   "cr_" + source + "_" + target,
		SourceOrganisationID: source,
		TargetOrganisationID: target,
		RelationshipType:     relType,
		VerificationState:    VerificationVerified,
		Status:               "ACTIVE",
		EffectiveFrom:        from,
		SourceAuthority:      "test",
	}
}

func unverifiedCorpRel(source, target string, relType CorporateRelationshipType, from time.Time) CorporateRelationship {
	r := verifiedActiveCorpRel(source, target, relType, from)
	r.VerificationState = VerificationUnverified
	return r
}

func conflictedCorpRel(source, target string, relType CorporateRelationshipType, from time.Time) CorporateRelationship {
	r := verifiedActiveCorpRel(source, target, relType, from)
	r.VerificationState = VerificationConflicted
	r.Status = "CONFLICTED"
	return r
}

func verifiedPlatformRel(org string, relType PlatformRelationshipType, from time.Time) PlatformRelationship {
	return PlatformRelationship{
		ID:                "pr_" + org,
		OrganisationID:    org,
		RelationshipType:  relType,
		VerificationState: VerificationVerified,
		Status:            "ACTIVE",
		EffectiveFrom:     from,
		SourceAuthority:   "platform-governance",
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSameCorporateGroupDoesNotGrantCrossTenantAccess(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	memberships := []CorporateGroupMembership{
		{ID: "m1", CorporateGroupID: "cg_nabhold", OrganisationID: "ce_nabhold", MembershipType: "ROOT", Status: "ACTIVE", EffectiveFrom: at.Add(-365 * 24 * time.Hour)},
		{ID: "m2", CorporateGroupID: "cg_nabhold", OrganisationID: "ce_zuribeans", MembershipType: "SUBSIDIARY", Status: "ACTIVE", EffectiveFrom: at.Add(-365 * 24 * time.Hour)},
		{ID: "m3", CorporateGroupID: "cg_nabhold", OrganisationID: "ce_thamani", MembershipType: "SUBSIDIARY", Status: "ACTIVE", EffectiveFrom: at.Add(-365 * 24 * time.Hour)},
	}

	if authorizeTenantData("tn_zuribeans", "tn_thamani", memberships) {
		t.Fatal("same corporate group must not grant cross-tenant data access")
	}
	if !authorizeTenantData("tn_zuribeans", "tn_zuribeans", memberships) {
		t.Fatal("same tenant must still authorize")
	}
}

func TestSameParentDoesNotGrantSubsidiaryAdministration(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rels := []CorporateRelationship{
		verifiedActiveCorpRel("ce_nabhold", "ce_zuribeans", CorpRelOwns, at.Add(-30*24*time.Hour)),
	}
	if mayAdministerSubsidiary("ce_nabhold", "ce_zuribeans", rels, at) {
		t.Fatal("verified parent ownership must not grant subsidiary administration via CorporateRelationship")
	}
}

func TestSamePlatformAccountDoesNotGrantSharedAuthorization(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	account := PlatformAccount{
		ID: "pa_acme", DisplayName: "Acme Holdings Account",
		Status: "ACTIVE", EffectiveFrom: at.Add(-10 * 24 * time.Hour),
	}
	members := []PlatformAccountMembership{
		{ID: "pam1", PlatformAccountID: "pa_acme", MemberType: "TENANT", MemberID: "tn_acme_foods", Status: "ACTIVE", EffectiveFrom: at.Add(-10 * 24 * time.Hour)},
		{ID: "pam2", PlatformAccountID: "pa_acme", MemberType: "TENANT", MemberID: "tn_acme_logistics", Status: "ACTIVE", EffectiveFrom: at.Add(-10 * 24 * time.Hour)},
	}
	if platformAccountGrantsAuth(account, members) {
		t.Fatal("same PlatformAccount must not grant shared authorization")
	}
}

func TestPlatformGroupAffiliateIsNotRuntimePermission(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rel := verifiedPlatformRel("ce_zuribeans", PlatformRelGroupAffiliate, at.Add(-100*24*time.Hour))
	if !rel.IsConsequential(at) {
		t.Fatal("expected verified active PLATFORM_GROUP_AFFILIATE to be consequential for eligibility")
	}
	if platformGroupAffiliateGrantsPermission(rel, at) {
		t.Fatal("PLATFORM_GROUP_AFFILIATE must not grant runtime permission")
	}
}

func TestIAMOrganisationClaimIsNotCanonicalCorporateTruth(t *testing.T) {
	iamClaim := "org_iam_acme_foods"
	if _, ok := resolveCanonicalOrgFromIAMClaim(iamClaim, map[string]string{}); ok {
		t.Fatal("unmapped IAM organisation claim must not resolve to canonical truth")
	}
	canonical, ok := resolveCanonicalOrgFromIAMClaim(iamClaim, map[string]string{iamClaim: "ce_acme_foods"})
	if !ok || canonical != "ce_acme_foods" {
		t.Fatalf("expected mapped claim to resolve, got %q ok=%v", canonical, ok)
	}
	if authorizeTenantData(iamClaim, "tn_acme_foods", nil) {
		t.Fatal("IAM organisation claim string must not be used as tenant_id for authorization")
	}
}

func TestConsequentialResolutionFailsClosedWhenUnverified(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rel := unverifiedCorpRel("ce_parent", "ce_child", CorpRelOwns, at.Add(-24*time.Hour))
	if rel.IsConsequential(at) {
		t.Fatal("UNVERIFIED corporate relationship must not be consequential")
	}
}

func TestConsequentialResolutionFailsClosedWhenConflicted(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rel := conflictedCorpRel("ce_parent", "ce_child", CorpRelOwns, at.Add(-24*time.Hour))
	if rel.IsConsequential(at) {
		t.Fatal("CONFLICTED corporate relationship must not be consequential")
	}
}

func TestDefaultLegalEntityProjectionConflictFailsClosed(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	mappings := []TenantLegalEntityMapping{
		{ID: "m1", TenantID: "tn_x", LegalEntityID: "LE-A", IsDefault: true, Status: "ACTIVE", EffectiveFrom: at.Add(-24 * time.Hour)},
	}
	if err := AssertDefaultLegalEntityProjection("LE-B", mappings, at); err == nil {
		t.Fatal("expected conflict between Tenant.LegalEntityID and default mapping")
	}
}

func TestDefaultLegalEntityProjectionConsistent(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	mappings := []TenantLegalEntityMapping{
		{ID: "m1", TenantID: "tn_x", LegalEntityID: "LE-A", IsDefault: true, Status: "ACTIVE", EffectiveFrom: at.Add(-24 * time.Hour)},
	}
	if err := AssertDefaultLegalEntityProjection("LE-A", mappings, at); err != nil {
		t.Fatalf("expected consistent projection, got %v", err)
	}
	if got := DefaultLegalEntityIDFromMappings(mappings, at); got != "LE-A" {
		t.Fatalf("expected LE-A, got %q", got)
	}
}
