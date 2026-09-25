package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// stubAccounts records whether any account state was touched.
type stubAccounts struct {
	calls int
	err   error
}

func (s *stubAccounts) GetPlatformAccount(context.Context, string) (domain.PlatformAccount, error) {
	s.calls++
	return domain.PlatformAccount{}, s.err
}

func (s *stubAccounts) ChangePlatformAccountStatus(context.Context, string, string, string, string, time.Time, repository.AuditActor) (domain.PlatformAccount, bool, error) {
	s.calls++
	return domain.PlatformAccount{}, false, s.err
}

func (s *stubAccounts) BindTenantPlatformAccount(_ context.Context, tenantID, accountID, reason, _, principalID string, at time.Time, _ repository.AuditActor) (domain.TenantPlatformAccountBinding, bool, error) {
	s.calls++
	return domain.TenantPlatformAccountBinding{ID: "tpab_0190a1b2c3d4e5f60718293a4b5c6d7e", TenantID: tenantID, PlatformAccountID: accountID,
		OrganisationID: "0190a1b2-c3d4-e5f6-0718-293a4b5c6d7e", Status: "ACTIVE", Reason: reason, BoundBy: principalID, EffectiveFrom: at}, true, s.err
}

func (s *stubAccounts) EndTenantPlatformAccountBinding(context.Context, string, string, string, time.Time, repository.AuditActor) (domain.TenantPlatformAccountBinding, error) {
	s.calls++
	return domain.TenantPlatformAccountBinding{}, s.err
}

func (s *stubAccounts) ListTenantPlatformAccountBindings(context.Context, string) ([]domain.TenantPlatformAccountBinding, error) {
	s.calls++
	return nil, s.err
}

// TestPlatformAccountRoutesAreScoped: the account lifecycle needs
// canonical:*, a tenant binding tenant:*, both for platform administrators
// with a registered Control Plane principal. Every refusal happens before
// any account or binding is touched.
func TestPlatformAccountRoutesAreScoped(t *testing.T) {
	const account = "pacct_0190a1b2c3d4e5f60718293a4b5c6d7e"
	bind := "/v1/tenants/" + testTenantID + "/platform-account-binding"
	for _, tc := range []struct {
		name      string
		principal auth.Principal
		method    string
		path      string
		body      string
		status    int
		code      string
	}{
		{"applicant scopes cannot bind", applicantPrincipal("application:write"), http.MethodPost, bind, `{"platform_account_id":"` + account + `","reason":"x"}`, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"tenant:write cannot change an account's status", staffPrincipal(RolePlatformAdmin, "tenant:write"), http.MethodPost, "/v1/platform-accounts/" + account + "/status", `{"status":"SUSPENDED","reason":"x"}`, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"canonical:write cannot bind a tenant", staffPrincipal(RolePlatformAdmin, "canonical:write"), http.MethodPost, bind, `{"platform_account_id":"` + account + `","reason":"x"}`, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"a tenant administrator cannot bind", staffPrincipal(RoleTenantAdmin, "tenant:write"), http.MethodPost, bind, `{"platform_account_id":"` + account + `","reason":"x"}`, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		{"platform staff need a registered principal", staffPrincipal(RolePlatformAdmin, "tenant:write"), http.MethodPost, bind, `{"platform_account_id":"` + account + `","reason":"x"}`, http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := &stubAccounts{}
			handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: tc.principal},
				Identities: repository.NewInMemoryRepository(), PlatformAccounts: accounts})
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || problemCode(t, response) != tc.code || accounts.calls != 0 {
				t.Fatalf("got %d %s (calls %d), want %d %s: %s", response.Code, problemCode(t, response), accounts.calls, tc.status, tc.code, response.Body.String())
			}
		})
	}
}

// TestPlatformAccountRequestsFollowTheSharedContract: bodies are validated
// against Shared platform.schema.json before any state is touched, and the
// principal making a binding comes from the credentials, never the body.
func TestPlatformAccountRequestsFollowTheSharedContract(t *testing.T) {
	const account = "pacct_0190a1b2c3d4e5f60718293a4b5c6d7e"
	principal := staffPrincipal(RolePlatformAdmin, "tenant:write", "canonical:write")
	identities := repository.NewInMemoryRepository()
	registered := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(context.Background(), registered))
	mustNoError(t, identities.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
		PrincipalID: registered.ID, Issuer: principal.Issuer, Subject: principal.Subject, Status: "ACTIVE"}))
	accounts := &stubAccounts{}
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: principal}, Identities: identities, PlatformAccounts: accounts})
	send := func(path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	bind := "/v1/tenants/" + testTenantID + "/platform-account-binding"
	for name, tc := range map[string]struct{ path, body string }{
		"binding without a reason":          {bind, `{"platform_account_id":"` + account + `"}`},
		"binding naming its own principal":  {bind, `{"platform_account_id":"` + account + `","reason":"x","bound_by":"someone-else"}`},
		"binding naming the organisation":   {bind, `{"platform_account_id":"` + account + `","reason":"x","organisation_id":"ce_1"}`},
		"binding a non-opaque account id":   {bind, `{"platform_account_id":"ACME-GLOBAL","reason":"x"}`},
		"account moved back to PENDING":     {"/v1/platform-accounts/" + account + "/status", `{"status":"PENDING","reason":"x"}`},
		"status change without a reason":    {"/v1/platform-accounts/" + account + "/status", `{"status":"CLOSED"}`},
		"ending a binding without a reason": {bind + "/end", `{}`},
	} {
		if response := send(tc.path, tc.body); response.Code != http.StatusBadRequest || problemCode(t, response) != "VALIDATION_FAILED" {
			t.Errorf("%s: got %d %s", name, response.Code, response.Body.String())
		}
	}
	if accounts.calls != 0 {
		t.Fatalf("invalid requests touched account state %d times", accounts.calls)
	}
	response := send(bind, `{"platform_account_id":"`+account+`","reason":"Consumes under the MSA."}`)
	var got domain.TenantPlatformAccountBinding
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &got) != nil || got.BoundBy != registered.ID {
		t.Fatalf("a valid binding is made by the caller's principal: %d %s", response.Code, response.Body.String())
	}
}
