-- ADR-BCP-027 LA-04D: guarded lifecycle replacing the blanket LA-04A activation ban.
-- Every state change requires a durable human transition record in the SAME
-- transaction. No UPDATE of actor/scope/window, no resurrection of suspension
-- or revocation, and no inference from group relationships.
CREATE TABLE registry.operating_legal_actor_mandate_transition (
    transition_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mandate_id uuid NOT NULL REFERENCES registry.operating_legal_actor_mandate(mandate_id),
    action text NOT NULL CHECK (action IN ('ACTIVATE','SUSPEND','REVOKE','EXPIRE')),
    from_status text NOT NULL,
    to_status text NOT NULL,
    authority_basis_reference text NOT NULL CHECK (length(btrim(authority_basis_reference)) BETWEEN 1 AND 500),
    evidence_references text[] NOT NULL CHECK (cardinality(evidence_references)>0 AND array_position(evidence_references,NULL) IS NULL),
    performed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    performed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (mandate_id,action)
);
CREATE INDEX operating_legal_actor_mandate_transition_lookup
    ON registry.operating_legal_actor_mandate_transition(mandate_id,performed_at);
CREATE FUNCTION registry.operating_legal_actor_mandate_transition_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'legal actor mandate transitions are immutable' USING ERRCODE='check_violation';
END $$;
CREATE TRIGGER operating_legal_actor_mandate_transition_immutable
BEFORE UPDATE OR DELETE ON registry.operating_legal_actor_mandate_transition
FOR EACH ROW EXECUTE FUNCTION registry.operating_legal_actor_mandate_transition_immutable();

-- Extend the LA-04C replay ledger without permitting mutation of historic rows.
ALTER TABLE registry.operating_legal_actor_mandate_command
  DROP CONSTRAINT operating_legal_actor_mandate_command_command_kind_check;
ALTER TABLE registry.operating_legal_actor_mandate_command
  ADD CONSTRAINT operating_legal_actor_mandate_command_command_kind_check
  CHECK (command_kind IN ('PROPOSE','DECIDE','LIFECYCLE'));

-- Adapt original append-only guard only for fields populated as a governed
-- consequence of the independent decision at first activation.
CREATE OR REPLACE FUNCTION registry.operating_legal_actor_mandate_history_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'operating legal-actor mandates cannot be deleted' USING ERRCODE='check_violation';
  END IF;
  IF ROW(NEW.tenant_id,NEW.operating_organisation_id,
    NEW.responsible_legal_entity_id,NEW.roles,NEW.activity_scope,
    NEW.market_scope,NEW.capability_scope,NEW.authority_basis_reference,
    NEW.evidence_references,NEW.effective_from,NEW.effective_to,
    NEW.created_by,NEW.created_at,NEW.supersedes_mandate_id,NEW.provenance)
    IS DISTINCT FROM
    ROW(OLD.tenant_id,OLD.operating_organisation_id,
    OLD.responsible_legal_entity_id,OLD.roles,OLD.activity_scope,
    OLD.market_scope,OLD.capability_scope,OLD.authority_basis_reference,
    OLD.evidence_references,OLD.effective_from,OLD.effective_to,
    OLD.created_by,OLD.created_at,OLD.supersedes_mandate_id,OLD.provenance) THEN
    RAISE EXCEPTION 'mandate identity, scope and window are immutable' USING ERRCODE='check_violation';
  END IF;
  RETURN NEW;
END $$;

-- The old blanket gate is removed ONLY as this new conditional DB guard is
-- installed inside the same transactional migration.
ALTER TABLE registry.operating_legal_actor_mandate
  DROP CONSTRAINT operating_legal_actor_mandate_activation_gate;
CREATE FUNCTION registry.operating_legal_actor_mandate_lifecycle_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  transition_record registry.operating_legal_actor_mandate_transition%ROWTYPE;
  check_record registry.operating_legal_actor_mandate_decision%ROWTYPE;
  now_utc timestamptz := clock_timestamp();
