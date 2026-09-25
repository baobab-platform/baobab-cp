# ADR-BCP-010 — Modular Control Plane Architecture, Governance Boundaries and Evolution Model

**Status:** Proposed — Normative Platform Architecture  
**Date:** 2026-09-11  
**Repository:** `baobab-platform/baobab-cp`  
**Related Repositories:** `baobab-platform/shared`, `baobab-platform/baobab-iam`, domain provider repositories  
**Supersedes:** Informal module organisation and ad hoc expansion of Control Plane responsibilities  
**Related ADRs:** ADR-BCP-001 through ADR-BCP-009, ADR-SHARED-007  

---

# 1. Context

The Baobab Control Plane has evolved from a relatively small orchestration component into the authoritative platform layer responsible for:

- canonical tenant and organisational context;
- Digital Estate registration;
- product subscriptions;
- capability entitlements;
- capability-provider resolution;
- engine topology;
- provisioning;
- readiness;
- reconciliation;
- isolation;
- residency;
- audit;
- platform lifecycle and runtime context resolution.

As this scope grows, there is a material architectural risk that `baobab-cp` becomes:

```text
a large undifferentiated application
```

or worse:

```text
a new ERP
a new workflow engine
a business-domain monolith
an integration hub
a universal API gateway
```

The platform therefore requires a normative module architecture defining:

1. which concerns belong in the Control Plane;
2. which concerns remain outside it;
3. how modules interact;
4. how new capabilities are introduced;
5. how platform governance evolves without coupling Digital Estates directly to providers;
6. how the modular monolith can later be decomposed if scaling or operational requirements justify it.

---

# 2. Decision

`baobab-cp` SHALL be organised as a **modular control-plane kernel**.

The Control Plane MAY grow broad in **platform governance**, but SHALL remain deliberately shallow in **business-domain behaviour**.

The guiding principle is:

> **The Control Plane governs what should be available, where it may operate, who may consume it, which implementation may provide it, and whether the platform is ready. Domain providers remain responsible for performing the business operation itself.**

The target module families SHALL be:

```text
baobab-cp
│
├── foundation
│   ├── registry
│   ├── tenancy
│   ├── organisation
│   ├── mapping
│   └── contract
│
├── context
│   ├── market
│   ├── estate
│   ├── context-resolution
│   ├── residency
│   └── isolation
│
├── commercial-control
│   ├── product
│   ├── subscription
│   ├── entitlement
│   ├── metering
│   └── quota
│
├── execution-control
│   ├── capability
│   ├── provider
│   ├── topology
│   ├── placement
│   └── rollout
│
├── orchestration
│   ├── provisioning
│   ├── changeset
│   ├── lifecycle
│   ├── reconciliation
│   └── readiness
│
├── governance
│   ├── policy
│   ├── security
│   ├── approval
│   ├── impact-analysis
│   └── provider-certification
│
└── operations
    ├── audit
    ├── eventing
    ├── observability
    ├── maintenance
    └── incident-control
```

Not every module must be implemented immediately.

The architecture defines their intended boundaries and dependency direction.

---

# 3. Architectural Position

The Control Plane SHALL remain a modular monolith unless explicit operational evidence justifies decomposition.

The default model is:

```text
                     BAOBAB CONTROL PLANE
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                    │
        ▼                    ▼                    ▼
   Foundation            Governance          Orchestration
        │                    │                    │
        └────────────────────┼────────────────────┘
                             ▼
                       Execution Control
                             │
                             ▼
                        Domain Providers
```

A module boundary SHALL be treated as an architectural boundary even when modules share the same Go process and PostgreSQL cluster.

---

# 4. Core Architectural Rule

Every new Control Plane feature SHALL answer:

> **Is this a platform governance concern or a business-domain concern?**

If the answer is business-domain concern, it SHALL normally remain outside `baobab-cp`.

Examples that belong in CP:

```text
Tenant X is entitled to capability Y.
Capability Y resolves to provider Z.
Provider Z is incompatible with isolation policy P.
Estate E is not ready.
Subscription S grants capabilities A, B and C.
Region R cannot satisfy residency requirements.
```

