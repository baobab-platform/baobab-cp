# ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model

**Status:** Proposed — Normative Implementation Architecture  
**Date:** 2026-09-10  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Repository:** `baobab-platform/baobab-cp`  
**Primary Runtime Owner:** `baobab-platform/baobab-cp`  
**Contract Authority:** `baobab-platform/shared`  
**Depends On:**  
- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts  
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model  
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model  

**Applies To:** Capability registry, capability grants, scopes, capability providers, engine instances, bindings, deterministic resolution, caching, revocation, readiness, audit and persistence  
**Architecture Style:** Fail-closed, deterministic, temporal, context-aware, capability-centric control plane  
**Target Runtime:** Go  
**Database:** PostgreSQL 17  
**Decision Type:** Foundational implementation architecture

---

# 1. Executive Decision

The Baobab Control Plane SHALL implement capability availability through five first-class runtime concepts:

```text
Capability
CapabilityGrant
CapabilityScope
CapabilityProvider
CapabilityBinding
```

and SHALL produce a sixth first-class result:

```text
CapabilityResolution
```

The resolution algorithm SHALL be deterministic, temporal, scope-aware, entitlement-aware, provider-aware, contract-aware, health-aware, region-aware, isolation-aware and fail-closed.

The following statements SHALL remain distinct:

```text
Capability exists.
Tenant is entitled to capability.
Provider can implement capability.
Binding makes provider eligible in this context.
Resolution selects exactly one provider instance.
```

No one statement SHALL imply another.

The Control Plane SHALL never resolve a provider merely because:

- the provider exists;
- an engine supports the capability;
- a binding exists;
- a tenant has a subscription;
- a Digital Estate can reach the engine.

A successful resolution SHALL require every applicable control to pass.

---

# 2. Architectural Intent

The runtime model SHALL answer this question:

> Given an authenticated principal, tenant, legal entity, Digital Estate, requested capability and operation context, is the capability permitted, and if so, which compatible provider instance SHALL serve it?

The canonical pipeline is:

```text
Identity
   │
   ▼
Context
   │
   ▼
Capability
   │
   ▼
Grant
   │
   ▼
Scope
   │
   ▼
Provider Candidates
   │
   ▼
Bindings
   │
   ▼
Eligibility
   │
   ▼
Precedence
   │
   ▼
Exactly One Provider
   │
   ▼
Resolution
```

---

# 3. Core Runtime Aggregate Model

The recommended logical model is:

```text
Capability
   │
   ├──── CapabilityDependency
   │
   ├──── CapabilityGrant ───── CapabilityScope
   │
   └──── CapabilityProvider
                 │
                 └──── CapabilityBinding ───── CapabilityScope
                                      │
                                      └──── EngineInstance
```

Supporting runtime concepts include:

```text
ProductSubscription
CapabilityComposition
CapabilityCompositionMember
ProviderContract
ProviderHealth
CapabilityResolution
ResolutionReason
ResolutionAudit
```

---

# 4. Capability Registry

The Control Plane SHALL maintain an authoritative runtime projection of the canonical capability registry defined in Shared.

Conceptual entity:

```text
Capability
├── id
├── canonical_key
├── name
├── description
├── domain
├── maturity
├── lifecycle_status
├── schema_version
├── current_contract_major
├── metadata
├── created_at
├── updated_at
└── version
```

The runtime registry SHALL NOT redefine canonical semantics.

Its purpose is to support efficient runtime resolution.

---

# 5. Canonical Key Rules

Capability keys SHALL use canonical Shared keys.

Examples:

```text
commerce.order.create
commerce.pricing.negotiated
supplier.onboarding.manage
finance.invoice.issue
logistics.shipment.track
intelligence.fx.query
```

The Control Plane SHALL NOT accept arbitrary tenant-defined capability keys into the canonical registry without architecture-approved registration.

---

# 6. Capability Lifecycle

Recommended runtime lifecycle values:

```text
DRAFT
ACTIVE
SUSPENDED
DEPRECATED
RETIRED
```

Only capabilities in eligible lifecycle states SHALL resolve.

Default rule:

```text
ACTIVE       -> resolvable
DEPRECATED   -> resolvable where policy allows
DRAFT        -> not production resolvable
SUSPENDED    -> denied
RETIRED      -> denied
```

The exact production eligibility matrix SHALL be configurable only through governed platform policy.

---

# 7. Capability Maturity

Maturity SHALL remain distinct from lifecycle:

```text
EXPERIMENTAL
PREVIEW
SUPPORTED
DEPRECATED
RETIRED
```

Production tenants SHALL not automatically receive `EXPERIMENTAL` or `PREVIEW` capabilities.

Explicit grant or environment policy SHALL be required.

---

# 8. Capability Dependencies

A capability MAY depend on other capabilities.

Conceptual entity:

```text
CapabilityDependency
├── id
├── capability_id
├── depends_on_capability_id
├── dependency_type
├── minimum_contract_version
├── maximum_contract_version
├── condition
├── effective_from
├── effective_to
└── status
```

Supported dependency types:

```text
REQUIRED
OPTIONAL
CONDITIONAL
```

Required dependency graphs SHALL be acyclic.

---

# 9. Capability Grant

A CapabilityGrant SHALL represent entitlement.

Conceptual schema:

```text
CapabilityGrant
├── id
├── tenant_id
├── capability_id
├── scope_id
├── source_type
├── source_reference
├── status
├── effective_from
├── effective_to
├── constraints
├── granted_by
├── revoked_at
├── revoked_by
├── version
├── created_at
└── updated_at
```

A grant SHALL answer:

```text
May this tenant/context consume this capability?
```

It SHALL NOT answer:

```text
Which provider serves it?
```

---

# 10. Grant Sources

