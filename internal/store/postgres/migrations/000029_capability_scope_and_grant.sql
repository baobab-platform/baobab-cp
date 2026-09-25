-- Gate: introduce CapabilityScope and CapabilityGrant as first-class
-- concepts (ADR-BCP-003 SS9-19, SS51-60) -- confirmed absent from this
-- schema entirely during the Capability Platform Phase-0 audit
-- (baobab-platform/baobab-cp #73).
--
-- CapabilityScope is deliberately distinct from mapping.mapping_scope: the
-- two share dimension vocabulary but are evaluated by different resolvers
-- for different purposes (ADR-SHARED-007 SS25). capability.capability_binding
-- and capability.tenant_capability are NOT repointed at this table by this
-- migration -- that is a larger, separately-tracked change (baobab-platform/baobab-cp
-- #74) that needs its own backfill/compatibility plan, exactly as
-- capability_binding.provider_id was left nullable and unbackfilled in
-- migration 000028 pending a provider-registration workflow.
--
-- CapabilityGrant answers exactly one question: may this tenant, in this
-- scope, consume this capability? It never determines which provider or
-- engine instance serves the request (ADR-BCP-003 SS9). It is additive: the
-- existing capability.tenant_capability boolean-enablement table is not
-- migrated or dropped by this change; replacing it with real
-- CapabilityGrant-backed entitlement is tracked separately (baobab-platform/baobab-cp
-- #72's "Eventually" list).

CREATE TABLE IF NOT EXISTS capability.capability_scope (
    scope_id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             text NOT NULL,
    legal_entity_id       text,
    organisation_id       text,
    business_unit_id      text,
    digital_estate_id     text,
    digital_property_id   text,
    channel_id            text,
    market_id             text,
    jurisdiction          text,
    currency_code         char(3),
    customer_segment_id   text,
    catalogue_id          text,
    operating_region_id   text,
    geographic_region_id  text,
    deployment_region     text,
    environment           text,
    isolation_profile_id  uuid REFERENCES policy.isolation_profile(isolation_profile_id),
    include_countries     text[] NOT NULL DEFAULT '{}',
    exclude_countries     text[] NOT NULL DEFAULT '{}',
    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CHECK (currency_code IS NULL OR currency_code ~ '^[A-Z]{3}$'),
    CHECK (environment IS NULL OR environment IN ('local', 'development', 'staging', 'production'))
);

CREATE INDEX IF NOT EXISTS capability_scope_tenant_idx
    ON capability.capability_scope(tenant_id);

-- CR-002-style closed set mirrored from baobab-platform/shared's
-- contracts/capability/v1/domain.schema.json #/$defs/capabilityGrantSource
-- and #/$defs/capabilityGrantStatus (ADR-BCP-003 SS10-11).
CREATE TABLE IF NOT EXISTS capability.capability_grant (
    grant_id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          text NOT NULL,
    capability_id      uuid NOT NULL REFERENCES capability.capability(capability_id),
    scope_id           uuid NOT NULL REFERENCES capability.capability_scope(scope_id),
    source             text NOT NULL,
    source_reference   text,
    status             text NOT NULL DEFAULT 'PENDING',
    effective_from     timestamptz NOT NULL DEFAULT now(),
    effective_to       timestamptz,
    constraints        jsonb NOT NULL DEFAULT '{}'::jsonb,
    granted_by         text,
    revoked_at         timestamptz,
    revoked_by         text,
    revocation_reason  text,
    version            bigint NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CHECK (source IN ('PLATFORM_BASELINE', 'PRODUCT_SUBSCRIPTION', 'CONTRACT', 'TRIAL', 'MANUAL_APPROVAL', 'INTERNAL_POLICY', 'MIGRATION')),
    CHECK (status IN ('PENDING', 'ACTIVE', 'SUSPENDED', 'REVOKED', 'EXPIRED')),
    CHECK (effective_to IS NULL OR effective_to > effective_from),
    -- SS10: "The grant SHALL retain provenance back to its source" -- every
    -- source except the platform's own baseline entitlement must name the
    -- record it came from (e.g. a subscription_id).
    CHECK (source = 'PLATFORM_BASELINE' OR source_reference IS NOT NULL)
);

-- Unlike capability_binding, multiple grants for the same
-- tenant/capability/scope MAY legitimately coexist (ADR-BCP-003 SS35,
-- "Grant Ambiguity") -- no exclusion constraint is added here.
CREATE INDEX IF NOT EXISTS capability_grant_tenant_capability_idx
    ON capability.capability_grant(tenant_id, capability_id, status);
