-- PEO-02 governed intention ledger; application callers cannot mint sponsorships.
-- Positive grants are only committed by independent human checker commands.
CREATE TABLE admission.founding_governance_intent (
 intent_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 kind text NOT NULL CHECK(kind IN ('SPONSORSHIP','DOCUMENTARY_DEFERRAL')),
 operating_organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
 proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
 proposal jsonb NOT NULL CHECK(jsonb_typeof(proposal)='object'),
 idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
 request_digest char(64) NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 status text NOT NULL DEFAULT 'PENDING'
   CHECK(status IN ('PENDING','APPROVED','REJECTED')),
 reviewed_by uuid REFERENCES identity.principal(principal_id),
 reviewed_at timestamptz,
 review_reference text,
 grant_id uuid,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK ((status='PENDING')=(reviewed_by IS NULL)),
 CHECK (reviewed_by IS NULL OR (reviewed_by<>proposed_by
    AND reviewed_at IS NOT NULL AND length(btrim(review_reference))>0)),
 UNIQUE(kind,proposed_by,idempotency_key)
);
CREATE INDEX founding_intent_reviewer_idx
 ON admission.founding_governance_intent(status,created_at);
CREATE FUNCTION admission.founding_governance_intent_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR (TG_OP='UPDATE' AND
  (ROW(NEW.intent_id,NEW.kind,NEW.operating_organisation_id,NEW.proposed_by,
    NEW.proposal,NEW.idempotency_key,NEW.request_digest,NEW.created_at)
   IS DISTINCT FROM
   ROW(OLD.intent_id,OLD.kind,OLD.operating_organisation_id,OLD.proposed_by,
    OLD.proposal,OLD.idempotency_key,OLD.request_digest,OLD.created_at)
   OR OLD.status<>'PENDING' OR NEW.status NOT IN ('APPROVED','REJECTED')))
 THEN
  RAISE EXCEPTION 'founding governance intent is append-only with one independent review'
    USING ERRCODE='check_violation';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER founding_governance_intent_guard BEFORE UPDATE OR DELETE
 ON admission.founding_governance_intent FOR EACH ROW EXECUTE FUNCTION admission.founding_governance_intent_guard();

-- Authoritative policy content is NOT inferred from the requirement name.
-- Intentionally no seeded allowlist: until separately reviewed, deferrals
-- fail closed rather than treating PAYE/UIF, KYC or statutory filings as waived.
CREATE TABLE admission.founding_documentary_policy_requirement (
 policy_reference text NOT NULL CHECK(length(btrim(policy_reference)) BETWEEN 1 AND 500),
 requirement_id text NOT NULL CHECK(length(btrim(requirement_id)) BETWEEN 3 AND 160),
 requirement_authority text NOT NULL
   CHECK(requirement_authority IN ('PLATFORM_DOCUMENTARY','STATUTORY','PROVIDER')),
 authority_basis_reference text NOT NULL CHECK(length(btrim(authority_basis_reference))>0),
 approved_by uuid NOT NULL REFERENCES identity.principal(principal_id),
 approved_at timestamptz NOT NULL,
 effective_from timestamptz NOT NULL,
 effective_to timestamptz NOT NULL,
 CHECK(effective_to>effective_from AND approved_at<=effective_to),
 PRIMARY KEY(policy_reference,requirement_id)
);
-- Prevent silent retroactive policy broadening through UPDATE/DELETE.
CREATE FUNCTION admission.founding_documentary_policy_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'policy requirement classification is immutable; publish a versioned policy'
 USING ERRCODE='check_violation';
END $$;
CREATE TRIGGER founding_documentary_policy_immutable BEFORE UPDATE OR DELETE
 ON admission.founding_documentary_policy_requirement FOR EACH ROW
 EXECUTE FUNCTION admission.founding_documentary_policy_immutable();
REVOKE ALL ON admission.founding_governance_intent FROM PUBLIC;
REVOKE ALL ON admission.founding_documentary_policy_requirement FROM PUBLIC;
