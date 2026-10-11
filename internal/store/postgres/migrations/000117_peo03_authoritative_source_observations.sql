-- PEO-03C: append-only authoritative-source observation handoff.
-- OEV-03 verification cases, claims, evidence and human VerificationResult
-- remain the sole positive-evidence decision path. Never persist source
-- credentials, SARS taxpayer PINs, raw provider responses or document bodies.
CREATE TABLE evidence.authoritative_source_observation (
    observation_id text PRIMARY KEY CHECK (observation_id ~ '^aobs_[a-z0-9]+$'),
    case_id text NOT NULL REFERENCES evidence.verification_case(case_id),
    claim_id text NOT NULL REFERENCES evidence.claim(claim_id),
    evidence_id text NOT NULL REFERENCES evidence.record(evidence_id),
    source_id text NOT NULL CHECK (source_id ~ '^esrc_[a-z0-9]+$'),
    method text NOT NULL CHECK (method IN (
        'SOURCE_API_LOOKUP','MANUAL_REGISTRY_LOOKUP','OFFICIAL_DOCUMENT_REVIEW')),
    outcome text NOT NULL CHECK (outcome IN ('MATCHED','NOT_MATCHED','INCONCLUSIVE','UNAVAILABLE')),
    source_record_reference text NOT NULL CHECK (length(btrim(source_record_reference)) BETWEEN 3 AND 256),
    provenance_reference text NOT NULL CHECK (length(btrim(provenance_reference)) BETWEEN 8 AND 512),
    response_digest text NOT NULL CHECK (response_digest ~ '^sha256:[0-9a-f]{64}$'),
    source_observed_at timestamptz NOT NULL,
    freshness_until timestamptz NOT NULL CHECK (freshness_until > source_observed_at),
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    recorded_by text NOT NULL CHECK (length(recorded_by) BETWEEN 3 AND 128),
    adapter_id text,
    witness_principal text,
    checker_principal text,
    is_synthetic boolean NOT NULL DEFAULT false,
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    request_digest text NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CHECK (
      (method = 'SOURCE_API_LOOKUP' AND adapter_id IS NOT NULL
         AND witness_principal IS NULL AND checker_principal IS NULL)
      OR
      (method <> 'SOURCE_API_LOOKUP' AND adapter_id IS NULL
         AND witness_principal IS NOT NULL AND checker_principal IS NOT NULL
         AND witness_principal <> checker_principal)
    ),
    UNIQUE(recorded_by,idempotency_key)
);
CREATE INDEX authoritative_source_claim_idx
    ON evidence.authoritative_source_observation(case_id,claim_id,recorded_at DESC);

-- Immutable evidence provenance: changing or deleting an observation is never
-- remediation; use a new observation, superseding check and verification result.
CREATE FUNCTION evidence.authoritative_source_observation_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'authoritative source observations are append-only'
        USING ERRCODE='check_violation';
END $$;
CREATE TRIGGER authoritative_source_observation_immutable
    BEFORE UPDATE OR DELETE ON evidence.authoritative_source_observation
    FOR EACH ROW EXECUTE FUNCTION evidence.authoritative_source_observation_immutable();

REVOKE ALL ON evidence.authoritative_source_observation FROM PUBLIC;
