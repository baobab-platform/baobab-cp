-- Target path: internal/store/postgres/migrations/000045_adr_bcp_018_organisation.sql
--
-- ADR-BCP-018 — Organisation profiles, corporate/platform relationships,
-- and explicit tenant mappings.
--
-- Conventions: single SQL file, registered in canonicalMigrationNames.
-- Tables live in registry.* alongside canonical_entity.
-- Tenants table is public.tenants (migration 000019); legal_entities is public.
-- canonical_entity_id is uuid (migration 000002).
--
-- Does not rewrite historical migrations. Tenant.legal_entity_id remains the
-- singular compatibility projection of the default TenantLegalEntityMapping.

-- Organisation profile (extends CanonicalEntity; identity is canonical_entity_id)
CREATE TABLE IF NOT EXISTS registry.organisation_profile (
    canonical_entity_id   uuid PRIMARY KEY
        REFERENCES registry.canonical_entity(canonical_entity_id),
    display_name          text NOT NULL,
    official_name         text,
    trading_names         jsonb NOT NULL DEFAULT '[]'::jsonb,
    organisation_form     text,
    jurisdiction          text,
    verification_state    text NOT NULL DEFAULT 'UNVERIFIED',
    source_authority      text NOT NULL,
    status                text NOT NULL DEFAULT 'ACTIVE',
    effective_from        timestamptz NOT NULL,
    effective_to          timestamptz,
    identifiers           jsonb NOT NULL DEFAULT '[]'::jsonb,
    addresses             jsonb NOT NULL DEFAULT '[]'::jsonb,
    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (verification_state IN (
        'UNVERIFIED','PENDING_REVIEW','VERIFIED','CONFLICTED','REJECTED','EXPIRED'
    )),
    CHECK (status IN (
        'DRAFT','VALIDATED','ACTIVE','DEPRECATED','SUSPENDED','MIGRATING','QUARANTINED','RETIRED'
    ))
);

-- Legal entity profile (runtime; first-party ids reconcile to Shared registry)
CREATE TABLE IF NOT EXISTS registry.legal_entity_profile (
    legal_entity_id                 varchar(63) PRIMARY KEY
        REFERENCES legal_entities(legal_entity_id),
    organisation_id                 uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    legal_name                      text NOT NULL,
    jurisdiction_of_incorporation   text,
    registration_identifiers        jsonb NOT NULL DEFAULT '[]'::jsonb,
    incorporation_date              date,
    legal_status                    text NOT NULL DEFAULT 'ACTIVE',
    source_authority                text NOT NULL,
    verification_state              text NOT NULL DEFAULT 'UNVERIFIED',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    evidence_references             jsonb NOT NULL DEFAULT '[]'::jsonb,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (verification_state IN (
        'UNVERIFIED','PENDING_REVIEW','VERIFIED','CONFLICTED','REJECTED','EXPIRED'
    ))
);

CREATE INDEX IF NOT EXISTS legal_entity_profile_organisation_idx
    ON registry.legal_entity_profile(organisation_id);

-- Corporate relationship (ADR vocabulary only)
CREATE TABLE IF NOT EXISTS registry.corporate_relationship (
    corporate_relationship_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_organisation_id      uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    target_organisation_id      uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    relationship_type           text NOT NULL,
    ownership_percentage        numeric(5,2),
    control_basis               text,
    direct_or_derived           text,
    verification_state          text NOT NULL DEFAULT 'UNVERIFIED',
    status                      text NOT NULL DEFAULT 'ACTIVE',
    effective_from              timestamptz NOT NULL,
    effective_to                timestamptz,
    source_authority            text NOT NULL,
    evidence_references         jsonb NOT NULL DEFAULT '[]'::jsonb,
    verified_by                 text,
    verified_at                 timestamptz,
    classification              text,
    metadata                    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CHECK (relationship_type IN (
        'OWNS','CONTROLS','BRANCH_OF','AFFILIATE_OF','JOINT_VENTURE_WITH','SUCCESSOR_OF'
    )),
    CHECK (ownership_percentage IS NULL
        OR (ownership_percentage >= 0 AND ownership_percentage <= 100)),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (source_organisation_id <> target_organisation_id),
    CHECK (verification_state IN (
        'UNVERIFIED','PENDING_REVIEW','VERIFIED','CONFLICTED','REJECTED','EXPIRED'
    )),
    CHECK (status IN ('ACTIVE','SUSPENDED','RETIRED','CONFLICTED')),
    CHECK (direct_or_derived IS NULL OR direct_or_derived IN ('DIRECT','DERIVED'))
);

