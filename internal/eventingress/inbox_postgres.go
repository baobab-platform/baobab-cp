package eventingress

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB is the part of a pgx pool the inbox uses.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresInbox is the Inbox over messaging.event_receipt (migration 000099).
type PostgresInbox struct{ DB DB }

var _ Inbox = PostgresInbox{}

func (i PostgresInbox) Accept(ctx context.Context, ev Event) (Receipt, error) {
	// One statement decides new versus seen, so concurrent deliveries of the same event cannot both be ACCEPTED.
	var receivedAt time.Time
	err := i.DB.QueryRow(ctx, `
		INSERT INTO messaging.event_receipt (source, event_id, event_type, tenant_id, key_id, body_sha256, body)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7)
		ON CONFLICT (source, event_id) DO NOTHING
		RETURNING received_at`, ev.Source, ev.ID, ev.Type, ev.TenantID, ev.KeyID, ev.BodySHA256, ev.Body).Scan(&receivedAt)
	if err == nil {
		return Receipt{EventID: ev.ID, Status: Accepted, ReceivedAt: receivedAt}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, err
	}
	var digest string
	if err := i.DB.QueryRow(ctx, `SELECT body_sha256, received_at FROM messaging.event_receipt WHERE source = $1 AND event_id = $2::uuid`,
		ev.Source, ev.ID).Scan(&digest, &receivedAt); err != nil {
		return Receipt{}, err
	}
	if digest != ev.BodySHA256 {
		return Receipt{}, ErrConflict
	}
	return Receipt{EventID: ev.ID, Status: Duplicate, ReceivedAt: receivedAt}, nil
}

func (i PostgresInbox) Due(ctx context.Context, now time.Time, types []string, limit int) ([]Pending, error) {
	rows, err := i.DB.Query(ctx, `
		SELECT source, event_id::text, event_type, body, attempts, received_at FROM messaging.event_receipt
		 WHERE status = 'pending' AND (next_attempt_at IS NULL OR next_attempt_at <= $1) AND event_type = ANY($2::text[])
		 ORDER BY received_at, event_id LIMIT $3`, now, types, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pending
	for rows.Next() {
		var p Pending
		if err := rows.Scan(&p.Source, &p.ID, &p.Type, &p.Body, &p.Attempts, &p.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (i PostgresInbox) MarkApplied(ctx context.Context, source, id string) error {
	_, err := i.DB.Exec(ctx, `UPDATE messaging.event_receipt SET status = 'applied', processed_at = now(), next_attempt_at = NULL, last_error = NULL
		WHERE source = $1 AND event_id = $2::uuid AND status = 'pending'`, source, id)
	return err
}

func (i PostgresInbox) MarkRetry(ctx context.Context, source, id string, attempts int, reason string, delay time.Duration) error {
	_, err := i.DB.Exec(ctx, `UPDATE messaging.event_receipt SET attempts = $3, last_error = $4, next_attempt_at = now() + make_interval(secs => $5)
		WHERE source = $1 AND event_id = $2::uuid AND status = 'pending'`, source, id, attempts, reason, delay.Seconds())
	return err
}

func (i PostgresInbox) MarkDeadLetter(ctx context.Context, source, id string, attempts int, reason string) error {
	_, err := i.DB.Exec(ctx, `UPDATE messaging.event_receipt SET status = 'dead_letter', attempts = $3, last_error = $4, processed_at = now(), next_attempt_at = NULL
		WHERE source = $1 AND event_id = $2::uuid AND status = 'pending'`, source, id, attempts, reason)
	return err
}

func (i PostgresInbox) Purge(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := i.DB.Exec(ctx, `DELETE FROM messaging.event_receipt WHERE status <> 'pending' AND received_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (i PostgresInbox) Backlog(ctx context.Context) (Backlog, error) {
	var b Backlog
	err := i.DB.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'pending'), count(*) FILTER (WHERE status = 'applied'),
		       count(*) FILTER (WHERE status = 'dead_letter'),
		       COALESCE(EXTRACT(EPOCH FROM now() - min(received_at) FILTER (WHERE status = 'pending'))::bigint, 0)
		  FROM messaging.event_receipt`).Scan(&b.Pending, &b.Applied, &b.DeadLetter, &b.OldestPendingSeconds)
	return b, err
}
