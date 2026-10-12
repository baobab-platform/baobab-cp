-- The intent is committed BEFORE uploading ciphertext. Deliberately no FK to
-- scan proof: failed intake must leave a discoverable, non-readable orphan.
-- Tombstones prevent a deleted artifact from being silently recreated.
CREATE TABLE evidence.document_custody (
 evidence_id text NOT NULL CHECK(evidence_id ~ '^evr_[a-z0-9]+$'),
 artifact_id text NOT NULL CHECK(artifact_id ~ '^eart_[a-z0-9]+$'),
 proof_hash text NOT NULL CHECK(proof_hash ~ '^[0-9a-f]{64}$'),
 object_key text NOT NULL UNIQUE,
 version_id text NOT NULL DEFAULT '',
 key_id text NOT NULL,
 wrapped_key bytea NOT NULL CHECK(octet_length(wrapped_key) BETWEEN 1 AND 8192),
 nonce bytea NOT NULL CHECK(octet_length(nonce)=12),
 cipher_digest text NOT NULL CHECK(cipher_digest ~ '^[0-9a-f]{64}$'),
 policy_id text NOT NULL,
 policy_version text NOT NULL,
 policy_hash text NOT NULL CHECK(policy_hash ~ '^[0-9a-f]{64}$'),
 retain_until timestamptz NOT NULL,
 delete_after timestamptz NOT NULL CHECK(delete_after >= retain_until),
 orphan_after timestamptz NOT NULL,
 legal_hold boolean NOT NULL DEFAULT false,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision >= 1),
 state text NOT NULL CHECK(state IN ('PENDING','ATTACHED','QUARANTINED','DELETING','DELETED')),
 quarantined_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(evidence_id,artifact_id),
 CHECK(state <> 'ATTACHED' OR version_id <> '')
);
CREATE INDEX document_custody_reconcile_idx ON evidence.document_custody(state,orphan_after);
REVOKE ALL ON evidence.document_custody FROM PUBLIC;
-- Intake has no cloud delete/hold-release/ReEncrypt permission. Maintenance
-- uses a separately approved workload role; SQL grants belong to bootstrap.
