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

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// Synthetic only: no real issuer, client, estate or token. This proves CP's
// boundary, not connected IAM acceptance.
const (
	estateEngine  = "ei_synthetic01"
	estateID      = "estate_synthetic"
	estateTenant  = "tn_synthetic01"
	estateBinding = "5d2c7f0e-3b1a-4c6d-9e8f-0a1b2c3d4e5f"
)

type estateRegistry struct{ allowed, noSubjectAudience bool }

func (r estateRegistry) IsActive(string) bool { return r.allowed }
func (r estateRegistry) SubjectPrincipalAudiences(clientID string) []string {
	if !r.allowed || r.noSubjectAudience || clientID != resolveCaller {
		return nil
	}
	return []string{buyerSubjectAudience}
}
func (r estateRegistry) AllowsScope(clientID, scope string) bool {
	return r.allowed && clientID == resolveCaller && scope == estateClientBindingScope
}

type estateStore struct {
	bindings []postgres.EstateClientBinding
	err      error
	records  []postgres.EstateClientBindingResolution
	failRec  bool
	reads    int
}

func (s *estateStore) EstateClientBindings(_ context.Context, issuer, client string) ([]postgres.EstateClientBinding, error) {
	s.reads++
	var out []postgres.EstateClientBinding
	for _, b := range s.bindings {
		if b.Issuer == issuer && b.BFFClientID == client {
			out = append(out, b)
		}
	}
	return out, s.err
}

func (s *estateStore) RecordEstateClientBindingResolution(_ context.Context, r postgres.EstateClientBindingResolution) error {
	if s.failRec {
		return errors.New("record unavailable")
	}
	s.records = append(s.records, r)
	return nil
}

type estateFixture struct {
	store        *estateStore
	subjects     *resolveSubjects
	introspector *resolveIntrospector
	caller       auth.Principal
	registry     estateRegistry
	environment  string
	enabled      bool
	evidenceExp  time.Time
}

func activeBinding(change func(*postgres.EstateClientBinding)) postgres.EstateClientBinding {
	b := postgres.EstateClientBinding{BindingID: estateBinding, Issuer: resolveIssuer, BFFClientID: resolveBFF,
		DigitalEstateID: estateID, TenantID: estateTenant, EngineInstanceID: estateEngine, Environment: "staging",
		DeploymentReference: "deployment.synthetic-estate.staging", Status: "ACTIVE", Revision: 3,
		EffectiveFrom: time.Now().Add(-time.Hour), EvidenceReference: "changeset:synthetic-0001"}
	if change != nil {
		change(&b)
	}
	return b
}

func newEstateFixture() *estateFixture {
	exp := time.Now().Add(5 * time.Minute).UTC()
	return &estateFixture{
		store: &estateStore{bindings: []postgres.EstateClientBinding{activeBinding(nil)}}, environment: "staging", enabled: true,
		registry: estateRegistry{allowed: true}, evidenceExp: exp,
		caller: auth.Principal{Issuer: resolveIssuer, Subject: resolveCaller, ActorType: "workload", ClientID: resolveCaller,
			TokenID: "workload-token", Scopes: map[string]struct{}{estateClientBindingScope: {}}},
		subjects: &resolveSubjects{verifier: fakeVerifier{principal: auth.Principal{Issuer: resolveIssuer, Subject: resolveBFF,
			ActorType: "workload", ClientID: resolveBFF, TokenID: "bff-token", ExpiresAt: exp,
			Scopes: map[string]struct{}{bffHandoffScope: {}}}}},
		introspector: &resolveIntrospector{result: auth.Introspection{Active: true, Issuer: resolveIssuer, Subject: resolveBFF,
			ClientID: resolveBFF, Audience: []string{buyerSubjectAudience}, Scopes: map[string]struct{}{bffHandoffScope: {}},
			ExpiresAt: exp}},
	}
}

func (f *estateFixture) client(change func(*auth.Principal)) {
	p := f.subjects.verifier.(fakeVerifier).principal
	change(&p)
	f.subjects.verifier = fakeVerifier{principal: p}
}

