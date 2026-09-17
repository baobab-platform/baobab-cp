package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// TestPostgresCapabilityScopeAndGrantRoundTrip proves CreateCapabilityScope,
// GetCapabilityScope, ListCapabilityScopes, CreateGrant, ListGrants and
// RevokeGrant round-trip against a real PostgreSQL 17 instance (migration
// 000029_capability_scope_and_grant.sql), not just compile.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresCapabilityScopeAndGrantRoundTrip(t *testing.T) {
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

	const capabilityID = "20000000-0000-0000-0000-0000000000e1"
	const capabilityKey = "erp.receivables.grant-test"
	const scopeID = "30000000-0000-0000-0000-0000000000e1"
	const grantID = "40000000-0000-0000-0000-0000000000e1"
	const tenantID = "tn_granttest"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM capability.capability_grant WHERE grant_id = $1::uuid`, grantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id = $1::uuid`, scopeID)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, capabilityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := admin.Exec(ctx, `INSERT INTO capability.capability(capability_id, code, name) VALUES ($1::uuid, $2, $3)`, capabilityID, capabilityKey, "ERP Receivables Grant Test"); err != nil {
		t.Fatalf("seed capability: %v", err)
	}

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	scope := capabilitydomain.CapabilityScope{
		ScopeID:          scopeID,
		TenantID:         tenantID,
		MarketID:         "market-za",
		IncludeCountries: []string{"ZA", "BW"},
		ExcludeCountries: []string{"BW"},
	}
	if err := repo.CreateCapabilityScope(ctx, scope); err != nil {
		t.Fatalf("create capability scope: %v", err)
	}
	if err := repo.CreateCapabilityScope(ctx, scope); err == nil {
		t.Fatal("expected creating a duplicate capability scope id to fail")
	}

	storedScope, err := repo.GetCapabilityScope(ctx, scopeID)
	if err != nil {
		t.Fatalf("get capability scope: %v", err)
	}
	if storedScope.TenantID != tenantID || storedScope.MarketID != "market-za" {
		t.Fatalf("unexpected stored scope: %+v", storedScope)
	}
	if len(storedScope.IncludeCountries) != 2 || len(storedScope.ExcludeCountries) != 1 {
		t.Fatalf("expected country lists to round-trip, got %+v", storedScope)
	}

	scopes, err := repo.ListCapabilityScopes(ctx, tenantID)
	if err != nil {
		t.Fatalf("list capability scopes: %v", err)
	}
	if len(scopes) != 1 || scopes[0].ScopeID != scopeID {
		t.Fatalf("expected exactly one capability scope for %s, got %+v", tenantID, scopes)
	}

	grant := capabilitydomain.CapabilityGrant{
		ID:            grantID,
		TenantID:      tenantID,
		CapabilityKey: capabilityKey,
		ScopeID:       scopeID,
		Source:        capabilitydomain.GrantSourcePlatformBaseline,
		Status:        capabilitydomain.GrantStatusActive,
		EffectiveFrom: time.Now().UTC().Add(-time.Hour),
	}
	if err := repo.CreateGrant(ctx, grant); err != nil {
		t.Fatalf("create grant: %v", err)
	}

	unknown := grant
	unknown.ID = "40000000-0000-0000-0000-0000000000e2"
	unknown.CapabilityKey = "does.not.exist"
	if err := repo.CreateGrant(ctx, unknown); err == nil {
		t.Fatal("expected create grant against an unknown capability_key to fail")
	}

	grants, err := repo.ListGrants(ctx, tenantID, capabilityKey)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 || grants[0].ID != grantID {
		t.Fatalf("expected exactly one grant, got %+v", grants)
	}
	if grants[0].Status != capabilitydomain.GrantStatusActive || grants[0].Version != 1 {
		t.Fatalf("unexpected grant state: %+v", grants[0])
	}
	if !grants[0].IsEffective(time.Now().UTC()) {
		t.Fatal("expected the round-tripped grant to report itself effective")
	}

	if err := repo.RevokeGrant(ctx, grantID, "admin-1", "test revocation", 1); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	if err := repo.RevokeGrant(ctx, grantID, "admin-1", "test revocation", 1); err == nil {
		t.Fatal("expected revoking with a stale version to fail")
	}

	revoked, err := repo.ListGrants(ctx, tenantID, capabilityKey)
	if err != nil {
		t.Fatalf("list grants after revoke: %v", err)
	}
	if len(revoked) != 1 || revoked[0].Status != capabilitydomain.GrantStatusRevoked {
		t.Fatalf("expected the grant to remain historically queryable as REVOKED, got %+v", revoked)
	}
	if revoked[0].RevokedBy != "admin-1" || revoked[0].RevocationReason != "test revocation" {
		t.Fatalf("expected revocation provenance to round-trip, got %+v", revoked[0])
	}
	if revoked[0].IsEffective(time.Now().UTC()) {
		t.Fatal("a revoked grant must never report itself effective")
	}
}

// TestPostgresCreateCapabilityScopeAcceptsUnrestrictedCountryLists is a
// regression test: CreateCapabilityScope used to bind a nil
// IncludeCountries/ExcludeCountries slice as an explicit SQL NULL rather
// than letting include_countries/exclude_countries' NOT NULL DEFAULT '{}'
// (migration 000029) apply, so the common case of a scope with no country
// restriction -- "an unspecified dimension means not further restricted",
// per CapabilityScope's own doc comment -- always failed the NOT NULL
// constraint. Surfaced by Programme Gate P4's CompositionExpansionService,
// the first caller to create a tenant-baseline scope with every dimension
// left unset.
func TestPostgresCreateCapabilityScopeAcceptsUnrestrictedCountryLists(t *testing.T) {
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

	const scopeID = "30000000-0000-0000-0000-0000000000f5"
	const tenantID = "tn_unrestrictedscope"

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()
	cleanup := func() { admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id = $1::uuid`, scopeID) }
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	if err := repo.CreateCapabilityScope(ctx, capabilitydomain.CapabilityScope{ScopeID: scopeID, TenantID: tenantID}); err != nil {
		t.Fatalf("create capability scope with no country restriction: %v", err)
	}
	stored, err := repo.GetCapabilityScope(ctx, scopeID)
	if err != nil {
		t.Fatalf("get capability scope: %v", err)
	}
	if len(stored.IncludeCountries) != 0 || len(stored.ExcludeCountries) != 0 {
		t.Fatalf("expected empty, not nil-vs-populated-mismatched, country lists, got %+v", stored)
	}
}
