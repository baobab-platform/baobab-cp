-- Target path: internal/store/postgres/migrations/000040_trade_lanes.sql
--
-- TradeLane persistence for ADR-BCP-011 / Gate ZB-02.
-- Shared remains the contract authority; this table is baobab-cp's storage.
--
-- market.market.market_id is uuid (migration 000006), so origin/destination
-- reference it directly. tenant_id is NOT a uuid anywhere else in this
-- schema -- market.market_assignment.tenant_id and tenants.tenant_id
-- (migration 000006/000019) are both text/varchar, since tenant IDs are
-- caller-assigned slugs (e.g. "tn_zuribeans"), not database-generated
-- UUIDs. This table follows that same convention rather than the uuid this
-- file originally sketched before checking the real schema.

CREATE TABLE market.trade_lane (
    trade_lane_id VARCHAR(63) PRIMARY KEY,
    tenant_id text NOT NULL,
    origin_market_id UUID NOT NULL REFERENCES market.market(market_id),
    destination_market_id UUID NOT NULL REFERENCES market.market(market_id),
    direction VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    permitted_capability_keys TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT trade_lane_id_format_chk
        CHECK (trade_lane_id ~ '^tlane_[a-z0-9]+$'),

    -- A TradeLane is explicitly cross-market. Domestic movement belongs to
    -- other logistics/fulfilment concepts, not this resource.
    CONSTRAINT trade_lane_markets_differ_chk
        CHECK (origin_market_id <> destination_market_id),

    CONSTRAINT trade_lane_direction_chk
        CHECK (direction IN ('IMPORT', 'EXPORT', 'CROSS_MARKET')),

    CONSTRAINT trade_lane_status_chk
        CHECK (status IN ('ACTIVE', 'SUSPENDED', 'RETIRED')),

    -- Prevent duplicate semantic routes for the same tenant. Reverse
    -- direction remains a distinct route and is allowed.
    CONSTRAINT trade_lane_unique_route
        UNIQUE (tenant_id, origin_market_id, destination_market_id, direction)
);

CREATE INDEX trade_lane_tenant_status_idx
    ON market.trade_lane (tenant_id, status);

CREATE INDEX trade_lane_origin_idx
    ON market.trade_lane (tenant_id, origin_market_id);

CREATE INDEX trade_lane_destination_idx
    ON market.trade_lane (tenant_id, destination_market_id);
