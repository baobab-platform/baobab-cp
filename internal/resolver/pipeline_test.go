package resolver

import (
	"context"
	"testing"
	"time"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
)

func TestResolutionPipelineBuildsFinalDecision(t *testing.T) {
	pipeline := ResolutionPipeline{}
	result, err := pipeline.Resolve(context.Background(), ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		CapabilityKey:     "commerce.order.create",
		Context: Context{
			TenantID:      "tenant-123",
			LegalEntityID: "legal-456",
			MarketID:      "market-789",
			CountryCode:   "ZA",
			CurrencyCode:  "ZAR",
			Locale:        "en-ZA",
		},
		Candidates: []domain.Mapping{{
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
		Bindings: []CapabilityBinding{{
			CapabilityKey:    "commerce.order.create",
			EngineID:         "engine-1",
			EngineInstanceID: "instance-1",
			BindingMode:      "PRIMARY",
			Priority:         100,
			Status:           "ACTIVE",
			ContractVersion:  "v1",
		}},
		EngineInstances: []EngineInstance{{
			ID:          "instance-1",
			EngineID:    "engine-1",
			Region:      "af-south-1",
			Environment: "production",
			Status:      "ACTIVE",
		}},
	})
	if err != nil {
		t.Fatalf("pipeline resolve failed: %v", err)
	}
	if result.Context.TenantID != "tenant-123" {
		t.Fatal("tenant context was not preserved")
	}
	if result.Mapping.Mapping.ID != "mapping-tenant" {
		t.Fatal("expected tenant mapping in final resolution")
	}
	if result.Capability.EngineInstanceID != "instance-1" {
		t.Fatal("expected selected engine instance in final resolution")
	}
	if !result.Policy.Allowed {
		t.Fatal("expected policy to allow final decision")
	}
}

func TestResolutionPipelineRejectsMissingTenant(t *testing.T) {
	pipeline := ResolutionPipeline{}
	_, err := pipeline.Resolve(context.Background(), ResolutionRequest{
		TenantID: "",
		Context:  Context{},
	})
	if err == nil {
		t.Fatal("expected missing tenant context rejection")
	}
}

// TestResolutionPipelineRejectsMissingCanonicalEntity is a regression test
// for docs/governance/gate-iam-0-discovery.md R-3: ResolutionRequest used to
// have no CanonicalEntityID field at all, and Resolve passed TenantID in its
// place -- meaning a request with a tenant but nothing else always "worked"
// by silently resolving the tenant as if it were the entity. Requiring
// CanonicalEntityID explicitly closes that gap: a request naming a tenant
// but no entity must fail, not quietly substitute one for the other.
func TestResolutionPipelineRejectsMissingCanonicalEntity(t *testing.T) {
	pipeline := ResolutionPipeline{}
	_, err := pipeline.Resolve(context.Background(), ResolutionRequest{
		TenantID: "tenant-123",
		Context:  Context{TenantID: "tenant-123"},
	})
	if err == nil {
		t.Fatal("expected missing canonical_entity_id rejection")
	}
}

func TestResolutionPipelineRejectsMissingCapability(t *testing.T) {
	pipeline := ResolutionPipeline{}
	_, err := pipeline.Resolve(context.Background(), ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		Context:           Context{TenantID: "tenant-123"},
	})
	if err == nil {
		t.Fatal("expected missing capability_key rejection")
	}
}

// TestResolutionPipelineRejectsCrossTenantMapping proves the fix for R-3
// actually enforces tenant isolation, not just field naming: a candidate
// mapping whose own TenantID differs from the requesting tenant must never
// be selected, even if its CanonicalEntityID matches -- ADR-0005 §2 treats
// this as exactly the boundary Tenant/CanonicalEntity separation exists to
// protect.
func TestResolutionPipelineRejectsCrossTenantMapping(t *testing.T) {
	pipeline := ResolutionPipeline{}
	_, err := pipeline.Resolve(context.Background(), ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		CapabilityKey:     "commerce.order.create",
		Context:           Context{TenantID: "tenant-123"},
		Candidates: []domain.Mapping{{
			ID:                      "mapping-other-tenant",
			MappingType:             "IDENTITY",
			TenantID:                "tenant-other",
			CanonicalEntityID:       "entity-abc",
			TargetCanonicalEntityID: "entity-tenant",
			ScopeID:                 "tenant-other",
			Direction:               "BIDIRECTIONAL",
			Cardinality:             "ONE_TO_ONE",
			Authority:               "baobab",
			Confidence:              "CONFIRMED",
			Status:                  "ACTIVE",
			EffectiveFrom:           "2025-01-01T00:00:00Z",
		}},
	})
	if err == nil {
		t.Fatal("expected cross-tenant mapping to be rejected")
	}
}