Examples that do not belong in CP:

```text
Approve purchase order 123.
Calculate VAT on invoice 456.
Allocate inventory item 789.
Approve supplier commercial terms.
Dispatch truck ABC.
Post ERP journal J123.
Accept petroleum inspection certificate.
```

---

# 5. Module Family — Foundation

## 5.1 Registry

The Registry module SHALL own canonical registration of platform-level resources.

Examples:

```text
Tenant
LegalEntity
CanonicalEntity
DigitalEstate identity
Product identity
Provider identity
Engine identity
```

Registry SHALL NOT own domain business records.

---

## 5.2 Tenancy

The Tenancy module SHALL own:

- tenant lifecycle;
- tenant status;
- tenant relationships;
- default tenant isolation requirements;
- tenant activation/suspension/deprovisioning state.

It SHALL NOT equate:

```text
Tenant = LegalEntity
```

universally.

---

## 5.3 Organisation

The Organisation module SHALL own canonical representations of:

```text
LegalEntity
Organisation
BusinessUnit
Parent / subsidiary relationships
```

Corporate relationships SHALL NOT imply operational authority.

Example:

```text
NABHOLD
├── ZuriBeans
└── Thamani
```

does not mean:

```text
ZuriBeans may access Thamani data
```

or vice versa.

---

## 5.4 Mapping

The Mapping module SHALL own:

```text
CanonicalEntity
ExternalReference
Mapping
MappingScope
```

Its purpose is provider interoperability.

It SHALL not become a generic transformation or ETL engine.

---

## 5.5 Contract

A dedicated Contract Compatibility module SHALL be introduced.

It SHALL govern:

```text
CapabilityContract
ContractVersion
ConsumerRequirement
ProviderContractSupport
CompatibilityPolicy
DeprecationPolicy
```

The module SHALL work with `baobab-platform/shared`, which remains the canonical contract authority.

Example:

```text
Digital Estate
requires
commerce.order.create
contract >= 2.1 < 3.0

Provider A → 2.3 ✓
Provider B → 1.9 ✗
```

A provider SHALL NOT be considered eligible merely because it advertises the same capability key.

---

# 6. Module Family — Context

## 6.1 Market

The Market module SHALL own:

```text
Market
Jurisdiction
MarketParticipation
Operating footprint
Commercial footprint
```

It SHALL preserve the distinction between:

```text
market
country
jurisdiction
legal presence
transaction geography
```

---

## 6.2 Estate

The Estate module SHALL own:

```text
DigitalEstate
DigitalProperty
Channel
Estate relationships
Estate requirements
```

A Digital Estate is a capability consumer.

It SHALL NOT be treated as synonymous with tenant or legal entity.

---

## 6.3 Estate Manifest

The Control Plane SHOULD introduce a first-class Estate Manifest.

Example:

```yaml
estate: zuribeans-b2b

requires:
  - commerce.catalogue.read
  - commerce.order.create
  - finance.invoice.read

optional:
  - intelligence.market.query
```

The Estate Manifest SHALL describe what an estate requires, not what it is already entitled to.

The distinction is:

```text
Estate Requirement
        │
        ▼
Capability Grant
        │
        ▼
Capability Binding
        │
        ▼
Readiness
```

---

## 6.4 Context Resolution

The Context module SHALL remain authoritative for canonical operational context.

It SHALL resolve dimensions such as:

```text
Principal
Tenant
LegalEntity
Organisation
DigitalEstate
DigitalProperty
Channel
Market
Jurisdiction
Currency
DeploymentRegion
Environment
Isolation
```

It SHALL not infer legal or geographic facts from unrelated dimensions.

---

## 6.5 Residency

The Residency module SHALL own:

```text
DataResidencyPolicy
AllowedRegion
ProhibitedRegion
Replication restriction
Backup residency
DR residency
```

Business geography SHALL not automatically determine data residency.

---

## 6.6 Isolation

The Isolation module SHALL own canonical isolation policies.

Candidate levels include:

