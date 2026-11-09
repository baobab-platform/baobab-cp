-- ADR-SHARED-045: tenant/employer/capability/jurisdiction BYOP choice.
-- No default, seed, registration, activation, or cross-employer inheritance.
-- This table alone grants no authority. Runtime also requires active IAM
-- workload scope, CP Context+grant+binding+health and EA-09 certification.
CREATE TABLE capability.people_provider_choice (
    choice_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^tn_[a-z0-9]+$'),
    legal_employer_id text NOT NULL CHECK (legal_employer_id ~ '^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$'),
    jurisdiction char(2) NOT NULL CHECK (jurisdiction ~ '^[A-Z]{2}$'),
    capability_key text NOT NULL CHECK (capability_key IN (
        'workforce.employment.read', 'workforce.employment.manage',
        'payroll.run.read', 'payroll.evidence.attest'
    )),
    binding_id uuid NOT NULL REFERENCES capability.capability_binding(id),
    provider_id uuid NOT NULL REFERENCES capability.capability_provider(provider_id),
    binding_revision bigint NOT NULL CHECK (binding_revision > 0),
    certification_id uuid NOT NULL REFERENCES capability.provider_capability_certification(certification_id),
    source_of_record text NOT NULL CHECK (source_of_record IN ('NATIVE', 'EXTERNAL')),
    status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'REVOKED', 'RETIRED')),
    effective_from timestamptz NOT NULL,
    effective_until timestamptz,
    effective_period tstzrange GENERATED ALWAYS AS (tstzrange(effective_from, effective_until, '[)')) STORED,
    proposed_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    approved_by uuid REFERENCES identity.principal(principal_id),
    approved_at timestamptz,
    choice_evidence_ref text NOT NULL CHECK (length(choice_evidence_ref) BETWEEN 6 AND 160),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_until IS NULL OR effective_until > effective_from),
    CHECK (status <> 'ACTIVE' OR (
        approved_by IS NOT NULL AND approved_by <> proposed_by
        AND approved_at IS NOT NULL AND choice_evidence_ref <> ''
    ))
);
CREATE INDEX people_choice_exact_read_idx
 ON capability.people_provider_choice(tenant_id, legal_employer_id, jurisdiction, capability_key, status);
ALTER TABLE capability.people_provider_choice ADD CONSTRAINT people_choice_one_active_source
 EXCLUDE USING gist (tenant_id WITH =, legal_employer_id WITH =, jurisdiction WITH =,
     capability_key WITH =, effective_period WITH &&)
 WHERE (status = 'ACTIVE');

-- A fabricated UUID or a workload Principal must never be evidence of an
-- independent human maker/checker approval. This is a DB backstop, not a
-- substitute for the future IAM/CP governed activation executor.
CREATE FUNCTION capability.people_provider_choice_human_approval_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'ACTIVE' AND (
        NOT EXISTS (
            SELECT 1 FROM identity.principal p
            WHERE p.principal_id = NEW.proposed_by
              AND p.actor_type = 'human' AND p.status = 'ACTIVE'
        ) OR NOT EXISTS (
            SELECT 1 FROM identity.principal p
            WHERE p.principal_id = NEW.approved_by
              AND p.actor_type = 'human' AND p.status = 'ACTIVE'
        )
    ) THEN
        RAISE EXCEPTION 'active People choice requires two current human principals'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER people_provider_choice_human_approval
    BEFORE INSERT OR UPDATE ON capability.people_provider_choice
    FOR EACH ROW EXECUTE FUNCTION capability.people_provider_choice_human_approval_guard();
