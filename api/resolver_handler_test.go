package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/resolver"
	"github.com/nabhold/baobab-cp/internal/service"
)

func TestResolverHandlerRejectsCallerSuppliedRegistryState(t *testing.T) {
	serviceInst := service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}
	handler := ResolverHandler{Service: serviceInst}

	body := map[string]any{
		"tenant_id": "tenant-123",
		"context": map[string]any{
			"tenant_id":       "tenant-123",
			"legal_entity_id": "legal-456",
			"market_id":       "market-789",
			"country_code":    "ZA",
			"currency_code":   "ZAR",
			"locale":          "en-ZA",
		},
		"mappings": []map[string]any{{
			"id":                         "mapping-tenant",
			"mapping_type":               "IDENTITY",
			"resolution_mode":            "SINGLE",
			"canonical_entity_id":        "tenant-123",
			"target_canonical_entity_id": "entity-tenant",
			"scope_id":                   "tenant-123",
			"direction":                  "BIDIRECTIONAL",
			"cardinality":                "ONE_TO_ONE",
			"authority":                  "baobab",
			"confidence":                 "CONFIRMED",
			"status":                     "ACTIVE",
			"resolution_priority":        50,
			"effective_from":             "2025-01-01T00:00:00Z",
		}},
		"bindings": []map[string]any{{
			"capability_key":     "baobab_trade",
			"engine_id":          "engine-1",
			"engine_instance_id": "instance-1",
			"binding_mode":       "PRIMARY",
			"priority":           100,
			"status":             "ACTIVE",
			"contract_version":   "v1",
		}},
		"engine_instances": []map[string]any{{
			"id":          "instance-1",
			"engine_id":   "engine-1",
			"region":      "af-south-1",
			"environment": "production",
			"status":      "ACTIVE",
		}},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader(payload))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000001")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected fail-closed 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("INVALID_REQUEST")) {
		t.Fatalf("caller-supplied registry state was not rejected: %s", w.Body.String())
	}
}

