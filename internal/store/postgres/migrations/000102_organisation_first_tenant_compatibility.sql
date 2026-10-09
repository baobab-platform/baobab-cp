-- LA-02 / Accepted ADR-BCP-026 and ADR-BCP-027 (Shared LA-01).
-- Additive compatibility migration: never create a fictitious legal person,
-- infer incorporation from a first-party registry id or alter a historic actor.
-- The v1 API remains legal_entity_id-required until LA-03.
--
-- Legacy rows with no provable PRIMARY mapping are grandfathered explicitly
-- for investigation, not silently given an invented Organisation. Direct
-- legacy/bootstrap writers with a real LegalEntity retain a temporary
-- non-enforcing default; the governed Store.RegisterTenant path explicitly
-- sets true. Any onboarding row, or any row with NULL legal projection,
-- MUST enforce PRIMARY integrity. LA-03 retires this bootstrap compatibility
-- window once remaining writers are migrated.
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS primary_organisation_enforced boolean;

-- An already valid, in-effect mapping is the ONLY evidence eligible for
-- automatic backfill. Do not derive PRIMARY from DEFAULT LegalEntity:
-- Nabhold can be the legal actor of a distinct ZuriBeans Organisation.
UPDATE tenants t
SET primary_organisation_enforced = (
    SELECT count(*) = 1
    FROM registry.tenant_organisation_mapping m
    JOIN registry.organisation_profile op
      ON op.canonical_entity_id = m.organisation_id
    JOIN registry.canonical_entity ce
      ON ce.canonical_entity_id = m.organisation_id
    WHERE m.tenant_id = t.tenant_id
      AND m.mapping_role = 'PRIMARY_ORGANISATION'
      AND m.status = 'ACTIVE'
      AND m.effective_from <= now()
      AND (m.effective_to IS NULL OR m.effective_to > now())
      AND op.status = 'ACTIVE'
      AND ce.entity_type IN ('ORGANISATION','BUYER_ORGANISATION','SUPPLIER_ORGANISATION')
)
WHERE primary_organisation_enforced IS NULL;

-- Preserve old direct legacy BOOTSTRAP clients until LA-03. Production
-- Store.RegisterTenant writes true explicitly; nullable/ONBOARDING commands
-- cannot opt out via the guard below.
ALTER TABLE tenants ALTER COLUMN primary_organisation_enforced SET DEFAULT false;
ALTER TABLE tenants ALTER COLUMN primary_organisation_enforced SET NOT NULL;

-- Nullable v2 compatibility projection; FK to real legal_entities remains.
-- No placeholder legal entity is minted on NULL.
ALTER TABLE tenants ALTER COLUMN legal_entity_id DROP NOT NULL;

CREATE TABLE IF NOT EXISTS registry.tenant_primary_organisation_migration_review (
    tenant_id varchar(63) PRIMARY KEY REFERENCES tenants(tenant_id),
    reason text NOT NULL CHECK (reason IN ('MISSING_OR_INACTIVE_PRIMARY')),
    provenance text NOT NULL DEFAULT 'migration-000102-no-inferred-identity',
    discovered_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    resolution_evidence_reference text,
    CHECK ((resolved_at IS NULL) = (resolution_evidence_reference IS NULL))
);

INSERT INTO registry.tenant_primary_organisation_migration_review (tenant_id, reason)
SELECT t.tenant_id, 'MISSING_OR_INACTIVE_PRIMARY'
FROM tenants t
WHERE t.primary_organisation_enforced = false
ON CONFLICT (tenant_id) DO NOTHING;

-- Fail closed for ONBOARDING and new defaultless operating tenants, and
-- prevent any enforced tenant from downgrading. A direct legacy BOOTSTRAP
-- with non-NULL legal_entity_id is not certified: it remains in the review
-- queue and MUST NOT be mistaken for a new Organisation-first provision.
CREATE OR REPLACE FUNCTION registry.tenant_primary_enforcement_guard()
RETURNS trigger LANGUAGE plpgsql AS $
BEGIN
    IF TG_OP = 'INSERT' AND
      (NEW.registration_basis = 'ONBOARDING' OR NEW.legal_entity_id IS NULL) AND
      NEW.primary_organisation_enforced IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'onboarding or defaultless tenant must enforce PRIMARY Organisation'
            USING ERRCODE = 'check_violation';
    ELSIF TG_OP = 'UPDATE'
      AND OLD.primary_organisation_enforced = true
      AND NEW.primary_organisation_enforced IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'cannot weaken PRIMARY Organisation enforcement'
            USING ERRCODE = 'check_violation';
    ELSIF TG_OP = 'UPDATE'
      AND (NEW.registration_basis = 'ONBOARDING' OR NEW.legal_entity_id IS NULL)
      AND NEW.primary_organisation_enforced IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'onboarding or defaultless tenant cannot disable PRIMARY Organisation'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$;
CREATE TRIGGER tenants_primary_organisation_guard
    BEFORE INSERT OR UPDATE OF primary_organisation_enforced ON tenants
    FOR EACH ROW EXECUTE FUNCTION registry.tenant_primary_enforcement_guard();