Supported grant sources SHOULD include:

```text
PLATFORM_BASELINE
PRODUCT_SUBSCRIPTION
CONTRACT
TRIAL
MANUAL_APPROVAL
INTERNAL_POLICY
MIGRATION
```

The grant SHALL retain provenance back to its source.

Example:

```text
source_type       = PRODUCT_SUBSCRIPTION
source_reference  = subscription_123
```

---

# 11. Grant Lifecycle

Recommended status values:

```text
PENDING
ACTIVE
SUSPENDED
REVOKED
EXPIRED
```

Only effective `ACTIVE` grants SHALL satisfy resolution.

A grant SHALL be considered effective only if:

```text
status = ACTIVE
AND effective_from <= resolution_time
AND (effective_to IS NULL OR effective_to > resolution_time)
```

---

# 12. Grant Revocation

Revocation SHALL be explicit.

A revoked grant SHALL remain historically queryable.

It SHALL NOT be deleted merely to remove access.

Recommended fields:

```text
revoked_at
revoked_by
revocation_reason
```

Revocation SHALL trigger:

```text
cache invalidation
audit event
capability grant event
readiness recomputation
```

---

# 13. CapabilityScope

CapabilityScope SHALL represent applicability dimensions.

Conceptual structure:

```text
CapabilityScope
├── id
├── tenant_id
├── legal_entity_id
├── organisation_id
├── business_unit_id
├── digital_estate_id
├── digital_property_id
├── channel_id
├── market_id
├── jurisdiction
├── currency_code
├── customer_segment_id
├── catalogue_id
├── operating_region_id
├── geographic_region_id
├── deployment_region
├── environment
├── isolation_profile_id
├── include_countries[]
├── exclude_countries[]
├── metadata
├── created_at
└── updated_at
```

Tenant scope SHALL be mandatory.

Other dimensions MAY be null.

---

# 14. Scope Rule

An unspecified scope dimension SHALL mean:

```text
not further restricted on this dimension
```

It SHALL NOT mean:

```text
wildcard bypassing all other restrictions
```

Every populated dimension SHALL have to match the resolved context according to canonical matching semantics.

---

# 15. Include and Exclude Rules

Exclusion SHALL take precedence over inclusion.

Example:

```text
include_countries:
  - ZA
  - BW
  - ZM

exclude_countries:
  - BW
```

Then Botswana SHALL be ineligible.

The resolver SHALL not treat contradictory scope definitions as valid configuration.

Validation SHOULD reject such states before activation.

---

# 16. Scope Specificity

Capability resolution SHALL compare candidate binding specificity.

Specificity SHALL be deterministic.

A simple conceptual scoring model MAY be:

```text
tenant               +1
legal_entity         +8
digital_estate       +8
digital_property     +4
channel              +4
market               +8
jurisdiction         +4
currency             +2
customer_segment     +2
region               +4
environment          +4
isolation_profile    +8
```

However, ADR-BCP-003 does NOT freeze these numeric values.

The actual algorithm SHALL be defined in Shared as a canonical ordered rule or explicit score model.

The required outcome is:

```text
more specific applicable binding > less specific applicable binding
```

---

# 17. Exact-Match Over Generic

Example:

```text
Binding A:
tenant = ZuriBeans

Binding B:
tenant = ZuriBeans
market = South Africa
```

For a South African context:

```text
Binding B
```

SHALL outrank Binding A.

---

# 18. Priority vs Specificity

Priority SHALL NOT casually override scope specificity.

Recommended evaluation sequence:

```text
1. Validity
2. Scope compatibility
3. Specificity
4. Binding mode
5. Explicit priority
6. Deterministic tie evaluation
```

Priority SHALL operate among otherwise comparable eligible bindings.

---

# 19. Ambiguity

If two or more bindings remain equally valid after every deterministic comparison rule:

```text
RESOLUTION SHALL FAIL
```

Reason:

```text
BINDING_AMBIGUOUS
```

The resolver SHALL never choose:

```text
first row
latest inserted
lowest UUID
random candidate
```

as a hidden tie-breaker.

---

# 20. Capability Provider

A CapabilityProvider SHALL represent an implementation capable of satisfying one or more canonical capabilities.

Conceptual entity:

```text
CapabilityProvider
├── id
├── provider_key
├── name
├── provider_type
├── engine_id
├── lifecycle_status
├── ownership
├── metadata
├── created_at
└── updated_at
```

Provider types MAY include:

```text
BAOBAB_ENGINE
EXTERNAL_SERVICE
PLATFORM_NATIVE
ADAPTER
```

---

# 21. Provider Capability Support

Provider support SHALL be explicit.

Conceptual table:

```text
CapabilityProviderSupport
├── provider_id
├── capability_id
├── contract_version
├── status
├── effective_from
├── effective_to
└── metadata
```

A provider SHALL not be considered eligible for a capability merely because it belongs to an engine commonly associated with that capability.

---

# 22. Provider Contract

A provider SHALL advertise which contract versions it supports.

Example:

```text
Provider:
baobab-trade.medusa

Capability:
commerce.order.create

Contracts:
1.0
1.1
```

Provider compatibility SHALL be checked before binding resolution succeeds.

---

# 23. Engine and Provider Separation

Engine describes technology/topology.

Provider describes capability implementation.

Example:

```text
Engine:
MedusaJS

Provider:
baobab-trade.medusa-commerce

Capabilities:
commerce.order.create
commerce.catalogue.read
inventory.availability.read
```

This enables one engine family to expose multiple provider abstractions if necessary.

---

# 24. CapabilityBinding

CapabilityBinding SHALL connect:

```text
Capability
       │
       ▼
Provider
       │
       ▼
EngineInstance
       │
       ▼
Scope
```

