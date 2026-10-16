package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const (
	tradeOrderClient   = "baobab-trade-legal-actor-assessor-staging"
	tradeEngine        = "ei_tradestaging01"
	tradeBindingID     = "4f1c2b3a-9d8e-4a7b-b6c5-0d1e2f3a4b5c"
	tradeLegalEntityID = "SYNTHETIC-LE-01"
)

var tradeDigest = strings.Repeat("ab", 32)

type tradeOrderRegistry struct {
	allowed        bool
	withoutContext bool
}

func (r tradeOrderRegistry) IsActive(string) bool { return r.allowed }
func (r tradeOrderRegistry) AllowsScope(clientID, scope string) bool {
	return r.allowed && clientID == tradeOrderClient &&
		(scope == tradeOrderAuthorityScope || scope == tradeSellerBindingAttestScope ||
			(scope == "context:resolve" && !r.withoutContext))
}

// tradeOrderStoreFixture implements both the resolver and the binding stores.
type tradeOrderStoreFixture struct {
	bound        bool
	boundErr     error
	binding      *postgres.SellerOperatingBinding
	bindingErr   error
	resolution   legalactor.Resolution
	recordErr    error
	requests     []legalactor.Request
	bindings     []string
	lookups      int
	recorded     []postgres.TradeOrderAuthorityDecision
	attestations []postgres.TradeNativeConfigurationAttestation
	attestation  *postgres.TradeNativeConfigurationAttestation
	proposed     []postgres.ProposeSellerOperatingBinding
	transitions  []postgres.TransitionSellerOperatingBinding
	actors       []string
	writeErr     error
}

func (f *tradeOrderStoreFixture) ResolveOperatingLegalActor(_ context.Context, r legalactor.Request) (legalactor.Resolution, error) {
	f.requests = append(f.requests, r)
	return f.resolution, nil
}

func (f *tradeOrderStoreFixture) TradeEngineInstanceBound(_ context.Context, tenant, instance string, _ time.Time) (bool, error) {
	f.bindings = append(f.bindings, tenant+"|"+instance)
	return f.bound, f.boundErr
}

func (f *tradeOrderStoreFixture) GetSellerOperatingBinding(_ context.Context, id string, version int) (postgres.SellerOperatingBinding, error) {
	f.lookups++
	if f.bindingErr != nil {
		return postgres.SellerOperatingBinding{}, f.bindingErr
	}
	if f.binding == nil || f.binding.BindingID != id || f.binding.BindingVersion != version {
		return postgres.SellerOperatingBinding{}, postgres.ErrSellerBindingNotFound
	}
	return *f.binding, nil
}

func (f *tradeOrderStoreFixture) RecordTradeOrderAuthorityDecision(_ context.Context, d postgres.TradeOrderAuthorityDecision) (string, error) {
	if f.recordErr != nil {
		return "", f.recordErr
	}
	f.recorded = append(f.recorded, d)
	return "b4e9c7d1-5b2f-4e6a-8c1d-9f0b7e3a2c55", nil
}

func (f *tradeOrderStoreFixture) RecordTradeNativeConfigurationAttestation(_ context.Context, a postgres.TradeNativeConfigurationAttestation) (postgres.TradeNativeConfigurationAttestation, error) {
	if f.writeErr != nil {
		return a, f.writeErr
	}
	a.AttestationID = "5a6b7c8d-9e0f-4a1b-8c2d-3e4f5a6b7c8d"
	a.RecordedAt = time.Now().UTC()
	f.attestations = append(f.attestations, a)
	return a, nil
}

func (f *tradeOrderStoreFixture) GetTradeNativeConfigurationAttestation(_ context.Context, id string) (postgres.TradeNativeConfigurationAttestation, error) {
	if f.attestation == nil || f.attestation.AttestationID != id {
		return postgres.TradeNativeConfigurationAttestation{}, postgres.ErrSellerBindingNotFound
	}
	return *f.attestation, nil
}

func (f *tradeOrderStoreFixture) ProposeOperatingBinding(_ context.Context, _ string, _ basestore.RequestMetadata, maker string, cmd postgres.ProposeSellerOperatingBinding) (postgres.SellerOperatingBinding, error) {
	if f.writeErr != nil {
		return postgres.SellerOperatingBinding{}, f.writeErr
	}
	f.proposed = append(f.proposed, cmd)
	f.actors = append(f.actors, maker)
	b := *f.binding
	b.Status, b.ProposedBy = "PENDING_APPROVAL", maker
	return b, nil
}

func (f *tradeOrderStoreFixture) TransitionOperatingBinding(_ context.Context, _ string, _ basestore.RequestMetadata, actor string, cmd postgres.TransitionSellerOperatingBinding) (postgres.SellerOperatingBinding, error) {
	if f.writeErr != nil {
		return postgres.SellerOperatingBinding{}, f.writeErr
	}
	f.transitions = append(f.transitions, cmd)
	f.actors = append(f.actors, actor)
	b := *f.binding
	b.Status, b.ApprovedBy, b.ApprovalID = "APPROVED", actor, "6b7c8d9e-0f1a-4b2c-8d3e-4f5a6b7c8d9e"
	return b, nil
}

