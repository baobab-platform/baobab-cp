package repository

import (
	"context"
	"os"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresCapabilityBindingExclusionConstraintFires is a regression test for
// the finding recorded in docs/adr/ADR-0005-bcp-db-001-conformance-gap.md: writing
// a binding's status lower-cased silently defeated the
// capability_binding_primary_excl exclusion constraint (defined against
// status = 'ACTIVE' AND binding_mode = 'PRIMARY'), letting two PRIMARY bindings
// for the same capability and scope coexist with overlapping validity - exactly
// the "ambiguous resolution" outcome BCP-DB-001 section 13 says the database must
// make impossible.
//
// It requires a real PostgreSQL 17 instance (btree_gist + exclusion constraints
// are not something a mock or SQLite substitute can validate - see BCP-DB-001
// section 49, "do not pretend SQLite proves PostgreSQL semantics"). Set
// TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresCapabilityBindingExclusionConstraintFires(t *testing.T) {
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

	// Isolate fixtures so the test is repeatable against a persistent database.
	const capabilityID = "10000000-0000-0000-0000-000000000001"
	const engineID = "10000000-0000-0000-0000-000000000002"
	const instanceA = "10000000-0000-0000-0000-000000000003"
	const instanceB = "10000000-0000-0000-0000-000000000004"
	const scopeID = "10000000-0000-0000-0000-000000000005"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE capability_id=$1`, capabilityID)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id=$1`, scopeID)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id=$1`, engineID)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id=$1`, engineID)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id=$1`, capabilityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	fixtures := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO capability.capability(capability_id, code, name) VALUES ($1,'test.exclusion.capability','Test Capability')`, []any{capabilityID}},
		{`INSERT INTO topology.engine(engine_id, code, name) VALUES ($1,'test-exclusion-engine','Test Engine')`, []any{engineID}},
		{`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status) VALUES ($1,$2,'af-south-1','production','ACTIVE')`, []any{instanceA, engineID}},
		{`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status) VALUES ($1,$2,'af-south-1','production','ACTIVE')`, []any{instanceB, engineID}},
		{`INSERT INTO capability.capability_scope(scope_id, tenant_id) VALUES ($1,'tenant-exclusion-test')`, []any{scopeID}},
	}
	for _, f := range fixtures {
		if _, err := admin.Exec(ctx, f.sql, f.args...); err != nil {
			t.Fatalf("fixture setup: %v", err)
		}
	}

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	first := resolver.CapabilityBinding{
		CapabilityKey:    "test.exclusion.capability",
		EngineID:         engineID,
		EngineInstanceID: instanceA,
		ScopeID:          scopeID,
		BindingMode:      "PRIMARY",
		Priority:         100,
		Status:           "ACTIVE",
		ContractVersion:  "v1",
	}
	if err := repo.CreateBinding(ctx, first); err != nil {
		t.Fatalf("create first primary binding: %v", err)
	}

	second := first
	second.EngineInstanceID = instanceB
	err = repo.CreateBinding(ctx, second)
	if err == nil {
		t.Fatal("expected the exclusion constraint to reject a second overlapping PRIMARY binding for the same capability and scope, got no error")
	}
	t.Logf("exclusion constraint correctly rejected the conflicting binding: %v", err)
}
