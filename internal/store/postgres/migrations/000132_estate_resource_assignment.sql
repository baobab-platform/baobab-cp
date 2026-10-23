-- ADR-SHARED-042 section 14, Decision 9 (Platform Owner, 23 October 2026;
-- Shared control-plane/v1 estate-resource-assignment; CP ADR-BCP-031): CP
-- proves the Digital Estate of a native commitment that does not arrive
-- through a governed BFF. An assignment binds one subject to one estate:
--   * WORKLOAD: an authenticated server application, by its IAM workload
--     client id (never a Medusa key);
--   * NATIVE_RESOURCE: one Trade native seller configuration exactly as Trade
--     attested it to CP under Trade-0 (registry.trade_native_configuration_attestation).
-- Governance, enforced here whatever path writes:
--   * a proposal is PROPOSED at revision 1, by a human Principal; the Tenant
--     is the single Tenant of the estate's governed commerce profiles;
--   * scope (subject, estate, Tenant, engine instance, Markets, operations,
--     interval, proposer) is immutable; a change is a new assignment;
--   * every change increments the revision by one and is backed, in the same
--     transaction, by an immutable human transition row; APPROVE and
--     REINSTATE are performed by a Principal other than the proposer
--     (four-eyes); REVOKED is terminal; nothing is deleted or truncated;
--   * at most one ACTIVE assignment covers a subject, engine instance,
--     Market and operation over an overlapping interval.
-- Every resolution answer is immutable evidence; only ATTESTED names an
-- assignment. Nothing here is registered or allocated.

