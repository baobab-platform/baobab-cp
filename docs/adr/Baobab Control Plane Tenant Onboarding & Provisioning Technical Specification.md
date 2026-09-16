# Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification

**Document ID:** BCP-TS-ONBOARDING-001  
**Status:** Proposed Normative Implementation Specification  
**Date:** 2026-09-11  
**Primary Repository:** `nabhold/baobab-cp`  
**Contract Authority:** `nabhold/shared`  
**Identity Authority:** `nabhold/baobab-iam`  
**Target Runtime:** Go Control Plane / PostgreSQL 17  
**Architecture:** Capability-centric, multi-tenant, multi-legal-entity, multi-market, multi-region, provider-neutral, fail-closed  
**Supersedes:** Repeated per-ADR implementation Gate-0 discovery exercises  
**Derived From:**

- ADR-BCP-001 — Parent Implementation Contract
- ADR-BCP-002 — Capability-Centric Architecture
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Resolution
- ADR-BCP-004 — Context, Market, Geography, Legal Entity and Digital Estate Resolution
- ADR-BCP-005 — Products, Capability Compositions, Subscriptions and Provisioning
- ADR-BCP-006 — Providers, Engines, Health, Failover and Migration
- ADR-BCP-007 — Runtime APIs, Caching and Service Consumption
- ADR-BCP-008 — Audit, Observability, Reconciliation and Readiness
- ADR-BCP-009 — Security, Isolation, Residency, Revocation and Failure Semantics
- ADR-SHARED-007 — Canonical Capability Contracts and Composition Registry

---

# 1. Purpose

This specification converts the Baobab Control Plane ADR set into a **single executable tenant-onboarding and provisioning programme**.

The ADRs SHALL NOT be implemented as nine independent workstreams.

Instead, they SHALL be implemented as one vertically integrated platform capability:

> **Given an organisation wishing to use Baobab, the Control Plane must be able to establish its canonical tenant and legal structure, configure Digital Estates and market participation, assign products and capability compositions, materialise entitlements, select compliant providers, provision the required runtime configuration, establish security and IAM context, reconcile desired against observed state, verify readiness, and activate the tenant.**

The complete lifecycle is:

```text
Organisation
     │
     ▼
Tenant
     │
     ▼
Legal / Organisational Context
     │
     ▼
Digital Estates
     │
     ▼
Markets / Participation / Geography
     │
     ▼
Product Subscription
     │
     ▼
Capability Composition
     │
     ▼
Capability Grants
     │
     ▼
Capability Scopes
     │
     ▼
Provider Bindings
     │
     ▼
Engine Instances
     │
     ▼
IAM / Workload Context
     │
     ▼
Isolation / Residency
     │
     ▼
Provider Provisioning
     │
     ▼
Reconciliation
     │
     ▼
Readiness
     │
     ▼
ACTIVE TENANT
```

---

# 2. Architecture Maxim

The implementation SHALL preserve the following chain:

```text
Identity
   ↓
Context
   ↓
Product
   ↓
Composition
   ↓
Entitlement
   ↓
Capability
   ↓
Scope
   ↓
Binding
   ↓
Provider
   ↓
Engine Instance
   ↓
Domain Authorization
   ↓
Business Operation
```

With the following cross-cutting concerns:

```text
Security
Isolation
Residency
Audit
Observability
Revocation
Reconciliation
Readiness
```

---

# 3. Mandatory Conflict Resolution Before Implementation

The following decisions SHALL be completed before schema freeze, migration generation or Phase 1 implementation.

These decisions are normative for this specification.

---

# 4. Conflict Resolution CR-001 — Centralise All Gate-0 Discovery

## 4.1 Problem

Nearly every ADR introduces a Gate 0 instructing implementation to inspect existing:

```text
Capability
CapabilityBinding
Context
Mapping
ProductSubscription
Engine
EngineInstance
DigitalEstate
```

and classify them:

```text
KEEP
REMODEL
SPLIT
MERGE
REMOVE
```

Repeating this discovery ADR by ADR would:

- duplicate work;
- produce inconsistent classification;
- allow one ADR to remodel something another ADR assumes unchanged;
- generate migration ordering conflicts;
- obscure cross-repository dependencies.

## 4.2 Decision

There SHALL be exactly **one central architecture inventory and conflict-resolution Gate**.

It is defined by this specification as:

```text
PHASE 0 — PLATFORM MODEL INVENTORY AND ARCHITECTURE LOCK
```

It SHALL cover both:

```text
nabhold/baobab-cp
nabhold/shared
```

and SHALL also inspect integration contracts referenced by:

```text
nabhold/baobab-iam
nabhold/baobab-trade
nabhold/baobab-erp
nabhold/baobab-cms
nabhold/baobab-pulse
```

where necessary to determine compatibility.

All per-ADR Gate-0 tasks SHALL be considered satisfied by this single central Gate.

---

# 5. Phase-0 Classification Deliverable

Phase 0 SHALL produce one authoritative matrix:

| Object | Existing State | Target State | Classification | Migration Action |
|---|---|---|---|---|
| Capability | inspect | CapabilityDefinition runtime projection | KEEP/REMODEL | determined centrally |
| CapabilityBinding | inspect | capability → provider → instance | REMODEL | mandatory |
| Engine | inspect | runtime technology family | KEEP/REMODEL | determined |
| EngineInstance | inspect | concrete topology runtime | KEEP | extend |
| Context | inspect | resolved immutable context | REMODEL | likely |
| MappingScope | inspect | mapping-specific scope | KEEP | preserve |
| CapabilityScope | absent/partial | dedicated entitlement/binding scope | ADD | mandatory |
| ProductSubscription | inspect | product-version subscription | REMODEL | mandatory |
| CapabilityProvider | absent/partial | first-class provider | ADD | mandatory |
| CapabilityGrant | inspect/absent | explicit entitlement | ADD/REMODEL | mandatory |
| DigitalEstate | inspect | capability consumer | KEEP/REMODEL | preserve |
| MarketParticipation | inspect/absent | explicit footprint model | ADD | mandatory |
| ProvisioningState | absent | tenant/product provisioning state | ADD | mandatory |
| ReadinessSnapshot | absent | readiness projection | ADD | mandatory |
| Drift | absent | reconciliation drift | ADD | mandatory |

No later phase SHALL silently reclassify one of these objects.

A change after Phase 0 requires an explicit architecture amendment.

---

# 6. Conflict Resolution CR-002 — Canonical `binding_mode`

