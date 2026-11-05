-- DEV-07 development issuer approvals are installed only by the CP approval
-- owner. Trade may request a decision but cannot create or amend approvals.
CREATE TABLE registry.trade_price_issuer_mandate (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workload_client_id text NOT NULL,
    issuer_id text NOT NULL,
    signer_key_fingerprint text NOT NULL CHECK (signer_key_fingerprint ~ '^[0-9a-f]{64}$'),
    binding_id uuid NOT NULL,
    binding_version integer NOT NULL,
    market_id text NOT NULL REFERENCES market.registry(market_id),
    policy_reference text NOT NULL,
    evidence_reference text NOT NULL CHECK (length(btrim(evidence_reference)) >= 8),
    proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    approved_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    effective_from timestamptz NOT NULL,
    effective_to timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (approved_by <> proposed_by AND effective_to > effective_from),
    FOREIGN KEY (binding_id, binding_version) REFERENCES registry.seller_operating_binding(binding_id, binding_version)
);
CREATE TABLE registry.trade_price_issuer_decision (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mandate_id uuid NOT NULL REFERENCES registry.trade_price_issuer_mandate(id),
    cart_id text NOT NULL,
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    source_digest text NOT NULL CHECK (source_digest ~ '^[0-9a-f]{64}$'),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '5 minutes'),
    UNIQUE (mandate_id, cart_id, currency, source_digest)
);
CREATE FUNCTION registry.trade_price_issuer_history_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'trade_price_issuer_mandate' AND TG_OP = 'UPDATE' THEN
        IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL AND
           (to_jsonb(NEW) - 'revoked_at') = (to_jsonb(OLD) - 'revoked_at') THEN
            NEW.revoked_at := clock_timestamp();
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'Trade price issuer authority history is immutable';
END $$;
CREATE TRIGGER trade_price_issuer_mandate_immutable BEFORE UPDATE OR DELETE ON registry.trade_price_issuer_mandate
    FOR EACH ROW EXECUTE FUNCTION registry.trade_price_issuer_history_guard();
CREATE TRIGGER trade_price_issuer_decision_immutable BEFORE UPDATE OR DELETE ON registry.trade_price_issuer_decision
    FOR EACH ROW EXECUTE FUNCTION registry.trade_price_issuer_history_guard();
CREATE TRIGGER trade_price_issuer_mandate_no_truncate BEFORE TRUNCATE ON registry.trade_price_issuer_mandate
    FOR EACH STATEMENT EXECUTE FUNCTION registry.trade_price_issuer_history_guard();
CREATE TRIGGER trade_price_issuer_decision_no_truncate BEFORE TRUNCATE ON registry.trade_price_issuer_decision
    FOR EACH STATEMENT EXECUTE FUNCTION registry.trade_price_issuer_history_guard();
