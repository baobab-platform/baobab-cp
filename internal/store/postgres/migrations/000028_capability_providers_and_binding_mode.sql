-- Gate: introduce CapabilityProvider and ProviderCapabilitySupport as
-- first-class concepts (ADR-BCP-002 §5.3, ADR-BCP-003 §20-23, ADR-BCP-006
-- §5-13, ADR-SHARED-007 §31-34) -- confirmed absent from this schema
-- entirely during the Capability Platform Phase-0 audit (baobab-platform/baobab-cp
-- #73). Also locks capability_binding.binding_mode to the canonical
-- five-value set (BCP-TS-ONBOARDING-001 CR-002, mirrored in
-- baobab-platform/shared's contracts/capability/v1/domain.schema.json) and adds
-- the provider_id column CR-005 requires between a binding and its
-- implementation.

CREATE TABLE IF NOT EXISTS capability.capability_provider (
    provider_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_key text NOT NULL UNIQUE,
    name text NOT NULL,
    provider_type text NOT NULL,
    engine_id uuid NOT NULL REFERENCES topology.engine(engine_id),
    status text NOT NULL DEFAULT 'DRAFT',
    ownership text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (provider_key ~ '^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$'),
    CHECK (provider_type IN ('BAOBAB_ENGINE', 'EXTERNAL_SERVICE', 'PLATFORM_NATIVE', 'ADAPTER')),
    CHECK (status IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'DEPRECATED', 'RETIRED'))
);

CREATE INDEX IF NOT EXISTS capability_provider_engine_idx
    ON capability.capability_provider(engine_id);

-- A provider declares what it can implement; a binding (below) declares
-- where it does so. No capability support is ever inferred merely from
-- engine association -- ADR-SHARED-007 §31.
CREATE TABLE IF NOT EXISTS capability.provider_capability_support (
    provider_id uuid NOT NULL REFERENCES capability.capability_provider(provider_id) ON DELETE CASCADE,
    capability_id uuid NOT NULL REFERENCES capability.capability(capability_id) ON DELETE CASCADE,
    contract_versions integer[] NOT NULL,
    status text NOT NULL DEFAULT 'ACTIVE',
    effective_from timestamptz NOT NULL DEFAULT now(),
    effective_to timestamptz,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (provider_id, capability_id),
    CHECK (cardinality(contract_versions) > 0),
    CHECK (status IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'DEPRECATED', 'RETIRED')),
    CHECK (effective_to IS NULL OR effective_to > effective_from)
);

-- CR-002: binding_mode locked to the canonical five-value set (PRIMARY,
-- FALLBACK, SHADOW, MIGRATION, DISABLED). The superseded seven-value set
-- conflated binding lifecycle with read/write access characteristics
-- (READ_ONLY) and migration-plan state (MIGRATION_SOURCE/MIGRATION_TARGET)
-- -- see BCP-TS-ONBOARDING-001 §7-9 for the rationale. No production data
-- exists yet, but existing rows are remapped defensively rather than
-- assumed absent:
--   SECONDARY, READ_ONLY      -> PRIMARY   (ordering now belongs to
--                                           priority; read/write access
--                                           belongs on provider-instance
--                                           characteristics, not
--                                           binding_mode)
--   MIGRATION_SOURCE/_TARGET  -> MIGRATION (source/target is now a
--                                           ProviderMigration plan
--                                           property, not binding
--                                           lifecycle)
UPDATE capability.capability_binding
SET binding_mode = 'PRIMARY'
WHERE binding_mode IN ('SECONDARY', 'READ_ONLY');

UPDATE capability.capability_binding
SET binding_mode = 'MIGRATION'
WHERE binding_mode IN ('MIGRATION_SOURCE', 'MIGRATION_TARGET');

ALTER TABLE capability.capability_binding
    DROP CONSTRAINT IF EXISTS capability_binding_binding_mode_check;

ALTER TABLE capability.capability_binding
    ADD CONSTRAINT capability_binding_binding_mode_check
    CHECK (binding_mode IN ('PRIMARY', 'FALLBACK', 'SHADOW', 'MIGRATION', 'DISABLED'));

-- CR-005: a binding identifies both its canonical provider and its
-- concrete engine instance -- neither alone is sufficient (ADR-BCP-002
-- §5.7, ADR-BCP-003 §24). Nullable for now: no provider-registration
-- workflow exists yet to backfill this for any binding created before
-- this migration (tracked: baobab-platform/baobab-cp#76, #82). It becomes
-- NOT NULL once that workflow exists and every existing binding has been
-- assigned a provider.
ALTER TABLE capability.capability_binding
    ADD COLUMN IF NOT EXISTS provider_id uuid REFERENCES capability.capability_provider(provider_id);

CREATE INDEX IF NOT EXISTS capability_binding_provider_idx
    ON capability.capability_binding(provider_id);
