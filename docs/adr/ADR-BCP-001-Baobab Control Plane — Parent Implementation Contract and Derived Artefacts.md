# Baobab Control Plane — Parent Implementation Contract and Derived Artefacts

**Parent Contract:** `BCP-IMPL-001`
**System:** Baobab Control Plane
**Repository:** `baobab-platform/baobab-cp`
**Implementation Language:** Go
**Authoritative Runtime Store:** PostgreSQL 17
**HTTP Contract:** OpenAPI 3.2.0
**Event Contract:** AsyncAPI 3.0.0
**Contract Authority:** `baobab-platform/shared`
**Status:** Proposed Normative Implementation Baseline
**Architecture Style:** Modular monolith control plane with explicit bounded contexts and independently evolvable adapters
**Scope:** Canonical identity, mappings, markets, estates, topology, capabilities, isolation, context resolution, audit and messaging

---

# Part I — Contract Hierarchy

## 1. Contract hierarchy

Baobab SHALL maintain the following hierarchy:

```text
Baobab Canonical Mapping Model
        │
        ▼
BCP-IMPL-001
Control Plane Physical Data Model
        │
        ├──────────────────────────────┐
        │                              │
        ▼                              ▼
BCP-DB-001                      BCP-GO-001
PostgreSQL Migration            Go Package and
Specification                   Interface Specification
        │
        └──────────────────┬───────────┘
                           ▼
                    BCP-API-001
                OpenAPI / AsyncAPI
                    Contract Suite
```

The parent contract defines semantics.

The child contracts define implementation.

No child contract may redefine the meaning of:

- Tenant;
- Legal Entity;
- CanonicalEntity;
- Mapping;
- MappingScope;
- Market;
- DigitalEstate;
- Engine;
- EngineInstance;
- Capability;
- CapabilityBinding;
- Context;
- IsolationProfile.

When an implementation requirement conflicts with the parent model, the implementation must be changed or a new architecture decision approved.

---

# Part II — Artefact 1: BCP-DB-001

# PostgreSQL 17 Migration Specification

## 2. Purpose

`BCP-DB-001` defines the complete initial database migration sequence for the Baobab Control Plane.

The sequence SHALL consist initially of:

```text
000001_extensions_and_schemas.sql
000002_canonical_registry.sql
000003_canonical_relationships.sql
000004_isolation_profiles.sql
000005_engine_topology.sql
000006_markets.sql
000007_digital_estates.sql
000008_capabilities.sql
000009_mapping_scopes.sql
000010_external_references.sql
000011_canonical_mappings.sql
000012_capability_bindings.sql
000013_context_snapshots.sql
000014_audit.sql
000015_messaging.sql
000016_idempotency_and_revisions.sql
000017_indexes_and_integrity.sql
000018_canonical_entity_versions.sql
```

These files become immutable after first production deployment.

---

# 3. Migration rules

Every migration SHALL:

- execute transactionally where PostgreSQL permits;
- use explicit schema qualification;
- avoid application-dependent data transformations unless explicitly documented;
- be reproducible against an empty PostgreSQL 17 database;
- be tested upgrading from the preceding release;
- be irreversible in production by default;
- contain no runtime secrets;
- contain no environment-specific tenant data;
- contain no business bootstrap data unless maintained separately as controlled seed data.

Production rollback SHALL normally mean:

```text
deploy compatible application
+
forward corrective migration
```

rather than destructive down migrations.

---

# 4. Naming conventions

Database identifiers SHALL use:

```text
snake_case
lowercase
singular table names where practical
```

Constraint suffixes:

```text
_pk      primary key
_fk      foreign key
_uq      unique constraint/index
_ck      check constraint
_excl    exclusion constraint
_idx     ordinary index
_gist    GiST index
```

Examples:

```text
mapping_mapping_canonical_idx
capability_binding_primary_excl
market_currency_market_fk
```

---

# 5. 000001 — Extensions and schemas

File:

```text
migrations/000001_extensions_and_schemas.sql
```

