package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// ADR-BCP-028 increment 2: governed JIT buyer registration. Synthetic
// evidence only; this is not connected acceptance.

type failingRegistrations struct{ *repository.Repository }

func (failingRegistrations) RegisterBuyerIdentity(context.Context, domain.Principal, domain.ExternalIdentity, repository.AuditActor) (domain.Principal, domain.ExternalIdentity, bool, error) {
	return domain.Principal{}, domain.ExternalIdentity{}, false, errors.New("database unavailable")
}

type registerOptions struct {
	enabled    bool
	parties    []string
	authorised []string
	identites  repository.IdentityRepository
}

func (f *resolveFixture) register(t *testing.T, o registerOptions) *httptest.ResponseRecorder {
	t.Helper()
	identities := o.identites
	if identities == nil {
		identities = f.repo
	}
	if o.parties == nil {
		o.parties = []string{resolveBFF}
	}
	if o.authorised == nil {
		o.authorised = []string{resolveBFF}
	}
	handler := New(Dependencies{Store: &fakeStore{}, Environment: f.environment, Identities: identities,
		WorkloadVerifier: fakeVerifier{principal: f.caller}, WorkloadRegistry: f.registry,
		BuyerPrincipalResolutionEnabled: f.enabled, BuyerPrincipalResolutionClientIDs: []string{resolveCaller},
		BuyerSubjects: f.subjects, BuyerSubjectIntrospector: f.introspector, BuyerAuthorisedParties: o.authorised,
		BuyerRegistrationEnabled: o.enabled, BuyerRegistrationClientIDs: []string{resolveCaller},
		BuyerRegistrationAuthorisedParties: o.parties})
	raw, _ := json.Marshal(map[string]any{"subject_token": resolveToken, "subject_token_type": accessTokenType, "correlation_id": resolveCorrelation})
	req := httptest.NewRequest(http.MethodPost, "/internal/identity/v1/principals/register-buyer", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", resolveCorrelation)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if bytes.Contains(res.Body.Bytes(), []byte(resolveToken)) || bytes.Contains(res.Body.Bytes(), []byte(resolveSubject)) {
		t.Fatalf("response disclosed the subject token or subject: %s", res.Body.String())
	}
	return res
}

func registrationFixture(t *testing.T) *resolveFixture {
	t.Helper()
	f := newResolveFixture(t)
	f.subject(func(p *auth.Principal) { p.Subject = "kratos-identity-new" })
	f.introspector.result.Subject = "kratos-identity-new"
	f.caller.Scopes[buyerRegistrationScope] = struct{}{}
	return f
}

func TestBuyerRegistrationCreatesIdentityOnlyOnceAndIdempotently(t *testing.T) {
	f := registrationFixture(t)
	before := len(f.repo.Principals)
	first := f.register(t, registerOptions{enabled: true})
	if first.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", first.Code, first.Body.String())
	}
	var created map[string]any
	mustNoError(t, json.Unmarshal(first.Body.Bytes(), &created))
	if created["outcome"] != "REGISTERED" || created["actor_type"] != "human" || created["status"] != "ACTIVE" {
		t.Fatalf("registration = %v", created)
	}
	for _, key := range []string{"tenant_id", "organisation_id", "roles", "membership_id", "customer_id"} {
		if _, ok := created[key]; ok {
			t.Fatalf("registration carried %s", key)
		}
	}
	second := f.register(t, registerOptions{enabled: true})
	var again map[string]any
	mustNoError(t, json.Unmarshal(second.Body.Bytes(), &again))
	if second.Code != http.StatusOK || again["outcome"] != "RESOLVED" || again["principal_id"] != created["principal_id"] {
		t.Fatalf("repeat = %d %v", second.Code, again)
	}
	if len(f.repo.Principals) != before+1 {
		t.Fatalf("principals %d -> %d, want exactly one more", before, len(f.repo.Principals))
	}
	// The registered identity now resolves.
	if res := f.post(t, nil, resolveCorrelation); res.Code != http.StatusOK {
		t.Fatalf("resolve after registration = %d", res.Code)
	}
}

func TestBuyerRegistrationNeverRevivesAStoppedIdentity(t *testing.T) {
	f := newResolveFixture(t) // the fixture's existing identity
	f.caller.Scopes[buyerRegistrationScope] = struct{}{}
	p := f.repo.Principals[f.principal.ID]
	p.Status = "SUSPENDED"
	f.repo.Principals[f.principal.ID] = p
	before := len(f.repo.Principals)
	res := f.register(t, registerOptions{enabled: true})
	if res.Code != http.StatusForbidden || problemCode(t, res) != "PRINCIPAL_NOT_ACTIVE" || len(f.repo.Principals) != before {
		t.Fatalf("got %d %s, principals %d -> %d", res.Code, problemCode(t, res), before, len(f.repo.Principals))
	}
}

func TestBuyerRegistrationDenials(t *testing.T) {
	cases := []struct {
		name   string
		change func(*resolveFixture, *registerOptions)
		status int
		code   string
	}{
		{"BFF client not eligible for registration", func(_ *resolveFixture, o *registerOptions) { o.parties = []string{"other-bff"} },
			http.StatusNotFound, ""},
		{"BFF client authorised for resolution but not registration", func(_ *resolveFixture, o *registerOptions) {
			o.authorised, o.parties = []string{resolveBFF, "second-bff"}, []string{"second-bff"}
		}, http.StatusForbidden, "REGISTRATION_NOT_ELIGIBLE"},
		{"eligible list names a client resolution does not authorise", func(f *resolveFixture, o *registerOptions) {
			o.parties = []string{resolveBFF, "unauthorised-bff"}
			f.subject(func(p *auth.Principal) { p.ClientID = "unauthorised-bff" })
			f.introspector.result.ClientID = "unauthorised-bff"
		}, http.StatusForbidden, "SUBJECT_TOKEN_NOT_FOR_CALLER"},
		{"resolve scope alone", func(f *resolveFixture, _ *registerOptions) { delete(f.caller.Scopes, buyerRegistrationScope) },
			http.StatusForbidden, ""},
		{"registry does not allow registration", func(f *resolveFixture, _ *registerOptions) { f.registry.noRegister = true },
			http.StatusForbidden, ""},
		{"revoked session", func(f *resolveFixture, _ *registerOptions) { f.introspector.result = auth.Introspection{Active: false} },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT"},
		{"store failure", func(f *resolveFixture, o *registerOptions) { o.identites = failingRegistrations{f.repo} },
			http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		{"registration disabled", func(_ *resolveFixture, o *registerOptions) { o.enabled = false }, http.StatusNotFound, ""},
		{"resolution disabled", func(f *resolveFixture, _ *registerOptions) { f.enabled = false }, http.StatusNotFound, ""},
		{"production", func(f *resolveFixture, _ *registerOptions) { f.environment = "production" }, http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := registrationFixture(t)
			o := registerOptions{enabled: true}
			tc.change(f, &o)
			before := len(f.repo.Principals)
			res := f.register(t, o)
			if res.Code != tc.status || (tc.code != "" && problemCode(t, res) != tc.code) {
				t.Fatalf("got %d %s, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
			if len(f.repo.Principals) != before {
				t.Fatalf("a denied registration created a Principal")
			}
		})
	}
}
