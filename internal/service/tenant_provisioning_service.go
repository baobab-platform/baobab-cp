package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// TenantProvisioningService drives Gate ZB-02's PLAN -> APPLY -> RECONCILE
// -> READY -> ACTIVE lifecycle. READY and ACTIVE are fail-closed: callers
// must supply readiness evidence proving all required control-plane
// resources and desired/observed state have converged.
type TenantProvisioningService struct {
	Repository repository.TenantProvisioningRepository
	Now        func() time.Time
}

func (s TenantProvisioningService) now() time.Time {
	if s.Now != nil { return s.Now() }
	return time.Now().UTC()
}

func (s TenantProvisioningService) Plan(ctx context.Context, tenantID, idempotencyKey, requestHash string, productRequests, marketRequests []string) (provisioningdomain.TenantProvisioning, error) {
	if s.Repository == nil { return provisioningdomain.TenantProvisioning{}, errors.New("tenant provisioning repository is required") }
	existing, err := s.Repository.GetTenantProvisioningByIdempotencyKey(ctx, tenantID, idempotencyKey)
	if err == nil { return existing, nil }
	p := provisioningdomain.TenantProvisioning{ID: domain.NewUUIDv7(), TenantID: tenantID, IdempotencyKey: idempotencyKey, RequestHash: requestHash, Status: provisioningdomain.ProvisioningStatusPlan, ProductRequests: productRequests, MarketRequests: marketRequests, StartedAt: s.now(), Version: 1}
	if err := s.Repository.CreateTenantProvisioning(ctx, p); err != nil {
		if errors.Is(err, repository.ErrTenantProvisioningAlreadyExists) { return s.Repository.GetTenantProvisioningByIdempotencyKey(ctx, tenantID, idempotencyKey) }
		return provisioningdomain.TenantProvisioning{}, err
	}
	return p, nil
}

func (s TenantProvisioningService) advance(ctx context.Context, id string, to provisioningdomain.ProvisioningStatus, reason string) (provisioningdomain.TenantProvisioning, error) {
	if s.Repository == nil { return provisioningdomain.TenantProvisioning{}, errors.New("tenant provisioning repository is required") }
	current, err := s.Repository.GetTenantProvisioning(ctx, id)
	if err != nil { return provisioningdomain.TenantProvisioning{}, err }
	next, err := current.Advance(to, reason)
	if err != nil { return provisioningdomain.TenantProvisioning{}, fmt.Errorf("advance tenant provisioning %s: %w", id, err) }
	if err := s.Repository.UpdateTenantProvisioning(ctx, next, current.Version); err != nil { return provisioningdomain.TenantProvisioning{}, err }
	return next, nil
}

func (s TenantProvisioningService) Apply(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusApply, "")
}
func (s TenantProvisioningService) Reconcile(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusReconcile, "")
}

// MarkReady requires evidence for every ZB-02 prerequisite. This prevents
// an operator or API caller from declaring readiness merely by advancing a
// state machine while bindings, context or providers are absent.
func (s TenantProvisioningService) MarkReady(ctx context.Context, id string, readiness provisioningdomain.ProvisioningReadiness) (provisioningdomain.TenantProvisioning, error) {
	if readiness.ProvisioningID != id { return provisioningdomain.TenantProvisioning{}, errors.New("readiness evidence belongs to a different provisioning") }
	if err := readiness.Validate(); err != nil { return provisioningdomain.TenantProvisioning{}, fmt.Errorf("invalid readiness evidence: %w", err) }
	if !readiness.Ready() { return provisioningdomain.TenantProvisioning{}, fmt.Errorf("provisioning is blocked: %s", strings.Join(readiness.BlockingReasons(), "; ")) }
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusReady, "")
}

// Activate re-checks readiness so READY cannot become a stale bypass after
// desired state changes or a provider becomes unavailable.
func (s TenantProvisioningService) Activate(ctx context.Context, id string, readiness provisioningdomain.ProvisioningReadiness) (provisioningdomain.TenantProvisioning, error) {
	if readiness.ProvisioningID != id || !readiness.Ready() { return provisioningdomain.TenantProvisioning{}, errors.New("current complete readiness evidence is required for activation") }
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusActive, "")
}

func (s TenantProvisioningService) Fail(ctx context.Context, id, reason string) (provisioningdomain.TenantProvisioning, error) {
	if reason == "" { return provisioningdomain.TenantProvisioning{}, errors.New("a failure reason is required") }
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusFailed, reason)
}
func (s TenantProvisioningService) Retry(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusApply, "")
}
func (s TenantProvisioningService) Cancel(ctx context.Context, id, reason string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusCancelled, reason)
}
