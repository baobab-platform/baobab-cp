package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
)

// TestTenantOnboardingRoutesAreScoped: requesting and authorising
// onboarding are separate privileges (ADR-BCP-017 sections 22, 39); either
// may read. Applicant, review and decision scopes never reach the routes,
// and every refusal happens before any request is read (the service here
// has no repository).
func TestTenantOnboardingRoutesAreScoped(t *testing.T) {
	const base = "/v1/tenant-onboarding-requests"
	const id = base + "/tor_0190a1b2c3d4e5f60718293a4b5c6d7e"
	for _, tc := range []struct {
		name      string
		principal auth.Principal
		method    string
		path      string
		status    int
		code      string
	}{
		{"an applicant cannot request onboarding", applicantPrincipal("application:write"), http.MethodPost, base, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"a decider cannot request onboarding without the scope", staffPrincipal(RolePlatformAdmin, "admission:decide"), http.MethodPost, base, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"the request scope cannot authorise", staffPrincipal(RolePlatformAdmin, "onboarding:request"), http.MethodPost, id + "/authorisation", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"the authorise scope cannot request", staffPrincipal(RolePlatformAdmin, "onboarding:authorise"), http.MethodPost, base, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"the authorise scope cannot fulfil", staffPrincipal(RolePlatformAdmin, "onboarding:authorise"), http.MethodPost, id + "/fulfilment", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"a tenant administrator cannot request", staffPrincipal(RoleTenantAdmin, "onboarding:request"), http.MethodPost, base, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"reviewers cannot read onboarding requests", staffPrincipal(RolePlatformAdmin, "admission:review"), http.MethodGet, base, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"the authorise scope reads, but needs a registered principal", staffPrincipal(RolePlatformAdmin, "onboarding:authorise"), http.MethodGet, id, http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED"},
		{"the request scope reads, but needs a registered principal", staffPrincipal(RolePlatformAdmin, "onboarding:request"), http.MethodGet, base, http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED"},
		{"platform staff need a registered principal to request", staffPrincipal(RolePlatformAdmin, "onboarding:request"), http.MethodPost, base, http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: tc.principal},
				Identities: repository.NewInMemoryRepository(), Onboarding: &onboarding.Service{}})
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"reason":"x"}`))
			request.Header.Set("Authorization", "Bearer token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || problemCode(t, response) != tc.code {
				t.Fatalf("got %d %s, want %d %s: %s", response.Code, problemCode(t, response), tc.status, tc.code, response.Body.String())
			}
		})
	}
}
