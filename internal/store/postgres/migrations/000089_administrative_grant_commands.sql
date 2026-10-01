-- ADA-05 grant administration: replay protection for the commands that
-- create or change an AdministrativeGrant (Idempotency-Key). A key is scoped
-- to the acting principal; replaying it with a different request is refused.
CREATE TABLE policy.administrative_grant_command (
    actor_id text NOT NULL,
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    request_hash char(64) NOT NULL,
    grant_id uuid NOT NULL REFERENCES policy.administrative_grant(grant_id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, idempotency_key)
);

-- The sweep that activates and expires grants scans by status and window.
CREATE INDEX administrative_grant_sweep_idx ON policy.administrative_grant (status, valid_from, valid_until)
    WHERE status IN ('PENDING', 'ACTIVE', 'SUSPENDED');
