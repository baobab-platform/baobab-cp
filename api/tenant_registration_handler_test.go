package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
)

// registeringAdmin is a platform administrator with tenant:write and a
// verified issuer.
func registeringAdmin() auth.Principal {
	p := adminPrincipal()
	p.Issuer = testRealm
	return p
}

const testOnboardingRequestID = "tor_0190a1b2c3d4e5f60718293a4b5c6d7e"

// registeredStaff returns identities in which principal is a registered
// Control Plane principal.
func registeredStaff(t *testing.T, principal auth.Principal) repository.IdentityRepository {
	t.Helper()
	identities := repository.NewInMemoryRepository()
	registered := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(context.Background(), registered))
	mustNoError(t, identities.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
		PrincipalID: registered.ID, Issuer: principal.Issuer, Subject: principal.Subject, Status: "ACTIVE"}))
	return identities
}

func registration(handler http.Handler, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Idempotency-Key", strings.Repeat("x", 16))
	request.Header.Set("X-Correlation-ID", "7c8f131b-d8ba-4d89-b60b-a187d3944074")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

const registrationBody = `{"tenant_onboarding_request_id":"` + testOnboardingRequestID + `","legal_entity_id":"THAMANI-GLOBAL","display_name":"Zuri Beans","isolation_strategy":"schema_per_tenant","residency_region":"af-south-1"}`

// TestRegisterTenantFulfilsItsOnboardingRequest: a tenant is registered for
// an AUTHORISED onboarding request (ADR-BCP-017 sections 22-24), with the
// fulfilment step run in the registration transaction and a Control
// Plane-minted tenant_id.
func TestRegisterTenantFulfilsItsOnboardingRequest(t *testing.T) {
	database := &fakeStore{}
	principal := registeringAdmin()
	handler := New(Dependencies{Store: database, AdminVerifier: fakeVerifier{principal: principal},
		Identities: registeredStaff(t, principal), Onboarding: &onboarding.Service{}})
	response := registration(handler, "/v1/tenants", registrationBody)
	if response.Code != http.StatusAccepted {
		t.Fatalf("got status %d: %s", response.Code, response.Body.String())
	}
	if database.calls != 1 || database.registered.Basis != domain.RegistrationOnboarding || !database.registeredStep ||
		database.registered.TenantOnboardingRequestID != testOnboardingRequestID {
		t.Fatalf("registration must carry its onboarding request and fulfilment step: calls %d, %+v, step %v",
			database.calls, database.registered, database.registeredStep)
	}
	if database.metadata.ActorID != "admin-123" || database.metadata.CorrelationID != "7c8f131b-d8ba-4d89-b60b-a187d3944074" {
		t.Fatalf("audit metadata not propagated: %#v", database.metadata)
	}
	if !domain.ValidTenantID(database.registeredTenantID) {
		t.Fatalf("expected a Control Plane-minted tenant_id, got %q", database.registeredTenantID)
	}
}

// TestRegisterTenantRefusesDirectRegistration: without an onboarding
// request, with a caller-supplied tenant_id, with bootstrap-only fields, or
// without the onboarding service, nothing is registered.
func TestRegisterTenantRefusesDirectRegistration(t *testing.T) {
	principal := registeringAdmin()
	for name, tc := range map[string]struct {
		body       string
		onboarding *onboarding.Service
		status     int
		code       string
	}{
		"no onboarding request": {`{"legal_entity_id":"THAMANI-GLOBAL","display_name":"Zuri Beans","isolation_strategy":"schema_per_tenant","residency_region":"af-south-1"}`,
			&onboarding.Service{}, http.StatusBadRequest, "VALIDATION_FAILED"},
		"malformed onboarding request":           {strings.Replace(registrationBody, testOnboardingRequestID, "ACME-1", 1), &onboarding.Service{}, http.StatusBadRequest, "VALIDATION_FAILED"},
		"caller-supplied tenant_id":              {strings.Replace(registrationBody, `{`, `{"tenant_id":"tn_client_supplied",`, 1), &onboarding.Service{}, http.StatusBadRequest, "VALIDATION_FAILED"},
		"bootstrap fields on the governed route": {strings.Replace(registrationBody, `{`, `{"bootstrap_reason":"migrating an existing tenant",`, 1), &onboarding.Service{}, http.StatusBadRequest, "VALIDATION_FAILED"},
		"no onboarding service":                  {registrationBody, nil, http.StatusServiceUnavailable, "TENANT_REGISTRATION_UNAVAILABLE"},
	} {
		database := &fakeStore{}
		handler := New(Dependencies{Store: database, AdminVerifier: fakeVerifier{principal: principal},
			Identities: registeredStaff(t, principal), Onboarding: tc.onboarding})
		response := registration(handler, "/v1/tenants", tc.body)
		if response.Code != tc.status || problemCode(t, response) != tc.code || database.calls != 0 {
			t.Errorf("%s: got %d %s (calls %d), want %d %s", name, response.Code, problemCode(t, response), database.calls, tc.status, tc.code)
		}
	}
}

// TestRegisterTenantReportsOnboardingRefusals: the fulfilment step's
// refusals, which roll the registration back, map to their own problems.
func TestRegisterTenantReportsOnboardingRefusals(t *testing.T) {
	principal := registeringAdmin()
	for err, want := range map[error]struct {
		status int
		code   string
	}{
		repository.ErrOnboardingRequestNotFound: {http.StatusUnprocessableEntity, "TENANT_ONBOARDING_REQUEST_NOT_FOUND"},
		onboarding.ErrTransition:                {http.StatusConflict, "TENANT_ONBOARDING_REQUEST_NOT_AUTHORISED"},
		onboarding.ErrTenantAlreadyOnboarded:    {http.StatusConflict, "TENANT_ALREADY_ONBOARDED"},
		onboarding.ErrDesiredStateMismatch:      {http.StatusUnprocessableEntity, "DESIRED_STATE_MISMATCH"},
	} {
		handler := New(Dependencies{Store: &fakeStore{registerErr: err}, AdminVerifier: fakeVerifier{principal: principal},
			Identities: registeredStaff(t, principal), Onboarding: &onboarding.Service{}})
		if response := registration(handler, "/v1/tenants", registrationBody); response.Code != want.status || problemCode(t, response) != want.code {
			t.Errorf("%v: got %d %s, want %d %s", err, response.Code, problemCode(t, response), want.status, want.code)
		}
	}
}

const bootstrapBody = `{"legal_entity_id":"NABHOLD","display_name":"Nabhold Group Africa","isolation_strategy":"schema_per_tenant","residency_region":"af-south-1","bootstrap_reason":"Pre-ADR-BCP-017 first-party tenant migrated into the registry.","evidence_reference":"migration-record/2026-09-nabhold-group"}`

// TestBootstrapRegistrationIsMigrationOnly: the path without an onboarding
// request is off unless configured, needs tenant:bootstrap, records its
// reason and evidence, and never takes an onboarding request.
func TestBootstrapRegistrationIsMigrationOnly(t *testing.T) {
	const path = "/v1/tenants/bootstrap-registrations"
	bootstrapper := staffPrincipal(RolePlatformAdmin, "tenant:bootstrap")
	for name, tc := range map[string]struct {
		principal auth.Principal
		enabled   bool
		body      string
		status    int
		code      string
	}{
		"disabled by default":                {bootstrapper, false, bootstrapBody, http.StatusForbidden, "TENANT_BOOTSTRAP_DISABLED"},
		"tenant:write cannot bootstrap":      {staffPrincipal(RolePlatformAdmin, "tenant:write"), true, bootstrapBody, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		"a tenant administrator cannot":      {staffPrincipal(RoleTenantAdmin, "tenant:bootstrap"), true, bootstrapBody, http.StatusForbidden, "AUTHORIZATION_DENIED"},
		"an onboarding request is refused":   {bootstrapper, true, strings.Replace(bootstrapBody, `{`, `{"tenant_onboarding_request_id":"`+testOnboardingRequestID+`",`, 1), http.StatusBadRequest, "VALIDATION_FAILED"},
		"a reason is required":               {bootstrapper, true, strings.Replace(bootstrapBody, "Pre-ADR-BCP-017 first-party tenant migrated into the registry.", "migrating", 1), http.StatusBadRequest, "VALIDATION_FAILED"},
		"evidence is required":               {bootstrapper, true, strings.Replace(bootstrapBody, `,"evidence_reference":"migration-record/2026-09-nabhold-group"`, "", 1), http.StatusBadRequest, "VALIDATION_FAILED"},
		"platform staff need a registration": {bootstrapper, true, bootstrapBody, http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED"},
	} {
		database := &fakeStore{}
		identities := registeredStaff(t, tc.principal)
		if name == "platform staff need a registration" {
			identities = repository.NewInMemoryRepository()
		}
		handler := New(Dependencies{Store: database, AdminVerifier: fakeVerifier{principal: tc.principal}, Identities: identities,
			Onboarding: &onboarding.Service{}, TenantBootstrapRegistration: tc.enabled})
		response := registration(handler, path, tc.body)
		if response.Code != tc.status || problemCode(t, response) != tc.code || database.calls != 0 {
			t.Errorf("%s: got %d %s (calls %d), want %d %s", name, response.Code, problemCode(t, response), database.calls, tc.status, tc.code)
		}
	}

	database := &fakeStore{}
	handler := New(Dependencies{Store: database, AdminVerifier: fakeVerifier{principal: bootstrapper}, Identities: registeredStaff(t, bootstrapper),
		TenantBootstrapRegistration: true})
	response := registration(handler, path, bootstrapBody)
	if response.Code != http.StatusAccepted || database.registered.Basis != domain.RegistrationBootstrap || database.registeredStep ||
		database.registered.BootstrapEvidenceReference != "migration-record/2026-09-nabhold-group" || database.registered.BootstrapReason == "" {
		t.Fatalf("an enabled bootstrap registration records its basis, reason and evidence: %d %s %+v", response.Code, response.Body.String(), database.registered)
	}
}
