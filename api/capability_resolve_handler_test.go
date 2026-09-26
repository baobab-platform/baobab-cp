package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

func seedCapabilityResolveFixture(t *testing.T, repo *repository.Repository, tenantID string) {
	t.Helper()
	repo.Mappings[tenantID] = []domain.Mapping{{
		ID:                      "mapping-tenant",
		MappingType:             "IDENTITY",
		TenantID:                tenantID,
		CanonicalEntityID:       tenantID,
		TargetCanonicalEntityID: "entity-tenant",
		ScopeID:                 tenantID,
		Direction:               "BIDIRECTIONAL",
		Cardinality:             "ONE_TO_ONE",
		Authority:               "baobab",
		Confidence:              "CONFIRMED",
		Status:                  "ACTIVE",
		ResolutionPriority:      50,
		EffectiveFrom:           "2025-01-01T00:00:00Z",
	}}
	repo.Bindings["commerce.order.create"] = []resolver.CapabilityBinding{{
		CapabilityKey:    "commerce.order.create",
		EngineID:         "engine-1",
		EngineInstanceID: "instance-1",
		BindingMode:      "PRIMARY",
		Priority:         100,
		Status:           "ACTIVE",
		ContractVersion:  "v1",
	}}
	repo.EngineInstances["engine-1"] = []resolver.EngineInstance{{
		ID:          "instance-1",
		EngineID:    "engine-1",
		Region:      "af-south-1",
		Environment: "production",
		Status:      "ACTIVE",
	}}
}

func seedResolvedContext(t *testing.T, repo *repository.Repository, id, tenantID string) {
	t.Helper()
	resolved := domain.Context{
		ID:            id,
		PrincipalID:   "principal-abc",
		TenantID:      tenantID,
		MarketID:      "market-789",
		CountryCode:   "ZA",
		CurrencyCode:  "ZAR",
		Locale:        "en-ZA",
		CorrelationID: "correlation-123",
		ResolvedAt:    time.Now().UTC(),
		Provenance: map[string]domain.ContextSource{
			"tenant_id": {Source: "verified_token", TrustLevel: domain.TrustVerified},
		},
	}
	if err := repo.CreateContext(context.Background(), resolved); err != nil {
		t.Fatalf("seed resolved context: %v", err)
	}
}

func TestCapabilityResolveHandlerRedeemsContextAndResolves(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	seedCapabilityResolveFixture(t, repo, "tenant-123")
	seedResolvedContext(t, repo, "context-1", "tenant-123")

	handler := CapabilityResolveHandler{
		Contexts: repo,
		Service:  service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"tenant-123"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"context_id":"context-1"`)) {
		t.Fatalf("expected response to echo context_id, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"capability_key":"commerce.order.create"`)) {
		t.Fatalf("expected response to echo requested capability_key, got %s", w.Body.String())
	}
	// Only the canonical ei_ identifier reaches the wire; the surrogate
	// never does (ADR-SHARED-012).
	if !bytes.Contains(w.Body.Bytes(), []byte(`"engine_instance_id":"ei_instance1"`)) ||
		!bytes.Contains(w.Body.Bytes(), []byte(`"id":"ei_instance1"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"instance-1"`)) {
		t.Fatalf("expected the canonical engine instance identifier, got %s", w.Body.String())
	}
}

// TestCapabilityResolveHandlerAcceptsRequestWhenClaimEmpty is this
// handler's counterpart to
// TestResolverHandlerAcceptsRequestSuppliedTenantWhenClaimEmpty: a
// workload token with no tenant_id claim of its own (the real-world case
// for every workload client today) can still redeem a context_id that was
// already tenant-validated when PlatformContextHandler created it.
func TestCapabilityResolveHandlerAcceptsRequestWhenClaimEmpty(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	seedCapabilityResolveFixture(t, repo, "tenant-123")
	seedResolvedContext(t, repo, "context-1", "tenant-123")

	handler := CapabilityResolveHandler{
		Contexts: repo,
		Service:  service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"tenant-123"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityResolveHandlerRejectsMissingFields(t *testing.T) {
	handler := CapabilityResolveHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader([]byte(`{}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("INVALID_REQUEST")) {
		t.Fatalf("expected missing-field rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityResolveHandlerRejectsUnauthenticated(t *testing.T) {
	handler := CapabilityResolveHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"tenant-123"}`)))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityResolveHandlerRejectsUnknownContextID(t *testing.T) {
	handler := CapabilityResolveHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader([]byte(`{"context_id":"missing-context","capability_key":"commerce.order.create","canonical_entity_id":"tenant-123"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusNotFound || !bytes.Contains(w.Body.Bytes(), []byte("CONTEXT_NOT_FOUND")) {
		t.Fatalf("expected 404 CONTEXT_NOT_FOUND, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestCapabilityResolveHandlerRejectsCrossTenantContext is a regression test
// for the cross-tenant leakage ADR-0005's tenant/legal-entity separation
// exists to prevent: a workload token for one tenant must not be able to
// redeem a context_id that was resolved for a different tenant.
func TestCapabilityResolveHandlerRejectsCrossTenantContext(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	seedResolvedContext(t, repo, "context-1", "tenant-victim")

	handler := CapabilityResolveHandler{Contexts: repo, Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"tenant-victim"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-attacker", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("TENANT_CONTEXT_MISMATCH")) {
		t.Fatalf("expected fail-closed cross-tenant rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityResolveHandlerFailsClosedWhenContextStoreUnavailable(t *testing.T) {
	handler := CapabilityResolveHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"tenant-123"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusServiceUnavailable || !bytes.Contains(w.Body.Bytes(), []byte("CONTEXT_STORE_UNAVAILABLE")) {
		t.Fatalf("expected fail-closed 503, got %d body=%s", w.Code, w.Body.String())
	}
}
