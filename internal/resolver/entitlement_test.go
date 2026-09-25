package resolver

import (
	"context"
	"testing"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
)

func TestEntitlementResolverGrantsWhenEffectiveAndCompatible(t *testing.T) {
	now := time.Now().UTC()
	resolver := EntitlementResolverImpl{}
	decision, err := resolver.Resolve(context.Background(), EntitlementResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans", MarketID: "market-za"},
		At:            now,
		Grants: []capabilitydomain.CapabilityGrant{
			{ID: "grant-1", CapabilityKey: "erp.receivables", ScopeID: "scope-1", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: now.Add(-time.Hour)},
		},
		Scopes: map[string]capabilitydomain.CapabilityScope{
			"scope-1": {TenantID: "tn_zuribeans", MarketID: "market-za"},
		},
	})
	if err != nil {
		t.Fatalf("resolve entitlement: %v", err)
	}
	if !decision.Entitled || decision.GrantID != "grant-1" {
		t.Fatalf("expected entitled decision citing grant-1, got %#v", decision)
	}
}

func TestEntitlementResolverDeniesWhenNoEffectiveGrant(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	resolver := EntitlementResolverImpl{}
	decision, err := resolver.Resolve(context.Background(), EntitlementResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans"},
		At:            now,
		Grants: []capabilitydomain.CapabilityGrant{
			{ID: "expired", CapabilityKey: "erp.receivables", ScopeID: "scope-1", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusExpired, EffectiveFrom: past.Add(-time.Hour), EffectiveTo: &past},
			{ID: "revoked", CapabilityKey: "erp.receivables", ScopeID: "scope-1", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusRevoked, EffectiveFrom: past},
		},
		Scopes: map[string]capabilitydomain.CapabilityScope{"scope-1": {TenantID: "tn_zuribeans"}},
	})
	if err != nil {
		t.Fatalf("resolve entitlement: %v", err)
	}
	if decision.Entitled {
		t.Fatalf("expected denial with no effective grant, got %#v", decision)
	}
}

func TestEntitlementResolverChecksOrganisationAndBusinessUnit(t *testing.T) {
	now := time.Now().UTC()
	resolver := EntitlementResolverImpl{}
	decision, err := resolver.Resolve(context.Background(), EntitlementResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans", OrganisationID: "org-a", BusinessUnitID: "bu-a"},
		At:            now,
		Grants: []capabilitydomain.CapabilityGrant{
			{ID: "grant-org-b", CapabilityKey: "erp.receivables", ScopeID: "scope-org-b", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: now.Add(-time.Hour)},
		},
		Scopes: map[string]capabilitydomain.CapabilityScope{
			"scope-org-b": {TenantID: "tn_zuribeans", OrganisationID: "org-b"},
		},
	})
	if err != nil {
		t.Fatalf("resolve entitlement: %v", err)
	}
	if decision.Entitled {
		t.Fatal("expected denial for a grant scoped to a different organisation")
	}
}

func TestEntitlementResolverDeniesWhenScopeIncompatible(t *testing.T) {
	now := time.Now().UTC()
	resolver := EntitlementResolverImpl{}
	decision, err := resolver.Resolve(context.Background(), EntitlementResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans", MarketID: "market-ng"},
		At:            now,
		Grants: []capabilitydomain.CapabilityGrant{
			{ID: "grant-za-only", CapabilityKey: "erp.receivables", ScopeID: "scope-za", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: now.Add(-time.Hour)},
		},
		Scopes: map[string]capabilitydomain.CapabilityScope{
			"scope-za": {TenantID: "tn_zuribeans", MarketID: "market-za"},
		},
	})
	if err != nil {
		t.Fatalf("resolve entitlement: %v", err)
	}
	if decision.Entitled {
		t.Fatalf("expected denial for market-scoped grant outside its market, got %#v", decision)
	}
}

func TestEntitlementResolverAllowsMultipleCoexistingGrants(t *testing.T) {
	// §35: unlike CapabilityBinding, multiple compatible grants for the
	// same capability/scope MAY legitimately coexist -- this must never
	// raise an ambiguity error the way binding resolution does.
	now := time.Now().UTC()
	resolver := EntitlementResolverImpl{}
	decision, err := resolver.Resolve(context.Background(), EntitlementResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans"},
		At:            now,
		Grants: []capabilitydomain.CapabilityGrant{
			{ID: "baseline", CapabilityKey: "erp.receivables", ScopeID: "scope-1", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: now.Add(-time.Hour)},
			{ID: "subscription", CapabilityKey: "erp.receivables", ScopeID: "scope-1", Source: capabilitydomain.GrantSourceProductSubscription, SourceReference: "sub_1", Status: capabilitydomain.GrantStatusActive, EffectiveFrom: now.Add(-time.Hour)},
		},
		Scopes: map[string]capabilitydomain.CapabilityScope{"scope-1": {TenantID: "tn_zuribeans"}},
	})
	if err != nil {
		t.Fatalf("resolve entitlement: %v", err)
	}
	if !decision.Entitled {
		t.Fatal("expected entitlement to be satisfied by the first effective compatible grant")
	}
}

func TestEntitlementResolverRejectsExcludedCountry(t *testing.T) {
	now := time.Now().UTC()
	resolver := EntitlementResolverImpl{}
	decision, err := resolver.Resolve(context.Background(), EntitlementResolutionQuery{
		CapabilityKey: "erp.receivables",
		Context:       Context{TenantID: "tn_zuribeans", CountryCode: "BW"},
		At:            now,
		Grants: []capabilitydomain.CapabilityGrant{
			{ID: "grant-1", CapabilityKey: "erp.receivables", ScopeID: "scope-1", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: now.Add(-time.Hour)},
		},
		Scopes: map[string]capabilitydomain.CapabilityScope{
			"scope-1": {TenantID: "tn_zuribeans", IncludeCountries: []string{"ZA", "BW", "ZM"}, ExcludeCountries: []string{"BW"}},
		},
	})
	if err != nil {
		t.Fatalf("resolve entitlement: %v", err)
	}
	if decision.Entitled {
		t.Fatal("expected excluded country to override inclusion")
	}
}

func TestEntitlementResolverRejectsMissingCapabilityKey(t *testing.T) {
	resolver := EntitlementResolverImpl{}
	if _, err := resolver.Resolve(context.Background(), EntitlementResolutionQuery{}); err == nil {
		t.Fatal("expected missing capability key rejection")
	}
}
