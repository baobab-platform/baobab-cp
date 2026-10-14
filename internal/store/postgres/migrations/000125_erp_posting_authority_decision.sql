-- P0-04B ERP (ADR-SHARED-034): immutable record of every ERP posting-authority
-- decision, so a posting can later answer which legal actor and mandate applied
-- at its time (ADR-BCP-027 §10.2), not only today's mandates. A row is a CP
-- fact about one operation reference; it grants no provider, posting or
-- finance-readiness authority. Native ERP identifiers are never stored.
CREATE TABLE registry.erp_posting_authority_decision (
    decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id text NOT NULL CHECK (length(btrim(client_id)) BETWEEN 1 AND 200),
    context_id uuid NOT NULL,
    engine_instance_id text NOT NULL CHECK (engine_instance_id ~ '^ei_[a-z0-9]+$'),
    operation_class text NOT NULL CHECK (operation_class ~ '^erp\.[a-z-]+\.[a-z-]+$'),
    placement_id uuid NOT NULL,
    placement_version integer NOT NULL CHECK (placement_version >= 1),
    operation_reference text NOT NULL CHECK (length(operation_reference) BETWEEN 1 AND 256 AND operation_reference ~ '^[\x21-\x7E]+$'),
    outcome text NOT NULL CHECK (outcome IN (
        'AUTHORIZED', 'NO_APPLICABLE_MANDATE', 'AMBIGUOUS', 'REVOKED_OR_EXPIRED',
        'ACTOR_NOT_VERIFIED', 'ENGINE_INSTANCE_NOT_BOUND', 'CONTEXT_NOT_USABLE')),
    tenant_id text,
    operating_organisation_id uuid,
    market char(2) CHECK (market ~ '^[A-Z]{2}$'),
    legal_actor_role text,
    activity text,
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
    -- AUTHORIZED carries the whole actor and a lease of at most 30 seconds;
    -- every other outcome discloses no mandate or legal entity.
    CONSTRAINT erp_posting_authority_decision_authorized_complete CHECK (
        outcome <> 'AUTHORIZED' OR (
            tenant_id IS NOT NULL AND operating_organisation_id IS NOT NULL AND market IS NOT NULL
            AND legal_actor_role IS NOT NULL AND activity IS NOT NULL AND mandate_id IS NOT NULL
            AND responsible_legal_entity_id IS NOT NULL AND evidence_references IS NOT NULL
            AND jsonb_array_length(evidence_references) > 0
            AND mandate_effective_from IS NOT NULL AND expires_at IS NOT NULL
            AND expires_at > evaluated_at AND expires_at <= evaluated_at + interval '30 seconds')),
    CONSTRAINT erp_posting_authority_decision_denied_discloses_nothing CHECK (
        outcome = 'AUTHORIZED' OR (
            mandate_id IS NULL AND supersedes_mandate_id IS NULL
            AND responsible_legal_entity_id IS NULL AND evidence_references IS NULL
            AND mandate_effective_from IS NULL AND mandate_effective_to IS NULL
            AND expires_at IS NULL))
);

CREATE INDEX erp_posting_authority_decision_mandate_idx
    ON registry.erp_posting_authority_decision (mandate_id, evaluated_at)
    WHERE mandate_id IS NOT NULL;
CREATE INDEX erp_posting_authority_decision_tenant_idx
    ON registry.erp_posting_authority_decision (tenant_id, evaluated_at);

CREATE FUNCTION registry.erp_posting_authority_decision_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ERP posting-authority decisions are immutable history'
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER erp_posting_authority_decision_immutable
    BEFORE UPDATE OR DELETE ON registry.erp_posting_authority_decision
    FOR EACH ROW EXECUTE FUNCTION registry.erp_posting_authority_decision_immutable();

REVOKE ALL ON registry.erp_posting_authority_decision FROM PUBLIC;
