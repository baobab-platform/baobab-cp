package resolver

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestDuplicateResolutionRequestsAreDeterministic(t *testing.T) {
	req := resilientResolutionRequest()
	first, err := (ResolutionPipeline{}).Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("first resolution: %v", err)
	}
	second, err := (ResolutionPipeline{}).Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("duplicate resolution: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("duplicate request changed decision: first=%#v second=%#v", first, second)
	}
}

func TestEngineStateChangeIsRevalidated(t *testing.T) {
	req := resilientResolutionRequest()
	if _, err := (ResolutionPipeline{}).Resolve(context.Background(), req); err != nil {
		t.Fatalf("active instance should route: %v", err)
	}
	req.EngineInstances[0].Status = "RETIRED"
	if _, err := (ResolutionPipeline{}).Resolve(context.Background(), req); err == nil {
		t.Fatal("retired instance must not route from a previously valid request")
	}
}

func TestConcurrentBindingChangeFailsClosed(t *testing.T) {
	req := resilientResolutionRequest()
	competing := req.Bindings[0]
	competing.ID = "binding-2"
	competing.EngineInstanceID = "instance-2"
	req.Bindings = append(req.Bindings, competing)
	if _, err := (ResolutionPipeline{}).Resolve(context.Background(), req); err == nil {
		t.Fatal("equal-ranked concurrent bindings must be ambiguous")
	}
}

func TestResolutionHasNoCorrectnessDependencyOnCache(t *testing.T) {
	// The authoritative pipeline accepts only the current registry snapshot;
	// it has no cache handle and therefore cannot return stale cached routing
	// when a cache is unavailable. A future cache integration must retain this
	// test and revalidate the authoritative snapshot before returning a hit.
	if _, err := (ResolutionPipeline{}).Resolve(context.Background(), resilientResolutionRequest()); err != nil {
		t.Fatalf("authoritative resolution failed without cache: %v", err)
	}
}

func resilientResolutionRequest() ResolutionRequest {
	now := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	return ResolutionRequest{
		TenantID:          "tn_zuribeans",
		CanonicalEntityID: "entity-1",
		CapabilityKey:     "commerce.order.create",
		Context:           Context{TenantID: "tn_zuribeans", CountryCode: "ZA", ResolvedAt: now},
		Candidates: []domain.Mapping{{
			ID: "mapping-1", MappingType: "IDENTITY", TenantID: "tn_zuribeans", CanonicalEntityID: "entity-1",
			TargetCanonicalEntityID: "external-entity-1", ScopeID: "tn_zuribeans", Direction: "BIDIRECTIONAL",
			Cardinality: "ONE_TO_ONE", Authority: "baobab", Confidence: "CONFIRMED", Status: "ACTIVE",
			EffectiveFrom: "2025-01-01T00:00:00Z",
		}},
		Bindings: []CapabilityBinding{{
			ID: "binding-1", CapabilityKey: "commerce.order.create", EngineID: "trade", EngineInstanceID: "instance-1",
			BindingMode: "PRIMARY", Priority: 100, Status: "ACTIVE", ContractVersion: "1.0.0", EffectiveFrom: now.Add(-time.Hour),
		}},
		EngineInstances: []EngineInstance{{
			ID: "instance-1", EngineID: "trade", Status: "ACTIVE", HealthStatus: "HEALTHY", EffectiveFrom: now.Add(-time.Hour),
		}},
	}
}
