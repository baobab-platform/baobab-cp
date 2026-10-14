package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const erpPostingClient = "baobab-erp-legal-actor-assessor-staging"

type erpPostingRegistry struct{ allowed bool }

func (r erpPostingRegistry) IsActive(string) bool { return r.allowed }
func (r erpPostingRegistry) AllowsScope(clientID, scope string) bool {
	return r.allowed && clientID == erpPostingClient && scope == erpPostingAuthorityScope
}

type erpPostingStoreFixture struct {
	bound      bool
	boundErr   error
	resolution legalactor.Resolution
	recordErr  error
	requests   []legalactor.Request
	bindings   []string
	recorded   []postgres.ErpPostingAuthorityDecision
}

func (f *erpPostingStoreFixture) ResolveOperatingLegalActor(_ context.Context, r legalactor.Request) (legalactor.Resolution, error) {
	f.requests = append(f.requests, r)
	return f.resolution, nil
}

func (f *erpPostingStoreFixture) ErpEngineInstanceBound(_ context.Context, tenant, instance string, _ time.Time) (bool, error) {
	f.bindings = append(f.bindings, tenant+"|"+instance)
	return f.bound, f.boundErr
}

func (f *erpPostingStoreFixture) RecordErpPostingAuthorityDecision(_ context.Context, d postgres.ErpPostingAuthorityDecision) (string, error) {
	if f.recordErr != nil {
		return "", f.recordErr
	}
	f.recorded = append(f.recorded, d)
	return "a3e9c7d1-5b2f-4e6a-8c1d-9f0b7e3a2c54", nil
}

type erpPostingFixture struct {
	handler http.Handler
	store   *erpPostingStoreFixture
	context domain.Context
}

type erpPostingOptions struct {
	allowlist     []string
	registered    bool
	tenantState   string
	contextExpiry time.Duration
	owned         bool
	environment   string
	enabled       bool
}

func defaultErpPostingOptions() erpPostingOptions {
	return erpPostingOptions{allowlist: []string{erpPostingClient}, registered: true, tenantState: "active",
		contextExpiry: time.Minute, owned: true, environment: "integration", enabled: true}
}

func newErpPostingFixture(t *testing.T, store *erpPostingStoreFixture, o erpPostingOptions) erpPostingFixture {
	t.Helper()
	repo := repository.NewInMemoryRepository()
	principal := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
	if err := repo.CreateIdentity(context.Background(), principal); err != nil {
		t.Fatal(err)
	}
	issuer, subject := "https://iam.example.test", erpPostingClient
	if err := repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID, Issuer: issuer,
		Subject: subject, Status: "ACTIVE"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	expiry := now.Add(o.contextExpiry)
	ctx := domain.Context{ID: domain.NewUUIDv7(), PrincipalID: principal.ID,
		TenantID: "tn_erpposting", OrganisationID: domain.NewUUIDv7(), CountryCode: "UG",
		MarketID: "market-ug", CorrelationID: domain.NewUUIDv7(), ResolvedAt: now.Add(-time.Second), ExpiresAt: &expiry}
	if !o.owned {
		ctx.PrincipalID = domain.NewPrincipalID()
	}
	repo.Contexts[ctx.ID] = ctx
	handler := New(Dependencies{
		Store:       &fakeStore{tenant: domain.Tenant{TenantID: ctx.TenantID, ObservedState: o.tenantState}},
		Environment: o.environment, ErpPostingAuthorityEnabled: o.enabled,
		ErpPostingAuthority: store, ErpPostingAuthorityClientIDs: o.allowlist,
		WorkloadVerifier: fakeVerifier{principal: auth.Principal{
			Issuer: issuer, Subject: subject, ActorType: "workload", ClientID: erpPostingClient,
			TenantID: ctx.TenantID, TokenID: "token-erp-posting",
			Scopes: map[string]struct{}{"context:resolve": {}, erpPostingAuthorityScope: {}}}},
		WorkloadRegistry: erpPostingRegistry{allowed: o.registered},
		Contexts:         repo, Identities: repo,
	})
	return erpPostingFixture{handler: handler, store: store, context: ctx}
}

