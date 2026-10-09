-- LA-04C / ADR-BCP-027: governed proposal and independent checker decision.
-- All mandates remain PENDING. Migration 000104's activation gate is retained.
-- LA-04D alone may introduce a reviewed activation path and lifecycle changes.
CREATE TABLE registry.operating_legal_actor_mandate_decision (
    decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mandate_id uuid NOT NULL UNIQUE REFERENCES registry.operating_legal_actor_mandate(mandate_id),
    decision text NOT NULL CHECK (decision IN ('APPROVE','REJECT')),
    decision_basis_reference text NOT NULL CHECK (length(btrim(decision_basis_reference)) BETWEEN 1 AND 500),
    evidence_references text[] NOT NULL CHECK (
        cardinality(evidence_references) >= 1 AND array_position(evidence_references, NULL) IS NULL
    ),
    legal_actor_verification_reference text,
    decided_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    decided_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (decision <> 'APPROVE' OR
        (legal_actor_verification_reference IS NOT NULL AND length(btrim(legal_actor_verification_reference)) > 0))
);
CREATE INDEX operating_legal_actor_mandate_decision_maker_idx
    ON registry.operating_legal_actor_mandate_decision(decided_by, decided_at);

-- The pre-decision requirements live in the database as well as the Go
-- service. Direct SQL must not be a bypass for maker/checker separation,
-- legal verification, evidence-backed actor or PRIMARY Organisation integrity.
CREATE FUNCTION registry.operating_legal_actor_mandate_decision_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    m registry.operating_legal_actor_mandate%ROWTYPE;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'legal-actor mandate decisions are immutable'
            USING ERRCODE='check_violation';
    END IF;
    SELECT * INTO m FROM registry.operating_legal_actor_mandate
      WHERE mandate_id=NEW.mandate_id FOR UPDATE;
    IF NOT FOUND OR m.status <> 'PENDING' OR m.created_by=NEW.decided_by
       OR NOT EXISTS (SELECT 1 FROM identity.principal p
                      WHERE p.principal_id=NEW.decided_by AND p.actor_type='human')
       OR NOT EXISTS (SELECT 1 FROM identity.principal p
                      WHERE p.principal_id=m.created_by AND p.actor_type='human') THEN
        RAISE EXCEPTION 'mandate decision requires independent human checker and PENDING intent'
            USING ERRCODE='check_violation';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM registry.tenant_organisation_mapping om
        WHERE om.tenant_id=m.tenant_id
          AND om.organisation_id=m.operating_organisation_id
          AND om.mapping_role='PRIMARY_ORGANISATION' AND om.status='ACTIVE'
          AND om.effective_from<=clock_timestamp()
          AND (om.effective_to IS NULL OR om.effective_to>clock_timestamp())
    ) THEN
        RAISE EXCEPTION 'mandate decision requires live PRIMARY Organisation'
            USING ERRCODE='check_violation';
    END IF;
    IF NEW.decision='APPROVE' AND NOT EXISTS (
        SELECT 1 FROM registry.legal_entity_profile lp
        WHERE lp.legal_entity_id=m.responsible_legal_entity_id
          AND lp.verification_state='VERIFIED'
          AND lp.legal_status='ACTIVE'
          AND lp.source_authority NOT IN ('shared-governance','control-plane-registration')
          AND lp.verified_at IS NOT NULL AND lp.verified_at<=clock_timestamp()
          AND jsonb_array_length(lp.evidence_references)>0
          AND lp.effective_from<=clock_timestamp()
          AND (lp.effective_to IS NULL OR lp.effective_to>clock_timestamp())
          AND EXISTS (
              SELECT 1 FROM jsonb_array_elements_text(lp.evidence_references) er
              WHERE er.value=NEW.legal_actor_verification_reference
          )
          AND NOT EXISTS (
              SELECT 1 FROM registry.first_party_organisation_identity fp
              WHERE fp.organisation_id=lp.organisation_id
                AND fp.identity_class='OPERATING_BUSINESS'
                AND fp.incorporation_claim='NOT_INCORPORATED'
          )
    ) THEN
        RAISE EXCEPTION 'legal actor is not independently verified for mandate approval'
            USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER operating_legal_actor_mandate_decision_guard
    BEFORE INSERT OR UPDATE OR DELETE ON registry.operating_legal_actor_mandate_decision
    FOR EACH ROW EXECUTE FUNCTION registry.operating_legal_actor_mandate_decision_guard();

-- Exact-replay ledger. Hash includes the command, path identity and maker or
-- checker; same key under different intent is a conflict, not permission.
CREATE TABLE registry.operating_legal_actor_mandate_command (
    command_kind text NOT NULL CHECK (command_kind IN ('PROPOSE','DECIDE')),
    idempotency_key varchar(128) NOT NULL CHECK (length(idempotency_key)>=16),
    request_digest char(64) NOT NULL CHECK (request_digest ~ '^[a-f0-9]{64}$'),
    actor_id uuid NOT NULL REFERENCES identity.principal(principal_id),
    mandate_id uuid NOT NULL REFERENCES registry.operating_legal_actor_mandate(mandate_id),
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (command_kind,idempotency_key)
);
CREATE FUNCTION registry.operating_legal_actor_mandate_command_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'legal-actor mandate idempotency evidence is immutable'
        USING ERRCODE='check_violation';
END;
$$;
CREATE TRIGGER operating_legal_actor_mandate_command_immutable
    BEFORE UPDATE OR DELETE ON registry.operating_legal_actor_mandate_command
    FOR EACH ROW EXECUTE FUNCTION registry.operating_legal_actor_mandate_command_immutable();

REVOKE ALL ON registry.operating_legal_actor_mandate_decision FROM PUBLIC;
REVOKE ALL ON registry.operating_legal_actor_mandate_command FROM PUBLIC;
