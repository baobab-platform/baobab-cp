-- ADR-BCP-021 gates CCM-02 and CCM-03: the generic changeset, its immutable
-- plans and approval decisions, and its outcome record (Shared
-- control-plane/v1/changeset.schema.json). Plans, decisions and outcomes
-- are never updated in place (section 209); a changeset is mutable only in
-- DRAFT (section 210), and its state follows changeset-lifecycle.yaml.

CREATE SCHEMA IF NOT EXISTS changeset;

CREATE TABLE changeset.changeset (
    changeset_id           text PRIMARY KEY CHECK (changeset_id ~ '^cs_[a-z0-9]+$'),
    changeset_type         text NOT NULL CHECK (changeset_type IN ('ONBOARD', 'MODIFY', 'EXPAND', 'REDUCE', 'SUSPEND',
                               'REINSTATE', 'MIGRATE', 'DECOMMISSION', 'SECURITY_REMEDIATION', 'EMERGENCY', 'RECONCILIATION_REPAIR')),
    title                  text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description            text,
    reason                 text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1000),
    business_justification text,
    source                 text NOT NULL CHECK (source IN ('HUMAN_CONSOLE', 'API', 'AUTOMATION', 'MIGRATION', 'RECONCILIATION', 'SECURITY_RESPONSE')),
    requested_by           text NOT NULL,
    requested_at           timestamptz NOT NULL,
    target_scope           jsonb NOT NULL,
    -- The tenant the desired change names, for listing and for the
    -- semantic lock on overlapping changes (section 66).
    target_tenant_id       text NOT NULL,
    base_revision          bigint NOT NULL CHECK (base_revision >= 1),
    desired_change         jsonb NOT NULL,
    risk_class             text CHECK (risk_class IS NULL OR risk_class IN ('LOW', 'MEDIUM', 'HIGH', 'CRITICAL')),
    state                  text NOT NULL CHECK (state IN ('DRAFT', 'VALIDATING', 'INVALID', 'PLANNING', 'BLOCKED', 'PLANNED',
                               'AWAITING_APPROVAL', 'REJECTED', 'CHANGES_REQUESTED', 'APPROVED', 'SCHEDULED', 'APPLYING', 'FAILED',
                               'PARTIALLY_APPLIED', 'VERIFYING', 'VERIFICATION_FAILED', 'COMPLETED', 'CANCELLED', 'SUPERSEDED',
                               'EXPIRED', 'COMPENSATED')),
    current_plan_id        text,
    approval_id            text,
    operation_id           text,
    blocking_reasons       jsonb,
    idempotency_key        text NOT NULL,
    request_hash           text NOT NULL,
    correlation_id         uuid,
    created_at             timestamptz NOT NULL,
    updated_at             timestamptz NOT NULL,
    revision               bigint NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (requested_by, idempotency_key),
    CHECK ((state IN ('INVALID', 'BLOCKED')) = (blocking_reasons IS NOT NULL)),
    CHECK (state NOT IN ('APPROVED', 'SCHEDULED', 'APPLYING', 'VERIFYING', 'COMPLETED', 'FAILED', 'PARTIALLY_APPLIED',
        'VERIFICATION_FAILED') OR approval_id IS NOT NULL),
    CHECK (state NOT IN ('APPLYING', 'VERIFYING', 'COMPLETED', 'FAILED', 'PARTIALLY_APPLIED', 'VERIFICATION_FAILED')
        OR operation_id IS NOT NULL)
);

CREATE INDEX changeset_created_idx ON changeset.changeset (created_at DESC, changeset_id DESC);
CREATE INDEX changeset_target_open_idx ON changeset.changeset (target_tenant_id)
    WHERE state NOT IN ('DRAFT', 'INVALID', 'REJECTED', 'COMPLETED', 'CANCELLED', 'SUPERSEDED', 'EXPIRED', 'COMPENSATED');

CREATE TABLE changeset.plan (
    plan_id       text PRIMARY KEY CHECK (plan_id ~ '^plan_[a-z0-9]+$'),
    changeset_id  text NOT NULL REFERENCES changeset.changeset (changeset_id),
    plan_version  integer NOT NULL CHECK (plan_version >= 1),
    plan_digest   text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    base_revision bigint NOT NULL CHECK (base_revision >= 1),
    document      jsonb NOT NULL,
    generated_at  timestamptz NOT NULL,
    expires_at    timestamptz NOT NULL,
    superseded_at timestamptz,
    UNIQUE (changeset_id, plan_version)
);

ALTER TABLE changeset.changeset
    ADD CONSTRAINT changeset_current_plan_fkey FOREIGN KEY (current_plan_id) REFERENCES changeset.plan (plan_id);

CREATE TABLE changeset.approval (
    approval_id    text PRIMARY KEY CHECK (approval_id ~ '^apd_[a-z0-9]+$'),
    changeset_id   text NOT NULL REFERENCES changeset.changeset (changeset_id),
    plan_id        text NOT NULL REFERENCES changeset.plan (plan_id),
    plan_version   integer NOT NULL,
    plan_digest    text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    decision       text NOT NULL CHECK (decision IN ('APPROVED', 'REJECTED', 'CHANGES_REQUESTED')),
    reason         text CHECK (reason IS NULL OR length(reason) BETWEEN 1 AND 1000),
    decided_by     text NOT NULL,
    decided_at     timestamptz NOT NULL,
    correlation_id uuid,
    CHECK (decision = 'APPROVED' OR reason IS NOT NULL),
    -- One decision per plan version: a new decision needs a new plan.
    UNIQUE (plan_id)
);

CREATE TABLE changeset.outcome (
    changeset_id text PRIMARY KEY REFERENCES changeset.changeset (changeset_id),
    document     jsonb NOT NULL,
    recorded_at  timestamptz NOT NULL
);

ALTER TABLE operations.execution_operation DROP CONSTRAINT IF EXISTS execution_operation_operation_type_check;
ALTER TABLE operations.execution_operation ADD CONSTRAINT execution_operation_operation_type_check
    CHECK (operation_type IN ('TENANT_PROVISIONING_APPLY', 'TENANT_PROVISIONING_REMEDIATE', 'CHANGESET_APPLY'));
