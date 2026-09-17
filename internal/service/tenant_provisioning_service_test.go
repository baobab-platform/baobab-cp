package service

import (
	"context"
	"errors"
	"testing"
	"time"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestTenantProvisioningServicePlanIsIdempotent(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := TenantProvisioningService{Repository: repo, Now: fixedClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	ctx := context.Background()

	first, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", []string{"solution.baobab-xbt"}, []string{"UG", "ZA"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if first.Status != provisioningdomain.ProvisioningStatusPlan {
		t.Fatalf("expected PLAN status, got %s", first.Status)
	}

	second, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", []string{"solution.baobab-xbt"}, []string{"UG", "ZA"})
	if err != nil {
		t.Fatalf("re-plan with the same idempotency key: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same aggregate to be returned for a repeated idempotency key, got %s vs %s", second.ID, first.ID)
	}

	all, err := repo.ListTenantProvisioningsForTenant(ctx, "tn_zuribeans")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly one TenantProvisioning to have been created, got %d", len(all))
	}
}

func TestTenantProvisioningServicePlanRejectsIdempotencyConflict(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := TenantProvisioningService{Repository: repo}
	ctx := context.Background()

	first, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", []string{"solution.baobab-xbt"}, []string{"UG", "ZA"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Same idempotency key, different request_hash: a caller changed the
	// desired state (e.g. added a market) but reused the old key. This
	// MUST be rejected, not silently replay the first request's plan.
	if _, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-2", []string{"solution.baobab-xbt"}, []string{"UG", "ZA", "KE"}); !errors.Is(err, ErrTenantProvisioningIdempotencyConflict) {
		t.Fatalf("expected ErrTenantProvisioningIdempotencyConflict, got %v", err)
	}

	// The original plan must be untouched by the rejected conflicting request.
	stored, err := repo.GetTenantProvisioning(ctx, first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.RequestHash != "hash-1" || len(stored.MarketRequests) != 2 {
		t.Fatalf("expected the original plan to be unchanged by the rejected conflict, got %+v", stored)
	}
}

func TestTenantProvisioningServicePlanDistinguishesIdempotencyKeys(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := TenantProvisioningService{Repository: repo}
	ctx := context.Background()

	first, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", nil, nil)
	if err != nil {
		t.Fatalf("plan 1: %v", err)
	}
	second, err := svc.Plan(ctx, "tn_zuribeans", "idem-2", "hash-2", nil, nil)
	if err != nil {
		t.Fatalf("plan 2: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("expected distinct idempotency keys to produce distinct aggregates")
	}
}

func TestTenantProvisioningServiceHappyPath(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := TenantProvisioningService{Repository: repo}
	ctx := context.Background()

	p, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", nil, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if p, err = svc.Apply(ctx, p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if p, err = svc.Reconcile(ctx, p.ID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if p, err = svc.MarkReady(ctx, p.ID); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if p, err = svc.Activate(ctx, p.ID); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if p.Status != provisioningdomain.ProvisioningStatusActive {
		t.Fatalf("expected ACTIVE, got %s", p.Status)
	}
	if p.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}

	stored, err := repo.GetTenantProvisioning(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Status != provisioningdomain.ProvisioningStatusActive || stored.Version != p.Version {
		t.Fatalf("expected the persisted row to match the returned aggregate, got %+v", stored)
	}
}

func TestTenantProvisioningServiceRejectsSkippingReady(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := TenantProvisioningService{Repository: repo}
	ctx := context.Background()

	p, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", nil, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := svc.Activate(ctx, p.ID); err == nil {
		t.Fatal("expected PLAN -> ACTIVE to be rejected without passing through APPLY/RECONCILE/READY")
	}
}

func TestTenantProvisioningServiceFailAndRetry(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := TenantProvisioningService{Repository: repo}
	ctx := context.Background()

	p, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", nil, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if p, err = svc.Apply(ctx, p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if p, err = svc.Fail(ctx, p.ID, "provider timeout"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if p.Status != provisioningdomain.ProvisioningStatusFailed || p.AttemptCount != 1 {
		t.Fatalf("expected FAILED with attempt_count=1, got %+v", p)
	}

	if p, err = svc.Retry(ctx, p.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if p.Status != provisioningdomain.ProvisioningStatusApply || p.LastError != "" {
		t.Fatalf("expected APPLY with last_error cleared after retry, got %+v", p)
	}
}

func TestTenantProvisioningServiceFailRequiresReason(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := TenantProvisioningService{Repository: repo}
	ctx := context.Background()

	p, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", nil, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := svc.Fail(ctx, p.ID, ""); err == nil {
		t.Fatal("expected Fail to require a non-empty reason")
	}
}

func TestTenantProvisioningServiceCancel(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := TenantProvisioningService{Repository: repo}
	ctx := context.Background()

	p, err := svc.Plan(ctx, "tn_zuribeans", "idem-1", "hash-1", nil, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	p, err = svc.Cancel(ctx, p.ID, "operator requested")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if p.Status != provisioningdomain.ProvisioningStatusCancelled {
		t.Fatalf("expected CANCELLED, got %s", p.Status)
	}
	if _, err := svc.Apply(ctx, p.ID); err == nil {
		t.Fatal("expected no transition out of CANCELLED")
	}
}
