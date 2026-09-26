// ADR-BCP-018 gate ORG-05 — the CorporateGroup derivation queue
// (migration 000055).

package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// GroupDerivationClaim is a group owing a derivation, claimed by one worker.
type GroupDerivationClaim struct {
	GroupID    string
	RequestSeq int64
	Attempts   int
}

// GroupDerivationState is a group's derivation record, for observability.
type GroupDerivationState struct {
	GroupID         string
	State           string // CURRENT, PENDING or RETRYING
	RequestedAt     *time.Time
	RequestReason   string
	Attempts        int
	NextAttemptAt   *time.Time
	LastAttemptAt   *time.Time
	LastSucceededAt *time.Time
	LastError       string
	LastAdded       int
	LastEnded       int
}

// Derivation states. A group is CURRENT when no derivation is owed, PENDING
// when one is owed and none has failed, and RETRYING (drifted) when the last
// attempt failed.
const (
	GroupDerivationCurrent  = "CURRENT"
	GroupDerivationPending  = "PENDING"
	GroupDerivationRetrying = "RETRYING"
)

// GroupDerivationRepository is the queue that keeps CorporateGroup
// membership converging on the corporate graph. Requests are written by
// database triggers in the transaction of each graph change, and by
// RequestCorporateGroupDerivations for the scheduled sweep.
type GroupDerivationRepository interface {
	// ClaimCorporateGroupDerivations returns up to limit groups whose
	// derivation is due at now and which no worker holds, leasing each for
	// lease so that no other worker derives it meanwhile.
	ClaimCorporateGroupDerivations(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]GroupDerivationClaim, error)
	// RecordCorporateGroupDerived records a successful derivation. It clears
	// the request only when no newer request arrived since the claim (seq);
	// otherwise the group stays due.
	RecordCorporateGroupDerived(ctx context.Context, groupID string, seq int64, at time.Time, added, ended int) error
	// RecordCorporateGroupDerivationFailure records a failed attempt and when
	// to retry. The request stays owed; when a newer request arrived since
	// the claim (seq), the retry is due at once, since the graph has changed.
	RecordCorporateGroupDerivationFailure(ctx context.Context, groupID string, seq int64, at time.Time, reason string, next time.Time) error
	// RequestCorporateGroupDerivations marks every derivable group as owing a
	// derivation and reports how many were marked.
	RequestCorporateGroupDerivations(ctx context.Context, reason string) (int, error)
	// GetCorporateGroupDerivation returns a group's derivation record, or nil
	// when the group has never been derivable.
	GetCorporateGroupDerivation(ctx context.Context, groupID string) (*GroupDerivationState, error)
}

var _ GroupDerivationRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) ClaimCorporateGroupDerivations(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]GroupDerivationClaim, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE registry.corporate_group_derivation d
		SET leased_until = $1::timestamptz + $2 * interval '1 microsecond', last_attempt_at = $1, updated_at = now()
		WHERE d.corporate_group_id IN (
			SELECT corporate_group_id FROM registry.corporate_group_derivation
			WHERE requested_at IS NOT NULL AND next_attempt_at <= $1 AND (leased_until IS NULL OR leased_until <= $1)
			ORDER BY next_attempt_at, corporate_group_id
			LIMIT $3
			FOR UPDATE SKIP LOCKED)
		RETURNING d.corporate_group_id::text, d.request_seq, d.attempts`, now, lease.Microseconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupDerivationClaim
	for rows.Next() {
		var (
			c     GroupDerivationClaim
			rowID string
		)
		if err := rows.Scan(&rowID, &c.RequestSeq, &c.Attempts); err != nil {
			return nil, err
		}
		if c.GroupID, err = domain.FormatResourceID(domain.CorporateGroupIDPrefix, rowID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) RecordCorporateGroupDerived(ctx context.Context, groupID string, seq int64, at time.Time, added, ended int) error {
	rowID, err := domain.ParseResourceID(domain.CorporateGroupIDPrefix, groupID)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		UPDATE registry.corporate_group_derivation SET
			last_succeeded_at = $2, last_error = NULL, attempts = 0, last_added = $4, last_ended = $5, leased_until = NULL,
			requested_at    = CASE WHEN request_seq = $3 THEN NULL ELSE requested_at END,
			request_reason  = CASE WHEN request_seq = $3 THEN NULL ELSE request_reason END,
			next_attempt_at = CASE WHEN request_seq = $3 THEN NULL ELSE now() END,
			updated_at = now()
		WHERE corporate_group_id = $1::uuid`, rowID, at, seq, added, ended)
	return err
}

func (r *PostgresRepository) RecordCorporateGroupDerivationFailure(ctx context.Context, groupID string, seq int64, at time.Time, reason string, next time.Time) error {
	rowID, err := domain.ParseResourceID(domain.CorporateGroupIDPrefix, groupID)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		UPDATE registry.corporate_group_derivation SET
			attempts = attempts + 1, last_error = left($3, 1000), last_attempt_at = $2, leased_until = NULL,
			next_attempt_at = CASE WHEN request_seq = $5 THEN $4 ELSE now() END, updated_at = now()
		WHERE corporate_group_id = $1::uuid AND requested_at IS NOT NULL`, rowID, at, reason, next, seq)
	return err
}

func (r *PostgresRepository) RequestCorporateGroupDerivations(ctx context.Context, reason string) (int, error) {
	var marked int
	err := r.pool.QueryRow(ctx, `SELECT registry.request_corporate_group_derivation(NULL, $1)`, reason).Scan(&marked)
	return marked, err
}

func (r *PostgresRepository) GetCorporateGroupDerivation(ctx context.Context, groupID string) (*GroupDerivationState, error) {
	rowID, err := domain.ParseResourceID(domain.CorporateGroupIDPrefix, groupID)
	if err != nil {
		return nil, err
	}
	s := GroupDerivationState{GroupID: groupID}
	var reason, lastError *string
	err = r.pool.QueryRow(ctx, `
		SELECT CASE WHEN requested_at IS NULL THEN 'CURRENT' WHEN attempts = 0 THEN 'PENDING' ELSE 'RETRYING' END,
			requested_at, request_reason, attempts, next_attempt_at, last_attempt_at, last_succeeded_at, last_error,
			last_added, last_ended
		FROM registry.corporate_group_derivation WHERE corporate_group_id = $1::uuid`, rowID).Scan(
		&s.State, &s.RequestedAt, &reason, &s.Attempts, &s.NextAttemptAt, &s.LastAttemptAt, &s.LastSucceededAt,
		&lastError, &s.LastAdded, &s.LastEnded)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if reason != nil {
		s.RequestReason = *reason
	}
	if lastError != nil {
		s.LastError = *lastError
	}
	return &s, nil
}
