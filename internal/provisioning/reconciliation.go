// Target path: internal/provisioning/reconciliation.go
//
// ZB-02 desired/observed reconciliation. Reconciliation is deterministic,
// read-after-write verification: it never declares convergence from APPLY
// success alone and never silently repairs unexpected destructive drift.
package provisioning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
)

type DriftKind string

const (
	DriftMissing    DriftKind = "MISSING"
	DriftMismatch   DriftKind = "MISMATCH"
	DriftUnexpected DriftKind = "UNEXPECTED"
)

type Drift struct {
	ResourceType string
	ResourceKey  string
	Kind         DriftKind
	Reason       string
	Repairable   bool
}

type ResourceObservation struct {
	ResourceType string
	ResourceKey  string
	DesiredHash  string
	ObservedHash string
	Present      bool
}

type ReconciliationReport struct {
	DesiredStateVersion  int64
	ObservedStateVersion int64
	Converged            bool
	Drift                []Drift
}

type ResourceReconciler interface {
	Key() string
	ReconcileResource(ctx context.Context, op provisioningdomain.TenantProvisioning) ([]Drift, error)
}

type DesiredObservedReconciler struct {
	Resources []ResourceReconciler
}

func (r DesiredObservedReconciler) Reconcile(
	ctx context.Context,
	op provisioningdomain.TenantProvisioning,
) (int64, []string, error) {
	report, err := r.Report(ctx, op)
	if err != nil {
		return op.ObservedStateVersion, nil, err
	}
	blockers := make([]string, 0, len(report.Drift))
	for _, d := range report.Drift {
		blockers = append(blockers, fmt.Sprintf("%s/%s: %s", d.ResourceType, d.ResourceKey, d.Reason))
	}
	return report.ObservedStateVersion, blockers, nil
}

func (r DesiredObservedReconciler) Report(
	ctx context.Context,
	op provisioningdomain.TenantProvisioning,
) (ReconciliationReport, error) {
	if len(r.Resources) == 0 {
		return ReconciliationReport{}, errors.New("at least one resource reconciler is required")
	}
	report := ReconciliationReport{
		DesiredStateVersion:  op.DesiredStateVersion,
		ObservedStateVersion: op.ObservedStateVersion,
	}
	seen := map[string]struct{}{}
	for _, resource := range r.Resources {
		if resource == nil || resource.Key() == "" {
			return ReconciliationReport{}, errors.New("resource reconciler key is required")
		}
		if _, ok := seen[resource.Key()]; ok {
			return ReconciliationReport{}, fmt.Errorf("duplicate resource reconciler %q", resource.Key())
		}
		seen[resource.Key()] = struct{}{}
		drift, err := resource.ReconcileResource(ctx, op)
		if err != nil {
			return ReconciliationReport{}, fmt.Errorf("%s reconciliation: %w", resource.Key(), err)
		}
		report.Drift = append(report.Drift, drift...)
	}
	sort.Slice(report.Drift, func(i, j int) bool {
		if report.Drift[i].ResourceType != report.Drift[j].ResourceType {
			return report.Drift[i].ResourceType < report.Drift[j].ResourceType
		}
		if report.Drift[i].ResourceKey != report.Drift[j].ResourceKey {
			return report.Drift[i].ResourceKey < report.Drift[j].ResourceKey
		}
		return report.Drift[i].Kind < report.Drift[j].Kind
	})
	if len(report.Drift) == 0 {
		report.ObservedStateVersion = op.DesiredStateVersion
		report.Converged = true
	}
	logDrift(ctx, op, report.Drift)
	return report, nil
}

// logDrift emits one structured, secret-free log line per drifted resource
// (ADR-BCP-010 §47's "resource_type/id" dimension) -- Drift.Reason is a
// fixed, non-user-controlled string (see the Drift constructions in
// reconciliation_resource.go), never provider/request payload content.
func logDrift(ctx context.Context, op provisioningdomain.TenantProvisioning, drift []Drift) {
	for _, d := range drift {
		slog.WarnContext(ctx, "provisioning resource drift detected",
			"tenant_id", op.TenantID, "provisioning_id", op.ID, "correlation_id", op.ID,
			"phase", "RECONCILE", "resource_type", d.ResourceType, "resource_id", d.ResourceKey,
			"drift_kind", d.Kind, "repairable", d.Repairable, "reason", d.Reason)
	}
}
