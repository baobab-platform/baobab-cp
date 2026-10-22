-- INT-03C Accepted Shared governed disposable identity lifecycle.
-- This is NOT a generic Principal deletion or a production IAM back door.
-- Two independent ACTIVE CP human operators act on one strictly recognised
-- development-only workload Principal. No direct SQL fixture mutations are
-- permitted as runtime acceptance.
CREATE TABLE identity.int03c_deactivation_request (
    request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    principal_id uuid NOT NULL REFERENCES identity.principal(principal_id),
    issuer text NOT NULL CHECK (issuer = 'https://hydra.int03.test'),
    subject text NOT NULL CHECK (subject IN (
        'baobab-int03c-issuer-owner', 'baobab-int03c-issuer-verifier')),
    run_id text NOT NULL CHECK (run_id ~ '^[0-9a-f]{32}$'),
    source_sha text NOT NULL CHECK (source_sha ~ '^[0-9a-f]{40}$'),
    plan_digest text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    idempotency_key uuid NOT NULL,
    maker_id uuid NOT NULL REFERENCES identity.principal(principal_id),
    checker_id uuid REFERENCES identity.principal(principal_id),
    decision text NOT NULL DEFAULT 'PENDING' CHECK (decision IN ('PENDING','COMPLETED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    CHECK (checker_id IS NULL OR checker_id <> maker_id),
    CHECK ((decision = 'COMPLETED') =
        (checker_id IS NOT NULL AND completed_at IS NOT NULL)),
    UNIQUE (maker_id, idempotency_key),
    UNIQUE (principal_id, run_id, subject)
);
CREATE INDEX int03c_deactivation_pending_idx
  ON identity.int03c_deactivation_request (principal_id, decision);

CREATE TABLE identity.int03c_deactivation_audit (
    audit_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id uuid NOT NULL UNIQUE REFERENCES identity.int03c_deactivation_request(request_id),
    principal_id uuid NOT NULL REFERENCES identity.principal(principal_id),
    maker_id uuid NOT NULL REFERENCES identity.principal(principal_id),
    checker_id uuid NOT NULL REFERENCES identity.principal(principal_id),
    plan_digest text NOT NULL,
    original_status text NOT NULL CHECK (original_status = 'ACTIVE'),
    final_status text NOT NULL CHECK (final_status = 'DISABLED'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (maker_id <> checker_id)
);
CREATE FUNCTION identity.int03c_audit_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'INT03C security audit is immutable' USING ERRCODE='check_violation';
END $$;
CREATE TRIGGER int03c_audit_immutable
BEFORE UPDATE OR DELETE ON identity.int03c_deactivation_audit
FOR EACH ROW EXECUTE FUNCTION identity.int03c_audit_immutable();

CREATE FUNCTION identity.int03c_deactivation_request_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'INT03C evidence cannot be deleted' USING ERRCODE='check_violation';
  END IF;
  IF NEW.decision <> 'COMPLETED' OR OLD.decision <> 'PENDING'
    OR NEW.checker_id IS NULL OR NEW.checker_id=OLD.maker_id
    OR NEW.completed_at IS NULL
    OR ROW(NEW.request_id,NEW.principal_id,NEW.issuer,NEW.subject,
           NEW.run_id,NEW.source_sha,NEW.plan_digest,NEW.idempotency_key,
           NEW.maker_id,NEW.created_at,NEW.expires_at)
      IS DISTINCT FROM
       ROW(OLD.request_id,OLD.principal_id,OLD.issuer,OLD.subject,
           OLD.run_id,OLD.source_sha,OLD.plan_digest,OLD.idempotency_key,
           OLD.maker_id,OLD.created_at,OLD.expires_at)
    OR NOT EXISTS (
      SELECT 1 FROM identity.int03c_deactivation_audit a
       WHERE a.request_id=OLD.request_id AND a.maker_id=OLD.maker_id
         AND a.checker_id=NEW.checker_id AND a.plan_digest=OLD.plan_digest)
    THEN
    RAISE EXCEPTION 'INT03C request mutation requires a committed two-person audit'
      USING ERRCODE='check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER int03c_deactivation_request_guard
BEFORE UPDATE OR DELETE ON identity.int03c_deactivation_request
FOR EACH ROW EXECUTE FUNCTION identity.int03c_deactivation_request_guard();
REVOKE ALL ON identity.int03c_deactivation_request FROM PUBLIC;
REVOKE ALL ON identity.int03c_deactivation_audit FROM PUBLIC;
