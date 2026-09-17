package resolver

import (
	"context"
	"testing"
	"time"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
)

func TestCapabilityExtractionKeepsConsumerContractStable(t *testing.T) {
	now := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	ctx := Context{TenantID: "tn_zuribeans", Environment: "production", DeploymentRegion: "af-south-1", ResolvedAt: now}
	scope := map[string]capabilitydomain.CapabilityScope{"zuribeans-production": {ScopeID: "zuribeans-production", TenantID: "tn_zuribeans", Environment: "production"}}

	before := resolveExtractedCapability(t, ctx, scope, capabilitydomain.CapabilityBinding{
		ID: "binding-idempiere", CapabilityKey: "warehouse.execution", EngineID: "idempiere",
		EngineInstanceID: "ERP-AF-SOUTH-02", ScopeID: "zuribeans-production", BindingMode: "PRIMARY",
		Priority: 100, Status: "ACTIVE", ContractVersion: "1.0.0", EffectiveFrom: now.Add(-time.Hour),
	}, domain.EngineInstance{ID: "ERP-AF-SOUTH-02", EngineID: "idempiere", Region: "af-south-1", Environment: "production", Status: "ACTIVE", HealthStatus: "HEALTHY"})

	after := resolveExtractedCapability(t, ctx, scope, capabilitydomain.CapabilityBinding{
		ID: "binding-wms", CapabilityKey: "warehouse.execution", EngineID: "wms",
		EngineInstanceID: "WMS-AF-SOUTH-01", ScopeID: "zuribeans-production", BindingMode: "PRIMARY",
		Priority: 100, Status: "ACTIVE", ContractVersion: "1.0.0", EffectiveFrom: now,
	}, domain.EngineInstance{ID: "WMS-AF-SOUTH-01", EngineID: "wms", Region: "af-south-1", Environment: "production", Status: "ACTIVE", HealthStatus: "HEALTHY"})

	if before.CapabilityKey != after.CapabilityKey || before.ContractVersion != after.ContractVersion {
		t.Fatalf("consumer contract changed during extraction: before=%#v after=%#v", before, after)
	}
	if before.EngineID == after.EngineID || before.EngineInstanceID == after.EngineInstanceID {
		t.Fatal("test did not move warehouse.execution from iDempiere to WMS")
	}
}

func resolveExtractedCapability(t *testing.T, ctx Context, scopes map[string]capabilitydomain.CapabilityScope, binding capabilitydomain.CapabilityBinding, instance domain.EngineInstance) ResolvedCapability {
	t.Helper()
	resolved, err := (CapabilityResolverImpl{}).Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "warehouse.execution", Context: ctx, Bindings: []capabilitydomain.CapabilityBinding{binding}, Scopes: scopes, At: ctx.ResolvedAt,
	})
	if err != nil {
		t.Fatalf("resolve capability: %v", err)
	}
	if _, err := (TopologyResolverImpl{}).Resolve(context.Background(), TopologyResolutionQuery{
		Context: ctx, SelectedEngineInstanceID: resolved.EngineInstanceID, EngineInstances: []domain.EngineInstance{instance}, At: ctx.ResolvedAt,
	}); err != nil {
		t.Fatalf("resolve topology: %v", err)
	}
	return resolved
}
