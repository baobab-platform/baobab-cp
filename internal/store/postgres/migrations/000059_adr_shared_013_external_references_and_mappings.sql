-- ADR-SHARED-013: ExternalReferences and canonical Mappings in Shared's
-- control-plane/v1 canonical-mapping shape.
--
-- An ExternalReference records only that a native object exists in a
-- registered external system; a Mapping relates a canonical entity to one,
-- or to another canonical entity, within a tenant. Each has a Control
-- Plane-minted canonical identifier (ref_, map_); the UUID row id is an
-- internal surrogate that no API carries.

CREATE TABLE mapping.external_reference (
    row_id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    external_reference_id text NOT NULL UNIQUE CHECK (external_reference_id ~ '^ref_[a-z0-9]+$' AND length(external_reference_id) BETWEEN 8 AND 63),
    system_namespace      text NOT NULL CHECK (system_namespace ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(system_namespace) <= 128),
    engine_id             text NOT NULL CHECK (engine_id ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND length(engine_id) BETWEEN 3 AND 63),
    engine_instance_id    text REFERENCES topology.engine_instance (engine_instance_key),
    environment           text CHECK (environment IN ('local', 'development', 'staging', 'production')),
    native_entity_type    text NOT NULL CHECK (native_entity_type ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(native_entity_type) <= 128),
    native_id             text NOT NULL CHECK (length(native_id) BETWEEN 1 AND 256),
    native_key            text CHECK (native_key IS NULL OR length(native_key) BETWEEN 1 AND 256),
    native_uri            text CHECK (native_uri IS NULL OR length(native_uri) <= 2048),
    source_authority      text NOT NULL CHECK (source_authority IN ('engine', 'external-sync', 'manual-import', 'reconciliation')),
    fingerprint           text CHECK (fingerprint IS NULL OR length(fingerprint) BETWEEN 1 AND 128),
    status                text NOT NULL CHECK (status IN ('active', 'unverified', 'suspect', 'orphan', 'archived')),
    first_seen_at         timestamptz NOT NULL,
    last_verified_at      timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- One native identity is one ExternalReference (Canonical Mapping Model
-- section 8.3). Which canonical entity it corresponds to lives in Mappings,
-- where ambiguity is detected rather than stored.
CREATE UNIQUE INDEX external_reference_native_identity_uq
    ON mapping.external_reference (system_namespace, engine_id, engine_instance_id, environment, native_entity_type, native_id)
    NULLS NOT DISTINCT;

-- A mapping scope's canonical identifier ($defs.mappingScopeId) is "scope_"
-- and its UUID's hex digits, generated like an engine instance's key.
ALTER TABLE mapping.mapping_scope
    ADD COLUMN IF NOT EXISTS mapping_scope_key text
        GENERATED ALWAYS AS ('scope_' || replace(mapping_scope_id::text, '-', '')) STORED;
CREATE UNIQUE INDEX IF NOT EXISTS mapping_scope_key_uq ON mapping.mapping_scope (mapping_scope_key);

CREATE TABLE mapping.mapping (
    row_id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mapping_id                 text NOT NULL UNIQUE CHECK (mapping_id ~ '^map_[a-z0-9]+$' AND length(mapping_id) BETWEEN 8 AND 63),
    tenant_id                  text NOT NULL,
    legal_entity_id            text,
    mapping_type               text NOT NULL CHECK (mapping_type IN ('IDENTITY', 'REPRESENTATION', 'ORGANISATIONAL', 'CONTENT',
        'COMMERCE', 'ERP', 'CATALOGUE', 'PRICING', 'TAX', 'WAREHOUSE', 'FULFILMENT', 'PAYMENT', 'DOMAIN', 'LOCALE', 'CURRENCY',
        'CHANNEL', 'CAPABILITY', 'INTEGRATION', 'MIGRATION', 'ALIAS', 'SUCCESSOR')),
    canonical_entity_id        uuid NOT NULL REFERENCES registry.canonical_entity (canonical_entity_id),
    external_reference_id      text REFERENCES mapping.external_reference (external_reference_id),
    target_canonical_entity_id uuid REFERENCES registry.canonical_entity (canonical_entity_id),
    scope_id                   text REFERENCES mapping.mapping_scope (mapping_scope_key),
    direction                  text NOT NULL CHECK (direction IN ('BIDIRECTIONAL', 'CANONICAL_TO_EXTERNAL', 'EXTERNAL_TO_CANONICAL', 'SOURCE_TO_TARGET')),
    cardinality                text NOT NULL CHECK (cardinality IN ('ONE_TO_ONE', 'ONE_TO_MANY', 'MANY_TO_ONE', 'MANY_TO_MANY')),
    authority                  text NOT NULL CHECK (authority IN ('control-plane', 'organisation', 'trade', 'erp', 'content', 'identity', 'external')),
    confidence                 text CHECK (confidence IN ('CONFIRMED', 'PROBABLE', 'CANDIDATE', 'REJECTED')),
    resolution_priority        integer CHECK (resolution_priority BETWEEN 0 AND 1000),
    status                     text NOT NULL CHECK (status IN ('DRAFT', 'VALIDATED', 'ACTIVE', 'DEPRECATED', 'SUSPENDED', 'MIGRATING', 'QUARANTINED', 'RETIRED')),
    effective_from             timestamptz NOT NULL,
    effective_to               timestamptz,
    supersedes_mapping_id      text REFERENCES mapping.mapping (mapping_id),
    metadata                   jsonb,
    revision                   bigint NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    created_by                 text NOT NULL,
    validated_at               timestamptz,
    validated_by               text,
    approved_at                timestamptz,
    approved_by                text,
    retired_at                 timestamptz,
    retired_by                 text,
    retirement_reason          text,
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    valid_period               tstzrange GENERATED ALWAYS AS (tstzrange(effective_from, effective_to, '[)')) STORED,
    CONSTRAINT mapping_one_target_ck CHECK ((external_reference_id IS NULL) <> (target_canonical_entity_id IS NULL)),
    CONSTRAINT mapping_distinct_entities_ck CHECK (target_canonical_entity_id IS NULL OR target_canonical_entity_id <> canonical_entity_id),
    CONSTRAINT mapping_period_ck CHECK (effective_to IS NULL OR effective_to > effective_from),
    -- Four-eyes (Canonical Mapping Model section 48): the approver is never
    -- the creator.
    CONSTRAINT mapping_four_eyes_ck CHECK (approved_by IS NULL OR approved_by <> created_by)
);

-- The same relationship is never ACTIVE twice at once within a scope.
ALTER TABLE mapping.mapping ADD CONSTRAINT mapping_identical_active_excl
    EXCLUDE USING gist (
        tenant_id WITH =,
        canonical_entity_id WITH =,
        (COALESCE(external_reference_id, target_canonical_entity_id::text)) WITH =,
        mapping_type WITH =,
        (COALESCE(scope_id, '')) WITH =,
        valid_period WITH &&
    ) WHERE (status = 'ACTIVE');

CREATE INDEX mapping_external_reference_idx ON mapping.mapping (tenant_id, external_reference_id) WHERE external_reference_id IS NOT NULL;
CREATE INDEX mapping_canonical_entity_idx ON mapping.mapping (tenant_id, canonical_entity_id);

-- The legacy registry.external_reference (migration 000010) embedded the
-- canonical entity in the reference and named no system namespace. It is
-- frozen: nothing may write it again.
CREATE OR REPLACE FUNCTION registry.legacy_external_reference_frozen() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'registry.external_reference is superseded by mapping.external_reference (ADR-SHARED-013)'
        USING ERRCODE = 'read_only_sql_transaction';
END;
$$;
CREATE TRIGGER legacy_external_reference_frozen
    BEFORE INSERT OR UPDATE ON registry.external_reference
    FOR EACH ROW EXECUTE FUNCTION registry.legacy_external_reference_frozen();

-- Its rows are reported, never converted by guesswork. An IAM organisation
-- link belongs in registry.iam_organisation_reference (ADR-BCP-018 ORG-10),
-- which records the issuer this row lacks; any other row needs an operator to
-- register it through the new API, since its system_namespace cannot be
-- established from the row.
CREATE VIEW mapping.legacy_external_reference_report AS
SELECT
    l.external_reference_id AS legacy_row_id,
    l.canonical_entity_id,
    l.provider,
    l.provider_key,
    l.created_at,
    CASE
        WHEN l.provider_key LIKE 'keycloak_organization:%' AND EXISTS (
            SELECT 1 FROM registry.iam_organisation_reference i
            WHERE i.organisation_id = l.canonical_entity_id
              AND i.provider_organisation_id = substr(l.provider_key, length('keycloak_organization:') + 1))
            THEN 'IAM_ORGANISATION_LINKED'
        WHEN l.provider_key LIKE 'keycloak_organization:%'
            THEN 'IAM_ORGANISATION_UNLINKED'
        ELSE 'SYSTEM_NAMESPACE_UNKNOWN'
    END AS disposition
FROM registry.external_reference l;