type tradeOrderFixture struct {
	handler http.Handler
	store   *tradeOrderStoreFixture
	context domain.Context
}

type tradeOrderOptions struct {
	allowlist      []string
	registered     bool
	tenantState    string
	contextExpiry  time.Duration
	owned          bool
	environment    string
	enabled        bool
	contextRevoked bool
}

func defaultTradeOrderOptions() tradeOrderOptions {
	return tradeOrderOptions{allowlist: []string{tradeOrderClient}, registered: true, tenantState: "active",
		contextExpiry: time.Minute, owned: true, environment: "integration", enabled: true}
}

func activeTradeBinding(ctx domain.Context, now time.Time) *postgres.SellerOperatingBinding {
	return &postgres.SellerOperatingBinding{BindingID: tradeBindingID, BindingVersion: 3, Status: "ACTIVE",
		TenantID: ctx.TenantID, OperatingOrganisationID: ctx.OrganisationID, ResponsibleLegalEntityID: tradeLegalEntityID,
		Market: ctx.CountryCode, CurrencyScope: []string{"UGX", "USD"}, TradeEngineInstanceID: tradeEngine,
		NativeConfigurationReference: "trade/native-config/store-01", NativeConfigurationDigest: tradeDigest,
		AttestationID: "5a6b7c8d-9e0f-4a1b-8c2d-3e4f5a6b7c8d", EffectiveFrom: now.Add(-time.Hour),
		ProposedBy: domain.NewUUIDv7(), ProposalID: domain.NewUUIDv7(), EvidenceReferences: []string{"synthetic:binding/01"},
		Revision: 3, UpdatedAt: now.Add(-time.Minute)}
}

func newTradeOrderFixture(t *testing.T, store *tradeOrderStoreFixture, o tradeOrderOptions) tradeOrderFixture {
	t.Helper()
	repo := repository.NewInMemoryRepository()
	principal := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
	mustNoError(t, repo.CreateIdentity(context.Background(), principal))
	issuer, subject := "https://iam.example.test", tradeOrderClient
	mustNoError(t, repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID, Issuer: issuer, Subject: subject, Status: "ACTIVE"}))
	now := time.Now().UTC()
	expiry := now.Add(o.contextExpiry)
	ctx := domain.Context{ID: domain.NewUUIDv7(), PrincipalID: principal.ID,
		TenantID: "tn_tradeorder", OrganisationID: domain.NewUUIDv7(), CountryCode: "UG",
		MarketID: "market-ug", CorrelationID: domain.NewUUIDv7(), ResolvedAt: now.Add(-time.Second), ExpiresAt: &expiry}
	if !o.owned {
		ctx.PrincipalID = domain.NewPrincipalID()
	}
	repo.Contexts[ctx.ID] = ctx
	if store.binding == nil && store.bindingErr == nil {
		store.binding = activeTradeBinding(ctx, now)
	}
	handler := New(Dependencies{
		Store:       &fakeStore{tenant: domain.Tenant{TenantID: ctx.TenantID, ObservedState: o.tenantState}},
		Environment: o.environment, TradeOrderAuthorityEnabled: o.enabled,
		TradeOrderAuthority: store, SellerOperatingBindings: store, TradeOrderAuthorityClientIDs: o.allowlist,
		WorkloadVerifier: fakeVerifier{principal: auth.Principal{
			Issuer: issuer, Subject: subject, ActorType: "workload", ClientID: tradeOrderClient,
			TenantID: ctx.TenantID, TokenID: "token-trade-order",
			Scopes: map[string]struct{}{"context:resolve": {}, tradeOrderAuthorityScope: {}, tradeSellerBindingAttestScope: {}}}},
		WorkloadRegistry: tradeOrderRegistry{allowed: o.registered, withoutContext: o.contextRevoked},
		Contexts:         repo, Identities: repo,
	})
	return tradeOrderFixture{handler: handler, store: store, context: ctx}
}

func (f tradeOrderFixture) post(t *testing.T, override map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"context_id": f.context.ID, "engine_instance_id": tradeEngine, "operation_class": "trade.order.commit",
		"seller_operating_binding_reference": map[string]any{"binding_id": tradeBindingID, "binding_version": 3},
		"native_configuration_digest":        tradeDigest, "currency": "UGX",
		"operation_reference": "trade/order.commit/cart_01/rev-2/attempt-1",
	}
	for k, v := range override {
		if v == nil {
			delete(body, k)
		} else {
			body[k] = v
		}
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/trade/v1/order-authority/resolve", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer verified-stub")
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	f.handler.ServeHTTP(out, req)
	return out
}

