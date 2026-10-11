// PEO-02U: independent, current source-verified release of negative
// documentary restrictions. This does not change evidence, legal actor,
// original 24-month deferral or PSP/readiness state.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/jackc/pgx/v5"
)

type DocumentaryReleaseProposal struct {
	VerificationResultID string `json:"verification_result_id"`
	EvidenceReference    string `json:"evidence_reference"`
	Reason               string `json:"reason"`
}
type DocumentaryReleaseDecision struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

// A release is admissible only if a live governed PLATFORM_DOCUMENTARY policy
// explicitly binds its requirement/market to an externally trusted source and
// the OEV-03 verifier has a CURRENT, independent VERIFIED result of the
// original Organisation's actual claim. No release from applicant references.
func documentaryReleaseVerified(ctx context.Context, tx pgx.Tx, restrictionID, resultID, actor string, now time.Time) error {
	var sourceID, claimType, jurisdiction string
	var evidenceDocument []byte
	var maker, checker, sourceApprover, policyApprover, verifier string
	err := tx.QueryRow(ctx, `SELECT sp.source_id, c.document->>'claim_type',
       c.document->>'jurisdiction', res.document,
       ri.proposed_by::text, rd.decided_by::text,
       sp.approved_by::text, p.approved_by::text, res.decided_by
 FROM admission.active_documentary_remediation_restriction restriction
 JOIN admission.documentary_review_decision rd
   ON rd.decision_id=restriction.decision_id
 JOIN admission.documentary_review_intent ri ON ri.review_id=rd.review_id
 JOIN admission.founding_documentary_policy_requirement p
   ON p.policy_reference=ri.policy_reference
  AND p.requirement_id=restriction.requirement_id
 JOIN admission.documentary_release_source_policy sp
   ON sp.policy_reference=ri.policy_reference
  AND sp.requirement_id=restriction.requirement_id
  AND sp.market_code=restriction.market_code
 JOIN evidence.result res ON res.result_id=$2
 JOIN evidence.claim c ON c.claim_id=res.claim_id AND c.case_id=res.case_id
 JOIN evidence.verification_case vc ON vc.case_id=res.case_id
 WHERE restriction.restriction_id=$1::uuid AND restriction.status='ACTIVE'
  AND p.requirement_authority='PLATFORM_DOCUMENTARY'
  AND p.effective_from<=$3 AND p.effective_to>$3
  AND sp.effective_from<=$3 AND sp.effective_to>$3
  AND sp.claim_type=c.document->>'claim_type'
  AND c.document->>'jurisdiction'=sp.market_code
  AND c.document->>'purpose'='COMPLIANCE_REMEDIATION'
  AND vc.document->>'purpose'='COMPLIANCE_REMEDIATION'
  AND c.current_result_id=res.result_id AND c.status='VERIFIED'
  AND res.outcome='VERIFIED' AND vc.status='VERIFIED'
  AND vc.subject_type='ORGANISATION'
  AND vc.subject_id=restriction.organisation_id::text
  AND res.decided_at<=$3
  AND EXISTS (
    SELECT 1 FROM evidence.check ch
    WHERE ch.case_id=res.case_id AND ch.claim_id=res.claim_id
      AND ch.source_id=sp.source_id AND ch.outcome IN ('VERIFIED','MATCHED')
      AND EXISTS (
        SELECT 1 FROM jsonb_array_elements_text(res.document->'check_ids') AS item(check_id)
        WHERE item.check_id=ch.check_id
      )
  )
 ORDER BY sp.effective_from DESC LIMIT 1`, restrictionID, resultID, now).
		Scan(&sourceID, &claimType, &jurisdiction, &evidenceDocument,
			&maker, &checker, &sourceApprover, &policyApprover, &verifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDocumentaryReviewDenied
	}
	if err != nil {
		return err
	}
	// The binding cannot make a source authoritative; the source registry
	// must independently allow that claim type in that jurisdiction.
	source, err := verification.SourceByID(sourceID)
	if err != nil || !source.Trusts(claimType, jurisdiction) ||
		actor == maker || actor == checker || actor == sourceApprover ||
		actor == policyApprover || actor == verifier {
		return ErrDocumentaryReviewDenied
	}
	var doc struct {
		FreshnessUntil *time.Time `json:"freshness_until"`
		Outcome        string     `json:"outcome"`
	}
	if json.Unmarshal(evidenceDocument, &doc) != nil ||
		doc.Outcome != "VERIFIED" || doc.FreshnessUntil == nil ||
		!now.Before(*doc.FreshnessUntil) {
		return ErrDocumentaryReviewDenied
	}
	return nil
}

