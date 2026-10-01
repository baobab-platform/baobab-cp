-- ADR-BCP-025 gate ER-05. The release drift of an engine instance
-- (section 2.7): the instance differs from its desired release in one of the
-- registered ways, for at least the policy's grace period. A row exists only
-- while a condition holds; it is removed when the instance converges.
-- condition_since is when the current reason first held continuously, so the
-- grace period is measured the same way by every evaluation, on an
-- observation or on the periodic sweep. opened_at is set when the grace period
-- has passed: the instance is then in drift, and a drift-detected event is
-- published once per opening or change of reason.
CREATE TABLE topology.engine_instance_release_drift (
    engine_instance_key  text PRIMARY KEY REFERENCES topology.engine_instance (engine_instance_key),
    reason_code          text NOT NULL CHECK (reason_code IN
        ('RELEASE_MISMATCH', 'REVOKED_RELEASE_RUNNING', 'UNKNOWN_ARTIFACT_RUNNING', 'RELEASE_UNOBSERVED', 'DEPLOYMENT_LOCATION_MISMATCH')),
    condition_since      timestamptz NOT NULL,
    opened_at            timestamptz,
    -- The reason last announced by an event; NULL until one was published.
    announced_reason     text,
    observed_state       text NOT NULL,
    observed_release_key text,
    observation_key      text,
    desired_release_key  text,
    evaluated_at         timestamptz NOT NULL,
    CHECK (announced_reason IS NULL OR opened_at IS NOT NULL)
);
CREATE INDEX engine_instance_release_drift_open_idx
    ON topology.engine_instance_release_drift (reason_code) WHERE opened_at IS NOT NULL;
