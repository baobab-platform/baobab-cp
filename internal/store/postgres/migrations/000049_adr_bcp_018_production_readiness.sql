-- ADR-BCP-018 gate ORG-16 — production readiness.
--
-- Indexes for the section 173 query paths that had none:
--   * live affiliates resting on a corporate relationship (divestiture
--     review, CorporateChangeReviewer.Review);
--   * tenant legal-entity mappings of a legal entity (audit lineage);
--   * group -> members and platform account -> members as of a date
--     (the live unique indexes are partial, so they cannot serve history);
--   * organisation -> platform account memberships, tenant mappings and
--     counterparty roles in every status (audit lineage, section 131);
--   * the platform owners of a platform (INTERNAL eligibility).
-- Every other section 173 path is served by an existing index; the
-- ORG-16 plan-shape test proves each path avoids sequential scans.
--
-- Audit immutability (ADR-BCP-008 section 37): audit_events is
-- append-only. REVOKE ... FROM PUBLIC (migration 000019) does not bind the
-- table owner, so a trigger now refuses UPDATE, DELETE and TRUNCATE for
-- everyone. Retention is policy-driven (section 38): an approved purge is
-- a governed maintenance change that disables this trigger explicitly and
-- is itself visible in the DDL log; see
-- docs/runbooks/adr-bcp-018-organisation-operations.md.

CREATE INDEX IF NOT EXISTS platform_relationship_basis_idx
    ON registry.platform_relationship(basis_relationship_id)
    WHERE basis_relationship_id IS NOT NULL AND status IN ('PENDING','ACTIVE','SUSPENDED');

CREATE INDEX IF NOT EXISTS tenant_legal_entity_mapping_legal_entity_idx
    ON registry.tenant_legal_entity_mapping(legal_entity_id);

CREATE INDEX IF NOT EXISTS corporate_group_membership_group_idx
    ON registry.corporate_group_membership(corporate_group_id, effective_from);

CREATE INDEX IF NOT EXISTS platform_account_membership_account_idx
    ON registry.platform_account_membership(platform_account_id, effective_from);

CREATE INDEX IF NOT EXISTS platform_account_membership_organisation_idx
    ON registry.platform_account_membership(organisation_id);

CREATE INDEX IF NOT EXISTS tenant_organisation_mapping_organisation_idx
    ON registry.tenant_organisation_mapping(organisation_id);

CREATE INDEX IF NOT EXISTS counterparty_role_organisation_idx
    ON registry.counterparty_role(organisation_id);

CREATE INDEX IF NOT EXISTS platform_relationship_owner_idx
    ON registry.platform_relationship(platform_id, effective_from)
    WHERE relationship_type = 'PLATFORM_OWNER';

CREATE OR REPLACE FUNCTION audit_events_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only (ADR-BCP-008 section 37): % refused', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

DROP TRIGGER IF EXISTS audit_events_no_update_or_delete ON audit_events;
CREATE TRIGGER audit_events_no_update_or_delete
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_append_only();

DROP TRIGGER IF EXISTS audit_events_no_truncate ON audit_events;
CREATE TRIGGER audit_events_no_truncate
    BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_events_append_only();