-- Make every not-yet-migrated direct BOOTSTRAP row discoverable, even
-- if inserted after the migration. A privileged actor must reconcile and
-- explicitly promote it to enforced mode with real PRIMARY provenance.
CREATE OR REPLACE FUNCTION registry.record_legacy_tenant_primary_review()
RETURNS trigger LANGUAGE plpgsql AS $
BEGIN
    IF NEW.primary_organisation_enforced = false THEN
        INSERT INTO registry.tenant_primary_organisation_migration_review (
            tenant_id, reason, provenance
        ) VALUES (
            NEW.tenant_id, 'MISSING_OR_INACTIVE_PRIMARY',
            'migration-000102-legacy-bootstrap-compatibility'
        ) ON CONFLICT (tenant_id) DO NOTHING;
    END IF;
    RETURN NULL;
END;
$;
CREATE TRIGGER tenants_legacy_primary_review
    AFTER INSERT ON tenants
    FOR EACH ROW EXECUTE FUNCTION registry.record_legacy_tenant_primary_review();

-- Deferred commit-time checks allow v1 registration to create a tenant,
-- legal mapping and primary organisation mapping in the SAME transaction.
-- Existence + entity type + validity are independently verified; legal
-- mapping is not allowed to select or redefine the primary Organisation.
CREATE OR REPLACE FUNCTION registry.assert_tenant_primary_organisation(p_tenant_id varchar)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    t tenants%ROWTYPE;
    valid_primaries integer;
    projection text;
BEGIN
    SELECT * INTO t FROM tenants WHERE tenant_id = p_tenant_id;
    IF NOT FOUND OR NOT t.primary_organisation_enforced THEN
        RETURN;
    END IF;

    SELECT count(*) INTO valid_primaries
    FROM registry.tenant_organisation_mapping m
    JOIN registry.organisation_profile op ON op.canonical_entity_id = m.organisation_id
    JOIN registry.canonical_entity ce ON ce.canonical_entity_id = m.organisation_id
    WHERE m.tenant_id = p_tenant_id
      AND m.mapping_role = 'PRIMARY_ORGANISATION' AND m.status = 'ACTIVE'
      AND m.effective_from <= now()
      AND (m.effective_to IS NULL OR m.effective_to > now())
      AND op.status = 'ACTIVE'
      AND ce.entity_type IN ('ORGANISATION','BUYER_ORGANISATION','SUPPLIER_ORGANISATION');

    IF valid_primaries <> 1 THEN
        RAISE EXCEPTION 'tenant % requires exactly one in-effect PRIMARY Organisation (got %)', p_tenant_id, valid_primaries
            USING ERRCODE = 'check_violation';
    END IF;

    SELECT m.legal_entity_id INTO projection
    FROM registry.tenant_legal_entity_mapping m
    WHERE m.tenant_id = p_tenant_id
      AND m.mapping_role = 'DEFAULT'
      AND m.status IN ('PENDING','ACTIVE','SUSPENDED')
    LIMIT 1;

    IF projection IS DISTINCT FROM t.legal_entity_id THEN
        RAISE EXCEPTION 'tenant % legal_entity_id differs from DEFAULT compatibility mapping', p_tenant_id
            USING ERRCODE = 'check_violation';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION registry.tenant_primary_integrity_trigger()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'tenants' THEN
        PERFORM registry.assert_tenant_primary_organisation(NEW.tenant_id);
    ELSIF TG_OP = 'DELETE' THEN
        PERFORM registry.assert_tenant_primary_organisation(OLD.tenant_id);
    ELSE
        PERFORM registry.assert_tenant_primary_organisation(NEW.tenant_id);
        IF TG_OP = 'UPDATE' AND OLD.tenant_id IS DISTINCT FROM NEW.tenant_id THEN
            PERFORM registry.assert_tenant_primary_organisation(OLD.tenant_id);
        END IF;
    END IF;
    RETURN NULL;
END;
$$;

-- Creation and changes of enforced tenants cannot commit without a PRIMARY.
CREATE CONSTRAINT TRIGGER tenants_primary_organisation_integrity
    AFTER INSERT OR UPDATE ON tenants
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION registry.tenant_primary_integrity_trigger();

-- A mutation removing/ending the last PRIMARY mapping is also checked at
-- commit, not just the next time the Tenant itself is updated.
CREATE CONSTRAINT TRIGGER tenant_organisation_primary_integrity
    AFTER INSERT OR UPDATE OR DELETE ON registry.tenant_organisation_mapping
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION registry.tenant_primary_integrity_trigger();

-- The nullable compatibility projection MUST remain consistent even when
-- the DEFAULT mapping changes without an UPDATE to tenants.
CREATE CONSTRAINT TRIGGER tenant_legal_entity_projection_integrity
    AFTER INSERT OR UPDATE OR DELETE ON registry.tenant_legal_entity_mapping
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION registry.tenant_primary_integrity_trigger();

CREATE INDEX IF NOT EXISTS tenant_primary_organisation_review_open_idx
    ON registry.tenant_primary_organisation_migration_review(discovered_at)
    WHERE resolved_at IS NULL;
