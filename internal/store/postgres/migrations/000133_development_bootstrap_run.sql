-- ADR-SHARED-043. Empty in protected deployments. Only the separately built,
-- guarded CP development command writes this synthetic run ledger.
CREATE SCHEMA IF NOT EXISTS development;
CREATE TABLE development.bootstrap_run (
 run_id text PRIMARY KEY CHECK (run_id ~ '^[0-9a-f]{32}$'),
 request_digest text NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 result jsonb NOT NULL,
 CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours')
);
REVOKE ALL ON development.bootstrap_run FROM PUBLIC;
CREATE TABLE development.principal_assignment (
 principal_id uuid PRIMARY KEY REFERENCES identity.principal(principal_id),
 run_id text NOT NULL REFERENCES development.bootstrap_run(run_id),
 client_id text NOT NULL,
 tenant_id text NOT NULL REFERENCES tenants(tenant_id),
 UNIQUE(run_id,client_id)
);
REVOKE ALL ON development.principal_assignment FROM PUBLIC;
