-- LA-04A / Accepted ADR-BCP-027: additive OperatingLegalActorMandate persistence.
-- Canonical source: Shared organisation/v2/legal-actor-mandate.schema.json at
-- 5930dcf07d16cbb138fa98af0059d9a35e011114.
--
-- IMPORTANT: no mandate may become ACTIVE in this foundation increment.
-- LA-04B must first implement independent maker/checker approval, actor and
-- PRIMARY-Organisation attestation, audit/outbox, revocation and APIs, then
-- explicitly replace operating_legal_actor_mandate_activation_gate.
-- Nothing here confers seller, issuer, importer, payment or ERP authority.
CREATE TABLE registry.operating_legal_actor_mandate (
    mandate_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id varchar(63) NOT NULL REFERENCES tenants(tenant_id),
    operating_organisation_id uuid NOT NULL
        REFERENCES registry.organisation_profile(canonical_entity_id),
    responsible_legal_entity_id varchar(63) NOT NULL
        REFERENCES registry.legal_entity_profile(legal_entity_id),
    roles text[] NOT NULL,
    activity_scope text[] NOT NULL,
    market_scope text[] NOT NULL,
    capability_scope text[] NOT NULL DEFAULT '{}'::text[],
    status text NOT NULL DEFAULT 'PENDING',
    authority_basis_reference text NOT NULL,
    evidence_references text[] NOT NULL,
    legal_actor_verification_reference text,
    effective_from timestamptz NOT NULL,
    effective_to timestamptz,
    created_by uuid NOT NULL REFERENCES identity.principal(principal_id),
    approved_by uuid REFERENCES identity.principal(principal_id),
    approved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_at timestamptz,
    supersedes_mandate_id uuid REFERENCES registry.operating_legal_actor_mandate(mandate_id),
    provenance text NOT NULL,
    CONSTRAINT operating_legal_actor_mandate_roles_ck CHECK (
        cardinality(roles) >= 1 AND array_position(roles, NULL) IS NULL AND
        roles <@ ARRAY[
            'CONTRACTING_PARTY','SELLER_OF_RECORD','INVOICE_ISSUER',
            'IMPORTER_OF_RECORD','EXPORTER_OF_RECORD','EMPLOYER',
            'ACCOUNTING_ENTITY','PAYMENT_BENEFICIARY','PROPERTY_OWNER',
            'SERVICE_PROVIDER_OF_RECORD'
        ]::text[]
    ),
    CONSTRAINT operating_legal_actor_mandate_activities_ck CHECK (
        cardinality(activity_scope) >= 1 AND
        array_position(activity_scope, NULL) IS NULL
    ),
    CONSTRAINT operating_legal_actor_mandate_markets_ck CHECK (
        cardinality(market_scope) >= 1 AND
        array_position(market_scope, NULL) IS NULL AND
        array_to_string(market_scope, ',') ~ '^[A-Z]{2}(,[A-Z]{2})*$'
    ),
    CONSTRAINT operating_legal_actor_mandate_capabilities_ck CHECK (
        array_position(capability_scope, NULL) IS NULL
    ),
    CONSTRAINT operating_legal_actor_mandate_state_ck CHECK (
        status IN ('PENDING','ACTIVE','SUSPENDED','REVOKED','EXPIRED')
    ),
    CONSTRAINT operating_legal_actor_mandate_evidence_ck CHECK (
        cardinality(evidence_references) >= 1 AND
        array_position(evidence_references, NULL) IS NULL AND
        length(btrim(authority_basis_reference)) > 0 AND
        length(btrim(provenance)) > 0
    ),
    CONSTRAINT operating_legal_actor_mandate_window_ck CHECK (
        effective_to IS NULL OR effective_to > effective_from
    ),
    CONSTRAINT operating_legal_actor_mandate_approval_ck CHECK (
        status <> 'ACTIVE' OR (
            approved_by IS NOT NULL AND approved_at IS NOT NULL AND
            approved_by <> created_by AND
            legal_actor_verification_reference IS NOT NULL AND
            length(btrim(legal_actor_verification_reference)) > 0
        )
    ),
    CONSTRAINT operating_legal_actor_mandate_revocation_ck CHECK (
        status <> 'REVOKED' OR revoked_at IS NOT NULL
    ),
    CONSTRAINT operating_legal_actor_mandate_activation_gate CHECK (
        status <> 'ACTIVE'
    )
);

CREATE INDEX operating_legal_actor_mandate_scope_idx
    ON registry.operating_legal_actor_mandate
    (tenant_id, operating_organisation_id, status, effective_from);
CREATE INDEX operating_legal_actor_mandate_actor_idx
    ON registry.operating_legal_actor_mandate
    (responsible_legal_entity_id, created_at);
CREATE INDEX operating_legal_actor_mandate_roles_idx
    ON registry.operating_legal_actor_mandate USING gin (roles);
CREATE INDEX operating_legal_actor_mandate_market_idx
    ON registry.operating_legal_actor_mandate USING gin (market_scope);

-- No DELETE: provenance must remain available even after revocation.
-- No change of actor, operating identity, scopes, evidence or effective window:
-- amendments require a new reviewed mandate with supersedes_mandate_id.
CREATE FUNCTION registry.operating_legal_actor_mandate_history_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'operating legal-actor mandates cannot be deleted'
            USING ERRCODE = 'check_violation';
    END IF;
    IF ROW(NEW.tenant_id, NEW.operating_organisation_id,
           NEW.responsible_legal_entity_id, NEW.roles, NEW.activity_scope,
           NEW.market_scope, NEW.capability_scope, NEW.authority_basis_reference,
           NEW.evidence_references, NEW.legal_actor_verification_reference,
           NEW.effective_from, NEW.effective_to, NEW.created_by, NEW.created_at,
           NEW.supersedes_mandate_id, NEW.provenance)
       IS DISTINCT FROM
       ROW(OLD.tenant_id, OLD.operating_organisation_id,
           OLD.responsible_legal_entity_id, OLD.roles, OLD.activity_scope,
           OLD.market_scope, OLD.capability_scope, OLD.authority_basis_reference,
           OLD.evidence_references, OLD.legal_actor_verification_reference,
           OLD.effective_from, OLD.effective_to, OLD.created_by, OLD.created_at,
           OLD.supersedes_mandate_id, OLD.provenance) THEN
        RAISE EXCEPTION 'operating legal-actor mandate identity, scope and evidence are immutable'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER operating_legal_actor_mandate_history_guard
    BEFORE UPDATE OR DELETE ON registry.operating_legal_actor_mandate
    FOR EACH ROW EXECUTE FUNCTION registry.operating_legal_actor_mandate_history_guard();

REVOKE ALL ON registry.operating_legal_actor_mandate FROM PUBLIC;
