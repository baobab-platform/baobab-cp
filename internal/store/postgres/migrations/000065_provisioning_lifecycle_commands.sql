-- ADR-SHARED-015 lifecycle commands: replan, withdraw, remediate, and
-- retry/cancel of operations.

-- Why a provisioning is BLOCKED when neither its plan's blockers nor the
-- pipeline's readiness findings say: EXECUTION_CANCELLED after a cancelled
-- apply or remediation. Cleared by a replan or a new execution.
ALTER TABLE provisioning.tenant_provisioning
    ADD COLUMN blocked_reason text CHECK (blocked_reason IS NULL OR blocked_reason ~ '^[A-Z][A-Z0-9_]*$');

-- A replan is idempotent per key: a replay returns the plan it created.
ALTER TABLE provisioning.plan
    ADD COLUMN idempotency_key text,
    ADD COLUMN request_hash text,
    ADD CONSTRAINT plan_idempotency_ck CHECK ((idempotency_key IS NULL) = (request_hash IS NULL));
CREATE UNIQUE INDEX plan_idempotency_uq ON provisioning.plan (tenant_provisioning_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- A retry is idempotent per key: a replay returns the operation it queued.
ALTER TABLE operations.execution_operation
    ADD COLUMN retry_idempotency_key text;