Responsibilities:

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SCHEMA IF NOT EXISTS registry;
CREATE SCHEMA IF NOT EXISTS mapping;
CREATE SCHEMA IF NOT EXISTS topology;
CREATE SCHEMA IF NOT EXISTS market;
CREATE SCHEMA IF NOT EXISTS estate;
CREATE SCHEMA IF NOT EXISTS capability;
CREATE SCHEMA IF NOT EXISTS policy;
CREATE SCHEMA IF NOT EXISTS audit;
CREATE SCHEMA IF NOT EXISTS messaging;
CREATE SCHEMA IF NOT EXISTS system;
```

`btree_gist` is mandatory because the platform combines scalar equality with temporal range overlap inside exclusion constraints.

PostgreSQL 17 explicitly supports exclusion constraints and stores them as first-class database constraints.

### 5.1 Migration journal

Create:

```sql
CREATE TABLE system.schema_metadata (
    key         text PRIMARY KEY,
    value       text NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
```

Initial values:

```text
database_contract = BCP-DB-001
database_contract_version = 1.0.0
canonical_model_version = 1.0.0
```

The migration framework itself may maintain its own version table separately.

---

# 6. 000002 — Canonical registry

Create:

```text
registry.canonical_entity
```

Full physical definition:

```sql
CREATE TABLE registry.canonical_entity (
    id                      uuid PRIMARY KEY,
    canonical_key           text NOT NULL,
    entity_type             text NOT NULL,
    subtype                 text,
    display_name            text NOT NULL,

    owner_tenant_id         uuid,
    owner_legal_entity_id   uuid,

    authority               text NOT NULL,
    classification          text NOT NULL,
    status                  text NOT NULL,

    schema_version          integer NOT NULL DEFAULT 1,

    effective_from          timestamptz NOT NULL,
    effective_to            timestamptz,

    metadata                jsonb NOT NULL DEFAULT '{}'::jsonb,

    version                 bigint NOT NULL DEFAULT 1,

    created_at              timestamptz NOT NULL,
    created_by              uuid,
    updated_at              timestamptz NOT NULL,
    updated_by              uuid,

    retired_at              timestamptz,
    retired_by              uuid,

    CONSTRAINT canonical_entity_key_uq
        UNIQUE(canonical_key),

    CONSTRAINT canonical_entity_schema_version_ck
        CHECK(schema_version > 0),

    CONSTRAINT canonical_entity_version_ck
        CHECK(version > 0),

    CONSTRAINT canonical_entity_period_ck
        CHECK(
            effective_to IS NULL
            OR effective_to > effective_from
        ),

    CONSTRAINT canonical_entity_status_ck
        CHECK(status IN (
            'DRAFT',
            'VALIDATED',
            'ACTIVE',
            'DEPRECATED',
            'SUSPENDED',
            'MIGRATING',
            'QUARANTINED',
            'RETIRED'
        )),

    CONSTRAINT canonical_entity_classification_ck
        CHECK(classification IN (
            'PUBLIC',
            'INTERNAL',
            'TENANT_CONFIDENTIAL',
            'RESTRICTED'
        ))
);
```

### 6.1 Canonical-key policy

`canonical_key` SHALL be globally unique within Control Plane v1.

Examples:

```text
tenant:nabhold
legal-entity:thamani-global
estate:thamani
market:kenya-b2b
product:green-coffee:ethiopia-guji
```

A future namespace design MUST be introduced explicitly rather than weakening uniqueness.

---

# 7. 000003 — Canonical relationships

Create:

```text
registry.canonical_relationship
```

```sql
CREATE TABLE registry.canonical_relationship (
    id                       uuid PRIMARY KEY,

    source_entity_id         uuid NOT NULL,
    target_entity_id         uuid NOT NULL,

    relationship_type        text NOT NULL,
    direction                text NOT NULL,

    effective_from           timestamptz NOT NULL,
    effective_to             timestamptz,

    valid_period             tstzrange
        GENERATED ALWAYS AS (
            tstzrange(effective_from, effective_to, '[)')
        ) STORED,

    status                   text NOT NULL,

    metadata                 jsonb NOT NULL DEFAULT '{}'::jsonb,

    version                  bigint NOT NULL DEFAULT 1,

    created_at               timestamptz NOT NULL,
    created_by               uuid,
    updated_at               timestamptz NOT NULL,
    updated_by               uuid,

    CONSTRAINT canonical_relationship_source_fk
        FOREIGN KEY(source_entity_id)
        REFERENCES registry.canonical_entity(id),

    CONSTRAINT canonical_relationship_target_fk
        FOREIGN KEY(target_entity_id)
        REFERENCES registry.canonical_entity(id),

    CONSTRAINT canonical_relationship_not_self_ck
        CHECK(source_entity_id <> target_entity_id),

    CONSTRAINT canonical_relationship_period_ck
        CHECK(
            effective_to IS NULL
            OR effective_to > effective_from
        )
);
```

Stored generated columns are supported by PostgreSQL 17 and are appropriate for immutable range calculations such as this.

---

# 8. 000004 — Isolation profiles

Create:

```text
policy.isolation_profile
```

```sql
CREATE TABLE policy.isolation_profile (
    id                        uuid PRIMARY KEY,

    profile_key               text NOT NULL,
    name                      text NOT NULL,
    classification            text NOT NULL,

    compute_isolation         text NOT NULL,
    database_isolation        text NOT NULL,
    storage_isolation         text NOT NULL,
    cache_isolation           text NOT NULL,
    queue_isolation           text NOT NULL,
    network_isolation         text NOT NULL,
    secret_isolation          text NOT NULL,
    encryption_isolation      text NOT NULL,
    observability_isolation   text NOT NULL,
    backup_isolation          text NOT NULL,
    deployment_isolation      text NOT NULL,

    data_residency            jsonb NOT NULL DEFAULT '{}'::jsonb,

    cross_tenant_policy       text NOT NULL,

    requirements              jsonb NOT NULL DEFAULT '{}'::jsonb,
    controls                  jsonb NOT NULL DEFAULT '{}'::jsonb,

    status                    text NOT NULL,

    profile_version           integer NOT NULL,

    created_at                timestamptz NOT NULL,
    created_by                uuid,
    updated_at                timestamptz NOT NULL,
    updated_by                uuid,

    CONSTRAINT isolation_profile_key_version_uq
        UNIQUE(profile_key, profile_version),

    CONSTRAINT isolation_profile_version_ck
        CHECK(profile_version > 0)
);
```

Isolation profile rows SHALL be versioned and SHOULD not be mutated after activation except for lifecycle metadata.

---

# 9. 000005 — Engine topology

Create:

```text
topology.engine
topology.engine_instance
topology.engine_instance_contract
```

### 9.1 Engine

```sql
CREATE TABLE topology.engine (
    id                          uuid PRIMARY KEY,
    engine_key                  text NOT NULL UNIQUE,
    name                        text NOT NULL,
    engine_type                 text NOT NULL,

    vendor                      text,
    technology                  text,
    distribution                text,
    ownership                   text NOT NULL,

    support_status              text NOT NULL,

    minimum_supported_version   text,
    recommended_version         text,

    metadata                    jsonb NOT NULL DEFAULT '{}'::jsonb,

    version                     bigint NOT NULL DEFAULT 1,

    created_at                  timestamptz NOT NULL,
    created_by                  uuid,
    updated_at                  timestamptz NOT NULL,
    updated_by                  uuid
);
```

Examples:

```text
payload
medusa
idempiere
```

### 9.2 Engine instance

```sql
CREATE TABLE topology.engine_instance (
    id                       uuid PRIMARY KEY,

    engine_id                uuid NOT NULL,

    instance_key             text NOT NULL UNIQUE,
    name                     text NOT NULL,

    environment              text NOT NULL,
    deployment_region        text,

    endpoint                 text,
    internal_endpoint        text,

    software_version         text NOT NULL,
    tenant_strategy          text NOT NULL,

    isolation_profile_id     uuid NOT NULL,

    data_residency_profile   jsonb NOT NULL DEFAULT '{}'::jsonb,

    lifecycle_status         text NOT NULL,

    credentials_reference    text,
    configuration_reference  text,
    observability_reference  text,

    started_at               timestamptz,
    deprecated_at            timestamptz,
    retire_at                timestamptz,

    metadata                 jsonb NOT NULL DEFAULT '{}'::jsonb,

    version                  bigint NOT NULL DEFAULT 1,

    created_at               timestamptz NOT NULL,
    created_by               uuid,
    updated_at               timestamptz NOT NULL,
    updated_by               uuid,

    CONSTRAINT engine_instance_engine_fk
        FOREIGN KEY(engine_id)
        REFERENCES topology.engine(id),

    CONSTRAINT engine_instance_isolation_fk
        FOREIGN KEY(isolation_profile_id)
        REFERENCES policy.isolation_profile(id)
);
```

### 9.3 Engine contract compatibility

```sql
CREATE TABLE topology.engine_instance_contract (
    engine_instance_id  uuid NOT NULL,
    contract_name       text NOT NULL,
    contract_version    text NOT NULL,
    status              text NOT NULL,

    PRIMARY KEY(
        engine_instance_id,
        contract_name,
        contract_version
    ),

    FOREIGN KEY(engine_instance_id)
        REFERENCES topology.engine_instance(id)
);
```

---

# 10. Engine health separation

Do not place frequently changing health state on `engine_instance`.

Create:

```text
topology.engine_instance_health
```

```sql
CREATE TABLE topology.engine_instance_health (
    engine_instance_id      uuid PRIMARY KEY,
    health_status           text NOT NULL,
    last_seen_at            timestamptz,
    checked_at              timestamptz NOT NULL,

    latency_ms              integer,
    details                 jsonb NOT NULL DEFAULT '{}'::jsonb,

    FOREIGN KEY(engine_instance_id)
        REFERENCES topology.engine_instance(id)
        ON DELETE CASCADE
);
```

Lifecycle is registry truth.

Health is ephemeral operational truth.

---

# 11. 000006 — Markets

Create:

```text
market.market
market.market_country
market.market_currency
market.market_locale
```

### 11.1 Market

```sql
CREATE TABLE market.market (
    id                       uuid PRIMARY KEY,

    canonical_key            text NOT NULL UNIQUE,
    name                     text NOT NULL,
    market_type              text NOT NULL,

    owner_tenant_id          uuid NOT NULL,
    legal_entity_id          uuid,
    operating_region_id      uuid,

    parent_market_id         uuid,

    default_country_code     char(2),
    default_currency_code    char(3),
    default_locale           text,
    timezone                 text,

    tax_profile_id           uuid,
    pricing_policy_id        uuid,
    catalogue_policy_id      uuid,
    payment_policy_id        uuid,
    fulfilment_policy_id     uuid,
    regulatory_profile_id    uuid,

    status                   text NOT NULL,

    effective_from           timestamptz NOT NULL,
    effective_to             timestamptz,

    metadata                 jsonb NOT NULL DEFAULT '{}'::jsonb,

    version                  bigint NOT NULL DEFAULT 1,

    created_at               timestamptz NOT NULL,
    created_by               uuid,
    updated_at               timestamptz NOT NULL,
    updated_by               uuid,

    FOREIGN KEY(parent_market_id)
        REFERENCES market.market(id),

    CHECK(
        effective_to IS NULL
        OR effective_to > effective_from
    )
);
```

### 11.2 Market countries

```sql
CREATE TABLE market.market_country (
    market_id       uuid NOT NULL,
    country_code    char(2) NOT NULL,
    is_default      boolean NOT NULL DEFAULT false,

    PRIMARY KEY(market_id, country_code),

    FOREIGN KEY(market_id)
        REFERENCES market.market(id)
);
```

At most one default country:

```sql
CREATE UNIQUE INDEX market_country_default_uq
ON market.market_country(market_id)
WHERE is_default;
```

### 11.3 Market currencies

```sql
CREATE TABLE market.market_currency (
    market_id       uuid NOT NULL,
    currency_code   char(3) NOT NULL,
    currency_role   text NOT NULL,
    is_default      boolean NOT NULL DEFAULT false,

    PRIMARY KEY(
        market_id,
        currency_code,
        currency_role
    ),

    FOREIGN KEY(market_id)
        REFERENCES market.market(id),

    CHECK(currency_role IN (
        'PRESENTATION',
        'TRANSACTION',
        'SETTLEMENT',
        'ACCOUNTING',
        'REPORTING'
    ))
);
```

Unique default by role:

```sql
CREATE UNIQUE INDEX market_currency_default_role_uq
ON market.market_currency(
    market_id,
    currency_role
)
WHERE is_default;
```

### 11.4 Market locales

```sql
CREATE TABLE market.market_locale (
    market_id       uuid NOT NULL,
    locale          text NOT NULL,
    is_default      boolean NOT NULL DEFAULT false,
    fallback_locale text,

    PRIMARY KEY(market_id, locale),

    FOREIGN KEY(market_id)
        REFERENCES market.market(id)
);
```

---

# 12. 000007 — Digital estates

Create:

```text
estate.digital_estate
estate.digital_property
estate.estate_market
```

### 12.1 Digital estate

```sql
CREATE TABLE estate.digital_estate (
    id                      uuid PRIMARY KEY,
    canonical_key           text NOT NULL UNIQUE,
    name                    text NOT NULL,

    owner_tenant_id         uuid NOT NULL,
    owner_legal_entity_id   uuid,

    brand_entity_id         uuid,

    estate_type             text NOT NULL,
    parent_estate_id        uuid,

    default_market_id       uuid,
    default_locale          text,
    default_currency_code   char(3),

    status                  text NOT NULL,

    metadata                jsonb NOT NULL DEFAULT '{}'::jsonb,

    version                 bigint NOT NULL DEFAULT 1,

    created_at              timestamptz NOT NULL,
    created_by              uuid,
    updated_at              timestamptz NOT NULL,
    updated_by              uuid,

    FOREIGN KEY(brand_entity_id)
        REFERENCES registry.canonical_entity(id),

    FOREIGN KEY(parent_estate_id)
        REFERENCES estate.digital_estate(id),

    FOREIGN KEY(default_market_id)
        REFERENCES market.market(id)
);
```

### 12.2 Digital property

```sql
CREATE TABLE estate.digital_property (
    id                    uuid PRIMARY KEY,

    digital_estate_id     uuid NOT NULL,

    property_type         text NOT NULL,

    hostname              text,
    application_id        text,
    uri                   text,

    default_market_id     uuid,

    status                text NOT NULL,

    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,

    version               bigint NOT NULL DEFAULT 1,

    created_at            timestamptz NOT NULL,
    created_by            uuid,
    updated_at            timestamptz NOT NULL,
    updated_by            uuid,

    FOREIGN KEY(digital_estate_id)
        REFERENCES estate.digital_estate(id),

    FOREIGN KEY(default_market_id)
        REFERENCES market.market(id),

    CHECK(property_type IN (
        'DOMAIN',
        'SUBDOMAIN',
        'WEB_APP',
        'MOBILE_APP',
        'PARTNER_PORTAL',
        'API',
        'INTERNAL'
    ))
);
```

Hostname uniqueness:

```sql
CREATE UNIQUE INDEX digital_property_active_hostname_uq
ON estate.digital_property(lower(hostname))
WHERE hostname IS NOT NULL
AND status = 'ACTIVE';
```

### 12.3 Estate-market binding

```sql
CREATE TABLE estate.estate_market (
    digital_estate_id   uuid NOT NULL,
    market_id           uuid NOT NULL,

    priority            integer NOT NULL DEFAULT 100,
    status              text NOT NULL,

    effective_from      timestamptz NOT NULL,
    effective_to        timestamptz,

    PRIMARY KEY(
        digital_estate_id,
        market_id,
        effective_from
    ),

    FOREIGN KEY(digital_estate_id)
        REFERENCES estate.digital_estate(id),

    FOREIGN KEY(market_id)
        REFERENCES market.market(id),

    CHECK(
        effective_to IS NULL
        OR effective_to > effective_from
    )
);
```

---

# 13. 000008 — Capabilities

Create:

```text
capability.capability
capability.capability_dependency
capability.engine_capability
```

### 13.1 Capability

```sql
CREATE TABLE capability.capability (
    id                  uuid PRIMARY KEY,

    capability_key      text NOT NULL UNIQUE,
    name                text NOT NULL,
    domain              text NOT NULL,
    description         text,

    contract_name       text NOT NULL,
    contract_version    text NOT NULL,

    criticality         text NOT NULL,
    status              text NOT NULL,

    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,

    version             bigint NOT NULL DEFAULT 1,

    created_at          timestamptz NOT NULL,
    created_by          uuid,
    updated_at          timestamptz NOT NULL,
    updated_by          uuid
);
```

### 13.2 Dependency

```sql
CREATE TABLE capability.capability_dependency (
    capability_id            uuid NOT NULL,
    depends_on_capability_id uuid NOT NULL,

    dependency_type          text NOT NULL,

    PRIMARY KEY(
        capability_id,
        depends_on_capability_id
    ),

    FOREIGN KEY(capability_id)
        REFERENCES capability.capability(id),

    FOREIGN KEY(depends_on_capability_id)
        REFERENCES capability.capability(id),

    CHECK(capability_id <> depends_on_capability_id)
);
```

### 13.3 Engine capability

```sql
CREATE TABLE capability.engine_capability (
    engine_id         uuid NOT NULL,
    capability_id     uuid NOT NULL,

    minimum_version   text,
    maximum_version   text,

    status            text NOT NULL,

    PRIMARY KEY(engine_id, capability_id),

    FOREIGN KEY(engine_id)
        REFERENCES topology.engine(id),

    FOREIGN KEY(capability_id)
        REFERENCES capability.capability(id)
);
```

---

# 14. 000009 — Mapping scopes

Create:

```text
mapping.mapping_scope
```

```sql
CREATE TABLE mapping.mapping_scope (
    id                    uuid PRIMARY KEY,

    tenant_id             uuid,
    legal_entity_id       uuid,
    organisation_id       uuid,
    business_unit_id      uuid,

    operating_region_id   uuid,
    geographic_region_id  uuid,

    market_id             uuid,
    country_code          char(2),

    digital_estate_id     uuid,
    digital_property_id   uuid,
    channel_id            uuid,

    currency_code         char(3),
    locale                text,

    catalogue_id          uuid,
    customer_segment_id   uuid,

    engine_id             uuid,
    engine_instance_id    uuid,

    environment           text,
    deployment_region     text,

    scope_hash            text NOT NULL UNIQUE,

    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_at            timestamptz NOT NULL,
    created_by            uuid,

    FOREIGN KEY(market_id)
        REFERENCES market.market(id),

    FOREIGN KEY(digital_estate_id)
        REFERENCES estate.digital_estate(id),

    FOREIGN KEY(digital_property_id)
        REFERENCES estate.digital_property(id),

    FOREIGN KEY(engine_id)
        REFERENCES topology.engine(id),

    FOREIGN KEY(engine_instance_id)
        REFERENCES topology.engine_instance(id)
);
```

`MappingScope` SHALL be immutable after creation.

Application-level canonical scope serialization MUST:

1. include only populated dimensions;
2. use deterministic key ordering;
3. canonicalise UUIDs to lowercase textual form;
4. canonicalise country/currency codes uppercase;
5. canonicalise locale according to Baobab locale rules;
6. hash UTF-8 canonical representation using SHA-256.

---

# 15. 000010 — External references

Create:

```text
mapping.external_reference
```

```sql
CREATE TABLE mapping.external_reference (
    id                    uuid PRIMARY KEY,

    engine_id             uuid,
    engine_instance_id    uuid,

    system_namespace      text NOT NULL,
    environment           text NOT NULL,

    native_entity_type    text NOT NULL,
    native_id             text NOT NULL,
    native_key            text,
    native_uri            text,

    source_authority      text,
    fingerprint           text,

    status                text NOT NULL,

    first_seen_at         timestamptz NOT NULL,
    last_verified_at      timestamptz,

    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,

    version               bigint NOT NULL DEFAULT 1,

    created_at            timestamptz NOT NULL,
    created_by            uuid,
    updated_at            timestamptz NOT NULL,
    updated_by            uuid,

    FOREIGN KEY(engine_id)
        REFERENCES topology.engine(id),

    FOREIGN KEY(engine_instance_id)
        REFERENCES topology.engine_instance(id)
);
```

Uniqueness:

```sql
CREATE UNIQUE INDEX external_reference_engine_native_uq
ON mapping.external_reference(
    engine_instance_id,
    native_entity_type,
    native_id
)
WHERE engine_instance_id IS NOT NULL
AND status <> 'RETIRED';

CREATE UNIQUE INDEX external_reference_namespace_native_uq
ON mapping.external_reference(
    system_namespace,
    environment,
    native_entity_type,
    native_id
)
WHERE engine_instance_id IS NULL
AND status <> 'RETIRED';
```

---

# 16. 000011 — Canonical mappings

Create:

```text
mapping.mapping_type_definition
mapping.mapping
```

### 16.1 Mapping type definition

```sql
CREATE TABLE mapping.mapping_type_definition (
    mapping_type       text PRIMARY KEY,

    resolution_mode    text NOT NULL,
    description        text NOT NULL,

    requires_approval  boolean NOT NULL DEFAULT false,
    cross_tenant       boolean NOT NULL DEFAULT false,

    status             text NOT NULL,

    CHECK(resolution_mode IN (
        'SINGLE',
        'MULTI',
        'RELATIONSHIP'
    ))
);
```

Initial values become controlled seed data:

```text
IDENTITY
REPRESENTATION
ORGANISATIONAL
CONTENT
COMMERCE
ERP
CATALOGUE
PRICING
TAX
WAREHOUSE
FULFILMENT
PAYMENT
DOMAIN
LOCALE
CURRENCY
CHANNEL
CAPABILITY
INTEGRATION
MIGRATION
ALIAS
SUCCESSOR
```

### 16.2 Mapping

```sql
CREATE TABLE mapping.mapping (
    id                           uuid PRIMARY KEY,

    mapping_type                 text NOT NULL,
    resolution_mode              text NOT NULL,

    canonical_entity_id          uuid NOT NULL,

    external_reference_id        uuid,
    target_canonical_entity_id   uuid,

    scope_id                     uuid NOT NULL,

    direction                    text NOT NULL,
    cardinality                  text NOT NULL,

    authority                    text NOT NULL,
    confidence                   text NOT NULL,

    resolution_priority          integer NOT NULL DEFAULT 100,

    status                       text NOT NULL,

    effective_from               timestamptz NOT NULL,
    effective_to                 timestamptz,

    valid_period                 tstzrange
        GENERATED ALWAYS AS (
            tstzrange(effective_from, effective_to, '[)')
        ) STORED,

    supersedes_mapping_id        uuid,

    mapping_version              integer NOT NULL DEFAULT 1,

    metadata                     jsonb NOT NULL DEFAULT '{}'::jsonb,

    version                      bigint NOT NULL DEFAULT 1,

    created_at                   timestamptz NOT NULL,
    created_by                   uuid,

    updated_at                   timestamptz NOT NULL,
    updated_by                   uuid,

    approved_at                  timestamptz,
    approved_by                  uuid,

    retired_at                   timestamptz,
    retired_by                   uuid,

    FOREIGN KEY(mapping_type)
        REFERENCES mapping.mapping_type_definition(mapping_type),

    FOREIGN KEY(canonical_entity_id)
        REFERENCES registry.canonical_entity(id),

    FOREIGN KEY(external_reference_id)
        REFERENCES mapping.external_reference(id),

    FOREIGN KEY(target_canonical_entity_id)
        REFERENCES registry.canonical_entity(id),

    FOREIGN KEY(scope_id)
        REFERENCES mapping.mapping_scope(id),

    FOREIGN KEY(supersedes_mapping_id)
        REFERENCES mapping.mapping(id),

    CHECK(
        (
            external_reference_id IS NOT NULL
        )::integer
        +
        (
            target_canonical_entity_id IS NOT NULL
        )::integer
        = 1
    ),

    CHECK(
        effective_to IS NULL
        OR effective_to > effective_from
    ),

    CHECK(resolution_mode IN (
        'SINGLE',
        'MULTI',
        'RELATIONSHIP'
    )),

    CHECK(direction IN (
        'BIDIRECTIONAL',
        'CANONICAL_TO_EXTERNAL',
        'EXTERNAL_TO_CANONICAL',
        'SOURCE_TO_TARGET'
    )),

    CHECK(cardinality IN (
        'ONE_TO_ONE',
        'ONE_TO_MANY',
        'MANY_TO_ONE',
        'MANY_TO_MANY'
    )),

    CHECK(confidence IN (
        'CONFIRMED',
        'PROBABLE',
        'CANDIDATE',
        'REJECTED'
    ))
);
```

### 16.3 Authoritative temporal exclusion

```sql
ALTER TABLE mapping.mapping
ADD CONSTRAINT mapping_single_authoritative_excl
EXCLUDE USING gist (
    canonical_entity_id WITH =,
    mapping_type WITH =,
    scope_id WITH =,
    valid_period WITH &&
)
WHERE (
    resolution_mode = 'SINGLE'
    AND status IN ('ACTIVE', 'DEPRECATED')
    AND confidence = 'CONFIRMED'
);
```

This is a core architectural invariant.

The application SHALL pre-check for readable API errors, but PostgreSQL remains the final authority.

---

# 17. 000012 — Capability bindings

Create:

```text
capability.capability_binding
```

```sql
CREATE TABLE capability.capability_binding (
    id                    uuid PRIMARY KEY,

    capability_id         uuid NOT NULL,
    engine_instance_id    uuid NOT NULL,
    scope_id              uuid NOT NULL,

    binding_mode          text NOT NULL,
    priority              integer NOT NULL DEFAULT 100,

    status                text NOT NULL,

    contract_version      text NOT NULL,

    effective_from        timestamptz NOT NULL,
    effective_to          timestamptz,

    valid_period          tstzrange
        GENERATED ALWAYS AS (
            tstzrange(effective_from, effective_to, '[)')
        ) STORED,

    fallback_binding_id   uuid,

    configuration         jsonb NOT NULL DEFAULT '{}'::jsonb,

    version               bigint NOT NULL DEFAULT 1,

    created_at            timestamptz NOT NULL,
    created_by            uuid,
    updated_at            timestamptz NOT NULL,
    updated_by            uuid,

    approved_at           timestamptz,
    approved_by           uuid,

    FOREIGN KEY(capability_id)
        REFERENCES capability.capability(id),

    FOREIGN KEY(engine_instance_id)
        REFERENCES topology.engine_instance(id),

    FOREIGN KEY(scope_id)
        REFERENCES mapping.mapping_scope(id),

    FOREIGN KEY(fallback_binding_id)
        REFERENCES capability.capability_binding(id),

    CHECK(
        effective_to IS NULL
        OR effective_to > effective_from
    ),

    CHECK(binding_mode IN (
        'PRIMARY',
        'SECONDARY',
        'FALLBACK',
        'READ_ONLY',
        'MIGRATION_SOURCE',
        'MIGRATION_TARGET',
        'SHADOW'
    ))
);
```

Temporal primary exclusion:

```sql
ALTER TABLE capability.capability_binding
ADD CONSTRAINT capability_binding_primary_excl
EXCLUDE USING gist (
    capability_id WITH =,
    scope_id WITH =,
    valid_period WITH &&
)
WHERE (
    status = 'ACTIVE'
    AND binding_mode = 'PRIMARY'
);
```

---

# 18. 000013 — Context snapshots

Create:

```text
audit.context_snapshot
```

```sql
CREATE TABLE audit.context_snapshot (
    id                  uuid PRIMARY KEY,

    context_version     text NOT NULL,
    context_hash        text NOT NULL UNIQUE,

    tenant_id           uuid,
    legal_entity_id     uuid,
    market_id           uuid,
    digital_estate_id   uuid,

    context_data        jsonb NOT NULL,

    resolved_at         timestamptz NOT NULL,

    created_at          timestamptz NOT NULL
);
```

This is not request logging.

Only contexts whose later reconstruction matters SHOULD be persisted.

---

# 19. 000014 — Audit

Create:

```text
audit.audit_record
```

```sql
CREATE TABLE audit.audit_record (
    id                   uuid PRIMARY KEY,

    tenant_id            uuid,
    actor_id             uuid,

    action               text NOT NULL,

    resource_type        text NOT NULL,
    resource_id          uuid NOT NULL,

    previous_version     jsonb,
    resulting_version    jsonb,

    reason               text,

    request_id           uuid,
    correlation_id       uuid,

    source_ip_hash       text,

    occurred_at          timestamptz NOT NULL,

    metadata             jsonb NOT NULL DEFAULT '{}'::jsonb
);
```

No application-level UPDATE or DELETE operation SHALL be exposed for audit records.

---

# 20. 000015 — Messaging

Create:

```text
messaging.outbox
messaging.inbox
```

### 20.1 Outbox

```sql
CREATE TABLE messaging.outbox (
    id                 uuid PRIMARY KEY,

    aggregate_type     text NOT NULL,
    aggregate_id       uuid NOT NULL,
    aggregate_version  bigint NOT NULL,

    event_type         text NOT NULL,

    tenant_id          uuid,

    correlation_id     uuid,
    causation_id       uuid,

    payload            jsonb NOT NULL,
    headers            jsonb NOT NULL DEFAULT '{}'::jsonb,

    occurred_at        timestamptz NOT NULL,

    published_at       timestamptz,
    publish_attempts   integer NOT NULL DEFAULT 0,
    next_attempt_at    timestamptz,

    last_error         text,

    created_at         timestamptz NOT NULL
);
```

### 20.2 Inbox

```sql
CREATE TABLE messaging.inbox (
    event_id             uuid PRIMARY KEY,

    source               text NOT NULL,
    event_type           text NOT NULL,

    received_at          timestamptz NOT NULL,
    processed_at         timestamptz,

    processing_status    text NOT NULL,

    payload_hash         text NOT NULL,

    processing_attempts  integer NOT NULL DEFAULT 0,
    last_error           text
);
```

Delivery semantics SHALL be:

```text
transport:
    at least once

consumer processing:
    idempotent

business effect:
    effectively once where supported
```

The architecture SHALL never claim mathematically guaranteed distributed exactly-once delivery.

---

# 21. 000016 — Idempotency and registry revisions

Create:

```text
system.idempotency_record
system.registry_revision
```

### 21.1 Idempotency

```sql
CREATE TABLE system.idempotency_record (
    id                  uuid PRIMARY KEY,

    idempotency_key     text NOT NULL,
    tenant_id           uuid,

    operation           text NOT NULL,
    request_hash        text NOT NULL,

    response_status     integer,
    response_headers    jsonb,
    response_body       jsonb,
    resource_id         uuid,

    state               text NOT NULL,

    created_at          timestamptz NOT NULL,
    completed_at        timestamptz,
    expires_at          timestamptz NOT NULL
);
```

Unique key:

```sql
CREATE UNIQUE INDEX idempotency_operation_key_uq
ON system.idempotency_record(
    coalesce(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid),
    operation,
    idempotency_key
);
```

### 21.2 Registry revision

```sql
CREATE TABLE system.registry_revision (
    registry_name       text PRIMARY KEY,
    revision            bigint NOT NULL DEFAULT 0,
    updated_at          timestamptz NOT NULL
);
```

Initial names:

```text
canonical
mapping
market
estate
topology
capability
isolation
```

Revision increments SHALL happen transactionally with material mutations.

---

# 22. 000017 — Indexes and integrity

This migration establishes indexes driven by actual resolver paths.

### Canonical

```sql
CREATE INDEX canonical_entity_type_status_idx
ON registry.canonical_entity(entity_type, status);

CREATE INDEX canonical_entity_tenant_type_idx
ON registry.canonical_entity(owner_tenant_id, entity_type)
WHERE owner_tenant_id IS NOT NULL;
```

### Relationship

```sql
CREATE INDEX canonical_relationship_source_idx
ON registry.canonical_relationship(
    source_entity_id,
    relationship_type,
    status
);

CREATE INDEX canonical_relationship_target_idx
ON registry.canonical_relationship(
    target_entity_id,
    relationship_type,
    status
);

CREATE INDEX canonical_relationship_period_gist
ON registry.canonical_relationship
USING gist(valid_period);
```

### Mapping scopes

```sql
CREATE INDEX mapping_scope_tenant_market_idx
ON mapping.mapping_scope(tenant_id, market_id);

CREATE INDEX mapping_scope_estate_idx
ON mapping.mapping_scope(digital_estate_id);

CREATE INDEX mapping_scope_engine_idx
ON mapping.mapping_scope(engine_id, engine_instance_id);

CREATE INDEX mapping_scope_geography_idx
ON mapping.mapping_scope(
    operating_region_id,
    country_code
);

CREATE INDEX mapping_scope_currency_locale_idx
ON mapping.mapping_scope(
    currency_code,
    locale
);
```

### Mapping

```sql
CREATE INDEX mapping_canonical_lookup_idx
ON mapping.mapping(
    canonical_entity_id,
    mapping_type,
    status
);

CREATE INDEX mapping_external_lookup_idx
ON mapping.mapping(
    external_reference_id,
    status
)
WHERE external_reference_id IS NOT NULL;

CREATE INDEX mapping_target_canonical_idx
ON mapping.mapping(
    target_canonical_entity_id,
    status
)
WHERE target_canonical_entity_id IS NOT NULL;

CREATE INDEX mapping_scope_lookup_idx
ON mapping.mapping(
    scope_id,
    mapping_type,
    status
);

CREATE INDEX mapping_valid_period_gist
ON mapping.mapping
USING gist(valid_period);
```

### Capability binding

```sql
CREATE INDEX capability_binding_lookup_idx
ON capability.capability_binding(
    capability_id,
    status,
    binding_mode
);

CREATE INDEX capability_binding_scope_idx
ON capability.capability_binding(scope_id);

CREATE INDEX capability_binding_period_gist
ON capability.capability_binding
USING gist(valid_period);
```

### Messaging

```sql
CREATE INDEX outbox_pending_idx
ON messaging.outbox(
    coalesce(next_attempt_at, occurred_at),
    occurred_at
)
WHERE published_at IS NULL;

CREATE INDEX inbox_processing_idx
ON messaging.inbox(
    processing_status,
    received_at
);
```

### Audit

```sql
CREATE INDEX audit_resource_idx
ON audit.audit_record(
    resource_type,
    resource_id,
    occurred_at DESC
);

CREATE INDEX audit_tenant_time_idx
ON audit.audit_record(
    tenant_id,
    occurred_at DESC
)
WHERE tenant_id IS NOT NULL;

CREATE INDEX audit_correlation_idx
ON audit.audit_record(correlation_id)
WHERE correlation_id IS NOT NULL;
```

---

# 23. Database privileges

Provision roles:

```text
baobab_cp_owner
baobab_cp_migrator
baobab_cp_runtime
baobab_cp_readonly
```

Rules:

```text
owner:
    owns schemas

migrator:
    DDL privileges
    used only during deployment

runtime:
    SELECT/INSERT/UPDATE required application tables
    no schema ownership
    no extension creation
    no arbitrary DDL

readonly:
    SELECT appropriate operational tables
```

No application runtime shall execute using a database superuser.

---

# 24. Database acceptance tests

Database CI SHALL directly test:

```text
duplicate canonical keys fail
invalid temporal periods fail
mapping XOR violation fails
SINGLE mapping overlap fails
adjacent ranges succeed
MULTI mappings may overlap
PRIMARY capability binding overlap fails
SHADOW and PRIMARY may coexist
active hostname collision fails
two default currencies for same role fail
foreign-key orphans fail
mapping scope hash duplicates fail
```

The database tests must run against PostgreSQL 17, not an SQLite substitute.

---

# Part III — Artefact 2: BCP-GO-001

# Go Package and Interface Specification

## 25. Go architecture objective

`baobab-platform/baobab-cp` SHALL begin as a **modular monolith**, not as a collection of premature microservices.

Boundaries SHALL nevertheless be strict enough that individual modules may later be extracted where operational evidence justifies it.

Go's official guidance recommends `internal/` for implementation packages in server projects and `cmd/` for commands, which fits this architecture directly.

---

# 26. Repository structure

```text
baobab-platform/baobab-cp/
│
├── cmd/
│   ├── api/
│   │   └── main.go
│   ├── worker/
│   │   └── main.go
│   ├── migrate/
│   │   └── main.go
│   └── reconcile/
│       └── main.go
│
├── internal/
│   ├── canonical/
│   ├── mapping/
│   ├── market/
│   ├── estate/
│   ├── topology/
│   ├── capability/
│   ├── isolation/
│   ├── resolver/
│   ├── contextmodel/
│   ├── audit/
│   ├── messaging/
│   ├── idempotency/
│   ├── authn/
│   ├── authz/
│   ├── contracts/
│   ├── observability/
│   ├── persistence/
│   └── platform/
│
├── migrations/
│
├── api/
│   └── openapi/
│       ├── openapi.yaml
│       ├── paths/
│       └── schemas/
│
├── events/
│   └── asyncapi/
│       ├── asyncapi.yaml
│       ├── channels/
│       └── messages/
│
├── contracts/
│   └── README.md
│
├── test/
│   ├── architecture/
│   ├── contract/
│   ├── integration/
│   ├── resolver/
│   └── migrations/
│
├── go.mod
├── go.sum
├── Dockerfile
└── README.md
```

The repository SHALL contain one primary Go module unless a future ADR justifies additional modules. Go modules are the language's standard dependency/versioning unit.

---

# 27. Internal bounded-context convention

Every major domain package SHOULD follow:

```text
internal/mapping/
├── domain.go
├── types.go
├── errors.go
├── repository.go
├── service.go
├── commands.go
├── queries.go
├── events.go
├── validation.go
└── postgres/
    ├── repository.go
    ├── queries.sql
    └── row.go
```

HTTP handlers belong outside domain logic:

```text
internal/mapping/http/
```

if module-local routing is preferred.

Alternatively central HTTP composition may exist under:

```text
internal/platform/httpapi/
```

but business behaviour must remain in application services.

---

# 28. Dependency direction

Allowed:

```text
transport
   ↓
application
   ↓
domain
   ↑
repository interfaces

infrastructure
   ↓ implements
repository interfaces
```

Forbidden:

```text
domain → net/http
domain → PostgreSQL driver
domain → Medusa client
domain → Payload client
domain → iDempiere client
```

---

# 29. Common value types

Define explicit domain value types:

```go
type LifecycleStatus string
type Classification string
type MappingType string
type MappingDirection string
type MappingCardinality string
type MappingConfidence string
type ResolutionMode string
type BindingMode string
type CapabilityCriticality string
type Environment string
type CountryCode string
type CurrencyCode string
type Locale string
type CanonicalKey string
```

These types SHALL implement validation.

Avoid transporting unvalidated string constants throughout the service.

---

# 30. Identifier type

Use one UUID implementation consistently.

Domain identifiers SHOULD be aliases or wrappers where type safety adds value:

```go
type CanonicalEntityID uuid.UUID
type MappingID uuid.UUID
type MarketID uuid.UUID
type EngineID uuid.UUID
type EngineInstanceID uuid.UUID
```

For high-frequency code where wrappers create excessive ceremony, plain `uuid.UUID` is acceptable, but the repository SHALL not mix several UUID libraries.

UUID generation:

```go
type IDGenerator interface {
    New() uuid.UUID
}
```

Production implementation SHALL produce UUIDv7.

Tests SHALL use deterministic generators.

---

# 31. Clock abstraction

Time-dependent lifecycle and temporal tests require:

```go
type Clock interface {
    Now() time.Time
}
```

Production:

```text
SystemClock
```

Tests:

```text
FixedClock
```

Do not scatter `time.Now()` throughout domain code.

---

# 32. Canonical package

Core type:

```go
type CanonicalEntity struct {
    ID                 uuid.UUID
    CanonicalKey       CanonicalKey
    EntityType         string
    Subtype            string
    DisplayName        string

    OwnerTenantID      *uuid.UUID
    OwnerLegalEntityID *uuid.UUID

    Authority          string
    Classification     Classification
    Status             LifecycleStatus

    SchemaVersion      int

    EffectiveFrom      time.Time
    EffectiveTo        *time.Time

    Metadata           map[string]any

    Version            int64

    CreatedAt          time.Time
    CreatedBy          *uuid.UUID
    UpdatedAt          time.Time
    UpdatedBy          *uuid.UUID

    RetiredAt          *time.Time
    RetiredBy          *uuid.UUID
}
```

Repository:

```go
type Repository interface {
    Create(
        context.Context,
        *CanonicalEntity,
    ) error

    GetByID(
        context.Context,
        uuid.UUID,
    ) (*CanonicalEntity, error)

    GetByKey(
        context.Context,
        CanonicalKey,
    ) (*CanonicalEntity, error)

    Search(
        context.Context,
        Query,
    ) (Page[CanonicalEntity], error)

    Save(
        context.Context,
        *CanonicalEntity,
        int64,
    ) error
}
```

---

# 33. Canonical commands

```go
type CreateEntityCommand struct { ... }
type ValidateEntityCommand struct { ... }
type ActivateEntityCommand struct { ... }
type SuspendEntityCommand struct { ... }
type RetireEntityCommand struct { ... }
```

Service:

```go
type Service interface {
    Create(
        context.Context,
        CreateEntityCommand,
    ) (*CanonicalEntity, error)

    Validate(
        context.Context,
        ValidateEntityCommand,
    ) error

    Activate(
        context.Context,
        ActivateEntityCommand,
    ) error

    Suspend(
        context.Context,
        SuspendEntityCommand,
    ) error

    Retire(
        context.Context,
        RetireEntityCommand,
    ) error
}
```

Generic `SetStatus()` SHALL NOT be part of the public application interface.

---

# 34. Mapping package

Core aggregates:

```go
type ExternalReference struct { ... }
type MappingScope struct { ... }
type Mapping struct { ... }
type MappingTypeDefinition struct { ... }
```

Scope:

```go
type MappingScope struct {
    ID                   uuid.UUID

    TenantID             *uuid.UUID
    LegalEntityID        *uuid.UUID
    OrganisationID       *uuid.UUID
    BusinessUnitID       *uuid.UUID

    OperatingRegionID    *uuid.UUID
    GeographicRegionID   *uuid.UUID

    MarketID             *uuid.UUID
    CountryCode          CountryCode

    DigitalEstateID      *uuid.UUID
    DigitalPropertyID    *uuid.UUID
    ChannelID            *uuid.UUID

    CurrencyCode         CurrencyCode
    Locale               Locale

    CatalogueID          *uuid.UUID
    CustomerSegmentID    *uuid.UUID

    EngineID             *uuid.UUID
    EngineInstanceID     *uuid.UUID

    Environment          Environment
    DeploymentRegion     string

    ScopeHash            string
}
```

---

# 35. Scope canonicalisation

Interface:

```go
type ScopeCanonicalizer interface {
    Canonicalize(MappingScope) ([]byte, error)
    Hash(MappingScope) (string, error)
}
```

One canonical implementation SHALL exist.

The canonical representation becomes part of the Control Plane contract because changing it changes scope deduplication.

Therefore it MUST have a version.

Recommended:

```text
scope-hash:v1:<hex-sha256>
```

---

# 36. Mapping repository

```go
type MappingRepository interface {
    CreateScope(
        context.Context,
        *MappingScope,
    ) error

    FindScopeByHash(
        context.Context,
        string,
    ) (*MappingScope, error)

    CreateExternalReference(
        context.Context,
        *ExternalReference,
    ) error

    GetExternalReference(
        context.Context,
        uuid.UUID,
    ) (*ExternalReference, error)

    CreateMapping(
        context.Context,
        *Mapping,
    ) error

    GetMapping(
        context.Context,
        uuid.UUID,
    ) (*Mapping, error)

    FindCandidates(
        context.Context,
        CandidateQuery,
    ) ([]MappingCandidate, error)

    SaveMapping(
        context.Context,
        *Mapping,
        int64,
    ) error
}
```

`FindCandidates` may optimise SQL selection but SHALL NOT make the final semantic resolution decision.

---

# 37. Mapping service

```go
type MappingService interface {
    Create(
        context.Context,
        CreateMappingCommand,
    ) (*Mapping, error)

    Validate(
        context.Context,
        ValidateMappingCommand,
    ) error

    Approve(
        context.Context,
        ApproveMappingCommand,
    ) error

    Activate(
        context.Context,
        ActivateMappingCommand,
    ) error

    Suspend(
        context.Context,
        SuspendMappingCommand,
    ) error

    Supersede(
        context.Context,
        SupersedeMappingCommand,
    ) (*Mapping, error)

    Retire(
        context.Context,
        RetireMappingCommand,
    ) error
}
```

---

# 38. Market package

Aggregate:

```go
type Market struct {
    ID                    uuid.UUID
    CanonicalKey          CanonicalKey
    Name                  string
    MarketType            string

    OwnerTenantID         uuid.UUID
    LegalEntityID         *uuid.UUID
    OperatingRegionID     *uuid.UUID

    ParentMarketID        *uuid.UUID

    DefaultCountry        CountryCode
    DefaultCurrency       CurrencyCode
    DefaultLocale         Locale
    Timezone              string

    Countries             []MarketCountry
    Currencies            []MarketCurrency
    Locales               []MarketLocale

    Status                LifecycleStatus

    EffectiveFrom         time.Time
    EffectiveTo           *time.Time

    Version               int64
}
```

Service interfaces SHALL expose commercial configuration operations rather than raw child-row CRUD.

---

# 39. Estate package

Types:

```go
type DigitalEstate struct { ... }
type DigitalProperty struct { ... }
type EstateMarketBinding struct { ... }
```

Repository:

```go
type Repository interface {
    GetEstate(context.Context, uuid.UUID) (*DigitalEstate, error)
    GetProperty(context.Context, uuid.UUID) (*DigitalProperty, error)

    FindPropertyByHostname(
        context.Context,
        string,
    ) (*DigitalProperty, error)

    MarketsForEstate(
        context.Context,
        uuid.UUID,
        time.Time,
    ) ([]EstateMarketBinding, error)
}
```

Hostname resolution SHALL lowercase and normalise hostnames before lookup.

---

# 40. Topology package

Types:

```go
type Engine struct { ... }
type EngineInstance struct { ... }
type EngineHealth struct { ... }
type EngineContract struct { ... }
```

Repository:

```go
type Repository interface {
    GetEngine(context.Context, uuid.UUID) (*Engine, error)

    GetInstance(
        context.Context,
        uuid.UUID,
    ) (*EngineInstance, error)

    InstancesForEngine(
        context.Context,
        uuid.UUID,
    ) ([]EngineInstance, error)

    ContractsForInstance(
        context.Context,
        uuid.UUID,
    ) ([]EngineContract, error)

    Health(
        context.Context,
        uuid.UUID,
    ) (*EngineHealth, error)
}
```

---

# 41. Capability package

Types:

```go
type Capability struct { ... }
type CapabilityDependency struct { ... }
type EngineCapability struct { ... }
type CapabilityBinding struct { ... }
```

Repository:

```go
type Repository interface {
    GetCapabilityByKey(
        context.Context,
        string,
    ) (*Capability, error)

    GetCapabilityByID(
        context.Context,
        uuid.UUID,
    ) (*Capability, error)

    FindBindingCandidates(
        context.Context,
        BindingCandidateQuery,
    ) ([]CapabilityBinding, error)

    CreateBinding(
        context.Context,
        *CapabilityBinding,
    ) error

    SaveBinding(
        context.Context,
        *CapabilityBinding,
        int64,
    ) error
}
```

---

# 42. Isolation package

```go
type IsolationProfile struct { ... }

type PolicyEvaluator interface {
    CheckBinding(
        context.Context,
        IsolationRequirement,
        IsolationProfile,
    ) error

    CheckResidency(
        context.Context,
        ResidencyRequirement,
        IsolationProfile,
        topology.EngineInstance,
    ) error

    CheckCrossTenant(
        context.Context,
        CrossTenantRequest,
    ) error
}
```

This package evaluates policy.

It must not directly execute infrastructure changes.

---

# 43. Runtime Context model

```go
type Context struct {
    ContextVersion       string

    TenantID             *uuid.UUID
    LegalEntityID        *uuid.UUID
    OrganisationID       *uuid.UUID
    BusinessUnitID       *uuid.UUID

    OperatingRegionID    *uuid.UUID
    GeographicRegionID   *uuid.UUID

    MarketID             *uuid.UUID
    CountryCode          CountryCode

    DigitalEstateID      *uuid.UUID
    DigitalPropertyID    *uuid.UUID
    ChannelID            *uuid.UUID

    CurrencyCode         CurrencyCode
    AccountingCurrency   CurrencyCode

    Locale               Locale
    Timezone             string

    ActorID               *uuid.UUID
    SubjectID             *uuid.UUID

    Environment           Environment

    RequestID             uuid.UUID
    CorrelationID         uuid.UUID
    CausationID           *uuid.UUID
    TraceID               string

    Provenance            map[string]ContextSource
}
```

---

# 44. Context trust levels

```go
type ContextSource struct {
    Source      string
    TrustLevel  TrustLevel
    Evidence    string
}

type TrustLevel string

const (
    TrustUntrusted  TrustLevel = "UNTRUSTED"
    TrustVerified   TrustLevel = "VERIFIED"
    TrustAuthorised TrustLevel = "AUTHORISED"
    TrustSystem     TrustLevel = "SYSTEM"
)
```

The resolver SHALL not equate user-supplied context claims with trusted context.

---

# 45. Resolver module

Create:

```text
internal/resolver/
├── context.go
├── mapping.go
├── capability.go
├── topology.go
├── specificity.go
├── provenance.go
└── errors.go
```

Primary interfaces:

```go
type ContextResolver interface {
    Resolve(
        context.Context,
        ResolutionEvidence,
    ) (contextmodel.Context, error)
}

type MappingResolver interface {
    Resolve(
        context.Context,
        MappingResolutionQuery,
    ) (ResolvedMapping, error)

    ResolveMany(
        context.Context,
        MappingResolutionQuery,
    ) ([]ResolvedMapping, error)
}

type CapabilityResolver interface {
    Resolve(
        context.Context,
        CapabilityResolutionQuery,
    ) (ResolvedCapability, error)
}

type TopologyResolver interface {
    ResolveInstance(
        context.Context,
        TopologyResolutionQuery,
    ) (topology.EngineInstance, error)
}
```

---

# 46. Context resolver responsibilities

```text
Authentication evidence
        ↓
Tenant resolution
        ↓
Digital property resolution
        ↓
Digital estate resolution
        ↓
Legal entity resolution
        ↓
Market resolution
        ↓
Country validation
        ↓
Currency resolution
        ↓
Locale resolution
        ↓
Channel resolution
        ↓
Authorization
        ↓
Trusted Context
```

The ContextResolver SHALL NOT invoke Payload, MedusaJS or iDempiere operational APIs.

---

# 47. Mapping resolution types

```go
type MappingResolutionQuery struct {
    CanonicalEntityID   uuid.UUID
    MappingType         MappingType

    EngineID            *uuid.UUID
    EngineInstanceID    *uuid.UUID
    CapabilityID        *uuid.UUID

    Context             contextmodel.Context
    EffectiveAt         time.Time

    Mode                ResolutionMode
}

type ResolvedMapping struct {
    Mapping             mapping.Mapping
    ExternalReference   *mapping.ExternalReference
    TargetEntityID      *uuid.UUID

    Specificity         int
    Provenance          ResolutionProvenance

    RegistryRevision    int64
}
```

---

# 48. Scope matcher interface

```go
type ScopeMatcher interface {
    Match(
        mapping.MappingScope,
        contextmodel.Context,
    ) ScopeMatch
}

type ScopeMatch struct {
    Compatible    bool
    Specificity   int
    Matched       []string
    Inherited     []string
    RejectedBy    []string
}
```

Scope matching MUST be deterministic.

---

# 49. Scope precedence

Initial canonical dimension precedence:

```text
tenant
legal_entity
organisation
business_unit
operating_region
market
country
digital_estate
digital_property
channel
catalogue
customer_segment
currency
locale
engine
engine_instance
environment
deployment_region
```

This list SHALL live in one versioned resolver policy.

No module may invent a separate precedence order.

---

# 50. MappingResolver errors

Typed domain errors:

```go
var (
    ErrMappingNotFound
    ErrMappingAmbiguous
    ErrMappingInactive
    ErrMappingOutOfPeriod
    ErrScopeMismatch
    ErrUntrustedContext
    ErrCrossTenantDenied
)
```

Errors SHALL be mappable deterministically to API Problem objects.

---

# 51. Capability resolution

```go
type CapabilityResolutionQuery struct {
    CapabilityKey  string
    Context        contextmodel.Context
    EffectiveAt    time.Time
}

type ResolvedCapability struct {
    Capability       capability.Capability
    Binding          capability.CapabilityBinding
    Engine           topology.Engine
    EngineInstance   topology.EngineInstance

    ContractVersion  string

    Provenance       ResolutionProvenance
}
```

Algorithm:

```text
lookup capability
        ↓
retrieve candidate bindings
        ↓
effective-period filter
        ↓
scope match
        ↓
environment filter
        ↓
isolation filter
        ↓
residency filter
        ↓
engine support validation
        ↓
contract compatibility
        ↓
lifecycle validation
        ↓
health policy
        ↓
binding-mode selection
        ↓
specificity
        ↓
deterministic result
```

---

# 52. Transaction manager

Application services requiring atomic operations SHALL depend upon:

```go
type TxManager interface {
    WithinTransaction(
        context.Context,
        func(context.Context) error,
    ) error
}
```

Repositories invoked with the transactional context use the same underlying transaction.

Avoid exposing raw SQL transactions throughout domain code.

---

# 53. Audit interface

```go
type Recorder interface {
    Record(
        context.Context,
        Record,
    ) error
}
```

The audit record MUST be inserted inside the same database transaction as critical state mutations.

---

# 54. Domain event interface

```go
type Event struct {
    ID               uuid.UUID
    Type             string
    Source           string
    Subject          string

    AggregateType    string
    AggregateID      uuid.UUID
    AggregateVersion int64

    TenantID         *uuid.UUID

    CorrelationID    *uuid.UUID
    CausationID      *uuid.UUID

    OccurredAt       time.Time

    ContractVersion  string

    Data             any
}

type Outbox interface {
    Append(
        context.Context,
        Event,
    ) error
}
```

---

# 55. Command transaction example

`ActivateMapping` SHALL conceptually execute:

```go
func (s *Service) Activate(
    ctx context.Context,
    cmd ActivateMappingCommand,
) error {
    return s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
        m, err := s.repo.GetForUpdate(txCtx, cmd.MappingID)
        if err != nil {
            return err
        }

        if err := s.authorizer.Authorize(...); err != nil {
            return err
        }

        if err := m.Activate(...); err != nil {
            return err
        }

        if err := s.repo.SaveMapping(
            txCtx,
            m,
            cmd.ExpectedVersion,
        ); err != nil {
            return err
        }

        if err := s.audit.Record(...); err != nil {
            return err
        }

        return s.outbox.Append(
            txCtx,
            MappingActivatedEvent(m),
        )
    })
}
```

The actual code need not copy this syntax exactly; the transaction semantics are normative.

---

# 56. HTTP transport interfaces

HTTP handlers SHOULD depend on application interfaces:

```go
type MappingHandler struct {
    service  mapping.Service
    resolver resolver.MappingResolver
}
```

Handlers are responsible for:

```text
decode
syntactic validation
authentication context extraction
calling application service
error translation
ETag
Location
response serialization
```

Handlers are NOT responsible for:

```text
scope precedence
mapping lifecycle
tenant policy
database queries
engine selection
```

---

# 57. Authentication and authorization boundary

Separate:

```text
authn
authz
```

Authentication answers:

```text
Who is calling?
```

Authorization answers:

```text
May this principal perform this action
on this resource in this scope?
```

Interfaces:

```go
type Authenticator interface {
    Authenticate(
        context.Context,
        CredentialEvidence,
    ) (Principal, error)
}

