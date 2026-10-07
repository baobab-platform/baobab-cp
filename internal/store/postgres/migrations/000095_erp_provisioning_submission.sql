-- ERP provisioning submissions (ADR-BCP-021 sections 24 and 27; Shared erp/v1
-- 1.2.0 requestErpProvisioning).
--
-- ERP answers a provisioning request with an operation id and its state, but
-- its state carries no provisioning id. This table is the Control Plane's only
-- link from an ERP operation back to the provisioning and the exact approved
-- plan it was requested under, and the record of how far ERP reports it has
-- got. ERP's 202 means "accepted", never "provisioned": readiness consults
-- last_state, so a tenant is not READY while ERP has not reported "active".
--
-- A submission is immutable apart from its progress: the plan tuple, tenant and
-- legal entities are fixed when it is recorded, and progress only moves
-- forward (last_revision), so a late or replayed ERP state can never rewind it.
CREATE TABLE provisioning.erp_submission (
    operation_id           uuid PRIMARY KEY,
    tenant_provisioning_id uuid NOT NULL REFERENCES provisioning.tenant_provisioning (tenant_provisioning_id),
    tenant_id              text NOT NULL,
    plan_id                text NOT NULL,
    plan_version           integer NOT NULL CHECK (plan_version >= 1),
    plan_digest            text NOT NULL,
    legal_entity_ids       text[] NOT NULL CHECK (cardinality(legal_entity_ids) >= 1),
    last_revision          bigint NOT NULL CHECK (last_revision >= 0),
    last_state             text NOT NULL
        CHECK (last_state IN ('accepted', 'validating', 'provisioning', 'reconciling', 'active', 'failed', 'cancelled')),
    submitted_at           timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    -- One operation per approved plan of a provisioning: a replayed request
    -- returns ERP's prior operation, so a second one is never recorded.
    UNIQUE (tenant_provisioning_id, plan_id, plan_version, plan_digest)
);

CREATE INDEX erp_submission_tenant_idx ON provisioning.erp_submission (tenant_id, submitted_at DESC);

CREATE FUNCTION provisioning.erp_submission_fixed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.operation_id <> OLD.operation_id
       OR NEW.tenant_provisioning_id <> OLD.tenant_provisioning_id
       OR NEW.tenant_id <> OLD.tenant_id
       OR NEW.plan_id <> OLD.plan_id
       OR NEW.plan_version <> OLD.plan_version
       OR NEW.plan_digest <> OLD.plan_digest
       OR NEW.legal_entity_ids <> OLD.legal_entity_ids THEN
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

CREATE TRIGGER erp_submission_fixed BEFORE UPDATE ON provisioning.erp_submission
    FOR EACH ROW EXECUTE FUNCTION provisioning.erp_submission_fixed();
