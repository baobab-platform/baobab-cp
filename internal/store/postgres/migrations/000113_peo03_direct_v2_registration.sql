-- PEO-03C: governed v2 admission authority consumed directly by v2 tenant
-- registration. A v2 request is not translated into the legacy v1 request.
-- This migration only permits authorised request -> fulfilled, in the SAME
-- transaction as tenant registration, PRIMARY binding and provisioning outbox.
ALTER TABLE admission.progressive_onboarding_request
  DROP CONSTRAINT progressive_onboarding_request_status_check;
ALTER TABLE admission.progressive_onboarding_request
  ADD CONSTRAINT progressive_onboarding_request_status_check
  CHECK (status IN ('REQUESTED','AUTHORISED','FULFILLED'));
ALTER TABLE admission.progressive_onboarding_request
  ADD COLUMN tenant_id text UNIQUE REFERENCES tenants(tenant_id),
  ADD COLUMN fulfilled_at timestamptz;
ALTER TABLE admission.progressive_onboarding_request
  DROP CONSTRAINT progressive_onboarding_request_check;
ALTER TABLE admission.progressive_onboarding_request
  ADD CONSTRAINT progressive_onboarding_request_check CHECK (
    (status='REQUESTED' AND authorised_by IS NULL AND authorised_at IS NULL
      AND authorisation_policy_reference IS NULL
      AND authorisation_evidence_reference IS NULL
      AND tenant_id IS NULL AND fulfilled_at IS NULL)
    OR (status='AUTHORISED' AND authorised_by IS NOT NULL AND authorised_at IS NOT NULL
      AND length(btrim(authorisation_policy_reference))>=3
      AND length(btrim(authorisation_evidence_reference))>=3
      AND authorised_by<>requested_by AND tenant_id IS NULL AND fulfilled_at IS NULL)
    OR (status='FULFILLED' AND authorised_by IS NOT NULL AND authorised_at IS NOT NULL
      AND length(btrim(authorisation_policy_reference))>=3
      AND length(btrim(authorisation_evidence_reference))>=3
      AND authorised_by<>requested_by AND tenant_id IS NOT NULL AND fulfilled_at IS NOT NULL)
  );
CREATE OR REPLACE FUNCTION admission.progressive_onboarding_guard()
RETURNS trigger LANGUAGE plpgsql AS $peo03$
BEGIN
 IF TG_OP='DELETE' OR
    NOT ((OLD.status='REQUESTED' AND NEW.status='AUTHORISED')
         OR (OLD.status='AUTHORISED' AND NEW.status='FULFILLED'
             AND NEW.tenant_id IS NOT NULL AND NEW.fulfilled_at IS NOT NULL))
    OR ROW(NEW.request_id,NEW.decision_id,NEW.client_application_id,NEW.organisation_id,
      NEW.desired_state,NEW.identity_resolution_policy_reference,NEW.reason,
      NEW.correlation_id,NEW.requested_by,NEW.requested_at,
      NEW.authorised_by,NEW.authorised_at,NEW.authorisation_policy_reference,
      NEW.authorisation_evidence_reference)
    IS DISTINCT FROM
    ROW(OLD.request_id,OLD.decision_id,OLD.client_application_id,OLD.organisation_id,
      OLD.desired_state,OLD.identity_resolution_policy_reference,OLD.reason,
      OLD.correlation_id,OLD.requested_by,OLD.requested_at,
      CASE WHEN OLD.status='REQUESTED' THEN NEW.authorised_by ELSE OLD.authorised_by END,
      CASE WHEN OLD.status='REQUESTED' THEN NEW.authorised_at ELSE OLD.authorised_at END,
      CASE WHEN OLD.status='REQUESTED' THEN NEW.authorisation_policy_reference ELSE OLD.authorisation_policy_reference END,
      CASE WHEN OLD.status='REQUESTED' THEN NEW.authorisation_evidence_reference ELSE OLD.authorisation_evidence_reference END)
 THEN
   RAISE EXCEPTION 'PEO-03C onboarding transition or authority mutation denied'
     USING ERRCODE='check_violation';
 END IF;
 IF OLD.status='REQUESTED' AND (NEW.tenant_id IS NOT NULL OR NEW.fulfilled_at IS NOT NULL) THEN
   RAISE EXCEPTION 'request authorisation cannot register tenant'
     USING ERRCODE='check_violation';
 END IF;
 RETURN NEW;
END $peo03$;
-- Deferred integrity ensures uncommitted tenant and PRIMARY mapping are
-- both present; no success with a nonmatching or deleted Organisation.
CREATE OR REPLACE FUNCTION admission.progressive_registration_primary_guard()
RETURNS trigger LANGUAGE plpgsql AS $peo03$
BEGIN
 IF NEW.status='FULFILLED' AND NOT EXISTS (
   SELECT 1 FROM registry.tenant_organisation_mapping m
   JOIN tenants t ON t.tenant_id=m.tenant_id
   WHERE m.tenant_id=NEW.tenant_id
     AND m.organisation_id=NEW.organisation_id
     AND m.mapping_role='PRIMARY_ORGANISATION'
     AND m.status='ACTIVE' AND t.primary_organisation_enforced=true
 ) THEN
   RAISE EXCEPTION 'PEO-03C registration lacks reviewed PRIMARY Organisation'
     USING ERRCODE='check_violation';
 END IF;
 RETURN NULL;
END $peo03$;
CREATE CONSTRAINT TRIGGER progressive_registration_primary_guard
 AFTER UPDATE ON admission.progressive_onboarding_request
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
 EXECUTE FUNCTION admission.progressive_registration_primary_guard();

-- Migration 000056 originally admitted only v1 FULFILLED requests. Preserve
-- that guard but also accept this independently governed v2 lifecycle. A v2
-- status alone is insufficient: the exact request must bind the active
-- reviewed PRIMARY Organisation and an approved decision.
CREATE OR REPLACE FUNCTION tenants_onboarding_fulfilled_check()
RETURNS trigger LANGUAGE plpgsql AS $peo03$
BEGIN
 IF NEW.registration_basis='ONBOARDING' AND NOT (
    EXISTS (SELECT 1 FROM admission.tenant_onboarding_request r
            WHERE r.tenant_id=NEW.tenant_id AND r.status='FULFILLED')
    OR EXISTS (
       SELECT 1 FROM admission.progressive_onboarding_request o
       JOIN admission.progressive_admission_decision d ON d.decision_id=o.decision_id
       JOIN registry.tenant_organisation_mapping m
         ON m.tenant_id=o.tenant_id
        AND m.organisation_id=o.organisation_id
       WHERE o.tenant_id=NEW.tenant_id AND o.status='FULFILLED'
         AND d.decision='APPROVED'
         AND m.mapping_role='PRIMARY_ORGANISATION' AND m.status='ACTIVE'
    )
 ) THEN
   RAISE EXCEPTION 'tenant % has no valid FULFILLED v1 or v2 onboarding request',
      NEW.tenant_id USING ERRCODE='check_violation';
 END IF;
 RETURN NULL;
END $peo03$;
