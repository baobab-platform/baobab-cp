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
