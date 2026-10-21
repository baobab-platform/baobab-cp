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
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// Synthetic only: no real issuer, token or Principal. This proves CP's
// boundary, not connected IAM acceptance.
const (
	resolveCaller      = "baobab-trade-workload"
	resolveBFF         = "zuribeans-bff"
	resolveIssuer      = "https://iam.example.test/"
	resolveSubject     = "kratos-identity-0001"
	resolveToken       = "synthetic-header.synthetic-payload.synthetic-signature"
	resolveCorrelation = "0b6f2c1e-6d0a-4f43-9c1a-2f1d6b1e9a40"
)

type resolveRegistry struct{ allowed, noRegister, noSubjectAudience bool }

func (r resolveRegistry) IsActive(string) bool { return r.allowed }
func (r resolveRegistry) SubjectPrincipalAudiences(clientID string) []string {
	if !r.allowed || r.noSubjectAudience || clientID != resolveCaller {
		return nil
	}
	return []string{buyerSubjectAudience}
}
func (r resolveRegistry) AllowsScope(clientID, scope string) bool {
	return r.allowed && clientID == resolveCaller && (scope == principalResolveScope || (scope == buyerRegistrationScope && !r.noRegister))
}

type resolveSubjects struct {
	verifier auth.TokenVerifier
	err      error
	audience string
}

func (s *resolveSubjects) For(_ context.Context, audience string) (auth.TokenVerifier, error) {
	s.audience = audience
	return s.verifier, s.err
}

type resolveIntrospector struct {
	result auth.Introspection
	err    error
	calls  int
}

func (i *resolveIntrospector) Introspect(_ context.Context, token string) (auth.Introspection, error) {
	i.calls++
	if token != resolveToken {
		return auth.Introspection{}, errors.New("unexpected token")
	}
	return i.result, i.err
}

type resolveFixture struct {
	repo         *repository.Repository
	subjects     *resolveSubjects
	introspector *resolveIntrospector
	caller       auth.Principal
	registry     resolveRegistry
	environment  string
	enabled      bool
	principal    domain.Principal
	external     domain.ExternalIdentity
}

func newResolveFixture(t *testing.T) *resolveFixture {
	t.Helper()
	repo := repository.NewInMemoryRepository()
	principal := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, repo.CreateIdentity(context.Background(), principal))
	external := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID,
		Issuer: resolveIssuer, Subject: resolveSubject, Status: "ACTIVE"}
	mustNoError(t, repo.LinkExternalIdentity(context.Background(), external))
	exp := time.Now().Add(5 * time.Minute).UTC()
	return &resolveFixture{
		repo: repo, principal: principal, external: external, environment: "integration", enabled: true,
		registry: resolveRegistry{allowed: true},
		caller: auth.Principal{Issuer: resolveIssuer, Subject: resolveCaller, ActorType: "workload", ClientID: resolveCaller,
			TokenID: "workload-token", Scopes: map[string]struct{}{principalResolveScope: {}}},
		subjects: &resolveSubjects{verifier: fakeVerifier{principal: auth.Principal{Issuer: resolveIssuer, Subject: resolveSubject,
			ActorType: "human", ClientID: resolveBFF, TokenID: "buyer-token", ExpiresAt: exp,
			Scopes: map[string]struct{}{buyerSubjectScope: {}}}}},
		introspector: &resolveIntrospector{result: auth.Introspection{Active: true, Issuer: resolveIssuer, Subject: resolveSubject,
			ClientID: resolveBFF, Audience: []string{buyerSubjectAudience}, Scopes: map[string]struct{}{"openid": {}, buyerSubjectScope: {}},
			ExpiresAt: exp}},
	}
}

func (f *resolveFixture) subject(change func(*auth.Principal)) {
	p := f.subjects.verifier.(fakeVerifier).principal
	change(&p)
	f.subjects.verifier = fakeVerifier{principal: p}
}

