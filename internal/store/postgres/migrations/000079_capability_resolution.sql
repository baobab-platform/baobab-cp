-- Every capability resolution decision (Shared capability/v1
-- resolution.schema.json; control-plane/v1 resolveCapability and
-- resolveCapabilityBatch), recorded under its resolution_id so it can be
-- audited. Written only by the Control Plane, never updated. Grant, binding,
-- provider and engine instance are the surrogate ids the decision named, not
-- foreign keys: a decision outlives what it named.
CREATE TABLE capability.capability_resolution (
    resolution_id       text PRIMARY KEY CHECK (resolution_id ~ '^res_[a-z0-9]+$' AND length(resolution_id) <= 63),
    context_id          text NOT NULL,
    tenant_id           text NOT NULL,
    capability_key      text NOT NULL,
    decision            text NOT NULL CHECK (decision IN ('RESOLVED', 'DENIED', 'UNAVAILABLE', 'AMBIGUOUS', 'INCOMPATIBLE')),
    reason_code         text,
    grant_id            uuid,
    binding_id          uuid,
    provider_id         uuid,
    engine_instance_id  uuid,
    contract_version    integer CHECK (contract_version IS NULL OR contract_version >= 1),
    service_reference   text,
    correlation_id      uuid NOT NULL,
    resolved_at         timestamptz NOT NULL,
    expires_at          timestamptz,
    -- A RESOLVED decision names its grant, binding and invocation; any other
    -- names why not.
    CHECK ((decision = 'RESOLVED') = (reason_code IS NULL)),
    CHECK (decision <> 'RESOLVED' OR (grant_id IS NOT NULL AND binding_id IS NOT NULL AND provider_id IS NOT NULL
        AND engine_instance_id IS NOT NULL AND service_reference IS NOT NULL))
);

CREATE INDEX capability_resolution_tenant_idx ON capability.capability_resolution (tenant_id, resolved_at);

-- ACTIVE bindings whose scope's tenant holds no effective ACTIVE grant for
-- the capability: capability resolution denies them (GRANT_NOT_FOUND) until
-- a grant is issued.
CREATE VIEW capability.tenant_binding_without_grant AS
SELECT cs.tenant_id, c.code AS capability_key, cb.id AS binding_id, cb.scope_id
FROM capability.capability_binding cb
JOIN capability.capability_scope cs ON cs.scope_id = cb.scope_id
JOIN capability.capability c ON c.capability_id = cb.capability_id
WHERE cb.status = 'ACTIVE'
  AND NOT EXISTS (
    SELECT 1 FROM capability.capability_grant g
    WHERE g.tenant_id = cs.tenant_id AND g.capability_id = cb.capability_id AND g.status = 'ACTIVE'
      AND g.effective_from <= now() AND (g.effective_to IS NULL OR g.effective_to > now()));