type Authorizer interface {
    Authorize(
        context.Context,
        Principal,
        Action,
        Resource,
        contextmodel.Context,
    ) error
}
```

---

# 58. Engine adapters

Engine integrations SHALL live outside domain packages:

```text
internal/integration/
├── payload/
├── medusa/
└── idempiere/
```

Canonical interface example:

```go
type ReferenceVerifier interface {
    Verify(
        context.Context,
        mapping.ExternalReference,
    ) (VerificationResult, error)
}
```

Provider registry:

```go
type VerifierRegistry interface {
    ForEngine(
        topology.Engine,
    ) (ReferenceVerifier, error)
}
```

Do not put:

```text
if engine == "medusa"
```

throughout business code.

---

# 59. Reconciliation service

Interface:

```go
type Reconciler interface {
    ReconcileReference(
        context.Context,
        uuid.UUID,
    ) (Result, error)

    ReconcileMapping(
        context.Context,
        uuid.UUID,
    ) (Result, error)

    Scan(
        context.Context,
        ScanQuery,
    ) (ScanResult, error)
}
```

States:

```text
HEALTHY
WARNING
DRIFTED
MISSING
CONFLICTING
QUARANTINED
```

Reconciliation SHALL not silently rewrite authoritative mappings.

---

# 60. Worker process

`cmd/worker` SHALL initially own:

```text
outbox publication
inbox processing
scheduled reconciliation
cache invalidation events
maintenance jobs
```

The API binary and worker binary MAY share internal packages but SHALL scale independently.

---

# 61. Migration binary

`cmd/migrate` SHALL:

```text
connect using migration credentials
validate PostgreSQL major compatibility
apply pending migrations
record migration version
fail closed on checksum mismatch
```

Application startup SHOULD NOT automatically mutate production schemas.

---

# 62. Observability

Each request receives:

```text
request_id
correlation_id
trace_id
```

Important metrics:

```text
baobab_cp_http_requests_total
baobab_cp_http_request_duration_seconds