```text
SHARED_LOGICAL
DEDICATED_SCHEMA
DEDICATED_DATABASE
DEDICATED_INSTANCE
DEDICATED_DEPLOYMENT
```

Provider selection SHALL satisfy the required isolation profile.

No silent downgrade is permitted.

---

# 7. Module Family — Commercial Control

## 7.1 Product

The Product module SHALL own:

```text
Product
ProductVersion
CapabilityComposition
Profile
Add-on
```

Products package capabilities.

Products SHALL NOT embed provider implementation details.

---

## 7.2 Subscription

The Subscription module SHALL represent commercial or administrative activation of a ProductVersion.

Subscription states MAY include:

```text
PENDING
PROVISIONING
ACTIVE
SUSPENDED
CANCELLED
EXPIRED
FAILED
```

Subscription SHALL not be used directly as the runtime authorization primitive.

---

## 7.3 Entitlement

The Entitlement module SHALL own capability grants derived from:

```text
subscription
internal platform grant
manual grant
partner agreement
migration
trial
contract override
```

The module SHALL retain grant provenance.

---

# 8. Metering Module

The Control Plane SHOULD introduce a Metering module before introducing billing.

Metering records platform usage such as:

```text
capability invocation count
provider usage
API consumption
AI inference usage
document processing
storage allocation
active seats
transaction volume
```

Metering answers:

> What was consumed?

It SHALL NOT calculate financial invoices.

The intended relationship is:

```text
Capability Usage
      │
      ▼
Metering
      │
      ▼
Usage Events
      │
      ▼
Future Billing / ERP
```

---

# 9. Quota Module

The Quota module SHALL be distinct from entitlement.

Entitlement answers:

> May the tenant use this capability?

Quota answers:

> How much may the tenant consume?

Example:

```text
Capability:
intelligence.market.query

Entitled:
YES

Monthly quota:
50,000

Consumed:
50,000

Runtime result:
QUOTA_EXCEEDED
```

A quota failure SHALL not be reported as:

```text
CAPABILITY_NOT_GRANTED
```

---

# 10. Module Family — Execution Control

## 10.1 Capability

The Capability module remains the central execution abstraction.

It SHALL own:

```text
CapabilityDefinition
CapabilityDependency
CapabilityGrant
CapabilityScope
CapabilityBinding
```

The canonical relationship remains:

```text
Capability
     │
     ▼
Grant
     │
     ▼
Scope
     │
     ▼
Binding
     │
     ▼
Provider
```

---

# 11. Provider Module

The Provider module SHALL own canonical provider identity and provider capability support.

A provider represents a Baobab-recognised implementation of one or more capabilities.

Example:

```text
Provider:
baobab-trade.medusa

Engine:
medusa
```

Provider SHALL be distinct from Engine and EngineInstance.

---

# 12. Provider Certification

A Provider Certification module SHALL be introduced before Baobab permits broad third-party provider registration.

Provider lifecycle SHOULD include:

```text
REGISTERED
      │
      ▼
VALIDATING
      │
      ▼
CERTIFIED
      │
      ▼
ACTIVE
```

Certification MAY evaluate:

```text
contract conformance
security requirements
isolation support
residency capabilities
observability
health behaviour
failover characteristics
performance baseline
```

Provider eligibility SHOULD eventually require:

```text
lifecycle = ACTIVE
AND
certification = VALID
```

---

# 13. Topology Module

The Topology module SHALL own:

```text
Engine
EngineInstance
DeploymentRegion
Environment
EngineInstanceHealth
Operational state
```

It SHALL describe runtime topology, not business state.

---

# 14. Placement Module

The Control Plane SHOULD introduce a Placement module when multi-region scale requires it.

Placement SHALL determine where a provider instance or tenant workload may safely operate.

Example:

```text
New Tenant
    │
    ▼
Requirements
├── Africa residency
├── dedicated database
└── high availability
    │
    ▼
Placement
├── ZA → eligible
├── UG → isolation mismatch
└── EU → residency prohibited
    │
    ▼
ZA selected
```

Placement decisions SHALL be deterministic and policy governed.