## 6.1 Conflict

BCP-001 defines:

```text
PRIMARY
SECONDARY
FALLBACK
READ_ONLY
MIGRATION_SOURCE
MIGRATION_TARGET
SHADOW
```

Later capability-centric ADRs converge on:

```text
PRIMARY
FALLBACK
SHADOW
MIGRATION
DISABLED
```

## 6.2 Decision

The canonical `binding_mode` SHALL be:

```text
PRIMARY
FALLBACK
SHADOW
MIGRATION
DISABLED
```

This specification adopts the later capability-centric model.

---

# 7. Why `SECONDARY` Is Removed

`SECONDARY` is ambiguous.

It could mean:

```text
standby
fallback
read replica
lower priority
second active provider
```

Those are materially different semantics.

Therefore:

```text
SECONDARY
```

SHALL NOT exist as a canonical binding mode.

Use:

```text
FALLBACK
```

for a provider eligible after primary failure.

Use priority/specificity for ordered candidate selection where applicable.

---

# 8. Why `READ_ONLY` Is Removed From `binding_mode`

Read/write semantics belong to:

```text
capability semantics
provider capability support
```

rather than binding lifecycle.

For example:

```text
finance.invoice.read
finance.invoice.issue
```

are distinct capability semantics.

Where infrastructure-specific replica routing becomes necessary, introduce explicit provider-instance access characteristics such as:

```text
READ_WRITE
READ_ONLY
```

on provider-instance support/topology.

Do not overload `binding_mode`.

---

# 9. Why `MIGRATION_SOURCE` and `MIGRATION_TARGET` Are Removed

Migration source/target are properties of a **migration plan**, not ordinary long-lived binding state.

Therefore:

```text
MIGRATION
```

identifies a binding participating in controlled migration.

The separate:

```text
ProviderMigration
```

aggregate SHALL identify:

```text
source_provider_id
source_engine_instance_id
target_provider_id
target_engine_instance_id
migration_stage
```

This produces a cleaner model.

---

# 10. Binding-Mode Semantics

| Mode | Meaning |
|---|---|
| `PRIMARY` | Normal authoritative binding |
| `FALLBACK` | Eligible only under approved failover semantics |
| `SHADOW` | Non-authoritative mirrored/validation execution |
| `MIGRATION` | Temporary binding participating in an explicit migration plan |
| `DISABLED` | Binding retained but ineligible |

No additional mode SHALL be added without a formal architecture decision.

---

# 11. Conflict Resolution CR-003 — Event Naming Convention

## 11.1 Conflict

BCP-001 uses:

```text
baobab.<aggregate>.<event>.v1
```

while later documents include mixed examples:

```text
capability_grant.created
capability.grant.created
```

This inconsistency would propagate into AsyncAPI, event consumers and generated contracts.

## 11.2 Decision

Baobab SHALL standardise canonical event names as:

```text
baobab.<bounded-context>.<aggregate>.<event>.v<major>
```

Examples:

```text
baobab.capability.grant.created.v1
baobab.capability.grant.revoked.v1

baobab.capability.binding.changed.v1

baobab.product.subscription.created.v1
baobab.product.subscription.activated.v1

baobab.topology.provider.suspended.v1
baobab.topology.engine-instance.unavailable.v1

baobab.estate.digital-estate.ready.v1

baobab.tenant.tenant.suspended.v1
```

---

# 12. Event Vocabulary Rules

Canonical event names SHALL:

1. start with `baobab`;
2. contain a bounded-context segment;
3. contain a singular aggregate segment;
4. contain a past-tense event;
5. terminate with major event-contract version;
6. use lowercase kebab-case inside compound segments;
7. never use underscores;
8. never omit the `baobab` namespace for canonical platform events.

Therefore:

```text
capability_grant.created
```

is invalid.

And:

```text
capability.grant.created
```

is not canonical because it lacks the platform namespace and version.

---

# 13. Shared Event Vocabulary Authority

`nabhold/shared` SHALL become authoritative for:

```text
event names
event schemas
versioning
event envelope
reason-code vocabulary
```

No runtime repository SHALL invent independent canonical event names.

**ERRATUM (`shared` ADR-SHARED-008, Gate ZB-01):** CR-003 above specified the canonical event-type format as `baobab.<bounded-context>.<aggregate>.<event>.v<major>`. This repository is not the event-vocabulary authority — §13 says so in the same breath — and `nabhold/shared` had already shipped and enforced a different format, `com.nabhold.<context>.<...>.v<N>` (`contracts/events/v1/envelope.schema.json`), across every real identity/ERP/supplier-onboarding event before this specification was written. CR-003's format is superseded; `com.nabhold.*` is canonical. See ADR-SHARED-008 for the full resolution and `baobab-cp` ADR-BCP-015 for the downstream correction to ADR-BCP-011/012/013/014 and `baobab-trade` ADR-0019/0020/0021/0022.

---

# 14. Conflict Resolution CR-004 — BCP-001 Migration Count Bug

BCP-001 reportedly states:

```text
17 migrations
```

while enumerating:

```text
000001 through 000018
```

This is a documentation defect.

## Decision

BCP-001 SHALL receive an erratum.

The correct count for the listed sequence is:

```text
18 migrations
```

The Phase-0 checklist SHALL record:

```text
ADR-BCP-001 ERRATUM:
Database Definition of Done migration count corrected
from 17 to 18.
```

Implementation tooling SHALL rely on actual migration files rather than the erroneous count.

This issue SHALL be resolved before using BCP-001's Definition of Done as an implementation checklist.

---

# 15. Conflict Resolution CR-005 — Capability Binding Shape

## 15.1 Conflict

BCP-001 effectively models:

```text
CapabilityBinding
      │
      ▼
EngineInstance
```

Later ADRs introduce:

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

and require bindings to operate through providers.

## 15.2 Decision

The later provider-aware architecture SHALL prevail.

The canonical target shape is:

```text
Capability
     │
     ▼
CapabilityBinding
     │
     ├── provider_id ─────────► CapabilityProvider
     │                            │
     │                            ▼
     │                          Engine
     │
     └── engine_instance_id ───► EngineInstance
```

Both provider and concrete engine instance SHALL be explicit in the initial production model.

---

# 16. Canonical Binding Fields

Conceptually:

```text
CapabilityBinding
├── id
├── capability_id
├── provider_id
├── engine_instance_id
├── capability_scope_id
├── binding_mode
├── priority
├── status
├── required_contract_version
├── effective_from
├── effective_to
├── configuration
├── version
├── created_at
└── updated_at
```

