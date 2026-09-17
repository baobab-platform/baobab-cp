-- Gate P4: Product and Composition Engine (basics). Adds the two tables
-- ProductSubscription -> CapabilityComposition -> CapabilityGrant expansion
-- needs that migration 000033 deliberately left unbuilt:
--
-- 1. capability.capability_composition / capability_composition_member --
--    the CapabilityComposition registry nabhold/shared's
--    contracts/capability/v1/composition.schema.json describes, but which
--    has never had a Go type or table in this repository. Until now,
--    product.product_version.composition_key (migration 000033) was a free
--    text column with nothing backing it -- any key was "valid" because
--    nothing ever looked one up.
-- 2. product.entitlement_projection -- the per-composition-member audit
--    trail of that expansion (Technical Specification SS91). It is written
--    by CompositionExpansionService as it works; resolution itself
--    continues to consult capability.capability_grant directly, never this
--    table.
--
-- Scope note (this Gate's "basics" cut, internal/service/composition_service.go):
-- CompositionExpansionService does not resolve capability_composition's own
-- includes_compositions/incompatible_with graph, nor evaluate
-- compositionMember.activation_condition, nor select OPTIONAL members via
-- ProductSubscription.subscription_profiles. Only MANDATORY and IMPORTANT
-- members with an empty activation_condition expand. Graph resolution and
-- profile-driven OPTIONAL selection remain later Programme Gate P4 work.

CREATE TABLE IF NOT EXISTS capability.capability_composition (
    composition_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    composition_key  text NOT NULL,
    name             text,
    composition_type text NOT NULL,
    version          text NOT NULL,
    includes_compositions jsonb NOT NULL DEFAULT '[]',
    incompatible_with     jsonb NOT NULL DEFAULT '[]',
    lifecycle        text NOT NULL DEFAULT 'DRAFT',
    metadata         jsonb NOT NULL DEFAULT '{}',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (composition_key, version),
    CHECK (composition_type IN ('PLATFORM', 'PRODUCT', 'PROFILE', 'ADD_ON', 'INTERNAL')),
    CHECK (version ~ '^\d+\.\d+\.\d+$'),
    CHECK (lifecycle IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'DEPRECATED', 'RETIRED'))
);

CREATE INDEX IF NOT EXISTS capability_composition_key_idx
    ON capability.capability_composition(composition_key);

CREATE TABLE IF NOT EXISTS capability.capability_composition_member (
    composition_id       uuid NOT NULL REFERENCES capability.capability_composition(composition_id) ON DELETE CASCADE,
    capability_key       text NOT NULL,
    criticality          text NOT NULL,
    version_constraint   text,
    activation_condition text,
    PRIMARY KEY (composition_id, capability_key),
    CHECK (criticality IN ('MANDATORY', 'IMPORTANT', 'OPTIONAL'))
);

-- product.product / product.product_version already exist (migration
-- 000033); entitlement_projection is added alongside them here rather than
-- there, since it did not exist until this Gate built the engine that
-- writes it.
CREATE TABLE IF NOT EXISTS product.entitlement_projection (
    entitlement_projection_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id            uuid NOT NULL REFERENCES product.product_subscription(subscription_id) ON DELETE CASCADE,
    tenant_id                  text NOT NULL,
    capability_key             text NOT NULL,
    status                     text NOT NULL DEFAULT 'PENDING',
    grant_id                   uuid REFERENCES capability.capability_grant(grant_id),
    failure_reason             text,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    CHECK (status IN ('PENDING', 'MATERIALIZED', 'FAILED')),
    CHECK (status <> 'MATERIALIZED' OR grant_id IS NOT NULL),
    CHECK (status <> 'FAILED' OR failure_reason IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS entitlement_projection_subscription_idx
    ON product.entitlement_projection(subscription_id);