---

# 15. Placement Decision Order

Mandatory constraints SHALL always precede optimisation.

```text
security
   ↓
residency
   ↓
isolation
   ↓
contract compatibility
   ↓
health
   ↓
capacity
   ↓
latency/cost optimisation
```

A cheaper provider SHALL never override a mandatory security or residency constraint.

---

# 16. Capacity Management

The topology/placement architecture SHOULD support provider capacity declarations.

Example:

```text
EngineInstance
├── capacity_class
├── allocated_capacity
├── saturation
└── admission_state
```

A provider may be:

```text
healthy
compatible
secure
```

yet still unable to accept another tenant.

Expected result:

```text
CAPACITY_NOT_AVAILABLE
```

rather than unsafe over-allocation.

---

# 17. Rollout Module

A controlled capability/provider rollout module SHOULD support staged adoption.

Candidate scopes:

```text
tenant
legal entity
Digital Estate
market
region
provider cohort
contract version
```

Example:

```text
New provider version
      │
      ├── internal tenant
      ├── ZuriBeans
      ├── pilot cohort
      └── general availability
```

Rollout SHALL not become a generic frontend feature-flag service.

---

# 18. Module Family — Orchestration

## 18.1 Provisioning

Provisioning SHALL continue to turn declarative desired state into platform state.

It SHALL support:

```text
plan
apply
observe
reconcile
verify
activate
```

Provisioning SHALL not become a domain business workflow engine.

---

# 19. ChangeSet Module

The Control Plane SHOULD generalise plan-before-apply into a first-class `ChangeSet`.

Conceptual model:

```text
ChangeSet
├── requested_changes
├── calculated_effects
├── affected_resources
├── affected_tenants
├── affected_estates
├── affected_capabilities
├── security_effects
├── readiness_effects
├── rollback_strategy
└── status
```

Tenant onboarding becomes one ChangeSet category.

Other examples:

```text
provider migration
product upgrade
region migration
capability addition
tenant suspension
estate expansion
```

---

# 20. Canonical Change Workflow

High-impact platform changes SHOULD follow:

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
APPROVAL
   │
   ▼
APPLY
   │
   ▼
RECONCILE
   │
   ▼
VERIFY
```

---

# 21. Lifecycle Module

The Lifecycle module SHALL govern legal state transitions across major CP resources.

Examples:

## Tenant

```text
DRAFT
  ↓
PROVISIONING
  ↓
ACTIVE
  ↓
SUSPENDED
  ↓
DEPROVISIONING
  ↓
DEPROVISIONED
```

## Provider

```text
DRAFT
  ↓
ACTIVE
  ↓
DEPRECATED
  ↓
RETIRED
```

## Product Version

```text
DRAFT
  ↓
PUBLISHED
  ↓
DEPRECATED
  ↓
WITHDRAWN
```

The Lifecycle module SHALL prevent invalid transitions.

---

# 22. Reconciliation

Reconciliation SHALL compare:

```text
Desired State
vs
Observed State
```

and produce:

```text
Drift
```

The reconciler may safely repair platform configuration.

It SHALL NOT autonomously alter business domain data such as:

```text
orders
contracts
invoices
shipments
supplier approval decisions
```

---

# 23. Readiness

Readiness SHALL remain hierarchical:

```text
ProviderReadiness
CapabilityReadiness
CompositionReadiness
ProductReadiness
EstateReadiness
TenantReadiness
PlatformReadiness
```

Readiness SHALL not be reduced to provider health.

---

# 24. Module Family — Governance

## 24.1 Policy

The Policy module SHALL govern deterministic platform-level rules.

Examples:

```text
required isolation
allowed regions
provider eligibility
contract compatibility
administrative approval requirements
```

It SHALL not absorb domain business policies.

---

# 25. Policy Simulation

The Control Plane SHOULD support policy simulation before applying major policy changes.

Example:

```text
Proposed Policy:
finance capabilities require DEDICATED_DATABASE
```

Simulation output:

```text
Affected tenants: 17
Still ready: 13
Blocked: 4

