-- The market registry (ADR-BCP-004 section 18; Shared
-- control-plane/v1/market.schema.json and market-lifecycle.yaml). A market's
-- configuration is its creator's document, validated against the Shared
-- MarketCreateRequest; the columns beside it are what the Control Plane
-- derives. DRAFT and VALIDATED follow the lifecycle's validation rules;
-- ACTIVE is reached only by activation, by someone other than the maker.
--
-- market.market (migration 000006) remains the country-keyed market
-- provisioning plans against; linking the two is a follow-up.

CREATE TABLE market.registry (
    market_id           text PRIMARY KEY CHECK (market_id ~ '^mkt_[a-z0-9]+$' AND length(market_id) <= 63),
    canonical_key       text NOT NULL UNIQUE,
    owner_tenant_id     varchar(63) NOT NULL REFERENCES tenants(tenant_id),
    parent_market_id    text REFERENCES market.registry(market_id),
    configuration       jsonb NOT NULL CHECK (jsonb_typeof(configuration) = 'object'),
    status              text NOT NULL CHECK (status IN ('DRAFT', 'VALIDATED', 'ACTIVE', 'DEPRECATED', 'SUSPENDED', 'MIGRATING', 'RETIRED')),
    validation_findings jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(validation_findings) = 'array'),
    revision            bigint NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          timestamptz NOT NULL,
    created_by          text NOT NULL,
    updated_at          timestamptz,
    updated_by          text,
    activated_at        timestamptz,
    activated_by        text,
    create_idempotency_key text,
    create_request_hash    text,
    CHECK (parent_market_id IS NULL OR parent_market_id <> market_id),
    CHECK ((updated_at IS NULL) = (updated_by IS NULL)),
    CHECK ((activated_at IS NULL) = (activated_by IS NULL)),
    -- VALIDATED means every validation rule holds.
    CHECK (status <> 'VALIDATED' OR jsonb_array_length(validation_findings) = 0),
    -- Only activation reaches ACTIVE, and never by the market's creator.
    CHECK (status IN ('DRAFT', 'VALIDATED') OR activated_by IS NOT NULL),
    CHECK (activated_by IS NULL OR activated_by <> created_by),
    CHECK ((create_idempotency_key IS NULL) = (create_request_hash IS NULL))
);

-- Create replays: one market per creator and Idempotency-Key.
CREATE UNIQUE INDEX market_registry_create_idempotency_uniq
    ON market.registry(created_by, create_idempotency_key) WHERE create_idempotency_key IS NOT NULL;
CREATE INDEX market_registry_owner_idx ON market.registry(owner_tenant_id);