---

# 17. Why Both Provider and Engine Instance Are Required

Provider answers:

> Which canonical implementation is responsible?

EngineInstance answers:

> Which concrete runtime deployment will fulfil it?

Example:

```text
Capability:
commerce.order.create

Provider:
baobab-trade.medusa

Engine:
medusa

EngineInstance:
medusa-prod-za-01
```

Without `provider_id`, capability semantics are coupled directly to infrastructure.

Without `engine_instance_id`, runtime routing is underspecified.

---

# 18. Binding Validation

Database and service-layer validation SHALL enforce:

```text
binding.provider.engine_id
=
binding.engine_instance.engine_id
```

and:

```text
provider supports binding.capability
```

before activation.

---

# 19. Migration Strategy for Existing Binding Model

Because Baobab has not entered production with immutable customer data, Phase 0 SHOULD prefer **remodelling incorrect pre-production migrations** rather than accumulating compensating migrations.

The implementation SHALL decide centrally whether:

```text
rewrite existing 000001–000018
```

or:

```text
retain them and append migrations
```

based on repository state.

Before production freeze, clean architectural migrations are preferred.

After production adoption, normal forward-only migration rules apply.

---

# 20. Target Domain Model

The consolidated domain model is:

```text
Tenant
 ├── LegalEntity
 ├── DigitalEstate
 ├── MarketParticipation
 ├── IsolationProfile
 ├── DataResidencyPolicy
 └── ProductSubscription
        │
        ▼
   ProductVersion
        │
        ▼
 CapabilityComposition
        │
        ▼
   CapabilityGrant
        │
        ▼
   CapabilityScope
        │
        ▼
 CapabilityBinding
        │
        ├── CapabilityProvider
        │       │
        │       ▼
        │     Engine
        │
        └── EngineInstance
```

Cross-cutting:

```text
CanonicalEntity
ExternalReference
Mapping
MappingScope
Context
ProvisioningState
Readiness
Drift
Audit
Outbox
```

---

# 21. Tenant Onboarding Aggregate

The Control Plane SHALL introduce a process aggregate:

```text
TenantProvisioning
```

It does not replace `Tenant`.

It tracks onboarding orchestration.

Conceptually:

```text
TenantProvisioning
├── id
├── tenant_id
├── request_version
├── desired_state_version
├── observed_state_version
├── status
├── current_phase
├── product_requests[]
├── estate_requests[]
├── market_requests[]
├── isolation_requirement
├── residency_requirement
├── readiness_status
├── blocking_reasons[]
├── started_at
├── completed_at
├── version
└── metadata
```

---

# 22. Tenant Provisioning State Machine

Canonical onboarding states SHALL be:

```text
DRAFT
  │
  ▼
VALIDATING
  │
  ▼
PLANNED
  │
  ▼
REGISTERING
  │
  ▼
CONFIGURING_CONTEXT
  │
  ▼
PROVISIONING_ENTITLEMENTS
  │
  ▼
PROVISIONING_PROVIDERS
  │
  ▼
VALIDATING_SECURITY
  │
  ▼
VERIFYING_READINESS
  │
  ├────────────► BLOCKED
  │                 │
  │                 ▼
  │             REMEDIATING
  │                 │
  └─────────────────┘
  │
  ▼
READY
  │
  ▼
ACTIVE
```

Terminal states MAY include:

```text
FAILED
CANCELLED
DEPROVISIONED
```

---

# 23. No Partial Hidden Activation

A tenant SHALL NOT be marked:

```text
ACTIVE
```

merely because:

```text
tenant row exists
```

or:

```text
Digital Estate deployed
```

Mandatory product capabilities must satisfy readiness gates.

---

# 24. Desired-State Onboarding Contract

Onboarding SHALL begin with a declarative desired-state request.

Conceptually:

```yaml
tenant:
  key: zuribeans
  display_name: ZuriBeans

legal_entities:
  - key: zuribeans-ug
    jurisdiction: UG

digital_estates:
  - key: zuribeans-b2b
    channel: B2B

  - key: zuribeans-supplier
    channel: B2B

market_participation:
  - market: UG
    activities:
      - LEGAL_PRESENCE
      - SOURCING
      - EXPORTING

  - market: ZA
    activities:
      - SELLING
      - IMPORTING

products:
  - product: solution.baobab-xbt
    version: "1"
    profiles:
      - profile.b2b
      - profile.crossborder
      - profile.commodity-trade
      - profile.supplier-management

isolation:
  profile: standard-tenant

residency:
  profile: africa-approved
```

This is illustrative syntax; canonical schemas SHALL live in Shared.

---

# 25. Plan Before Apply

Every onboarding SHALL follow:

```text
REQUEST
   │
   ▼
VALIDATE
   │
   ▼
PLAN
   │
   ▼
IMPACT ANALYSIS
   │
   ▼
APPLY
   │
   ▼
RECONCILE
   │
   ▼
VERIFY READINESS
   │
   ▼
ACTIVATE
```

The system SHALL NOT apply a complex onboarding request before generating a deterministic provisioning plan.

---

# 26. Provisioning Plan

Conceptually:

```text
TenantProvisioningPlan
├── tenant_operations[]
├── legal_entity_operations[]
├── estate_operations[]
├── market_operations[]
├── subscription_operations[]
├── capability_grant_operations[]
├── binding_operations[]
├── provider_operations[]
├── mapping_operations[]
├── IAM_requirements[]
├── security_checks[]
├── readiness_requirements[]
├── blockers[]
├── warnings[]
└── plan_hash
```

---

# 27. Provisioning Plan Properties

A plan SHALL be:

```text
deterministic
versioned
auditable
dry-run capable
idempotent when applied
```

The same desired-state version against the same authoritative state SHOULD generate the same plan.

---

# 28. Core Onboarding Workflow

The normative workflow is:

```text
Tenant Request
     │
     ▼
1. Validate canonical contracts
     │
     ▼
2. Register tenant
     │
     ▼
3. Register legal entities
     │
     ▼
4. Register Digital Estates
     │
     ▼
5. Register market participation
     │
     ▼
6. Apply isolation/residency requirements
     │
     ▼
7. Create product subscriptions
     │
     ▼
8. Expand capability compositions
     │
     ▼
9. Materialise capability grants
     │
     ▼
10. Resolve provider requirements
     │
     ▼
11. Create capability bindings
     │
     ▼
12. Provision provider configuration
     │
     ▼
13. Establish IAM mappings/context
     │
     ▼
14. Reconcile desired vs observed state
     │
     ▼
15. Evaluate readiness
     │
     ▼
16. Activate tenant
```