3 → provider isolation incompatible
1 → residency mismatch
```

Policy simulation SHALL reuse impact-analysis infrastructure.

---

# 26. Security

The Security module SHALL own platform authorization semantics, revocation propagation, administrative security and delegation rules.

IAM remains responsible for:

```text
identity
authentication
credentials
sessions
MFA
OIDC/OAuth
workload authentication
```

CP Security remains responsible for:

```text
tenant context
legal-entity context
capability entitlement
provider eligibility
isolation
residency
revocation of platform authority
```

---

# 27. Approval and Administrative Governance

High-risk platform changes SHOULD support controlled approval.

Candidate operations include:

```text
change isolation profile
change residency policy
replace finance provider
grant privileged capability
suspend tenant
retire provider
move production workload between regions
```

Possible governance rules:

```text
automatic approval
single administrator
security approval
two-person approval
break-glass
```

All SHALL be auditable.

---

# 28. Impact Analysis

A first-class dependency/impact graph SHALL be available.

Forward relationship:

```text
Tenant
 ↓
Subscription
 ↓
Product
 ↓
Composition
 ↓
Capability
 ↓
Grant
 ↓
Binding
 ↓
Provider
 ↓
EngineInstance
```

Reverse impact:

```text
EngineInstance failure
        │
        ▼
Provider
        │
        ▼
Bindings
        │
        ▼
Capabilities
        │
        ▼
Tenants
        │
        ▼
Digital Estates
```

Impact analysis SHALL support:

```text
migration planning
incident response
deprecation
ChangeSets
policy simulation
readiness explanation
```

---

# 29. Module Family — Operations

## 29.1 Audit

Audit SHALL remain append-oriented and authoritative for control-plane changes and decisions.

It SHALL answer:

```text
who
did what
to which resource
for which tenant
when
why
with what result
```

---

# 30. Eventing

Eventing SHALL own:

```text
transactional outbox
canonical control-plane events
delivery status
idempotency metadata
```

Canonical event vocabulary remains defined by `baobab-platform/shared`.

---

# 31. Observability

The Observability module SHALL expose:

```text
metrics
traces
structured logs
correlation identifiers
resolution identifiers
readiness state
reconciliation state
provider state
```

OpenTelemetry-compatible conventions SHOULD be preferred.

---

# 32. Maintenance

The Control Plane SHOULD model maintenance explicitly.

Concepts MAY include:

```text
MaintenanceWindow
DrainPolicy
OperationalRestriction
```

A provider in scheduled maintenance SHALL not necessarily be treated the same as an unexpected outage.

---

# 33. Incident Control

The operations architecture SHOULD permit containment at:

```text
tenant
legal entity
Digital Estate
capability
provider
engine instance
region
```

This enables narrow response to incidents.

Example:

```text
quarantine one provider
```

rather than:

```text
disable the entire platform
```

---

# 34. Tenant Blueprint Module

The Control Plane SHOULD eventually support reusable onboarding blueprints.

Examples:

```text
blueprint.xbt-b2b-commodity
blueprint.xbt-logistics
blueprint.commerce-b2c
```

A blueprint MAY propose:

```text
products
profiles
estate types
default capabilities
default isolation
recommended providers
```

A blueprint SHALL NOT become an immutable tenant classification.

The relationship SHALL be:

```text
Blueprint
   ↓
Desired State Draft
   ↓
Tenant-specific validation
   ↓
Provisioning Plan
```

---

# 35. Service Catalogue

The Control Plane SHOULD distinguish:

```text
Capability Catalogue
```

from:

```text
Service Catalogue
```

Capability Catalogue answers:

> What can Baobab do?

Service Catalogue answers:

> Which actual platform services implement and expose those capabilities?

Example:

```text
Capability
finance.invoice.manage

Provider
baobab-erp.idempiere

Service
erp-finance-api

Contract
finance-api/v2

Instances
ZA-PROD-01
UG-PROD-01
```

---

# 36. Commercial Entitlement Separation

As SaaS maturity increases, commercial entitlement SHOULD be separated from technical capability entitlement.

Potential relationship:

```text
Subscription
      │
      ▼
