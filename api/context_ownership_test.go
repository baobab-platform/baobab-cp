package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

// TestStoredContextIsBoundToItsResolvingPrincipal: a context_id is a handle,
// not a bearer credential. Every consumer answers a caller that is not the
// resolving principal exactly as it answers an unknown id, whether or not the
// caller's token carries a tenant_id -- no real workload token does, which is
// what once let any workload redeem any context.
func TestStoredContextIsBoundToItsResolvingPrincipal(t *testing.T) {
	contexts := repository.NewInMemoryRepository()
	seedResolutionContext(t, contexts, resolutionContext, resolutionTenant, "UG")
	resolutionIdentity(t, contexts, "baobab-other")

	scope := map[string]struct{}{"context:resolve": {}}
	callers := map[string]auth.Principal{
		"owner, no tenant claim":      {Subject: "baobab-trade", Issuer: resolutionIssuer, ActorType: "workload", TokenID: "o", Scopes: scope},
		"another registered workload": {Subject: "baobab-other", Issuer: resolutionIssuer, ActorType: "workload", TokenID: "s", Scopes: scope},
		"unregistered workload":       {Subject: "baobab-nobody", Issuer: resolutionIssuer, ActorType: "workload", TokenID: "u", Scopes: scope},
		"same subject, other issuer":  {Subject: "baobab-trade", Issuer: "https://other.test/realms/x", ActorType: "workload", TokenID: "i", Scopes: scope},
		"no issuer":                   {Subject: "baobab-trade", ActorType: "workload", TokenID: "n", Scopes: scope},
	}
	body := func(id string) []byte {
		raw, _ := json.Marshal(map[string]any{"context_id": id, "capability_key": resolutionKey, "correlation_id": "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a99"})
		return raw
	}
	handlers := map[string]http.HandlerFunc{
		"resolve": CapabilityResolveHandler{Contexts: contexts, Identities: contexts, Service: service.CapabilityResolutionService{Store: newResolutionStore()}}.Resolve,
	}
	for name, p := range callers {
		for handler, h := range handlers {
			call := func(id string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader(body(id)))
				w := httptest.NewRecorder()
				h(w, req.WithContext(auth.WithPrincipal(context.Background(), p)))
				return w
			}
			owned, unknown := call(resolutionContext), call("context-never-existed")
			if name == "owner, no tenant claim" {
				if owned.Code != http.StatusOK {
					t.Fatalf("%s/%s: the resolving principal must be able to consume its context: %d %s", handler, name, owned.Code, owned.Body.String())
				}
				continue
			}
			if owned.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound || owned.Body.String() != unknown.Body.String() {
				t.Fatalf("%s/%s: a context another principal resolved must be indistinguishable from an unknown one:\n%d %s\n%d %s",
					handler, name, owned.Code, owned.Body.String(), unknown.Code, unknown.Body.String())
			}
		}
	}
}

// TestContextConsumptionWithoutIdentityLookupFailsClosed: with no identity
// lookup configured no context can be consumed, rather than every context.
func TestContextConsumptionWithoutIdentityLookupFailsClosed(t *testing.T) {
	contexts := repository.NewInMemoryRepository()
	seedResolutionContext(t, contexts, resolutionContext, resolutionTenant, "UG")
	p := auth.Principal{Subject: "baobab-trade", Issuer: resolutionIssuer, ActorType: "workload", TokenID: "o", Scopes: map[string]struct{}{"context:resolve": {}}}
	raw, _ := json.Marshal(map[string]any{"context_id": resolutionContext, "capability_key": resolutionKey, "correlation_id": "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a99"})
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	CapabilityResolveHandler{Contexts: contexts, Service: service.CapabilityResolutionService{Store: newResolutionStore()}}.Resolve(w, req.WithContext(auth.WithPrincipal(context.Background(), p)))
	if w.Code == http.StatusOK {
		t.Fatalf("a context was consumed with no identity lookup configured: %s", w.Body.String())
	}
}

// TestBatchResolutionIsBoundToTheResolvingPrincipal covers the batch consumer.
func TestBatchResolutionIsBoundToTheResolvingPrincipal(t *testing.T) {
	contexts := repository.NewInMemoryRepository()
	seedResolutionContext(t, contexts, resolutionContext, resolutionTenant, "UG")
	resolutionIdentity(t, contexts, "baobab-other")
	h := CapabilityResolveBatchHandler{Contexts: contexts, Identities: contexts, Service: service.CapabilityResolutionService{Store: newResolutionStore()}}.Resolve
	call := func(subject, id string) int {
		raw, _ := json.Marshal(map[string]any{"context_id": id, "capabilities": []string{resolutionKey}, "correlation_id": "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a98"})
		p := auth.Principal{Subject: subject, Issuer: resolutionIssuer, ActorType: "workload", TokenID: "t", Scopes: map[string]struct{}{"context:resolve": {}}}
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve-batch", bytes.NewReader(raw)).WithContext(auth.WithPrincipal(context.Background(), p)))
		return w.Code
	}
	if got := call("baobab-trade", resolutionContext); got != http.StatusOK {
		t.Fatalf("owner: %d", got)
	}
	if got := call("baobab-other", resolutionContext); got != http.StatusNotFound {
		t.Fatalf("another registered workload must see 404, got %d", got)
	}
}
