// PEO-02E: durable, at-least-once authenticated founding lifecycle delivery.
// A positive HTTP response is not enough: receiver must explicitly confirm
// the exact CloudEvent ID has been committed to its durable inbox.
package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type FoundingOutboxRelay struct {
	Store     *Store
	Endpoint  string
	TokenFile string
	Client    *http.Client
}

func NewFoundingOutboxRelay(store *Store, endpoint, tokenFile string) (*FoundingOutboxRelay, error) {
	target, err := url.Parse(endpoint)
	if err != nil || target.Hostname() == "" || target.User != nil ||
		target.RawQuery != "" || target.Fragment != "" ||
		target.Path != "/internal/v1/founding-lifecycle-events" ||
		(target.Scheme != "https" && !(target.Scheme == "http" &&
			(target.Hostname() == "localhost" || target.Hostname() == "127.0.0.1"))) {
		return nil, errors.New("PEO-02E delivery requires explicit TLS subscriptions event inbox endpoint")
	}
	if store == nil || tokenFile == "" || !strings.HasPrefix(tokenFile, "/") {
		return nil, errors.New("PEO-02E requires PostgreSQL and absolute IAM-managed token path")
	}
	return &FoundingOutboxRelay{
		Store: store, Endpoint: endpoint, TokenFile: tokenFile,
		Client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func (r *FoundingOutboxRelay) send(ctx context.Context, raw []byte) error {
	token, err := os.ReadFile(r.TokenFile)
	if err != nil {
		return errors.New("IAM workload identity unavailable")
	}
	bearer := strings.TrimSpace(string(token))
	if bearer == "" || len(bearer) > 8192 || strings.ContainsAny(bearer, " \r\n\t") {
		return errors.New("invalid IAM-managed bearer token file")
	}
	var event struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &event); err != nil || event.ID == "" {
		return errors.New("invalid canonical outbox CloudEvent")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/cloudevents+json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	client := r.Client
	if client == nil {
		return errors.New("authenticated HTTP client absent")
	}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("receiver request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusOK {
		return fmt.Errorf("receiver did not accept durable receipt: HTTP %d", res.StatusCode)
	}
	var ack struct {
		EventID         string `json:"event_id"`
		DurablyReceived bool   `json:"durably_received"`
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8193))
	if err != nil || len(data) > 8192 || json.Unmarshal(data, &ack) != nil ||
		!ack.DurablyReceived || ack.EventID != event.ID {
		return errors.New("receiver did not attest durable receipt of same canonical event")
	}
	return nil
}

// DeliverOne locks one outstanding lifecycle event and marks it published
// ONLY after a durable receiver acknowledgement. A retry after lost ACK is
// safe: the receiver's event-ID/digest inbox returns idempotent replay.
func (r *FoundingOutboxRelay) DeliverOne(ctx context.Context) (bool, error) {
	tx, err := r.Store.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var outboxID string
	var event []byte
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id::text, payload, publish_attempts
		FROM messaging.outbox WHERE published_at IS NULL
		AND (next_attempt_at IS NULL OR next_attempt_at<=clock_timestamp())
		AND (event_type LIKE 'com.baobab-platform.control-plane.founding-sponsorship.%.v1'
		 OR event_type LIKE 'com.baobab-platform.control-plane.founding-documentary-deferral.%.v1')
		ORDER BY occurred_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).
		Scan(&outboxID, &event, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sendErr := r.send(ctx, event)
	if sendErr == nil {
		_, err = tx.Exec(ctx, `UPDATE messaging.outbox SET published_at=clock_timestamp(),
			publish_attempts=publish_attempts+1,next_attempt_at=NULL,last_error=NULL
			WHERE id=$1::uuid AND published_at IS NULL`, outboxID)
	} else {
		exponent := attempts
		if exponent > 7 {
			exponent = 7
		}
		delay := 2 << exponent
		if delay > 300 {
			delay = 300
		}
		_, err = tx.Exec(ctx, `UPDATE messaging.outbox SET
			publish_attempts=publish_attempts+1,
			next_attempt_at=clock_timestamp()+($2::int * INTERVAL '1 second'),
			last_error=$3 WHERE id=$1::uuid AND published_at IS NULL`,
			outboxID, delay, truncateDeliveryFailure(sendErr.Error()))
	}
	if err != nil {
		return true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return true, err
	}
	return true, sendErr
}

func truncateDeliveryFailure(detail string) string {
	if len(detail) > 240 {
		return detail[:240]
	}
	return detail
}

func (r *FoundingOutboxRelay) Run(ctx context.Context, interval time.Duration, observe func(error)) {
	if interval < time.Second {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for i := 0; i < 32; i++ {
			hasEvent, err := r.DeliverOne(ctx)
			if err != nil && observe != nil {
				observe(err)
			}
			if !hasEvent || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