Commercial Entitlement
      │
      ▼
Capability Grants
```

This enables:

```text
trial plans
grandfathered products
partner agreements
enterprise overrides
contract-specific access
promotional entitlements
```

without contaminating runtime resolution logic.

---

# 37. Explicit Non-Goals

The Control Plane SHALL NOT become:

```text
an ERP
a commerce platform
a CMS
a logistics execution system
an accounting ledger
a customer master database
an inventory engine
a business approval engine
a generic ETL platform
a universal data-plane proxy
a monolithic IAM replacement
a generic feature-flag platform
```

---

# 38. Dependency Direction

Module dependencies SHOULD flow toward foundational abstractions.

Preferred direction:

```text
Operations
    ↓
Governance
    ↓
Orchestration
    ↓
Execution Control
    ↓
Commercial / Context
    ↓
Foundation
```

Foundation modules SHALL not depend on higher-level orchestration modules.

For example:

```text
registry
```

SHALL NOT depend on:

```text
provisioning
```

---

# 39. No Direct Database Coupling Across Modules

Modules MAY reside in one PostgreSQL database, but SHALL communicate through module services/repositories/contracts rather than arbitrary cross-module table access.

This prevents:

```text
product module
directly updating capability_binding table
```

Instead:

```text
Product
   │
   ▼
Entitlement Service
   │
   ▼
Capability module
```

---

# 40. Module Ownership of Data

Each table or aggregate SHALL have exactly one owning module.

Other modules MAY:

```text
reference
query through defined interface
subscribe to events
```

but SHALL not independently mutate that state.

---

# 41. Module APIs

Each module SHOULD expose:

```text
application service interface
domain interface
repository interface
event interface
```

where appropriate.

Example Go structure:

```text
internal/
├── capability/
│   ├── domain/
│   ├── application/
│   ├── ports/
│   ├── adapters/
│   └── api/
│
├── product/
├── context/
├── topology/
├── provisioning/
└── ...
```

Exact package organisation may evolve, but architectural dependencies SHALL remain explicit.

---

# 42. Cross-Module Commands

Cross-module mutations SHOULD be coordinated through application services rather than implicit persistence hooks.

Example:

```text
Activate Subscription
        │
        ▼
Product Application Service
        │
        ▼
Entitlement Expansion
        │
        ▼
Capability Grants
        │
        ▼
Readiness Recalculation
```

---

# 43. Cross-Module Events

Asynchronous reactions SHOULD use canonical internal/domain events where appropriate.

Example:

```text
SubscriptionActivated
        │
        ▼
GrantMaterialisationRequested
        │
        ▼
CapabilityGrantsChanged
        │
        ▼
