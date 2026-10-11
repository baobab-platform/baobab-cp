package verification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCIPC struct {
	record CompanyRecord
	err    error
	calls  int
}

func (f *fakeCIPC) LookupCompany(ctx context.Context, ref string) (CompanyRecord, error) {
	f.calls++
	if ref != "2020/123456/07" {
		return CompanyRecord{}, errors.New("unexpected official company reference")
	}
	return f.record, f.err
}

func syntheticCIPCClaim() Claim {
	return Claim{
		ClaimID: "ecl_syntheticcipc01", Subject: Subject{SubjectType: "ORGANISATION", SubjectID: "org_synthetic"},
		ClaimType: "LEGAL_NAME", Jurisdiction: "ZA",
		ClaimedValue: ClaimedValue{Value: "Synthetic Sample Ltd"},
		Purpose:      "ORGANISATION_ADMISSION", AssertedBy: "applicant-1",
	}
}

func syntheticCIPCRecord(now time.Time) CompanyRecord {
	return CompanyRecord{
		RegistrationNumber: "2020/123456/07", LegalName: "Synthetic Sample Ltd",
		EntityStatus: "IN_BUSINESS", SourceReference: "synthetic/cipc/company-01",
		ProvenanceRef:  "synthetic-immutable-object/cipc-response-01",
		ResponseDigest: "sha256:" + strings.Repeat("a", 64),
		ObservedAt:     now.Add(-time.Second), FreshnessUntil: now.Add(time.Hour),
		Synthetic: true,
	}
}

