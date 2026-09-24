package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/metrics"
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
		Mappings:  tenantMappings(nil),
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
				Mappings:  tenantMappings(nil),
			}
			if _, _, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "org-1", "correlation-123", time.Now()); err == nil {
				t.Fatalf("expected organisation resolution to fail closed for %q", tc.name)
			}
		})
	}
}

// tenantMappings is a fixed TenantOrganisationMappingReader.
type tenantMappings []domain.TenantOrganisationMapping

func (m tenantMappings) ListTenantOrganisationMappings(context.Context, string, time.Time) ([]domain.TenantOrganisationMapping, error) {
	return m, nil
}

// TestContextResolutionServiceAttestsGenericOrganisationByMapping proves
// ADR-BCP-018 gate ORG-14 on this resolver: a platform-scoped ORGANISATION is
// attested only by an ACTIVE mapping to the requesting tenant, never by
// having been registered by it, and a supplied organisation_id fails closed
// when no mapping reader is configured.
func TestContextResolutionServiceAttestsGenericOrganisationByMapping(t *testing.T) {
	now := time.Now().UTC()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["org-1"] = canonicalOrganisation("org-1", domain.EntityTypeOrganisation, "ACTIVE", "tenant-123")
	mapped := tenantMappings{{TenantID: "tenant-123", OrganisationID: "org-1", Status: domain.RelationshipStatusActive, EffectiveFrom: now.Add(-time.Hour)}}
	ended := tenantMappings{{TenantID: "tenant-123", OrganisationID: "org-1", Status: domain.RelationshipStatusEnded, EffectiveFrom: now.Add(-time.Hour)}}
	for name, tc := range map[string]struct {
		mappings TenantOrganisationMappingReader
		allow    bool
	}{
		"active mapping":                 {mapped, true},
		"registering tenant, no mapping": {tenantMappings(nil), false},
		"ended mapping":                  {ended, false},
		"no mapping reader":              {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			svc := ContextResolutionService{
				Identity:  identityServiceFor(repository.NewInMemoryRepository()),
				Tenants:   &fakeTenantStore{tenant: activeTenant()},
				Canonical: canonical,
				Mappings:  tc.mappings,
			}
			_, resolved, err := svc.Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "org-1", "correlation-123", now)
			if tc.allow && (err != nil || resolved.OrganisationID != "org-1") {
				t.Fatalf("expected attestation, got %v", err)
			}
			if !tc.allow && err == nil {
				t.Fatal("expected organisation resolution to fail closed")
			}
		})
	}
}

