package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

type bindingDiscoveryRegistry struct{ allowed, withoutContext bool }

func (r bindingDiscoveryRegistry) IsActive(string) bool { return r.allowed }
func (r bindingDiscoveryRegistry) AllowsScope(clientID, scope string) bool {
	return r.allowed && clientID == tradeOrderClient &&
		(scope == tradeSellerBindingResolveScope || (scope == "context:resolve" && !r.withoutContext))
}

// bindingDiscoveryStore filters a registry exactly as the SQL candidate query
// does, so the handler sees what PostgreSQL would return.
type bindingDiscoveryStore struct {
	*tradeOrderStoreFixture
	registry      []postgres.SellerOperatingBinding
	candidatesErr error
	queries       int
	resolutions   []postgres.SellerBindingResolution
}

func (s *bindingDiscoveryStore) SellerBindingCandidates(_ context.Context, tenant, org, market, engine,
	ref, currency string) ([]postgres.SellerOperatingBinding, error) {
	s.queries++
	if s.candidatesErr != nil {
		return nil, s.candidatesErr
	}
	var out []postgres.SellerOperatingBinding
	for _, b := range s.registry {
		inScope := false
		for _, c := range b.CurrencyScope {
			inScope = inScope || c == currency
		}
		if b.TenantID == tenant && b.OperatingOrganisationID == org && b.Market == market &&
			b.TradeEngineInstanceID == engine && b.NativeConfigurationReference == ref && inScope {
			out = append(out, b)
		}
	}
	return out, nil
}

func (s *bindingDiscoveryStore) RecordSellerBindingResolution(_ context.Context, r postgres.SellerBindingResolution) (string, error) {
	if s.recordErr != nil {
		return "", s.recordErr
	}
	s.resolutions = append(s.resolutions, r)
	return "c5f9d7e1-6b3f-4f7a-9d2e-0a1b2c3d4e5f", nil
}

type goldenBindingVectors struct {
	EvaluatedAt time.Time `json:"evaluated_at"`
	Registry    []struct {
		BindingID      string     `json:"binding_id"`
		BindingVersion int        `json:"binding_version"`
		Status         string     `json:"status"`
		TenantID       string     `json:"tenant_id"`
		Organisation   string     `json:"operating_organisation_id"`
		Market         string     `json:"market"`
		Engine         string     `json:"trade_engine_instance_id"`
		Reference      string     `json:"native_configuration_reference"`
		Digest         string     `json:"native_configuration_digest"`
		CurrencyScope  []string   `json:"currency_scope"`
		EffectiveFrom  time.Time  `json:"effective_from"`
		EffectiveTo    *time.Time `json:"effective_to"`
	} `json:"registry"`
	Vectors []struct {
		VectorID string `json:"vector_id"`
		Context  struct {
			TenantID     string `json:"tenant_id"`
			Organisation string `json:"operating_organisation_id"`
			Market       string `json:"market"`
		} `json:"context"`
		Request         bindingResolveRequest  `json:"request"`
		ExpectedOutcome string                 `json:"expected_outcome"`
		ExpectedBinding *tradeBindingReference `json:"expected_binding_reference"`
	} `json:"vectors"`
}