func (f *resolveFixture) post(t *testing.T, body map[string]any, header string) *httptest.ResponseRecorder {
	t.Helper()
	handler := New(Dependencies{Store: &fakeStore{}, Environment: f.environment, Identities: f.repo,
		WorkloadVerifier: fakeVerifier{principal: f.caller}, WorkloadRegistry: f.registry,
		BuyerPrincipalResolutionEnabled: f.enabled,
		BuyerSubjects:                   f.subjects, BuyerSubjectIntrospector: f.introspector, BuyerAuthorisedParties: []string{resolveBFF}})
	if body == nil {
		body = map[string]any{"subject_token": resolveToken, "subject_token_type": accessTokenType, "correlation_id": resolveCorrelation}
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/identity/v1/principals/resolve", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("Content-Type", "application/json")
	if header != "-" {
		req.Header.Set("X-Correlation-ID", header)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if strings.Contains(res.Body.String(), resolveToken) || strings.Contains(res.Body.String(), resolveSubject) {
		t.Fatalf("response disclosed the subject token or subject: %s", res.Body.String())
	}
	return res
}

func TestPrincipalResolutionResolvesAnActiveBuyer(t *testing.T) {
	f := newResolveFixture(t)
	res := f.post(t, nil, resolveCorrelation)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	var out map[string]any
	mustNoError(t, json.Unmarshal(res.Body.Bytes(), &out))
	if out["outcome"] != "RESOLVED" || out["principal_id"] != f.principal.ID || out["external_identity_id"] != f.external.ID ||
		out["actor_type"] != "human" || out["status"] != "ACTIVE" {
		t.Fatalf("resolution = %v", out)
	}
	// The resolution carries no authority of any kind.
	for _, key := range []string{"tenant_id", "organisation_id", "roles", "membership_id", "issuer", "subject"} {
		if _, ok := out[key]; ok {
			t.Fatalf("resolution carried %s", key)
		}
	}
	if f.subjects.audience != buyerSubjectAudience || f.introspector.calls != 1 {
		t.Fatalf("audience %q, introspections %d", f.subjects.audience, f.introspector.calls)
	}
	if res.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("cache-control = %q", res.Header().Get("Cache-Control"))
	}
}

func TestPrincipalResolutionEvidenceNeverOutlivesTheToken(t *testing.T) {
	f := newResolveFixture(t)
	earlier := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
	f.introspector.result.ExpiresAt = earlier
	res := f.post(t, nil, resolveCorrelation)
	var out struct {
		EvidenceExpiresAt time.Time `json:"evidence_expires_at"`
	}
	mustNoError(t, json.Unmarshal(res.Body.Bytes(), &out))
	if !out.EvidenceExpiresAt.Equal(earlier) {
		t.Fatalf("evidence_expires_at = %s, want %s", out.EvidenceExpiresAt, earlier)
	}
}

func TestPrincipalResolutionDeniesTheCaller(t *testing.T) {
	cases := map[string]func(*resolveFixture){
		"not the registered caller": func(f *resolveFixture) { f.caller.ClientID = "zuribeans-backend" },
		"no subject audience":       func(f *resolveFixture) { f.registry.noSubjectAudience = true },
		"scope revoked":             func(f *resolveFixture) { f.registry.allowed = false },
		"human caller":              func(f *resolveFixture) { f.caller.ActorType = "human" },
		"missing scope in token":    func(f *resolveFixture) { f.caller.Scopes = map[string]struct{}{"context:resolve": {}} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newResolveFixture(t)
			change(f)
			res := f.post(t, nil, resolveCorrelation)
			if res.Code != http.StatusForbidden {
				t.Fatalf("status = %d: %s", res.Code, res.Body.String())
			}
			if f.introspector.calls != 0 {
				t.Fatal("subject evidence was examined for a denied caller")
			}
		})
	}
}

func TestPrincipalResolutionIsNotMountedOutsideItsBoundary(t *testing.T) {
	for name, change := range map[string]func(*resolveFixture){
		"disabled":   func(f *resolveFixture) { f.enabled = false },
		"production": func(f *resolveFixture) { f.environment = "production" },
		"unset":      func(f *resolveFixture) { f.environment = "" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newResolveFixture(t)
			change(f)
			if res := f.post(t, nil, resolveCorrelation); res.Code != http.StatusNotFound || problemCode(t, res) == "IDENTITY_NOT_RESOLVED" {
				t.Fatalf("status = %d: %s", res.Code, res.Body.String())
			}
		})
	}
}

func TestPrincipalResolutionRejectsAssertedIdentityAndMalformedRequests(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"subject_token": resolveToken, "subject_token_type": accessTokenType, "correlation_id": resolveCorrelation}
	}
	for name, change := range map[string]func(map[string]any){
		"asserted issuer":    func(b map[string]any) { b["issuer"] = resolveIssuer },
		"asserted subject":   func(b map[string]any) { b["subject"] = resolveSubject },
		"asserted principal": func(b map[string]any) { b["principal_id"] = domain.NewPrincipalID() },
		"asserted email":     func(b map[string]any) { b["email"] = "buyer@example.test" },
		"asserted customer":  func(b map[string]any) { b["customer_id"] = "cus_01" },
		"id token":           func(b map[string]any) { b["subject_token_type"] = "urn:ietf:params:oauth:token-type:id_token" },
		"not a jws":          func(b map[string]any) { b["subject_token"] = "opaque-medusa-token" },
		"missing token":      func(b map[string]any) { delete(b, "subject_token") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newResolveFixture(t)
			body := base()
			change(body)
			if res := f.post(t, body, resolveCorrelation); res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d: %s", res.Code, res.Body.String())
			}
		})
	}
	t.Run("correlation header mismatch", func(t *testing.T) {
		f := newResolveFixture(t)
		if res := f.post(t, nil, "5d2a3c4b-1e0f-4a9b-8c7d-6e5f4a3b2c1d"); res.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", res.Code)
		}
		if res := f.post(t, nil, "-"); res.Code != http.StatusBadRequest {
			t.Fatalf("missing header status = %d", res.Code)
		}
	})
}

