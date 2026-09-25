# ADR-BCP-006 — Capability Provider Lifecycle, Engine Topology, Health, Failover and Migration Model

**Status:** Proposed — Normative Platform Architecture  
**Date:** 2026-09-11  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Repository:** `baobab-platform/baobab-cp`  
**Runtime Authority:** `baobab-platform/baobab-cp`  
**Contract Authority:** `baobab-platform/shared`  
**Identity Authority:** `baobab-platform/baobab-iam`  

**Depends On:**
- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
- ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
- ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model

**Applies To:** Capability providers, engines, engine instances, provider contracts, health, region topology, lifecycle, failover, migration, replacement, canary, shadowing, retirement and operational recovery  
**Architecture Style:** Provider-neutral, topology-aware, health-aware, deterministic, replaceable, fail-closed  
**Decision Type:** Foundational runtime-provider architecture

---

# 1. Executive Decision

Baobab SHALL model capability implementation through four distinct runtime concepts:

```text
Capability
Provider
Engine
EngineInstance
```

These SHALL NOT be treated as aliases.

The canonical relationship SHALL be:

```text
Capability
    │
    ▼
CapabilityProvider
    │
    ▼
Engine
    │
    ▼
EngineInstance
```

A capability describes **what** Baobab can do.

A provider describes **which implementation offers that capability**.

An engine describes **the technology/runtime family**.

An engine instance describes **a deployable, region-specific, environment-specific runtime instance**.

The Control Plane SHALL be able to replace one provider or engine instance with another without forcing Digital Estates to change capability semantics.

The governing rule is:

> **Digital Estates depend on capabilities; the Control Plane depends on providers; providers depend on engines; operations depend on engine instances.**

---

# 2. Problem Statement

The platform currently includes or plans providers based on:

```text
MedusaJS
iDempiere
Payload CMS
Haystack
Keycloak
future logistics providers
future document providers
future payment providers
external APIs
```

Over time, any of these MAY need to be:

```text
upgraded
scaled
migrated
replaced
regionalised
isolated
degraded
failed over
retired
```

The architecture SHALL support these changes without exposing engine topology to Digital Estates.

---

# 3. Core Separation

The following SHALL remain distinct:

```text
Capability ≠ Provider
Provider ≠ Engine
Engine ≠ EngineInstance
EngineInstance ≠ Health
Health ≠ Lifecycle
Binding ≠ ProviderSupport
```

Each exists for a different reason.

---

# 4. Capability

Capability remains the business/platform primitive.

Example:

```text
commerce.order.create
finance.invoice.issue
content.page.read
intelligence.market.query
```

It SHALL NOT identify technology.

---

# 5. Capability Provider

A CapabilityProvider SHALL represent a Baobab-approved implementation of one or more canonical capabilities.

Conceptually:

```text
CapabilityProvider
├── id
├── provider_key
├── name
├── provider_type
├── engine_id
├── lifecycle_status
├── ownership
├── supported_regions
├── supported_isolation_profiles
├── metadata
├── created_at
└── updated_at
```

Examples:

```text
baobab-trade.medusa
baobab-erp.idempiere
baobab-cms.payload
baobab-pulse.haystack
```

These are provider identities.

They SHALL not become capability names.

---

# 6. Provider Type

Initial types MAY include:

```text
BAOBAB_ENGINE
EXTERNAL_SERVICE
PLATFORM_NATIVE
ADAPTER
```

Example:

```text
baobab-trade.medusa
type = BAOBAB_ENGINE
```

A sanctions-screening provider could be:

```text
external.sanctions.vendor-x
type = EXTERNAL_SERVICE
```

---

# 7. Engine

Engine SHALL represent the underlying technology/runtime family.

Conceptually:

```text
Engine
├── id
├── engine_key
├── name
├── engine_type
├── vendor
├── technology
├── lifecycle_status
├── metadata
└── version
```

Examples:

```text
medusa
idempiere
payload
haystack
keycloak
```

---

# 8. Engine Is Not Provider

One engine MAY support multiple providers.

For example:

```text
Medusa Engine
   │
   ├── commerce provider
   ├── inventory provider
   └── pricing provider
```

Likewise, a capability MAY be provided by multiple different engines.

This separation SHALL remain explicit.

---

# 9. Engine Instance

EngineInstance SHALL represent a concrete runtime deployment.

Conceptually:

```text
EngineInstance
├── id
├── engine_id
├── instance_key
├── environment
├── deployment_region
├── isolation_profile
├── residency_profile
├── endpoint_reference
├── credential_reference
├── lifecycle_status
├── version
├── configuration
├── created_at
└── updated_at
```

Examples:

```text
medusa-prod-za-shared-01
idempiere-prod-za-dedicated-zuri
haystack-prod-africa-south-01
```

---

# 10. Instance Identity

Engine instances SHALL have stable platform identities.

Their physical infrastructure MAY change underneath.

For example:

```text
engine_instance_id = ei_123
```

MAY continue across:

```text
pod replacement
node replacement
container replacement
host replacement
```

provided it remains the same logical service instance.

---

# 11. Provider Support

Provider support SHALL explicitly declare:

```text
provider
capability
contract versions
```

Conceptually:

```text
ProviderCapabilitySupport
├── provider_id
├── capability_id
├── supported_contract_versions
├── status
├── effective_from
├── effective_to
└── metadata
```

No implicit support SHALL be inferred from engine identity.

---

# 12. Provider Contract Compatibility

A provider SHALL declare exact capability contract compatibility.

Example:

```text
Provider:
baobab-trade.medusa

Capability:
commerce.order.create

Supports:
1.0
1.1
```

