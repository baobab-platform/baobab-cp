-- PEO-03G: SARS assisted eFiling/SOQS witness ledger; never store PINs,
-- taxpayer references, portal passwords, captures or raw document bodies.
-- A witnessed finding is NOT a VERIFIED OEV-03 result or active source trust.
CREATE TABLE evidence.assisted_official_witness (
    intent_id text PRIMARY KEY CHECK(intent_id ~ '^asrs_[a-z0-9]+$'),
    case_id text NOT NULL REFERENCES evidence.verification_case(case_id),
    claim_id text NOT NULL REFERENCES evidence.claim(claim_id),
    source_id text NOT NULL CHECK(source_id IN (
      'esrc_sarstcs','esrc_sarsvat','esrc_sarspaye','esrc_sarsuif',
      'esrc_sarssdl','esrc_sarscustoms','esrc_uifdept')),
    evidence_id text NOT NULL REFERENCES evidence.record(evidence_id),
    official_reference text NOT NULL CHECK(length(official_reference) BETWEEN 4 AND 128),
    provenance_digest text NOT NULL CHECK(provenance_digest ~ '^sha256:[0-9a-f]{64}$'),
    observed_at timestamptz NOT NULL,
    freshness_until timestamptz NOT NULL CHECK(freshness_until>observed_at),
    witness_actor text NOT NULL,
    witness_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    checker_actor text,
    checked_at timestamptz,
    outcome text NOT NULL DEFAULT 'PENDING'
        CHECK(outcome IN ('PENDING','ATTESTED','REJECTED')),
    decision_reason text,
    idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
    CHECK((outcome='PENDING' AND checker_actor IS NULL AND checked_at IS NULL)
       OR (outcome<>'PENDING' AND checker_actor IS NOT NULL AND checked_at IS NOT NULL
           AND witness_actor<>checker_actor)),
    UNIQUE(witness_actor,idempotency_key)
);
CREATE INDEX assisted_official_witness_due
    ON evidence.assisted_official_witness(freshness_until,case_id)
    WHERE outcome='ATTESTED';
REVOKE ALL ON evidence.assisted_official_witness FROM PUBLIC;