// baseSuccessfulRequest returns the fixture TestResolutionPipelineBuildsFinalDecision
// uses, so the entitlement-gate tests below can layer Grants/Scopes onto an
// otherwise-identical, otherwise-successful request.
func baseSuccessfulRequest() ResolutionRequest {
	return ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		CapabilityKey:     "commerce.order.create",
		Context: Context{
			TenantID:      "tenant-123",
			LegalEntityID: "legal-456",
			MarketID:      "market-789",
			CountryCode:   "ZA",
			CurrencyCode:  "ZAR",
			Locale:        "en-ZA",
		},
		Candidates: []domain.Mapping{{
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
		Bindings: []CapabilityBinding{{
			CapabilityKey:    "commerce.order.create",
			EngineID:         "engine-1",
			EngineInstanceID: "instance-1",
			BindingMode:      "PRIMARY",
			Priority:         100,
			Status:           "ACTIVE",
			ContractVersion:  "v1",
		}},
		EngineInstances: []EngineInstance{{
			ID:          "instance-1",
			EngineID:    "engine-1",
			Region:      "af-south-1",
			Environment: "production",
			Status:      "ACTIVE",
		}},
	}
}

// TestResolutionPipelineSkipsEntitlementGateWhenGrantsNil proves the
// backward-compatibility rule ResolutionRequest.Grants documents: a caller
// that never populates Grants (every caller today, since no
// capability.capability_grant backfill exists yet) gets exactly today's
// behavior, unaffected by the new entitlement gate.
func TestResolutionPipelineSkipsEntitlementGateWhenGrantsNil(t *testing.T) {
	pipeline := ResolutionPipeline{}
	req := baseSuccessfulRequest()
	req.Grants = nil
	if _, err := pipeline.Resolve(context.Background(), req); err != nil {
		t.Fatalf("expected nil Grants to skip the entitlement gate, got: %v", err)
	}
}

// TestResolutionPipelineEnforcesEntitlementWhenGrantsPopulated proves the
// opt-in half of the same rule: once a caller supplies Grants (even a
// non-matching or empty set), the entitlement gate is real and fails
// closed.
func TestResolutionPipelineEnforcesEntitlementWhenGrantsPopulated(t *testing.T) {
	pipeline := ResolutionPipeline{}
	req := baseSuccessfulRequest()
	req.Grants = []capabilitydomain.CapabilityGrant{}
	if _, err := pipeline.Resolve(context.Background(), req); err == nil {
		t.Fatal("expected resolution to fail closed with no effective grants")
	}
}

func TestResolutionPipelineRoutesThroughEffectiveGrant(t *testing.T) {
	pipeline := ResolutionPipeline{}
	req := baseSuccessfulRequest()
	req.Grants = []capabilitydomain.CapabilityGrant{
		{ID: "grant-1", CapabilityKey: "commerce.order.create", ScopeID: "scope-1", Source: capabilitydomain.GrantSourcePlatformBaseline, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: time.Now().UTC().Add(-time.Hour)},
	}
	req.Scopes = map[string]capabilitydomain.CapabilityScope{
		"scope-1": {TenantID: "tenant-123"},
	}
	result, err := pipeline.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("expected resolution to succeed with an effective compatible grant: %v", err)
	}
	if result.Trace.GrantID != "grant-1" {
		t.Fatalf("expected trace to cite the satisfying grant, got %#v", result.Trace)
	}
}

func TestResolutionPipelineEnforcesCapabilityLifecycleWhenPopulated(t *testing.T) {
	pipeline := ResolutionPipeline{}
	req := baseSuccessfulRequest()
	suspended := capabilitydomain.Capability{Key: "commerce.order.create", Lifecycle: capabilitydomain.CapabilityLifecycleSuspended}
	req.Capability = &suspended
	if _, err := pipeline.Resolve(context.Background(), req); err == nil {
		t.Fatal("expected a SUSPENDED capability to fail resolution closed")
	}

	active := capabilitydomain.Capability{Key: "commerce.order.create", Lifecycle: capabilitydomain.CapabilityLifecycleActive}
	req.Capability = &active
	if _, err := pipeline.Resolve(context.Background(), req); err != nil {
		t.Fatalf("expected an ACTIVE capability to resolve, got: %v", err)
	}
}
