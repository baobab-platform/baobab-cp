// Target path: internal/provisioning/orchestrator.go
//
// ZB-02 orchestration over the existing persisted PLAN -> APPLY -> RECONCILE
// -> READY -> ACTIVE aggregate. Phase workers own business logic; the
// orchestrator owns ordering, fail-closed transitions and retry-safe control.
package provisioning

import (
	"context"
	"errors"
	"fmt"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
)

type TenantProvisioningStore interface {
	GetTenantProvisioning(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error)
	UpdateTenantProvisioning(ctx context.Context, next provisioningdomain.TenantProvisioning, expectedVersion int64) error
}

type PhaseWorker interface {
	Name() string
	Run(ctx context.Context, operation provisioningdomain.TenantProvisioning) (PhaseResult, error)
}

type PhaseResult struct {
	ObservedStateVersion int64
	BlockingReasons      []string
	Evidence             map[string]string
}

type Orchestrator struct {
	store     TenantProvisioningStore
	apply     PhaseWorker
	reconcile PhaseWorker
	readiness PhaseWorker
}

func NewOrchestrator(store TenantProvisioningStore, apply, reconcile, readiness PhaseWorker) *Orchestrator {
	return &Orchestrator{store: store, apply: apply, reconcile: reconcile, readiness: readiness}
}

// Run advances one operation as far as deterministic phase results allow.
// It never marks READY/ACTIVE merely because a worker returned nil: version
// convergence and zero blockers are explicit readiness invariants.
func (o *Orchestrator) Run(ctx context.Context, operationID string) (provisioningdomain.TenantProvisioning, error) {
	if o == nil || o.store == nil || o.apply == nil || o.reconcile == nil || o.readiness == nil {
		return provisioningdomain.TenantProvisioning{}, errors.New("provisioning orchestrator is not initialized")
	}
	for {
		op, err := o.store.GetTenantProvisioning(ctx, operationID)
		if err != nil {
			return provisioningdomain.TenantProvisioning{}, err
		}

		switch op.Status {
		case provisioningdomain.ProvisioningStatusPlan:
			if op, err = o.advance(ctx, op, provisioningdomain.ProvisioningStatusApply, ""); err != nil {
				return op, err
			}
		case provisioningdomain.ProvisioningStatusApply:
			result, runErr := o.apply.Run(ctx, op)
			if runErr != nil {
				return o.fail(ctx, op, "APPLY", runErr)
			}
			op.ObservedStateVersion = max64(op.ObservedStateVersion, result.ObservedStateVersion)
			op.BlockingReasons = append([]string(nil), result.BlockingReasons...)
			if op, err = o.save(ctx, op); err != nil {
				return op, err
			}
			if op, err = o.advance(ctx, op, provisioningdomain.ProvisioningStatusReconcile, ""); err != nil {
				return op, err
			}
		case provisioningdomain.ProvisioningStatusReconcile:
			result, runErr := o.reconcile.Run(ctx, op)
			if runErr != nil {
				return o.fail(ctx, op, "RECONCILE", runErr)
			}
			op.ObservedStateVersion = result.ObservedStateVersion
			op.BlockingReasons = append([]string(nil), result.BlockingReasons...)
			if op, err = o.save(ctx, op); err != nil {
				return op, err
			}
			if op.ObservedStateVersion != op.DesiredStateVersion || len(op.BlockingReasons) != 0 {
				// Remain in RECONCILE. A later invocation can retry after
				// providers/resources converge; never manufacture readiness.
				return op, nil
			}
			if op, err = o.advance(ctx, op, provisioningdomain.ProvisioningStatusReady, ""); err != nil {
				return op, err
			}
		case provisioningdomain.ProvisioningStatusReady:
			result, runErr := o.readiness.Run(ctx, op)
			if runErr != nil {
				return o.fail(ctx, op, "READY", runErr)
			}
			if result.ObservedStateVersion != op.DesiredStateVersion || len(result.BlockingReasons) != 0 {
				op.ObservedStateVersion = result.ObservedStateVersion
				op.BlockingReasons = append([]string(nil), result.BlockingReasons...)
				if op, err = o.save(ctx, op); err != nil {
					return op, err
				}
				return op, nil
			}
			return o.advance(ctx, op, provisioningdomain.ProvisioningStatusActive, "")
		case provisioningdomain.ProvisioningStatusActive,
			provisioningdomain.ProvisioningStatusCancelled:
			return op, nil
		case provisioningdomain.ProvisioningStatusFailed:
			return op, nil
		default:
			return op, fmt.Errorf("unsupported provisioning status %q", op.Status)
		}
	}
}

func (o *Orchestrator) Retry(ctx context.Context, operationID string) (provisioningdomain.TenantProvisioning, error) {
	op, err := o.store.GetTenantProvisioning(ctx, operationID)
	if err != nil {
		return op, err
	}
	if op.Status != provisioningdomain.ProvisioningStatusFailed {
		return op, errors.New("only FAILED provisioning can be retried")
	}
	if op, err = o.advance(ctx, op, provisioningdomain.ProvisioningStatusApply, ""); err != nil {
		return op, err
	}
	return o.Run(ctx, operationID)
}

func (o *Orchestrator) fail(ctx context.Context, op provisioningdomain.TenantProvisioning, phase string, cause error) (provisioningdomain.TenantProvisioning, error) {
	failed, err := o.advance(ctx, op, provisioningdomain.ProvisioningStatusFailed, phase+": "+cause.Error())
	if err != nil {
		return op, errors.Join(cause, err)
	}
	return failed, cause
}

func (o *Orchestrator) advance(ctx context.Context, op provisioningdomain.TenantProvisioning, to provisioningdomain.ProvisioningStatus, reason string) (provisioningdomain.TenantProvisioning, error) {
	expected := op.Version
	next, err := op.Advance(to, reason)
	if err != nil {
		return op, err
	}
	if err := o.store.UpdateTenantProvisioning(ctx, next, expected); err != nil {
		return op, err
	}
	return next, nil
}

func (o *Orchestrator) save(ctx context.Context, op provisioningdomain.TenantProvisioning) (provisioningdomain.TenantProvisioning, error) {
	expected := op.Version
	op.Version++
	if err := o.store.UpdateTenantProvisioning(ctx, op, expected); err != nil {
		return op, err
	}
	return op, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
