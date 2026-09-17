package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/store"
)

type fakeTenantStore struct {
	tenant domain.Tenant
	err    error
}

func (f *fakeTenantStore) RegisterTenant(context.Context, string, store.RequestMetadata, domain.RegisterTenant) (domain.Operation, error) {
	return domain.Operation{}, errors.New("not implemented")
}
func (f *fakeTenantStore) ResolveContext(context.Context, store.RequestMetadata, string, string) (domain.ResolvedContext, error) {
	return domain.ResolvedContext{}, errors.New("not implemented")
}
func (f *fakeTenantStore) GetTenant(_ context.Context, tenantID string) (domain.Tenant, error) {
	if f.err != nil {
		return domain.Tenant{}, f.err
	}
	return f.tenant, nil
}
func (f *fakeTenantStore) GetEntitlement(context.Context, string, string) (domain.Entitlement, error) {
	return domain.Entitlement{}, errors.New("not implemented")
}
func (f *fakeTenantStore) UpdateTenantLifecycle(context.Context, string, domain.LifecycleStatus) error {
	return errors.New("not implemented")
}
func (f *fakeTenantStore) Ping(context.Context) error { return nil }

func workloadPrincipalForContext() auth.Principal {
	return auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", TenantID: "tenant-123", ClientID: "baobab-trade", TokenID: "token-123"}
}

func activeTenant() domain.Tenant {
	return domain.Tenant{TenantID: "tenant-123", LegalEntityID: "THAMANI-GLOBAL", DisplayName: "Zuri Beans", IsolationStrategy: "schema_per_tenant", ResidencyRegion: "af-south-1", DesiredState: string(domain.LifecycleActive), ObservedState: string(domain.LifecycleActive), Revision: 1}
}

func identityServiceFor(repo *repository.Repository) IdentityService {
	return IdentityService{Repository: repo, Provision: WorkloadOnlyProvisioningPolicy}
}

func TestContextResolutionServiceResolvesTenantAndLegalEntity(t *testing.T) {
	svc := ContextResolutionService{
		Identity: identityServiceFor(repository.NewInMemoryRepository()),
		Tenants:  &fakeTenantStore{tenant: activeTenant()},
	}
	_, resolved, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "", "correlation-123", time.Now())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.TenantID != "tenant-123" {
		t.Fatalf("expected tenant_id tenant-123, got %q", resolved.TenantID)
	}
	if resolved.LegalEntityID != "THAMANI-GLOBAL" {
		t.Fatalf("expected legal_entity_id to be populated from the tenant record, got %q", resolved.LegalEntityID)
	}
}

func TestContextResolutionServiceRejectsUnknownTenant(t *testing.T) {
	svc := ContextResolutionService{
		Identity: identityServiceFor(repository.NewInMemoryRepository()),
		Tenants:  &fakeTenantStore{err: domain.NotFoundError("tenant not found")},
	}
	if _, _, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "", "correlation-123", time.Now()); err == nil {
		t.Fatal("expected an unknown tenant to be rejected")
	}
}

func TestContextResolutionServiceRejectsInactiveTenant(t *testing.T) {
	tenant := activeTenant()
	tenant.ObservedState = string(domain.LifecycleSuspended)
	svc := ContextResolutionService{
		Identity: identityServiceFor(repository.NewInMemoryRepository()),
		Tenants:  &fakeTenantStore{tenant: tenant},
	}
	_, _, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "", "correlation-123", time.Now())
	if !errors.Is(err, ErrTenantNotActive) {
		t.Fatalf("expected ErrTenantNotActive for a suspended tenant, got %v", err)
	}
}

func TestContextResolutionServiceWrapsIdentityFailure(t *testing.T) {
	// No Provision policy: IdentityService fails closed on an unknown
	// principal.
	svc := ContextResolutionService{
		Identity: IdentityService{Repository: repository.NewInMemoryRepository()},
		Tenants:  &fakeTenantStore{tenant: activeTenant()},
	}
	_, _, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "", "correlation-123", time.Now())
	if !errors.Is(err, ErrIdentityResolutionFailed) {
		t.Fatalf("expected ErrIdentityResolutionFailed, got %v", err)
	}
}

func TestContextResolutionServiceRequiresTenantStore(t *testing.T) {
	svc := ContextResolutionService{Identity: identityServiceFor(repository.NewInMemoryRepository())}
	if _, _, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "", "correlation-123", time.Now()); err == nil {
		t.Fatal("expected a nil Tenants store to be rejected")
	}
}