CREATE INDEX IF NOT EXISTS corporate_relationship_source_idx
    ON registry.corporate_relationship(source_organisation_id, effective_from);
CREATE INDEX IF NOT EXISTS corporate_relationship_target_idx
    ON registry.corporate_relationship(target_organisation_id, effective_from);

-- Corporate group (root optional — graphs need not have a unique root)
CREATE TABLE IF NOT EXISTS registry.corporate_group (
    corporate_group_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name            text NOT NULL,
    root_organisation_id    uuid
        REFERENCES registry.canonical_entity(canonical_entity_id),
    status                  text NOT NULL DEFAULT 'ACTIVE',
    effective_from          timestamptz NOT NULL,
    effective_to            timestamptz,
    metadata                jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('ACTIVE','SUSPENDED','RETIRED'))
);

-- Membership carries derivation lineage (not a second ownership database)
CREATE TABLE IF NOT EXISTS registry.corporate_group_membership (
    corporate_group_membership_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    corporate_group_id              uuid NOT NULL
        REFERENCES registry.corporate_group(corporate_group_id) ON DELETE CASCADE,
    organisation_id                 uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    membership_type                 text NOT NULL,
    status                          text NOT NULL DEFAULT 'ACTIVE',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    basis_relationship_ids          uuid[] NOT NULL,
    derived_at                      timestamptz NOT NULL,
    derivation_version              text NOT NULL,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (cardinality(basis_relationship_ids) >= 1),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('ACTIVE','SUSPENDED','RETIRED')),
    CHECK (membership_type IN ('ROOT','SUBSIDIARY','AFFILIATE','JOINT_VENTURE','OTHER'))
);

CREATE INDEX IF NOT EXISTS corporate_group_membership_org_idx
    ON registry.corporate_group_membership(organisation_id);

-- Platform relationship (multiple concurrent types per organisation allowed)
CREATE TABLE IF NOT EXISTS registry.platform_relationship (
    platform_relationship_id    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    platform_id                 text NOT NULL,
    organisation_id             uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    relationship_type           text NOT NULL,
    verification_state          text NOT NULL DEFAULT 'UNVERIFIED',
    status                      text NOT NULL DEFAULT 'ACTIVE',
    effective_from              timestamptz NOT NULL,
    effective_to                timestamptz,
    basis_relationship_id       uuid
        REFERENCES registry.corporate_relationship(corporate_relationship_id),
    admission_decision_id       text,
    source_authority            text NOT NULL,
    evidence_references         jsonb NOT NULL DEFAULT '[]'::jsonb,
    verified_by                 text,
    verified_at                 timestamptz,
    metadata                    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CHECK (relationship_type IN (
        'PLATFORM_OWNER','PLATFORM_OPERATOR','PLATFORM_GROUP_AFFILIATE',
        'PLATFORM_PARTNER','EXTERNAL_CLIENT','MANAGED_ENTITY'
    )),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (verification_state IN (
        'UNVERIFIED','PENDING_REVIEW','VERIFIED','CONFLICTED','REJECTED','EXPIRED'
    )),
    CHECK (status IN ('ACTIVE','SUSPENDED','RETIRED','PENDING'))
);

CREATE INDEX IF NOT EXISTS platform_relationship_org_idx
    ON registry.platform_relationship(organisation_id, effective_from);

CREATE UNIQUE INDEX IF NOT EXISTS platform_relationship_active_type_uniq
    ON registry.platform_relationship(organisation_id, platform_id, relationship_type)
    WHERE status = 'ACTIVE' AND effective_to IS NULL;

