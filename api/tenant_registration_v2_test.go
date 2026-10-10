package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
	"github.com/baobab-platform/baobab-cp/internal/store"
)

// The LA-03 v2 intake must remain unavailable until the reviewed rollout
// enables the feature. No request can bypass authenticated platform admin.
func TestOrganisationFirstV2RoutesDefaultOff(t *testing.T) {
	handler := New(Dependencies{Store: &fakeStore{}})
	for _, url := range []string{
		"/v2/tenants",
		"/v2/tenant-onboarding/tor_0190a1b2c3d4e5f60718293a4b5c6d7e/primary-organisation",
	} {
		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("disabled route %s returned %d", url, recorder.Code)
		}
	}
}

func TestOrganisationFirstV2RequiresPrivilegedIdentity(t *testing.T) {
	handler := New(Dependencies{
		Store: &fakeStore{}, OrganisationFirstV2: true, Environment: "test",
		AdminVerifier: fakeVerifier{err: errors.New("invalid bearer")},
	})
	for _, url := range []string{
		"/v2/tenants",
		"/v2/tenant-onboarding/tor_0190a1b2c3d4e5f60718293a4b5c6d7e/primary-organisation",
	} {
		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer invalid")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("v2 route %s admitted an unauthenticated principal: %d", url, recorder.Code)
		}
	}
}

// ---- LA-03B: controlled runtime composition (ADR-BCP-027 section 12) --------
//
// The Organisation-first routes are a distinct, default-off route family. They
// mount only in the approved nonproduction environments, never in production
// or an unset/unknown environment, and are independent of the PEO-02/03 route
// families. Every request still needs a human platform administrator and an
// AUTHORISED TenantOnboardingRequest.

const testOrganisationID = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6c"

var (
	permittedV2Environments = []string{"development", "test", "integration", "sandbox", "staging"}
	deniedV2Environments    = []string{"production", "", "prod", "Production", "local", "preview", "production-eu"}
	organisationFirstPaths  = []string{"/v2/tenants", "/v2/tenant-onboarding/" + testOnboardingRequestID + "/primary-organisation"}
)

const registrationBodyV2 = `{"tenant_onboarding_request_id":"` + testOnboardingRequestID + `","organisation_id":"` + testOrganisationID +
	`","display_name":"ZuriBeans","isolation_strategy":"schema_per_tenant","residency_region":"af-south-1"}`

// organisationFirstFakeStore adds the Organisation-first persistence the v2
// handlers require to the shared fakeStore.
type organisationFirstFakeStore struct {
	*fakeStore
	prepareCalls  int
	registerCalls int
	registered    domain.RegisterTenantV2
	hadStep       bool
	registerErr   error
}

func (s *organisationFirstFakeStore) PrepareOnboardingOrganisation(context.Context, string, store.RequestMetadata, string, string, string, string, ...string) (string, error) {
	s.prepareCalls++
	return testOrganisationID, nil
}

func (s *organisationFirstFakeStore) RegisterTenantV2(_ context.Context, _ string, _ store.RequestMetadata, command domain.RegisterTenantV2, step store.RegistrationStep) (domain.Operation, error) {
	s.registerCalls++
	s.registered, s.hadStep = command, step != nil
	if s.registerErr != nil {
		return domain.Operation{}, s.registerErr
	}
	return domain.Operation{OperationID: "7c8f131b-d8ba-4d89-b60b-a187d3944074", TenantID: command.TenantID, State: "accepted", Revision: 1}, nil
}

func (s *organisationFirstFakeStore) touched() bool {
	return s.prepareCalls+s.registerCalls+s.calls > 0
}

// Stubs whose methods are never reached: they only make the PEO route
// families mountable so the tests can prove the families are independent.
type stubProgressiveWriter struct{ progressiveApplicationWriter }
type stubFoundingWriter struct{ foundingGovernanceWriter }

func organisationFirstHandler(t *testing.T, environment string, enabled bool, database store.TenantStore, extra func(*Dependencies)) http.Handler {
	t.Helper()
	principal := registeringAdmin()
	dependencies := Dependencies{Store: database, AdminVerifier: fakeVerifier{principal: principal},
		Identities: registeredStaff(t, principal), Onboarding: &onboarding.Service{},
		OrganisationFirstV2: enabled, Environment: environment}
	if extra != nil {
		extra(&dependencies)
	}
	return New(dependencies)
}

