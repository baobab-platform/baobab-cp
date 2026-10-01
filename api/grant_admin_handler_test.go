package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// grantAdminFake keeps grants in memory but applies the same rules as the
// Postgres repository: the statuses a transition may reach come from
// administration.TransitionTarget, versions are checked, and a delegation
// is planned against the stored source.
type grantAdminFake struct {
	relations administration.Relations
	grants    map[string]administration.Grant
	order     []string
	keys      map[string]string
}

func newGrantAdminFake() *grantAdminFake {
	return &grantAdminFake{grants: map[string]administration.Grant{}, keys: map[string]string{}}
}

func (f *grantAdminFake) put(g administration.Grant) {
	f.grants[g.GrantID] = g
	f.order = append(f.order, g.GrantID)
}

func (f *grantAdminFake) AdministrativeGrantsOf(_ context.Context, principalID string) ([]administration.Grant, map[string]administration.Grant, error) {
	var out []administration.Grant
	for _, id := range f.order {
		if g := f.grants[id]; g.PrincipalID == principalID {
			out = append(out, g)
		}
	}
	return out, f.grants, nil
}

func (f *grantAdminFake) EffectiveRelations(context.Context, []string, time.Time) (administration.Relations, error) {
	return f.relations, nil
}

func (f *grantAdminFake) GetAdministrativeGrant(_ context.Context, id string) (administration.Grant, error) {
	g, ok := f.grants[id]
	if !ok {
		return administration.Grant{}, repository.ErrGrantNotFound
	}
	return g, nil
}

func (f *grantAdminFake) ListAdministrativeGrants(_ context.Context, filter repository.GrantFilter, _ int, _ string) ([]administration.Grant, string, error) {
	var out []administration.Grant
	for _, id := range f.order {
		g := f.grants[id]
		if (filter.PrincipalID == "" || g.PrincipalID == filter.PrincipalID) && (filter.Status == "" || g.Status == filter.Status) {
			out = append(out, g)
		}
	}
	return out, "", nil
}

func (f *grantAdminFake) replay(actor repository.AuditActor, key, hash string) (administration.Grant, bool, error) {
	if prior, ok := f.keys[actor.ActorID+key]; ok {
		id, h, _ := strings.Cut(prior, "|")
		if h != hash {
			return administration.Grant{}, false, repository.ErrGrantCommandKeyReused
		}
		return f.grants[id], true, nil
	}
	return administration.Grant{}, false, nil
}

func (f *grantAdminFake) IssueAdministrativeGrant(_ context.Context, actor repository.AuditActor, key, hash string, g administration.Grant) (administration.Grant, error) {
	if prior, ok, err := f.replay(actor, key, hash); ok || err != nil {
		return prior, err
	}
	g.GrantID = domain.NewResourceID("agr")
	f.put(g)
	f.keys[actor.ActorID+key] = g.GrantID + "|" + hash
	return g, nil
}

func (f *grantAdminFake) DelegateAdministrativeGrant(_ context.Context, actor repository.AuditActor, key, hash, sourceID string,
	plan func(administration.Grant, map[string]administration.Grant) (administration.Grant, error)) (administration.Grant, error) {
	if prior, ok, err := f.replay(actor, key, hash); ok || err != nil {
		return prior, err
	}
	source, ok := f.grants[sourceID]
	if !ok {
		return administration.Grant{}, repository.ErrGrantNotFound
	}
	g, err := plan(source, f.grants)
	if err != nil {
		return administration.Grant{}, err
	}
	g.GrantID = domain.NewResourceID("agr")
	f.put(g)
	f.keys[actor.ActorID+key] = g.GrantID + "|" + hash
	return g, nil
}

func (f *grantAdminFake) TransitionAdministrativeGrant(_ context.Context, actor repository.AuditActor, key, hash, id string,
	command administration.Command, _ string, version int64, now time.Time) (administration.Grant, error) {
	if prior, ok, err := f.replay(actor, key, hash); ok || err != nil {
		return prior, err
	}
	g, ok := f.grants[id]
	if !ok {
		return administration.Grant{}, repository.ErrGrantNotFound
	}
	if g.Version != version {
		return administration.Grant{}, repository.ErrGrantVersionMismatch
	}
	to, err := administration.TransitionTarget(g, command, now)
	if err != nil {
		return administration.Grant{}, err
	}
	g.Status, g.Version = to, g.Version+1
	f.grants[id] = g
	f.keys[actor.ActorID+key] = id + "|" + hash
	return g, nil
}