func loadGoldenBindingVectors(t *testing.T) (goldenBindingVectors, []postgres.SellerOperatingBinding) {
	t.Helper()
	raw, err := contracts.ReadEmbedded("trade/v2/examples/binding-resolution-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var g goldenBindingVectors
	mustNoError(t, json.Unmarshal(raw, &g))
	if len(g.Vectors) == 0 || len(g.Registry) == 0 {
		t.Fatal("empty golden vector set")
	}
	var registry []postgres.SellerOperatingBinding
	for _, x := range g.Registry {
		registry = append(registry, postgres.SellerOperatingBinding{BindingID: x.BindingID, BindingVersion: x.BindingVersion,
			Status: x.Status, TenantID: x.TenantID, OperatingOrganisationID: x.Organisation, Market: x.Market,
			TradeEngineInstanceID: x.Engine, NativeConfigurationReference: x.Reference, NativeConfigurationDigest: x.Digest,
			CurrencyScope: x.CurrencyScope, EffectiveFrom: x.EffectiveFrom, EffectiveTo: x.EffectiveTo})
	}
	return g, registry
}

// The Shared golden vectors pin the precedence; CP's resolver must reproduce
// every outcome and binding exactly.
func TestSellerBindingDiscoveryGoldenVectors(t *testing.T) {
	g, registry := loadGoldenBindingVectors(t)
	for _, v := range g.Vectors {
		store := &bindingDiscoveryStore{registry: registry}
		candidates, _ := store.SellerBindingCandidates(context.Background(), v.Context.TenantID, v.Context.Organisation,
			v.Context.Market, v.Request.EngineInstanceID, v.Request.NativeConfigurationReference, v.Request.Currency)
		outcome, chosen := resolveSellerBinding(candidates, v.Request.NativeConfigurationDigest, g.EvaluatedAt)
		if outcome != v.ExpectedOutcome {
			t.Fatalf("%s: outcome %s, want %s", v.VectorID, outcome, v.ExpectedOutcome)
		}
		if (chosen == nil) != (v.ExpectedBinding == nil) ||
			(chosen != nil && (chosen.BindingID != v.ExpectedBinding.BindingID || chosen.BindingVersion != v.ExpectedBinding.BindingVersion)) {
			t.Fatalf("%s: binding %+v, want %+v", v.VectorID, chosen, v.ExpectedBinding)
		}
	}
}

type bindingDiscoveryFixture struct {
	handler http.Handler
	store   *bindingDiscoveryStore
	context domain.Context
}

func newBindingDiscoveryFixture(t *testing.T, store *bindingDiscoveryStore, o tradeOrderOptions,
	tenant, org, market string) bindingDiscoveryFixture {
	t.Helper()
	repo := repository.NewInMemoryRepository()
	principal := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
	mustNoError(t, repo.CreateIdentity(context.Background(), principal))
	issuer := "https://iam.example.test"
	mustNoError(t, repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID, Issuer: issuer, Subject: tradeOrderClient, Status: "ACTIVE"}))
	now := time.Now().UTC()
	expiry := now.Add(o.contextExpiry)
	ctx := domain.Context{ID: domain.NewUUIDv7(), PrincipalID: principal.ID, TenantID: tenant, OrganisationID: org,
		CountryCode: market, MarketID: "market", CorrelationID: domain.NewUUIDv7(), ResolvedAt: now.Add(-time.Second),
		ExpiresAt: &expiry}
	if !o.owned {
		ctx.PrincipalID = domain.NewPrincipalID()
	}
	repo.Contexts[ctx.ID] = ctx
	if store.tradeOrderStoreFixture == nil {
		store.tradeOrderStoreFixture = &tradeOrderStoreFixture{bound: true}
	}
	handler := New(Dependencies{
		Store:       &fakeStore{tenant: domain.Tenant{TenantID: tenant, ObservedState: o.tenantState}},
		Environment: o.environment, TradeOrderAuthorityEnabled: o.enabled,
		TradeOrderAuthority: store.tradeOrderStoreFixture, SellerOperatingBindings: store.tradeOrderStoreFixture,
		SellerBindingDiscovery: store, TradeOrderAuthorityClientIDs: o.allowlist,
		WorkloadVerifier: fakeVerifier{principal: auth.Principal{Issuer: issuer, Subject: tradeOrderClient,
			ActorType: "workload", ClientID: tradeOrderClient, TenantID: tenant, TokenID: "token-discovery",
			Scopes: map[string]struct{}{"context:resolve": {}, tradeSellerBindingResolveScope: {}}}},
		WorkloadRegistry: bindingDiscoveryRegistry{allowed: o.registered, withoutContext: o.contextRevoked},
		Contexts:         repo, Identities: repo,
	})
	return bindingDiscoveryFixture{handler: handler, store: store, context: ctx}
}

func (f bindingDiscoveryFixture) post(t *testing.T, req bindingResolveRequest, extra map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req.ContextID = f.context.ID
	body := map[string]any{}
	raw, _ := json.Marshal(req)
	mustNoError(t, json.Unmarshal(raw, &body))
	for k, v := range extra {
		body[k] = v
	}
	raw, _ = json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/internal/trade/v1/seller-operating-bindings/resolve", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer verified-stub")
	r.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	f.handler.ServeHTTP(out, r)
	return out
}

