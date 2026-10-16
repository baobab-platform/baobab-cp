-- P0-04B Trade-0 (ADR-SHARED-039): CP is the system of record for reusable,
-- versioned Seller Operating Bindings. Trade attests its native configuration
-- (opaque reference + SHA-256 digest); a human maker proposes, a different
-- checker approves and a third operator activates. CP revalidates at
-- activation. Every status change needs an immutable human transition row in
-- the SAME transaction; scope is immutable; nothing is ever deleted. Order
-- authority decisions are immutable history. None of this grants authority by
-- itself: every consequential Trade operation still needs a current decision.

-- 1. Trade native-configuration attestations: evidence only.
CREATE TABLE registry.trade_native_configuration_attestation (
    attestation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id text NOT NULL CHECK (length(btrim(client_id)) BETWEEN 1 AND 200),
    context_id uuid NOT NULL,
    tenant_id text NOT NULL,
    operating_organisation_id uuid NOT NULL,
    market char(2) NOT NULL CHECK (market ~ '^[A-Z]{2}$'),
    engine_instance_id text NOT NULL CHECK (engine_instance_id ~ '^ei_[a-z0-9]+$'),
    native_configuration_reference text NOT NULL CHECK (
        length(native_configuration_reference) BETWEEN 1 AND 128
        AND native_configuration_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
    native_configuration_digest char(64) NOT NULL CHECK (native_configuration_digest ~ '^[0-9a-f]{64}$'),
    currency_scope text[] NOT NULL CHECK (
        cardinality(currency_scope) > 0 AND array_position(currency_scope, NULL) IS NULL),
    attested_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    correlation_id text,
    CHECK (attested_at <= recorded_at + interval '5 seconds')
);
CREATE INDEX trade_native_configuration_attestation_scope_idx
    ON registry.trade_native_configuration_attestation (tenant_id, operating_organisation_id, engine_instance_id);

CREATE FUNCTION registry.trade_seller_binding_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is immutable history', TG_TABLE_NAME USING ERRCODE = 'check_violation';
END $$;

CREATE TRIGGER trade_native_configuration_attestation_immutable
    BEFORE UPDATE OR DELETE ON registry.trade_native_configuration_attestation
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();

-- 2. Versioned bindings. (binding_id, binding_version) is the governed identity.
CREATE TABLE registry.seller_operating_binding (
    binding_id uuid NOT NULL,
    binding_version integer NOT NULL CHECK (binding_version >= 1),
    status text NOT NULL CHECK (status IN ('PENDING_APPROVAL','APPROVED','ACTIVE','SUSPENDED',
                                            'REVOKED','REJECTED','SUPERSEDED','EXPIRED')),
    tenant_id text NOT NULL,
    operating_organisation_id uuid NOT NULL,
    responsible_legal_entity_id text NOT NULL,
    market char(2) NOT NULL CHECK (market ~ '^[A-Z]{2}$'),
    currency_scope text[] NOT NULL CHECK (
        cardinality(currency_scope) > 0 AND array_position(currency_scope, NULL) IS NULL),
    trade_engine_instance_id text NOT NULL CHECK (trade_engine_instance_id ~ '^ei_[a-z0-9]+$'),
    native_configuration_reference text NOT NULL,
    native_configuration_digest char(64) NOT NULL CHECK (native_configuration_digest ~ '^[0-9a-f]{64}$'),
    attestation_id uuid NOT NULL REFERENCES registry.trade_native_configuration_attestation(attestation_id),
    effective_from timestamptz NOT NULL,
    effective_to timestamptz,
    supersedes_binding_version integer,
    proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    proposal_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    approved_by uuid REFERENCES identity.principal(principal_id),
    approval_id uuid UNIQUE,
    activated_by uuid REFERENCES identity.principal(principal_id),
    activation_id uuid UNIQUE,
    evidence_references text[] NOT NULL CHECK (
        cardinality(evidence_references) > 0 AND array_position(evidence_references, NULL) IS NULL),
    revision integer NOT NULL DEFAULT 0 CHECK (revision >= 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (binding_id, binding_version),
    CHECK (effective_to IS NULL OR effective_to > effective_from),
    CHECK (supersedes_binding_version IS NULL OR supersedes_binding_version = binding_version - 1),
    CHECK (status NOT IN ('APPROVED','ACTIVE','SUSPENDED') OR (approved_by IS NOT NULL AND approval_id IS NOT NULL)),
    CHECK (status NOT IN ('ACTIVE','SUSPENDED') OR (activated_by IS NOT NULL AND activation_id IS NOT NULL)),
    -- Maker, checker and operator are three distinct principals.
    CHECK (approved_by IS NULL OR approved_by <> proposed_by),
    CHECK (activated_by IS NULL OR (activated_by <> proposed_by AND activated_by <> approved_by))
);
-- At most one in-force (active or suspended) version per binding, and at most
-- one approved successor waiting to supersede it.
CREATE UNIQUE INDEX seller_operating_binding_one_in_force_version
    ON registry.seller_operating_binding (binding_id)
    WHERE status IN ('ACTIVE','SUSPENDED');
CREATE UNIQUE INDEX seller_operating_binding_one_approved_version
    ON registry.seller_operating_binding (binding_id)
    WHERE status = 'APPROVED';
CREATE INDEX seller_operating_binding_scope_idx
    ON registry.seller_operating_binding (tenant_id, operating_organisation_id, trade_engine_instance_id, status);

CREATE TABLE registry.seller_operating_binding_transition (
    transition_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    binding_id uuid NOT NULL,
    binding_version integer NOT NULL,
    action text NOT NULL CHECK (action IN ('APPROVE','REJECT','ACTIVATE','SUSPEND','REVOKE','SUPERSEDE')),
    from_status text NOT NULL,
    to_status text NOT NULL,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 3 AND 1000),
    evidence_references text[] NOT NULL DEFAULT '{}' CHECK (array_position(evidence_references, NULL) IS NULL),
    performed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    performed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (binding_id, binding_version) REFERENCES registry.seller_operating_binding(binding_id, binding_version)
);
CREATE INDEX seller_operating_binding_transition_lookup
    ON registry.seller_operating_binding_transition (binding_id, binding_version, performed_at);
CREATE TRIGGER seller_operating_binding_transition_immutable
    BEFORE UPDATE OR DELETE ON registry.seller_operating_binding_transition
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();

-- 3. The lifecycle guard: scope immutable, every change backed by a matching
-- human transition row in this transaction, separation of duties, and CP
-- revalidation at activation. Terminal states never resurrect.
CREATE FUNCTION registry.seller_operating_binding_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    t registry.seller_operating_binding_transition%ROWTYPE;
    a registry.trade_native_configuration_attestation%ROWTYPE;
    now_utc timestamptz := clock_timestamp();
    expected_action text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'seller operating bindings cannot be deleted' USING ERRCODE = 'check_violation';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('seller-operating-binding/' || NEW.binding_id::text, 0));
    IF TG_OP = 'INSERT' THEN
        SELECT * INTO a FROM registry.trade_native_configuration_attestation WHERE attestation_id = NEW.attestation_id;
        IF NEW.status <> 'PENDING_APPROVAL' OR NEW.approved_by IS NOT NULL OR NEW.activated_by IS NOT NULL
           OR NEW.revision <> 0 OR NOT FOUND
           OR ROW(a.tenant_id, a.operating_organisation_id, a.market, a.engine_instance_id,
                  a.native_configuration_reference, a.native_configuration_digest, a.currency_scope)
              IS DISTINCT FROM
              ROW(NEW.tenant_id, NEW.operating_organisation_id, NEW.market, NEW.trade_engine_instance_id,
                  NEW.native_configuration_reference, NEW.native_configuration_digest, NEW.currency_scope)
           OR NOT EXISTS (SELECT 1 FROM identity.principal p WHERE p.principal_id = NEW.proposed_by AND p.actor_type = 'human') THEN
            RAISE EXCEPTION 'a binding proposal must be PENDING_APPROVAL, made by a human and match its attestation exactly'
                USING ERRCODE = 'check_violation';
        END IF;
        -- Versions are consecutive, keep the binding's identity, and supersede only a live predecessor.
        IF NEW.binding_version = 1 THEN
            IF NEW.supersedes_binding_version IS NOT NULL
               OR EXISTS (SELECT 1 FROM registry.seller_operating_binding b WHERE b.binding_id = NEW.binding_id) THEN
                RAISE EXCEPTION 'binding version 1 must be a new binding' USING ERRCODE = 'check_violation';
            END IF;
        ELSIF NEW.supersedes_binding_version IS NULL OR NOT EXISTS (
            SELECT 1 FROM registry.seller_operating_binding b
             WHERE b.binding_id = NEW.binding_id AND b.binding_version = NEW.binding_version - 1
               AND b.status IN ('ACTIVE','SUSPENDED')
               AND b.tenant_id = NEW.tenant_id AND b.operating_organisation_id = NEW.operating_organisation_id
               AND b.responsible_legal_entity_id = NEW.responsible_legal_entity_id
               AND b.trade_engine_instance_id = NEW.trade_engine_instance_id)
           OR EXISTS (SELECT 1 FROM registry.seller_operating_binding b
                       WHERE b.binding_id = NEW.binding_id AND b.binding_version >= NEW.binding_version) THEN
            RAISE EXCEPTION 'a new binding version must supersede the live previous version of the same seller'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END IF;
    -- UPDATE: identity, scope, proposal and earlier provenance are immutable.
    IF ROW(NEW.binding_id, NEW.binding_version, NEW.tenant_id, NEW.operating_organisation_id,
           NEW.responsible_legal_entity_id, NEW.market, NEW.currency_scope, NEW.trade_engine_instance_id,
           NEW.native_configuration_reference, NEW.native_configuration_digest, NEW.attestation_id,
           NEW.effective_from, NEW.effective_to, NEW.supersedes_binding_version, NEW.proposed_by,
           NEW.proposal_id, NEW.evidence_references, NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.binding_id, OLD.binding_version, OLD.tenant_id, OLD.operating_organisation_id,
           OLD.responsible_legal_entity_id, OLD.market, OLD.currency_scope, OLD.trade_engine_instance_id,
           OLD.native_configuration_reference, OLD.native_configuration_digest, OLD.attestation_id,
           OLD.effective_from, OLD.effective_to, OLD.supersedes_binding_version, OLD.proposed_by,
           OLD.proposal_id, OLD.evidence_references, OLD.created_at)
       OR (OLD.approved_by IS NOT NULL AND ROW(NEW.approved_by, NEW.approval_id) IS DISTINCT FROM ROW(OLD.approved_by, OLD.approval_id))
       OR NEW.revision <> OLD.revision + 1 THEN
        RAISE EXCEPTION 'binding identity, scope and provenance are immutable' USING ERRCODE = 'check_violation';
    END IF;
    expected_action := CASE
        WHEN OLD.status = 'PENDING_APPROVAL' AND NEW.status = 'APPROVED' THEN 'APPROVE'
        WHEN OLD.status = 'PENDING_APPROVAL' AND NEW.status = 'REJECTED' THEN 'REJECT'
        WHEN OLD.status IN ('APPROVED','SUSPENDED') AND NEW.status = 'ACTIVE' THEN 'ACTIVATE'
        WHEN OLD.status = 'ACTIVE' AND NEW.status = 'SUSPENDED' THEN 'SUSPEND'
        WHEN OLD.status IN ('APPROVED','ACTIVE','SUSPENDED') AND NEW.status = 'REVOKED' THEN 'REVOKE'
        WHEN OLD.status IN ('ACTIVE','SUSPENDED') AND NEW.status = 'SUPERSEDED' THEN 'SUPERSEDE'
    END;
    IF expected_action IS NULL THEN
        RAISE EXCEPTION 'binding transition % -> % is not governed', OLD.status, NEW.status USING ERRCODE = 'check_violation';
    END IF;
    SELECT * INTO t FROM registry.seller_operating_binding_transition x
     WHERE x.binding_id = OLD.binding_id AND x.binding_version = OLD.binding_version
       AND x.action = expected_action AND x.from_status = OLD.status AND x.to_status = NEW.status
       AND x.performed_at >= now() -- this transaction's own row (now() is the transaction start)
     ORDER BY x.performed_at DESC LIMIT 1;
    IF NOT FOUND OR NOT EXISTS (SELECT 1 FROM identity.principal p WHERE p.principal_id = t.performed_by AND p.actor_type = 'human') THEN
        RAISE EXCEPTION 'missing authenticated human binding transition' USING ERRCODE = 'check_violation';
    END IF;
    IF expected_action IN ('APPROVE','REJECT') THEN
        IF t.performed_by = OLD.proposed_by THEN
            RAISE EXCEPTION 'the checker must not be the proposer' USING ERRCODE = 'check_violation';
        END IF;
        IF expected_action = 'APPROVE' AND (NEW.approved_by IS DISTINCT FROM t.performed_by OR NEW.approval_id IS NULL) THEN
            RAISE EXCEPTION 'approval provenance must name the checker' USING ERRCODE = 'check_violation';
        END IF;
    ELSIF expected_action = 'ACTIVATE' THEN
        IF t.performed_by IN (OLD.proposed_by, OLD.approved_by)
           OR NEW.activated_by IS DISTINCT FROM t.performed_by OR NEW.activation_id IS NULL
           OR (OLD.activation_id IS NOT NULL AND NEW.activation_id = OLD.activation_id) THEN
            RAISE EXCEPTION 'activation needs a third, distinct operator and a fresh activation' USING ERRCODE = 'check_violation';
        END IF;
        -- CP revalidation: effective now, live PRIMARY Organisation, verified legal entity,
        -- and the engine instance still bound to the tenant.
        IF now_utc < OLD.effective_from OR (OLD.effective_to IS NOT NULL AND now_utc >= OLD.effective_to)
           OR NOT EXISTS (
            SELECT 1 FROM registry.tenant_organisation_mapping om
             WHERE om.tenant_id = OLD.tenant_id AND om.organisation_id = OLD.operating_organisation_id
               AND om.mapping_role = 'PRIMARY_ORGANISATION' AND om.status = 'ACTIVE'
               AND om.effective_from <= now_utc AND (om.effective_to IS NULL OR om.effective_to > now_utc))
           OR NOT EXISTS (
            SELECT 1 FROM registry.legal_entity_profile lp
             WHERE lp.legal_entity_id = OLD.responsible_legal_entity_id
               AND lp.verification_state = 'VERIFIED' AND lp.legal_status = 'ACTIVE'
               AND lp.verified_at IS NOT NULL AND lp.verified_at <= now_utc
               AND lp.effective_from <= now_utc AND (lp.effective_to IS NULL OR lp.effective_to > now_utc))
           OR NOT EXISTS (
            SELECT 1 FROM capability.capability_binding cb
              JOIN topology.engine_instance ei ON ei.engine_instance_id = cb.engine_instance_id
              JOIN topology.engine e ON e.engine_id = ei.engine_id
              JOIN capability.capability_scope cs ON cs.scope_id = cb.scope_id
             WHERE ei.engine_instance_key = OLD.trade_engine_instance_id AND e.code = 'baobab-trade'
               AND ei.status = 'ACTIVE' AND ei.valid_period @> now_utc
               AND cs.tenant_id = OLD.tenant_id AND cb.status = 'ACTIVE' AND cb.valid_period @> now_utc) THEN
            RAISE EXCEPTION 'binding activation revalidation failed' USING ERRCODE = 'check_violation';
        END IF;
        -- A successor version activates only by superseding its live predecessor in this transaction.
        IF OLD.supersedes_binding_version IS NOT NULL AND EXISTS (
            SELECT 1 FROM registry.seller_operating_binding b
             WHERE b.binding_id = OLD.binding_id AND b.binding_version = OLD.supersedes_binding_version
               AND b.status IN ('ACTIVE','SUSPENDED')) THEN
            RAISE EXCEPTION 'supersede the predecessor before activating its successor' USING ERRCODE = 'check_violation';
        END IF;
    ELSIF expected_action = 'SUPERSEDE' AND NOT EXISTS (
            SELECT 1 FROM registry.seller_operating_binding b
             WHERE b.binding_id = OLD.binding_id AND b.binding_version = OLD.binding_version + 1
               AND b.status = 'APPROVED') THEN
        RAISE EXCEPTION 'only an approved successor version may supersede a binding' USING ERRCODE = 'check_violation';
    END IF;
    IF expected_action <> 'ACTIVATE' AND ROW(NEW.activated_by, NEW.activation_id) IS DISTINCT FROM ROW(OLD.activated_by, OLD.activation_id) THEN
        RAISE EXCEPTION 'activation provenance changes only on activation' USING ERRCODE = 'check_violation';
    END IF;
    NEW.updated_at := now_utc;
    RETURN NEW;
