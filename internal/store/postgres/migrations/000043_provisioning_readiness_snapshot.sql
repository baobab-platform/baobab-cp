-- Target path:
-- internal/store/postgres/migrations/000043_provisioning_readiness_snapshot.sql
--
-- Gate ZB-03.1: persist readiness evaluation evidence, closing the deferred
-- ZB-02 item docs/reconciliation/gate-zb02-completion-report.md flagged as
-- NOT IMPLEMENTED ("Persisted readiness snapshots") --
-- internal/provisioning/readiness.go's ReadinessReport was computed on
-- demand only; there was no durable, queryable record of why a tenant was
-- or was not READY at a given point in time.
--
-- Snapshots are immutable evidence, not the mutable current-state row
-- provisioning.tenant_provisioning already is: a later evaluation inserts a
-- new snapshot rather than updating an old one, so an operator can see the
-- readiness history of a run, not just its latest state. No UPDATE path is
-- provided for either table by design.
CREATE TABLE IF NOT EXISTS provisioning.readiness_snapshot (
    readiness_snapshot_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_provisioning_id  uuid NOT NULL
        REFERENCES provisioning.tenant_provisioning(tenant_provisioning_id) ON DELETE CASCADE,
    tenant_id               text NOT NULL,
    desired_state_version   bigint NOT NULL,
    observed_state_version  bigint NOT NULL,
    overall_ready           boolean NOT NULL,
    blocking_reasons        jsonb NOT NULL DEFAULT '[]',
    evaluated_at            timestamptz NOT NULL,
    created_at              timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS readiness_snapshot_provisioning_idx
    ON provisioning.readiness_snapshot(tenant_provisioning_id, evaluated_at DESC);
CREATE INDEX IF NOT EXISTS readiness_snapshot_tenant_idx
    ON provisioning.readiness_snapshot(tenant_id);

CREATE TABLE IF NOT EXISTS provisioning.readiness_check (
    readiness_check_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    readiness_snapshot_id   uuid NOT NULL
        REFERENCES provisioning.readiness_snapshot(readiness_snapshot_id) ON DELETE CASCADE,
    check_key               text NOT NULL,
    resource_type           text NOT NULL,
    status                  text NOT NULL,
    reason                  text,
    evidence_reference      text,
    evaluated_at            timestamptz NOT NULL,
    CHECK (status IN ('PASS', 'FAIL'))
);

CREATE INDEX IF NOT EXISTS readiness_check_snapshot_idx
    ON provisioning.readiness_check(readiness_snapshot_id);
