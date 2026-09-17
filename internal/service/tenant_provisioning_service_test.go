package service

import (
	"context"
	"testing"
	"time"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func readyEvidence(id string) provisioningdomain.ProvisioningReadiness {
	now := time.Now().UTC()
	checks := make([]provisioningdomain.ReadinessCheck, 0, len(provisioningdomain.RequiredReadinessChecks()))
	for _, name := range provisioningdomain.RequiredReadinessChecks() {
		checks = append(checks, provisioningdomain.ReadinessCheck{Name: name, Ready: true, CheckedAt: now})
	}
	return provisioningdomain.ProvisioningReadiness{ProvisioningID: id, Checks: checks}
}

func TestTenantProvisioningServicePlanIsIdempotent(t *testing.T) {
	repo := repository.NewInMemoryRepository(); svc := TenantProvisioningService{Repository: repo, Now: fixedClock(time.Date(2026,1,1,0,0,0,0,time.UTC))}; ctx := context.Background()
	first, err := svc.Plan(ctx,"tn_zuribeans","idem-1","hash-1",[]string{"solution.baobab-xbt"},[]string{"UG","ZA"}); if err != nil { t.Fatal(err) }
	second, err := svc.Plan(ctx,"tn_zuribeans","idem-1","hash-1",[]string{"solution.baobab-xbt"},[]string{"UG","ZA"}); if err != nil { t.Fatal(err) }
	if second.ID != first.ID { t.Fatal("idempotent plan created a duplicate") }
}

func TestTenantProvisioningServiceHappyPathRequiresEvidence(t *testing.T) {
	repo := repository.NewInMemoryRepository(); svc := TenantProvisioningService{Repository: repo}; ctx := context.Background()
	p, err := svc.Plan(ctx,"tn_zuribeans","idem-1","hash-1",nil,nil); if err != nil { t.Fatal(err) }
	if p, err = svc.Apply(ctx,p.ID); err != nil { t.Fatal(err) }
	if p, err = svc.Reconcile(ctx,p.ID); err != nil { t.Fatal(err) }
	evidence := readyEvidence(p.ID)
	if p, err = svc.MarkReady(ctx,p.ID,evidence); err != nil { t.Fatal(err) }
	if p, err = svc.Activate(ctx,p.ID,evidence); err != nil { t.Fatal(err) }
	if p.Status != provisioningdomain.ProvisioningStatusActive || p.CompletedAt == nil { t.Fatalf("expected completed ACTIVE provisioning: %+v", p) }
}

func TestTenantProvisioningServiceBlocksIncompleteReadiness(t *testing.T) {
	repo := repository.NewInMemoryRepository(); svc := TenantProvisioningService{Repository: repo}; ctx := context.Background()
	p, _ := svc.Plan(ctx,"tn_zuribeans","idem-1","hash-1",nil,nil); p, _ = svc.Apply(ctx,p.ID); p, _ = svc.Reconcile(ctx,p.ID)
	if _, err := svc.MarkReady(ctx,p.ID,provisioningdomain.ProvisioningReadiness{ProvisioningID:p.ID}); err == nil { t.Fatal("expected incomplete readiness evidence to be rejected") }
}

func TestTenantProvisioningServiceRejectsSkippingReady(t *testing.T) {
	repo := repository.NewInMemoryRepository(); svc := TenantProvisioningService{Repository: repo}; ctx := context.Background()
	p, _ := svc.Plan(ctx,"tn_zuribeans","idem-1","hash-1",nil,nil)
	if _, err := svc.Activate(ctx,p.ID,readyEvidence(p.ID)); err == nil { t.Fatal("expected PLAN -> ACTIVE to be rejected") }
}

func TestTenantProvisioningServiceFailAndRetry(t *testing.T) {
	repo := repository.NewInMemoryRepository(); svc := TenantProvisioningService{Repository: repo}; ctx := context.Background()
	p, _ := svc.Plan(ctx,"tn_zuribeans","idem-1","hash-1",nil,nil); p, _ = svc.Apply(ctx,p.ID)
	p, err := svc.Fail(ctx,p.ID,"provider timeout"); if err != nil { t.Fatal(err) }
	if p.Status != provisioningdomain.ProvisioningStatusFailed || p.AttemptCount != 1 { t.Fatalf("unexpected failed provisioning: %+v",p) }
	p, err = svc.Retry(ctx,p.ID); if err != nil { t.Fatal(err) }
	if p.Status != provisioningdomain.ProvisioningStatusApply || p.LastError != "" { t.Fatalf("unexpected retry: %+v",p) }
}

func TestTenantProvisioningServiceCancel(t *testing.T) {
	repo := repository.NewInMemoryRepository(); svc := TenantProvisioningService{Repository: repo}; ctx := context.Background()
	p, _ := svc.Plan(ctx,"tn_zuribeans","idem-1","hash-1",nil,nil); p, err := svc.Cancel(ctx,p.ID,"operator requested"); if err != nil { t.Fatal(err) }
	if p.Status != provisioningdomain.ProvisioningStatusCancelled { t.Fatalf("expected CANCELLED, got %s",p.Status) }
}
