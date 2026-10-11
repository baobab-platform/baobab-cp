-- ADR-BCP-026 PEO-T20 / ADR-BCP-027 LA-T27.
-- Independent corporate-control proof custody, SEPARATE from company registration.
-- The recorded attestation cannot directly update a LegalEntityProfile,
-- CorporateRelationship, PlatformRelationship, Tenant or IAM permission.
CREATE TABLE evidence.founding_control_attestation (
    attestation_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    relationship_id text NOT NULL CHECK (relationship_id ~ '^crel_[a-z0-9_]+$'),
    issuer_id text NOT NULL CHECK (length(issuer_id) BETWEEN 3 AND 128),
    signer_id text NOT NULL CHECK (length(signer_id) BETWEEN 3 AND 128),
    evidence_ref text NOT NULL CHECK (length(evidence_ref) BETWEEN 8 AND 256),
    document_digest text NOT NULL CHECK (document_digest ~ '^sha256:[0-9a-f]{64}$'),
    proof_digest text NOT NULL CHECK (proof_digest ~ '^sha256:[0-9a-f]{64}$'),
    proof_document jsonb NOT NULL,
    maker_principal text NOT NULL CHECK (length(maker_principal) BETWEEN 4 AND 128),
    checker_principal text NOT NULL CHECK (length(checker_principal) BETWEEN 4 AND 128),
    checker_approval_ref text NOT NULL CHECK (length(checker_approval_ref) BETWEEN 8 AND 256),
    checker_approved_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (maker_principal <> checker_principal),
    UNIQUE(relationship_id,proof_digest)
);
CREATE INDEX founding_control_attestation_by_relation
    ON evidence.founding_control_attestation(relationship_id, recorded_at DESC);
REVOKE ALL ON evidence.founding_control_attestation FROM PUBLIC;
-- Approved runtime grants are applied only by governed database bootstrap.
-- Appending a proof does not establish VERIFIED; current OEV-03 registrations,
-- corporate-graph state, source signer and approvals are checked at read time.

-- Only deliberate, logged data-retention administration can alter historical
-- records through an elevated migration/maintenance boundary. Normal runtime
-- connections cannot silently revise a signed corporate-control approval.
CREATE OR REPLACE FUNCTION evidence.reject_founding_control_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'founding control attestation is append-only; use governed retention process';
END;
$$;
CREATE TRIGGER founding_control_attestation_append_only
BEFORE UPDATE OR DELETE ON evidence.founding_control_attestation
FOR EACH ROW EXECUTE FUNCTION evidence.reject_founding_control_mutation();
