-- ADR-SHARED-015: tenant provisioning as desired-state convergence.
--
-- Additive. The frozen desired state, immutable plans, digest-bound plan
-- approvals and durable execution operations get their own tables. The
-- provisioning aggregate gains its public tp_ key, its provenance and its
-- canonical lifecycle state. Until the orchestrator is migrated, `state` is
-- derived from the legacy `status` it still drives; it becomes authoritative
-- when apply runs as an operation.

ALTER TABLE provisioning.tenant_provisioning
    ADD COLUMN tenant_provisioning_key text
        GENERATED ALWAYS AS ('tp_' || replace(tenant_provisioning_id::text, '-', '')) STORED,
    ADD COLUMN state text
        GENERATED ALWAYS AS (CASE status
            WHEN 'PLAN' THEN 'PLANNED'
            WHEN 'APPLY' THEN 'REGISTERING'
            WHEN 'RECONCILE' THEN 'VERIFYING_READINESS'
            WHEN 'READY' THEN 'READY'
            WHEN 'ACTIVE' THEN 'ACTIVE'
            WHEN 'FAILED' THEN 'FAILED'
            WHEN 'CANCELLED' THEN 'CANCELLED'
        END) STORED,
    ADD COLUMN tenant_onboarding_request_id text
        CHECK (tenant_onboarding_request_id IS NULL OR tenant_onboarding_request_id ~ '^tor_[a-z0-9]+$'),
    ADD COLUMN admission_decision_id text
        CHECK (admission_decision_id IS NULL OR admission_decision_id ~ '^adm_[a-z0-9]+$'),
    ADD COLUMN created_by text,
    ADD COLUMN readiness_status text NOT NULL DEFAULT 'UNKNOWN'
        CHECK (readiness_status IN ('UNKNOWN', 'NOT_READY', 'BLOCKED', 'DEGRADED', 'READY')),
    ADD CONSTRAINT tenant_provisioning_provenance_ck
        CHECK ((tenant_onboarding_request_id IS NULL) = (admission_decision_id IS NULL));
CREATE UNIQUE INDEX tenant_provisioning_key_uq ON provisioning.tenant_provisioning (tenant_provisioning_key);

-- The business intent, frozen from an AUTHORISED TenantOnboardingRequest.
-- It never names engines, providers, grants or bindings; a plan is
-- regenerated against it without changing it.
CREATE TABLE provisioning.desired_state (
    tenant_provisioning_id uuid NOT NULL REFERENCES provisioning.tenant_provisioning (tenant_provisioning_id),
    desired_state_version  bigint NOT NULL CHECK (desired_state_version >= 1),
    document               jsonb NOT NULL,
    desired_state_digest   text NOT NULL CHECK (desired_state_digest ~ '^sha256:[0-9a-f]{64}$'),
    frozen_at              timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_provisioning_id, desired_state_version)
);

-- Immutable plans. A replan is a new plan_version; the previous plan is
-- superseded, never edited.
CREATE TABLE provisioning.plan (
    plan_id                text PRIMARY KEY CHECK (plan_id ~ '^plan_[a-z0-9]+$'),
    tenant_provisioning_id uuid NOT NULL REFERENCES provisioning.tenant_provisioning (tenant_provisioning_id),
    plan_version           integer NOT NULL CHECK (plan_version >= 1),
    plan_digest            text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    base_revision          bigint NOT NULL CHECK (base_revision >= 1),
    desired_state_version  bigint NOT NULL,
    document               jsonb NOT NULL,
    generated_at           timestamptz NOT NULL DEFAULT now(),
    expires_at             timestamptz,
    superseded_at          timestamptz,
    UNIQUE (tenant_provisioning_id, plan_version),
    FOREIGN KEY (tenant_provisioning_id, desired_state_version)
        REFERENCES provisioning.desired_state (tenant_provisioning_id, desired_state_version)
);
CREATE UNIQUE INDEX plan_current_uq ON provisioning.plan (tenant_provisioning_id) WHERE superseded_at IS NULL;
-- Only superseding is an update, and it is final.
CREATE FUNCTION provisioning.plan_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.superseded_at IS NOT NULL
        OR (NEW.plan_id, NEW.tenant_provisioning_id, NEW.plan_version, NEW.plan_digest, NEW.base_revision,
            NEW.desired_state_version, NEW.document, NEW.generated_at, NEW.expires_at)
        IS DISTINCT FROM (OLD.plan_id, OLD.tenant_provisioning_id, OLD.plan_version, OLD.plan_digest, OLD.base_revision,
            OLD.desired_state_version, OLD.document, OLD.generated_at, OLD.expires_at) THEN
        RAISE EXCEPTION 'a provisioning plan is immutable; replan to a new plan_version (ADR-SHARED-015)'
            USING ERRCODE = 'read_only_sql_transaction';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER plan_immutable BEFORE UPDATE ON provisioning.plan
    FOR EACH ROW EXECUTE FUNCTION provisioning.plan_immutable();

