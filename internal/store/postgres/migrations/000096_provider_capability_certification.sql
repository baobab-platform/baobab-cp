-- EA-09: release-bound ProviderCapabilityCertification.
--
-- Certification qualifies one provider/capability/contract-major on one
-- immutable EngineRelease. It is independent of provider lifecycle, release
-- approval, bindings, grants and health. Revocation preserves the immutable
-- qualification evidence while making the record ineligible for policy.
CREATE TABLE capability.provider_capability_certification (
    certification_id      uuid PRIMARY KEY,
    certification_key     text GENERATED ALWAYS AS
        ('cert_' || replace(certification_id::text, '-', '')) STORED,
    provider_id           uuid NOT NULL
        REFERENCES capability.capability_provider(provider_id),
    capability_id         uuid NOT NULL
        REFERENCES capability.capability(capability_id),
    contract_version      integer NOT NULL CHECK (contract_version > 0),
    engine_release_id     uuid NOT NULL
        REFERENCES topology.engine_release(engine_release_id),
    qualification_profile text NOT NULL
        CHECK (length(qualification_profile) BETWEEN 3 AND 128),
    evidence              jsonb NOT NULL
        CHECK (jsonb_typeof(evidence) = 'array' AND jsonb_array_length(evidence) > 0),
    request_digest        text NOT NULL
        CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    status                text NOT NULL DEFAULT 'CERTIFIED'
        CHECK (status IN ('CERTIFIED', 'REVOKED')),
    certified_by          text NOT NULL CHECK (length(certified_by) BETWEEN 3 AND 128),
    certified_at          timestamptz NOT NULL,
    valid_until           timestamptz,
    reason                text NOT NULL CHECK (length(reason) BETWEEN 3 AND 500),
    revoked_by            text,
    revoked_at            timestamptz,
    revocation_reason     text
        CHECK (revocation_reason IS NULL OR length(revocation_reason) BETWEEN 3 AND 500),
    version               bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    CHECK (valid_until IS NULL OR valid_until > certified_at),
    CHECK (
        (status = 'CERTIFIED' AND revoked_by IS NULL AND revoked_at IS NULL AND revocation_reason IS NULL)
        OR
        (status = 'REVOKED' AND revoked_by IS NOT NULL AND revoked_at IS NOT NULL AND revocation_reason IS NOT NULL)
    )
);

CREATE UNIQUE INDEX provider_capability_certification_key_uq
    ON capability.provider_capability_certification(certification_key);

-- At most one current certification for a provider/capability/major/release.
-- A revoked certification may later be replaced by a new qualification.
CREATE UNIQUE INDEX provider_capability_certification_current_uq
    ON capability.provider_capability_certification(
        provider_id, capability_id, contract_version, engine_release_id
    ) WHERE status = 'CERTIFIED';

CREATE INDEX provider_capability_certification_lookup_idx
    ON capability.provider_capability_certification(
        provider_id, capability_id, engine_release_id, status
    );

CREATE INDEX provider_capability_certification_release_idx
    ON capability.provider_capability_certification(engine_release_id, status);
