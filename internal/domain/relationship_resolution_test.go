// Target path: baobab-platform/baobab-cp/internal/domain/relationship_resolution_test.go
//
// ADR-BCP-018 / ADR-BCP-017 — unit tests for fail-closed INTERNAL eligibility.

package domain

import (
	"testing"
	"time"
)

func TestDeriveInternalEligibility_Owner(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	pr := PlatformRelationship{
		ID:                "pr1",
		OrganisationID:    "ce_nabhold",
		RelationshipType:  PlatformRelOwner,
		VerificationState: VerificationVerified,
		Status:            "ACTIVE",
		EffectiveFrom:     at.Add(-24 * time.Hour),
		SourceAuthority:   "platform-governance",
	}
	ev := InternalEligibilityEvidence{
		OrganisationID:       "ce_nabhold",
		PlatformRelationship: &pr,
		EvaluatedAt:          at,
	}
	if !DeriveInternalEligibility(ev, nil) {
		t.Fatal("PLATFORM_OWNER with verified relationship must be INTERNAL eligible")
	}
}

func TestDeriveInternalEligibility_GroupAffiliateRequiresCorpEdge(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	pr := PlatformRelationship{
		ID:                "pr2",
		OrganisationID:    "ce_zuribeans",
		RelationshipType:  PlatformRelGroupAffiliate,
		VerificationState: VerificationVerified,
		Status:            "ACTIVE",
		EffectiveFrom:     at.Add(-24 * time.Hour),
		SourceAuthority:   "platform-governance",
	}
	owners := map[string]struct{}{"ce_nabhold": {}}

	// No corporate edge → deny
	ev := InternalEligibilityEvidence{
		OrganisationID:       "ce_zuribeans",
		PlatformRelationship: &pr,
		EvaluatedAt:          at,
	}
	if DeriveInternalEligibility(ev, owners) {
		t.Fatal("PLATFORM_GROUP_AFFILIATE without corporate edge must fail closed")
	}

	// Verified OWNS edge from owner → allow eligibility (not authorization)
	rel := CorporateRelationship{
		ID:                   "cr1",
		SourceOrganisationID: "ce_nabhold",
		TargetOrganisationID: "ce_zuribeans",
		RelationshipType:     CorpRelOwns,
		VerificationState:    VerificationVerified,
		Status:               "ACTIVE",
		EffectiveFrom:        at.Add(-48 * time.Hour),
		SourceAuthority:      "nabhold-governance",
	}
	ev.CorporateRelationships = []CorporateRelationship{rel}
	if !DeriveInternalEligibility(ev, owners) {
		t.Fatal("PLATFORM_GROUP_AFFILIATE with verified corporate edge to owner must be eligible")
	}
}

func TestDeriveInternalEligibility_UnverifiedPlatformFailsClosed(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	pr := PlatformRelationship{
		ID:                "pr3",
		OrganisationID:    "ce_x",
		RelationshipType:  PlatformRelOwner,
		VerificationState: VerificationUnverified,
		Status:            "ACTIVE",
		EffectiveFrom:     at.Add(-24 * time.Hour),
		SourceAuthority:   "test",
	}
	ev := InternalEligibilityEvidence{
		OrganisationID:       "ce_x",
		PlatformRelationship: &pr,
		EvaluatedAt:          at,
	}
	if DeriveInternalEligibility(ev, nil) {
		t.Fatal("UNVERIFIED platform relationship must fail closed")
	}
}

func TestDeriveInternalEligibility_ExternalClientNeverInternal(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	pr := PlatformRelationship{
		ID:                "pr4",
		OrganisationID:    "ce_acme",
		RelationshipType:  PlatformRelExternalClient,
		VerificationState: VerificationVerified,
		Status:            "ACTIVE",
		EffectiveFrom:     at.Add(-24 * time.Hour),
		SourceAuthority:   "control-plane-admission",
	}
	ev := InternalEligibilityEvidence{
		OrganisationID:       "ce_acme",
		PlatformRelationship: &pr,
		EvaluatedAt:          at,
	}
	if DeriveInternalEligibility(ev, map[string]struct{}{"ce_nabhold": {}}) {
		t.Fatal("EXTERNAL_CLIENT must not derive INTERNAL eligibility")
	}
}

func TestFilterConsequentialCorporateRelationships(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	rels := []CorporateRelationship{
		{
			ID: "ok", SourceOrganisationID: "a", TargetOrganisationID: "b",
			RelationshipType: CorpRelOwns, VerificationState: VerificationVerified,
			Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour), SourceAuthority: "t",
		},
		{
			ID: "bad", SourceOrganisationID: "a", TargetOrganisationID: "c",
			RelationshipType: CorpRelOwns, VerificationState: VerificationConflicted,
			Status: "CONFLICTED", EffectiveFrom: at.Add(-time.Hour), SourceAuthority: "t",
		},
	}
	out := FilterConsequentialCorporateRelationships(rels, at)
	if len(out) != 1 || out[0].ID != "ok" {
		t.Fatalf("expected only verified active relationship, got %+v", out)
	}
}
