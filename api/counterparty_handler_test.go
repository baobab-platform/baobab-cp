package api

import (
	"net/http"
	"testing"

	"github.com/nabhold/baobab-cp/internal/repository"
)

// TestCounterpartyRoutesRefuseBeforePersistence covers what the HTTP layer
// enforces for ADR-BCP-018 gate ORG-13 before any repository call: bodies
// are decoded strictly (a decision cannot name its own reviewer, a role
// cannot carry its own source authority), decisions are validated, and only
// platform administrators reach the routes.
func TestCounterpartyRoutesRefuseBeforePersistence(t *testing.T) {
	// A nil repository is enough: every case is refused before persistence.
	deps := func(principal fakeVerifier) Dependencies {
		return Dependencies{Store: &fakeStore{}, AdminVerifier: principal, Counterparties: (*repository.PostgresRepository)(nil)}
	}
	handler := New(deps(fakeVerifier{principal: adminPrincipal()}))
	decision := "/v1/organisation-resolution-candidates/orc_0190a1b2c3d4e5f60718293a4b5c6d7e/decision"
	for name, tc := range map[string]struct{ path, body string }{
		"decision naming its reviewer":           {decision, `{"decision":"DISTINCT","reason":"different","decided_by":"someone"}`},
		"decision merging":                       {decision, `{"decision":"MERGED","reason":"same"}`},
		"duplicate without survivor":             {decision, `{"decision":"DUPLICATE_CONFIRMED","reason":"same"}`},
		"distinct naming a survivor":             {decision, `{"decision":"DISTINCT","reason":"x","surviving_organisation_id":"ce_1"}`},
		"decision without reason":                {decision, `{"decision":"DISTINCT"}`},
		"role carrying its own source authority": {"/v1/tenants/" + testTenantID + "/counterparty-roles", `{"organisation_id":"x","role":"BUYER","source_authority":"trade"}`},
		"role granting access":                   {"/v1/tenants/" + testTenantID + "/counterparty-roles", `{"organisation_id":"x","role":"BUYER","portal_access":true}`},
		"ending without a JSON body":             {"/v1/counterparty-roles/crole_0190a1b2c3d4e5f60718293a4b5c6d7e/end", `not json`},
	} {
		if response := adminRequest(t, handler, http.MethodPost, tc.path, tc.body); response.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}

	tenantAdmin := New(deps(fakeVerifier{principal: tenantAdminPrincipal()}))
	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/tenants/" + testTenantID + "/counterparty-roles", `{"organisation_id":"x","role":"BUYER"}`},
		{http.MethodGet, "/v1/tenants/" + testTenantID + "/counterparty-roles", ``},
		{http.MethodPost, "/v1/organisation-reconciliation", `{}`},
		{http.MethodGet, "/v1/organisation-resolution-candidates", ``},
		{http.MethodPost, decision, `{"decision":"DISTINCT","reason":"different"}`},
	} {
		response := adminRequest(t, tenantAdmin, route.method, route.path, route.body)
		if response.Code != http.StatusForbidden {
			t.Errorf("a tenant administrator must not reach %s %s: %d %s", route.method, route.path, response.Code, response.Body.String())
		}
	}
}
