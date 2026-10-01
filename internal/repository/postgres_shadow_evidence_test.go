package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestShadowEvidenceAggregatesPerDayAndPermission covers migration 000092:
// observations add to the day's row, days count once, and the read sums per
// permission.
func TestShadowEvidenceAggregatesPerDayAndPermission(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	const permission = "evidence.test"
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM policy.administrative_shadow_daily WHERE permission = $1`, permission)
	}
	cleanup()
	t.Cleanup(cleanup)

	day1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	obs := func(day time.Time, grants, agreement string, n int64, at time.Time) ShadowObservation {
		return ShadowObservation{Day: day, Permission: permission, Legacy: "allow", Grants: grants, Agreement: agreement, Decisions: n, FirstAt: at, LastAt: at}
	}
	must := func(o ...ShadowObservation) {
		t.Helper()
		if err := repo.RecordShadowObservations(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	must(obs(day1, "allow", "agree", 3, day1.Add(time.Hour)))
	must(obs(day1, "allow", "agree", 2, day1.Add(5*time.Hour)), obs(day1, "deny", "grants_narrower", 1, day1.Add(2*time.Hour)))
	must(obs(day2, "error", "not_evaluated", 4, day2.Add(time.Hour)), obs(day2, "allow", "agree", 0, day2)) // zero counts are not stored
	must()

	all, err := repo.ShadowEvidence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got *struct{ ok bool }
	for _, ev := range all {
		if ev.Permission != permission {
			continue
		}
		got = &struct{ ok bool }{true}
		if ev.Decisions != 10 || ev.Agree != 5 || ev.GrantsNarrower != 1 || ev.NotEvaluated != 4 || ev.UnresolvedOrError != 4 || ev.GrantsBroader != 0 || ev.ObservedDays != 2 {
			t.Fatalf("evidence summed wrongly: %+v", ev)
		}
		if ev.FirstObservedAt == nil || !ev.FirstObservedAt.Equal(day1.Add(time.Hour)) || !ev.LastObservedAt.Equal(day2.Add(time.Hour)) {
			t.Fatalf("observation window: %v .. %v", ev.FirstObservedAt, ev.LastObservedAt)
		}
	}
	if got == nil {
		t.Fatal("the permission is missing from the evidence")
	}
	// A value outside the closed sets is refused by the table, not stored.
	if err := repo.RecordShadowObservations(ctx, []ShadowObservation{obs(day1, "maybe", "agree", 1, day1)}); err == nil {
		t.Fatal("an unknown grants outcome must be refused")
	}
}
