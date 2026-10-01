package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	pool := throwawayDatabase(t, ctx, baseURL)

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

// throwawayDatabase creates an empty database for one test and drops it
// when the test ends.
func throwawayDatabase(t *testing.T, ctx context.Context, baseURL string) *pgxpool.Pool {
	t.Helper()
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
	return pool
}

// pgxIdentifier quotes name as a PostgreSQL identifier for use in DDL where
// a bind parameter isn't accepted (CREATE/DROP DATABASE). name here is
// always this test's own generated dbName, never external input.
func pgxIdentifier(name string) string {
	return `"` + name + `"`
}

// TestBindingProviderBackfill upgrades bindings created before providers
// were enforced (000077): a binding whose engine has exactly one ACTIVE
// provider supporting its capability takes it; one with none or several
// is left for an operator, reported with the reason, and the constraint,
// added NOT VALID, lets the migration through but refuses changing such a
// binding while it stays ACTIVE.
func TestBindingProviderBackfill(t *testing.T) {
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	pool := throwawayDatabase(t, ctx, baseURL)
	migrations, err := LoadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigrationsThrough(ctx, pool, migrations, 76); err != nil {
		t.Fatalf("apply migrations through 000076: %v", err)
	}

	const capability = "80000000-0000-0000-0000-000000000001"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO capability.capability(capability_id, code, name) VALUES ($1, 'test.backfill.capability', 'Backfill')`, capability)
	provider := func(key, engine, status string) {
		exec(`WITH p AS (INSERT INTO capability.capability_provider(provider_key, name, provider_type, engine_id, status)
			VALUES ($1, $1, 'BAOBAB_ENGINE', $2::uuid, $3) RETURNING provider_id)
			INSERT INTO capability.provider_capability_support(provider_id, capability_id, contract_versions) SELECT provider_id, $4::uuid, '{1}' FROM p`,
			key, engine, status, capability)
	}
	// One binding per engine, each in its own scope: the single-provider
	// engine, the two-provider engine, the engine with only a suspended
	// provider, and the engine with none.
	bindings := map[string]string{}
	for i, name := range []string{"single", "ambiguous", "suspended", "none"} {
		engine := fmt.Sprintf("80000000-0000-0000-0000-00000000010%d", i)
		instance := fmt.Sprintf("80000000-0000-0000-0000-00000000020%d", i)
		scope := fmt.Sprintf("80000000-0000-0000-0000-00000000030%d", i)
		binding := fmt.Sprintf("80000000-0000-0000-0000-00000000040%d", i)
		exec(`INSERT INTO topology.engine(engine_id, code, name) VALUES ($1::uuid, $2, $2)`, engine, "backfill-"+name)
		exec(`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status) VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE')`, instance, engine)
		exec(`INSERT INTO capability.capability_scope(scope_id, tenant_id) VALUES ($1::uuid, $2)`, scope, "tn_backfill"+name)
		exec(`INSERT INTO capability.capability_binding(id, capability_id, engine_instance_id, scope_id, binding_mode, priority, status, contract_version, effective_from)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'PRIMARY', 1, 'ACTIVE', 1, now())`, binding, capability, instance, scope)
		bindings[name] = binding
		switch name {
		case "single":
			provider("backfill-single.engine", engine, "ACTIVE")
		case "ambiguous":
			provider("backfill-ambiguous.one", engine, "ACTIVE")
			provider("backfill-ambiguous.two", engine, "ACTIVE")
		case "suspended":
			provider("backfill-suspended.engine", engine, "SUSPENDED")
		}
	}

	if err := applyMigrationsThrough(ctx, pool, migrations, 82); err != nil {
		t.Fatalf("apply 000077 over bindings without providers: %v", err)
	}

	var single string
	if err := pool.QueryRow(ctx, `SELECT cp.provider_key FROM capability.capability_binding cb JOIN capability.capability_provider cp USING (provider_id) WHERE cb.id = $1::uuid`,
		bindings["single"]).Scan(&single); err != nil || single != "backfill-single.engine" {
		t.Fatalf("the single-provider binding was not backfilled: %q %v", single, err)
	}
	reported := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT binding_id::text, reason FROM capability.binding_without_provider WHERE capability_key = 'test.backfill.capability'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, reason string
		if err := rows.Scan(&id, &reason); err != nil {
			t.Fatal(err)
		}
		reported[id] = reason
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{bindings["ambiguous"]: "AMBIGUOUS_PROVIDER", bindings["suspended"]: "NO_PROVIDER", bindings["none"]: "NO_PROVIDER"}
	if fmt.Sprint(reported) != fmt.Sprint(want) {
		t.Fatalf("binding_without_provider = %v, want %v", reported, want)
	}

	// An unresolved binding may not change while it stays ACTIVE, but may
	// leave ACTIVE.
	_, err = pool.Exec(ctx, `UPDATE capability.capability_binding SET priority = 2 WHERE id = $1::uuid`, bindings["none"])
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "capability_binding_active_provider_check" {
		t.Fatalf("changing an ACTIVE binding without a provider: %v", err)
	}
	exec(`UPDATE capability.capability_binding SET status = 'SUSPENDED' WHERE id = $1::uuid`, bindings["none"])

	// EA-02E (000083) fails loudly while any ACTIVE binding names no
	// provider: it names each one and its reason, and changes nothing.
	err = ApplyMigrations(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), bindings["ambiguous"]+" (AMBIGUOUS_PROVIDER") ||
		!strings.Contains(err.Error(), bindings["suspended"]+" (NO_PROVIDER") || strings.Contains(err.Error(), bindings["none"]) {
		t.Fatalf("000083 over unresolved bindings: %v", err)
	}
	var applied bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM system.schema_migration WHERE version = 83)`).Scan(&applied); err != nil || applied {
		t.Fatalf("000083 was recorded although it failed: %v %v", applied, err)
	}

	// The operator resolves them: names the ambiguous binding's provider,
	// and activates the suspended engine's provider, which 000083 then
	// backfills. A provider that does not support a binding's contract
	// major is never its candidate.
	exec(`UPDATE capability.capability_binding SET provider_id = (SELECT provider_id FROM capability.capability_provider WHERE provider_key = 'backfill-ambiguous.two')
		WHERE id = $1::uuid`, bindings["ambiguous"])
	exec(`UPDATE capability.capability_provider SET status = 'ACTIVE' WHERE provider_key = 'backfill-suspended.engine'`)
	exec(`INSERT INTO capability.capability_binding(id, capability_id, engine_instance_id, scope_id, binding_mode, priority, status, contract_version, effective_from)
		VALUES ('80000000-0000-0000-0000-000000000499', $1::uuid, '80000000-0000-0000-0000-000000000200', '80000000-0000-0000-0000-000000000300',
			'PRIMARY', 2, 'SUSPENDED', 'v2', now())`, capability)
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("000083 after the bindings were resolved: %v", err)
	}
	var suspended string
	var validated bool
	var candidates int
	if err := pool.QueryRow(ctx, `SELECT
			(SELECT cp.provider_key FROM capability.capability_binding cb JOIN capability.capability_provider cp USING (provider_id) WHERE cb.id = $1::uuid),
			(SELECT convalidated FROM pg_constraint WHERE conname = 'capability_binding_active_provider_check'),
			(SELECT count(*) FROM capability.binding_provider_candidate WHERE binding_id = '80000000-0000-0000-0000-000000000499')`,
		bindings["suspended"]).Scan(&suspended, &validated, &candidates); err != nil {
		t.Fatal(err)
	}
	if suspended != "backfill-suspended.engine" || !validated || candidates != 0 {
		t.Fatalf("after 000083: provider %q, constraint validated %v, candidates for a v2 binding %d", suspended, validated, candidates)
	}
}
