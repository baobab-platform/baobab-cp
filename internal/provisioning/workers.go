// Target path: internal/provisioning/workers.go
package provisioning

import (
	"context"
	"errors"
	"log/slog"
	"time"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
)

// ApplyStep is one idempotent desired-state materializer, e.g.
// MarketParticipation, CapabilityGrant, CapabilityBinding, EngineInstance
// selection inputs, Context prerequisites or TradeLane.
type ApplyStep interface {
	Key() string
	Apply(ctx context.Context, op provisioningdomain.TenantProvisioning) error
}

type ApplyWorker struct{ Steps []ApplyStep }

func (ApplyWorker) Name() string { return "APPLY" }
func (w ApplyWorker) Run(ctx context.Context, op provisioningdomain.TenantProvisioning) (PhaseResult, error) {
	if len(w.Steps) == 0 {
		return PhaseResult{}, errors.New("APPLY has no configured materializers")
	}
	for _, step := range w.Steps {
		if step == nil {
			return PhaseResult{}, errors.New("nil APPLY step")
		}
		stepStarted := time.Now()
		if err := step.Apply(ctx, op); err != nil {
			slog.ErrorContext(ctx, "provisioning apply step failed",
				"tenant_id", op.TenantID, "provisioning_id", op.ID, "correlation_id", op.ID,
				"phase", "APPLY", "resource_type", step.Key(), "attempt", op.AttemptCount,
				"duration_ms", time.Since(stepStarted).Milliseconds(), "outcome", "failed", "error", err)
			return PhaseResult{}, err
		}
		slog.InfoContext(ctx, "provisioning apply step completed",
			"tenant_id", op.TenantID, "provisioning_id", op.ID, "correlation_id", op.ID,
			"phase", "APPLY", "resource_type", step.Key(), "attempt", op.AttemptCount,
			"duration_ms", time.Since(stepStarted).Milliseconds(), "outcome", "applied")
	}
	// Applying desired state does not itself prove observation/convergence.
	return PhaseResult{ObservedStateVersion: op.ObservedStateVersion}, nil
}

type Reconciler interface {
	Reconcile(ctx context.Context, op provisioningdomain.TenantProvisioning) (observedVersion int64, blockers []string, err error)
}
type ReconcileWorker struct{ Reconciler Reconciler }

func (ReconcileWorker) Name() string { return "RECONCILE" }
func (w ReconcileWorker) Run(ctx context.Context, op provisioningdomain.TenantProvisioning) (PhaseResult, error) {
	if w.Reconciler == nil {
		return PhaseResult{}, errors.New("reconciler is required")
	}
	v, b, err := w.Reconciler.Reconcile(ctx, op)
	return PhaseResult{ObservedStateVersion: v, BlockingReasons: b}, err
}

type ReadinessEvaluator interface {
	Evaluate(ctx context.Context, op provisioningdomain.TenantProvisioning) (observedVersion int64, blockers []string, evidence map[string]string, err error)
}
type ReadinessWorker struct{ Evaluator ReadinessEvaluator }

func (ReadinessWorker) Name() string { return "READY" }
func (w ReadinessWorker) Run(ctx context.Context, op provisioningdomain.TenantProvisioning) (PhaseResult, error) {
	if w.Evaluator == nil {
		return PhaseResult{}, errors.New("readiness evaluator is required")
	}
	v, b, e, err := w.Evaluator.Evaluate(ctx, op)
	return PhaseResult{ObservedStateVersion: v, BlockingReasons: b, Evidence: e}, err
}
