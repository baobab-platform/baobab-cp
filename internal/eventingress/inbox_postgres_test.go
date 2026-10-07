package eventingress

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

func TestPostgresInbox(t *testing.T) {
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
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	const source = "urn:baobab-platform:service:inbox-test"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM messaging.event_receipt WHERE source = ANY($1::text[])`, []string{source, source + "-other"})
	}
	cleanup()
	t.Cleanup(cleanup)
	inbox := PostgresInbox{DB: pool}
	event := func(id, digest string) Event {
		return Event{Source: source, ID: id, Type: erpType, TenantID: "tn_inboxtest", KeyID: "erp-delivery-2026-10", Body: []byte(`{"id":"` + id + `"}`), BodySHA256: digest}
	}
	d1, d2 := repeat("a", 64), repeat("b", 64)
	const a, b, c = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"

	first, err := inbox.Accept(ctx, event(a, d1))
	if err != nil || first.Status != Accepted || first.ReceivedAt.IsZero() {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	again, err := inbox.Accept(ctx, event(a, d1))
	if err != nil || again.Status != Duplicate || !again.ReceivedAt.Equal(first.ReceivedAt) {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	if _, err := inbox.Accept(ctx, event(a, d2)); !errors.Is(err, ErrConflict) {
		t.Fatalf("the same identity with other content must conflict: %v", err)
	}
	// The identity is (source, id): another source may use the same id.
	other := event(a, d2)
	other.Source = source + "-other"
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM messaging.event_receipt WHERE source = $1`, other.Source) })
	if r, err := inbox.Accept(ctx, other); err != nil || r.Status != Accepted {
		t.Fatalf("another source's identical id is a different event: %+v %v", r, err)
	}

	// Concurrent deliveries of one event: exactly one is ACCEPTED.
	var wg sync.WaitGroup
	results := make(chan Status, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, err := inbox.Accept(ctx, event(b, d1)); err == nil {
				results <- r.Status
			}
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for status := range results {
		if status == Accepted {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("exactly one concurrent delivery may be accepted, got %d", accepted)
	}
	if _, err := inbox.Accept(ctx, event(c, d1)); err != nil {
		t.Fatal(err)
	}

	// Due: only the handled types, only when the next attempt has come, oldest first.
	now := time.Now().Add(time.Minute)
	due, err := inbox.Due(ctx, now, []string{erpType}, 100)
	if err != nil {
		t.Fatal(err)
	}
	mine := filterSource(due, source)
	if len(mine) != 3 || mine[0].ID != a {
		t.Fatalf("due=%+v", mine)
	}
	if none, _ := inbox.Due(ctx, now, []string{"com.baobab-platform.erp.invoice.changed.v1"}, 100); len(filterSource(none, source)) != 0 {
		t.Fatal("a type nobody handles was returned")
	}
	if err := inbox.MarkRetry(ctx, source, a, 1, "not yet", time.Hour); err != nil {
		t.Fatal(err)
	}
	due, _ = inbox.Due(ctx, now, []string{erpType}, 100)
	if mine = filterSource(due, source); len(mine) != 2 {
		t.Fatalf("a retry that is not due was returned: %+v", mine)
	}
	if err := inbox.MarkApplied(ctx, source, b); err != nil {
		t.Fatal(err)
	}
	if err := inbox.MarkDeadLetter(ctx, source, c, 2, "gave up"); err != nil {
		t.Fatal(err)
	}
	due, _ = inbox.Due(ctx, now.Add(2*time.Hour), []string{erpType}, 100)
	if mine = filterSource(due, source); len(mine) != 1 || mine[0].ID != a || mine[0].Attempts != 1 {
		t.Fatalf("only the retry, now due, remains: %+v", mine)
	}

	// What was received is fixed; a processed receipt does not return to pending.
	if _, err := pool.Exec(ctx, `UPDATE messaging.event_receipt SET body = '{}'::bytea WHERE source = $1 AND event_id = $2::uuid`, source, a); err == nil {
		t.Fatal("a received body must be immutable")
	}
	if _, err := pool.Exec(ctx, `UPDATE messaging.event_receipt SET body_sha256 = $3 WHERE source = $1 AND event_id = $2::uuid`, source, a, d2); err == nil {
		t.Fatal("a received digest must be immutable")
	}
	if _, err := pool.Exec(ctx, `UPDATE messaging.event_receipt SET status = 'pending' WHERE source = $1 AND event_id = $2::uuid`, source, b); err == nil {
		t.Fatal("a processed receipt must not return to pending")
	}
	if err := inbox.MarkApplied(ctx, source, c); err != nil { // already dead-lettered: a late MarkApplied changes nothing
		t.Fatal(err)
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM messaging.event_receipt WHERE source = $1 AND event_id = $2::uuid`, source, c).Scan(&status)
	if status != "dead_letter" {
		t.Fatalf("a dead-lettered event was applied: %s", status)
	}

	// Backlog and purge: processed receipts older than the cutoff go, pending ones never.
	backlog, err := inbox.Backlog(ctx)
	if err != nil || backlog.Pending < 1 || backlog.Applied < 1 || backlog.DeadLetter < 1 {
		t.Fatalf("backlog=%+v err=%v", backlog, err)
	}
	purged, err := inbox.Purge(ctx, time.Now().Add(time.Hour))
	if err != nil || purged < 2 {
		t.Fatalf("purged=%d err=%v", purged, err)
	}
	var left int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM messaging.event_receipt WHERE source = $1`, source).Scan(&left)
	if left != 1 {
		t.Fatalf("only the pending receipt may remain, got %d", left)
	}
}

func filterSource(in []Pending, source string) []Pending {
	var out []Pending
	for _, p := range in {
		if p.Source == source {
			out = append(out, p)
		}
	}
	return out
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n)
	for len(out) < n {
		out = append(out, s...)
	}
	return string(out)
}
