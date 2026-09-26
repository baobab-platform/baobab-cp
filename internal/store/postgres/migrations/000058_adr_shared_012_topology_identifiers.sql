-- ADR-SHARED-012: one grammar per topology identifier.
--
-- Engine instances keep their UUID as an internal surrogate. Their canonical
-- identifier, the only one any API carries, is "ei_" and the UUID's hex
-- digits, generated so it can never drift from the row it names
-- (domain.EngineInstanceKey derives the same value).
ALTER TABLE topology.engine_instance
    ADD COLUMN IF NOT EXISTS engine_instance_key text
        GENERATED ALWAYS AS ('ei_' || replace(engine_instance_id::text, '-', '')) STORED;

CREATE UNIQUE INDEX IF NOT EXISTS engine_instance_key_uq
    ON topology.engine_instance (engine_instance_key);

-- An engine's code is its engineId: the repository that owns it, e.g.
-- baobab-trade. New and changed rows must conform. Existing rows are not
-- renamed, since a code is referenced by registrations elsewhere; any that do
-- not conform are listed for review.
ALTER TABLE topology.engine
    ADD CONSTRAINT engine_code_engine_id_ck
        CHECK (length(code) BETWEEN 3 AND 63 AND code ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$') NOT VALID;

CREATE OR REPLACE VIEW topology.engine_code_nonconforming AS
    SELECT engine_id, code, created_at
    FROM topology.engine
    WHERE NOT (length(code) BETWEEN 3 AND 63 AND code ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$');

-- An identity reference's engine instance, when recorded, is canonical.
ALTER TABLE identity.identity_reference
    ADD CONSTRAINT identity_reference_engine_instance_ck
        CHECK (engine_instance_id IS NULL OR engine_instance_id ~ '^ei_[a-z0-9]+$') NOT VALID;

CREATE OR REPLACE VIEW identity.identity_reference_engine_instance_nonconforming AS
    SELECT identity_reference_id, engine, engine_instance_id
    FROM identity.identity_reference
    WHERE engine_instance_id IS NOT NULL AND engine_instance_id !~ '^ei_[a-z0-9]+$';
