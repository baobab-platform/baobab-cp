package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/service/progressive"
)

func TestPEO02RReviewMilestonesUseOriginalUTCAnniversary(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PostgreSQL 17 acceptance DB not configured")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	starts := []time.Time{
		time.Date(2024, 2, 29, 23, 45, 0, 0, time.UTC),
		time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 3, 14, 55, 0, 0, time.UTC),
	}
	for _, zone := range []string{"UTC", "Africa/Johannesburg", "America/New_York"} {
		tx, e := db.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE '`+zone+`'`); e != nil {
			tx.Rollback(ctx)
			t.Fatal(e)
		}
		for _, first := range starts {
			for _, months := range []int{6, 12, 18, 24} {
				var actual time.Time
				e = tx.QueryRow(ctx,
					`SELECT admission.founding_review_milestone_utc($1,$2)`,
					first, months).Scan(&actual)
				if e != nil {
					tx.Rollback(ctx)
					t.Fatal(e)
				}
				want := progressive.CalendarAnniversaryUTC(first, months)
				if !actual.Equal(want) {
					t.Errorf("zone %s month %d from %v: got %v, want %v",
						zone, months, first, actual.UTC(), want)
				}
			}
		}
		var invalid *time.Time
		if e = tx.QueryRow(ctx,
			`SELECT admission.founding_review_milestone_utc($1,25)`,
			starts[0]).Scan(&invalid); e != nil || invalid != nil {
			tx.Rollback(ctx)
			t.Fatalf("unapproved month 25 checkpoint accepted: %+v %v", invalid, e)
		}
		if e = tx.Rollback(ctx); e != nil {
			t.Fatal(e)
		}
	}
}

func TestPEO02RReviewSweepIsIdempotentAndNeverIssuesPermissions(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PostgreSQL 17 acceptance DB not configured")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	// The suite contains no verified founding sponsor. The scheduler must
	// neither create one nor manufacture a positive documentary finding.
	var beforeGrants, beforeDeferrals int
	if err = db.pool.QueryRow(ctx,
		`SELECT count(*) FROM admission.founding_group_sponsorship`).
		Scan(&beforeGrants); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx,
		`SELECT count(*) FROM admission.founding_documentary_deferral`).
		Scan(&beforeDeferrals); err != nil {
		t.Fatal(err)
	}
	_, err = db.SweepFoundingDocumentaryReviews(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SweepFoundingDocumentaryReviews(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	var grants, deferrals, duplicates, invalid int
	err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.founding_group_sponsorship`).Scan(&grants)
	if err != nil || grants != beforeGrants {
		t.Fatalf("review scheduler changed sponsorships: %v", err)
	}
	err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.founding_documentary_deferral`).Scan(&deferrals)
	if err != nil || deferrals != beforeDeferrals {
		t.Fatalf("review scheduler changed original deferrals: %v", err)
	}
	err = db.pool.QueryRow(ctx, `SELECT count(*) FROM (
	   SELECT deferral_id,milestone_month,COUNT(*) FROM admission.founding_documentary_review_checkpoint
	   GROUP BY deferral_id,milestone_month HAVING COUNT(*) > 1) duplicates`).Scan(&duplicates)
	if err != nil || duplicates != 0 {
		t.Fatalf("scheduler duplicated a review checkpoint: %v %v", duplicates, err)
	}
	err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.founding_documentary_review_checkpoint
	   WHERE status<>'REASSESSMENT_REQUIRED' OR scheduled_at>clock_timestamp()`).Scan(&invalid)
	if err != nil || invalid != 0 {
		t.Fatalf("review scheduler created a positive or future decision: %v %v", invalid, err)
	}
}
