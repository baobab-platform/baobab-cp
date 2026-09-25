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

// TestPostgresResolvedContextRoundTrip proves CreateContext, GetContext and
// DeleteContextsByTenant round-trip against a real PostgreSQL instance
// (migration 000031_resolved_context_store.sql), not just compile.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresResolvedContextRoundTrip(t *testing.T) {
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

	const contextIDActive = "50000000-0000-0000-0000-0000000000c1"
	const contextIDExpired = "50000000-0000-0000-0000-0000000000c2"
	const tenantID = "tn_contexttest"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM context.resolved_context WHERE context_id IN ($1::uuid, $2::uuid)`, contextIDActive, contextIDExpired)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	resolved := domain.Context{
		ID:               contextIDActive,
		PrincipalID:      "principal-abc",
		TenantID:         tenantID,
		LegalEntityID:    "legal-456",
		MarketID:         "market-789",
		CountryCode:      "ZA",
		CurrencyCode:     "ZAR",
		Locale:           "en-ZA",
		DeploymentRegion: "af-south-1",
		Environment:      "production",
		CorrelationID:    "correlation-123",
		ResolvedAt:       now,
		ExpiresAt:        &expiresAt,
		Provenance: map[string]domain.ContextSource{
			"tenant_id": {Source: "verified_token", TrustLevel: domain.TrustVerified},
		},
	}
	if err := repo.CreateContext(ctx, resolved); err != nil {
		t.Fatalf("create context: %v", err)
	}
	if err := repo.CreateContext(ctx, resolved); err == nil {
		t.Fatal("expected creating a duplicate context id to fail")
	}

	fetched, err := repo.GetContext(ctx, contextIDActive)
	if err != nil {
		t.Fatalf("get context: %v", err)
	}
	if fetched.TenantID != tenantID || fetched.MarketID != "market-789" || fetched.CurrencyCode != "ZAR" {
		t.Fatalf("unexpected fetched context: %+v", fetched)
	}
	// PostgreSQL's timestamptz stores microsecond precision; Go's time.Now()
	// carries nanoseconds, so compare truncated to the DB's precision rather
	// than exact equality.
	if fetched.ExpiresAt == nil || !fetched.ExpiresAt.Truncate(time.Microsecond).Equal(expiresAt.Truncate(time.Microsecond)) {
		t.Fatalf("expected expires_at to round-trip, got %+v", fetched.ExpiresAt)
	}
	if fetched.Provenance["tenant_id"].TrustLevel != domain.TrustVerified {
		t.Fatalf("expected provenance to round-trip, got %+v", fetched.Provenance)
	}

	if _, err := repo.GetContext(ctx, "50000000-0000-0000-0000-0000000000c9"); !errors.Is(err, ErrContextNotFound) {
		t.Fatalf("expected ErrContextNotFound for a missing context, got %v", err)
	}

	expiredAt := now.Add(-time.Minute)
	expired := domain.Context{
		ID:            contextIDExpired,
		PrincipalID:   "principal-abc",
		TenantID:      tenantID,
		CorrelationID: "correlation-456",
		ResolvedAt:    now.Add(-time.Hour),
		ExpiresAt:     &expiredAt,
		Provenance: map[string]domain.ContextSource{
			"tenant_id": {Source: "verified_token", TrustLevel: domain.TrustVerified},
		},
	}
	if err := repo.CreateContext(ctx, expired); err != nil {
		t.Fatalf("create expired context: %v", err)
	}
	if _, err := repo.GetContext(ctx, contextIDExpired); !errors.Is(err, ErrContextNotFound) {
		t.Fatalf("expected an expired context to report ErrContextNotFound, got %v", err)
	}

	removed, err := repo.DeleteContextsByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("delete contexts by tenant: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected both the active and expired context rows removed, got %d", removed)
	}
	if _, err := repo.GetContext(ctx, contextIDActive); !errors.Is(err, ErrContextNotFound) {
		t.Fatal("expected the deleted context to be gone")
	}
}
