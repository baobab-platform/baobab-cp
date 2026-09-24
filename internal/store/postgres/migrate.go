package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

// canonicalMigrationNames is the only migration sequence this package will ever
// apply. 000019-000021 were previously committed as a separate, unregistered
// golang-migrate-style up/down pair set (000001_control_plane, 000002_audit_identity,
// 000003_context_resolution_audit) that collided in version number with this list
// and was never applied by ApplyMigrations, even though internal/store/postgres's
// TenantStore implementation depends on the tables they create (tenants,
// legal_entities, product_subscriptions, provisioning_operations, outbox_events,
// audit_events). They are renumbered here to continue this sequence instead of
// silently shadowing it; see docs/reconciliation/shared-control-plane-audit.md §2.1.
var canonicalMigrationNames = []string{
	"000001_extensions_and_schemas.sql",
	"000002_canonical_registry.sql",
	"000003_canonical_relationships.sql",
	"000004_isolation_profiles.sql",
	"000005_engine_topology.sql",
	"000006_markets.sql",
	"000007_digital_estates.sql",
	"000008_capabilities.sql",
	"000009_mapping_scopes.sql",
	"000010_external_references.sql",
	"000011_canonical_mappings.sql",
	"000012_capability_bindings.sql",
	"000013_context_snapshots.sql",
	"000014_audit.sql",
	"000015_messaging.sql",
	"000016_idempotency_and_revisions.sql",
	"000017_indexes_and_integrity.sql",
	"000018_canonical_entity_versions.sql",
	"000019_tenant_lifecycle.sql",
	"000020_audit_identity_extensions.sql",
	"000021_context_resolution_audit_extensions.sql",
	"000022_canonical_mapping_temporal_integrity.sql",
	"000023_outbox_tenant_identity.sql",
	"000024_resolution_spine_invariants.sql",
	"000025_mapping_scope_dimensions.sql",
	"000026_canonical_identity.sql",
	"000027_identity_references.sql",
	"000028_capability_providers_and_binding_mode.sql",
	"000029_capability_scope_and_grant.sql",
	"000030_capability_lifecycle_maturity_and_dependency.sql",
	"000031_resolved_context_store.sql",
	"000032_workforce_membership.sql",
	"000033_product_registry_and_subscription.sql",
	"000034_capability_binding_scope_rewire.sql",
	"000035_retire_tenant_capability.sql",
	"000036_capability_composition_and_entitlement_projection.sql",
	"000037_market_participation_capabilities.sql",
	"000038_tenant_provisioning.sql",
	"000039_market_participation_governance.sql",
	"000040_trade_lanes.sql",
	"000041_market_assignment_legacy_status_backfill_fix.sql",
	"000042_tenant_manifest.sql",
	"000043_provisioning_readiness_snapshot.sql",
	"000044_provisioning_reconciliation_snapshot.sql",
	"000045_adr_bcp_018_organisation.sql",
}

func LoadMigrations() ([]Migration, error) {
	migrations := make([]Migration, 0, len(canonicalMigrationNames))
	for _, name := range canonicalMigrationNames {
		contents, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		version, err := strconv.Atoi(name[:6])
		if err != nil {
			return nil, fmt.Errorf("parse migration version %s: %w", name, err)
		}
		sum := sha256.Sum256(contents)
		migrations = append(migrations, Migration{Version: version, Name: name, SQL: string(contents), Checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("migration database pool is nil")
	}
	migrations, err := LoadMigrations()
	if err != nil {
		return err
	}

	// pg_advisory_lock is session-scoped: it must be acquired, held and
	// released on one physical connection, and nothing it is meant to
	// protect may run before it is held. pgxpool.Pool.Exec/Begin each borrow
	// a connection from the pool independently, so calling them directly
	// (as this function previously did) gives no real mutual exclusion
	// between two concurrent ApplyMigrations callers (e.g. two replicas of
	// cmd/migrate, or two test binaries against the same database) - they
	// can each run on different connections and race on
	// "CREATE SCHEMA IF NOT EXISTS system" before either has locked
	// anything, exactly as store_integration_test.go and
	// postgres_integration_test.go do when run concurrently against one
	// TEST_DATABASE_URL. Acquire one dedicated connection and run the whole
	// procedure on it instead.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(894217301)`); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(894217301)`)
	if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS system; CREATE TABLE IF NOT EXISTS system.schema_migration (version integer PRIMARY KEY, name text NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration journal: %w", err)
	}
	for _, migration := range migrations {
		var recordedName, checksum string
		err := conn.QueryRow(ctx, `SELECT name, checksum FROM system.schema_migration WHERE version = $1`, migration.Version).Scan(&recordedName, &checksum)
		if err == nil {
			if recordedName != migration.Name || checksum != migration.Checksum {
				return fmt.Errorf("migration %s checksum mismatch", migration.Name)
			}
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

func (m Migration) String() string {
	return strings.TrimSpace(fmt.Sprintf("%06d %s", m.Version, m.Name))
}
