package repository

import (
	"context"
	"os"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresMappingScopeRoundTrips is a regression test for Gate 2
// (docs/reconciliation/platform-resolution-spine-audit.md): until migration
// 000025_mapping_scope_dimensions.sql, mapping.mapping_scope had no columns
// for most of the dimensions domain.MappingScope already carried Go fields
// for (Gate 1, #61) -- and nothing in this repository ever wrote or read a
// MappingScope from Postgres at all. This proves CreateMappingScope,
// GetMappingScope and ListMappingScopes actually round-trip every one of
// those columns against a real PostgreSQL 17 instance, not just compile.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresMappingScopeRoundTrips(t *testing.T) {
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

	const scopeID = "30000000-0000-0000-0000-000000000001"
	const otherScopeID = "30000000-0000-0000-0000-000000000002"
	const tenantID = "tn_gate2test"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM mapping.mapping_scope WHERE mapping_scope_id = ANY($1)`, []string{scopeID, otherScopeID})
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	scope := domain.MappingScope{
		ScopeID:            scopeID,
		TenantID:           tenantID,
		LegalEntityID:      "le-1",
		OrganisationID:     "org-1",
		BusinessUnitID:     "bu-1",
		OperatingRegionID:  "or-1",
		GeographicRegionID: "gr-1",
		Country:            "KE",
		Currency:           "KES",
		Locale:             "en-KE",
		CatalogueID:        "cat-1",
		CustomerSegmentID:  "seg-1",
		Environment:        "production",
		DeploymentRegion:   "af-south-1",
		IncludeCountries:   []string{"KE", "UG"},
		ExcludeCountries:   []string{"SO"},
	}
	if err := repo.CreateMappingScope(ctx, scope); err != nil {
		t.Fatalf("create mapping scope: %v", err)
	}

	if err := repo.CreateMappingScope(ctx, scope); err == nil {
		t.Fatal("expected creating a duplicate mapping scope id to fail")
	}

	stored, err := repo.GetMappingScope(ctx, scopeID)
	if err != nil {
		t.Fatalf("get mapping scope: %v", err)
	}
	if stored.TenantID != tenantID {
		t.Fatalf("expected tenant_id %s, got %q", tenantID, stored.TenantID)
	}
	if stored.LegalEntityID != "le-1" || stored.OrganisationID != "org-1" || stored.BusinessUnitID != "bu-1" {
		t.Fatalf("unexpected identity dimensions: %+v", stored)
	}
	if stored.OperatingRegionID != "or-1" || stored.GeographicRegionID != "gr-1" {
		t.Fatalf("unexpected region dimensions: %+v", stored)
	}
	if stored.Country != "KE" || stored.Currency != "KES" || stored.Locale != "en-KE" {
		t.Fatalf("unexpected locale dimensions: %+v", stored)
	}
	if stored.CatalogueID != "cat-1" || stored.CustomerSegmentID != "seg-1" {
		t.Fatalf("unexpected catalogue/segment dimensions: %+v", stored)
	}
	if stored.Environment != "production" || stored.DeploymentRegion != "af-south-1" {
		t.Fatalf("unexpected environment dimensions: %+v", stored)
	}
	if len(stored.IncludeCountries) != 2 || stored.IncludeCountries[0] != "KE" || stored.IncludeCountries[1] != "UG" {
		t.Fatalf("expected include_countries to round-trip, got %+v", stored.IncludeCountries)
	}
	if len(stored.ExcludeCountries) != 1 || stored.ExcludeCountries[0] != "SO" {
		t.Fatalf("expected exclude_countries to round-trip, got %+v", stored.ExcludeCountries)
	}
	if stored.CreatedAt == "" || stored.UpdatedAt == "" {
		t.Fatal("expected created_at and updated_at to round-trip, got empty strings")
	}

	other := domain.MappingScope{ScopeID: otherScopeID, TenantID: "tn_other_gate2test"}
	if err := repo.CreateMappingScope(ctx, other); err != nil {
		t.Fatalf("create second mapping scope: %v", err)
	}

	scopes, err := repo.ListMappingScopes(ctx, tenantID)
	if err != nil {
		t.Fatalf("list mapping scopes: %v", err)
	}
	if len(scopes) != 1 || scopes[0].ScopeID != scopeID {
		t.Fatalf("expected exactly one mapping scope for %s, got %+v", tenantID, scopes)
	}
}
