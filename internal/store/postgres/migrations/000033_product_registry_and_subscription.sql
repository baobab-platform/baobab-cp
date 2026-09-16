-- Gate: remodel ProductSubscription against nabhold/shared's new
-- contracts/product/v1 package (ADR-SHARED-009, Programme Gate P1).
-- Programme Gate P0's classification (docs/reconciliation/phase-0-
-- architecture-inventory-and-lock.md) found the previous product_subscriptions
-- table (migration 000019, folded from the orphaned generation-A migration
-- set) to be the thinnest object in the whole inventory relative to its
-- target state: tenant_id/product_id/status only, no ProductVersion or
-- composition linkage at all. This migration replaces it.
--
-- No production data exists yet for this table (Technical Specification
-- SS55's pre-production policy), so it is dropped and replaced rather than
-- altered in place.
--
-- product.product / product.product_version are created here as the
-- registry side of the new contract, but are deliberately left unpopulated
-- and unenforced by product_subscription.product_id (no FK) -- building
-- real Product/ProductVersion CRUD and a version-selection workflow for
-- tenant onboarding is Programme Gate P4 ("Product and Composition Engine")
-- scope, not this migration's. product_subscription.product_version_id is
-- therefore nullable for the same reason capability_binding.provider_id was
-- left nullable in migration 000028: a required field in the target model
-- with no producing workflow yet (tracked: nabhold/baobab-cp#74-class
-- follow-up, Programme Gate P4).

CREATE SCHEMA IF NOT EXISTS product;

CREATE TABLE IF NOT EXISTS product.product (
    product_id   text PRIMARY KEY,
    name         text NOT NULL,
    description  text,
    status       text NOT NULL DEFAULT 'DRAFT',
    owner        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (product_id ~ '^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$'),
    CHECK (status IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'DEPRECATED', 'RETIRED'))
);

CREATE TABLE IF NOT EXISTS product.product_version (
    product_version_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id          text NOT NULL REFERENCES product.product(product_id) ON DELETE CASCADE,
    version             text NOT NULL,
    composition_key     text NOT NULL,
    status              text NOT NULL DEFAULT 'DRAFT',
    released_at         timestamptz,
    deprecated_at       timestamptz,
    retired_at          timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (product_id, version),
    CHECK (version ~ '^\d+\.\d+\.\d+$'),
    CHECK (status IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'DEPRECATED', 'RETIRED')),
    CHECK (status <> 'DEPRECATED' OR deprecated_at IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS product_version_product_idx
    ON product.product_version(product_id);

DROP TABLE IF EXISTS product_subscriptions;

-- product_id is intentionally NOT a foreign key to product.product here:
-- RegisterTenant (internal/store/postgres/store.go) has always accepted a
-- caller-requested product_id without requiring master-data registration
-- first (the same loose-coupling pattern capability.capability_scope and
-- capability.capability_grant already use for tenant_id), and Product
-- master-data entry does not exist as a workflow yet. Tightening this to a
-- real FK is Programme Gate P4 work, once that workflow exists.
CREATE TABLE IF NOT EXISTS product.product_subscription (
    subscription_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             text NOT NULL,
    product_id            text NOT NULL,
    product_version_id    uuid REFERENCES product.product_version(product_version_id),
    subscription_profiles text[] NOT NULL DEFAULT '{}',
    status                text NOT NULL DEFAULT 'PENDING',
    source                text NOT NULL DEFAULT 'ONBOARDING',
    source_reference      text,
    effective_from        timestamptz NOT NULL DEFAULT now(),
    effective_to          timestamptz,
    cancelled_at          timestamptz,
    cancellation_reason   text,
    version               bigint NOT NULL DEFAULT 1,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    -- One row per tenant/product, carried forward unchanged from the
    -- replaced table's PRIMARY KEY(tenant_id, product_id): GetEntitlement
    -- and ResolveContext (store.go) both do a plain tenant_id+product_id
    -- equality lookup with no effective-dating awareness. Real multi-
    -- subscription-over-time support needs that read path updated first,
    -- which is Programme Gate P4 scope, not a schema-only change.
    UNIQUE (tenant_id, product_id),
    CHECK (status IN ('DRAFT', 'PENDING', 'ACTIVE', 'SUSPENDED', 'CANCELLED', 'EXPIRED')),
    CHECK (source IN ('ONBOARDING', 'CONTRACT', 'TRIAL', 'MANUAL_APPROVAL', 'MIGRATION')),
    CHECK (source = 'ONBOARDING' OR source_reference IS NOT NULL),
    CHECK (effective_to IS NULL OR effective_to > effective_from),
    CHECK (status <> 'CANCELLED' OR cancelled_at IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS product_subscription_tenant_idx
    ON product.product_subscription(tenant_id, product_id, status);
