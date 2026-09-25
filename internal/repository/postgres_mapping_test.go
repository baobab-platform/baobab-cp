package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresCanonicalMappingExclusionConstraintFires is a regression test
// for docs/reconciliation/shared-control-plane-audit.md §2.2/§10.4: until
// migration 000022, mapping.canonical_mapping had no temporal-validity
// columns at all, so two active mappings of the same type for the same
// source entity could coexist with no way to express (or reject) an
// overlapping validity window - the "ambiguous authoritative mapping"
// outcome the Canonical Mapping Model requires resolution to fail on
// (§67.5). It also proves CreateMapping/GetMapping/ListMappings actually
// round-trip effective_from/effective_to instead of discarding them.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresCanonicalMappingExclusionConstraintFires(t *testing.T) {
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

	const sourceEntity = "20000000-0000-0000-0000-000000000001"
	const targetA = "20000000-0000-0000-0000-000000000002"
	const targetB = "20000000-0000-0000-0000-000000000003"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM mapping.canonical_mapping WHERE source_entity_id=$1`, sourceEntity)
		admin.Exec(ctx, `DELETE FROM registry.canonical_entity WHERE canonical_entity_id = ANY($1)`, []string{sourceEntity, targetA, targetB})
	}
	cleanup()
	t.Cleanup(cleanup)

	const tenantID = "tn_gate1test"
	for _, id := range []string{sourceEntity, targetA, targetB} {
		if _, err := admin.Exec(ctx, `INSERT INTO registry.canonical_entity(canonical_entity_id, tenant_id, entity_type) VALUES ($1,$2,'PRODUCT')`, id, tenantID); err != nil {
			t.Fatalf("fixture setup for %s: %v", id, err)
		}
	}

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	now := time.Now().UTC().Truncate(time.Second)
	first := domain.Mapping{
		ID:                      "20000000-0000-0000-0000-0000000000a1",
		MappingType:             "IDENTITY",
		TenantID:                tenantID,
		CanonicalEntityID:       sourceEntity,
		TargetCanonicalEntityID: targetA,
		ScopeID:                 sourceEntity,
		Direction:               "SOURCE_TO_TARGET",
		Cardinality:             "ONE_TO_ONE",
		Authority:               "baobab",
		Confidence:              "CONFIRMED",
		Status:                  "active",
		EffectiveFrom:           now.Add(-time.Hour).Format(time.RFC3339),
	}
	if err := repo.CreateMapping(ctx, first); err != nil {
		t.Fatalf("create first mapping: %v", err)
	}

	// A second active IDENTITY mapping for the same source entity, with an
	// overlapping (open-ended) validity window, must be rejected - having
	// both active would make "which target is authoritative" ambiguous.
	second := first
	second.ID = "20000000-0000-0000-0000-0000000000a2"
	second.TargetCanonicalEntityID = targetB
	second.EffectiveFrom = now.Format(time.RFC3339)
	err = repo.CreateMapping(ctx, second)
	if err == nil {
		t.Fatal("expected the exclusion constraint to reject a second overlapping active IDENTITY mapping for the same source entity")
	}
	if !errors.Is(err, ErrMappingOverlap) {
		t.Fatalf("expected ErrMappingOverlap, got: %v", err)
	}

	// Retiring the first mapping (bounding its validity window to end before
	// the second one starts) must let a new one take over: not "any overlap
	// forever", but "no overlap in time".
	if _, err := admin.Exec(ctx, `UPDATE mapping.canonical_mapping SET effective_to=$2 WHERE canonical_mapping_id=$1::uuid`, first.ID, now.Add(-30*time.Minute)); err != nil {
		t.Fatalf("retire first mapping: %v", err)
	}
	if err := repo.CreateMapping(ctx, second); err != nil {
		t.Fatalf("expected a non-overlapping successor mapping to be accepted: %v", err)
	}

	stored, err := repo.GetMapping(ctx, second.ID)
	if err != nil {
		t.Fatalf("get mapping: %v", err)
	}
	if stored.EffectiveFrom == "" {
		t.Fatal("expected effective_from to round-trip through persistence, got empty string")
	}
	if stored.TargetCanonicalEntityID != targetB {
		t.Fatalf("expected target %s, got %s", targetB, stored.TargetCanonicalEntityID)
	}
	if stored.TenantID != tenantID {
		t.Fatalf("expected tenant_id %s (derived from the source canonical entity), got %q", tenantID, stored.TenantID)
	}
	if stored.CreatedAt == "" {
		t.Fatal("expected created_at to round-trip through persistence, got empty string")
	}

	mappings, err := repo.ListMappings(ctx, sourceEntity)
	if err != nil {
		t.Fatalf("list mappings: %v", err)
	}
	if len(mappings) != 2 {
		t.Fatalf("expected 2 mappings (one retired, one active) for the source entity, got %d", len(mappings))
	}
}
