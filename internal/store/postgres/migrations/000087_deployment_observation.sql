-- ADR-BCP-025 gate ER-04. A deployment observation is a time-bounded report,
-- by registered infrastructure tooling, of what is actually running on one
-- engine instance (section 2.6). Observations are append-only; the current
-- one and the observed release are derived on read. The Control Plane assigns
-- recorded_at and the monotonic ingestion_sequence, never the reporter.
CREATE TABLE topology.deployment_observation (
    observation_key      text PRIMARY KEY CHECK (observation_key ~ '^dob_[a-z0-9]{3,59}$'),
    -- Strictly increasing in acceptance order. It breaks ties between equal
    -- observed_at so every replica derives the same current observation.
    ingestion_sequence   bigint GENERATED ALWAYS AS IDENTITY NOT NULL UNIQUE,
    engine_instance_key  text NOT NULL REFERENCES topology.engine_instance (engine_instance_key),
    -- [{"digest": "sha256:...", "platform": "linux/amd64"}]; digests only.
    artifacts            jsonb NOT NULL CHECK (jsonb_typeof(artifacts) = 'array' AND jsonb_array_length(artifacts) BETWEEN 1 AND 64),
    -- What the reporter saw. A mismatch with the instance's own is drift,
    -- never an update to the instance.
    environment          text NOT NULL CHECK (environment <> ''),
    region               text NOT NULL CHECK (region ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND length(region) <= 63),
    observed_at          timestamptz NOT NULL,
    expires_at           timestamptz NOT NULL,
    recorded_at          timestamptz NOT NULL,
    source               text NOT NULL CHECK (source <> ''),
    CHECK (expires_at > observed_at)
);
CREATE INDEX deployment_observation_current_idx
    ON topology.deployment_observation (engine_instance_key, observed_at DESC, ingestion_sequence DESC);

CREATE FUNCTION topology.deployment_observation_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'deployment observations are append-only (ADR-BCP-025 section 2.6)';
END;
$$;
CREATE TRIGGER deployment_observation_append_only BEFORE UPDATE OR DELETE ON topology.deployment_observation
    FOR EACH ROW EXECUTE FUNCTION topology.deployment_observation_append_only();
CREATE TRIGGER deployment_observation_no_truncate BEFORE TRUNCATE ON topology.deployment_observation
    FOR EACH STATEMENT EXECUTE FUNCTION topology.deployment_observation_append_only();
