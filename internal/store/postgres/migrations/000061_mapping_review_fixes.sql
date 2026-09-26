-- Review fixes to migrations 000058 and 000059.

-- An engine instance identifier is 6 to 63 characters (Shared
-- control-plane/v1 engineInstanceId), not merely ei_ followed by anything.
-- Still NOT VALID: existing rows are reported, not rewritten.
ALTER TABLE identity.identity_reference DROP CONSTRAINT identity_reference_engine_instance_ck;
ALTER TABLE identity.identity_reference
    ADD CONSTRAINT identity_reference_engine_instance_ck
        CHECK (engine_instance_id IS NULL
            OR (length(engine_instance_id) BETWEEN 6 AND 63 AND engine_instance_id ~ '^ei_[a-z0-9]+$')) NOT VALID;

CREATE OR REPLACE VIEW identity.identity_reference_engine_instance_nonconforming AS
    SELECT identity_reference_id, engine, engine_instance_id
    FROM identity.identity_reference
    WHERE engine_instance_id IS NOT NULL
      AND NOT (length(engine_instance_id) BETWEEN 6 AND 63 AND engine_instance_id ~ '^ei_[a-z0-9]+$');

-- createMapping requires an Idempotency-Key: a retry with the same key and
-- body returns the mapping it created; the same key with another body is
-- refused. Keys are the proposer's own.
ALTER TABLE mapping.mapping
    ADD COLUMN create_idempotency_key text,
    ADD COLUMN create_request_hash text,
    ADD CONSTRAINT mapping_create_idempotency_ck
        CHECK ((create_idempotency_key IS NULL) = (create_request_hash IS NULL));
CREATE UNIQUE INDEX mapping_create_idempotency_uq
    ON mapping.mapping (created_by, create_idempotency_key)
    WHERE create_idempotency_key IS NOT NULL;
