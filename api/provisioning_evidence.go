package api

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// The readiness and drift evidence of a provisioning, as control-plane/v1
// ProvisioningReadiness and ProvisioningDrift (ADR-SHARED-015): projections
// of the snapshots each evaluation persists, never asserted independently
// of them.

// ProvisioningReadiness is the tenant-level verdict with the snapshots it
// derives from.
type ProvisioningReadiness struct {
	TenantProvisioningID string              `json:"tenant_provisioning_id"`
	TenantID             string              `json:"tenant_id"`
	Status               string              `json:"status"`
	EvaluatedAt          *time.Time          `json:"evaluated_at,omitempty"`
	Snapshots            []ReadinessSnapshot `json:"snapshots"`
}

// ReadinessSnapshot is one evaluation at one level of the readiness
// hierarchy.
type ReadinessSnapshot struct {
	ID              string           `json:"readiness_snapshot_id"`
	TenantID        string           `json:"tenant_id"`
	Level           string           `json:"level"`
	Status          string           `json:"status"`
	BlockingReasons []BlockingReason `json:"blocking_reasons"`
	ComputedAt      time.Time        `json:"computed_at"`
}

// BlockingReason is a registered reason code (authorization/v1
// reason-code-registry.yaml) with what the check found.
type BlockingReason struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// ProvisioningDrift is the per-object drift of the latest reconciliation.
type ProvisioningDrift struct {
	TenantProvisioningID string        `json:"tenant_provisioning_id"`
	TenantID             string        `json:"tenant_id"`
	DesiredStateVersion  int64         `json:"desired_state_version"`
	ObservedAt           *time.Time    `json:"observed_at,omitempty"`
	Items                []DriftRecord `json:"items"`
}

// DriftRecord is one object whose observed state differs from its desired
// state.
type DriftRecord struct {
	ID                  string     `json:"drift_record_id"`
	TenantID            string     `json:"tenant_id"`
	ObjectType          string     `json:"object_type"`
	ObjectReference     string     `json:"object_reference"`
	DesiredStateVersion int64      `json:"desired_state_version"`
	DesiredStateDigest  string     `json:"desired_state_digest,omitempty"`
	ObservedStateDigest string     `json:"observed_state_digest,omitempty"`
	Description         string     `json:"drift_description"`
	SafeToReconcile     bool       `json:"safe_to_reconcile"`
	Resolution          string     `json:"resolution"`
	DetectedAt          time.Time  `json:"detected_at"`
	ReconciledAt        *time.Time `json:"reconciled_at,omitempty"`
	ReconciliationRunID string     `json:"reconciliation_run_id"`
}

// readinessCheckCodes is the registered code each readiness check reports
// when it fails: the capability_resolution_denial code where one names the
// condition, otherwise a provisioning_blocker code.
var readinessCheckCodes = map[string]string{
	"market-participation":    "MARKET_PARTICIPATION_NOT_READY",
	"capability-grants":       "GRANT_NOT_FOUND",
	"capability-bindings":     "BINDING_NOT_FOUND",
	"engine-instances":        "PROVIDER_UNAVAILABLE",
	"context-resolution":      "CONTEXT_INVALID",
	"trade-lanes":             "TRADE_LANE_NOT_READY",
	"isolation-and-residency": "ISOLATION_POLICY_MISMATCH",
}

// driftObjectTypes is the driftObjectType of each reconciled resource type.
var driftObjectTypes = map[string]string{
	"market-participation":               "MARKET_PARTICIPATION",
	"capability-grant":                   "CAPABILITY_GRANT",
	"capability-binding-engine-instance": "CAPABILITY_BINDING",
	"trade-lane":                         "TRADE_LANE",
}

// provisioningReadiness is the latest evaluation's verdict. Readiness
// checks are tenant-wide, so each evaluation is one TENANT snapshot. Never
// evaluated is UNKNOWN.
func provisioningReadiness(c repository.ConvergedProvisioning, snapshots []provisioningdomain.ReadinessSnapshotRecord) ProvisioningReadiness {
	out := ProvisioningReadiness{TenantProvisioningID: c.Key, TenantID: c.TenantID, Status: "UNKNOWN", Snapshots: []ReadinessSnapshot{}}
	if len(snapshots) == 0 {
		return out
	}
	latest := snapshots[0]
	snapshot := ReadinessSnapshot{ID: "ready_" + compactID(latest.ID), TenantID: c.TenantID, Level: "TENANT",
		Status: readinessVerdict(latest), BlockingReasons: []BlockingReason{}, ComputedAt: latest.EvaluatedAt}
	if !latest.OverallReady {
		for _, check := range latest.Checks {
			if code, known := readinessCheckCodes[check.CheckKey]; known && check.Status != "PASS" {
				snapshot.BlockingReasons = append(snapshot.BlockingReasons, BlockingReason{Code: code, Detail: truncate(check.Reason, 500)})
			}
		}
		// A verdict is explained or not given.
		if len(snapshot.BlockingReasons) == 0 {
			return out
		}
	}
	evaluated := latest.EvaluatedAt
	out.Status, out.EvaluatedAt, out.Snapshots = snapshot.Status, &evaluated, []ReadinessSnapshot{snapshot}
	return out
}

// readinessVerdict is the readiness status an evaluation records.
func readinessVerdict(s provisioningdomain.ReadinessSnapshotRecord) string {
	if s.OverallReady {
		return "READY"
	}
	return "NOT_READY"
}

// provisioningDrift is the drift the latest reconciliation found.
func provisioningDrift(c repository.ConvergedProvisioning, snapshots []provisioningdomain.ReconciliationSnapshotRecord) ProvisioningDrift {
	out := ProvisioningDrift{TenantProvisioningID: c.Key, TenantID: c.TenantID, DesiredStateVersion: c.DesiredStateVersion, Items: []DriftRecord{}}
	if len(snapshots) == 0 {
		return out
	}
	latest := snapshots[0]
	observed := latest.EvaluatedAt
	out.ObservedAt = &observed
	version := latest.DesiredStateVersion
	if version < 1 {
		version = c.DesiredStateVersion
	}
	for i, d := range latest.Drift {
		objectType, known := driftObjectTypes[d.ResourceType]
		if !known {
			continue
		}
		record := DriftRecord{ID: fmt.Sprintf("drift_%s%d", compactID(latest.ID), i), TenantID: c.TenantID, ObjectType: objectType,
			ObjectReference: truncate(d.ResourceID, 128), DesiredStateVersion: version,
			DesiredStateDigest: truncate(d.DesiredHash, 128), ObservedStateDigest: truncate(d.ObservedHash, 128),
			Description: truncate(d.DriftKind+": "+d.Reason, 1000), SafeToReconcile: d.Repairable,
			Resolution: "UNRESOLVED", DetectedAt: d.DetectedAt, ReconciliationRunID: latest.ID}
		switch {
		case d.ResolvedAt != nil:
			record.Resolution, record.ReconciledAt = "RECONCILED", d.ResolvedAt
		case d.Blocking && !d.Repairable:
			record.Resolution = "BLOCKED"
		}
		out.Items = append(out.Items, record)
	}
	return out
}

// compactID is a UUID as lowercase alphanumerics, the form a minted
// ready_ or drift_ identifier takes.
func compactID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", ""))
}

// truncate cuts s to at most n bytes, on a character boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
