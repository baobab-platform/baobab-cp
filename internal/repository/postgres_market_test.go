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

// TestPostgresMarketRoundTrip proves CreateMarket, GetMarket,
// GetMarketByCode, AssignMarketToTenant (including its overlap-rejection
// exclusion constraint) and ListActiveMarketsForTenant round-trip against a
// real PostgreSQL instance (migrations 000006_markets.sql and
// 000024_resolution_spine_invariants.sql's market_assignment_active_excl),
// not just compile.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresMarketRoundTrip(t *testing.T) {
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

	const marketID = "70000000-0000-0000-0000-0000000000e1"
	const secondMarketID = "70000000-0000-0000-0000-0000000000e2"
	const assignmentID = "70000000-0000-0000-0000-0000000000e3"
	const overlapAssignmentID = "70000000-0000-0000-0000-0000000000e4"
	const tenantID = "tn_markettest"
	const marketCode = "ZA-MARKETTEST"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM market.market_assignment WHERE market_assignment_id IN ($1::uuid, $2::uuid)`, assignmentID, overlapAssignmentID)
		admin.Exec(ctx, `DELETE FROM market.market WHERE market_id IN ($1::uuid, $2::uuid)`, marketID, secondMarketID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	market := domain.Market{ID: marketID, Code: marketCode, Name: "Market Test", Currency: "ZAR", Region: "af-south-1", IsActive: true}
	if err := repo.CreateMarket(ctx, market); err != nil {
		t.Fatalf("create market: %v", err)
	}
	if err := repo.CreateMarket(ctx, market); err == nil {
		t.Fatal("expected creating a duplicate market id to fail")
	}

	fetched, err := repo.GetMarket(ctx, marketID)
	if err != nil {
		t.Fatalf("get market: %v", err)
	}
	if fetched.Code != marketCode || fetched.Currency != "ZAR" {
		t.Fatalf("unexpected fetched market: %+v", fetched)
	}
	byCode, err := repo.GetMarketByCode(ctx, marketCode)
	if err != nil || byCode.ID != marketID {
		t.Fatalf("get market by code: %v, %+v", err, byCode)
	}

	now := time.Now().UTC()
	assignment := domain.MarketAssignment{
		ID: assignmentID, TenantID: tenantID, LegalEntityID: "ZURIBEANS-ZA", MarketID: marketID,
		Capabilities:  []domain.MarketParticipationCapability{domain.MarketParticipationSelling, domain.MarketParticipationImporting},
		EffectiveFrom: now.Add(-time.Hour),
		Status:        domain.MarketParticipationActive, Source: domain.MarketParticipationSourceProvisioning, PolicyVersion: "1",
	}
	if err := repo.AssignMarketToTenant(ctx, assignment); err != nil {
		t.Fatalf("assign market: %v", err)
	}

	overlapping := domain.MarketAssignment{
		ID: overlapAssignmentID, TenantID: tenantID, MarketID: marketID,
		Capabilities: []domain.MarketParticipationCapability{domain.MarketParticipationSelling}, EffectiveFrom: now,
		Status: domain.MarketParticipationActive, Source: domain.MarketParticipationSourceProvisioning, PolicyVersion: "1",
	}
	if err := repo.AssignMarketToTenant(ctx, overlapping); !errors.Is(err, ErrMarketAssignmentOverlap) {
		t.Fatalf("expected ErrMarketAssignmentOverlap for an overlapping assignment, got %v", err)
	}

	active, err := repo.ListActiveMarketsForTenant(ctx, tenantID, now)
	if err != nil {
		t.Fatalf("list active markets: %v", err)
	}
	if len(active) != 1 || active[0].ID != marketID {
		t.Fatalf("expected exactly one active market for %s, got %+v", tenantID, active)
	}

	assignments, err := repo.ListMarketAssignmentsForTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list market assignments: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected exactly one market assignment for %s, got %+v", tenantID, assignments)
	}
	if assignments[0].LegalEntityID != "ZURIBEANS-ZA" {
		t.Fatalf("expected legal_entity_id to round-trip, got %+v", assignments[0])
	}
	gotCapabilities := map[domain.MarketParticipationCapability]bool{}
	for _, c := range assignments[0].Capabilities {
		gotCapabilities[c] = true
	}
	if len(gotCapabilities) != 2 || !gotCapabilities[domain.MarketParticipationSelling] || !gotCapabilities[domain.MarketParticipationImporting] {
		t.Fatalf("expected SELLING and IMPORTING capabilities to round-trip, got %+v", assignments[0].Capabilities)
	}
}
