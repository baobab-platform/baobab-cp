package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

// leasedDevelopmentIdentityRepository models CP's authoritative bootstrap
// assignment without a live database. It delegates normal identity operations
// to the standard repository; only the leased assignment is specialized.
type leasedDevelopmentIdentityRepository struct {
	repository.IdentityRepository
	leaseExpiry time.Time
}

func (r leasedDevelopmentIdentityRepository) DevelopmentTenantLease(_ context.Context, _, clientID, tenantID string, at time.Time) (time.Time, error) {
	if clientID != "baobab-dev-nabhold-estate" || tenantID != "tenant-123" || !r.leaseExpiry.After(at) {
		return time.Time{}, repository.ErrIdentityNotFound
	}
	return r.leaseExpiry, nil
}

// An HTTP TTL is only an additional limit: it may never extend the
// disposable CP bootstrap lease, including when no TTL is configured.
func TestPlatformContextHandlerCapsDevelopmentLease(t *testing.T) {
	for _, tt := range []struct {
		name string
		ttl  time.Duration
	}{
		{"long configured TTL", 15 * time.Minute},
		{"unbounded configured TTL", 0},
		{"short configured TTL", 5 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := repository.NewInMemoryRepository()
			lease := time.Now().UTC().Add(30 * time.Second)
			handler := PlatformContextHandler{
				ContextResolution: service.ContextResolutionService{
					Identity: service.IdentityService{Repository: leasedDevelopmentIdentityRepository{
						IdentityRepository: base, leaseExpiry: lease,
					}, Provision: service.WorkloadOnlyProvisioningPolicy},
					Tenants: &fakeStore{},
				},
				Contexts: base,
				TTL:      tt.ttl,
			}
			principal := auth.Principal{
				Issuer: "https://hydra.int03.test", Subject: "baobab-dev-nabhold-estate",
				ClientID: "baobab-dev-nabhold-estate", ActorType: "workload",
				Scopes: map[string]struct{}{"context:resolve": {}}, TokenID: "synthetic-test",
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve",
				bytes.NewReader([]byte(`{"tenant_id":"tenant-123"}`)))
			req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
			w := httptest.NewRecorder()
			handler.Resolve(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var response platformContextResolveResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			want := lease
			if tt.ttl > 0 && response.ResolvedAt.Add(tt.ttl).Before(want) {
				want = response.ResolvedAt.Add(tt.ttl)
			}
			if response.ExpiresAt == nil || !response.ExpiresAt.Equal(want) {
				t.Fatalf("context expiry=%v, expected bound %s", response.ExpiresAt, want)
			}
			stored, err := base.GetContext(context.Background(), response.ContextID)
			if err != nil || stored.ExpiresAt == nil || !stored.ExpiresAt.Equal(want) {
				t.Fatalf("persisted context exceeded lease, context=%+v err=%v", stored.ExpiresAt, err)
			}
		})
	}
}

func TestPlatformContextHandlerResolvesAndPersistsContext(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	handler := PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:  &fakeStore{},
		},
		Contexts: repo,
		TTL:      5 * time.Minute,
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", nil)
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000001")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var response platformContextResolveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ContextID == "" {
		t.Fatal("expected a non-empty context_id")
	}
	if response.TenantID != "tenant-123" {
		t.Fatalf("expected tenant_id tenant-123, got %q", response.TenantID)
	}
	if response.ExpiresAt == nil || !response.ExpiresAt.After(response.ResolvedAt) {
		t.Fatalf("expected expires_at to be set from TTL, got %+v", response.ExpiresAt)
	}

	stored, err := repo.GetContext(context.Background(), response.ContextID)
	if err != nil {
		t.Fatalf("expected the resolved context to be persisted: %v", err)
	}
	if stored.TenantID != "tenant-123" {
		t.Fatalf("unexpected persisted context: %+v", stored)
	}
}

