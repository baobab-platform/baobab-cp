// Package repository_test (external test package, unlike this package's
// other postgres_*_test.go files) because it exercises
// service.CompositionExpansionService end to end -- internal/service
// imports internal/repository, so a same-package test here would be an
// import cycle.
package repository_test

import (
	"context"
	"os"
	"testing"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	productdomain "github.com/baobab-platform/baobab-cp/internal/product/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresCompositionProductAndEntitlementProjectionRoundTrip proves
// CreateComposition/GetActiveComposition, CreateProduct/GetProduct,
// CreateProductVersion/GetProductVersion and
// CreateEntitlementProjection/ListEntitlementProjections round-trip against
// a real PostgreSQL 17 instance (migrations 000033, 000036), and that
// service.CompositionExpansionService.Expand drives the whole
// ProductSubscription -> CapabilityComposition -> CapabilityGrant pipeline
// end to end against real tables, not just the in-memory fixture.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresCompositionProductAndEntitlementProjectionRoundTrip(t *testing.T) {
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

	const capabilityID = "20000000-0000-0000-0000-0000000000f4"
	const capabilityKey = "erp.receivables.composition-test"
	const productID = "baobab-composition-test"
	const compositionKey = "solution.baobab-composition-test"
	const tenantID = "tn_compositiontest"
	var subscriptionID string

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM product.entitlement_projection WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_grant WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM product.product_subscription WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM product.product WHERE product_id = $1`, productID)
		admin.Exec(ctx, `DELETE FROM capability.capability_composition WHERE composition_key = $1`, compositionKey)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, capabilityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := admin.Exec(ctx, `INSERT INTO capability.capability(capability_id, code, name) VALUES ($1::uuid, $2, $3)`, capabilityID, capabilityKey, "Composition Test Capability"); err != nil {
		t.Fatalf("seed capability: %v", err)
	}

	repo, err := repository.Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	// Product / ProductVersion CRUD.
	if err := repo.CreateProduct(ctx, productdomain.Product{ID: productID, Name: "Composition Test Product", Status: productdomain.ProductLifecycleActive}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	product, err := repo.GetProduct(ctx, productID)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if product.Name != "Composition Test Product" {
		t.Fatalf("unexpected product: %+v", product)
	}

	if err := repo.CreateProductVersion(ctx, productdomain.ProductVersion{ProductID: productID, Version: "1.0.0", CompositionKey: compositionKey, Status: productdomain.ProductLifecycleActive}); err != nil {
		t.Fatalf("create product version: %v", err)
	}
	versions, err := admin.Query(ctx, `SELECT product_version_id::text FROM product.product_version WHERE product_id = $1`, productID)
	if err != nil {
		t.Fatalf("query product version id: %v", err)
	}
	var productVersionID string
	if versions.Next() {
		if err := versions.Scan(&productVersionID); err != nil {
			t.Fatalf("scan product version id: %v", err)
		}
	}
	versions.Close()
	if productVersionID == "" {
		t.Fatal("expected a product_version_id to have been assigned")
	}

	version, err := repo.GetProductVersion(ctx, productVersionID)
	if err != nil {
		t.Fatalf("get product version: %v", err)
	}
	if version.CompositionKey != compositionKey || version.Status != productdomain.ProductLifecycleActive {
		t.Fatalf("unexpected product version: %+v", version)
	}

	// CapabilityComposition CRUD.
	if err := repo.CreateComposition(ctx, capabilitydomain.CapabilityComposition{
		CompositionKey:  compositionKey,
		Name:            "Composition Test",
		CompositionType: capabilitydomain.CompositionTypeProduct,
		Version:         "1.0.0",
		Lifecycle:       capabilitydomain.CapabilityLifecycleActive,
		Members: []capabilitydomain.CompositionMember{
			{CapabilityKey: capabilityKey, Criticality: capabilitydomain.MembershipCriticalityMandatory},
		},
	}); err != nil {
		t.Fatalf("create composition: %v", err)
	}
	// A newer but DRAFT version must not be selected by GetActiveComposition.
	if err := repo.CreateComposition(ctx, capabilitydomain.CapabilityComposition{
		CompositionKey:  compositionKey,
		CompositionType: capabilitydomain.CompositionTypeProduct,
		Version:         "2.0.0",
		Lifecycle:       capabilitydomain.CapabilityLifecycleDraft,
		Members: []capabilitydomain.CompositionMember{
			{CapabilityKey: capabilityKey, Criticality: capabilitydomain.MembershipCriticalityMandatory},
		},
	}); err != nil {
		t.Fatalf("create draft composition: %v", err)
	}

	composition, err := repo.GetActiveComposition(ctx, compositionKey)
	if err != nil {
		t.Fatalf("get active composition: %v", err)
	}
	if composition.Version != "1.0.0" {
		t.Fatalf("expected the ACTIVE 1.0.0 composition, got version %s", composition.Version)
	}
	if len(composition.Members) != 1 || composition.Members[0].CapabilityKey != capabilityKey {
		t.Fatalf("unexpected composition members: %+v", composition.Members)
	}

	// Seed a real product_subscription row for entitlement_projection's FK.
	if err := admin.QueryRow(ctx, `
		INSERT INTO product.product_subscription(tenant_id, product_id, product_version_id, status, source)
		VALUES ($1, $2, $3::uuid, 'ACTIVE', 'ONBOARDING')
		RETURNING subscription_id::text`, tenantID, productID, productVersionID,
	).Scan(&subscriptionID); err != nil {
		t.Fatalf("seed product subscription: %v", err)
	}

	// Drive the full expansion pipeline end to end.
	svc := service.CompositionExpansionService{Compositions: repo, Grants: repo, Projections: repo}
	projections, err := svc.Expand(ctx, subscriptionID, tenantID, version)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(projections) != 1 || projections[0].Status != productdomain.EntitlementProjectionStatusMaterialized {
		t.Fatalf("expected exactly one MATERIALIZED projection, got %+v", projections)
	}

	stored, err := repo.ListEntitlementProjections(ctx, subscriptionID)
	if err != nil {
		t.Fatalf("list entitlement projections: %v", err)
	}
	if len(stored) != 1 || stored[0].CapabilityKey != capabilityKey || stored[0].GrantID == "" {
		t.Fatalf("unexpected stored projections: %+v", stored)
	}

	grants, err := repo.ListGrants(ctx, tenantID, capabilityKey)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 || grants[0].Source != capabilitydomain.GrantSourceProductSubscription || grants[0].SourceReference != subscriptionID {
		t.Fatalf("expected a single subscription-sourced grant, got %+v", grants)
	}
}
