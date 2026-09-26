package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestLegacyCanonicalMappingsMigrateWithAppliedSemantics covers migration
// 000060 (ADR-SHARED-013): a legacy canonical-to-canonical mapping moves to
// mapping.mapping with the semantics the resolver applied to it, the runtime
// resolver reads it from there, a row whose tenant, type or target cannot be
// established is reported instead of migrated, running the migration again
// changes nothing, and the legacy table accepts no new rows.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestLegacyCanonicalMappingsMigrateWithAppliedSemantics(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	tenant, otherTenant := "tn_legacy"+suffix, "tn_other"+suffix
	source, target, foreign, orphan := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	migrated, crossTenant, noTenant, unregistered := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	entities := []string{source, target, foreign, orphan}
	legacy := []string{migrated, crossTenant, noTenant, unregistered}
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM mapping.mapping WHERE canonical_entity_id = ANY($1::uuid[])`, entities)
		admin.Exec(ctx, `DELETE FROM mapping.canonical_mapping WHERE canonical_mapping_id = ANY($1::uuid[])`, legacy)
		admin.Exec(ctx, `DELETE FROM registry.canonical_entity WHERE canonical_entity_id = ANY($1::uuid[])`, entities)
	})

	// The legacy table is frozen, so its rows are planted the way they existed
	// before migration 000060: with triggers off for this transaction only.
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatalf("disable triggers: %v", err)
	}
	for id, owner := range map[string]*string{source: &tenant, target: &tenant, foreign: &otherTenant, orphan: nil} {
		if _, err := tx.Exec(ctx, `INSERT INTO registry.canonical_entity (canonical_entity_id, tenant_id, entity_type) VALUES ($1, $2, 'PRODUCT')`, id, owner); err != nil {
			t.Fatalf("plant entity: %v", err)
		}
	}
	from := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	for _, row := range []struct{ id, source, target, mappingType string }{
		{migrated, source, target, "identity"},
		{crossTenant, source, foreign, "ALIAS"},
		{noTenant, orphan, target, "ALIAS"},
		{unregistered, source, target, "vendor-sku"},
	} {
		if _, err := tx.Exec(ctx, `INSERT INTO mapping.canonical_mapping (canonical_mapping_id, source_entity_id, target_entity_id, mapping_type, status, effective_from)
			VALUES ($1, $2, $3, $4, 'active', $5)`, row.id, row.source, row.target, row.mappingType, from); err != nil {
			t.Fatalf("plant legacy mapping: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit legacy rows: %v", err)
	}

	var copied int64
	if err := admin.QueryRow(ctx, `SELECT mapping.migrate_legacy_canonical_mappings()`).Scan(&copied); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if copied != 1 {
		t.Fatalf("expected exactly the one establishable row to migrate, got %d", copied)
	}
	if err := admin.QueryRow(ctx, `SELECT mapping.migrate_legacy_canonical_mappings()`).Scan(&copied); err != nil || copied != 0 {
		t.Fatalf("expected a second run to change nothing, got %d, %v", copied, err)
	}

	report := map[string]string{}
	rows, err := admin.Query(ctx, `SELECT legacy_row_id::text, disposition FROM mapping.legacy_canonical_mapping_report WHERE legacy_row_id = ANY($1::uuid[])`, legacy)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for rows.Next() {
		var id, disposition string
		if err := rows.Scan(&id, &disposition); err != nil {
			t.Fatalf("scan report: %v", err)
		}
		report[id] = disposition
	}
	rows.Close()
	for id, want := range map[string]string{migrated: "MIGRATED", crossTenant: "CROSS_TENANT", noTenant: "TENANT_UNKNOWN", unregistered: "MAPPING_TYPE_UNREGISTERED"} {
		if report[id] != want {
			t.Fatalf("legacy row %s: expected %s, got %q", id, want, report[id])
		}
	}

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()
	mappings, err := repo.ListMappings(ctx, source)
	if err != nil {
		t.Fatalf("list mappings: %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("expected the migrated mapping only, got %+v", mappings)
	}
	m := mappings[0]
	wantID := "map_" + strings.ReplaceAll(migrated, "-", "")
	if m.ID != wantID || m.TenantID != tenant || m.MappingType != "IDENTITY" || m.TargetCanonicalEntityID != target ||
		m.Direction != "SOURCE_TO_TARGET" || m.Cardinality != "ONE_TO_ONE" || m.Authority != "control-plane" ||
		m.Confidence != "CONFIRMED" || m.ResolutionPriority != 0 || m.ScopeID != "" || m.Status != "ACTIVE" ||
		m.Revision != 1 || m.CreatedBy != "migration:000060" {
		t.Fatalf("migrated mapping does not carry the applied semantics: %+v", m)
	}
	if m.Metadata["source"] != "legacy-canonical-mapping" || m.Metadata["legacy_canonical_mapping_id"] != migrated || m.Metadata["legacy_mapping_type"] != "identity" {
		t.Fatalf("migrated mapping metadata: %+v", m.Metadata)
	}
	if got, err := time.Parse(time.RFC3339, m.EffectiveFrom); err != nil || !got.Equal(from) {
		t.Fatalf("effective_from %q, want %s", m.EffectiveFrom, from)
	}

	resolved, err := resolver.MappingResolverImpl{}.Resolve(ctx, resolver.MappingResolutionQuery{
		CanonicalEntityID: source,
		Context:           resolver.Context{TenantID: tenant},
		Candidates:        mappings,
	})
	if err != nil || resolved.Mapping.ID != wantID {
		t.Fatalf("expected the runtime resolver to resolve the migrated mapping, got %+v, %v", resolved, err)
	}

	_, err = admin.Exec(ctx, `INSERT INTO mapping.canonical_mapping (source_entity_id, target_entity_id, mapping_type) VALUES ($1, $2, 'ALIAS')`, target, source)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("expected the legacy table to refuse new rows, got %v", err)
	}
}
