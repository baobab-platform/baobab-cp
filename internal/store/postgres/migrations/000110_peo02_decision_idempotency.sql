-- PEO-02 immutable decision replay record. An approval/rejection is
-- single-shot; a retry may recover only the same checked intent+payload.
CREATE TABLE admission.founding_governance_decision_command (
 actor_id uuid NOT NULL REFERENCES identity.principal(principal_id),
 idempotency_key varchar(128) NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
 intent_id uuid NOT NULL UNIQUE REFERENCES admission.founding_governance_intent(intent_id),
 request_digest char(64) NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_id,idempotency_key)
);
CREATE FUNCTION admission.founding_decision_command_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'founding governance decision commands are immutable'
  USING ERRCODE='check_violation';
END $$;
CREATE TRIGGER founding_decision_command_immutable
BEFORE UPDATE OR DELETE ON admission.founding_governance_decision_command
FOR EACH ROW EXECUTE FUNCTION admission.founding_decision_command_immutable();
REVOKE ALL ON admission.founding_governance_decision_command FROM PUBLIC;
