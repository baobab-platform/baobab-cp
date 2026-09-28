-- ADR-SHARED-016: provider migration execution. One approval binds the
-- migration's current plan; each lifecycle transition runs as a durable
-- PROVIDER_MIGRATION_ADVANCE operation; the ledger records every binding the
-- migration touched, so execution, rollback and staleness act on exactly
-- the bindings the approved plan named.

ALTER TABLE topology.provider_migration
    ADD COLUMN approval_id         text,
    ADD COLUMN operation_id        text,
    ADD COLUMN shifted_cohort_keys text[] NOT NULL DEFAULT '{}';

-- A decision on one exact plan version, bound to its digest. The approver
-- is never the migration's creator.
CREATE TABLE topology.provider_migration_approval (
    approval_id           text PRIMARY KEY CHECK (approval_id ~ '^apd_[a-z0-9]+$'),
    provider_migration_id text NOT NULL REFERENCES topology.provider_migration (provider_migration_id),
    plan_id               text NOT NULL REFERENCES topology.provider_migration_plan (plan_id),
    plan_version          integer NOT NULL CHECK (plan_version >= 1),
    plan_digest           text NOT NULL CHECK (plan_digest ~ '^sha256:[0-9a-f]{64}$'),
    decision              text NOT NULL CHECK (decision IN ('APPROVED', 'REJECTED', 'CHANGES_REQUESTED')),
    reason                text CHECK (reason IS NULL OR length(reason) BETWEEN 1 AND 1000),
    decided_by            text NOT NULL,
    decided_at            timestamptz NOT NULL,
    correlation_id        uuid,
    CHECK (decision = 'APPROVED' OR reason IS NOT NULL),
    -- One decision per plan version: a new decision needs a new plan.
    UNIQUE (plan_id)
);

ALTER TABLE topology.provider_migration
    ADD CONSTRAINT provider_migration_approval_fkey
    FOREIGN KEY (approval_id) REFERENCES topology.provider_migration_approval (approval_id);

-- Every source binding the migration moves, the MIGRATION binding created
-- for it on the target, and where it is: PREPARED (target bound in
-- MIGRATION mode), SHIFTED (authority on the target), RETIRED (source
-- retired) or REMOVED (target binding removed by cancel or rollback).
CREATE TABLE topology.provider_migration_binding (
    provider_migration_id     text NOT NULL REFERENCES topology.provider_migration (provider_migration_id),
    source_binding_id         uuid NOT NULL REFERENCES capability.capability_binding (id),
    source_mode               text NOT NULL CHECK (source_mode IN ('PRIMARY', 'FALLBACK')),
    capability_key            text NOT NULL,
    cohort_key                text NOT NULL,
    target_binding_id         uuid NOT NULL UNIQUE REFERENCES capability.capability_binding (id),
    target_engine_instance_id uuid NOT NULL REFERENCES topology.engine_instance (engine_instance_id),
    state                     text NOT NULL CHECK (state IN ('PREPARED', 'SHIFTED', 'RETIRED', 'REMOVED')),
    updated_at                timestamptz NOT NULL,
    PRIMARY KEY (provider_migration_id, source_binding_id)
);

CREATE INDEX provider_migration_binding_cohort_idx
    ON topology.provider_migration_binding (provider_migration_id, cohort_key);

ALTER TABLE operations.execution_operation DROP CONSTRAINT IF EXISTS execution_operation_operation_type_check;
ALTER TABLE operations.execution_operation ADD CONSTRAINT execution_operation_operation_type_check
    CHECK (operation_type IN ('TENANT_PROVISIONING_APPLY', 'TENANT_PROVISIONING_REMEDIATE', 'CHANGESET_APPLY',
        'PROVIDER_MIGRATION_ADVANCE'));
ALTER TABLE operations.execution_operation DROP CONSTRAINT IF EXISTS execution_operation_subject_type_check;
ALTER TABLE operations.execution_operation ADD CONSTRAINT execution_operation_subject_type_check
    CHECK (subject_type IN ('TENANT_PROVISIONING', 'CHANGESET', 'PROVIDER_MIGRATION'));

