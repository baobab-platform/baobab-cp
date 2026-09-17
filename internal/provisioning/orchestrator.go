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
	"log/slog"
	"time"

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
		phaseStarted := time.Now()

		switch op.Status {
		case provisioningdomain.ProvisioningStatusPlan:
			if op, err = o.advance(ctx, op, provisioningdomain.ProvisioningStatusApply, ""); err != nil {
				return op, err
			}
			logPhaseOutcome(ctx, op, "PLAN", phaseStarted, "advanced")
		case provisioningdomain.ProvisioningStatusApply:
			result, runErr := o.apply.Run(ctx, op)
			if runErr != nil {
				return o.fail(ctx, op, "APPLY", phaseStarted, runErr)
			}
			op.ObservedStateVersion = max64(op.ObservedStateVersion, result.ObservedStateVersion)
			op.BlockingReasons = append([]string(nil), result.BlockingReasons...)
			if op, err = o.save(ctx, op); err != nil {
				return op, err
			}
			if op, err = o.advance(ctx, op, provisioningdomain.ProvisioningStatusReconcile, ""); err != nil {
				return op, err
			}
			logPhaseOutcome(ctx, op, "APPLY", phaseStarted, "advanced")
		case provisioningdomain.ProvisioningStatusReconcile:
			result, runErr := o.reconcile.Run(ctx, op)
			if runErr != nil {
				return o.fail(ctx, op, "RECONCILE", phaseStarted, runErr)
			}
			op.ObservedStateVersion = result.ObservedStateVersion
			op.BlockingReasons = append([]string(nil), result.BlockingReasons...)
			if op, err = o.save(ctx, op); err != nil {
				return op, err
			}
			if op.ObservedStateVersion != op.DesiredStateVersion || len(op.BlockingReasons) != 0 {
				// Remain in RECONCILE. A later invocation can retry after
				// providers/resources converge; never manufacture readiness.
				logPhaseOutcome(ctx, op, "RECONCILE", phaseStarted, "blocked")
				return op, nil
			}
			if op, err = o.advance(ctx, op, provisioningdomain.ProvisioningStatusReady, ""); err != nil {
				return op, err
			}
			logPhaseOutcome(ctx, op, "RECONCILE", phaseStarted, "converged")
		case provisioningdomain.ProvisioningStatusReady:
			result, runErr := o.readiness.Run(ctx, op)
			if runErr != nil {
				return o.fail(ctx, op, "READY", phaseStarted, runErr)
			}
			if result.ObservedStateVersion != op.DesiredStateVersion || len(result.BlockingReasons) != 0 {
				op.ObservedStateVersion = result.ObservedStateVersion
				op.BlockingReasons = append([]string(nil), result.BlockingReasons...)
				if op, err = o.save(ctx, op); err != nil {
					return op, err
				}
				logPhaseOutcome(ctx, op, "READY", phaseStarted, "blocked")
				return op, nil
			}
			active, err := o.advance(ctx, op, provisioningdomain.ProvisioningStatusActive, "")
			if err != nil {
				return active, err
			}
			logPhaseOutcome(ctx, active, "READY", phaseStarted, "advanced")
			return active, nil
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

func (o *Orchestrator) fail(ctx context.Context, op provisioningdomain.TenantProvisioning, phase string, phaseStarted time.Time, cause error) (provisioningdomain.TenantProvisioning, error) {
	failed, err := o.advance(ctx, op, provisioningdomain.ProvisioningStatusFailed, phase+": "+cause.Error())
	if err != nil {
		slog.ErrorContext(ctx, "provisioning phase failed and could not be recorded",
			"tenant_id", op.TenantID, "provisioning_id", op.ID, "correlation_id", op.ID,
			"phase", phase, "attempt", op.AttemptCount, "duration_ms", time.Since(phaseStarted).Milliseconds(),
			"outcome", "failed", "error", cause, "record_error", err)
		return op, errors.Join(cause, err)
	}
	slog.ErrorContext(ctx, "provisioning phase failed",
		"tenant_id", failed.TenantID, "provisioning_id", failed.ID, "correlation_id", failed.ID,
		"phase", phase, "desired_state_version", failed.DesiredStateVersion, "observed_state_version", failed.ObservedStateVersion,
		"attempt", failed.AttemptCount, "duration_ms", time.Since(phaseStarted).Milliseconds(),
		"outcome", "failed", "error", cause)
	return failed, cause
}

// logPhaseOutcome emits one structured, secret-free log line per completed
// orchestration phase (ADR-BCP-010 §47's dimensions: tenant, operation,
// phase, state-version convergence, attempt count, duration, outcome).
// correlation_id mirrors provisioning_id deliberately: it is the same
// UUIDv7 internal/repository's outbox wiring uses as the CorrelationID on
// this operation's milestone events, so orchestration logs and emitted
// events can be joined on one key.
func logPhaseOutcome(ctx context.Context, op provisioningdomain.TenantProvisioning, phase string, phaseStarted time.Time, outcome string) {
	slog.InfoContext(ctx, "provisioning phase completed",
		"tenant_id", op.TenantID, "provisioning_id", op.ID, "correlation_id", op.ID,
		"phase", phase, "desired_state_version", op.DesiredStateVersion, "observed_state_version", op.ObservedStateVersion,
		"attempt", op.AttemptCount, "blocking_reasons", len(op.BlockingReasons),
		"duration_ms", time.Since(phaseStarted).Milliseconds(), "outcome", outcome)
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