If a consumer requires:

```text
2.x
```

that provider SHALL be ineligible.

---

# 13. Engine Contract Compatibility

Engine instance compatibility MAY further constrain provider support.

Example:

```text
Provider supports contract 1.1
```

but a specific instance running an older adapter version may support only:

```text
1.0
```

The resolver SHALL evaluate actual instance compatibility.

---

# 14. Provider Lifecycle

Recommended provider lifecycle:

```text
DRAFT
ACTIVE
DEPRECATED
SUSPENDED
RETIRED
```

Only eligible lifecycle states SHALL participate in normal resolution.

---

# 15. Engine Lifecycle

Engine lifecycle MAY include:

```text
PLANNED
ACTIVE
DEPRECATED
SUSPENDED
RETIRED
```

Provider and engine lifecycle SHALL remain distinct.

---

# 16. Engine Instance Lifecycle

Recommended instance lifecycle:

```text
PROVISIONING
ACTIVE
DRAINING
MAINTENANCE
SUSPENDED
FAILED
DECOMMISSIONING
DECOMMISSIONED
```

This lifecycle SHALL reflect operational intent, not instantaneous health.

---

# 17. Lifecycle vs Health

This distinction is mandatory.

Example:

```text
Lifecycle: ACTIVE
Health: UNAVAILABLE
```

means the instance is intended to be active but currently unhealthy.

Example:

```text
Lifecycle: MAINTENANCE
Health: HEALTHY
```

means the instance responds technically but SHALL not receive normal traffic.

---

# 18. Health Model

Provider/instance health SHOULD use canonical states:

```text
UNKNOWN
HEALTHY
DEGRADED
UNAVAILABLE
```

---

# 19. Health Inputs

Health MAY be derived from:

```text
liveness
readiness
dependency availability
latency
error rate
queue lag
database connectivity
contract probe
provider-specific health checks
```

No single generic HTTP 200 SHALL be assumed sufficient for every provider.

---

# 20. Health Is Ephemeral

Health SHALL NOT be persisted as if it were long-lived configuration.

It SHOULD exist as ephemeral operational state.

Conceptually:

```text
EngineInstanceHealth
├── engine_instance_id
├── status
├── checked_at
├── expires_at
├── latency
├── reason
└── metadata
```

---

# 21. Health Expiry

Health data SHALL have bounded validity.

If health data becomes stale:

```text
HEALTH = UNKNOWN
```

SHALL be preferred over trusting stale information.

---

# 22. Capability Criticality and Health

Different capabilities MAY tolerate different health states.

Example:

```text
read-only intelligence query
```

may tolerate DEGRADED.

Whereas:

```text
payment authorization
```

may require HEALTHY.

Criticality SHALL be policy-driven.

---

# 23. Binding and Provider Relationship

CapabilityBinding SHALL select a Provider and EngineInstance for a scope.

Conceptually:

```text
Capability
      │
      ▼
Binding
      │
      ├── Provider
      └── EngineInstance
```

Provider support tells us:

```text
this provider can implement capability X
```

Binding tells us:

```text
this provider/instance should implement capability X here
```

---

# 24. Topology Model

Target topology:

```text
Capability
    │
    ▼
Provider
    │
    ▼
Engine
    │
    ▼
EngineInstance
    │
    ├── Environment
    ├── Region
    ├── Residency
    ├── Isolation
    └── Health
```

Binding overlays scope and precedence.

---

# 25. Region Awareness

An engine may have multiple instances:

```text
Medusa
├── ZA production instance
├── UG production instance
└── staging instance
```

Provider resolution MAY select among them according to:

```text
tenant context
market
residency
latency
isolation
environment
```

---

# 26. Region Is Not Market

This remains mandatory:

```text
DeploymentRegion ≠ Market
```

A ZA market request MAY be served by another region if residency and policy permit.

Conversely, a ZA-residency requirement MAY force a specific region even for a non-ZA market.

---

# 27. Isolation Compatibility

Each engine instance SHALL declare supported isolation characteristics.

Example:

```text
shared logical tenancy
dedicated schema
dedicated database
dedicated instance
```

A request requiring stronger isolation SHALL not resolve to a weaker instance.

---

# 28. Isolation Strength

The platform SHOULD define an ordered compatibility model where appropriate.

Conceptually:

```text
SHARED
   <
DEDICATED_SCHEMA
   <
DEDICATED_DATABASE
   <
DEDICATED_INSTANCE
```

Exact semantics SHALL be defined separately.

Higher isolation MAY satisfy lower requirements only where policy explicitly permits.

---

# 29. Data Residency Compatibility

Each engine instance SHALL declare residency characteristics.

Example:

```text
data_region = ZA
replication_regions = [ZA]
```

or:

```text
data_region = UG
```

Capability resolution SHALL reject residency-incompatible instances.

---

# 30. Provider Eligibility

A provider/instance SHALL be eligible only when all applicable checks pass:

```text
provider lifecycle
engine lifecycle
instance lifecycle
health
capability support
contract version
scope
environment
region
residency
isolation
effective dates
binding mode
```

---

# 31. Provider Eligibility Pipeline

```text
Candidate Provider
      │
      ▼
Provider Active?
      │ no -> reject
      ▼
Capability Supported?
      │ no -> reject
      ▼
Contract Compatible?
      │ no -> reject
      ▼
Engine Active?
      │ no -> reject
      ▼
Instance Active?
      │ no -> reject
      ▼
Environment Matches?
      │ no -> reject
      ▼
Residency Compatible?
      │ no -> reject
      ▼
Isolation Compatible?
      │ no -> reject
      ▼
Health Acceptable?
      │ no -> reject
      ▼
Eligible
```

