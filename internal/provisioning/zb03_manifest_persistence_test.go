// Target path: internal/provisioning/zb03_manifest_persistence_test.go
package provisioning

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTenantManifestSurvivesProcessRestart proves the Gate ZB-03.1 gap this
// file closes: a process that restarts mid-provisioning can rebuild the
// exact same Orchestrator pipeline from PostgreSQL alone, without any
// in-memory ResolvedManifest surviving the restart -- unlike
// TestZB02OrchestrationRecoversAfterSimulatedCrash (which proves orchestrator
// *state* recovery but still hands the "restarted" process the same
// already-resolved manifest value via a shared Go closure), "process 2" here
// obtains its ResolvedManifest purely by calling GetTenantManifest +
// RehydrateResolvedManifest -- nothing about the original `resolved` value
// is referenced after the manifest is persisted.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestTenantManifestSurvivesProcessRestart(t *testing.T) {
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

	f := seedZB02Fixture(t, ctx, admin, repo, "manifestrestart")

	// -- "process 1": resolve the manifest, persist both the TenantProvisioning
	// row and its resolved-manifest snapshot atomically, then forget
	// `resolved` ever existed (it goes out of scope below).
	provisioningID := domain.NewUUIDv7()
	now := time.Now().UTC()
	var scopeID string
	{
		resolved, err := ResolveManifest(ctx, repo, f.manifest())
		if err != nil {
			t.Fatalf("resolve manifest: %v", err)
		}
		scopeID, err = EnsureDefaultCapabilityScope(ctx, repo, f.TenantID)
		if err != nil {
			t.Fatalf("ensure default capability scope: %v", err)
		}
		record, err := NewTenantManifestRecord(provisioningID, f.manifest(), resolved, "test")
		if err != nil {
			t.Fatalf("build manifest record: %v", err)
		}
		seed := provisioningdomain.TenantProvisioning{
			ID: provisioningID, TenantID: f.TenantID,
			IdempotencyKey: "zb03-manifest-restart-idem", RequestHash: "zb03-manifest-restart-hash",
			Status:              provisioningdomain.ProvisioningStatusPlan,
			DesiredStateVersion: resolved.DesiredStateVersion,
			StartedAt:           now, Version: 1,
		}
		if err := repo.CreateTenantProvisioningWithManifest(ctx, seed, record); err != nil {
			t.Fatalf("create tenant provisioning with manifest: %v", err)
		}
	}

	// -- "process 2": knows only provisioningID and scopeID (both opaque
	// strings a restarted process would recover from its own durable
	// bookkeeping, e.g. a work queue) -- it rehydrates the ResolvedManifest
	// from PostgreSQL and builds a brand new Orchestrator from that alone.
	record, err := repo.GetTenantManifest(ctx, provisioningID)
	if err != nil {
		t.Fatalf("get tenant manifest: %v", err)
	}
	if record.SchemaVersion != "baobab.nabhold.com/v1" {
		t.Fatalf("expected schema_version to be persisted, got %q", record.SchemaVersion)
	}
	if record.ManifestHash == "" {
		t.Fatal("expected a non-empty manifest_hash")
	}
	if record.Source != "test" {
		t.Fatalf("expected source=test, got %q", record.Source)
	}

	rehydrated, err := RehydrateResolvedManifest(record)
	if err != nil {
		t.Fatalf("rehydrate resolved manifest: %v", err)
	}
	if rehydrated.TenantID != f.TenantID {
		t.Fatalf("expected rehydrated tenant_id %s, got %s", f.TenantID, rehydrated.TenantID)
	}
	if len(rehydrated.Markets) != 2 || len(rehydrated.TradeLanes) != 1 {
		t.Fatalf("expected rehydrated manifest to round-trip its markets/trade lanes, got %+v", rehydrated)
	}

	deps := ZB02Dependencies{
		Tenants: tenantStore, Repo: repo, Provisioning: repo,
		Now: func() time.Time { return now },
	}
	restartedOrchestrator, err := BuildZB02Pipeline(deps, rehydrated, scopeID)
	if err != nil {
		t.Fatalf("build orchestrator from rehydrated manifest: %v", err)
	}
	final, err := restartedOrchestrator.Run(ctx, provisioningID)
	if err != nil {
		t.Fatalf("run orchestrator from rehydrated manifest: %v", err)
	}
	if final.Status != provisioningdomain.ProvisioningStatusActive {
		t.Fatalf("expected the rehydrated pipeline to reach ACTIVE, got %s (blocking_reasons=%v)", final.Status, final.BlockingReasons)
	}

	// A provisioning ID with no manifest ever recorded fails closed, not
	// with a zero-value ResolvedManifest silently accepted downstream.
	if _, err := repo.GetTenantManifest(ctx, domain.NewUUIDv7()); err == nil {
		t.Fatal("expected an error for a provisioning id with no recorded manifest")
	} else if err != repository.ErrTenantManifestNotFound {
		t.Fatalf("expected ErrTenantManifestNotFound, got %v", err)
	}
}
