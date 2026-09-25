package repository

import (
	"context"
	"os"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresDigitalEstateRoundTrip proves CreateDigitalEstate,
// GetDigitalEstate and ListDigitalEstatesForTenant round-trip against a real
// PostgreSQL instance (migration 000007_digital_estates.sql, which -- until
// now -- had no Go code reading or writing it at all).
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresDigitalEstateRoundTrip(t *testing.T) {
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

	const estateID = "60000000-0000-0000-0000-0000000000d1"
	const secondEstateID = "60000000-0000-0000-0000-0000000000d2"
	const tenantID = "tn_estatetest"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM estate.digital_estate WHERE digital_estate_id IN ($1::uuid, $2::uuid)`, estateID, secondEstateID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	estate := domain.DigitalEstate{ID: estateID, TenantID: tenantID, Name: "Estate Test Storefront", Domain: "estatetest.example.com", Status: domain.DigitalEstateActive}
	if err := repo.CreateDigitalEstate(ctx, estate); err != nil {
		t.Fatalf("create digital estate: %v", err)
	}
	if err := repo.CreateDigitalEstate(ctx, estate); err == nil {
		t.Fatal("expected creating a duplicate digital estate id to fail")
	}

	fetched, err := repo.GetDigitalEstate(ctx, estateID)
	if err != nil {
		t.Fatalf("get digital estate: %v", err)
	}
	if fetched.TenantID != tenantID || fetched.Domain != "estatetest.example.com" || fetched.Status != domain.DigitalEstateActive {
		t.Fatalf("unexpected fetched digital estate: %+v", fetched)
	}
	if fetched.CreatedAt.IsZero() {
		t.Fatal("expected created_at to be populated")
	}

	second := domain.DigitalEstate{ID: secondEstateID, TenantID: tenantID, Name: "Estate Test Wholesale", Domain: "wholesale.estatetest.example.com"}
	if err := repo.CreateDigitalEstate(ctx, second); err != nil {
		t.Fatalf("create second digital estate: %v", err)
	}

	estates, err := repo.ListDigitalEstatesForTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list digital estates: %v", err)
	}
	if len(estates) != 2 {
		t.Fatalf("expected exactly two digital estates for %s, got %+v", tenantID, estates)
	}
}