baobab_cp_context_resolution_total
baobab_cp_context_resolution_failures_total

baobab_cp_mapping_resolution_total
baobab_cp_mapping_resolution_duration_seconds
baobab_cp_mapping_ambiguity_total

baobab_cp_capability_resolution_total
baobab_cp_capability_resolution_failures_total

baobab_cp_outbox_pending
baobab_cp_outbox_publish_failures_total

baobab_cp_reconciliation_conflicts

baobab_cp_db_transaction_failures_total
```

Logs SHALL prefer identifiers over copying sensitive business payloads.

---

# 63. Go test layers

```text
domain unit tests
resolver tests
property-based resolver tests
repository integration tests
PostgreSQL constraint tests
HTTP contract tests
AsyncAPI payload tests
authorization tests
tenant-escape tests
migration tests
reconciliation tests
outbox tests
race tests
```

CI SHOULD include:

```text
go test ./...
go test -race ./...
go vet ./...
```

plus organisation-approved static analysis.

---

# Part IV — Artefact 3: BCP-API-001

# OpenAPI and AsyncAPI Contract Suite

## 64. Contract authority

Canonical API/event definitions SHALL live in:

```text
baobab-platform/shared/
└── contracts/
    └── control-plane/
        ├── openapi/
        ├── asyncapi/
        ├── schemas/
        └── examples/
