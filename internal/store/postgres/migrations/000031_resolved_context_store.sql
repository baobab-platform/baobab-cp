-- Persists resolved Context values so they can be looked up later by
-- context_id (ADR-BCP-004 SS70 "Context Identifier", SS73 "Context Cache").
-- This is the store the Runtime APIs (baobab-platform/baobab-cp #74 sub-work item 6)
-- need: baobab-platform/shared's resolutionRequest contract requires callers to pass
-- a pre-resolved context_id rather than an inline context, which means
-- something durable has to answer "what did context_id X resolve to" after
-- the POST /v1/context/resolve call that minted it returns.
--
-- A resolved context is immutable (SS71): this table is insert-and-read-only
-- from the application layer -- there is deliberately no updated_at and no
-- UPDATE statement anywhere that touches it. expires_at (SS72) bounds how
-- long a row remains valid for lookup; nothing in this migration enforces
-- that at the database level (no partial index, no cron/TTL job) -- callers
-- are expected to treat an expired row as not found, exactly as
-- Context.IsExpired(at) already does in Go.
CREATE SCHEMA IF NOT EXISTS context;

-- Columns mirror internal/domain.Context field-for-field. isolation_profile_id
-- is deliberately plain text, not a foreign key into policy.isolation_profile
-- (unlike capability.capability_scope.isolation_profile_id): a resolved
-- Context reflects whatever evidence the resolver was given at request time,
-- which is not guaranteed to already be a registered isolation profile.
CREATE TABLE IF NOT EXISTS context.resolved_context (
    context_id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    principal_id          text NOT NULL,
    tenant_id             text NOT NULL,
    legal_entity_id       text,
    organisation_id       text,
    business_unit_id      text,
    digital_estate_id     text,
    digital_property_id   text,
    channel_id            text,
    market_id             text,
    jurisdiction          text,
    country_code          text,
    currency_code         text,
    locale                text,
    deployment_region     text,
    environment           text,
    isolation_profile_id  text,
    correlation_id        text NOT NULL,
    resolved_at           timestamptz NOT NULL,
    expires_at            timestamptz,
    provenance            jsonb NOT NULL DEFAULT '{}'::jsonb,
    CHECK (expires_at IS NULL OR expires_at > resolved_at)
);

CREATE INDEX IF NOT EXISTS resolved_context_tenant_idx
    ON context.resolved_context(tenant_id);

-- Supports a future reaper/invalidation job (ADR-BCP-004 SS74) finding
-- expired rows without a full table scan; no such job exists yet in this
-- codebase.
CREATE INDEX IF NOT EXISTS resolved_context_expires_at_idx
    ON context.resolved_context(expires_at);
