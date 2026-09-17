package domain

import (
	"testing"
	"time"
)

func validTenantProvisioning() TenantProvisioning {
	return TenantProvisioning{
		TenantID:       "tn_zuribeans",
		IdempotencyKey: "idem-1",
		RequestHash:    "abc123",
		Status:         ProvisioningStatusPlan,
		StartedAt:      time.Now().UTC(),
	}
}

func TestTenantProvisioningValidateAccepts(t *testing.T) {
	if err := validTenantProvisioning().Validate(); err != nil {
		t.Fatalf("expected valid tenant provisioning, got: %v", err)
	}
}

func TestTenantProvisioningValidateRequiresIdempotencyKey(t *testing.T) {
	p := validTenantProvisioning()
	p.IdempotencyKey = ""
	if err := p.Validate(); err == nil {
		t.Fatal("expected rejection of a missing idempotency_key")
	}
}

func TestTenantProvisioningValidateRequiresCompletedAtWhenActive(t *testing.T) {
	p := validTenantProvisioning()
	p.Status = ProvisioningStatusActive
	if err := p.Validate(); err == nil {
		t.Fatal("expected rejection of ACTIVE without completed_at")
	}
	now := time.Now().UTC()
	p.CompletedAt = &now
	if err := p.Validate(); err != nil {
		t.Fatalf("expected ACTIVE with completed_at to be valid, got: %v", err)
	}
}

func TestTenantProvisioningValidateRequiresLastErrorWhenFailed(t *testing.T) {
	p := validTenantProvisioning()
	p.Status = ProvisioningStatusFailed
	if err := p.Validate(); err == nil {
		t.Fatal("expected rejection of FAILED without last_error")
	}
	p.LastError = "provider unavailable"
	if err := p.Validate(); err != nil {
		t.Fatalf("expected FAILED with last_error to be valid, got: %v", err)
	}
}

func TestTransitionProvisioningForwardPath(t *testing.T) {
	path := []ProvisioningStatus{
		ProvisioningStatusPlan, ProvisioningStatusApply, ProvisioningStatusReconcile,
		ProvisioningStatusReady, ProvisioningStatusActive,
	}
	for i := 0; i < len(path)-1; i++ {
		if _, ok := TransitionProvisioning(path[i], path[i+1]); !ok {
			t.Fatalf("expected %s -> %s to be a legal transition", path[i], path[i+1])
		}
	}
}

func TestTransitionProvisioningRejectsSkippingStates(t *testing.T) {
	if _, ok := TransitionProvisioning(ProvisioningStatusPlan, ProvisioningStatusReady); ok {
		t.Fatal("expected PLAN -> READY (skipping APPLY/RECONCILE) to be rejected")
	}
	if _, ok := TransitionProvisioning(ProvisioningStatusPlan, ProvisioningStatusActive); ok {
		t.Fatal("expected PLAN -> ACTIVE to be rejected")
	}
}

func TestTransitionProvisioningRejectsTerminalOutbound(t *testing.T) {
	if _, ok := TransitionProvisioning(ProvisioningStatusActive, ProvisioningStatusApply); ok {
		t.Fatal("expected no outbound transition from ACTIVE")
	}
	if _, ok := TransitionProvisioning(ProvisioningStatusCancelled, ProvisioningStatusApply); ok {
		t.Fatal("expected no outbound transition from CANCELLED")
	}
}

func TestTransitionProvisioningAllowsRetryFromFailed(t *testing.T) {
	if _, ok := TransitionProvisioning(ProvisioningStatusFailed, ProvisioningStatusApply); !ok {
		t.Fatal("expected FAILED -> APPLY (retry) to be a legal transition")
	}
	if _, ok := TransitionProvisioning(ProvisioningStatusFailed, ProvisioningStatusReconcile); ok {
		t.Fatal("expected FAILED -> RECONCILE to be rejected -- a failure invalidates progress past APPLY")
	}
	if _, ok := TransitionProvisioning(ProvisioningStatusFailed, ProvisioningStatusReady); ok {
		t.Fatal("expected FAILED -> READY to be rejected")
	}
}

