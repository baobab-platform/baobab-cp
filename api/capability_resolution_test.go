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
	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

// resolutionStore is an in-memory service.CapabilityResolutionStore.
type resolutionStore struct {
	capabilities map[string]capabilitydomain.Capability
	grants       []capabilitydomain.CapabilityGrant
	scopes       map[string]capabilitydomain.CapabilityScope
	bindings     []resolver.CapabilityBinding
	instances    []resolver.EngineInstance
	invocations  map[string]repository.ProviderInvocation
	recorded     []repository.CapabilityResolutionRecord
}

func (s *resolutionStore) GetCapability(_ context.Context, key string) (capabilitydomain.Capability, error) {
	c, ok := s.capabilities[key]
	if !ok {
		return c, repository.ErrCapabilityNotFound
	}
	return c, nil
}
func (s *resolutionStore) ListGrants(_ context.Context, tenantID, key string) ([]capabilitydomain.CapabilityGrant, error) {
	var out []capabilitydomain.CapabilityGrant
	for _, g := range s.grants {
		if g.TenantID == tenantID && g.CapabilityKey == key {
			out = append(out, g)
		}
	}
	return out, nil
}
func (s *resolutionStore) GetCapabilityScope(_ context.Context, id string) (capabilitydomain.CapabilityScope, error) {
	return s.scopes[id], nil
}
func (s *resolutionStore) ListBindings(_ context.Context, key string) ([]resolver.CapabilityBinding, error) {
	var out []resolver.CapabilityBinding
	for _, b := range s.bindings {
		if b.CapabilityKey == key {
			out = append(out, b)
		}
	}
	return out, nil
}
func (s *resolutionStore) ListActiveInstances(context.Context, string) ([]resolver.EngineInstance, error) {
	return s.instances, nil
}
func (s *resolutionStore) HealthLevels(context.Context, string, string, string) (health.Levels, error) {
	return health.Levels{}, nil
}
func (s *resolutionStore) ProviderInvocationByID(_ context.Context, id string) (repository.ProviderInvocation, bool, error) {
	inv, ok := s.invocations[id]
	return inv, ok, nil
}
func (s *resolutionStore) RecordCapabilityResolution(_ context.Context, r repository.CapabilityResolutionRecord) error {
	s.recorded = append(s.recorded, r)
	return nil
}

const (
	resolutionTenant   = "tn_resolution"
	resolutionKey      = "commerce.order.create"
	resolutionScope    = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a01"
	resolutionGrant    = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a02"
	resolutionBinding  = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a03"
	resolutionProvider = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a04"
	resolutionInstance = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a05"
	resolutionEngine   = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a06"
	resolutionContext  = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a07"
)

// newResolutionStore holds one capability, granted to the tenant, bound to
// one ACTIVE instance whose provider declares its invocation.
func newResolutionStore() *resolutionStore {
	return &resolutionStore{
		capabilities: map[string]capabilitydomain.Capability{resolutionKey: {Key: resolutionKey, Lifecycle: capabilitydomain.CapabilityLifecycleActive}},
		grants: []capabilitydomain.CapabilityGrant{{ID: resolutionGrant, TenantID: resolutionTenant, CapabilityKey: resolutionKey,
			ScopeID: resolutionScope, Status: capabilitydomain.GrantStatusActive, EffectiveFrom: time.Now().Add(-time.Hour)}},
		scopes: map[string]capabilitydomain.CapabilityScope{resolutionScope: {ScopeID: resolutionScope, TenantID: resolutionTenant}},
		bindings: []resolver.CapabilityBinding{{ID: resolutionBinding, CapabilityKey: resolutionKey, ProviderID: resolutionProvider,
			EngineID: resolutionEngine, EngineInstanceID: resolutionInstance, ScopeID: resolutionScope, BindingMode: "PRIMARY",
			Priority: 100, Status: "ACTIVE", ContractVersion: "1"}},
		instances: []resolver.EngineInstance{{ID: resolutionInstance, EngineID: resolutionEngine, Region: "af-south-1",
			Environment: "production", Status: "ACTIVE"}},
		invocations: map[string]repository.ProviderInvocation{resolutionProvider: {ServiceReference: "service://baobab-trade/orders", Protocol: "http"}},
	}
}

const resolutionIssuer = "https://iam.test/realms/baobab"

// resolutionIdentity registers a workload's canonical principal and returns
// its id, as the first context resolution would.
func resolutionIdentity(t *testing.T, repo *repository.Repository, subject string) string {
	t.Helper()
	p, err := service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy}.Resolve(context.Background(), resolutionIssuer, subject, "workload")
	if err != nil {
		t.Fatalf("register %s: %v", subject, err)
	}
	return p.ID
}