Conceptual schema:

```text
CapabilityBinding
├── id
├── capability_id
├── provider_id
├── engine_instance_id
├── scope_id
├── binding_mode
├── priority
├── status
├── contract_version
├── effective_from
├── effective_to
├── configuration
├── version
├── created_at
└── updated_at
```

---

# 25. Binding Lifecycle

Recommended states:

```text
DRAFT
ACTIVE
SUSPENDED
DEPRECATED
RETIRED
```

Only eligible effective bindings SHALL enter provider selection.

---

# 26. Binding Modes

The Control Plane SHALL support at least:

```text
PRIMARY
FALLBACK
SHADOW
MIGRATION
DISABLED
```

## PRIMARY

Normal authoritative provider candidate.

## FALLBACK

Candidate only under explicitly defined fallback conditions.

## SHADOW

May receive non-authoritative shadow execution where security and privacy permit.

SHADOW SHALL never determine normal business result.

## MIGRATION

Temporary controlled provider during cutover.

## DISABLED

Excluded from resolution.

---

# 27. Fallback Semantics

Fallback SHALL NOT mean:

```text
"If anything goes wrong, try something else."
```

Fallback SHALL be governed.

A fallback may activate only for specified reasons such as:

```text
PRIMARY_UNAVAILABLE
PRIMARY_DEGRADED
REGION_FAILURE
```

It SHALL NOT bypass:

```text
entitlement
scope
contract compatibility
residency
isolation
authorization
```

---

# 28. Provider Health

Provider lifecycle and health SHALL remain separate.

Recommended health states:

```text
UNKNOWN
HEALTHY
DEGRADED
UNAVAILABLE
```

Provider health SHOULD be stored separately from long-lived registry data.

---

# 29. Health Eligibility

By default:

```text
HEALTHY       -> eligible
DEGRADED      -> policy-dependent
UNAVAILABLE   -> ineligible
UNKNOWN       -> fail closed for critical production capability
```

Exact policy MAY vary by capability criticality.

---

# 30. Engine Instance Eligibility

An EngineInstance SHALL be eligible only if all applicable checks succeed:

```text
correct provider
correct capability
correct environment
compatible region
compatible residency
compatible isolation
active lifecycle
acceptable health
effective time window
supported contract version
scope match
```

---

# 31. CapabilityResolution

A successful resolution SHALL be persisted or audit-recorded sufficiently to reconstruct the decision.

Conceptual response:

```text
CapabilityResolution
├── resolution_id
├── context_id
├── principal_id
├── tenant_id
├── legal_entity_id
├── digital_estate_id
├── capability_id
├── capability_key
├── capability_contract_version
├── grant_id
├── binding_id
├── provider_id
├── engine_id
├── engine_instance_id
├── effective_configuration
├── isolation_profile_id
├── deployment_region
├── resolved_at
├── expires_at
├── correlation_id
├── decision
├── reason_code
└── provenance
```

---

# 32. Resolution SHALL Be Immutable

Once issued, a resolution record SHALL represent the facts used at that point in time.

Subsequent:

```text
grant change
binding change
provider health change
```

SHALL NOT rewrite historical resolution records.

---

# 33. Resolution Time

The server SHALL establish:

```text
resolved_at
```

Clients SHALL not provide the authoritative resolution timestamp.

Historical replay endpoints MAY accept an explicit evaluation time only under separate privileged audit semantics.

---

# 34. Full Deterministic Resolution Algorithm

The normative conceptual algorithm SHALL be:

```text
START
  │
  ▼
Validate request contract
  │
  ├── invalid -> DENY
  ▼
Validate authenticated principal
  │
  ├── invalid -> DENY
  ▼
Resolve canonical identity
  │
  ├── revoked/disabled -> DENY
  ▼
Resolve tenant
  │
  ├── unknown/inactive -> DENY
  ▼
Resolve legal entity
  │
  ├── invalid -> DENY
  ▼
Resolve Digital Estate
  │
  ├── invalid/not permitted -> DENY
  ▼
Construct immutable platform context
  │
  ▼
Resolve capability key
  │
  ├── unknown -> DENY
  ├── inactive -> DENY
  ▼
Find active effective grants
  │
  ├── none -> DENY
  ▼
Match grant scopes
  │
  ├── none -> DENY
  ▼
Resolve grant ambiguity
  │
  ├── conflicting -> DENY / AMBIGUOUS
  ▼
Validate capability dependencies
  │
  ├── unsatisfied -> FAIL CLOSED
  ▼
Find provider support
  │
  ▼
Find active effective bindings
  │
  ▼
Match binding scopes
  │
  ▼
Filter contract compatibility
  │
  ▼
Filter environment
  │
  ▼
Filter residency
  │
  ▼
Filter isolation
  │
  ▼
Filter provider lifecycle
  │
  ▼
Filter engine lifecycle
  │
  ▼
Filter health
  │
  ▼
Evaluate binding mode
  │
  ▼
Evaluate specificity
  │
  ▼
Evaluate priority
  │
  ▼
Exactly one winner?
  │
  ├── zero -> FAIL CLOSED
  ├── multiple -> AMBIGUOUS
  ▼
Produce resolution
  │
  ▼
Audit
  │
  ▼
RETURN
```

---

# 35. Grant Ambiguity

Unlike bindings, multiple compatible grants MAY legitimately coexist if they grant the same capability.

Example:

```text
platform baseline grant
+
enterprise product grant
```

The resolver MAY treat entitlement as satisfied when at least one effective compatible grant exists.

However, conflicting constraints SHALL be handled by explicit policy.

The default security rule SHOULD be:

```text
deny constraints take precedence
```

---

# 36. Negative Grants

