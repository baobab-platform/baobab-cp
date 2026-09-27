package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"gopkg.in/yaml.v3"
)

// TestEveryReadinessCheckHasARegisteredCode: a failing required check is
// always explained with a code.
func TestEveryReadinessCheckHasARegisteredCode(t *testing.T) {
	for _, check := range provisioning.ZB02RequiredReadinessChecks {
		if readinessCheckCodes[check] == "" {
			t.Errorf("readiness check %q has no reason code", check)
		}
	}
}

// provisioningBlockerCodes are the codes the planner reports as plan
// blockers and the executor records as a BLOCKED provisioning's reason.
var provisioningBlockerCodes = []string{
	"MARKET_ACTIVITIES_UNDECLARED", "MARKET_NOT_AVAILABLE", "PRODUCT_NOT_AVAILABLE", "COMPOSITION_NOT_AVAILABLE",
	"CAPABILITY_NOT_AVAILABLE", "NO_PROVIDER", "NO_RESIDENCY_COMPLIANT_PROVIDER", "NO_PRODUCTION_PERMITTED_PROVIDER",
	"PLAN_STALE", "PLAN_DIGEST_MISMATCH", "PLAN_NOT_EXECUTABLE", "EXECUTION_CANCELLED",
}

// TestBlockingReasonsAreRegisteredCodes: every code the Control Plane
// gives as a blocking reason is registered in Shared's reason-code registry
// under a category blocking reasons use (ADR-SHARED-015 section 8).
func TestBlockingReasonsAreRegisteredCodes(t *testing.T) {
	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		t.Skip("SHARED_CONTRACTS_DIR is not set")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "contracts", "authorization", "v1", "reason-code-registry.yaml"))
	mustNoError(t, err)
	var registry struct {
		ReasonCodes []struct{ Code, Category string } `yaml:"reason_codes"`
	}
	mustNoError(t, yaml.Unmarshal(raw, &registry))
	registered := map[string]bool{}
	for _, entry := range registry.ReasonCodes {
		if entry.Category == "capability_resolution_denial" || entry.Category == "provisioning_blocker" {
			registered[entry.Code] = true
		}
	}
	codes := slices.Clone(provisioningBlockerCodes)
	for _, code := range readinessCheckCodes {
		codes = append(codes, code)
	}
	for _, code := range codes {
		if !registered[code] {
			t.Errorf("blocking reason %s is not a registered capability_resolution_denial or provisioning_blocker code", code)
		}
	}
}

// TestProvisioningEvidenceConformsToShared: readiness and drift project
// their snapshots as the Shared ProvisioningReadiness and ProvisioningDrift,
// and a verdict is never given without its explanation.
func TestProvisioningEvidenceConformsToShared(t *testing.T) {
	at := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	c := repository.ConvergedProvisioning{ID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b", TenantID: "tn_0199a1b2c3d47e8f9a0b1c2d3e4f5a6b", DesiredStateVersion: 2}
	var err error
	c.Key, err = domain.FormatResourceID("tp", c.ID)
	mustNoError(t, err)
	snapshot := func(ready bool, checks ...provisioningdomain.ReadinessCheckRecord) []provisioningdomain.ReadinessSnapshotRecord {
		return []provisioningdomain.ReadinessSnapshotRecord{{ID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6c", OverallReady: ready, EvaluatedAt: at, Checks: checks}}
	}
	failed := func(key, reason string) provisioningdomain.ReadinessCheckRecord {
		return provisioningdomain.ReadinessCheckRecord{CheckKey: key, Status: "FAIL", Reason: reason}
	}

	notReady := provisioningReadiness(c, snapshot(false, failed("capability-bindings", "binding missing"),
		provisioningdomain.ReadinessCheckRecord{CheckKey: "engine-instances", Status: "PASS"}, failed("trade-lanes", "lane inactive")))
	if notReady.Status != "NOT_READY" || len(notReady.Snapshots[0].BlockingReasons) != 2 ||
		notReady.Snapshots[0].BlockingReasons[0].Code != "BINDING_NOT_FOUND" || notReady.Snapshots[0].BlockingReasons[1].Code != "TRADE_LANE_NOT_READY" {
		t.Fatalf("not ready: %+v", notReady)
	}
	if ready := provisioningReadiness(c, snapshot(true)); ready.Status != "READY" || len(ready.Snapshots) != 1 {
		t.Fatalf("ready: %+v", ready)
	}
	// A failure no registered code explains is not a verdict.
	if unexplained := provisioningReadiness(c, snapshot(false, failed("unregistered-check", "?"))); unexplained.Status != "UNKNOWN" || len(unexplained.Snapshots) != 0 {
		t.Fatalf("unexplained: %+v", unexplained)
	}

	resolved := at.Add(time.Minute)
	drift := provisioningDrift(c, []provisioningdomain.ReconciliationSnapshotRecord{{ID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6d",
		DesiredStateVersion: 2, EvaluatedAt: at, Drift: []provisioningdomain.ResourceDriftRecord{
			{ResourceType: "capability-grant", ResourceID: "commerce.order.manage@scope", DriftKind: "MISSING", Reason: "desired resource is not observed",
				DesiredHash: "present", Repairable: true, Blocking: true, DetectedAt: at},
			{ResourceType: "trade-lane", ResourceID: "UG-ZA", DriftKind: "UNEXPECTED", Reason: "unexpected observed resource", Blocking: true, DetectedAt: at},
			{ResourceType: "market-participation", ResourceID: "UG", DriftKind: "MISMATCH", Reason: "differs", Repairable: true,
				Blocking: true, DetectedAt: at, ResolvedAt: &resolved},
		}}})
	if len(drift.Items) != 3 || drift.Items[0].ObjectType != "CAPABILITY_GRANT" || drift.Items[0].Resolution != "UNRESOLVED" ||
		drift.Items[1].Resolution != "BLOCKED" || drift.Items[1].ObjectType != "TRADE_LANE" || drift.Items[2].Resolution != "RECONCILED" {
		t.Fatalf("drift: %+v", drift)
	}

	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		return
	}
	for definition, value := range map[string]any{
		"ProvisioningReadiness#not-ready": notReady, "ProvisioningReadiness#unknown": provisioningReadiness(c, nil),
		"ProvisioningDrift#observed": drift, "ProvisioningDrift#unobserved": provisioningDrift(c, nil),
	} {
		name, _, _ := strings.Cut(definition, "#")
		raw, err := json.Marshal(value)
		mustNoError(t, err)
		var body any
		mustNoError(t, json.Unmarshal(raw, &body))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "control-plane/v1/tenant-provisioning.schema.json#/$defs/"+name), body)
	}
}
