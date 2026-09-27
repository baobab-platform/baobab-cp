-- ADR-BCP-020 gate ADA-02: administrative grants (Shared administration/v1
-- AdministrativeGrant). One principal, one permission, one scope, a
-- lifecycle and provenance. The Control Plane evaluates them; nothing here
-- is derived from IAM roles.
CREATE TABLE policy.administrative_grant (
    grant_id uuid PRIMARY KEY,
    principal_id text NOT NULL CHECK (principal_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$'),
    permission text NOT NULL CHECK (permission ~ '^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$'),
    scope_level text NOT NULL CHECK (scope_level IN ('PLATFORM', 'PLATFORM_ACCOUNT', 'CORPORATE_GROUP', 'ORGANISATION',
        'TENANT', 'LEGAL_ENTITY', 'MARKET', 'DIGITAL_ESTATE', 'RESOURCE')),
    scope jsonb NOT NULL CHECK (scope->>'level' = scope_level),
    conditions jsonb,
    grant_type text NOT NULL CHECK (grant_type IN ('STANDING', 'TIME_BOUND', 'JUST_IN_TIME')),
    source text NOT NULL CHECK (source IN ('DIRECT', 'PROFILE', 'DELEGATION', 'BOOTSTRAP')),
    profile_key text,
    delegated_from uuid REFERENCES policy.administrative_grant(grant_id),
    delegation_depth integer NOT NULL DEFAULT 0 CHECK (delegation_depth BETWEEN 0 AND 3),
    delegable_depth integer NOT NULL DEFAULT 0 CHECK (delegable_depth BETWEEN 0 AND 2),
    risk_class text NOT NULL CHECK (risk_class IN ('LOW', 'MODERATE', 'HIGH', 'CRITICAL')),
    valid_from timestamptz NOT NULL,
    valid_until timestamptz,
    status text NOT NULL CHECK (status IN ('PENDING', 'ACTIVE', 'SUSPENDED', 'EXPIRED', 'REVOKED')),
    granted_by text NOT NULL,
    approval_reference text CHECK (approval_reference IS NULL OR approval_reference ~ '^apd_[a-z0-9]+$'),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz,
    revoked_at timestamptz,
    revoked_by text,
    revocation_reason text,
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    -- Nobody grants to themselves (section 39).
    CONSTRAINT administrative_grant_not_self_ck CHECK (granted_by <> principal_id),
    CONSTRAINT administrative_grant_window_ck CHECK (
        (grant_type = 'STANDING' AND valid_until IS NULL)
        OR (grant_type <> 'STANDING' AND valid_until > valid_from)),
    CONSTRAINT administrative_grant_profile_ck CHECK ((source = 'PROFILE') = (profile_key IS NOT NULL)),
    CONSTRAINT administrative_grant_delegation_ck CHECK (
        (source = 'DELEGATION') = (delegated_from IS NOT NULL)
        AND (source = 'DELEGATION') = (delegation_depth > 0)),
    -- Bootstrap authority is platform-scoped and never standing (sections 128-129).
    CONSTRAINT administrative_grant_bootstrap_ck CHECK (
        source <> 'BOOTSTRAP' OR (scope_level = 'PLATFORM' AND grant_type = 'TIME_BOUND')),
    CONSTRAINT administrative_grant_revocation_ck CHECK (
        (status = 'REVOKED') = (revoked_at IS NOT NULL AND revoked_by IS NOT NULL AND revocation_reason IS NOT NULL))
);

-- Evaluation and the effective-authority read model load every grant of a
-- principal, in any state, so denials can name why.
CREATE INDEX administrative_grant_principal_idx ON policy.administrative_grant (principal_id, permission);
CREATE INDEX administrative_grant_delegated_from_idx ON policy.administrative_grant (delegated_from)
    WHERE delegated_from IS NOT NULL;