// TestContextResolutionServiceAttestsExpectedOrganisationKind proves
// ADR-BCP-024 on the service: the exact kind is enforced, including for a
// generic ORGANISATION attested by mapping, and the kind is compared only
// after tenant attestation so an unattested organisation's kind is never
// revealed.
func TestContextResolutionServiceAttestsExpectedOrganisationKind(t *testing.T) {
	now := time.Now().UTC()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["buyer"] = canonicalOrganisation("buyer", domain.EntityTypeBuyerOrganisation, "ACTIVE", "tenant-123")
	canonical.Entities["supplier"] = canonicalOrganisation("supplier", domain.EntityTypeSupplierOrganisation, "ACTIVE", "tenant-123")
	canonical.Entities["foreign-supplier"] = canonicalOrganisation("foreign-supplier", domain.EntityTypeSupplierOrganisation, "ACTIVE", "tenant-999")
	canonical.Entities["generic"] = canonicalOrganisation("generic", domain.EntityTypeOrganisation, "ACTIVE", "")
	svc := ContextResolutionService{
		Identity:  identityServiceFor(repository.NewInMemoryRepository()),
		Tenants:   &fakeTenantStore{tenant: activeTenant()},
		Canonical: canonical,
		Mappings:  tenantMappings{{TenantID: "tenant-123", OrganisationID: "generic", Status: domain.RelationshipStatusActive, EffectiveFrom: now.Add(-time.Hour)}},
	}
	for name, tc := range map[string]struct {
		organisationID, expected string
		wantErr                  error // nil means success; errAny means any error
	}{
		"buyer as buyer":               {"buyer", domain.EntityTypeBuyerOrganisation, nil},
		"generic as organisation":      {"generic", domain.EntityTypeOrganisation, nil},
		"no expectation":               {"supplier", "", nil},
		"supplier as buyer":            {"supplier", domain.EntityTypeBuyerOrganisation, errAny},
		"generic as buyer":             {"generic", domain.EntityTypeBuyerOrganisation, errAny},
		"foreign supplier as buyer":    {"foreign-supplier", domain.EntityTypeBuyerOrganisation, domain.ErrOrganisationNotMappedToTenant},
		"non-organisation expected":    {"buyer", domain.EntityTypeProduct, errAny},
		"expectation, no organisation": {"", domain.EntityTypeBuyerOrganisation, errAny},
	} {
		t.Run(name, func(t *testing.T) {
			_, resolved, err := svc.ResolveExpectedOrganisationKind(context.Background(), workloadPrincipalForContext(), "tenant-123", tc.organisationID, tc.expected, "correlation-123", now)
			switch {
			case tc.wantErr == nil && (err != nil || resolved.OrganisationID != tc.organisationID):
				t.Fatalf("expected attestation of %q, got %v", tc.organisationID, err)
			case tc.wantErr == errAny && err == nil:
				t.Fatal("expected resolution to fail closed")
			case tc.wantErr != nil && tc.wantErr != errAny && !errors.Is(err, tc.wantErr):
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

var errAny = errors.New("any error")

// fakeRoles holds counterparty roles keyed by organisation|tenant|role.
type fakeRoles struct {
	held map[string]bool
	err  error
}

func (f fakeRoles) HoldsCounterpartyRole(_ context.Context, organisationID, tenantID, role string, _ time.Time) (bool, error) {
	return f.held[organisationID+"|"+tenantID+"|"+role], f.err
}

// TestContextResolutionServiceAttestsKindByCounterpartyRole proves
// ADR-BCP-024 clause 8: a generic ORGANISATION satisfies a buyer or
// supplier kind only while it holds that role for the requesting tenant; a
// legacy kind still has to match exactly, and a failing role lookup fails
// closed.
func TestContextResolutionServiceAttestsKindByCounterpartyRole(t *testing.T) {
	now := time.Now().UTC()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["generic"] = canonicalOrganisation("generic", domain.EntityTypeOrganisation, "ACTIVE", "")
	canonical.Entities["supplier"] = canonicalOrganisation("supplier", domain.EntityTypeSupplierOrganisation, "ACTIVE", "tenant-123")
	mappings := tenantMappings{{TenantID: "tenant-123", OrganisationID: "generic", Status: domain.RelationshipStatusActive, EffectiveFrom: now.Add(-time.Hour)}}
	buyerRole := fakeRoles{held: map[string]bool{"generic|tenant-123|BUYER": true, "supplier|tenant-123|BUYER": true}}
	otherTenantRole := fakeRoles{held: map[string]bool{"generic|tenant-999|BUYER": true}}
	for name, tc := range map[string]struct {
		roles          CounterpartyRoleReader
		organisationID string
		expected       string
		allow          bool
	}{
		"generic holding BUYER":           {buyerRole, "generic", domain.EntityTypeBuyerOrganisation, true},
		"generic without SUPPLIER role":   {buyerRole, "generic", domain.EntityTypeSupplierOrganisation, false},
		"generic, role in another tenant": {otherTenantRole, "generic", domain.EntityTypeBuyerOrganisation, false},
		"generic, no role reader":         {nil, "generic", domain.EntityTypeBuyerOrganisation, false},
		"legacy supplier holding BUYER":   {buyerRole, "supplier", domain.EntityTypeBuyerOrganisation, false},
		"role lookup failure":             {fakeRoles{err: errors.New("db down")}, "generic", domain.EntityTypeBuyerOrganisation, false},
		"legacy supplier as itself":       {nil, "supplier", domain.EntityTypeSupplierOrganisation, true},
	} {
		t.Run(name, func(t *testing.T) {
			svc := ContextResolutionService{
				Identity:          identityServiceFor(repository.NewInMemoryRepository()),
				Tenants:           &fakeTenantStore{tenant: activeTenant()},
				Canonical:         canonical,
				Mappings:          mappings,
				CounterpartyRoles: tc.roles,
			}
			_, resolved, err := svc.ResolveExpectedOrganisationKind(context.Background(), workloadPrincipalForContext(), "tenant-123", tc.organisationID, tc.expected, "correlation-123", now)
			if tc.allow && (err != nil || resolved.OrganisationID != tc.organisationID) {
				t.Fatalf("expected attestation, got %v", err)
			}
			if !tc.allow && err == nil {
				t.Fatal("expected resolution to fail closed")
			}
		})
	}
}

// TestContextResolutionServiceCountsOrganisationFailures proves ADR-BCP-018
// section 130's resolution counters: each fail-closed organisation outcome
// is counted, and an organisation the tenant is not attested for also
// counts as a denied cross-tenant access.
func TestContextResolutionServiceCountsOrganisationFailures(t *testing.T) {
	now := time.Now().UTC()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["foreign"] = canonicalOrganisation("foreign", domain.EntityTypeBuyerOrganisation, "ACTIVE", "tenant-999")
	canonical.Entities["supplier"] = canonicalOrganisation("supplier", domain.EntityTypeSupplierOrganisation, "ACTIVE", "tenant-123")
	svc := ContextResolutionService{
		Identity:  identityServiceFor(repository.NewInMemoryRepository()),
		Tenants:   &fakeTenantStore{tenant: activeTenant()},
		Canonical: canonical,
		Mappings:  tenantMappings(nil),
	}
	resolve := func(org, kind string) {
		t.Helper()
		if _, _, err := svc.ResolveExpectedOrganisationKind(context.Background(), workloadPrincipalForContext(), "tenant-123", org, kind, "correlation-123", now); err == nil {
			t.Fatalf("%s as %q must fail closed", org, kind)
		}
	}
	notMapped, crossTenant, wrongKind := metrics.RelationshipResolutionFailures.Value(metrics.OutcomeNotMapped),
		metrics.CrossTenantGroupAccessDenied.Value(), metrics.RelationshipResolutionFailures.Value(metrics.OutcomeWrongKind)
	resolve("foreign", "")
	resolve("supplier", domain.EntityTypeBuyerOrganisation)
	if got := metrics.RelationshipResolutionFailures.Value(metrics.OutcomeNotMapped); got != notMapped+1 {
		t.Fatalf("not_mapped %d -> %d", notMapped, got)
	}
	if got := metrics.CrossTenantGroupAccessDenied.Value(); got != crossTenant+1 {
		t.Fatalf("cross_tenant_group_access_denied_total %d -> %d; a wrong kind is not a cross-tenant access", crossTenant, got)
	}
	if got := metrics.RelationshipResolutionFailures.Value(metrics.OutcomeWrongKind); got != wrongKind+1 {
		t.Fatalf("wrong_kind %d -> %d", wrongKind, got)
	}
}
