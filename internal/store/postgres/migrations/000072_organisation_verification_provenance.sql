-- ADR-BCP-023 OEV-03 (OEV-00 inventory, "Organisation (profile)"): the
-- organisation profile records who verified it and when, and cannot be
-- VERIFIED without evidence, as its sibling legal-entity, corporate- and
-- platform-relationship tables already require.
--
-- Existing VERIFIED rows are backfilled from the audit record of the
-- transition that verified them (organisation.verified, or
-- organisation.created for first-party governance, which creates the
-- profile VERIFIED). Nothing is guessed: a VERIFIED row without evidence or
-- without that audit record fails the constraint below, and the migration
-- stops for review rather than inventing provenance.

ALTER TABLE registry.organisation_profile
    ADD COLUMN IF NOT EXISTS verified_by text,
    ADD COLUMN IF NOT EXISTS verified_at timestamptz;

UPDATE registry.organisation_profile op
SET (verified_by, verified_at) = (
    SELECT a.actor_id, a.occurred_at FROM audit_events a
    WHERE a.target = 'organisation/' || op.canonical_entity_id::text
      AND a.action IN ('organisation.verified', 'organisation.created')
      AND a.actor_id IS NOT NULL
    ORDER BY (a.action = 'organisation.verified') DESC, a.occurred_at DESC
    LIMIT 1
)
WHERE op.verification_state = 'VERIFIED' AND op.verified_at IS NULL;

ALTER TABLE registry.organisation_profile
    DROP CONSTRAINT IF EXISTS organisation_profile_verified_has_evidence;
ALTER TABLE registry.organisation_profile
    ADD CONSTRAINT organisation_profile_verified_has_evidence CHECK (
        verification_state <> 'VERIFIED'
        OR (jsonb_array_length(evidence_references) >= 1
            AND verified_by IS NOT NULL AND verified_at IS NOT NULL)
    );
