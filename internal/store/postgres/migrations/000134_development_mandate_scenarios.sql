-- DEV-02: isolated, explicitly NON-AUTHORISING policy inputs, not legal
-- mandate activation or legal verification. Only guarded CP setup seeds these.
CREATE TABLE development.mandate_scenario (
 run_id text NOT NULL REFERENCES development.bootstrap_run(run_id),
 enterprise_key text NOT NULL CHECK (enterprise_key IN ('zuribeans','equator-estate')),
 case_key text NOT NULL CHECK (case_key IN ('valid_scoped','missing','stale','revoked','wrong_market','pending')),
 tenant_id text NOT NULL REFERENCES tenants(tenant_id),
 operating_organisation_id uuid NOT NULL REFERENCES registry.organisation_profile(canonical_entity_id),
 synthetic_parent_legal_entity_id text NOT NULL REFERENCES registry.legal_entity_profile(legal_entity_id),
 fixture_mandate_id uuid,
 status text NOT NULL CHECK (status IN ('ACTIVE','MISSING','REVOKED','PENDING')),
 market_scope text[] NOT NULL CHECK (market_scope <@ ARRAY['ZA','UG']::text[] AND cardinality(market_scope)=1),
 effective_from timestamptz NOT NULL,
 effective_to timestamptz NOT NULL CHECK (effective_to > effective_from),
 PRIMARY KEY(run_id,enterprise_key,case_key),
 UNIQUE(fixture_mandate_id),
 CHECK ((case_key='missing')=(fixture_mandate_id IS NULL)),
 CHECK ((case_key='missing')=(status='MISSING'))
);
COMMENT ON TABLE development.mandate_scenario IS
 'Synthetic scope/lifecycle test inputs only; never registry.operating_legal_actor_mandate or verified legal evidence';
CREATE FUNCTION development.mandate_scenario_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'development mandate scenarios are immutable; recreate disposable run' USING ERRCODE='check_violation';
END $$;
CREATE TRIGGER mandate_scenario_immutable BEFORE UPDATE OR DELETE ON development.mandate_scenario
 FOR EACH ROW EXECUTE FUNCTION development.mandate_scenario_immutable();
REVOKE ALL ON development.mandate_scenario FROM PUBLIC;
