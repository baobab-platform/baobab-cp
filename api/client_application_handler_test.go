package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service/application"
)

func applicantPrincipal(scopes ...string) auth.Principal {
	set := map[string]struct{}{}
	for _, s := range scopes {
		set[s] = struct{}{}
	}
	return auth.Principal{Subject: "applicant-subject", Issuer: testRealm, ActorType: "human", TokenID: "token-applicant", Scopes: set}
}

func staffPrincipal(role string, scopes ...string) auth.Principal {
	p := applicantPrincipal(scopes...)
	p.Subject, p.TokenID = "staff-subject", "token-staff"
	p.Roles = map[string]struct{}{role: {}}
	return p
}

func problemCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	return body.Code
}

// TestClientApplicationRoutesAreScoped covers ADR-BCP-017's HTTP authority
// (section 38 threat model, ADR-BCP-020 sections 34-35): applicants need
// application scopes and never reach review or decision; review and
// decision are distinct privileged scopes for platform administrators with
// a registered principal. Every refusal happens before any application is
// read or written: the service here has no repository.
func TestClientApplicationRoutesAreScoped(t *testing.T) {
	const app = "/v1/client-applications/capp_0190a1b2c3d4e5f60718293a4b5c6d7e"
	const review = "/v1/admission/applications/capp_0190a1b2c3d4e5f60718293a4b5c6d7e"
	handler := func(p auth.Principal, identities repository.IdentityRepository) http.Handler {
		return New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: p}, Identities: identities,
			Applications: &application.Service{}})
	}
	empty := repository.NewInMemoryRepository()

	for _, tc := range []struct {
		name      string
		principal auth.Principal
		method    string
		path      string
		body      string
		status    int
		code      string
	}{
		{"applicant without application:write cannot create", applicantPrincipal("application:read"), http.MethodPost, "/v1/client-applications", `{}`, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"applicant without application:read cannot list", applicantPrincipal("application:write"), http.MethodGet, "/v1/client-applications", "", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"applicant cannot open the review queue", applicantPrincipal("application:read", "application:write"), http.MethodGet, "/v1/admission/applications", "", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"applicant cannot decide", applicantPrincipal("application:write", "admission:decide"), http.MethodPost, review + "/decision", `{}`, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"tenant administrator cannot review", staffPrincipal(RoleTenantAdmin, "admission:review"), http.MethodPost, review + "/begin-validation", "", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"reviewer cannot decide", staffPrincipal(RolePlatformAdmin, "admission:review"), http.MethodPost, review + "/decision", `{}`, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"decider cannot review", staffPrincipal(RolePlatformAdmin, "admission:decide"), http.MethodPost, review + "/begin-review", "", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"platform staff need a registered principal", staffPrincipal(RolePlatformAdmin, "admission:review"), http.MethodGet, review, "", http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED"},
		{"a malformed Idempotency-Key is refused", applicantPrincipal("application:write"), http.MethodPost, "/v1/client-applications", `{}`, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY"},
		{"applicant-shaped state is refused", applicantPrincipal("application:write"), http.MethodPost, "/v1/client-applications", `{"status":"APPROVED"}`, http.StatusBadRequest, "VALIDATION_FAILED"},
		{"a verified claim is refused", applicantPrincipal("application:write"), http.MethodPost, "/v1/client-applications",
			`{"organisation_profile":{"registration_identifiers":[{"type":"LEI","value":"X","verified":true}]}}`, http.StatusBadRequest, "VALIDATION_FAILED"},
		{"an update names its version", applicantPrincipal("application:write"), http.MethodPatch, app, `{"requirements":{}}`, http.StatusBadRequest, "VALIDATION_FAILED"},
		{"an out-of-range page is refused", applicantPrincipal("application:read"), http.MethodGet, "/v1/client-applications?limit=0", "", http.StatusBadRequest, "INVALID_REQUEST"},
		{"an unknown status filter is refused", staffPrincipal(RolePlatformAdmin, "admission:review"), http.MethodGet, "/v1/admission/applications?status=ACTIVE", "", http.StatusBadRequest, "INVALID_REQUEST"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer token")
			if tc.code == "INVALID_IDEMPOTENCY_KEY" {
				request.Header.Set("Idempotency-Key", "short")
			}
			response := httptest.NewRecorder()
			handler(tc.principal, repository.NewInMemoryRepository()).ServeHTTP(response, request)
			if response.Code != tc.status || problemCode(t, response) != tc.code {
				t.Fatalf("got %d %s, want %d %s: %s", response.Code, problemCode(t, response), tc.status, tc.code, response.Body.String())
			}
		})
	}

	unauthenticated := httptest.NewRecorder()
	handler(applicantPrincipal("application:write"), empty).ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/v1/client-applications", strings.NewReader(`{}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated visitor cannot apply (ADR-BCP-017 section 60): %d", unauthenticated.Code)
	}

	// An applicant's first request registers their principal and nothing else.
	identities := repository.NewInMemoryRepository()
	request := httptest.NewRequest(http.MethodPost, "/v1/client-applications", strings.NewReader(`{"status":"DRAFT"}`))
	request.Header.Set("Authorization", "Bearer token")
	handler(applicantPrincipal("application:write"), identities).ServeHTTP(httptest.NewRecorder(), request)
	if _, err := identities.ResolveIdentity(context.Background(), testRealm, "applicant-subject"); err != nil {
		t.Fatalf("the applicant principal must be registered on first use: %v", err)
	}
	if _, err := identities.GetWorkforceMembership(context.Background(), "any", testTenantID); err == nil {
		t.Fatal("registration must not create tenant membership")
	}
}
