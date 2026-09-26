-- ADR-BCP-017 sections 22-24: there is no direct tenant registration.
-- Every tenant records the basis it was registered on:
--
--   ONBOARDING  registered for an AUTHORISED TenantOnboardingRequest, which
--               the same transaction records FULFILLED with this tenant
--   BOOTSTRAP   registered outside the admission workflow for a tenant that
--               predates it (tenant:bootstrap), with its reason and evidence
--   LEGACY      registered before this migration; never written again
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS registration_basis varchar(16) NOT NULL DEFAULT 'LEGACY',
    ADD COLUMN IF NOT EXISTS bootstrap_reason text,
    ADD COLUMN IF NOT EXISTS bootstrap_evidence_reference varchar(255);

-- Existing rows keep LEGACY; the default is removed so a new row must state
-- its basis.
ALTER TABLE tenants ALTER COLUMN registration_basis DROP DEFAULT;

ALTER TABLE tenants DROP CONSTRAINT IF EXISTS tenants_registration_basis_ck;
ALTER TABLE tenants ADD CONSTRAINT tenants_registration_basis_ck CHECK (
    registration_basis IN ('ONBOARDING', 'BOOTSTRAP', 'LEGACY')
    AND (registration_basis <> 'BOOTSTRAP'
         OR (bootstrap_reason IS NOT NULL AND bootstrap_evidence_reference IS NOT NULL
             AND length(btrim(bootstrap_reason)) BETWEEN 20 AND 1000 AND length(btrim(bootstrap_evidence_reference)) >= 1))
    AND (registration_basis = 'BOOTSTRAP' OR (bootstrap_reason IS NULL AND bootstrap_evidence_reference IS NULL))
);

-- A tenant never gains or changes its basis after registration.
CREATE OR REPLACE FUNCTION tenants_registration_basis_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.registration_basis = 'LEGACY' THEN
        RAISE EXCEPTION 'a new tenant must be registered for an onboarding request or as a bootstrap tenant'
            USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.registration_basis IS DISTINCT FROM OLD.registration_basis
        OR NEW.bootstrap_reason IS DISTINCT FROM OLD.bootstrap_reason
        OR NEW.bootstrap_evidence_reference IS DISTINCT FROM OLD.bootstrap_evidence_reference) THEN
        RAISE EXCEPTION 'a tenant''s registration basis is immutable' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS tenants_registration_basis_guard ON tenants;
CREATE TRIGGER tenants_registration_basis_guard
    BEFORE INSERT OR UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION tenants_registration_basis_guard();

-- An ONBOARDING tenant commits only together with the FULFILLED request that
-- produced it: checked at commit, after registration has fulfilled it.
CREATE OR REPLACE FUNCTION tenants_onboarding_fulfilled_check() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.registration_basis = 'ONBOARDING' AND NOT EXISTS (
        SELECT 1 FROM admission.tenant_onboarding_request
        WHERE tenant_id = NEW.tenant_id AND status = 'FULFILLED') THEN
        RAISE EXCEPTION 'tenant % has no FULFILLED onboarding request', NEW.tenant_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS tenants_onboarding_fulfilled_check ON tenants;
CREATE CONSTRAINT TRIGGER tenants_onboarding_fulfilled_check
    AFTER INSERT ON tenants
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION tenants_onboarding_fulfilled_check();
