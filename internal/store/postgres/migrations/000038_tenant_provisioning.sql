-- Gate ZB-02 (Control Plane Completion) / Programme Gate P7 (Tenant
-- Provisioning Engine) "basics": the real TenantProvisioning process
-- aggregate (Technical Specification SS21) that public.provisioning_operations
-- (migration 000019) was never more than an idempotency/replay-detection
-- record for. Gate P0's classification (docs/reconciliation/phase-0-
-- architecture-inventory-and-lock.md) flagged this gap explicitly:
-- "provisioning_operations is an idempotency/replay-detection record, not
-- a provisioning-orchestration aggregate... Programme Gate P7 scope".
--
-- This migration adds a new, separate table rather than remodelling
-- provisioning_operations in place: RegisterTenant (internal/store/postgres/
-- store.go) actively reads/writes provisioning_operations today as a live,
-- narrowly-scoped idempotency guard, and changing its shape is a larger,
-- separate concern from adding the process aggregate itself. A later gate
-- may fold provisioning_operations into this table as one of its inputs,
-- exactly as Gate P0's own note anticipated, once RegisterTenant's flow is
-- rebuilt on top of it.
--
-- Scope note (this migration's own basics cut,
-- internal/provisioning/domain/tenant_provisioning.go): only Gate ZB-02's
-- own simplified five-stage lifecycle (PLAN -> APPLY -> RECONCILE -> READY
-- -> ACTIVE, plus FAILED/CANCELLED terminals) is modelled here, not the
-- Technical Specification SS22's full thirteen-state machine.

CREATE SCHEMA IF NOT EXISTS provisioning;

CREATE TABLE IF NOT EXISTS provisioning.tenant_provisioning (
    tenant_provisioning_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               text NOT NULL,
    idempotency_key         text NOT NULL,
    request_hash            text NOT NULL,
    status                  text NOT NULL DEFAULT 'PLAN',
    desired_state_version   bigint NOT NULL DEFAULT 1,
    observed_state_version  bigint NOT NULL DEFAULT 0,
    product_requests        jsonb NOT NULL DEFAULT '[]',
    market_requests         jsonb NOT NULL DEFAULT '[]',
    isolation_requirement   text,
    residency_requirement   text,
    blocking_reasons        jsonb NOT NULL DEFAULT '[]',
    attempt_count           integer NOT NULL DEFAULT 0,
    last_error              text,
    started_at              timestamptz NOT NULL DEFAULT now(),
    completed_at            timestamptz,
    version                 bigint NOT NULL DEFAULT 1,
    metadata                jsonb NOT NULL DEFAULT '{}',
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, idempotency_key),
    CHECK (status IN ('PLAN', 'APPLY', 'RECONCILE', 'READY', 'ACTIVE', 'FAILED', 'CANCELLED')),
    CHECK (status <> 'ACTIVE' OR completed_at IS NOT NULL),
    CHECK (status <> 'FAILED' OR last_error IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS tenant_provisioning_tenant_idx
    ON provisioning.tenant_provisioning(tenant_id);
