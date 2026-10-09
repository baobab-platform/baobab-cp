package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LA-02 exercises the real upgrade from v1, not a fresh hand-written schema.
// TEST_DATABASE_URL must name a PostgreSQL administrator capable of creating
// the disposable test database; absent URL intentionally skips integration.
func TestLA02UpgradePreservesLegacyAndEnforcesPrimary(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; PostgreSQL integration test unavailable")
	}
	ctx := context.Background()
	pool := throwawayDatabase(t, ctx, url)
	migrations, err := LoadMigrations()
	if err != nil { t.Fatal(err) }
	if err := applyMigrationsThrough(ctx, pool, migrations, 101); err != nil { t.Fatalf("pre-LA02 migration: %v", err) }

	const actor = "NABHOLD"
	if _, err := pool.Exec(ctx, "INSERT INTO legal_entities(legal_entity_id) VALUES($1)", actor); err != nil { t.Fatal(err) }
	oldMissing := domain.NewTenantID()
	oldValid := domain.NewTenantID()
	for _, tenantID := range []string{oldMissing, oldValid} {
		legacyTenant(t, ctx, pool, tenantID, actor)
	}

	// A real PRIMARY for oldValid is independently established, never
	// inferred from the legal actor of oldMissing.
	oldOrg := newPrimaryOrganisation(t, ctx, pool, oldValid)
	insertPrimary(t, ctx, pool, oldValid, oldOrg)

	if err := ApplyMigrations(ctx, pool); err != nil { t.Fatalf("upgrade LA-02: %v", err) }
	var oldMissingEnforced, oldValidEnforced bool
	if err := pool.QueryRow(ctx, "SELECT primary_organisation_enforced FROM tenants WHERE tenant_id=$1", oldMissing).Scan(&oldMissingEnforced); err != nil { t.Fatal(err) }
	if err := pool.QueryRow(ctx, "SELECT primary_organisation_enforced FROM tenants WHERE tenant_id=$1", oldValid).Scan(&oldValidEnforced); err != nil { t.Fatal(err) }
	if oldMissingEnforced || !oldValidEnforced { t.Fatalf("legacy coercion: missing=%v valid=%v", oldMissingEnforced, oldValidEnforced) }
	var reviews int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM registry.tenant_primary_organisation_migration_review WHERE tenant_id=$1 AND resolved_at IS NULL", oldMissing).Scan(&reviews); err != nil || reviews != 1 {
		t.Fatalf("unmapped legacy tenant must receive review, count=%d err=%v", reviews, err)
	}
	// Existing identifiers and legal attribution are unchanged by migration.
	var preservedActor string
	if err := pool.QueryRow(ctx, "SELECT legal_entity_id FROM tenants WHERE tenant_id=$1", oldMissing).Scan(&preservedActor); err != nil || preservedActor != actor {
		t.Fatalf("historic legal actor was rewritten: %q err=%v", preservedActor, err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil { t.Fatalf("migration replay must be a no-op: %v", err) }

	// One atomic registration of a pre-incorporation operating business.
	// Nabhold's legal id is deliberately ABSENT from this tenant; the only
	// legal actor information would arrive through LA-04 mandates later.
	newTenant := domain.NewTenantID()
	newOrg := domain.NewUUIDv7()
	tx, err := pool.Begin(ctx)
	if err != nil { t.Fatal(err) }
	if _, err := tx.Exec(ctx, `INSERT INTO tenants
		(tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region,
		 registration_basis, bootstrap_reason, bootstrap_evidence_reference,primary_organisation_enforced)
		VALUES($1,NULL,'ZuriBeans','row_level_security','af-south-1','BOOTSTRAP',
		 'Migration integration verification only','test-la02-internal',true)`, newTenant); err != nil { tx.Rollback(ctx); t.Fatal(err) }
	insertCanonicalOrgTx(t, ctx, tx, newTenant, newOrg)
	if _, err := tx.Exec(ctx, `INSERT INTO registry.tenant_organisation_mapping
		(tenant_id, organisation_id, mapping_role, status, effective_from, provenance)
		VALUES($1,$2::uuid,'PRIMARY_ORGANISATION','ACTIVE',now(),'test-la02')`, newTenant, newOrg); err != nil { tx.Rollback(ctx); t.Fatal(err) }
	if err := tx.Commit(ctx); err != nil { t.Fatalf("v2-ready nullable legal actor tenant failed: %v", err) }

	store := &Store{pool: pool}
	got, err := store.GetTenant(ctx, newTenant)
	if err != nil { t.Fatal(err) }
	if got.PrimaryOrganisationID != newOrg || got.LegalEntityID != "" || got.TenantID != newTenant {
		t.Fatalf("nullable legal projection / primary Organisation drift: %+v", got)
	}

	// A new tenant must not commit without a real PRIMARY (even when it
	// provides a valid real legal entity).
	badTenant := domain.NewTenantID()
	badTx, err := pool.Begin(ctx)
	if err != nil { t.Fatal(err) }
	if _, err := badTx.Exec(ctx, `INSERT INTO tenants
		(tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region,
		 registration_basis, bootstrap_reason, bootstrap_evidence_reference,primary_organisation_enforced)
		VALUES($1,NULL,'Unmapped Business','row_level_security','af-south-1','BOOTSTRAP',
		 'Unmapped tenant must not be accepted','test-la02-negative',true)`, badTenant); err != nil { t.Fatal(err) }
	if err := badTx.Commit(ctx); err == nil {
		t.Fatal("missing PRIMARY mapping was accepted")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM tenants WHERE tenant_id=$1", badTenant).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed registration did not rollback, count=%d err=%v", count, err)
	}

	// Once enforced, a PRIMARY may not silently be ended or cleared. The
	// transaction must rollback, preserving historical mapping and tenant.
	endTx, err := pool.Begin(ctx)
	if err != nil { t.Fatal(err) }
	if _, err := endTx.Exec(ctx, `UPDATE registry.tenant_organisation_mapping
		SET status='ENDED' WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION'`, newTenant); err != nil { endTx.Rollback(ctx); t.Fatal(err) }
	if err := endTx.Commit(ctx); err == nil { t.Fatal("removing PRIMARY committed") }

	if _, err := pool.Exec(ctx, "UPDATE tenants SET primary_organisation_enforced=false WHERE tenant_id=$1", newTenant); err == nil {
		t.Fatal("new tenant bypassed primary integrity")
	}
	// New tenant cannot falsely declare a DEFAULT legal person without an
	// explicit mapping. Existing historical DEFAULT remains unchanged.
	if _, err := pool.Exec(ctx, "UPDATE tenants SET legal_entity_id=$2 WHERE tenant_id=$1", newTenant, actor); err == nil {
		t.Fatal("new tenant acquired a fictitious DEFAULT projection")
	}

	// Legacy/bootstrap writers with a real actor can still stage a row for
	// migration; it is NOT considered a certified, Organisation-first tenant.
	bootstrapID := domain.NewTenantID()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants
		(tenant_id,legal_entity_id,display_name,isolation_strategy,residency_region,
		 registration_basis,bootstrap_reason,bootstrap_evidence_reference)
		VALUES($1,$2,'Old-style bootstrap','row_level_security','af-south-1',
		 'BOOTSTRAP','Privileged historical bootstrap compatibility','review-needed')`,
		bootstrapID, actor); err != nil {
		t.Fatalf("historical bootstrap compatibility lost: %v", err)
	}
	var staged bool
	if err := pool.QueryRow(ctx, "SELECT primary_organisation_enforced FROM tenants WHERE tenant_id=$1", bootstrapID).Scan(&staged); err != nil || staged {
		t.Fatalf("legacy bootstrap cannot be silently certified: %v %v", staged, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM registry.tenant_primary_organisation_migration_review WHERE tenant_id=$1 AND resolved_at IS NULL", bootstrapID).Scan(&reviews); err != nil || reviews != 1 {
		t.Fatalf("bootstrap must enter the reconciliation queue: %d %v", reviews, err)
	}

	// The old valid tenant's PRIMARY is preserved. Sharing a legal actor
	// must never collapse two tenant identities.
	oldTenant, err := store.GetTenant(ctx, oldValid)
	if err != nil { t.Fatal(err) }
	if oldTenant.PrimaryOrganisationID != oldOrg || oldTenant.LegalEntityID != actor {
		t.Fatalf("valid legacy primary/actor changed: %+v", oldTenant)
	}
}

func legacyTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, legalActor string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants
		(tenant_id,legal_entity_id,display_name,isolation_strategy,residency_region,
		 registration_basis,bootstrap_reason,bootstrap_evidence_reference)
		VALUES($1,$2,'Legacy Business','row_level_security','af-south-1',
		'BOOTSTRAP','Preexisting test tenant with legacy mapping','migration-test')`,
		tenantID, legalActor); err != nil { t.Fatal(err) }
	if _, err := pool.Exec(ctx, `INSERT INTO registry.tenant_legal_entity_mapping
		(tenant_id,legal_entity_id,mapping_role,status,effective_from,provenance)
		VALUES($1,$2,'DEFAULT','ACTIVE',now(),'test-legacy')`, tenantID, legalActor); err != nil { t.Fatal(err) }
}

