-- Target path: baobab-platform/baobab-cp/migrations/<timestamp>_adr_bcp_018_organisation.up.sql
--
-- ADR-BCP-018 — Forward-only migration for Organisation, relationship and
-- tenant mapping tables.
--
-- Rules:
--   - Do NOT rewrite historical migrations.
--   - Do NOT drop or rename Tenant.legal_entity_id (compatibility projection).
--   - Do NOT destroy existing BUYER_ORGANISATION / SUPPLIER_ORGANISATION rows.
--   - Backfill of tenant_legal_entity_mapping from tenant.legal_entity_id is
--     non-destructive and idempotent.
--
-- Schema notes:
--   Tables live alongside existing registry.canonical_entity and related
--   registry tables. Adjust schema prefix if the project uses a different
--   search_path. Column types follow existing CP conventions (text ids,
--   timestamptz, jsonb metadata).

BEGIN;

-- ---------------------------------------------------------------------------
-- organisation_profile: extension of canonical_entity
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS registry.organisation_profile (
    canonical_entity_id   text PRIMARY KEY,
    display_name          text NOT NULL,
    official_name         text,
    trading_names         jsonb NOT NULL DEFAULT '[]'::jsonb,
    organisation_form     text,
    jurisdiction          text,
    verification_state    text NOT NULL,
    source_authority      text NOT NULL,
    status                text NOT NULL,
    effective_from        timestamptz NOT NULL,
    effective_to          timestamptz,
    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT organisation_profile_verification_state_check
        CHECK (verification_state IN (
            'UNVERIFIED', 'PENDING_REVIEW', 'VERIFIED',
            'CONFLICTED', 'REJECTED', 'EXPIRED'
        ))
);

COMMENT ON TABLE registry.organisation_profile IS
    'ADR-BCP-018: Organisation profile anchored to canonical_entity. Identity is canonical_entity_id.';

-- ---------------------------------------------------------------------------
-- legal_entity_profile: runtime legal person attached to organisation
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS registry.legal_entity_profile (
    legal_entity_id                text PRIMARY KEY,
    organisation_id                text NOT NULL,
    legal_name                     text NOT NULL,
    jurisdiction_of_incorporation  text,
    legal_status                   text NOT NULL,
    source_authority               text NOT NULL,
    verification_state             text NOT NULL,
    effective_from                 timestamptz NOT NULL,
    effective_to                   timestamptz,
    evidence_references            jsonb NOT NULL DEFAULT '[]'::jsonb,
    metadata                       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                     timestamptz NOT NULL DEFAULT now(),
    updated_at                     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT legal_entity_profile_verification_state_check
        CHECK (verification_state IN (
            'UNVERIFIED', 'PENDING_REVIEW', 'VERIFIED',
            'CONFLICTED', 'REJECTED', 'EXPIRED'
        ))
);

CREATE INDEX IF NOT EXISTS legal_entity_profile_organisation_id_idx
    ON registry.legal_entity_profile (organisation_id);

COMMENT ON TABLE registry.legal_entity_profile IS
    'ADR-BCP-018: Runtime LegalEntityProfile. First-party ids governed by Shared registry; external ids Control-Plane-issued.';

-- ---------------------------------------------------------------------------
-- corporate_relationship
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS registry.corporate_relationship (
    id                      text PRIMARY KEY,
    source_organisation_id  text NOT NULL,
    target_organisation_id  text NOT NULL,
    relationship_type       text NOT NULL,
    ownership_percentage    numeric(5,2),
    verification_state      text NOT NULL,
    status                  text NOT NULL,
    effective_from          timestamptz NOT NULL,
    effective_to            timestamptz,
    evidence_references     jsonb NOT NULL DEFAULT '[]'::jsonb,
    source_authority        text NOT NULL,
    metadata                jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT corporate_relationship_verification_state_check
        CHECK (verification_state IN (
            'UNVERIFIED', 'PENDING_REVIEW', 'VERIFIED',
            'CONFLICTED', 'REJECTED', 'EXPIRED'
        )),
    CONSTRAINT corporate_relationship_status_check
        CHECK (status IN ('ACTIVE', 'SUSPENDED', 'RETIRED', 'CONFLICTED'))
);

CREATE INDEX IF NOT EXISTS corporate_relationship_source_idx
    ON registry.corporate_relationship (source_organisation_id);
CREATE INDEX IF NOT EXISTS corporate_relationship_target_idx
    ON registry.corporate_relationship (target_organisation_id);

COMMENT ON TABLE registry.corporate_relationship IS
    'ADR-BCP-018: Ownership/control edges. NOT an authorization boundary. Fail closed when not VERIFIED+ACTIVE.';

