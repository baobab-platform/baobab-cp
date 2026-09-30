package repository

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestSyncCapabilityCatalogue proves the capability registry converges on
// the canonical catalogue (ADR-SHARED-017 SS29, G-CP-2): unknown
// capabilities are created with provenance, an unchanged definition is left
// alone, a changed one updates the projection, and a change to a
// capability's identity is refused with nothing written.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestSyncCapabilityCatalogue(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)

	const (
		alpha  = "test.cataloguesync.alpha"
		beta   = "test.cataloguesync.beta"
		engine = "82000000-0000-0000-0000-000000000001"
	)
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE engine_id = $1`, engine)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = $1`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code IN ($1, $2)`, alpha, beta)
	}
	cleanup()
	t.Cleanup(cleanup)

	canonical := func(key, name, digest string, versions ...int) CatalogueCapability {
		return CatalogueCapability{
			Capability:       capabilitydomain.Capability{Key: key, Name: name, DomainKey: "test", Lifecycle: "ACTIVE", Maturity: "EXPERIMENTAL"},
			ContractVersions: versions, DataClassification: "INTERNAL", Owner: "baobab-test", Source: "fixtures/catalogue-sync-definitions",
			Digest: "sha256:" + digest,
		}
	}
	digest := func(c byte) string {
		b := make([]byte, 64)
		for i := range b {
			b[i] = c
		}
		return string(b)
	}
	type row struct {
		name, status, owner, source, digest string
		versions                            []int32
	}
	read := func(key string) (row, bool) {
		t.Helper()
		var r row
		err := admin.QueryRow(ctx, `SELECT name, status, COALESCE(canonical_owner, ''), COALESCE(canonical_source, ''),
			COALESCE(canonical_digest, ''), COALESCE(contract_versions, '{}') FROM capability.capability WHERE code = $1`, key).
			Scan(&r.name, &r.status, &r.owner, &r.source, &r.digest, &r.versions)
		if err != nil {
			return r, false
		}
		return r, true
	}

	// An unknown capability is created, with its provenance.
	report, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{canonical(alpha, "Alpha", digest('a'), 1)})
	if err != nil || !slices.Equal(report.Created, []string{alpha}) {
		t.Fatalf("create: %+v %v", report, err)
	}
	if r, ok := read(alpha); !ok || r.name != "Alpha" || r.owner != "baobab-test" || r.source != "fixtures/catalogue-sync-definitions" ||
		r.digest != "sha256:"+digest('a') || !slices.Equal(r.versions, []int32{1}) {
		t.Fatalf("created row = %+v", r)
	}

	// Re-syncing an unchanged definition writes nothing.
	report, err = repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{canonical(alpha, "Alpha", digest('a'), 1)})
	if err != nil || !slices.Equal(report.Unchanged, []string{alpha}) || len(report.Updated) != 0 {
		t.Fatalf("unchanged: %+v %v", report, err)
	}

	// A changed definition updates the projection.
	changed := canonical(alpha, "Alpha renamed", digest('b'), 1, 2)
	changed.Capability.Lifecycle = "DEPRECATED"
	report, err = repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{changed})
	if err != nil || !slices.Equal(report.Updated, []string{alpha}) {
		t.Fatalf("update: %+v %v", report, err)
	}
	if r, _ := read(alpha); r.name != "Alpha renamed" || r.status != "DEPRECATED" || !slices.Equal(r.versions, []int32{1, 2}) {
		t.Fatalf("updated row = %+v", r)
	}

	// Provider support at major 2 means the catalogue cannot drop it.
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine(engine_id, code, name) VALUES ($1, 'cataloguesync-engine', 'Engine')`, engine); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `WITH p AS (INSERT INTO capability.capability_provider(provider_key, name, provider_type, engine_id, status)
			VALUES ('cataloguesync-engine.provider', 'Provider', 'BAOBAB_ENGINE', $1, 'ACTIVE') RETURNING provider_id)
		INSERT INTO capability.provider_capability_support(provider_id, capability_id, contract_versions)
		SELECT p.provider_id, c.capability_id, '{2}' FROM p, capability.capability c WHERE c.code = $2`, engine, alpha); err != nil {
		t.Fatal(err)
	}
	// A conflict refuses the whole sync: beta, listed before it, is not
	// created either.
	_, err = repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{canonical(beta, "Beta", digest('c'), 1),
		canonical(alpha, "Alpha", digest('d'), 1)})
	if !errors.Is(err, ErrCatalogueConflict) {
		t.Fatalf("dropping a supported contract major: %v", err)
	}
	if _, ok := read(beta); ok {
		t.Fatal("a refused sync created a capability")
	}
	if r, _ := read(alpha); r.digest != "sha256:"+digest('b') {
		t.Fatalf("a refused sync changed %s: %+v", alpha, r)
	}
	// An unchanged definition is never refused, even beside support the
	// definition does not declare.
	if _, err := admin.Exec(ctx, `UPDATE capability.provider_capability_support SET contract_versions = '{3}'
		WHERE capability_id = (SELECT capability_id FROM capability.capability WHERE code = $1)`, alpha); err != nil {
		t.Fatal(err)
	}
	unchangedAlpha := canonical(alpha, "Alpha renamed", digest('b'), 1, 2)
	unchangedAlpha.Capability.Lifecycle = "DEPRECATED"
	if report, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{unchangedAlpha}); err != nil ||
		!slices.Equal(report.Unchanged, []string{alpha}) {
		t.Fatalf("an unchanged definition: %+v %v", report, err)
	}
	if _, err := admin.Exec(ctx, `UPDATE capability.provider_capability_support SET contract_versions = '{2}'
		WHERE capability_id = (SELECT capability_id FROM capability.capability WHERE code = $1)`, alpha); err != nil {
		t.Fatal(err)
	}

	// A capability's domain is part of its identity.
	otherDomain := canonical(alpha, "Alpha", digest('e'), 1, 2)
	otherDomain.Capability.DomainKey = "other"
	if _, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{otherDomain}); !errors.Is(err, ErrCatalogueConflict) {
		t.Fatalf("changing a capability's domain: %v", err)
	}

	// A capability created by provider registration alone is outside the
	// catalogue until a sync projects it.
	if _, err := admin.Exec(ctx, `INSERT INTO capability.capability(code, name, domain_key) VALUES ($1, 'Beta', 'test')`, beta); err != nil {
		t.Fatal(err)
	}
	var outside int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM capability.capability_outside_catalogue WHERE code = $1`, beta).Scan(&outside); err != nil || outside != 1 {
		t.Fatalf("outside catalogue before sync: %d %v", outside, err)
	}
	report, err = repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{canonical(beta, "Beta", digest('f'), 1)})
	if err != nil || !slices.Equal(report.Updated, []string{beta}) {
		t.Fatalf("projecting an existing capability: %+v %v", report, err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM capability.capability_outside_catalogue WHERE code = $1`, beta).Scan(&outside); err != nil || outside != 0 {
		t.Fatalf("outside catalogue after sync: %d %v", outside, err)
	}
}
