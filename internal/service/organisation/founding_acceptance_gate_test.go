package organisation

import (
	"context"
	"errors"
	"testing"
	"time"
)

type acceptanceEvidenceFixture struct {
	result FoundingCompanyReconciliation
	err    error
	calls  int
}

func (f *acceptanceEvidenceFixture) Reconcile(_ context.Context, _, _ FoundingIdentityInput) (FoundingCompanyReconciliation, error) {
	f.calls++
	return f.result, f.err
}

func acceptanceFixture(t *testing.T) (FoundingAcceptanceGate, FirstPartyRegistry, FoundingIdentityInput, FoundingIdentityInput) {
	t.Helper()
	reg := syntheticFoundingRegistry(t)
	now := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	snapshots := fixtureFoundingSnapshot{}
	ids := map[string]string{
		"NABHOLD":        "00000000-0000-7000-8000-000000000001",
		"THAMANI-GLOBAL": "00000000-0000-7000-8000-000000000002",
		"ZURIBEANS":      "00000000-0000-7000-8000-000000000003",
		"EQUATOR-ESTATE": "00000000-0000-7000-8000-000000000004",
	}
	for _, e := range reg.Entities {
		legal := e.IdentityClass == "LEGAL_PERSON"
		snapshots[e.ID] = FoundingTenantSnapshot{
			OrganisationID: ids[e.ID], IdentityClass: e.IdentityClass,
			IncorporationClaim: e.IncorporationClaim, TenantIDs: []string{"tn_" + e.ID},
			PrimaryEnforced: true, LegalProfileVerified: legal,
			LegalProfileCount: func() int {
				if legal {
					return 1
				}
				return 0
			}(),
			SponsorshipEligible: !legal, ActiveMandateCount: 1,
		}
	}
	proof := &acceptanceEvidenceFixture{result: FoundingCompanyReconciliation{
		NabholdRegistrationVerified: true, ThamaniRegistrationVerified: true,
		NabholdRegistrationEvidence: "vres_nabhold_independent",
		ThamaniRegistrationEvidence: "vres_thamani_independent",
		AssessedAt:                  now,
	}}
	gate := FoundingAcceptanceGate{Snapshot: snapshots, Evidence: proof, Now: func() time.Time { return now }}
	return gate, reg, FoundingIdentityInput{
		RegistryID: "NABHOLD", OrganisationID: ids["NABHOLD"],
		VerificationCaseID:         "vcase_parent_synthetic",
		DeclaredRegistrationNumber: "synthetic-nabhold-registration",
	}, FoundingIdentityInput{
		RegistryID: "THAMANI-GLOBAL", OrganisationID: ids["THAMANI-GLOBAL"],
		VerificationCaseID:         "vcase_child_synthetic",
		DeclaredRegistrationNumber: "synthetic-thamani-registration",
	}
}

func TestPEO04CIdentityAdmissionDoesNotDependOnAutomaticallyInferredShareholding(t *testing.T) {
	gate, reg, parent, child := acceptanceFixture(t)
	got, err := gate.Assess(context.Background(), reg, parent, child)
	if err != nil || !got.GroupIdentityAndTenantReady || got.IndependentControlVerified || len(got.Entries) != 4 {
		t.Fatalf("independent founding tenant admission rejected without ownership grant: %+v %v", got, err)
	}
	tenants := map[string]bool{}
	for _, entry := range got.Entries {
		if entry.Readiness != "IDENTITY_TENANT_ACCEPTED_NOT_PRODUCTION" || tenants[entry.TenantID] {
			t.Fatalf("unqualified or nonisolated founding tenant: %+v", entry)
		}
		tenants[entry.TenantID] = true
	}
}

func TestPEO04CNoLegalStatusInferenceFromFirstPartyOrSyntheticProfile(t *testing.T) {
	gate, reg, parent, child := acceptanceFixture(t)
	evidence := gate.Evidence.(*acceptanceEvidenceFixture)
	evidence.result.ThamaniRegistrationVerified = false
	evidence.result.ThamaniRegistrationEvidence = ""
	got, err := gate.Assess(context.Background(), reg, parent, child)
	if err != nil || got.GroupIdentityAndTenantReady {
		t.Fatalf("missing real Thamani CIPC accepted: %+v %v", got, err)
	}
	pending := false
	for _, entry := range got.Entries {
		if entry.FirstPartyID == "THAMANI-GLOBAL" {
			pending = entry.Readiness == "PENDING" && len(entry.Missing) > 0
		}
	}
	if !pending {
		t.Fatal("Thamani CIPC gap not explicitly surfaced")
	}
	evidence.result.ThamaniRegistrationVerified = true
	evidence.result.ThamaniRegistrationEvidence = "vres_child"
	gate.Snapshot = fixtureFoundingSnapshot{
		"NABHOLD": {OrganisationID: parent.OrganisationID, IdentityClass: "LEGAL_PERSON",
			IncorporationClaim: "REGISTERED_EVIDENCED", TenantIDs: []string{"tn_shared"}, PrimaryEnforced: true,
			LegalProfileVerified: true},
		"THAMANI-GLOBAL": {OrganisationID: child.OrganisationID, IdentityClass: "LEGAL_PERSON",
			IncorporationClaim: "REGISTERED_EVIDENCED", TenantIDs: []string{"tn_shared"}, PrimaryEnforced: true,
			LegalProfileVerified: true},
		"ZURIBEANS":      {OrganisationID: "org_zb", IdentityClass: "OPERATING_BUSINESS", IncorporationClaim: "NOT_INCORPORATED"},
		"EQUATOR-ESTATE": {OrganisationID: "org_eq", IdentityClass: "OPERATING_BUSINESS", IncorporationClaim: "NOT_INCORPORATED"},
	}
	if _, err := gate.Assess(context.Background(), reg, parent, child); !errors.Is(err, ErrFoundingRosterIntegrity) {
		t.Fatalf("shared tenant identity not rejected: %v", err)
	}
}

func TestPEO04CRejectsRegistryMutationAndCaseRebinding(t *testing.T) {
	gate, reg, parent, child := acceptanceFixture(t)
	parent.OrganisationID = child.OrganisationID
	if _, err := gate.Assess(context.Background(), reg, parent, child); !errors.Is(err, ErrFoundingAcceptance) {
		t.Fatalf("cross-company case rebind allowed: %v", err)
	}
	_, reg, parent, child = acceptanceFixture(t)
	reg.Entities = reg.Entities[:3]
	if _, err := gate.Assess(context.Background(), reg, parent, child); !errors.Is(err, ErrFoundingAcceptance) {
		t.Fatalf("three-member roster passed four-founder gate: %v", err)
	}
}

func TestPEO04CRejectsExpiredOrStaleIndependentSourceAssessment(t *testing.T) {
	gate, reg, parent, child := acceptanceFixture(t)
	proof := gate.Evidence.(*acceptanceEvidenceFixture)
	proof.result.AssessedAt = gate.Now().Add(-2 * time.Minute)
	if _, err := gate.Assess(context.Background(), reg, parent, child); !errors.Is(err, ErrFoundingAcceptance) {
		t.Fatalf("stale OEV-03 result could admit founded tenant: %v", err)
	}
	proof.result.AssessedAt = gate.Now().Add(2 * time.Second)
	if _, err := gate.Assess(context.Background(), reg, parent, child); !errors.Is(err, ErrFoundingAcceptance) {
		t.Fatalf("future official source result accepted: %v", err)
	}
}