```

`baobab-cp` SHALL consume the contract.

A generated or vendored reference MAY exist inside `baobab-cp`, but it must be traceable to the authoritative shared version.

---

# 65. OpenAPI version

New HTTP definitions SHALL target:

```yaml
openapi: 3.2.0
```

OpenAPI 3.2.0 is the current published version and retains the language-neutral contract purpose required here.

If organisation tooling cannot yet reliably consume 3.2, a temporary compatibility ADR MAY pin generation to OpenAPI 3.1.2 while preserving semantics.

That is a tooling accommodation, not an architectural rollback.

---

# 66. OpenAPI root

Conceptually:

```yaml
openapi: 3.2.0

info:
  title: Baobab Control Plane API
  version: 1.0.0
  description: >
    Canonical identity, mapping, market, estate,
    topology, capability and resolution API
    for the Baobab Platform.

servers:
  - url: https://cp.api.baobab.example/api/v1

security:
  - bearerAuth: []

tags:
  - name: Canonical Entities
  - name: External References
  - name: Mappings
  - name: Markets
  - name: Digital Estates
  - name: Engines
  - name: Capabilities
  - name: Isolation
  - name: Resolution
  - name: Audit
```

Production URLs SHALL be environment-configured rather than hardcoded into canonical contracts where inappropriate.

---

# 67. Common HTTP headers

All responses SHOULD include:

```text
X-Request-ID
X-Correlation-ID
```

Mutable resource responses SHOULD include:

```text
ETag
```

Create requests supporting retry safety SHOULD accept:

```text
Idempotency-Key
```

Conditional mutations SHOULD accept:

```text
If-Match
```

Trace propagation SHALL follow the platform's distributed tracing standard.

---

# 68. Pagination contract

List APIs SHALL use cursor pagination.

Request:

```text
?page[size]=50
&page[after]=<opaque-cursor>
```

Response:

```json
{
  "items": [],
  "page": {
    "size": 50,
    "next": "...",
    "previous": null
  }
}
```

Cursors SHALL be opaque.

Do not expose physical table offsets as an API contract.

---

# 69. Filtering

Examples:

```text
GET /canonical-entities?filter[entity_type]=PRODUCT
GET /canonical-entities?filter[status]=ACTIVE
GET /mappings?filter[canonical_entity_id]=...
GET /mappings?filter[mapping_type]=COMMERCE
GET /markets?filter[owner_tenant_id]=...
```

Unknown filters SHOULD return a semantic validation error rather than be silently ignored.

---

# 70. Problem contract

All errors SHALL use one canonical structure inspired by HTTP problem semantics:

```yaml
Problem:
  type: object
  required:
    - type
    - code
    - title
    - status
    - request_id
    - correlation_id
  properties:
    type:
      type: string
    code:
      type: string
    title:
      type: string
    status:
      type: integer
    detail:
      type: string
    instance:
      type: string
    request_id:
      type: string
      format: uuid
    correlation_id:
      type: string
      format: uuid
    violations:
      type: array
      items:
        $ref: "#/components/schemas/Violation"
