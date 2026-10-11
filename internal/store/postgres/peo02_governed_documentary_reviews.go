// PEO-02S: explicit, independent documentary review/appeal decisions.
// A review can record missing evidence or impose a scoped negative hold.
// It must NEVER verify legal claims or grant provider/financial permission.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
)

var ErrDocumentaryReviewDenied = errors.New("PEO-02S: independent review authority or evidence not established")

type ReviewAssessmentInput struct {
	DeferralID      string    `json:"deferral_id"`
	OrganisationID  string    `json:"organisation_id"`
	MilestoneMonth  int       `json:"milestone_month"`
	ReviewReference string    `json:"review_reference"`
	ReviewedAt      time.Time `json:"reviewed_at"`
	ReviewedBy      string    `json:"reviewed_by"`
	Applicability   []struct {
		AssessmentReference string    `json:"assessment_reference"`
		OrganisationID      string    `json:"organisation_id"`
		RequirementID       string    `json:"requirement_id"`
		Authority           string    `json:"authority"`
		Applies             string    `json:"applies"`
		PolicyReference     string    `json:"policy_reference"`
		PolicyVersion       string    `json:"policy_version"`
		ContextReference    string    `json:"context_reference"`
		Capability          string    `json:"capability"`
		Market              string    `json:"market"`
		EvidenceState       string    `json:"evidence_state"`
		EvidenceReference   string    `json:"evidence_reference"`
		EvaluatedAt         time.Time `json:"evaluated_at"`
		ValidUntil          time.Time `json:"valid_until"`
	} `json:"applicability_assessments"`
}
type ReviewReceipt struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
type ReviewRestriction struct {
	RequirementID string `json:"requirement_id"`
	CapabilityKey string `json:"capability_key"`
	MarketCode    string `json:"market_code"`
}
type ReviewDecisionInput struct {
	Outcome           string              `json:"outcome"`
	DecisionReference string              `json:"decision_reference"`
	Reason            string              `json:"reason"`
	Restrictions      []ReviewRestriction `json:"restrictions,omitempty"`
}
type ReviewAppealInput struct {
	Reason            string `json:"reason"`
	EvidenceReference string `json:"evidence_reference"`
}
type ReviewAppealDecisionInput struct {
	Outcome           string `json:"outcome"`
	Reason            string `json:"reason"`
	EvidenceReference string `json:"evidence_reference"`
}

func reviewDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}
func validateReviewKey(meta basestore.RequestMetadata, actor, key string) error {
	if requireProgressiveHuman(meta, actor) != nil ||
		len(key) < 16 || len(key) > 128 {
		return ErrDocumentaryReviewDenied
	}
	return nil
}
func reviewActorExists(ctx context.Context, tx pgx.Tx, id string) error {
	var found bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.principal
  WHERE principal_id=$1::uuid AND actor_type='human')`, id).Scan(&found); err != nil {
		return err
	}
	if !found {
		return ErrDocumentaryReviewDenied
	}
	return nil
}
func validateReviewAssessment(raw []byte, actor, deferral string, m int, now time.Time) (ReviewAssessmentInput, error) {
	var in ReviewAssessmentInput
	if len(raw) == 0 || len(raw) > 64*1024 {
		return in, ErrDocumentaryReviewDenied
	}
	if err := contracts.Validate(contracts.MustSchema("admission/v2/documentary-review.schema.json#/$defs/DocumentaryReviewAssessment"), raw); err != nil {
		return in, ErrDocumentaryReviewDenied
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, ErrDocumentaryReviewDenied
	}
	if in.DeferralID != deferral || in.MilestoneMonth != m || in.ReviewedBy != actor ||
		in.ReviewedAt.IsZero() || in.ReviewedAt.After(now) ||
		now.Sub(in.ReviewedAt) > 24*time.Hour || !domain.IsUUID(in.OrganisationID) {
		return in, ErrDocumentaryReviewDenied
	}
	seen := map[string]bool{}
	contextRef := ""
	for _, v := range in.Applicability {
		if seen[v.RequirementID] || v.OrganisationID != in.OrganisationID ||
			v.ContextReference == "" || (contextRef != "" && contextRef != v.ContextReference) ||
			v.EvaluatedAt.IsZero() || v.EvaluatedAt.After(now) ||
			!now.Before(v.ValidUntil) {
			return in, ErrDocumentaryReviewDenied
		}
		// Approved evidence and NOT_APPLICABLE are input claims; only a
		// future independently attested evaluator can promote their meaning.
		seen[v.RequirementID] = true
		contextRef = v.ContextReference
	}
	return in, nil
}

// ProposeDocumentaryReview snapshots all original requirements, policy and
// staged assessments. No self-attested source may mark a requirement fulfilled.
func (s *Store) ProposeDocumentaryReview(ctx context.Context, key string, meta basestore.RequestMetadata,
	actor, deferral string, m int, raw []byte) (ReviewReceipt, error) {
	empty := ReviewReceipt{}
	if validateReviewKey(meta, actor, key) != nil || !domain.IsUUID(deferral) {
		return empty, ErrDocumentaryReviewDenied
	}
	now := time.Now().UTC()
	input, err := validateReviewAssessment(raw, actor, deferral, m, now)
	if err != nil {
		return empty, err
	}
	stable := json.RawMessage(raw)
	digest, err := reviewDigest(stable)
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
		"peo02s:propose:"+actor+":"+key); err != nil {
		return empty, err
	}
	var prevID, prevHash, prevStatus string
	err = tx.QueryRow(ctx, `SELECT review_id::text,request_digest,state
  FROM admission.documentary_review_intent WHERE proposed_by=$1::uuid AND idempotency_key=$2`, actor, key).
		Scan(&prevID, &prevHash, &prevStatus)
	if err == nil {
		if prevHash != digest {
			return empty, ErrDocumentaryReviewDenied
		}
		return ReviewReceipt{prevID, prevStatus}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var sourceOrg, policy string
	var requirements []string
	err = tx.QueryRow(ctx, `SELECT organisation_id::text,policy_reference,requirement_ids
   FROM admission.founding_documentary_review_checkpoint
   WHERE deferral_id=$1::uuid AND milestone_month=$2 AND scheduled_at<=clock_timestamp()
   FOR SHARE`, deferral, m).Scan(&sourceOrg, &policy, &requirements)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrDocumentaryReviewDenied
	}
	if err != nil {
		return empty, err
	}
	if sourceOrg != input.OrganisationID || len(input.Applicability) != len(requirements) {
		return empty, ErrDocumentaryReviewDenied
	}
	originals := map[string]bool{}
	for _, req := range requirements {
		originals[req] = true
	}
	for _, v := range input.Applicability {
		if !originals[v.RequirementID] || v.PolicyReference != policy {
			return empty, ErrDocumentaryReviewDenied
		}
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02s:checkpoint:"+deferral+":"+fmt.Sprint(m)); err != nil {
		return empty, err
	}
	// Another review at the same original milestone requires a previously
	// adjudicated independent RETURN_FOR_REASSESSMENT appeal, never a retry.
	revision := 1
	var reopenedFrom *string
	var priorRevision int
	var priorAppealID, priorAppealOutcome string
	err = tx.QueryRow(ctx, `SELECT r.revision,
	   COALESCE(a.appeal_id::text,''),COALESCE(ad.outcome,'')
	   FROM admission.documentary_review_intent r
	   LEFT JOIN admission.documentary_review_decision d ON d.review_id=r.review_id
	   LEFT JOIN admission.documentary_review_appeal a ON a.decision_id=d.decision_id
	   LEFT JOIN admission.documentary_appeal_decision ad ON ad.appeal_id=a.appeal_id
	   WHERE r.deferral_id=$1::uuid AND r.milestone_month=$2
	   ORDER BY r.revision DESC LIMIT 1 FOR UPDATE OF r`,
		deferral, m).Scan(&priorRevision, &priorAppealID, &priorAppealOutcome)
	if err == nil {
		if priorAppealOutcome != "RETURN_FOR_REASSESSMENT" || priorAppealID == "" || priorRevision >= 16 {
			return empty, ErrDocumentaryReviewDenied
		}
		revision = priorRevision + 1
		reopenedFrom = &priorAppealID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO admission.documentary_review_intent
	  (deferral_id,milestone_month,revision,reopened_from_appeal_id,
	   organisation_id,proposed_by,idempotency_key,request_digest,assessment,policy_reference)
	  VALUES($1::uuid,$2,$3,$4::uuid,$5::uuid,$6::uuid,$7,$8,$9::jsonb,$10)
	  RETURNING review_id::text`,
		deferral, m, revision, reopenedFrom, sourceOrg, actor, key, digest, raw, policy).Scan(&id)
	if err != nil {
		return empty, err
	}
	if err = foundingAudit(ctx, tx, meta, "documentary_review_proposed", id, "DOCUMENTARY_REVIEW",
		map[string]any{"deferral_id": deferral, "milestone": m, "revision": revision}); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return ReviewReceipt{id, "PENDING"}, nil
}
func validateRestrictionScope(x ReviewRestriction) bool {
	return len(x.RequirementID) >= 3 && len(x.CapabilityKey) >= 3 &&
		len(x.MarketCode) == 2 && x.MarketCode[0] >= 'A' && x.MarketCode[0] <= 'Z' &&
		x.MarketCode[1] >= 'A' && x.MarketCode[1] <= 'Z' &&
		!strings.ContainsAny(x.CapabilityKey, "* /?") &&
		!strings.ContainsAny(x.RequirementID, "* /?")
}
func (s *Store) DecideDocumentaryReview(ctx context.Context, key string, meta basestore.RequestMetadata,
	actor, reviewID string, in ReviewDecisionInput) (ReviewReceipt, error) {
	empty := ReviewReceipt{}
	if validateReviewKey(meta, actor, key) != nil || !domain.IsUUID(reviewID) ||
		(in.Outcome != "REMEDIATION_REQUIRED" && in.Outcome != "ASSESSMENT_INCOMPLETE") ||
		len(strings.TrimSpace(in.Reason)) < 10 || len(in.Reason) > 2000 ||
		len(strings.TrimSpace(in.DecisionReference)) < 3 || len(in.DecisionReference) > 500 ||
		(in.Outcome == "REMEDIATION_REQUIRED" && len(in.Restrictions) == 0) ||
		(in.Outcome == "ASSESSMENT_INCOMPLETE" && len(in.Restrictions) > 0) {
		return empty, ErrDocumentaryReviewDenied
	}
	digest, err := reviewDigest(in)
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
	// Serialize the actor/key pair BEFORE locking an individual review. The
	// unique index spans reviews, so per-review locking alone is insufficient.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02s:review-decision-key:"+actor+":"+key); err != nil {
		return empty, err
	}
	var reusedDecisionID, reusedReviewID, reusedDigest string
	err = tx.QueryRow(ctx, `SELECT decision_id::text,review_id::text,request_digest
   FROM admission.documentary_review_decision
   WHERE decided_by=$1::uuid AND idempotency_key=$2`, actor, key).
		Scan(&reusedDecisionID, &reusedReviewID, &reusedDigest)
	if err == nil {
		if reusedReviewID == reviewID && reusedDigest == digest {
			return ReviewReceipt{reusedDecisionID, "DECIDED"}, nil
		}
		return empty, ErrDocumentaryReviewDenied
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02s:review:"+reviewID); err != nil {
		return empty, err
	}
	var maker, org, state string
	var assessment []byte
	err = tx.QueryRow(ctx, `SELECT proposed_by::text,organisation_id::text,state,assessment
   FROM admission.documentary_review_intent WHERE review_id=$1::uuid FOR UPDATE`,
		reviewID).Scan(&maker, &org, &state, &assessment)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrDocumentaryReviewDenied
	}
	if err != nil {
		return empty, err
	}
	if maker == actor {
		return empty, ErrDocumentaryReviewDenied
	}
	if state == "DECIDED" {
		var oldID, oldDigest, oldActor, oldKey string
		err = tx.QueryRow(ctx, `SELECT decision_id::text,request_digest,decided_by::text,idempotency_key
    FROM admission.documentary_review_decision WHERE review_id=$1::uuid`, reviewID).
			Scan(&oldID, &oldDigest, &oldActor, &oldKey)
		if err != nil {
			return empty, err
		}
		if oldDigest == digest && oldActor == actor && oldKey == key {
			return ReviewReceipt{oldID, "DECIDED"}, nil
		}
		return empty, ErrDocumentaryReviewDenied
	}
	var reviewed ReviewAssessmentInput
	if err = json.Unmarshal(assessment, &reviewed); err != nil {
		return empty, err
	}
	expected := map[string]ReviewRestriction{}
	for _, a := range reviewed.Applicability {
		// A restricted scope must correspond to an unresolved named
		// requirement. Explicit NOT_APPLICABLE or claimant-asserted
		// APPROVED_EVIDENCE inputs cannot become an arbitrary negative
		// capability hold without fresh source-backed assessment.
		if (a.Applies == "REQUIRED" || a.Applies == "NEEDS_ASSESSMENT") &&
			a.EvidenceState != "APPROVED_EVIDENCE" &&
			a.Capability != "" && a.Market != "" {
			expected[a.RequirementID] = ReviewRestriction{
				RequirementID: a.RequirementID, CapabilityKey: a.Capability, MarketCode: a.Market}
		}
	}
	seen := map[string]bool{}
	for _, r := range in.Restrictions {
		allowed, ok := expected[r.RequirementID]
		if !ok || !validateRestrictionScope(r) ||
			allowed.CapabilityKey != r.CapabilityKey || allowed.MarketCode != r.MarketCode ||
			seen[r.RequirementID] {
			return empty, ErrDocumentaryReviewDenied
		}
		seen[r.RequirementID] = true
	}
	var decisionID string
	err = tx.QueryRow(ctx, `INSERT INTO admission.documentary_review_decision
  (review_id,decided_by,idempotency_key,request_digest,outcome,decision_reference,reason)
  VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7) RETURNING decision_id::text`,
		reviewID, actor, key, digest, in.Outcome, in.DecisionReference, in.Reason).Scan(&decisionID)
	if err != nil {
		return empty, err
	}
	for _, r := range in.Restrictions {
		if _, err = tx.Exec(ctx, `INSERT INTO admission.documentary_remediation_restriction
    (decision_id,organisation_id,requirement_id,capability_key,market_code)
    VALUES($1::uuid,$2::uuid,$3,$4,$5)`,
			decisionID, org, r.RequirementID, r.CapabilityKey, r.MarketCode); err != nil {
			return empty, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE admission.documentary_review_intent SET state='DECIDED'
   WHERE review_id=$1::uuid AND state='PENDING'`, reviewID); err != nil {
		return empty, err
	}
	if err = foundingAudit(ctx, tx, meta, "documentary_review_decided", decisionID, "DOCUMENTARY_REVIEW",
		map[string]any{"review_id": reviewID, "outcome": in.Outcome, "scope_count": len(in.Restrictions)}); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return ReviewReceipt{decisionID, "DECIDED"}, nil
}
func (s *Store) FileDocumentaryReviewAppeal(ctx context.Context, key string, meta basestore.RequestMetadata,
	actor, decisionID string, in ReviewAppealInput) (ReviewReceipt, error) {
	empty := ReviewReceipt{}
	if validateReviewKey(meta, actor, key) != nil || !domain.IsUUID(decisionID) ||
		len(strings.TrimSpace(in.Reason)) < 10 || len(in.Reason) > 2000 ||
		len(strings.TrimSpace(in.EvidenceReference)) < 3 || len(in.EvidenceReference) > 500 {
		return empty, ErrDocumentaryReviewDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if err = reviewActorExists(ctx, tx, actor); err != nil {
		return empty, err
	}
	var checker string
	err = tx.QueryRow(ctx, `SELECT decided_by::text FROM admission.documentary_review_decision
   WHERE decision_id=$1::uuid`, decisionID).Scan(&checker)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrDocumentaryReviewDenied
	}
	if err != nil {
		return empty, err
	}
	if actor == checker {
		return empty, ErrDocumentaryReviewDenied
	}
	// Appeal idempotency is actor-scoped across all decisions. A reused key
	// cannot be inserted for a different decision and surface as HTTP 500.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02s:appeal-key:"+actor+":"+key); err != nil {
		return empty, err
	}
	var reusedAppealID, reusedDecisionID, reusedReason, reusedEvidence string
	err = tx.QueryRow(ctx, `SELECT appeal_id::text,decision_id::text,reason,evidence_reference
   FROM admission.documentary_review_appeal
   WHERE raised_by=$1::uuid AND idempotency_key=$2`, actor, key).
		Scan(&reusedAppealID, &reusedDecisionID, &reusedReason, &reusedEvidence)
	if err == nil {
		if reusedDecisionID == decisionID && reusedReason == in.Reason &&
			reusedEvidence == in.EvidenceReference {
			return ReviewReceipt{reusedAppealID, "FILED"}, nil
		}
		return empty, ErrDocumentaryReviewDenied
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02s:appeal:"+decisionID); err != nil {
		return empty, err
	}
	var appealID, priorActor, priorKey, priorReason, priorEvidence string
	err = tx.QueryRow(ctx, `SELECT appeal_id::text,raised_by::text,idempotency_key,reason,evidence_reference
   FROM admission.documentary_review_appeal WHERE decision_id=$1::uuid`,
		decisionID).Scan(&appealID, &priorActor, &priorKey, &priorReason, &priorEvidence)
	if err == nil {
		if priorActor != actor || priorKey != key || priorReason != in.Reason ||
			priorEvidence != in.EvidenceReference {
			return empty, ErrDocumentaryReviewDenied
		}
		return ReviewReceipt{appealID, "FILED"}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO admission.documentary_review_appeal
    (decision_id,raised_by,idempotency_key,reason,evidence_reference)
    VALUES($1::uuid,$2::uuid,$3,$4,$5) RETURNING appeal_id::text`,
		decisionID, actor, key, in.Reason, in.EvidenceReference).Scan(&appealID)
	if err != nil {
		return empty, err
	}
	if err = foundingAudit(ctx, tx, meta, "documentary_appeal_filed", appealID, "DOCUMENTARY_APPEAL",
		map[string]any{"review_decision_id": decisionID}); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return ReviewReceipt{appealID, "FILED"}, nil
}
func (s *Store) DecideDocumentaryReviewAppeal(ctx context.Context, key string, meta basestore.RequestMetadata,
	actor, appealID string, in ReviewAppealDecisionInput) (ReviewReceipt, error) {
	empty := ReviewReceipt{}
	if validateReviewKey(meta, actor, key) != nil || !domain.IsUUID(appealID) ||
		(in.Outcome != "UPHOLD" && in.Outcome != "RETURN_FOR_REASSESSMENT") ||
		len(strings.TrimSpace(in.Reason)) < 10 || len(in.Reason) > 2000 ||
		len(strings.TrimSpace(in.EvidenceReference)) < 3 || len(in.EvidenceReference) > 500 {
		return empty, ErrDocumentaryReviewDenied
	}
	digest, err := reviewDigest(in)
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
	// Separate actor/key locking precedes the individual appeal row lock:
	// otherwise the global unique index can reject a different appeal as 500.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02s:appeal-decision-key:"+actor+":"+key); err != nil {
		return empty, err
	}
	var reusedDecisionID, reusedAppealID, reusedDigest string
	err = tx.QueryRow(ctx, `SELECT decision_id::text,appeal_id::text,request_digest
   FROM admission.documentary_appeal_decision
   WHERE decided_by=$1::uuid AND idempotency_key=$2`, actor, key).
		Scan(&reusedDecisionID, &reusedAppealID, &reusedDigest)
	if err == nil {
		if reusedAppealID == appealID && reusedDigest == digest {
			return ReviewReceipt{reusedDecisionID, "DECIDED"}, nil
		}
		return empty, ErrDocumentaryReviewDenied
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var appellant, previousChecker, reviewMaker, state string
	err = tx.QueryRow(ctx, `SELECT a.raised_by::text,d.decided_by::text,r.proposed_by::text,a.state
   FROM admission.documentary_review_appeal a
   JOIN admission.documentary_review_decision d ON d.decision_id=a.decision_id
   JOIN admission.documentary_review_intent r ON r.review_id=d.review_id
   WHERE a.appeal_id=$1::uuid FOR UPDATE OF a`, appealID).
		Scan(&appellant, &previousChecker, &reviewMaker, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrDocumentaryReviewDenied
	}
	if err != nil {
		return empty, err
	}
	if actor == appellant || actor == previousChecker || actor == reviewMaker {
		return empty, ErrDocumentaryReviewDenied
	}
	if state == "DECIDED" {
		var id, oldKey, oldActor, oldDigest string
		err = tx.QueryRow(ctx, `SELECT decision_id::text,idempotency_key,decided_by::text,request_digest
    FROM admission.documentary_appeal_decision WHERE appeal_id=$1::uuid`, appealID).
			Scan(&id, &oldKey, &oldActor, &oldDigest)
		if err != nil {
			return empty, err
		}
		if oldKey == key && oldActor == actor && oldDigest == digest {
			return ReviewReceipt{id, "DECIDED"}, nil
		}
		return empty, ErrDocumentaryReviewDenied
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO admission.documentary_appeal_decision
    (appeal_id,decided_by,idempotency_key,request_digest,outcome,reason,evidence_reference)
    VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7) RETURNING decision_id::text`,
		appealID, actor, key, digest, in.Outcome, in.Reason, in.EvidenceReference).Scan(&id)
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `UPDATE admission.documentary_review_appeal SET state='DECIDED'
   WHERE appeal_id=$1::uuid AND state='FILED'`, appealID); err != nil {
		return empty, err
	}
	// Successful appeals only REQUEST a fresh independent reassessment. This
	// intentionally never deletes, relaxes or auto-expires a scope hold.
	if err = foundingAudit(ctx, tx, meta, "documentary_appeal_decided", id, "DOCUMENTARY_APPEAL",
		map[string]any{"appeal_id": appealID, "outcome": in.Outcome}); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return ReviewReceipt{id, "DECIDED"}, nil
}

// ScopeBlock is a negative-only current-time query for registered consumers.
// It conveys no positive applicability, permit, evidence or entitlement.
func (s *Store) DocumentaryScopeBlocked(ctx context.Context, org, capability, market string) (bool, error) {
	if !domain.IsUUID(org) || !validateRestrictionScope(ReviewRestriction{
		RequirementID: "scope-check", CapabilityKey: capability, MarketCode: market}) {
		return true, ErrDocumentaryReviewDenied
	}
	var blocked bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1
  FROM admission.active_documentary_remediation_restriction
  WHERE organisation_id=$1::uuid AND capability_key=$2 AND market_code=$3)`,
		org, capability, market).Scan(&blocked)
	if err != nil {
		return true, err
	}
	return blocked, nil
}