func tradeAuthorizedResolution(now time.Time) legalactor.Resolution {
	r := authorizedResolution(now)
	r.ValidUntil = ptrTime(now.Add(time.Hour))
	r.ResponsibleLegalEntityID = tradeLegalEntityID
	return r
}

func ptrTime(t time.Time) *time.Time { return &t }

func decodeTradeDecision(t *testing.T, out *httptest.ResponseRecorder) tradeOrderAuthorityDecision {
	t.Helper()
	if out.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
	}
	var d tradeOrderAuthorityDecision
	if err := json.Unmarshal(out.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTradeOrderAuthorityRoutesAreOptInNonproductionAndAllowlisted(t *testing.T) {
	for name, mutate := range map[string]func(*tradeOrderOptions){
		"disabled":     func(o *tradeOrderOptions) { o.enabled = false },
		"production":   func(o *tradeOrderOptions) { o.environment = "production" },
		"unset-env":    func(o *tradeOrderOptions) { o.environment = "" },
		"no-allowlist": func(o *tradeOrderOptions) { o.allowlist = []string{" ", ""} },
	} {
		t.Run(name, func(t *testing.T) {
			o := defaultTradeOrderOptions()
			mutate(&o)
			f := newTradeOrderFixture(t, &tradeOrderStoreFixture{bound: true}, o)
			if out := f.post(t, nil); out.Code != http.StatusNotFound {
				t.Fatalf("resolver must stay unmounted, got %d", out.Code)
			}
			for _, path := range []string{"/internal/trade/v1/seller-operating-bindings/attestations",
				"/v2/seller-operating-bindings", "/v2/seller-operating-bindings/" + tradeBindingID + "/versions/3/activate"} {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
				out := httptest.NewRecorder()
				f.handler.ServeHTTP(out, req)
				if out.Code != http.StatusNotFound {
					t.Fatalf("%s must stay unmounted, got %d", path, out.Code)
				}
			}
		})
	}
}

func TestTradeOrderAuthorityRejectsUntrustedCallers(t *testing.T) {
	for name, mutate := range map[string]func(*tradeOrderOptions){
		"not-allowlisted":                   func(o *tradeOrderOptions) { o.allowlist = []string{"someone-else"} },
		"not-registered":                    func(o *tradeOrderOptions) { o.registered = false },
		"context-scope-revoked-in-registry": func(o *tradeOrderOptions) { o.contextRevoked = true },
	} {
		t.Run(name, func(t *testing.T) {
			o := defaultTradeOrderOptions()
			mutate(&o)
			store := &tradeOrderStoreFixture{bound: true}
			if out := newTradeOrderFixture(t, store, o).post(t, nil); out.Code != http.StatusForbidden {
				t.Fatalf("status=%d", out.Code)
			}
			if len(store.requests)+len(store.recorded)+len(store.bindings)+store.lookups != 0 {
				t.Fatal("untrusted caller reached CP authority")
			}
		})
	}
	o := defaultTradeOrderOptions()
	o.owned = false
	store := &tradeOrderStoreFixture{bound: true}
	if out := newTradeOrderFixture(t, store, o).post(t, nil); out.Code != http.StatusNotFound || len(store.recorded) != 0 {
		t.Fatalf("another workload's context must not redeem, got %d", out.Code)
	}
}

// The caller can never select the seller, legal entity, scope or time.
func TestTradeOrderAuthorityRejectsCallerSelectedAuthority(t *testing.T) {
	for _, field := range []string{"tenant_id", "operating_organisation_id", "responsible_legal_entity_id",
		"mandate_id", "legal_actor_role", "activity", "market", "capability", "effective_at",
		"sales_channel_id", "cart_id", "binding_status"} {
		store := &tradeOrderStoreFixture{bound: true}
		f := newTradeOrderFixture(t, store, defaultTradeOrderOptions())
		if out := f.post(t, map[string]any{field: "forged"}); out.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", field, out.Code)
		}
		if len(store.recorded) != 0 || len(store.requests) != 0 {
			t.Fatalf("%s: forged request reached authority", field)
		}
	}
	for name, override := range map[string]map[string]any{
		"uncatalogued-class": {"operation_class": "trade.anything.do"},
		"missing-binding":    {"seller_operating_binding_reference": nil},
		"missing-currency":   {"currency": nil},
		"bad-digest":         {"native_configuration_digest": "ABC"},
	} {
		store := &tradeOrderStoreFixture{bound: true}
		if out := newTradeOrderFixture(t, store, defaultTradeOrderOptions()).post(t, override); out.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", name, out.Code)
		}
	}
}