func TestPrincipalResolutionDeniesSubjectEvidence(t *testing.T) {
	cases := []struct {
		name   string
		change func(*resolveFixture)
		status int
		code   string
	}{
		{"signature, issuer or audience invalid", func(f *resolveFixture) { f.subjects.verifier = fakeVerifier{err: auth.ErrInvalidToken} },
			http.StatusForbidden, "SUBJECT_TOKEN_INVALID"},
		{"workload token as subject", func(f *resolveFixture) { f.subject(func(p *auth.Principal) { p.ActorType = "workload" }) },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_FOR_CALLER"},
		{"without trade:buy", func(f *resolveFixture) {
			f.subject(func(p *auth.Principal) { p.Scopes = map[string]struct{}{"context:resolve": {}} })
		}, http.StatusForbidden, "SUBJECT_TOKEN_NOT_FOR_CALLER"},
		{"unregistered BFF client", func(f *resolveFixture) { f.subject(func(p *auth.Principal) { p.ClientID = "medusa-storefront" }) },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_FOR_CALLER"},
		{"revoked", func(f *resolveFixture) { f.introspector.result = auth.Introspection{Active: false} },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT"},
		{"introspection names another subject", func(f *resolveFixture) { f.introspector.result.Subject = "someone-else" },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT"},
		{"introspection names another client", func(f *resolveFixture) { f.introspector.result.ClientID = "other-client" },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT"},
		{"introspection names another audience", func(f *resolveFixture) { f.introspector.result.Audience = []string{"baobab-erp"} },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT"},
		{"introspection lost trade:buy", func(f *resolveFixture) { f.introspector.result.Scopes = map[string]struct{}{"openid": {}} },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT"},
		{"introspection expired", func(f *resolveFixture) { f.introspector.result.ExpiresAt = time.Now().Add(-time.Second) },
			http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT"},
		{"issuer unavailable", func(f *resolveFixture) { f.subjects.err = errors.New("discovery failed") },
			http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		{"introspection unavailable", func(f *resolveFixture) { f.introspector.err = auth.ErrIntrospectionUnavailable },
			http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newResolveFixture(t)
			tc.change(f)
			res := f.post(t, nil, resolveCorrelation)
			if res.Code != tc.status || problemCode(t, res) != tc.code {
				t.Fatalf("got %d %s, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
		})
	}
}

func TestPrincipalResolutionNeverCreatesOrRevivesAnIdentity(t *testing.T) {
	t.Run("unknown identity", func(t *testing.T) {
		f := newResolveFixture(t)
		f.subject(func(p *auth.Principal) { p.Subject = "kratos-identity-unknown" })
		f.introspector.result.Subject = "kratos-identity-unknown"
		principals := len(f.repo.Principals)
		res := f.post(t, nil, resolveCorrelation)
		if res.Code != http.StatusNotFound || problemCode(t, res) != "IDENTITY_NOT_RESOLVED" || len(f.repo.Principals) != principals {
			t.Fatalf("got %d %s, principals %d -> %d", res.Code, problemCode(t, res), principals, len(f.repo.Principals))
		}
	})
	for name, change := range map[string]func(*resolveFixture){
		"suspended principal": func(f *resolveFixture) {
			p := f.repo.Principals[f.principal.ID]
			p.Status = "SUSPENDED"
			f.repo.Principals[f.principal.ID] = p
		},
		"revoked external identity": func(f *resolveFixture) {
			e := f.repo.ExternalIdentities[resolveIssuer+"\x00"+resolveSubject]
			e.Status = "REVOKED"
			f.repo.ExternalIdentities[resolveIssuer+"\x00"+resolveSubject] = e
		},
		"workload principal": func(f *resolveFixture) {
			p := f.repo.Principals[f.principal.ID]
			p.ActorType = "workload"
			f.repo.Principals[f.principal.ID] = p
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newResolveFixture(t)
			change(f)
			res := f.post(t, nil, resolveCorrelation)
			if res.Code != http.StatusForbidden || problemCode(t, res) != "PRINCIPAL_NOT_ACTIVE" {
				t.Fatalf("got %d %s", res.Code, problemCode(t, res))
			}
		})
	}
}
