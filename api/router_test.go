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
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/store"
)

const testTenantID = "tn_01k4zuribeans"

type fakeStore struct {
	calls              int
	registeredTenantID string
	resolveErr         error
	resolved           domain.ResolvedContext
	resolvedCalls      int
	tenant             domain.Tenant
	tenantErr          error
	entitlement        domain.Entitlement
	entitlementErr     error
	entitlementCall    int
	metadata           store.RequestMetadata
	registered         domain.RegisterTenant
	registeredStep     bool
	registerErr        error
}

func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) RegisterTenant(_ context.Context, _ string, metadata store.RequestMetadata, command domain.RegisterTenant, step store.RegistrationStep) (domain.Operation, error) {
	f.calls++
	f.metadata = metadata
	f.registeredTenantID = command.TenantID
	f.registered, f.registeredStep = command, step != nil
	if f.registerErr != nil {
		return domain.Operation{}, f.registerErr
	}
	return domain.Operation{OperationID: "7c8f131b-d8ba-4d89-b60b-a187d3944074", TenantID: command.TenantID, State: "accepted", Revision: 1}, nil
}
func (f *fakeStore) ResolveContext(_ context.Context, metadata store.RequestMetadata, tenantID, productID string) (domain.ResolvedContext, error) {
	f.resolvedCalls++
	f.metadata = metadata
	if f.resolveErr != nil {
		return domain.ResolvedContext{}, f.resolveErr
	}
	return domain.ResolvedContext{TenantID: tenantID, EntityID: tenantID, LifecycleStatus: "active", ProductID: productID, Entitled: true, CacheTTLSeconds: 15, CorrelationID: metadata.CorrelationID}, nil
}
func (f *fakeStore) GetTenant(_ context.Context, tenantID string) (domain.Tenant, error) {
	if f.tenantErr != nil {
		return domain.Tenant{}, f.tenantErr
	}
	if f.tenant.TenantID == "" {
		// DesiredState and ObservedState are both real domain.LifecycleStatus
		// values in production (Store.UpdateTenantLifecycle always sets them
		// identically) -- "active" here, not the placeholder "ready" this
		// fixture used before ContextResolutionService started checking
		// ObservedState for the ADR-BCP-004 §52 "tenant must be active" stage.
		f.tenant = domain.Tenant{TenantID: tenantID, LegalEntityID: "ZURIBEANS", DisplayName: "Zuri Beans", IsolationStrategy: "schema_per_tenant", ResidencyRegion: "af-south-1", DesiredState: string(domain.LifecycleActive), ObservedState: string(domain.LifecycleActive), Revision: 1}
	}
	return f.tenant, nil
}
func (f *fakeStore) GetEntitlement(_ context.Context, tenantID, productID string) (domain.Entitlement, error) {
	f.entitlementCall++
	if f.entitlementErr != nil {
		return domain.Entitlement{}, f.entitlementErr
	}
	if f.entitlement.TenantID == "" {
		f.entitlement = domain.Entitlement{TenantID: tenantID, ProductID: productID, Status: "ACTIVE", Tier: "standard"}
	}
	return f.entitlement, nil
}
func (f *fakeStore) UpdateTenantLifecycle(_ context.Context, tenantID string, next domain.LifecycleStatus) error {
	if !next.Valid() {
		return domain.NotFoundError("invalid tenant lifecycle status")
	}
	return nil
}

func TestRegisterTenantRejectsInvalidToken(t *testing.T) {
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{err: errors.New("invalid")}})
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer invalid")
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d", response.Code)
	}
}

func TestResolveContext(t *testing.T) {
	store := &fakeStore{}
	handler := New(Dependencies{Store: store, WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()}})
	body := `{"product_id":"baobab_trade"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/context/resolve", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer workload-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", response.Code, response.Body.String())
	}
	if store.resolvedCalls != 1 {
		t.Fatalf("expected one resolve call, got %d", store.resolvedCalls)
	}
}

func TestResolveContextFailsClosed(t *testing.T) {
	store := &fakeStore{resolveErr: store.ErrContextDenied}
	handler := New(Dependencies{Store: store, WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()}})
	req := httptest.NewRequest(http.MethodPost, "/v1/context/resolve", strings.NewReader(`{"product_id":"baobab_trade"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("got status %d: %s", response.Code, response.Body.String())
	}
}

