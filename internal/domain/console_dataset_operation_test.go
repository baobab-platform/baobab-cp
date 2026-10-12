package domain

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"
)

func TestCX05GovernedDatasetDeniesByDefault(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	id := "0199d000-1111-7777-8000-111111111111"
	rows := []ConsoleDatasetRecord{{OrganisationID: id, DisplayName: "=SUM(2+2)", Status: "ACTIVE", VerificationState: "UNVERIFIED", SourceAuthority: "CP", Revision: 1}}
	source, err := DatasetSourceFingerprint(rows)
	if err != nil {
		t.Fatal(err)
	}
	intent := ConsoleDatasetIntent{Kind: ConsoleExport, MakerPrincipalID: "prn_maker", Purpose: "reviewed disclosure",
		RecipientReference: "ref_01", IdempotencyKey: "cx05-synthetic-idempotency-example", SourceFingerprint: source, TargetIDs: []string{id}, ValidUntil: now.Add(time.Hour)}
	digest, err := intent.CanonicalDigest(now)
	if err != nil {
		t.Fatal(err)
	}
	approval := ConsoleDatasetApproval{PlanDigest: digest, CheckerPrincipalID: "prn_checker", ApprovedAt: now, EvidenceReference: "case_1"}
	allow := func(_, _, p string) bool { return p == "organisation.view" || p == "organisation.export" }
	csvBytes, err := BuildApprovedRedactedCSV(intent, approval, rows, now, allow, true)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := csv.NewReader(strings.NewReader(string(csvBytes))).ReadAll()
	if err != nil || len(parsed) != 2 || parsed[1][1] != "'=SUM(2+2)" {
		t.Fatalf("unsafe export %q err %v", csvBytes, err)
	}
	r := append([]ConsoleDatasetRecord{}, rows...)
	r[0].Status = "SUSPENDED"
	if _, err = BuildApprovedRedactedCSV(intent, approval, r, now, allow, true); err == nil {
		t.Fatal("stale source allowed")
	}
	if _, err = BuildApprovedRedactedCSV(intent, approval, rows, now, allow, false); err == nil {
		t.Fatal("unreserved disclosure allowed")
	}
	if _, err = BuildApprovedRedactedCSV(intent, approval, rows, now, func(string, string, string) bool { return false }, true); err == nil {
		t.Fatal("unauthorised export allowed")
	}
	approval.CheckerPrincipalID = intent.MakerPrincipalID
	if _, err = BuildApprovedRedactedCSV(intent, approval, rows, now, allow, true); err == nil {
		t.Fatal("self approval allowed")
	}
}
func TestCX05BulkValuesVersionsAndRightsBoundToChecker(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	id := "0199d000-1111-7777-8000-111111111111"
	rows := []ConsoleDatasetRecord{{OrganisationID: id, DisplayName: "Nabhold", Status: "ACTIVE", VerificationState: "PENDING_REVIEW", SourceAuthority: "CP", Revision: 4}}
	source, _ := DatasetSourceFingerprint(rows)
	changes := []ConsoleMetadataChange{{OrganisationID: id, ExpectedVersion: 4, InternalLabel: "Board review label"}}
	changehash, _ := BulkChangeFingerprint(changes)
	intent := ConsoleDatasetIntent{Kind: ConsoleBulk, Action: "organisation.metadata.update", MakerPrincipalID: "prn_maker", Purpose: "governed bulk update",
		RecipientReference: "ref_02", IdempotencyKey: "cx05-synthetic-idempotency-example", SourceFingerprint: source, ChangeFingerprint: changehash,
		TargetIDs: []string{id}, ValidUntil: now.Add(time.Hour)}
	digest, err := intent.CanonicalDigest(now)
	if err != nil {
		t.Fatal(err)
	}
	approval := ConsoleDatasetApproval{PlanDigest: digest, CheckerPrincipalID: "prn_checker", ApprovedAt: now, EvidenceReference: "case_2"}
	allow := func(principal, _, p string) bool {
		if principal == "prn_maker" {
			return p == "organisation.view" || p == "organisation.bulk.propose" || p == "organisation.manage"
		}
		if principal == "prn_checker" {
			return p == "organisation.view" || p == "organisation.bulk.approve"
		}
		return false
	}
	calls := 0
	commit := func(_ context.Context, h string, items []ConsoleMetadataChange) error {
		if h != digest || len(items) != 1 {
			t.Fatal("incorrect transaction inputs")
		}
		calls++
		return nil
	}
	if err := ApplyApprovedMetadataBulk(context.Background(), intent, approval, rows, changes, now, allow, true, commit); err != nil {
		t.Fatal(err)
	}
	altered := append([]ConsoleMetadataChange(nil), changes...)
	altered[0].InternalLabel = "change after approval"
	if err := ApplyApprovedMetadataBulk(context.Background(), intent, approval, rows, altered, now, allow, true, commit); err == nil {
		t.Fatal("post-approval tampering allowed")
	}
	if calls != 1 {
		t.Fatal("unapproved commit occurred")
	}
	changedSource := append([]ConsoleDatasetRecord(nil), rows...)
	changedSource[0].OrganisationID = "0199d000-1111-7777-8000-222222222222"
	if err := ApplyApprovedMetadataBulk(context.Background(), intent, approval, changedSource, changes, now, allow, true, commit); err == nil {
		t.Fatal("foreign source fingerprint or target accepted")
	}
	staleRevision := append([]ConsoleDatasetRecord(nil), rows...)
	staleRevision[0].Revision = 5
	if err := ApplyApprovedMetadataBulk(context.Background(), intent, approval, staleRevision, changes, now, allow, true, commit); err == nil {
		t.Fatal("unreviewed source revision accepted")
	}
	if calls != 1 {
		t.Fatal("invalid source reached commit adapter")
	}

	if err := ApplyApprovedMetadataBulk(context.Background(), intent, approval, rows, changes, now, func(principal, _, p string) bool {
		return principal != "prn_checker" || p != "organisation.bulk.approve"
	}, true, commit); err == nil {
		t.Fatal("checker right absent")
	}
	if err := ApplyApprovedMetadataBulk(context.Background(), intent, approval, rows, changes, now, allow, true, nil); err == nil {
		t.Fatal("missing durable TX accepted")
	}
	if err := ApplyApprovedMetadataBulk(context.Background(), intent, approval, rows, changes, now.Add(2*time.Hour), allow, true, commit); err == nil {
		t.Fatal("expired approval reused")
	}
}

