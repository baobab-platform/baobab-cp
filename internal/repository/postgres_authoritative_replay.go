package repository

import (
	"context"
	"errors"

	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"github.com/jackc/pgx/v5"
)

// GetAuthoritativeObservationByKey is a metadata-only replay read. It cannot
// read raw source material, official credentials or decrypted artifacts.
func (r *PostgresRepository) GetAuthoritativeObservationByKey(ctx context.Context, actorID, key string) (authority.CaptureReceipt, error) {
	var result authority.CaptureReceipt
	if actorID == "" || len(key) < 16 || len(key) > 128 {
		return result, ErrAuthoritativeSourceConflict
	}
	err := r.pool.QueryRow(ctx, `SELECT observation_id,evidence_id,case_id,claim_id,
		source_id,source_observed_at,freshness_until
		FROM evidence.authoritative_source_observation
		WHERE recorded_by=$1 AND idempotency_key=$2`, actorID, key).
		Scan(&result.ObservationID, &result.EvidenceID, &result.CaseID,
			&result.ClaimID, &result.SourceID, &result.ObservedAt, &result.FreshnessUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return authority.CaptureReceipt{}, ErrVerificationNotFound
	}
	if err != nil {
		return authority.CaptureReceipt{}, err
	}
	result.AuthorityGranted = false
	return result, nil
}