---

# 32. Primary Provider

A binding with mode:

```text
PRIMARY
```

SHALL represent the normal authoritative provider.

It SHALL not automatically mean globally preferred.

Preference remains scope-specific.

---

# 33. Fallback Provider

`FALLBACK` SHALL represent a provider permitted to take over under defined failure conditions.

Fallback SHALL NOT bypass platform policy.

---

# 34. Fallback Conditions

Fallback activation MAY occur for explicit reasons such as:

```text
PRIMARY_UNAVAILABLE
PRIMARY_DEGRADED
REGION_UNAVAILABLE
PROVIDER_MAINTENANCE
```

The reason SHALL be recorded.

---

# 35. Fallback SHALL Not Hide Semantic Incompatibility

If fallback provider implements materially different behaviour outside canonical contract expectations:

```text
fallback SHALL NOT occur
```

Compatibility is mandatory.

---

# 36. Failover Decision

Conceptually:

```text
Resolve PRIMARY
    │
    ▼
Eligible?
  /   \
YES   NO
 │     │
 ▼     ▼
Use   Evaluate configured
      FALLBACK bindings
           │
           ▼
      Exactly one eligible?
        /       \
      YES       NO
       │         │
       ▼         ▼
     Use       Fail closed
```

---

# 37. Failover Audit

A failover SHALL record:

```text
original primary
failure reason
fallback provider
fallback instance
time
correlation ID
context
```

---

# 38. Automatic vs Manual Failover

Baobab MAY support both:

```text
AUTOMATIC
MANUAL
```

Critical capabilities MAY require manual failover depending on risk policy.

---

# 39. Failback

When primary provider recovers, the platform SHALL NOT necessarily immediately switch back.

Failback policy MAY include:

```text
health stability window
manual approval
traffic ramp
consistency check
```

This avoids oscillation.

---

# 40. Flapping Protection

Providers repeatedly switching between:

```text
HEALTHY
UNAVAILABLE
```

SHALL not cause uncontrolled provider oscillation.

The resolver/reconciler SHOULD use:

```text
hysteresis
minimum healthy duration
cooldown
```

where appropriate.

---

# 41. Shadow Provider

`SHADOW` SHALL permit controlled non-authoritative execution.

Conceptually:

```text
Request
   │
   ├────────► Primary
   │            │
   │            ▼
   │        Authoritative result
   │
   └────────► Shadow
                │
                ▼
           Comparison only
```

Shadow SHALL never determine the normal result.

---

# 42. Shadow Use Cases

Shadow mode MAY support:

```text
provider migration
contract conformance
performance comparison
new-engine validation
```

---

# 43. Shadow Restrictions

Shadow traffic SHALL respect:

```text
privacy
data residency
tenant policy
data classification
cost controls
```

Sensitive operations SHALL not be duplicated casually.

---

# 44. Migration Binding

`MIGRATION` binding mode SHALL support controlled provider replacement.

Example:

```text
Provider A
     │
     ▼
Migration
     │
     ▼
Provider B
```

---

# 45. Provider Migration Lifecycle

Recommended stages:

```text
DISCOVER
PLAN
PREPARE
SHADOW
CANARY
SHIFT
VALIDATE
RETIRE_OLD
COMPLETE
```

---

# 46. Discovery Stage

Before migration:

```text
Which capabilities?
Which tenants?
Which estates?
Which markets?
Which bindings?
Which contracts?
Which data?
Which provider-native mappings?
```

All SHALL be known.

---

# 47. Plan Stage

The migration plan SHALL define:

```text
source provider
target provider
capabilities
contexts
contract compatibility
data migration requirements
cutover strategy
rollback strategy
readiness criteria
```

---

# 48. Prepare Stage

Target provider SHALL be:

```text
registered
contract-compatible
provisioned
healthy
configured
bound
```

before receiving authoritative traffic.

---

# 49. Shadow Stage

Where safe, the target MAY receive shadow workload.

Comparison SHALL examine:

```text
response schema
response semantics
latency
errors
side effects
```

Mutating operations require extreme care and often cannot be blindly shadowed.

---

# 50. Canary Stage

A subset of eligible contexts MAY shift to the new provider.

For example:

```text
5% of tenants
specific Digital Estate
specific market
specific internal tenant
```

Canary SHALL be explicit in scope.

---

# 51. Canary Shall Use Deterministic Scope

The resolver SHALL NOT randomly route security-sensitive capability requests unless a formal traffic policy exists.

Preferred canary dimensions:

```text
tenant
legal entity
estate
market
region
explicit rollout cohort
```

---

# 52. Shift Stage

Traffic SHALL progressively move:

```text
Provider A 100%
Provider B   0%

↓

Provider A  90%
Provider B  10%

↓

Provider A  50%
Provider B  50%

↓

Provider A   0%
Provider B 100%
```

Only where capability semantics safely permit percentage-based rollout.

Otherwise deterministic cohort migration SHALL be used.

---

# 53. Stateful Capabilities

Stateful providers require special handling.

Examples:

```text
orders
inventory
finance
content
```

Migration SHALL account for system-of-record ownership.

The Control Plane SHALL NOT assume that routing alone migrates state.

---

# 54. Stateless vs Stateful Migration

Stateless example:

```text
currency-rate query
```

may be migrated mostly by binding changes.

Stateful example:

```text
ERP accounting
```

may require:

```text
data migration
reconciliation
cutover freeze
post-cutover verification
```

---

