-- ADR-BCP-006 Gate 8: provider migrations and their immutable plans
-- (Shared control-plane/v1 provider-migration.schema.json, sections 119-120).
-- A migration is created in PLAN; nothing here binds or moves traffic.

CREATE TABLE topology.provider_migration (
    provider_migration_id text PRIMARY KEY CHECK (provider_migration_id ~ '^pmg_[a-z0-9]+$'),
    source_provider_key   text NOT NULL,
    target_provider_key   text NOT NULL,
    capability_keys       text[] NOT NULL CHECK (cardinality(capability_keys) > 0),
    request               jsonb NOT NULL,
    stage                 text NOT NULL CHECK (stage IN ('DISCOVER', 'PLAN', 'PREPARE', 'SHADOW', 'CANARY', 'SHIFT',
                              'VALIDATE', 'RETIRE_OLD', 'COMPLETE', 'CANCELLED', 'ROLLED_BACK')),
    current_cohort_key    text,
    plan_id               text NOT NULL,
    plan_version          integer NOT NULL CHECK (plan_version >= 1),
    plan_digest           text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    blocked               boolean NOT NULL DEFAULT false,
    failure_reason        text,
    idempotency_key       text NOT NULL UNIQUE,
    request_hash          text NOT NULL,
    created_by            text NOT NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    started_at            timestamptz,
    completed_at          timestamptz,
    revision              bigint NOT NULL DEFAULT 1 CHECK (revision >= 1),
    -- Moving a provider's own instances is an engine upgrade or instance
    -- replacement, never a provider migration (sections 59-60).
    CHECK (source_provider_key <> target_provider_key),
    CHECK ((stage IN ('COMPLETE', 'CANCELLED', 'ROLLED_BACK')) = (completed_at IS NOT NULL))
);

-- Open migrations by source provider, for the in-progress blocker.
CREATE INDEX provider_migration_open_source_idx
    ON topology.provider_migration (source_provider_key)
    WHERE stage NOT IN ('COMPLETE', 'CANCELLED', 'ROLLED_BACK');

CREATE TABLE topology.provider_migration_plan (
    plan_id               text PRIMARY KEY CHECK (plan_id ~ '^plan_[a-z0-9]+$'),
    provider_migration_id text NOT NULL REFERENCES topology.provider_migration (provider_migration_id),
    plan_version          integer NOT NULL CHECK (plan_version >= 1),
    plan_digest           text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    base_revision         bigint NOT NULL CHECK (base_revision >= 1),
    document              jsonb NOT NULL,
    generated_at          timestamptz NOT NULL,
    expires_at            timestamptz NOT NULL,
    superseded_at         timestamptz,
    UNIQUE (provider_migration_id, plan_version)
);

-- The migration's current plan is one of its own plans.
ALTER TABLE topology.provider_migration
    ADD CONSTRAINT provider_migration_current_plan_fkey
    FOREIGN KEY (plan_id) REFERENCES topology.provider_migration_plan (plan_id)
    DEFERRABLE INITIALLY DEFERRED;
