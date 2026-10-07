-- ERP provisioning submissions record the Finance baseline references they relied on (Shared erp/v1 1.3.0, FB-03).
--
-- A provisioning can cover several legal entities, so this is the complete set of FinanceBaselineReference values exactly as
-- the request carried them (baseline id, legal entity, version, digest, effective_from and the fixed ERP authority), never
-- one scalar. With the immutable plan tuple already on the row it answers "which Finance baseline caused this ERP
-- provisioning?". The references carry no accounting value: the Control Plane refers to a baseline and ERP owns it.
--
-- Like the plan tuple, the set is fixed when the submission is recorded; only ERP's progress advances.
ALTER TABLE provisioning.erp_submission
    ADD COLUMN finance_baselines jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(finance_baselines) = 'array');

CREATE OR REPLACE FUNCTION provisioning.erp_submission_fixed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.operation_id <> OLD.operation_id
       OR NEW.tenant_provisioning_id <> OLD.tenant_provisioning_id
       OR NEW.tenant_id <> OLD.tenant_id
       OR NEW.plan_id <> OLD.plan_id
       OR NEW.plan_version <> OLD.plan_version
       OR NEW.plan_digest <> OLD.plan_digest
       OR NEW.legal_entity_ids <> OLD.legal_entity_ids
       OR NEW.finance_baselines <> OLD.finance_baselines THEN
        RAISE EXCEPTION 'an ERP submission is fixed once recorded; only its progress advances';
    END IF;
    IF NEW.last_revision < OLD.last_revision THEN
        RAISE EXCEPTION 'ERP progress never moves backwards';
    END IF;
    IF NEW.last_revision = OLD.last_revision AND NEW.last_state <> OLD.last_state THEN
        RAISE EXCEPTION 'an ERP state changes only at a newer revision';
    END IF;
    RETURN NEW;
END $$;
