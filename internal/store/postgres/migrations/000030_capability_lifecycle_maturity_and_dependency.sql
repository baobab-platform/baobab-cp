-- Gate: complete capability.capability's lifecycle/maturity model
-- (ADR-BCP-003 SS4, SS6-SS7) and introduce capability.capability_dependency
-- (SS8) -- both confirmed absent or incomplete during the Capability
-- Platform build-out.
--
-- Migration 000024 added capability.capability.status but locked it to a
-- four-value set (DRAFT, ACTIVE, DEPRECATED, RETIRED) that omits SUSPENDED
-- -- mirrored in baobab-platform/shared's contracts/capability/v1/domain.schema.json
-- #/$defs/capabilityLifecycle, which is explicitly five-valued. No maturity
-- (SS7, distinct from lifecycle) or domain (SS5's namespace segment) column
-- existed at all.

UPDATE capability.capability SET status = upper(status);

ALTER TABLE capability.capability
    DROP CONSTRAINT IF EXISTS capability_status_ck;

ALTER TABLE capability.capability
    ADD CONSTRAINT capability_status_ck
    CHECK (status IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'DEPRECATED', 'RETIRED'));

ALTER TABLE capability.capability
    ADD COLUMN IF NOT EXISTS domain_key text,
    ADD COLUMN IF NOT EXISTS maturity text NOT NULL DEFAULT 'SUPPORTED';

ALTER TABLE capability.capability
    ADD CONSTRAINT capability_maturity_ck
    CHECK (maturity IN ('EXPERIMENTAL', 'PREVIEW', 'SUPPORTED', 'DEPRECATED', 'RETIRED'));

-- SS8: "A capability MAY depend on other capabilities... Required
-- dependency graphs SHALL be acyclic." Acyclicity itself is enforced in
-- Go (internal/capability/domain.HasCapabilityDependencyCycle) rather than
-- at the database level: a per-row CHECK cannot see the whole graph, and a
-- trigger-based graph-cycle check is disproportionate for a table with no
-- write path yet (see below).
CREATE TABLE IF NOT EXISTS capability.capability_dependency (
    id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    capability_id              uuid NOT NULL REFERENCES capability.capability(capability_id) ON DELETE CASCADE,
    depends_on_capability_id   uuid NOT NULL REFERENCES capability.capability(capability_id),
    dependency_type            text NOT NULL,
    version_constraint         text,
    condition                  text,
    status                     text NOT NULL DEFAULT 'ACTIVE',
    effective_from             timestamptz NOT NULL DEFAULT now(),
    effective_to               timestamptz,
    CHECK (dependency_type IN ('REQUIRED', 'OPTIONAL', 'CONDITIONAL')),
    CHECK (dependency_type <> 'CONDITIONAL' OR condition IS NOT NULL),
    CHECK (capability_id <> depends_on_capability_id),
    CHECK (status IN ('DRAFT', 'ACTIVE', 'DEPRECATED', 'RETIRED')),
    CHECK (effective_to IS NULL OR effective_to > effective_from),
    UNIQUE (capability_id, depends_on_capability_id)
);

CREATE INDEX IF NOT EXISTS capability_dependency_depends_on_idx
    ON capability.capability_dependency(depends_on_capability_id);
