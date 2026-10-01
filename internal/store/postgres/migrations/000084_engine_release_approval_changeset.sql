-- ADR-BCP-025 section 2.4: an engine release becomes APPROVED only through
-- an ENGINE_RELEASE_APPROVAL changeset (Shared control-plane/v1
-- changeset-lifecycle.yaml); recording never approves one. A changeset may
-- now target an engine release, by its canonical erl_ identifier
-- (migration 000082 release_key).
ALTER TABLE changeset.changeset DROP CONSTRAINT changeset_target_type_check;
ALTER TABLE changeset.changeset
    ADD CONSTRAINT changeset_target_type_check
        CHECK (target_type IN ('TENANT', 'MARKET', 'MAPPING', 'PROVIDER', 'ENGINE_RELEASE'));
