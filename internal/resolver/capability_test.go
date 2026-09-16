package resolver

import (
	"context"
	"testing"
	"time"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
)

func TestCapabilityResolverResolveUsesHighestPriorityBinding(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	query := CapabilityResolutionQuery{
		CapabilityKey: "baobab_trade",
		Context: Context{
			TenantID:      "tenant-123",
			LegalEntityID: "legal-456",
			MarketID:      "market-789",
			CountryCode:   "ZA",
			CurrencyCode:  "ZAR",
			Locale:        "en-ZA",
		},
		Bindings: []CapabilityBinding{
			{CapabilityKey: "baobab_trade", EngineID: "engine-1", EngineInstanceID: "instance-1", BindingMode: "FALLBACK", Status: "ACTIVE", Priority: 10, ContractVersion: "v1"},
			{CapabilityKey: "baobab_trade", EngineID: "engine-2", EngineInstanceID: "instance-2", BindingMode: "PRIMARY", Status: "ACTIVE", Priority: 100, ContractVersion: "v1"},
		},
	}

	resolved, err := resolver.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.BindingMode != "PRIMARY" {
		t.Fatalf("expected PRIMARY binding, got %q", resolved.BindingMode)
	}
	if resolved.EngineID != "engine-2" {
		t.Fatalf("expected engine-2, got %q", resolved.EngineID)
	}
}

func TestCapabilityResolverBindingModePrecedesPriority(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	// Per nabhold/shared's canonical scope-specificity.yaml, binding_mode
	// preference is resolved BEFORE explicit priority: a PRIMARY binding
	// SHALL win over a FALLBACK binding even when the FALLBACK carries a
	// far higher administrative priority. Priority is a tie-breaker of
	// last resort among candidates still tied after binding mode, never a
	// substitute for it (ADR-BCP-003 SS18, "Priority vs Specificity").
	query := CapabilityResolutionQuery{
		CapabilityKey: "baobab_trade",
		Context:       Context{TenantID: "tenant-123"},
		Bindings: []CapabilityBinding{
			{ID: "fallback", CapabilityKey: "baobab_trade", EngineID: "engine-1", EngineInstanceID: "instance-1", BindingMode: "FALLBACK", Status: "ACTIVE", Priority: 1000, ContractVersion: "v1"},
			{ID: "primary", CapabilityKey: "baobab_trade", EngineID: "engine-2", EngineInstanceID: "instance-2", BindingMode: "PRIMARY", Status: "ACTIVE", Priority: 1, ContractVersion: "v1"},
		},
	}

	resolved, err := resolver.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.BindingID != "primary" {
		t.Fatalf("expected the PRIMARY binding to win over a higher-priority FALLBACK, got %#v", resolved)
	}
}

func TestCapabilityResolverSkipsEligibilityGateWhenCapabilityNil(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	_, err := resolver.Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "baobab_trade",
		Context:       Context{TenantID: "tenant-123"},
		Bindings: []CapabilityBinding{
			{CapabilityKey: "baobab_trade", EngineID: "engine-1", EngineInstanceID: "instance-1", BindingMode: "PRIMARY", Status: "ACTIVE", ContractVersion: "v1"},
		},
	})
	if err != nil {
		t.Fatalf("expected nil Capability to skip the eligibility gate, got: %v", err)
	}
}

func TestCapabilityResolverEnforcesLifecycleEligibilityWhenCapabilityPopulated(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	bindings := []CapabilityBinding{
		{CapabilityKey: "baobab_trade", EngineID: "engine-1", EngineInstanceID: "instance-1", BindingMode: "PRIMARY", Status: "ACTIVE", ContractVersion: "v1"},
	}
	suspended := capabilitydomain.Capability{Key: "baobab_trade", Lifecycle: capabilitydomain.CapabilityLifecycleSuspended}
	_, err := resolver.Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "baobab_trade",
		Context:       Context{TenantID: "tenant-123"},
		Bindings:      bindings,
		Capability:    &suspended,
	})
	if err == nil {
		t.Fatal("expected a SUSPENDED capability to fail resolution closed")
	}

	active := capabilitydomain.Capability{Key: "baobab_trade", Lifecycle: capabilitydomain.CapabilityLifecycleActive}
	_, err = resolver.Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "baobab_trade",
		Context:       Context{TenantID: "tenant-123"},
		Bindings:      bindings,
		Capability:    &active,
	})
	if err != nil {
		t.Fatalf("expected an ACTIVE capability to resolve, got: %v", err)
	}
}

