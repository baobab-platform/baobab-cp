package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

func TestCapabilityResolveBatchHandlerResolvesEachEntityIndependently(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	seedCapabilityResolveFixture(t, repo, "tenant-123")
	seedResolvedContext(t, repo, "context-1", "tenant-123")

	handler := CapabilityResolveBatchHandler{
		Contexts: repo,
		Service:  service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo},
	}

	// "tenant-123" resolves (seeded fixture); "unknown-entity" does not --
	// per ADR-BCP-003 §70, one failing item must not hide the other's
	// success or abort the batch.
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve-batch", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_ids":["tenant-123","unknown-entity"]}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"capability_key":"commerce.order.create"`) {
		t.Fatalf("expected response to echo requested capability_key, got %s", body)
	}
	if !strings.Contains(body, `"canonical_entity_id":"tenant-123","status":"RESOLVED"`) {
		t.Fatalf("expected tenant-123 to resolve, got %s", body)
	}
	if !strings.Contains(body, `"canonical_entity_id":"unknown-entity","status":"DENIED"`) {
		t.Fatalf("expected unknown-entity to be denied without aborting the batch, got %s", body)
	}
}

func TestCapabilityResolveBatchHandlerRejectsEmptyList(t *testing.T) {
	handler := CapabilityResolveBatchHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve-batch", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_ids":[]}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("INVALID_REQUEST")) {
		t.Fatalf("expected empty-list rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityResolveBatchHandlerRejectsOversizedBatch(t *testing.T) {
	handler := CapabilityResolveBatchHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	ids := make([]string, maxBatchResolveEntities+1)
	for i := range ids {
		ids[i] = `"entity"`
	}
	body := `{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_ids":[` + strings.Join(ids, ",") + `]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve-batch", bytes.NewReader([]byte(body)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("INVALID_REQUEST")) {
		t.Fatalf("expected oversized-batch rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityResolveBatchHandlerRejectsUnauthenticated(t *testing.T) {
	handler := CapabilityResolveBatchHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve-batch", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_ids":["entity-1"]}`)))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityResolveBatchHandlerRejectsCrossTenantContext(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	seedResolvedContext(t, repo, "context-1", "tenant-victim")

	handler := CapabilityResolveBatchHandler{Contexts: repo, Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve-batch", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_ids":["entity-1"]}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-attacker", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("TENANT_CONTEXT_MISMATCH")) {
		t.Fatalf("expected fail-closed cross-tenant rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityResolveBatchHandlerFailsClosedWhenContextStoreUnavailable(t *testing.T) {
	handler := CapabilityResolveBatchHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve-batch", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_ids":["entity-1"]}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusServiceUnavailable || !bytes.Contains(w.Body.Bytes(), []byte("CONTEXT_STORE_UNAVAILABLE")) {
		t.Fatalf("expected fail-closed 503, got %d body=%s", w.Code, w.Body.String())
	}
}