---

# 29. Phase 1 — Canonical Platform Registry

## Objective

Establish the canonical organisational and platform spine.

Implement:

```text
Tenant
LegalEntity
CanonicalEntity
ExternalReference
Mapping
MappingScope
Market
MarketParticipation
DigitalEstate
DigitalProperty
Channel
IsolationProfile
DataResidencyPolicy
```

## Acceptance condition

The platform SHALL be capable of representing all three acceptance fixtures without creating tenant-specific schema.

---

# 30. Phase 2 — Capability Registry and Entitlement Spine

Implement:

```text
Capability
CapabilityScope
CapabilityGrant
CapabilityDependency
CapabilityProvider
ProviderCapabilitySupport
CapabilityBinding
```

Canonical `binding_mode` SHALL already be locked to:

```text
PRIMARY
FALLBACK
SHADOW
MIGRATION
DISABLED
```

No migration SHALL encode the superseded BCP-001 enum.

---

# 31. Phase 3 — Product Composition and Subscription

Implement:

```text
Product
ProductVersion
CapabilityComposition
CompositionMember
ProductSubscription
SubscriptionProfile
EntitlementProjection
```

The relationship SHALL be:

```text
ProductSubscription
      │
      ▼
ProductVersion
      │
      ▼
CapabilityComposition
      │
      ▼
Capability Grants
```

Runtime capability authorization SHALL never depend directly on product-name checks.

---

# 32. Phase 4 — Context Resolution

Implement:

```text
ContextRequest
ResolvedContext
OperationScope
ContextProvenance
```

Resolution SHALL establish as applicable:

```text
principal
tenant
legal entity
Digital Estate
digital property
channel
market
jurisdiction
currency
deployment region
environment
isolation
```

Dangerous inference SHALL remain prohibited.

---

# 33. Phase 5 — Provider Topology

Implement:

```text
Engine
EngineInstance
CapabilityProvider
ProviderCapabilitySupport
EngineInstanceHealth
ProviderMigration
```

The architecture SHALL support:

```text
shared provider
dedicated provider
regional provider
fallback provider
future external provider
```

without changing Digital Estate contracts.

---

# 34. Phase 6 — Tenant Provisioning Engine

Implement:

```text
TenantProvisioning
ProvisioningPlan
ProvisioningOperation
ProvisioningState
ProvisioningResult
```

The provisioning engine SHALL orchestrate platform state.

It SHALL NOT become an arbitrary business workflow engine.

---

# 35. Provisioning Operations

Supported operation types SHOULD include:

```text
CREATE
UPDATE
ACTIVATE
SUSPEND
REVOKE
BIND
UNBIND
PROVISION
DEPROVISION
RECONCILE
VALIDATE
```

Each operation SHALL be:

```text
idempotent
auditable
retryable where safe
```

---

# 36. Phase 7 — Provider Adapters

Provider adapters SHALL translate desired capability/provider state into engine-native configuration.

Initial adapters:

```text
Baobab Trade / MedusaJS
Baobab ERP / iDempiere
Baobab CMS / Payload
Baobab Pulse / Haystack
Baobab IAM / Keycloak integration boundary
```

---

# 37. Adapter Boundary

The pattern SHALL be:

```text
Canonical Baobab Desired State
          │
          ▼
     Provider Adapter
          │
          ▼
Provider-Native Configuration
```

The adapter SHALL NOT make provider-native identifiers canonical platform identity.

---

# 38. Example — Medusa Provisioning

The adapter MAY provision/reference:

```text
sales channel
region
stock location
catalogue mapping
tenant provider configuration
```

It SHALL NOT make CP owner of:

```text
orders
carts
payments
inventory transactions
```

---

# 39. Example — iDempiere Provisioning

The adapter MAY provision/reference:

```text
client
organisation
accounting context
canonical mappings
```

It SHALL NOT make CP owner of:

```text
journal entries
invoices
receivables
payables
```

---

# 40. Phase 8 — IAM Integration

Implement the integration boundary required to associate authenticated identities/workloads with canonical CP context.

IAM SHALL provide:

```text
identity
authentication
assurance
workload identity
coarse scopes
```

CP SHALL provide:

```text
tenant
legal entity
estate
capability entitlement
provider selection
```

---

# 41. Phase 9 — Security, Isolation and Residency

Before activation, provisioning SHALL verify:

```text
tenant isolation
legal entity isolation
Digital Estate scope
required isolation profile
provider isolation support
residency policy
provider region
environment
credential references
provider trust
```

Failure of mandatory checks blocks activation.

---

# 42. Phase 10 — Reconciliation and Readiness

Implement:

```text
DesiredState
ObservedState
Drift
ReconciliationRun
ReadinessSnapshot
ImpactAnalysis
```

The reconciler SHALL continuously evaluate:

```text
what should exist
vs
what actually exists
```

---

# 43. Reconciliation Model

```text
Desired State
     │
     ▼
Observe
     │
     ▼
Compare
     │
     ▼
Drift?
  ┌──┴──┐
  │     │
 NO    YES
  │     │
  │     ▼
  │   Safe to fix?
  │    ┌──┴──┐
  │   YES    NO
  │    │      │
  │    ▼      ▼
  │ Reconcile BLOCKED
  │    │
  └────┴──────┐
              ▼
      Readiness Evaluation
```

---

# 44. Readiness Hierarchy

Implement:

```text
ProviderReadiness
CapabilityReadiness
ProductReadiness
EstateReadiness
TenantReadiness
```

Readiness SHALL be explainable.

Never return merely:

```text
ready = false
```

where structured reasons are available.

---

# 45. Phase 11 — Runtime Capability Resolution

Implement:

```text
POST /v1/context/resolve

POST /v1/capabilities/resolve

POST /v1/capabilities/resolve-batch
```

Canonical runtime decision:

```text
identity
   ↓
context
   ↓
grant
   ↓
scope
   ↓
binding
   ↓
provider eligibility
   ↓
ALLOW / DENY
```

CP SHALL return provider resolution.

CP SHALL NOT proxy ordinary domain business traffic.

---

# 46. Runtime Flow

