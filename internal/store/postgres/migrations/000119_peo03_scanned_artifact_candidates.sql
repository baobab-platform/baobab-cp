-- PEO-03K: independently scanned artifact reference and encrypted OCR
-- candidates. Candidate plaintext is NEVER persisted in PostgreSQL.
CREATE TABLE evidence.scanned_artifact (
    evidence_id text NOT NULL REFERENCES evidence.record(evidence_id),
    artifact_id text NOT NULL CHECK (artifact_id ~ '^eart_[a-z0-9]+$'),
    media_type text NOT NULL CHECK (media_type IN ('application/pdf','image/jpeg','image/png')),
    document_digest text NOT NULL CHECK (document_digest ~ '^sha256:[0-9a-f]{64}$'),
    scanner_id text NOT NULL CHECK (length(scanner_id) BETWEEN 3 AND 96),
    scanner_version text NOT NULL CHECK (length(scanner_version) BETWEEN 1 AND 96),
    scan_outcome text NOT NULL CHECK (scan_outcome='CLEAN'),
    scanned_at timestamptz NOT NULL,
    signature bytea NOT NULL CHECK (octet_length(signature)=64),
    PRIMARY KEY(evidence_id,artifact_id)
);
REVOKE ALL ON evidence.scanned_artifact FROM PUBLIC;

CREATE TABLE evidence.document_candidate_attempt (
    attempt_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    case_id text NOT NULL REFERENCES evidence.verification_case(case_id),
    claim_id text NOT NULL REFERENCES evidence.claim(claim_id),
    evidence_id text NOT NULL,
    artifact_id text NOT NULL,
    actor_id text NOT NULL,
    client_id text NOT NULL,
    idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
    document_digest text NOT NULL CHECK (document_digest ~ '^sha256:[0-9a-f]{64}$'),
    processor_id text NOT NULL,
    processor_version text NOT NULL,
    observed_at timestamptz NOT NULL,
    sealed_candidates bytea NOT NULL CHECK(octet_length(sealed_candidates) BETWEEN 29 AND 65536),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(evidence_id,artifact_id)
        REFERENCES evidence.scanned_artifact(evidence_id,artifact_id),
    UNIQUE(actor_id,idempotency_key)
);
CREATE INDEX document_candidate_case_evidence_idx ON evidence.document_candidate_attempt(case_id,evidence_id);
REVOKE ALL ON evidence.document_candidate_attempt FROM PUBLIC;

-- Grant only to the separately provisioned CP database runtime role through
-- governed database bootstrap. Never use PUBLIC or an applicant SQL role.
