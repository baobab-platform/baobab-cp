package provisioning

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestZB02ResourcesFailClosedAcrossTenants proves spec §42's multi-tenant
// isolation requirement for the resources this Gate ZB-02 pass added:
// CapabilityScope, MarketAssignment, CapabilityGrant, CapabilityBinding and
// TradeLane. Tenant A is provisioned to ACTIVE for real; tenant B (a
// completely independent tenant/legal entity, seeded the same way) then
// attempts to reference tenant A's resources -- its market participation,
// its capability scope, its trade lane -- and every attempt must fail
// closed, never silently resolve or bind to the other tenant's state.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestZB02ResourcesFailClosedAcrossTenants(t *testing.T) {
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

	// Two completely independent tenants/legal entities/markets/
	// capabilities/engines -- nothing shared between them except the code
	// under test.
	tenantA := seedZB02Fixture(t, ctx, admin, repo, "isoa")
	tenantB := seedZB02Fixture(t, ctx, admin, repo, "isob")

	now := time.Now().UTC()
	deps := ZB02Dependencies{Tenants: tenantStore, Repo: repo, Provisioning: repo, Now: func() time.Time { return now }}
	svc := service.TenantProvisioningService{Repository: repo, Now: func() time.Time { return now }}

	// -- provision tenant A for real, so there is real state to attempt
	// cross-tenant access against, not just empty rows --
	resolvedA, err := ResolveManifest(ctx, repo, tenantA.manifest())
	if err != nil {
		t.Fatalf("resolve tenant A manifest: %v", err)
	}
	scopeA, err := EnsureDefaultCapabilityScope(ctx, repo, tenantA.TenantID)
	if err != nil {
		t.Fatalf("ensure tenant A capability scope: %v", err)
	}
	orchestratorA, err := BuildZB02Pipeline(deps, resolvedA, scopeA)
	if err != nil {
		t.Fatalf("build tenant A pipeline: %v", err)
	}
	plannedA, err := svc.Plan(ctx, tenantA.TenantID, "iso-a-idem-1", "iso-a-hash-1", nil, []string{"UG", "ZA"})
	if err != nil {
		t.Fatalf("plan tenant A: %v", err)
	}
	finalA, err := orchestratorA.Run(ctx, plannedA.ID)
	if err != nil {
		t.Fatalf("run tenant A orchestrator: %v", err)
	}
	if finalA.Status != "ACTIVE" {
		t.Fatalf("expected tenant A to reach ACTIVE, got %s (blocking_reasons=%v)", finalA.Status, finalA.BlockingReasons)
	}

	scopeB, err := EnsureDefaultCapabilityScope(ctx, repo, tenantB.TenantID)
	if err != nil {
		t.Fatalf("ensure tenant B capability scope: %v", err)
	}
	if scopeA == scopeB {
		t.Fatal("expected distinct tenants to receive distinct default capability scopes")
	}

	// -- (1) MarketAssignment: tenant B has no participation in tenant A's
	// market, even though the market row itself is shared reference data --
	if _, err := repo.GetEffectiveMarketAssignment(ctx, tenantB.TenantID, tenantA.MarketUGID, now); err == nil {
		t.Fatal("expected tenant B to have no market participation for tenant A's market")
	}

	// -- (2) CapabilityScope: a grant desired under tenant B's identity but
	// pointed at tenant A's scope must be rejected, not silently bound to
	// tenant A's scope --
	grantProvisioner := NewCapabilityGrantProvisioner(repo)
	if _, _, err := grantProvisioner.Apply(ctx, DesiredCapabilityGrant{
		TenantID: tenantB.TenantID, CapabilityKey: tenantB.CapabilityKey, ScopeID: scopeA,
		Source: capabilitydomain.GrantSourcePlatformBaseline, EffectiveFrom: now,
	}); err == nil || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("expected a tenant-mismatch error binding tenant B's grant to tenant A's scope, got %v", err)
	}

	// -- (3) CapabilityBinding: same fail-closed requirement for bindings --
	bindingProvisioner := NewCapabilityBindingProvisioner(repo)
	if _, _, err := bindingProvisioner.Apply(ctx, DesiredCapabilityBinding{
		CapabilityKey: tenantB.CapabilityKey, EngineID: tenantB.EngineID, EngineInstanceID: tenantB.InstanceID,
		ScopeID: scopeA, BindingMode: capabilitydomain.BindingModePrimary, ContractVersion: "v1", EffectiveFrom: now,
	}, resolver.Context{TenantID: tenantB.TenantID}); err == nil {
		t.Fatal("expected a tenant-mismatch error binding tenant B's capability binding to tenant A's scope")
	}

	// -- (4) TradeLane: tenant B cannot read tenant A's trade lane by ID,
	// even knowing its deterministic ID --
	laneA := deterministicTradeLaneID(tenantA.TenantID, tenantA.MarketUGID, tenantA.MarketZAID, domain.TradeLaneCrossMarket)
	if _, err := repo.GetTradeLane(ctx, tenantB.TenantID, laneA); err == nil {
		t.Fatal("expected tenant B to be unable to read tenant A's trade lane")
	}
	// The lane genuinely exists -- for tenant A.
	if _, err := repo.GetTradeLane(ctx, tenantA.TenantID, laneA); err != nil {
		t.Fatalf("expected tenant A's own trade lane to be readable, got %v", err)
	}

	// -- (5) CapabilityScope itself never silently aliases across tenants:
	// reading tenant A's own scope by ID still reports tenant A as owner --
	scope, err := repo.GetCapabilityScope(ctx, scopeA)
	if err != nil {
		t.Fatalf("get tenant A's own scope: %v", err)
	}
	if scope.TenantID != tenantA.TenantID {
		t.Fatalf("expected capability scope %s to report tenant A as owner, got %q", scopeA, scope.TenantID)
	}
}
