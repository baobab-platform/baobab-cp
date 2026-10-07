package repository

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestEnterProviderProvisioning proves, against real PostgreSQL, which canonical states may move into
// PROVISIONING_PROVIDERS, that a resumed execution is left alone, and that the revision the orchestrator holds is
// not disturbed. Set TEST_DATABASE_URL to run it.
func TestEnterProviderProvisioning(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	const tenantID = "tn_providerphase"
	cleanup := func() { pool.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id = $1`, tenantID) }
	cleanup()
	t.Cleanup(cleanup)

	for _, c := range []struct {
		from    string
		allowed bool
		want    string
	}{
		{"REGISTERING", true, "PROVISIONING_PROVIDERS"},
		{"CONFIGURING_CONTEXT", true, "PROVISIONING_PROVIDERS"},
		{"PROVISIONING_ENTITLEMENTS", true, "PROVISIONING_PROVIDERS"},
		{"PROVISIONING_PROVIDERS", true, "PROVISIONING_PROVIDERS"},
		{"VERIFYING_READINESS", true, "VERIFYING_READINESS"},
		{"REMEDIATING", true, "REMEDIATING"},
		{"PLANNED", false, "PLANNED"},
		{"BLOCKED", false, "BLOCKED"},
		{"FAILED", false, "FAILED"},
		{"CANCELLED", false, "CANCELLED"},
		{"ACTIVE", false, "ACTIVE"},
	} {
		cleanup()
		var id string
		var version int64
		if err := pool.QueryRow(ctx, `INSERT INTO provisioning.tenant_provisioning (tenant_id, idempotency_key, request_hash, state, status,
			completed_at, last_error) VALUES ($1, 'k', 'h', $2, CASE WHEN $2 = 'ACTIVE' THEN 'ACTIVE' WHEN $2 = 'FAILED' THEN 'FAILED'
			WHEN $2 = 'CANCELLED' THEN 'CANCELLED' ELSE 'APPLY' END, CASE WHEN $2 = 'ACTIVE' THEN now() END,
			CASE WHEN $2 = 'FAILED' THEN 'x' END) RETURNING tenant_provisioning_id::text, version`, tenantID, c.from).Scan(&id, &version); err != nil {
			t.Fatalf("%s: %v", c.from, err)
		}
		err := repo.EnterProviderProvisioning(ctx, id)
		if (err == nil) != c.allowed {
			t.Errorf("%s: allowed=%v err=%v", c.from, c.allowed, err)
		}
		var state string
		var after int64
		if err := pool.QueryRow(ctx, `SELECT state, version FROM provisioning.tenant_provisioning WHERE tenant_provisioning_id = $1::uuid`, id).Scan(&state, &after); err != nil {
			t.Fatal(err)
		}
		if state != c.want {
			t.Errorf("%s: state %s, want %s", c.from, state, c.want)
		}
		if after != version {
			t.Errorf("%s: the revision moved from %d to %d", c.from, version, after)
		}
	}
}
