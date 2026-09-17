-- Target path:
-- internal/store/postgres/migrations/000039_market_participation_governance.sql
--
-- ZB-02 / Programme Gate P7 governance completion for ADR-BCP-011
-- MarketParticipation, currently persisted as market.market_assignment.
--
-- Migration 000037 explicitly deferred status/source/policy_version to P7.
-- This migration supplies those fields without renaming the established table.
--
-- If the separately supplied TradeLane pack already occupies migration 000039,
-- renumber this file before applying. Never ship duplicate migration numbers.

ALTER TABLE market.market_assignment
    ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'PENDING',
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'MIGRATION',
    ADD COLUMN IF NOT EXISTS source_reference text,
    ADD COLUMN IF NOT EXISTS policy_version text NOT NULL DEFAULT '1',
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE market.market_assignment
    ADD CONSTRAINT market_assignment_status_chk
        CHECK (status IN ('PENDING', 'ACTIVE', 'SUSPENDED', 'RETIRED')),
    ADD CONSTRAINT market_assignment_source_chk
        CHECK (source IN ('PROVISIONING', 'POLICY', 'ADMIN', 'MIGRATION')),
    ADD CONSTRAINT market_assignment_policy_version_nonempty_chk
        CHECK (length(btrim(policy_version)) > 0);

-- Runtime resolution frequently asks for the one assignment covering a market
-- at a point in time. The pre-existing exclusion constraint remains the
-- authority preventing overlapping periods for the same tenant+market.
CREATE INDEX IF NOT EXISTS market_assignment_tenant_market_status_idx
    ON market.market_assignment (tenant_id, market_id, status);

-- Existing rows predate governance metadata. MIGRATION records provenance
-- truthfully; they should be explicitly reconciled before production logic
-- assumes they were created by the provisioning engine.
UPDATE market.market_assignment
SET source = 'MIGRATION',
    policy_version = COALESCE(NULLIF(policy_version, ''), '1'),
    updated_at = now()
WHERE source = 'MIGRATION';