-- Distinct, non-empty commitment operations only (a CHECK cannot hold a subquery).
CREATE FUNCTION registry.estate_resource_assignment_operations_valid(operations text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT cardinality(operations) >= 1 AND array_position(operations, NULL) IS NULL
       AND operations <@ ARRAY['trade.order.commit', 'trade.order.create', 'trade.draft-order.finalize']
       AND cardinality(operations) = (SELECT count(DISTINCT o) FROM unnest(operations) o)
$$;

CREATE TABLE registry.estate_resource_assignment (
    assignment_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scope text NOT NULL CHECK (scope IN ('WORKLOAD', 'NATIVE_RESOURCE')),
    workload_client_id text CHECK (workload_client_id ~ '^[a-z][a-z0-9-]{2,62}$'),
    native_configuration_reference text CHECK (
        length(native_configuration_reference) BETWEEN 1 AND 128
        AND native_configuration_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
    native_configuration_digest char(64) CHECK (native_configuration_digest ~ '^[0-9a-f]{64}$'),
    digital_estate_id text NOT NULL CHECK (
        length(digital_estate_id) BETWEEN 3 AND 63 AND digital_estate_id ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$'),
    tenant_id text NOT NULL CHECK (length(tenant_id) BETWEEN 6 AND 63 AND tenant_id ~ '^tn_[a-z0-9]+$'),
    trade_engine_instance_id text NOT NULL CHECK (
        length(trade_engine_instance_id) BETWEEN 6 AND 63 AND trade_engine_instance_id ~ '^ei_[a-z0-9]+$'),
    market_scope text[] NOT NULL CHECK (registry.estate_commerce_profile_markets_valid(market_scope)),
    permitted_operations text[] NOT NULL CHECK (registry.estate_resource_assignment_operations_valid(permitted_operations)),
    status text NOT NULL CHECK (status IN ('PROPOSED', 'ACTIVE', 'SUSPENDED', 'REVOKED')),
    revision integer NOT NULL CHECK (revision >= 1),
    effective_from timestamptz NOT NULL,
    effective_until timestamptz,
    proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    approved_by uuid REFERENCES identity.principal(principal_id),
    proposal_reference text NOT NULL CHECK (length(btrim(proposal_reference)) BETWEEN 1 AND 500),
    approval_reference text CHECK (length(btrim(approval_reference)) BETWEEN 1 AND 500),
    evidence_reference text NOT NULL CHECK (length(btrim(evidence_reference)) BETWEEN 1 AND 500),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (effective_until IS NULL OR effective_until > effective_from),
    CHECK (CASE scope
        WHEN 'WORKLOAD' THEN workload_client_id IS NOT NULL
             AND native_configuration_reference IS NULL AND native_configuration_digest IS NULL
        ELSE workload_client_id IS NULL
             AND native_configuration_reference IS NOT NULL AND native_configuration_digest IS NOT NULL END),
    CHECK (status NOT IN ('ACTIVE', 'SUSPENDED') OR (approved_by IS NOT NULL AND approval_reference IS NOT NULL)),
    CHECK (approved_by IS NULL OR approved_by <> proposed_by)
);
CREATE INDEX estate_resource_assignment_workload_idx
    ON registry.estate_resource_assignment (workload_client_id, trade_engine_instance_id) WHERE scope = 'WORKLOAD';
CREATE INDEX estate_resource_assignment_native_idx
    ON registry.estate_resource_assignment (native_configuration_reference, trade_engine_instance_id)
    WHERE scope = 'NATIVE_RESOURCE';

CREATE TABLE registry.estate_resource_assignment_transition (
    transition_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    assignment_id uuid NOT NULL REFERENCES registry.estate_resource_assignment(assignment_id),
    action text NOT NULL CHECK (action IN ('APPROVE', 'SUSPEND', 'REINSTATE', 'REVOKE')),
    from_status text NOT NULL,
    to_status text NOT NULL,
    to_revision integer NOT NULL CHECK (to_revision >= 2),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    evidence_reference text NOT NULL CHECK (length(btrim(evidence_reference)) BETWEEN 1 AND 500),
    performed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    performed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (assignment_id, to_revision)
);
CREATE TRIGGER estate_resource_assignment_transition_immutable
    BEFORE UPDATE OR DELETE ON registry.estate_resource_assignment_transition
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();
CREATE TRIGGER estate_resource_assignment_transition_no_truncate
    BEFORE TRUNCATE ON registry.estate_resource_assignment_transition
    FOR EACH STATEMENT EXECUTE FUNCTION registry.trade_seller_binding_immutable();

-- Idempotent human commands: a key replays only the identical command by the same actor.
CREATE TABLE registry.estate_resource_assignment_command (
    command_kind text NOT NULL CHECK (command_kind IN ('PROPOSE', 'TRANSITION')),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    actor_principal_id uuid NOT NULL REFERENCES identity.principal(principal_id),
    request_digest char(64) NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    assignment_id uuid NOT NULL REFERENCES registry.estate_resource_assignment(assignment_id),
    result_revision integer NOT NULL,
    -- The exact assignment this command returned; a replay answers with it,
    -- never with the assignment's later state.
    result jsonb NOT NULL CHECK (jsonb_typeof(result) = 'object'
        AND result->>'assignment_id' = assignment_id::text AND (result->>'revision')::integer = result_revision),
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (command_kind, actor_principal_id, idempotency_key)
);
CREATE TRIGGER estate_resource_assignment_command_immutable
    BEFORE UPDATE OR DELETE ON registry.estate_resource_assignment_command
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();
CREATE TRIGGER estate_resource_assignment_command_no_truncate
    BEFORE TRUNCATE ON registry.estate_resource_assignment_command
    FOR EACH STATEMENT EXECUTE FUNCTION registry.trade_seller_binding_immutable();

CREATE FUNCTION registry.estate_resource_assignment_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    t registry.estate_resource_assignment_transition%ROWTYPE;
    expected_action text;
    tenants text[];
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'estate resource assignments are never deleted' USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'PROPOSED' OR NEW.revision <> 1 OR NEW.approved_by IS NOT NULL OR NEW.approval_reference IS NOT NULL
           OR NOT EXISTS (SELECT 1 FROM identity.principal p WHERE p.principal_id = NEW.proposed_by AND p.actor_type = 'human' AND p.status = 'ACTIVE') THEN
            RAISE EXCEPTION 'an assignment is proposed by a human, PROPOSED at revision 1' USING ERRCODE = 'check_violation';
        END IF;
        -- The Tenant is the estate's own: the single Tenant of its governed commerce profiles.
        SELECT array_agg(DISTINCT tenant_id) INTO tenants
          FROM registry.estate_commerce_profile WHERE digital_estate_id = NEW.digital_estate_id;
        IF tenants IS NULL OR cardinality(tenants) <> 1 OR tenants[1] <> NEW.tenant_id THEN
            RAISE EXCEPTION 'the assignment Tenant must be the estate''s single governed Tenant' USING ERRCODE = 'check_violation';
        END IF;
        NEW.created_at := clock_timestamp();
        NEW.updated_at := NEW.created_at;
        RETURN NEW;
    END IF;
    IF ROW(NEW.assignment_id, NEW.scope, NEW.workload_client_id, NEW.native_configuration_reference,
           NEW.native_configuration_digest, NEW.digital_estate_id, NEW.tenant_id, NEW.trade_engine_instance_id,
           NEW.market_scope, NEW.permitted_operations, NEW.effective_from, NEW.effective_until, NEW.proposed_by,
           NEW.proposal_reference, NEW.evidence_reference, NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.assignment_id, OLD.scope, OLD.workload_client_id, OLD.native_configuration_reference,
           OLD.native_configuration_digest, OLD.digital_estate_id, OLD.tenant_id, OLD.trade_engine_instance_id,
           OLD.market_scope, OLD.permitted_operations, OLD.effective_from, OLD.effective_until, OLD.proposed_by,
           OLD.proposal_reference, OLD.evidence_reference, OLD.created_at)
       OR NEW.revision <> OLD.revision + 1
       OR (OLD.approved_by IS NOT NULL AND ROW(NEW.approved_by, NEW.approval_reference)
           IS DISTINCT FROM ROW(OLD.approved_by, OLD.approval_reference)) THEN
        RAISE EXCEPTION 'assignment scope and provenance are immutable; every change increments the revision by one'
            USING ERRCODE = 'check_violation';
    END IF;
    expected_action := CASE
        WHEN OLD.status = 'PROPOSED' AND NEW.status = 'ACTIVE' THEN 'APPROVE'
        WHEN OLD.status = 'ACTIVE' AND NEW.status = 'SUSPENDED' THEN 'SUSPEND'
        WHEN OLD.status = 'SUSPENDED' AND NEW.status = 'ACTIVE' THEN 'REINSTATE'
        WHEN OLD.status IN ('PROPOSED', 'ACTIVE', 'SUSPENDED') AND NEW.status = 'REVOKED' THEN 'REVOKE'
    END;
    IF expected_action IS NULL THEN
        RAISE EXCEPTION 'assignment transition % -> % is not governed', OLD.status, NEW.status USING ERRCODE = 'check_violation';
    END IF;
    SELECT * INTO t FROM registry.estate_resource_assignment_transition x
     WHERE x.assignment_id = OLD.assignment_id AND x.to_revision = NEW.revision AND x.action = expected_action
       AND x.from_status = OLD.status AND x.to_status = NEW.status
       -- This transaction's own row: its inserting transaction is the current
       -- one. A timestamp cannot prove that (performed_at is writer-supplied,
       -- so a pre-committed future-dated row would pass); xmin can.
       AND x.xmin = pg_current_xact_id()::xid;
    IF NOT FOUND OR NOT EXISTS (SELECT 1 FROM identity.principal p WHERE p.principal_id = t.performed_by AND p.actor_type = 'human' AND p.status = 'ACTIVE') THEN
        RAISE EXCEPTION 'missing authenticated human assignment transition' USING ERRCODE = 'check_violation';
    END IF;
    IF expected_action IN ('APPROVE', 'REINSTATE') AND t.performed_by = OLD.proposed_by THEN
        RAISE EXCEPTION 'the approver must not be the proposer' USING ERRCODE = 'check_violation';
    END IF;
    IF expected_action = 'APPROVE' AND (NEW.approved_by IS DISTINCT FROM t.performed_by OR NEW.approval_reference IS NULL) THEN
        RAISE EXCEPTION 'approval provenance must name the approver' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.status = 'ACTIVE' THEN
        PERFORM pg_advisory_xact_lock(hashtextextended('estate-resource-assignment/'
            || coalesce(NEW.workload_client_id, NEW.native_configuration_reference) || '/' || NEW.trade_engine_instance_id, 0));
        IF EXISTS (
            SELECT 1 FROM registry.estate_resource_assignment o
             WHERE o.assignment_id <> NEW.assignment_id AND o.status = 'ACTIVE' AND o.scope = NEW.scope
               AND o.workload_client_id IS NOT DISTINCT FROM NEW.workload_client_id
               AND o.native_configuration_reference IS NOT DISTINCT FROM NEW.native_configuration_reference
               AND o.trade_engine_instance_id = NEW.trade_engine_instance_id
               AND o.market_scope && NEW.market_scope AND o.permitted_operations && NEW.permitted_operations
               AND tstzrange(o.effective_from, o.effective_until, '[)')
                   && tstzrange(NEW.effective_from, NEW.effective_until, '[)')) THEN
            RAISE EXCEPTION 'another ACTIVE assignment already covers this subject, engine instance, Market and operation'
                USING ERRCODE = 'unique_violation';
        END IF;
    END IF;
    NEW.updated_at := clock_timestamp();
    RETURN NEW;
END $$;

CREATE TRIGGER estate_resource_assignment_guard
    BEFORE INSERT OR UPDATE OR DELETE ON registry.estate_resource_assignment
    FOR EACH ROW EXECUTE FUNCTION registry.estate_resource_assignment_guard();
CREATE TRIGGER estate_resource_assignment_no_truncate
    BEFORE TRUNCATE ON registry.estate_resource_assignment
    FOR EACH STATEMENT EXECUTE FUNCTION registry.trade_seller_binding_immutable();

-- Immutable evidence of every resolution answer. Only ATTESTED names an assignment.
CREATE TABLE registry.estate_resource_assignment_resolution (
    resolution_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    attestation_id uuid UNIQUE,
    caller_client_id text NOT NULL CHECK (length(btrim(caller_client_id)) BETWEEN 1 AND 200),
    scope text NOT NULL CHECK (scope IN ('WORKLOAD', 'NATIVE_RESOURCE')),
    subject_reference text NOT NULL CHECK (length(btrim(subject_reference)) BETWEEN 1 AND 200),
    digital_estate_id text NOT NULL,
    engine_instance_id text NOT NULL,
    market_id text NOT NULL,
    operation text NOT NULL,
    outcome text NOT NULL CHECK (outcome IN (
        'ATTESTED', 'WORKLOAD_EVIDENCE_INVALID', 'NATIVE_RESOURCE_NOT_ATTESTED', 'ASSIGNMENT_NOT_FOUND',
        'ASSIGNMENT_NOT_ACTIVE', 'OPERATION_NOT_PERMITTED', 'MARKET_NOT_PERMITTED',
        'ENGINE_INSTANCE_NOT_PERMITTED', 'ASSIGNMENT_AMBIGUOUS')),
    assignment_id uuid REFERENCES registry.estate_resource_assignment(assignment_id),
    assignment_revision integer,
    tenant_id text,
    candidate_count integer NOT NULL CHECK (candidate_count >= 0),
    resolved_at timestamptz NOT NULL,
    expires_at timestamptz,
    correlation_id text,
    CHECK (CASE WHEN outcome = 'ATTESTED'
        THEN attestation_id IS NOT NULL AND assignment_id IS NOT NULL AND assignment_revision IS NOT NULL
             AND tenant_id IS NOT NULL AND expires_at IS NOT NULL AND expires_at > resolved_at
             AND expires_at <= resolved_at + interval '60 seconds'
        ELSE attestation_id IS NULL AND assignment_id IS NULL AND assignment_revision IS NULL
             AND tenant_id IS NULL AND expires_at IS NULL END)
);
CREATE INDEX estate_resource_assignment_resolution_attestation_idx
    ON registry.estate_resource_assignment_resolution (attestation_id) WHERE outcome = 'ATTESTED';
CREATE TRIGGER estate_resource_assignment_resolution_immutable
    BEFORE UPDATE OR DELETE ON registry.estate_resource_assignment_resolution
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();
CREATE TRIGGER estate_resource_assignment_resolution_no_truncate
    BEFORE TRUNCATE ON registry.estate_resource_assignment_resolution
    FOR EACH STATEMENT EXECUTE FUNCTION registry.trade_seller_binding_immutable();

-- Decision 9: the commerce profile resolution may now also record
-- GOVERNED_RESOURCE_ASSIGNMENT provenance (its CHECK already allows the kind).

REVOKE ALL ON registry.estate_resource_assignment FROM PUBLIC;
REVOKE ALL ON registry.estate_resource_assignment_transition FROM PUBLIC;
REVOKE ALL ON registry.estate_resource_assignment_command FROM PUBLIC;
REVOKE ALL ON registry.estate_resource_assignment_resolution FROM PUBLIC;
