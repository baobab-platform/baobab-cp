-- ADR-BCP-025 section 2.1.1: a contract version is the capability
-- contract's major version, a positive integer. capability_binding has
-- stored it as text holding values such as "v1" and "1.0.0"; the
-- Control Plane now stores the major alone ("1"), as the Shared contracts,
-- engine registrations and engine releases already carry it.
--
-- "v1", "1" and "1.x.y" all normalise to 1. A value that cannot be
-- normalised is a data defect: the migration stops and names every such
-- binding for an operator, and nothing is guessed.
DO $$
DECLARE
    defects text;
BEGIN
    SELECT string_agg(format('%s (%L)', id, contract_version), '; ' ORDER BY id)
    INTO defects
    FROM capability.capability_binding
    WHERE lower(btrim(contract_version)) !~ '^v?0*[1-9][0-9]{0,8}(\.[0-9]+(\.[0-9]+)?)?$';
    IF defects IS NOT NULL THEN
        RAISE EXCEPTION 'ER-02: capability bindings whose contract_version is not a contract major: %', defects
            USING HINT = 'Correct each binding''s contract_version to the major version it binds, then re-run the migration.',
                  ERRCODE = 'check_violation';
    END IF;
END;
$$;

-- A changed value is a change of the binding, so its version moves.
UPDATE capability.capability_binding
SET contract_version = (substring(lower(btrim(contract_version)) FROM '^v?([0-9]+)')::integer)::text,
    version = version + 1,
    updated_at = now()
WHERE contract_version <> (substring(lower(btrim(contract_version)) FROM '^v?([0-9]+)')::integer)::text;

ALTER TABLE capability.capability_binding
    ADD CONSTRAINT capability_binding_contract_major_check CHECK (contract_version ~ '^[1-9][0-9]{0,8}$');
