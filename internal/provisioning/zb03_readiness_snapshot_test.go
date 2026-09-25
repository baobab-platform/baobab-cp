// Target path: internal/provisioning/zb03_readiness_snapshot_test.go
package provisioning

import (
	"context"
	"os"
	"testing"
	"time"

	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestReadinessEvaluationPersistsQueryableSnapshot proves the Gate ZB-03.1
// gap this file closes: a READY-phase evaluation now leaves a durable,
// queryable evidence record in provisioning.readiness_snapshot/
// readiness_check (migration 000043) -- previously ReadinessReport was
// computed on demand only, with nothing an operator could query afterward
// to see why a tenant was (or was not) READY.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestReadinessEvaluationPersistsQueryableSnapshot(t *testing.T) {
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

	f := seedZB02Fixture(t, ctx, admin, repo, "readinesssnap")
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM provisioning.readiness_snapshot WHERE tenant_id = $1`, f.TenantID)
	})

	resolved, err := ResolveManifest(ctx, repo, f.manifest())
	if err != nil {
		t.Fatalf("resolve manifest: %v", err)
	}
	scopeID, err := EnsureDefaultCapabilityScope(ctx, repo, f.TenantID)
	if err != nil {
		t.Fatalf("ensure default capability scope: %v", err)
	}

	now := time.Now().UTC()
	deps := ZB02Dependencies{
		Tenants: tenantStore, Repo: repo, Provisioning: repo,
		Now: func() time.Time { return now },
	}
	svc := service.TenantProvisioningService{Repository: repo, Now: func() time.Time { return now }}
	planned, err := svc.Plan(ctx, f.TenantID, "zb03-readiness-idem", "zb03-readiness-hash", nil, []string{"UG", "ZA"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	orch, err := BuildZB02Pipeline(deps, resolved, scopeID)
	if err != nil {
		t.Fatalf("build pipeline: %v", err)
	}
	final, err := orch.Run(ctx, planned.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if final.Status != provisioningdomain.ProvisioningStatusActive {
		t.Fatalf("expected ACTIVE, got %s (blocking_reasons=%v)", final.Status, final.BlockingReasons)
	}

	snapshots, err := repo.ListReadinessSnapshots(ctx, planned.ID)
	if err != nil {
		t.Fatalf("list readiness snapshots: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one persisted readiness snapshot")
	}
	latest := snapshots[0]
	if !latest.OverallReady {
		t.Fatalf("expected the persisted snapshot that led to ACTIVE to be overall_ready=true, got %+v", latest)
	}
	if latest.TenantID != f.TenantID {
		t.Fatalf("expected snapshot tenant_id %s, got %s", f.TenantID, latest.TenantID)
	}
	if len(latest.BlockingReasons) != 0 {
		t.Fatalf("expected no blocking reasons on the ready snapshot, got %v", latest.BlockingReasons)
	}

	wantChecks := []string{
		"market-participation", "capability-grants", "capability-bindings",
		"engine-instances", "context-resolution", "trade-lanes", "isolation-and-residency",
	}
	if len(latest.Checks) != len(wantChecks) {
		t.Fatalf("expected %d persisted checks, got %d: %+v", len(wantChecks), len(latest.Checks), latest.Checks)
	}
	seen := map[string]bool{}
	for _, c := range latest.Checks {
		seen[c.CheckKey] = true
		if c.Status != "PASS" {
			t.Fatalf("expected check %s to be PASS on the ready snapshot, got %s (reason=%q)", c.CheckKey, c.Status, c.Reason)
		}
		if c.ResourceType != c.CheckKey {
			t.Fatalf("expected resource_type to mirror check_key for %s, got %s", c.CheckKey, c.ResourceType)
		}
	}
	for _, want := range wantChecks {
		if !seen[want] {
			t.Fatalf("expected a persisted check for %q, got %+v", want, latest.Checks)
		}
	}

	// A provisioning run with no readiness evaluation ever recorded returns
	// an empty slice, not an error -- List semantics, not Get semantics.
	empty, err := repo.ListReadinessSnapshots(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("expected no error listing snapshots for an unknown provisioning id, got %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected an empty slice, got %+v", empty)
	}
}
