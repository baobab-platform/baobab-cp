-- LA-03: immutable, governed, pre-tenant Organisation identity binding.
-- An AUTHORISED TenantOnboardingRequest must bind exactly one reviewed
-- Organisation before a v2 tenant registration transaction can fulfil it.
-- This is NOT legal incorporation evidence, ownership, or a legal-actor mandate.
CREATE TABLE admission.tenant_onboarding_organisation (
    tenant_onboarding_request_id uuid PRIMARY KEY
      REFERENCES admission.tenant_onboarding_request(tenant_onboarding_request_id),
    organisation_id uuid NOT NULL
      REFERENCES registry.organisation_profile(canonical_entity_id),
    admission_decision_id uuid NOT NULL
      REFERENCES admission.admission_decision(admission_decision_id),
    reviewed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    identity_resolution_policy_reference text NOT NULL CHECK (length(btrim(identity_resolution_policy_reference)) > 0),
    evidence_reference text NOT NULL CHECK (length(btrim(evidence_reference)) > 0),
    bound_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organisation_id)
);

-- No implicit reassignment once an identity is approved; corrections need a
-- separately reviewed forward lifecycle rather than editing this history.
CREATE OR REPLACE FUNCTION admission.tenant_onboarding_organisation_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'onboarding Organisation identity binding is immutable'
          USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM admission.tenant_onboarding_request r
        JOIN registry.organisation_profile op
          ON op.canonical_entity_id = NEW.organisation_id
        JOIN registry.canonical_entity ce
          ON ce.canonical_entity_id = op.canonical_entity_id
        WHERE r.tenant_onboarding_request_id = NEW.tenant_onboarding_request_id
          AND r.admission_decision_id = NEW.admission_decision_id
          AND r.status = 'AUTHORISED'
          AND NEW.reviewed_by <> r.requested_by
          AND NEW.reviewed_by <> r.authorised_by
          AND op.status = 'ACTIVE'
          AND ce.entity_type = 'ORGANISATION'
          AND ce.tenant_id IS NULL
    ) THEN
        RAISE EXCEPTION 'pre-tenant Organisation requires AUTHORISED request, independent review and unassigned active Organisation'
          USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_onboarding_organisation_guard
    BEFORE INSERT OR UPDATE OR DELETE ON admission.tenant_onboarding_organisation
    FOR EACH ROW EXECUTE FUNCTION admission.tenant_onboarding_organisation_guard();

-- Guard v2 registration's claim of ownership: a request cannot be fulfilled
-- by any tenant which is mapped to another primary Organisation. The actual
-- mapping and fulfilment also remain subject to migrations 000056 and 000102.
CREATE OR REPLACE FUNCTION admission.tenant_onboarding_organisation_match()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'FULFILLED'
       AND EXISTS (SELECT 1 FROM admission.tenant_onboarding_organisation b
          WHERE b.tenant_onboarding_request_id = NEW.tenant_onboarding_request_id)
       AND NOT EXISTS (
          SELECT 1 FROM admission.tenant_onboarding_organisation b
          JOIN registry.tenant_organisation_mapping m
            ON m.organisation_id = b.organisation_id
           AND m.tenant_id = NEW.tenant_id
           AND m.mapping_role = 'PRIMARY_ORGANISATION'
           AND m.status = 'ACTIVE'
          WHERE b.tenant_onboarding_request_id = NEW.tenant_onboarding_request_id
       ) THEN
        RAISE EXCEPTION 'fulfilled tenant PRIMARY Organisation does not match reviewed identity binding'
          USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER tenant_onboarding_organisation_match
    AFTER UPDATE ON admission.tenant_onboarding_request
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION admission.tenant_onboarding_organisation_match();
