package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// TestTenantRoutesServeTheirSharedContract: the tenant read, entitlement and
// lifecycle routes answer with the bodies Shared control-plane/v1
// tenant.schema.json and errors/v1 problem-details describe, including
// every problem they return (CP Console FE-00 gap B2, phase 2).
//
// Go's regexp engine cannot compile problem-details.schema.json (its
// trace_id pattern uses a lookahead), so a problem is checked against the
// schema's requirements directly: the required members, the UPPER_SNAKE
// code grammar and a UUID correlation_id.
func TestTenantRoutesServeTheirSharedContract(t *testing.T) {
	var (
		tenantSchema    = contracts.MustSchema("control-plane/v1/tenant.schema.json#/$defs/Tenant")
		entitlement     = contracts.MustSchema("control-plane/v1/tenant.schema.json#/$defs/Entitlement")
		lifecycleResult = contracts.MustSchema("control-plane/v1/tenant.schema.json#/$defs/TenantLifecycleResult")
	)
	missing := domain.NotFoundError("not found")
	for _, tc := range []struct {
		name   string
		store  *fakeStore
		method string
		path   string
		status int
		schema *contracts.Schema
		code   string
	}{
		{"tenant", &fakeStore{}, http.MethodGet, "/v1/tenants/" + testTenantID, http.StatusOK, tenantSchema, ""},
		{"legacy legal entity", &fakeStore{tenant: domain.Tenant{TenantID: testTenantID, LegalEntityID: "zuri_beans", DisplayName: "Zuri Beans",
			IsolationStrategy: "row_level_security", ResidencyRegion: "af-south-1", Metadata: map[string]string{"cost_centre": "ops"},
			DesiredState: "active", ObservedState: "pending", Revision: 3}}, http.MethodGet, "/v1/tenants/" + testTenantID, http.StatusOK, tenantSchema, ""},
		{"malformed tenant", &fakeStore{}, http.MethodGet, "/v1/tenants/not-a-tenant", http.StatusBadRequest, nil, "INVALID_TENANT_ID"},
		{"unknown tenant", &fakeStore{tenantErr: missing}, http.MethodGet, "/v1/tenants/" + testTenantID, http.StatusNotFound, nil, "TENANT_NOT_FOUND"},
		{"entitlement", &fakeStore{}, http.MethodGet, "/v1/entitlements?tenantId=" + testTenantID + "&productId=baobab-trade", http.StatusOK, entitlement, ""},
		{"malformed entitlement query", &fakeStore{}, http.MethodGet, "/v1/entitlements?tenantId=" + testTenantID + "&productId=X", http.StatusUnprocessableEntity, nil, "VALIDATION_FAILED"},
		{"unknown entitlement", &fakeStore{entitlementErr: missing}, http.MethodGet, "/v1/entitlements?tenantId=" + testTenantID + "&productId=baobab-trade", http.StatusNotFound, nil, "ENTITLEMENT_NOT_FOUND"},
		{"suspend", &fakeStore{}, http.MethodPost, "/v1/tenants/" + testTenantID + "/suspend", http.StatusOK, lifecycleResult, ""},
		{"activate", &fakeStore{}, http.MethodPost, "/v1/tenants/" + testTenantID + "/activate", http.StatusOK, lifecycleResult, ""},
		{"decommission", &fakeStore{}, http.MethodPost, "/v1/tenants/" + testTenantID + "/decommission", http.StatusOK, lifecycleResult, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := New(Dependencies{Store: tc.store, AdminVerifier: fakeVerifier{principal: adminPrincipal()}})
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer admin-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.code != "" {
				expectProblem(t, response, tc.code)
				return
			}
			if err := contracts.Validate(tc.schema, response.Body.Bytes()); err != nil {
				t.Fatalf("the body does not match its Shared contract: %v\n%s", err, response.Body.String())
			}
		})
	}
}

var (
	problemCodeGrammar = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+$`)
	uuidPattern        = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// expectProblem checks an application/problem+json body against Shared
// errors/v1 problem-details.schema.json.
func expectProblem(t *testing.T, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"type", "title", "status", "code", "correlation_id", "retryable"} {
		if _, ok := body[member]; !ok {
			t.Fatalf("problem lacks %s: %v", member, body)
		}
	}
	if got, _ := body["code"].(string); got != code || !problemCodeGrammar.MatchString(got) {
		t.Fatalf("code %q, want %s", got, code)
	}
	if id, _ := body["correlation_id"].(string); !uuidPattern.MatchString(id) {
		t.Fatalf("correlation_id %q is not a UUID", id)
	}
	if status, _ := body["status"].(float64); int(status) != response.Code {
		t.Fatalf("status member %v, response %d", body["status"], response.Code)
	}
}
