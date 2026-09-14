package service

import (
	"context"
	"testing"
	"time"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/resolver"
)

func TestResolutionServiceResolve(t *testing.T) {
	service := ResolutionService{Pipeline: resolver.ResolutionPipeline{}}
	result, err := service.Resolve(context.Background(), ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		CapabilityKey:     "commerce.order.create",
		Context: resolver.Context{
			TenantID:      "tenant-123",
			LegalEntityID: "legal-456",
			MarketID:      "market-789",
			CountryCode:   "ZA",
			CurrencyCode:  "ZAR",
			Locale:        "en-ZA",
		},
		Mappings: []domain.Mapping{{
			ID:                      "mapping-tenant",
			MappingType:             "IDENTITY",
			TenantID:                "tenant-123",
			CanonicalEntityID:       "entity-abc",
			TargetCanonicalEntityID: "entity-tenant",
			ScopeID:                 "tenant-123",
			Direction:               "BIDIRECTIONAL",
			Cardinality:             "ONE_TO_ONE",
			Authority:               "baobab",
			Confidence:              "CONFIRMED",
			Status:                  "ACTIVE",
			ResolutionPriority:      50,
			EffectiveFrom:           "2025-01-01T00:00:00Z",
		}},
		Bindings: []resolver.CapabilityBinding{{
			CapabilityKey:    "commerce.order.create",
			EngineID:         "engine-1",
			EngineInstanceID: "instance-1",
			BindingMode:      "PRIMARY",
			Priority:         100,
			Status:           "ACTIVE",
			ContractVersion:  "v1",
		}},
		EngineInstances: []resolver.EngineInstance{{
			ID:          "instance-1",
			EngineID:    "engine-1",
			Region:      "af-south-1",
			Environment: "production",
			Status:      "ACTIVE",
		}},
	})
	if err != nil {
		t.Fatalf("service resolve failed: %v", err)
	}
	if result.Context.TenantID != "tenant-123" {
		t.Fatal("tenant context not preserved")
	}
	if result.Capability.EngineInstanceID != "instance-1" {
		t.Fatal("engine instance was not selected")
	}
}

func TestResolutionServiceRequiresTenant(t *testing.T) {
	service := ResolutionService{Pipeline: resolver.ResolutionPipeline{}}
	_, err := service.Resolve(context.Background(), ResolutionRequest{
		TenantID: "",
		Context:  resolver.Context{},
	})
	if err == nil {
		t.Fatal("expected tenant requirement error")
	}
}

func TestResolutionServiceRejectsInvalidCapabilityKey(t *testing.T) {
	service := ResolutionService{Pipeline: resolver.ResolutionPipeline{}}
	_, err := service.Resolve(context.Background(), ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		CapabilityKey:     "baobab_trade",
		Context:           resolver.Context{TenantID: "tenant-123"},
	})
	if err == nil {
		t.Fatal("expected provider/product key to be rejected as a capability key")
	}
}

func TestResolutionServiceUsesRepositoryState(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	repo.Mappings["entity-abc"] = []domain.Mapping{{
		ID: "authoritative", MappingType: "IDENTITY", TenantID: "tenant-123", CanonicalEntityID: "entity-abc",
		TargetCanonicalEntityID: "entity-1", ScopeID: "tenant-123", Direction: "BIDIRECTIONAL", Cardinality: "ONE_TO_ONE",
		Authority: "baobab", Confidence: "CONFIRMED", Status: "ACTIVE", EffectiveFrom: "2025-01-01T00:00:00Z",
	}}
	repo.Bindings["commerce.order.create"] = []resolver.CapabilityBinding{{CapabilityKey: "commerce.order.create", EngineID: "engine-1", EngineInstanceID: "instance-1", BindingMode: "PRIMARY", Status: "ACTIVE", ContractVersion: "v1"}}
	repo.EngineInstances["engine-1"] = []resolver.EngineInstance{{ID: "instance-1", EngineID: "engine-1", Environment: "production", Status: "ACTIVE"}}
	service := ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo}
	result, err := service.Resolve(context.Background(), ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		CapabilityKey:     "commerce.order.create",
		Context: resolver.Context{
			TenantID:    "tenant-123",
			MarketID:    "market-1",
			CountryCode: "ZA",
		},
		Mappings: []domain.Mapping{{ID: "untrusted", CanonicalEntityID: "entity-abc", Status: "ACTIVE"}},
	})
	if err != nil {
		t.Fatalf("repository-backed resolve failed: %v", err)
	}
	if result.Mapping.Mapping.ID != "authoritative" {
		t.Fatalf("expected repository mapping, got %s", result.Mapping.Mapping.ID)
	}
}

