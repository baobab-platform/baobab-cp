-- The Finance baseline references an ERP provisioning request will carry, recorded BEFORE the request is sent (FB-03
-- follow-up).
--
-- ERP's idempotency key includes the reference set, and Finance can supersede a baseline at any time. Without a durable
-- record made before the POST, a crash between ERP accepting the request and the Control Plane recording the operation
-- leaves a retry free to resolve a newer baseline: other references, another key, a second operation, and the first one
-- orphaned at ERP. With the intent fixed first, every retry for the approved plan sends the same references under the
-- same key, so ERP returns its prior operation.
--
-- One intent per approved plan of a provisioning. It is fixed once recorded: a different plan is a different tuple and
-- gets its own intent. The references carry no accounting value; functional currencies are ERP's answers, kept verbatim.
CREATE TABLE provisioning.erp_submission_intent (
    tenant_provisioning_id uuid NOT NULL REFERENCES provisioning.tenant_provisioning (tenant_provisioning_id),
    plan_id                text NOT NULL,
    plan_version           integer NOT NULL CHECK (plan_version >= 1),
    plan_digest            text NOT NULL,
    finance_baselines      jsonb NOT NULL CHECK (jsonb_typeof(finance_baselines) = 'array' AND jsonb_array_length(finance_baselines) >= 1),
    functional_currencies  text[] NOT NULL CHECK (cardinality(functional_currencies) >= 1),
    recorded_at            timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_provisioning_id, plan_id, plan_version, plan_digest)
);

CREATE FUNCTION provisioning.erp_submission_intent_fixed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'an ERP submission intent is fixed once recorded';
END $$;

CREATE TRIGGER erp_submission_intent_fixed
    BEFORE UPDATE ON provisioning.erp_submission_intent
    FOR EACH ROW EXECUTE FUNCTION provisioning.erp_submission_intent_fixed();
