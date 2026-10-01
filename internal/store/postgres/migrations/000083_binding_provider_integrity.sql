-- EA-02E: binding integrity. An ACTIVE binding identifies its canonical
-- capability, its provider, a contract major that provider supports, and
-- the engine instance serving it (ADR-SHARED-017 sections 36, 60). 000077
-- added capability_binding_active_provider_check NOT VALID and reported the
-- bindings it could not backfill in capability.binding_without_provider;
-- this migration closes that out.
--
-- A provider is a binding's candidate only if it also supports the
-- binding's contract major, as CreateBinding already requires of new
-- bindings: a provider that cannot serve the bound contract is never
-- assigned to it.
CREATE OR REPLACE VIEW capability.binding_provider_candidate AS
SELECT cb.id AS binding_id, cp.provider_id
FROM capability.capability_binding cb
JOIN topology.engine_instance ei ON ei.engine_instance_id = cb.engine_instance_id
JOIN capability.capability_provider cp ON cp.engine_id = ei.engine_id AND cp.status = 'ACTIVE'
JOIN capability.provider_capability_support pcs
    ON pcs.provider_id = cp.provider_id AND pcs.capability_id = cb.capability_id AND pcs.status = 'ACTIVE'
WHERE substring(lower(btrim(cb.contract_version)) FROM '^v?([0-9]+)')::integer = ANY (pcs.contract_versions);

-- Bindings that now have exactly one candidate take it, as in 000077.
UPDATE capability.capability_binding cb
SET provider_id = c.provider_id
FROM (
    SELECT binding_id, min(provider_id::text)::uuid AS provider_id
    FROM capability.binding_provider_candidate
    GROUP BY binding_id
    HAVING count(*) = 1
) c
WHERE cb.id = c.binding_id AND cb.provider_id IS NULL;

-- Nothing else is guessed. Any ACTIVE binding still naming no provider
-- stops the migration with its id and reason; an operator resolves each
-- one (names its provider, activates the one eligible provider, or takes
-- the binding out of ACTIVE) and the migration is re-run.
DO $$
DECLARE
    unresolved text;
BEGIN
    SELECT string_agg(format('%s (%s, capability %s, engine instance %s)', binding_id, reason, capability_key, engine_instance_id),
                      '; ' ORDER BY binding_id)
    INTO unresolved
    FROM capability.binding_without_provider;
    IF unresolved IS NOT NULL THEN
        RAISE EXCEPTION 'EA-02E: ACTIVE capability bindings name no provider: %', unresolved
            USING HINT = 'Resolve each binding in capability.binding_without_provider, then re-run the migration.',
                  ERRCODE = 'check_violation';
    END IF;
END;
$$;

ALTER TABLE capability.capability_binding VALIDATE CONSTRAINT capability_binding_active_provider_check;
