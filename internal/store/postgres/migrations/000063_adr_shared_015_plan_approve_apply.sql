-- ADR-SHARED-015: plan, approve and apply.
--
-- The canonical state becomes authoritative. The legacy orchestrator still
-- drives `status`; a status change that does not also set the state carries
-- its canonical projection into `state`, so both stay consistent while the
-- Control Plane can set states the legacy status cannot express (BLOCKED).

ALTER TABLE provisioning.tenant_provisioning ALTER COLUMN state DROP EXPRESSION;
UPDATE provisioning.tenant_provisioning SET state = CASE status
    WHEN 'PLAN' THEN 'PLANNED'
    WHEN 'APPLY' THEN 'REGISTERING'
    WHEN 'RECONCILE' THEN 'VERIFYING_READINESS'
    ELSE status
END WHERE state IS NULL;
ALTER TABLE provisioning.tenant_provisioning
    ALTER COLUMN state SET NOT NULL,
    ADD CONSTRAINT tenant_provisioning_state_ck CHECK (state IN (
        'DRAFT', 'VALIDATING', 'PLANNED', 'REGISTERING', 'CONFIGURING_CONTEXT', 'PROVISIONING_ENTITLEMENTS',
        'PROVISIONING_PROVIDERS', 'VALIDATING_SECURITY', 'VERIFYING_READINESS', 'BLOCKED', 'REMEDIATING',
        'READY', 'ACTIVE', 'FAILED', 'CANCELLED', 'DEPROVISIONED'));

CREATE FUNCTION provisioning.tenant_provisioning_state_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.state IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.status IS NOT DISTINCT FROM OLD.status OR NEW.state IS DISTINCT FROM OLD.state) THEN
        RETURN NEW;
    END IF;
    NEW.state := CASE NEW.status
        WHEN 'PLAN' THEN 'PLANNED'
        WHEN 'APPLY' THEN 'REGISTERING'
        WHEN 'RECONCILE' THEN 'VERIFYING_READINESS'
        ELSE NEW.status
    END;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_provisioning_state_sync BEFORE INSERT OR UPDATE ON provisioning.tenant_provisioning
    FOR EACH ROW EXECUTE FUNCTION provisioning.tenant_provisioning_state_sync();

-- A tenant has one live provisioning from desired state. Changing a
-- provisioned tenant is a change of its own, not a second provisioning.
CREATE UNIQUE INDEX tenant_provisioning_live_uq ON provisioning.tenant_provisioning (tenant_id)
    WHERE tenant_onboarding_request_id IS NOT NULL AND state NOT IN ('CANCELLED', 'DEPROVISIONED');

-- One decision per plan: a rejected plan is replanned, never re-decided.
CREATE UNIQUE INDEX plan_approval_plan_uq ON provisioning.plan_approval (plan_id);

-- An executor holds an operation under a lease; an expired lease is
-- reclaimed, so a crashed executor's operation is resumed, not lost.
ALTER TABLE operations.execution_operation
    ADD COLUMN lease_expires_at timestamptz;
CREATE INDEX execution_operation_runnable_idx ON operations.execution_operation (created_at)
    WHERE status IN ('QUEUED', 'PREPARING', 'RUNNING');