// fakeWorkloadRegistry lets tests control auth.WorkloadRegistry's verdict
// without touching the filesystem (see internal/auth/workload_registry_test.go
// for LoadWorkloadRegistryFile's own coverage).
type fakeWorkloadRegistry struct{ active bool }

func (r fakeWorkloadRegistry) IsActive(string) bool { return r.active }

func TestResolveContextRejectsRevokedWorkload(t *testing.T) {
	// Gate ZB-03.10: a workload token can pass signature/issuer/audience/
	// actor_type verification yet belong to a client_id the workload
	// registry no longer marks ACTIVE -- this must still be rejected.
	store := &fakeStore{}
	handler := New(Dependencies{
		Store:            store,
		WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()},
		WorkloadRegistry: fakeWorkloadRegistry{active: false},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/context/resolve", strings.NewReader(`{"product_id":"baobab_trade"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("got status %d: %s", response.Code, response.Body.String())
	}
	if store.resolvedCalls != 0 {
		t.Fatalf("expected no resolve call for a rejected request, got %d", store.resolvedCalls)
	}
}

func TestResolveContextAllowsActiveWorkload(t *testing.T) {
	store := &fakeStore{}
	handler := New(Dependencies{
		Store:            store,
		WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()},
		WorkloadRegistry: fakeWorkloadRegistry{active: true},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/context/resolve", strings.NewReader(`{"product_id":"baobab_trade"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", response.Code, response.Body.String())
	}
}

