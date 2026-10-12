-- CX-05D: database-backed caller-scoped directory pagination.
-- No new unrestricted materialized index or access policy is introduced.
-- Profile IDs are always joined to current CP evaluated grants in Go.
CREATE INDEX IF NOT EXISTS organisation_profile_directory_name_key_idx
    ON registry.organisation_profile (lower(display_name), canonical_entity_id);
CREATE INDEX IF NOT EXISTS organisation_profile_directory_state_idx
    ON registry.organisation_profile (verification_state, status, canonical_entity_id);