# 55. System-of-Record Rule

During migration, exactly one system SHALL remain authoritative for each mutable business domain unless an explicitly designed multi-writer architecture exists.

Default:

```text
single authoritative writer
```

---

# 56. Dual Write

Dual write SHALL NOT be introduced casually.

It requires a separate explicit architecture decision if used for authoritative domain state.

---

# 57. Provider Replacement Example — Trade

Suppose Baobab later replaces Medusa.

Digital Estate continues requesting:

```text
commerce.order.create
```

Migration:

```text
Digital Estate
      │
      ▼
commerce.order.create
      │
      ▼
CP
 ┌────┴────┐
 ▼         ▼
Medusa   New Provider
 old        new
```

The estate SHALL not change canonical capability semantics.

---

# 58. Provider Replacement Example — ERP

Suppose an ERP provider is replaced.

The capability remains:

```text
finance.invoice.issue
```

The provider may change from:

```text
iDempiere
```

to another ERP.

Canonical contract remains stable unless business semantics genuinely change.

---

# 59. Engine Upgrade

Engine upgrades SHALL be distinct from provider migration.

Example:

```text
Medusa version N
→
Medusa version N+1
```

Provider identity may remain unchanged if capability semantics remain compatible.

---

# 60. Instance Replacement

Instance replacement may occur without provider or engine change.

Example:

```text
medusa-prod-01
→
medusa-prod-02
```

This is topology migration.

---

# 61. Rolling Upgrade

Where supported:

```text
old instances
      │
      ▼
new instances join
      │
      ▼
health verified
      │
      ▼
bindings shift
      │
      ▼
old instances drain
```

---

# 62. Draining

An instance in:

```text
DRAINING
```

SHALL receive no new long-running work where avoidable.

Existing in-flight operations MAY complete.

---

# 63. Maintenance

`MAINTENANCE` SHALL exclude an instance from normal resolution unless explicit maintenance policy allows read-only or other constrained use.

---

# 64. Decommissioning

Decommission sequence SHOULD be:

```text
block new bindings
      │
      ▼
identify affected consumers
      │
      ▼
create replacement bindings
      │
      ▼
validate readiness
      │
      ▼
shift traffic
      │
      ▼
drain
      │
      ▼
retire
      │
      ▼
decommission
```

---

# 65. Provider Retirement

Provider retirement SHALL be blocked if unresolved active dependencies remain unless an explicit emergency override is authorised.

---

# 66. Impact Analysis

Before retirement, CP SHALL determine:

```text
Provider
  │
  ▼
Bindings
  │
  ▼
Capabilities
  │
  ▼
Grants
  │
  ▼
Tenants
  │
  ▼
Digital Estates
  │
  ▼
Products
```

---

# 67. Reverse Impact Query

Operators SHOULD be able to ask:

> If `baobab-trade.medusa` disappears, what breaks?

The system SHOULD answer with concrete affected:

```text
capabilities
tenants
estates
products
markets
regions
```

---

# 68. Capability Readiness and Provider Failure

Provider health changes SHALL influence readiness.

Example:

```text
commerce.order.create
Primary unavailable
Fallback healthy
```

Result:

```text
READY_DEGRADED
```

or equivalent governed status.

Without fallback:

```text
NOT_READY
```

---

# 69. Provider Readiness

Provider readiness differs from health.

Readiness answers whether the provider can satisfy the tenant/context requirements.

Example:

```text
Provider healthy
but no tenant configuration
```

Therefore:

```text
health = HEALTHY
readiness = NOT_READY
```

---

# 70. Provider Readiness Checks

May include:

```text
health
contract compatibility
tenant configuration
required mappings
region
residency
isolation
credential availability
required external dependencies
```

---

# 71. Dependency Health

Provider health MAY depend on downstream services.

Example:

```text
Medusa healthy
payment provider unavailable
```

For capability:

```text
commerce.catalogue.read
```

the provider may remain healthy.

For:

```text
commerce.checkout.execute
```

it may be degraded or unavailable.

Health MAY therefore be capability-specific.

---

# 72. Capability-Specific Health

The platform SHOULD permit:

```text
ProviderCapabilityHealth
```

when one provider supports multiple capabilities with differing operational states.

---

# 73. Health Hierarchy

Conceptually:

```text
EngineInstanceHealth
       │
       ▼
ProviderHealth
       │
       ▼
CapabilityHealth
       │
       ▼
Tenant Capability Readiness
```

These SHALL not be collapsed.

---

# 74. External Provider Model

External services SHALL participate through the same provider abstraction where appropriate.

Example:

```text
Capability:
counterparty.sanctions.screen

Provider:
external.vendor-x

Engine:
external-api

Instance:
vendor-x-production
```

This provides consistent resolution semantics.

---

# 75. External Provider SLA

External provider metadata MAY include:

```text
SLA class
region availability
rate limits
contract version
cost classification
```

These MAY inform selection policy where formally modelled.

---

# 76. Cost-Aware Routing

Cost SHALL NOT be introduced as an implicit resolver rule.

If provider cost influences selection, it SHALL be an explicit governed policy.

Security, residency, entitlement and compatibility SHALL take precedence.

---

# 77. Latency-Aware Routing

Likewise, latency MAY be a provider-selection factor only after all mandatory controls pass.

Fast but non-compliant SHALL never beat compliant.

---

# 78. Selection Order

Recommended conceptual order:

```text
1. Entitlement
2. Scope
3. Lifecycle
4. Contract compatibility
5. Environment
6. Residency
7. Isolation
8. Health
9. Binding mode
10. Specificity
11. Priority
12. Optional optimisation policy
```

