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

// TestPostgresIsolationProfileRoundTrip proves CreateIsolationProfile,
// GetIsolationProfile, AssignIsolationProfileToTenant (including its
// overlap-rejection exclusion constraint) and
// GetCurrentIsolationProfileForTenant round-trip against a real PostgreSQL
// instance (migrations 000004_isolation_profiles.sql and
// 000024_resolution_spine_invariants.sql's
// tenant_isolation_profile_active_excl), not just compile.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresIsolationProfileRoundTrip(t *testing.T) {
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

	const profileID = "80000000-0000-0000-0000-0000000000f1"
	const secondProfileID = "80000000-0000-0000-0000-0000000000f2"
	const profileName = "Schema Per Tenant Isolation Test"
	const secondProfileName = "Row Level Security Isolation Test"
	const tenantID = "tn_isolationtest"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM policy.tenant_isolation_profile WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM policy.isolation_profile WHERE isolation_profile_id IN ($1::uuid, $2::uuid)`, profileID, secondProfileID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	profile := domain.IsolationProfile{ID: profileID, Name: profileName, Strategy: "schema_per_tenant"}
	if err := repo.CreateIsolationProfile(ctx, profile); err != nil {
		t.Fatalf("create isolation profile: %v", err)
	}
	if err := repo.CreateIsolationProfile(ctx, profile); err == nil {
		t.Fatal("expected creating a duplicate isolation profile id to fail")
	}

	fetched, err := repo.GetIsolationProfile(ctx, profileID)
	if err != nil {
		t.Fatalf("get isolation profile: %v", err)
	}
	if fetched.Name != profileName || fetched.Strategy != "schema_per_tenant" {
		t.Fatalf("unexpected fetched isolation profile: %+v", fetched)
	}

	second := domain.IsolationProfile{ID: secondProfileID, Name: secondProfileName, Strategy: "row_level_security"}
	if err := repo.CreateIsolationProfile(ctx, second); err != nil {
		t.Fatalf("create second isolation profile: %v", err)
	}

	now := time.Now().UTC()
	assignment := domain.TenantIsolationProfileAssignment{TenantID: tenantID, IsolationProfileID: profileID, EffectiveFrom: now.Add(-time.Hour)}
	if err := repo.AssignIsolationProfileToTenant(ctx, assignment); err != nil {
		t.Fatalf("assign isolation profile: %v", err)
	}

	// A different profile for the same tenant with an overlapping period is
	// still rejected: the exclusion constraint is on tenant_id alone.
	overlapping := domain.TenantIsolationProfileAssignment{TenantID: tenantID, IsolationProfileID: secondProfileID, EffectiveFrom: now}
	if err := repo.AssignIsolationProfileToTenant(ctx, overlapping); !errors.Is(err, ErrTenantIsolationProfileOverlap) {
		t.Fatalf("expected ErrTenantIsolationProfileOverlap for an overlapping assignment, got %v", err)
	}

	current, err := repo.GetCurrentIsolationProfileForTenant(ctx, tenantID, now)
	if err != nil {
		t.Fatalf("get current isolation profile: %v", err)
	}
	if current.ID != profileID {
		t.Fatalf("expected the current isolation profile to be %s, got %+v", profileID, current)
	}
}
