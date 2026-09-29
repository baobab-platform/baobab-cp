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
		Markets:   contractMarkets{{CountryCode: "UG", RegistryMarketID: "mkt_contractug", CurrencyCode: "UGX"}},
	}

	request := []byte(`{"organisation_id":"org-contract","expected_organisation_type":"BUYER_ORGANISATION","country_code":"UG"}`)
	conform("platform-context request", "PlatformContextResolveRequest", request)
	w := call(PlatformContextHandler{ContextResolution: contexts, Contexts: repo, TTL: 5 * time.Minute}.Resolve,
		"/v1/platform-context/resolve", request)
	conform("platform-context response", "PlatformContext", w.Body.Bytes())
	var resolved platformContextResolveResponse
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &resolved))
	if resolved.CountryCode != "UG" || resolved.MarketID != "mkt_contractug" || resolved.CurrencyCode != "UGX" {
		t.Fatalf("the context does not carry the tenant's market participation: %+v", resolved)
	}
	// A country the tenant does not participate in is refused.
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", bytes.NewReader([]byte(`{"country_code":"KE"}`)))
	req = req.WithContext(auth.WithPrincipal(context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-0000000000c3"), principal))
	refused := httptest.NewRecorder()
	PlatformContextHandler{ContextResolution: contexts, Contexts: repo}.Resolve(refused, req)
	if refused.Code != http.StatusForbidden || !bytes.Contains(refused.Body.Bytes(), []byte(`"MARKET_CONTEXT_NOT_PARTICIPATING"`)) {
		t.Fatalf("a foreign country: %d %s", refused.Code, refused.Body.String())
	}

	// With the tenant's market in its context, the deprecated composed
	// resolution reaches a decision.
	seedCapabilityResolveFixture(t, repo, tenantID)
	composed := []byte(`{"capability_key":"commerce.order.create","canonical_entity_id":"` + tenantID + `"}`)
	conform("composed request", "ComposedResolutionRequest", composed)
	resolution := service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: repo}
	w = call(ResolverHandler{Service: resolution, ContextResolution: contexts}.Resolve, "/v1/resolve", composed)
	conform("composed response", "ComposedResolution", w.Body.Bytes())
}

type contractMarkets []domain.MarketParticipation

func (m contractMarkets) ActiveMarketParticipations(context.Context, string, time.Time) ([]domain.MarketParticipation, error) {
	return m, nil
}
