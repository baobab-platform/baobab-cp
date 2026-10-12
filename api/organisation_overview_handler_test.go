package api

import (
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestProjectOrganisationOverviewPreservesAuthorityWithoutRawEvidence(t *testing.T) {
	id := "0199d000-1111-7777-8000-111111111111"
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	verified := now.Add(-time.Hour)
	profile := &domain.Organisation{
		CanonicalEntityID: id, DisplayName: "Example operating organisation",
		VerificationState:  domain.VerificationUnverified,
		Identifiers:        []domain.OrganisationIdentifier{{}},
		EvidenceReferences: []string{"private-evidence-ref"},
		Metadata:           map[string]any{"sensitive": "not-published"},
	}
	legal := []domain.LegalEntityProfile{{
		OrganisationID: id, LegalEntityID: "le_0199d000111177778000222222222222",
		LegalName: "Example Pty Ltd", JurisdictionOfIncorporation: "ZA",
		LegalStatus: domain.LegalStatusActive, VerificationState: domain.VerificationVerified,
		SourceAuthority: "certified-source", EvidenceReferences: []string{"ev_private"},
		VerifiedAt: &verified,
	}}
	corporate := []domain.CorporateRelationship{{
		ID: "cor_1", SourceOrganisationID: id, TargetOrganisationID: "org_other",
		VerificationState: domain.VerificationPendingReview, RelationshipType: domain.CorpRelOwns,
		Status: "PENDING", DirectOrDerived: "DIRECT", SourceAuthority: "applicant",
		EffectiveFrom: now,
	}}
	out, ok := projectOrganisationOverview(id, now, profile, legal, corporate, nil, nil)
	if !ok {
		t.Fatal("expected an owned profile to render")
	}
	if out.Profile == nil || len(out.Profile.EvidenceReferences) > 0 || len(out.Profile.Identifiers) > 0 || len(out.Profile.Metadata) > 0 {
		t.Fatal("raw evidence and person identifiers must not leak")
	}
	if got := out.LegalEntities[0]; got.VerificationState != domain.VerificationVerified || got.EvidenceCount != 1 || got.VerifiedAt == nil {
		t.Fatalf("verification provenance lost: %+v", got)
	}
	if out.CorporateRelationships[0].VerificationState != domain.VerificationPendingReview {
		t.Fatal("applicant corporate claim must never become verified")
	}
}

func TestProjectOrganisationOverviewRejectsCrossSubjectAndUnboundedRows(t *testing.T) {
	id := "0199d000-1111-7777-8000-111111111111"
	profile := &domain.Organisation{CanonicalEntityID: id}
	now := time.Now().UTC()
	tests := []struct {
		name     string
		legal    []domain.LegalEntityProfile
		corp     []domain.CorporateRelationship
		platform []domain.PlatformRelationship
		groups   []domain.CorporateGroupMembership
	}{
		{name: "foreign legal entity", legal: []domain.LegalEntityProfile{{OrganisationID: "org_foreign"}}},
		{name: "foreign corporate link", corp: []domain.CorporateRelationship{{SourceOrganisationID: "org_foreign", TargetOrganisationID: "org_other"}}},
		{name: "foreign platform affiliation", platform: []domain.PlatformRelationship{{OrganisationID: "org_foreign"}}},
		{name: "foreign group member", groups: []domain.CorporateGroupMembership{{OrganisationID: "org_foreign"}}},
		{name: "unbounded legal rows", legal: make([]domain.LegalEntityProfile, maxOverviewRows+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := projectOrganisationOverview(id, now, profile, tc.legal, tc.corp, tc.groups, tc.platform)
			if ok {
				t.Fatal("unauthorised source composition must fail closed")
			}
		})
	}
}
