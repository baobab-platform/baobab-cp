package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// TestOrganisationGrantDelegatesAMappedTenantOverHTTP is the ACME to ACME
// Uganda case through the delegation route (ADR-BCP-018 section 50;
// ADR-BCP-020 sections 43-44): an organisation grant delegates a slice at a
// tenant only while an effective TenantOrganisationMapping ties that tenant
// to the organisation, and the delegation stops being usable when it ends.
func TestOrganisationGrantDelegatesAMappedTenantOverHTTP(t *testing.T) {
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()
	ids := map[string]string{}
	for _, subject := range []string{"jane", "bob"} {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
		ids[subject] = p.ID
	}
	now := time.Now().UTC()
	acme := "0a1b2c3d-0000-4000-8000-000000000001"
	orgScope := administration.Scope{Level: administration.LevelOrganisation, OrganisationID: acme}
	store := newGrantAdminFake()
	seed := func(id, permission string, depth int, risk administration.RiskClass) {
		store.put(administration.Grant{GrantID: id, PrincipalID: ids["jane"], Permission: permission, Scope: orgScope,
			GrantType: administration.TypeStanding, Source: administration.SourceDirect, DelegableDepth: depth, RiskClass: risk,
			ValidFrom: now.Add(-time.Hour), Status: administration.StatusActive, GrantedBy: ids["bob"], Reason: "seed", CreatedAt: now, Version: 1})
	}
	seed("agr_orgview", "tenant.view", 1, administration.RiskLow)
	seed("agr_orgdeleg", "administrator.delegate", 0, administration.RiskHigh)

	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: tokenVerifier{"jane": {Subject: "jane", Issuer: testRealm, ActorType: "human",
		TokenID: "t-jane", Scopes: map[string]struct{}{"administrator:write": {}}, Roles: map[string]struct{}{}}},
		Identities: identities, AdministrativeGrants: store, AdministrativeGrantAdmin: store})
	delegate := func(tenant string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"principal_id": ids["bob"], "permission": "tenant.view",
			"scope": map[string]any{"level": "TENANT", "tenant_id": tenant}, "valid_until": now.Add(time.Hour).Format(time.RFC3339), "reason": "Cover ACME Uganda."})
		r := httptest.NewRequest(http.MethodPost, "/v1/admin/grants/agr_orgview/delegations", bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer jane")
		r.Header.Set("Idempotency-Key", "org-tenant-test-"+tenant+"-abcdef")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	// No mapping: the organisation grant does not reach the tenant, so it cannot be delegated.
	if w := delegate("tn_acmeug"); w.Code != http.StatusForbidden || problemCode(t, w) != "SCOPE_MISMATCH" {
		t.Fatalf("delegating an unmapped tenant: %d %s", w.Code, w.Body.String())
	}
	// An effective mapping ties tn_acmeug to ACME; tn_other stays unmapped.
	store.relations = administration.Relations{TenantOrganisations: map[string][]string{"tn_acmeug": {acme}}}
	made := delegate("tn_acmeug")
	if made.Code != http.StatusCreated {
		t.Fatalf("delegating a mapped tenant: %d %s", made.Code, made.Body.String())
	}
	var g administration.Grant
	mustNoError(t, json.Unmarshal(made.Body.Bytes(), &g))
	if g.Scope.Level != administration.LevelTenant || g.Scope.TenantID != "tn_acmeug" || g.DelegatedFromGrantID != "agr_orgview" || g.GrantedBy != ids["jane"] {
		t.Fatalf("unexpected delegation %+v", g)
	}
	if w := delegate("tn_other"); w.Code != http.StatusForbidden {
		t.Fatalf("delegating a tenant the organisation is not mapped to: %d %s", w.Code, w.Body.String())
	}
	// The delegation is usable while the mapping holds and not after it ends.
	store.put(g)
	_, sources, _ := store.AdministrativeGrantsOf(ctx, ids["bob"])
	rel := store.relations
	after := time.Now().UTC().Add(time.Second) // the grant starts when it was made
	if !administration.Usable(g, sources, after, rel) {
		t.Fatal("the delegation is not usable while the mapping is effective")
	}
	if administration.Usable(g, sources, after, administration.Relations{}) {
		t.Fatal("the delegation outlived the mapping")
	}
}
