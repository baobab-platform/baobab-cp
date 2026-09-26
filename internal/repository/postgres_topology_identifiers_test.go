package repository

import (
	"context"
	"os"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresTopologyIdentifiers (ADR-SHARED-012, migration 000058): an
// engine instance's canonical key is generated from its UUID exactly as
// domain.EngineInstanceKey derives it; a new engine code must be an engine
// id; an existing non-conforming code is reported, not renamed; an identity
// reference records only a canonical engine instance.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresTopologyIdentifiers(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}

	engine, instance, legacy := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	code := "baobab-topology-test-" + engine[len(engine)-8:]
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = $1::uuid`, instance)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = ANY($1::uuid[])`, []string{engine, legacy})
	})
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine (engine_id, code, name) VALUES ($1::uuid, $2, $2)`, engine, code); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine_instance (engine_instance_id, engine_id, region, environment, status)
		VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE')`, instance, engine); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := admin.QueryRow(ctx, `SELECT engine_instance_key FROM topology.engine_instance WHERE engine_instance_id = $1::uuid`, instance).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != domain.EngineInstanceKey(instance) || !domain.ValidEngineInstanceID(key) {
		t.Fatalf("generated key %q, Go derives %q", key, domain.EngineInstanceKey(instance))
	}

	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine (engine_id, code, name) VALUES ($1::uuid, 'Legacy_Engine', 'x')`, legacy); err == nil {
		t.Fatal("a new engine code outside the engineId grammar must be refused")
	}
	// A row that predates the constraint is reported, not renamed.
	if _, err := admin.Exec(ctx, `ALTER TABLE topology.engine DROP CONSTRAINT engine_code_engine_id_ck`); err != nil {
		t.Fatal(err)
	}
	_, insertErr := admin.Exec(ctx, `INSERT INTO topology.engine (engine_id, code, name) VALUES ($1::uuid, 'Legacy_Engine', 'x')`, legacy)
	if _, err := admin.Exec(ctx, `ALTER TABLE topology.engine ADD CONSTRAINT engine_code_engine_id_ck
		CHECK (length(code) BETWEEN 3 AND 63 AND code ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if insertErr != nil {
		t.Fatal(insertErr)
	}
	var reported int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM topology.engine_code_nonconforming WHERE engine_id = $1::uuid`, legacy).Scan(&reported); err != nil || reported != 1 {
		t.Fatalf("legacy engine code reported %d times: %v", reported, err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM topology.engine_code_nonconforming WHERE engine_id = $1::uuid`, engine).Scan(&reported); err != nil || reported != 0 {
		t.Fatalf("conforming engine code reported: %d %v", reported, err)
	}
}
