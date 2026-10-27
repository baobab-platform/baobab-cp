-- DEV-07: native CP UUIDs and existing prefixed IDs describe the same typed
-- resources. Accept both wire representations; never manufacture aliases.
DO $$ DECLARE c record; BEGIN
  FOR c IN SELECT conname FROM pg_constraint
    WHERE conrelid='registry.estate_client_binding'::regclass AND contype='c'
      AND (pg_get_constraintdef(oid) LIKE '%digital_estate_id%' OR pg_get_constraintdef(oid) LIKE '%engine_instance_id%')
  LOOP EXECUTE format('ALTER TABLE registry.estate_client_binding DROP CONSTRAINT %I', c.conname); END LOOP;
END $$;
ALTER TABLE registry.estate_client_binding ADD CONSTRAINT estate_client_binding_estate_identifier CHECK (
  length(digital_estate_id) BETWEEN 3 AND 63 AND (digital_estate_id ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' OR
  digital_estate_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'));
ALTER TABLE registry.estate_client_binding ADD CONSTRAINT estate_client_binding_instance_identifier CHECK (
  length(engine_instance_id) BETWEEN 6 AND 63 AND (engine_instance_id ~ '^ei_[a-z0-9]+$' OR
  engine_instance_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'));

-- Records which exact guarded run owns the binding and workload. The private
-- development command uses this ledger; no production HTTP bypass is added.
CREATE TABLE development.buyer_identity_fixture (
  run_id text PRIMARY KEY REFERENCES development.bootstrap_run(run_id),
  principal_id uuid NOT NULL REFERENCES identity.principal(principal_id),
  binding_id uuid NOT NULL REFERENCES registry.estate_client_binding(binding_id),
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  estate_id uuid NOT NULL REFERENCES estate.digital_estate(digital_estate_id),
  engine_instance_id uuid NOT NULL REFERENCES topology.engine_instance(engine_instance_id),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
REVOKE ALL ON development.buyer_identity_fixture FROM PUBLIC;