The initial implementation SHOULD avoid arbitrary negative grants unless a real requirement exists.

If negative grants are introduced later, precedence SHALL be explicit.

A deny SHALL never accidentally be weaker than a general allow.

---

# 37. Grant Constraints

Grant constraints MAY include:

```text
max transaction value
allowed markets
allowed channels
usage ceiling
environment restriction
trial limits
```

However, business-domain policy SHALL NOT be pushed into grants merely because a constraint field exists.

Example:

```text
"may issue invoices only below ZAR 10,000"
```

is likely domain authorization, not platform entitlement.

---

# 38. Provider Selection SHALL Not Depend on Client Preference by Default

Clients SHALL NOT generally request:

```text
provider = medusa
```

or:

```text
engine_instance = x
```

for ordinary business requests.

Provider selection is a Control Plane concern.

Explicit provider override SHALL require privileged administrative or migration use cases.

---

# 39. Effective Configuration

Binding configuration MAY specialise provider behaviour.

Example:

```text
sales_channel_id
warehouse_reference
provider_account_reference
catalogue_reference
```

The resolution output MAY expose non-secret effective configuration required by the consumer.

Secrets SHALL remain secret references.

---

# 40. Configuration Precedence

If configuration may exist at several levels:

```text
provider default
binding configuration
tenant configuration
estate configuration
```

the merge order SHALL be explicitly defined.

Recommended:

```text
platform/provider defaults
        <
tenant-level binding config
        <
more-specific binding config
```

No undocumented deep-merge semantics SHALL exist.

---

# 41. Product Subscription Relationship

ProductSubscription SHALL remain a commercial/control-plane concept.

Conceptually:

```text
ProductSubscription
       │
       ▼
CapabilityComposition
       │
       ▼
CapabilityRequirements
       │
       ▼
CapabilityGrants
```

The product subscription SHALL not be consulted directly for every provider-selection decision once grants are materialised.

---

# 42. Composition Expansion

Composition expansion SHALL happen:

```text
at provisioning
on subscription change
on composition version upgrade
on explicit reconciliation
```

not necessarily on every runtime request.

The resulting grants SHOULD be materialised for efficient evaluation.

---

# 43. Composition Upgrade

When a composition evolves:

```text
v1 -> v2
```

CP SHALL determine:

```text
added capabilities
removed capabilities
changed version constraints
new dependencies
deprecated capabilities
```

before activation.

---

# 44. Mandatory Capability Readiness

A tenant/product SHALL not be marked fully ready if mandatory capabilities cannot resolve.

Example:

```text
Baobab XBT
├── contract.manage       READY
├── invoice.manage        READY
├── shipment.manage       NOT_READY
└── market.query          READY
```

Overall:

```text
NOT_READY
```

if shipment management is mandatory.

---

# 45. Readiness Aggregate

Recommended runtime model:

```text
CapabilityReadiness
├── tenant_id
├── digital_estate_id
├── composition_id
├── capability_id
├── status
├── reason_code
├── last_evaluated_at
└── resolution_reference
```

Suggested states:

```text
READY
DEGRADED
NOT_READY
UNKNOWN
```

---

# 46. Caching

The resolver MAY cache successful decisions.

The cache key SHALL include every security- and routing-relevant dimension.

At minimum:

```text
principal
tenant
legal entity
Digital Estate
channel
market
capability
environment
deployment region
contract requirement
scope-relevant operation dimensions
```

Omission of a security-relevant dimension SHALL be treated as a defect.

---

# 47. Positive Cache TTL

Successful resolutions SHALL use short, bounded TTLs.

Long-lived caches SHALL be avoided because:

```text
grants revoke
bindings change
provider health changes
identity revokes
tenant suspends
```

The exact TTL SHALL be based on operational requirements.

---

# 48. Negative Caching

Negative decisions MAY be cached briefly to prevent repeated expensive invalid requests.

However:

```text
DENY
```

SHALL not become a long-lived stale state where a new valid grant has been added.

---

# 49. Revocation Events

The following events SHOULD invalidate relevant resolution cache entries:

```text
identity.disabled
identity.suspended
identity.revoked

tenant.suspended
tenant.decommissioned

capability.grant.revoked
capability.grant.suspended

capability.binding.suspended
capability.binding.retired

capability.provider.retired

engine_instance.unavailable
```

---

# 50. Cache Fail-Safe Rule

When invalidation state is uncertain:

```text
re-resolve
```

shall be preferred over continuing to trust stale privileged access.

---

# 51. Persistence Design

The capability schema SHOULD contain:

```text
capability.capability
capability.capability_dependency
capability.capability_scope
capability.capability_grant
capability.capability_provider
capability.capability_provider_support
capability.capability_binding
capability.capability_composition
capability.capability_composition_member
capability.capability_resolution
capability.capability_readiness
```

Supporting topology remains under:

```text
topology.engine
topology.engine_instance
topology.engine_instance_contract
topology.engine_instance_health
```

---

# 52. Suggested `capability.capability`

Conceptual PostgreSQL structure:

```sql
CREATE TABLE capability.capability (
    id                  uuid PRIMARY KEY,
    capability_key      text NOT NULL UNIQUE,
    name                text NOT NULL,
    description         text,
    domain_key          text NOT NULL,

    lifecycle_status    text NOT NULL,
    maturity            text NOT NULL,

    schema_version      integer NOT NULL,
    contract_metadata   jsonb NOT NULL DEFAULT '{}'::jsonb,

    version             bigint NOT NULL DEFAULT 1,

    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL
);
```

---

# 53. Suggested `capability.capability_scope`

Conceptual:

