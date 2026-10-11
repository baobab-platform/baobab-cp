package organisation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fixtureFoundingSnapshot map[string]FoundingTenantSnapshot

func (f fixtureFoundingSnapshot) SnapshotFoundingEntity(_ context.Context, id string, _ time.Time) (FoundingTenantSnapshot, error) {
	return f[id], nil
}
func syntheticFoundingRegistry(t *testing.T) FirstPartyRegistry {
	t.Helper()
	raw := []byte(`schema:
  name: baobab-platform-legal-entity-registry
entities:
  - id: NABHOLD
    legal_name: "Synthetic Holding Ltd"
    role: holding_company
    identity_class: LEGAL_PERSON
    incorporation_claim: REGISTERED_EVIDENCED
  - id: THAMANI-GLOBAL
    legal_name: "Synthetic Logistics Ltd"
    role: subsidiary
    identity_class: LEGAL_PERSON
    incorporation_claim: REGISTERED_EVIDENCED
  - id: ZURIBEANS
    legal_name: "Synthetic Trade"
    role: subsidiary
    identity_class: OPERATING_BUSINESS
    incorporation_claim: NOT_INCORPORATED
  - id: EQUATOR-ESTATE
    legal_name: "Synthetic Property"
    role: subsidiary
    identity_class: OPERATING_BUSINESS
    incorporation_claim: NOT_INCORPORATED
`)
	reg, err := ParseFirstPartyRegistry(raw)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}
func TestPEO04BQuadrupleFoundingIdentityTenantReadinessNoInventedCompany(t *testing.T) {
	reg := syntheticFoundingRegistry(t)
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	f := fixtureFoundingSnapshot{}
	for i, entity := range reg.Entities {
		id := "00000000-0000-7000-8000-00000000000" + string(rune('1'+i))
		f[entity.ID] = FoundingTenantSnapshot{OrganisationID: id,
			IdentityClass: entity.IdentityClass, IncorporationClaim: entity.IncorporationClaim,
			TenantIDs: []string{"tn_synthetic_" + entity.ID}, PrimaryEnforced: true,
			LegalProfileCount: 0, LegalProfileVerified: false,
			SponsorshipEligible: false, ActiveMandateCount: 0}
	}
	rows, err := QualifyFoundingRoster(context.Background(), reg, f, now)
	if err != nil || len(rows) != 4 {
		t.Fatalf("four independently recognised businesses required: %v %+v", err, rows)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if !row.OrganisationRecognised || !row.TenantPrimaryIsolated {
			t.Fatalf("missing distinct founding identity and tenant: %+v", row)
		}
		if seen[row.TenantID] {
			t.Fatalf("shared tenant reused: %s", row.TenantID)
		}
		seen[row.TenantID] = true
		if row.Readiness != "PENDING" {
			t.Fatalf("unverified company or mandate prematurely admitted: %+v", row)
		}
		if row.IdentityClass == "OPERATING_BUSINESS" && row.CompanyVerificationRecorded {
			t.Fatal("unincorporated business became a verified legal person")
		}
	}
}

func TestPEO04BRejectsCrossTenantAndCorporateProfileFabrication(t *testing.T) {
	reg := syntheticFoundingRegistry(t)
	now := time.Now().UTC()
	f := fixtureFoundingSnapshot{}
	for i, e := range reg.Entities {
		org := "00000000-0000-7000-8000-00000000000" + string(rune('1'+i))
		f[e.ID] = FoundingTenantSnapshot{OrganisationID: org, IdentityClass: e.IdentityClass, IncorporationClaim: e.IncorporationClaim,
			TenantIDs: []string{"tn_" + e.ID}, PrimaryEnforced: true, LegalProfileCount: 0}
	}
	dup := f["ZURIBEANS"]
	dup.TenantIDs = f["NABHOLD"].TenantIDs
	f["ZURIBEANS"] = dup
	if _, err := QualifyFoundingRoster(context.Background(), reg, f, now); !errors.Is(err, ErrFoundingRosterIntegrity) {
		t.Fatalf("shared tenant accepted: %v", err)
	}
	repaired := dup
	repaired.TenantIDs = []string{"tn_zuribeans"}
	repaired.LegalProfileCount = 1
	f["ZURIBEANS"] = repaired
	rows, err := QualifyFoundingRoster(context.Background(), reg, f, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.FirstPartyID == "ZURIBEANS" && (!strings.Contains(strings.Join(row.Missing, ","), "FALSE_OR_LEGACY_INCORPORATION_PROFILE") || row.Readiness != "PENDING") {
			t.Fatalf("ZuriBeans legacy false incorporation not blocked: %+v", row)
		}
	}
}

func TestPEO04BRequiresIndependentSponsorshipMandateAndCompanyReview(t *testing.T) {
	reg := syntheticFoundingRegistry(t)
	now := time.Now().UTC()
	f := fixtureFoundingSnapshot{}
	for i, e := range reg.Entities {
		f[e.ID] = FoundingTenantSnapshot{OrganisationID: "00000000-0000-7000-8000-00000000000" + string(rune('1'+i)),
			IdentityClass: e.IdentityClass, IncorporationClaim: e.IncorporationClaim,
			TenantIDs: []string{"tn_" + e.ID}, PrimaryEnforced: true, LegalProfileCount: 0,
			LegalProfileVerified: e.IdentityClass == "LEGAL_PERSON",
			SponsorshipEligible:  e.IdentityClass == "OPERATING_BUSINESS",
			ActiveMandateCount:   1}
	}
	rows, err := QualifyFoundingRoster(context.Background(), reg, f, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Readiness != "IDENTITY_AND_TENANT_GATES_SATISFIED_NOT_PRODUCTION" {
			t.Fatalf("governed evidence-complete fixture incorrectly pending: %+v", row)
		}
		if strings.Contains(row.Readiness, "PRODUCTION_READY") {
			t.Fatal("tenant readiness was treated as provider certification")
		}
	}
}
