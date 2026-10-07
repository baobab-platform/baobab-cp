-- Context authority purpose (Shared control-plane/v1 1.34.0; docs/architecture/
-- context-authority-for-workloads.md section 13, owner ruling 2026-10-07).
--
-- The canonical lifecycle activates a tenant only after provider provisioning
-- and readiness (Technical Specification section 22; ADR-BCP-017 sections 22
-- and 45), so the ERP provisioning that justifies activation cannot be
-- authorised by a context that requires an ACTIVE tenant. Rather than weaken
-- the ACTIVE rule for every context, every stored context states what it is
-- authority for:
--
--   RUNTIME              the product of POST /v1/platform-context/resolve; the
--                        tenant must be ACTIVE (unchanged).
--   TENANT_PROVISIONING  created only by the Control Plane's own provisioning
--                        execution, never through an HTTP operation; bound to
--                        the approved plan tuple; at most 15 minutes.
--
-- A resolved context is immutable (ADR-BCP-004 section 71): the purpose and the
-- plan tuple are fixed when the row is inserted, and the trigger below refuses
-- to change them afterwards. Whether a provisioning context is still usable is
-- decided at validation time from authoritative state (ADR-BCP-009 section 61),
-- never by rewriting the row.
ALTER TABLE context.resolved_context
    ADD COLUMN authority_purpose text NOT NULL DEFAULT 'RUNTIME'
        CHECK (authority_purpose IN ('RUNTIME', 'TENANT_PROVISIONING')),
    ADD COLUMN provisioning_authority jsonb;

ALTER TABLE context.resolved_context
    ADD CONSTRAINT resolved_context_purpose_authority CHECK (
        (authority_purpose = 'RUNTIME' AND provisioning_authority IS NULL)
        OR (authority_purpose = 'TENANT_PROVISIONING'
            AND provisioning_authority IS NOT NULL
            AND jsonb_typeof(provisioning_authority) = 'object'
            AND provisioning_authority ?& ARRAY['tenant_provisioning_id', 'plan_id', 'plan_version', 'plan_digest']
            AND expires_at IS NOT NULL
            AND expires_at <= resolved_at + interval '15 minutes')
    );

CREATE FUNCTION context.resolved_context_purpose_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.authority_purpose IS DISTINCT FROM OLD.authority_purpose
       OR NEW.provisioning_authority IS DISTINCT FROM OLD.provisioning_authority
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.principal_id IS DISTINCT FROM OLD.principal_id THEN
        RAISE EXCEPTION 'a resolved context is immutable; resolve a new one';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER resolved_context_purpose_immutable
    BEFORE UPDATE ON context.resolved_context
    FOR EACH ROW EXECUTE FUNCTION context.resolved_context_purpose_immutable();
