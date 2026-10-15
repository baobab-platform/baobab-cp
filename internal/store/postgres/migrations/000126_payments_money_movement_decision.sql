-- P0-04B Payments (ADR-SHARED-035): immutable record of every Payments
-- money-movement authority decision, so each movement can later answer which
-- legal actor and mandate applied at its time (ADR-BCP-027 §10.2). A row is a
-- CP fact about one operation reference, attempt and instruction fingerprint;
-- it grants no provider, financial-approval, readiness or accounting authority.
CREATE TABLE registry.payments_money_movement_decision (
    decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id text NOT NULL CHECK (length(btrim(client_id)) BETWEEN 1 AND 200),
    context_id uuid NOT NULL,
    engine_instance_id text NOT NULL CHECK (engine_instance_id ~ '^ei_[a-z0-9]+$'),
    operation_class text NOT NULL CHECK (operation_class ~ '^payments\.[a-z-]+\.[a-z-]+$'),
    operation_reference text NOT NULL CHECK (length(operation_reference) BETWEEN 1 AND 256 AND operation_reference ~ '^[\x21-\x7E]+$'),
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 1000),
    instruction_fingerprint char(64) NOT NULL CHECK (instruction_fingerprint ~ '^[0-9a-f]{64}$'),
    outcome text NOT NULL CHECK (outcome IN (
        'AUTHORIZED', 'NO_APPLICABLE_MANDATE', 'AMBIGUOUS', 'REVOKED_OR_EXPIRED',
        'ACTOR_NOT_VERIFIED', 'ENGINE_INSTANCE_NOT_BOUND', 'CONTEXT_NOT_USABLE',
        'INSTRUCTION_MISMATCH', 'OPERATION_NOT_ACTIVATED')),
    tenant_id text,
    operating_organisation_id uuid,
    market char(2) CHECK (market ~ '^[A-Z]{2}$'),
    legal_actor_role text,
    activity text NOT NULL CHECK (length(activity) BETWEEN 3 AND 160),
    capability_key text,
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
    CONSTRAINT payments_money_movement_decision_authorized_complete CHECK (
        outcome <> 'AUTHORIZED' OR (
            tenant_id IS NOT NULL AND operating_organisation_id IS NOT NULL AND market IS NOT NULL
            AND legal_actor_role IS NOT NULL AND capability_key IS NOT NULL AND mandate_id IS NOT NULL
            AND responsible_legal_entity_id IS NOT NULL AND evidence_references IS NOT NULL
            AND jsonb_array_length(evidence_references) > 0
            AND mandate_effective_from IS NOT NULL AND expires_at IS NOT NULL
            AND expires_at > evaluated_at AND expires_at <= evaluated_at + interval '30 seconds')),
    CONSTRAINT payments_money_movement_decision_denied_discloses_nothing CHECK (
        outcome = 'AUTHORIZED' OR (
            mandate_id IS NULL AND supersedes_mandate_id IS NULL
            AND responsible_legal_entity_id IS NULL AND evidence_references IS NULL
            AND mandate_effective_from IS NULL AND mandate_effective_to IS NULL
            AND expires_at IS NULL)),
    -- Collection authority is never disbursement authority: a payout can only
    -- ever be recorded as not activated, with no role or capability.
    CONSTRAINT payments_money_movement_decision_payout_never_authorized CHECK (
        operation_class <> 'payments.payout.execute' OR (
            outcome = 'OPERATION_NOT_ACTIVATED' AND legal_actor_role IS NULL AND capability_key IS NULL)),
    CONSTRAINT payments_money_movement_decision_not_activated_is_payout CHECK (
        outcome <> 'OPERATION_NOT_ACTIVATED' OR operation_class = 'payments.payout.execute')
);

CREATE INDEX payments_money_movement_decision_mandate_idx
    ON registry.payments_money_movement_decision (mandate_id, evaluated_at)
    WHERE mandate_id IS NOT NULL;
CREATE INDEX payments_money_movement_decision_tenant_idx
    ON registry.payments_money_movement_decision (tenant_id, evaluated_at);
CREATE INDEX payments_money_movement_decision_operation_idx
    ON registry.payments_money_movement_decision (operation_reference, attempt);

CREATE FUNCTION registry.payments_money_movement_decision_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Payments money-movement decisions are immutable history'
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER payments_money_movement_decision_immutable
    BEFORE UPDATE OR DELETE ON registry.payments_money_movement_decision
    FOR EACH ROW EXECUTE FUNCTION registry.payments_money_movement_decision_immutable();

REVOKE ALL ON registry.payments_money_movement_decision FROM PUBLIC;