```text
Digital Estate
     │
     ▼
Estate API / BFF
     │
     ▼
IAM Authentication
     │
     ▼
CP Context Resolution
     │
     ▼
CP Capability Resolution
     │
     ▼
Capability Provider
     │
     ▼
Domain Authorization
     │
     ▼
Business Operation
```

---

# 47. Phase 12 — Audit, Events and Observability

Implement:

```text
AuditRecord
TransactionalOutbox
canonical events
metrics
traces
correlation IDs
resolution IDs
reason codes
```

Canonical event naming SHALL follow:

```text
baobab.<bounded-context>.<aggregate>.<event>.v<major>
```

---

# 48. Mandatory Event Examples

```text
baobab.tenant.tenant.created.v1
baobab.tenant.tenant.activated.v1

baobab.product.subscription.created.v1
baobab.product.subscription.activated.v1

baobab.capability.grant.created.v1
baobab.capability.grant.revoked.v1

baobab.capability.binding.created.v1
baobab.capability.binding.changed.v1

baobab.topology.provider.suspended.v1

baobab.estate.digital-estate.ready.v1

baobab.provisioning.tenant-provisioning.completed.v1
```

---

# 49. Onboarding API Surface

The target administrative API SHOULD expose an onboarding workflow rather than requiring consumers to orchestrate dozens of lower-level resources themselves.

Candidate:

```text
POST /v1/tenant-provisionings

GET  /v1/tenant-provisionings/{id}

POST /v1/tenant-provisionings/{id}/validate

POST /v1/tenant-provisionings/{id}/plan

POST /v1/tenant-provisionings/{id}/apply

POST /v1/tenant-provisionings/{id}/reconcile

GET  /v1/tenant-provisionings/{id}/readiness

GET  /v1/tenant-provisionings/{id}/impact

POST /v1/tenant-provisionings/{id}/cancel
```

Lower-level APIs SHALL remain available to authorised platform administrators and internal services.

---

# 50. Plan Response Example

```json
{
  "provisioning_id": "tp_123",
  "status": "PLANNED",
  "operations": {
    "tenants": 1,
    "legal_entities": 1,
    "digital_estates": 2,
    "market_participations": 2,
    "subscriptions": 1,
    "capability_grants": 24,
    "capability_bindings": 18,
    "provider_operations": 7
  },
  "blockers": [],
  "warnings": []
}
```

---

# 51. Readiness Response Example

```json
{
  "tenant": "READY",
  "identity": "READY",
  "context": "READY",
  "subscriptions": "READY",
  "entitlements": "READY",
  "providers": "DEGRADED",
  "security": "READY",
  "overall": "BLOCKED",
  "blocking_reasons": [
    {
      "capability": "logistics.dispatch.manage",
      "code": "BINDING_NOT_FOUND"
    }
  ]
}
```

---

# 52. Migration Architecture

Schema work SHALL not begin until Phase 0 has locked:

```text
binding enum
binding shape
provider model
event vocabulary
CapabilityScope
ProductVersion
TenantProvisioning
```

This avoids encoding contradictory ADR assumptions into PostgreSQL migrations.

---

# 53. Recommended Migration Domains

Target PostgreSQL organisation:

```text
registry
mapping
market
estate
product
capability
topology
policy
provisioning
operations
audit
messaging
system
```

---

# 54. Migration Sequence Principle

Do NOT organise migrations merely according to ADR number.

Organise them according to dependency order:

```text
1. base registries
2. organisational context
3. market / estate
4. capability registry
5. providers / topology
6. capability scope
7. capability grants
8. capability bindings
9. products / compositions
10. subscriptions
11. provisioning
12. readiness / reconciliation
13. audit / outbox
```

---

# 55. Pre-Production Migration Policy

Because production customer data does not yet constrain the platform:

> Incorrect pre-production schema SHALL be remodelled cleanly rather than preserved indefinitely for historical convenience.

Once the production baseline is declared, migrations become normal forward-only production migrations.

---

# 56. Acceptance Fixture A — ZuriBeans

ZuriBeans SHALL be the primary **golden reference tenant**.

It SHALL NOT be hard-coded.

It is a declarative fixture proving B2B, supplier, commodity and cross-border operation.

---

# 57. ZuriBeans Organisation

```text
Tenant:
ZuriBeans

Legal Entity:
ZuriBeans operating company

Parent:
NABHOLD GROUP AFRICA

Relationship:
subsidiary

Operational isolation:
independent from Thamani
```

No parent-company relationship SHALL create implicit access to Thamani.

---

# 58. ZuriBeans Estates

At minimum:

```text
ZuriBeans B2B Buyer Portal
ZuriBeans Supplier Portal
ZuriBeans Operations Portal
```

---

# 59. ZuriBeans Markets

Acceptance fixture:

```text
Uganda
├── LEGAL_PRESENCE
├── SOURCING
└── EXPORTING

South Africa
├── SELLING
└── IMPORTING
```

This SHALL prove that market participation does not require matching legal presence.

---

# 60. ZuriBeans Product Composition

```text
solution.baobab-xbt
+
profile.b2b
+
profile.crossborder
+
profile.commodity-trade
+
profile.supplier-management
```

---

# 61. ZuriBeans Mandatory Capabilities

Acceptance set SHOULD include at least:

```text
counterparty.buyer.onboard
counterparty.supplier.onboard

commercial.rfq.manage
commercial.quotation.manage
commercial.contract.manage

commerce.catalogue.read
commerce.order.create
inventory.availability.read

trade.execution.manage
logistics.shipment.manage
documents.trade.manage

finance.invoice.manage
finance.receivable.manage
finance.settlement.manage

intelligence.fx.query
intelligence.commodity.query
intelligence.market.query

content.page.read
```

---

# 62. ZuriBeans Expected Providers

Initial mapping:

```text
Commerce / Inventory
→ baobab-trade / MedusaJS

Finance / ERP
→ baobab-erp / iDempiere

Content
→ baobab-cms / Payload

Intelligence
→ baobab-pulse / Haystack
```

Where a mandatory logistics capability has no implementation, the fixture SHALL expose the gap rather than fake readiness.

---

# 63. ZuriBeans Acceptance Tests

The fixture passes only if:

1. ZuriBeans can be provisioned without tenant-specific CP code.
2. ZuriBeans and Thamani remain isolated.
3. Uganda→South Africa cross-border context resolves correctly.
4. South Africa destination does not fabricate South African legal presence.
5. XBT profiles expand deterministically.
6. grants preserve subscription provenance.
7. provider bindings resolve correctly.
8. IAM identity can resolve to ZuriBeans context.
9. invalid Thamani identity/context cannot access ZuriBeans grants.
10. readiness identifies missing providers accurately.
11. provider failure produces correct degraded state.
12. revocation invalidates access within policy bounds.

