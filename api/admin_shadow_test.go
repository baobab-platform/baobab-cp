package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// TestEveryGuardedRouteIsMapped: every route behind requireAdminRole names
// the permission it performs (or is explicitly marked unregistered), every
// named permission is registered in Shared, and the table names no route
// the router lacks.
func TestEveryGuardedRouteIsMapped(t *testing.T) {
	catalogue := administration.MustDefaultCatalogue()
	routes := map[string]bool{}
	guarded := 0
	sentinel := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	err := chi.Walk(fullRouter(t), func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		routes[key] = true
		for _, mw := range middlewares {
			if _, gate := mw(sentinel).(adminGate); gate {
				guarded++
				if _, ok := adminRoutePermissions[key]; !ok {
					t.Errorf("%s is role-guarded but maps to no permission", key)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if guarded < 50 {
		t.Fatalf("found only %d role-guarded routes; the walk is not seeing requireAdminRole", guarded)
	}
	for key, permission := range adminRoutePermissions {
		if !routes[key] {
			t.Errorf("%s is mapped but no such route exists", key)
		}
		if _, ok := catalogue.Permission(permission); permission != "" && !ok {
			t.Errorf("%s maps to unregistered permission %q", key, permission)
		}
	}
}

// TestShadowEvaluationNeverChangesTheDecision: the legacy role decision
// stands whatever grants say; the comparison is only counted.
func TestShadowEvaluationNeverChangesTheDecision(t *testing.T) {
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()
	ids := map[string]string{}
	for _, subject := range []string{"admin", "granted", "tenantadmin"} {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
		ids[subject] = p.ID
	}
	now := time.Now().UTC()
	platformGrant := func(id, principal string) administration.Grant {
		return administration.Grant{GrantID: id, PrincipalID: principal, Permission: "support.diagnostics.view",
			Scope: administration.Scope{Level: administration.LevelPlatform}, GrantType: administration.TypeStanding,
			Source: administration.SourceDirect, RiskClass: administration.RiskModerate, ValidFrom: now.Add(-time.Hour),
			Status: administration.StatusActive, GrantedBy: "prn_platformops", Reason: "test", Version: 1}
	}
	grants := grantsFake{
		ids["granted"]:     {platformGrant("agr_granted1", ids["granted"])},
		ids["tenantadmin"]: {platformGrant("agr_tenantadm", ids["tenantadmin"])},
	}
	principal := func(subject, role string) auth.Principal {
		return auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject,
			Scopes: map[string]struct{}{"capabilities:explain": {}}, Roles: map[string]struct{}{role: {}}}
	}
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: tokenVerifier{
		"admin": principal("admin", RolePlatformAdmin), "granted": principal("granted", RolePlatformAdmin),
		"tenantadmin": principal("tenantadmin", RoleTenantAdmin),
	}, Identities: identities, AdministrativeGrants: grants})
	count := func(legacy, grantsOutcome, agreement string) uint64 {
		return metrics.AdministrativeAuthorityShadow.Value("support.diagnostics.view", legacy, grantsOutcome, agreement)
	}
	call := func(token string) int {
		request := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	narrower, agree, broader := count("allow", "deny", "grants_narrower"), count("allow", "allow", "agree"), count("deny", "allow", "grants_broader")

	// A platform administrator without grants: the role still allows (the
	// handler runs); grants would deny, counted as narrower.
	if code := call("admin"); code == http.StatusForbidden {
		t.Fatalf("the legacy role decision changed: %d", code)
	}
	// A platform administrator with the grant: both allow.
	call("granted")
	// A tenant administrator on a platform-only route with a platform
	// grant: the role check denies, and still denies, while grants would
	// allow. Counted as broader.
	if code := call("tenantadmin"); code != http.StatusForbidden {
		t.Fatalf("shadow evaluation widened access: %d", code)
	}
	if count("allow", "deny", "grants_narrower") != narrower+1 || count("allow", "allow", "agree") != agree+1 ||
		count("deny", "allow", "grants_broader") != broader+1 {
		t.Fatalf("shadow counts: narrower %d->%d agree %d->%d broader %d->%d",
			narrower, count("allow", "deny", "grants_narrower"), agree, count("allow", "allow", "agree"),
			broader, count("deny", "allow", "grants_broader"))
	}
}

// failingIdentities is an identity store that is down.
type failingIdentities struct{ repository.IdentityRepository }

func (failingIdentities) ResolveIdentity(context.Context, string, string) (domain.Principal, error) {
	return domain.Principal{}, errors.New("connection refused")
}

// routed returns a request carrying the chi route pattern and parameters a
// guarded route would.
func routed(method, pattern, target, body string, params map[string]string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	rc := chi.NewRouteContext()
	rc.RoutePatterns = []string{pattern}
	for k, v := range params {
		rc.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}

// TestShadowSeparatesStoreFailuresAndUnanchoredScopes: an identity store
// failure is an error, never an absent principal; and a grant at a level the
// route did not anchor is not compared, never counted as narrower.
func TestShadowSeparatesStoreFailuresAndUnanchoredScopes(t *testing.T) {
	ctx := context.Background()
	principal := auth.Principal{Subject: "orgadmin", Issuer: testRealm, ActorType: "human"}
	tenantView := routed(http.MethodGet, "/v1/tenants/{tenantID}", "/v1/tenants/ten_1", "", map[string]string{"tenantID": "ten_1"})

	down := &API{identities: failingIdentities{}, grants: grantsFake{}}
	if outcome, comparable := down.shadowGrants(ctx, tenantView, principal, "tenant.view"); outcome != metrics.ShadowError || !comparable {
		t.Fatalf("store failure: %s comparable=%v", outcome, comparable)
	}

	identities := repository.NewInMemoryRepository()
	p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, p))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
		PrincipalID: p.ID, Issuer: testRealm, Subject: "orgadmin", Status: "ACTIVE"}))
	grant := func(level administration.ScopeLevel, scope administration.Scope) administration.Grant {
		scope.Level = level
		return administration.Grant{GrantID: "agr_orgadmin1", PrincipalID: p.ID, Permission: "tenant.view", Scope: scope,
			GrantType: administration.TypeStanding, Source: administration.SourceDirect, RiskClass: administration.RiskLow,
			ValidFrom: time.Now().Add(-time.Hour), Status: administration.StatusActive, GrantedBy: "prn_platformops", Reason: "test", Version: 1}
	}
	// The tenant route does not resolve its organisation, so an
	// organisation grant may cover it: not comparable.
	orgScoped := &API{identities: identities, grants: grantsFake{p.ID: {grant(administration.LevelOrganisation, administration.Scope{OrganisationID: "org_1"})}}}
	if outcome, comparable := orgScoped.shadowGrants(ctx, tenantView, principal, "tenant.view"); outcome != metrics.ShadowDeny || comparable {
		t.Fatalf("unanchored organisation grant: %s comparable=%v", outcome, comparable)
	}
	// A grant for another tenant is judged: the route names its tenant.
	otherTenant := &API{identities: identities, grants: grantsFake{p.ID: {grant(administration.LevelTenant, administration.Scope{TenantID: "ten_2"})}}}
	if outcome, comparable := otherTenant.shadowGrants(ctx, tenantView, principal, "tenant.view"); outcome != metrics.ShadowDeny || !comparable {
		t.Fatalf("other tenant grant: %s comparable=%v", outcome, comparable)
	}
}