func TestTradeOrderAuthorityAuthorizesUnderTheActiveBindingOnly(t *testing.T) {
	now := time.Now().UTC()
	store := &tradeOrderStoreFixture{bound: true, resolution: tradeAuthorizedResolution(now)}
	f := newTradeOrderFixture(t, store, defaultTradeOrderOptions())
	out := f.post(t, nil)
	d := decodeTradeDecision(t, out)
	if out.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatal("order authority could be cached")
	}
	if len(store.requests) != 1 {
		t.Fatalf("expected one resolver call, got %d", len(store.requests))
	}
	got := store.requests[0]
	if got.TenantID != f.context.TenantID || got.OperatingOrganisationID != f.context.OrganisationID ||
		got.Market != "UG" || got.Role != "SELLER_OF_RECORD" || got.Activity != d.Activity || got.Activity == "" ||
		got.Capability != "" {
		t.Fatalf("scope not derived from context and catalogue: %+v", got)
	}
	if store.bindings[0] != f.context.TenantID+"|"+tradeEngine {
		t.Fatalf("engine binding checked for the wrong tenant: %v", store.bindings)
	}
	if d.Outcome != "AUTHORIZED" || d.DecisionID == "" || d.ProviderPermissionsGranted ||
		d.ResponsibleLegalEntityID != tradeLegalEntityID || d.MandateEffectiveFrom == nil || d.ExpiresAt == nil ||
		d.LegalActorRole != "SELLER_OF_RECORD" || d.Currency != "UGX" ||
		d.BindingReference.BindingID != tradeBindingID || d.BindingReference.BindingVersion != 3 ||
		d.NativeConfigurationDigest != tradeDigest {
		t.Fatalf("incomplete AUTHORIZED decision: %+v", d)
	}
	if lease := d.ExpiresAt.Sub(d.EvaluatedAt); lease <= 0 || lease > 30*time.Second {
		t.Fatalf("lease %s exceeds 30 s", lease)
	}
	if len(store.recorded) != 1 || store.recorded[0].BindingVersion != 3 || store.recorded[0].Currency != "UGX" ||
		store.recorded[0].ClientID != tradeOrderClient || store.recorded[0].MandateID != d.MandateID {
		t.Fatalf("decision not recorded before disclosure: %+v", store.recorded)
	}
}

func TestTradeOrderAuthorityLeaseNeverOutlivesBindingOrContext(t *testing.T) {
	now := time.Now().UTC()
	store := &tradeOrderStoreFixture{bound: true, resolution: tradeAuthorizedResolution(now)}
	o := defaultTradeOrderOptions()
	o.contextExpiry = 5 * time.Second
	f := newTradeOrderFixture(t, store, o)
	if d := decodeTradeDecision(t, f.post(t, nil)); d.ExpiresAt == nil || d.ExpiresAt.After(*f.context.ExpiresAt) {
		t.Fatalf("lease outlives context: %+v", d.ExpiresAt)
	}
	store = &tradeOrderStoreFixture{bound: true, resolution: tradeAuthorizedResolution(now)}
	f = newTradeOrderFixture(t, store, defaultTradeOrderOptions())
	end := time.Now().UTC().Add(3 * time.Second)
	store.binding.EffectiveTo = &end
	if d := decodeTradeDecision(t, f.post(t, nil)); d.ExpiresAt == nil || d.ExpiresAt.After(end) {
		t.Fatalf("lease outlives binding: %+v", d.ExpiresAt)
	}
}

// A reserved (Trade-B/C) class is never AUTHORIZED and never reads a context,
// binding or mandate.
func TestTradeOrderAuthorityReservedClassesAreNotActivated(t *testing.T) {
	catalogue, err := tradeOrderCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	reserved := 0
	for class, op := range catalogue {
		if op.Activatable {
			continue
		}
		reserved++
		store := &tradeOrderStoreFixture{bound: true, resolution: tradeAuthorizedResolution(time.Now().UTC())}
		d := decodeTradeDecision(t, newTradeOrderFixture(t, store, defaultTradeOrderOptions()).
			post(t, map[string]any{"operation_class": class}))
		if d.Outcome != "OPERATION_NOT_ACTIVATED" || d.TenantID != "" || d.MandateID != "" ||
			len(store.requests)+len(store.bindings)+store.lookups != 0 || len(store.recorded) != 1 {
			t.Fatalf("%s: reserved class reached authority: %+v", class, d)
		}
	}
	if reserved != 11 {
		t.Fatalf("expected eleven reserved classes, got %d", reserved)
	}
}