-- ---------------------------------------------------------------------------
-- corporate_group + membership
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS registry.corporate_group (
    id                   text PRIMARY KEY,
    display_name         text NOT NULL,
    root_organisation_id text NOT NULL,
    status               text NOT NULL,
    effective_from       timestamptz NOT NULL,
    effective_to         timestamptz,
    metadata             jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS registry.corporate_group_membership (
    id                  text PRIMARY KEY,
    corporate_group_id  text NOT NULL REFERENCES registry.corporate_group (id),
    organisation_id     text NOT NULL,
    membership_type     text NOT NULL,
    status              text NOT NULL,
    effective_from      timestamptz NOT NULL,
    effective_to        timestamptz,
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS corporate_group_membership_org_idx
    ON registry.corporate_group_membership (organisation_id);

-- ---------------------------------------------------------------------------
-- platform_relationship, platform_account, membership
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS registry.platform_relationship (
    id                   text PRIMARY KEY,
    organisation_id      text NOT NULL,
    relationship_type    text NOT NULL,
    verification_state   text NOT NULL,
    status               text NOT NULL,
    effective_from       timestamptz NOT NULL,
    effective_to         timestamptz,
    evidence_references  jsonb NOT NULL DEFAULT '[]'::jsonb,
    source_authority     text NOT NULL,
    metadata             jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT platform_relationship_verification_state_check
        CHECK (verification_state IN (
            'UNVERIFIED', 'PENDING_REVIEW', 'VERIFIED',
            'CONFLICTED', 'REJECTED', 'EXPIRED'
        ))
);

CREATE INDEX IF NOT EXISTS platform_relationship_org_idx
    ON registry.platform_relationship (organisation_id);

CREATE TABLE IF NOT EXISTS registry.platform_account (
    id                      text PRIMARY KEY,
    display_name            text NOT NULL,
    primary_organisation_id text,
    status                  text NOT NULL,
    effective_from          timestamptz NOT NULL,
    effective_to            timestamptz,
    billing_reference       text,
    metadata                jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS registry.platform_account_membership (
    id                   text PRIMARY KEY,
    platform_account_id  text NOT NULL REFERENCES registry.platform_account (id),
    member_type          text NOT NULL,
    member_id            text NOT NULL,
    role                 text,
    status               text NOT NULL,
    effective_from       timestamptz NOT NULL,
    effective_to         timestamptz,
    metadata             jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT platform_account_membership_member_type_check
        CHECK (member_type IN ('ORGANISATION', 'TENANT'))
);

CREATE INDEX IF NOT EXISTS platform_account_membership_member_idx
    ON registry.platform_account_membership (member_type, member_id);

-- ---------------------------------------------------------------------------
-- Explicit tenant mappings
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS registry.tenant_organisation_mapping (
    id                text PRIMARY KEY,
    tenant_id         text NOT NULL,
    organisation_id   text NOT NULL,
    is_default        boolean NOT NULL DEFAULT false,
    status            text NOT NULL,
    effective_from    timestamptz NOT NULL,
    effective_to      timestamptz,
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS tenant_organisation_mapping_tenant_idx
    ON registry.tenant_organisation_mapping (tenant_id);

CREATE TABLE IF NOT EXISTS registry.tenant_legal_entity_mapping (
    id                text PRIMARY KEY,
    tenant_id         text NOT NULL,
    legal_entity_id   text NOT NULL,
    is_default        boolean NOT NULL DEFAULT false,
    status            text NOT NULL,
    effective_from    timestamptz NOT NULL,
    effective_to      timestamptz,
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS tenant_legal_entity_mapping_tenant_idx
    ON registry.tenant_legal_entity_mapping (tenant_id);

-- At most one active default legal-entity mapping per tenant (partial unique).
CREATE UNIQUE INDEX IF NOT EXISTS tenant_legal_entity_mapping_default_uidx
    ON registry.tenant_legal_entity_mapping (tenant_id)
    WHERE is_default = true AND status = 'ACTIVE' AND effective_to IS NULL;

-- ---------------------------------------------------------------------------
-- Non-destructive backfill: project Tenant.legal_entity_id into default mapping
-- ---------------------------------------------------------------------------
-- Adjust the tenant table/schema name to match the live Control Plane schema.
-- This block is written to be safe if the source column is already empty or
-- if mapping rows already exist (NOT EXISTS guard).

INSERT INTO registry.tenant_legal_entity_mapping (
    id,
    tenant_id,
    legal_entity_id,
    is_default,
    status,
    effective_from,
    metadata
)
SELECT
    'tlem_backfill_' || t.tenant_id,
    t.tenant_id,
    t.legal_entity_id,
    true,
    'ACTIVE',
    COALESCE(t.provisioned_at, now()),
    jsonb_build_object('source', 'adr-bcp-018-backfill')
FROM registry.tenant t
WHERE t.legal_entity_id IS NOT NULL
  AND t.legal_entity_id <> ''
  AND NOT EXISTS (
      SELECT 1
      FROM registry.tenant_legal_entity_mapping m
      WHERE m.tenant_id = t.tenant_id
        AND m.is_default = true
        AND m.status = 'ACTIVE'
        AND m.effective_to IS NULL
  );

COMMIT;