// TestOrganisationFirstV2MountsOnlyInApprovedNonproduction: the flag alone is
// not enough. Production, an unset environment and any unknown spelling stay
// closed, and nothing reaches persistence.
func TestOrganisationFirstV2MountsOnlyInApprovedNonproduction(t *testing.T) {
	for _, environment := range permittedV2Environments {
		handler := New(Dependencies{Store: &fakeStore{}, OrganisationFirstV2: true, Environment: environment,
			AdminVerifier: fakeVerifier{err: errors.New("invalid bearer")}})
		for _, url := range organisationFirstPaths {
			if code := registration(handler, url, "{}").Code; code != http.StatusUnauthorized {
				t.Errorf("%q: %s should be mounted behind authentication (want 401), got %d", environment, url, code)
			}
		}
	}
	for _, environment := range deniedV2Environments {
		database := &organisationFirstFakeStore{fakeStore: &fakeStore{}}
		handler := organisationFirstHandler(t, environment, true, database, nil)
		for _, url := range organisationFirstPaths {
			if code := registration(handler, url, registrationBodyV2).Code; code != http.StatusNotFound {
				t.Errorf("environment %q: %s must stay closed even with the flag on and a valid administrator, got %d", environment, url, code)
			}
		}
		if database.touched() {
			t.Errorf("environment %q: a closed route reached persistence", environment)
		}
	}
}

func TestOrganisationFirstV2IsOffByDefaultInEveryEnvironment(t *testing.T) {
	for _, environment := range append(append([]string{}, permittedV2Environments...), deniedV2Environments...) {
		database := &organisationFirstFakeStore{fakeStore: &fakeStore{}}
		handler := organisationFirstHandler(t, environment, false, database, nil)
		for _, url := range organisationFirstPaths {
			if code := registration(handler, url, registrationBodyV2).Code; code != http.StatusNotFound {
				t.Errorf("environment %q: %s mounted without the flag, got %d", environment, url, code)
			}
		}
		if database.touched() {
			t.Errorf("environment %q: a disabled route reached persistence", environment)
		}
	}
}

// TestOrganisationFirstV2IsIndependentOfThePEORouteFamilies: each family needs
// its own readiness and activation. Enabling one mounts none of the others.
func TestOrganisationFirstV2IsIndependentOfThePEORouteFamilies(t *testing.T) {
	peoPaths := map[string]string{
		"/v2/client-applications":                       "progressive admission",
		"/v2/founding-governance/sponsorship-proposals": "founding governance",
	}
	t.Run("Organisation-first on, PEO families off", func(t *testing.T) {
		handler := organisationFirstHandler(t, "test", true, &organisationFirstFakeStore{fakeStore: &fakeStore{}}, nil)
		for url, family := range peoPaths {
			if code := registration(handler, url, "{}").Code; code != http.StatusNotFound {
				t.Errorf("enabling Organisation-first v2 mounted %s (%s): %d", url, family, code)
			}
		}
	})
	t.Run("PEO families on, Organisation-first off", func(t *testing.T) {
		handler := New(Dependencies{Store: &fakeStore{}, Environment: "test", AdminVerifier: fakeVerifier{err: errors.New("invalid bearer")},
			ProgressiveApplicationsEnabled: true, ProgressiveApplications: stubProgressiveWriter{},
			FoundingGovernanceEnabled: true, FoundingGovernance: stubFoundingWriter{}})
		if code := registration(handler, "/v2/client-applications", "{}").Code; code != http.StatusUnauthorized {
			t.Fatalf("control: progressive admission should be mounted behind authentication, got %d", code)
		}
		for _, url := range organisationFirstPaths {
			if code := registration(handler, url, "{}").Code; code != http.StatusNotFound {
				t.Errorf("enabling the PEO families mounted Organisation-first %s: %d", url, code)
			}
		}
	})
}

// TestOrganisationFirstV2RefusesEveryoneButAHumanPlatformAdministrator.
func TestOrganisationFirstV2RefusesEveryoneButAHumanPlatformAdministrator(t *testing.T) {
	humanWithoutTenantWrite := registeringAdmin()
	humanWithoutTenantWrite.Scopes = map[string]struct{}{"tenant:read": {}}
	for name, principal := range map[string]auth.Principal{
		"workload identity":              workloadPrincipal(),
		"human without tenant:write":     humanWithoutTenantWrite,
		"tenant administrator":           tenantAdminPrincipal(),
		"unauthenticated (empty bearer)": {},
	} {
		database := &organisationFirstFakeStore{fakeStore: &fakeStore{}}
		handler := New(Dependencies{Store: database, AdminVerifier: fakeVerifier{principal: principal},
			Identities: registeredStaff(t, registeringAdmin()), Onboarding: &onboarding.Service{},
			OrganisationFirstV2: true, Environment: "test"})
		for _, url := range organisationFirstPaths {
			if code := registration(handler, url, registrationBodyV2).Code; code < 400 || code >= 500 {
				t.Errorf("%s: %s must be refused with a client error, got %d", name, url, code)
			}
		}
		if database.touched() {
			t.Errorf("%s: a refused caller reached persistence", name)
		}
	}
}

