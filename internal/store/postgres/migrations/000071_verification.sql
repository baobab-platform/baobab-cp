-- ADR-BCP-023 gate OEV-03: the verification workflow (Shared evidence/v1).
-- Each table keeps structured the columns lifecycle, integrity and the
-- evidence chain depend on (ids, links, status, who asserted, checked,
-- decided or resolved, and version), and the rest of the record as the
-- Shared document it is validated against (section 275: flexible
-- attributes, never core semantics). Evidence content is never stored here
-- (section 13): a record points at its source record or credential.

CREATE SCHEMA IF NOT EXISTS evidence;

CREATE TABLE evidence.record (
    evidence_id     text PRIMARY KEY CHECK (evidence_id ~ '^evr_[a-z0-9]+$'),
    source_id       text NOT NULL,
    status          text NOT NULL CHECK (status IN ('RECEIVED', 'QUARANTINED', 'AVAILABLE', 'SUPERSEDED', 'RESTRICTED', 'EXPIRED', 'DESTROYED')),
    supersedes      text REFERENCES evidence.record(evidence_id),
    obtained_by     text,
    submitted_by    text,
    document        jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
    version         bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at      timestamptz NOT NULL,
    create_idempotency_key text,
    create_request_hash    text,
    CHECK (obtained_by IS NOT NULL OR submitted_by IS NOT NULL),
    CHECK ((create_idempotency_key IS NULL) = (create_request_hash IS NULL))
);
CREATE UNIQUE INDEX evidence_record_idempotency_uniq
    ON evidence.record(obtained_by, create_idempotency_key) WHERE create_idempotency_key IS NOT NULL;

CREATE TABLE evidence.verification_case (
    case_id        text PRIMARY KEY CHECK (case_id ~ '^vcase_[a-z0-9]+$'),
    subject_type   text NOT NULL,
    subject_id     text NOT NULL,
    status         text NOT NULL CHECK (status IN ('DRAFT', 'COLLECTING_EVIDENCE', 'READY_FOR_REVIEW', 'VERIFYING', 'INFORMATION_REQUIRED',
                       'CONFLICTED', 'VERIFIED', 'NOT_VERIFIED', 'CANCELLED', 'SUPERSEDED', 'EXPIRED')),
    opened_by      text NOT NULL,
    opened_at      timestamptz NOT NULL,
    completed_at   timestamptz,
    document       jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
    version        bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    create_idempotency_key text,
    create_request_hash    text,
    CHECK ((status IN ('VERIFIED', 'NOT_VERIFIED', 'CANCELLED', 'SUPERSEDED', 'EXPIRED')) = (completed_at IS NOT NULL)),
    CHECK ((create_idempotency_key IS NULL) = (create_request_hash IS NULL))
);
CREATE UNIQUE INDEX verification_case_idempotency_uniq
    ON evidence.verification_case(opened_by, create_idempotency_key) WHERE create_idempotency_key IS NOT NULL;
CREATE INDEX verification_case_list_idx ON evidence.verification_case(opened_at DESC, case_id DESC);

CREATE TABLE evidence.claim (
    claim_id          text PRIMARY KEY CHECK (claim_id ~ '^ecl_[a-z0-9]+$'),
    case_id           text NOT NULL REFERENCES evidence.verification_case(case_id),
    asserted_by       text NOT NULL,
    status            text NOT NULL CHECK (status IN ('SELF_ASSERTED', 'UNDER_VERIFICATION', 'VERIFIED', 'PARTIALLY_VERIFIED', 'NOT_VERIFIED',
                          'CONFLICTED', 'EXPIRED', 'REVOKED', 'WITHDRAWN', 'SUPERSEDED')),
    current_result_id text,
    document          jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
    version           bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at        timestamptz NOT NULL,
    -- A verified, unverified or conflicted standing comes only from a result.
    CHECK (status IN ('SELF_ASSERTED', 'UNDER_VERIFICATION', 'WITHDRAWN', 'SUPERSEDED') OR current_result_id IS NOT NULL)
);
CREATE INDEX claim_case_idx ON evidence.claim(case_id, created_at, claim_id);

CREATE TABLE evidence.claim_evidence (
    claim_id    text NOT NULL REFERENCES evidence.claim(claim_id),
    evidence_id text NOT NULL REFERENCES evidence.record(evidence_id),
    PRIMARY KEY (claim_id, evidence_id)
);

CREATE TABLE evidence.check (
    check_id     text PRIMARY KEY CHECK (check_id ~ '^vchk_[a-z0-9]+$'),
    case_id      text NOT NULL REFERENCES evidence.verification_case(case_id),
    claim_id     text NOT NULL REFERENCES evidence.claim(claim_id),
    source_id    text NOT NULL,
    outcome      text NOT NULL,
    performed_by text NOT NULL,
    performed_at timestamptz NOT NULL,
    document     jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object')
);
CREATE INDEX check_case_idx ON evidence.check(case_id, performed_at, check_id);

CREATE TABLE evidence.discrepancy (
    discrepancy_id text PRIMARY KEY CHECK (discrepancy_id ~ '^edis_[a-z0-9]+$'),
    case_id        text NOT NULL REFERENCES evidence.verification_case(case_id),
    status         text NOT NULL CHECK (status IN ('OPEN', 'UNDER_REVIEW', 'RESOLVED', 'ACCEPTED_EXCEPTION', 'FALSE_POSITIVE', 'SUPERSEDED')),
    resolved_by    text,
    detected_at    timestamptz NOT NULL,
    document       jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
    version        bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    CHECK ((status IN ('RESOLVED', 'ACCEPTED_EXCEPTION', 'FALSE_POSITIVE')) = (resolved_by IS NOT NULL))
);
CREATE INDEX discrepancy_case_idx ON evidence.discrepancy(case_id, detected_at, discrepancy_id);

CREATE TABLE evidence.result (
    result_id  text PRIMARY KEY CHECK (result_id ~ '^vres_[a-z0-9]+$'),
    case_id    text NOT NULL REFERENCES evidence.verification_case(case_id),
    claim_id   text NOT NULL REFERENCES evidence.claim(claim_id),
    outcome    text NOT NULL,
    decided_by text NOT NULL,
    decided_at timestamptz NOT NULL,
    supersedes text REFERENCES evidence.result(result_id),
    document   jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object')
);
CREATE INDEX result_case_idx ON evidence.result(case_id, decided_at, result_id);

ALTER TABLE evidence.claim ADD CONSTRAINT claim_current_result_fk
    FOREIGN KEY (current_result_id) REFERENCES evidence.result(result_id);

-- Nobody checks or decides a claim they asserted (section 169).
CREATE FUNCTION evidence.refuse_self_verification() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    asserter text;
    actor text;
BEGIN
    SELECT asserted_by INTO asserter FROM evidence.claim WHERE claim_id = NEW.claim_id;
    IF TG_TABLE_NAME = 'check' THEN actor := NEW.performed_by; ELSE actor := NEW.decided_by; END IF;
    IF asserter = actor THEN
        RAISE EXCEPTION 'a claim is never checked or decided by its asserter' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER check_not_self BEFORE INSERT ON evidence.check FOR EACH ROW EXECUTE FUNCTION evidence.refuse_self_verification();
CREATE TRIGGER result_not_self BEFORE INSERT ON evidence.result FOR EACH ROW EXECUTE FUNCTION evidence.refuse_self_verification();
