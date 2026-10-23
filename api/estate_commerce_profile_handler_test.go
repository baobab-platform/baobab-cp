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
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// Synthetic only: no real estate, Tenant, Market or workload. This proves CP's
// boundary, not connected acceptance or a registered estate profile.
const (
	commerceProfileID = "9a1b2c3d-4e5f-4a6b-8c7d-0e1f2a3b4c5d"
	commerceMarket    = "zuribeans_ug"
	commerceCaller    = "synthetic-trade-workload"
)

type commerceRegistry struct{ allowed, scopeless bool }

func (r commerceRegistry) IsActive(string) bool { return r.allowed }
func (r commerceRegistry) AllowsScope(clientID, scope string) bool {
	return r.allowed && !r.scopeless && clientID == commerceCaller && scope == estateCommerceProfileScope
}

type commerceStore struct {
	profiles   []postgres.EstateCommerceProfile
	provenance bool
	provErr    error
	err        error
	failRec    bool
	records    []postgres.EstateCommerceProfileResolution
	asked      []string
}

func (s *commerceStore) EstateCommerceProfiles(_ context.Context, estate string) ([]postgres.EstateCommerceProfile, error) {
	var out []postgres.EstateCommerceProfile
	for _, p := range s.profiles {
		if p.DigitalEstateID == estate {
			out = append(out, p)
		}
	}
	return out, s.err
}

func (s *commerceStore) EstateBindingProvenance(_ context.Context, caller, binding, estate, engine string, since time.Time) (bool, error) {
	s.asked = append(s.asked, caller+"|"+binding+"|"+estate+"|"+engine)
	if time.Since(since) > 2*bindingProvenanceWindow {
		return false, errors.New("window not bounded")
	}
	return s.provenance, s.provErr
}

func (s *commerceStore) RecordEstateCommerceProfileResolution(_ context.Context, r postgres.EstateCommerceProfileResolution) error {
	if s.failRec {
		return errors.New("record unavailable")
	}
	s.records = append(s.records, r)
	return nil
}

func activeProfile(change func(*postgres.EstateCommerceProfile)) postgres.EstateCommerceProfile {
	p := postgres.EstateCommerceProfile{CommerceProfileID: commerceProfileID, DigitalEstateID: estateID, TenantID: estateTenant,
		TradeEngineInstanceID: estateEngine, CommerceModel: "B2B", MarketScope: []string{commerceMarket}, Status: "ACTIVE",
		Revision: 2, EffectiveFrom: time.Now().Add(-time.Hour), EvidenceReference: "changeset:synthetic-0002"}
	if change != nil {
		change(&p)
	}
	return p
}

type commerceFixture struct {
	store       *commerceStore
	registry    commerceRegistry
	caller      auth.Principal
	environment string
	enabled     bool
}

func newCommerceFixture() *commerceFixture {
	return &commerceFixture{
		store:    &commerceStore{profiles: []postgres.EstateCommerceProfile{activeProfile(nil)}, provenance: true},
		registry: commerceRegistry{allowed: true}, environment: "staging", enabled: true,
		caller: auth.Principal{Issuer: resolveIssuer, Subject: commerceCaller, ActorType: "workload", ClientID: commerceCaller,
			TokenID: "workload-token", Scopes: map[string]struct{}{estateCommerceProfileScope: {}}},
	}
}

func commerceBody(change map[string]any) map[string]any {
	body := map[string]any{"digital_estate_id": estateID, "engine_instance_id": estateEngine, "market_id": commerceMarket,
		"estate_provenance": map[string]any{"kind": "ESTATE_CLIENT_BINDING", "reference": estateBinding},
		"correlation_id":    resolveCorrelation}
	for k, v := range change {
		if v == nil {
			delete(body, k)
		} else {
			body[k] = v
		}
	}
	return body
}