BEGIN
  IF TG_OP='INSERT' THEN
    IF NEW.status <> 'PENDING' THEN
      RAISE EXCEPTION 'legal mandate insert must be PENDING' USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.status=OLD.status OR NEW.status NOT IN ('ACTIVE','SUSPENDED','REVOKED','EXPIRED') THEN
    RAISE EXCEPTION 'mandate transition requires a distinct governed state'
       USING ERRCODE='check_violation';
  END IF;
  -- Serialize ALL scope-overlap checks, including concurrently activated
  -- mandates with different IDs but the same tenant/Organisation.
  PERFORM pg_advisory_xact_lock(hashtextextended(
    OLD.tenant_id || '/' || OLD.operating_organisation_id::text, 0));
  SELECT * INTO transition_record
    FROM registry.operating_legal_actor_mandate_transition t
   WHERE t.mandate_id=OLD.mandate_id
     AND t.from_status=OLD.status AND t.to_status=NEW.status
     AND t.action=CASE NEW.status
         WHEN 'ACTIVE' THEN 'ACTIVATE'
         WHEN 'SUSPENDED' THEN 'SUSPEND'
         WHEN 'REVOKED' THEN 'REVOKE'
         WHEN 'EXPIRED' THEN 'EXPIRE' END
   ORDER BY performed_at DESC LIMIT 1;
  IF NOT FOUND OR NOT EXISTS (
    SELECT 1 FROM identity.principal p
    WHERE p.principal_id=transition_record.performed_by
      AND p.actor_type='human'
  ) THEN
    RAISE EXCEPTION 'missing authenticated human lifecycle transition'
      USING ERRCODE='check_violation';
  END IF;
  IF NEW.status='ACTIVE' THEN
    IF OLD.status<>'PENDING' OR now_utc<OLD.effective_from
        OR (OLD.effective_to IS NOT NULL AND now_utc>=OLD.effective_to) THEN
      RAISE EXCEPTION 'approval must be currently effective and cannot resurrect a mandate'
        USING ERRCODE='check_violation';
    END IF;
    SELECT * INTO check_record FROM registry.operating_legal_actor_mandate_decision
      WHERE mandate_id=OLD.mandate_id AND decision='APPROVE';
    IF NOT FOUND OR check_record.decided_at>now_utc
       OR check_record.decided_by=OLD.created_by
       OR transition_record.performed_by IN (OLD.created_by,check_record.decided_by)
       OR NEW.approved_by IS DISTINCT FROM check_record.decided_by
       OR NEW.approved_at IS DISTINCT FROM check_record.decided_at
       OR NEW.legal_actor_verification_reference IS DISTINCT FROM check_record.legal_actor_verification_reference
       OR NEW.revoked_at IS DISTINCT FROM OLD.revoked_at THEN
      RAISE EXCEPTION 'requires independent maker checker and distinct activation operator'
        USING ERRCODE='check_violation';
    END IF;
    IF NOT EXISTS (
      SELECT 1 FROM registry.tenant_organisation_mapping om
      WHERE om.tenant_id=OLD.tenant_id AND om.organisation_id=OLD.operating_organisation_id
        AND om.mapping_role='PRIMARY_ORGANISATION' AND om.status='ACTIVE'
        AND om.effective_from<=now_utc
        AND (om.effective_to IS NULL OR om.effective_to>now_utc)
    ) OR NOT EXISTS (
      SELECT 1 FROM registry.legal_entity_profile lp
      WHERE lp.legal_entity_id=OLD.responsible_legal_entity_id
        AND lp.verification_state='VERIFIED' AND lp.legal_status='ACTIVE'
        AND lp.source_authority NOT IN ('shared-governance','control-plane-registration')
        AND lp.verified_at IS NOT NULL AND lp.verified_at<=now_utc
        AND jsonb_array_length(lp.evidence_references)>0
        AND lp.effective_from<=now_utc
        AND (lp.effective_to IS NULL OR lp.effective_to>now_utc)
        AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(lp.evidence_references) e
                    WHERE e.value=check_record.legal_actor_verification_reference)
        AND NOT EXISTS (SELECT 1 FROM registry.first_party_organisation_identity fp
          WHERE fp.organisation_id=lp.organisation_id
            AND fp.identity_class='OPERATING_BUSINESS'
            AND fp.incorporation_claim='NOT_INCORPORATED')
    ) THEN
      RAISE EXCEPTION 'PRIMARY Organisation or independent legal actor verification not current'
        USING ERRCODE='check_violation';
    END IF;
    -- Supersession is a reviewed identity transition, never an implicit
    -- change of responsibility or a cross-tenant reference. The nominated
    -- predecessor must have the SAME tenant and operating Organisation and
    -- already be terminal/non-authorising before a successor activates.
    IF OLD.supersedes_mandate_id IS NOT NULL AND NOT EXISTS (
      SELECT 1 FROM registry.operating_legal_actor_mandate predecessor
      WHERE predecessor.mandate_id=OLD.supersedes_mandate_id
        AND predecessor.tenant_id=OLD.tenant_id
        AND predecessor.operating_organisation_id=OLD.operating_organisation_id
        AND predecessor.status IN ('SUSPENDED','REVOKED','EXPIRED')
    ) THEN
      RAISE EXCEPTION 'supersession requires same-tenant terminal predecessor'
        USING ERRCODE='check_violation';
    END IF;
    -- Strictly reject overlapping roles x activity x markets x time x
    -- capability (empty capability scope is a wildcard). Do not
    -- automatically revoke or mutate a superseded prior mandate.
    IF EXISTS (
      SELECT 1 FROM registry.operating_legal_actor_mandate active_m
      WHERE active_m.mandate_id<>OLD.mandate_id
        AND active_m.tenant_id=OLD.tenant_id
        AND active_m.operating_organisation_id=OLD.operating_organisation_id
        AND active_m.status='ACTIVE'
        AND active_m.roles && OLD.roles
        AND active_m.activity_scope && OLD.activity_scope
        AND active_m.market_scope && OLD.market_scope
        AND (cardinality(active_m.capability_scope)=0
          OR cardinality(OLD.capability_scope)=0
          OR active_m.capability_scope && OLD.capability_scope)
        AND tstzrange(active_m.effective_from,active_m.effective_to,'[)')
         && tstzrange(OLD.effective_from,OLD.effective_to,'[)')
    ) THEN
      RAISE EXCEPTION 'ambiguous overlapping active legal actor scope'
        USING ERRCODE='check_violation';
    END IF;
  ELSIF NEW.status='SUSPENDED' THEN
    IF OLD.status<>'ACTIVE'
       OR ROW(NEW.approved_by,NEW.approved_at,NEW.legal_actor_verification_reference,NEW.revoked_at)
         IS DISTINCT FROM ROW(OLD.approved_by,OLD.approved_at,OLD.legal_actor_verification_reference,OLD.revoked_at)
    THEN
      RAISE EXCEPTION 'only ACTIVE may be suspended without mutating authority evidence'
        USING ERRCODE='check_violation';
    END IF;
  ELSIF NEW.status='REVOKED' THEN
    IF OLD.status NOT IN ('PENDING','ACTIVE','SUSPENDED')
       OR NEW.revoked_at IS NULL OR NEW.revoked_at<transition_record.performed_at-interval '1 minute'
       OR ROW(NEW.approved_by,NEW.approved_at,NEW.legal_actor_verification_reference)
         IS DISTINCT FROM ROW(OLD.approved_by,OLD.approved_at,OLD.legal_actor_verification_reference)
    THEN
      RAISE EXCEPTION 'invalid terminal mandate revocation' USING ERRCODE='check_violation';
    END IF;
  ELSIF NEW.status='EXPIRED' THEN
    IF OLD.status NOT IN ('PENDING','ACTIVE','SUSPENDED')
       OR OLD.effective_to IS NULL OR OLD.effective_to>now_utc
       OR ROW(NEW.approved_by,NEW.approved_at,NEW.legal_actor_verification_reference,NEW.revoked_at)
         IS DISTINCT FROM ROW(OLD.approved_by,OLD.approved_at,OLD.legal_actor_verification_reference,OLD.revoked_at)
    THEN
      RAISE EXCEPTION 'cannot mark mandate expired before its end' USING ERRCODE='check_violation';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER operating_legal_actor_mandate_lifecycle_guard
BEFORE INSERT OR UPDATE ON registry.operating_legal_actor_mandate
FOR EACH ROW EXECUTE FUNCTION registry.operating_legal_actor_mandate_lifecycle_guard();

REVOKE ALL ON registry.operating_legal_actor_mandate_transition FROM PUBLIC;
