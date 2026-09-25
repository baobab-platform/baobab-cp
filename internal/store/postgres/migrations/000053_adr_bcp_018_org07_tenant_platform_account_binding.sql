-- ADR-BCP-018 gate ORG-07 — explicit tenant PlatformAccount binding
-- (sections 45, 48, 119, 141, 152) and the section 83 account lifecycle.
-- Contract: baobab-platform/shared contracts/organisation/v1/platform.schema.json.
--
-- A binding records which PlatformAccount's commercial terms a tenant
-- consumes under. It is explicit and effective-dated; a tenant has at most
-- one ACTIVE binding and may have none. It is commercial provenance only:
-- nothing resolves access, joins tenants or changes grants through it, and
-- it is never inferred from organisation or account membership.

CREATE TABLE IF NOT EXISTS registry.tenant_platform_account_binding (
    binding_id           uuid PRIMARY KEY,
    tenant_id            varchar(63) NOT NULL REFERENCES tenants(tenant_id),
    platform_account_id  uuid NOT NULL REFERENCES registry.platform_account(platform_account_id),
    -- The tenant's primary organisation whose live account membership
    -- justified the binding when it was made. Recorded, never resolved.
    organisation_id      uuid NOT NULL REFERENCES registry.canonical_entity(canonical_entity_id),
    status               text NOT NULL,
    reason               text NOT NULL,
    evidence_reference   text,
    bound_by             uuid NOT NULL REFERENCES identity.principal(principal_id),
    effective_from       timestamptz NOT NULL,
    effective_to         timestamptz,
    end_reason           text,
    ended_by             uuid REFERENCES identity.principal(principal_id),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CHECK (status IN ('ACTIVE','ENDED')),
    CHECK (length(reason) BETWEEN 1 AND 2000),
    CHECK (end_reason IS NULL OR length(end_reason) BETWEEN 1 AND 2000),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CONSTRAINT tenant_platform_account_binding_end_shape CHECK (
        (status = 'ACTIVE' AND effective_to IS NULL AND end_reason IS NULL AND ended_by IS NULL)
        OR (status = 'ENDED' AND effective_to IS NOT NULL AND end_reason IS NOT NULL AND ended_by IS NOT NULL))
);

-- At most one ACTIVE binding per tenant (section 119: tenant binding explicit).
CREATE UNIQUE INDEX IF NOT EXISTS tenant_platform_account_binding_active_uniq
    ON registry.tenant_platform_account_binding (tenant_id) WHERE status = 'ACTIVE';
CREATE INDEX IF NOT EXISTS tenant_platform_account_binding_account_active_idx
    ON registry.tenant_platform_account_binding (platform_account_id) WHERE status = 'ACTIVE';

-- History is evidence: a binding may only move ACTIVE -> ENDED, setting its
-- end fields; nothing else about it ever changes, and no row is deleted.
CREATE OR REPLACE FUNCTION registry.tenant_platform_account_binding_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'tenant_platform_account_binding rows are never deleted (ADR-BCP-018 section 119)';
    END IF;
    IF OLD.status <> 'ACTIVE' OR NEW.status <> 'ENDED'
        OR NEW.binding_id <> OLD.binding_id OR NEW.tenant_id <> OLD.tenant_id
        OR NEW.platform_account_id <> OLD.platform_account_id OR NEW.organisation_id <> OLD.organisation_id
        OR NEW.reason <> OLD.reason OR NEW.evidence_reference IS DISTINCT FROM OLD.evidence_reference
        OR NEW.bound_by <> OLD.bound_by OR NEW.effective_from <> OLD.effective_from
        OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'a tenant PlatformAccount binding may only be ended (ADR-BCP-018 section 119)';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS tenant_platform_account_binding_guard ON registry.tenant_platform_account_binding;
CREATE TRIGGER tenant_platform_account_binding_guard
    BEFORE UPDATE OR DELETE ON registry.tenant_platform_account_binding
    FOR EACH ROW EXECUTE FUNCTION registry.tenant_platform_account_binding_guard();

-- Section 83: CLOSED is final. The permitted transitions are enforced by
-- the Control Plane; the database refuses the one that would be
-- irreversible to get wrong.
CREATE OR REPLACE FUNCTION registry.platform_account_closed_is_final() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'CLOSED' AND NEW.status <> 'CLOSED' THEN
        RAISE EXCEPTION 'a CLOSED PlatformAccount cannot be reopened (ADR-BCP-018 section 83)';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS platform_account_closed_is_final ON registry.platform_account;
CREATE TRIGGER platform_account_closed_is_final
    BEFORE UPDATE OF status ON registry.platform_account
    FOR EACH ROW EXECUTE FUNCTION registry.platform_account_closed_is_final();
