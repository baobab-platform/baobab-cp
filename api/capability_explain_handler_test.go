package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/resolver"
	"github.com/nabhold/baobab-cp/internal/service"
)

func explainAdminPrincipal() auth.Principal {
	return auth.Principal{Subject: "admin-123", ActorType: "human", TokenID: "token-123", Scopes: map[string]struct{}{"capabilities:explain": {}}}
}

func TestCapabilityExplainHandlerExplainsSuccessfulResolution(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	seedCapabilityResolveFixture(t, repo, "tenant-123")
	seedResolvedContext(t, repo, "context-1", "tenant-123")

	handler := CapabilityExplainHandler{
		Contexts: repo,
		Service:  service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"tenant-123"}`)))
	req = req.WithContext(auth.WithPrincipal(context.Background(), explainAdminPrincipal()))
	w := httptest.NewRecorder()

	handler.Explain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"capability_key":"commerce.order.create"`) {
		t.Fatalf("expected explanation to identify requested capability_key, got %s", body)
	}
	if !strings.Contains(body, `"outcome":"ROUTED"`) {
		t.Fatalf("expected ROUTED outcome, got %s", body)
	}
	if !strings.Contains(body, `"binding_id":"instance-1"`) && !strings.Contains(body, `"mapping_id":"mapping-tenant"`) {
		t.Fatalf("expected trace detail in explanation, got %s", body)
	}
}

func TestCapabilityExplainHandlerExplainsFailedResolution(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	seedResolvedContext(t, repo, "context-1", "tenant-123")

	handler := CapabilityExplainHandler{
		Contexts: repo,
		Service:  service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo},
	}

	// No mappings seeded for "unknown-entity" -- the explanation should
	// surface the FAILED outcome and its reason rather than a generic error.
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"unknown-entity"}`)))
	req = req.WithContext(auth.WithPrincipal(context.Background(), explainAdminPrincipal()))
	w := httptest.NewRecorder()

	handler.Explain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (explanation of a failure is still a successful explain), got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"outcome":"FAILED"`) {
		t.Fatalf("expected FAILED outcome, got %s", body)
	}
	if !strings.Contains(body, "no mappings for unknown-entity") {
		t.Fatalf("expected the underlying reason to be surfaced, got %s", body)
	}
}

func TestCapabilityExplainHandlerRejectsMissingFields(t *testing.T) {
	handler := CapabilityExplainHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", bytes.NewReader([]byte(`{"context_id":"context-1"}`)))
	req = req.WithContext(auth.WithPrincipal(context.Background(), explainAdminPrincipal()))
	w := httptest.NewRecorder()

	handler.Explain(w, req)
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("INVALID_REQUEST")) {
		t.Fatalf("expected missing-field rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityExplainHandlerRejectsNonAdmin(t *testing.T) {
	handler := CapabilityExplainHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"entity-1"}`)))
	// A workload principal -- even with a context:resolve scope -- must not
	// be able to reach privileged diagnostics meant for admin operators.
	principal := auth.Principal{Subject: "workload-1", ActorType: "workload", TenantID: "tenant-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Explain(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for non-admin principal, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityExplainHandlerRejectsAdminWithoutExplainScope(t *testing.T) {
	handler := CapabilityExplainHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"entity-1"}`)))
	principal := auth.Principal{Subject: "admin-1", ActorType: "human", Scopes: map[string]struct{}{"tenant:read": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Explain(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for admin missing capabilities:explain scope, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityExplainHandlerAllowsCrossTenantAdmin(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	seedCapabilityResolveFixture(t, repo, "tenant-victim")
	seedResolvedContext(t, repo, "context-1", "tenant-victim")

	handler := CapabilityExplainHandler{
		Contexts: repo,
		Service:  service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo},
	}

	// Unlike CapabilityResolveHandler/CapabilityResolveBatchHandler, an
	// admin explaining a context is deliberately not restricted to their
	// own tenant -- there is no principal.TenantID to compare against, and
	// cross-tenant diagnostics are exactly this endpoint's purpose.
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"tenant-victim"}`)))
	req = req.WithContext(auth.WithPrincipal(context.Background(), explainAdminPrincipal()))
	w := httptest.NewRecorder()

	handler.Explain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for cross-tenant admin diagnostics, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityExplainHandlerReturnsContextNotFound(t *testing.T) {
	handler := CapabilityExplainHandler{Contexts: repository.NewInMemoryRepository(), Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", bytes.NewReader([]byte(`{"context_id":"missing","capability_key":"commerce.order.create","canonical_entity_id":"entity-1"}`)))
	req = req.WithContext(auth.WithPrincipal(context.Background(), explainAdminPrincipal()))
	w := httptest.NewRecorder()

	handler.Explain(w, req)
	if w.Code != http.StatusNotFound || !bytes.Contains(w.Body.Bytes(), []byte("CONTEXT_NOT_FOUND")) {
		t.Fatalf("expected 404 CONTEXT_NOT_FOUND, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityExplainHandlerFailsClosedWhenContextStoreUnavailable(t *testing.T) {
	handler := CapabilityExplainHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", bytes.NewReader([]byte(`{"context_id":"context-1","capability_key":"commerce.order.create","canonical_entity_id":"entity-1"}`)))
	req = req.WithContext(auth.WithPrincipal(context.Background(), explainAdminPrincipal()))
	w := httptest.NewRecorder()

	handler.Explain(w, req)
	if w.Code != http.StatusServiceUnavailable || !bytes.Contains(w.Body.Bytes(), []byte("CONTEXT_STORE_UNAVAILABLE")) {
		t.Fatalf("expected fail-closed 503, got %d body=%s", w.Code, w.Body.String())
	}
}
