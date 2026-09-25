-- ADR-BCP-017 — ClientApplication and AdmissionDecision
-- (gates OA-01, OA-03, OA-04, OA-09).
-- Contract: baobab-platform/shared contracts/admission/v1.
--
-- An application belongs to an applicant principal and to no tenant: it
-- creates no tenant, membership, subscription or capability (section 5).
-- The organisation profile, requirements and markets are applicant
-- evidence, kept as the contract's JSON documents and never read as
-- canonical organisation state (section 7). Evidence is held by reference
-- only (section 42). Status changes follow Shared's lifecycle.yaml; the
-- Control Plane refuses every other transition.

CREATE SCHEMA IF NOT EXISTS admission;

CREATE SEQUENCE IF NOT EXISTS admission.client_application_reference_seq;

CREATE TABLE IF NOT EXISTS admission.client_application (
    client_application_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reference               text NOT NULL UNIQUE,
    status                  text NOT NULL DEFAULT 'DRAFT',
    application_channel     text NOT NULL,
    version                 bigint NOT NULL DEFAULT 1,
    applicant_principal_id  uuid NOT NULL REFERENCES identity.principal(principal_id),
    assigned_reviewer       uuid REFERENCES identity.principal(principal_id),
    organisation_profile    jsonb NOT NULL DEFAULT '{}'::jsonb,
    requirements            jsonb NOT NULL DEFAULT '{}'::jsonb,
    requested_markets       jsonb NOT NULL DEFAULT '[]'::jsonb,
    evidence                jsonb NOT NULL DEFAULT '[]'::jsonb,
    information_requests    jsonb NOT NULL DEFAULT '[]'::jsonb,
    create_idempotency_key  text,
    create_request_hash     text,
    closure_reason          text,
    created_at              timestamptz NOT NULL,
    updated_at              timestamptz NOT NULL,
    submitted_at            timestamptz,
    closed_at               timestamptz,
    CHECK (status IN ('DRAFT','SUBMITTED','VALIDATING','INFORMATION_REQUIRED','UNDER_REVIEW',
                      'APPROVED','REJECTED','WITHDRAWN','EXPIRED','CANCELLED')),
    CHECK (application_channel IN ('SELF_SERVICE','ASSISTED_ENTERPRISE','INTERNAL_GROUP')),
    CHECK (version >= 1),
    CHECK (reference ~ '^APP-[0-9]{4}-[0-9]{6,}$'),
    CHECK (jsonb_typeof(organisation_profile) = 'object' AND jsonb_typeof(requirements) = 'object'
       AND jsonb_typeof(requested_markets) = 'array' AND jsonb_typeof(evidence) = 'array'
       AND jsonb_typeof(information_requests) = 'array'),
    -- States only a submitted application reaches carry submitted_at.
    CONSTRAINT client_application_submitted CHECK (
        status IN ('DRAFT','WITHDRAWN','EXPIRED') OR submitted_at IS NOT NULL),
    CONSTRAINT client_application_closed CHECK (
        (status IN ('APPROVED','REJECTED','WITHDRAWN','EXPIRED','CANCELLED')) = (closed_at IS NOT NULL)),
    CHECK ((create_idempotency_key IS NULL) = (create_request_hash IS NULL))
);

-- An applicant's own applications, newest first.
CREATE INDEX IF NOT EXISTS client_application_applicant_idx
    ON admission.client_application(applicant_principal_id, created_at DESC, client_application_id DESC);
-- The review queue: applications in given statuses, newest first (the
-- order and keyset ListClientApplications pages by).
CREATE INDEX IF NOT EXISTS client_application_status_idx
    ON admission.client_application(status, created_at DESC, client_application_id DESC);
-- Create replays: one application per applicant and Idempotency-Key.
CREATE UNIQUE INDEX IF NOT EXISTS client_application_create_idempotency_uniq
    ON admission.client_application(applicant_principal_id, create_idempotency_key)
    WHERE create_idempotency_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS admission.admission_decision (
    admission_decision_id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_application_id           uuid NOT NULL UNIQUE REFERENCES admission.client_application(client_application_id),
    decision                        text NOT NULL,
    reason                          text NOT NULL,
    decided_by                      uuid NOT NULL REFERENCES identity.principal(principal_id),
    decided_at                      timestamptz NOT NULL,
    approved_subscription_type      text,
    internal_eligibility            jsonb,
    approved_market_scope           text[],
    approved_product_requirements   text[],
    approved_isolation_requirements text,
    conditions                      text[],
    evidence_references             text[],
    CHECK (decision IN ('APPROVED','REJECTED')),
    CHECK (length(reason) BETWEEN 1 AND 2000),
    CHECK (approved_subscription_type IS NULL
        OR approved_subscription_type IN ('COMMERCIAL','INTERNAL','TRIAL','PARTNER','MANUAL','MIGRATION')),
    CHECK (approved_isolation_requirements IS NULL
        OR approved_isolation_requirements IN ('schema_per_tenant','row_level_security')),
    -- Section 21: an approval classifies, scopes and cites its evidence; a
    -- rejection carries none of that.
    CONSTRAINT admission_decision_approval_shape CHECK (
        CASE decision
        WHEN 'APPROVED' THEN approved_subscription_type IS NOT NULL
            AND cardinality(approved_market_scope) >= 1 AND cardinality(evidence_references) >= 1
        ELSE approved_subscription_type IS NULL AND approved_market_scope IS NULL
            AND approved_product_requirements IS NULL AND approved_isolation_requirements IS NULL
            AND conditions IS NULL AND internal_eligibility IS NULL
        END),
    -- Section 13: INTERNAL, and only INTERNAL, records eligibility evidence.
    CONSTRAINT admission_decision_internal_evidence CHECK (
        (approved_subscription_type IS NOT DISTINCT FROM 'INTERNAL') = (internal_eligibility IS NOT NULL))
);

-- An AdmissionDecision is immutable (section 21): a later subscription
-- change is a reclassification, never an edit of the decision.
CREATE OR REPLACE FUNCTION admission.admission_decision_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'admission_decision is immutable (ADR-BCP-017 section 21): % refused', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

DROP TRIGGER IF EXISTS admission_decision_no_update_or_delete ON admission.admission_decision;
CREATE TRIGGER admission_decision_no_update_or_delete
    BEFORE UPDATE OR DELETE ON admission.admission_decision
    FOR EACH ROW EXECUTE FUNCTION admission.admission_decision_immutable();

DROP TRIGGER IF EXISTS admission_decision_no_truncate ON admission.admission_decision;
CREATE TRIGGER admission_decision_no_truncate
    BEFORE TRUNCATE ON admission.admission_decision
    FOR EACH STATEMENT EXECUTE FUNCTION admission.admission_decision_immutable();