---

# 64. Acceptance Fixture B — Coal-Hauling Business

The coal-haulier fixture SHALL deliberately exercise capabilities materially different from the existing NABHOLD businesses.

Its purpose is to prove:

> Baobab XBT is not secretly a coffee/e-commerce platform.

---

# 65. Coal-Haulier Organisation

Example:

```text
Tenant:
Acme Coal Logistics

Legal Entity:
Acme Coal Logistics (Pty) Ltd

Home Jurisdiction:
South Africa
```

Optional future fixture MAY include additional legal entities.

---

# 66. Coal-Haulier Geography

Example:

```text
South Africa
└── TRANSPORTING

Botswana
└── TRANSITING

Zambia
└── DELIVERING
```

Acceptance condition:

```text
Botswana transit
≠
Botswana legal presence
```

---

# 67. Coal-Haulier Estates

```text
Customer Portal
Dispatch Console
Fleet Operations Estate
Management Console
```

---

# 68. Coal-Haulier Product Composition

```text
solution.baobab-xbt
+
profile.b2b
+
profile.crossborder
+
profile.logistics
```

---

# 69. Coal-Haulier Capabilities

Mandatory acceptance capabilities:

```text
counterparty.customer.onboard
commercial.contract.manage

logistics.transport-order.manage
logistics.load.manage
logistics.dispatch.manage
logistics.route.manage
logistics.border-milestone.manage
logistics.delivery.manage

documents.pod.manage

finance.invoice.manage
finance.receivable.manage
finance.job-profitability.calculate

intelligence.route-risk.assess
intelligence.fuel.query
```

---

# 70. Coal-Haulier Gap Test

If Baobab has no provider for:

```text
logistics.dispatch.manage
```

the expected result is:

```text
TenantProvisioning = BLOCKED

CapabilityReadiness:
logistics.dispatch.manage = NOT_READY

Reason:
BINDING_NOT_FOUND
```

The implementation SHALL NOT:

```text
pretend Trade implements it
mark tenant ACTIVE
hard-code a temporary exception
```

This test proves readiness correctness.

---

# 71. Coal-Haulier Acceptance Tests

1. Tenant can be modelled without modifying core CP schema.
2. Transit country does not create false legal/market presence.
3. Logistics profile expands independently from commodity profile.
4. Missing logistics provider is detected.
5. Product activation remains blocked when a mandatory capability lacks a provider.
6. Adding a compliant logistics provider later allows reconciliation to converge to READY without rebuilding the tenant.
7. Finance can still resolve independently through ERP.
8. provider failure blast radius remains tenant/capability specific.

---

# 72. Acceptance Fixture C — Oil Trader

The oil-trader fixture is the complexity and multi-jurisdiction acceptance test.

It SHALL prove that the Control Plane supports complex cross-border commodity activity without tenant-specific routing logic.

---

# 73. Oil-Trader Organisation

Example:

```text
Tenant:
Pan-African Petroleum Trading

Legal Entity:
Pan-African Petroleum Trading Ltd
```

An extended fixture MAY later include:

```text
Uganda Trading Entity
Kenya Trading Entity
UAE Treasury Entity
```

without changing tenancy architecture.

---

# 74. Oil-Trader Product Composition

```text
solution.baobab-xbt
+
profile.b2b
+
profile.crossborder
+
profile.commodity-trade
+
addon.advanced-intelligence
```

---

# 75. Oil-Trader Capabilities

Mandatory acceptance capabilities SHOULD include:

```text
counterparty.onboarding.manage
counterparty.due-diligence.manage

commercial.opportunity.manage
commercial.offer.manage
commercial.contract.manage

commodity.pricing.resolve

trade.execution.manage
logistics.shipment.manage

documents.inspection.manage
documents.trade.manage

finance.settlement.manage
finance.trade-profitability.calculate

intelligence.commodity.query
intelligence.fx.query
intelligence.regulatory.query
intelligence.risk.assess
```

---

# 76. Oil-Trader Operational Context

The fixture SHALL exercise:

```text
multiple counterparties
multiple jurisdictions
origin market
destination market
transaction currency
settlement currency
inspection documents
regulatory intelligence
commodity pricing
```

No one of these dimensions SHALL silently infer another.

---

# 77. Oil-Trader Gap Test

If:

```text
documents.inspection.manage
```

has no compliant provider and is mandatory:

```text
ProductReadiness = NOT_READY
TenantProvisioning = BLOCKED
```

If the capability is optional under a configured composition:

```text
ProductReadiness = DEGRADED
```

may be acceptable according to composition policy.

---

# 78. Oil-Trader Acceptance Tests

1. No oil-specific CP branching exists.
2. commodity-trade profile is reusable.
3. multiple jurisdictions resolve without fabricating legal presence.
4. USD settlement does not infer a US market or US legal presence.
5. commodity pricing can resolve independently of commerce provider.
6. inspection-document capability can be provided by a future provider.
7. advanced intelligence is composable as an add-on.
8. isolation and residency restrictions affect provider eligibility correctly.
9. no Keycloak role explosion occurs.
10. domain engines retain business approval authority.

---

# 79. Cross-Fixture Acceptance Matrix

| Requirement | ZuriBeans | Coal Haulier | Oil Trader |
|---|---:|---:|---:|
| B2B | ✓ | ✓ | ✓ |
| Cross-border | ✓ | ✓ | ✓ |
| Commodity | ✓ | — | ✓ |
| Supplier management | ✓ | — | optional |
| Logistics | moderate | primary | moderate |
| ERP | ✓ | ✓ | ✓ |
| Intelligence | ✓ | ✓ | ✓ |
| Transit geography | possible | ✓ | possible |
| Multi-jurisdiction | ✓ | ✓ | ✓ |
| Missing-provider test | ✓ | ✓ | ✓ |
| Isolation test | ✓ | ✓ | ✓ |
| Generic onboarding | ✓ | ✓ | ✓ |

All three SHALL use the same provisioning algorithm.

---

# 80. Anti-Hard-Coding Acceptance Rule

The architecture FAILS acceptance if adding the coal-haulier or oil-trader fixture requires core code such as:

```go
if tenant == "zuribeans" { ... }

if industry == "coal" { ... }

if industry == "oil" { ... }
```