```

Violation:

```yaml
Violation:
  type: object
  required:
    - code
    - message
  properties:
    field:
      type: string
    code:
      type: string
    message:
      type: string
```

---

# 71. Stable error codes

Examples:

```text
BAOBAB_CP_400_INVALID_REQUEST
BAOBAB_CP_401_UNAUTHENTICATED
BAOBAB_CP_403_FORBIDDEN
BAOBAB_CP_404_NOT_FOUND

BAOBAB_CP_409_CANONICAL_KEY_CONFLICT
BAOBAB_CP_409_MAPPING_CONFLICT
BAOBAB_CP_409_BINDING_CONFLICT
BAOBAB_CP_409_LIFECYCLE_CONFLICT

BAOBAB_CP_412_VERSION_MISMATCH

BAOBAB_CP_422_SCOPE_INVALID
BAOBAB_CP_422_CONTEXT_INVALID
BAOBAB_CP_422_MAPPING_AMBIGUOUS
BAOBAB_CP_422_CAPABILITY_UNAVAILABLE
BAOBAB_CP_422_RESIDENCY_VIOLATION

BAOBAB_CP_503_DEPENDENCY_UNAVAILABLE
```

Human text may evolve.

Machine error codes shall remain stable within API major version.

---

# 72. Canonical entity schema

```yaml
CanonicalEntity:
  type: object
  required:
    - id
    - canonical_key
    - entity_type
    - display_name
    - authority
    - classification
    - status
    - version
    - effective_from
  properties:
    id:
      type: string
      format: uuid

    canonical_key:
      type: string

    entity_type:
      type: string

    subtype:
      type:
        - string
        - "null"

    display_name:
      type: string

    owner_tenant_id:
      type:
        - string
        - "null"
      format: uuid

    owner_legal_entity_id:
      type:
        - string
        - "null"
      format: uuid

    authority:
      type: string

    classification:
      $ref: "#/components/schemas/Classification"

    status:
      $ref: "#/components/schemas/LifecycleStatus"

    schema_version:
      type: integer
      minimum: 1

    effective_from:
      type: string
      format: date-time

    effective_to:
      type:
        - string
        - "null"
      format: date-time

    metadata:
      type: object

    version:
      type: integer
      minimum: 1
```

---

# 73. Create canonical entity

```text
POST /canonical-entities
```

Request:

```yaml
CreateCanonicalEntityRequest:
  type: object
  required:
    - canonical_key
    - entity_type
    - display_name
    - authority
    - classification
    - effective_from
  properties:
    canonical_key:
      type: string
    entity_type:
      type: string
    subtype:
      type: string
    display_name:
      type: string
    owner_tenant_id:
      type: string
      format: uuid
    owner_legal_entity_id:
      type: string
      format: uuid
    authority:
      type: string
    classification:
      $ref: "#/components/schemas/Classification"
    effective_from:
      type: string
      format: date-time
    effective_to:
      type: string
      format: date-time
    metadata:
      type: object
```

Success:

```text
201 Created
Location: /api/v1/canonical-entities/{id}
ETag: "1"
```

---

# 74. Canonical lifecycle commands

```text
POST /canonical-entities/{id}/validate
POST /canonical-entities/{id}/activate
POST /canonical-entities/{id}/suspend
POST /canonical-entities/{id}/retire
```

Command request:

```yaml
LifecycleCommand:
  type: object
  properties:
    effective_at:
      type: string
      format: date-time
    reason:
      type: string
```

`If-Match` SHALL be mandatory for privileged lifecycle transitions.

---

# 75. External reference schema

```yaml
ExternalReference:
  type: object
  required:
    - id
    - system_namespace
    - environment
    - native_entity_type
    - native_id
    - status
  properties:
    id:
      type: string
      format: uuid

    engine_id:
      type:
        - string
        - "null"
      format: uuid

    engine_instance_id:
      type:
        - string
        - "null"
      format: uuid

    system_namespace:
      type: string

    environment:
      type: string

    native_entity_type:
      type: string

    native_id:
      type: string

    native_key:
      type:
        - string
        - "null"

    native_uri:
      type:
        - string
        - "null"

    source_authority:
      type:
        - string
        - "null"

    fingerprint:
      type:
        - string
        - "null"

    status:
      $ref: "#/components/schemas/LifecycleStatus"
```

---

# 76. MappingScope API schema

```yaml
MappingScope:
  type: object
  properties:
    tenant_id:
      type: string
      format: uuid
    legal_entity_id:
      type: string
      format: uuid
    organisation_id:
      type: string
      format: uuid
    business_unit_id:
      type: string
      format: uuid

    operating_region_id:
      type: string
      format: uuid
    geographic_region_id:
      type: string
      format: uuid

    market_id:
      type: string
      format: uuid
    country_code:
      type: string
      pattern: "^[A-Z]{2}$"

    digital_estate_id:
      type: string
      format: uuid
    digital_property_id:
      type: string
      format: uuid
    channel_id:
      type: string
      format: uuid

    currency_code:
      type: string
      pattern: "^[A-Z]{3}$"
    locale:
      type: string

    catalogue_id:
      type: string
      format: uuid
    customer_segment_id:
      type: string
      format: uuid

    engine_id:
      type: string
      format: uuid
    engine_instance_id:
      type: string
      format: uuid

    environment:
      type: string
    deployment_region:
      type: string
```

Clients SHALL not submit `scope_hash`.

That is calculated by the Control Plane.

---

# 77. Mapping schema

```yaml
Mapping:
  type: object
  required:
    - id
    - mapping_type
    - canonical_entity_id
    - scope
    - direction
    - cardinality
    - confidence
    - status
    - effective_from
    - version
  properties:
    id:
      type: string
      format: uuid

    mapping_type:
      type: string

    resolution_mode:
      $ref: "#/components/schemas/ResolutionMode"

    canonical_entity_id:
      type: string
      format: uuid

    external_reference_id:
      type:
        - string
        - "null"
      format: uuid

    target_canonical_entity_id:
      type:
        - string
        - "null"
      format: uuid

    scope:
      $ref: "#/components/schemas/MappingScope"

    direction:
      $ref: "#/components/schemas/MappingDirection"

    cardinality:
      $ref: "#/components/schemas/MappingCardinality"

    authority:
      type: string

    confidence:
      $ref: "#/components/schemas/MappingConfidence"

    resolution_priority:
      type: integer

    status:
      $ref: "#/components/schemas/LifecycleStatus"

    effective_from:
      type: string
      format: date-time

    effective_to:
      type:
        - string
        - "null"
      format: date-time

    supersedes_mapping_id:
      type:
        - string
        - "null"
      format: uuid

    mapping_version:
      type: integer

    metadata:
      type: object

    version:
      type: integer
```

---

# 78. Mapping lifecycle resources

```text
POST /mappings
GET  /mappings/{id}
GET  /mappings

POST /mappings/{id}/validate
POST /mappings/{id}/approve
POST /mappings/{id}/activate
POST /mappings/{id}/suspend
POST /mappings/{id}/supersede
POST /mappings/{id}/retire
```

`supersede` requires replacement definition:

```yaml
SupersedeMappingRequest:
  type: object
  required:
    - replacement
    - effective_at
    - reason
  properties:
    replacement:
      $ref: "#/components/schemas/CreateMappingRequest"
    effective_at:
      type: string
      format: date-time
    reason:
      type: string
```

One operation creates the successor and closes the predecessor transactionally.

---

# 79. Market API

```text
POST /markets
GET  /markets
GET  /markets/{id}
PATCH /markets/{id}

POST /markets/{id}/activate
POST /markets/{id}/suspend
POST /markets/{id}/retire

PUT /markets/{id}/countries
PUT /markets/{id}/currencies
PUT /markets/{id}/locales
```

The `PUT` semantics for child configuration mean:

```text
replace the complete intended configuration
```

with optimistic concurrency.

---

# 80. Digital estate API

```text
POST /digital-estates
GET  /digital-estates
GET  /digital-estates/{id}
PATCH /digital-estates/{id}

POST /digital-estates/{id}/activate
POST /digital-estates/{id}/retire

POST /digital-estates/{id}/properties
GET  /digital-properties/{id}

PUT /digital-estates/{id}/markets
```

Hostname lookup MAY expose an internal operational endpoint:

```text
GET /digital-properties:resolve?hostname=...
```

but public context resolution should generally use the resolver API.

---

# 81. Engine API

```text
POST /engines
GET  /engines
GET  /engines/{id}

POST /engine-instances
GET  /engine-instances
GET  /engine-instances/{id}

POST /engine-instances/{id}/activate
POST /engine-instances/{id}/deprecate
POST /engine-instances/{id}/retire

PUT /engine-instances/{id}/contracts
```

Credentials are referenced, never returned.

---

# 82. Capability API

```text
POST /capabilities
GET  /capabilities
GET  /capabilities/{id}

PUT /engines/{engine_id}/capabilities

POST /capability-bindings
GET  /capability-bindings
GET  /capability-bindings/{id}

POST /capability-bindings/{id}/approve
POST /capability-bindings/{id}/activate
POST /capability-bindings/{id}/suspend
POST /capability-bindings/{id}/retire
```

---

# 83. Isolation API

```text
POST /isolation-profiles
GET  /isolation-profiles
GET  /isolation-profiles/{id}