func (f erpPostingFixture) post(t *testing.T, override map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"context_id": f.context.ID, "engine_instance_id": "ei_erpstaging01",
		"operation_class": "erp.gl-journal.post",
		"native_placement_reference": map[string]any{
			"placement_id": "0b6d1f9a-3c2e-4a8b-9d7f-5e4c3b2a1908", "placement_version": 2},
		"operation_reference": "erp/gl-journal/record-1000234/complete/trx-7f3a",
	}
	for k, v := range override {
		if v == nil {
			delete(body, k)
		} else {
			body[k] = v
		}
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/erp/v1/posting-authority/resolve", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer verified-stub")
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	f.handler.ServeHTTP(out, req)
	return out
}

func authorizedResolution(now time.Time) legalactor.Resolution {
	until := now.Add(30 * time.Second)
	from := now.Add(-24 * time.Hour)
	return legalactor.Resolution{
		Outcome: legalactor.Authorized, EvaluatedAt: now, PolicyReference: legalactor.PolicyReference,
		MandateID: "7d2b9e4f-6a1c-4f3e-b8d5-0c9a8e7f6d21", ResponsibleLegalEntityID: "SYNTHETIC-LE-01",
		EvidenceReferences: []string{"synthetic:mandate-evidence/01"}, ValidUntil: &until,
		SupersedesMandateID: "1e8f7d6c-5b4a-4392-8170-6f5e4d3c2b1a", MandateEffectiveFrom: &from,
	}
}

func decodeErpDecision(t *testing.T, out *httptest.ResponseRecorder) erpPostingAuthorityDecision {
	t.Helper()
	if out.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
	}
	var d erpPostingAuthorityDecision
	if err := json.Unmarshal(out.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestErpPostingAuthorityRouteIsOptInNonproductionAndAllowlisted(t *testing.T) {
	for name, mutate := range map[string]func(*erpPostingOptions){
		"disabled":     func(o *erpPostingOptions) { o.enabled = false },
		"production":   func(o *erpPostingOptions) { o.environment = "production" },
		"unset-env":    func(o *erpPostingOptions) { o.environment = "" },
		"no-allowlist": func(o *erpPostingOptions) { o.allowlist = []string{" ", ""} },
	} {
		t.Run(name, func(t *testing.T) {
			o := defaultErpPostingOptions()
			mutate(&o)
			f := newErpPostingFixture(t, &erpPostingStoreFixture{bound: true}, o)
			if out := f.post(t, nil); out.Code != http.StatusNotFound {
				t.Fatalf("route must stay unmounted, got %d", out.Code)
			}
		})
	}
}

func TestErpPostingAuthorityRejectsUntrustedCallers(t *testing.T) {
	for name, mutate := range map[string]func(*erpPostingOptions){
		"not-allowlisted": func(o *erpPostingOptions) { o.allowlist = []string{"someone-else"} },
		"not-registered":  func(o *erpPostingOptions) { o.registered = false },
	} {
		t.Run(name, func(t *testing.T) {
			o := defaultErpPostingOptions()
			mutate(&o)
			store := &erpPostingStoreFixture{bound: true}
			f := newErpPostingFixture(t, store, o)
			if out := f.post(t, nil); out.Code != http.StatusForbidden {
				t.Fatalf("status=%d", out.Code)
			}
			if len(store.requests)+len(store.recorded)+len(store.bindings) != 0 {
				t.Fatal("untrusted caller reached CP authority")
			}
		})
	}
	o := defaultErpPostingOptions()
	o.owned = false
	store := &erpPostingStoreFixture{bound: true}
	if out := newErpPostingFixture(t, store, o).post(t, nil); out.Code != http.StatusNotFound || len(store.recorded) != 0 {
		t.Fatalf("another workload's context must not redeem, got %d", out.Code)
	}
}

// LA-T23: the caller can never select the legal actor, scope or time.
func TestErpPostingAuthorityRejectsCallerSelectedAuthority(t *testing.T) {
	for _, field := range []string{"tenant_id", "operating_organisation_id", "responsible_legal_entity_id",
		"legal_entity_id", "mandate_id", "role", "legal_actor_role", "activity", "market",
		"capability", "effective_at", "ad_client_id"} {
		store := &erpPostingStoreFixture{bound: true}
		f := newErpPostingFixture(t, store, defaultErpPostingOptions())
		if out := f.post(t, map[string]any{field: "forged"}); out.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", field, out.Code)
		}
		if len(store.recorded) != 0 || len(store.requests) != 0 {
			t.Fatalf("%s: forged request reached authority", field)
		}
	}
	store := &erpPostingStoreFixture{bound: true}
	f := newErpPostingFixture(t, store, defaultErpPostingOptions())
	if out := f.post(t, map[string]any{"operation_class": "erp.anything.post"}); out.Code != http.StatusBadRequest {
		t.Fatalf("uncatalogued class: status=%d", out.Code)
	}
}