// Rebase the golden registry onto "now" so effective dates keep their
// meaning relative to the real clock the handler reads.
func liveGoldenRegistry(t *testing.T) (goldenBindingVectors, []postgres.SellerOperatingBinding) {
	g, registry := loadGoldenBindingVectors(t)
	shift := time.Now().UTC().Sub(g.EvaluatedAt)
	for i := range registry {
		registry[i].EffectiveFrom = registry[i].EffectiveFrom.Add(shift)
		if registry[i].EffectiveTo != nil {
			end := registry[i].EffectiveTo.Add(shift)
			registry[i].EffectiveTo = &end
		}
	}
	return g, registry
}

func TestSellerBindingDiscoveryEndToEndOverGoldenVectors(t *testing.T) {
	g, registry := liveGoldenRegistry(t)
	for _, v := range g.Vectors {
		store := &bindingDiscoveryStore{registry: registry}
		f := newBindingDiscoveryFixture(t, store, defaultTradeOrderOptions(), v.Context.TenantID, v.Context.Organisation, v.Context.Market)
		out := f.post(t, v.Request, nil)
		if out.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", v.VectorID, out.Code, out.Body.String())
		}
		var got bindingResolution
		mustNoError(t, json.Unmarshal(out.Body.Bytes(), &got))
		if got.Outcome != v.ExpectedOutcome || got.ProviderPermissionsGranted || got.ResolutionID == "" ||
			got.ContextID != f.context.ID || got.NativeConfigurationDigest != v.Request.NativeConfigurationDigest ||
			got.NativeConfigurationReference != v.Request.NativeConfigurationReference || got.Currency != v.Request.Currency ||
			got.EngineInstanceID != v.Request.EngineInstanceID {
			t.Fatalf("%s: %+v", v.VectorID, got)
		}
		if v.ExpectedBinding == nil {
			if got.BindingReference != nil || got.Status != "" || got.EffectiveFrom != nil {
				t.Fatalf("%s: a denial disclosed a binding: %+v", v.VectorID, got)
			}
		} else if got.BindingReference == nil || *got.BindingReference != *v.ExpectedBinding || got.Status != "ACTIVE" {
			t.Fatalf("%s: wrong binding %+v", v.VectorID, got.BindingReference)
		}
		rec := store.resolutions
		if len(rec) != 1 || rec[0].Outcome != got.Outcome || rec[0].TenantID != v.Context.TenantID ||
			rec[0].OperatingOrganisationID != v.Context.Organisation || rec[0].Market != v.Context.Market ||
			rec[0].ClientID != tradeOrderClient {
			t.Fatalf("%s: resolution not recorded with the context scope: %+v", v.VectorID, rec)
		}
	}
}

func goldenRequest(t *testing.T) (bindingResolveRequest, []postgres.SellerOperatingBinding, goldenBindingVectors) {
	g, registry := liveGoldenRegistry(t)
	return g.Vectors[0].Request, registry, g
}

func TestSellerBindingDiscoveryRejectsCallerSelectedScope(t *testing.T) {
	req, registry, g := goldenRequest(t)
	c := g.Vectors[0].Context
	for _, field := range []string{"tenant_id", "operating_organisation_id", "market", "binding_id", "binding_reference",
		"status", "responsible_legal_entity_id", "cart_id", "sales_channel_id", "region_id", "currency_scope"} {
		store := &bindingDiscoveryStore{registry: registry}
		f := newBindingDiscoveryFixture(t, store, defaultTradeOrderOptions(), c.TenantID, c.Organisation, c.Market)
		if out := f.post(t, req, map[string]any{field: "forged"}); out.Code != http.StatusBadRequest ||
			store.queries != 0 || len(store.resolutions) != 0 {
			t.Fatalf("%s: forged request reached the registry (%d)", field, out.Code)
		}
	}
}

