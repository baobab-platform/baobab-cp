-- ADR-BCP-020 gate ADA-06: a changeset may now target the principal an
-- administrative grant is issued to (ADMINISTRATIVE_GRANT_ISSUANCE) or the
-- grant a delegation rests on (ADMINISTRATIVE_GRANT_DELEGATION). The open-
-- changeset lock of ADR-BCP-021 section 66 is held on (target_type,
-- target_id), so two open changes for one grantee, or one source grant, are
-- serialised.
ALTER TABLE changeset.changeset DROP CONSTRAINT changeset_target_type_check;
ALTER TABLE changeset.changeset
    ADD CONSTRAINT changeset_target_type_check
        CHECK (target_type IN ('TENANT', 'MARKET', 'MAPPING', 'PROVIDER', 'ENGINE_RELEASE', 'ENGINE_INSTANCE',
                               'ADMINISTRATIVE_PRINCIPAL', 'ADMINISTRATIVE_GRANT'));
