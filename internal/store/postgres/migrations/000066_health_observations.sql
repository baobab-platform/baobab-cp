-- ADR-BCP-006 sections 18-22 and 72-73, ADR-SHARED-007 section 37.1:
-- health is a time-bounded observation, and each capability declares the
-- health it tolerates.

-- Which provider health a capability tolerates (capability/v1
-- capabilityHealthCriticality). CRITICAL accepts only HEALTHY; STANDARD,
-- every existing capability, keeps today's HEALTHY or UNKNOWN.
ALTER TABLE capability.capability
    ADD COLUMN health_criticality text NOT NULL DEFAULT 'STANDARD'
        CONSTRAINT capability_health_criticality_ck CHECK (health_criticality IN ('CRITICAL', 'STANDARD'));

-- The newest observation of each subject: an engine instance, a provider,
-- or one capability on a provider. Health is ephemeral operational state
-- (section 20), so an older observation is replaced, not kept; history
-- belongs in observability storage (section 117).
CREATE TABLE topology.health_observation (
    health_observation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    engine_instance_id uuid REFERENCES topology.engine_instance(engine_instance_id) ON DELETE CASCADE,
    provider_id uuid REFERENCES capability.capability_provider(provider_id) ON DELETE CASCADE,
    capability_id uuid REFERENCES capability.capability(capability_id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('UNKNOWN', 'HEALTHY', 'DEGRADED', 'UNAVAILABLE')),
    observed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    source text NOT NULL CHECK (source IN ('ACTIVE_PROBE', 'PASSIVE_TELEMETRY', 'ENGINE_REPORT', 'OPERATOR')),
    reasons text[] NOT NULL DEFAULT '{}',
    recorded_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT health_observation_subject_ck CHECK (
        (engine_instance_id IS NOT NULL AND provider_id IS NULL AND capability_id IS NULL)
        OR (engine_instance_id IS NULL AND provider_id IS NOT NULL)),
    CONSTRAINT health_observation_window_ck CHECK (expires_at > observed_at),
    CONSTRAINT health_observation_reasons_ck CHECK (status = 'HEALTHY' OR cardinality(reasons) > 0),
    CONSTRAINT health_observation_reason_codes_ck CHECK (
        cardinality(reasons) <= 16
        AND array_to_string(reasons, ',') ~ '^([A-Z][A-Z0-9]*(_[A-Z0-9]+)+(,|$))*$')
);
CREATE UNIQUE INDEX health_observation_instance_uq ON topology.health_observation (engine_instance_id)
    WHERE engine_instance_id IS NOT NULL;
CREATE UNIQUE INDEX health_observation_provider_uq ON topology.health_observation (provider_id)
    WHERE provider_id IS NOT NULL AND capability_id IS NULL;
CREATE UNIQUE INDEX health_observation_provider_capability_uq ON topology.health_observation (provider_id, capability_id)
    WHERE capability_id IS NOT NULL;

-- engine_instance.health_status (migration 000024) was never read or
-- written by the Control Plane and stored health as if it were
-- configuration, which section 20 forbids. Health comes from
-- health_observation only.
ALTER TABLE topology.engine_instance DROP COLUMN health_status;
-- Dropping the column dropped engine_instance_eligibility_idx with it;
-- keep the eligibility index on the remaining columns.
CREATE INDEX engine_instance_eligibility_idx
    ON topology.engine_instance(engine_id, environment, region, status)
    WHERE status IN ('ACTIVE', 'DRAINING');
