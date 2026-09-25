// Target path: internal/provisioning/zb03_organisation_context_test.go
package provisioning

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAuthoritativeContextResolverOrganisationFailsClosed proves ADR-BCP-016's
// OrganisationID verification stage: a caller-asserted organisation_id is
// never trusted merely because it is well-formed. It must name a real,
// ACTIVE, organisation-kind CanonicalEntity owned by the requesting tenant,
// mirroring the established MarketID/DigitalEstateID/IsolationProfileID
// negative-test pattern in zb02_context_negative_test.go.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestAuthoritativeContextResolverOrganisationFailsClosed(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()

	tenantStore, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open tenant store: %v", err)
	}
	defer tenantStore.Close()
	if err := tenantStore.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	repo, err := repository.Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	tenantA := seedZB02Fixture(t, ctx, admin, repo, "orga")
	tenantB := seedZB02Fixture(t, ctx, admin, repo, "orgb")
	now := time.Now().UTC()

	resolver := NewAuthoritativeContextResolver(ContextAuthorityAdapter{Tenants: tenantStore, Repo: repo})
	resolver.now = func() time.Time { return now }

	seedOrg := func(tenantID, entityType, status string) string {
		id := domain.NewUUIDv7()
		if err := repo.CreateCanonicalEntity(ctx, domain.CanonicalEntity{
			ID: id, CanonicalKey: "org:" + tenantID + ":" + id, EntityType: entityType,
			DisplayName: "Test Organisation", OwnerTenantID: tenantID,
			Authority: "baobab", Classification: "INTERNAL", Status: status,
			EffectiveFrom: now,
		}); err != nil {
			t.Fatalf("seed canonical entity %s: %v", id, err)
		}
		t.Cleanup(func() {
			admin.Exec(ctx, `DELETE FROM registry.canonical_entity WHERE canonical_entity_id = $1::uuid`, id)
		})
		return id
	}

	activeBuyerOrgA := seedOrg(tenantA.TenantID, domain.EntityTypeBuyerOrganisation, "ACTIVE")
	activeSupplierOrgA := seedOrg(tenantA.TenantID, domain.EntityTypeSupplierOrganisation, "ACTIVE")
	activeOrgB := seedOrg(tenantB.TenantID, domain.EntityTypeBuyerOrganisation, "ACTIVE")
	inactiveOrgA := seedOrg(tenantA.TenantID, domain.EntityTypeBuyerOrganisation, "SUSPENDED")
	nonOrgEntityA := seedOrg(tenantA.TenantID, domain.EntityTypeProduct, "ACTIVE")

	base := ContextResolutionRequest{PrincipalID: "principal-1", CorrelationID: "corr-1"}

	cases := []struct {
		name string
		req  ContextResolutionRequest
	}{
		{
			name: "unknown organisation id",
			req:  withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.OrganisationID = domain.NewUUIDv7() }),
		},
		{
			name: "cross-tenant organisation (tenant A requesting tenant B's organisation)",
			req:  withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.OrganisationID = activeOrgB }),
		},
		{
			name: "inactive organisation",
			req:  withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.OrganisationID = inactiveOrgA }),
		},
		{
			name: "canonical entity that is not an organisation kind",
			req:  withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.OrganisationID = nonOrgEntityA }),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolver.Resolve(ctx, tc.req); err == nil {
				t.Fatalf("expected organisation resolution to fail closed for %q, but it succeeded", tc.name)
			}
		})
	}

	// Positive controls: an ACTIVE buyer or supplier organisation owned by
	// the requesting tenant must resolve successfully.
	for _, org := range []string{activeBuyerOrgA, activeSupplierOrgA} {
		got, err := resolver.Resolve(ctx, withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.OrganisationID = org }))
		if err != nil {
			t.Fatalf("expected the valid organisation request to succeed, got %v", err)
		}
		if got.OrganisationID != org {
			t.Fatalf("expected resolved organisation_id %q, got %q", org, got.OrganisationID)
		}
	}
}
