package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestCreateBindingResolvesItsProvider proves a binding names its engine's
// provider (migration 000077): CreateBinding takes the engine's only
// ACTIVE provider supporting the capability, refuses a provider of another
// engine, refuses an ACTIVE binding whose provider is ambiguous unless it
// names one, SaveBinding cannot make a provider-less binding ACTIVE, and
// an ACTIVE binding's contract major must be one its provider supports.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestCreateBindingResolvesItsProvider(t *testing.T) {
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
		capabilityID = "81000000-0000-0000-0000-000000000001"
		capability   = "test.bindingprovider.capability"
		engine       = "81000000-0000-0000-0000-000000000002"
		otherEngine  = "81000000-0000-0000-0000-000000000003"
		instance     = "81000000-0000-0000-0000-000000000004"
	)
	scopes := []string{"81000000-0000-0000-0000-000000000011", "81000000-0000-0000-0000-000000000012",
		"81000000-0000-0000-0000-000000000013", "81000000-0000-0000-0000-000000000014"}
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE capability_id = $1`, capabilityID)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id = ANY($1::uuid[])`, scopes)
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE engine_id IN ($1, $2)`, engine, otherEngine)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id = $1`, engine)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id IN ($1, $2)`, engine, otherEngine)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1`, capabilityID)
	}
	cleanup()
	t.Cleanup(cleanup)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO capability.capability(capability_id, code, name) VALUES ($1, $2, 'Binding provider')`, capabilityID, capability)
	exec(`INSERT INTO topology.engine(engine_id, code, name) VALUES ($1, 'bindingprovider-engine', 'Engine'), ($2, 'bindingprovider-other', 'Other')`, engine, otherEngine)
	exec(`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status) VALUES ($1, $2, 'af-south-1', 'production', 'ACTIVE')`, instance, engine)
	for i, scope := range scopes {
		exec(`INSERT INTO capability.capability_scope(scope_id, tenant_id) VALUES ($1, $2)`, scope, "tn_bindingprovider"+string(rune('a'+i)))
	}
	provider := func(key, engineID string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `WITH p AS (INSERT INTO capability.capability_provider(provider_key, name, provider_type, engine_id, status)
			VALUES ($1, $1, 'BAOBAB_ENGINE', $2, 'ACTIVE') RETURNING provider_id),
			s AS (INSERT INTO capability.provider_capability_support(provider_id, capability_id, contract_versions) SELECT provider_id, $3, '{1}' FROM p)
			SELECT provider_id::text FROM p`, key, engineID, capabilityID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := provider("bindingprovider-engine.first", engine)
	foreign := provider("bindingprovider-other.foreign", otherEngine)

	binding := func(scope, status, providerID string) resolver.CapabilityBinding {
		return resolver.CapabilityBinding{CapabilityKey: capability, EngineID: engine, EngineInstanceID: instance, ScopeID: scope,
			BindingMode: "PRIMARY", Priority: 1, Status: status, ContractVersion: "1", ProviderID: providerID}
	}
	stored := func(scope string) (id, providerID string, version int64) {
		t.Helper()
		if err := admin.QueryRow(ctx, `SELECT id::text, COALESCE(provider_id::text, ''), version FROM capability.capability_binding WHERE capability_id = $1 AND scope_id = $2`,
			capabilityID, scope).Scan(&id, &providerID, &version); err != nil {
			t.Fatal(err)
		}
		return
	}

	// The engine's only provider is taken when the binding names none.
	if err := repo.CreateBinding(ctx, binding(scopes[0], "ACTIVE", "")); err != nil {
		t.Fatal(err)
	}
	if _, got, _ := stored(scopes[0]); got != first {
		t.Fatalf("binding provider = %q, want the engine's only provider %s", got, first)
	}
	// Another engine's provider is never the binding's.
	if err := repo.CreateBinding(ctx, binding(scopes[1], "ACTIVE", foreign)); !errors.Is(err, ErrBindingProviderUnresolved) {
		t.Fatalf("a provider of another engine: %v", err)
	}

	// With two providers an ACTIVE binding must name one.
	second := provider("bindingprovider-engine.second", engine)
	if err := repo.CreateBinding(ctx, binding(scopes[1], "ACTIVE", "")); !errors.Is(err, ErrBindingProviderUnresolved) {
		t.Fatalf("an ambiguous provider: %v", err)
	}
	if err := repo.CreateBinding(ctx, binding(scopes[1], "ACTIVE", second)); err != nil {
		t.Fatal(err)
	}
	if _, got, _ := stored(scopes[1]); got != second {
		t.Fatalf("binding provider = %q, want the named %s", got, second)
	}

	// A DRAFT binding may wait for its provider, but cannot become ACTIVE
	// without one.
	if err := repo.CreateBinding(ctx, binding(scopes[2], "DRAFT", "")); err != nil {
		t.Fatal(err)
	}
	id, got, version := stored(scopes[2])
	if got != "" {
		t.Fatalf("an ambiguous DRAFT binding took provider %s", got)
	}
	activated := binding(scopes[2], "ACTIVE", "")
	activated.ID = id
	if err := repo.SaveBinding(ctx, activated, version); !errors.Is(err, ErrBindingProviderUnresolved) {
		t.Fatalf("activating a binding without a provider: %v", err)
	}

	// An ACTIVE binding's contract major must be one its provider supports
	// (ADR-SHARED-017 SS36, SS60); the providers here support only major 1.
	unsupported := binding(scopes[3], "ACTIVE", first)
	unsupported.ContractVersion = "v2"
	if err := repo.CreateBinding(ctx, unsupported); !errors.Is(err, ErrBindingContractUnsupported) {
		t.Fatalf("an ACTIVE binding at an unsupported contract major: %v", err)
	}
	draft := binding(scopes[3], "DRAFT", first)
	draft.ContractVersion = "2"
	if err := repo.CreateBinding(ctx, draft); err != nil {
		t.Fatalf("a DRAFT binding may name an unsupported major until activated: %v", err)
	}
	id, _, version = stored(scopes[3])
	draft.ID, draft.Status = id, "ACTIVE"
	if err := repo.SaveBinding(ctx, draft, version); !errors.Is(err, ErrBindingContractUnsupported) {
		t.Fatalf("activating a binding at an unsupported contract major: %v", err)
	}
	draft.ContractVersion = "1.0.0"
	if err := repo.SaveBinding(ctx, draft, version); err != nil {
		t.Fatalf("activating a binding at a supported contract major: %v", err)
	}
}
