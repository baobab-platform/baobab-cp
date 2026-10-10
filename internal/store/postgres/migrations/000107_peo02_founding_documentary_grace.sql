-- PEO-02 foundation: first-party sponsorship and immutable one-time grace.
-- No automatic INSERT of real organisations, rights, subscriptions or legal
-- persons. Platform documentary grace is NOT statutory/provider evidence.
-- Grace expiry: exactly 24 calendar months from an explicit UTC instant (ADR-BCP-026/A1).
-- PostgreSQL performs month arithmetic in the SESSION time zone, so an unqualified
-- `timestamptz + interval '24 months'` shifts the instant by any DST offset difference.
-- This is the single database definition of the window; month-end days clamp
-- (Jan 31 -> Feb 28/29), matching progressive.CalendarAnniversaryUTC.
CREATE FUNCTION admission.founding_grace_expiry(provisional_approval_at timestamptz)
RETURNS timestamptz LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT ((provisional_approval_at AT TIME ZONE 'UTC') + interval '24 months') AT TIME ZONE 'UTC'
$$;

CREATE TABLE admission.founding_group_sponsorship (
  sponsorship_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sponsor_organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
  operating_organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
  platform_id text NOT NULL,
  status text NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','SUSPENDED','REVOKED','EXPIRED')),
  scope text NOT NULL DEFAULT 'INTERNAL_GROUP_ADMISSION'
    CHECK (scope='INTERNAL_GROUP_ADMISSION'),
  authority_basis_reference text NOT NULL CHECK (length(btrim(authority_basis_reference)) BETWEEN 1 AND 500),
  evidence_references text[] NOT NULL DEFAULT '{}'::text[]
    CHECK (array_position(evidence_references,NULL) IS NULL),
  proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  approved_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  approved_at timestamptz NOT NULL,
  effective_from timestamptz NOT NULL,
  effective_to timestamptz NOT NULL,
  provenance text NOT NULL CHECK (length(btrim(provenance)) BETWEEN 1 AND 500),
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK (proposed_by<>approved_by AND effective_to>effective_from
      AND approved_at<=effective_to AND (revoked_at IS NULL OR revoked_at>=approved_at)),
  CHECK (status<>'REVOKED' OR revoked_at IS NOT NULL),
  -- A sponsor vouches for a different operating Organisation, never for itself.
  CHECK (sponsor_organisation_id<>operating_organisation_id)
);
CREATE INDEX founding_sponsorship_target_idx
  ON admission.founding_group_sponsorship(operating_organisation_id,status,effective_to);

CREATE OR REPLACE FUNCTION admission.founding_sponsorship_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'founding sponsorship history cannot be deleted' USING ERRCODE='check_violation';
  END IF;
  IF TG_OP='UPDATE' THEN
    IF ROW(NEW.sponsorship_id,NEW.sponsor_organisation_id,NEW.operating_organisation_id,
       NEW.platform_id,NEW.scope,NEW.authority_basis_reference,NEW.evidence_references,
       NEW.proposed_by,NEW.approved_by,NEW.approved_at,NEW.effective_from,NEW.effective_to,
       NEW.provenance,NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.sponsorship_id,OLD.sponsor_organisation_id,OLD.operating_organisation_id,
       OLD.platform_id,OLD.scope,OLD.authority_basis_reference,OLD.evidence_references,
       OLD.proposed_by,OLD.approved_by,OLD.approved_at,OLD.effective_from,OLD.effective_to,
       OLD.provenance,OLD.created_at)
       OR OLD.status NOT IN ('ACTIVE','SUSPENDED')
       OR NEW.status NOT IN ('SUSPENDED','REVOKED','EXPIRED')
       OR NEW.status=OLD.status THEN
      RAISE EXCEPTION 'sponsorship identity and approval are immutable; only forward suspension/revocation/expiry allowed'
        USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.status<>'ACTIVE' OR NEW.approved_at>clock_timestamp()
     OR NEW.effective_from>clock_timestamp()
     OR NEW.effective_to<=clock_timestamp()
     OR NOT EXISTS (SELECT 1 FROM identity.principal
         WHERE principal_id=NEW.proposed_by AND actor_type='human')
     OR NOT EXISTS (SELECT 1 FROM identity.principal
         WHERE principal_id=NEW.approved_by AND actor_type='human') THEN
    RAISE EXCEPTION 'sponsorship requires an independent current human grant with bounded scope'
      USING ERRCODE='check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER founding_sponsorship_guard BEFORE INSERT OR UPDATE OR DELETE
 ON admission.founding_group_sponsorship FOR EACH ROW
 EXECUTE FUNCTION admission.founding_sponsorship_guard();

CREATE TABLE admission.founding_documentary_deferral (
  deferral_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organisation_id uuid NOT NULL UNIQUE REFERENCES registry.organisation_profile(canonical_entity_id),
  sponsorship_id uuid NOT NULL REFERENCES admission.founding_group_sponsorship(sponsorship_id),
  admission_decision_id uuid NOT NULL REFERENCES admission.admission_decision(admission_decision_id),
  requirement_ids text[] NOT NULL,
  policy_reference text NOT NULL CHECK (length(btrim(policy_reference)) BETWEEN 1 AND 500),
  approval_reference text NOT NULL CHECK (length(btrim(approval_reference)) BETWEEN 1 AND 500),
  proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  approved_by uuid NOT NULL REFERENCES identity.principal(principal_id),
  approved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  effective_from timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  maximum_duration_months integer NOT NULL DEFAULT 24 CHECK(maximum_duration_months=24),
  status text NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','FULFILLED','REVOKED','EXPIRED')),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK (cardinality(requirement_ids)>0 AND array_position(requirement_ids,NULL) IS NULL),
  CHECK (proposed_by<>approved_by AND expires_at>effective_from)
);
CREATE INDEX founding_documentary_expiry_idx
 ON admission.founding_documentary_deferral(status,expires_at);

