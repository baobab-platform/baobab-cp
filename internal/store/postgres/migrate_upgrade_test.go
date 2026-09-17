package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// applyMigrationsThrough replicates ApplyMigrations' per-migration
// transaction/journal logic, but stops after the given version -- used to
// reconstruct "the schema as it existed before a later migration" for an
// upgrade-path test.
func applyMigrationsThrough(ctx context.Context, pool *pgxpool.Pool, migrations []Migration, through int) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS system; CREATE TABLE IF NOT EXISTS system.schema_migration (version integer PRIMARY KEY, name text NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration journal: %w", err)
	}
	for _, migration := range migrations {
		if migration.Version > through {
			continue
		}
		var recordedName string
		err := conn.QueryRow(ctx, `SELECT name FROM system.schema_migration WHERE version = $1`, migration.Version).Scan(&recordedName)
		if err == nil {
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read migration journal for %s: %w", migration.Name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", migration.Name, err)
		}
		if _, err = tx.Exec(ctx, migration.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO system.schema_migration(version, name, checksum) VALUES ($1, $2, $3)`, migration.Version, migration.Name, migration.Checksum); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", migration.Name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", migration.Name, err)
		}
	}
	return nil
}

// TestApplyMigrationsUpgradesExistingMarketAssignmentSafely is the ZB-02
// spec's migration Scenario B/C (§44): a database carrying representative
// pre-ZB-02 data (a market.market_assignment row from before migration
// 000039 introduced status/source/policy_version) is migrated to latest,
// and the backfill must not silently change that row's operational
// semantics. It also re-runs ApplyMigrations a second time (Scenario A's
// idempotency check applied to an already-upgraded database).
//
// This test creates and drops its own throwaway database (derived from
// TEST_DATABASE_URL) rather than sharing the database other packages'
// tests use, since it deliberately applies only a prefix of the canonical
// migration sequence before continuing -- a state no other test may
// observe. Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestApplyMigrationsUpgradesExistingMarketAssignmentSafely(t *testing.T) {
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}

	adminURL := *parsed
	adminURL.Path = "/postgres"
	adminPool, err := pgxpool.New(ctx, adminURL.String())
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	// t.Cleanup runs LIFO, and (unlike a defer in this same function) always
	// runs *after* every defer in this function has already fired. A
	// `defer adminPool.Close()` here would therefore close adminPool before
	// the DROP DATABASE cleanup below ever got to use it, and pgxpool.Exec
	// on a closed pool fails silently, leaking the throwaway database on
	// every run. Register every close/drop as t.Cleanup instead, in
	// registration order [adminPool.Close, dropDatabase, pool.Close] so
	// they execute in the reverse, correct order: pool.Close, dropDatabase,
	// adminPool.Close.
	t.Cleanup(adminPool.Close)

	dbName := fmt.Sprintf("baobab_cp_migration_upgrade_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+pgxIdentifier(dbName)); err != nil {
		t.Fatalf("create throwaway database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := adminPool.Exec(cleanupCtx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName); err != nil {
			t.Logf("terminate backends for throwaway database %s: %v", dbName, err)
		}
		if _, err := adminPool.Exec(cleanupCtx, `DROP DATABASE IF EXISTS `+pgxIdentifier(dbName)); err != nil {
			t.Logf("drop throwaway database %s: %v", dbName, err)
		}
	})

	testURL := *parsed
	testURL.Path = "/" + dbName
	pool, err := pgxpool.New(ctx, testURL.String())
	if err != nil {
		t.Fatalf("connect throwaway database: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := LoadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}

	// -- Scenario B, part 1: build the pre-ZB-02 schema (everything before
	// 000039's governance columns) --
	const preZB02Version = 38
	if err := applyMigrationsThrough(ctx, pool, migrations, preZB02Version); err != nil {
		t.Fatalf("apply pre-ZB-02 migrations: %v", err)
	}

	// -- seed a representative pre-existing row exactly as it would have
	// existed under the pre-000039 schema: no status/source/policy_version
	// columns exist yet at all --
	const (
		marketID     = "70000000-0000-0000-0000-0000000af001"
		assignmentID = "70000000-0000-0000-0000-0000000af002"
		tenantID     = "tn_migration_upgrade_test"
	)
	if _, err := pool.Exec(ctx, `INSERT INTO market.market(market_id, code, name, currency, region, is_active) VALUES ($1::uuid, 'MU', 'Migration Upgrade Test Market', 'USD', 'af-south-1', true)`, marketID); err != nil {
		t.Fatalf("seed market: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO market.market_assignment(market_assignment_id, tenant_id, market_id, effective_from) VALUES ($1::uuid, $2, $3::uuid, now() - interval '30 days')`, assignmentID, tenantID, marketID); err != nil {
		t.Fatalf("seed pre-existing market assignment: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO market.market_participation_capability(market_assignment_id, capability) VALUES ($1::uuid, 'SELLING')`, assignmentID); err != nil {
		t.Fatalf("seed pre-existing capability: %v", err)
	}

	// -- Scenario B, part 2: migrate to latest --
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	// -- the backfill must not have silently deactivated a participation
	// that was already effective before governance columns existed --
	var status, source, policyVersion string
	if err := pool.QueryRow(ctx, `SELECT status, source, policy_version FROM market.market_assignment WHERE market_assignment_id = $1::uuid`, assignmentID).Scan(&status, &source, &policyVersion); err != nil {
		t.Fatalf("read backfilled row: %v", err)
	}
	if status != "ACTIVE" {
		t.Fatalf("expected the migration to backfill a pre-existing, already-effective participation as ACTIVE (not silently deactivate it via a PENDING default), got status=%q", status)
	}
	if source != "MIGRATION" {
		t.Fatalf("expected source=MIGRATION provenance for a backfilled row, got %q", source)
	}
	if policyVersion == "" {
		t.Fatal("expected a non-empty policy_version to have been backfilled")
	}

	// -- Scenario A style idempotency: re-running ApplyMigrations against
	// an already fully-migrated database must be a safe no-op --
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("re-apply migrations against an up-to-date database: %v", err)
	}
}

// pgxIdentifier quotes name as a PostgreSQL identifier for use in DDL where
// a bind parameter isn't accepted (CREATE/DROP DATABASE). name here is
// always this test's own generated dbName, never external input.
func pgxIdentifier(name string) string {
	return `"` + name + `"`
}
