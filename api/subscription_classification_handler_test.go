package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service/subscription"
)

// TestSubscriptionClassificationRoutesAreScoped covers ADR-BCP-018 ORG-11's
// HTTP authority: classifying and reading classifications are distinct
// privileged scopes for platform administrators with a registered Control
// Plane principal (no JIT provisioning for staff). Applicant scopes,
// admission scopes and tenant administrators never reach them, and a token
// the verifier refuses (another issuer or audience) is unauthenticated.
// Every refusal happens before any subscription is read: the classifier
// here has no repository.
func TestSubscriptionClassificationRoutesAreScoped(t *testing.T) {
	const sub = "/v1/product-subscriptions/sub_0190a1b2c3d4e5f60718293a4b5c6d7e"
	body := `{"admission_decision_id":"adm_0190a1b2c3d4e5f60718293a4b5c6d7e","reason":"x"}`
	handler := func(p auth.Principal, verifyErr error) http.Handler {
		return New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: p, err: verifyErr},
			Identities: repository.NewInMemoryRepository(), Classifications: &subscription.Classifier{}})
	}
	for _, tc := range []struct {
		name      string
		principal auth.Principal
		verifyErr error
		method    string
		path      string
		status    int
		code      string
	}{
		{"applicant scopes cannot classify", applicantPrincipal("application:read", "application:write"), nil, http.MethodPost, sub + "/classification", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"applicant scopes cannot read classifications", applicantPrincipal("application:read", "application:write"), nil, http.MethodGet, sub + "/classification", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"an applicant holding the scope still lacks the platform role", applicantPrincipal("subscription:classify"), nil, http.MethodPost, sub + "/classification", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"a reviewer cannot classify", staffPrincipal(RolePlatformAdmin, "admission:review"), nil, http.MethodPost, sub + "/classification", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"a decider cannot classify without the scope", staffPrincipal(RolePlatformAdmin, "admission:decide"), nil, http.MethodPost, sub + "/reclassification", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"read access cannot classify", staffPrincipal(RolePlatformAdmin, "subscription:read"), nil, http.MethodPost, sub + "/reclassification", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"classify access is not read access", staffPrincipal(RolePlatformAdmin, "subscription:classify"), nil, http.MethodGet, sub + "/classification", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"a tenant administrator cannot classify", staffPrincipal(RoleTenantAdmin, "subscription:classify"), nil, http.MethodPost, sub + "/classification", http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"platform staff need a registered principal to classify", staffPrincipal(RolePlatformAdmin, "subscription:classify"), nil, http.MethodPost, sub + "/classification", http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED"},
		{"platform staff need a registered principal to read", staffPrincipal(RolePlatformAdmin, "subscription:read"), nil, http.MethodGet, "/v1/tenants/tn_example/products/zuritrade/classification", http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED"},
		{"a token from another issuer or audience is refused", staffPrincipal(RolePlatformAdmin, "subscription:classify"), errors.New("oidc: id token issued by a different provider"), http.MethodPost, sub + "/classification", http.StatusUnauthorized, "AUTH_TOKEN_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer token")
			response := httptest.NewRecorder()
			handler(tc.principal, tc.verifyErr).ServeHTTP(response, request)
			if response.Code != tc.status || problemCode(t, response) != tc.code {
				t.Fatalf("got %d %s, want %d %s: %s", response.Code, problemCode(t, response), tc.status, tc.code, response.Body.String())
			}
		})
	}
}