func TestResolveContextUnaffectedWhenWorkloadRegistryUnconfigured(t *testing.T) {
	// The default (nil WorkloadRegistry, as every other existing test in
	// this file already exercises) must preserve pre-ZB-03.10 behavior
	// exactly -- this is what TestResolveContext above already proves, but
	// stated explicitly here as the control case for the two tests above.
	store := &fakeStore{}
	handler := New(Dependencies{Store: store, WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()}})
	req := httptest.NewRequest(http.MethodPost, "/v1/context/resolve", strings.NewReader(`{"product_id":"baobab_trade"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", response.Code, response.Body.String())
	}
}

func TestGetTenant(t *testing.T) {
	store := &fakeStore{}
	handler := New(Dependencies{Store: store, AdminVerifier: fakeVerifier{principal: adminPrincipal()}})
	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/"+testTenantID, nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", response.Code, response.Body.String())
	}
	if response.Body.String() == "" {
		t.Fatal("empty tenant response")
	}
}

func TestGetEntitlement(t *testing.T) {
	store := &fakeStore{}
	handler := New(Dependencies{Store: store, AdminVerifier: fakeVerifier{principal: adminPrincipal()}})
	req := httptest.NewRequest(http.MethodGet, "/v1/entitlements?tenantId="+testTenantID+"&productId=baobab-trade", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", response.Code, response.Body.String())
	}
	if store.entitlementCall != 1 {
		t.Fatalf("expected one entitlement lookup, got %d", store.entitlementCall)
	}
}

func TestLifecycleActionEndpoint(t *testing.T) {
	store := &fakeStore{}
	handler := New(Dependencies{Store: store, AdminVerifier: fakeVerifier{principal: adminPrincipal()}})
	for _, tc := range []struct {
		path   string
		method string
	}{
		{path: "/v1/tenants/" + testTenantID + "/suspend", method: http.MethodPost},
		{path: "/v1/tenants/" + testTenantID + "/activate", method: http.MethodPost},
		{path: "/v1/tenants/" + testTenantID + "/decommission", method: http.MethodPost},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("path %s got status %d: %s", tc.path, response.Code, response.Body.String())
		}
	}
}

func TestResolverRouteIsRegistered(t *testing.T) {
	handler := New(Dependencies{
		Store:            &fakeStore{},
		WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/resolve", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected registered resolver route to reject empty input with 400, got %d: %s", response.Code, response.Body.String())
	}
}

// TestPlatformContextRouteIsRegisteredDistinctFromLegacyContextResolve is a
// regression test for the naming collision PlatformContextHandler's doc
// comment describes: /v1/context/resolve (the pre-existing, unrelated
// tenant+product entitlement endpoint) must keep working exactly as before,
// while the new ADR-BCP-004 PlatformContext resolver lives at its own path
// and is independently reachable.
func TestPlatformContextRouteIsRegisteredDistinctFromLegacyContextResolve(t *testing.T) {
	store := &fakeStore{}
	handler := New(Dependencies{
		Store:            store,
		WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()},
	})

	legacyReq := httptest.NewRequest(http.MethodPost, "/v1/context/resolve", strings.NewReader(`{"product_id":"baobab_trade"}`))
	legacyReq.Header.Set("Authorization", "Bearer workload-token")
	legacyResponse := httptest.NewRecorder()
	handler.ServeHTTP(legacyResponse, legacyReq)
	if legacyResponse.Code != http.StatusOK {
		t.Fatalf("expected the pre-existing /v1/context/resolve to keep working, got %d: %s", legacyResponse.Code, legacyResponse.Body.String())
	}
	if store.resolvedCalls != 1 {
		t.Fatalf("expected the legacy handler to have been called, got %d calls", store.resolvedCalls)
	}

	// No Identity dependency is configured, so the new route fails closed on
	// identity resolution rather than 404/405 -- either way proves the route
	// is registered and distinct from the legacy one above.
	newReq := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", nil)
	newReq.Header.Set("Authorization", "Bearer workload-token")
	newResponse := httptest.NewRecorder()
	handler.ServeHTTP(newResponse, newReq)
	if newResponse.Code == http.StatusNotFound || newResponse.Code == http.StatusMethodNotAllowed {
		t.Fatalf("expected /v1/platform-context/resolve to be a registered route, got %d: %s", newResponse.Code, newResponse.Body.String())
	}
}

func TestCapabilitiesResolveRouteIsRegistered(t *testing.T) {
	handler := New(Dependencies{
		Store:            &fakeStore{},
		WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected registered capabilities-resolve route to reject empty input with 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCapabilitiesResolveBatchRouteIsRegistered(t *testing.T) {
	handler := New(Dependencies{
		Store:            &fakeStore{},
		WorkloadVerifier: fakeVerifier{principal: workloadPrincipal()},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/resolve-batch", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected registered capabilities-resolve-batch route to reject empty input with 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCapabilitiesExplainRouteIsRegistered(t *testing.T) {
	explainAdmin := auth.Principal{Subject: "admin-explain", ActorType: "human", TokenID: "token-explain", Scopes: map[string]struct{}{"capabilities:explain": {}}, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	handler := New(Dependencies{
		Store:         &fakeStore{},
		AdminVerifier: fakeVerifier{principal: explainAdmin},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities/explain", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected registered capabilities-explain route to reject empty input with 400, got %d: %s", response.Code, response.Body.String())
	}
}

// TestExternalReferenceRoutes proves the ADR-BCP-016 onboarding path is a
// real, reachable HTTP surface, not just a repository-level capability: an
// admin links a CanonicalEntity to a Keycloak Organization, then a
// reverse lookup resolves that native identifier back to the entity -- the
// mechanism a Trade/ZuriBeans onboarding flow would use to populate its own
// canonical_organisation_id-shaped column.
func TestExternalReferenceRoutes(t *testing.T) {
	repo := repository.NewCanonicalRepository()
	repo.Entities["org-1"] = domain.CanonicalEntity{ID: "org-1", EntityType: domain.EntityTypeBuyerOrganisation, Status: "ACTIVE", OwnerTenantID: testTenantID}
	canonical := service.CanonicalEntityService{Repository: repo}
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: adminPrincipal()}, Canonical: canonical, ExternalReferences: repo})

	body := `{"engine_id":"baobab-iam","native_type":"keycloak_organization","native_id":"kc-org-1"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/canonical-entities/org-1/external-references", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer admin-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create got status %d: %s", response.Code, response.Body.String())
	}

	// Repeating the exact same link must be rejected, not silently
	// duplicated (registry.external_reference's UNIQUE constraint).
	request2 := httptest.NewRequest(http.MethodPost, "/v1/canonical-entities/org-1/external-references", strings.NewReader(body))
	request2.Header.Set("Authorization", "Bearer admin-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request2)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected a duplicate link to be rejected with 409, got %d: %s", response.Code, response.Body.String())
	}

	lookup := httptest.NewRequest(http.MethodGet, "/v1/external-references?engine_id=baobab-iam&native_type=keycloak_organization&native_id=kc-org-1", nil)
	lookup.Header.Set("Authorization", "Bearer admin-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, lookup)
	if response.Code != http.StatusOK {
		t.Fatalf("lookup got status %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"id":"org-1"`) {
		t.Fatalf("expected the lookup to resolve back to org-1, got: %s", response.Body.String())
	}

	missing := httptest.NewRequest(http.MethodGet, "/v1/external-references?engine_id=baobab-iam&native_type=keycloak_organization&native_id=does-not-exist", nil)
	missing.Header.Set("Authorization", "Bearer admin-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected an unlinked native id to 404, got %d: %s", response.Code, response.Body.String())
	}
}