```sql
CREATE TABLE capability.capability_scope (
    id                    uuid PRIMARY KEY,
    tenant_id             text NOT NULL,
    legal_entity_id       text,
    organisation_id       text,
    business_unit_id      text,
    digital_estate_id     text,
    digital_property_id   text,
    channel_id            text,
    market_id             text,
    jurisdiction          text,
    currency_code         char(3),
    customer_segment_id   text,
    catalogue_id          text,
    operating_region_id   text,
    geographic_region_id  text,
    deployment_region     text,
    environment           text,
    isolation_profile_id  uuid,

    include_countries     text[] NOT NULL DEFAULT '{}',
    exclude_countries     text[] NOT NULL DEFAULT '{}',

    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL
);
```

Exact identifier types SHALL conform to Shared canonical contracts.

---

# 54. Suggested `capability.capability_grant`

Conceptual:

```sql
CREATE TABLE capability.capability_grant (
    id                 uuid PRIMARY KEY,
    tenant_id          text NOT NULL,
    capability_id      uuid NOT NULL,
    scope_id           uuid NOT NULL,

    source_type        text NOT NULL,
    source_reference   text,

    status             text NOT NULL,

    effective_from     timestamptz NOT NULL,
    effective_to       timestamptz,

    constraints        jsonb NOT NULL DEFAULT '{}'::jsonb,

    granted_by         text,
    revoked_at         timestamptz,
    revoked_by         text,
    revocation_reason  text,

    version            bigint NOT NULL DEFAULT 1,

    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL,

    FOREIGN KEY(capability_id)
        REFERENCES capability.capability(id),

    FOREIGN KEY(scope_id)
        REFERENCES capability.capability_scope(id),

    CHECK(effective_to IS NULL OR effective_to > effective_from)
);
```

---

# 55. Suggested `capability.capability_provider`

Conceptual:

```sql
CREATE TABLE capability.capability_provider (
    id                uuid PRIMARY KEY,
    provider_key      text NOT NULL UNIQUE,
    name              text NOT NULL,
    provider_type     text NOT NULL,
    engine_id         uuid,

    lifecycle_status  text NOT NULL,

    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,

    version           bigint NOT NULL DEFAULT 1,

    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL
);
```

---

# 56. Suggested `capability.capability_provider_support`

Conceptual:

```sql
CREATE TABLE capability.capability_provider_support (
    provider_id        uuid NOT NULL,
    capability_id      uuid NOT NULL,
    contract_version   text NOT NULL,

    status             text NOT NULL,

    effective_from     timestamptz NOT NULL,
    effective_to       timestamptz,

    metadata           jsonb NOT NULL DEFAULT '{}'::jsonb,

    PRIMARY KEY (
        provider_id,
        capability_id,
        contract_version
    )
);
```

---

# 57. Suggested `capability.capability_binding`

Conceptual:

```sql
CREATE TABLE capability.capability_binding (
    id                  uuid PRIMARY KEY,

    capability_id       uuid NOT NULL,
    provider_id         uuid NOT NULL,
    engine_instance_id  uuid NOT NULL,
    scope_id             uuid NOT NULL,

    binding_mode        text NOT NULL,
    priority            integer NOT NULL DEFAULT 0,

    contract_version    text NOT NULL,

    status              text NOT NULL,

    effective_from      timestamptz NOT NULL,
    effective_to        timestamptz,

    configuration       jsonb NOT NULL DEFAULT '{}'::jsonb,

    version             bigint NOT NULL DEFAULT 1,

    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,

    CHECK(effective_to IS NULL OR effective_to > effective_from)
);
```

---

# 58. Temporal Range Optimisation

PostgreSQL range types SHOULD be considered.

Example:

```text
tstzrange(effective_from, effective_to, '[)')
```

may support efficient overlap and exclusion constraints.

The implementation SHOULD prevent contradictory overlapping `PRIMARY` bindings where deterministic resolution would otherwise be impossible.

---

# 59. Database-Level Invariants

Where practical, PostgreSQL constraints SHALL enforce:

```text
valid effective ranges
valid lifecycle enums
unique capability key
unique provider key
positive versions
foreign-key integrity
no self dependency
valid composition membership
no contradictory bindings where representable
```

Application logic SHALL not be the only defence against invalid topology.

---

# 60. Soft Delete

Capability control-plane records SHOULD generally use lifecycle retirement rather than destructive deletion.

Historical relationships matter for audit and incident reconstruction.

---

# 61. Repository Interfaces

The Go implementation SHOULD expose repository interfaces such as:

```go
type CapabilityRepository interface
type CapabilityGrantRepository interface
type CapabilityProviderRepository interface
type CapabilityBindingRepository interface
type CapabilityCompositionRepository interface
```

Interfaces SHALL express domain operations rather than database implementation details.

---

# 62. Resolver Interfaces

Recommended conceptual split:

```go
type ContextResolver interface
type EntitlementResolver interface
type DependencyResolver interface
type BindingResolver interface
type ProviderEligibilityResolver interface
type CapabilityResolver interface
```

The outer `CapabilityResolver` MAY orchestrate the narrower components.

---

# 63. Resolver Responsibility

`CapabilityResolver` SHALL coordinate.

It SHALL NOT absorb persistence, HTTP, IAM token parsing or provider business logic directly.

---

# 64. Resolution Request

Conceptual Go model:

```go
type CapabilityResolutionRequest struct {
    PrincipalID       string
    TenantID          string
    LegalEntityID     string
    DigitalEstateID   string
    ChannelID         string
    MarketID          string
    CapabilityKey     string
    ContractVersion   string
    CorrelationID     string
    OperationScope    OperationScope
}
```

Exact shape SHALL come from Shared contracts.

---

# 65. OperationScope

OperationScope MAY carry resolution-relevant request dimensions such as:

```text
origin country
destination country
currency
jurisdiction
transaction type
```