// TestShadowResolvesTheBindingAccount: the binding routes name their
// platform account in the body or through the active binding, and the
// handler still reads the body it was sent.
func TestShadowResolvesTheBindingAccount(t *testing.T) {
	body := `{"platform_account_id":"pac_1","reason":"contract"}`
	bind := routed(http.MethodPost, "/v1/tenants/{tenantID}/platform-account-binding", "/v1/tenants/ten_1/platform-account-binding", body,
		map[string]string{"tenantID": "ten_1"})
	a := &API{environment: "production"}
	res, err := a.shadowResource(context.Background(), bind)
	mustNoError(t, err)
	if res.PlatformAccountID != "pac_1" || res.TenantID != "ten_1" || res.Environment != "production" {
		t.Fatalf("bind resource: %+v", res)
	}
	if rest, _ := io.ReadAll(bind.Body); string(rest) != body {
		t.Fatalf("the handler would read %q", rest)
	}

	end := routed(http.MethodPost, "/v1/tenants/{tenantID}/platform-account-binding/end", "/v1/tenants/ten_1/platform-account-binding/end", "{}",
		map[string]string{"tenantID": "ten_1"})
	a.platformAccounts = bindingsFake{{TenantID: "ten_1", PlatformAccountID: "pac_old", Status: "ENDED"}, {TenantID: "ten_1", PlatformAccountID: "pac_2", Status: "ACTIVE"}}
	res, err = a.shadowResource(context.Background(), end)
	mustNoError(t, err)
	if res.PlatformAccountID != "pac_2" {
		t.Fatalf("end resource: %+v", res)
	}
}

type bindingsFake []domain.TenantPlatformAccountBinding

func (f bindingsFake) GetPlatformAccount(context.Context, string) (domain.PlatformAccount, error) {
	return domain.PlatformAccount{}, errors.New("not found")
}

func (f bindingsFake) ChangePlatformAccountStatus(context.Context, string, string, string, string, time.Time, repository.AuditActor) (domain.PlatformAccount, bool, error) {
	return domain.PlatformAccount{}, false, errors.New("read only")
}

func (f bindingsFake) BindTenantPlatformAccount(context.Context, string, string, string, string, string, time.Time, repository.AuditActor) (domain.TenantPlatformAccountBinding, bool, error) {
	return domain.TenantPlatformAccountBinding{}, false, errors.New("read only")
}

func (f bindingsFake) EndTenantPlatformAccountBinding(context.Context, string, string, string, time.Time, repository.AuditActor) (domain.TenantPlatformAccountBinding, error) {
	return domain.TenantPlatformAccountBinding{}, errors.New("read only")
}

func (f bindingsFake) ListTenantPlatformAccountBindings(context.Context, string) ([]domain.TenantPlatformAccountBinding, error) {
	return f, nil
}
