-- PEO-03: progressive v2 applicant-owned draft and submission surface.
-- Independent from v1 until authorised reviewer/migration handoff is proven.
-- An applicant cannot use these rows to create an AdmissionDecision/Tenant.
CREATE TABLE admission.client_application_v2 (
 client_application_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 reference text NOT NULL UNIQUE CHECK (reference ~ '^APP-[0-9]{4}-[0-9]{6,}$'),
 applicant_principal_id uuid NOT NULL REFERENCES identity.principal(principal_id),
 application_channel text NOT NULL DEFAULT 'SELF_SERVICE'
   CHECK (application_channel='SELF_SERVICE'),
 status text NOT NULL DEFAULT 'DRAFT'
   CHECK (status IN ('DRAFT','SUBMITTED')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>=1),
 business_identity jsonb,
 requirements jsonb NOT NULL DEFAULT '{}'::jsonb,
 requested_markets jsonb NOT NULL DEFAULT '[]'::jsonb,
 evidence jsonb NOT NULL DEFAULT '[]'::jsonb,
 information_requests jsonb NOT NULL DEFAULT '[]'::jsonb,
 create_idempotency_key varchar(128) NOT NULL
   CHECK (length(create_idempotency_key) BETWEEN 16 AND 128),
 create_request_digest char(64) NOT NULL
   CHECK (create_request_digest ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 submitted_at timestamptz,
 CHECK (business_identity IS NULL OR jsonb_typeof(business_identity)='object'),
 CHECK (jsonb_typeof(requirements)='object'
   AND jsonb_typeof(requested_markets)='array'
   AND jsonb_typeof(evidence)='array'
   AND jsonb_typeof(information_requests)='array'),
 CHECK (status='DRAFT' OR (business_identity IS NOT NULL
   AND jsonb_array_length(requested_markets)>0 AND submitted_at IS NOT NULL)),
 UNIQUE (applicant_principal_id,create_idempotency_key)
);
CREATE INDEX client_application_v2_applicant_idx
 ON admission.client_application_v2(applicant_principal_id,created_at DESC);

CREATE FUNCTION admission.client_application_v2_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
   RAISE EXCEPTION 'progressive application history cannot be deleted' USING ERRCODE='check_violation';
 END IF;
 IF TG_OP='UPDATE' AND (
    ROW(NEW.client_application_id,NEW.reference,NEW.applicant_principal_id,
        NEW.application_channel,NEW.create_idempotency_key,NEW.create_request_digest,NEW.created_at)
    IS DISTINCT FROM
    ROW(OLD.client_application_id,OLD.reference,OLD.applicant_principal_id,
        OLD.application_channel,OLD.create_idempotency_key,OLD.create_request_digest,OLD.created_at)
    OR OLD.status<>'DRAFT'
    OR NEW.status NOT IN ('DRAFT','SUBMITTED')
    OR NEW.version<>OLD.version+1
    OR (NEW.status='DRAFT' AND NEW.submitted_at IS NOT NULL)
    OR (NEW.status='SUBMITTED' AND NEW.submitted_at IS NULL)
 ) THEN
    RAISE EXCEPTION 'invalid progressive application update, approval, or post-submission rewrite'
      USING ERRCODE='check_violation';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER client_application_v2_guard
BEFORE UPDATE OR DELETE ON admission.client_application_v2
FOR EACH ROW EXECUTE FUNCTION admission.client_application_v2_guard();
REVOKE ALL ON admission.client_application_v2 FROM PUBLIC;
