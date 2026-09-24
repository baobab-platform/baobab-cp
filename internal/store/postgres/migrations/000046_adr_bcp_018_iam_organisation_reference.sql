-- ADR-BCP-018 gate ORG-10 — IAM organisation projection.
--
-- Contract authority: baobab-platform/shared contracts/organisation/v1/
-- iam.schema.json (IamOrganisationReference). Rows are keyed by uuid; the
-- API renders them as opaque iamorg_ ids.
--
-- A Keycloak Organization is an IAM projection of a canonical Organisation,
-- never the Organisation itself (sections 64, 97, 180). Invariants:
--   * one canonical Organisation may have several IAM organisations, e.g.
--     one per realm or region (section 65);
--   * one provider organisation within one issuer is actively linked to at
--     most one canonical Organisation, so an IAM claim can never resolve
--     ambiguously (section 66, fail closed);
--   * links are retired, never deleted, so history stays queryable.
--
-- registry.external_reference (migration 000010) stays the generic engine
-- reference table; it has no issuer, status or effective window.

CREATE TABLE IF NOT EXISTS registry.iam_organisation_reference (
    iam_organisation_reference_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id                uuid NOT NULL
        REFERENCES registry.canonical_entity(canonical_entity_id),
    provider                       text NOT NULL,
    issuer                         text NOT NULL,
    provider_organisation_id       text NOT NULL,
    status                         text NOT NULL DEFAULT 'ACTIVE',
    effective_from                 timestamptz NOT NULL,
    effective_to                   timestamptz,
    source_authority               text NOT NULL,
    created_at                     timestamptz NOT NULL DEFAULT now(),
    updated_at                     timestamptz NOT NULL DEFAULT now(),
    CHECK (provider IN ('keycloak')),
    CHECK (issuer ~ '^https://' AND length(issuer) <= 512),
    CHECK (provider_organisation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$' AND length(provider_organisation_id) <= 255),
    CHECK (status IN ('ACTIVE','RETIRED')),
    CHECK (btrim(source_authority) <> ''),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (status <> 'RETIRED' OR effective_to IS NOT NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS iam_organisation_reference_active_uniq
    ON registry.iam_organisation_reference(provider, issuer, provider_organisation_id)
    WHERE status = 'ACTIVE';
CREATE INDEX IF NOT EXISTS iam_organisation_reference_organisation_idx
    ON registry.iam_organisation_reference(organisation_id);
