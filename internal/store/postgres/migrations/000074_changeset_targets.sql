-- ADR-BCP-021 adoption: a changeset targets a tenant, a market or a mapping
-- (Shared changeset-lifecycle.yaml change_kinds target). The semantic lock
-- on overlapping changes (section 66) is held on (target_type, target_id);
-- target_tenant_id stays for tenant changesets and tenant listing.
--
-- An approval records the approver's verified token subject as well as the
-- principal, so a mapping activation's maker-checker compares the same
-- identity the direct route compares (mapping.created_by is a subject).

ALTER TABLE changeset.changeset
    ADD COLUMN target_type text,
    ADD COLUMN target_id   text;

UPDATE changeset.changeset SET target_type = 'TENANT', target_id = target_tenant_id;

ALTER TABLE changeset.changeset
    ALTER COLUMN target_type SET NOT NULL,
    ALTER COLUMN target_id SET NOT NULL,
    ALTER COLUMN target_tenant_id DROP NOT NULL,
    ADD CONSTRAINT changeset_target_type_check CHECK (target_type IN ('TENANT', 'MARKET', 'MAPPING')),
    ADD CONSTRAINT changeset_target_tenant_check CHECK ((target_type = 'TENANT') = (target_tenant_id IS NOT NULL)
        AND (target_type <> 'TENANT' OR target_tenant_id = target_id));

DROP INDEX changeset.changeset_target_open_idx;
CREATE INDEX changeset_target_open_idx ON changeset.changeset (target_type, target_id)
    WHERE state NOT IN ('DRAFT', 'INVALID', 'REJECTED', 'COMPLETED', 'CANCELLED', 'SUPERSEDED', 'EXPIRED', 'COMPENSATED');
CREATE INDEX changeset_target_tenant_idx ON changeset.changeset (target_tenant_id) WHERE target_tenant_id IS NOT NULL;

ALTER TABLE changeset.approval ADD COLUMN decided_by_subject text;
