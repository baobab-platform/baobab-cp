-- ADR-BCP-022 sections 17-18: the administrative contract
-- (control-plane/v1 canonical-entity.schema.json) describes what the Control
-- Plane records. registry.canonical_entity kept only the identity, key,
-- kind, owner and status of an entity: the display name, subtype, authority,
-- classification, schema version and validity window a registration carries
-- were discarded, and reads substituted placeholders for them.
--
-- The columns are added nullable. A row registered before this migration
-- keeps NULL, which the API reports as absent; nothing is back-filled from a
-- guess. Every registration through the canonical registry writes them from
-- now on.
ALTER TABLE registry.canonical_entity
    ADD COLUMN IF NOT EXISTS display_name text,
    ADD COLUMN IF NOT EXISTS subtype text,
    ADD COLUMN IF NOT EXISTS authority text,
    ADD COLUMN IF NOT EXISTS classification text,
    ADD COLUMN IF NOT EXISTS schema_version integer,
    ADD COLUMN IF NOT EXISTS effective_from timestamptz,
    ADD COLUMN IF NOT EXISTS effective_to timestamptz;

ALTER TABLE registry.canonical_entity
    ADD CONSTRAINT canonical_entity_display_name_ck
        CHECK (display_name IS NULL OR (btrim(display_name) <> '' AND length(display_name) <= 255)),
    ADD CONSTRAINT canonical_entity_subtype_ck
        CHECK (subtype IS NULL OR (btrim(subtype) <> '' AND length(subtype) <= 64)),
    ADD CONSTRAINT canonical_entity_authority_ck
        CHECK (authority IS NULL OR authority ~ '^[a-z][a-z0-9-]*$'),
    ADD CONSTRAINT canonical_entity_classification_ck
        CHECK (classification IS NULL OR classification IN ('PUBLIC', 'INTERNAL', 'TENANT_CONFIDENTIAL', 'RESTRICTED')),
    ADD CONSTRAINT canonical_entity_schema_version_ck
        CHECK (schema_version IS NULL OR schema_version >= 1),
    ADD CONSTRAINT canonical_entity_effective_period_ck
        CHECK (effective_to IS NULL OR (effective_from IS NOT NULL AND effective_to > effective_from));