No optimisation SHALL precede security/compliance constraints.

---

# 79. Provider Priority

Priority SHALL remain explicit integer or comparable ordered value.

It SHALL not become a substitute for binding specificity.

---

# 80. Deterministic Selection

Equal candidates after all rules SHALL yield:

```text
AMBIGUOUS
```

not random selection.

---

# 81. Stateful Failover

Failover for stateful systems SHALL be conservative.

For example:

```text
ERP primary fails
```

The presence of another healthy ERP instance SHALL not automatically make it safe to write there.

Replication state and authoritative-write safety must be known.

---

# 82. Failover Safety Class

Providers SHOULD declare failover safety classification.

Potential values:

```text
STATELESS_SAFE
READ_ONLY_SAFE
SYNCHRONOUS_REPLICA_SAFE
MANUAL_FAILOVER_REQUIRED
NO_FAILOVER
```

---

# 83. Failover Safety Example

```text
FX query:
STATELESS_SAFE
```

Possible automatic failover.

```text
ERP ledger posting:
MANUAL_FAILOVER_REQUIRED
```

unless a proven replicated architecture exists.

---

# 84. Write Authority

For write capabilities, provider topology SHALL identify authoritative write target.

Conceptually:

```text
WRITE_PRIMARY
READ_REPLICA
```

may be introduced where necessary.

---

# 85. Read Replica

Read-capability bindings MAY use read replicas where contract semantics allow.

Example:

```text
finance.invoice.read
```

could use a replica.

But:

```text
finance.invoice.issue
```

must resolve to authoritative write provider.

---

# 86. Binding Modes Extension

Future binding modes MAY include:

```text
WRITE_PRIMARY
READ_ONLY
REGION_PREFERRED
CANARY
```

Any new binding mode SHALL have formally defined semantics.

---

# 87. Configuration Drift

Provider configuration SHALL be reconciled.

Example:

```text
CP expects tenant sales channel X
Provider no longer has X
```

This SHALL produce drift:

```text
PROVIDER_CONFIGURATION_DRIFT
```

---

# 88. Credentials

Engine credentials SHALL not be embedded in bindings or resolution outputs.

Bindings SHALL reference secure credential identities.

Example:

```text
credential_ref = secret://...
```

or equivalent abstraction.

---

# 89. Credential Rotation

Credential rotation SHALL not require capability identity change.

Engine instance configuration SHALL support rotation independently.

---

# 90. Endpoint References

Raw provider URLs SHOULD not become canonical business identifiers.

Resolution MAY return an internal endpoint reference or routing key.

Where API gateway/service discovery is used, CP MAY resolve to:

```text
service reference
gateway route
provider identity
```

rather than expose physical URLs.

---

# 91. API Gateway Separation

The Control Plane SHALL NOT become the data-plane proxy merely because it knows provider topology.

Conceptually:

```text
CP
  decides where

Gateway / Service Mesh / Domain API
  carries traffic
```

---

# 92. Topology Reconciliation

CP SHALL declare desired provider topology.

Infrastructure tooling SHALL provision physical infrastructure.

Example:

```text
CP desired:
dedicated ERP instance in ZA
```

Infrastructure system:

```text
creates runtime
```

Then CP observes/reconciles it.

---

# 93. Infrastructure Boundary

CP SHALL not become a generic Kubernetes or cloud-management system.

It SHALL maintain platform desired state and references.

Infrastructure repositories/tooling own detailed infrastructure execution.

---

# 94. Engine Instance Registration

Engine instances SHALL be registered through controlled workflows.

Registration SHALL verify:

```text
engine identity
environment
region
isolation
residency
version
provider compatibility
health endpoint
credentials
```

---

# 95. Instance Attestation

Future architecture MAY require engine instances to attest identity/workload trust through IAM or infrastructure identity.

This SHOULD be compatible with workload identity principles already established in IAM ADRs.

---

# 96. Provider Registration Flow

```text
Provider Contract
      │
      ▼
Validate canonical capabilities
      │
      ▼
Validate contract versions
      │
      ▼
Associate Engine
      │
      ▼
Register Provider
      │
      ▼
Register Engine Instances
      │
      ▼
Validate Health
      │
      ▼
Eligible for Binding
```

---

# 97. Provider Cannot Self-Grant

A provider registration SHALL NOT automatically create:

```text
tenant grants
bindings
subscriptions
```

These remain separate control-plane concepts.

---

# 98. Engine Cannot Self-Assign Tenant

An engine instance SHALL not claim:

```text
I serve tenant X
```

as authoritative.

CP bindings determine scope.

---

# 99. Multi-Tenant Engine

A shared engine instance MAY serve multiple tenants if:

```text
isolation profile allows it
provider supports it
tenant policy allows it
residency permits it
```

Each tenant still receives separate bindings and context.

---

# 100. Dedicated Engine Instance

Some tenants/capabilities MAY require dedicated instances.

Example:

```text
Tenant A
finance.*
→ dedicated ERP instance
```

Other capabilities may remain shared.

---

# 101. Mixed Isolation

One tenant MAY use:

```text
Trade -> shared
ERP -> dedicated
Pulse -> shared
```

The model SHALL support this naturally.

---

# 102. Multi-Region Engine

An engine MAY have:

```text
Instance ZA
Instance UG
Instance EU
```

Bindings and context determine selection.

Digital Estates SHALL remain unaware of those details.

---

# 103. Disaster Recovery Instance

A DR instance SHALL not automatically receive normal traffic.

It MAY be represented through:

```text
FALLBACK
```