func (f *grantAdminFake) ReplaceAdministrativeGrant(_ context.Context, actor repository.AuditActor, key, hash, id string, version int64,
	reason string, now time.Time, plan func(administration.Grant) (administration.Grant, error)) (repository.GrantReplacement, error) {
	if prior, ok, err := f.replay(actor, key, hash); ok || err != nil {
		old := f.grants[prior.SupersedesGrantID]
		return repository.GrantReplacement{Replacement: prior, Superseded: old, RevokedDelegations: []string{}}, err
	}
	old, ok := f.grants[id]
	if !ok {
		return repository.GrantReplacement{}, repository.ErrGrantNotFound
	}
	if old.Version != version {
		return repository.GrantReplacement{}, repository.ErrGrantVersionMismatch
	}
	next, err := plan(old)
	if err != nil {
		return repository.GrantReplacement{}, err
	}
	next.GrantID = domain.NewResourceID("agr")
	f.put(next)
	old.Status, old.SupersededByGrantID, old.Version = administration.StatusRevoked, next.GrantID, old.Version+1
	old.RevokedAt, old.RevokedBy, old.RevocationReason = &now, actor.ActorID, "replaced: "+reason
	f.grants[id] = old
	f.keys[actor.ActorID+key] = next.GrantID + "|" + hash
	return repository.GrantReplacement{Replacement: next, Superseded: old, RevokedDelegations: []string{}}, nil
}

func (f *grantAdminFake) SweepAdministrativeGrants(context.Context, time.Time) (int, int, error) {
	return 0, 0, nil
}

