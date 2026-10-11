package repository

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/verification"
)

// boundCIPCCheck rejects a positive CIPC verification check unless its
// source reference resolves to exactly one CURRENT, immutable, same-claim,
// same-case, same-source authoritative observation with available evidence.
// It derives the evidence, observed timestamp and expiry from PostgreSQL;
// no HTTP client or reviewer can supply a fictitious positive proof chain.
//
// Historical checks remain append-only. This gate applies to newly recorded
// CIPC positive checks only. Broader issuers require their own certified
// policies; they are not automatically covered by this CIPC-specific gate.
func (r *PostgresRepository) boundCIPCCheck(
	ctx context.Context, tx pgx.Tx, caseID string, rec verification.CheckRecord, now time.Time,
) (verification.CheckRecord, string, error) {
	if rec.SourceID != "esrc_cipc" || !isPositiveSourceOutcome(rec.Outcome) {
		return rec, "", nil
	}
	if rec.SourceRecordReference == "" || now.IsZero() ||
		(rec.Method != "SOURCE_API_LOOKUP" &&
			rec.Method != "MANUAL_REGISTRY_LOOKUP" &&
			rec.Method != "OFFICIAL_DOCUMENT_REVIEW") {
		return rec, "", ErrAuthoritativeSourceConflict
	}
	rows, err := tx.Query(ctx, `SELECT o.observation_id, o.evidence_id,
			 o.source_observed_at,o.freshness_until
		FROM evidence.authoritative_source_observation o
		JOIN evidence.record e ON e.evidence_id = o.evidence_id
		WHERE o.case_id=$1 AND o.claim_id=$2 AND o.source_id='esrc_cipc'
		  AND o.source_record_reference=$3 AND o.method=$4
		  AND o.outcome='MATCHED' AND e.status='AVAILABLE'
		  AND e.source_id=o.source_id
		  AND o.source_observed_at <= $5::timestamptz
		  AND o.freshness_until > $5::timestamptz
		  AND ($6::boolean OR NOT o.is_synthetic)
		ORDER BY o.recorded_at DESC LIMIT 2`,
		caseID, rec.ClaimID, rec.SourceRecordReference, rec.Method, now,
		sourceObservationTestEnvironment(r.Environment))
	if err != nil {
		return rec, "", err
	}
	defer rows.Close()
	var proofID, evidenceID string
	var observedAt, expiresAt time.Time
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&proofID, &evidenceID, &observedAt, &expiresAt); err != nil {
			return rec, "", err
		}
	}
	if err := rows.Err(); err != nil {
		return rec, "", err
	}
	if count != 1 || !strings.HasPrefix(proofID, "aobs_") ||
		!strings.HasPrefix(evidenceID, "evr_") ||
		len(rec.EvidenceIDs) > 1 ||
		len(rec.EvidenceIDs) == 1 && rec.EvidenceIDs[0] != evidenceID ||
		rec.SourceObservedAt != nil && !rec.SourceObservedAt.Equal(observedAt) ||
		rec.FreshnessUntil != nil && rec.FreshnessUntil.After(expiresAt) {
		return rec, "", ErrAuthoritativeSourceConflict
	}
	rec.EvidenceIDs = []string{evidenceID}
	rec.SourceObservedAt = &observedAt
	rec.FreshnessUntil = &expiresAt
	return rec, proofID, nil
}

func isPositiveSourceOutcome(outcome string) bool {
	return outcome == "VERIFIED" || outcome == "MATCHED" || outcome == "PARTIALLY_VERIFIED"
}
