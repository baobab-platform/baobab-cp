-- Shadow evidence for the roles-to-grants migration (ADR-BCP-020 section 144;
-- roles-to-grants decision). Every role-guarded request compares the legacy
-- role decision with the AdministrativeGrant decision; the Prometheus counter
-- forgets on restart, so the comparison is also kept here, aggregated per UTC
-- day, permission and outcome. It is evidence for the owner's decision to move
-- a permission from roles to grants; it never authorises anything. Rows carry
-- counts only: no principal, tenant, route parameter or request content.
CREATE TABLE policy.administrative_shadow_daily (
    day date NOT NULL,
    permission text NOT NULL CHECK (permission ~ '^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$' OR permission = 'unregistered'),
    legacy text NOT NULL CHECK (legacy IN ('allow', 'deny')),
    grants text NOT NULL CHECK (grants IN ('allow', 'deny', 'step_up', 'approval_required', 'not_ready', 'unmapped', 'unresolved', 'error')),
    agreement text NOT NULL CHECK (agreement IN ('agree', 'grants_broader', 'grants_narrower', 'not_evaluated')),
    decisions bigint NOT NULL CHECK (decisions > 0),
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    PRIMARY KEY (day, permission, legacy, grants, agreement),
    CHECK (last_observed_at >= first_observed_at)
);

CREATE INDEX administrative_shadow_daily_permission_idx ON policy.administrative_shadow_daily (permission, day);
