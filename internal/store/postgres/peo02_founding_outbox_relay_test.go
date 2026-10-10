package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

func TestPEO02EDurableRelayWithholdsPublicationUntilMatchingReceipt(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PostgreSQL 17 acceptance DB not configured")
	}
	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	grant, org := domain.NewUUIDv7(), domain.NewUUIDv7()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = publishFoundingLifecycle(ctx, tx, basestore.RequestMetadata{
		CorrelationID: domain.NewUUIDv7()}, "SPONSORSHIP", grant, org, "", "SUSPENDED"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var outboxID string
	var envelope []byte
	err = db.pool.QueryRow(ctx, `SELECT id::text,payload FROM messaging.outbox
		WHERE aggregate_id=$1 AND event_type LIKE '%founding-sponsorship.suspended.v1'`,
		grant).Scan(&outboxID, &envelope)
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(envelope, &event); err != nil {
		t.Fatal(err)
	}
	acknowledge := false
	callCount := 0
	token := domain.NewUUIDv7()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !acknowledge {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"event_id":%q,"durably_received":true}`, event.ID)
	}))
	defer server.Close()
	secretFile := filepath.Join(t.TempDir(), "iam-issued-synthetic-test-token")
	if err = os.WriteFile(secretFile, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	relay, err := NewFoundingOutboxRelay(db, server.URL+"/internal/v1/founding-lifecycle-events", secretFile)
	if err != nil {
		t.Fatal(err)
	}
	found, err := relay.DeliverOne(ctx)
	if !found || err == nil {
		t.Fatalf("unacknowledged delivery should remain pending: %v %v", found, err)
	}
	var marked bool
	if err = db.pool.QueryRow(ctx, `SELECT published_at IS NOT NULL
		FROM messaging.outbox WHERE id=$1::uuid`, outboxID).Scan(&marked); err != nil || marked {
		t.Fatalf("unsafe publication before durable ACK: %v %v", marked, err)
	}
	_, err = db.pool.Exec(ctx, `UPDATE messaging.outbox SET next_attempt_at=clock_timestamp()
		WHERE id=$1::uuid`, outboxID)
	if err != nil {
		t.Fatal(err)
	}
	acknowledge = true
	found, err = relay.DeliverOne(ctx)
	if !found || err != nil {
		t.Fatalf("retry should be accepted after durable receipt: %v %v", found, err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT published_at IS NOT NULL
		FROM messaging.outbox WHERE id=$1::uuid`, outboxID).Scan(&marked); err != nil || !marked {
		t.Fatalf("missing durable publication after ACK: %v %v", marked, err)
	}
	if callCount != 2 {
		t.Fatalf("expected two authenticated deliveries, got %d", callCount)
	}
}
