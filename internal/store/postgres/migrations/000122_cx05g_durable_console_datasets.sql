-- CX-05G: durable governed datasets. Mutation authority remains with CP,
-- never inferred from an organisation directory read or corporate control.
CREATE TABLE IF NOT EXISTS operations.console_dataset_plan (
    digest text PRIMARY KEY CHECK (digest ~ '^[0-9a-f]{64}$'),
    kind text NOT NULL CHECK (kind IN ('REDACTED_CSV_EXPORT','BULK_METADATA_PROPOSAL')),
    maker text NOT NULL,
    idempotency_key text NOT NULL,
    intent jsonb NOT NULL,
    changes jsonb NOT NULL DEFAULT '[]'::jsonb,
    state text NOT NULL DEFAULT 'PENDING_APPROVAL'
        CHECK (state IN ('PENDING_APPROVAL','APPROVED','COMPLETED')),
    approval jsonb,
    checker text,
    artifact bytea,
    expires_at timestamptz NOT NULL,
    approved_at timestamptz,
    executed_at timestamptz,
    audit_count integer NOT NULL DEFAULT 0,
    download_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (maker,idempotency_key),
    CHECK (state = 'PENDING_APPROVAL' OR (checker IS NOT NULL AND approval IS NOT NULL)),
    CHECK (state <> 'COMPLETED' OR executed_at IS NOT NULL),
    CHECK (artifact IS NULL OR kind='REDACTED_CSV_EXPORT')
);
CREATE INDEX console_dataset_expiry_idx ON operations.console_dataset_plan(expires_at, state);

-- A dedicated revision for the ONLY allowed bulk field, not a generic
-- organisation identity/version or applicant-controlled status transition.
ALTER TABLE registry.organisation_profile
    ADD COLUMN IF NOT EXISTS console_metadata_version bigint NOT NULL DEFAULT 1
        CHECK (console_metadata_version >= 1);