-- ADR-SHARED-016 section 4: the workload an engine instance is attested to
-- run as, recorded by its controlled registration (ADR-BCP-006 sections
-- 94-95), never by the engine. An instance without one has no claimant, so
-- its migration tasks time out safely.
ALTER TABLE topology.engine_instance
    ADD COLUMN workload_client_id text CHECK (workload_client_id IS NULL OR length(workload_client_id) BETWEEN 1 AND 128);
CREATE INDEX engine_instance_workload_idx ON topology.engine_instance (workload_client_id) WHERE workload_client_id IS NOT NULL;

-- The steps a running advance still has to run: an advance that reaches an
-- engine step waits for its engine migration tasks, and each report resumes
-- it from next_step.
CREATE TABLE topology.provider_migration_run (
    operation_id          text PRIMARY KEY REFERENCES operations.execution_operation (operation_id) DEFERRABLE INITIALLY DEFERRED,
    provider_migration_id text NOT NULL REFERENCES topology.provider_migration (provider_migration_id),
    transition            text NOT NULL,
    cohort_key            text,
    steps                 jsonb NOT NULL,
    next_step             integer NOT NULL CHECK (next_step >= 0),
    -- The stage the migration takes when the run succeeds: a forward
    -- transition moves the stage when accepted, cancel and roll_back only
    -- when they finish.
    target_stage          text NOT NULL,
    updated_at            timestamptz NOT NULL
);

-- Engine migration tasks (Shared engine-migration-task.schema.json): the
-- stateful steps an assigned engine instance claims and reports under its
-- workload identity. Never business data, credentials or endpoints.
CREATE TABLE topology.engine_migration_task (
    task_id               text PRIMARY KEY CHECK (task_id ~ '^emt_[a-z0-9]+$'),
    provider_migration_id text NOT NULL REFERENCES topology.provider_migration (provider_migration_id),
    operation_id          text NOT NULL REFERENCES operations.execution_operation (operation_id) DEFERRABLE INITIALLY DEFERRED,
    step_id               text NOT NULL,
    operation             text NOT NULL CHECK (operation IN ('FREEZE_COHORT_WRITES', 'MIGRATE_COHORT_DATA', 'RECONCILE_COHORT_DATA',
                              'UNFREEZE_COHORT_WRITES')),
    role                  text NOT NULL CHECK (role IN ('SOURCE', 'TARGET')),
    direction             text NOT NULL CHECK (direction IN ('FORWARD', 'REVERSE')),
    engine_instance_id    uuid NOT NULL REFERENCES topology.engine_instance (engine_instance_id),
    provider_key          text NOT NULL,
    cohort_key            text NOT NULL,
    capabilities          text[] NOT NULL CHECK (cardinality(capabilities) > 0),
    contexts              jsonb NOT NULL,
    counterpart           jsonb,
    status                text NOT NULL CHECK (status IN ('PENDING', 'CLAIMED', 'SUCCEEDED', 'FAILED', 'CANCELLED')),
    attempt               integer NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    claimed_by            text,
    lease_expires_at      timestamptz,
    deadline_at           timestamptz NOT NULL,
    result                jsonb,
    report_hash           text,
    reported_at           timestamptz,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    revision              bigint NOT NULL DEFAULT 1 CHECK (revision >= 1),
    CHECK (status <> 'CLAIMED' OR (claimed_by IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CHECK (status NOT IN ('SUCCEEDED', 'FAILED') OR (result IS NOT NULL AND reported_at IS NOT NULL)),
    CHECK (operation <> 'MIGRATE_COHORT_DATA' OR counterpart IS NOT NULL),
    UNIQUE (operation_id, step_id, engine_instance_id, role)
);
CREATE INDEX engine_migration_task_open_idx ON topology.engine_migration_task (engine_instance_id, task_id)
    WHERE status IN ('PENDING', 'CLAIMED');
CREATE INDEX engine_migration_task_step_idx ON topology.engine_migration_task (operation_id, step_id);
