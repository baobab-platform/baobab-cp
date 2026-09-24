-- ADR-BCP-018 — Organisation profiles, corporate/platform relationships,
-- platform accounts and explicit tenant mappings.
--
-- Contract authority: baobab-platform/shared contracts/organisation/v1.
-- Rows are keyed by uuid; the API renders them as opaque contract ids
-- (crel_, cgrp_, cgm_, prel_, pacct_, pam_, tom_, tlem_ + 32 hex digits).
--
-- Invariants enforced here rather than only in Go (ADR-BCP-018 §117-119):
--   * VERIFIED rows carry evidence and a verification timestamp;
--   * DERIVED corporate facts carry lineage;
--   * PLATFORM_GROUP_AFFILIATE carries its corporate basis;
--   * at most one live row per natural key, so retries converge instead of
--     duplicating (live = PENDING, ACTIVE or SUSPENDED; ENDED/RETIRED rows
--     are history and stay queryable).
--
-- tenants.legal_entity_id remains the compatibility projection of the
-- tenant's live DEFAULT tenant_legal_entity_mapping.

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
    evidence_references   jsonb NOT NULL DEFAULT '[]'::jsonb,
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

CREATE TABLE IF NOT EXISTS registry.legal_entity_profile (
    legal_entity_id                 varchar(63) PRIMARY KEY
        REFERENCES legal_entities(legal_entity_id),
    organisation_id                 uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    legal_name                      text NOT NULL,
    jurisdiction_of_incorporation   text,
    registration_identifiers        jsonb NOT NULL DEFAULT '[]'::jsonb,
    incorporation_date              date,
    legal_status                    text NOT NULL DEFAULT 'UNKNOWN',
    source_authority                text NOT NULL,
    verification_state              text NOT NULL DEFAULT 'UNVERIFIED',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    evidence_references             jsonb NOT NULL DEFAULT '[]'::jsonb,
    verified_by                     text,
    verified_at                     timestamptz,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (legal_status IN (
        'ACTIVE','DORMANT','IN_ADMINISTRATION','IN_LIQUIDATION','DISSOLVED','UNKNOWN'
    )),
    CHECK (verification_state IN (
        'UNVERIFIED','PENDING_REVIEW','VERIFIED','CONFLICTED','REJECTED','EXPIRED'
    )),
    CONSTRAINT legal_entity_profile_verified_has_evidence CHECK (
        verification_state <> 'VERIFIED'
        OR (jsonb_array_length(evidence_references) >= 1 AND verified_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS legal_entity_profile_organisation_idx
    ON registry.legal_entity_profile(organisation_id);

-- Directed corporate facts: source RELATIONSHIP_TYPE target ("A OWNS B").
-- Platform-scoped: no tenant_id. Cycles are not rejected (real cross-holdings
-- exist); resolvers must treat them as ambiguity, never as a unique parent.
CREATE TABLE IF NOT EXISTS registry.corporate_relationship (
    corporate_relationship_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_organisation_id      uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    target_organisation_id      uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    relationship_type           text NOT NULL,
    ownership_percentage        numeric(5,2),
    control_basis               text,
    direct_or_derived           text NOT NULL DEFAULT 'DIRECT',
    basis_relationship_ids      uuid[] NOT NULL DEFAULT '{}',
    derived_at                  timestamptz,
    derivation_version          text,
    verification_state          text NOT NULL DEFAULT 'UNVERIFIED',
    status                      text NOT NULL DEFAULT 'PENDING',
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
        OR (ownership_percentage > 0 AND ownership_percentage <= 100)),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (source_organisation_id <> target_organisation_id),
    CHECK (verification_state IN (
        'UNVERIFIED','PENDING_REVIEW','VERIFIED','CONFLICTED','REJECTED','EXPIRED'
    )),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','ENDED','RETIRED')),
    CHECK (direct_or_derived IN ('DIRECT','DERIVED')),
    CHECK (classification IS NULL
        OR classification IN ('PUBLIC','INTERNAL','TENANT_CONFIDENTIAL','RESTRICTED')),
    CONSTRAINT corporate_relationship_derived_has_lineage CHECK (
        direct_or_derived <> 'DERIVED'
        OR (cardinality(basis_relationship_ids) >= 1
            AND derived_at IS NOT NULL AND derivation_version IS NOT NULL)
    ),
    CONSTRAINT corporate_relationship_verified_has_evidence CHECK (
        verification_state <> 'VERIFIED'
        OR (jsonb_array_length(evidence_references) >= 1
            AND verified_by IS NOT NULL AND verified_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS corporate_relationship_source_idx
    ON registry.corporate_relationship(source_organisation_id, effective_from);
CREATE INDEX IF NOT EXISTS corporate_relationship_target_idx
    ON registry.corporate_relationship(target_organisation_id, effective_from);
-- One live row per directed fact; conflicting evidence is recorded through
-- verification_state CONFLICTED, not parallel rows.
CREATE UNIQUE INDEX IF NOT EXISTS corporate_relationship_live_uniq
    ON registry.corporate_relationship(source_organisation_id, target_organisation_id, relationship_type)
    WHERE status IN ('PENDING','ACTIVE','SUSPENDED');

CREATE TABLE IF NOT EXISTS registry.corporate_group (
    corporate_group_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name            text NOT NULL,
    root_organisation_id    uuid
        REFERENCES registry.canonical_entity(canonical_entity_id),
    status                  text NOT NULL DEFAULT 'PENDING',
    grouping_policy         text NOT NULL,
    effective_from          timestamptz NOT NULL,
    effective_to            timestamptz,
    classification          text,
    metadata                jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','RETIRED')),
    CHECK (classification IS NULL
        OR classification IN ('PUBLIC','INTERNAL','TENANT_CONFIDENTIAL','RESTRICTED'))
);

CREATE TABLE IF NOT EXISTS registry.corporate_group_membership (
    corporate_group_membership_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    corporate_group_id              uuid NOT NULL
        REFERENCES registry.corporate_group(corporate_group_id),
    organisation_id                 uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    group_role                      text,
    basis_relationship_ids          uuid[] NOT NULL DEFAULT '{}',
    manual_basis_reference          text,
    status                          text NOT NULL DEFAULT 'PENDING',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    derived_at                      timestamptz NOT NULL,
    derivation_version              text NOT NULL,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT corporate_group_membership_has_basis CHECK (
        cardinality(basis_relationship_ids) >= 1 OR manual_basis_reference IS NOT NULL
    ),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','ENDED')),
    CHECK (group_role IS NULL
        OR group_role IN ('ROOT','MEMBER','BRANCH','JOINT_VENTURE','OTHER'))
);

CREATE INDEX IF NOT EXISTS corporate_group_membership_org_idx
    ON registry.corporate_group_membership(organisation_id);
CREATE UNIQUE INDEX IF NOT EXISTS corporate_group_membership_live_uniq
    ON registry.corporate_group_membership(corporate_group_id, organisation_id)
    WHERE status IN ('PENDING','ACTIVE','SUSPENDED');

-- An organisation may hold several concurrent relationship types.
CREATE TABLE IF NOT EXISTS registry.platform_relationship (
    platform_relationship_id    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    platform_id                 text NOT NULL,
    organisation_id             uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    relationship_type           text NOT NULL,
    verification_state          text NOT NULL DEFAULT 'UNVERIFIED',
    status                      text NOT NULL DEFAULT 'PENDING',
    effective_from              timestamptz NOT NULL,
    effective_to                timestamptz,
    basis_relationship_id       uuid
        REFERENCES registry.corporate_relationship(corporate_relationship_id),
    admission_decision_id       text,
    source_authority            text NOT NULL,
    evidence_references         jsonb NOT NULL DEFAULT '[]'::jsonb,
    verified_by                 text,
    verified_at                 timestamptz,
    classification              text,
    metadata                    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CHECK (relationship_type IN (
        'PLATFORM_OWNER','PLATFORM_OPERATOR','PLATFORM_GROUP_AFFILIATE',
        'PLATFORM_PARTNER','EXTERNAL_CLIENT','MANAGED_ENTITY'
    )),
    CHECK (platform_id ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (verification_state IN (
        'UNVERIFIED','PENDING_REVIEW','VERIFIED','CONFLICTED','REJECTED','EXPIRED'
    )),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','ENDED','RETIRED')),
    CHECK (classification IS NULL
        OR classification IN ('PUBLIC','INTERNAL','TENANT_CONFIDENTIAL','RESTRICTED')),
    CONSTRAINT platform_relationship_affiliate_has_basis CHECK (
        relationship_type <> 'PLATFORM_GROUP_AFFILIATE' OR basis_relationship_id IS NOT NULL
    ),
    CONSTRAINT platform_relationship_verified_has_evidence CHECK (
        verification_state <> 'VERIFIED'
        OR (jsonb_array_length(evidence_references) >= 1
            AND verified_by IS NOT NULL AND verified_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS platform_relationship_org_idx
    ON registry.platform_relationship(organisation_id, effective_from);
CREATE UNIQUE INDEX IF NOT EXISTS platform_relationship_live_uniq
    ON registry.platform_relationship(organisation_id, platform_id, relationship_type)
    WHERE status IN ('PENDING','ACTIVE','SUSPENDED');

-- Commercial/administrative grouping; not a tenant, not an authorization boundary.
CREATE TABLE IF NOT EXISTS registry.platform_account (
    platform_account_id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name                text NOT NULL,
    primary_organisation_id     uuid
        REFERENCES registry.canonical_entity(canonical_entity_id),
    status                      text NOT NULL DEFAULT 'PENDING',
    contract_references         jsonb NOT NULL DEFAULT '[]'::jsonb,
    billing_profile_reference   text,
    support_profile_reference   text,
    effective_from              timestamptz NOT NULL,
    effective_to                timestamptz,
    metadata                    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','CLOSED'))
);

CREATE TABLE IF NOT EXISTS registry.platform_account_membership (
    platform_account_membership_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    platform_account_id             uuid NOT NULL
        REFERENCES registry.platform_account(platform_account_id),
    organisation_id                 uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    account_role                    text NOT NULL,
    status                          text NOT NULL DEFAULT 'PENDING',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    evidence_reference              text,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (account_role IN (
        'PRIMARY_ACCOUNT_ORGANISATION','CONTRACTING_PARTY','BILLING_PARTY',
        'SERVICE_RECIPIENT','ACCOUNT_MEMBER'
    )),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','ENDED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS platform_account_membership_live_uniq
    ON registry.platform_account_membership(platform_account_id, organisation_id, account_role)
    WHERE status IN ('PENDING','ACTIVE','SUSPENDED');

CREATE TABLE IF NOT EXISTS registry.tenant_organisation_mapping (
    tenant_organisation_mapping_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                       varchar(63) NOT NULL
        REFERENCES tenants(tenant_id),
    organisation_id                 uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    mapping_role                    text NOT NULL,
    status                          text NOT NULL DEFAULT 'ACTIVE',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    provenance                      text NOT NULL,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (mapping_role IN (
        'PRIMARY_ORGANISATION','OPERATING_ORGANISATION','ADDITIONAL_ORGANISATION'
    )),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','ENDED'))
);

CREATE INDEX IF NOT EXISTS tenant_organisation_mapping_tenant_idx
    ON registry.tenant_organisation_mapping(tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS tenant_organisation_mapping_live_uniq
    ON registry.tenant_organisation_mapping(tenant_id, organisation_id, mapping_role)
    WHERE status IN ('PENDING','ACTIVE','SUSPENDED');
CREATE UNIQUE INDEX IF NOT EXISTS tenant_organisation_mapping_primary_uniq
    ON registry.tenant_organisation_mapping(tenant_id)
    WHERE mapping_role = 'PRIMARY_ORGANISATION' AND status IN ('PENDING','ACTIVE','SUSPENDED');

CREATE TABLE IF NOT EXISTS registry.tenant_legal_entity_mapping (
    tenant_legal_entity_mapping_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                       varchar(63) NOT NULL
        REFERENCES tenants(tenant_id),
    legal_entity_id                 varchar(63) NOT NULL
        REFERENCES legal_entities(legal_entity_id),
    mapping_role                    text NOT NULL,
    status                          text NOT NULL DEFAULT 'ACTIVE',
    effective_from                  timestamptz NOT NULL,
    effective_to                    timestamptz,
    provenance                      text NOT NULL,
    metadata                        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    CHECK (mapping_role IN ('DEFAULT','ADDITIONAL')),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','ENDED'))
);

CREATE INDEX IF NOT EXISTS tenant_legal_entity_mapping_tenant_idx
    ON registry.tenant_legal_entity_mapping(tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS tenant_legal_entity_mapping_live_uniq
    ON registry.tenant_legal_entity_mapping(tenant_id, legal_entity_id)
    WHERE status IN ('PENDING','ACTIVE','SUSPENDED');
CREATE UNIQUE INDEX IF NOT EXISTS tenant_legal_entity_mapping_default_uniq
    ON registry.tenant_legal_entity_mapping(tenant_id)
    WHERE mapping_role = 'DEFAULT' AND status IN ('PENDING','ACTIVE','SUSPENDED');

-- Backfill: every existing tenant's singular legal_entity_id becomes its
-- live DEFAULT mapping. Idempotent and non-destructive.
INSERT INTO registry.tenant_legal_entity_mapping (
    tenant_id, legal_entity_id, mapping_role, status, effective_from, provenance
)
SELECT t.tenant_id, t.legal_entity_id, 'DEFAULT', 'ACTIVE', t.created_at, 'migration-000045-backfill'
FROM tenants t
WHERE NOT EXISTS (
    SELECT 1 FROM registry.tenant_legal_entity_mapping m
    WHERE m.tenant_id = t.tenant_id
      AND m.mapping_role = 'DEFAULT'
      AND m.status IN ('PENDING','ACTIVE','SUSPENDED')
);
