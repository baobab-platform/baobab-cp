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
	return report, nil
}