func TestTradeOrderAuthorityBindingDenials(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate   func(*postgres.SellerOperatingBinding)
		override map[string]any
		missing  bool
		want     string
	}{
		"not-found":      {missing: true, want: "BINDING_NOT_ACTIVE"},
		"pending":        {mutate: func(b *postgres.SellerOperatingBinding) { b.Status = "PENDING_APPROVAL" }, want: "BINDING_NOT_ACTIVE"},
		"approved":       {mutate: func(b *postgres.SellerOperatingBinding) { b.Status = "APPROVED" }, want: "BINDING_NOT_ACTIVE"},
		"suspended":      {mutate: func(b *postgres.SellerOperatingBinding) { b.Status = "SUSPENDED" }, want: "BINDING_NOT_ACTIVE"},
		"revoked":        {mutate: func(b *postgres.SellerOperatingBinding) { b.Status = "REVOKED" }, want: "BINDING_NOT_ACTIVE"},
		"superseded":     {mutate: func(b *postgres.SellerOperatingBinding) { b.Status = "SUPERSEDED" }, want: "BINDING_NOT_ACTIVE"},
		"not-yet":        {mutate: func(b *postgres.SellerOperatingBinding) { b.EffectiveFrom = time.Now().Add(time.Hour) }, want: "BINDING_NOT_ACTIVE"},
		"ended":          {mutate: func(b *postgres.SellerOperatingBinding) { b.EffectiveTo = ptrTime(time.Now().Add(-time.Second)) }, want: "BINDING_NOT_ACTIVE"},
		"other-version":  {override: map[string]any{"seller_operating_binding_reference": map[string]any{"binding_id": tradeBindingID, "binding_version": 2}}, want: "BINDING_NOT_ACTIVE"},
		"other-tenant":   {mutate: func(b *postgres.SellerOperatingBinding) { b.TenantID = "tn_other" }, want: "BINDING_SCOPE_MISMATCH"},
		"other-org":      {mutate: func(b *postgres.SellerOperatingBinding) { b.OperatingOrganisationID = domain.NewUUIDv7() }, want: "BINDING_SCOPE_MISMATCH"},
		"other-market":   {mutate: func(b *postgres.SellerOperatingBinding) { b.Market = "KE" }, want: "BINDING_SCOPE_MISMATCH"},
		"other-engine":   {mutate: func(b *postgres.SellerOperatingBinding) { b.TradeEngineInstanceID = "ei_tradeother" }, want: "BINDING_SCOPE_MISMATCH"},
		"other-currency": {override: map[string]any{"currency": "KES"}, want: "BINDING_SCOPE_MISMATCH"},
		"digest":         {override: map[string]any{"native_configuration_digest": strings.Repeat("cd", 32)}, want: "NATIVE_CONFIGURATION_MISMATCH"},
	} {
		t.Run(name, func(t *testing.T) {
			store := &tradeOrderStoreFixture{bound: true, resolution: tradeAuthorizedResolution(time.Now().UTC())}
			f := newTradeOrderFixture(t, store, defaultTradeOrderOptions())
			if tc.missing {
				store.binding = nil
			}
			if tc.mutate != nil {
				tc.mutate(store.binding)
			}
			d := decodeTradeDecision(t, f.post(t, tc.override))
			if d.Outcome != tc.want || len(store.requests) != 0 || d.MandateID != "" || d.ExpiresAt != nil ||
				len(store.recorded) != 1 || store.recorded[0].Outcome != tc.want {
				t.Fatalf("want %s before mandate resolution, got %+v", tc.want, d)
			}
		})
	}
}

func TestTradeOrderAuthorityMandateMustBeTheBindingsLegalEntity(t *testing.T) {
	resolution := tradeAuthorizedResolution(time.Now().UTC())
	resolution.ResponsibleLegalEntityID = "SYNTHETIC-LE-OTHER"
	store := &tradeOrderStoreFixture{bound: true, resolution: resolution}
	d := decodeTradeDecision(t, newTradeOrderFixture(t, store, defaultTradeOrderOptions()).post(t, nil))
	if d.Outcome != "NO_APPLICABLE_MANDATE" || d.MandateID != "" || d.ResponsibleLegalEntityID != "" {
		t.Fatalf("foreign legal entity's mandate authorised the binding: %+v", d)
	}
}

func TestTradeOrderAuthorityDenialsDiscloseNoActor(t *testing.T) {
	now := time.Now().UTC()
	for _, outcome := range []legalactor.Outcome{legalactor.NoApplicableMandate, legalactor.Ambiguous,
		legalactor.RevokedOrExpired, legalactor.ActorNotVerified} {
		store := &tradeOrderStoreFixture{bound: true, resolution: legalactor.Resolution{
			Outcome: outcome, EvaluatedAt: now, PolicyReference: legalactor.PolicyReference}}
		d := decodeTradeDecision(t, newTradeOrderFixture(t, store, defaultTradeOrderOptions()).post(t, nil))
		if d.Outcome != string(outcome) || d.MandateID != "" || d.ResponsibleLegalEntityID != "" ||
			d.ExpiresAt != nil || len(d.EvidenceReferences) != 0 || len(store.recorded) != 1 {
			t.Fatalf("%s leaked actor or was not recorded: %+v", outcome, d)
		}
	}
	broken := tradeAuthorizedResolution(now)
	broken.MandateEffectiveFrom = nil
	store := &tradeOrderStoreFixture{bound: true, resolution: broken}
	if d := decodeTradeDecision(t, newTradeOrderFixture(t, store, defaultTradeOrderOptions()).post(t, nil)); d.Outcome != "ACTOR_NOT_VERIFIED" || d.MandateID != "" {
		t.Fatalf("incomplete resolution must deny: %+v", d)
	}
}

