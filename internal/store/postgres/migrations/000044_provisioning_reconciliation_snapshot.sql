-- Target path:
-- internal/store/postgres/migrations/000044_provisioning_reconciliation_snapshot.sql
--
-- Gate ZB-03.1: persist reconciliation/drift evaluation evidence, closing
-- the deferred ZB-02 item docs/reconciliation/gate-zb02-completion-report.md
-- flagged as NOT IMPLEMENTED ("Persisted reconciliation/drift snapshots") --
-- internal/provisioning/reconciliation.go's ReconciliationReport was
-- computed on demand only.
--
-- Same immutable-evidence design as 000043's readiness_snapshot: no UPDATE
-- path, a later evaluation inserts a new snapshot rather than overwriting
-- an old one. resolved_at exists for forward compatibility but is never
-- set by this pass -- see ResourceDriftRecord's doc comment
-- (internal/provisioning/domain/reconciliation_snapshot_record.go) for why
-- drift-ticket lifecycle tracking is explicitly out of this pass's scope.
CREATE TABLE IF NOT EXISTS provisioning.reconciliation_snapshot (
    reconciliation_snapshot_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_provisioning_id      uuid NOT NULL
        REFERENCES provisioning.tenant_provisioning(tenant_provisioning_id) ON DELETE CASCADE,
    tenant_id                   text NOT NULL,
    desired_state_version       bigint NOT NULL,
    observed_state_version      bigint NOT NULL,
    converged                   boolean NOT NULL,
    evaluated_at                timestamptz NOT NULL,
    created_at                  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS reconciliation_snapshot_provisioning_idx
    ON provisioning.reconciliation_snapshot(tenant_provisioning_id, evaluated_at DESC);
CREATE INDEX IF NOT EXISTS reconciliation_snapshot_tenant_idx
    ON provisioning.reconciliation_snapshot(tenant_id);

CREATE TABLE IF NOT EXISTS provisioning.resource_drift (
    resource_drift_id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reconciliation_snapshot_id   uuid NOT NULL
        REFERENCES provisioning.reconciliation_snapshot(reconciliation_snapshot_id) ON DELETE CASCADE,
    resource_type                text NOT NULL,
    resource_id                  text NOT NULL,
    drift_kind                   text NOT NULL,
    desired_hash                 text,
    observed_hash                text,
    repairable                   boolean NOT NULL,
    blocking                     boolean NOT NULL,
    reason                       text NOT NULL,
    detected_at                  timestamptz NOT NULL,
    resolved_at                  timestamptz,
    CHECK (drift_kind IN ('MISSING', 'MISMATCH', 'UNEXPECTED'))
);

CREATE INDEX IF NOT EXISTS resource_drift_snapshot_idx
    ON provisioning.resource_drift(reconciliation_snapshot_id);