ReadinessRecalculationRequested
```

Not every in-process operation requires asynchronous events.

Simple transactional coordination SHOULD remain simple.

---

# 44. Avoid Distributed-Monolith Architecture

The modular architecture SHALL NOT be prematurely decomposed into dozens of network services.

A module should become an independent service only when justified by factors such as:

```text
independent scaling
security boundary
availability requirement
independent deployment cadence
specialised infrastructure
team ownership
regulatory isolation
```

Not merely because a module exists.

---

# 45. Candidate Future Service Extraction

Possible future extraction candidates include:

```text
metering
high-volume event processing
reconciliation workers
placement
policy evaluation
```

but no extraction is mandated by this ADR.

---

# 46. Module Maturity Classification

Control Plane modules SHOULD be classified:

```text
FOUNDATIONAL
CORE
ADVANCED
FUTURE
```

Recommended initial classification:

## FOUNDATIONAL

```text
registry
tenancy
organisation
mapping
market
estate
product
capability
topology
context
security
```

## CORE

```text
subscription
entitlement
provisioning
reconciliation
readiness
audit
eventing
contract
lifecycle
```

## ADVANCED

```text
changeset
impact-analysis
approval
provider-certification
estate-manifest
quota
maintenance
rollout
```

## FUTURE

```text
metering
placement
capacity optimisation
commercial entitlement
blueprints
cost-aware routing
```

This classification is sequencing guidance, not a permanent architectural limitation.

---

# 47. Implementation Priority

The next Control Plane expansion SHOULD prioritise:

```text
1. Contract Compatibility
2. Lifecycle
3. Impact / Dependency Graph
4. ChangeSet
5. Governance / Approval
6. Provider Certification
7. Estate Manifest
```

before significant work on:

```text
metering
quota
placement
cost optimisation
```

---

# 48. Why Contract Compatibility Is High Priority

The capability model currently risks treating:

```text
same capability key
```

as sufficient interoperability.

That will become unsafe as providers and Digital Estates independently evolve.

The platform SHALL eventually resolve:

```text
Capability
+
Contract Requirement
+
Provider Contract Support
```

not merely capability name.

---

# 49. Why Lifecycle Is High Priority

Lifecycle semantics already exist independently for:

```text
Tenant
Provider
Engine
EngineInstance
ProductVersion
Subscription
Grant
Binding
Provisioning
```

Without a consistent lifecycle discipline, impossible combinations will proliferate.

Examples:

```text
RETIRED provider + ACTIVE binding
SUSPENDED tenant + ACTIVE estate
WITHDRAWN product version + new subscription
DECOMMISSIONED instance + PRIMARY binding
```

The Lifecycle module SHALL prevent or explicitly govern these states.

---

# 50. Why Impact Analysis Is High Priority

The Control Plane increasingly needs to answer:

> What will break if I change this?

Examples:

```text
retire provider
change product composition
move region
change isolation requirement
revoke capability
deprecate contract version
```

Impact analysis is therefore foundational to safe governance.

---

# 51. Reference Tenant Validation

The modular architecture SHALL continue to be validated using:

```text
ZuriBeans
Coal-Hauling Business
Oil Trader
```

No module SHALL assume a particular industry.

---

# 52. ZuriBeans Validation

ZuriBeans SHALL exercise:

```text
B2B
supplier onboarding
commodity trading
cross-border operation
ERP
commerce
content
intelligence
```

---

# 53. Coal-Haulier Validation

Coal-hauling SHALL exercise:

```text
logistics
dispatch
routes
loads
cross-border transit
proof of delivery
job profitability
```

and SHALL expose any hidden commerce-specific assumptions.

---

# 54. Oil-Trader Validation

Oil trading SHALL exercise:

```text
multi-jurisdiction commodity trade
advanced intelligence
settlement
inspection documents
cross-border execution
provider gaps
```

---

# 55. Security Boundary

The architectural security chain remains:

```text
IAM
 │
 ▼
Authenticated Principal
 │
 ▼
Control Plane
 │
 ├── Context
 ├── Entitlement
 ├── Provider
 ├── Isolation
 └── Residency
 │
 ▼
Domain Provider
 │
 ▼