// TestContextResolutionServiceResolvesRequestSuppliedTenantWhenClaimEmpty is
// the regression test for the real-world case every other test in this file
// skips over: no workload client in nabhold/baobab-iam mints a tenant_id
// claim today (see api.resolveWorkloadTenant's doc comment for why), so
// principal.TenantID is empty for every real workload token. Before this,
// auth.NewOperationContext's own "verified workload principal is required"
// check on an empty TenantID meant this path was unreachable outside tests
// that hand-set a synthetic principal.TenantID. The caller (an API handler)
// is responsible for reconciling the effective tenant via
// api.resolveWorkloadTenant and passing it explicitly; this test exercises
// that Resolve actually honors the passed-in tenantID rather than the
// principal's own (empty) claim.
func TestContextResolutionServiceResolvesRequestSuppliedTenantWhenClaimEmpty(t *testing.T) {
	principal := workloadPrincipalForContext()
	principal.TenantID = ""
	svc := ContextResolutionService{
		Identity: identityServiceFor(repository.NewInMemoryRepository()),
		Tenants:  &fakeTenantStore{tenant: activeTenant()},
	}
	_, resolved, err := svc.Resolve(context.Background(), principal, "tenant-123", "", "correlation-123", time.Now())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.TenantID != "tenant-123" {
		t.Fatalf("expected the explicitly passed tenantID to win, got %q", resolved.TenantID)
	}
}

// canonicalOrganisation seeds an ACTIVE, tenant-owned BUYER_ORGANISATION
// CanonicalEntity directly into an in-memory CanonicalRepository, bypassing
// CanonicalEntityService's DRAFT-first lifecycle -- only Resolve's own
// verification is this test's concern.
func canonicalOrganisation(id, entityType, status, ownerTenantID string) domain.CanonicalEntity {
	return domain.CanonicalEntity{ID: id, EntityType: entityType, Status: status, OwnerTenantID: ownerTenantID}
}

func TestContextResolutionServiceResolvesOrganisationID(t *testing.T) {
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["org-1"] = canonicalOrganisation("org-1", domain.EntityTypeBuyerOrganisation, "ACTIVE", "tenant-123")
	svc := ContextResolutionService{
		Identity:  identityServiceFor(repository.NewInMemoryRepository()),
		Tenants:   &fakeTenantStore{tenant: activeTenant()},
		Canonical: canonical,
	}
	_, resolved, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "org-1", "correlation-123", time.Now())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.OrganisationID != "org-1" {
		t.Fatalf("expected organisation_id to be resolved, got %q", resolved.OrganisationID)
	}
}

func TestContextResolutionServiceRejectsOrganisationIDWithoutCanonicalRepository(t *testing.T) {
	svc := ContextResolutionService{
		Identity: identityServiceFor(repository.NewInMemoryRepository()),
		Tenants:  &fakeTenantStore{tenant: activeTenant()},
	}
	if _, _, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "org-1", "correlation-123", time.Now()); err == nil {
		t.Fatal("expected a supplied organisation_id to fail closed when no Canonical repository is configured")
	}
}

func TestContextResolutionServiceRejectsOrganisationID(t *testing.T) {
	cases := []struct {
		name string
		org  domain.CanonicalEntity
	}{
		{name: "unknown organisation id", org: domain.CanonicalEntity{}},
		{name: "wrong entity type", org: canonicalOrganisation("org-1", domain.EntityTypeProduct, "ACTIVE", "tenant-123")},
		{name: "inactive organisation", org: canonicalOrganisation("org-1", domain.EntityTypeBuyerOrganisation, "SUSPENDED", "tenant-123")},
		{name: "cross-tenant organisation", org: canonicalOrganisation("org-1", domain.EntityTypeBuyerOrganisation, "ACTIVE", "tenant-999")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canonical := repository.NewCanonicalRepository()
			if tc.org.ID != "" {
				canonical.Entities[tc.org.ID] = tc.org
			}
			svc := ContextResolutionService{
				Identity:  identityServiceFor(repository.NewInMemoryRepository()),
				Tenants:   &fakeTenantStore{tenant: activeTenant()},
				Canonical: canonical,
			}
			if _, _, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "org-1", "correlation-123", time.Now()); err == nil {
				t.Fatalf("expected organisation resolution to fail closed for %q", tc.name)
			}
		})
	}
}