func repositoryBackedFixture() *repository.Repository {
	repo := repository.NewInMemoryRepository()
	repo.Mappings["entity-abc"] = []domain.Mapping{{
		ID: "authoritative", MappingType: "IDENTITY", TenantID: "tenant-123", CanonicalEntityID: "entity-abc",
		TargetCanonicalEntityID: "entity-1", ScopeID: "tenant-123", Direction: "BIDIRECTIONAL", Cardinality: "ONE_TO_ONE",
		Authority: "baobab", Confidence: "CONFIRMED", Status: "ACTIVE", EffectiveFrom: "2025-01-01T00:00:00Z",
	}}
	repo.Bindings["commerce.order.create"] = []resolver.CapabilityBinding{{CapabilityKey: "commerce.order.create", EngineID: "engine-1", EngineInstanceID: "instance-1", BindingMode: "PRIMARY", Status: "ACTIVE", ContractVersion: "v1"}}
	repo.EngineInstances["engine-1"] = []resolver.EngineInstance{{ID: "instance-1", EngineID: "engine-1", Environment: "production", Status: "ACTIVE"}}
	return repo
}

func resolveTenant123() ResolutionRequest {
	return ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		CapabilityKey:     "commerce.order.create",
		Context:           resolver.Context{TenantID: "tenant-123", MarketID: "market-1", CountryCode: "ZA"},
		Mappings:          []domain.Mapping{{ID: "untrusted", CanonicalEntityID: "entity-abc", Status: "ACTIVE"}},
	}
}

// TestResolutionServiceSkipsEntitlementByDefault proves EnforceEntitlement's
// zero value (false) leaves every existing caller's behavior unchanged even
// when Grants/Scopes/CapabilityRegistry happen to be wired: the fields are
// consulted only when EnforceEntitlement is explicitly true.
func TestResolutionServiceSkipsEntitlementByDefault(t *testing.T) {
	repo := repositoryBackedFixture()
	service := ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo, Grants: repo, Scopes: repo, CapabilityRegistry: repo}
	if _, err := service.Resolve(context.Background(), resolveTenant123()); err != nil {
		t.Fatalf("expected EnforceEntitlement=false to skip the gate entirely, got: %v", err)
	}
}

func TestResolutionServiceEnforcesEntitlementWhenEnabled(t *testing.T) {
	repo := repositoryBackedFixture()
	service := ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo, Grants: repo, Scopes: repo, EnforceEntitlement: true}
	if _, err := service.Resolve(context.Background(), resolveTenant123()); err == nil {
		t.Fatal("expected resolution to fail closed with no grants on record")
	}

	if err := repo.CreateCapabilityScope(context.Background(), capabilitydomain.CapabilityScope{ScopeID: "scope-1", TenantID: "tenant-123", MarketID: "market-1"}); err != nil {
		t.Fatalf("create capability scope: %v", err)
	}
	if err := repo.CreateGrant(context.Background(), capabilitydomain.CapabilityGrant{ID: "grant-1", TenantID: "tenant-123", CapabilityKey: "commerce.order.create", ScopeID: "scope-1", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if _, err := service.Resolve(context.Background(), resolveTenant123()); err != nil {
		t.Fatalf("expected resolution to succeed with an effective compatible grant, got: %v", err)
	}
}

func TestResolutionServiceEnforcesCapabilityLifecycleWhenRegistered(t *testing.T) {
	repo := repositoryBackedFixture()
	if err := repo.CreateCapabilityScope(context.Background(), capabilitydomain.CapabilityScope{ScopeID: "scope-1", TenantID: "tenant-123", MarketID: "market-1"}); err != nil {
		t.Fatalf("create capability scope: %v", err)
	}
	if err := repo.CreateGrant(context.Background(), capabilitydomain.CapabilityGrant{ID: "grant-1", TenantID: "tenant-123", CapabilityKey: "commerce.order.create", ScopeID: "scope-1", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	// The lifecycle lookup must use the caller's canonical capability key,
	// not a provider/product placeholder.
	repo.Capabilities["commerce.order.create"] = capabilitydomain.Capability{Key: "commerce.order.create", Name: "Baobab Trade", DomainKey: "trade", Lifecycle: capabilitydomain.CapabilityLifecycleSuspended, Maturity: capabilitydomain.CapabilityMaturitySupported}
	service := ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo, Grants: repo, Scopes: repo, CapabilityRegistry: repo, EnforceEntitlement: true}
	if _, err := service.Resolve(context.Background(), resolveTenant123()); err == nil {
		t.Fatal("expected a SUSPENDED registered capability to fail resolution closed")
	}
}
