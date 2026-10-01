package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

func enforcedPolicy(status string, permissions ...string) *administration.EnforcementPolicy {
	p := &administration.EnforcementPolicy{}
	p.Criteria = administration.EnforcementCriteria{Status: status, MinimumObservationDays: 14, MinimumDecisions: 100}
	p.Critical.Enforcement = "PROHIBITED"
	for _, key := range permissions {
		p.Enforced = append(p.Enforced, administration.EnforcedPermission{Permission: key, ApprovedBy: "prn_owner", ApprovedAt: "2026-10-30T00:00:00Z", EvidenceRef: "readiness-test"})
	}
	return p
}

// TestEnforcedPermissionIsDecidedByGrantsAndFailsClosed: once the owner's
// policy lists a permission, AdministrativeGrants decide it, a role no longer
// suffices, and every failure refuses. Other permissions keep their roles.
func TestEnforcedPermissionIsDecidedByGrantsAndFailsClosed(t *testing.T) {
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()
	ids := map[string]string{}
	for _, subject := range []string{"roleonly", "granted", "grantonly", "assured"} {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
		ids[subject] = p.ID
	}
	now := time.Now().UTC()
	grant := func(id, principal, permission string, risk administration.RiskClass) administration.Grant {
		return administration.Grant{GrantID: id, PrincipalID: principal, Permission: permission,
			Scope: administration.Scope{Level: administration.LevelPlatform}, GrantType: administration.TypeStanding,
			Source: administration.SourceDirect, RiskClass: risk, ValidFrom: now.Add(-time.Hour),
			Status: administration.StatusActive, GrantedBy: "prn_platformops", Reason: "test", Version: 1}
	}
	grants := grantsFake{
		ids["granted"]:   {grant("agr_granted01", ids["granted"], "support.diagnostics.view", administration.RiskModerate)},
		ids["grantonly"]: {grant("agr_grantonly1", ids["grantonly"], "support.diagnostics.view", administration.RiskModerate)},
		ids["assured"]:   {grant("agr_assured001", ids["assured"], "support.diagnostics.view", administration.RiskHigh)},
	}
	principal := func(subject string, platformAdmin bool, acr string) auth.Principal {
		p := auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject,
			Scopes: map[string]struct{}{"capabilities:explain": {}}, Roles: map[string]struct{}{}, Assurance: auth.Assurance{ACR: acr}}
		if platformAdmin {
			p.Roles[RolePlatformAdmin] = struct{}{}
		}
		return p
	}
	verifier := tokenVerifier{
		"roleonly": principal("roleonly", true, "1"), "granted": principal("granted", true, "1"),
		"grantonly": principal("grantonly", false, "1"), "stranger": principal("stranger", true, "1"),
		"assured": principal("assured", true, "1"), "steppedup": principal("assured", true, "2"),
	}
	build := func(e *administration.Enforcement, identityStore repository.IdentityRepository) http.Handler {
		return New(Dependencies{Store: &fakeStore{}, AdminVerifier: verifier, Identities: identityStore, AdministrativeGrants: grants, enforcement: e})
	}
	call := func(h http.Handler, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	catalogue := administration.MustDefaultCatalogue()
	enforce := func(status string, rollback ...string) *administration.Enforcement {
		return administration.NewEnforcement(enforcedPolicy(status, "support.diagnostics.view"), catalogue, rollback)
	}
	enforced := build(enforce("APPROVED"), identities)
	count := func(result string) uint64 {
		return metrics.AdministrativeAuthorityEnforced.Value("support.diagnostics.view", result)
	}
	allowedBefore, deniedBefore := count("allowed"), count("denied")

	// A role without a grant is no longer enough.
	if w := call(enforced, "roleonly"); w.Code != http.StatusForbidden || problemCode(t, w) != "AUTHORIZATION_DENIED" {
		t.Errorf("a role without a grant must be refused under enforcement: %d %s", w.Code, w.Body.String())
	}
	// A grant decides, with or without the role.
	if w := call(enforced, "granted"); w.Code == http.StatusForbidden {
		t.Errorf("a role and a grant must pass: %d", w.Code)
	}
	if w := call(enforced, "grantonly"); w.Code == http.StatusForbidden {
		t.Errorf("a grant alone decides under enforcement: %d %s", w.Code, w.Body.String())
	}
	// A caller with no Control Plane principal is refused.
	if w := call(enforced, "stranger"); w.Code != http.StatusForbidden {
		t.Errorf("an unresolved caller must be refused: %d", w.Code)
	}
	// A HIGH grant needs the stepped-up session; the refusal says so.
	if w := call(enforced, "assured"); w.Code != http.StatusForbidden || problemCode(t, w) != "AUTHENTICATION_ASSURANCE_INSUFFICIENT" {
		t.Errorf("a password session on a HIGH grant must be asked to step up: %d %s", w.Code, w.Body.String())
	}
	if w := call(enforced, "steppedup"); w.Code == http.StatusForbidden {
		t.Errorf("a stepped-up session must pass: %d %s", w.Code, w.Body.String())
	}
	// A failing identity store refuses, retryably; it never falls back to roles.
	if w := call(build(enforce("APPROVED"), failingIdentities{}), "granted"); w.Code != http.StatusServiceUnavailable || problemCode(t, w) != "AUTH_VERIFIER_UNAVAILABLE" {
		t.Errorf("a store failure under enforcement must refuse as unavailable: %d %s", w.Code, w.Body.String())
	}
	if count("allowed") != allowedBefore+3 || count("denied") < deniedBefore+3 {
		t.Errorf("enforced decisions are counted: allowed %d->%d denied %d->%d", allowedBefore, count("allowed"), deniedBefore, count("denied"))
	}

	// Everything that returns authority to roles.
	for name, e := range map[string]*administration.Enforcement{
		"criteria not approved":                enforce("PROPOSED"),
		"rolled back by permission":            enforce("APPROVED", "support.diagnostics.view"),
		"rolled back for all":                  enforce("APPROVED", "*"),
		"another permission enforced":          administration.NewEnforcement(enforcedPolicy("APPROVED", "tenant.view"), catalogue, nil),
		"nothing enforced (the state shipped)": administration.NewEnforcement(enforcedPolicy("APPROVED"), catalogue, nil),
	} {
		h := build(e, identities)
		if w := call(h, "roleonly"); w.Code == http.StatusForbidden {
			t.Errorf("%s: the role decision must stand: %d", name, w.Code)
		}
		if w := call(h, "grantonly"); w.Code != http.StatusForbidden {
			t.Errorf("%s: a grant alone is not authority while roles decide: %d", name, w.Code)
		}
	}
	// With no grant store, enforcement refuses instead of reverting to roles.
	noStore := New(Dependencies{Store: &fakeStore{}, AdminVerifier: verifier, Identities: identities, enforcement: enforce("APPROVED")})
	if w := call(noStore, "roleonly"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("enforcement without a grant store must refuse: %d", w.Code)
	}
}