Only explicitly approved fields SHALL influence platform resolution.

Arbitrary domain payloads SHALL not be passed into CP.

---

# 66. Resolution Response

Conceptual:

```go
type CapabilityResolution struct {
    ResolutionID      string
    ContextID         string
    CapabilityKey     string
    GrantID           string
    BindingID         string
    ProviderID        string
    EngineInstanceID  string
    ContractVersion   string
    ResolvedAt        time.Time
    ExpiresAt         time.Time
    CorrelationID     string
}
```

---

# 67. API Model

The platform SHOULD expose a primary capability-resolution boundary such as:

```text
POST /v1/capabilities/resolve
```

Request:

```json
{
  "tenant_id": "tn_xxx",
  "legal_entity_id": "...",
  "digital_estate_id": "zuribeans",
  "market_id": "...",
  "capability_key": "commerce.order.create",
  "contract_version": "1.0",
  "operation_scope": {},
  "correlation_id": "..."
}
```

Response:

```json
{
  "decision": "RESOLVED",
  "resolution_id": "...",
  "capability_key": "commerce.order.create",
  "provider_id": "...",
  "engine_instance_id": "...",
  "contract_version": "1.0",
  "expires_at": "..."
}
```

---

# 68. Context + Capability Resolution

CP MAY ultimately support a combined endpoint:

```text
POST /v1/context/resolve
```

that returns:

```text
resolved context
+
requested capability resolutions
```

This MAY reduce network overhead.

However, combining APIs SHALL NOT collapse domain semantics.

---

# 69. Batch Capability Resolution

The API SHOULD support resolving multiple capabilities in one context.

Example:

```json
{
  "capabilities": [
    "commerce.order.create",
    "inventory.availability.read",
    "payment.authorize"
  ]
}
```

Each SHALL receive an independent decision.

---

# 70. Partial Batch Failure

Batch resolution SHALL NOT hide per-capability failures.

Example:

```text
commerce.order.create           RESOLVED
inventory.availability.read     RESOLVED
payment.authorize               DENIED
```

The consumer decides whether the overall business operation can proceed.

---

# 71. Authorization Integration

The resolver SHALL accept only authenticated workload or trusted service requests.

IAM SHALL establish:

```text
principal
client
scope
assurance
```

CP SHALL establish:

```text
tenant
legal entity
estate
capability entitlement
provider resolution
```

Domain engines SHALL establish:

```text
business action permission
```

---

# 72. Digital Estate Rule

Browsers SHALL not normally resolve provider topology.

Digital Estate server-side components or domain APIs SHALL request capability resolution.

This prevents provider topology leakage and reduces client coupling.

---

# 73. Audit Model

Every resolution SHOULD produce sufficient structured audit data.

Example:

```text
resolution_id
principal_id
tenant_id
legal_entity_id
digital_estate_id
capability_key
grant_id
binding_id
provider_id
engine_instance_id
decision
reason_code
resolved_at
correlation_id
```

---

# 74. Denied Resolution Audit

Denied decisions SHALL also be observable.

This is necessary for:

```text
security incidents
misconfiguration
tenant support
entitlement disputes
provider outages
```

Sensitive failure details SHALL not be exposed to unprivileged callers.

---

# 75. Resolution Reason Codes

The implementation SHALL use Shared canonical reason codes.

Examples:

```text
GRANT_NOT_FOUND
GRANT_REVOKED
BINDING_NOT_FOUND
BINDING_AMBIGUOUS
PROVIDER_UNAVAILABLE
CONTRACT_VERSION_UNSUPPORTED
ISOLATION_POLICY_MISMATCH
RESIDENCY_POLICY_MISMATCH
```

---

# 76. Metrics

Recommended metrics:

```text
capability_resolution_total
capability_resolution_duration_seconds
capability_resolution_denied_total
capability_resolution_ambiguous_total
capability_provider_unavailable_total
capability_cache_hit_total
capability_cache_miss_total
```

Labels SHOULD include low-cardinality dimensions such as:

```text
capability
result
provider
region
reason_code
```

Tenant labels SHALL be reviewed carefully for cardinality and confidentiality.

---

# 77. Tracing

A capability resolution SHOULD appear as a trace span.

Useful attributes:

```text
capability.key
provider.key
engine.instance
resolution.result
reason_code
```

Secrets and private business data SHALL not be recorded.

---

# 78. Configuration Validation

Configuration changes that create:

```text
ambiguous bindings
unsatisfied mandatory dependencies
invalid provider versions
invalid isolation compatibility
invalid residency
```

SHOULD fail at write time where possible.

Runtime resolution SHALL still remain fail-closed as defence in depth.

---

# 79. Administrative Preview

CP SHOULD eventually support an administrative preview operation:

```text
What would resolve for this context?
```

without invoking a provider.

Example:

```text
tenant = ZuriBeans
market = ZA
capability = commerce.order.create
```

returns the candidate evaluation path.

This is valuable for debugging.

---

# 80. Explainability

Administrative resolution diagnostics SHOULD be able to show:

```text
Grant G matched.
Grant H rejected: market mismatch.
Binding B1 matched.
Binding B2 rejected: contract mismatch.
Binding B3 rejected: provider unavailable.
Binding B1 selected by higher specificity.
```

Such diagnostics SHALL require privileged access.

---

# 81. Provider Impact Analysis

The model SHALL support reverse queries:

```text
Provider
  ↓
Bindings
  ↓
Capabilities
  ↓
Tenants / Estates
```

This SHALL permit controlled maintenance and decommissioning.

---

# 82. Capability Impact Analysis

Likewise:

```text
Capability
  ↓
Compositions
  ↓
Grants
  ↓
Digital Estates
  ↓
Providers
```

This SHALL assist contract changes and deprecation.