or equivalent special-case branching in capability resolution.

Industry differentiation belongs in:

```text
product composition
capability profiles
configuration
domain engines
```

not tenant-specific resolver code.

---

# 81. Capability Gap Discovery as Product Intelligence

Tenant onboarding SHALL also function as a controlled capability-gap mechanism.

Example:

```text
Customer Requirement
      │
      ▼
Capability Composition
      │
      ▼
Mandatory Capability
      │
      ▼
No Eligible Provider
      │
      ▼
Platform Gap
```

This allows NABHOLD to decide whether to:

```text
extend an engine
build a new provider
integrate external SaaS
defer product activation
```

without corrupting CP architecture.

---

# 82. Repository Responsibilities

## `nabhold/shared`

Owns:

```text
canonical schemas
capability taxonomy
composition definitions
event vocabulary
reason codes
OpenAPI
AsyncAPI
generated DTOs/SDK contracts
```

Does not own runtime state.

---

# 83. `nabhold/baobab-cp`

Owns:

```text
tenants
legal entities
markets
Digital Estates
product subscriptions
grants
scopes
providers
bindings
context resolution
provisioning state
readiness
reconciliation
audit metadata
outbox
```

---

# 84. `nabhold/baobab-iam`

Owns:

```text
identity
authentication
credentials
sessions
MFA
passkeys
OIDC/OAuth
workload identity
identity lifecycle
```

It does not own business capability grants.

---

# 85. Domain Providers

Examples:

```text
baobab-trade
baobab-erp
baobab-cms
baobab-pulse
```

own:

```text
business data
domain workflows
business authorization
provider-native state
```

---

# 86. Implementation Programme

The individual ADR Gates SHALL be consolidated into the following programme.

---

# 87. Programme Gate P0 — Architecture Inventory and Lock

Perform once.

Deliverables:

```text
current-state inventory
KEEP/REMODEL/SPLIT/MERGE/REMOVE matrix
binding enum lock
binding model lock
event vocabulary lock
BCP-001 erratum
migration strategy decision
```

No schema work proceeds before P0 passes.

---

# 88. Programme Gate P1 — Shared Contract Foundation

Implement in `nabhold/shared`:

```text
Capability
CapabilityScope
CapabilityGrant
CapabilityProvider
CapabilityBinding
Product
ProductVersion
Composition
Subscription
Context
Provisioning
Readiness
Drift
event vocabulary
reason codes
```

All schemas validate.

---

# 89. Programme Gate P2 — Canonical Registry Spine

Implement CP organisational foundation:

```text
Tenant
LegalEntity
CanonicalEntity
ExternalReference
Mapping
Market
MarketParticipation
DigitalEstate
```

Create basic fixture records.

---

# 90. Programme Gate P3 — Capability and Entitlement Spine

Implement:

```text
Capability
CapabilityGrant
CapabilityScope
Provider
ProviderCapabilitySupport
Binding
```

Deterministic resolver unit tests required.

---

# 91. Programme Gate P4 — Product and Composition Engine

Implement:

```text
Product
ProductVersion
Composition
Composition expansion
Profile dependencies
Subscription
Grant materialisation
Grant provenance
```

---

# 92. Programme Gate P5 — Context Resolution

Implement authoritative context resolution.

Acceptance:

```text
tenant
legal entity
estate
market
jurisdiction
channel
isolation
```

must resolve without dangerous inference.

---

# 93. Programme Gate P6 — Provider Topology

Implement:

```text
Engine
EngineInstance
Health
Provider eligibility
Isolation compatibility
Residency compatibility
```

---

# 94. Programme Gate P7 — Tenant Provisioning Engine

Implement:

```text
TenantProvisioning
Plan
Apply
Idempotency
Status transitions
Retry
Failure handling
```

---

# 95. Programme Gate P8 — Provider Reconciliation

Implement adapters/reconciliation for initial engines.

The tenant provisioning engine SHALL not directly embed Medusa/iDempiere/Haystack/Payload-specific logic.

---

# 96. Programme Gate P9 — IAM and Security Integration

Implement:

```text
workload authentication
user delegation
context mapping
least privilege
tenant isolation
legal entity isolation
revocation
```

---

# 97. Programme Gate P10 — Readiness and Drift

Implement hierarchical readiness and reconciliation.

Acceptance:

```text
mandatory missing capability
```

must prevent activation.

---

# 98. Programme Gate P11 — Runtime Resolution APIs

Implement:

```text
/context/resolve
/capabilities/resolve
/capabilities/resolve-batch
```

plus safe caching.

---

# 99. Programme Gate P12 — Audit, Outbox and Observability

Implement canonical:

```text
audit
events
metrics
tracing
correlation
outbox
```

Event names SHALL follow the locked Shared convention.

---

# 100. Programme Gate P13 — ZuriBeans Golden Tenant

Run full end-to-end onboarding.

No mock bypasses.

Required:

```text
PLAN
APPLY
RECONCILE
READY
ACTIVE
```

or explicit blocker for genuinely unimplemented mandatory provider capability.

---

# 101. Programme Gate P14 — Coal-Haulier Genericity Test

Run complete onboarding without changing core CP architecture.

Any hidden coffee/e-commerce coupling SHALL be treated as a defect.

---

# 102. Programme Gate P15 — Oil-Trader Complexity Test

Run multi-jurisdiction commodity-trade scenario.

No oil-specific resolver branch permitted.

---

# 103. Programme Gate P16 — Failure and Revocation Testing

Inject:

```text
grant revocation
tenant suspension
provider outage
region outage
cache outage
event-bus outage
binding ambiguity
residency mismatch
isolation mismatch
```

Security invariants must hold.

---

# 104. Programme Gate P17 — Production Readiness

Review:

```text
security
SLOs
capacity
database
backups
DR
observability
runbooks
migration state
contract compatibility
provider readiness
```

Only then declare the onboarding platform production-ready.

---

# 105. PR Strategy

Because the platform is polyrepo/polyglot, each Programme Gate SHOULD produce separately reviewable changes.

Where one Gate affects multiple repositories:

```text
shared PR first
      │
      ▼
CP PR
      │
      ▼
provider/consumer PRs
```

Contract authority SHALL land before dependent implementations unless coordinated compatibility requires otherwise.

---

# 106. Database Transaction Boundaries

Local CP state changes SHALL use PostgreSQL transactions.

Cross-provider provisioning SHALL not pretend to be one distributed transaction.

Pattern:

