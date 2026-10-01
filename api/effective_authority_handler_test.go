package api

import (
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

type grantsFake map[string][]administration.Grant

func (f grantsFake) EffectiveRelations(context.Context, []string, time.Time) (administration.Relations, error) {
	return administration.Relations{}, nil
}

func (f grantsFake) AdministrativeGrantsOf(_ context.Context, principalID string) ([]administration.Grant, map[string]administration.Grant, error) {
	return f[principalID], nil, nil
}

// TestEffectiveAuthorityReportsOnlyTheCallersUsableGrants: the read model
// is built from grants alone (ADR-BCP-020 sections 99-102, 110). An IAM
// role confers nothing here, another principal's grants never appear, and
// suspended or expired ones are left out.
func TestEffectiveAuthorityReportsOnlyTheCallersUsableGrants(t *testing.T) {
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()
	ids := map[string]string{}
	for _, subject := range []string{"jane", "bob", "nogrants", "suspended"} {
		status := "ACTIVE"
		if subject == "suspended" {
			status = "SUSPENDED"
		}
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: status}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
		ids[subject] = p.ID
	}
	now := time.Now().UTC()
	grant := func(id, principal, permission string, status administration.Status) administration.Grant {
		return administration.Grant{GrantID: id, PrincipalID: principal, Permission: permission,
			Scope: administration.Scope{Level: administration.LevelTenant, TenantID: "tn_acmeug"}, GrantType: administration.TypeStanding,
			Source: administration.SourceDirect, RiskClass: administration.RiskLow, ValidFrom: now.Add(-time.Hour), Status: status,
			GrantedBy: "prn_platformops", Reason: "test", CreatedAt: now.Add(-time.Hour), Version: 1}
	}
	grants := grantsFake{
		ids["jane"]: {grant("agr_janeview", ids["jane"], "tenant.view", administration.StatusActive),
			grant("agr_janesusp", ids["jane"], "market.view", administration.StatusSuspended)},
		ids["bob"]:       {grant("agr_bobview1", ids["bob"], "tenant.view", administration.StatusActive)},
		ids["suspended"]: {grant("agr_suspview", ids["suspended"], "tenant.view", administration.StatusActive)},
	}
	principal := func(subject string, scopes ...string) auth.Principal {
		p := auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject,
			Scopes: map[string]struct{}{}, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
		for _, s := range scopes {
			p.Scopes[s] = struct{}{}
		}
		return p
	}
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: tokenVerifier{
		"jane": principal("jane", "authority:self"), "nogrants": principal("nogrants", "authority:self"),
		"unregistered": principal("stranger", "authority:self"), "noscope": principal("jane"),
		"suspended": principal("suspended", "authority:self"),
	}, Identities: identities, AdministrativeGrants: grants})
	get := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/v1/admin/effective-authority", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	read := func(token string) map[string]any {
		t.Helper()
		response := get(token)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s: %d %s (Cache-Control %q)", token, response.Code, response.Body.String(), response.Header().Get("Cache-Control"))
		}
		var body map[string]any
		mustNoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		if dir := os.Getenv("SHARED_CONTRACTS_DIR"); dir != "" {
			contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "administration/v1/grant.schema.json#/$defs/EffectiveAuthority"), body)
		}
		return body
	}

	jane := read("jane")
	list := jane["grants"].([]any)
	if jane["principal_id"] != ids["jane"] || len(list) != 1 || list[0].(map[string]any)["grant_id"] != "agr_janeview" {
		t.Fatalf("jane's effective authority: %v", jane)
	}
	// A platform-admin realm role confers nothing in this read model.
	if empty := read("nogrants"); len(empty["grants"].([]any)) != 0 {
		t.Fatalf("a principal with no grants must get an empty list: %v", empty)
	}
	if response := get("unregistered"); response.Code != http.StatusForbidden {
		t.Fatalf("an unregistered caller: %d %s", response.Code, response.Body.String())
	}
	// An inactive principal is refused, not shown grants it cannot use.
	if response := get("suspended"); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "PRINCIPAL_INACTIVE") {
		t.Fatalf("a suspended principal: %d %s", response.Code, response.Body.String())
	}
	if response := get("noscope"); response.Code != http.StatusForbidden {
		t.Fatalf("a caller without authority:self: %d", response.Code)
	}
}
