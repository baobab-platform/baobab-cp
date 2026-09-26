-- ADR-BCP-018 gate ORG-05, sections 26-29: CorporateGroup membership is
-- derived state. This table is the derivation queue and its drift record,
-- one row per derivable group.
--
-- A group needs re-deriving whenever the corporate graph changes. The
-- triggers below mark every derivable group in the same transaction as the
-- change, so a committed change can never go unnoticed. A worker
-- (organisation.GroupDerivationWorker) then re-derives asynchronously: a
-- failed derivation never rolls back the authoritative relationship, it
-- stays recorded here and is retried. A scheduled sweep also marks every
-- group, repairing drift from anything the triggers cannot see, such as a
-- relationship reaching its effective_to.
--
-- Every derivable group is marked, not only those touching the changed
-- organisations: which groups a graph change affects is itself the
-- derivation's question, and groups are few.
--
-- Group membership confers no access (section 29). Nothing reads this table
-- for authorization, tenancy, PlatformAccount membership or INTERNAL
-- eligibility.
CREATE TABLE IF NOT EXISTS registry.corporate_group_derivation (
    corporate_group_id  uuid PRIMARY KEY
        REFERENCES registry.corporate_group(corporate_group_id),
    -- requested_at is set while a derivation is owed and cleared once one
    -- that started after the latest request has succeeded.
    requested_at        timestamptz,
    request_reason      text,
    -- request_seq changes with every request, so a derivation only clears
    -- the request it actually saw.
    request_seq         bigint NOT NULL DEFAULT 0,
    attempts            integer NOT NULL DEFAULT 0,
    next_attempt_at     timestamptz,
    -- leased_until is set while a worker derives the group, so no other
    -- worker derives it concurrently; a request never shortens it.
    leased_until        timestamptz,
    last_attempt_at     timestamptz,
    last_succeeded_at   timestamptz,
    last_error          text,
    last_added          integer NOT NULL DEFAULT 0,
    last_ended          integer NOT NULL DEFAULT 0,
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (attempts >= 0),
    CHECK (requested_at IS NOT NULL OR next_attempt_at IS NULL),
    CHECK (request_reason IS NULL OR length(request_reason) <= 200),
    CHECK (last_error IS NULL OR length(last_error) <= 1000)
);

CREATE INDEX IF NOT EXISTS corporate_group_derivation_due_idx
    ON registry.corporate_group_derivation(next_attempt_at)
    WHERE requested_at IS NOT NULL;

-- request_corporate_group_derivation marks groups as owing a derivation:
-- one group, or every derivable group when target is NULL. A group already
-- waiting on a retry backoff becomes due now, because the graph it failed
-- against has changed; a group being derived stays leased, and is derived
-- again once that derivation ends.
CREATE OR REPLACE FUNCTION registry.request_corporate_group_derivation(target uuid, reason text)
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE
    marked integer;
BEGIN
    INSERT INTO registry.corporate_group_derivation AS d
        (corporate_group_id, requested_at, request_reason, request_seq, next_attempt_at)
    SELECT g.corporate_group_id, now(), left(reason, 200), 1, now()
    FROM registry.corporate_group g
    WHERE (target IS NULL OR g.corporate_group_id = target)
      AND g.grouping_policy = 'verified-control-majority/v1'
      AND g.status IN ('PENDING', 'ACTIVE')
    ON CONFLICT (corporate_group_id) DO UPDATE SET
        requested_at = COALESCE(d.requested_at, now()),
        request_reason = left(reason, 200),
        request_seq = d.request_seq + 1,
        next_attempt_at = now(),
        updated_at = now();
    GET DIAGNOSTICS marked = ROW_COUNT;
    RETURN marked;
END;
$$;

CREATE OR REPLACE FUNCTION registry.corporate_relationship_changed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM registry.request_corporate_group_derivation(NULL, 'corporate relationship ' || lower(TG_OP));
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS corporate_relationship_requests_group_derivation ON registry.corporate_relationship;
CREATE TRIGGER corporate_relationship_requests_group_derivation
    AFTER INSERT OR UPDATE OR DELETE ON registry.corporate_relationship
    FOR EACH STATEMENT EXECUTE FUNCTION registry.corporate_relationship_changed();

-- A new group, or a change to a group's root, policy or status, needs that
-- group derived.
CREATE OR REPLACE FUNCTION registry.corporate_group_changed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT'
       OR NEW.root_organisation_id IS DISTINCT FROM OLD.root_organisation_id
       OR NEW.grouping_policy IS DISTINCT FROM OLD.grouping_policy
       OR NEW.status IS DISTINCT FROM OLD.status THEN
        PERFORM registry.request_corporate_group_derivation(NEW.corporate_group_id, 'corporate group ' || lower(TG_OP));
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS corporate_group_requests_derivation ON registry.corporate_group;
CREATE TRIGGER corporate_group_requests_derivation
    AFTER INSERT OR UPDATE ON registry.corporate_group
    FOR EACH ROW EXECUTE FUNCTION registry.corporate_group_changed();

-- Groups that existed before this migration start owed a derivation.
SELECT registry.request_corporate_group_derivation(NULL, 'initial derivation');