func TestTradeOrderAuthorityUnboundEngineAndUnusableContext(t *testing.T) {
	store := &tradeOrderStoreFixture{bound: false, resolution: tradeAuthorizedResolution(time.Now().UTC())}
	d := decodeTradeDecision(t, newTradeOrderFixture(t, store, defaultTradeOrderOptions()).post(t, nil))
	if d.Outcome != "ENGINE_INSTANCE_NOT_BOUND" || store.lookups != 0 || len(store.requests) != 0 {
		t.Fatalf("unbound engine instance must deny before binding and mandate: %+v", d)
	}
	o := defaultTradeOrderOptions()
	o.tenantState = "suspended"
	store = &tradeOrderStoreFixture{bound: true, resolution: tradeAuthorizedResolution(time.Now().UTC())}
	d = decodeTradeDecision(t, newTradeOrderFixture(t, store, o).post(t, nil))
	if d.Outcome != "CONTEXT_NOT_USABLE" || d.TenantID != "" || len(store.bindings) != 0 || len(store.recorded) != 1 {
		t.Fatalf("inactive tenant must deny without scope: %+v", d)
	}
}

func TestTradeOrderAuthorityFailsClosed(t *testing.T) {
	for name, store := range map[string]*tradeOrderStoreFixture{
		"unrecorded":   {bound: true, resolution: tradeAuthorizedResolution(time.Now().UTC()), recordErr: errors.New("db down")},
		"engine-check": {boundErr: errors.New("db down")},
		"binding-read": {bound: true, bindingErr: errors.New("db down")},
	} {
		out := newTradeOrderFixture(t, store, defaultTradeOrderOptions()).post(t, nil)
		if out.Code != http.StatusServiceUnavailable || bytes.Contains(out.Body.Bytes(), []byte(tradeLegalEntityID)) {
			t.Fatalf("%s: must be unavailable without disclosure: %d %s", name, out.Code, out.Body.String())
		}
	}
}

// CP's loader and the Shared schema enum agree on the closed catalogue; only
// the three Trade-A classes are activatable.
func TestTradeOrderCatalogueMatchesSchemaEnum(t *testing.T) {
	catalogue, err := tradeOrderCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := contracts.ReadEmbedded("trade/v2/order-authority.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	mustNoError(t, json.Unmarshal(raw, &schema))
	enum := schema.Defs["operationClass"].Enum
	if len(enum) != len(catalogue) || len(catalogue) != 14 {
		t.Fatalf("catalogue %d vs schema enum %d", len(catalogue), len(enum))
	}
	activatable := []string{}
	for _, class := range enum {
		op, ok := catalogue[class]
		if !ok {
			t.Fatalf("%s in schema but not catalogue", class)
		}
		if op.Activatable {
			activatable = append(activatable, class)
		}
	}
	want := map[string]bool{"trade.order.commit": true, "trade.order.create": true, "trade.draft-order.finalize": true}
	if len(activatable) != len(want) {
		t.Fatalf("unexpected activatable classes: %v", activatable)
	}
	for _, class := range activatable {
		if !want[class] || catalogue[class].Increment != "TRADE_A" {
			t.Fatalf("%s must not be activatable in Trade-A", class)
		}
	}
}

// --- Seller Operating Binding lifecycle routes ---

func (f tradeOrderFixture) attest(t *testing.T, override map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"context_id": f.context.ID, "engine_instance_id": tradeEngine,
		"native_configuration_reference": "trade/native-config/store-01", "native_configuration_digest": tradeDigest,
		"currency_scope": []string{"UGX"}, "attested_at": time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)}
	for k, v := range override {
		body[k] = v
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/trade/v1/seller-operating-bindings/attestations", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer verified-stub")
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	f.handler.ServeHTTP(out, req)
	return out
}

func TestSellerBindingAttestationTakesScopeFromTheRuntimeContext(t *testing.T) {
	store := &tradeOrderStoreFixture{}
	f := newTradeOrderFixture(t, store, defaultTradeOrderOptions())
	out := f.attest(t, nil)
	if out.Code != http.StatusCreated || len(store.attestations) != 1 {
		t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
	}
	a := store.attestations[0]
	if a.TenantID != f.context.TenantID || a.OperatingOrganisationID != f.context.OrganisationID || a.Market != "UG" ||
		a.ClientID != tradeOrderClient || a.ContextID != f.context.ID {
		t.Fatalf("attestation scope not from context: %+v", a)
	}
	for _, field := range []string{"tenant_id", "operating_organisation_id", "market", "responsible_legal_entity_id"} {
		store := &tradeOrderStoreFixture{}
		if out := newTradeOrderFixture(t, store, defaultTradeOrderOptions()).attest(t, map[string]any{field: "forged"}); out.Code != http.StatusBadRequest || len(store.attestations) != 0 {
			t.Fatalf("%s: forged attestation accepted: %d", field, out.Code)
		}
	}
	o := defaultTradeOrderOptions()
	o.registered = false
	store = &tradeOrderStoreFixture{}
	if out := newTradeOrderFixture(t, store, o).attest(t, nil); out.Code != http.StatusForbidden || len(store.attestations) != 0 {
		t.Fatalf("unregistered workload attested: %d", out.Code)
	}
	store = &tradeOrderStoreFixture{}
	future := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	if out := newTradeOrderFixture(t, store, defaultTradeOrderOptions()).attest(t, map[string]any{"attested_at": future}); out.Code != http.StatusUnprocessableEntity {
		t.Fatalf("future attestation accepted: %d", out.Code)
	}
}

