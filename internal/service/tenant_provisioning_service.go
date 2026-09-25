package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// ErrTenantProvisioningIdempotencyConflict is returned by Plan when an
// idempotency key that already identifies a TenantProvisioning is reused
// with a different request_hash. Per the ZB-00/ZB-02 provisioning
// idempotency rule, a caller MUST NOT be allowed to apply a different
// desired state under an old idempotency identity: replay is only safe
// when the request is byte-for-byte (hash-for-hash) the same request.
var ErrTenantProvisioningIdempotencyConflict = errors.New("idempotency key reused with a different request")

// TenantProvisioningService drives TenantProvisioning through Gate ZB-02's
// PLAN -> APPLY -> RECONCILE -> READY -> ACTIVE lifecycle (Programme Gate
// P7 "basics" -- see internal/provisioning/domain's package doc for exactly
// what is and isn't in scope). It owns idempotent creation and optimistic-
// locked phase transitions; it does not decide when a phase's work is
// actually done -- callers (a future gate's phase orchestration) call
// Apply/Reconcile/MarkReady/Activate once their own work for that phase
// succeeds, and Fail/Retry when it doesn't.
type TenantProvisioningService struct {
	Repository repository.TenantProvisioningRepository
	Now        func() time.Time
}

func (s TenantProvisioningService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Plan idempotently creates a TenantProvisioning in PLAN status for
// (tenantID, idempotencyKey): a caller retrying the same onboarding
// request (identical requestHash) with the same idempotency key gets back
// the existing aggregate unchanged, rather than a duplicate or an error,
// matching Programme Gate P7's "Idempotency" requirement. Reusing the key
// with a *different* requestHash returns
// ErrTenantProvisioningIdempotencyConflict instead of silently replaying
// stale desired state under a new request.
func (s TenantProvisioningService) Plan(ctx context.Context, tenantID, idempotencyKey, requestHash string, productRequests, marketRequests []string) (provisioningdomain.TenantProvisioning, error) {
	if s.Repository == nil {
		return provisioningdomain.TenantProvisioning{}, errors.New("tenant provisioning repository is required")
	}
	existing, err := s.Repository.GetTenantProvisioningByIdempotencyKey(ctx, tenantID, idempotencyKey)
	if err == nil {
		if existing.RequestHash != requestHash {
			return provisioningdomain.TenantProvisioning{}, fmt.Errorf(
				"%w: tenant %s idempotency key %s", ErrTenantProvisioningIdempotencyConflict, tenantID, idempotencyKey,
			)
		}
		return existing, nil
	}

	provisioning := provisioningdomain.TenantProvisioning{
		ID:              domain.NewUUIDv7(),
		TenantID:        tenantID,
		IdempotencyKey:  idempotencyKey,
		RequestHash:     requestHash,
		Status:          provisioningdomain.ProvisioningStatusPlan,
		ProductRequests: productRequests,
		MarketRequests:  marketRequests,
		StartedAt:       s.now(),
		Version:         1,
	}
	if err := s.Repository.CreateTenantProvisioning(ctx, provisioning); err != nil {
		// A concurrent caller may have just created the same
		// (tenant_id, idempotency_key) row. Re-read and apply the same
		// hash check as above rather than assuming it is a safe replay --
		// two concurrent callers racing with genuinely different desired
		// state under the same key must still conflict, not silently let
		// whichever write won decide the outcome.
		if errors.Is(err, repository.ErrTenantProvisioningAlreadyExists) {
			raced, getErr := s.Repository.GetTenantProvisioningByIdempotencyKey(ctx, tenantID, idempotencyKey)
			if getErr != nil {
				return provisioningdomain.TenantProvisioning{}, getErr
			}
			if raced.RequestHash != requestHash {
				return provisioningdomain.TenantProvisioning{}, fmt.Errorf(
					"%w: tenant %s idempotency key %s", ErrTenantProvisioningIdempotencyConflict, tenantID, idempotencyKey,
				)
			}
			return raced, nil
		}
		return provisioningdomain.TenantProvisioning{}, err
	}
	return provisioning, nil
}

// advance loads id, applies the requested transition, and persists the
// result under an optimistic lock. It never retries a version conflict
// itself -- that decision (re-read and re-apply, or surface the conflict)
// belongs to the caller, exactly as UpdateTenantProvisioning's contract
// requires.
func (s TenantProvisioningService) advance(ctx context.Context, id string, to provisioningdomain.ProvisioningStatus, reason string) (provisioningdomain.TenantProvisioning, error) {
	if s.Repository == nil {
		return provisioningdomain.TenantProvisioning{}, errors.New("tenant provisioning repository is required")
	}
	current, err := s.Repository.GetTenantProvisioning(ctx, id)
	if err != nil {
		return provisioningdomain.TenantProvisioning{}, err
	}
	next, err := current.Advance(to, reason)
	if err != nil {
		return provisioningdomain.TenantProvisioning{}, fmt.Errorf("advance tenant provisioning %s: %w", id, err)
	}
	if err := s.Repository.UpdateTenantProvisioning(ctx, next, current.Version); err != nil {
		return provisioningdomain.TenantProvisioning{}, err
	}
	return next, nil
}

// Apply transitions PLAN -> APPLY, or retries FAILED -> APPLY.
func (s TenantProvisioningService) Apply(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusApply, "")
}

// Reconcile transitions APPLY -> RECONCILE.
func (s TenantProvisioningService) Reconcile(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusReconcile, "")
}

// MarkReady transitions RECONCILE -> READY.
func (s TenantProvisioningService) MarkReady(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusReady, "")
}

// Activate transitions READY -> ACTIVE. This is Gate ZB-02's own explicit
// warning made structural: TenantProvisioning cannot reach ACTIVE by
// skipping READY (Technical Specification SS23, "No Partial Hidden
// Activation") -- TransitionProvisioning simply has no such edge.
func (s TenantProvisioningService) Activate(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusActive, "")
}

// Fail records a failed attempt from any non-terminal phase. The failure
// is not itself the end of the process: Retry can move it back to APPLY.
func (s TenantProvisioningService) Fail(ctx context.Context, id, reason string) (provisioningdomain.TenantProvisioning, error) {
	if reason == "" {
		return provisioningdomain.TenantProvisioning{}, errors.New("a failure reason is required")
	}
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusFailed, reason)
}

// Retry re-attempts a FAILED provisioning by moving it back to APPLY.
// Progress claimed past APPLY (RECONCILE/READY) is not restored --
// re-running APPLY is expected to re-derive it.
func (s TenantProvisioningService) Retry(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusApply, "")
}

// Cancel transitions any non-terminal phase to CANCELLED.
func (s TenantProvisioningService) Cancel(ctx context.Context, id, reason string) (provisioningdomain.TenantProvisioning, error) {
	return s.advance(ctx, id, provisioningdomain.ProvisioningStatusCancelled, reason)
}
