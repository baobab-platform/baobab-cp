package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

// TestContextResolutionRoutesConformToShared: the bodies of
// resolvePlatformContext and the deprecated resolveComposed conform to the
// Shared control-plane/v1 platform-context.schema.json they are described by.
func TestContextResolutionRoutesConformToShared(t *testing.T) {
	const tenantID = "tn_contract"
	dir := contracttest.SharedDir(t)
	conform := func(label, def string, raw []byte) {
		t.Helper()
		var body any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "control-plane/v1/platform-context.schema.json#/$defs/"+def), body)
	}
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload",
		TenantID: tenantID, ClientID: "baobab-trade", TokenID: "token-contract", Scopes: map[string]struct{}{"context:resolve": {}}}
	call := func(h http.HandlerFunc, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		ctx := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-0000000000c1")
		req = req.WithContext(auth.WithPrincipal(ctx, principal))
		w := httptest.NewRecorder()
		h(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w
	}

	repo := repository.NewInMemoryRepository()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["org-contract"] = domain.CanonicalEntity{ID: "org-contract", EntityType: domain.EntityTypeBuyerOrganisation,
		Status: "ACTIVE", OwnerTenantID: tenantID}
	active := domain.Tenant{TenantID: tenantID, LegalEntityID: "CONTRACT-LE", ObservedState: string(domain.LifecycleActive)}
	contexts := service.ContextResolutionService{
		Identity:  service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
		Tenants:   &fakeStore{tenant: active},
		Canonical: canonical,
		Mappings:  noOrganisationMappings{},
	}

	request := []byte(`{"organisation_id":"org-contract","expected_organisation_type":"BUYER_ORGANISATION"}`)
	conform("platform-context request", "PlatformContextResolveRequest", request)
	w := call(PlatformContextHandler{ContextResolution: contexts, Contexts: repo, TTL: 5 * time.Minute}.Resolve,
		"/v1/platform-context/resolve", request)
	conform("platform-context response", "PlatformContext", w.Body.Bytes())

	// The composed decision body is shared with CapabilityResolveHandler,
	// which adds context_id; /v1/resolve's own context carries no market,
	// so its pipeline never reaches a decision (RESOLUTION_FAILED).
	seedCapabilityResolveFixture(t, repo, tenantID)
	seedResolvedContext(t, repo, "context-contract", tenantID)
	composed := []byte(`{"capability_key":"commerce.order.create","canonical_entity_id":"` + tenantID + `"}`)
	conform("composed request", "ComposedResolutionRequest", composed)
	resolution := service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo}
	w = call(CapabilityResolveHandler{Contexts: repo, Service: resolution}.Resolve, "/v1/capabilities/resolve",
		[]byte(`{"context_id":"context-contract","capability_key":"commerce.order.create","canonical_entity_id":"`+tenantID+`"}`))
	var body map[string]any
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	delete(body, "context_id")
	raw, _ := json.Marshal(body)
	conform("composed response", "ComposedResolution", raw)

	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader(composed))
	req = req.WithContext(auth.WithPrincipal(context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-0000000000c2"), principal))
	failed := httptest.NewRecorder()
	ResolverHandler{Service: resolution, ContextResolution: contexts}.Resolve(failed, req)
	if failed.Code != http.StatusBadRequest || !bytes.Contains(failed.Body.Bytes(), []byte(`"RESOLUTION_FAILED"`)) {
		t.Fatalf("/v1/resolve: %d %s", failed.Code, failed.Body.String())
	}
}