or a dedicated disaster-recovery topology state.

Activation SHALL follow provider failover policy.

---

# 104. Recovery Point and Recovery Time

Provider/engine topology MAY record operational objectives such as:

```text
RPO
RTO
```

These SHALL support operational governance.

They SHALL not replace actual tested recovery procedures.

---

# 105. Provider Backup Responsibility

CP may record backup capability/readiness metadata.

Actual backup execution belongs to infrastructure/provider operations.

---

# 106. Health Probe Contract

Shared SHOULD define a canonical health/readiness envelope.

Conceptually:

```json
{
  "status": "HEALTHY",
  "checked_at": "...",
  "capabilities": {
    "commerce.order.create": "HEALTHY",
    "inventory.availability.read": "DEGRADED"
  }
}
```

Provider-native health responses MAY be adapted into this canonical form.

---

# 107. Health Probe Security

Health endpoints SHALL not expose sensitive topology or tenant information publicly.

---

# 108. Operational Events

Recommended events include:

```text
provider.registered
provider.updated
provider.deprecated
provider.retired

engine.registered
engine.updated
engine.deprecated
engine.retired

engine-instance.provisioning
engine-instance.activated
engine-instance.degraded
engine-instance.unavailable
engine-instance.draining
engine-instance.retired

provider.failover.started
provider.failover.completed
provider.failback.completed

provider.migration.started
provider.migration.stage-changed
provider.migration.completed
provider.migration.failed
```

---

# 109. Health Events

Health events SHOULD be rate-controlled and deduplicated.

Repeated identical:

```text
UNAVAILABLE
```

events SHALL not flood the event bus unnecessarily.

---

# 110. Audit

Provider topology changes SHALL capture:

```text
actor
provider
engine
instance
capability
binding
old state
new state
reason
correlation ID
timestamp
```

---

# 111. Observability

Recommended metrics:

```text
provider_health_status
engine_instance_health_status
provider_failover_total
provider_failover_duration_seconds
provider_migration_total
provider_migration_duration_seconds
binding_resolution_by_provider_total
provider_contract_mismatch_total
provider_configuration_drift_total
```

---

# 112. Provider Dashboard

Operators SHOULD be able to see:

```text
Provider
Engine
Version
Instances
Regions
Health
Capabilities
Contract versions
Bindings
Tenants impacted
Lifecycle
```

---

# 113. Impact Example

```text
Provider:
baobab-trade.medusa

Capabilities:
commerce.order.create
commerce.catalogue.read
inventory.availability.read

Instances:
ZA prod
UG prod

Affected:
ZuriBeans
Thamani
```

This SHALL be derivable, not manually maintained.

---

# 114. Persistence Model

The existing topology schema SHOULD evolve around:

```text
topology.engine
topology.engine_instance
topology.engine_instance_health

capability.capability_provider
capability.capability_provider_support
capability.capability_binding

operations.provider_migration
operations.provider_failover
```

Exact schema names MAY vary.

---

# 115. Suggested Engine Table

Conceptually:

```sql
CREATE TABLE topology.engine (
    id                  uuid PRIMARY KEY,
    engine_key          text NOT NULL UNIQUE,
    name                text NOT NULL,
    engine_type         text NOT NULL,
    vendor              text,
    technology          text NOT NULL,
    lifecycle_status    text NOT NULL,
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,
    version             bigint NOT NULL DEFAULT 1,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL
);
```

---

# 116. Suggested Engine Instance Table

```sql
CREATE TABLE topology.engine_instance (
    id                    uuid PRIMARY KEY,
    engine_id             uuid NOT NULL,
    instance_key          text NOT NULL UNIQUE,

    environment           text NOT NULL,
    deployment_region     text NOT NULL,

    isolation_profile_id  uuid,
    residency_profile_id  uuid,

    endpoint_reference    text,
    credential_reference  text,

    lifecycle_status      text NOT NULL,
    software_version      text,

    configuration         jsonb NOT NULL DEFAULT '{}'::jsonb,

    version               bigint NOT NULL DEFAULT 1,

    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,

    FOREIGN KEY(engine_id)
        REFERENCES topology.engine(id)
);
```

---

# 117. Suggested Health Table

Health SHOULD be ephemeral, but if stored for operational use:

```sql
CREATE TABLE topology.engine_instance_health (
    engine_instance_id uuid PRIMARY KEY,
    health_status      text NOT NULL,
    checked_at         timestamptz NOT NULL,
    expires_at         timestamptz NOT NULL,
    reason_code        text,
    metrics            jsonb NOT NULL DEFAULT '{}'::jsonb
);
```

Historical health may belong in observability storage rather than transactional PostgreSQL.

---

# 118. Suggested Provider Table

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

# 119. Suggested Migration Table

Conceptually:

```sql
CREATE TABLE operations.provider_migration (
    id                 uuid PRIMARY KEY,
    capability_id      uuid NOT NULL,
    source_provider_id uuid NOT NULL,
    target_provider_id uuid NOT NULL,
    scope_id            uuid,
    stage               text NOT NULL,
    status              text NOT NULL,
    plan                jsonb NOT NULL,
    started_at          timestamptz,
    completed_at        timestamptz,
    failure_reason      text,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL
);
```

---

# 120. Provider Migration Plan

The plan SHOULD include:

```text
scope
source provider
target provider
capabilities
contract versions
migration mode
data strategy
canary strategy
validation
rollback
cutover window
owners
```

---

# 121. API Surface

Candidate administrative APIs:

```text
GET  /v1/providers
POST /v1/providers

GET  /v1/engines
POST /v1/engines

GET  /v1/engine-instances
POST /v1/engine-instances

POST /v1/providers/{id}/deprecate
POST /v1/providers/{id}/retire

POST /v1/engine-instances/{id}/drain
POST /v1/engine-instances/{id}/maintenance
POST /v1/engine-instances/{id}/activate

POST /v1/provider-migrations/plan
POST /v1/provider-migrations
GET  /v1/provider-migrations/{id}

POST /v1/provider-failover
POST /v1/provider-failback
```

Exact contracts SHALL reside in Shared.

---

# 122. Administrative Preview

Operators SHOULD be able to ask:

```text
If provider X is suspended,
which capability resolutions change?
```

before applying the change.

---

# 123. Change Plan

Potentially disruptive topology mutations SHOULD follow:

```text
PLAN
  │
  ▼
IMPACT ANALYSIS
  │
  ▼
VALIDATE
  │
  ▼
APPLY
  │
  ▼
OBSERVE
```

---

# 124. Emergency Changes

Emergency provider suspension MAY bypass normal planning.

Such action SHALL:

```text
require privileged authority
emit critical audit record
trigger readiness recalculation
trigger cache invalidation
```

---

# 125. Cache Invalidation

Provider lifecycle, binding, health and migration changes MAY invalidate cached capability resolutions.

At minimum:

```text
provider suspended
provider retired
binding changed
instance unavailable
instance draining
contract compatibility changed
residency/isolation changed
```

SHALL invalidate affected resolutions as required by policy.

---

# 126. Long-Running Operations

A capability resolution valid when an operation starts MAY become invalid mid-operation.

Domains SHALL define transaction semantics.

CP SHALL not interrupt business transactions blindly.

For long-running workflows, domains MAY revalidate capability/context at safe checkpoints.

---

# 127. Resolution Assertion Expiry

If short-lived resolution assertions are introduced later:

```text
provider changes
```

SHALL not need to invalidate an assertion already near expiry unless security policy requires immediate revocation.

This requires separate signed-resolution ADR design.

---

# 128. Domain-System Authority

The Control Plane SHALL select providers.

It SHALL NOT take over provider business state.

Example:

```text
CP knows:
iDempiere is the selected finance provider.

CP does not own:
journal entries.
```

---

# 129. Provider Adapter Boundary

A provider adapter SHALL translate:

```text
Canonical Capability Contract
            │
            ▼
Provider-Native Contract
```

Example:

```text
commerce.order.create
        │
        ▼
Medusa-native API
```

---

# 130. Adapter Responsibility

Adapters MAY handle:

```text
identifier translation
request transformation
response transformation
error mapping
contract version translation
```

They SHALL not silently alter business semantics.

---

# 131. Adapter Placement

Adapters SHOULD belong near the provider/domain implementation.

CP SHOULD NOT accumulate every provider-specific adapter and become an integration monolith.

---

# 132. Provider-Specific Logic in CP

The following SHALL be avoided:

```go
if provider == "medusa" { ... }
if provider == "idempiere" { ... }
```

inside generic capability resolution.

Provider-specific behaviour belongs behind interfaces/adapters.

---

# 133. Reference Topology — ZuriBeans

Example:

```text
ZuriBeans
   │
   ▼
Capability Grants
   │
   ├── commerce.* 
   │       │
   │       ▼
   │   Medusa Provider
   │       │
   │       ▼
   │   Medusa ZA/UG instance
   │
   ├── finance.*
   │       │
   │       ▼
   │   iDempiere Provider
   │       │
   │       ▼
   │   dedicated ERP instance
   │
   └── intelligence.*
           │
           ▼
       Haystack Provider
           │
           ▼
       shared Pulse instance
```

---

# 134. Reference Topology — Thamani

```text
Thamani
   │
   ├── commerce.*
   │       ▼
   │    Medusa
   │
   ├── content.*
   │       ▼
   │    Payload
   │
   ├── finance.*
   │       ▼
   │    iDempiere
   │
   └── intelligence.*
           ▼
        Haystack
```

Thamani and ZuriBeans MAY use the same provider technologies while remaining operationally independent.

---

# 135. Independent Legal Entities

Shared provider technology SHALL NOT imply shared business state.

For example:

```text
ZuriBeans
    \
     \ 
      Medusa platform
     /
    /
Thamani
```

does NOT mean:

```text
shared inventory
shared orders
shared customers
shared suppliers
```

unless explicitly modelled as legitimate cross-entity transactions.

---

# 136. External Customer Example

A coal-haulage customer could use:

```text
commercial.*   -> provider A
logistics.*    -> provider B
finance.*      -> iDempiere
intelligence.* -> Pulse
```

This demonstrates that Baobab capability composition does not require every domain to be implemented by the initial engines.

---

# 137. Provider Portability Goal

The architecture SHALL be considered successful when:

```text
Digital Estate
```

can remain unchanged while:

```text
Provider A
```

is replaced by:

```text
Provider B
```

provided both conform to the required canonical capability contract.

---

# 138. Non-Goals

This ADR does NOT:

- define provider business workflows;
- define cloud infrastructure implementation;
- make CP an API gateway;
- make CP a service mesh;
- define Kubernetes scheduling;
- define vendor-specific upgrade procedures;
- make automatic failover safe for all stateful systems;
- permit multi-writer domain state by default;
- make provider health synonymous with product readiness.

---

# 139. Rejected Alternatives

## Digital Estates call engines directly

Rejected as canonical architecture.

## Capability names include engine identity

Rejected.

## Provider support inferred from engine technology

Rejected.

## Health stored as provider lifecycle

Rejected.