Business Authorization
```

Neither CP nor IAM SHALL absorb the other's responsibilities.

---

# 56. Governance Rule for New Modules

A proposed new CP module SHALL be accepted only if it satisfies all of the following:

1. It governs platform state rather than business transaction state.
2. Its authority is not already owned by a domain engine.
3. Its boundary can be clearly described.
4. Its authoritative data can be identified.
5. Its lifecycle can be defined.
6. Its relationship to Shared contracts can be stated.
7. Its relationship to IAM can be stated.
8. Its impact on tenant isolation is understood.
9. Its failure semantics are defined.
10. It does not introduce provider-specific assumptions into generic CP logic.

---

# 57. Module Review Checklist

Before implementing any new module, document:

```text
Purpose
Authority
Owned aggregates
Owned tables
Inbound interfaces
Outbound interfaces
Events
Security boundary
Tenant isolation model
Lifecycle
Failure semantics
Caching
Observability
Reconciliation responsibilities
Shared contract dependencies
Provider dependencies
Non-goals
```

---

# 58. Consequences

## Positive

The modular architecture provides:

- clearer ownership;
- safer growth;
- reduced cross-domain coupling;
- controlled Control Plane scope;
- better migration planning;
- testable module boundaries;
- easier future service extraction;
- more reliable tenant isolation;
- more predictable provider replacement;
- improved operational governance;
- cleaner onboarding;
- stronger SaaS evolution path.

## Negative

The architecture introduces:

- more explicit internal contracts;
- stricter module boundaries;
- more up-front modelling;
- additional lifecycle and governance work;
- potentially more internal coordination code.

These costs are accepted because the alternative is uncontrolled architectural coupling.

---

# 59. Rejected Alternative — Single Large Control Plane Domain

Rejected because:

```text
everything imports everything
```

eventually destroys ownership boundaries and makes safe evolution difficult.

---

# 60. Rejected Alternative — Microservice per Module

Rejected as the default.

This would introduce premature:

```text
network complexity
distributed transactions
deployment overhead
failure modes
observability burden
```

without proven operational need.

---

# 61. Rejected Alternative — Provider-Centric Architecture

Rejected.

Digital Estates SHALL depend on:

```text
capabilities
```

not:

```text
Medusa
iDempiere
Payload
Haystack
Keycloak
```

---

# 62. Rejected Alternative — Industry-Specific Control Plane Modules

Examples rejected:

```text
coffee module
coal module
oil module
```

Industry differences SHALL primarily be expressed through:

```text
capability compositions
profiles
providers
configuration
domain services
```

---

# 63. Rejected Alternative — Put Billing Inside CP

Rejected.

CP may own:

```text
metering
quota
entitlement
```

but accounting and financial billing belong elsewhere.

---

# 64. Rejected Alternative — Let IAM Own Platform Entitlements

Rejected.

IAM owns identity and authentication.

CP owns capability and contextual entitlement.

Domain engines own business authorization.

---

# 65. Rejected Alternative — Use Database Tables as Module APIs

Rejected.

Shared database deployment does not justify uncontrolled cross-module persistence access.

Module ownership remains explicit.

---

# 66. Target Long-Term Shape

```text
                       BAOBAB CONTROL PLANE
                                │
         ┌──────────────────────┼──────────────────────┐
         │                      │                      │
         ▼                      ▼                      ▼
    FOUNDATION              CONTEXT               COMMERCIAL
         │                      │                      │
 Registry/Tenancy       Market/Estate           Product
 Organisation           Isolation               Subscription
 Mapping                Residency               Entitlement
 Contract               Context                 Metering/Quota
         │                      │                      │
         └──────────────────────┼──────────────────────┘
                                ▼
                       EXECUTION CONTROL
                                │
                  Capability / Provider / Topology
                                │
                                ▼
                         ORCHESTRATION
                                │
             Provisioning / ChangeSet / Lifecycle
                  Reconciliation / Readiness
                                │
                                ▼
                           GOVERNANCE
                                │
             Policy / Security / Approval
             Impact / Provider Certification
                                │
                                ▼
                           OPERATIONS
                                │
        Audit / Eventing / Observability / Maintenance
                                │
                                ▼
                          DOMAIN PROVIDERS
                  ┌────────┬────────┬────────┐
                  ▼        ▼        ▼        ▼
                Trade     ERP      CMS      Pulse
```

---

# 67. Decision Summary

The Control Plane SHALL evolve as a **modular platform-governance kernel**.

The implementation SHALL preserve the distinction:

```text
Platform Governance
        ≠
Business Execution
```

The Control Plane may determine:

```text
what
who
where
which provider
under which policy
whether ready
```

The domain provider determines:

```text
how the business transaction is executed
```

---

# 68. Architectural Maxims

> **The Control Plane shall be broad in governance, narrow in business behaviour, and explicit in authority.**

> **A module owns one category of platform truth; shared deployment does not imply shared ownership.**

> **Capabilities are the stable contract, providers are replaceable implementations, and modules exist to preserve that separation.**

> **No new Control Plane module shall be introduced merely because implementing the concern somewhere in `baobab-cp` is convenient. It must represent a genuine platform authority.**

> **The modular monolith is the default. Distribution is an operational decision, not an architectural fashion.**