func TestPEO03BCIPCSyntheticRegistrationAndIndependentCheck(t *testing.T) {
	now := time.Date(2026, time.October, 11, 8, 0, 0, 0, time.UTC)
	company := &fakeCIPC{record: syntheticCIPCRecord(now)}
	claim := syntheticCIPCClaim()
	adapter := CIPCCompaniesAdapter{
		Registry: company, Certified: true, AllowSynthetic: true,
		AdapterID: "baobab.cipc.synthetic",
	}
	obs, err := adapter.LookupCIPC(context.Background(), "vcase_synthetic01", claim, "2020/123456/07", now)
	if err != nil {
		t.Fatal(err)
	}
	if company.calls != 1 || obs.Outcome != "MATCHED" || obs.SourceID != "esrc_cipc" {
		t.Fatalf("synthetic observation not bound to CIPC: %+v", obs)
	}
	rec, err := SourceCheckRecord(claim, "vcase_synthetic01", "reviewer-2", obs, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Outcome != "MATCHED" || rec.SourceRecordReference != obs.SourceReference {
		t.Fatalf("OEV-03 source check was not derived from the observation: %+v", rec)
	}
	if err := ValidateCheck(claim, "reviewer-2", rec, map[string]Evidence{}); err != nil {
		t.Fatalf("source check must conform to the existing OEV-03 lifecycle: %v", err)
	}
	// This check only supports a *separate* reviewer decision: no claim
	// standing, case or tenant is mutated by this adapter.
	if claim.CurrentResultID != "" || claim.Status == "VERIFIED" {
		t.Fatal("observation must never auto-verify claims")
	}
}

func TestPEO03BCIPCFailsClosed(t *testing.T) {
	now := time.Date(2026, time.October, 11, 8, 0, 0, 0, time.UTC)
	claim := syntheticCIPCClaim()
	for _, tc := range []struct {
		name string
		edit func(*CompanyRecord)
	}{
		{"wrong registration number", func(c *CompanyRecord) { c.RegistrationNumber = "wrong" }},
		{"different legal name", func(c *CompanyRecord) { c.LegalName = "Unrelated Company" }},
		{"missing provenance", func(c *CompanyRecord) { c.ProvenanceRef = "" }},
		{"missing cryptographic digest", func(c *CompanyRecord) { c.ResponseDigest = "" }},
		{"expired", func(c *CompanyRecord) { c.FreshnessUntil = now.Add(-time.Second) }},
		{"stale observation", func(c *CompanyRecord) { c.ObservedAt = now.Add(-time.Hour) }},
		{"future observation", func(c *CompanyRecord) { c.ObservedAt = now.Add(time.Minute) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := syntheticCIPCRecord(now)
			tc.edit(&record)
			adapter := CIPCCompaniesAdapter{Registry: &fakeCIPC{record: record},
				Certified: true, AllowSynthetic: true, AdapterID: "baobab.cipc.synthetic"}
			if _, err := adapter.LookupCIPC(context.Background(), "vcase_synthetic01",
				claim, "2020/123456/07", now); !errors.Is(err, ErrUntrustedObservation) {
				t.Fatalf("accepted invalid source: %v", err)
			}
		})
	}
	t.Run("synthetic credential refused in real runtime", func(t *testing.T) {
		adapter := CIPCCompaniesAdapter{Registry: &fakeCIPC{record: syntheticCIPCRecord(now)},
			Certified: true, AdapterID: "baobab.cipc"}
		if _, err := adapter.LookupCIPC(context.Background(), "vcase_synthetic01", claim,
			"2020/123456/07", now); !errors.Is(err, ErrUntrustedObservation) {
			t.Fatalf("synthetic response was promoted: %v", err)
		}
	})
	t.Run("uncertified client never queried", func(t *testing.T) {
		r := &fakeCIPC{record: syntheticCIPCRecord(now)}
		adapter := CIPCCompaniesAdapter{Registry: r, AllowSynthetic: true, AdapterID: "candidate"}
		if _, err := adapter.LookupCIPC(context.Background(), "vcase_synthetic01", claim,
			"2020/123456/07", now); !errors.Is(err, ErrUntrustedObservation) || r.calls != 0 {
			t.Fatalf("uncertified provider invoked or allowed: %v", err)
		}
	})
	t.Run("API outage cannot grant authority", func(t *testing.T) {
		adapter := CIPCCompaniesAdapter{Registry: &fakeCIPC{err: errors.New("synthetic outage")},
			Certified: true, AllowSynthetic: true, AdapterID: "candidate"}
		if _, err := adapter.LookupCIPC(context.Background(), "vcase_synthetic01", claim,
			"2020/123456/07", now); !errors.Is(err, ErrUntrustedObservation) {
			t.Fatalf("outage allowed: %v", err)
		}
	})
	t.Run("VAT is not CIPC's remit", func(t *testing.T) {
		claim.ClaimType = "VAT_REGISTRATION"
		adapter := CIPCCompaniesAdapter{Registry: &fakeCIPC{record: syntheticCIPCRecord(now)},
			Certified: true, AllowSynthetic: true, AdapterID: "baobab.cipc.synthetic"}
		if _, err := adapter.LookupCIPC(context.Background(), "vcase_synthetic01", claim,
			"2020/123456/07", now); !errors.Is(err, ErrUntrustedObservation) {
			t.Fatalf("CIPC illegally verified VAT: %v", err)
		}
	})
}

func TestPEO03BSARSAssistedEvidenceIsNotTrustedUntilAuthorityEnabled(t *testing.T) {
	now := time.Date(2026, time.October, 11, 8, 0, 0, 0, time.UTC)
	claim := syntheticCIPCClaim()
	claim.ClaimType = "TAX_COMPLIANCE_STATUS"
	claim.ClaimedValue.Value = "COMPLIANT"
	obs := AssistedOfficialReview("vcase_tcs01", claim, "esrc_sarstcs", "synthetic/tcs-001",
		"sha256:"+strings.Repeat("a", 64), "vault/synthetic-tcs-evidence",
		"human-operator", "human-reviewer", now.Add(-time.Minute), now.Add(time.Hour))
	if obs.WitnessPrincipal == obs.CheckerPrincipal {
		t.Fatal("independent assistance required")
	}
	if _, err := SourceCheckRecord(claim, "vcase_tcs01", "human-reviewer", obs,
		now, false); !errors.Is(err, ErrUntrustedObservation) {
		t.Fatalf("unactivated SARS source incorrectly authorised: %v", err)
	}
	// No SARS PIN, taxpayer password, tax reference or raw portal payload is
	// accepted/stored by the observation type or this assisted constructor.
	if obs.SourceID == "esrc_cipc" || obs.AdapterID != "" {
		t.Fatal("assisted SARS review masqueraded as automated CIPC lookup")
	}
}

func TestPEO03BCannotBorrowSomeoneElsesClaimOrReview(t *testing.T) {
	now := time.Now().UTC()
	claim := syntheticCIPCClaim()
	obs := SourceObservation{CaseID: "vcase_original", ClaimID: claim.ClaimID,
		Subject: claim.Subject, ClaimType: claim.ClaimType, Jurisdiction: "ZA",
		Purpose: claim.Purpose, SourceID: "esrc_cipc", Method: "SOURCE_API_LOOKUP",
		Outcome: "MATCHED", SourceReference: "official/record-01",
		ProvenanceRef: "immutable/evidence-01", AdapterID: "baobab.cipc",
		ResponseDigest: "sha256:" + strings.Repeat("a", 64),
		ObservedAt:     now.Add(-time.Second), FreshnessUntil: now.Add(time.Hour)}
	for _, tc := range []struct {
		name  string
		tweak func(*SourceObservation)
	}{
		{"case mismatch", func(o *SourceObservation) { o.CaseID = "vcase_other" }},
		{"claim mismatch", func(o *SourceObservation) { o.ClaimID = "ecl_other" }},
		{"subject mismatch", func(o *SourceObservation) { o.Subject.SubjectID = "other" }},
		{"other market", func(o *SourceObservation) { o.Jurisdiction = "UG" }},
		{"other purpose", func(o *SourceObservation) { o.Purpose = "MARKET_EXPANSION" }},
		{"false source", func(o *SourceObservation) { o.SourceID = "esrc_applicant" }},
		{"other method", func(o *SourceObservation) { o.Method = "MANUAL_REGISTRY_LOOKUP" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := obs
			tc.tweak(&other)
			if _, err := SourceCheckRecord(claim, "vcase_original", "reviewer-2", other,
				now, false); !errors.Is(err, ErrUntrustedObservation) {
				t.Fatalf("cross-boundary source use accepted: %v", err)
			}
		})
	}
	if _, err := SourceCheckRecord(claim, "vcase_original", claim.AssertedBy,
		obs, now, false); !errors.Is(err, ErrUntrustedObservation) {
		t.Fatalf("self-verification accepted: %v", err)
	}
}