---

# 83. Resolver Determinism Test Matrix

The implementation SHALL include tests for at least:

| Scenario | Expected |
|---|---|
| One valid grant, one valid binding | Resolve |
| No grant | Deny |
| Expired grant | Deny |
| Suspended grant | Deny |
| Valid grant, no binding | Fail closed |
| More-specific binding | Select specific |
| Equal-specificity equal-priority bindings | Ambiguous |
| Higher-priority comparable binding | Select higher priority |
| Unhealthy primary + valid configured fallback | Fallback |
| Unhealthy primary without fallback | Fail closed |
| Residency mismatch | Fail closed |
| Isolation mismatch | Fail closed |
| Contract mismatch | Fail closed |
| Wrong market | Fail closed |
| Wrong Digital Estate | Fail closed |
| Revoked tenant | Deny |
| Revoked identity | Deny |

---

# 84. ZuriBeans Example

Context:

```text
Tenant: ZuriBeans
Legal Entity: ZuriBeans Uganda
Estate: zuribeans
Channel: B2B
Market: Uganda
Capability: commerce.pricing.negotiated
```

Resolution:

```text
Capability exists                  YES
Grant exists                       YES
Grant scope matches                YES
Provider support exists            YES
Binding for Uganda exists          YES
Contract compatible                YES
Provider healthy                   YES
Isolation compatible               YES
Residency compatible               YES
Ambiguity                          NO

RESULT:
RESOLVED
```

---

# 85. Coal-Haulier Example

Context:

```text
Tenant: Coal Logistics Ltd
Legal Entity: South Africa
Origin: ZA
Transit: BW
Destination: ZM
Capability: logistics.route.manage
```

CP SHALL evaluate only cross-border dimensions relevant to capability routing.

It SHALL NOT require:

```text
Botswana legal entity
Zambia legal entity
```

unless tenant/legal rules explicitly require such context.

---

# 86. Thamani Example

Context:

```text
Tenant: Thamani
Estate: thamani
Channel: B2C
Market: ZA
Capability: commerce.checkout.execute
```

Its binding may select a commerce provider entirely different from a B2B capability used by ZuriBeans.

The resolver architecture SHALL support both without tenant-type branching.

---

# 87. Binding Override Example

Suppose:

```text
Tenant-wide:
commerce.order.create -> Provider A

South Africa:
commerce.order.create -> Provider B
```

For ZA:

```text
Provider B
```

shall win because the binding is more specific.

Outside ZA:

```text
Provider A
```

may remain applicable.

---

# 88. Region Example

Suppose:

```text
Provider A
region = africa-south
residency = ZA

Provider B
region = europe-west
```

If policy requires South African residency:

```text
Provider B
```

SHALL be ineligible even if technically healthy.

---

# 89. Multi-Tenant Isolation

The database and resolver SHALL enforce that:

```text
Tenant A grant
```

cannot satisfy:

```text
Tenant B request.
```

A missing tenant predicate in a query SHALL be treated as a security defect.

---

# 90. Transaction Boundaries

Mutation operations such as:

```text
grant creation
binding activation
binding retirement
provider registration
composition activation
```

SHALL be transactional.

Associated outbox events SHOULD be written in the same transaction.

---

# 91. Outbox

Capability lifecycle changes SHOULD use transactional outbox publishing.

Example:

```text
DB transaction
├── revoke grant
└── insert outbox event

COMMIT

publisher
└── capability.grant.revoked
```

This prevents state/event divergence.

---

# 92. Idempotency

Administrative mutation APIs SHALL support canonical idempotency where retry may occur.

Repeated provisioning SHALL not create duplicate grants or bindings.

---

# 93. Reconciliation

The reconciler SHALL periodically verify:

```text
expected grants exist
mandatory bindings exist
providers remain valid
provider contracts remain compatible
engine instances remain eligible
mandatory capabilities remain ready
```

Drift SHALL produce observable readiness failures.

---

# 94. Tenant Suspension

Tenant suspension SHALL effectively disable all capability resolutions.

No individual grant mutation SHALL be required.

This is an example of higher-order platform state taking precedence.

---

# 95. Legal Entity Suspension

If a legal entity becomes inactive:

```text
capabilities scoped to it
```

SHALL stop resolving.

Other legal entities under the same tenant MAY remain unaffected.

---

# 96. Digital Estate Suspension

A Digital Estate MAY be suspended independently where architecture permits.

Estate-scoped capabilities SHALL then fail resolution.

---

# 97. Provider Retirement

Provider retirement SHALL:

```text
prevent new bindings
invalidate active provider eligibility
trigger impact analysis
trigger readiness recalculation
emit lifecycle event
```

Migration SHOULD precede retirement wherever possible.

---

# 98. Capability Deprecation

Deprecation SHALL not necessarily deny immediately.

However:

```text
new compositions
new grants
new provider registrations
```

MAY be prevented according to deprecation policy.

---

# 99. Migration from Current Model

Because there is no production data, CP SHALL remodel rather than layer permanent adapters around incorrect pre-production assumptions.

The migration SHALL review:

```text
existing capabilities
capability bindings
product subscriptions
mapping scopes
contexts
engine topology
migrations
tests
seed data
```

Classify each:

```text
KEEP
RENAME
NORMALISE
SPLIT
MERGE
REMOVE
REBUILD
```

---

# 100. Do Not Preserve Semantic Mistakes

The following are examples where migration compatibility SHALL NOT trump correctness:

```text
binding implies entitlement
product equals engine
engine equals capability
Digital Estate equals tenant
market equals legal entity
provider health stored as lifecycle
```

These SHALL be corrected.

---

# 101. Implementation Gates

## Gate 0 — Existing-State Audit

Inventory current:

