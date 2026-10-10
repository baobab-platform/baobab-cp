-- PEO-02: append-only idempotency ledger for human suspension and revocation.
-- The underlying grants are never deleted; an inactive grant never confers authority.
-- This table records human decisions only; it does not create canonical events.
CREATE TABLE admission.founding_lifecycle_command (
  command_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_id uuid NOT NULL REFERENCES identity.principal(principal_id),
  idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 128),
  target_kind text NOT NULL CHECK (target_kind IN ('SPONSORSHIP','DOCUMENTARY_DEFERRAL')),
  target_id uuid NOT NULL,
  action text NOT NULL CHECK (action IN ('SUSPEND','REVOKE')),
  request_digest text NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
  receipt jsonb NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE (actor_id,idempotency_key),
  CHECK (target_kind='SPONSORSHIP' OR action='REVOKE')
);
CREATE INDEX founding_lifecycle_target_idx ON admission.founding_lifecycle_command(target_kind,target_id,recorded_at);
REVOKE ALL ON admission.founding_lifecycle_command FROM PUBLIC;
