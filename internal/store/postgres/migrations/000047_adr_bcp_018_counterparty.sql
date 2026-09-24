-- ADR-BCP-018 gate ORG-13 — buyer/supplier reconciliation.
--
-- Contract authority: baobab-platform/shared contracts/organisation/v1/
-- counterparty.schema.json. Rows are keyed by uuid; the API renders them as
-- opaque crole_ and orc_ ids.
--
-- ADR-BCP-016's BUYER_ORGANISATION and SUPPLIER_ORGANISATION canonical
-- entities keep their ids and entity types (sections 11, 112, 194). ORG-13
-- attaches an organisation profile to each and records its commercial
-- capacity as a tenant-scoped counterparty_role (ADR-BCP-014 sections
-- 10-17). Possible duplicate Organisations, found on governed identifiers
-- only, are quarantined as organisation_resolution_candidate rows and never
-- merged here: a canonical merge is an ADR-BCP-021 controlled change
-- (ADR-BCP-023 section 52).

CREATE TABLE IF NOT EXISTS registry.counterparty_role (
    counterparty_role_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id       uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    tenant_id             varchar(63) NOT NULL
        REFERENCES tenants(tenant_id),
    role                  text NOT NULL,
    status                text NOT NULL,
    effective_from        timestamptz NOT NULL,
    effective_to          timestamptz,
    source_authority      text NOT NULL,
    legacy_entity_type    text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CHECK (role IN (
        'CUSTOMER','BUYER','SUPPLIER','VENDOR','DISTRIBUTOR','RESELLER','CARRIER',
        'FREIGHT_FORWARDER','CUSTOMS_BROKER','WAREHOUSE_OPERATOR','THIRD_PARTY_LOGISTICS',
        'INSURER','BANK','PAYMENT_PROVIDER','AGENT','SERVICE_PROVIDER','AFFILIATE',
        'INTERCOMPANY_COUNTERPARTY'
    )),
    CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','ENDED')),
    CHECK (legacy_entity_type IS NULL OR legacy_entity_type IN ('BUYER_ORGANISATION','SUPPLIER_ORGANISATION')),
    CHECK (btrim(source_authority) <> ''),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status <> 'ENDED' OR effective_to IS NOT NULL)
);

-- One live role per organisation, tenant and role; the unique index also
-- serves "does this organisation hold this role for this tenant".
CREATE UNIQUE INDEX IF NOT EXISTS counterparty_role_live_uniq
    ON registry.counterparty_role(organisation_id, tenant_id, role)
    WHERE status IN ('PENDING','ACTIVE','SUSPENDED');
CREATE INDEX IF NOT EXISTS counterparty_role_tenant_idx
    ON registry.counterparty_role(tenant_id, role);

CREATE TABLE IF NOT EXISTS registry.organisation_resolution_candidate (
    candidate_id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- An unordered pair stored in canonical order, so a pair has one row.
    organisation_a                uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    organisation_b                uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    matched_identifiers           jsonb NOT NULL,
    status                        text NOT NULL DEFAULT 'OPEN',
    source                        text NOT NULL,
    detected_at                   timestamptz NOT NULL,
    decision_reason               text,
    surviving_organisation_id     uuid,
    decision_evidence             jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- The matched identifiers a decision was taken on. A DISTINCT pair is
    -- reopened only when a match outside this set appears.
    decided_matched_identifiers   jsonb,
    decided_by                    text,
    decided_at                    timestamptz,
    created_at                    timestamptz NOT NULL DEFAULT now(),
    updated_at                    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organisation_a, organisation_b),
    CHECK (organisation_a < organisation_b),
    CHECK (jsonb_typeof(matched_identifiers) = 'array' AND jsonb_array_length(matched_identifiers) > 0),
    CHECK (status IN ('OPEN','DISTINCT','DUPLICATE_CONFIRMED')),
    CHECK (btrim(source) <> ''),
    CHECK (
        (status = 'OPEN' AND decided_by IS NULL AND decided_at IS NULL AND decision_reason IS NULL
            AND surviving_organisation_id IS NULL AND decided_matched_identifiers IS NULL)
        OR (status <> 'OPEN' AND decided_by IS NOT NULL AND decided_at IS NOT NULL
            AND btrim(decision_reason) <> '' AND decided_matched_identifiers IS NOT NULL)
    ),
    CHECK ((status = 'DUPLICATE_CONFIRMED') = (surviving_organisation_id IS NOT NULL)),
    CHECK (surviving_organisation_id IS NULL OR surviving_organisation_id IN (organisation_a, organisation_b))
);

CREATE INDEX IF NOT EXISTS organisation_resolution_candidate_b_idx
    ON registry.organisation_resolution_candidate(organisation_b);
CREATE INDEX IF NOT EXISTS organisation_resolution_candidate_open_idx
    ON registry.organisation_resolution_candidate(detected_at, candidate_id)
    WHERE status = 'OPEN';
