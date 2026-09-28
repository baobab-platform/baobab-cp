package verification

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
)

// example is Shared's evidence/v1 worked example.
type example struct {
	Evidence      []Evidence    `json:"evidence"`
	Claims        []Claim       `json:"claims"`
	Checks        []Check       `json:"checks"`
	Results       []Result      `json:"results"`
	Discrepancies []Discrepancy `json:"discrepancies"`
}

func loadExample(t *testing.T) example {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(contracttest.SharedDir(t), "contracts", "evidence", "v1", "examples", "organisation-admission-verification.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ex example
	if err := json.Unmarshal(raw, &ex); err != nil {
		t.Fatal(err)
	}
	return ex
}

func find[T any](items []T, match func(T) bool) T {
	for _, item := range items {
		if match(item) {
			return item
		}
	}
	var zero T
	return zero
}

// TestLifecycleIsShared: transitions and actors come from the embedded
// lifecycle.yaml.
func TestLifecycleIsShared(t *testing.T) {
	if next, err := Next(MachineCase, "READY_FOR_REVIEW", "begin_verification", ActorReviewer); err != nil || next != CaseVerifying {
		t.Fatalf("begin_verification: %s %v", next, err)
	}
	if _, err := Next(MachineCase, "VERIFIED", "expire", ActorReviewer); !errors.Is(err, ErrTransition) {
		t.Fatalf("a reviewer expiring a case: %v", err)
	}
	if _, err := Next(MachineCase, "DRAFT", "complete_verified", ActorReviewer); !errors.Is(err, ErrTransition) {
		t.Fatalf("DRAFT to VERIFIED: %v", err)
	}
	if err := NextTo(MachineClaim, ClaimUnderCheck, "record_result", "VERIFIED", ActorReviewer); err != nil {
		t.Fatal(err)
	}
	if err := NextTo(MachineClaim, "CONFLICTED", "record_result", "VERIFIED", ActorPlatform); !errors.Is(err, ErrTransition) {
		t.Fatalf("the platform resolving a conflict: %v", err)
	}
	if ContentAccessible("QUARANTINED") || !ContentAccessible(EvidenceAvail) {
		t.Fatal("quarantined content is never accessible")
	}
	if InitialClaimStatus("APPLICANT") != "SELF_ASSERTED" || InitialClaimStatus("REVIEWER") != ClaimUnderCheck {
		t.Fatal("initial claim statuses")
	}
	if !CaseOpen("CONFLICTED") || CaseOpen("VERIFIED") || CaseOpen("CANCELLED") {
		t.Fatal("which cases take new work")
	}
}

// TestSourceRegistry: authority is per claim type and jurisdiction, and an
// applicant is trusted for nothing.
func TestSourceRegistry(t *testing.T) {
	ursb, err := SourceByID("esrc_ursb")
	if err != nil {
		t.Fatal(err)
	}
	if !ursb.Trusts("REGISTRATION_IDENTIFIER", "UG") || ursb.Trusts("REGISTRATION_IDENTIFIER", "ZA") || ursb.Trusts("TAX_REGISTRATION", "UG") {
		t.Fatal("URSB's authority")
	}
	applicant, err := SourceByID("esrc_applicant")
	if err != nil || applicant.Trusts("LEGAL_NAME", "UG") {
		t.Fatalf("an applicant is trusted for nothing: %v", err)
	}
	if _, err := SourceByID("esrc_nosuch"); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("unknown source: %v", err)
	}
	if len(Sources()) < 3 {
		t.Fatal("the registry lists its sources")
	}
}

