-- ADR-BCP-018 gate ORG-11 — ProductSubscription classification and
-- provenance (ADR-BCP-017 sections 10-13, 48; ADR-SHARED-011).
-- Contract: baobab-platform/shared contracts/product/v1.
--
-- A classification is an immutable record: which subscription type, from
-- which governed source (an AdmissionDecision, a reclassification, a
-- migration or a manual governance decision), by whom and why. An INTERNAL
-- record always carries the eligibility evidence the Control Plane
-- evaluated from verified platform and corporate relationships. The
-- subscription points at its current record; reclassification appends a
-- record and moves the pointer, so tenant, organisation and subscription
-- identity never change. CapabilityGrant semantics are untouched:
-- classification affects charge policy only.

CREATE TABLE IF NOT EXISTS product.subscription_classification (
    classification_id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id             uuid NOT NULL REFERENCES product.product_subscription(subscription_id),
    tenant_id                   text NOT NULL,
    subscription_type           text NOT NULL,
    previous_subscription_type  text,
    classification_source       text NOT NULL,
    classification_reference    text NOT NULL,
    reason                      text NOT NULL,
    classified_at               timestamptz NOT NULL,
    classified_by               uuid NOT NULL REFERENCES identity.principal(principal_id),
    internal_eligibility        jsonb,
    CHECK (subscription_type IN ('COMMERCIAL','INTERNAL','TRIAL','PARTNER','MANUAL','MIGRATION')),
    CHECK (previous_subscription_type IS NULL
        OR previous_subscription_type IN ('COMMERCIAL','INTERNAL','TRIAL','PARTNER','MANUAL','MIGRATION')),
    CHECK (classification_source IN ('ADMISSION_DECISION','RECLASSIFICATION','MIGRATION','MANUAL_GOVERNANCE')),
    CHECK (classification_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$'),
    CHECK (length(reason) BETWEEN 1 AND 2000),
    CONSTRAINT subscription_classification_admission_reference CHECK (
        classification_source <> 'ADMISSION_DECISION' OR classification_reference ~ '^adm_[a-z0-9]+$'),
    CONSTRAINT subscription_classification_reclassification_previous CHECK (
        classification_source <> 'RECLASSIFICATION' OR previous_subscription_type IS NOT NULL),
    -- INTERNAL, and only INTERNAL, carries eligibility evidence (section 13).
    CONSTRAINT subscription_classification_internal_evidence CHECK (
        (subscription_type = 'INTERNAL') = (internal_eligibility IS NOT NULL)),
    -- Lets product_subscription's composite foreign key prove that its
    -- current classification belongs to it and has its type.
    UNIQUE (classification_id, subscription_id, subscription_type)
);

CREATE INDEX IF NOT EXISTS subscription_classification_subscription_idx
    ON product.subscription_classification(subscription_id, classified_at DESC, classification_id DESC);
-- Drift: the INTERNAL records whose recorded basis may have lapsed.
CREATE INDEX IF NOT EXISTS subscription_classification_internal_idx
    ON product.subscription_classification(subscription_id)
    WHERE subscription_type = 'INTERNAL';

-- Classification records are evidence: immutable once written.
CREATE OR REPLACE FUNCTION product.subscription_classification_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'subscription_classification is immutable (ADR-SHARED-011): % refused', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

DROP TRIGGER IF EXISTS subscription_classification_no_update_or_delete ON product.subscription_classification;
CREATE TRIGGER subscription_classification_no_update_or_delete
    BEFORE UPDATE OR DELETE ON product.subscription_classification
    FOR EACH ROW EXECUTE FUNCTION product.subscription_classification_immutable();

DROP TRIGGER IF EXISTS subscription_classification_no_truncate ON product.subscription_classification;
CREATE TRIGGER subscription_classification_no_truncate
    BEFORE TRUNCATE ON product.subscription_classification
    FOR EACH STATEMENT EXECUTE FUNCTION product.subscription_classification_immutable();

-- The subscription's current classification: its type, durably, and the
-- record that explains it. Both are NULL until the subscription is first
-- classified (existing subscriptions predate ORG-11).
ALTER TABLE product.product_subscription
    ADD COLUMN IF NOT EXISTS subscription_type text,
    ADD COLUMN IF NOT EXISTS classification_id uuid;

ALTER TABLE product.product_subscription
    DROP CONSTRAINT IF EXISTS product_subscription_classified_together,
    ADD CONSTRAINT product_subscription_classified_together CHECK (
        (subscription_type IS NULL) = (classification_id IS NULL)),
    DROP CONSTRAINT IF EXISTS product_subscription_current_classification_fk,
    ADD CONSTRAINT product_subscription_current_classification_fk
        FOREIGN KEY (classification_id, subscription_id, subscription_type)
        REFERENCES product.subscription_classification(classification_id, subscription_id, subscription_type);