type fakeVerifier struct {
	principal auth.Principal
	err       error
}

func (f fakeVerifier) Verify(context.Context, string) (auth.Principal, error) {
	if f.err != nil {
		return auth.Principal{}, f.err
	}
	return f.principal, nil
}

func adminPrincipal() auth.Principal {
	return auth.Principal{Subject: "admin-123", ActorType: "human", TokenID: "token-123", Scopes: map[string]struct{}{"tenant:write": {}, "tenant:read": {}, "canonical:write": {}, "canonical:read": {}}, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
}

func workloadPrincipal() auth.Principal {
	return auth.Principal{Subject: "workload-123", ActorType: "workload", TenantID: testTenantID, ClientID: "client-123", TokenID: "token-456", Scopes: map[string]struct{}{"context:resolve": {}}}
}

// tenantAdminPrincipal is a "cp:tenant-admin" caller (Gate IAM-5 phase 3):
// unlike adminPrincipal (platform-admin), authorization for this principal
// depends on an ACTIVE domain.WorkforceMembership resolved from
// Issuer/Subject, which callers populate into an in-memory
// repository.WorkforceMembershipRepository per test case.
func tenantAdminPrincipal() auth.Principal {
	return auth.Principal{
		Subject: "tenant-admin-subject", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "human", TokenID: "token-tenant-admin",
		Scopes: map[string]struct{}{"tenant:write": {}, "tenant:read": {}},
		Roles:  map[string]struct{}{RoleTenantAdmin: {}},
	}
}

// noRolePrincipal carries valid scopes but no realm role at all -- the
// pre-Gate-IAM-5-phase-3 shape every admin caller used to have, now
// insufficient on its own.
func noRolePrincipal() auth.Principal {
	return auth.Principal{Subject: "no-role-123", ActorType: "human", TokenID: "token-no-role", Scopes: map[string]struct{}{"tenant:write": {}, "tenant:read": {}, "canonical:write": {}, "canonical:read": {}}}
}

// TestRequireAdminRoleTenantScoping is Gate IAM-5 phase 3's
// (docs/governance/gate-iam-5-workforce-sso-scope.md) coverage for
// ADR-0009 §27/§102-104's role-aware, tenant-scoped admin authorization:
// a "cp:platform-admin" caller reaches every tenant unconditionally; a
// "cp:tenant-admin" caller reaches only a tenant it has an ACTIVE
// WorkforceMembership for; a caller with no realm role at all, or a
// tenant-admin whose membership doesn't match (wrong tenant, wrong
// status, or missing entirely), is denied even though its OAuth scope is
// otherwise sufficient.
func TestRequireAdminRoleTenantScoping(t *testing.T) {
	setup := func(t *testing.T, principal auth.Principal, seed func(repo *repository.Repository)) http.Handler {
		t.Helper()
		repo := repository.NewInMemoryRepository()
		if seed != nil {
			seed(repo)
		}
		return New(Dependencies{
			Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: principal},
			Identities: repo, Memberships: repo,
		})
	}
	get := func(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	t.Run("platform admin reaches any tenant unconditionally", func(t *testing.T) {
		handler := setup(t, adminPrincipal(), nil)
		if response := get(t, handler, "/v1/tenants/"+testTenantID); response.Code != http.StatusOK {
			t.Fatalf("got status %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("tenant admin with an active membership for the target tenant is authorized", func(t *testing.T) {
		principal := tenantAdminPrincipal()
		handler := setup(t, principal, func(repo *repository.Repository) {
			p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
			mustNoError(t, repo.CreateIdentity(context.Background(), p))
			mustNoError(t, repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: principal.Issuer, Subject: principal.Subject, Status: "ACTIVE"}))
			mustNoError(t, repo.CreateWorkforceMembership(context.Background(), domain.WorkforceMembership{ID: domain.NewWorkforceMembershipID(), PrincipalID: p.ID, TenantID: testTenantID, Roles: []string{RoleTenantAdmin}, Status: "ACTIVE"}))
		})
		if response := get(t, handler, "/v1/tenants/"+testTenantID); response.Code != http.StatusOK {
			t.Fatalf("got status %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("tenant admin with a membership for a different tenant is denied", func(t *testing.T) {
		principal := tenantAdminPrincipal()
		handler := setup(t, principal, func(repo *repository.Repository) {
			p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
			mustNoError(t, repo.CreateIdentity(context.Background(), p))
			mustNoError(t, repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: principal.Issuer, Subject: principal.Subject, Status: "ACTIVE"}))
			mustNoError(t, repo.CreateWorkforceMembership(context.Background(), domain.WorkforceMembership{ID: domain.NewWorkforceMembershipID(), PrincipalID: p.ID, TenantID: "tn_othertenanthere", Roles: []string{RoleTenantAdmin}, Status: "ACTIVE"}))
		})
		if response := get(t, handler, "/v1/tenants/"+testTenantID); response.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for a membership scoped to a different tenant, got %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("tenant admin with a suspended membership for the target tenant is denied", func(t *testing.T) {
		principal := tenantAdminPrincipal()
		handler := setup(t, principal, func(repo *repository.Repository) {
			p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
			mustNoError(t, repo.CreateIdentity(context.Background(), p))
			mustNoError(t, repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: principal.Issuer, Subject: principal.Subject, Status: "ACTIVE"}))
			mustNoError(t, repo.CreateWorkforceMembership(context.Background(), domain.WorkforceMembership{ID: domain.NewWorkforceMembershipID(), PrincipalID: p.ID, TenantID: testTenantID, Roles: []string{RoleTenantAdmin}, Status: "SUSPENDED"}))
		})
		if response := get(t, handler, "/v1/tenants/"+testTenantID); response.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for a suspended membership, got %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("tenant admin with no membership at all is denied", func(t *testing.T) {
		handler := setup(t, tenantAdminPrincipal(), nil)
		if response := get(t, handler, "/v1/tenants/"+testTenantID); response.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for no membership, got %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("caller with no realm role is denied despite sufficient scope", func(t *testing.T) {
		handler := setup(t, noRolePrincipal(), nil)
		if response := get(t, handler, "/v1/tenants/"+testTenantID); response.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for no realm role, got %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("tenant creation is platform-admin only even with a matching membership", func(t *testing.T) {
		principal := tenantAdminPrincipal()
		repo := repository.NewInMemoryRepository()
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, repo.CreateIdentity(context.Background(), p))
		mustNoError(t, repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: principal.Issuer, Subject: principal.Subject, Status: "ACTIVE"}))
		handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: principal}, Identities: repo, Memberships: repo})
		req := httptest.NewRequest(http.MethodPost, "/v1/tenants", strings.NewReader(`{"legal_entity_id":"THAMANI-GLOBAL","display_name":"Zuri Beans","isolation_strategy":"schema_per_tenant","residency_region":"af-south-1"}`))
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("Idempotency-Key", strings.Repeat("x", 16))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for a tenant-admin creating a tenant, got %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("canonical entities are platform-admin only even with a matching membership", func(t *testing.T) {
		principal := tenantAdminPrincipal()
		repo := repository.NewInMemoryRepository()
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, repo.CreateIdentity(context.Background(), p))
		mustNoError(t, repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: principal.Issuer, Subject: principal.Subject, Status: "ACTIVE"}))
		mustNoError(t, repo.CreateWorkforceMembership(context.Background(), domain.WorkforceMembership{ID: domain.NewWorkforceMembershipID(), PrincipalID: p.ID, TenantID: testTenantID, Roles: []string{RoleTenantAdmin}, Status: "ACTIVE"}))
		canonical := service.CanonicalEntityService{Repository: repository.NewCanonicalRepository()}
		handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: principal}, Identities: repo, Memberships: repo, Canonical: canonical})
		req := httptest.NewRequest(http.MethodPost, "/v1/canonical-entities", strings.NewReader(`{"canonical_key":"tenant:product2","entity_type":"PRODUCT","display_name":"Product","authority":"baobab","classification":"INTERNAL"}`))
		req.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for a tenant-admin creating a canonical entity, got %d: %s", response.Code, response.Body.String())
		}
	})

	t.Run("requireAdminRole fails closed when Identities/Memberships are unset", func(t *testing.T) {
		handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: tenantAdminPrincipal()}})
		if response := get(t, handler, "/v1/tenants/"+testTenantID); response.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 when Identities/Memberships are unset, got %d: %s", response.Code, response.Body.String())
		}
	})
}

func mustNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
