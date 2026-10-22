-- ADR-SHARED-042 CAA-04 (Platform Owner ruling of 22 October 2026; Shared
-- control-plane/v1 estate-client-binding): CP is the system of record for the
-- governed binding of a confidential estate BFF's registered OAuth client to
-- the Digital Estate it serves. Trade learns the estate only from CP's
-- attestation; azp alone never establishes it. Bindings change only through
-- the reviewed registry process: scope is immutable, every change increments
-- the revision by exactly one, REVOKED is terminal and nothing is deleted.
-- digital_estate_id follows the Shared estateId grammar (resourceId).

CREATE TABLE registry.estate_client_binding (
    binding_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issuer text NOT NULL CHECK (issuer ~ '^https://[^[:space:]?#]+$'),
    bff_client_id text NOT NULL CHECK (
        length(bff_client_id) BETWEEN 1 AND 255 AND bff_client_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'),
    digital_estate_id text NOT NULL CHECK (
        length(digital_estate_id) BETWEEN 3 AND 63 AND digital_estate_id ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$'),
    tenant_id text NOT NULL CHECK (length(tenant_id) BETWEEN 6 AND 63 AND tenant_id ~ '^tn_[a-z0-9]+$'),
    engine_instance_id text NOT NULL CHECK (
        length(engine_instance_id) BETWEEN 6 AND 63 AND engine_instance_id ~ '^ei_[a-z0-9]+$'),
    environment text NOT NULL CHECK (environment IN ('local', 'development', 'staging', 'production')),
    deployment_reference text NOT NULL CHECK (
        length(deployment_reference) BETWEEN 3 AND 128 AND deployment_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'SUSPENDED', 'REVOKED')),
    revision integer NOT NULL CHECK (revision >= 1),
    effective_from timestamptz NOT NULL,
    effective_until timestamptz,
    evidence_reference text NOT NULL CHECK (length(btrim(evidence_reference)) BETWEEN 1 AND 500),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (effective_until IS NULL OR effective_until > effective_from),
    UNIQUE (issuer, bff_client_id, digital_estate_id, environment, engine_instance_id, deployment_reference)
);

CREATE INDEX estate_client_binding_client_idx
    ON registry.estate_client_binding (issuer, bff_client_id);

CREATE FUNCTION registry.estate_client_binding_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'estate client bindings are never deleted' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.binding_id <> OLD.binding_id OR NEW.issuer <> OLD.issuer OR NEW.bff_client_id <> OLD.bff_client_id
       OR NEW.digital_estate_id <> OLD.digital_estate_id OR NEW.tenant_id <> OLD.tenant_id
       OR NEW.engine_instance_id <> OLD.engine_instance_id OR NEW.environment <> OLD.environment
       OR NEW.deployment_reference <> OLD.deployment_reference THEN
        RAISE EXCEPTION 'estate client binding scope is immutable' USING ERRCODE = 'check_violation';
    END IF;
    IF OLD.status = 'REVOKED' THEN
        RAISE EXCEPTION 'a revoked estate client binding is terminal' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.revision <> OLD.revision + 1 THEN
        RAISE EXCEPTION 'every estate client binding change increments the revision by one'
            USING ERRCODE = 'check_violation';
    END IF;
    NEW.updated_at := clock_timestamp();
    RETURN NEW;
END $$;

CREATE TRIGGER estate_client_binding_guard
    BEFORE UPDATE OR DELETE ON registry.estate_client_binding
    FOR EACH ROW EXECUTE FUNCTION registry.estate_client_binding_guard();

-- Immutable evidence of every answer. Only ATTESTED names a binding.
CREATE TABLE registry.estate_client_binding_resolution (
    resolution_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    caller_client_id text NOT NULL CHECK (length(btrim(caller_client_id)) BETWEEN 1 AND 200),
    issuer text,
    bff_client_id text,
    engine_instance_id text NOT NULL,
    environment text NOT NULL,
    requested_digital_estate_id text,
    outcome text NOT NULL CHECK (outcome IN (
        'ATTESTED', 'CLIENT_EVIDENCE_INVALID', 'CLIENT_NOT_FOR_SUBJECT', 'BINDING_NOT_FOUND',
        'BINDING_AMBIGUOUS', 'BINDING_NOT_ACTIVE', 'ENVIRONMENT_MISMATCH', 'ENGINE_INSTANCE_NOT_PERMITTED')),
    binding_id uuid REFERENCES registry.estate_client_binding(binding_id),
    revision integer,
    candidate_count integer NOT NULL CHECK (candidate_count >= 0),
    resolved_at timestamptz NOT NULL,
    correlation_id text,
    CHECK ((outcome = 'ATTESTED') = (binding_id IS NOT NULL AND revision IS NOT NULL))
);

CREATE TRIGGER estate_client_binding_resolution_immutable
    BEFORE UPDATE OR DELETE ON registry.estate_client_binding_resolution
    FOR EACH ROW EXECUTE FUNCTION registry.trade_seller_binding_immutable();