CREATE OR REPLACE FUNCTION admission.founding_documentary_deferral_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  src RECORD;
  duplicate_count integer;
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'founding grace record is permanent evidence, not deletable'
      USING ERRCODE='check_violation';
  END IF;
  IF TG_OP='UPDATE' THEN
    IF ROW(NEW.deferral_id,NEW.organisation_id,NEW.sponsorship_id,NEW.admission_decision_id,
      NEW.requirement_ids,NEW.policy_reference,NEW.approval_reference,NEW.proposed_by,NEW.approved_by,
      NEW.approved_at,NEW.effective_from,NEW.expires_at,NEW.maximum_duration_months,NEW.recorded_at)
      IS DISTINCT FROM
      ROW(OLD.deferral_id,OLD.organisation_id,OLD.sponsorship_id,OLD.admission_decision_id,
      OLD.requirement_ids,OLD.policy_reference,OLD.approval_reference,OLD.proposed_by,OLD.approved_by,
      OLD.approved_at,OLD.effective_from,OLD.expires_at,OLD.maximum_duration_months,OLD.recorded_at)
      OR OLD.status<>'ACTIVE' OR NEW.status NOT IN ('FULFILLED','REVOKED','EXPIRED') THEN
      RAISE EXCEPTION 'one-time grace cannot restart, enlarge or be reactivated'
        USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
  END IF;
  SELECT s.operating_organisation_id,s.status,s.scope,s.proposed_by AS sponsor_proposer,
    s.approved_by AS sponsor_approver,s.effective_from AS sponsor_from,s.effective_to AS sponsor_to,
    d.decided_at,d.decided_by,d.decision,a.application_channel,a.applicant_principal_id
  INTO src
  FROM admission.founding_group_sponsorship s
  CROSS JOIN admission.admission_decision d
  JOIN admission.client_application a ON a.client_application_id=d.client_application_id
  WHERE s.sponsorship_id=NEW.sponsorship_id AND d.admission_decision_id=NEW.admission_decision_id
  FOR SHARE OF s,d,a;
  IF NOT FOUND
     OR src.operating_organisation_id<>NEW.organisation_id
     OR src.status<>'ACTIVE' OR src.scope<>'INTERNAL_GROUP_ADMISSION'
     OR src.decision<>'APPROVED' OR src.application_channel<>'INTERNAL_GROUP'
     OR NEW.effective_from IS DISTINCT FROM src.decided_at
     OR NEW.expires_at IS DISTINCT FROM admission.founding_grace_expiry(src.decided_at)
     OR NEW.approved_at<NEW.effective_from OR NEW.approved_at>clock_timestamp()
     OR NEW.effective_from>clock_timestamp() OR NEW.expires_at<=clock_timestamp()
     OR src.sponsor_from>NEW.effective_from OR src.sponsor_to<=NEW.approved_at
     OR NEW.status<>'ACTIVE'
     OR NEW.proposed_by IN (NEW.approved_by,src.sponsor_approver)
     OR NEW.approved_by IN (src.applicant_principal_id,src.decided_by,src.sponsor_proposer,src.sponsor_approver)
     OR NOT EXISTS (SELECT 1 FROM identity.principal WHERE principal_id=NEW.proposed_by AND actor_type='human')
     OR NOT EXISTS (SELECT 1 FROM identity.principal WHERE principal_id=NEW.approved_by AND actor_type='human')
  THEN
    RAISE EXCEPTION 'deferral requires independently reviewed founding sponsorship and original 24-month provisional approval'
      USING ERRCODE='check_violation';
  END IF;
  SELECT count(DISTINCT trim(x)) INTO duplicate_count FROM unnest(NEW.requirement_ids) AS x;
  IF duplicate_count<>cardinality(NEW.requirement_ids) OR
    EXISTS (SELECT 1 FROM unnest(NEW.requirement_ids) x WHERE length(btrim(x))<3) THEN
    RAISE EXCEPTION 'deferral requires distinct named platform document requirements'
      USING ERRCODE='check_violation';
  END IF;
  -- The requirement classification still belongs to the approved policy
  -- evaluator; a DB row cannot establish that a statutory/provider gate is
  -- platform documentary. Consumers must re-evaluate at their own PEP.
  RETURN NEW;
END $$;
CREATE TRIGGER founding_documentary_deferral_guard BEFORE INSERT OR UPDATE OR DELETE
 ON admission.founding_documentary_deferral FOR EACH ROW
 EXECUTE FUNCTION admission.founding_documentary_deferral_guard();

-- The ONLY permissible positive read of grace evidence. Expired sponsorships
-- or deferrals disappear even if a scheduled state transition has not run.
CREATE VIEW admission.active_founding_documentary_deferral AS
SELECT d.deferral_id,d.organisation_id,d.sponsorship_id,d.requirement_ids,
 d.policy_reference,d.effective_from,d.expires_at,d.maximum_duration_months
FROM admission.founding_documentary_deferral d
JOIN admission.founding_group_sponsorship s ON s.sponsorship_id=d.sponsorship_id
WHERE d.status='ACTIVE' AND s.status='ACTIVE'
 AND d.effective_from<=clock_timestamp() AND d.expires_at>clock_timestamp()
 AND s.effective_from<=clock_timestamp() AND s.effective_to>clock_timestamp()
 AND s.operating_organisation_id=d.organisation_id;

REVOKE ALL ON admission.founding_group_sponsorship FROM PUBLIC;
REVOKE ALL ON admission.founding_documentary_deferral FROM PUBLIC;
REVOKE ALL ON admission.active_founding_documentary_deferral FROM PUBLIC;