type sellerBindingHumanOptions struct {
	scope      string
	permission string
	org        string
	source     administration.Source
	acr        string
}

func newSellerBindingHumanFixture(t *testing.T, store *tradeOrderStoreFixture, org string, o sellerBindingHumanOptions) (http.Handler, string) {
	t.Helper()
	now := time.Now().UTC()
	repo := repository.NewInMemoryRepository()
	human := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, repo.CreateIdentity(context.Background(), human))
	mustNoError(t, repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
		PrincipalID: human.ID, Issuer: testRealm, Subject: "operator", Status: "ACTIVE"}))
	if o.org == "" {
		o.org = org
	}
	if o.source == "" {
		o.source = administration.SourceDirect
	}
	if o.acr == "" {
		o.acr = "2"
	}
	grant := administration.Grant{GrantID: domain.NewUUIDv7(), PrincipalID: human.ID, Permission: o.permission,
		Scope:     administration.Scope{Level: administration.LevelOrganisation, OrganisationID: o.org},
		RiskClass: administration.RiskHigh, Status: administration.StatusActive, ValidFrom: now.Add(-time.Minute),
		Source: o.source, GrantType: administration.TypeStanding}
	p := auth.Principal{Issuer: testRealm, Subject: "operator", ActorType: "human", TokenID: "token-seller-binding",
		ExpiresAt: now.Add(time.Minute), Scopes: map[string]struct{}{o.scope: {}},
		Assurance: auth.Assurance{ACR: o.acr, AuthenticatedAt: now}}
	h := New(Dependencies{Store: &fakeStore{}, Environment: "integration", TradeOrderAuthorityEnabled: true,
		TradeOrderAuthority: store, SellerOperatingBindings: store, TradeOrderAuthorityClientIDs: []string{tradeOrderClient},
		WorkloadRegistry: tradeOrderRegistry{allowed: true}, Contexts: repo, Identities: repo,
		AdminVerifier:        fakeVerifier{principal: p},
		AdministrativeGrants: governanceGrantReader{grants: map[string][]administration.Grant{human.ID: {grant}}}})
	return h, human.ID
}

func sellerBindingRequest(path string, body map[string]any) *http.Request {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer fixture")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "seller-binding-cmd-0001")
	return req
}

func TestSellerBindingProposalNeedsTheExactOrganisationGrant(t *testing.T) {
	org := domain.NewUUIDv7()
	attestation := postgres.TradeNativeConfigurationAttestation{AttestationID: "5a6b7c8d-9e0f-4a1b-8c2d-3e4f5a6b7c8d",
		OperatingOrganisationID: org}
	body := map[string]any{"attestation_id": attestation.AttestationID, "responsible_legal_entity_id": tradeLegalEntityID,
		"effective_from": time.Now().UTC().Format(time.RFC3339), "evidence_references": []string{"synthetic:binding/01"}}
	for name, tc := range map[string]struct {
		o    sellerBindingHumanOptions
		want int
	}{
		"allowed":     {sellerBindingHumanOptions{scope: "seller-operating-binding:propose", permission: "organisation.seller-operating-binding.propose"}, http.StatusAccepted},
		"sibling-org": {sellerBindingHumanOptions{scope: "seller-operating-binding:propose", permission: "organisation.seller-operating-binding.propose", org: domain.NewUUIDv7()}, http.StatusNotFound},
		"delegated":   {sellerBindingHumanOptions{scope: "seller-operating-binding:propose", permission: "organisation.seller-operating-binding.propose", source: administration.SourceDelegation}, http.StatusNotFound},
		"wrong-perm":  {sellerBindingHumanOptions{scope: "seller-operating-binding:propose", permission: "organisation.seller-operating-binding.approve"}, http.StatusNotFound},
		"weak-acr":    {sellerBindingHumanOptions{scope: "seller-operating-binding:propose", permission: "organisation.seller-operating-binding.propose", acr: "1"}, http.StatusNotFound},
		"wrong-scope": {sellerBindingHumanOptions{scope: "seller-operating-binding:approve", permission: "organisation.seller-operating-binding.propose"}, http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			store := &tradeOrderStoreFixture{attestation: &attestation, binding: &postgres.SellerOperatingBinding{
				BindingID: tradeBindingID, BindingVersion: 1, TenantID: "tn_tradeorder", OperatingOrganisationID: org,
				ResponsibleLegalEntityID: tradeLegalEntityID, Market: "UG", CurrencyScope: []string{"UGX"},
				TradeEngineInstanceID: tradeEngine, NativeConfigurationReference: "trade/native-config/store-01",
				NativeConfigurationDigest: tradeDigest, AttestationID: attestation.AttestationID,
				EffectiveFrom: time.Now().UTC(), ProposalID: domain.NewUUIDv7(),
				EvidenceReferences: []string{"synthetic:binding/01"}, Revision: 1, UpdatedAt: time.Now().UTC()}}
			h, humanID := newSellerBindingHumanFixture(t, store, org, tc.o)
			out := httptest.NewRecorder()
			h.ServeHTTP(out, sellerBindingRequest("/v2/seller-operating-bindings", body))
			if out.Code != tc.want {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
			if tc.want == http.StatusAccepted {
				if len(store.proposed) != 1 || store.actors[0] != humanID {
					t.Fatalf("maker not the canonical human: %+v", store.actors)
				}
			} else if len(store.proposed) != 0 {
				t.Fatal("denied proposal reached the registry")
			}
		})
	}
}