func TestSellerBindingDiscoveryCallerGatingAndRoute(t *testing.T) {
	req, registry, g := goldenRequest(t)
	c := g.Vectors[0].Context
	for name, mutate := range map[string]func(*tradeOrderOptions){
		"disabled":     func(o *tradeOrderOptions) { o.enabled = false },
		"production":   func(o *tradeOrderOptions) { o.environment = "production" },
		"no-allowlist": func(o *tradeOrderOptions) { o.allowlist = nil },
	} {
		o := defaultTradeOrderOptions()
		mutate(&o)
		store := &bindingDiscoveryStore{registry: registry}
		if out := newBindingDiscoveryFixture(t, store, o, c.TenantID, c.Organisation, c.Market).post(t, req, nil); out.Code != http.StatusNotFound {
			t.Fatalf("%s: route must stay unmounted, got %d", name, out.Code)
		}
	}
	for name, mutate := range map[string]func(*tradeOrderOptions){
		"not-allowlisted":       func(o *tradeOrderOptions) { o.allowlist = []string{"someone-else"} },
		"not-registered":        func(o *tradeOrderOptions) { o.registered = false },
		"context-scope-revoked": func(o *tradeOrderOptions) { o.contextRevoked = true },
	} {
		o := defaultTradeOrderOptions()
		mutate(&o)
		store := &bindingDiscoveryStore{registry: registry}
		if out := newBindingDiscoveryFixture(t, store, o, c.TenantID, c.Organisation, c.Market).post(t, req, nil); out.Code != http.StatusForbidden ||
			store.queries+len(store.resolutions) != 0 {
			t.Fatalf("%s: untrusted caller reached the registry (%d)", name, out.Code)
		}
	}
	o := defaultTradeOrderOptions()
	o.owned = false
	store := &bindingDiscoveryStore{registry: registry}
	if out := newBindingDiscoveryFixture(t, store, o, c.TenantID, c.Organisation, c.Market).post(t, req, nil); out.Code != http.StatusNotFound ||
		store.queries != 0 {
		t.Fatalf("another workload's context must not redeem, got %d", out.Code)
	}
}

func TestSellerBindingDiscoveryContextAndEngineDenials(t *testing.T) {
	req, registry, g := goldenRequest(t)
	c := g.Vectors[0].Context
	o := defaultTradeOrderOptions()
	o.tenantState = "suspended"
	store := &bindingDiscoveryStore{registry: registry}
	out := newBindingDiscoveryFixture(t, store, o, c.TenantID, c.Organisation, c.Market).post(t, req, nil)
	var got bindingResolution
	mustNoError(t, json.Unmarshal(out.Body.Bytes(), &got))
	if got.Outcome != "CONTEXT_NOT_USABLE" || store.queries != 0 || len(store.resolutions) != 1 ||
		store.resolutions[0].TenantID != "" {
		t.Fatalf("inactive tenant must deny without scope: %+v", got)
	}
	store = &bindingDiscoveryStore{registry: registry, tradeOrderStoreFixture: &tradeOrderStoreFixture{bound: false}}
	out = newBindingDiscoveryFixture(t, store, defaultTradeOrderOptions(), c.TenantID, c.Organisation, c.Market).post(t, req, nil)
	mustNoError(t, json.Unmarshal(out.Body.Bytes(), &got))
	if got.Outcome != "ENGINE_INSTANCE_NOT_BOUND" || store.queries != 0 || got.BindingReference != nil {
		t.Fatalf("unbound engine must deny before the registry: %+v", got)
	}
}

func TestSellerBindingDiscoveryFailsClosed(t *testing.T) {
	req, registry, g := goldenRequest(t)
	c := g.Vectors[0].Context
	for name, store := range map[string]*bindingDiscoveryStore{
		"unrecorded":    {registry: registry, tradeOrderStoreFixture: &tradeOrderStoreFixture{bound: true, recordErr: errors.New("db down")}},
		"registry-down": {registry: registry, candidatesErr: errors.New("db down")},
		"engine-check":  {registry: registry, tradeOrderStoreFixture: &tradeOrderStoreFixture{boundErr: errors.New("db down")}},
	} {
		out := newBindingDiscoveryFixture(t, store, defaultTradeOrderOptions(), c.TenantID, c.Organisation, c.Market).post(t, req, nil)
		if out.Code != http.StatusServiceUnavailable || bytes.Contains(out.Body.Bytes(), []byte("binding_reference")) {
			t.Fatalf("%s: must be unavailable without disclosure: %d %s", name, out.Code, out.Body.String())
		}
	}
}
