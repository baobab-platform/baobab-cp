-- PEO-03B: submitted v2 applicant claim -> independent review -> immutable
-- decision -> separately requested and authorised v2 onboarding.
-- Deliberately no FK or conversion to v1 client_application/admission_decision;
-- no tenant creation, entitlement, verification or legal actor is inferred.
CREATE TABLE admission.progressive_admission_review (
 review_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 client_application_id uuid NOT NULL UNIQUE REFERENCES admission.client_application_v2(client_application_id),
 organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
 reviewed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
 evidence_reference text NOT NULL CHECK(length(btrim(evidence_reference)) BETWEEN 3 AND 500),
 identity_resolution_policy_reference text NOT NULL CHECK(length(btrim(identity_resolution_policy_reference)) BETWEEN 3 AND 500),
 approved_subscription_type text NOT NULL CHECK(approved_subscription_type IN ('COMMERCIAL','INTERNAL')),
 approved_market_scope text[] NOT NULL CHECK(cardinality(approved_market_scope) BETWEEN 1 AND 50),
 approved_product_requirements text[] NOT NULL DEFAULT '{}',
 approved_isolation_strategy text NOT NULL CHECK(approved_isolation_strategy IN ('schema_per_tenant','row_level_security')),
 reviewed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(cardinality(approved_product_requirements)<=50),
 CHECK(array_position(approved_market_scope,NULL) IS NULL),
 CHECK(array_position(approved_product_requirements,NULL) IS NULL)
);
CREATE TABLE admission.progressive_admission_decision (
 decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 review_id uuid NOT NULL UNIQUE REFERENCES admission.progressive_admission_review(review_id),
 client_application_id uuid NOT NULL UNIQUE REFERENCES admission.client_application_v2(client_application_id),
 decision text NOT NULL CHECK(decision IN ('APPROVED','REJECTED')),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 10 AND 2000),
 decision_evidence_reference text NOT NULL CHECK(length(btrim(decision_evidence_reference)) BETWEEN 3 AND 500),
 decided_by uuid NOT NULL REFERENCES identity.principal(principal_id),
 decided_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE admission.progressive_onboarding_request (
 request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 decision_id uuid NOT NULL UNIQUE REFERENCES admission.progressive_admission_decision(decision_id),
 client_application_id uuid NOT NULL REFERENCES admission.client_application_v2(client_application_id),
 organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
 status text NOT NULL DEFAULT 'REQUESTED' CHECK(status IN ('REQUESTED','AUTHORISED')),
 desired_state jsonb NOT NULL CHECK(jsonb_typeof(desired_state)='object'),
 identity_resolution_policy_reference text NOT NULL CHECK(length(btrim(identity_resolution_policy_reference)) BETWEEN 3 AND 500),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 10 AND 2000),
 correlation_id uuid NOT NULL,
 requested_by uuid NOT NULL REFERENCES identity.principal(principal_id),
 requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 authorised_by uuid REFERENCES identity.principal(principal_id),
 authorised_at timestamptz,
 authorisation_policy_reference text,
 authorisation_evidence_reference text,
 CHECK ((status='REQUESTED' AND authorised_by IS NULL AND authorised_at IS NULL
   AND authorisation_policy_reference IS NULL AND authorisation_evidence_reference IS NULL)
 OR (status='AUTHORISED' AND authorised_by IS NOT NULL AND authorised_at IS NOT NULL
   AND length(btrim(authorisation_policy_reference))>=3
   AND length(btrim(authorisation_evidence_reference))>=3
   AND authorised_by<>requested_by))
);
-- Preserve immutable evidence: no post hoc replacement of approved scope,
-- organisation, reviewer, legal form or decision, even by an admin.
CREATE FUNCTION admission.progressive_bridge_immutable()
RETURNS trigger LANGUAGE plpgsql AS $peo03$
BEGIN
 RAISE EXCEPTION 'PEO-03B admission evidence/decision is immutable'
   USING ERRCODE='check_violation';
END $peo03$;
CREATE TRIGGER progressive_review_immutable BEFORE UPDATE OR DELETE
 ON admission.progressive_admission_review FOR EACH ROW
 EXECUTE FUNCTION admission.progressive_bridge_immutable();
CREATE TRIGGER progressive_decision_immutable BEFORE UPDATE OR DELETE
 ON admission.progressive_admission_decision FOR EACH ROW
 EXECUTE FUNCTION admission.progressive_bridge_immutable();
CREATE FUNCTION admission.progressive_onboarding_guard()
RETURNS trigger LANGUAGE plpgsql AS $peo03$
BEGIN
 IF TG_OP='DELETE' OR OLD.status<>'REQUESTED' OR NEW.status<>'AUTHORISED'
    OR ROW(NEW.request_id,NEW.decision_id,NEW.client_application_id,NEW.organisation_id,
      NEW.desired_state,NEW.identity_resolution_policy_reference,NEW.reason,
      NEW.correlation_id,NEW.requested_by,NEW.requested_at)
    IS DISTINCT FROM
    ROW(OLD.request_id,OLD.decision_id,OLD.client_application_id,OLD.organisation_id,
      OLD.desired_state,OLD.identity_resolution_policy_reference,OLD.reason,
      OLD.correlation_id,OLD.requested_by,OLD.requested_at)
 THEN
   RAISE EXCEPTION 'PEO-03B onboarding authority is one-way and immutable'
     USING ERRCODE='check_violation';
 END IF;
 RETURN NEW;
END $peo03$;
CREATE TRIGGER progressive_onboarding_guard BEFORE UPDATE OR DELETE
 ON admission.progressive_onboarding_request FOR EACH ROW
 EXECUTE FUNCTION admission.progressive_onboarding_guard();
REVOKE ALL ON admission.progressive_admission_review FROM PUBLIC;
REVOKE ALL ON admission.progressive_admission_decision FROM PUBLIC;
REVOKE ALL ON admission.progressive_onboarding_request FROM PUBLIC;
-- Exact replay journal: actor + key identifies one immutable command receipt.
CREATE TABLE admission.progressive_bridge_command (
 actor_id uuid NOT NULL REFERENCES identity.principal(principal_id),
 idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 16 AND 128),
 action text NOT NULL CHECK(action IN ('REVIEW','DECIDE','REQUEST','AUTHORISE')),
 target_id uuid NOT NULL,
 request_digest char(64) NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}
),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_id,idempotency_key)
);
CREATE TRIGGER progressive_bridge_command_immutable BEFORE UPDATE OR DELETE
 ON admission.progressive_bridge_command FOR EACH ROW
 EXECUTE FUNCTION admission.progressive_bridge_immutable();
REVOKE ALL ON admission.progressive_bridge_command FROM PUBLIC;