func TestErpPostingAuthorityAuthorizesFromCatalogueAndContextOnly(t *testing.T) {
	now := time.Now().UTC()
	store := &erpPostingStoreFixture{bound: true, resolution: authorizedResolution(now)}
	f := newErpPostingFixture(t, store, defaultErpPostingOptions())
	out := f.post(t, nil)
	d := decodeErpDecision(t, out)
	if out.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatal("posting authority could be cached")
	}
	if len(store.requests) != 1 {
		t.Fatalf("expected one resolver call, got %d", len(store.requests))
	}
	got := store.requests[0]
	if got.TenantID != f.context.TenantID || got.OperatingOrganisationID != f.context.OrganisationID ||
		got.Market != "UG" || got.Role != "ACCOUNTING_ENTITY" || got.Activity != "GENERAL_LEDGER_POSTING" ||
		got.Capability != "" {
		t.Fatalf("scope not derived from context and catalogue: %+v", got)
	}
	if store.bindings[0] != f.context.TenantID+"|ei_erpstaging01" {
		t.Fatalf("engine binding checked for the wrong tenant: %v", store.bindings)
	}
	if d.Outcome != "AUTHORIZED" || d.DecisionID == "" || d.ProviderPermissionsGranted ||
		d.MandateID == "" || d.SupersedesMandateID == "" || d.ResponsibleLegalEntityID != "SYNTHETIC-LE-01" ||
		d.MandateEffectiveFrom == nil || d.ExpiresAt == nil || d.LegalActorRole != "ACCOUNTING_ENTITY" {
		t.Fatalf("incomplete AUTHORIZED decision: %+v", d)
	}
	if lease := d.ExpiresAt.Sub(d.EvaluatedAt); lease <= 0 || lease > 30*time.Second {
		t.Fatalf("lease %s exceeds 30 s", lease)
	}
	if len(store.recorded) != 1 || store.recorded[0].MandateID != d.MandateID ||
		store.recorded[0].PlacementVersion != 2 || store.recorded[0].ClientID != erpPostingClient {
		t.Fatalf("decision not recorded before disclosure: %+v", store.recorded)
	}
}

// A mandate's evidence list is uncapped; the decision projects it whole.
func TestErpPostingAuthorityProjectsTheWholeEvidenceList(t *testing.T) {
	resolution := authorizedResolution(time.Now().UTC())
	resolution.EvidenceReferences = nil
	for i := 0; i < 25; i++ {
		resolution.EvidenceReferences = append(resolution.EvidenceReferences, fmt.Sprintf("synthetic:evidence/%d", i))
	}
	store := &erpPostingStoreFixture{bound: true, resolution: resolution}
	d := decodeErpDecision(t, newErpPostingFixture(t, store, defaultErpPostingOptions()).post(t, nil))
	if d.Outcome != "AUTHORIZED" || len(d.EvidenceReferences) != 25 || len(store.recorded[0].EvidenceReferences) != 25 {
		t.Fatalf("evidence was not carried whole: %+v", d)
	}
}

func TestErpPostingAuthorityLeaseNeverOutlivesContext(t *testing.T) {
	now := time.Now().UTC()
	o := defaultErpPostingOptions()
	o.contextExpiry = 5 * time.Second
	store := &erpPostingStoreFixture{bound: true, resolution: authorizedResolution(now)}
	f := newErpPostingFixture(t, store, o)
	d := decodeErpDecision(t, f.post(t, nil))
	if d.ExpiresAt == nil || d.ExpiresAt.After(*f.context.ExpiresAt) {
		t.Fatalf("lease outlives context: %v > %v", d.ExpiresAt, f.context.ExpiresAt)
	}
}