```text
Capability
CapabilityBinding
Context
MappingScope
ProductSubscription
Engine
EngineInstance
```

and compare against ADR-BCP-002, ADR-SHARED-007 and this ADR.

---

## Gate 1 — Domain Types

Implement:

```text
Capability
CapabilityDependency
CapabilityScope
CapabilityGrant
CapabilityProvider
ProviderSupport
CapabilityBinding
CapabilityResolution
CapabilityReadiness
```

---

## Gate 2 — Persistence

Create or remodel PostgreSQL structures.

---

## Gate 3 — Registry Services

Implement capability/provider registry and lifecycle.

---

## Gate 4 — Grant Services

Implement entitlement management and composition expansion.

---

## Gate 5 — Scope Matcher

Implement canonical scope semantics.

---

## Gate 6 — Provider Compatibility

Implement provider/contract compatibility.

---

## Gate 7 — Binding Resolver

Implement specificity, priority and ambiguity logic.

---

## Gate 8 — Health/Isolation/Residency

Integrate runtime eligibility.

---

## Gate 9 — Full Capability Resolver

Integrate all stages.

---

## Gate 10 — Caching and Revocation

Add bounded success caching and event-driven invalidation.

---

## Gate 11 — API

Implement OpenAPI contract generated/validated from Shared.

---

## Gate 12 — Audit/Events

Implement transactional outbox and structured audit.

---

## Gate 13 — Readiness and Diagnostics

Implement readiness and privileged explainability.

---

## Gate 14 — Reference Integrations

Validate:

```text
ZuriBeans
Thamani
coal-haulage model
petroleum-trader model
```

---

## Gate 15 — Hardening

Run:

```text
multi-tenant isolation tests
fuzzing
property-based resolution tests
load tests
race tests
revocation tests
failover tests
ambiguous-topology tests
```

---

# 102. Definition of Done

ADR-BCP-003 is implemented when:

1. capability grants are distinct from bindings;
2. bindings are distinct from provider support;
3. capability scopes are first-class;
4. provider support is explicit;
5. contract compatibility is enforced;
6. resolution is deterministic;
7. ambiguity fails closed;
8. scope specificity is canonical;
9. priority semantics are canonical;
10. provider health is separate from lifecycle;
11. residency and isolation affect eligibility;
12. resolution output is immutable;
13. revocation invalidates access correctly;
14. tenant suspension globally blocks capability resolution;
15. readiness can be computed;
16. reverse impact analysis is possible;
17. Shared contracts govern API shapes;
18. IAM remains coarse-grained;
19. domain engines retain business authorization;
20. all reference business models resolve without tenant-specific special cases.

---

# 103. Non-Goals

This ADR does not:

- define provider business logic;
- define ERP workflows;
- define Medusa internals;
- define logistics workflows;
- define customer approval policy;
- create a workflow engine;
- create a new XBT service;
- move authorization into Keycloak;
- make CP a general API gateway.

---

# 104. Architectural Invariants

The following are immutable design rules unless superseded by future ADR:

1. Capability existence does not imply entitlement.
2. Entitlement does not imply provider selection.
3. Provider support does not imply binding.
4. Binding does not imply entitlement.
5. Resolution requires all applicable controls.
6. Resolution is deterministic.
7. Ambiguity is an error.
8. Resolver is fail-closed.
9. Scopes are explicit.
10. Tenant isolation is mandatory.
11. Legal entity is distinct from tenant.
12. Digital Estate is distinct from tenant.
13. Provider is distinct from engine.
14. Provider health is distinct from lifecycle.
15. Domain business rules remain outside CP.
16. IAM business-role explosion is prohibited.
17. Product packaging remains separate from runtime capability identity.
18. No hidden resolver tie-breakers are permitted.

---

# 105. Final Target

The runtime capability spine SHALL become:

```text
              AUTHENTICATED REQUEST
                       │
                       ▼
                  PLATFORM CONTEXT
                       │
                       ▼
                    CAPABILITY
                       │
                       ▼
                CAPABILITY GRANT
                       │
                       ▼
                 CAPABILITY SCOPE
                       │
                       ▼
            CAPABILITY PROVIDER SET
                       │
                       ▼
                CAPABILITY BINDINGS
                       │
                       ▼
        CONTRACT / REGION / ISOLATION /
        RESIDENCY / HEALTH ELIGIBILITY
                       │
                       ▼
             DETERMINISTIC PRECEDENCE
                       │
                       ▼
               EXACTLY ONE PROVIDER
                       │
                       ▼
                ENGINE INSTANCE
                       │
                       ▼
              CAPABILITY RESOLUTION
```

The Control Plane SHALL thereby become capable of supporting many Digital Estates and many provider technologies without sacrificing tenant isolation, deterministic behaviour, auditability or domain boundaries.

---

# 106. Decision

**ACCEPTED TARGET IMPLEMENTATION ARCHITECTURE, subject to formal approval.**

Upon approval:

1. current capability/binding implementation SHALL be reconciled against this ADR;
2. pre-production persistence SHALL be remodelled where necessary;
3. Shared SHALL remain contract authority;
4. CP SHALL own runtime capability state;
5. capability grants SHALL become explicit;
6. provider support SHALL become explicit;
7. capability scopes SHALL become first-class;
8. deterministic resolution SHALL replace any implicit provider selection;
9. ambiguous topology SHALL fail closed;
10. capability readiness SHALL become a platform concept;
11. integration work SHALL validate against ZuriBeans, Thamani and materially different external operating models.

---

# 107. Architectural Maxim

> **A capability is available only when it exists, is granted, is contextually applicable, has a valid provider, has a valid binding, satisfies contract and policy constraints, and resolves deterministically to exactly one eligible engine instance.**