// Target path: internal/provisioning/zb03_reconciliation_snapshot_test.go
package provisioning

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// TestReconciliationEvaluationPersistsQueryableSnapshot proves the Gate
// ZB-03.1 gap this file closes: a RECONCILE-phase evaluation now leaves a
// durable, queryable evidence record in
// provisioning.reconciliation_snapshot/resource_drift (migration 000044) --
// previously ReconciliationReport was computed on demand only. Drives a
// real UG/ZA manifest to ACTIVE (proving the real ReconcileWorker ->
// Snapshots -> repository wiring fires, not just the conversion helper in
// isolation) and asserts the converged snapshot has zero drift rows.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestReconciliationEvaluationPersistsQueryableSnapshot(t *testing.T) {
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

	f := seedZB02Fixture(t, ctx, admin, repo, "reconcilesnap")
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM provisioning.reconciliation_snapshot WHERE tenant_id = $1`, f.TenantID)
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
	planned, err := svc.Plan(ctx, f.TenantID, "zb03-reconcile-idem", "zb03-reconcile-hash", nil, []string{"UG", "ZA"})
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

	snapshots, err := repo.ListReconciliationSnapshots(ctx, planned.ID)
	if err != nil {
		t.Fatalf("list reconciliation snapshots: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one persisted reconciliation snapshot")
	}
	latest := snapshots[0]
	if !latest.Converged {
		t.Fatalf("expected the snapshot that led to ACTIVE to be converged, got %+v", latest)
	}
	if latest.TenantID != f.TenantID {
		t.Fatalf("expected snapshot tenant_id %s, got %s", f.TenantID, latest.TenantID)
	}
	if len(latest.Drift) != 0 {
		t.Fatalf("expected zero drift rows on the converged snapshot, got %+v", latest.Drift)
	}

	empty, err := repo.ListReconciliationSnapshots(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("expected no error listing snapshots for an unknown provisioning id, got %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected an empty slice, got %+v", empty)
	}
}

// TestReconciliationSnapshotRoundTripsDriftFields directly exercises
// reconciliationSnapshotRecordFromReport (the conversion helper
// ReconcileWorker.Run uses) against a fabricated report containing real
// drift, then persists and re-reads it -- proving every drift field
// (resource_type/id, drift_kind, desired/observed hash, repairable,
// blocking, reason) round-trips through PostgreSQL correctly, including
// the MISSING-drift case where no hashes exist to persist.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestReconciliationSnapshotRoundTripsDriftFields(t *testing.T) {
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

	const tenantID = "tn_zb03reconcilefields"
	const provisioningID = "90000000-0000-0000-0000-0000000000d1"
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM provisioning.reconciliation_snapshot WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id = $1`, tenantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	now := time.Now().UTC()
	seed := provisioningdomain.TenantProvisioning{
		ID: provisioningID, TenantID: tenantID, IdempotencyKey: "idem-reconcile-fields", RequestHash: "hash-reconcile-fields",
		Status: provisioningdomain.ProvisioningStatusReconcile, DesiredStateVersion: 2, ObservedStateVersion: 1,
		StartedAt: now, Version: 1,
	}
	if err := repo.CreateTenantProvisioning(ctx, seed); err != nil {
		t.Fatalf("seed tenant provisioning: %v", err)
	}

	report := ReconciliationReport{
		TenantID: tenantID, ProvisioningID: provisioningID,
		DesiredStateVersion: 2, ObservedStateVersion: 1, Converged: false, EvaluatedAt: now,
		Drift: []Drift{
			{
				ResourceType: "capability-grant", ResourceKey: "trade.settlement|scope-1", Kind: DriftMismatch,
				Reason: "observed state differs from desired state", Repairable: true,
				DesiredHash: "hash-desired-1", ObservedHash: "hash-observed-1",
			},
			{
				ResourceType: "market-participation", ResourceKey: "market-ug", Kind: DriftMissing,
				Reason: "desired resource is not observed", Repairable: true,
			},
		},
	}
	record := reconciliationSnapshotRecordFromReport(provisioningID, report)
	if len(record.Drift) != 2 {
		t.Fatalf("expected 2 drift records from the conversion helper, got %d: %+v", len(record.Drift), record.Drift)
	}
	for _, d := range record.Drift {
		if !d.Blocking {
			t.Fatalf("expected every drift record to be Blocking=true, got %+v", d)
		}
		if d.ResolvedAt != nil {
			t.Fatalf("expected ResolvedAt to be nil (not yet tracked), got %+v", d)
		}
	}

	snapshotID, err := repo.SaveReconciliationSnapshot(ctx, record)
	if err != nil {
		t.Fatalf("save reconciliation snapshot: %v", err)
	}
	if snapshotID == "" {
		t.Fatal("expected a generated snapshot id")
	}

	snapshots, err := repo.ListReconciliationSnapshots(ctx, provisioningID)
	if err != nil {
		t.Fatalf("list reconciliation snapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected exactly 1 snapshot, got %d", len(snapshots))
	}
	got := snapshots[0]
	if got.Converged {
		t.Fatal("expected converged=false to round-trip")
	}
	if len(got.Drift) != 2 {
		t.Fatalf("expected 2 persisted drift rows, got %d: %+v", len(got.Drift), got.Drift)
	}

	byType := map[string]provisioningdomain.ResourceDriftRecord{}
	for _, d := range got.Drift {
		byType[d.ResourceType] = d
	}
	mismatch, ok := byType["capability-grant"]
	if !ok {
		t.Fatalf("expected a capability-grant drift row, got %+v", got.Drift)
	}
	if mismatch.DriftKind != "MISMATCH" || mismatch.ResourceID != "trade.settlement|scope-1" ||
		mismatch.DesiredHash != "hash-desired-1" || mismatch.ObservedHash != "hash-observed-1" || !mismatch.Repairable {
		t.Fatalf("mismatch drift row round-tripped incorrectly: %+v", mismatch)
	}
	missing, ok := byType["market-participation"]
	if !ok {
		t.Fatalf("expected a market-participation drift row, got %+v", got.Drift)
	}
	if missing.DriftKind != "MISSING" || missing.DesiredHash != "" || missing.ObservedHash != "" {
		t.Fatalf("missing drift row round-tripped incorrectly (expected empty hashes): %+v", missing)
	}
}
