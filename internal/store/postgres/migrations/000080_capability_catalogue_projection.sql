-- capability.capability becomes a projection of Shared's Canonical
-- Capability Catalogue (ADR-SHARED-017 SS28-29, gate G-CP-2): canonical
-- capability semantics converge from contracts/capability/v1/catalogue.yaml,
-- independently of provider registration, and every projected row records
-- where its definition came from. Written only by the catalogue sync.
ALTER TABLE capability.capability
    ADD COLUMN contract_versions integer[]
        CHECK (contract_versions IS NULL OR (cardinality(contract_versions) > 0 AND 1 <= ALL (contract_versions))),
    ADD COLUMN data_classification text
        CHECK (data_classification IN ('PUBLIC', 'INTERNAL', 'TENANT_CONFIDENTIAL', 'RESTRICTED')),
    ADD COLUMN canonical_owner text,
    ADD COLUMN canonical_source text,
    ADD COLUMN canonical_digest text CHECK (canonical_digest ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN canonical_synced_at timestamptz,
    ADD CONSTRAINT capability_canonical_provenance_check CHECK (
        (canonical_digest IS NULL) = (canonical_source IS NULL)
        AND (canonical_digest IS NULL) = (canonical_owner IS NULL)
        AND (canonical_digest IS NULL) = (canonical_synced_at IS NULL));

-- Capabilities the catalogue has not projected: rows created before the
-- sync existed, or by provider registration alone. Once this view is empty
-- in every environment, provider registration can stop creating
-- capabilities (G-CP-3).
CREATE VIEW capability.capability_outside_catalogue AS
SELECT capability_id, code, name, status, created_at
FROM capability.capability
WHERE canonical_digest IS NULL;