-- Platform account (not a tenant; not an authorization boundary)
CREATE TABLE IF NOT EXISTS registry.platform_account (
    platform_account_id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name                text NOT NULL,
    primary_organisation_id     uuid
        REFERENCES registry.canonical_entity(canonical_entity_id),
    status                      text NOT NULL DEFAULT 'ACTIVE',
    effective_from              timestamptz NOT NULL,
    effective_to                timestamptz,
    billing_reference           text,
    metadata                    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('ACTIVE','SUSPENDED','RETIRED','PENDING'))
);

CREATE TABLE IF NOT EXISTS registry.platform_account_membership (
    platform_account_membership_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    platform_account_id             uuid NOT NULL
        REFERENCES registry.platform_account(platform_account_id) ON DELETE CASCADE,
    member_type                     text NOT NULL,
    member_id                       text NOT NULL,
    role                            text,
    status                          text NOT NULL DEFAULT 'ACTIVE',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (member_type IN ('ORGANISATION','TENANT')),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('ACTIVE','SUSPENDED','RETIRED'))
);

-- Tenant ↔ Organisation mapping
CREATE TABLE IF NOT EXISTS registry.tenant_organisation_mapping (
    tenant_organisation_mapping_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                       varchar(63) NOT NULL
        REFERENCES tenants(tenant_id),
    organisation_id                 uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    mapping_role                    text NOT NULL,
    is_default                      boolean NOT NULL DEFAULT false,
    status                          text NOT NULL DEFAULT 'ACTIVE',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    provenance                      text NOT NULL,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (mapping_role IN (
        'PRIMARY_ORGANISATION','OPERATING_ORGANISATION','ADDITIONAL_ORGANISATION','DEFAULT'
    )),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('ACTIVE','SUSPENDED','RETIRED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS tenant_organisation_mapping_default_uniq
    ON registry.tenant_organisation_mapping(tenant_id)
    WHERE is_default AND status = 'ACTIVE' AND effective_to IS NULL;

CREATE INDEX IF NOT EXISTS tenant_organisation_mapping_tenant_idx
    ON registry.tenant_organisation_mapping(tenant_id);

-- Tenant ↔ Legal entity mapping (default row projects Tenant.legal_entity_id)
CREATE TABLE IF NOT EXISTS registry.tenant_legal_entity_mapping (
    tenant_legal_entity_mapping_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                       varchar(63) NOT NULL
        REFERENCES tenants(tenant_id),
    legal_entity_id                 varchar(63) NOT NULL
        REFERENCES legal_entities(legal_entity_id),
    mapping_role                    text NOT NULL,
    is_default                      boolean NOT NULL DEFAULT false,
    status                          text NOT NULL DEFAULT 'ACTIVE',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    provenance                      text NOT NULL,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (mapping_role IN ('DEFAULT','PRIMARY','ADDITIONAL')),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('ACTIVE','SUSPENDED','RETIRED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS tenant_legal_entity_mapping_default_uniq
    ON registry.tenant_legal_entity_mapping(tenant_id)
    WHERE is_default AND status = 'ACTIVE' AND effective_to IS NULL;

CREATE INDEX IF NOT EXISTS tenant_legal_entity_mapping_tenant_idx
    ON registry.tenant_legal_entity_mapping(tenant_id);

-- Backfill default tenant_legal_entity_mapping from existing tenants
INSERT INTO registry.tenant_legal_entity_mapping (
    tenant_id, legal_entity_id, mapping_role, is_default, status,
    effective_from, provenance
)
SELECT
    t.tenant_id,
    t.legal_entity_id,
    'DEFAULT',
    true,
    'ACTIVE',
    t.created_at,
    'migration-000045-backfill'
FROM tenants t
WHERE NOT EXISTS (
    SELECT 1 FROM registry.tenant_legal_entity_mapping m
    WHERE m.tenant_id = t.tenant_id AND m.is_default AND m.status = 'ACTIVE'
);