func TestCX05PlanHashCannotAliasViaDelimiterCollision(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	r := ConsoleDatasetIntent{Kind: ConsoleExport, MakerPrincipalID: "prn_maker", Purpose: "review",
		RecipientReference: "team\x00case", IdempotencyKey: "cx05-synthetic-idempotency-example",
		SourceFingerprint: strings.Repeat("a", 64), TargetIDs: []string{"0199d000-1111-7777-8000-111111111111"},
		ValidUntil: now.Add(time.Hour)}
	if _, err := r.CanonicalDigest(now); err == nil {
		t.Fatal("NUL recipient accepted")
	}
	r.RecipientReference = "case"
	r.Purpose = "review\x00team"
	if _, err := r.CanonicalDigest(now); err == nil {
		t.Fatal("NUL purpose accepted")
	}
}
func TestCX05NoAuthorityTransferAcrossPrincipals(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	id := "0199d000-1111-7777-8000-111111111111"
	rows := []ConsoleDatasetRecord{{OrganisationID: id, DisplayName: "Current", Status: "ACTIVE", SourceAuthority: "CP", Revision: 2}}
	hash, _ := DatasetSourceFingerprint(rows)
	intent := ConsoleDatasetIntent{Kind: ConsoleExport, MakerPrincipalID: "prn_maker", Purpose: "reviewed",
		RecipientReference: "case", IdempotencyKey: "cx05-synthetic-idempotency-example",
		SourceFingerprint: hash, TargetIDs: []string{id}, ValidUntil: now.Add(time.Hour)}
	digest, _ := intent.CanonicalDigest(now)
	approve := ConsoleDatasetApproval{PlanDigest: digest, CheckerPrincipalID: "prn_checker", ApprovedAt: now, EvidenceReference: "case"}
	wrongMaker := func(principal, _, perm string) bool { return principal == "prn_checker" }
	if _, err := BuildApprovedRedactedCSV(intent, approve, rows, now, wrongMaker, true); err == nil {
		t.Fatal("checker grants substituted for maker")
	}
	wrongChecker := func(principal, _, perm string) bool { return principal == "prn_maker" }
	if _, err := BuildApprovedRedactedCSV(intent, approve, rows, now, wrongChecker, true); err == nil {
		t.Fatal("maker grants substituted for checker")
	}
}
