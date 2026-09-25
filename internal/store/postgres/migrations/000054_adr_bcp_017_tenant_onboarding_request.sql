-- ADR-BCP-017 sections 22-24, 39, 41, 44, 46 — TenantOnboardingRequest: the
-- governed handoff from an APPROVED AdmissionDecision to provisioning.
-- Contract: baobab-platform/shared contracts/admission/v1/onboarding.schema.json
-- and onboarding-lifecycle.yaml.
--
-- Approval activates nothing: a request exists only when an onboarding
-- requester (never the applicant or the decider) makes one, and provisioning
-- may fulfil it only after a different principal authorised it. The desired
-- state's classification, markets, products and isolation are copied from
-- the decision. At most one live request exists per decision, a tenant is
-- produced by at most one request, and the decision is never rewritten.

CREATE TABLE IF NOT EXISTS admission.tenant_onboarding_request (
    tenant_onboarding_request_id uuid PRIMARY KEY,
    client_application_id        uuid NOT NULL REFERENCES admission.client_application(client_application_id),
    admission_decision_id        uuid NOT NULL REFERENCES admission.admission_decision(admission_decision_id),
    status                       text NOT NULL,
    display_name                 text NOT NULL,
    residency_region             text NOT NULL,
    isolation_strategy           text NOT NULL,
    subscription_type            text NOT NULL,
    market_scope                 text[] NOT NULL,
    product_requirements         text[] NOT NULL,
    reason                       text NOT NULL,
    correlation_id               uuid NOT NULL,
    requested_by                 uuid NOT NULL REFERENCES identity.principal(principal_id),
    requested_at                 timestamptz NOT NULL,
    authorised_by                uuid REFERENCES identity.principal(principal_id),
    authorised_at                timestamptz,
    tenant_id                    varchar(63) REFERENCES tenants(tenant_id),
    fulfilled_at                 timestamptz,
    cancelled_by                 uuid REFERENCES identity.principal(principal_id),
    cancelled_at                 timestamptz,
    cancellation_reason          text,
    updated_at                   timestamptz NOT NULL DEFAULT now(),
    CHECK (status IN ('REQUESTED','AUTHORISED','FULFILLED','CANCELLED')),
    CHECK (length(display_name) BETWEEN 1 AND 255),
    CHECK (residency_region ~ '^[a-z]{2}-[a-z]+-[0-9]+$'),
    CHECK (isolation_strategy IN ('schema_per_tenant','row_level_security')),
    CHECK (subscription_type IN ('COMMERCIAL','INTERNAL','TRIAL','PARTNER','MANUAL','MIGRATION')),
    CHECK (cardinality(market_scope) >= 1),
    CHECK (length(reason) BETWEEN 1 AND 2000),
    CHECK (cancellation_reason IS NULL OR length(cancellation_reason) BETWEEN 1 AND 2000),
    -- Section 39: the authoriser is never the requester.
    CHECK (authorised_by IS NULL OR authorised_by <> requested_by),
    CONSTRAINT tenant_onboarding_request_status_shape CHECK (
        CASE status
        WHEN 'REQUESTED' THEN authorised_by IS NULL AND authorised_at IS NULL AND tenant_id IS NULL AND fulfilled_at IS NULL
            AND cancelled_by IS NULL AND cancelled_at IS NULL AND cancellation_reason IS NULL
        WHEN 'AUTHORISED' THEN authorised_by IS NOT NULL AND authorised_at IS NOT NULL AND tenant_id IS NULL
            AND fulfilled_at IS NULL AND cancelled_by IS NULL AND cancelled_at IS NULL AND cancellation_reason IS NULL
        WHEN 'FULFILLED' THEN authorised_by IS NOT NULL AND authorised_at IS NOT NULL AND tenant_id IS NOT NULL
            AND fulfilled_at IS NOT NULL AND cancelled_by IS NULL AND cancelled_at IS NULL AND cancellation_reason IS NULL
        ELSE cancelled_by IS NOT NULL AND cancelled_at IS NOT NULL AND cancellation_reason IS NOT NULL
            AND tenant_id IS NULL AND fulfilled_at IS NULL
        END)
);

-- Section 46: one live request per decision; replays return it.
CREATE UNIQUE INDEX IF NOT EXISTS tenant_onboarding_request_live_decision_uniq
    ON admission.tenant_onboarding_request (admission_decision_id)
    WHERE status IN ('REQUESTED','AUTHORISED','FULFILLED');
-- A tenant is produced by at most one request (section 41 traceability).
CREATE UNIQUE INDEX IF NOT EXISTS tenant_onboarding_request_tenant_uniq
    ON admission.tenant_onboarding_request (tenant_id) WHERE tenant_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS tenant_onboarding_request_status_idx
    ON admission.tenant_onboarding_request (status, requested_at DESC);

-- The lifecycle (onboarding-lifecycle.yaml) and the request's identity and
-- desired state are enforced here too: only permitted transitions, nothing
-- about the request itself ever changes, and no row is deleted.
CREATE OR REPLACE FUNCTION admission.tenant_onboarding_request_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'tenant_onboarding_request rows are never deleted (ADR-BCP-017 section 44)';
    END IF;
    IF NOT ((OLD.status = 'REQUESTED' AND NEW.status IN ('AUTHORISED','CANCELLED'))
         OR (OLD.status = 'AUTHORISED' AND NEW.status IN ('FULFILLED','CANCELLED'))) THEN
        RAISE EXCEPTION 'tenant onboarding request % -> % is not a permitted transition', OLD.status, NEW.status;
    END IF;
    IF NEW.tenant_onboarding_request_id <> OLD.tenant_onboarding_request_id
        OR NEW.client_application_id <> OLD.client_application_id OR NEW.admission_decision_id <> OLD.admission_decision_id
        OR NEW.display_name <> OLD.display_name OR NEW.residency_region <> OLD.residency_region
        OR NEW.isolation_strategy <> OLD.isolation_strategy OR NEW.subscription_type <> OLD.subscription_type
        OR NEW.market_scope <> OLD.market_scope OR NEW.product_requirements <> OLD.product_requirements
        OR NEW.reason <> OLD.reason OR NEW.correlation_id <> OLD.correlation_id
        OR NEW.requested_by <> OLD.requested_by OR NEW.requested_at <> OLD.requested_at
        OR (OLD.authorised_by IS NOT NULL AND (NEW.authorised_by IS DISTINCT FROM OLD.authorised_by
            OR NEW.authorised_at IS DISTINCT FROM OLD.authorised_at)) THEN
        RAISE EXCEPTION 'a tenant onboarding request only changes status (ADR-BCP-017 section 23)';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS tenant_onboarding_request_guard ON admission.tenant_onboarding_request;
CREATE TRIGGER tenant_onboarding_request_guard
    BEFORE UPDATE OR DELETE ON admission.tenant_onboarding_request
    FOR EACH ROW EXECUTE FUNCTION admission.tenant_onboarding_request_guard();