END $$;

CREATE TRIGGER seller_operating_binding_guard
    BEFORE INSERT OR UPDATE OR DELETE ON registry.seller_operating_binding
    FOR EACH ROW EXECUTE FUNCTION registry.seller_operating_binding_guard();

-- Idempotent replay ledger for human binding commands: a key is unique per
-- command kind, and a different principal or payload is never a retry.
CREATE TABLE registry.seller_operating_binding_command (
    command_kind text NOT NULL CHECK (command_kind IN ('PROPOSE','TRANSITION')),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    request_digest char(64) NOT NULL,
    actor_id uuid NOT NULL REFERENCES identity.principal(principal_id),
    response jsonb NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (command_kind, idempotency_key)
);
CREATE TRIGGER seller_operating_binding_command_immutable
    BEFORE UPDATE OR DELETE ON registry.seller_operating_binding_command
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();

-- 4. Immutable Trade order-authority decisions.
CREATE TABLE registry.trade_order_authority_decision (
    decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id text NOT NULL CHECK (length(btrim(client_id)) BETWEEN 1 AND 200),
    context_id uuid NOT NULL,
    engine_instance_id text NOT NULL CHECK (engine_instance_id ~ '^ei_[a-z0-9]+$'),
    operation_class text NOT NULL CHECK (operation_class ~ '^trade\.[a-z-]+\.[a-z-]+$'),
    operation_reference text NOT NULL CHECK (length(operation_reference) BETWEEN 1 AND 256 AND operation_reference ~ '^[\x21-\x7E]+$'),
    binding_id uuid NOT NULL,
    binding_version integer NOT NULL CHECK (binding_version >= 1),
    native_configuration_digest char(64) NOT NULL CHECK (native_configuration_digest ~ '^[0-9a-f]{64}$'),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    outcome text NOT NULL CHECK (outcome IN (
        'AUTHORIZED','NO_APPLICABLE_MANDATE','AMBIGUOUS','REVOKED_OR_EXPIRED','ACTOR_NOT_VERIFIED',
        'ENGINE_INSTANCE_NOT_BOUND','CONTEXT_NOT_USABLE','BINDING_NOT_ACTIVE','BINDING_SCOPE_MISMATCH',
        'NATIVE_CONFIGURATION_MISMATCH','OPERATION_NOT_ACTIVATED')),
    tenant_id text,
    operating_organisation_id uuid,
    market char(2) CHECK (market ~ '^[A-Z]{2}$'),
    legal_actor_role text,
    activity text NOT NULL CHECK (length(activity) BETWEEN 3 AND 160),
    mandate_id uuid,
    supersedes_mandate_id uuid,
    responsible_legal_entity_id text,
    evidence_references jsonb CHECK (evidence_references IS NULL OR jsonb_typeof(evidence_references) = 'array'),
    mandate_effective_from timestamptz,
    mandate_effective_to timestamptz,
    evaluated_at timestamptz NOT NULL,
    expires_at timestamptz,
    policy_reference text NOT NULL CHECK (length(btrim(policy_reference)) BETWEEN 1 AND 500),
    correlation_id text,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT trade_order_authority_decision_authorized_complete CHECK (
        outcome <> 'AUTHORIZED' OR (
            tenant_id IS NOT NULL AND operating_organisation_id IS NOT NULL AND market IS NOT NULL
            AND legal_actor_role = 'SELLER_OF_RECORD' AND mandate_id IS NOT NULL
            AND responsible_legal_entity_id IS NOT NULL AND evidence_references IS NOT NULL
            AND jsonb_array_length(evidence_references) > 0
            AND mandate_effective_from IS NOT NULL AND expires_at IS NOT NULL
            AND expires_at > evaluated_at AND expires_at <= evaluated_at + interval '30 seconds')),
    CONSTRAINT trade_order_authority_decision_denied_discloses_nothing CHECK (
        outcome = 'AUTHORIZED' OR (
            mandate_id IS NULL AND supersedes_mandate_id IS NULL
            AND responsible_legal_entity_id IS NULL AND evidence_references IS NULL
            AND mandate_effective_from IS NULL AND mandate_effective_to IS NULL AND expires_at IS NULL)),
    -- Catalogue version 1 activates only the Trade-A classes; every other class
    -- is reserved, and only a reserved class is ever "not activated".
    CONSTRAINT trade_order_authority_decision_reserved_classes CHECK (
        (operation_class IN ('trade.order.commit','trade.order.create','trade.draft-order.finalize'))
        = (outcome <> 'OPERATION_NOT_ACTIVATED'))
);
CREATE INDEX trade_order_authority_decision_binding_idx
    ON registry.trade_order_authority_decision (binding_id, binding_version, evaluated_at);
CREATE INDEX trade_order_authority_decision_operation_idx
    ON registry.trade_order_authority_decision (operation_reference);
CREATE TRIGGER trade_order_authority_decision_immutable
    BEFORE UPDATE OR DELETE ON registry.trade_order_authority_decision
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();

REVOKE ALL ON registry.trade_native_configuration_attestation FROM PUBLIC;
REVOKE ALL ON registry.seller_operating_binding FROM PUBLIC;
REVOKE ALL ON registry.seller_operating_binding_transition FROM PUBLIC;
REVOKE ALL ON registry.trade_order_authority_decision FROM PUBLIC;
REVOKE ALL ON registry.seller_operating_binding_command FROM PUBLIC;