func TestSellerBindingTransitionPathMustMatchCommand(t *testing.T) {
	org := domain.NewUUIDv7()
	binding := activeTradeBinding(domain.Context{TenantID: "tn_tradeorder", OrganisationID: org, CountryCode: "UG"}, time.Now().UTC())
	binding.Status = "PENDING_APPROVAL"
	body := func(id string, version int, transition string) map[string]any {
		return map[string]any{"binding_reference": map[string]any{"binding_id": id, "binding_version": version},
			"transition": transition, "expected_status": "PENDING_APPROVAL", "reason": "checker review complete",
			"evidence_references": []string{"synthetic:approval/01"}}
	}
	path := "/v2/seller-operating-bindings/" + tradeBindingID + "/versions/3/approve"
	for name, tc := range map[string]struct {
		body map[string]any
		want int
	}{
		"allowed":         {body(tradeBindingID, 3, "APPROVE"), http.StatusOK},
		"other-version":   {body(tradeBindingID, 2, "APPROVE"), http.StatusBadRequest},
		"other-binding":   {body("9f1c2b3a-9d8e-4a7b-b6c5-0d1e2f3a4b5c", 3, "APPROVE"), http.StatusBadRequest},
		"other-action":    {body(tradeBindingID, 3, "ACTIVATE"), http.StatusBadRequest},
		"activate-scoped": {body(tradeBindingID, 3, "APPROVE"), http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			b := *binding
			store := &tradeOrderStoreFixture{binding: &b}
			o := sellerBindingHumanOptions{scope: "seller-operating-binding:approve", permission: "organisation.seller-operating-binding.approve"}
			if name == "activate-scoped" {
				o.permission = "organisation.seller-operating-binding.activate"
			}
			h, _ := newSellerBindingHumanFixture(t, store, org, o)
			out := httptest.NewRecorder()
			h.ServeHTTP(out, sellerBindingRequest(path, tc.body))
			if out.Code != tc.want {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
			if (tc.want == http.StatusOK) != (len(store.transitions) == 1) {
				t.Fatalf("transition reached registry: %d", len(store.transitions))
			}
		})
	}
	// Without the grant, an existing binding answers exactly like an unknown one.
	for _, b := range []*postgres.SellerOperatingBinding{binding, nil} {
		store := &tradeOrderStoreFixture{binding: b}
		if b == nil {
			store.bindingErr = postgres.ErrSellerBindingNotFound
		}
		h, _ := newSellerBindingHumanFixture(t, store, org, sellerBindingHumanOptions{scope: "seller-operating-binding:approve",
			permission: "organisation.seller-operating-binding.approve", org: domain.NewUUIDv7()})
		out := httptest.NewRecorder()
		h.ServeHTTP(out, sellerBindingRequest(path, body(tradeBindingID, 3, "APPROVE")))
		if out.Code != http.StatusNotFound || !strings.Contains(out.Body.String(), "SELLER_BINDING_NOT_FOUND") {
			t.Fatalf("existence disclosed: %d %s", out.Code, out.Body.String())
		}
	}
	// The SQL guard's separation-of-duties refusal surfaces as 409.
	b := *binding
	store := &tradeOrderStoreFixture{binding: &b, writeErr: postgres.ErrSellerBindingConflict}
	h, _ := newSellerBindingHumanFixture(t, store, org, sellerBindingHumanOptions{scope: "seller-operating-binding:approve",
		permission: "organisation.seller-operating-binding.approve"})
	out := httptest.NewRecorder()
	h.ServeHTTP(out, sellerBindingRequest(path, body(tradeBindingID, 3, "APPROVE")))
	if out.Code != http.StatusConflict {
		t.Fatalf("guard refusal status=%d", out.Code)
	}
}