-- A decision on one exact plan, bound to its digest. The approver is never
-- the provisioning's requester.
CREATE TABLE provisioning.plan_approval (
    approval_id            text PRIMARY KEY CHECK (approval_id ~ '^apd_[a-z0-9]+$'),
    tenant_provisioning_id uuid NOT NULL REFERENCES provisioning.tenant_provisioning (tenant_provisioning_id),
    plan_id                text NOT NULL REFERENCES provisioning.plan (plan_id),
    plan_version           integer NOT NULL,
    plan_digest            text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    decision               text NOT NULL CHECK (decision IN ('APPROVED', 'REJECTED', 'CHANGES_REQUESTED')),
    reason                 text CHECK (reason IS NULL OR length(reason) BETWEEN 1 AND 1000),
    decided_by             text NOT NULL,
    decided_at             timestamptz NOT NULL DEFAULT now(),
    correlation_id         uuid,
    CHECK (decision = 'APPROVED' OR reason IS NOT NULL)
);
CREATE INDEX plan_approval_plan_idx ON provisioning.plan_approval (plan_id, decided_at DESC);

-- Durable long-running operations (ADR-BCP-022 sections 54-67). Status is the
-- operation's, never its subject's.
CREATE SCHEMA IF NOT EXISTS operations;
CREATE TABLE operations.execution_operation (
    operation_id      text PRIMARY KEY CHECK (operation_id ~ '^op_[a-z0-9]+$'),
    operation_type    text NOT NULL CHECK (operation_type IN ('TENANT_PROVISIONING_APPLY', 'TENANT_PROVISIONING_REMEDIATE')),
    status            text NOT NULL CHECK (status IN ('QUEUED', 'PREPARING', 'RUNNING', 'WAITING', 'VERIFYING', 'BLOCKED',
        'SUCCEEDED', 'FAILED', 'PARTIALLY_APPLIED', 'COMPENSATING', 'COMPENSATED', 'COMPENSATION_FAILED',
        'CANCEL_REQUESTED', 'CANCELLED')),
    subject_type      text NOT NULL CHECK (subject_type IN ('TENANT_PROVISIONING', 'CHANGESET')),
    subject_id        text NOT NULL,
    tenant_id         text,
    plan_id           text,
    plan_digest       text CHECK (plan_digest IS NULL OR plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    approval_id       text,
    requested_by      text NOT NULL,
    idempotency_key   text,
    request_hash      text,
    current_phase     text,
    completed_steps   integer CHECK (completed_steps IS NULL OR completed_steps >= 0),
    total_steps       integer CHECK (total_steps IS NULL OR total_steps >= 0),
    current_step      text,
    execution_attempt integer NOT NULL DEFAULT 1 CHECK (execution_attempt >= 1),
    retryable         boolean NOT NULL DEFAULT false,
    result            jsonb,
    problem           jsonb,
    revision          bigint NOT NULL DEFAULT 1 CHECK (revision >= 1),
    correlation_id    uuid,
    created_at        timestamptz NOT NULL DEFAULT now(),
    started_at        timestamptz,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    CHECK (status <> 'FAILED' OR problem IS NOT NULL),
    -- Success names the resulting resource and its state, which is not the
    -- operation's.
    CHECK (status <> 'SUCCEEDED' OR (completed_at IS NOT NULL AND result ?& ARRAY['resource_type', 'resource_id', 'resource_state'])),
    CHECK ((idempotency_key IS NULL) = (request_hash IS NULL))
);
CREATE UNIQUE INDEX execution_operation_idempotency_uq
    ON operations.execution_operation (requested_by, operation_type, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
CREATE INDEX execution_operation_subject_idx ON operations.execution_operation (subject_type, subject_id, created_at DESC);
