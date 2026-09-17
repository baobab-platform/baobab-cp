// Target path: internal/provisioning/domain/reconciliation_snapshot_record.go
package domain

import "time"

// ResourceDriftRecord is one persisted drift finding within a
// ReconciliationSnapshotRecord (Gate ZB-03.1, migration 000044). Mirrors
// internal/provisioning.Drift.
//
// ResolvedAt is intentionally always nil as of this pass: reconciliation
// evaluates the tenant's *entire* current desired/observed state on every
// run (internal/provisioning.DesiredObservedReconciler.Report), it does not
// track individual drift findings as tickets across runs to know when a
// specific one stopped recurring. Tracking that would be a real, separate
// feature (matching drift identity across snapshots by resource_type +
// resource_id), not implied by "persist the evidence this pass already
// computes." The column exists so a future pass can add that without a
// migration.
type ResourceDriftRecord struct {
	ResourceType string
	ResourceID   string
	DriftKind    string
	DesiredHash  string
	ObservedHash string
	Repairable   bool
	Blocking     bool
	Reason       string
	DetectedAt   time.Time
	ResolvedAt   *time.Time
}

// ReconciliationSnapshotRecord is the persisted, immutable evidence record
// for one RECONCILE-phase evaluation of a TenantProvisioning run. Closes
// the ZB-02 deferred item docs/reconciliation/gate-zb02-completion-
// report.md flagged as NOT IMPLEMENTED ("Persisted reconciliation/drift
// snapshots") -- ReconciliationReport (internal/provisioning/
// reconciliation.go) was previously computed on demand only. A later
// evaluation produces a new snapshot; existing snapshots are never updated
// in place.
type ReconciliationSnapshotRecord struct {
	ID                   string
	TenantProvisioningID string
	TenantID             string
	DesiredStateVersion  int64
	ObservedStateVersion int64
	Converged            bool
	EvaluatedAt          time.Time
	Drift                []ResourceDriftRecord
	CreatedAt            time.Time
}
