package api

import (
	"context"
	"net/http"
	"net/http/httptest"
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
