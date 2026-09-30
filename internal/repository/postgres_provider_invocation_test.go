package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestRegisterEngineRecordsProviderInvocation: a provider's logical
// invocation reference is recorded by registration and read back for
// resolution; a registration without one clears it, and the store refuses
// a deployment hostname (migration 000078).
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestRegisterEngineRecordsProviderInvocation(t *testing.T) {
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

	const engine, providerKey, capability = "baobab-invocationtest", "baobab-invocationtest.engine", "trade.invocationtest.execute"
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key = $1`, providerKey)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capability)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)

	registration := func(invocation *ProviderInvocation) EngineRegistrationRecord {
		return EngineRegistrationRecord{
			Repository: engine,
			Capabilities: []capabilitydomain.Capability{{Key: capability, Name: "Invocation test", DomainKey: "trade",
				Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}},
			Provider: EngineRegistrationProvider{ProviderKey: providerKey, Name: "Invocation test", ProviderType: "BAOBAB_ENGINE",
				EngineKey: "engine", Lifecycle: "ACTIVE", Ownership: engine, Invocation: invocation},
			Support: []EngineRegistrationSupport{{CapabilityKey: capability, ContractVersions: []int{1}}},
		}
	}
	providerID := func() string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `SELECT provider_id::text FROM capability.capability_provider WHERE provider_key = $1`, providerKey).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// Registration references catalogue capabilities; it never creates one.
	if _, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{
		Capability: capabilitydomain.Capability{Key: capability, Name: "Invocation test", DomainKey: "trade",
			Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported},
		ContractVersions: []int{1}, DataClassification: "INTERNAL", Owner: engine, Source: "fixtures/invocation-test",
		Digest: "sha256:" + strings.Repeat("1", 64),
	}}); err != nil {
		t.Fatal(err)
	}

	want := ProviderInvocation{ServiceReference: "service://baobab-invocationtest/orders", Protocol: "http"}
	if err := repo.RegisterEngine(ctx, registration(&want)); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := repo.ProviderInvocationByID(ctx, providerID()); err != nil || !ok || got != want {
		t.Fatalf("registered invocation = %+v %v %v, want %+v", got, ok, err, want)
	}
	// The registration is the provider's whole description: one without an
	// invocation clears it.
	if err := repo.RegisterEngine(ctx, registration(nil)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.ProviderInvocationByID(ctx, providerID()); err != nil || ok {
		t.Fatalf("an invocation survived a registration without one: %v %v", ok, err)
	}
	// Only a logical reference is stored, never a host.
	if err := repo.RegisterEngine(ctx, registration(&ProviderInvocation{ServiceReference: "https://orders.internal:8443", Protocol: "http"})); err == nil {
		t.Fatal("a deployment hostname was stored as an invocation reference")
	}
	if _, err := admin.Exec(ctx, `UPDATE capability.capability_provider SET service_reference = 'service://x/y' WHERE provider_key = $1`, providerKey); err == nil {
		t.Fatal("a service reference was stored without a protocol")
	}
}