```text
CP transaction
├── desired state
├── provisioning operation
└── outbox event
       │
       ▼
provider action
       │
       ▼
observed state
       │
       ▼
reconciliation
```

---

# 107. Failure Recovery

Example:

```text
Tenant created
Subscription created
Grants created
Medusa configured
ERP provisioning fails
```

Expected:

```text
TenantProvisioning = BLOCKED / FAILED
```

with:

```text
completed operations retained
failed operation identified
safe retry supported
readiness NOT_READY
```

The system SHALL not blindly delete successfully provisioned external state.

---

# 108. Idempotency

Every provisioning mutation SHALL tolerate retry.

Example:

```text
ensure ZuriBeans Medusa sales channel exists
```

rather than:

```text
create another sales channel on every retry
```

---

# 109. Concurrency

A given tenant provisioning aggregate SHALL not have competing mutation workers.

Use:

```text
optimistic versioning
advisory lock
lease
queue serialization
```

or equivalent safe mechanism.

---

# 110. Activation Gate

A tenant becomes ACTIVE only when:

```text
tenant active
AND
legal context valid
AND
estates valid
AND
subscriptions active
AND
mandatory grants effective
AND
mandatory bindings valid
AND
mandatory providers eligible
AND
isolation satisfied
AND
residency satisfied
AND
IAM integration valid
AND
reconciliation in sync
AND
readiness acceptable
```

---

# 111. Partial Product Readiness

If a composition defines optional capabilities:

```text
mandatory capabilities ready
optional capability unavailable
```

the product MAY be:

```text
DEGRADED
```

where product policy permits.

Mandatory missing capabilities SHALL block activation.

---

# 112. Definition of Done — Architecture

The consolidated programme is architecturally complete when:

1. one central Gate-0 inventory exists;
2. no ADR-specific discovery work conflicts with it;
3. canonical binding modes are locked;
4. `SECONDARY` is removed;
5. `READ_ONLY` is modelled outside binding lifecycle;
6. migration source/target are represented through migration state;
7. canonical event vocabulary is defined in Shared;
8. BCP-001 migration count erratum is recorded;
9. binding contains provider and engine-instance identity;
10. provider is first-class;
11. CapabilityScope is separate from MappingScope;
12. TenantProvisioning is first-class;
13. provisioning uses desired state;
14. plan-before-apply exists;
15. readiness gates activation.

---

# 113. Definition of Done — Functional

The programme is functionally complete when:

1. ZuriBeans can be onboarded declaratively.
2. A coal-hauling company can be onboarded using the same algorithm.
3. An oil trader can be onboarded using the same algorithm.
4. capability compositions differ without tenant-specific resolver code.
5. subscriptions deterministically materialise grants.
6. grants have provenance.
7. grants have scope.
8. provider bindings resolve deterministically.
9. missing provider blocks mandatory capability readiness.
10. adding the provider later allows reconciliation to converge.
11. provider replacement does not require estate redesign.
12. runtime context resolution is authoritative.
13. runtime capability resolution is provider-neutral.
14. domain authorization remains with domain providers.

---

# 114. Definition of Done — Security

The programme is secure when:

1. tenants cannot cross boundaries;
2. legal entities remain independently isolated;
3. ZuriBeans cannot consume Thamani grants;
4. Thamani cannot consume ZuriBeans grants;
5. parent ownership confers no implicit operational authority;
6. browser-supplied tenant context is non-authoritative;
7. provider forcing is prevented;
8. isolation downgrade fails;
9. residency violation fails;
10. production cannot resolve to non-production instance;
11. grant revocation invalidates access;
12. tenant suspension invalidates access;
13. provider quarantine invalidates eligibility;
14. cached authorization cannot cross tenant/context boundaries.

---

# 115. Definition of Done — Operational

The programme is operationally complete when:

1. all provisioning operations are auditable;
2. desired and observed state can be compared;
3. drift is visible;
4. reconciliation is idempotent;
5. readiness is hierarchical;
6. blockers have reason codes;
7. provider blast radius can be calculated;
8. events use canonical naming;
9. outbox delivery is monitored;
10. traces span CP and providers;
11. critical alerts have runbooks;
12. onboarding failures can be resumed safely.

---

# 116. Final Target Architecture

```text
                     TENANT ONBOARDING REQUEST
                               │
                               ▼
                    ┌─────────────────────┐
                    │ Provisioning Planner│
                    └──────────┬──────────┘
                               │
                          Desired State
                               │
        ┌──────────────────────┼───────────────────────┐
        ▼                      ▼                       ▼
    Organisation            Product               Security
    Context                 Composition            Policy
        │                      │                       │
        ▼                      ▼                       ▼
 Tenant / Entity         Capability Grants     Isolation/Residency
 Estate / Market               │                       │
        └──────────────────────┼───────────────────────┘
                               ▼
                      Capability Resolution
                               │
                               ▼
                     Provider Requirements
                               │
             ┌─────────────────┼─────────────────────┐
             ▼                 ▼                     ▼
           Trade              ERP                  Pulse
          Medusa            iDempiere             Haystack
             │                 │                     │
             └─────────────────┼─────────────────────┘
                               ▼
                         Observed State
                               │
                               ▼
                         Reconciliation
                               │
                               ▼
                           Readiness
                               │
                    ┌──────────┴──────────┐
                    ▼                     ▼
                 BLOCKED                READY
                    │                     │
                    ▼                     ▼
               Remediation             ACTIVE
```

---

# 117. Architectural Consequence

After this specification is implemented, onboarding a new customer SHALL predominantly become a **declarative configuration exercise**, not a Control Plane development project.

ZuriBeans:

```text
XBT
+ B2B
+ Cross-Border
+ Commodity Trade
+ Supplier Management
```

Coal haulier:

```text
XBT
+ B2B
+ Cross-Border
+ Logistics
```

Oil trader:

```text
XBT
+ B2B
+ Cross-Border
+ Commodity Trade
+ Advanced Intelligence
```

The Control Plane algorithm remains unchanged.

---

# 118. Final Implementation Principle

> **Baobab shall onboard organisations by composing canonical context, products, capabilities, scopes and providers — not by teaching the Control Plane about individual industries or tenants.**

And:

> **The individual ADR Gates are requirements. This specification is the programme that integrates them.**

And finally:

> **A tenant is not onboarded because its database record exists. A tenant is onboarded when its required capability composition has been provisioned, secured, reconciled, proven ready and activated through an auditable control-plane process.**