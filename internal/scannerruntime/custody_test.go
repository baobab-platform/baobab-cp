package scannerruntime

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
)

func TestMountedCustodyPoliciesAreScopedApprovedAndReadFresh(t *testing.T) {
	now := time.Now().UTC()
	p := ApprovedRetentionPolicy{Current: true, RetentionPolicy: repository.RetentionPolicy{ID: "synthetic-only", Version: "v1", Purpose: "ORGANISATION_ADMISSION", Jurisdiction: "ZA", Classification: "CONFIDENTIAL", MinimumHours: 1, MaximumHours: 4, OrphanGraceHours: 1, QuarantineHours: 1}, ProposedBy: "synthetic-maker", ApprovedBy: "synthetic-checker", ApprovalReference: "synthetic-policy-approval", EffectiveFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	encode := func(p ApprovedRetentionPolicy) []byte {
		raw, _ := json.Marshal(map[string]any{"policies": []ApprovedRetentionPolicy{p}})
		return raw
	}
	path := privateFixture(t, "policies", encode(p))
	reader := MountedCustodyPolicies{Path: path, Now: func() time.Time { return now }}
	if got, err := reader.CustodyPolicy(context.Background(), p.ID, ""); err != nil || got.Version != "v1" {
		t.Fatal(err)
	}
	if _, err := reader.CustodyPolicy(context.Background(), "unapproved", ""); err == nil {
		t.Fatal("unapproved retention defaulted")
	}
	for _, mutate := range []func(*ApprovedRetentionPolicy){func(p *ApprovedRetentionPolicy) { p.ApprovedBy = p.ProposedBy }, func(p *ApprovedRetentionPolicy) { p.ExpiresAt = now }, func(p *ApprovedRetentionPolicy) { p.MinimumHours = 0 }, func(p *ApprovedRetentionPolicy) { p.MaximumHours = 0 }, func(p *ApprovedRetentionPolicy) { p.QuarantineHours = 0 }, func(p *ApprovedRetentionPolicy) { p.Jurisdiction = "" }} {
		bad := p
		mutate(&bad)
		if os.WriteFile(path, encode(bad), 0600) != nil {
			t.Fatal("fixture write failed")
		}
		if _, err := reader.CustodyPolicy(context.Background(), p.ID, ""); err == nil {
			t.Fatal("changed/unapproved policy snapshot admitted")
		}
	}
	if os.Remove(path) != nil {
		t.Fatal("fixture removal failed")
	}
	if _, err := reader.CustodyPolicy(context.Background(), p.ID, ""); err == nil {
		t.Fatal("withdrawn policy cached")
	}
}
func TestCustodyMaintenanceApprovalsBindOperationAndSeparateReviewers(t *testing.T) {
	now := time.Now().UTC()
	a := repository.CustodyApproval{Operation: "DELETE", EvidenceID: "evr_synthetic", ArtifactID: "eart_synthetic", ProofHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExpectedState: "DELETING", ExpectedRevision: 1, ExpectedVersion: "synthetic-version", ProposedBy: "synthetic-maker", ApprovedBy: "synthetic-checker", ApprovalReference: "synthetic-approval", EffectiveFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	if !a.Valid(now) {
		t.Fatal("approved snapshot denied")
	}
	for _, change := range []func(*repository.CustodyApproval){func(a *repository.CustodyApproval) { a.ApprovedBy = a.ProposedBy }, func(a *repository.CustodyApproval) { a.ExpiresAt = now }, func(a *repository.CustodyApproval) { a.Operation = "BULK_DELETE" }, func(a *repository.CustodyApproval) { a.ProofHash = "" }, func(a *repository.CustodyApproval) { a.TargetKey = "alias/arbitrary" }} {
		bad := a
		change(&bad)
		if bad.Valid(now) {
			t.Fatal("unreviewed mutation accepted")
		}
	}
}

func TestCustodyPoliciesKeepHistoricalVersionsAndSingleCurrentSelection(t *testing.T) {
	now := time.Now().UTC()
	first := ApprovedRetentionPolicy{Current: false, RetentionPolicy: repository.RetentionPolicy{ID: "synthetic-only", Version: "v1", Purpose: "ORGANISATION_ADMISSION", Jurisdiction: "ZA", Classification: "CONFIDENTIAL", MinimumHours: 1, MaximumHours: 4, OrphanGraceHours: 1, QuarantineHours: 1}, ProposedBy: "maker", ApprovedBy: "checker", ApprovalReference: "synthetic-approval", EffectiveFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	next := first
	next.Version = "v2"
	next.Current = true
	next.MaximumHours = 8
	raw, _ := json.Marshal(map[string]any{"policies": []ApprovedRetentionPolicy{first, next}})
	path := privateFixture(t, "versioned-policies", raw)
	m := MountedCustodyPolicies{Path: path, Now: func() time.Time { return now }}
	if p, e := m.CustodyPolicy(context.Background(), first.ID, ""); e != nil || p.Version != "v2" {
		t.Fatal("current version selection failed", e)
	}
	if p, e := m.CustodyPolicy(context.Background(), first.ID, "v1"); e != nil || p.MaximumHours != 4 {
		t.Fatal("historical policy overwritten", e)
	}
	first.Current = true
	raw, _ = json.Marshal(map[string]any{"policies": []ApprovedRetentionPolicy{first, next}})
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("fixture write failed")
	}
	if _, e := m.CustodyPolicy(context.Background(), first.ID, ""); e == nil {
		t.Fatal("ambiguous current retention selected")
	}
}
