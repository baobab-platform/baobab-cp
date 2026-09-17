-- Target path:
-- internal/store/postgres/migrations/000041_market_assignment_legacy_status_backfill_fix.sql
--
-- Corrective, forward-only fix for 000039's backfill. 000039 added
-- market.market_assignment.status with DEFAULT 'PENDING', which retroactively
-- gave every pre-existing row (created before this governance model existed)
-- status='PENDING' -- and MarketAssignment.IsOperationalAt()/
-- MarketParticipationService.RequireOperational() require status='ACTIVE'
-- for a participation to be treated as operational. A tenant's market
-- participation that was already effective (created, within its effective
-- period) before Gate ZB-02 would silently stop being treated as
-- operational the moment 000039 ran -- exactly the backfill hazard the
-- ZuriBeans Go-Live spec warns against ("introducing status = PENDING for
-- every existing participation could unintentionally deactivate existing
-- behavior. Explicitly design backfill semantics").
--
-- A brand new row created by the ZB-02 application code always sets status
-- explicitly (MarketAssignment.Validate() requires a valid, non-empty
-- Status via ValidateMarketParticipationGovernance) -- the column DEFAULT
-- is therefore only ever actually used by 000039's own ADD COLUMN backfill,
-- never by any Go INSERT path. Retargeting that backfill to ACTIVE for the
-- rows it created is safe and does not change any future insert's
-- behavior.
--
-- This only touches rows 000039 itself just backfilled (source='MIGRATION'
-- AND status='PENDING'): a row an operator or a later migration
-- deliberately set to PENDING for a real reason is left alone.
UPDATE market.market_assignment
SET status = 'ACTIVE',
    updated_at = now()
WHERE source = 'MIGRATION'
  AND status = 'PENDING';