POST /isolation-profiles/{id}/validate
POST /isolation-profiles/{id}/activate
POST /isolation-profiles/{id}/supersede
```

Activated versions SHOULD be treated as immutable policy snapshots.

---

# 84. Context resolution API

```text
POST /resolve/context
```

Request:

```yaml
ResolveContextRequest:
  type: object
  properties:
    hostname:
      type: string

    requested_tenant_id:
      type: string
      format: uuid

    requested_market_id:
      type: string
      format: uuid

    requested_currency:
      type: string
      pattern: "^[A-Z]{3}$"

    requested_locale:
      type: string

    channel_hint:
      type: string
      format: uuid

    environment:
      type: string
```

These are **claims/hints**, not trusted Context.

---

# 85. Resolved context

```yaml
ResolvedContext:
  type: object
  required:
    - context_version
    - request_id
    - correlation_id
    - environment
    - provenance
  properties:
    context_version:
      type: string

    tenant_id:
      type:
        - string
        - "null"
      format: uuid

    legal_entity_id:
      type:
        - string
        - "null"
      format: uuid

    organisation_id:
      type:
        - string
        - "null"
      format: uuid

    business_unit_id:
      type:
        - string
        - "null"
      format: uuid

    operating_region_id:
      type:
        - string
        - "null"
      format: uuid

    market_id:
      type:
        - string
        - "null"
      format: uuid

    country_code:
      type:
        - string
        - "null"

    digital_estate_id:
      type:
        - string
        - "null"
      format: uuid

    digital_property_id:
      type:
        - string
        - "null"
      format: uuid

    channel_id:
      type:
        - string
        - "null"
      format: uuid

    currency:
      type:
        - string
        - "null"

    accounting_currency:
      type:
        - string
        - "null"

    locale:
      type:
        - string
        - "null"

    timezone:
      type:
        - string
        - "null"

    request_id:
      type: string
      format: uuid

    correlation_id:
      type: string
      format: uuid

    trace_id:
      type:
        - string
        - "null"

    environment:
      type: string

    provenance:
      type: object
      additionalProperties:
        $ref: "#/components/schemas/ContextProvenance"
```

---

# 86. Mapping resolution API

```text
POST /resolve/mapping
POST /resolve/mappings
```

Request:

```yaml
ResolveMappingRequest:
  type: object
  required:
    - canonical_entity_id
    - mapping_type
    - context
  properties:
    canonical_entity_id:
      type: string
      format: uuid

    mapping_type:
      type: string

    engine_id:
      type: string
      format: uuid

    engine_instance_id:
      type: string
      format: uuid

    capability_id:
      type: string
      format: uuid

    effective_at:
      type: string
      format: date-time

    context:
      $ref: "#/components/schemas/ContextReference"
```

---

# 87. Resolved mapping response

```yaml
ResolvedMapping:
  type: object
  required:
    - mapping
    - resolution
  properties:
    mapping:
      $ref: "#/components/schemas/Mapping"

    external_reference:
      $ref: "#/components/schemas/ExternalReference"

    target_canonical_entity_id:
      type:
        - string
        - "null"
      format: uuid

    resolution:
      type: object
      required:
        - specificity
        - registry_revision
      properties:
        specificity:
          type: integer
        registry_revision:
          type: integer
        matched_dimensions:
          type: array
          items:
            type: string
        inherited_dimensions:
          type: array
          items:
            type: string
```

---

# 88. Capability resolution API

```text
POST /resolve/capability
```

Request:

```yaml
ResolveCapabilityRequest:
  type: object
  required:
    - capability
    - context
  properties:
    capability:
      type: string

    effective_at:
      type: string
      format: date-time

    context:
      $ref: "#/components/schemas/ContextReference"
```

Response:

```yaml
ResolvedCapability:
  type: object
  required:
    - capability
    - binding
    - engine
    - engine_instance
    - contract_version
  properties:
    capability:
      $ref: "#/components/schemas/Capability"

    binding:
      $ref: "#/components/schemas/CapabilityBinding"

    engine:
      $ref: "#/components/schemas/Engine"

    engine_instance:
      $ref: "#/components/schemas/EngineInstance"

    contract_version:
      type: string

    resolution:
      $ref: "#/components/schemas/ResolutionProvenance"
```

---

# 89. Audit API

Audit is principally an administrative/read capability.

```text
GET /audit-records
GET /audit-records/{id}
```

Filters:

```text
resource_type
resource_id
actor_id
tenant_id
correlation_id
occurred_after
occurred_before
```

No mutation endpoints.

---

# 90. Security schemes

Conceptual OpenAPI:

```yaml
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
```

The contract SHOULD avoid binding itself unnecessarily to the token issuer.

Authorization scopes/claims are governed separately.

Machine-to-machine authentication MAY eventually introduce OAuth2 client credentials without redesigning resource contracts.

---

# 91. Idempotency OpenAPI component

```yaml
parameters:
  IdempotencyKey:
    name: Idempotency-Key
    in: header
    required: true
    schema:
      type: string
      minLength: 16
      maxLength: 255
```

The same key with a different payload MUST produce a conflict rather than executing a different operation.

---

# 92. ETag component

```yaml
parameters:
  IfMatch:
    name: If-Match
    in: header
    required: true
    schema:
      type: string
```

Lifecycle and mutable resource updates SHALL use it.

---

# 93. AsyncAPI version

Canonical event contracts SHALL target:

```yaml
asyncapi: 3.0.0
```

AsyncAPI 3 separates channels, operations and messages, which is useful because Baobab may later use different broker technologies without redefining event semantics.

---

# 94. AsyncAPI root

```yaml
asyncapi: 3.0.0

id: urn:nabhold:baobab:control-plane

info:
  title: Baobab Control Plane Events
  version: 1.0.0
  description: >
    Canonical events published and consumed
    by the Baobab Control Plane.

defaultContentType: application/json

channels:
  canonicalEntityEvents:
    address: baobab.canonical-entity

  mappingEvents:
    address: baobab.mapping

  marketEvents:
    address: baobab.market

  estateEvents:
    address: baobab.digital-estate

  topologyEvents:
    address: baobab.topology

  capabilityEvents:
    address: baobab.capability

operations:
  publishCanonicalEntityEvents:
    action: send
    channel:
      $ref: "#/channels/canonicalEntityEvents"

  publishMappingEvents:
    action: send
    channel:
      $ref: "#/channels/mappingEvents"
```

Physical broker/topic naming can be supplied through deployment bindings.

---

# 95. Canonical event envelope

Every event SHALL contain:

```yaml
CanonicalEvent:
  type: object
  required:
    - specversion
    - id
    - type
    - source
    - subject
    - time
    - aggregate_type
    - aggregate_id
    - aggregate_version
    - contract_version
    - data
  properties:
    specversion:
      type: string
      const: "1.0"

    id:
      type: string
      format: uuid

    type:
      type: string

    source:
      type: string

    subject:
      type: string

    time:
      type: string
      format: date-time

    aggregate_type:
      type: string

    aggregate_id:
      type: string
      format: uuid

    aggregate_version:
      type: integer
      minimum: 1

    tenant_id:
      type:
        - string
        - "null"
      format: uuid

    correlation_id:
      type:
        - string
        - "null"
      format: uuid

    causation_id:
      type:
        - string
        - "null"
      format: uuid

    contract_version:
      type: string

    context:
      type:
        - object
        - "null"

    data:
      type: object
```

---

# 96. Event naming convention

Format:

```text
baobab.<aggregate>.<past-tense-event>.v<major>
```

Examples:

```text
baobab.canonical-entity.created.v1
baobab.mapping.activated.v1
baobab.market.configuration-changed.v1
baobab.engine-instance.deprecated.v1
baobab.capability-binding.activated.v1
```

The event name major version changes only for incompatible semantic changes.

Payload schema revisions that remain backward compatible do not require renaming to `v2`.

---

# 97. Canonical entity events

```text
baobab.canonical-entity.created.v1
baobab.canonical-entity.validated.v1
baobab.canonical-entity.activated.v1
baobab.canonical-entity.suspended.v1
baobab.canonical-entity.retired.v1
```

Created data:

```yaml
CanonicalEntityCreatedData:
  type: object
  required:
    - canonical_entity
  properties:
    canonical_entity:
      $ref: "./schemas/canonical-entity.yaml"
```

Retired event SHOULD include:

```text
retired_at
reason
successor_id if applicable
```

---

# 98. External-reference events

```text
baobab.external-reference.registered.v1
baobab.external-reference.verified.v1
baobab.external-reference.quarantined.v1
baobab.external-reference.retired.v1
```

Verification event:

```yaml
ExternalReferenceVerifiedData:
  type: object
  required:
    - external_reference_id
    - verified_at
  properties:
    external_reference_id:
      type: string
      format: uuid
    verified_at:
      type: string
      format: date-time
    fingerprint:
      type:
        - string
        - "null"
```

---

# 99. Mapping events

```text
baobab.mapping.created.v1
baobab.mapping.validated.v1
baobab.mapping.approved.v1
baobab.mapping.activated.v1
baobab.mapping.suspended.v1
baobab.mapping.superseded.v1
baobab.mapping.quarantined.v1
baobab.mapping.retired.v1
```

Activated event payload:

```yaml
MappingActivatedData:
  type: object
  required:
    - mapping_id
    - canonical_entity_id
    - mapping_type
    - scope_id
    - effective_from
  properties:
    mapping_id:
      type: string
      format: uuid

    canonical_entity_id:
      type: string
      format: uuid

    external_reference_id:
      type:
        - string
        - "null"
      format: uuid

    target_canonical_entity_id:
      type:
        - string
        - "null"
      format: uuid

    mapping_type:
      type: string

    scope_id:
      type: string
      format: uuid

    effective_from:
      type: string
      format: date-time

    effective_to:
      type:
        - string
        - "null"
      format: date-time
```

---

# 100. Mapping supersession event

```yaml
MappingSupersededData:
  type: object
  required:
    - previous_mapping_id
    - successor_mapping_id
    - effective_at
  properties:
    previous_mapping_id:
      type: string
      format: uuid

    successor_mapping_id:
      type: string
      format: uuid

    effective_at:
      type: string
      format: date-time

    reason:
      type:
        - string
        - "null"
```

This event is important for caches and downstream mapping projections.

---

# 101. Market events

```text
baobab.market.created.v1
baobab.market.activated.v1
baobab.market.configuration-changed.v1
baobab.market.suspended.v1
baobab.market.retired.v1
```

`configuration-changed` SHOULD identify categories changed:

```json
{
  "changed": ["currencies", "locales", "countries"]
}
```

Consumers should re-fetch authoritative current configuration rather than expect every event to carry an enormous market document.

---

# 102. Estate events

```text
baobab.digital-estate.created.v1
baobab.digital-estate.activated.v1
baobab.digital-estate.retired.v1

baobab.digital-property.bound.v1
baobab.digital-property.changed.v1
baobab.digital-property.unbound.v1

baobab.estate-market.bound.v1
baobab.estate-market.unbound.v1
```

---

# 103. Topology events

```text
baobab.engine.registered.v1

baobab.engine-instance.registered.v1
baobab.engine-instance.activated.v1
baobab.engine-instance.deprecated.v1
baobab.engine-instance.retired.v1

baobab.engine-instance.contract-changed.v1
```

Ephemeral health telemetry SHOULD NOT necessarily become canonical domain events.

If needed:

```text
baobab.engine-instance.health-changed.v1
```

belongs to an operational event stream rather than the canonical configuration event stream.

---

# 104. Capability events

```text
baobab.capability.registered.v1

baobab.capability-binding.created.v1
baobab.capability-binding.approved.v1
baobab.capability-binding.activated.v1
baobab.capability-binding.suspended.v1
baobab.capability-binding.retired.v1

