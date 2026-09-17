package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// TestPostgresTenantProvisioningConcurrentUpdatesOnlyOneWins is the ZB-02
// spec's concurrent-workers proof (§34): two real goroutines race to
// advance the *same* TenantProvisioning row from the *same* observed
// version -- worker A advancing RECONCILE -> READY, worker B racing a
// stale RECONCILE -> APPLY re-attempt against that same version. Exactly
// one UpdateTenantProvisioning MUST succeed; the other MUST fail with
// ErrTenantProvisioningVersionConflict, never silently overwrite the
// winner. This exercises PostgreSQL's own UPDATE ... WHERE version=$N
// row-affected check under genuine concurrency, not a simulated stale
// version from a single goroutine.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresTenantProvisioningConcurrentUpdatesOnlyOneWins(t *testing.T) {
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

	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	const provisioningID = "90000000-0000-0000-0000-0000000000c1"
	const tenantID = "tn_provisioning_concurrency_test"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id = $1`, tenantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	now := time.Now().UTC()
	seed := provisioningdomain.TenantProvisioning{
		ID: provisioningID, TenantID: tenantID, IdempotencyKey: "idem-concurrency", RequestHash: "hash-concurrency",
		Status:               provisioningdomain.ProvisioningStatusReconcile,
		DesiredStateVersion:  1,
		ObservedStateVersion: 1,
		StartedAt:            now,
		Version:              1,
	}
	if err := repo.CreateTenantProvisioning(ctx, seed); err != nil {
		t.Fatalf("seed tenant provisioning: %v", err)
	}

	// Both workers observe the same version-1 row before either writes.
	observed, err := repo.GetTenantProvisioning(ctx, provisioningID)
	if err != nil {
		t.Fatalf("observe tenant provisioning: %v", err)
	}

	toReady, err := observed.Advance(provisioningdomain.ProvisioningStatusReady, "")
	if err != nil {
		t.Fatalf("worker A: advance to READY: %v", err)
	}
	toApply, err := observed.Advance(provisioningdomain.ProvisioningStatusApply, "worker B stale retry")
	if err != nil {
		t.Fatalf("worker B: advance to APPLY: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = repo.UpdateTenantProvisioning(ctx, toReady, observed.Version)
	}()
	go func() {
		defer wg.Done()
		errs[1] = repo.UpdateTenantProvisioning(ctx, toApply, observed.Version)
	}()
	wg.Wait()

	succeeded := 0
	conflicted := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrTenantProvisioningVersionConflict):
			conflicted++
		default:
			t.Fatalf("unexpected error from concurrent update: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("expected exactly one winner and one version conflict, got succeeded=%d conflicted=%d (errs=%v)", succeeded, conflicted, errs)
	}

	final, err := repo.GetTenantProvisioning(ctx, provisioningID)
	if err != nil {
		t.Fatalf("get final tenant provisioning: %v", err)
	}
	if final.Version != observed.Version+1 {
		t.Fatalf("expected exactly one version increment from two racing updates, got version=%d", final.Version)
	}
	if final.Status != provisioningdomain.ProvisioningStatusReady && final.Status != provisioningdomain.ProvisioningStatusApply {
		t.Fatalf("expected the winning status to be READY or APPLY (whichever update actually committed), got %s", final.Status)
	}
	// Whichever worker won, the loser's write must not also have landed --
	// the final status can only reflect one of the two racing updates, and
	// the succeeded/conflicted counts above already proved only one write
	// was accepted by PostgreSQL's own WHERE version=$N check.
}
