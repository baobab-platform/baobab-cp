-- ADR-SHARED-042 section 14, Decision 8 (Platform Owner, 22 October 2026;
-- Shared control-plane/v1 estate-commerce-profile; CP ADR-BCP-030): CP is the
-- sole authority for the governed commerce model (B2B or B2C) of a Digital
-- Estate on one Trade engine instance in a Market scope. Profiles change only
-- through the reviewed registry process:
--   * scope (estate, Tenant, engine instance, model, Markets) is immutable; a
--     change of model is a new profile that supersedes the old one;
--   * every change increments the revision by exactly one;
--   * REVOKED is terminal and nothing is deleted;
--   * an approval reference is mandatory;
--   * at most one ACTIVE profile covers any (estate, engine instance, Market):
--     there is no MIXED model and no default.
-- digital_estate_id follows the Shared estateId grammar (resourceId), as in
-- registry.estate_client_binding.

CREATE TABLE registry.estate_commerce_profile (
    commerce_profile_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    digital_estate_id text NOT NULL CHECK (
        length(digital_estate_id) BETWEEN 3 AND 63 AND digital_estate_id ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$'),
    tenant_id text NOT NULL CHECK (length(tenant_id) BETWEEN 6 AND 63 AND tenant_id ~ '^tn_[a-z0-9]+$'),
    trade_engine_instance_id text NOT NULL CHECK (
        length(trade_engine_instance_id) BETWEEN 6 AND 63 AND trade_engine_instance_id ~ '^ei_[a-z0-9]+$'),
    commerce_model text NOT NULL CHECK (commerce_model IN ('B2B', 'B2C')),
    market_scope text[] NOT NULL CHECK (
        cardinality(market_scope) >= 1
        AND array_position(market_scope, NULL) IS NULL),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'SUSPENDED', 'REVOKED')),
    revision integer NOT NULL CHECK (revision >= 1),
    effective_from timestamptz NOT NULL,
    effective_until timestamptz,
    supersedes uuid REFERENCES registry.estate_commerce_profile(commerce_profile_id),
    approval_reference text NOT NULL CHECK (length(btrim(approval_reference)) BETWEEN 1 AND 500),
    evidence_reference text NOT NULL CHECK (length(btrim(evidence_reference)) BETWEEN 1 AND 500),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (effective_until IS NULL OR effective_until > effective_from),
    CHECK (supersedes IS NULL OR supersedes <> commerce_profile_id)
);

CREATE INDEX estate_commerce_profile_estate_idx
    ON registry.estate_commerce_profile (digital_estate_id, trade_engine_instance_id);

