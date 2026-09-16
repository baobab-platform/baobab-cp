-- Gate: repoint capability.capability_binding.scope_id at
-- capability.capability_scope instead of mapping.mapping_scope (Gate P0's
-- classification, docs/reconciliation/phase-0-architecture-inventory-and-
-- lock.md, tracked as #74; ADR-SHARED-007 SS25).
--
-- Migration 000029 introduced capability.capability_scope precisely because
-- it is NOT mapping.mapping_scope -- the two share dimension vocabulary but
-- are evaluated by different resolvers for different purposes -- but
-- deliberately left capability_binding's own scope_id pointed at the old
-- table, since no backfill/compatibility plan existed yet. That plan is:
-- no production data exists for this table (Technical Specification SS55),
-- so the column is repointed outright rather than accumulating a
-- compensating migration.
--
-- capability_binding_primary_excl (the GiST exclusion constraint proving
-- at most one ACTIVE PRIMARY binding per capability/scope/period) is
-- unaffected: it constrains scope_id's own column values, not what table
-- they reference, so it continues to hold exactly as before once the FK
-- target changes.

ALTER TABLE capability.capability_binding
    DROP CONSTRAINT IF EXISTS capability_binding_scope_id_fkey;

ALTER TABLE capability.capability_binding
    ADD CONSTRAINT capability_binding_scope_id_fkey
    FOREIGN KEY (scope_id) REFERENCES capability.capability_scope(scope_id) ON DELETE CASCADE;
