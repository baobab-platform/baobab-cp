-- ADR-BCP-018 gate ORG-11 — billing projection sync state (ADR-SHARED-011;
-- ADR-SUB-0003 section 16). Contract: baobab-platform/shared
-- contracts/subscriptions/v1.
--
-- The Control Plane owns the ProductSubscription and its classification;
-- baobab-subscriptions owns the billing projection. This table records only
-- what the Control Plane last asked the engine to converge to (the
-- subscription's authoritative revision and status) and the bounded state
-- the engine reported back. It never holds billing amounts, provider data or
-- payment data, and the engine never reads or writes it.

CREATE TABLE IF NOT EXISTS product.billing_projection_sync (
    subscription_id          uuid PRIMARY KEY REFERENCES product.product_subscription(subscription_id),
    tenant_id                text NOT NULL,
    synced_revision          bigint NOT NULL DEFAULT 0,
    synced_status            text,
    billing_subscription_id  text,
    billing_state            text,
    readiness_status         text,
    last_error_code          text,
    attempts                 integer NOT NULL DEFAULT 0,
    -- NULL: due now. Set only after a failed attempt, to back off.
    next_attempt_at          timestamptz,
    updated_at               timestamptz NOT NULL DEFAULT now(),
    CHECK (synced_revision >= 0),
    CHECK (attempts >= 0),
    CHECK (billing_subscription_id IS NULL OR billing_subscription_id ~ '^bsub_[a-z0-9]{1,58}$'),
    CHECK (billing_state IS NULL OR billing_state IN
        ('PENDING_CONFIGURATION','PROVISIONING','ACTIVE','SUSPENDED','TERMINATING','TERMINATED')),
    CHECK (readiness_status IS NULL OR readiness_status IN ('READY','BLOCKED')),
    CHECK (last_error_code IS NULL OR last_error_code ~ '^[A-Z][A-Z0-9_]{1,63}$')
);

CREATE INDEX IF NOT EXISTS billing_projection_sync_due
    ON product.billing_projection_sync (next_attempt_at);
