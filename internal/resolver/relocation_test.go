package resolver

import (
	"testing"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
)

func TestEngineRelocationPreservesCanonicalIdentity(t *testing.T) {
	cutover := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	binding := capabilitydomain.CapabilityBinding{
		ID: "binding-warehouse", CapabilityKey: "warehouse.execution", EngineID: "idempiere",
		EngineInstanceID: "ERP-AF-SOUTH-01", ScopeID: "tn_zuribeans", BindingMode: "PRIMARY",
		Status: "ACTIVE", ContractVersion: "1.0.0", EffectiveFrom: cutover.Add(-time.Hour), Version: 7,
	}
	from := domain.EngineInstance{ID: "ERP-AF-SOUTH-01", EngineID: "idempiere", Status: "DRAINING"}
	to := domain.EngineInstance{ID: "ERP-AF-SOUTH-02", EngineID: "idempiere", Status: "ACTIVE", EffectiveFrom: cutover.Add(-time.Minute)}
	native := domain.ExternalReference{ID: "ref_warehouse01", SystemNamespace: "idempiere", EngineID: "baobab-erp", NativeEntityType: "m_warehouse", NativeID: "1000000"}

	now := cutover.Add(-2 * time.Hour)
	plan, err := PlanEngineRelocation(binding, from, to, health.CriticalityStandard, health.Levels{EngineInstance: healthyAt(to.ID, now)}, now, cutover)
	if err != nil {
		t.Fatalf("plan relocation: %v", err)
	}
	if plan.Previous.EngineInstanceID != "ERP-AF-SOUTH-01" || plan.Successor.EngineInstanceID != "ERP-AF-SOUTH-02" {
		t.Fatalf("unexpected relocation plan: %#v", plan)
	}
	if plan.Previous.EffectiveTo == nil || !plan.Previous.EffectiveTo.Equal(plan.Successor.EffectiveFrom) {
		t.Fatal("relocation must be temporally contiguous")
	}
	if native.ID != "ref_warehouse01" || native.NativeID != "1000000" {
		t.Fatal("relocation changed canonical or native identity")
	}
}

func healthyAt(instanceID string, now time.Time) *health.Observation {
	return &health.Observation{Subject: health.Subject{EngineInstanceID: instanceID}, Status: health.StatusHealthy,
		ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Source: health.SourceActiveProbe}
}

func TestEngineRelocationRejectsDifferentEngineOrFailedTarget(t *testing.T) {
	now := time.Now().UTC()
	cutover := now.Add(time.Hour)
	binding := capabilitydomain.CapabilityBinding{CapabilityKey: "warehouse.execution", EngineInstanceID: "ERP-AF-SOUTH-01"}
	from := domain.EngineInstance{ID: "ERP-AF-SOUTH-01", EngineID: "idempiere"}
	target := domain.EngineInstance{ID: "ERP-AF-SOUTH-02", EngineID: "idempiere", Status: "ACTIVE"}
	down := &health.Observation{Subject: health.Subject{EngineInstanceID: target.ID}, Status: health.StatusUnavailable,
		ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Source: health.SourceActiveProbe, Reasons: []string{"HEALTH_PROBE_FAILED"}}
	expired := healthyAt(target.ID, now.Add(-time.Hour))
	cases := []struct {
		name        string
		target      domain.EngineInstance
		criticality health.Criticality
		health      *health.Observation
	}{
		{"different engine", domain.EngineInstance{ID: "WMS-AF-SOUTH-01", EngineID: "wms", Status: "ACTIVE"}, health.CriticalityStandard, healthyAt("WMS-AF-SOUTH-01", now)},
		{"unavailable target", target, health.CriticalityStandard, down},
		{"critical capability, unobserved target", target, health.CriticalityCritical, nil},
		{"critical capability, expired target health", target, health.CriticalityCritical, expired},
	}
	for _, c := range cases {
		if _, err := PlanEngineRelocation(binding, from, c.target, c.criticality, health.Levels{EngineInstance: c.health}, now, cutover); err == nil {
			t.Errorf("%s: expected target rejection", c.name)
		}
	}
	if _, err := PlanEngineRelocation(binding, from, target, health.CriticalityCritical, health.Levels{EngineInstance: healthyAt(target.ID, now)}, now, cutover); err != nil {
		t.Fatalf("healthy target refused for a critical capability: %v", err)
	}
}
