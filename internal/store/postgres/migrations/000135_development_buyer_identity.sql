-- Public estate identity follows the existing engine_instance_key model:
-- the CP owns the stable key; UUIDs remain internal relational surrogates.
-- No engine identifier grammar or legacy estate binding is changed.
ALTER TABLE estate.digital_estate ADD COLUMN digital_estate_key text
  GENERATED ALWAYS AS ('estate_' || replace(digital_estate_id::text, '-', '')) STORED;
ALTER TABLE estate.digital_estate ADD CONSTRAINT digital_estate_public_key_unique UNIQUE(digital_estate_key);

-- Only the separate, guarded CP development command writes this run ledger.
CREATE TABLE development.buyer_identity_fixture (
  run_id text PRIMARY KEY REFERENCES development.bootstrap_run(run_id),
  principal_id uuid NOT NULL REFERENCES identity.principal(principal_id),
  binding_id uuid NOT NULL REFERENCES registry.estate_client_binding(binding_id),
  buyer_organisation_id uuid NOT NULL REFERENCES registry.canonical_entity(canonical_entity_id),
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  estate_id uuid NOT NULL REFERENCES estate.digital_estate(digital_estate_id),
  engine_instance_id uuid NOT NULL REFERENCES topology.engine_instance(engine_instance_id),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
REVOKE ALL ON development.buyer_identity_fixture FROM PUBLIC;