func (f *commerceFixture) post(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	handler := New(Dependencies{Store: &fakeStore{}, Environment: f.environment,
		WorkloadVerifier: fakeVerifier{principal: f.caller}, WorkloadRegistry: f.registry,
		EstateCommerceProfileEnabled: f.enabled, EstateCommerceProfiles: f.store})
	if body == nil {
		body = commerceBody(nil)
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/estate/v1/commerce-profiles/resolve", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", resolveCorrelation)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func TestEstateCommerceProfileAttestsTheSingleActiveModel(t *testing.T) {
	f := newCommerceFixture()
	res := f.post(t, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("attestation is cacheable")
	}
	if err := contracts.Validate(commerceProfileAttestationSchema, res.Body.Bytes()); err != nil {
		t.Fatalf("attestation does not match the Shared contract: %v", err)
	}
	var out commerceProfileAttestation
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.CommerceModel != "B2B" || out.CommerceProfileID != commerceProfileID || out.ProfileRevision != 2 ||
		out.TenantID != estateTenant || out.MarketID != commerceMarket || out.ProfileEffectiveUntil != nil {
		t.Fatalf("attestation = %+v", out)
	}
	if !out.ExpiresAt.After(out.IssuedAt) || out.ExpiresAt.Sub(out.IssuedAt) > commerceAttestationTTL {
		t.Fatalf("lifetime %s exceeds the contract", out.ExpiresAt.Sub(out.IssuedAt))
	}
	if len(f.store.records) != 1 || f.store.records[0].Outcome != "ATTESTED" ||
		f.store.records[0].AttestationID != out.AttestationID || f.store.records[0].CommerceModel != "B2B" {
		t.Fatalf("records = %+v", f.store.records)
	}
	if want := commerceCaller + "|" + estateBinding + "|" + estateID + "|" + estateEngine; len(f.store.asked) != 1 || f.store.asked[0] != want {
		t.Fatalf("provenance was checked as %v, want %s", f.store.asked, want)
	}
}

func TestEstateCommerceProfileNeverOutlivesTheProfile(t *testing.T) {
	f := newCommerceFixture()
	until := time.Now().Add(20 * time.Second)
	f.store.profiles = []postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.EffectiveUntil = &until })}
	res := f.post(t, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	var out commerceProfileAttestation
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if out.ProfileEffectiveUntil == nil || out.ExpiresAt.After(*out.ProfileEffectiveUntil) {
		t.Fatalf("expires_at %s outlives the profile end %v", out.ExpiresAt, out.ProfileEffectiveUntil)
	}
	ending := time.Now().Add(300 * time.Millisecond)
	f.store.profiles = []postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.EffectiveUntil = &ending })}
	if res := f.post(t, nil); res.Code == http.StatusOK && time.Until(ending) > 0 {
		var short commerceProfileAttestation
		_ = json.Unmarshal(res.Body.Bytes(), &short)
		if short.ExpiresAt.After(ending) {
			t.Fatalf("an almost-ended profile was attested past its end")
		}
	}
}

func TestEstateCommerceProfileRefusesAnythingButExactlyOneActiveProfile(t *testing.T) {
	later := time.Now().Add(time.Hour)
	ended := time.Now().Add(-time.Minute)
	cases := map[string]struct {
		profiles []postgres.EstateCommerceProfile
		status   int
		code     string
	}{
		"none":         {nil, http.StatusNotFound, "PROFILE_NOT_REGISTERED"},
		"other estate": {[]postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.DigitalEstateID = "estate_other" })}, http.StatusNotFound, "PROFILE_NOT_REGISTERED"},
		"other engine": {[]postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.TradeEngineInstanceID = "ei_other01" })}, http.StatusForbidden, "ENGINE_INSTANCE_NOT_PERMITTED"},
		"other market": {[]postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.MarketScope = []string{"zuribeans_za"} })}, http.StatusNotFound, "PROFILE_NOT_REGISTERED"},
		"suspended":    {[]postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.Status = "SUSPENDED" })}, http.StatusForbidden, "PROFILE_NOT_ACTIVE"},
		"revoked":      {[]postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.Status = "REVOKED" })}, http.StatusForbidden, "PROFILE_NOT_ACTIVE"},
		"not yet":      {[]postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.EffectiveFrom = later })}, http.StatusForbidden, "PROFILE_NOT_ACTIVE"},
		"ended":        {[]postgres.EstateCommerceProfile{activeProfile(func(p *postgres.EstateCommerceProfile) { p.EffectiveUntil = &ended })}, http.StatusForbidden, "PROFILE_NOT_ACTIVE"},
		"two active (B2B and B2C)": {[]postgres.EstateCommerceProfile{activeProfile(nil), activeProfile(func(p *postgres.EstateCommerceProfile) {
			p.CommerceProfileID, p.CommerceModel = "1f2e3d4c-5b6a-4978-8a9b-0c1d2e3f4a5b", "B2C"
		})}, http.StatusConflict, "PROFILE_AMBIGUOUS"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCommerceFixture()
			f.store.profiles = tc.profiles
			res := f.post(t, nil)
			if res.Code != tc.status || problemCode(t, res) != tc.code {
				t.Fatalf("status = %d code = %q, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
			if len(f.store.records) != 1 || f.store.records[0].Outcome != tc.code || f.store.records[0].CommerceProfileID != "" ||
				f.store.records[0].CommerceModel != "" {
				t.Fatalf("denial record = %+v", f.store.records)
			}
		})
	}
}

