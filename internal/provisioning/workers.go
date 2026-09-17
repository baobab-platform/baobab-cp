// Target path: internal/provisioning/workers.go
package provisioning

import (
	"context"
	"errors"
	"fmt"
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

// Reconciler is satisfied by DesiredObservedReconciler. Report (not a
// flattened tuple) is the interface method so ReconcileWorker.Run can
// persist the full drift evidence, not just its summary fields.
type Reconciler interface {
	Report(ctx context.Context, op provisioningdomain.TenantProvisioning) (ReconciliationReport, error)
}

// DriftSnapshotStore persists immutable reconciliation/drift evidence (Gate
// ZB-03.1, migration 000044). Optional: a nil Snapshots disables
// persistence without disabling reconciliation itself.
type DriftSnapshotStore interface {
	SaveReconciliationSnapshot(ctx context.Context, snapshot provisioningdomain.ReconciliationSnapshotRecord) (string, error)
}

type ReconcileWorker struct {
	Reconciler Reconciler
	Snapshots  DriftSnapshotStore
}

func (ReconcileWorker) Name() string { return "RECONCILE" }
func (w ReconcileWorker) Run(ctx context.Context, op provisioningdomain.TenantProvisioning) (PhaseResult, error) {
	if w.Reconciler == nil {
		return PhaseResult{}, errors.New("reconciler is required")
	}
	report, err := w.Reconciler.Report(ctx, op)
	if err != nil {
		return PhaseResult{}, err
	}
	if w.Snapshots != nil {
		record := reconciliationSnapshotRecordFromReport(op.ID, report)
		if _, saveErr := w.Snapshots.SaveReconciliationSnapshot(ctx, record); saveErr != nil {
			// Same posture as ReadinessWorker: a failure to persist
			// evidence must not itself block provisioning progress.
			slog.ErrorContext(ctx, "reconciliation snapshot persistence failed",
				"tenant_id", op.TenantID, "provisioning_id", op.ID, "correlation_id", op.ID,
				"phase", "RECONCILE", "error", saveErr)
		}
	}
	blockers := make([]string, 0, len(report.Drift))
	for _, d := range report.Drift {
		blockers = append(blockers, fmt.Sprintf("%s/%s: %s", d.ResourceType, d.ResourceKey, d.Reason))
	}
	return PhaseResult{ObservedStateVersion: report.ObservedStateVersion, BlockingReasons: blockers}, nil
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

// reconciliationSnapshotRecordFromReport converts a ReconciliationReport
// into its persisted form. Every Drift item is treated as blocking -- true
// of every drift finding this codebase produces today, since Reconciler.Report
// itself only ever advances ObservedStateVersion on zero drift (see
// DesiredObservedReconciler.Report). ResolvedAt is always nil; see
// ResourceDriftRecord's doc comment for why.
func reconciliationSnapshotRecordFromReport(provisioningID string, report ReconciliationReport) provisioningdomain.ReconciliationSnapshotRecord {
	drift := make([]provisioningdomain.ResourceDriftRecord, 0, len(report.Drift))
	for _, d := range report.Drift {
		drift = append(drift, provisioningdomain.ResourceDriftRecord{
			ResourceType: d.ResourceType, ResourceID: d.ResourceKey, DriftKind: string(d.Kind),
			DesiredHash: d.DesiredHash, ObservedHash: d.ObservedHash,
			Repairable: d.Repairable, Blocking: true, Reason: d.Reason, DetectedAt: report.EvaluatedAt,
		})
	}
	return provisioningdomain.ReconciliationSnapshotRecord{
		TenantProvisioningID: provisioningID, TenantID: report.TenantID,
		DesiredStateVersion: report.DesiredStateVersion, ObservedStateVersion: report.ObservedStateVersion,
		Converged: report.Converged, EvaluatedAt: report.EvaluatedAt, Drift: drift,
	}
}