-- Every Market in scope is a canonical resourceId and is listed once.
CREATE FUNCTION registry.estate_commerce_profile_markets_valid(markets text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
    SELECT coalesce(bool_and(m ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(m) BETWEEN 3 AND 63), false)
           AND count(*) = count(DISTINCT m)
    FROM unnest(markets) AS m
$$;

ALTER TABLE registry.estate_commerce_profile
    ADD CONSTRAINT estate_commerce_profile_markets_check
    CHECK (registry.estate_commerce_profile_markets_valid(market_scope));

CREATE FUNCTION registry.estate_commerce_profile_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'estate commerce profiles are never deleted' USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.commerce_profile_id <> OLD.commerce_profile_id OR NEW.digital_estate_id <> OLD.digital_estate_id
           OR NEW.tenant_id <> OLD.tenant_id OR NEW.trade_engine_instance_id <> OLD.trade_engine_instance_id
           OR NEW.commerce_model <> OLD.commerce_model OR NEW.market_scope <> OLD.market_scope
           OR NEW.supersedes IS DISTINCT FROM OLD.supersedes THEN
            RAISE EXCEPTION 'estate commerce profile scope is immutable; a new model is a superseding profile'
                USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.status = 'REVOKED' THEN
            RAISE EXCEPTION 'a revoked estate commerce profile is terminal' USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.revision <> OLD.revision + 1 THEN
            RAISE EXCEPTION 'every estate commerce profile change increments the revision by one'
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.updated_at := clock_timestamp();
    ELSE
        IF NEW.revision <> 1 THEN
            RAISE EXCEPTION 'a new estate commerce profile starts at revision 1' USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    -- One ACTIVE model per estate, engine instance and Market. Serialised per
    -- estate so two concurrent activations cannot both pass.
    IF NEW.status = 'ACTIVE' THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(
            'estate-commerce-profile/' || NEW.digital_estate_id || '/' || NEW.trade_engine_instance_id, 0));
        IF EXISTS (
            SELECT 1 FROM registry.estate_commerce_profile p
            WHERE p.commerce_profile_id <> NEW.commerce_profile_id
              AND p.digital_estate_id = NEW.digital_estate_id
              AND p.trade_engine_instance_id = NEW.trade_engine_instance_id
              AND p.status = 'ACTIVE'
              AND p.market_scope && NEW.market_scope
              AND tstzrange(p.effective_from, p.effective_until, '[)')
                  && tstzrange(NEW.effective_from, NEW.effective_until, '[)')) THEN
            RAISE EXCEPTION 'another ACTIVE commerce profile already covers this estate, engine instance and Market'
                USING ERRCODE = 'unique_violation';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER estate_commerce_profile_guard
    BEFORE INSERT OR UPDATE OR DELETE ON registry.estate_commerce_profile
    FOR EACH ROW EXECUTE FUNCTION registry.estate_commerce_profile_guard();

CREATE TRIGGER estate_commerce_profile_no_truncate
    BEFORE TRUNCATE ON registry.estate_commerce_profile
    FOR EACH STATEMENT EXECUTE FUNCTION registry.trade_seller_binding_immutable();

-- Immutable evidence of every answer. Only ATTESTED names a profile.
CREATE TABLE registry.estate_commerce_profile_resolution (
    resolution_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    attestation_id uuid UNIQUE,
    caller_client_id text NOT NULL CHECK (length(btrim(caller_client_id)) BETWEEN 1 AND 200),
    digital_estate_id text NOT NULL,
    engine_instance_id text NOT NULL,
    market_id text NOT NULL,
    provenance_kind text NOT NULL CHECK (provenance_kind IN ('ESTATE_CLIENT_BINDING', 'GOVERNED_RESOURCE_ASSIGNMENT')),
    provenance_reference text NOT NULL CHECK (length(btrim(provenance_reference)) BETWEEN 1 AND 500),
    outcome text NOT NULL CHECK (outcome IN (
        'ATTESTED', 'PROFILE_NOT_REGISTERED', 'PROFILE_NOT_ACTIVE', 'PROFILE_AMBIGUOUS',
        'ESTATE_PROVENANCE_INVALID', 'ENGINE_INSTANCE_NOT_PERMITTED')),
    commerce_profile_id uuid REFERENCES registry.estate_commerce_profile(commerce_profile_id),
    profile_revision integer,
    commerce_model text CHECK (commerce_model IS NULL OR commerce_model IN ('B2B', 'B2C')),
    candidate_count integer NOT NULL CHECK (candidate_count >= 0),
    resolved_at timestamptz NOT NULL,
    expires_at timestamptz,
    correlation_id text,
    CHECK (CASE WHEN outcome = 'ATTESTED'
        THEN commerce_profile_id IS NOT NULL AND profile_revision IS NOT NULL AND commerce_model IS NOT NULL
             AND attestation_id IS NOT NULL AND expires_at IS NOT NULL AND expires_at > resolved_at
        ELSE commerce_profile_id IS NULL AND profile_revision IS NULL AND commerce_model IS NULL
             AND attestation_id IS NULL AND expires_at IS NULL END)
);

CREATE TRIGGER estate_commerce_profile_resolution_immutable
    BEFORE UPDATE OR DELETE ON registry.estate_commerce_profile_resolution
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();

REVOKE ALL ON registry.estate_commerce_profile FROM PUBLIC;
REVOKE ALL ON registry.estate_commerce_profile_resolution FROM PUBLIC;