func TestCapabilityResolverUsesScopeBeforePriority(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	query := CapabilityResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans", MarketID: "market-za"},
		Scopes: map[string]capabilitydomain.CapabilityScope{
			"global": {},
			"tenant": {TenantID: "tn_zuribeans", MarketID: "market-za"},
		},
		Bindings: []CapabilityBinding{
			{ID: "global", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-global", ScopeID: "global", BindingMode: "PRIMARY", Priority: 1000, Status: "ACTIVE"},
			{ID: "tenant", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-za", ScopeID: "tenant", BindingMode: "PRIMARY", Priority: 10, Status: "ACTIVE"},
		},
	}
	resolved, err := resolver.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("resolve scoped binding: %v", err)
	}
	if resolved.BindingID != "tenant" || resolved.Specificity != 2 {
		t.Fatalf("expected tenant/market binding, got %#v", resolved)
	}
}

func TestCapabilityResolverAppliesTemporalWindow(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	resolver := CapabilityResolverImpl{}
	resolved, err := resolver.Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans"},
		At:            now,
		Bindings: []CapabilityBinding{
			{ID: "expired", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-old", BindingMode: "PRIMARY", Priority: 100, Status: "ACTIVE", EffectiveFrom: past.Add(-time.Hour), EffectiveTo: &past},
			{ID: "current", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-current", BindingMode: "PRIMARY", Priority: 10, Status: "ACTIVE", EffectiveFrom: past, EffectiveTo: &future},
		},
	})
	if err != nil || resolved.BindingID != "current" {
		t.Fatalf("expected current temporal binding, got %#v err=%v", resolved, err)
	}
}

func TestCapabilityResolverFailsClosedOnAmbiguity(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	_, err := resolver.Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans"},
		Bindings: []CapabilityBinding{
			{ID: "a", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-a", BindingMode: "PRIMARY", Priority: 100, Status: "ACTIVE"},
			{ID: "b", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-b", BindingMode: "PRIMARY", Priority: 100, Status: "ACTIVE"},
		},
	})
	if err == nil || err.Error() != "capability binding is ambiguous" {
		t.Fatalf("expected ambiguity failure, got %v", err)
	}
}

func TestCapabilityResolverTreatsShadowOnlyAsNoEligibleBinding(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	_, err := resolver.Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans"},
		Bindings: []CapabilityBinding{
			{ID: "shadow", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-shadow", BindingMode: "SHADOW", Priority: 100, Status: "ACTIVE"},
		},
	})
	if err == nil || err.Error() != "capability not found" {
		t.Fatalf("expected shadow-only binding to resolve as not found, got %v", err)
	}
}

func TestCapabilityResolverExcludesDisabledBindings(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	resolved, err := resolver.Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans"},
		Bindings: []CapabilityBinding{
			{ID: "disabled", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-disabled", BindingMode: "DISABLED", Priority: 1000, Status: "ACTIVE"},
			{ID: "fallback", CapabilityKey: "erp.receivables", EngineInstanceID: "erp-fallback", BindingMode: "FALLBACK", Priority: 1, Status: "ACTIVE"},
		},
	})
	if err != nil {
		t.Fatalf("resolve with disabled binding present: %v", err)
	}
	if resolved.BindingID != "fallback" {
		t.Fatalf("expected disabled binding to be excluded entirely, got %#v", resolved)
	}
}

func TestCapabilityResolverRejectsUnknownCapability(t *testing.T) {
	resolver := CapabilityResolverImpl{}
	_, err := resolver.Resolve(context.Background(), CapabilityResolutionQuery{
		CapabilityKey: "missing_capability",
		Context:       Context{TenantID: "tenant-123"},
		Bindings:      nil,
	})
	if err == nil {
		t.Fatal("expected missing capability error")
	}
}
