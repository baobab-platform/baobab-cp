-- ADR-IAM-0033 MP2-C: Control Plane-owned, append-only identity runtime
-- verification profiles. A profile qualifies an existing CapabilityProvider +
-- EngineInstance pair; it creates neither provider lifecycle, capability
-- support, binding, entitlement nor deployment truth.

CREATE TABLE identity.identity_provider_runtime_profile (
    provider_id                uuid NOT NULL REFERENCES capability.capability_provider(provider_id),
    engine_instance_id         uuid NOT NULL REFERENCES topology.engine_instance(engine_instance_id),
    configuration_reference    text NOT NULL REFERENCES mapping.external_reference(external_reference_id),
    security_domain_reference  text NOT NULL REFERENCES mapping.external_reference(external_reference_id),
    artifact_digest            text NOT NULL CHECK (artifact_digest ~ '^sha256:[a-f0-9]{64}$'),
    revision                   bigint NOT NULL CHECK (revision >= 1),
    published_at               timestamptz NOT NULL,
    recorded_at                timestamptz NOT NULL,
    source                     text NOT NULL CHECK (length(source) BETWEEN 3 AND 256),
    content_digest             text NOT NULL CHECK (content_digest ~ '^sha256:[a-f0-9]{64}$'),
    PRIMARY KEY (provider_id, engine_instance_id, revision)
);

CREATE INDEX identity_provider_runtime_profile_current_idx
    ON identity.identity_provider_runtime_profile(provider_id, engine_instance_id, revision DESC);

CREATE TABLE identity.identity_runtime_capability_observation (
    provider_id               uuid NOT NULL,
    engine_instance_id        uuid NOT NULL,
    profile_revision          bigint NOT NULL,
    capability                text NOT NULL CHECK (capability IN (
        'HUMAN_AUTHENTICATION','CREDENTIAL_MANAGEMENT','SESSION_MANAGEMENT',
        'ACCOUNT_VERIFICATION','ACCOUNT_RECOVERY','MFA','PASSKEY',
        'OAUTH_AUTHORIZATION_SERVER','OIDC_PROVIDER','WORKLOAD_TOKEN_ISSUANCE',
        'ENTERPRISE_SSO','SAML_FEDERATION','OIDC_FEDERATION','IDENTITY_BROKERING',
        'IDENTITY_LIFECYCLE_PROVISIONING','DIRECTORY_SYNCHRONIZATION'
    )),
    verification_status       text NOT NULL CHECK (verification_status IN (
        'UNVERIFIED','VERIFIED','UNSUPPORTED','DEPLOYMENT_DEPENDENT'
    )),
    evidence_reference        text REFERENCES mapping.external_reference(external_reference_id),
    evidence_artifact_digest  text CHECK (
        evidence_artifact_digest IS NULL OR evidence_artifact_digest ~ '^sha256:[a-f0-9]{64}$'
    ),
    evidence_observed_at      timestamptz,
    evidence_expires_at       timestamptz,
    PRIMARY KEY (provider_id, engine_instance_id, profile_revision, capability),
    FOREIGN KEY (provider_id, engine_instance_id, profile_revision)
        REFERENCES identity.identity_provider_runtime_profile(provider_id, engine_instance_id, revision)
        ON DELETE RESTRICT,
    CHECK (
        (
            verification_status = 'VERIFIED'
            AND evidence_reference IS NOT NULL
            AND evidence_artifact_digest IS NOT NULL
            AND evidence_observed_at IS NOT NULL
            AND evidence_expires_at IS NOT NULL
            AND evidence_expires_at > evidence_observed_at
        )
        OR
        (
            verification_status <> 'VERIFIED'
            AND evidence_reference IS NULL
            AND evidence_artifact_digest IS NULL
            AND evidence_observed_at IS NULL
            AND evidence_expires_at IS NULL
        )
    )
);

CREATE OR REPLACE FUNCTION identity.identity_runtime_profile_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'identity runtime profiles are append-only; publish a new revision';
END;
$$;

CREATE TRIGGER identity_provider_runtime_profile_append_only
    BEFORE UPDATE OR DELETE ON identity.identity_provider_runtime_profile
    FOR EACH ROW EXECUTE FUNCTION identity.identity_runtime_profile_append_only();

CREATE TRIGGER identity_provider_runtime_profile_no_truncate
    BEFORE TRUNCATE ON identity.identity_provider_runtime_profile
    FOR EACH STATEMENT EXECUTE FUNCTION identity.identity_runtime_profile_append_only();

CREATE TRIGGER identity_runtime_capability_observation_append_only
    BEFORE UPDATE OR DELETE ON identity.identity_runtime_capability_observation
    FOR EACH ROW EXECUTE FUNCTION identity.identity_runtime_profile_append_only();

CREATE TRIGGER identity_runtime_capability_observation_no_truncate
    BEFORE TRUNCATE ON identity.identity_runtime_capability_observation
    FOR EACH STATEMENT EXECUTE FUNCTION identity.identity_runtime_profile_append_only();
