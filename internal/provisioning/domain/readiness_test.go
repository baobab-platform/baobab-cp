package domain

import (
	"testing"
	"time"
)

func TestProvisioningReadinessFailsClosedWhenEvidenceMissing(t *testing.T) {
	r := ProvisioningReadiness{ProvisioningID: "p-1"}
	if r.Ready() {
		t.Fatal("expected missing evidence to block readiness")
	}
	if len(r.BlockingReasons()) != len(RequiredReadinessChecks()) {
		t.Fatalf("expected one blocker per missing required check")
	}
}

func TestProvisioningReadinessRequiresEveryCheckToPass(t *testing.T) {
	now := time.Now().UTC()
	checks := make([]ReadinessCheck, 0, len(RequiredReadinessChecks()))
	for _, name := range RequiredReadinessChecks() {
		checks = append(checks, ReadinessCheck{Name: name, Ready: true, CheckedAt: now})
	}
	r := ProvisioningReadiness{ProvisioningID: "p-1", Checks: checks}
	if !r.Ready() {
		t.Fatalf("expected complete successful evidence to be ready: %v", r.BlockingReasons())
	}

	r.Checks[0].Ready = false
	r.Checks[0].Reason = "provider not reconciled"
	if r.Ready() {
		t.Fatal("expected a failed prerequisite to block readiness")
	}
	if len(r.BlockingReasons()) != 1 {
		t.Fatalf("expected exactly one blocker, got %v", r.BlockingReasons())
	}
}
