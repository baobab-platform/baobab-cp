-- PEO-02R / ADR-BCP-026: durable periodic review checkpoints.
-- These are due-work records, not evidence approval, a grace extension, or
-- permission to restrict an unrelated capability. Historic deferrals and
-- their original 24-calendar-month expiry are immutable.
CREATE OR REPLACE FUNCTION admission.founding_review_milestone_utc(
    original_approval timestamptz, milestone integer)
RETURNS timestamptz LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT CASE WHEN milestone IN (6,12,18,24)
      THEN ((original_approval AT TIME ZONE 'UTC')
             + (milestone * interval '1 month')) AT TIME ZONE 'UTC'
      ELSE NULL END
$$;

CREATE TABLE admission.founding_documentary_review_checkpoint (
    deferral_id uuid NOT NULL REFERENCES admission.founding_documentary_deferral(deferral_id),
    milestone_month integer NOT NULL CHECK (milestone_month IN (6,12,18,24)),
    organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
    scheduled_at timestamptz NOT NULL,
    policy_reference text NOT NULL CHECK (length(btrim(policy_reference)) BETWEEN 1 AND 500),
    requirement_ids text[] NOT NULL CHECK (
      cardinality(requirement_ids)>0 AND array_position(requirement_ids,NULL) IS NULL),
    status text NOT NULL DEFAULT 'REASSESSMENT_REQUIRED'
      CHECK (status='REASSESSMENT_REQUIRED'),
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (deferral_id,milestone_month)
);
CREATE INDEX founding_documentary_review_due_idx
 ON admission.founding_documentary_review_checkpoint(scheduled_at,deferral_id,milestone_month);

CREATE OR REPLACE FUNCTION admission.founding_documentary_review_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source RECORD;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'founding documentary review history is append only'
      USING ERRCODE='check_violation';
  END IF;
  SELECT organisation_id,effective_from,policy_reference,requirement_ids
    INTO source FROM admission.founding_documentary_deferral
    WHERE deferral_id=NEW.deferral_id;
  IF NOT FOUND OR
     NEW.organisation_id IS DISTINCT FROM source.organisation_id OR
     NEW.policy_reference IS DISTINCT FROM source.policy_reference OR
     NEW.requirement_ids IS DISTINCT FROM source.requirement_ids OR
     NEW.scheduled_at IS DISTINCT FROM
       admission.founding_review_milestone_utc(source.effective_from,NEW.milestone_month) OR
     NEW.scheduled_at>clock_timestamp() OR
     NEW.status<>'REASSESSMENT_REQUIRED'
  THEN
    RAISE EXCEPTION 'review checkpoint requires original deferral, immutable policy and due UTC milestone'
      USING ERRCODE='check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER founding_documentary_review_guard
 BEFORE INSERT OR UPDATE OR DELETE ON admission.founding_documentary_review_checkpoint
 FOR EACH ROW EXECUTE FUNCTION admission.founding_documentary_review_guard();

-- Governance has not approved a universal deferral override, review
-- completion or a restriction policy. Reviewers must record those decisions
-- through subsequent explicit authorised request/assessment contracts.
REVOKE ALL ON admission.founding_documentary_review_checkpoint FROM PUBLIC;