func estateBody(change map[string]any) map[string]any {
	body := map[string]any{"purpose": "BUYER_OPERATION", "client_evidence": resolveToken, "client_evidence_type": accessTokenType,
		"subject_authorised_party": resolveBFF, "engine_instance_id": estateEngine, "environment": "staging",
		"correlation_id": resolveCorrelation}
	for k, v := range change {
		if v == nil {
			delete(body, k)
		} else {
			body[k] = v
		}
	}
	return body
}

func (f *estateFixture) post(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	handler := New(Dependencies{Store: &fakeStore{}, Environment: f.environment,
		WorkloadVerifier: fakeVerifier{principal: f.caller}, WorkloadRegistry: f.registry,
		EstateClientBindingEnabled: f.enabled, EstateClientBindings: f.store,
		BuyerSubjects: f.subjects, BuyerSubjectIntrospector: f.introspector, BuyerAuthorisedParties: []string{resolveBFF}})
	if body == nil {
		body = estateBody(nil)
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/estate/v1/client-bindings/resolve", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", resolveCorrelation)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if strings.Contains(res.Body.String(), resolveToken) {
		t.Fatalf("response disclosed the client evidence: %s", res.Body.String())
	}
	return res
}

func TestEstateClientBindingAttestsTheSingleActiveBinding(t *testing.T) {
	f := newEstateFixture()
	res := f.post(t, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("cache-control = %q", res.Header().Get("Cache-Control"))
	}
	var out estateBindingAttestation
	mustNoError(t, json.Unmarshal(res.Body.Bytes(), &out))
	if out.Outcome != "ATTESTED" || out.DigitalEstateID != estateID || out.TenantID != estateTenant ||
		out.BindingID != estateBinding || out.Revision != 3 || out.Environment != "staging" {
		t.Fatalf("attestation = %+v", out)
	}
	if lifetime := out.ExpiresAt.Sub(out.AttestedAt); lifetime <= 0 || lifetime > 60*time.Second {
		t.Fatalf("attestation lifetime = %v", lifetime)
	}
	if len(f.store.records) != 1 || f.store.records[0].Outcome != "ATTESTED" || f.store.records[0].Revision != 3 {
		t.Fatalf("records = %+v", f.store.records)
	}
	// A narrowing hint naming the bound estate attests the same binding.
	if res := f.post(t, estateBody(map[string]any{"requested_digital_estate_id": estateID})); res.Code != http.StatusOK {
		t.Fatalf("narrowed status = %d: %s", res.Code, res.Body.String())
	}
}

