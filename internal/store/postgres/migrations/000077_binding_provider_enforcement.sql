-- A binding names its canonical provider as well as its engine instance
-- (ADR-BCP-002 section 5.7, ADR-BCP-003 section 24; Shared capability/v1
-- binding.schema.json requires provider_id). 000028 added provider_id as
-- nullable until providers were registered (baobab-platform/baobab-cp#76,
-- #82); engines now register their providers from their Shared
-- EngineRegistration, so bindings can name them.
--
-- A binding's provider is the one ACTIVE provider on the binding's engine
-- that ACTIVELY supports the binding's capability. Where there is exactly
-- one, existing bindings take it now; where there is none, or more than
-- one, the binding is left for an operator and reported below.

-- Each binding's eligible providers under that rule.
CREATE VIEW capability.binding_provider_candidate AS
SELECT cb.id AS binding_id, cp.provider_id
FROM capability.capability_binding cb
JOIN topology.engine_instance ei ON ei.engine_instance_id = cb.engine_instance_id
JOIN capability.capability_provider cp ON cp.engine_id = ei.engine_id AND cp.status = 'ACTIVE'
JOIN capability.provider_capability_support pcs
    ON pcs.provider_id = cp.provider_id AND pcs.capability_id = cb.capability_id AND pcs.status = 'ACTIVE';

UPDATE capability.capability_binding cb
SET provider_id = c.provider_id
FROM (
    SELECT binding_id, min(provider_id::text)::uuid AS provider_id
    FROM capability.binding_provider_candidate
    GROUP BY binding_id
    HAVING count(*) = 1
) c
WHERE cb.id = c.binding_id AND cb.provider_id IS NULL;

-- From now on no binding becomes or stays ACTIVE without a provider. NOT
-- VALID: rows that could not be backfilled are not checked until they
-- change; a later migration validates the constraint once
-- capability.binding_without_provider is empty.
ALTER TABLE capability.capability_binding
    ADD CONSTRAINT capability_binding_active_provider_check
    CHECK (status <> 'ACTIVE' OR provider_id IS NOT NULL) NOT VALID;

-- ACTIVE bindings still naming no provider, and why.
CREATE VIEW capability.binding_without_provider AS
SELECT cb.id AS binding_id, c.code AS capability_key, cb.engine_instance_id, cb.scope_id,
       CASE WHEN count(pc.provider_id) = 0 THEN 'NO_PROVIDER' ELSE 'AMBIGUOUS_PROVIDER' END AS reason
FROM capability.capability_binding cb
JOIN capability.capability c ON c.capability_id = cb.capability_id
LEFT JOIN capability.binding_provider_candidate pc ON pc.binding_id = cb.id
WHERE cb.status = 'ACTIVE' AND cb.provider_id IS NULL
GROUP BY cb.id, c.code, cb.engine_instance_id, cb.scope_id;
