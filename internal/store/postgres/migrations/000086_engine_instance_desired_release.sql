-- ADR-BCP-025 gate ER-03. Each engine instance gains a desired release
-- (section 2.5), the "CP desired" half of ADR-BCP-006 section 92. It is set
-- or cleared only through an ENGINE_INSTANCE_DESIRED_RELEASE changeset, or
-- by a revocation that disposes of it (section 2.4). The Control Plane
-- never deploys: infrastructure tooling reads the desired release.
ALTER TABLE topology.engine_instance
    ADD COLUMN desired_release_id          uuid REFERENCES topology.engine_release (engine_release_id),
    -- Increments on every change of desired release; a changeset's plan is
    -- computed against it, so a stale plan never applies.
    ADD COLUMN desired_release_version     bigint NOT NULL DEFAULT 1 CHECK (desired_release_version >= 1),
    -- The changeset that set the current desired release; NULL when a
    -- revocation set it, or it was never set.
    ADD COLUMN desired_release_changeset_id text,
    ADD COLUMN desired_release_updated_at  timestamptz;

CREATE INDEX engine_instance_desired_release_idx
    ON topology.engine_instance (desired_release_id) WHERE desired_release_id IS NOT NULL;

-- A changeset may now target an engine instance, by its canonical ei_
-- identifier (migration 000058 engine_instance_key).
ALTER TABLE changeset.changeset DROP CONSTRAINT changeset_target_type_check;
ALTER TABLE changeset.changeset
    ADD CONSTRAINT changeset_target_type_check
        CHECK (target_type IN ('TENANT', 'MARKET', 'MAPPING', 'PROVIDER', 'ENGINE_RELEASE', 'ENGINE_INSTANCE'));

-- The database refuses what the Control Plane never does: a newly desired
-- release that is not APPROVED (release-policy.yaml
-- desired_state.may_become_desired) or is another engine's.
CREATE FUNCTION topology.engine_instance_desired_release_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    release_status text;
    release_engine uuid;
BEGIN
    IF NEW.desired_release_id IS NOT NULL AND NEW.desired_release_id IS DISTINCT FROM OLD.desired_release_id THEN
        SELECT status, engine_id INTO release_status, release_engine
        FROM topology.engine_release WHERE engine_release_id = NEW.desired_release_id;
        IF release_status IS DISTINCT FROM 'APPROVED' THEN
            RAISE EXCEPTION 'only an APPROVED release may become desired (ADR-BCP-025 section 2.5)';
        END IF;
        IF release_engine IS DISTINCT FROM NEW.engine_id THEN
            RAISE EXCEPTION 'a desired release must be a release of the instance''s own engine';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER engine_instance_desired_release_guard BEFORE UPDATE OF desired_release_id ON topology.engine_instance
    FOR EACH ROW EXECUTE FUNCTION topology.engine_instance_desired_release_guard();

-- Revoking a release never leaves it desired (section 2.4): a revocation
-- disposes of every desiring instance before the release becomes REVOKED,
-- in the same transaction.
CREATE FUNCTION topology.engine_release_revocation_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'REVOKED' AND OLD.status <> 'REVOKED'
        AND EXISTS (SELECT 1 FROM topology.engine_instance WHERE desired_release_id = NEW.engine_release_id) THEN
        RAISE EXCEPTION 'a revoked release is never desired: dispose of every instance that desires it first (ADR-BCP-025 section 2.4)';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER engine_release_revocation_guard BEFORE UPDATE OF status ON topology.engine_release
    FOR EACH ROW EXECUTE FUNCTION topology.engine_release_revocation_guard();