func TestEstateClientBindingNeverOutlivesTheClientEvidence(t *testing.T) {
	f := newEstateFixture()
	soon := time.Now().Add(20 * time.Second).UTC()
	f.client(func(p *auth.Principal) { p.ExpiresAt = soon })
	f.introspector.result.ExpiresAt = soon
	res := f.post(t, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	var out estateBindingAttestation
	mustNoError(t, json.Unmarshal(res.Body.Bytes(), &out))
	if out.ExpiresAt.After(soon) {
		t.Fatalf("expires_at %v outlives evidence %v", out.ExpiresAt, soon)
	}
}

func TestEstateClientBindingRefusesBindingsThatAreNotExactlyOneActive(t *testing.T) {
	until := time.Now().Add(-time.Minute)
	later := time.Now().Add(time.Hour)
	cases := map[string]struct {
		bindings []postgres.EstateClientBinding
		body     map[string]any
		status   int
		code     string
	}{
		"none for client": {nil, nil, http.StatusNotFound, "BINDING_NOT_FOUND"},
		"other client only": {[]postgres.EstateClientBinding{activeBinding(func(b *postgres.EstateClientBinding) { b.BFFClientID = "other-bff" })},
			nil, http.StatusNotFound, "BINDING_NOT_FOUND"},
		"narrowed to an unbound estate": {[]postgres.EstateClientBinding{activeBinding(nil)},
			estateBody(map[string]any{"requested_digital_estate_id": "estate_other"}), http.StatusNotFound, "BINDING_NOT_FOUND"},
		"wrong environment": {[]postgres.EstateClientBinding{activeBinding(func(b *postgres.EstateClientBinding) { b.Environment = "development" })},
			nil, http.StatusForbidden, "ENVIRONMENT_MISMATCH"},
		"wrong engine instance": {[]postgres.EstateClientBinding{activeBinding(func(b *postgres.EstateClientBinding) { b.EngineInstanceID = "ei_other01" })},
			nil, http.StatusForbidden, "ENGINE_INSTANCE_NOT_PERMITTED"},
		"revoked": {[]postgres.EstateClientBinding{activeBinding(func(b *postgres.EstateClientBinding) { b.Status = "REVOKED" })},
			nil, http.StatusForbidden, "BINDING_NOT_ACTIVE"},
		"suspended": {[]postgres.EstateClientBinding{activeBinding(func(b *postgres.EstateClientBinding) { b.Status = "SUSPENDED" })},
			nil, http.StatusForbidden, "BINDING_NOT_ACTIVE"},
		"expired": {[]postgres.EstateClientBinding{activeBinding(func(b *postgres.EstateClientBinding) { b.EffectiveUntil = &until })},
			nil, http.StatusForbidden, "BINDING_NOT_ACTIVE"},
		"not yet effective": {[]postgres.EstateClientBinding{activeBinding(func(b *postgres.EstateClientBinding) { b.EffectiveFrom = later })},
			nil, http.StatusForbidden, "BINDING_NOT_ACTIVE"},
		"ambiguous estates": {[]postgres.EstateClientBinding{activeBinding(nil), activeBinding(func(b *postgres.EstateClientBinding) {
			b.BindingID, b.DigitalEstateID = "6e3d8a1f-4c2b-4d7e-8f90-1b2c3d4e5f60", "estate_second"
		})}, nil, http.StatusConflict, "BINDING_AMBIGUOUS"},
		"ambiguous deployments of one estate": {[]postgres.EstateClientBinding{activeBinding(nil), activeBinding(func(b *postgres.EstateClientBinding) {
			b.BindingID, b.DeploymentReference = "6e3d8a1f-4c2b-4d7e-8f90-1b2c3d4e5f60", "deployment.synthetic-estate.blue"
		})}, estateBody(map[string]any{"requested_digital_estate_id": estateID}), http.StatusConflict, "BINDING_AMBIGUOUS"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newEstateFixture()
			f.store.bindings = tc.bindings
			res := f.post(t, tc.body)
			if res.Code != tc.status || problemCode(t, res) != tc.code {
				t.Fatalf("status = %d code = %q, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
			if strings.Contains(res.Body.String(), estateTenant) || strings.Contains(res.Body.String(), "estate_second") {
				t.Fatalf("denial disclosed a binding: %s", res.Body.String())
			}
			if len(f.store.records) != 1 || f.store.records[0].Outcome != tc.code || f.store.records[0].BindingID != "" {
				t.Fatalf("records = %+v", f.store.records)
			}
		})
	}
	// Narrowing resolves the two-estate ambiguity to exactly one.
	f := newEstateFixture()
	f.store.bindings = cases["ambiguous estates"].bindings
	if res := f.post(t, estateBody(map[string]any{"requested_digital_estate_id": "estate_second"})); res.Code != http.StatusOK {
		t.Fatalf("narrowed status = %d: %s", res.Code, res.Body.String())
	}
}

func TestEstateClientBindingRefusesClientEvidenceThatIsNotTheBFF(t *testing.T) {
	cases := map[string]struct {
		client func(*auth.Principal)
		intro  func(*auth.Introspection)
		body   map[string]any
		code   string
	}{
		"human token":                       {func(p *auth.Principal) { p.ActorType = "human" }, nil, nil, "CLIENT_EVIDENCE_INVALID"},
		"no handoff scope":                  {func(p *auth.Principal) { p.Scopes = map[string]struct{}{buyerSubjectScope: {}} }, nil, nil, "CLIENT_EVIDENCE_INVALID"},
		"unregistered client":               {func(p *auth.Principal) { p.ClientID = "rogue-bff" }, nil, estateBody(map[string]any{"subject_authorised_party": "rogue-bff"}), "CLIENT_EVIDENCE_INVALID"},
		"no token id":                       {func(p *auth.Principal) { p.TokenID = "" }, nil, nil, "CLIENT_EVIDENCE_INVALID"},
		"expired":                           {func(p *auth.Principal) { p.ExpiresAt = time.Now().Add(-time.Second) }, nil, nil, "CLIENT_EVIDENCE_INVALID"},
		"other buyer client":                {nil, nil, estateBody(map[string]any{"subject_authorised_party": "other-bff"}), "CLIENT_NOT_FOR_SUBJECT"},
		"revoked at issuer":                 {nil, func(i *auth.Introspection) { i.Active = false }, nil, "CLIENT_EVIDENCE_INVALID"},
		"introspection client":              {nil, func(i *auth.Introspection) { i.ClientID = "other-bff" }, nil, "CLIENT_EVIDENCE_INVALID"},
		"introspection scope":               {nil, func(i *auth.Introspection) { i.Scopes = map[string]struct{}{"openid": {}} }, nil, "CLIENT_EVIDENCE_INVALID"},
		"introspection audience":            {nil, func(i *auth.Introspection) { i.Audience = []string{"baobab-control-plane"} }, nil, "CLIENT_EVIDENCE_INVALID"},
		"caller states another environment": {nil, nil, estateBody(map[string]any{"environment": "development"}), "ENVIRONMENT_MISMATCH"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newEstateFixture()
			if tc.client != nil {
				f.client(tc.client)
			}
			if tc.intro != nil {
				tc.intro(&f.introspector.result)
			}
			res := f.post(t, tc.body)
			if res.Code != http.StatusForbidden || problemCode(t, res) != tc.code {
				t.Fatalf("status = %d code = %q, want 403 %s: %s", res.Code, problemCode(t, res), tc.code, res.Body.String())
			}
			if f.store.reads != 0 {
				t.Fatalf("bindings were read %d times before the evidence was accepted", f.store.reads)
			}
		})
	}
}

func TestEstateClientBindingCallerAndRequestBoundary(t *testing.T) {
	cases := map[string]struct {
		fixture func(*estateFixture)
		body    map[string]any
		status  int
		code    string
	}{
		"inactive workload":      {func(f *estateFixture) { f.registry.allowed = false }, nil, http.StatusForbidden, ""},
		"no subject audience":    {func(f *estateFixture) { f.registry.noSubjectAudience = true }, nil, http.StatusForbidden, "CALLER_NOT_PERMITTED"},
		"human caller":           {func(f *estateFixture) { f.caller.ActorType = "human" }, nil, http.StatusForbidden, ""},
		"asserted tenant":        {nil, estateBody(map[string]any{"tenant_id": estateTenant}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"asserted binding":       {nil, estateBody(map[string]any{"binding_id": estateBinding}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"no purpose":             {nil, estateBody(map[string]any{"purpose": nil}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"id token":               {nil, estateBody(map[string]any{"client_evidence_type": "urn:ietf:params:oauth:token-type:id_token"}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"issuer unavailable":     {func(f *estateFixture) { f.subjects.err = errors.New("down") }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		"introspection outage":   {func(f *estateFixture) { f.introspector.err = errors.New("down") }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		"store outage":           {func(f *estateFixture) { f.store.err = errors.New("down") }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		"unrecorded attestation": {func(f *estateFixture) { f.store.failRec = true }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newEstateFixture()
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

func TestEstateClientBindingRouteIsDefaultOffAndNonproduction(t *testing.T) {
	for name, change := range map[string]func(*estateFixture){
		"disabled":    func(f *estateFixture) { f.enabled = false },
		"production":  func(f *estateFixture) { f.environment = "production" },
		"integration": func(f *estateFixture) { f.environment = "integration" },
		"unset":       func(f *estateFixture) { f.environment = "" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newEstateFixture()
			change(f)
			if res := f.post(t, nil); res.Code != http.StatusNotFound && res.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d: route mounted", res.Code)
			}
		})
	}
}