// TestRulesAgreeWithTheSharedExample: the example's recorded check and
// result pass the rules, and the example's negatives fail them.
func TestRulesAgreeWithTheSharedExample(t *testing.T) {
	ex := loadExample(t)
	regno := find(ex.Claims, func(c Claim) bool { return c.ClaimID == "ecl_regno" })
	name := find(ex.Claims, func(c Claim) bool { return c.ClaimID == "ecl_name" })
	evidence := map[string]Evidence{}
	for _, e := range ex.Evidence {
		evidence[e.EvidenceID] = e
	}
	checks := map[string]Check{}
	for _, c := range ex.Checks {
		checks[c.CheckID] = c
	}
	discrepancies := map[string]Discrepancy{}
	for _, d := range ex.Discrepancies {
		discrepancies[d.DiscrepancyID] = d
	}
	registry := checks["vchk_regnoreg"]
	record := CheckRecord{ClaimID: registry.ClaimID, Method: registry.Method, SourceID: registry.SourceID, Outcome: registry.Outcome,
		Dimensions: registry.Dimensions, EvidenceIDs: registry.EvidenceIDs, SourceRecordReference: registry.SourceRecordReference}
	if err := ValidateCheck(regno, registry.PerformedBy, record, evidence); err != nil {
		t.Fatalf("the example's registry check: %v", err)
	}
	if err := ValidateCheck(regno, regno.AssertedBy, record, evidence); !errors.Is(err, ErrSelfVerification) {
		t.Fatalf("the applicant checking their own claim: %v", err)
	}
	quarantined := record
	quarantined.EvidenceIDs = []string{"evr_poa01"}
	if err := ValidateCheck(regno, registry.PerformedBy, quarantined, evidence); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a positive check citing quarantined evidence: %v", err)
	}
	otherSource := record
	otherSource.SourceID = "esrc_applicant"
	if err := ValidateCheck(regno, registry.PerformedBy, otherSource, evidence); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a check citing another source's evidence: %v", err)
	}
	otherSubject := regno
	otherSubject.Subject.SubjectID = "ce_other"
	if err := ValidateCheck(otherSubject, registry.PerformedBy, record, evidence); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a check citing another subject's evidence: %v", err)
	}
	otherPurpose := regno
	otherPurpose.Purpose = "PERIODIC_REVIEW"
	if err := ValidateCheck(otherPurpose, registry.PerformedBy, record, evidence); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a check citing evidence gathered for another purpose: %v", err)
	}
	unknown := record
	unknown.SourceID = "esrc_nosuch"
	if err := ValidateCheck(regno, registry.PerformedBy, unknown, evidence); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("an unknown source: %v", err)
	}

	verified := find(ex.Results, func(r Result) bool { return r.ResultID == "vres_regno" })
	rec := ResultRecord{ClaimID: verified.ClaimID, Outcome: verified.Outcome, CheckIDs: verified.CheckIDs, Dimensions: verified.Dimensions,
		Freshness: verified.Freshness}
	if err := ValidateResult(regno, verified.CaseID, verified.DecidedBy, rec, checks, discrepancies); err != nil {
		t.Fatalf("the example's VERIFIED result: %v", err)
	}
	if err := ValidateResult(regno, verified.CaseID, regno.AssertedBy, rec, checks, discrepancies); !errors.Is(err, ErrSelfVerification) {
		t.Fatalf("the applicant deciding their own claim: %v", err)
	}
	applicantOnly := rec
	applicantOnly.CheckIDs = []string{"vchk_regnodoc"}
	if err := ValidateResult(regno, verified.CaseID, verified.DecidedBy, applicantOnly, checks, discrepancies); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("VERIFIED on an applicant-supplied source alone: %v", err)
	}
	otherJurisdiction := regno
	otherJurisdiction.Jurisdiction = "ZA"
	if err := ValidateResult(otherJurisdiction, verified.CaseID, verified.DecidedBy, rec, checks, discrepancies); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("VERIFIED by a source trusted elsewhere: %v", err)
	}
	borrowed := rec
	borrowed.CheckIDs = []string{"vchk_regnoreg", "vchk_namereg"}
	if err := ValidateResult(regno, verified.CaseID, verified.DecidedBy, borrowed, checks, discrepancies); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a result citing another claim's check: %v", err)
	}

	conflicted := find(ex.Results, func(r Result) bool { return r.ResultID == "vres_name" })
	crec := ResultRecord{ClaimID: conflicted.ClaimID, Outcome: conflicted.Outcome, CheckIDs: conflicted.CheckIDs,
		Dimensions: conflicted.Dimensions, Freshness: conflicted.Freshness, DiscrepancyIDs: conflicted.DiscrepancyIDs}
	if err := ValidateResult(name, conflicted.CaseID, conflicted.DecidedBy, crec, checks, discrepancies); err != nil {
		t.Fatalf("the example's CONFLICTED result: %v", err)
	}
	wrong := crec
	wrong.DiscrepancyIDs = []string{"edis_addr"}
	if err := ValidateResult(name, conflicted.CaseID, conflicted.DecidedBy, wrong, checks, discrepancies); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a discrepancy about another claim type: %v", err)
	}
}

func TestDiscrepancyAndStanding(t *testing.T) {
	one := DiscrepancyRecord{ConflictingValues: []SourceValue{{SourceID: "esrc_ursb", Value: "a"}, {SourceID: "esrc_ursb", Value: "b"}}}
	if err := ValidateDiscrepancy(one); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a discrepancy within one source: %v", err)
	}
	two := DiscrepancyRecord{ConflictingValues: []SourceValue{{SourceID: "esrc_ursb", Value: "a"}, {SourceID: "esrc_applicant", Value: "b"}}}
	if err := ValidateDiscrepancy(two); err != nil {
		t.Fatal(err)
	}
	if ClaimStanding("CONFLICTED") != "CONFLICTED" {
		t.Fatal("a conflicted result is the claim's standing")
	}
}