// TestGrantAdministrationAPI exercises the ADA-05 routes end to end through
// the router: who may call, what is issued directly, what waits for
// approval, how a transition and a delegation are refused or applied, and
// that responses are the contract's AdministrativeGrant.
func TestGrantAdministrationAPI(t *testing.T) {
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()
	ids := map[string]string{}
	for _, subject := range []string{"ops", "jane", "bob", "dormant"} {
		status := "ACTIVE"
		if subject == "dormant" {
			status = "SUSPENDED"
		}
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: status}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
		ids[subject] = p.ID
	}
	now := time.Now().UTC()
	store := newGrantAdminFake()
	tenant := administration.Scope{Level: administration.LevelTenant, TenantID: "tn_acmeug"}
	// Jane holds a delegable tenant.view and the authority to delegate; Bob holds neither.
	source := administration.Grant{GrantID: "agr_janesource", PrincipalID: ids["jane"], Permission: "tenant.view", Scope: tenant,
		GrantType: administration.TypeStanding, Source: administration.SourceDirect, DelegableDepth: 1, RiskClass: administration.RiskLow,
		ValidFrom: now.Add(-time.Hour), Status: administration.StatusActive, GrantedBy: ids["ops"], Reason: "seed", CreatedAt: now, Version: 1}
	delegate := source
	delegate.GrantID, delegate.Permission, delegate.DelegableDepth, delegate.RiskClass = "agr_janedelegt", "administrator.delegate", 0, administration.RiskHigh
	store.put(source)
	store.put(delegate)

	principal := func(subject string, roles bool, scopes ...string) auth.Principal {
		p := auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject,
			Scopes: map[string]struct{}{}, Roles: map[string]struct{}{}, Assurance: auth.Assurance{ACR: "2", AuthenticatedAt: time.Now()}}
		if roles {
			p.Roles[RolePlatformAdmin] = struct{}{}
		}
		for _, s := range scopes {
			p.Scopes[s] = struct{}{}
		}
		return p
	}
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: tokenVerifier{
		"ops":         principal("ops", true, "administrator:read", "administrator:write"),
		"opsreadonly": principal("ops", true, "administrator:read"),
		"norole":      principal("ops", false, "administrator:read", "administrator:write"),
		"jane":        principal("jane", false, "administrator:write"),
		"janepassword": func() auth.Principal {
			p := principal("jane", false, "administrator:write")
			p.Assurance = auth.Assurance{ACR: "1", AuthenticatedAt: time.Now()}
			return p
		}(),
		"bob":     principal("bob", false, "administrator:write"),
		"dormant": principal("dormant", true, "administrator:read", "administrator:write"),
	}, Identities: identities, AdministrativeGrants: store, AdministrativeGrantAdmin: store})

	var counter int
	call := func(token, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if s, ok := body.(string); ok {
			reader = bytes.NewReader([]byte(s))
		} else {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		request := httptest.NewRequest(method, path, reader)
		request.Header.Set("Authorization", "Bearer "+token)
		if method == http.MethodPost {
			counter++
			request.Header.Set("Idempotency-Key", "idem-key-"+strings.Repeat("0", 8)+string(rune('a'+counter%26))+string(rune('a'+counter/26)))
		}
		for k, v := range headers {
			if v == "" {
				request.Header.Del(k)
			} else {
				request.Header.Set(k, v)
			}
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	expect := func(r *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if r.Code != status || (code != "" && problemCode(t, r) != code) {
			t.Fatalf("want %d %s, got %d %s", status, code, r.Code, r.Body.String())
		}
	}
	validateGrant := func(r *httptest.ResponseRecorder) administration.Grant {
		t.Helper()
		var g administration.Grant
		mustNoError(t, json.Unmarshal(r.Body.Bytes(), &g))
		if dir := os.Getenv("SHARED_CONTRACTS_DIR"); dir != "" {
			var raw map[string]any
			mustNoError(t, json.Unmarshal(r.Body.Bytes(), &raw))
			contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "administration/v1/grant.schema.json#/$defs/AdministrativeGrant"), raw)
		}
		return g
	}
	issueBody := func(principal, permission string) map[string]any {
		return map[string]any{"principal_id": principal, "permission": permission, "grant_type": "STANDING",
			"scope": map[string]any{"level": "TENANT", "tenant_id": "tn_acmeug"}, "reason": "test"}
	}

	// Who may call: the scope, and the platform administrator role.
	expect(call("opsreadonly", http.MethodPost, "/v1/admin/grants", issueBody(ids["bob"], "tenant.view"), nil), http.StatusForbidden, "AUTHORIZATION_DENIED")
	expect(call("norole", http.MethodPost, "/v1/admin/grants", issueBody(ids["bob"], "tenant.view"), nil), http.StatusForbidden, "AUTHORIZATION_DENIED")
	expect(call("dormant", http.MethodGet, "/v1/admin/grants", nil, nil), http.StatusOK, "")

	// Issuing.
	created := call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["bob"], "tenant.view"), nil)
	expect(created, http.StatusCreated, "")
	g := validateGrant(created)
	if g.Source != administration.SourceDirect || g.GrantedBy != ids["ops"] || created.Header().Get("ETag") != `"1"` ||
		created.Header().Get("Location") != "/v1/admin/grants/"+g.GrantID {
		t.Fatalf("unexpected issue response %+v headers %v", g, created.Header())
	}
	expect(call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["ops"], "tenant.view"), nil), http.StatusForbidden, "SELF_APPROVAL_PROHIBITED")
	expect(call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["bob"], "tenant.suspend"), nil), http.StatusConflict, "APPROVAL_REQUIRED")
	expect(call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["bob"], "tenant.own"), nil), http.StatusUnprocessableEntity, "INVALID_GRANT")
	expect(call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["dormant"], "tenant.view"), nil), http.StatusUnprocessableEntity, "PRINCIPAL_INACTIVE")
	expect(call("ops", http.MethodPost, "/v1/admin/grants", issueBody("prn_nobody0001", "tenant.view"), nil), http.StatusUnprocessableEntity, "INVALID_GRANT")
	expect(call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["bob"], "tenant.view"), map[string]string{"Idempotency-Key": ""}), http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY")
	expect(call("ops", http.MethodPost, "/v1/admin/grants", `{"principal_id":"x","unknown":1}`, nil), http.StatusBadRequest, "INVALID_REQUEST")
	// A replayed key returns the same grant; the same key for another request is refused.
	fixed := map[string]string{"Idempotency-Key": "idem-fixed-key-0001"}
	first := call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["jane"], "market.view"), fixed)
	expect(first, http.StatusCreated, "")
	second := call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["jane"], "market.view"), fixed)
	expect(second, http.StatusCreated, "")
	if validateGrant(first).GrantID != validateGrant(second).GrantID {
		t.Fatal("a replayed issue must return the same grant")
	}
	expect(call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["jane"], "tenant.view"), fixed), http.StatusConflict, "IDEMPOTENCY_KEY_REUSED")

	// Inspecting.
	expect(call("ops", http.MethodGet, "/v1/admin/grants/"+g.GrantID, nil, nil), http.StatusOK, "")
	expect(call("ops", http.MethodGet, "/v1/admin/grants/agr_missing0001", nil, nil), http.StatusNotFound, "ADMINISTRATIVE_GRANT_NOT_FOUND")
	expect(call("ops", http.MethodGet, "/v1/admin/grants/not-an-id", nil, nil), http.StatusNotFound, "ADMINISTRATIVE_GRANT_NOT_FOUND")
	list := call("ops", http.MethodGet, "/v1/admin/grants?principal_id="+ids["bob"]+"&status=ACTIVE", nil, nil)
	expect(list, http.StatusOK, "")
	if dir := os.Getenv("SHARED_CONTRACTS_DIR"); dir != "" {
		var raw map[string]any
		mustNoError(t, json.Unmarshal(list.Body.Bytes(), &raw))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "administration/v1/grant-administration.schema.json#/$defs/GrantPage"), raw)
	}
	expect(call("ops", http.MethodGet, "/v1/admin/grants?status=BOGUS", nil, nil), http.StatusBadRequest, "INVALID_REQUEST")
	expect(call("ops", http.MethodGet, "/v1/admin/grants?limit=0", nil, nil), http.StatusBadRequest, "INVALID_REQUEST")

	// Transitions.
	path := "/v1/admin/grants/" + g.GrantID + "/transitions"
	suspend := map[string]any{"command": "suspend", "reason": "review"}
	expect(call("ops", http.MethodPost, path, suspend, nil), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED")
	expect(call("ops", http.MethodPost, path, suspend, map[string]string{"If-Match": `"9"`}), http.StatusPreconditionFailed, "GRANT_VERSION_MISMATCH")
	expect(call("ops", http.MethodPost, path, map[string]any{"command": "activate", "reason": "x"}, map[string]string{"If-Match": `"1"`}), http.StatusUnprocessableEntity, "INVALID_GRANT")
	expect(call("ops", http.MethodPost, path, map[string]any{"command": "withdraw", "reason": "x"}, map[string]string{"If-Match": `"1"`}), http.StatusConflict, "GRANT_TRANSITION_INVALID")
	done := call("ops", http.MethodPost, path, suspend, map[string]string{"If-Match": `"1"`})
	expect(done, http.StatusOK, "")
	if after := validateGrant(done); after.Status != administration.StatusSuspended || done.Header().Get("ETag") != `"2"` {
		t.Fatalf("unexpected transition result %+v", after)
	}
	expect(call("ops", http.MethodPost, "/v1/admin/grants/agr_missing0001/transitions", suspend, map[string]string{"If-Match": `"1"`}), http.StatusNotFound, "ADMINISTRATIVE_GRANT_NOT_FOUND")

	// Replacement (Shared replaceAdministrativeGrant): atomic, linked, never an amend.
	repBody := func(permission string) map[string]any {
		return map[string]any{"permission": permission, "grant_type": "STANDING", "dependent_delegations": "REVOKE", "reason": "Narrowing.",
			"scope": map[string]any{"level": "TENANT", "tenant_id": "tn_acmeug"}}
	}
	narrower := call("ops", http.MethodPost, "/v1/admin/grants", issueBody(ids["bob"], "market.view"), nil)
	expect(narrower, http.StatusCreated, "")
	target := validateGrant(narrower)
	rpath := "/v1/admin/grants/" + target.GrantID + "/replacements"
	etag1 := map[string]string{"If-Match": `"1"`}
	expect(call("ops", http.MethodPost, rpath, repBody("market.view"), nil), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED")
	expect(call("ops", http.MethodPost, rpath, repBody("market.view"), map[string]string{"If-Match": `"9"`}), http.StatusPreconditionFailed, "GRANT_VERSION_MISMATCH")
	bad := repBody("market.view")
	bad["dependent_delegations"] = "RETAIN"
	expect(call("ops", http.MethodPost, rpath, bad, etag1), http.StatusUnprocessableEntity, "INVALID_GRANT")
	// A replacement that adds HIGH authority is a changeset, not a direct route.
	expect(call("ops", http.MethodPost, rpath, repBody("tenant.suspend"), etag1), http.StatusConflict, "APPROVAL_REQUIRED")
	expect(call("ops", http.MethodPost, "/v1/admin/grants/agr_missing0001/replacements", repBody("market.view"), etag1), http.StatusNotFound, "ADMINISTRATIVE_GRANT_NOT_FOUND")
	replacedResp := call("ops", http.MethodPost, rpath, repBody("tenant.view"), etag1)
	expect(replacedResp, http.StatusCreated, "")
	var replaced struct {
		Replacement administration.Grant `json:"replacement"`
		Superseded  administration.Grant `json:"superseded"`
		Revoked     []string             `json:"revoked_delegations"`
	}
	mustNoError(t, json.Unmarshal(replacedResp.Body.Bytes(), &replaced))
	if replaced.Replacement.SupersedesGrantID != target.GrantID || replaced.Superseded.SupersededByGrantID != replaced.Replacement.GrantID ||
		replaced.Superseded.Status != administration.StatusRevoked || replaced.Replacement.Status != administration.StatusActive || replaced.Revoked == nil {
		t.Fatalf("unexpected replacement %+v", replaced)
	}
	if dir := os.Getenv("SHARED_CONTRACTS_DIR"); dir != "" {
		var raw map[string]any
		mustNoError(t, json.Unmarshal(replacedResp.Body.Bytes(), &raw))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "administration/v1/grant-administration.schema.json#/$defs/GrantReplacement"), raw)
	}
	// The replaced grant cannot be replaced again.
	expect(call("ops", http.MethodPost, rpath, repBody("tenant.view"), map[string]string{"If-Match": `"2"`}), http.StatusConflict, "GRANT_REPLACEMENT_INVALID")

	// Delegation is decided by the caller's own grants, not by any role.
	delegation := func(to string) map[string]any {
		return map[string]any{"principal_id": to, "permission": "tenant.view", "scope": map[string]any{"level": "TENANT", "tenant_id": "tn_acmeug"},
			"valid_until": now.Add(time.Hour).Format(time.RFC3339), "reason": "cover"}
	}
	dpath := "/v1/admin/grants/agr_janesource/delegations"
	expect(call("bob", http.MethodPost, dpath, delegation(ids["ops"]), nil), http.StatusForbidden, "NO_ADMINISTRATIVE_GRANT")
	// administrator.delegate is HIGH risk: a password session is asked to
	// step up (section 72) even though the grant is usable, and the step-up
	// session is not (section 74: assurance proves nothing without a grant).
	expect(call("janepassword", http.MethodPost, dpath, delegation(ids["ops"]), nil), http.StatusForbidden, "AUTHENTICATION_ASSURANCE_INSUFFICIENT")
	expect(call("jane", http.MethodPost, dpath, delegation(ids["jane"]), nil), http.StatusForbidden, "SELF_APPROVAL_PROHIBITED")
	expect(call("jane", http.MethodPost, dpath, delegation(ids["dormant"]), nil), http.StatusUnprocessableEntity, "PRINCIPAL_INACTIVE")
	wide := delegation(ids["bob"])
	wide["valid_until"] = now.Add(time.Hour).Format(time.RFC3339)
	wide["scope"] = map[string]any{"level": "PLATFORM"}
	expect(call("jane", http.MethodPost, dpath, wide, nil), http.StatusForbidden, "SCOPE_MISMATCH")
	made := call("jane", http.MethodPost, dpath, delegation(ids["bob"]), nil)
	expect(made, http.StatusCreated, "")
	if d := validateGrant(made); d.Source != administration.SourceDelegation || d.DelegatedFromGrantID != "agr_janesource" || d.GrantedBy != ids["jane"] {
		t.Fatalf("unexpected delegation %+v", d)
	}
	// A provably narrower scope (here one environment) is delegable, and the
	// delegation keeps it. A scope the source does not contain is not.
	inProduction := delegation(ids["ops"])
	inProduction["scope"] = map[string]any{"level": "TENANT", "tenant_id": "tn_acmeug", "environment": "production"}
	narrowed := call("jane", http.MethodPost, dpath, inProduction, nil)
	expect(narrowed, http.StatusCreated, "")
	if d := validateGrant(narrowed); d.Scope.Environment != "production" || d.DelegatedFromGrantID != "agr_janesource" {
		t.Fatalf("the narrower delegation lost its scope: %+v", d)
	}
	elsewhere := delegation(ids["ops"])
	elsewhere["scope"] = map[string]any{"level": "TENANT", "tenant_id": "tn_other"}
	expect(call("jane", http.MethodPost, dpath, elsewhere, nil), http.StatusForbidden, "SCOPE_MISMATCH")
	// Bob's delegation cannot be delegated further: the source allowed one hop.
	bobsPath := "/v1/admin/grants/" + validateGrant(made).GrantID + "/delegations"
	expect(call("bob", http.MethodPost, bobsPath, delegation(ids["ops"]), nil), http.StatusForbidden, "NO_ADMINISTRATIVE_GRANT")
}
