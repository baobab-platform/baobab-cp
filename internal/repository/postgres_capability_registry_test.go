package repository

import (
	"context"
	"os"
	"testing"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresCapabilityRegistryRoundTrip proves CreateCapability,
// GetCapability, CreateCapabilityDependency and ListCapabilityDependencies
// round-trip against a real PostgreSQL 17 instance (migration
// 000030_capability_lifecycle_maturity_and_dependency.sql), and that
// CreateCapabilityDependency's whole-graph acyclic check actually rejects a
// REQUIRED cycle spanning more than the two capabilities being linked.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresCapabilityRegistryRoundTrip(t *testing.T) {
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

	const idA = "20000000-0000-0000-0000-0000000000f1"
	const idB = "20000000-0000-0000-0000-0000000000f2"
	const idC = "20000000-0000-0000-0000-0000000000f3"
	const keyA = "commerce.order.create.regtest"
	const keyB = "finance.invoice.issue.regtest"
	const keyC = "logistics.shipment.track.regtest"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM capability.capability_dependency WHERE capability_id = ANY($1::uuid[])`, []string{idA, idB, idC})
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = ANY($1::uuid[])`, []string{idA, idB, idC})
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	capA := capabilitydomain.Capability{ID: idA, Key: keyA, Name: "Order Create", DomainKey: "commerce", Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}
	capB := capabilitydomain.Capability{ID: idB, Key: keyB, Name: "Invoice Issue", DomainKey: "finance", Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}
	capC := capabilitydomain.Capability{ID: idC, Key: keyC, Name: "Shipment Track", DomainKey: "logistics", Lifecycle: capabilitydomain.CapabilityLifecycleSuspended, Maturity: capabilitydomain.CapabilityMaturityPreview}

	for _, c := range []capabilitydomain.Capability{capA, capB, capC} {
		if err := repo.CreateCapability(ctx, c); err != nil {
			t.Fatalf("create capability %s: %v", c.Key, err)
		}
	}

	stored, err := repo.GetCapability(ctx, keyC)
	if err != nil {
		t.Fatalf("get capability: %v", err)
	}
	if stored.Lifecycle != capabilitydomain.CapabilityLifecycleSuspended || stored.Maturity != capabilitydomain.CapabilityMaturityPreview {
		t.Fatalf("unexpected stored capability: %+v", stored)
	}
	if stored.IsResolvable() {
		t.Fatal("a SUSPENDED capability round-tripped as resolvable")
	}
	if !capA.IsResolvable() {
		// sanity: the fixture itself should be ACTIVE/resolvable so the
		// contrast with capC above is meaningful.
		t.Fatal("test fixture capA should be resolvable")
	}

	// A -> B (REQUIRED): fine on its own.
	if err := repo.CreateCapabilityDependency(ctx, capabilitydomain.CapabilityDependency{CapabilityKey: keyA, DependsOnCapability: keyB, DependencyType: capabilitydomain.DependencyTypeRequired}); err != nil {
		t.Fatalf("create dependency A->B: %v", err)
	}
	// B -> C (REQUIRED): fine on its own.
	if err := repo.CreateCapabilityDependency(ctx, capabilitydomain.CapabilityDependency{CapabilityKey: keyB, DependsOnCapability: keyC, DependencyType: capabilitydomain.DependencyTypeRequired}); err != nil {
		t.Fatalf("create dependency B->C: %v", err)
	}
	// C -> A (REQUIRED) would close A->B->C->A: must be rejected by the
	// whole-graph acyclic check, not merely a per-edge check.
	if err := repo.CreateCapabilityDependency(ctx, capabilitydomain.CapabilityDependency{CapabilityKey: keyC, DependsOnCapability: keyA, DependencyType: capabilitydomain.DependencyTypeRequired}); err == nil {
		t.Fatal("expected a three-hop REQUIRED cycle to be rejected")
	}
	// The same edge as OPTIONAL is fine: SS8 scopes the acyclic
	// requirement to REQUIRED dependencies only.
	if err := repo.CreateCapabilityDependency(ctx, capabilitydomain.CapabilityDependency{CapabilityKey: keyC, DependsOnCapability: keyA, DependencyType: capabilitydomain.DependencyTypeOptional}); err != nil {
		t.Fatalf("expected an OPTIONAL edge completing the same cycle to be accepted: %v", err)
	}

	deps, err := repo.ListCapabilityDependencies(ctx, keyA)
	if err != nil {
		t.Fatalf("list dependencies: %v", err)
	}
	if len(deps) != 1 || deps[0].DependsOnCapability != keyB || deps[0].DependencyType != capabilitydomain.DependencyTypeRequired {
		t.Fatalf("expected exactly one REQUIRED dependency A->B, got %+v", deps)
	}
}
