-- Gate: retire the legacy capability.tenant_capability boolean-enablement
-- table (Gate P0's classification, docs/reconciliation/phase-0-architecture-
-- inventory-and-lock.md, tracked as #72's "Eventually" list;
-- ADR-BCP-003 SS9-19).
--
-- Migration 000029 introduced capability.capability_grant as the real,
-- provenance-tracked entitlement model and deliberately left
-- tenant_capability in place, unmigrated, because no backfill plan existed
-- yet. No Go code reads or writes capability.tenant_capability (confirmed
-- by repository-wide search: the table is referenced only by migrations
-- 000008/000024/000029 and by the two Gate P0/audit docs updated alongside
-- this migration) and no production data exists for it (Technical
-- Specification SS55), so -- consistent with how 000033 handled
-- product_subscriptions and 000034 handled capability_binding.scope_id --
-- it is dropped outright rather than accumulating a compensating migration.

DROP INDEX IF EXISTS capability.tenant_capability_tenant_idx;

DROP TABLE IF EXISTS capability.tenant_capability;
