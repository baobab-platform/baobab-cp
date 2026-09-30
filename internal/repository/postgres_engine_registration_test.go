package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestRegisterEngineRequiresCatalogue proves provider registration no
// longer originates capabilities (ADR-SHARED-017 SS28, SS59, G-CP-3): a
// capability outside the catalogue projection, a different domain or an
// uncatalogued contract major is refused with nothing written, and a
// registration inside the catalogue succeeds without rewriting it.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestRegisterEngineRequiresCatalogue(t *testing.T) {
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
		engine      = "baobab-registrationtest"
		providerKey = "baobab-registrationtest.engine"
		catalogued  = "test.registrationtest.catalogued"
		unknown     = "test.registrationtest.unknown"
		legacy      = "test.registrationtest.legacy"
	)
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key = $1`, providerKey)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code IN ($1, $2, $3)`, catalogued, unknown, legacy)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)

	definition := func(key string) capabilitydomain.Capability {
		return capabilitydomain.Capability{Key: key, Name: "Registration test", DomainKey: "test",
			Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}
	}
	if _, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{Capability: definition(catalogued), ContractVersions: []int{1},
		DataClassification: "INTERNAL", Owner: engine, Source: "fixtures/registration-test", Digest: "sha256:" + strings.Repeat("2", 64)}}); err != nil {
		t.Fatal(err)
	}
	// A capability created outside any sync, as registration used to.
	if _, err := admin.Exec(ctx, `INSERT INTO capability.capability (code, name, domain_key) VALUES ($1, 'Legacy', 'test')`, legacy); err != nil {
		t.Fatal(err)
	}
	registration := func(capability capabilitydomain.Capability, versions ...int) EngineRegistrationRecord {
		return EngineRegistrationRecord{
			Repository:   engine,
			Capabilities: []capabilitydomain.Capability{capability},
			Provider: EngineRegistrationProvider{ProviderKey: providerKey, Name: "Registration test", ProviderType: "BAOBAB_ENGINE",
				EngineKey: "engine", Lifecycle: "ACTIVE", Ownership: engine},
			Support: []EngineRegistrationSupport{{CapabilityKey: capability.Key, ContractVersions: versions}},
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := admin.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	otherDomain := definition(catalogued)
	otherDomain.DomainKey = "other"
	for name, reg := range map[string]EngineRegistrationRecord{
		"an unknown capability":          registration(definition(unknown), 1),
		"a capability no sync projected": registration(definition(legacy), 1),
		"a different domain":             registration(otherDomain, 1),
		"an uncatalogued contract major": registration(definition(catalogued), 1, 2),
	} {
		if err := repo.RegisterEngine(ctx, reg); !errors.Is(err, ErrRegistrationOutsideCatalogue) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n := count(`SELECT count(*) FROM capability.capability WHERE code = $1`, unknown); n != 0 {
		t.Fatal("registration created a capability")
	}
	if n := count(`SELECT count(*) FROM capability.capability_provider WHERE provider_key = $1`, providerKey); n != 0 {
		t.Fatal("a refused registration recorded its provider")
	}

	// Inside the catalogue, registration records the provider and its
	// support, and leaves the capability as the sync projected it.
	renamed := definition(catalogued)
	renamed.Name = "Renamed by a registration"
	if err := repo.RegisterEngine(ctx, registration(renamed, 1)); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM capability.provider_capability_support pcs
		JOIN capability.capability_provider p USING (provider_id) JOIN capability.capability c USING (capability_id)
		WHERE p.provider_key = $1 AND c.code = $2 AND pcs.contract_versions = '{1}'`, providerKey, catalogued); n != 1 {
		t.Fatalf("support rows = %d", n)
	}
	if n := count(`SELECT count(*) FROM capability.capability WHERE code = $1 AND name = 'Registration test'`, catalogued); n != 1 {
		t.Fatal("registration rewrote a catalogue capability")
	}
}
