-- EA-02D: a capability provider becomes ACTIVE only through a
-- PROVIDER_ACTIVATION changeset (Shared control-plane/v1
-- changeset-lifecycle.yaml); registration never activates one (EA-02C).
--
-- Providers keep their UUID as an internal surrogate. Their canonical
-- identifier, the one a desired change and a plan step carry
-- (capability/v1 capabilityProviderId), is "provider_" and the UUID's hex
-- digits, generated so it can never drift from the row it names
-- (domain.ProviderID derives the same value), as engine instances do
-- (migration 000058).
ALTER TABLE capability.capability_provider
    ADD COLUMN IF NOT EXISTS canonical_provider_id text
        GENERATED ALWAYS AS ('provider_' || replace(provider_id::text, '-', '')) STORED;

CREATE UNIQUE INDEX IF NOT EXISTS capability_provider_canonical_id_uq
    ON capability.capability_provider (canonical_provider_id);

-- A changeset may now target a provider.
ALTER TABLE changeset.changeset DROP CONSTRAINT changeset_target_type_check;
ALTER TABLE changeset.changeset
    ADD CONSTRAINT changeset_target_type_check CHECK (target_type IN ('TENANT', 'MARKET', 'MAPPING', 'PROVIDER'));