// TestOrganisationFirstV2CannotBypassAnAuthorisedOnboardingRequest: even where
// the routes are mounted and the caller is a platform administrator, nothing
// is registered without an authorised TenantOnboardingRequest, the service
// that fulfils it, and persistence that supports Organisation-first writes.
func TestOrganisationFirstV2CannotBypassAnAuthorisedOnboardingRequest(t *testing.T) {
	t.Run("no onboarding service", func(t *testing.T) {
		database := &organisationFirstFakeStore{fakeStore: &fakeStore{}}
		handler := organisationFirstHandler(t, "test", true, database, func(d *Dependencies) { d.Onboarding = nil })
		for _, url := range organisationFirstPaths {
			response := registration(handler, url, registrationBodyV2)
			if response.Code != http.StatusServiceUnavailable || problemCode(t, response) != "ONBOARDING_UNAVAILABLE" {
				t.Errorf("%s: got %d %s", url, response.Code, problemCode(t, response))
			}
		}
		if database.touched() {
			t.Error("registration reached persistence without an onboarding service")
		}
	})
	t.Run("persistence without Organisation-first support", func(t *testing.T) {
		handler := organisationFirstHandler(t, "test", true, &fakeStore{}, nil)
		for _, url := range organisationFirstPaths {
			response := registration(handler, url, registrationBodyV2)
			if response.Code != http.StatusServiceUnavailable || problemCode(t, response) != "ORGANISATION_FIRST_UNAVAILABLE" {
				t.Errorf("%s: got %d %s", url, response.Code, problemCode(t, response))
			}
		}
	})
	t.Run("the request cannot choose its own authority", func(t *testing.T) {
		for name, body := range map[string]string{
			"no onboarding request":        strings.Replace(registrationBodyV2, `"tenant_onboarding_request_id":"`+testOnboardingRequestID+`",`, ``, 1),
			"malformed onboarding request": strings.Replace(registrationBodyV2, testOnboardingRequestID, "ACME-1", 1),
			"no Organisation":              strings.Replace(registrationBodyV2, `"organisation_id":"`+testOrganisationID+`",`, ``, 1),
			"caller-supplied tenant_id":    strings.Replace(registrationBodyV2, `{`, `{"tenant_id":"tn_client_supplied",`, 1),
			"caller-supplied basis":        strings.Replace(registrationBodyV2, `{`, `{"basis":"BOOTSTRAP",`, 1),
			"bootstrap fields":             strings.Replace(registrationBodyV2, `{`, `{"bootstrap_reason":"migrating an existing tenant",`, 1),
			"unbound legal entity alias":   strings.Replace(registrationBodyV2, `{`, `{"legal_entity_id":"zuribeans_za",`, 1),
		} {
			database := &organisationFirstFakeStore{fakeStore: &fakeStore{}}
			handler := organisationFirstHandler(t, "test", true, database, nil)
			response := registration(handler, "/v2/tenants", body)
			if response.Code != http.StatusBadRequest || database.registerCalls != 0 {
				t.Errorf("%s: got %d (register calls %d), want 400 and no registration", name, response.Code, database.registerCalls)
			}
		}
	})
	t.Run("refusals from the fulfilment step", func(t *testing.T) {
		for err, want := range map[error]struct {
			status int
			code   string
		}{
			onboarding.ErrTransition:            {http.StatusConflict, "TENANT_ONBOARDING_REQUEST_NOT_AUTHORISED"},
			onboarding.ErrDesiredStateMismatch:  {http.StatusUnprocessableEntity, "DESIRED_STATE_MISMATCH"},
			errors.New("identity not reviewed"): {http.StatusConflict, "ORGANISATION_AUTHORITY_NOT_SATISFIED"},
		} {
			database := &organisationFirstFakeStore{fakeStore: &fakeStore{}, registerErr: err}
			handler := organisationFirstHandler(t, "test", true, database, nil)
			if response := registration(handler, "/v2/tenants", registrationBodyV2); response.Code != want.status || problemCode(t, response) != want.code {
				t.Errorf("%v: got %d %s, want %d %s", err, response.Code, problemCode(t, response), want.status, want.code)
			}
		}
	})
	t.Run("an authorised request registers with a server-minted tenant and no invented legal entity", func(t *testing.T) {
		database := &organisationFirstFakeStore{fakeStore: &fakeStore{}}
		handler := organisationFirstHandler(t, "test", true, database, nil)
		response := registration(handler, "/v2/tenants", registrationBodyV2)
		if response.Code != http.StatusAccepted || database.registerCalls != 1 {
			t.Fatalf("got %d (register calls %d): %s", response.Code, database.registerCalls, response.Body.String())
		}
		got := database.registered
		if got.Basis != domain.RegistrationOnboarding || !database.hadStep || got.TenantOnboardingRequestID != testOnboardingRequestID ||
			got.OrganisationID != testOrganisationID || !domain.ValidTenantID(got.TenantID) {
			t.Fatalf("registration must carry its onboarding request, fulfilment step and a Control Plane-minted tenant: %+v step %v", got, database.hadStep)
		}
		if got.LegalEntityID != "" {
			t.Fatalf("no legal entity was supplied, none may be invented: %q", got.LegalEntityID)
		}
	})
}