func TestResolverHandlerRejectsBadInput(t *testing.T) {
	handler := ResolverHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":""}`)))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestResolverHandlerRejectsTenantSpoofing(t *testing.T) {
	handler := ResolverHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-attacker"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-authorised", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("TENANT_CONTEXT_MISMATCH")) {
		t.Fatalf("expected fail-closed tenant mismatch, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestResolverHandlerRejectsNativeIDInjection(t *testing.T) {
	handler := ResolverHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123","native_id":"1000000"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()
	handler.Resolve(w, req)
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("INVALID_REQUEST")) {
		t.Fatalf("expected native ID injection rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestResolverHandlerRejectsAuthorizationBypass(t *testing.T) {
	handler := ResolverHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", TokenID: "token-123"}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()
	handler.Resolve(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing-scope rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestResolverHandlerDoesNotLeakInternalDenialReason(t *testing.T) {
	// ADR-0008 §45 ("Denial Model"): reason codes SHALL not expose sensitive
	// information indiscriminately to external clients. An authorized
	// request with nothing to resolve (no mappings/bindings loaded — this
	// test's ResolutionService has no Repository, and the request body
	// supplies none) fails deep inside the pipeline with an internal cause
	// such as "mapping not found". The client must see a fixed, opaque
	// message instead, mirroring resolveContext's ErrContextDenied handling.
	handler := ResolverHandler{
		Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}},
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repository.NewInMemoryRepository(), Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:  &fakeStore{},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123","capability_key":"commerce.order.create","canonical_entity_id":"entity-abc"}`)))
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000002")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected fail-closed 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("RESOLUTION_FAILED")) {
		t.Fatalf("expected RESOLUTION_FAILED code, got %s", w.Body.String())
	}
	for _, leaked := range []string{"mapping not found", "resolution FAILED", "resolution failed:"} {
		if bytes.Contains(w.Body.Bytes(), []byte(leaked)) {
			t.Fatalf("response leaked internal resolver detail %q: %s", leaked, w.Body.String())
		}
	}
}

// TestResolverHandlerRejectsMissingCanonicalEntityID is a regression test
// for docs/governance/gate-iam-0-discovery.md R-3: contracts/control-plane/
// v1/canonical-mapping.schema.json's resolutionRequest has always required
// canonical_entity_id, but this handler never accepted the field at all, so
// there was no way for a caller to supply one -- every request silently
// resolved against the tenant instead of a real entity.
func TestResolverHandlerRejectsMissingCanonicalEntityID(t *testing.T) {
	handler := ResolverHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("INVALID_REQUEST")) {
		t.Fatalf("expected missing canonical_entity_id rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestResolverHandlerRejectsUnresolvableIdentity is Gate IAM-3 phase 4's
// regression test for ADR-0004 §11 ("No business-domain operation SHALL
// proceed merely because iss + sub exists"): an authorized, in-scope
// workload request whose identity cannot be resolved (here, an unset
// Identity field -- a nil ProvisioningPolicy fails closed per
// service.IdentityService's own default) must not reach the resolution
// pipeline at all.
func TestResolverHandlerRejectsUnresolvableIdentity(t *testing.T) {
	handler := ResolverHandler{
		Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}},
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repository.NewInMemoryRepository()},
			Tenants:  &fakeStore{},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123","capability_key":"commerce.order.create","canonical_entity_id":"entity-abc"}`)))
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("IDENTITY_RESOLUTION_FAILED")) {
		t.Fatalf("expected unresolvable identity rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestResolverHandlerRejectsInactiveTenant is a regression test for
// ADR-BCP-004 §52/§53: before ContextResolutionService existed, the trusted
// Context this handler built never looked up the tenant record at all, so a
// workload token for a suspended or decommissioned tenant could still
// successfully resolve.
// TestResolverHandlerAcceptsRequestSuppliedTenantWhenClaimEmpty is the
// regression test for the real-world workload path: no workload client in
// nabhold/baobab-iam mints a tenant_id claim today (see
// resolveWorkloadTenant's doc comment), so this -- not the synthetic
// principal.TenantID every other test in this file hand-sets -- is what a
// real workload token looks like. Before this, every such request 403'd at
// api/router.go's authorize() before ever reaching this handler.
func TestResolverHandlerAcceptsRequestSuppliedTenantWhenClaimEmpty(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	active := domain.Tenant{TenantID: "tenant-123", LegalEntityID: "THAMANI-GLOBAL", ObservedState: string(domain.LifecycleActive)}
	handler := ResolverHandler{
		Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}},
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:  &fakeStore{tenant: active},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123","capability_key":"commerce.order.create","canonical_entity_id":"entity-abc"}`)))
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000006")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	// No mappings/bindings are loaded, so this still denies deep in the
	// resolution pipeline -- the point of this test is that it gets *past*
	// authentication and tenant reconciliation to reach that pipeline at
	// all, which TENANT_CONTEXT_MISMATCH or IDENTITY_RESOLUTION_FAILED
	// would mean it did not.
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("RESOLUTION_FAILED")) {
		t.Fatalf("expected the request to reach the resolution pipeline (RESOLUTION_FAILED), got %d body=%s", w.Code, w.Body.String())
	}
}

// TestResolverHandlerRejectsMissingTenantWhenClaimEmpty confirms a workload
// with no tenant_id claim still cannot leave the tenant entirely
// unconstrained by omitting it from the request too.
func TestResolverHandlerRejectsMissingTenantWhenClaimEmpty(t *testing.T) {
	handler := ResolverHandler{Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}}}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"canonical_entity_id":"entity-abc"}`)))
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("TENANT_CONTEXT_MISMATCH")) {
		t.Fatalf("expected a workload with no tenant claim and no requested tenant to be rejected, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestResolverHandlerRejectsInactiveTenant(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	suspended := domain.Tenant{TenantID: "tenant-123", LegalEntityID: "THAMANI-GLOBAL", ObservedState: string(domain.LifecycleSuspended)}
	handler := ResolverHandler{
		Service: service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}},
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:  &fakeStore{tenant: suspended},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123","capability_key":"commerce.order.create","canonical_entity_id":"entity-abc"}`)))
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000005")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("TENANT_NOT_ACTIVE")) {
		t.Fatalf("expected a suspended tenant to be rejected, got %d body=%s", w.Code, w.Body.String())
	}
}

var _ = domain.RegisterTenant{}
