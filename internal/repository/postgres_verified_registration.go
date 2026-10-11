package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/verification"
)

// ErrVerifiedRegistrationUnavailable denies an absent, conflicting,
// superseded or stale registration identity. It is never an optimistic allow.
var ErrVerifiedRegistrationUnavailable = errors.New("no current independently verified registration identity for this case and organisation")

// ResolveVerifiedRegistration derives the company identifier *only* from
// Control Plane's OEV-03 ledger. It cannot be supplied by the browser or by a
// Trade/ERP/Payments consumer. The case, exact Organisation, current claim
// standing, positive CIPC check, same result/check chain and expiry are bound.
//
// A verified registration NUMBER establishes the registry identifier's
// existence; it still does NOT prove that this applicant controls the company
// or can act on its behalf. Those are separate legal-actor requirements.
func (r *PostgresRepository) ResolveVerifiedRegistration(
	ctx context.Context, caseID string, subject verification.Subject,
) (verification.VerifiedRegistrationBinding, error) {
	empty := verification.VerifiedRegistrationBinding{}
	if !strings.HasPrefix(caseID, "vcase_") ||
		subject.SubjectType != "ORGANISATION" || subject.SubjectID == "" {
		return empty, ErrVerifiedRegistrationUnavailable
	}
	src, err := verification.SourceByID("esrc_cipc")
	if err != nil || !src.Trusts("REGISTRATION_IDENTIFIER", "ZA") {
		return empty, ErrVerifiedRegistrationUnavailable
	}
	// Obtain at most two eligible results so ambiguous corporate identities
	// fail closed instead of selecting one by sort order.
	rows, err := r.pool.Query(ctx, `SELECT cl.claim_id,
	    cl.document->'claimed_value'->>'value',
	    res.result_id,
	    (res.document->>'freshness_until')::timestamptz
	FROM evidence.verification_case vc
	JOIN evidence.claim cl ON cl.case_id=vc.case_id
	JOIN evidence.result res ON res.result_id=cl.current_result_id
	WHERE vc.case_id=$1 AND vc.subject_type=$2 AND vc.subject_id=$3
	  AND cl.document->'subject'->>'subject_type'=$2
	  AND cl.document->'subject'->>'subject_id'=$3
	  AND cl.document->>'claim_type'='REGISTRATION_IDENTIFIER'
	  AND cl.document->>'jurisdiction'='ZA'
	  AND cl.status='VERIFIED' AND res.outcome='VERIFIED'
	  AND res.case_id=vc.case_id AND res.claim_id=cl.claim_id
	  AND vc.status NOT IN ('CANCELLED','SUPERSEDED','EXPIRED')
	  AND res.document->>'freshness_until' IS NOT NULL
	  AND (res.document->>'freshness_until')::timestamptz>clock_timestamp()
	  AND EXISTS (
	    SELECT 1 FROM evidence.check chk
	    WHERE chk.case_id=vc.case_id AND chk.claim_id=cl.claim_id
	      AND chk.source_id='esrc_cipc'
	      AND chk.outcome IN ('MATCHED','VERIFIED')
	      AND EXISTS (
	        SELECT 1 FROM jsonb_array_elements_text(res.document->'check_ids') AS cited(check_id)
	        WHERE cited.check_id=chk.check_id
	      )
	      -- PEO-03E: a pre-existing, self-asserted or legacy check citing
	      -- the CIPC name alone cannot bootstrap legal-name/status identity.
	      -- Require the current independently recorded observation that
	      -- actually supported the check when it was made.
	      AND EXISTS (
	        SELECT 1 FROM evidence.authoritative_source_observation ao
	        JOIN evidence.record source_evidence ON source_evidence.evidence_id=ao.evidence_id
	        WHERE ao.case_id=vc.case_id AND ao.claim_id=cl.claim_id
	          AND ao.source_id='esrc_cipc'
	          AND ao.outcome='MATCHED'
	          AND ao.source_record_reference=chk.document->>'source_record_reference'
	          AND ao.method=chk.document->>'method'
	          AND ao.recorded_at <= chk.performed_at
	          AND ao.source_observed_at <= chk.performed_at
	          AND ao.freshness_until>clock_timestamp()
	          AND source_evidence.status='AVAILABLE'
	          AND source_evidence.source_id=ao.source_id
	          AND ($4::boolean OR NOT ao.is_synthetic)
	      )
	  )
	LIMIT 2`, caseID, subject.SubjectType, subject.SubjectID,
		sourceObservationTestEnvironment(r.Environment))
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	var found verification.VerifiedRegistrationBinding
	count := 0
	for rows.Next() {
		var claimID, registrationNumber, resultID string
		var validUntil time.Time
		if err := rows.Scan(&claimID, &registrationNumber, &resultID, &validUntil); err != nil {
			return empty, err
		}
		found = verification.VerifiedRegistrationBinding{
			CaseID: caseID, Subject: subject,
			RegistrationNumber: strings.TrimSpace(registrationNumber),
			ClaimID:            claimID, ResultID: resultID, SourceID: "esrc_cipc",
			FreshnessUntil: validUntil,
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}
	if count != 1 || found.RegistrationNumber == "" {
		return empty, ErrVerifiedRegistrationUnavailable
	}
	return found, nil
}