func TestErpPostingAuthorityDenialsDiscloseNoActor(t *testing.T) {
	now := time.Now().UTC()
	for _, outcome := range []legalactor.Outcome{legalactor.NoApplicableMandate, legalactor.Ambiguous,
		legalactor.RevokedOrExpired, legalactor.ActorNotVerified} {
		store := &erpPostingStoreFixture{bound: true, resolution: legalactor.Resolution{
			Outcome: outcome, EvaluatedAt: now, PolicyReference: legalactor.PolicyReference}}
		d := decodeErpDecision(t, newErpPostingFixture(t, store, defaultErpPostingOptions()).post(t, nil))
		if d.Outcome != string(outcome) || d.MandateID != "" || d.ResponsibleLegalEntityID != "" ||
			d.ExpiresAt != nil || len(d.EvidenceReferences) != 0 || len(store.recorded) != 1 {
			t.Fatalf("%s leaked actor or was not recorded: %+v", outcome, d)
		}
	}
	// An AUTHORIZED resolution missing lineage is not trusted.
	broken := authorizedResolution(now)
	broken.MandateEffectiveFrom = nil
	store := &erpPostingStoreFixture{bound: true, resolution: broken}
	d := decodeErpDecision(t, newErpPostingFixture(t, store, defaultErpPostingOptions()).post(t, nil))
	if d.Outcome != "ACTOR_NOT_VERIFIED" || d.MandateID != "" {
		t.Fatalf("incomplete resolution must deny: %+v", d)
	}
}

func TestErpPostingAuthorityUnboundEngineInstanceNeverResolvesMandate(t *testing.T) {
	store := &erpPostingStoreFixture{bound: false, resolution: authorizedResolution(time.Now().UTC())}
	d := decodeErpDecision(t, newErpPostingFixture(t, store, defaultErpPostingOptions()).post(t, nil))
	if d.Outcome != "ENGINE_INSTANCE_NOT_BOUND" || len(store.requests) != 0 || d.MandateID != "" {
		t.Fatalf("unbound engine instance must deny before mandate resolution: %+v", d)
	}
}

func TestErpPostingAuthorityInactiveTenantIsNotUsable(t *testing.T) {
	o := defaultErpPostingOptions()
	o.tenantState = "suspended"
	store := &erpPostingStoreFixture{bound: true, resolution: authorizedResolution(time.Now().UTC())}
	d := decodeErpDecision(t, newErpPostingFixture(t, store, o).post(t, nil))
	if d.Outcome != "CONTEXT_NOT_USABLE" || len(store.requests) != 0 || len(store.bindings) != 0 ||
		d.TenantID != "" || len(store.recorded) != 1 {
		t.Fatalf("inactive tenant must deny without scope: %+v", d)
	}
}

func TestErpPostingAuthorityFailsClosedWhenUnrecordedOrUnverifiable(t *testing.T) {
	store := &erpPostingStoreFixture{bound: true, resolution: authorizedResolution(time.Now().UTC()),
		recordErr: errors.New("db down")}
	out := newErpPostingFixture(t, store, defaultErpPostingOptions()).post(t, nil)
	if out.Code != http.StatusServiceUnavailable || bytes.Contains(out.Body.Bytes(), []byte("SYNTHETIC-LE-01")) {
		t.Fatalf("unrecorded decision disclosed: %d %s", out.Code, out.Body.String())
	}
	store = &erpPostingStoreFixture{boundErr: errors.New("db down")}
	if out := newErpPostingFixture(t, store, defaultErpPostingOptions()).post(t, nil); out.Code != http.StatusServiceUnavailable {
		t.Fatalf("binding failure must be unavailable, got %d", out.Code)
	}
}

func TestErpPostingCatalogueMatchesSchemaEnum(t *testing.T) {
	catalogue, err := erpPostingCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	for class, op := range catalogue {
		f := newErpPostingFixture(t, &erpPostingStoreFixture{bound: false}, defaultErpPostingOptions())
		if out := f.post(t, map[string]any{"operation_class": class}); out.Code != http.StatusOK {
			t.Fatalf("%s: catalogued class rejected by schema: %d", class, out.Code)
		}
		if !legalactorKnownRole(op.Role) {
			t.Fatalf("%s: unknown role %s", class, op.Role)
		}
	}
	if len(catalogue) != 6 {
		t.Fatalf("expected six catalogued operations, got %d", len(catalogue))
	}
}

func legalactorKnownRole(role string) bool {
	switch role {
	case "ACCOUNTING_ENTITY", "INVOICE_ISSUER", "CONTRACTING_PARTY":
		return true
	}
	return false
}
