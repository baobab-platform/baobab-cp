package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

func TestPEO02LifecycleFactsAreCanonicalAndTransactional(t *testing.T) {
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
	tests := []struct {
		kind, status, def, typ string
	}{
		{"SPONSORSHIP", "ACTIVE", "SponsorshipActivated", "founding-sponsorship.activated"},
		{"SPONSORSHIP", "SUSPENDED", "SponsorshipSuspended", "founding-sponsorship.suspended"},
		{"SPONSORSHIP", "REVOKED", "SponsorshipRevoked", "founding-sponsorship.revoked"},
		{"SPONSORSHIP", "EXPIRED", "SponsorshipExpired", "founding-sponsorship.expired"},
		{"DOCUMENTARY_DEFERRAL", "ACTIVE", "DeferralActivated", "founding-documentary-deferral.activated"},
		{"DOCUMENTARY_DEFERRAL", "REVOKED", "DeferralRevoked", "founding-documentary-deferral.revoked"},
		{"DOCUMENTARY_DEFERRAL", "EXPIRED", "DeferralExpired", "founding-documentary-deferral.expired"},
	}
	for _, test := range tests {
		t.Run(test.def, func(t *testing.T) {
			grantID, orgID := domain.NewUUIDv7(), domain.NewUUIDv7()
			meta := basestore.RequestMetadata{CorrelationID: domain.NewUUIDv7()}
			tx, err := db.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if err = publishFoundingLifecycle(ctx, tx, meta, test.kind, grantID, orgID, "", test.status); err != nil {
				t.Fatal(err)
			}
			var eventType string
			var raw []byte
			err = tx.QueryRow(ctx, `SELECT event_type,payload FROM messaging.outbox WHERE aggregate_id=$1`,
				grantID).Scan(&eventType, &raw)
			if err != nil {
				t.Fatal(err)
			}
			expected := "com.baobab-platform.control-plane." + test.typ + ".v1"
			if eventType != expected {
				t.Fatalf("unexpected canonical event type %q", eventType)
			}
			var envelope struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			}
			if err = json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			schema := contracts.MustSchema("admission/v2/founding-lifecycle-events.schema.json#/$defs/" + test.def)
			if err = contracts.Validate(schema, envelope.Data); err != nil {
				t.Fatalf("outbox payload violates canonical Shared type %s: %v", test.def, err)
			}
			// The event must not leak from a rolled-back governance change.
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var present bool
			if err = db.pool.QueryRow(ctx, `SELECT EXISTS(
			  SELECT 1 FROM messaging.outbox WHERE aggregate_id=$1)`, grantID).Scan(&present); err != nil {
				t.Fatal(err)
			}
			if present {
				t.Fatal("uncommitted governance event escaped rollback")
			}
		})
	}
}

func TestPEO02UnknownLifecycleFactCannotBePublished(t *testing.T) {
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
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = publishFoundingLifecycle(ctx, tx,
		basestore.RequestMetadata{CorrelationID: domain.NewUUIDv7()},
		"SPONSORSHIP", domain.NewUUIDv7(), domain.NewUUIDv7(), "", "REINSTATED"); err == nil {
		t.Fatal("unknown, authority-broadening lifecycle event accepted")
	}
}