func (s *Store) ProposeDocumentaryRelease(ctx context.Context, key string, meta basestore.RequestMetadata,
	actor, restrictionID string, in DocumentaryReleaseProposal) (ReviewReceipt, error) {
	empty := ReviewReceipt{}
	if validateReviewKey(meta, actor, key) != nil || !domain.IsUUID(restrictionID) ||
		!strings.HasPrefix(in.VerificationResultID, "vres_") ||
		len(strings.TrimSpace(in.EvidenceReference)) < 3 || len(in.EvidenceReference) > 500 ||
		len(strings.TrimSpace(in.Reason)) < 10 || len(in.Reason) > 2000 {
		return empty, ErrDocumentaryReviewDenied
	}
	digest, err := reviewDigest(struct {
		Restriction string
		Input       DocumentaryReleaseProposal
	}{restrictionID, in})
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if err = reviewActorExists(ctx, tx, actor); err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02u:proposal:"+actor+":"+key); err != nil {
		return empty, err
	}
	var oldID, oldRestriction, oldDigest string
	err = tx.QueryRow(ctx, `SELECT release_intent_id::text,restriction_id::text,request_digest
	FROM admission.documentary_restriction_release_intent
	WHERE proposed_by=$1::uuid AND idempotency_key=$2`, actor, key).
		Scan(&oldID, &oldRestriction, &oldDigest)
	if err == nil {
		if oldRestriction == restrictionID && oldDigest == digest {
			return ReviewReceipt{ID: oldID, Status: "PENDING"}, nil
		}
		return empty, ErrDocumentaryReviewDenied
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	if err = documentaryReleaseVerified(ctx, tx, restrictionID, in.VerificationResultID, actor, time.Now().UTC()); err != nil {
		return empty, err
	}

	var id string
	err = tx.QueryRow(ctx, `INSERT INTO admission.documentary_restriction_release_intent
	  (restriction_id,verification_result_id,proposed_by,idempotency_key,
	   request_digest,reason,evidence_reference)
	  VALUES($1::uuid,$2,$3::uuid,$4,$5,$6,$7) RETURNING release_intent_id::text`,
		restrictionID, in.VerificationResultID, actor, key, digest, in.Reason, in.EvidenceReference).Scan(&id)
	if err != nil {
		return empty, err
	}
	if err = foundingAudit(ctx, tx, meta, "documentary_release_proposed",
		id, "DOCUMENTARY_RELEASE", map[string]any{"restriction_id": restrictionID,
			"verification_result_id": in.VerificationResultID}); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return ReviewReceipt{ID: id, Status: "PENDING"}, nil
}

func (s *Store) DecideDocumentaryRelease(ctx context.Context, key string, meta basestore.RequestMetadata,
	actor, intentID string, in DocumentaryReleaseDecision) (ReviewReceipt, error) {
	empty := ReviewReceipt{}
	if validateReviewKey(meta, actor, key) != nil || !domain.IsUUID(intentID) ||
		(in.Outcome != "APPROVE" && in.Outcome != "REJECT") ||
		len(strings.TrimSpace(in.Reason)) < 10 || len(in.Reason) > 2000 {
		return empty, ErrDocumentaryReviewDenied
	}
	digest, err := reviewDigest(struct {
		IntentID string
		Input    DocumentaryReleaseDecision
	}{intentID, in})
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if err = reviewActorExists(ctx, tx, actor); err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02u:decision:"+actor+":"+key); err != nil {
		return empty, err
	}
	var oldID, oldIntent, oldDigest, oldOutcome string
	err = tx.QueryRow(ctx, `SELECT release_decision_id::text,release_intent_id::text,request_digest,outcome
	FROM admission.documentary_restriction_release_decision
	WHERE decided_by=$1::uuid AND idempotency_key=$2`, actor, key).
		Scan(&oldID, &oldIntent, &oldDigest, &oldOutcome)
	if err == nil {
		if oldIntent == intentID && oldDigest == digest {
			return ReviewReceipt{ID: oldID, Status: oldOutcome}, nil
		}
		return empty, ErrDocumentaryReviewDenied
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var restrictionID, resultID, maker string
	err = tx.QueryRow(ctx, `SELECT restriction_id::text,verification_result_id,proposed_by::text
	FROM admission.documentary_restriction_release_intent
	WHERE release_intent_id=$1::uuid FOR UPDATE`, intentID).
		Scan(&restrictionID, &resultID, &maker)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrDocumentaryReviewDenied
	}
	if err != nil {
		return empty, err
	}
	if actor == maker {
		return empty, ErrDocumentaryReviewDenied
	}
	// The source result and authority can expire BETWEEN maker and checker.
	// Reject does not require a positive verified source, but approval does.
	if in.Outcome == "APPROVE" {
		if err = documentaryReleaseVerified(ctx, tx, restrictionID, resultID, actor, time.Now().UTC()); err != nil {
			return empty, err
		}
	}
	// Serialize competing approvals across intents for one restriction.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02u:restriction:"+restrictionID); err != nil {
		return empty, err
	}
	var previous string
	err = tx.QueryRow(ctx, `SELECT release_decision_id::text
	FROM admission.documentary_restriction_release_decision
	WHERE release_intent_id=$1::uuid`, intentID).Scan(&previous)
	if err == nil {
		return empty, ErrDocumentaryReviewDenied
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	if in.Outcome == "APPROVE" {
		var held bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(
		 SELECT 1 FROM admission.active_documentary_remediation_restriction
		 WHERE restriction_id=$1::uuid)`, restrictionID).Scan(&held); err != nil {
			return empty, err
		}
		if !held {
			return empty, ErrDocumentaryReviewDenied
		}
	}
	var decisionID string
	err = tx.QueryRow(ctx, `INSERT INTO admission.documentary_restriction_release_decision
	  (release_intent_id,restriction_id,verification_result_id,decided_by,
	   idempotency_key,request_digest,outcome,reason)
	  VALUES($1::uuid,$2::uuid,$3,$4::uuid,$5,$6,$7,$8)
	  RETURNING release_decision_id::text`,
		intentID, restrictionID, resultID, actor, key, digest, in.Outcome, in.Reason).Scan(&decisionID)
	if err != nil {
		return empty, err
	}
	if err = foundingAudit(ctx, tx, meta, "documentary_release_decided",
		decisionID, "DOCUMENTARY_RELEASE", map[string]any{"release_intent_id": intentID,
			"restriction_id": restrictionID, "outcome": in.Outcome,
			"verification_result_id": resultID}); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return ReviewReceipt{ID: decisionID, Status: in.Outcome}, nil
}
