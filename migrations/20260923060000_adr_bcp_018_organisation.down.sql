-- Target path: baobab-platform/baobab-cp/migrations/<timestamp>_adr_bcp_018_organisation.down.sql
--
-- ADR-BCP-018 — Down migration (for dev/rollback only).
-- Does NOT restore or rewrite historical migrations.
-- Does NOT touch tenant.legal_entity_id.

BEGIN;

DROP TABLE IF EXISTS registry.platform_account_membership;
DROP TABLE IF EXISTS registry.platform_account;
DROP TABLE IF EXISTS registry.platform_relationship;
DROP TABLE IF EXISTS registry.corporate_group_membership;
DROP TABLE IF EXISTS registry.corporate_group;
DROP TABLE IF EXISTS registry.corporate_relationship;
DROP TABLE IF EXISTS registry.tenant_organisation_mapping;
DROP TABLE IF EXISTS registry.tenant_legal_entity_mapping;
DROP TABLE IF EXISTS registry.legal_entity_profile;
DROP TABLE IF EXISTS registry.organisation_profile;

COMMIT;
