package resolver

import (
	"testing"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestEngineRelocationPreservesCanonicalIdentity(t *testing.T) {
	cutover := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	binding := capabilitydomain.CapabilityBinding{
		ID: "binding-warehouse", CapabilityKey: "warehouse.execution", EngineID: "idempiere",
		EngineInstanceID: "ERP-AF-SOUTH-01", ScopeID: "tn_zuribeans", BindingMode: "PRIMARY",
		Status: "ACTIVE", ContractVersion: "1.0.0", EffectiveFrom: cutover.Add(-time.Hour), Version: 7,
	}
	from := domain.EngineInstance{ID: "ERP-AF-SOUTH-01", EngineID: "idempiere", Status: "DRAINING", HealthStatus: "HEALTHY"}
	to := domain.EngineInstance{ID: "ERP-AF-SOUTH-02", EngineID: "idempiere", Status: "ACTIVE", HealthStatus: "HEALTHY", EffectiveFrom: cutover.Add(-time.Minute)}
	native := domain.ExternalReference{ID: "ref_warehouse01", SystemNamespace: "idempiere", EngineID: "baobab-erp", NativeEntityType: "m_warehouse", NativeID: "1000000"}

	plan, err := PlanEngineRelocation(binding, from, to, cutover)
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

func TestEngineRelocationRejectsDifferentEngineOrFailedTarget(t *testing.T) {
	cutover := time.Now().UTC()
	binding := capabilitydomain.CapabilityBinding{CapabilityKey: "warehouse.execution", EngineInstanceID: "ERP-AF-SOUTH-01"}
	from := domain.EngineInstance{ID: "ERP-AF-SOUTH-01", EngineID: "idempiere"}
	for _, target := range []domain.EngineInstance{
		{ID: "WMS-AF-SOUTH-01", EngineID: "wms", Status: "ACTIVE", HealthStatus: "HEALTHY"},
		{ID: "ERP-AF-SOUTH-02", EngineID: "idempiere", Status: "ACTIVE", HealthStatus: "FAILED"},
	} {
		if _, err := PlanEngineRelocation(binding, from, target, cutover); err == nil {
			t.Fatalf("expected target rejection: %#v", target)
		}
	}
}
