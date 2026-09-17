// Target path: internal/provisioning/domain/readiness_snapshot_record.go
package domain

import "time"

// ReadinessCheckRecord is one persisted probe result within a
// ReadinessSnapshotRecord (Gate ZB-03.1, migration 000043). CheckKey
// mirrors internal/provisioning.ReadinessEvidence.Check (e.g.
// "market-participation", "capability-grants"); ResourceType is the same
// value -- at readiness granularity a check evaluates a whole resource
// family for the tenant, not one specific instance (contrast with
// reconciliation drift, which is per-instance and already logged with a
// distinct resource_id in internal/provisioning/reconciliation.go's
// logDrift).
type ReadinessCheckRecord struct {
	CheckKey          string    `json:"check_key"`
	ResourceType      string    `json:"resource_type"`
	Status            string    `json:"status"`
	Reason            string    `json:"reason,omitempty"`
	EvidenceReference string    `json:"evidence_reference,omitempty"`
	EvaluatedAt       time.Time `json:"evaluated_at"`
}

// ReadinessSnapshotRecord is the persisted, immutable evidence record for
// one readiness evaluation of a TenantProvisioning run. Closes the ZB-02
// deferred item docs/reconciliation/gate-zb02-completion-report.md flagged
// as NOT IMPLEMENTED ("Persisted readiness snapshots") -- ReadinessReport
// (internal/provisioning/readiness.go) was previously computed on demand
// only, with no durable, queryable record of why a tenant was or was not
// READY at a given point in time. A later evaluation produces a new
// snapshot; existing snapshots are never updated in place.
type ReadinessSnapshotRecord struct {
	ID                   string                 `json:"id,omitempty"`
	TenantProvisioningID string                 `json:"tenant_provisioning_id"`
	TenantID             string                 `json:"tenant_id"`
	DesiredStateVersion  int64                  `json:"desired_state_version"`
	ObservedStateVersion int64                  `json:"observed_state_version"`
	OverallReady         bool                   `json:"overall_ready"`
	BlockingReasons      []string               `json:"blocking_reasons,omitempty"`
	EvaluatedAt          time.Time              `json:"evaluated_at"`
	Checks               []ReadinessCheckRecord `json:"checks,omitempty"`
	CreatedAt            time.Time              `json:"created_at,omitempty"`
}
