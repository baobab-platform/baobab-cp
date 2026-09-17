package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// TestPostgresTenantProvisioningRoundTrip proves CreateTenantProvisioning
// (including its idempotency-key UNIQUE constraint), GetTenantProvisioning,
// GetTenantProvisioningByIdempotencyKey, ListTenantProvisioningsForTenant
// and UpdateTenantProvisioning's optimistic lock round-trip against a real
// PostgreSQL instance (migration 000038_tenant_provisioning.sql), not just
// compile.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresTenantProvisioningRoundTrip(t *testing.T) {
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

	const provisioningID = "90000000-0000-0000-0000-0000000000f1"
	const tenantID = "tn_provisioningtest"

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
	provisioning := provisioningdomain.TenantProvisioning{
		ID: provisioningID, TenantID: tenantID, IdempotencyKey: "idem-1", RequestHash: "hash-1",
		Status:          provisioningdomain.ProvisioningStatusPlan,
		ProductRequests: []string{"solution.baobab-xbt"},
		MarketRequests:  []string{"UG", "ZA"},
		StartedAt:       now,
		Version:         1,
	}
	if err := repo.CreateTenantProvisioning(ctx, provisioning); err != nil {
		t.Fatalf("create tenant provisioning: %v", err)
	}

	duplicate := provisioning
	duplicate.ID = "90000000-0000-0000-0000-0000000000f2"
	if err := repo.CreateTenantProvisioning(ctx, duplicate); !errors.Is(err, ErrTenantProvisioningAlreadyExists) {
		t.Fatalf("expected ErrTenantProvisioningAlreadyExists for a duplicate idempotency key, got %v", err)
	}

	fetched, err := repo.GetTenantProvisioning(ctx, provisioningID)
	if err != nil {
		t.Fatalf("get tenant provisioning: %v", err)
	}
	if fetched.Status != provisioningdomain.ProvisioningStatusPlan || len(fetched.ProductRequests) != 1 || len(fetched.MarketRequests) != 2 {
		t.Fatalf("unexpected fetched tenant provisioning: %+v", fetched)
	}

	byKey, err := repo.GetTenantProvisioningByIdempotencyKey(ctx, tenantID, "idem-1")
	if err != nil || byKey.ID != provisioningID {
		t.Fatalf("get by idempotency key: %v, %+v", err, byKey)
	}

	applied, err := fetched.Advance(provisioningdomain.ProvisioningStatusApply, "")
	if err != nil {
		t.Fatalf("advance to APPLY: %v", err)
	}
	if err := repo.UpdateTenantProvisioning(ctx, applied, fetched.Version); err != nil {
		t.Fatalf("update tenant provisioning: %v", err)
	}

	// The now-stale expectedVersion (the pre-update version) must be
	// rejected.
	if err := repo.UpdateTenantProvisioning(ctx, applied, fetched.Version); !errors.Is(err, ErrTenantProvisioningVersionConflict) {
		t.Fatalf("expected ErrTenantProvisioningVersionConflict for a stale expectedVersion, got %v", err)
	}

	reconciled, err := repo.GetTenantProvisioning(ctx, provisioningID)
	if err != nil {
		t.Fatalf("re-get tenant provisioning: %v", err)
	}
	if reconciled.Status != provisioningdomain.ProvisioningStatusApply || reconciled.Version != fetched.Version+1 {
		t.Fatalf("unexpected state after update: %+v", reconciled)
	}

	all, err := repo.ListTenantProvisioningsForTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list tenant provisionings: %v", err)
	}
	if len(all) != 1 || all[0].ID != provisioningID {
		t.Fatalf("expected exactly one tenant provisioning for %s, got %+v", tenantID, all)
	}
}
