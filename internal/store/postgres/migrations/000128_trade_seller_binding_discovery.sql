-- P0-04B Trade-A TA-02 (ADR-SHARED-039 §5): Seller Operating Binding discovery.
-- Trade asks which ACTIVE binding version applies to its attested native
-- configuration; CP answers from its own registry and records every answer.
-- A resolution identifies a candidate and is never authority.

-- 1. Discovery reads candidates by the context-derived scope and reference.
CREATE INDEX seller_operating_binding_discovery_idx
    ON registry.seller_operating_binding (tenant_id, operating_organisation_id, market,
                                          trade_engine_instance_id, native_configuration_reference);

-- 2. No overlapping in-force scopes: a binding never becomes ACTIVE while a
-- different binding of the same tenant, Organisation, market and engine is in
-- force (ACTIVE or SUSPENDED) for the same reference with an intersecting
-- currency scope. AMBIGUOUS_BINDING then remains defence in depth only. The
-- advisory lock serialises concurrent activations of one scope.
CREATE FUNCTION registry.seller_operating_binding_no_overlap() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'ACTIVE' AND OLD.status IS DISTINCT FROM 'ACTIVE' THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(
            'seller-operating-binding-scope/' || NEW.tenant_id || '/' || NEW.operating_organisation_id || '/' ||
            NEW.market || '/' || NEW.trade_engine_instance_id || '/' || NEW.native_configuration_reference, 0));
        IF EXISTS (
            SELECT 1 FROM registry.seller_operating_binding b
             WHERE b.binding_id <> NEW.binding_id
               AND b.status IN ('ACTIVE','SUSPENDED')
               AND b.tenant_id = NEW.tenant_id
               AND b.operating_organisation_id = NEW.operating_organisation_id
               AND b.market = NEW.market
               AND b.trade_engine_instance_id = NEW.trade_engine_instance_id
               AND b.native_configuration_reference = NEW.native_configuration_reference
               AND b.currency_scope && NEW.currency_scope) THEN
            RAISE EXCEPTION 'another in-force binding already covers this native configuration and currency'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END $$;

-- Fires after seller_operating_binding_guard (triggers run in name order).
CREATE TRIGGER seller_operating_binding_no_overlap
    BEFORE UPDATE ON registry.seller_operating_binding
    FOR EACH ROW EXECUTE FUNCTION registry.seller_operating_binding_no_overlap();

-- 3. Immutable discovery evidence. Only RESOLVED names a binding.
CREATE TABLE registry.trade_seller_binding_resolution (
    resolution_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id text NOT NULL CHECK (length(btrim(client_id)) BETWEEN 1 AND 200),
    context_id uuid NOT NULL,
    engine_instance_id text NOT NULL CHECK (engine_instance_id ~ '^ei_[a-z0-9]+$'),
    native_configuration_reference text NOT NULL CHECK (
        length(native_configuration_reference) BETWEEN 1 AND 128
        AND native_configuration_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
    native_configuration_digest char(64) NOT NULL CHECK (native_configuration_digest ~ '^[0-9a-f]{64}$'),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    outcome text NOT NULL CHECK (outcome IN (
        'RESOLVED','NO_APPLICABLE_BINDING','AMBIGUOUS_BINDING','BINDING_NOT_ACTIVE',
        'NATIVE_CONFIGURATION_MISMATCH','ENGINE_INSTANCE_NOT_BOUND','CONTEXT_NOT_USABLE')),
    tenant_id text,
    operating_organisation_id uuid,
    market char(2) CHECK (market ~ '^[A-Z]{2}$'),
    binding_id uuid,
    binding_version integer CHECK (binding_version >= 1),
    candidate_count integer NOT NULL CHECK (candidate_count >= 0),
    resolved_at timestamptz NOT NULL,
    correlation_id text,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT trade_seller_binding_resolution_names_only_when_resolved CHECK (
        (outcome = 'RESOLVED') = (binding_id IS NOT NULL AND binding_version IS NOT NULL)),
    CONSTRAINT trade_seller_binding_resolution_resolved_has_scope CHECK (
        outcome <> 'RESOLVED' OR (tenant_id IS NOT NULL AND operating_organisation_id IS NOT NULL
                                   AND market IS NOT NULL AND candidate_count >= 1)),
    CONSTRAINT trade_seller_binding_resolution_resolved_binding_fk
        FOREIGN KEY (binding_id, binding_version)
        REFERENCES registry.seller_operating_binding (binding_id, binding_version)
);
CREATE INDEX trade_seller_binding_resolution_binding_idx
    ON registry.trade_seller_binding_resolution (binding_id, binding_version)
    WHERE binding_id IS NOT NULL;

CREATE TRIGGER trade_seller_binding_resolution_immutable
    BEFORE UPDATE OR DELETE ON registry.trade_seller_binding_resolution
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();

REVOKE ALL ON registry.trade_seller_binding_resolution FROM PUBLIC;
