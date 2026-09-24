package domain

import (
	"testing"
	"time"
)

func TestDeriveInternalEligibility_Owner(t *testing.T) {
	at := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	ok := DeriveInternalEligibility(InternalEligibilityEvidence{
		OrganisationID: "org-1",
		PlatformRelationships: []PlatformRelationship{{
			OrganisationID: "org-1", PlatformID: "p", RelationshipType: PlatformRelOwner,
			VerificationState: VerificationVerified, Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour),
			SourceAuthority: "platform-governance",
		}},
		EvaluatedAt: at,
	}, nil)
	if !ok {
		t.Fatal("PLATFORM_OWNER must be INTERNAL eligible")
	}
}

func TestDeriveInternalEligibility_ExternalDenied(t *testing.T) {
	at := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	ok := DeriveInternalEligibility(InternalEligibilityEvidence{
		OrganisationID: "org-ext",
		PlatformRelationships: []PlatformRelationship{{
			OrganisationID: "org-ext", PlatformID: "p", RelationshipType: PlatformRelExternalClient,
			VerificationState: VerificationVerified, Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour),
			SourceAuthority: "control-plane-admission",
		}},
		EvaluatedAt: at,
	}, map[string]struct{}{"org-owner": {}})
	if ok {
		t.Fatal("EXTERNAL_CLIENT must not be INTERNAL eligible")
	}
}

func TestDeriveInternalEligibility_AffiliateRequiresOwnsControls(t *testing.T) {
	at := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	// JV only — must not grant
	ok := DeriveInternalEligibility(InternalEligibilityEvidence{
		OrganisationID: "org-aff",
		PlatformRelationships: []PlatformRelationship{{
			OrganisationID: "org-aff", PlatformID: "p", RelationshipType: PlatformRelGroupAffiliate,
			VerificationState: VerificationVerified, Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour),
			SourceAuthority: "platform-governance",
		}},
		CorporateRelationships: []CorporateRelationship{{
			SourceOrganisationID: "org-owner", TargetOrganisationID: "org-aff",
			RelationshipType: CorpRelJointVentureWith, VerificationState: VerificationVerified,
			Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour), SourceAuthority: "board",
		}},
		EvaluatedAt: at,
	}, map[string]struct{}{"org-owner": {}})
	if ok {
		t.Fatal("JOINT_VENTURE_WITH must not satisfy affiliate INTERNAL path")
	}
	// OWNS — grants
	ok = DeriveInternalEligibility(InternalEligibilityEvidence{
		OrganisationID: "org-aff",
		PlatformRelationships: []PlatformRelationship{{
			OrganisationID: "org-aff", PlatformID: "p", RelationshipType: PlatformRelGroupAffiliate,
			VerificationState: VerificationVerified, Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour),
			SourceAuthority: "platform-governance",
		}},
		CorporateRelationships: []CorporateRelationship{{
			SourceOrganisationID: "org-owner", TargetOrganisationID: "org-aff",
			RelationshipType: CorpRelOwns, VerificationState: VerificationVerified,
			Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour), SourceAuthority: "board",
		}},
		EvaluatedAt: at,
	}, map[string]struct{}{"org-owner": {}})
	if !ok {
		t.Fatal("verified OWNS to platform owner must satisfy affiliate INTERNAL path")
	}
}

func TestIsConsequential_FailClosed(t *testing.T) {
	at := time.Now().UTC()
	rel := CorporateRelationship{
		RelationshipType: CorpRelOwns, VerificationState: VerificationUnverified,
		Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour), SourceAuthority: "x",
	}
	if rel.IsConsequential(at) {
		t.Fatal("UNVERIFIED must not be consequential")
	}
}