func seedResolutionContext(t *testing.T, repo *repository.Repository, id, tenantID, country string) {
	t.Helper()
	seedResolutionContextFor(t, repo, id, resolutionIdentity(t, repo, "baobab-trade"), tenantID, country)
}

func seedResolutionContextFor(t *testing.T, repo *repository.Repository, id, principalID, tenantID, country string) {
	t.Helper()
	c := domain.Context{ID: id, PrincipalID: principalID, TenantID: tenantID, CountryCode: country, CorrelationID: "correlation-123",
		ResolvedAt: time.Now().UTC(), Provenance: map[string]domain.ContextSource{"tenant_id": {Source: "verified_token", TrustLevel: domain.TrustVerified}}}
	if country != "" {
		c.MarketID, c.CurrencyCode = "mkt_contractug", "UGX"
	}
	if err := repo.CreateContext(context.Background(), c); err != nil {
		t.Fatalf("seed resolved context: %v", err)
	}
}

// TestCapabilityResolutionConformsToCapabilityV1 drives resolveCapability
// through each decision and resolveCapabilityBatch through a mixed batch.
// Every body conforms to Shared capability/v1 resolution.schema.json, and
// every decision is recorded.
func TestCapabilityResolutionConformsToCapabilityV1(t *testing.T) {
	dir := contracttest.SharedDir(t)
	resolutionSchema := contracttest.CompileSchema(t, dir, "capability/v1/resolution.schema.json#/$defs/resolution")
	batchSchema := contracttest.CompileSchema(t, dir, "capability/v1/resolution.schema.json#/$defs/batchResolution")
	principal := auth.Principal{Subject: "baobab-trade", Issuer: resolutionIssuer, ActorType: "workload", TenantID: resolutionTenant, ClientID: "baobab-trade",
		TokenID: "token-resolution", Scopes: map[string]struct{}{"context:resolve": {}}}
	contexts := repository.NewInMemoryRepository()
	seedResolutionContext(t, contexts, resolutionContext, resolutionTenant, "UG")
	seedResolutionContext(t, contexts, "context-nomarket", resolutionTenant, "")
	seedResolutionContext(t, contexts, "context-other", "tn_other", "UG")

	post := func(h http.HandlerFunc, path string, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
		w := httptest.NewRecorder()
		h(w, req)
		return w
	}
	request := func(contextID string, extra map[string]any) map[string]any {
		body := map[string]any{"context_id": contextID, "capability_key": resolutionKey, "correlation_id": "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a99"}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}
	type decision struct {
		Decision   string `json:"decision"`
		ReasonCode string `json:"reason_code"`
		GrantID    string `json:"grant_id"`
		BindingID  string `json:"binding_id"`
		Invocation *struct {
			ServiceReference string `json:"service_reference"`
			ProviderID       string `json:"provider_id"`
			EngineInstanceID string `json:"engine_instance_id"`
		} `json:"invocation"`
	}
	resolve := func(store *resolutionStore, body map[string]any) decision {
		t.Helper()
		w := post(CapabilityResolveHandler{Contexts: contexts, Identities: contexts, Service: service.CapabilityResolutionService{Store: store}}.Resolve,
			"/v1/capabilities/resolve", body)
		if w.Code != http.StatusOK {
			t.Fatalf("resolve: %d %s", w.Code, w.Body.String())
		}
		var v any
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &v))
		contracttest.ValidateJSON(t, resolutionSchema, v)
		var d decision
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &d))
		return d
	}

	// RESOLVED: the grant, the binding and how to invoke its provider.
	store := newResolutionStore()
	got := resolve(store, request(resolutionContext, nil))
	if got.Decision != "RESOLVED" || got.GrantID != "grant_0199a1b2c3d47e8f9a0b1c2d3e4f5a02" || got.BindingID != "bind_0199a1b2c3d47e8f9a0b1c2d3e4f5a03" ||
		got.Invocation == nil || got.Invocation.ServiceReference != "service://baobab-trade/orders" ||
		got.Invocation.ProviderID != "provider_0199a1b2c3d47e8f9a0b1c2d3e4f5a04" || got.Invocation.EngineInstanceID != "ei_0199a1b2c3d47e8f9a0b1c2d3e4f5a05" {
		t.Fatalf("resolved: %+v", got)
	}
	if len(store.recorded) != 1 || store.recorded[0].Decision != "RESOLVED" || store.recorded[0].ContextID != resolutionContext {
		t.Fatalf("recorded: %+v", store.recorded)
	}

	// Every other decision names its registered reason.
	for name, tc := range map[string]struct {
		mutate   func(*resolutionStore)
		body     map[string]any
		decision string
		code     string
	}{
		"unknown capability": {mutate: func(s *resolutionStore) { delete(s.capabilities, resolutionKey) }, decision: "DENIED", code: "CAPABILITY_UNKNOWN"},
		"inactive capability": {mutate: func(s *resolutionStore) {
			s.capabilities[resolutionKey] = capabilitydomain.Capability{Key: resolutionKey, Lifecycle: capabilitydomain.CapabilityLifecycleDeprecated}
		}, decision: "UNAVAILABLE", code: "CAPABILITY_INACTIVE"},
		"no grant":        {mutate: func(s *resolutionStore) { s.grants = nil }, decision: "DENIED", code: "GRANT_NOT_FOUND"},
		"suspended grant": {mutate: func(s *resolutionStore) { s.grants[0].Status = capabilitydomain.GrantStatusSuspended }, decision: "DENIED", code: "GRANT_SUSPENDED"},
		"no binding":      {mutate: func(s *resolutionStore) { s.bindings = nil }, decision: "UNAVAILABLE", code: "BINDING_NOT_FOUND"},
		"tied bindings": {mutate: func(s *resolutionStore) {
			twin := s.bindings[0]
			twin.ID = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a08"
			s.bindings = append(s.bindings, twin)
		}, decision: "AMBIGUOUS", code: "BINDING_AMBIGUOUS"},
		"unsupported contract version": {body: map[string]any{"required_contract_version": 2}, decision: "INCOMPATIBLE", code: "CONTRACT_VERSION_UNSUPPORTED"},
		"no invocation declared":       {mutate: func(s *resolutionStore) { s.invocations = nil }, decision: "UNAVAILABLE", code: "PROVIDER_INVOCATION_UNDECLARED"},
		"no market in the context":     {body: map[string]any{"context_id": "context-nomarket"}, decision: "DENIED", code: "RESIDENCY_POLICY_MISMATCH"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newResolutionStore()
			if tc.mutate != nil {
				tc.mutate(s)
			}
			got := resolve(s, request(resolutionContext, tc.body))
			if got.Decision != tc.decision || got.ReasonCode != tc.code || got.Invocation != nil || got.GrantID != "" {
				t.Fatalf("got %+v, want %s %s", got, tc.decision, tc.code)
			}
			if len(s.recorded) != 1 || s.recorded[0].ReasonCode != tc.code {
				t.Fatalf("recorded: %+v", s.recorded)
			}
		})
	}

	// The request and the context are checked before anything is decided.
	for name, tc := range map[string]struct {
		body   map[string]any
		status int
		code   string
	}{
		"no correlation id":        {body: map[string]any{"context_id": resolutionContext, "capability_key": resolutionKey}, status: http.StatusBadRequest, code: "VALIDATION_FAILED"},
		"canonical entity offered": {body: request(resolutionContext, map[string]any{"canonical_entity_id": "e-1"}), status: http.StatusBadRequest, code: "VALIDATION_FAILED"},
		"unknown context":          {body: request("context-missing", nil), status: http.StatusNotFound, code: "CONTEXT_NOT_FOUND"},
		"another tenant's context": {body: request("context-other", nil), status: http.StatusForbidden, code: "TENANT_CONTEXT_MISMATCH"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newResolutionStore()
			w := post(CapabilityResolveHandler{Contexts: contexts, Identities: contexts, Service: service.CapabilityResolutionService{Store: s}}.Resolve, "/v1/capabilities/resolve", tc.body)
			if w.Code != tc.status || !bytes.Contains(w.Body.Bytes(), []byte(tc.code)) || len(s.recorded) != 0 {
				t.Fatalf("got %d %s (recorded %d)", w.Code, w.Body.String(), len(s.recorded))
			}
		})
	}

	// A batch decides each capability on its own and records each.
	store = newResolutionStore()
	w := post(CapabilityResolveBatchHandler{Contexts: contexts, Identities: contexts, Service: service.CapabilityResolutionService{Store: store}}.Resolve,
		"/v1/capabilities/resolve-batch", map[string]any{"context_id": resolutionContext,
			"capabilities": []string{resolutionKey, "commerce.order.cancel"}, "correlation_id": "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a98"})
	if w.Code != http.StatusOK {
		t.Fatalf("batch: %d %s", w.Code, w.Body.String())
	}
	var batch any
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &batch))
	contracttest.ValidateJSON(t, batchSchema, batch)
	var decided struct {
		Decisions []decision `json:"decisions"`
	}
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &decided))
	if len(decided.Decisions) != 2 || decided.Decisions[0].Decision != "RESOLVED" || decided.Decisions[1].ReasonCode != "CAPABILITY_UNKNOWN" ||
		len(store.recorded) != 2 {
		t.Fatalf("batch decisions: %+v (recorded %d)", decided.Decisions, len(store.recorded))
	}
}
