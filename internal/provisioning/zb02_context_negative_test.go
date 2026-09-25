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

// TestAuthoritativeContextResolverFailsClosed proves spec §21's negative
// context-resolution requirements for AuthoritativeContextResolver, which
// previously only had positive-path coverage
// (TestAuthoritativeContextResolution, an in-memory fake test). Every case
// here must be rejected, never silently resolved.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestAuthoritativeContextResolverFailsClosed(t *testing.T) {
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

	tenantA := seedZB02Fixture(t, ctx, admin, repo, "ctxa")
	tenantB := seedZB02Fixture(t, ctx, admin, repo, "ctxb")
	now := time.Now().UTC()

	resolver := NewAuthoritativeContextResolver(ContextAuthorityAdapter{Tenants: tenantStore, Repo: repo})
	resolver.now = func() time.Time { return now }

	// Tenant A has a real, effective, ACTIVE participation for MarketUGID.
	if err := repo.CreateMarketAssignment(ctx, domain.MarketAssignment{
		ID: domain.NewUUIDv7(), TenantID: tenantA.TenantID, LegalEntityID: tenantA.LegalEntityID,
		MarketID: tenantA.MarketUGID, Capabilities: []domain.MarketParticipationCapability{domain.MarketParticipationSelling},
		EffectiveFrom: now.Add(-24 * time.Hour), Status: domain.MarketParticipationActive,
		Source: domain.MarketParticipationSourceProvisioning, PolicyVersion: "1",
	}); err != nil {
		t.Fatalf("seed tenant A market participation: %v", err)
	}

	// An inactive market, and an expired participation for tenant B, and a
	// DigitalEstate owned by tenant A -- all used by the negative cases
	// below.
	inactiveMarketID := domain.NewUUIDv7()
	if err := repo.CreateMarket(ctx, domain.Market{ID: inactiveMarketID, Code: "INACTIVE-CTX", Name: "Inactive Test Market", Currency: "USD", Region: "af-south-1", IsActive: false}); err != nil {
		t.Fatalf("seed inactive market: %v", err)
	}
	expiredTo := now.Add(-time.Hour)
	if err := repo.CreateMarketAssignment(ctx, domain.MarketAssignment{
		ID: domain.NewUUIDv7(), TenantID: tenantB.TenantID, LegalEntityID: tenantB.LegalEntityID,
		MarketID: tenantB.MarketUGID, Capabilities: []domain.MarketParticipationCapability{domain.MarketParticipationSelling},
		EffectiveFrom: now.Add(-48 * time.Hour), EffectiveTo: &expiredTo, Status: domain.MarketParticipationActive,
		Source: domain.MarketParticipationSourceProvisioning, PolicyVersion: "1",
	}); err != nil {
		t.Fatalf("seed tenant B expired market participation: %v", err)
	}
	estateA := domain.NewUUIDv7()
	if err := repo.CreateDigitalEstate(ctx, domain.DigitalEstate{ID: estateA, TenantID: tenantA.TenantID, Name: "Tenant A Estate", Domain: "tenant-a.example.com"}); err != nil {
		t.Fatalf("seed tenant A digital estate: %v", err)
	}

	base := ContextResolutionRequest{PrincipalID: "principal-1", CorrelationID: "corr-1"}

	cases := []struct {
		name string
		req  ContextResolutionRequest
	}{
		{
			name: "missing tenant_id",
			req:  ContextResolutionRequest{PrincipalID: "principal-1", CorrelationID: "corr-1"},
		},
		{
			name: "missing principal_id",
			req:  ContextResolutionRequest{TenantID: tenantA.TenantID, CorrelationID: "corr-1"},
		},
		{
			name: "missing correlation_id",
			req:  ContextResolutionRequest{PrincipalID: "principal-1", TenantID: tenantA.TenantID},
		},
		{
			name: "wrong legal entity for the tenant",
			req:  withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.LegalEntityID = tenantB.LegalEntityID }),
		},
		{
			name: "inactive market",
			req:  withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.MarketID = inactiveMarketID }),
		},
		{
			name: "market with no participation at all",
			req:  withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.MarketID = tenantA.MarketZAID }),
		},
		{
			name: "expired market participation",
			req:  withTenant(base, tenantB.TenantID, func(r *ContextResolutionRequest) { r.MarketID = tenantB.MarketUGID }),
		},
		{
			name: "cross-tenant market participation (tenant B requesting tenant A's market)",
			req:  withTenant(base, tenantB.TenantID, func(r *ContextResolutionRequest) { r.MarketID = tenantA.MarketUGID }),
		},
		{
			name: "cross-tenant digital estate",
			req:  withTenant(base, tenantB.TenantID, func(r *ContextResolutionRequest) { r.DigitalEstateID = estateA }),
		},
		{
			name: "unknown isolation profile",
			req:  withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.IsolationProfileID = "does-not-exist" }),
		},
		{
			name: "unknown tenant",
			req:  withTenant(base, "tn_does_not_exist_at_all", nil),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolver.Resolve(ctx, tc.req); err == nil {
				t.Fatalf("expected context resolution to fail closed for %q, but it succeeded", tc.name)
			}
		})
	}

	// Positive control: the same tenant A + effective market participation
	// combination that several negative cases deliberately vary from must
	// still succeed, proving the failures above are about the specific
	// violation, not a broken fixture.
	if _, err := resolver.Resolve(ctx, withTenant(base, tenantA.TenantID, func(r *ContextResolutionRequest) { r.MarketID = tenantA.MarketUGID })); err != nil {
		t.Fatalf("expected the valid control request to succeed, got %v", err)
	}
}

func withTenant(base ContextResolutionRequest, tenantID string, mutate func(*ContextResolutionRequest)) ContextResolutionRequest {
	req := base
	req.TenantID = tenantID
	if mutate != nil {
		mutate(&req)
	}
	return req
}
