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
	CheckKey          string
	ResourceType      string
	Status            string
	Reason            string
	EvidenceReference string
	EvaluatedAt       time.Time
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
	ID                   string
	TenantProvisioningID string
	TenantID             string
	DesiredStateVersion  int64
	ObservedStateVersion int64
	OverallReady         bool
	BlockingReasons      []string
	EvaluatedAt          time.Time
	Checks               []ReadinessCheckRecord
	CreatedAt            time.Time
}