// TestPlatformContextHandlerAcceptsRequestSuppliedTenantWhenClaimEmpty is
// this handler's counterpart to
// TestResolverHandlerAcceptsRequestSuppliedTenantWhenClaimEmpty: no workload
// client in baobab-platform/baobab-iam mints a tenant_id claim today, so a request
// body tenant_id is how the real-world workload path supplies one.
func TestPlatformContextHandlerAcceptsRequestSuppliedTenantWhenClaimEmpty(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	handler := PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:  &fakeStore{},
		},
		Contexts: repo,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123"}`)))
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000007")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var response platformContextResolveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.TenantID != "tenant-123" {
		t.Fatalf("expected the request-supplied tenant to be honored, got %q", response.TenantID)
	}
}

// TestPlatformContextHandlerRejectsMissingTenantWhenClaimEmpty confirms a
// workload with no tenant_id claim still cannot omit it from the request.
func TestPlatformContextHandlerRejectsMissingTenantWhenClaimEmpty(t *testing.T) {
	handler := PlatformContextHandler{Contexts: repository.NewInMemoryRepository()}
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", nil)
	principal := auth.Principal{Subject: "baobab-trade", ActorType: "workload", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("TENANT_CONTEXT_MISMATCH")) {
		t.Fatalf("expected a workload with no tenant claim and no requested tenant to be rejected, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformContextHandlerRejectsUnauthenticated(t *testing.T) {
	handler := PlatformContextHandler{Contexts: repository.NewInMemoryRepository()}
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", nil)
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformContextHandlerRejectsUnresolvableIdentity(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	// No Provision policy set: IdentityService fails closed on an unknown
	// principal, mirroring TestResolverHandlerRejectsUnresolvableIdentity.
	handler := PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{Identity: service.IdentityService{Repository: repo}, Tenants: &fakeStore{}},
		Contexts:          repo,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", nil)
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	req = req.WithContext(auth.WithPrincipal(context.Background(), principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("IDENTITY_RESOLUTION_FAILED")) {
		t.Fatalf("expected unresolvable identity rejection, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformContextHandlerFailsClosedWhenContextStoreUnavailable(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	handler := PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:  &fakeStore{},
		},
		// Contexts intentionally left nil.
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", nil)
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000003")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusServiceUnavailable || !bytes.Contains(w.Body.Bytes(), []byte("CONTEXT_STORE_UNAVAILABLE")) {
		t.Fatalf("expected fail-closed 503, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestPlatformContextHandlerRejectsInactiveTenant is PlatformContextHandler's
// counterpart to TestResolverHandlerRejectsInactiveTenant.
func TestPlatformContextHandlerRejectsInactiveTenant(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	decommissioned := domain.Tenant{TenantID: "tenant-123", LegalEntityID: "THAMANI-GLOBAL", ObservedState: string(domain.LifecycleDecommissioned)}
	handler := PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:  &fakeStore{tenant: decommissioned},
		},
		Contexts: repo,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", nil)
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000004")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("TENANT_NOT_ACTIVE")) {
		t.Fatalf("expected a decommissioned tenant to be rejected, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestPlatformContextHandlerResolvesOrganisationID proves this endpoint is a
// real, reachable HTTP path for the ADR-BCP-016 OrganisationID verification
// stage ZB-03.2 added to ContextResolutionService -- not just a Go-test-only
// code path.
func TestPlatformContextHandlerResolvesOrganisationID(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["org-1"] = domain.CanonicalEntity{ID: "org-1", EntityType: domain.EntityTypeBuyerOrganisation, Status: "ACTIVE", OwnerTenantID: "tenant-123"}
	handler := PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity:  service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:   &fakeStore{},
			Canonical: canonical,
			Mappings:  noOrganisationMappings{},
		},
		Contexts: repo,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123","organisation_id":"org-1"}`)))
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000008")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var response platformContextResolveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	stored, err := repo.GetContext(context.Background(), response.ContextID)
	if err != nil {
		t.Fatalf("expected the resolved context to be persisted: %v", err)
	}
	if stored.OrganisationID != "org-1" {
		t.Fatalf("expected organisation_id org-1 to be resolved and persisted, got %+v", stored)
	}
}

// TestPlatformContextHandlerRejectsCrossTenantOrganisationID proves the
// fail-closed OrganisationID stage is actually reachable over HTTP, not just
// wired internally: a caller-asserted organisation_id belonging to a
// different tenant must deny the whole request.
func TestPlatformContextHandlerRejectsCrossTenantOrganisationID(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["org-1"] = domain.CanonicalEntity{ID: "org-1", EntityType: domain.EntityTypeBuyerOrganisation, Status: "ACTIVE", OwnerTenantID: "tenant-999"}
	handler := PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity:  service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:   &fakeStore{},
			Canonical: canonical,
			Mappings:  noOrganisationMappings{},
		},
		Contexts: repo,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", bytes.NewReader([]byte(`{"tenant_id":"tenant-123","organisation_id":"org-1"}`)))
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000009")
	req = req.WithContext(auth.WithPrincipal(requestContext, principal))
	w := httptest.NewRecorder()

	handler.Resolve(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected a cross-tenant organisation_id to be rejected with 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// noOrganisationMappings is a TenantOrganisationMappingReader with no
// mappings: only owner-tenant attestation of buyer/supplier records applies.
type noOrganisationMappings struct{}

func (noOrganisationMappings) ListTenantOrganisationMappings(context.Context, string, time.Time) ([]domain.TenantOrganisationMapping, error) {
	return nil, nil
}

// This exercises the public v1 route with an omitted hint: governed tenant
// identity is authoritative state, not a newly required request property.
func TestPlatformContextGovernedPrimaryWhenHintOmitted(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "derived", false: "missing_denied"}[valid], func(t *testing.T) {
			repo := repository.NewInMemoryRepository()
			canonical := repository.NewCanonicalRepository()
			org := "019343b9-f6a0-7000-8000-000000000001"
			canonical.Entities[org] = domain.CanonicalEntity{ID: org, EntityType: domain.EntityTypeOrganisation, Status: "ACTIVE"}
			mappings := primaryContextMappings{}
			if valid {
				mappings = append(mappings, domain.TenantOrganisationMapping{TenantID: "tenant-123", OrganisationID: org, MappingRole: "PRIMARY_ORGANISATION", Status: "ACTIVE", EffectiveFrom: time.Now().Add(-time.Hour)})
			}
			h := PlatformContextHandler{ContextResolution: service.ContextResolutionService{
				Identity:  service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
				Tenants:   &fakeStore{tenant: domain.Tenant{TenantID: "tenant-123", ObservedState: "active", PrimaryOrganisationEnforced: true}},
				Canonical: canonical, Mappings: mappings,
			}, Contexts: repo, TTL: time.Minute}
			request := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", nil)
			request = request.WithContext(auth.WithPrincipal(context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000009"), auth.Principal{Subject: "workload-primary", Issuer: "https://iam.example.test", ActorType: "workload", TenantID: "tenant-123", ClientID: "test", TokenID: "test", Scopes: map[string]struct{}{"context:resolve": {}}}))
			w := httptest.NewRecorder()
			h.Resolve(w, request)
			if !valid {
				if w.Code != http.StatusForbidden || bytes.Contains(w.Body.Bytes(), []byte("context_id")) {
					t.Fatalf("missing PRIMARY created authority: %d %s", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != http.StatusOK {
				t.Fatalf("resolve: %d %s", w.Code, w.Body.String())
			}
			var result platformContextResolveResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			stored, err := repo.GetContext(context.Background(), result.ContextID)
			if err != nil || result.OrganisationID != org || stored.OrganisationID != org {
				t.Fatalf("PRIMARY was not persisted: %+v %+v %v", result, stored, err)
			}
		})
	}
}

type primaryContextMappings []domain.TenantOrganisationMapping

func (m primaryContextMappings) ListTenantOrganisationMappings(context.Context, string, time.Time) ([]domain.TenantOrganisationMapping, error) {
	return m, nil
}