func TestEstateCommerceProfileRequiresVerifiedEstateProvenance(t *testing.T) {
	cases := map[string]struct {
		fixture func(*commerceFixture)
		body    map[string]any
		status  int
		code    string
	}{
		"no attestation for this caller, estate and engine": {func(f *commerceFixture) { f.store.provenance = false }, nil, http.StatusForbidden, "ESTATE_PROVENANCE_INVALID"},
		"resource assignment is not yet governed":           {nil, commerceBody(map[string]any{"estate_provenance": map[string]any{"kind": "GOVERNED_RESOURCE_ASSIGNMENT", "reference": "assignment:synthetic"}}), http.StatusForbidden, "ESTATE_PROVENANCE_INVALID"},
		"reference is not a binding":                        {nil, commerceBody(map[string]any{"estate_provenance": map[string]any{"kind": "ESTATE_CLIENT_BINDING", "reference": "zuribeans"}}), http.StatusForbidden, "ESTATE_PROVENANCE_INVALID"},
		"sales channel is not provenance":                   {nil, commerceBody(map[string]any{"estate_provenance": map[string]any{"kind": "SALES_CHANNEL", "reference": "zuribeans"}}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"provenance outage":                                 {func(f *commerceFixture) { f.store.provErr = errors.New("down") }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCommerceFixture()
			if tc.fixture != nil {
				tc.fixture(f)
			}
			res := f.post(t, tc.body)
			if res.Code != tc.status || problemCode(t, res) != tc.code {
				t.Fatalf("status = %d code = %q, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
		})
	}
}

func TestEstateCommerceProfileCallerAndRequestBoundary(t *testing.T) {
	cases := map[string]struct {
		fixture func(*commerceFixture)
		body    map[string]any
		status  int
		code    string
	}{
		"inactive workload":      {func(f *commerceFixture) { f.registry.allowed = false }, nil, http.StatusForbidden, ""},
		"scope not allocated":    {func(f *commerceFixture) { f.registry.scopeless = true }, nil, http.StatusForbidden, ""},
		"human caller":           {func(f *commerceFixture) { f.caller.ActorType = "human" }, nil, http.StatusForbidden, ""},
		"asserted model":         {nil, commerceBody(map[string]any{"commerce_model": "B2C"}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"asserted tenant":        {nil, commerceBody(map[string]any{"tenant_id": estateTenant}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"asserted profile":       {nil, commerceBody(map[string]any{"commerce_profile_id": commerceProfileID}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"no market":              {nil, commerceBody(map[string]any{"market_id": nil}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"no provenance":          {nil, commerceBody(map[string]any{"estate_provenance": nil}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"store outage":           {func(f *commerceFixture) { f.store.err = errors.New("down") }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		"unrecorded attestation": {func(f *commerceFixture) { f.store.failRec = true }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCommerceFixture()
			if tc.fixture != nil {
				tc.fixture(f)
			}
			res := f.post(t, tc.body)
			if res.Code != tc.status || (tc.code != "" && problemCode(t, res) != tc.code) {
				t.Fatalf("status = %d code = %q, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
		})
	}
}

func TestEstateCommerceProfileRouteIsDefaultOffAndNonproduction(t *testing.T) {
	for name, change := range map[string]func(*commerceFixture){
		"disabled":    func(f *commerceFixture) { f.enabled = false },
		"production":  func(f *commerceFixture) { f.environment = "production" },
		"integration": func(f *commerceFixture) { f.environment = "integration" },
		"unset":       func(f *commerceFixture) { f.environment = "" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newCommerceFixture()
			change(f)
			if res := f.post(t, nil); res.Code != http.StatusNotFound && res.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d: route mounted", res.Code)
			}
		})
	}
}
