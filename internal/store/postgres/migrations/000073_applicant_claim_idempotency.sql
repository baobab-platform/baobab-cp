-- ADR-BCP-023 applicant claims: creating one is replayable. A retry with the
-- same Idempotency-Key and body returns the original claim; the key is
-- unique per asserter, so a lost response never mints a second claim.

ALTER TABLE evidence.claim
    ADD COLUMN IF NOT EXISTS create_idempotency_key text,
    ADD COLUMN IF NOT EXISTS create_request_hash    text;

CREATE UNIQUE INDEX IF NOT EXISTS claim_create_idempotency_idx
    ON evidence.claim (asserted_by, create_idempotency_key)
    WHERE create_idempotency_key IS NOT NULL;