func TestTransitionProvisioningRejectsInvalidStatus(t *testing.T) {
	if _, ok := TransitionProvisioning("BOGUS", ProvisioningStatusApply); ok {
		t.Fatal("expected an invalid from-status to be rejected")
	}
	if _, ok := TransitionProvisioning(ProvisioningStatusPlan, "BOGUS"); ok {
		t.Fatal("expected an invalid to-status to be rejected")
	}
}

func TestProvisioningStatusIsTerminal(t *testing.T) {
	if !ProvisioningStatusActive.IsTerminal() {
		t.Fatal("expected ACTIVE to be terminal")
	}
	if !ProvisioningStatusCancelled.IsTerminal() {
		t.Fatal("expected CANCELLED to be terminal")
	}
	if ProvisioningStatusFailed.IsTerminal() {
		t.Fatal("expected FAILED to be non-terminal (retryable)")
	}
	if ProvisioningStatusPlan.IsTerminal() {
		t.Fatal("expected PLAN to be non-terminal")
	}
}

func TestTenantProvisioningAdvanceHappyPath(t *testing.T) {
	p := validTenantProvisioning()
	p.Version = 1

	applied, err := p.Advance(ProvisioningStatusApply, "")
	if err != nil {
		t.Fatalf("advance to APPLY: %v", err)
	}
	if applied.Status != ProvisioningStatusApply || applied.Version != 2 {
		t.Fatalf("unexpected state after advance: %+v", applied)
	}

	reconciled, err := applied.Advance(ProvisioningStatusReconcile, "")
	if err != nil {
		t.Fatalf("advance to RECONCILE: %v", err)
	}
	ready, err := reconciled.Advance(ProvisioningStatusReady, "")
	if err != nil {
		t.Fatalf("advance to READY: %v", err)
	}
	active, err := ready.Advance(ProvisioningStatusActive, "")
	if err != nil {
		t.Fatalf("advance to ACTIVE: %v", err)
	}
	if active.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set on reaching ACTIVE")
	}
	if err := active.Validate(); err != nil {
		t.Fatalf("expected the fully-advanced aggregate to validate, got: %v", err)
	}
}

func TestTenantProvisioningAdvanceRejectsIllegalTransition(t *testing.T) {
	p := validTenantProvisioning()
	if _, err := p.Advance(ProvisioningStatusReady, ""); err == nil {
		t.Fatal("expected PLAN -> READY to be rejected by Advance")
	}
}

func TestTenantProvisioningAdvanceRecordsFailureAndRetry(t *testing.T) {
	p := validTenantProvisioning()
	applied, err := p.Advance(ProvisioningStatusApply, "")
	if err != nil {
		t.Fatalf("advance to APPLY: %v", err)
	}
	failed, err := applied.Advance(ProvisioningStatusFailed, "provider timeout")
	if err != nil {
		t.Fatalf("advance to FAILED: %v", err)
	}
	if failed.AttemptCount != 1 || failed.LastError != "provider timeout" {
		t.Fatalf("expected attempt_count=1 and last_error recorded, got %+v", failed)
	}

	retried, err := failed.Advance(ProvisioningStatusApply, "")
	if err != nil {
		t.Fatalf("retry to APPLY: %v", err)
	}
	if retried.LastError != "" {
		t.Fatal("expected last_error to be cleared on retry")
	}
	if retried.AttemptCount != 1 {
		t.Fatalf("expected attempt_count to remain 1 across the retry itself, got %d", retried.AttemptCount)
	}

	secondFailure, err := retried.Advance(ProvisioningStatusFailed, "provider timeout again")
	if err != nil {
		t.Fatalf("advance to FAILED a second time: %v", err)
	}
	if secondFailure.AttemptCount != 2 {
		t.Fatalf("expected attempt_count=2 after a second failure, got %d", secondFailure.AttemptCount)
	}
}

func TestTenantProvisioningAdvanceRecordsCancellationReason(t *testing.T) {
	p := validTenantProvisioning()
	cancelled, err := p.Advance(ProvisioningStatusCancelled, "requested by operator")
	if err != nil {
		t.Fatalf("advance to CANCELLED: %v", err)
	}
	if len(cancelled.BlockingReasons) != 1 || cancelled.BlockingReasons[0] != "requested by operator" {
		t.Fatalf("expected cancellation reason to be recorded, got %+v", cancelled.BlockingReasons)
	}
}