## Random load balancing inside capability resolver

Rejected.

## Automatic failover for every provider

Rejected.

## Routing change assumed to migrate business state

Rejected.

## CP implements all provider adapters centrally

Rejected.

## One engine instance per tenant by default

Rejected.

## One shared engine instance for everything

Rejected.

Isolation is contextual.

---

# 140. Implementation Gates

## Gate 0 — Current Topology Audit

Inspect:

```text
Engine
EngineInstance
Capability
CapabilityBinding
health state
deployment region
isolation
provider concepts
```

Classify:

```text
KEEP
REMODEL
SPLIT
MERGE
REMOVE
```

---

## Gate 1 — Shared Provider Contracts

Implement canonical:

```text
CapabilityProvider
ProviderCapabilitySupport
EngineRef
EngineInstanceRef
Health
FailoverPolicy
MigrationPlan
```

---

## Gate 2 — Provider Registry

Implement provider lifecycle and capability support.

---

## Gate 3 — Engine Topology

Formalise engine and engine-instance models.

---

## Gate 4 — Health Model

Implement ephemeral health and capability-specific health where needed.

---

## Gate 5 — Eligibility Integration

Integrate:

```text
region
residency
isolation
health
contract
lifecycle
```

with capability resolution.

---

## Gate 6 — Fallback

Implement governed fallback semantics.

---

## Gate 7 — Impact Analysis

Implement provider/instance reverse dependency queries.

---

## Gate 8 — Migration Planner

Implement source/target migration plan and validation.

---

## Gate 9 — Shadow/Canary

Implement only where safe and justified.

---

## Gate 10 — Draining and Retirement

Implement controlled provider/instance retirement.

---

## Gate 11 — Topology Reconciliation

Integrate desired-state topology with infrastructure tooling.

---

## Gate 12 — Observability

Implement metrics, audit, tracing and readiness integration.

---

## Gate 13 — Reference Provider Migrations

Test at minimum:

```text
Medusa instance replacement
iDempiere maintenance/failover
Payload provider migration scenario
Pulse provider migration scenario
```

---

## Gate 14 — Resilience Tests

Test:

```text
provider unavailable
instance unavailable
health stale
fallback active
fallback unavailable
region failure
residency mismatch
isolation mismatch
contract mismatch
binding ambiguity
provider retirement
```

---

## Gate 15 — Stateful Migration Tests

Validate that routing changes cannot accidentally produce dual authoritative writers.

---

# 141. Definition of Done

ADR-BCP-006 SHALL be considered implemented when:

1. CapabilityProvider is first-class.
2. Engine is distinct from Provider.
3. EngineInstance is distinct from Engine.
4. Health is distinct from lifecycle.
5. Provider support is explicit.
6. Contract compatibility is enforced.
7. Region is explicit.
8. Residency is enforced.
9. Isolation is enforced.
10. Provider health affects eligibility.
11. Fallback behaviour is governed.
12. Failover is auditable.
13. Stateful failover is conservative.
14. Provider migrations can be planned.
15. Shadow/canary modes are explicit.
16. Instances can drain safely.
17. Providers can be retired safely.
18. Impact analysis is available.
19. Digital Estates remain provider-neutral.
20. Provider replacement does not require capability renaming.
21. Shared provider technology does not weaken tenant/legal-entity isolation.
22. No arbitrary tie-breaking exists.
23. CP does not become the data-plane proxy.
24. CP does not become infrastructure orchestration software.
25. System-of-record ownership remains with domain engines.

---

# 142. Final Target Architecture

```text
                    DIGITAL ESTATES
                         │
                         ▼
                 Canonical Capability
                         │
                         ▼
                  BAOBAB CONTROL PLANE
                         │
                Capability Resolution
                         │
          ┌──────────────┼───────────────┐
          │              │               │
          ▼              ▼               ▼
        Grant          Scope          Binding
                                         │
                                         ▼
                                CapabilityProvider
                                         │
                                         ▼
                                      Engine
                                         │
                         ┌───────────────┼───────────────┐
                         ▼               ▼               ▼
                      Instance A      Instance B      Instance C
                         │               │               │
                      Region ZA       Region UG        DR Region
                         │               │               │
                      Health          Health           Health
                         │
                         ▼
                      DOMAIN
                    BUSINESS WORK
```

Migration adds:

```text
Provider A
   │
   ├── PRIMARY
   ├── SHADOW
   └── MIGRATION
         │
         ▼
Provider B
         │
         ▼
CANARY
         │
         ▼
PRIMARY
```

---

# 143. Decision

**ACCEPTED TARGET PROVIDER AND ENGINE TOPOLOGY ARCHITECTURE, subject to formal approval.**

Upon approval:

1. provider identity SHALL become distinct from engine identity;
2. engine instances SHALL become explicit topology entities;
3. provider capability support SHALL be contract-driven;
4. health SHALL be separated from lifecycle;
5. capability resolution SHALL evaluate topology, region, residency, isolation, contract and health;
6. fallback SHALL be explicitly governed;
7. stateful failover SHALL default to conservative behaviour;
8. provider migrations SHALL be planned, auditable and reversible where possible;
9. engine replacement SHALL not force Digital Estate redesign;
10. CP SHALL remain the control plane and not become the data plane.

---

# 144. Architectural Maxim

> **Capabilities must outlive providers, providers must outlive individual deployments where possible, and Digital Estates must outlive both.**

And operationally:

> **The Control Plane chooses the provider; the provider chooses nothing about entitlement; the engine performs the work; the instance supplies the runtime; health and policy determine whether that runtime is safe to use.**