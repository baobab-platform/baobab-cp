-- PEO-02S / ADR-BCP-026: governed review decisions, scoped negative
-- remediation, and appeals. All states fail closed; none certify evidence.
-- Forward-only migration, independent of the immutable month-6/12/18/24
-- source deferral and scheduler checkpoints in migration 000114.

CREATE TABLE admission.documentary_review_intent (
  review_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  deferral_id uuid NOT NULL,
  milestone_month integer NOT NULL,
  organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
  proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
  request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
  assessment jsonb NOT NULL,
  policy_reference text NOT NULL CHECK(length(btrim(policy_reference)) BETWEEN 2 AND 500),
  state text NOT NULL DEFAULT 'PENDING' CHECK(state IN ('PENDING','DECIDED')),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY (deferral_id,milestone_month) REFERENCES
    admission.founding_documentary_review_checkpoint(deferral_id,milestone_month),
  UNIQUE (deferral_id,milestone_month),
  UNIQUE (proposed_by,idempotency_key)
);
CREATE TABLE admission.documentary_review_decision (
  decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  review_id uuid NOT NULL UNIQUE REFERENCES admission.documentary_review_intent(review_id),
  decided_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  outcome text NOT NULL CHECK (outcome IN ('REMEDIATION_REQUIRED','ASSESSMENT_INCOMPLETE')),
  decision_reference text NOT NULL CHECK(length(btrim(decision_reference)) BETWEEN 3 AND 500),
  reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 10 AND 2000),
  decided_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE admission.documentary_remediation_restriction (
  restriction_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  decision_id uuid NOT NULL REFERENCES admission.documentary_review_decision(decision_id),
  organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
  requirement_id text NOT NULL CHECK(length(btrim(requirement_id)) BETWEEN 3 AND 200),
  capability_key text NOT NULL CHECK(length(btrim(capability_key)) BETWEEN 3 AND 200),
  market_code text NOT NULL CHECK(market_code ~ '^[A-Z]{2}$'),
  status text NOT NULL DEFAULT 'ACTIVE' CHECK(status='ACTIVE'),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(decision_id,requirement_id,capability_key,market_code)
);
CREATE INDEX documentary_remediation_scope_idx
 ON admission.documentary_remediation_restriction
 (organisation_id,capability_key,market_code,status);

CREATE TABLE admission.documentary_review_appeal (
  appeal_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  decision_id uuid NOT NULL UNIQUE REFERENCES admission.documentary_review_decision(decision_id),
  raised_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
  reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 10 AND 2000),
  evidence_reference text NOT NULL CHECK(length(btrim(evidence_reference)) BETWEEN 3 AND 500),
  state text NOT NULL DEFAULT 'FILED' CHECK(state IN ('FILED','DECIDED')),
  raised_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(raised_by,idempotency_key)
);
CREATE TABLE admission.documentary_appeal_decision (
  decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  appeal_id uuid NOT NULL UNIQUE REFERENCES admission.documentary_review_appeal(appeal_id),
  decided_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  outcome text NOT NULL CHECK(outcome IN ('UPHOLD','RETURN_FOR_REASSESSMENT')),
  reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 10 AND 2000),
  evidence_reference text NOT NULL CHECK(length(btrim(evidence_reference)) BETWEEN 3 AND 500),
  decided_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- No DELETE, reactivation, retroactive revision or write-through from review
-- history. A future independent policy-bound release mechanism must be
-- separately authorised and will not rewrite this historical evidence.
CREATE OR REPLACE FUNCTION admission.documentary_review_protect()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR (TG_OP='UPDATE' AND (
       (TG_TABLE_NAME='documentary_review_intent' AND
        (ROW(NEW.review_id,NEW.deferral_id,NEW.milestone_month,NEW.organisation_id,
            NEW.proposed_by,NEW.idempotency_key,NEW.request_digest,NEW.assessment,
            NEW.policy_reference,NEW.created_at)
         IS DISTINCT FROM
         ROW(OLD.review_id,OLD.deferral_id,OLD.milestone_month,OLD.organisation_id,
             OLD.proposed_by,OLD.idempotency_key,OLD.request_digest,OLD.assessment,
             OLD.policy_reference,OLD.created_at)
         OR OLD.state<>'PENDING' OR NEW.state<>'DECIDED'))
       OR (TG_TABLE_NAME='documentary_review_appeal' AND
        (ROW(NEW.appeal_id,NEW.decision_id,NEW.raised_by,NEW.idempotency_key,
             NEW.reason,NEW.evidence_reference,NEW.raised_at)
         IS DISTINCT FROM
         ROW(OLD.appeal_id,OLD.decision_id,OLD.raised_by,OLD.idempotency_key,
             OLD.reason,OLD.evidence_reference,OLD.raised_at)
         OR OLD.state<>'FILED' OR NEW.state<>'DECIDED'))
       OR TG_TABLE_NAME NOT IN ('documentary_review_intent','documentary_review_appeal')
      ))) THEN
      RAISE EXCEPTION 'PEO-02 review history is append-only and state transitions are forward only'
        USING ERRCODE='check_violation';
  END IF;
  RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;

CREATE TRIGGER documentary_review_intent_guard BEFORE UPDATE OR DELETE
 ON admission.documentary_review_intent FOR EACH ROW
 EXECUTE FUNCTION admission.documentary_review_protect();
CREATE TRIGGER documentary_review_decision_guard BEFORE UPDATE OR DELETE
 ON admission.documentary_review_decision FOR EACH ROW
 EXECUTE FUNCTION admission.documentary_review_protect();
CREATE TRIGGER documentary_remediation_guard BEFORE UPDATE OR DELETE
 ON admission.documentary_remediation_restriction FOR EACH ROW
 EXECUTE FUNCTION admission.documentary_review_protect();
CREATE TRIGGER documentary_appeal_guard BEFORE UPDATE OR DELETE
 ON admission.documentary_review_appeal FOR EACH ROW
 EXECUTE FUNCTION admission.documentary_review_protect();
CREATE TRIGGER documentary_appeal_decision_guard BEFORE UPDATE OR DELETE
 ON admission.documentary_appeal_decision FOR EACH ROW
 EXECUTE FUNCTION admission.documentary_review_protect();

REVOKE ALL ON admission.documentary_review_intent,
 admission.documentary_review_decision,
 admission.documentary_remediation_restriction,
 admission.documentary_review_appeal,
 admission.documentary_appeal_decision FROM PUBLIC;

-- Negative-only projection: downstream PEP must query current scope
-- independently, not trust admission events as an authority grant.
CREATE VIEW admission.active_documentary_remediation_restriction AS
 SELECT restriction_id,organisation_id,requirement_id,capability_key,
        market_code,decision_id,created_at
 FROM admission.documentary_remediation_restriction WHERE status='ACTIVE';
REVOKE ALL ON admission.active_documentary_remediation_restriction FROM PUBLIC;
