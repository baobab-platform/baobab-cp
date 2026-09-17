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

// ReadinessEvaluator is satisfied by *ReadinessEvaluatorImpl. Report (not a
// flattened tuple) is the interface method so ReadinessWorker.Run can
// persist the full evidence report, not just its summary fields.
type ReadinessEvaluator interface {
	Report(ctx context.Context, op provisioningdomain.TenantProvisioning) (ReadinessReport, error)
}

// ReadinessSnapshotStore persists immutable readiness evidence (Gate
// ZB-03.1, migration 000043). Optional: a nil Snapshots disables
// persistence without disabling readiness evaluation itself, matching this
// package's existing "basics" precedent for optional dependencies.
type ReadinessSnapshotStore interface {
	SaveReadinessSnapshot(ctx context.Context, snapshot provisioningdomain.ReadinessSnapshotRecord) (string, error)
}

type ReadinessWorker struct {
	Evaluator ReadinessEvaluator
	Snapshots ReadinessSnapshotStore
}

func (ReadinessWorker) Name() string { return "READY" }
func (w ReadinessWorker) Run(ctx context.Context, op provisioningdomain.TenantProvisioning) (PhaseResult, error) {
	if w.Evaluator == nil {
		return PhaseResult{}, errors.New("readiness evaluator is required")
	}
	report, err := w.Evaluator.Report(ctx, op)
	if err != nil {
		return PhaseResult{}, err
	}
	if w.Snapshots != nil {
		record := readinessSnapshotRecordFromReport(op.ID, report)
		if _, saveErr := w.Snapshots.SaveReadinessSnapshot(ctx, record); saveErr != nil {
			// A failure to persist evidence must not itself block
			// provisioning progress -- the in-memory report driving this
			// phase's PhaseResult below is unaffected either way. Logged,
			// not swallowed silently.
			slog.ErrorContext(ctx, "readiness snapshot persistence failed",
				"tenant_id", op.TenantID, "provisioning_id", op.ID, "correlation_id", op.ID,
				"phase", "READY", "error", saveErr)
		}
	}
	evidence := make(map[string]string, len(report.Evidence))
	for _, item := range report.Evidence {
		evidence[item.Check] = item.Reference
	}
	return PhaseResult{ObservedStateVersion: report.ObservedStateVersion, BlockingReasons: report.BlockingReasons, Evidence: evidence}, nil
}

// readinessSnapshotRecordFromReport converts a ReadinessReport into its
// persisted form. ResourceType mirrors CheckKey: at readiness granularity a
// check evaluates a whole resource family for the tenant, not one specific
// instance (see ReadinessCheckRecord's doc comment).
func readinessSnapshotRecordFromReport(provisioningID string, report ReadinessReport) provisioningdomain.ReadinessSnapshotRecord {
	checks := make([]provisioningdomain.ReadinessCheckRecord, 0, len(report.Evidence))
	for _, item := range report.Evidence {
		checks = append(checks, provisioningdomain.ReadinessCheckRecord{
			CheckKey: item.Check, ResourceType: item.Check, Status: string(item.Status),
			Reason: item.Reason, EvidenceReference: item.Reference, EvaluatedAt: item.ObservedAt,
		})
	}
	return provisioningdomain.ReadinessSnapshotRecord{
		TenantProvisioningID: provisioningID, TenantID: report.TenantID,
		DesiredStateVersion: report.DesiredStateVersion, ObservedStateVersion: report.ObservedStateVersion,
		OverallReady: report.Ready, BlockingReasons: report.BlockingReasons,
		EvaluatedAt: report.EvaluatedAt, Checks: checks,
	}
}
