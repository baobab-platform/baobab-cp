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
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	DriftKind    string     `json:"drift_kind"`
	DesiredHash  string     `json:"desired_hash,omitempty"`
	ObservedHash string     `json:"observed_hash,omitempty"`
	Repairable   bool       `json:"repairable"`
	Blocking     bool       `json:"blocking"`
	Reason       string     `json:"reason"`
	DetectedAt   time.Time  `json:"detected_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
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
	ID                   string                `json:"id,omitempty"`
	TenantProvisioningID string                `json:"tenant_provisioning_id"`
	TenantID             string                `json:"tenant_id"`
	DesiredStateVersion  int64                 `json:"desired_state_version"`
	ObservedStateVersion int64                 `json:"observed_state_version"`
	Converged            bool                  `json:"converged"`
	EvaluatedAt          time.Time             `json:"evaluated_at"`
	Drift                []ResourceDriftRecord `json:"drift,omitempty"`
	CreatedAt            time.Time             `json:"created_at,omitempty"`
}
