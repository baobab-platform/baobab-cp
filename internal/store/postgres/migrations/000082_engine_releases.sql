-- ADR-BCP-025 gate ER-02: the Control Plane's immutable record of engine
-- releases (Shared topology/v1 release.schema.json).
--
-- Immutability is enforced here as well as in code (ADR-BCP-025 section
-- 4): a release's engine, version, artifacts, provider support,
-- declaration digest, source revision and provenance never change; only
-- its status moves, along release-policy.yaml status_transitions, with an
-- audited reason. Nothing is ever deleted. An artifact digest belongs to
-- at most one release of one engine (section 2.1 rule 3).
--
-- Releases keep a UUID surrogate. Their canonical identifier, the one every
-- API carries (control-plane/v1 engineReleaseId), is "erl_" and the UUID's
-- hex digits, generated so it never drifts from the row, as engine
-- instances (000058) and providers (000081) do.

CREATE TABLE topology.engine_release (
    engine_release_id     uuid PRIMARY KEY,
    release_key           text GENERATED ALWAYS AS ('erl_' || replace(engine_release_id::text, '-', '')) STORED,
    engine_id             uuid NOT NULL REFERENCES topology.engine(engine_id),
    release_version       text NOT NULL
        CHECK (length(release_version) <= 64
            AND release_version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'),
    source_revision       text NOT NULL CHECK (source_revision ~ '^[0-9a-f]{40}$'),
    declaration_digest    text NOT NULL CHECK (declaration_digest ~ '^sha256:[0-9a-f]{64}$'),
    provenance            jsonb,
    -- sha256 over the release's immutable content, so a replay is
    -- recognised as byte-identical without re-reading every child row.
    content_digest        text NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    status                text NOT NULL DEFAULT 'CANDIDATE'
        CHECK (status IN ('CANDIDATE', 'APPROVED', 'DEPRECATED', 'REVOKED')),
    recorded_by           text NOT NULL CHECK (length(recorded_by) BETWEEN 3 AND 128),
    recorded_at           timestamptz NOT NULL,
    reason                text NOT NULL CHECK (length(reason) BETWEEN 3 AND 500),
    status_changed_by     text,
    status_changed_at     timestamptz,
    status_reason         text CHECK (status_reason IS NULL OR length(status_reason) BETWEEN 3 AND 500),
    UNIQUE (engine_id, release_version),
    CHECK ((status = 'CANDIDATE') = (status_changed_at IS NULL)),
    CHECK ((status_changed_at IS NULL) = (status_changed_by IS NULL)),
    CHECK ((status_changed_at IS NULL) = (status_reason IS NULL))
);
CREATE UNIQUE INDEX engine_release_key_uq ON topology.engine_release (release_key);
CREATE INDEX engine_release_engine_status_idx ON topology.engine_release (engine_id, status);
CREATE INDEX engine_release_recorded_idx ON topology.engine_release (recorded_at DESC, engine_release_id DESC);

CREATE TABLE topology.engine_release_artifact (
    engine_release_id uuid NOT NULL REFERENCES topology.engine_release(engine_release_id),
    ordinal           integer NOT NULL CHECK (ordinal >= 0),
    artifact_type     text NOT NULL CHECK (artifact_type IN ('OCI_IMAGE')),
    repository        text NOT NULL CHECK (length(repository) <= 255),
    digest            text NOT NULL CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
    platform          text CHECK (platform IS NULL OR length(platform) <= 64),
    display_tag       text CHECK (display_tag IS NULL OR length(display_tag) <= 128),
    PRIMARY KEY (engine_release_id, ordinal),
    -- One owner per digest, across every release of every engine.
    CONSTRAINT engine_release_artifact_digest_uq UNIQUE (digest)
);

CREATE TABLE topology.engine_release_provider_support (
    engine_release_id uuid NOT NULL REFERENCES topology.engine_release(engine_release_id),
    ordinal           integer NOT NULL CHECK (ordinal >= 0),
    provider_key      text NOT NULL CHECK (provider_key ~ '^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$'),
    capability_key    text NOT NULL,
    contract_versions integer[] NOT NULL CHECK (cardinality(contract_versions) > 0 AND 1 <= ALL (contract_versions)),
    PRIMARY KEY (engine_release_id, ordinal),
    UNIQUE (engine_release_id, provider_key, capability_key)
);

-- A release's identity never changes; its status moves only along the
-- release lifecycle (CANDIDATE -> APPROVED -> DEPRECATED, and any of them
-- -> REVOKED, which is terminal). Releases are never deleted.
CREATE FUNCTION topology.engine_release_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'engine releases are never deleted (ADR-BCP-025 section 2.1)';
    END IF;
    IF (NEW.engine_release_id, NEW.engine_id, NEW.release_version, NEW.source_revision, NEW.declaration_digest,
            NEW.provenance, NEW.content_digest, NEW.recorded_by, NEW.recorded_at, NEW.reason)
        IS DISTINCT FROM
        (OLD.engine_release_id, OLD.engine_id, OLD.release_version, OLD.source_revision, OLD.declaration_digest,
            OLD.provenance, OLD.content_digest, OLD.recorded_by, OLD.recorded_at, OLD.reason) THEN
        RAISE EXCEPTION 'an engine release is immutable; record a new version (ADR-BCP-025 section 2.1 rule 1)';
    END IF;
    IF NEW.status IS DISTINCT FROM OLD.status AND NOT (
        (OLD.status = 'CANDIDATE' AND NEW.status = 'APPROVED')
        OR (OLD.status = 'APPROVED' AND NEW.status = 'DEPRECATED')
        OR (OLD.status <> 'REVOKED' AND NEW.status = 'REVOKED')) THEN
        RAISE EXCEPTION 'engine release status % cannot move to % (release-policy.yaml status_transitions)', OLD.status, NEW.status;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER engine_release_immutable BEFORE UPDATE OR DELETE ON topology.engine_release
    FOR EACH ROW EXECUTE FUNCTION topology.engine_release_immutable();

-- A release's artifacts and provider support are part of its identity.
CREATE FUNCTION topology.engine_release_part_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% rows are part of an immutable engine release (ADR-BCP-025 section 2.1)', TG_TABLE_NAME;
END;
$$;
CREATE TRIGGER engine_release_artifact_immutable BEFORE UPDATE OR DELETE ON topology.engine_release_artifact
    FOR EACH ROW EXECUTE FUNCTION topology.engine_release_part_immutable();
CREATE TRIGGER engine_release_provider_support_immutable BEFORE UPDATE OR DELETE ON topology.engine_release_provider_support
    FOR EACH ROW EXECUTE FUNCTION topology.engine_release_part_immutable();
