-- PEO-02U: documentary restriction RELEASE is an independently governed,
-- evidence-bound decision. Historical negative findings remain immutable.
-- No original deferral date, legal mandate or provider grant is modified.

CREATE TABLE admission.documentary_release_source_policy (
  policy_reference text NOT NULL,
  requirement_id text NOT NULL,
  claim_type text NOT NULL CHECK(length(btrim(claim_type)) BETWEEN 3 AND 160),
  source_id text NOT NULL CHECK(length(btrim(source_id)) BETWEEN 3 AND 160),
  market_code text NOT NULL CHECK(market_code ~ '^[A-Z]{2}$'),
  authority_basis_reference text NOT NULL CHECK(length(btrim(authority_basis_reference)) BETWEEN 3 AND 500),
  approved_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  approved_at timestamptz NOT NULL,
  effective_from timestamptz NOT NULL,
  effective_to timestamptz NOT NULL,
  CHECK(effective_to>effective_from AND approved_at<=effective_to),
  PRIMARY KEY (policy_reference, requirement_id, market_code, source_id)
);

CREATE TABLE admission.documentary_restriction_release_intent (
  release_intent_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  restriction_id uuid NOT NULL REFERENCES admission.documentary_remediation_restriction(restriction_id),
  verification_result_id text NOT NULL REFERENCES evidence.result(result_id),
  proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
  request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
  reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 10 AND 2000),
  evidence_reference text NOT NULL CHECK(length(btrim(evidence_reference)) BETWEEN 3 AND 500),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(proposed_by,idempotency_key)
);

CREATE TABLE admission.documentary_restriction_release_decision (
  release_decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  release_intent_id uuid NOT NULL UNIQUE REFERENCES admission.documentary_restriction_release_intent(release_intent_id),
  restriction_id uuid NOT NULL REFERENCES admission.documentary_remediation_restriction(restriction_id),
  verification_result_id text NOT NULL REFERENCES evidence.result(result_id),
  decided_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
  request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
  outcome text NOT NULL CHECK(outcome IN ('APPROVE','REJECT')),
  reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 10 AND 2000),
  decided_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(decided_by,idempotency_key)
);
-- Each restriction/source-result pair can be independently released only once.
-- A revoked/expired result re-imposes the negative hold automatically; later
-- independently verified successor results may support a NEW approval.
CREATE UNIQUE INDEX documentary_restriction_one_approval_per_source_result
 ON admission.documentary_restriction_release_decision(restriction_id,verification_result_id)
 WHERE outcome='APPROVE';

CREATE FUNCTION admission.documentary_restriction_release_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'documentary release policy and decisions are append-only'
    USING ERRCODE='check_violation';
END $$;
CREATE TRIGGER documentary_release_source_policy_immutable BEFORE UPDATE OR DELETE
 ON admission.documentary_release_source_policy FOR EACH ROW
 EXECUTE FUNCTION admission.documentary_restriction_release_immutable();
CREATE TRIGGER documentary_release_intent_immutable BEFORE UPDATE OR DELETE
 ON admission.documentary_restriction_release_intent FOR EACH ROW
 EXECUTE FUNCTION admission.documentary_restriction_release_immutable();
CREATE TRIGGER documentary_release_decision_immutable BEFORE UPDATE OR DELETE
 ON admission.documentary_restriction_release_decision FOR EACH ROW
 EXECUTE FUNCTION admission.documentary_restriction_release_immutable();

-- CP remains authoritative for the negative status, including its removal.
-- Do NOT rewrite or delete the source restriction, even after successful appeal.
CREATE OR REPLACE VIEW admission.active_documentary_remediation_restriction AS
 SELECT r.restriction_id,r.organisation_id,r.requirement_id,
        r.capability_key,r.market_code,r.decision_id,r.created_at
 FROM admission.documentary_remediation_restriction r
 WHERE r.status='ACTIVE'
   AND NOT EXISTS (
      SELECT 1 FROM admission.documentary_restriction_release_decision d
      JOIN evidence.result result ON result.result_id=d.verification_result_id
      JOIN evidence.claim claim ON claim.claim_id=result.claim_id
      JOIN evidence.verification_case verification_case ON verification_case.case_id=result.case_id
      JOIN admission.documentary_review_decision originating_decision
        ON originating_decision.decision_id=r.decision_id
      JOIN admission.documentary_review_intent original_review
        ON original_review.review_id=originating_decision.review_id
      JOIN admission.founding_documentary_policy_requirement policy
        ON policy.policy_reference=original_review.policy_reference
       AND policy.requirement_id=r.requirement_id
       AND policy.requirement_authority='PLATFORM_DOCUMENTARY'
       AND policy.effective_from<=clock_timestamp()
       AND policy.effective_to>clock_timestamp()
      JOIN admission.documentary_release_source_policy source_policy
        ON source_policy.policy_reference=original_review.policy_reference
       AND source_policy.requirement_id=r.requirement_id
       AND source_policy.market_code=r.market_code
       AND source_policy.claim_type=claim.document->>'claim_type'
       AND source_policy.market_code=claim.document->>'jurisdiction'
       AND source_policy.effective_from<=clock_timestamp()
       AND source_policy.effective_to>clock_timestamp()
       AND EXISTS (
          SELECT 1 FROM evidence.check source_check
          WHERE source_check.claim_id=result.claim_id
            AND source_check.case_id=result.case_id
            AND source_check.source_id=source_policy.source_id
            AND source_check.outcome IN ('VERIFIED','MATCHED')
            AND EXISTS (
              SELECT 1 FROM jsonb_array_elements_text(result.document->'check_ids') AS cited(check_id)
              WHERE cited.check_id=source_check.check_id
            )
       )
      WHERE d.restriction_id=r.restriction_id AND d.outcome='APPROVE'
        AND result.outcome='VERIFIED' AND claim.status='VERIFIED'
        AND claim.current_result_id=result.result_id
        AND verification_case.status='VERIFIED'
        AND verification_case.subject_type='ORGANISATION'
        AND verification_case.subject_id=r.organisation_id::text
        AND (result.document->>'freshness_until') IS NOT NULL
        AND (result.document->>'freshness_until')::timestamptz>clock_timestamp()
   );

REVOKE ALL ON admission.documentary_release_source_policy,
 admission.documentary_restriction_release_intent,
 admission.documentary_restriction_release_decision FROM PUBLIC;
REVOKE ALL ON admission.active_documentary_remediation_restriction FROM PUBLIC;
