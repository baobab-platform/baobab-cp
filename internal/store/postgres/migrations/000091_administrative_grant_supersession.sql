-- Atomic grant replacement (roles-to-grants decision; Shared 1.28.0). A
-- replacement is a new immutable grant that names the grant it replaced,
-- and the replaced grant is REVOKED in the same transaction and names its
-- replacement. Neither record's authority-bearing fields are rewritten.
ALTER TABLE policy.administrative_grant
    ADD COLUMN supersedes_grant_id uuid REFERENCES policy.administrative_grant(grant_id),
    ADD COLUMN superseded_by_grant_id uuid REFERENCES policy.administrative_grant(grant_id),
    ADD CONSTRAINT administrative_grant_supersession_ck CHECK (
        supersedes_grant_id IS DISTINCT FROM grant_id
        AND superseded_by_grant_id IS DISTINCT FROM grant_id
        AND (superseded_by_grant_id IS NULL OR status = 'REVOKED'));

-- A grant is replaced at most once.
CREATE UNIQUE INDEX administrative_grant_superseded_by_uq ON policy.administrative_grant (superseded_by_grant_id)
    WHERE superseded_by_grant_id IS NOT NULL;
CREATE UNIQUE INDEX administrative_grant_supersedes_uq ON policy.administrative_grant (supersedes_grant_id)
    WHERE supersedes_grant_id IS NOT NULL;