func newPrimaryOrganisation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) string {
	t.Helper()
	orgID := domain.NewUUIDv7()
	tx, err := pool.Begin(ctx)
	if err != nil { t.Fatal(err) }
	insertCanonicalOrgTx(t, ctx, tx, tenantID, orgID)
	if err := tx.Commit(ctx); err != nil { t.Fatal(err) }
	return orgID
}

func insertCanonicalOrgTx(t *testing.T, ctx context.Context, tx pgx.Tx, tenantID, orgID string) {
	t.Helper()
	if _, err := tx.Exec(ctx, `INSERT INTO registry.canonical_entity
		(canonical_entity_id,tenant_id,entity_type,status)
		VALUES($1::uuid,$2,'ORGANISATION','active')`, orgID, tenantID); err != nil {
		t.Fatalf("insert canonical Organisation: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO registry.organisation_profile
		(canonical_entity_id, display_name, verification_state, source_authority,
		 status, effective_from)
		VALUES($1::uuid,'Operating Business','UNVERIFIED','test-la02',
		'ACTIVE',now())`, orgID); err != nil { t.Fatalf("insert organisation profile: %v", err) }
}

func insertPrimary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, orgID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO registry.tenant_organisation_mapping
		(tenant_id,organisation_id,mapping_role,status,effective_from,provenance)
		VALUES($1,$2::uuid,'PRIMARY_ORGANISATION','ACTIVE',now(),'test-la02')`, tenantID, orgID); err != nil { t.Fatal(err) }
}

// TestLA02MigrationDefinition prevents accidental removal of the nullable
// projection/deferrable integrity semantics when no PostgreSQL URL is set.
func TestLA02MigrationDefinition(t *testing.T) {
	migrations, err := LoadMigrations()
	if err != nil { t.Fatal(err) }
	var sql string
	for _, m := range migrations {
		if m.Version == 102 { sql = m.SQL }
	}
	if sql == "" { t.Fatal("LA-02 migration not registered") }
	for _, invariant := range []string{
		"ALTER COLUMN legal_entity_id DROP NOT NULL",
		"primary_organisation_enforced",
		"DEFERRABLE INITIALLY DEFERRED",
		"tenant_primary_organisation_migration_review",
		"registry.assert_tenant_primary_organisation",
	} {
		if !strings.Contains(sql, invariant) { t.Fatalf("LA-02 migration lacks %s", invariant) }
	}
}
