package repository

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/market"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestMarketParticipationProjection proves the country projection follows
// market-lifecycle.yaml participation: a country row alone is not
// plannable, an available covering market makes it so and supplies its
// attributes, and the primary market is the earliest-activated one whose
// default_country is the country, else the earliest listing it. Markets
// outside the available statuses never count.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestMarketParticipationProjection(t *testing.T) {
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

	// The uncovered-country view names ACTIVE as the only available status.
	if got := market.AvailableStatuses(); !slices.Equal(got, []string{market.StatusActive}) {
		t.Fatalf("participation available_statuses %v no longer match market.uncovered_country_market", got)
	}

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	tenant, legalEntity := "tn_mpp"+suffix, "MPP-"+strings.ToUpper(suffix)
	const country, neighbour = "XR", "XS"
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM market.market WHERE code IN ($1, $2)`, country, neighbour)
		admin.Exec(ctx, `DELETE FROM market.registry WHERE owner_tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntity)
	}
	cleanup()
	t.Cleanup(cleanup)
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntity)
	mustExec(`INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference, tenant_id, legal_entity_id,
		display_name, isolation_strategy, residency_region) VALUES ('BOOTSTRAP', 'Test fixture registered outside admission', 'test-fixture',
		$1, $2, 'Participation projection test', 'row_level_security', 'af-south-1')`, tenant, legalEntity)

	now := time.Now().UTC()
	registry := func(id, status string, activatedAt *time.Time, config string) {
		t.Helper()
		var by *string
		if activatedAt != nil {
			checker := "checker"
			by = &checker
		}
		mustExec(`INSERT INTO market.registry (market_id, canonical_key, owner_tenant_id, configuration, status, created_at, created_by, activated_at, activated_by)
			VALUES ($1, $1, $2, $3::jsonb, $4, $5, 'maker', $6, $7)`, id+suffix, tenant, config, status, now.Add(-72*time.Hour), activatedAt, by)
	}
	project := func() {
		t.Helper()
		tx, err := repo.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := projectCountryParticipation(ctx, tx, country); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	type row struct {
		Name, Currency, Region, Primary string
		Active                          bool
	}
	projected := func() row {
		t.Helper()
		var r row
		if err := admin.QueryRow(ctx, `SELECT name, currency, region, COALESCE(registry_market_id, ''), is_active FROM market.market WHERE code = $1`,
			country).Scan(&r.Name, &r.Currency, &r.Region, &r.Primary, &r.Active); err != nil {
			t.Fatal(err)
		}
		return r
	}
	plannable := func(want bool, label string) {
		t.Helper()
		got, err := repo.PlanningMarket(ctx, country)
		if err != nil || got != want {
			t.Fatalf("%s: PlanningMarket = %v, %v; want %v", label, got, err, want)
		}
		var uncovered bool
		if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market.uncovered_country_market WHERE code = $1)`, country).Scan(&uncovered); err != nil {
			t.Fatal(err)
		}
		if uncovered == want {
			t.Fatalf("%s: uncovered_country_market lists %s = %v", label, country, uncovered)
		}
	}
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }

	// A country row the registry does not cover is not plannable, however
	// active it claims to be.
	if err := repo.CreateMarket(ctx, domain.Market{ID: domain.NewUUIDv7(), Code: country, Name: "Legacy", Currency: "USD", Region: "af-south-1", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	plannable(false, "a legacy row")
	// Nor does a covering market outside the available statuses count.
	registry("mkt_suspended", "SUSPENDED", at(-96*time.Hour), `{"name": "Suspended", "default_country": "XR", "default_currency": "EUR"}`)
	plannable(false, "only a suspended market")

	// A market listing the country covers it and becomes its primary.
	registry("mkt_regional", "ACTIVE", at(-48*time.Hour),
		`{"name": "Regional", "default_country": "XS", "countries": ["XS", "XR"], "default_currency": "KES", "operating_region_id": "east_africa"}`)
	project()
	if r := projected(); r != (row{"Regional", "KES", "east_africa", "mkt_regional" + suffix, true}) {
		t.Fatalf("projection from the listing market: %+v", r)
	}
	plannable(true, "a covering ACTIVE market")

	// A market whose default_country is the country outranks an earlier
	// one that only lists it; with no operating region, the region is the
	// country.
	registry("mkt_national", "ACTIVE", at(-24*time.Hour), `{"name": "National", "default_country": "XR", "default_currency": "UGX"}`)
	project()
	if r := projected(); r != (row{"National", "UGX", "XR", "mkt_national" + suffix, true}) {
		t.Fatalf("projection from the default-country market: %+v", r)
	}
	// Between default-country markets, the earliest activated is primary.
	registry("mkt_later", "ACTIVE", at(-time.Hour), `{"name": "Later", "default_country": "XR", "default_currency": "TZS"}`)
	project()
	if r := projected(); r.Primary != "mkt_national"+suffix {
		t.Fatalf("a later default-country market became primary: %+v", r)
	}
}
