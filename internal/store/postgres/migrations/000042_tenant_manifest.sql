-- Target path: internal/store/postgres/migrations/000042_tenant_manifest.sql
--
-- Gate ZB-03.1: persist the desired-state manifest a TenantProvisioning run
-- was actually built from, so restart recovery does not depend on a
-- process's in-memory ResolvedManifest closure. Closes the deferred item
-- docs/reconciliation/gate-zb02-completion-report.md §2 flagged as
-- NOT IMPLEMENTED ("Manifest rehydration across process restarts") and
-- internal/provisioning/composition_root.go's own doc comment names as a
-- deliberate simplification: "persisting/rehydrating the resolved manifest
-- across process restarts is a follow-up, not this pass's job."
--
-- One row per TenantProvisioning run (1:1, not versioned across retries --
-- a caller that wants different desired state creates a new
-- TenantProvisioning with a new idempotency key, per the existing Plan()
-- semantics; this table only needs to answer "what manifest is THIS
-- provisioning run's desired state," not track manifest history).
--
-- raw_manifest is the TenantManifest exactly as submitted (symbolic
-- references: market codes, capability keys). resolved_manifest is the
-- ResolvedManifest after ResolveManifest verified every symbolic reference
-- against authoritative registries (canonical market/capability IDs) --
-- this is what BuildZB02Pipeline actually needs to rebuild an Orchestrator
-- after a restart, without re-running resolution against registries that
-- may have changed since (a manifest already resolved and applied must not
-- silently re-resolve to different canonical IDs on retry).
--
-- No credentials or secrets are ever present in a TenantManifest (see
-- internal/provisioning/manifest.go's field list) -- both JSON columns are
-- safe to store and read back without additional redaction.
--
-- ON DELETE CASCADE: a manifest snapshot has no lifecycle independent of
-- the TenantProvisioning run it describes -- it is not itself the
-- durable evidence record (that is ReadinessSnapshot/ReconciliationSnapshot,
-- a later ZB-03.1 slice), only the input the run was built from.
CREATE TABLE IF NOT EXISTS provisioning.tenant_manifest (
    tenant_manifest_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_provisioning_id  uuid NOT NULL UNIQUE
        REFERENCES provisioning.tenant_provisioning(tenant_provisioning_id) ON DELETE CASCADE,
    tenant_id               text NOT NULL,
    schema_version          text NOT NULL,
    manifest_hash           text NOT NULL,
    desired_state_version   bigint NOT NULL,
    source                  text NOT NULL,
    raw_manifest            jsonb NOT NULL,
    resolved_manifest       jsonb NOT NULL,
    created_at              timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS tenant_manifest_tenant_idx
    ON provisioning.tenant_manifest(tenant_id);