baobab.capability-binding.migration-started.v1
baobab.capability-binding.migration-completed.v1
```

---

# 105. Isolation events

```text
baobab.isolation-profile.created.v1
baobab.isolation-profile.validated.v1
baobab.isolation-profile.activated.v1
baobab.isolation-profile.superseded.v1
baobab.isolation-profile.retired.v1
```

---

# 106. Event compatibility policy

Event evolution SHALL follow:

### Backward-compatible

May be introduced without new event major:

```text
new optional property
new optional metadata
new enum value only where consumers are required to tolerate unknown values
new message headers
```

### Breaking

Requires new event major:

```text
remove required field
change field meaning
change primitive type incompatibly
change aggregate semantics
change event guarantee materially
```

---

# 107. Consumer rules

Consumers SHALL:

- deduplicate using event `id`;
- not rely on global ordering;
- track aggregate version when order matters;
- tolerate events being delivered more than once;
- tolerate delayed delivery;
- ignore unknown optional fields;
- not infer deletion from silence;
- not access Control Plane tables directly.

---

# 108. Producer rules

Baobab Control Plane SHALL:

- generate event IDs before persistence;
- write event to outbox in same transaction as aggregate mutation;
- preserve aggregate version;
- include event occurrence time;
- propagate correlation and causation IDs;
- never publish secrets;
- avoid leaking unrelated tenant data;
- serialize against canonical schema;
- validate event before publication.

---

# 109. Contract examples

Every OpenAPI and AsyncAPI schema SHALL have at least one valid example.

Critical error cases SHOULD also have examples:

```text
mapping conflict
ambiguous resolution
stale ETag
cross-tenant denial
residency violation
unavailable capability
```

Examples become part of developer documentation and contract tests.

---

# 110. Contract CI

`baobab-platform/shared` SHALL test:

```text
OpenAPI syntactic validity
OpenAPI semantic linting
AsyncAPI validity
JSON Schema validity
reference integrity
event-name conventions
error-code conventions
breaking-change detection
examples against schemas
```

`baobab-platform/baobab-cp` SHALL test:

```text
handlers conform to OpenAPI
responses conform to schemas
events conform to AsyncAPI message schemas
every documented operation has implementation
every published event has contract
```

Consumer repositories SHALL run compatibility tests against the shared contract version they declare.

---

# Part V — Traceability Matrix

## 111. Parent → database → Go → API/Event traceability

| Parent concept    | PostgreSQL                      | Go module                  | HTTP/Event                               |
| ----------------- | ------------------------------- | -------------------------- | ---------------------------------------- |
| CanonicalEntity   | `registry.canonical_entity`     | `canonical`                | `/canonical-entities`, canonical events  |
| ExternalReference | `mapping.external_reference`    | `mapping`                  | `/external-references`, reference events |
| Mapping           | `mapping.mapping`               | `mapping`                  | `/mappings`, mapping events              |
| MappingScope      | `mapping.mapping_scope`         | `mapping` / `resolver`     | embedded scope schema                    |
| Market            | `market.*`                      | `market`                   | `/markets`, market events                |
| DigitalEstate     | `estate.*`                      | `estate`                   | `/digital-estates`, estate events        |
| Engine            | `topology.engine`               | `topology`                 | `/engines`, topology events              |
| EngineInstance    | `topology.engine_instance`      | `topology`                 | `/engine-instances`                      |
| Capability        | `capability.capability`         | `capability`               | `/capabilities`                          |
| CapabilityBinding | `capability.capability_binding` | `capability` / `resolver`  | `/capability-bindings`                   |
| Context           | runtime + audit snapshot        | `contextmodel`, `resolver` | `/resolve/context`                       |
| IsolationProfile  | `policy.isolation_profile`      | `isolation`                | `/isolation-profiles`                    |
| Audit             | `audit.audit_record`            | `audit`                    | read APIs                                |
| Events            | `messaging.outbox`              | `messaging`                | AsyncAPI                                 |
| Idempotency       | `system.idempotency_record`     | `idempotency`              | HTTP header contract                     |

Every production implementation ticket SHOULD reference at least one row of this traceability matrix.

---

# Part VI — Cross-Cutting Implementation Contracts

## 112. Transaction contract

All critical mutations follow:

```text
authenticate
    ↓
authorize
    ↓
validate request
    ↓
BEGIN
    ↓
load aggregate / SELECT FOR UPDATE where needed
    ↓
validate lifecycle and policy
    ↓
mutate
    ↓
persist
    ↓
audit
    ↓
increment registry revision
    ↓
append outbox event
    ↓
COMMIT
```

No network call to Payload, Medusa or iDempiere may occur while holding the database transaction unless specifically justified by an ADR.

---

# 113. Concurrency contract

Mutable aggregate operations SHALL use optimistic concurrency.

API:

```text
ETag
If-Match
```

Persistence:

```text
WHERE id = ?
AND version = expected
```

Successful mutation:

```text
version = version + 1
```

Stale version:

```text
HTTP 412
BAOBAB_CP_412_VERSION_MISMATCH
```

---

# 114. Temporal contract

Every temporal resource uses:

```text
[start, end)
```

Therefore:

```text
Mapping A
2027-01-01 → 2027-06-01

Mapping B
2027-06-01 → ∞
```

does **not** overlap.

Historical resolver queries SHALL accept an explicit:

```text
effective_at
```

If absent:

```text
current trusted system time
```

is used.

---

# 115. Resolution contract

Mapping resolution order:

```text
1 validity
2 active lifecycle
3 confirmed confidence
4 context compatibility
5 target compatibility
6 scope specificity
7 explicit override semantics
8 resolution priority
9 deterministic tie detection
```

If two candidates remain equivalently authoritative:

```text
fail
```

Do not select one arbitrarily.

---

# 116. Inheritance contract

Initial configuration precedence:

```text
Platform
   ↓
Tenant
   ↓
Legal Entity
   ↓
Organisation / Business Unit
   ↓
Operating Region
   ↓
Market
   ↓
Digital Estate
   ↓
Digital Property
   ↓
Channel
   ↓
Segment / catalogue
   ↓
Currency / locale specialisation
   ↓
Request-specific authorised override
```

Absence:

```text
inherit
```

Explicit disablement MUST be represented separately from absence.

---

# 117. Security contract

Cross-tenant relationships are:

```text
DENY BY DEFAULT
```

Privileged operations include at least:

```text
activate mapping
approve mapping
cross-tenant mapping
activate capability binding
change accounting/tax/payment mapping
activate isolation profile
retire engine instance
```

They require:

```text
authenticated principal
authorisation decision
tenant/context validation
audit record
```

Higher-risk operations MAY require separate approver identity.

---

# 118. Engine independence contract

No Control Plane domain package may import a Payload-, Medusa- or iDempiere-specific implementation package.

Allowed direction:

```text
integration/payload
     ↓ implements
generic Control Plane port
```

Not:

```text
mapping/domain
     ↓ imports
integration/medusa
```

This rule SHALL be architecture-tested.

---

# 119. Database independence boundary

Baobab does **not** seek database-vendor abstraction for the Control Plane.

PostgreSQL 17 features are intentional architectural dependencies:

```text
uuid
jsonb
tstzrange
GiST
exclusion constraints
partial indexes
transactions
explicit row locking
```

Trying to hide these behind lowest-common-denominator SQL would weaken correctness.

---

# 120. Contract version dimensions

Never conflate:

```text
Canonical model version
Database migration version
OpenAPI document version
API major version
AsyncAPI document version
Event major version
Engine software version
Engine contract version
Mapping revision
Aggregate version
Isolation profile version
```

Each exists for a different reason.

---

# 121. Initial production vertical slice

The first implementation release SHALL prove this complete path:

```text
Create CanonicalEntity
        ↓
Activate CanonicalEntity
        ↓
Register Engine
        ↓
Register EngineInstance
        ↓
Register Capability
        ↓
Declare Engine Capability
        ↓
Create MappingScope
        ↓
Register ExternalReference
        ↓
Create Mapping
        ↓
Approve Mapping
        ↓
Activate Mapping
        ↓
Create CapabilityBinding
        ↓
Activate CapabilityBinding
        ↓
Resolve Context
        ↓
Resolve Capability
        ↓
Resolve Mapping
        ↓
Return deterministic result
        ↓
Audit mutations
        ↓
Publish events through outbox
```

This is the first **architectural vertical slice**, not merely CRUD.

---

# 122. Second vertical slice

Connect exactly one real engine.

Recommended:

```text
MedusaJS
```

because it exercises:

```text
product mappings
sales-channel/market concepts
engine capability binding
external-reference verification
estate-context resolution
```

Payload would also be acceptable if implementation sequence makes it easier.

iDempiere SHOULD follow after the mapping and resolver contracts have proven stable because ERP mappings carry greater financial and organisational consequences.

---

# 123. Third vertical slice

Introduce iDempiere with explicit mappings for:

```text
Baobab canonical identity
       │
       ├── iDempiere AD_Client representation
       ├── AD_Org representation
       ├── M_Product representation
       ├── C_BPartner representation
       └── M_Warehouse representation
```

There SHALL be no universal invariant such as:

```text
Baobab Tenant == AD_Client
```

or:

```text
Baobab LegalEntity == AD_Org
```

Those are deployment/mapping decisions.

---

# 124. Definition of done — BCP-DB-001

Database artefact is complete when:

```text
all 17 migrations exist
fresh migration succeeds
upgrade migration succeeds
checks exist
FKs exist
temporal exclusion works
indexes exist
database roles documented
constraint tests run in CI
schema contract metadata exists
```

---

# 125. Definition of done — BCP-GO-001

Go artefact is complete when:

```text
bounded packages exist
dependency direction is enforced
domain types exist
repository ports exist
application service interfaces exist
resolver interfaces exist
transaction manager exists
audit/outbox ports exist
authn/authz boundaries exist
integration adapter ports exist
architecture tests exist
```

No Payload, Medusa or iDempiere business implementation is necessary to declare this artefact structurally complete.

---

# 126. Definition of done — BCP-API-001

Contract artefact is complete when:

```text
OpenAPI document validates
AsyncAPI document validates

all resources exist
all lifecycle operations exist
all resolution operations exist

error schema exists
stable error codes exist

ETag contract exists
idempotency contract exists

all canonical events exist
event envelope exists

examples validate
breaking-change checks exist
```

---

# 127. Repository responsibility matrix

```text
baobab-platform/shared
    owns:
        canonical schemas
        OpenAPI
        AsyncAPI
        error contracts
        event contracts
        identifiers
        compatibility policy

baobab-platform/baobab-cp
    owns:
        Go implementation
        migrations
        runtime database state
        resolver implementation
        REST implementation
        event production
        reconciliation
        audit

engine repositories
    own:
        engine-native adapters
        engine capability implementation
        contract compatibility
        engine-native resource semantics

digital estates
    own:
        experience
        presentation
        estate-specific routing/client integration

infrastructure
    owns:
        deployment
        database provisioning
        networking
        secrets
        observability platform
        backups
```

---

# 128. Architectural non-goals for this phase

Do not introduce merely because the system sounds "enterprise":

```text
Kafka-specific architecture
graph database
service mesh
event sourcing
CQRS framework
distributed workflow engine
generic rules engine
custom policy DSL
schema registry server
microservice decomposition
Kubernetes operator
```

They remain future options.

Correct contracts, PostgreSQL invariants, deterministic Go services and strong testing come first.

---

# 129. Final governing implementation contract

The three derived artefacts SHALL collectively preserve the following invariant:

> **The Baobab Control Plane governs canonical identity, contextual relationships, platform topology and capability selection; PostgreSQL protects durable facts, Go interprets and governs those facts, OpenAPI defines synchronous interactions, AsyncAPI defines asynchronous interactions, and specialised engines retain ownership of operational business execution.**

The physical implementation therefore becomes:

```text
                         baobab-platform/shared
                               │
                    Canonical Contracts
                               │
             ┌─────────────────┴─────────────────┐
             │                                   │
        OpenAPI 3.2                        AsyncAPI 3.0
             │                                   │
             ▼                                   ▼
                       baobab-platform/baobab-cp
                               │
                 ┌─────────────┴─────────────┐
                 │                           │
               Go API                    Go Worker
                 │                           │
                 ├──── ContextResolver       ├──── Outbox
                 ├──── MappingResolver       ├──── Inbox
                 ├──── CapabilityResolver    ├──── Reconciliation
                 └──── TopologyResolver      └──── Maintenance
                 │                           │
                 └─────────────┬─────────────┘
                               │
                         PostgreSQL 17
                               │
      ┌──────────────┬─────────┼─────────┬──────────────┐
      │              │         │         │              │
  Canonical       Mapping    Market    Estate       Topology
  Registry        Registry   Registry   Registry     Registry
      │              │         │         │              │
      └──────────────┴─────────┼─────────┴──────────────┘
                               │
                    Capability / Isolation
                               │
                         Audit / Outbox
                               │
                               ▼
                  APIs + Canonical Events
                               │
                ┌──────────────┼──────────────┐
                ▼              ▼              ▼
             Payload        MedusaJS      iDempiere
                │              │              │
                └──────────────┼──────────────┘
                               ▼
                     Independent Estates
```

And one rule remains non-negotiable:

> **No operational engine ID, table structure, vendor concept or deployment choice may silently become Baobab's canonical enterprise model.**

That is the boundary that keeps the platform replaceable, polyglot and capable of growing across businesses, markets, countries and future product lines without rebuilding its foundations.
