-- Gate P0's classification (docs/reconciliation/phase-0-architecture-
-- inventory-and-lock.md) found market.market_assignment (migration 000006)
-- proves a tenant may participate in a market at all, but cannot represent
-- what it may do there -- the entire point of ADR-BCP-011: "a market SHALL
-- NOT be assigned a permanent architectural role such as SOURCE MARKET/
-- EXPORT MARKET"; the same market may independently hold any subset of
-- SOURCING/PROCUREMENT/SELLING/IMPORTING/EXPORTING/WAREHOUSING/
-- DISTRIBUTION/FULFILMENT/etc (ADR-BCP-011 §6).
--
-- This migration closes that gap with a child table (ADR-BCP-011 §81's
-- suggested schema) rather than a fixed set of boolean columns, since the
-- capability taxonomy is explicitly open to "controlled registry evolution"
-- (§6) and a new value must never require a schema migration. It also adds
-- the optional legal_entity_id ADR-BCP-011 §5's canonical MarketParticipation
-- concept names ("Tenant + Legal Entity where applicable + Market + ...").
--
-- Out of scope for this migration (Programme Gate P7/P10, not this
-- capability-flag remodel): status/source/policy_version governance
-- fields, TradeLane, provisioning, readiness and reconciliation.

ALTER TABLE market.market_assignment
    ADD COLUMN IF NOT EXISTS legal_entity_id text;

CREATE TABLE IF NOT EXISTS market.market_participation_capability (
    market_assignment_id uuid NOT NULL REFERENCES market.market_assignment(market_assignment_id) ON DELETE CASCADE,
    capability            text NOT NULL,
    PRIMARY KEY (market_assignment_id, capability),
    CHECK (capability IN (
        'LEGAL_PRESENCE', 'SOURCING', 'PROCUREMENT', 'SELLING', 'IMPORTING',
        'EXPORTING', 'WAREHOUSING', 'DISTRIBUTION', 'FULFILMENT', 'PROCESSING', 'TRANSIT'
    ))
);

CREATE INDEX IF NOT EXISTS market_participation_capability_capability_idx
    ON market.market_participation_capability(capability);
