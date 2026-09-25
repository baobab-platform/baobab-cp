# ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model

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
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model

**Applies To:** Baobab products, solution profiles, capability compositions, product subscriptions, capability grants, Digital Estate provisioning, readiness, lifecycle, upgrade and deprovisioning  
**Architecture Style:** Declarative product composition, explicit entitlement, context-aware provisioning, capability-first SaaS control plane  
**Decision Type:** Foundational product and provisioning architecture

---

# 1. Executive Decision

Baobab SHALL separate the following concepts:

```text
Product
CapabilityComposition
Subscription
CapabilityGrant
CapabilityBinding
DigitalEstate
ProvisioningState
Readiness
```

They SHALL NOT be treated as aliases.

The governing model is:

```text
Commercial Product / Solution
          │
          ▼
Capability Composition
          │
          ▼
Subscription
          │
          ▼
Capability Grants
          │
          ▼
Provider / Binding Resolution
          │
          ▼
Digital Estate Readiness
```

A product describes what is commercially offered.

A composition describes which capabilities form that offer.

A subscription establishes that a tenant has contracted for or been assigned the offer.

A grant establishes runtime entitlement.

A binding determines which provider may fulfil a granted capability.

A Digital Estate consumes capabilities.

Readiness determines whether the subscribed product can actually operate in the tenant's current context.

The primary architectural rule is:

> **Products package capabilities; subscriptions activate commercial intent; grants establish runtime entitlement; bindings provide implementation; Digital Estates consume the resulting capabilities.**

---

# 2. Why This Separation Is Necessary

Without this ADR, Baobab could easily drift into one of several incorrect models.

## 2.1 Product equals engine

Example:

```text
Baobab Trade
=
MedusaJS
```

Rejected.

MedusaJS is a provider technology, not a commercial product identity.

---

## 2.2 Subscription equals permission

Example:

```text
Tenant subscribed to XBT
therefore all XBT APIs are available
```

Rejected.

Subscriptions SHALL be transformed into explicit scoped capability grants.

---

## 2.3 Digital Estate equals product

Example:

```text
ZuriBeans Estate
=
XBT
```

Rejected.

A Digital Estate MAY consume capabilities originating from several products or profiles.

---

## 2.4 Product provisioning equals infrastructure provisioning

Rejected.

Commercial activation, capability grant materialisation, provider readiness and infrastructure reconciliation are separate phases.

---

# 3. Core Conceptual Model

The canonical relationship SHALL be:

```text
Product
   │
   ▼
ProductVersion
   │
   ▼
CapabilityComposition
   │
   ├── required capabilities
   ├── optional capabilities
   ├── required profiles
   ├── optional profiles
   └── dependencies
   │
   ▼
Subscription
   │
   ▼
Entitlement Expansion
   │
   ▼
CapabilityGrant
   │
   ▼
CapabilityBinding
   │
   ▼
Provider / EngineInstance
   │
   ▼
Digital Estate Consumption
```

---

# 4. Product

A Product SHALL represent a commercial or platform-level offering.

Conceptually:

```text
Product
├── id
├── product_key
├── name
├── description
├── product_type
├── lifecycle_status
├── current_version
├── commercial_metadata
├── created_at
└── updated_at
```

Examples:

```text
solution.baobab-xbt
solution.baobab-commerce
solution.baobab-intelligence
```

The product model SHALL remain implementation-neutral.

---

# 5. Product Type

Initial product types MAY include:

```text
PLATFORM
SOLUTION
MODULE
ADD_ON
INTERNAL
```

Examples:

```text
PLATFORM
    Baobab Core

SOLUTION
    Baobab XBT

ADD_ON
    Advanced Intelligence
```

Product type SHALL be commercial/governance metadata, not runtime behaviour.

---

# 6. Product Version

Products SHALL be versioned.

A ProductVersion SHALL define the commercial/capability composition for a specific version.

Conceptually:

```text
ProductVersion
├── id
├── product_id
├── version
├── composition_id
├── effective_from
├── effective_to
├── status
├── upgrade_policy
└── metadata
```

This allows Baobab to evolve product packaging without mutating historical subscriptions.

---

# 7. Product Versioning Rule

A product version change SHALL NOT silently mutate active subscriptions.

Instead:

```text
Product v1
       │
       ▼
Tenant subscription v1
```

remains valid until:

```text
upgrade
migration
renewal
explicit policy
```

moves the subscription to v2.

---

# 8. Capability Composition

ProductVersion SHALL reference a canonical CapabilityComposition.

Example:

```text
Baobab XBT v1
       │
       ▼
solution.baobab-xbt@1.0
```

The composition SHALL remain defined canonically in `baobab-platform/shared`.

CP SHALL materialise runtime relationships from that contract.

---

# 9. Composition Structure

A composition MAY contain:

```text
required capabilities
optional capabilities
required sub-compositions
optional profiles
incompatible profiles
version requirements
activation conditions
```

Example:

```text
solution.baobab-xbt
│
├── platform.core
├── commercial.core
├── finance.core
└── intelligence.core
```

with optional profiles:

```text
profile.b2b
profile.b2c
profile.crossborder
profile.logistics
profile.commodity-trade
profile.supplier-management
```

---

# 10. Product Profiles

Profiles SHALL be reusable compositions.

They SHALL NOT be tenant classes.

For example:

```text
profile.b2b
```

describes capabilities commonly required for B2B operations.

It does NOT mean:

```text
tenant.type = B2B
```

A tenant MAY subscribe to or activate multiple profiles simultaneously.

---

# 11. Baobab XBT

Baobab XBT SHALL be represented as a product/solution composed from capabilities.

It SHALL NOT become:

```text
another orchestration service
another database
another workflow runtime
```

Conceptually:

```text
BAOBAB XBT
│
├── Core Business Capabilities
│
├── B2B Profile
│
├── B2C Profile
│
├── Cross-Border Profile
│
├── Logistics Profile
│
├── Commodity Trade Profile
│
└── Supplier Management Profile
```

---

# 12. Product ≠ Profile

A profile MAY be included in more than one product.

For example:

```text
profile.crossborder
```

could be reused by:

```text
Baobab XBT
future logistics solution
future procurement solution
```

The model SHALL therefore avoid embedding profile definitions inside product-specific application code.

---

# 13. Subscription

A Subscription SHALL represent the commercial or administrative activation of a ProductVersion for a tenant.

Conceptually:

```text
ProductSubscription
├── id
├── tenant_id
├── product_id
├── product_version_id
├── status
├── subscription_type
├── effective_from
├── effective_to
├── billing_reference
├── contract_reference
├── configuration
├── created_at
└── updated_at
```

---

# 14. Subscription Type

Initial types MAY include:

```text
COMMERCIAL
INTERNAL
TRIAL
PARTNER
MANUAL
MIGRATION
```

The type SHALL not alter capability semantics.

It may influence lifecycle or billing behaviour.

---

# 15. Subscription Lifecycle

Recommended states:

```text
PENDING
PROVISIONING
ACTIVE
SUSPENDED
CANCELLED
EXPIRED
FAILED
```

A subscription SHALL not become `ACTIVE` until mandatory readiness criteria pass.

---

# 16. Subscription Does Not Directly Authorize Runtime Calls

This is a central decision.

The runtime resolver SHALL NOT ask:

```text
Does tenant have product subscription X?
```

for every capability request.

Instead:

```text
Subscription
     │
     ▼
Composition expansion
     │
     ▼
Capability Grants
```

The capability resolver SHALL evaluate grants.

---

# 17. Why Materialise Grants

Materialised grants provide:

```text
explicit entitlement
clear provenance
scope
temporal validity
revocation
auditability
efficient runtime evaluation
product-independent runtime semantics
```

They also permit capabilities to be granted outside a commercial product when legitimate.

---

# 18. Entitlement Expansion

Provisioning SHALL expand a subscription into capability requirements.

Conceptually:

```text
Subscription
   │
   ▼
Load ProductVersion
   │
   ▼
Load CapabilityComposition
   │
   ▼
Expand nested compositions
   │
   ▼
Validate dependency graph
   │
   ▼
Apply selected profiles
   │
   ▼
Resolve optional/conditional requirements
   │
   ▼
Generate capability requirements
   │
   ▼
Create/update CapabilityGrants
```

---

# 19. Expansion SHALL Be Deterministic

The same:

```text
product version
+
profile set
+
subscription configuration
```

SHALL result in the same canonical capability requirement set.

Hidden application-specific behaviour SHALL not modify expansion.

---

# 20. Required vs Optional Capabilities

Composition members SHALL distinguish:

```text
REQUIRED
OPTIONAL
CONDITIONAL
```

A required capability must be ready before the product can be considered fully operational.

An optional capability may be omitted.

A conditional capability becomes required only when its explicit condition is satisfied.

---

# 21. Conditional Capabilities

Conditions SHALL be canonical and machine-readable.

Example:

```text
if profile.crossborder enabled
then documents.trade.manage required
```

or:

```text
if channel = B2B
then commercial.quotation.manage required
```

Conditions SHALL NOT exist only in prose.

---

# 22. Context-Dependent Capability Requirements

Some capabilities SHALL become relevant only in certain operational contexts.

Example:

```text
Domestic transaction
    ↓
no customs-document capability

Cross-border transaction
    ↓
trade-document capability may be required
```

This SHALL be handled through:

```text
composition
+
grant scope
+
context
```

not through duplicate products.

---

# 23. Subscription Scope

A subscription SHALL primarily belong to a tenant.

It MAY be further constrained to:

```text
legal entities
Digital Estates
markets
channels
regions
```

where product policy requires.

However, subscription scope SHALL not replace CapabilityScope.

Capability grants SHALL remain the runtime entitlement representation.

---

# 24. Capability Grant Provenance

Grants created from subscriptions SHALL preserve:

```text
source_type = PRODUCT_SUBSCRIPTION
source_reference = subscription_id
```

This SHALL make revocation and audit deterministic.

---

# 25. Grant Reconciliation

CP SHALL periodically reconcile:

```text
subscription
composition
selected profiles
generated grants
```

Expected and actual entitlement SHALL remain aligned.

Drift SHALL be detected.

---

# 26. Grant Reconciliation Example

Expected:

```text
subscription X
requires:
A
B
C
```

Actual:

```text
A
B
```

Then:

```text
C missing
```

SHALL produce:

```text
ENTITLEMENT_DRIFT
```

and readiness SHALL degrade.

---

# 27. Digital Estate Provisioning

Digital Estate provisioning SHALL be treated as an orchestrated control-plane lifecycle.

Conceptually:

```text
Create / identify tenant
        │
        ▼
Create / identify legal entity
        │
        ▼
Register Digital Estate
        │
        ▼
Configure estate relationships
        │
        ▼
Activate product subscription
        │
        ▼
Expand capability composition
        │
        ▼
Materialise grants
        │
        ▼
Resolve mandatory bindings
        │
        ▼
Reconcile engine/provider topology
        │
        ▼
Validate readiness
        │
        ▼
Activate estate/product capability set
```

---

# 28. Provisioning SHALL Be Declarative

Provisioning SHALL be driven by desired state.

Example:

```text
Tenant ZuriBeans
should have:

Product:
Baobab XBT v1

Profiles:
B2B
Cross-Border
Commodity Trade
Supplier Management

Estate:
zuribeans
```

CP SHALL derive the required entitlement and provider state.

---

# 29. Provisioning Is Not Business Data Seeding

Product provisioning SHALL NOT automatically create:

```text
orders
customers
suppliers
invoices
shipments
```

Those are domain data.

Provisioning MAY create platform configuration and provider bootstrap state required for operation.

---

# 30. Provider Bootstrap

Provisioning MAY require provider-specific configuration.

Example:

```text
Medusa
    sales channel
    region
    stock location references

iDempiere
    organisation reference
    client reference

Payload
    tenant content configuration
```

Such actions SHALL occur through provider adapters/reconciliation.

They SHALL NOT make provider-native identifiers canonical platform identity.

---

# 31. Desired State vs Provider State

CP SHALL maintain the distinction:

```text
Desired State
      │
      ▼
Reconciler
      │
      ▼
Provider State
```

Example:

```text
Desired:
ZuriBeans has commerce.order.create in ZA

Provider:
Medusa sales channel and engine binding exist
```

---

# 32. Provisioning State

A provisioning aggregate SHOULD exist.

Conceptually:

```text
ProvisioningState
├── id
├── tenant_id
├── subscription_id
├── digital_estate_id
├── desired_state_version
├── observed_state_version
├── status
├── last_reconciled_at
├── failure_reason
└── metadata
```

---

# 33. Provisioning Status

Recommended statuses:

```text
PENDING
PLANNING
PROVISIONING
WAITING_DEPENDENCY
READY
DEGRADED
FAILED
DEPROVISIONING
DEPROVISIONED
```

---

# 34. Readiness

Readiness SHALL be evaluated at multiple levels.

```text
CapabilityReadiness
ProductReadiness
EstateReadiness
TenantReadiness
```

These SHALL be related but distinct.

---

# 35. Capability Readiness

Capability readiness answers:

> Can this specific capability currently resolve for this intended scope?

Example:

```text
commerce.order.create = READY
```

---

# 36. Product Readiness

Product readiness SHALL aggregate mandatory capability readiness.

Example:

```text
Baobab XBT
├── counterparty.manage        READY
├── commercial.contract       READY
├── finance.invoice           READY
├── logistics.shipment        NOT_READY
└── intelligence.market       READY
```

If `logistics.shipment` is mandatory:

```text
ProductReadiness = NOT_READY
```

---

# 37. Estate Readiness

Digital Estate readiness SHALL consider:

```text
identity integration
tenant/context configuration
required subscriptions
mandatory grants
mandatory bindings
provider readiness
required APIs
estate-specific configuration
```

An estate SHALL NOT be marked READY merely because the frontend deploys successfully.

---

# 38. Tenant Readiness

Tenant readiness MAY summarise:

```text
identity
legal entity
estate
market participation
subscription
capabilities
providers
isolation
residency
```

It SHOULD be an operational indicator, not a replacement for detailed readiness states.

---

# 39. Readiness Model

Recommended statuses:

```text
UNKNOWN
NOT_READY
PROVISIONING
READY
DEGRADED
BLOCKED
```

Reason codes SHALL explain why.

---

# 40. Readiness Is Not Health

This distinction SHALL be preserved:

```text
Health:
Is a provider currently operational?

Readiness:
Can the required platform configuration successfully deliver the product?
```

A provider may be healthy while a tenant is not ready because no valid binding exists.

---

# 41. ZuriBeans Product Composition

Illustrative subscription:

```text
Tenant:
ZuriBeans

Product:
Baobab XBT

Profiles:
B2B
Cross-Border
Commodity Trade
Supplier Management
```

Expanded capability set MAY include:

```text
organisation.account.manage
supplier.onboarding.manage
commercial.rfq.manage
commercial.quotation.manage
commercial.contract.manage
pricing.negotiated.resolve
commerce.order.manage
inventory.availability.read
trade.execution.manage
logistics.shipment.manage
documents.trade.manage
finance.invoice.manage
finance.receivable.manage
finance.settlement.manage
intelligence.commodity.query
intelligence.fx.query
intelligence.market.query
```

---

# 42. ZuriBeans Provisioning Flow

```text
Create ZuriBeans tenant/context
          │
          ▼
Register ZuriBeans estate
          │
          ▼
Subscribe to XBT
          │
          ▼
Apply profiles:
B2B + Cross-Border + Commodity
          │
          ▼
Expand composition
          │
          ▼
Materialise grants
          │
          ▼
Resolve providers
          │
    ┌─────┼─────┬──────┐
    ▼     ▼     ▼      ▼
  Trade   ERP  Pulse   CMS
          │
          ▼
Validate readiness
          │
          ▼
Activate
```

---

# 43. Thamani Composition

Illustrative:

```text
Tenant:
Thamani

Product:
Baobab Commerce / XBT subset

Profiles:
B2C
Supplier Management
```

Potential capabilities:

```text
customer.profile.manage
commerce.catalogue.read
commerce.cart.manage
commerce.checkout.execute
commerce.promotion.apply
payment.authorize
fulfilment.order.manage
returns.manage
supplier.onboarding.manage
content.publish
```

Cross-border profile MAY be added later without replacing the tenant or estate.

---

# 44. Coal Haulier Composition

Illustrative:

```text
Product:
Baobab XBT

Profiles:
B2B
Cross-Border
Logistics
```

Capabilities MAY include:

```text
organisation.customer.manage
commercial.contract.manage
pricing.service.resolve
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
```

This SHOULD be provisionable without creating coal-specific CP logic.

---

# 45. Petroleum Trader Composition

Illustrative:

```text
Product:
Baobab XBT

Profiles:
B2B
Cross-Border
Commodity Trade
```

Potential capabilities:

```text
counterparty.onboarding.manage
counterparty.due-diligence.manage
commercial.opportunity.manage
commercial.offer.manage
commercial.contract.manage
trade.execution.manage
documents.inspection.manage
documents.trade.manage
logistics.shipment.manage
finance.settlement.manage
finance.trade-profitability.calculate
intelligence.commodity.query
intelligence.fx.query
intelligence.regulatory.query
intelligence.risk.assess
```

Again, no petroleum-specific control-plane branch SHALL be required.

---

# 46. Profile Composition Shall Be Orthogonal

The following SHALL be possible:

```text
B2B + Domestic
B2B + Cross-Border
B2C + Domestic
B2C + Cross-Border
B2B + Logistics
B2B + Commodity Trade
B2C + Supplier Management
```

Profiles SHALL combine independently unless an explicit incompatibility exists.

---

# 47. Incompatible Profiles

Compositions MAY declare incompatibilities.

Example:

```text
profile.foo
incompatible_with
profile.bar
```

Such incompatibility SHALL be explicit and validated during provisioning.

It SHALL NOT be hidden in implementation code.

---

# 48. Profile Dependencies

Profiles MAY depend on others.

Example:

```text
profile.commodity-trade
requires
commercial.core
```

CP SHALL validate dependency closure.

---

# 49. Optional Add-Ons

Optional add-ons SHALL be modelled as additional compositions or products.

Example:

```text
addon.advanced-intelligence
```

may grant:

```text
intelligence.risk.assess
intelligence.forecast.generate
intelligence.opportunity.discover
```

without creating a separate estate.

---

# 50. Internal Platform Capabilities

Some baseline capabilities MAY be granted independent of paid products.

Example:

```text
context.resolve
identity.self
audit.correlation
```

These MAY originate from:

```text
PLATFORM_BASELINE
```

grants.

---

# 51. Product Suspension

Suspending a product subscription SHALL not require deleting grants.

Instead:

```text
Subscription = SUSPENDED
```

SHALL trigger:

```text
derived grants -> suspended or ineffective
readiness -> BLOCKED / NOT_READY
```

Historical entitlement SHALL remain auditable.

---

# 52. Product Cancellation

Cancellation SHALL move the subscription through a controlled lifecycle.

Example:

```text
ACTIVE
  │
  ▼
CANCEL_PENDING
  │
  ▼
DEPROVISIONING
  │
  ▼
CANCELLED
```

Exact states MAY be refined, but abrupt destructive teardown SHALL be avoided.

---

# 53. Deprovisioning

Deprovisioning SHALL include:

```text
revoke/suspend grants
invalidate caches
stop new runtime resolutions
retire tenant-specific bindings where appropriate
remove provider bootstrap state only when safe
archive configuration
preserve audit history
```

---

# 54. Data Retention During Deprovisioning

Deprovisioning a product SHALL NOT automatically delete business data.

Domain data retention SHALL follow:

```text
legal requirements
contract terms
domain retention policies
tenant policy
```

The Control Plane SHALL not destroy domain data casually.

---

# 55. Shared Engine State

If multiple tenants share an engine instance, deprovisioning one tenant SHALL NOT affect unrelated tenants.

Provider teardown SHALL be scope-aware.

---

# 56. Upgrade Model

A product upgrade SHALL be planned as a diff.

Example:

```text
XBT v1
   │
   ▼
XBT v2
```

Compute:

```text
added capabilities
removed capabilities
new dependencies
new provider requirements
new contracts
deprecated contracts
scope changes
configuration changes
```

---

# 57. Upgrade Planning

Before activation:

```text
Can all new mandatory capabilities resolve?
Are compatible providers available?
Does any binding need migration?
Will any existing capability disappear?
Are Digital Estates compatible?
```

If not:

```text
upgrade SHALL NOT activate
```

unless explicit degraded policy exists.

---

# 58. Upgrade States

Recommended:

```text
PLANNED
VALIDATING
READY
MIGRATING
ACTIVE
FAILED
ROLLED_BACK
```

Product upgrade state SHALL be observable.

---

# 59. Rollback

Where possible, product upgrades SHALL remain reversible until irreversible provider/domain migrations occur.

Rollback SHALL NOT simply restore an old subscription record if domain state has become incompatible.

Upgrade ADRs or provider migration plans MAY be required for major transitions.

---

# 60. Trial Subscriptions

Trials SHALL use the same architecture.

Example:

```text
Subscription type = TRIAL
effective_to = ...
```

Trial limitations SHALL be expressed through:

```text
grants
scope
constraints
```

where appropriate.

No separate trial runtime architecture SHALL exist.

---

# 61. Enterprise Customisation

An enterprise customer MAY receive:

```text
base product
+
selected profiles
+
optional add-ons
+
contract-specific grants
```

Example:

```text
XBT
+
B2B
+
Logistics
+
Advanced Intelligence
+
custom capability grant
```

This SHALL remain declarative.

---

# 62. Tenant-Specific Product Forks

The following SHALL be avoided:

```text
XBT-ZuriBeans
XBT-CoalCustomer
XBT-OilCustomer
```

unless the products are genuinely commercially distinct.

Prefer:

```text
XBT
+
different compositions/profiles/scopes
```

---

# 63. Product Catalog

CP SHOULD maintain runtime product metadata sourced from canonical contracts or governed product configuration.

Conceptually:

```text
Product
ProductVersion
CompositionReference
CommercialMetadata
Lifecycle
```

The canonical capability composition itself remains defined in Shared.

---

# 64. Commercial Metadata

Commercial metadata MAY contain references to:

```text
billing plan
sales SKU
contract template
pricing model
```

CP SHALL not become a full billing system unless separately decided.

---

# 65. Billing Separation

Subscription lifecycle MAY integrate with future billing.

But:

```text
billing
```

and:

```text
runtime entitlement
```

SHALL remain separate concerns.

Payment failure MAY cause subscription suspension through policy.

The capability resolver SHALL still operate on effective grants.

---

# 66. Product Catalog vs Capability Catalog

These SHALL remain separate.

```text
Product Catalog:
What do we sell?

Capability Catalog:
What can the platform do?
```

A product catalog item references capability compositions.

---

# 67. Digital Estate Requirement Profile

A Digital Estate MAY declare required capabilities.

Conceptually:

```text
DigitalEstateRequirement
├── estate_id
├── capability_key
├── required_contract_version
├── criticality
└── scope
```

This SHALL enable readiness evaluation.

---

# 68. Estate Requirements vs Product Grants

Estate requirement:

```text
I need capability X
```

Product grant:

```text
Tenant is entitled to capability X
```

Binding:

```text
Provider Y can satisfy X here
```

All three must align for readiness.

---

# 69. Readiness Equation

Conceptually:

```text
Estate Capability Ready
=
Requirement exists
AND
Grant exists
AND
Dependencies satisfied
AND
Binding exists
AND
Provider eligible
AND
Contract compatible
```

---

# 70. Product Readiness Equation

```text
Product Ready
=
all mandatory product capabilities ready
AND
all mandatory profile dependencies ready
AND
no blocking provisioning failures
```

---

# 71. Activation Gate

A subscription SHALL become `ACTIVE` only when:

```text
subscription valid
composition valid
grants materialised
mandatory dependencies satisfied
mandatory bindings resolved
provider readiness acceptable
estate prerequisites satisfied
```

unless an explicit policy allows partial activation.

---

# 72. Partial Activation

Partial activation MAY be supported for products designed for modular activation.

If used, it SHALL be explicit.

Example:

```text
XBT Core          READY
Logistics Profile NOT_READY
```

Possible overall:

```text
DEGRADED
```

rather than incorrectly reporting `READY`.

---

# 73. Criticality

Capabilities MAY carry product-specific criticality:

```text
MANDATORY
IMPORTANT
OPTIONAL
```

This criticality belongs to composition membership, not the global capability definition.

The same capability may be optional in one product and mandatory in another.

---

# 74. Product Scope vs Capability Scope

A product MAY be subscribed at tenant level.

Individual grants MAY be more tightly scoped.

Example:

```text
XBT subscription:
tenant-wide

commerce.b2b-pricing:
only B2B estate

crossborder.documents:
only cross-border markets
```

This is expected.

---

# 75. Subscription Configuration

Subscription configuration MAY select:

```text
profiles
markets
channels
optional capabilities
add-ons
service tier
```

It SHALL not contain provider-native implementation configuration.

Provider-specific configuration belongs to bindings/provider adapters.

---

# 76. Provisioning Plan

Before mutating runtime state, CP SHOULD create a provisioning plan.

Conceptually:

```text
ProvisioningPlan
├── required grants
├── grants to remove
├── required bindings
├── provider actions
├── estate configuration
├── readiness checks
├── blocking issues
└── plan_version
```

---

# 77. Plan Before Apply

The pattern SHALL be:

```text
Desired Subscription
        │
        ▼
PLAN
        │
        ├── validate
        ├── diff
        ├── detect blockers
        └── show impact
        │
        ▼
APPLY
```

This makes provisioning safer and auditable.

---

# 78. Idempotent Provisioning

Applying the same desired product state repeatedly SHALL converge to the same runtime state.

Provisioning SHALL be idempotent.

---

# 79. Reconciliation Loop

The conceptual reconciliation loop is:

```text
Desired Product State
        │
        ▼
Observe Runtime State
        │
        ▼
Compute Diff
        │
        ├── grants
        ├── bindings
        ├── provider config
        ├── readiness
        └── estate requirements
        │
        ▼
Apply Safe Changes
        │
        ▼
Re-evaluate
```

---

# 80. Drift

Possible drift states include:

```text
MISSING_GRANT
UNEXPECTED_GRANT
MISSING_BINDING
INCOMPATIBLE_PROVIDER
PROVIDER_NOT_READY
STALE_COMPOSITION_VERSION
ESTATE_REQUIREMENT_UNSATISFIED
```

These SHALL be observable.

---

# 81. Manual Changes

Direct administrative creation of grants or bindings MAY be allowed.

Such changes SHALL retain provenance.

Example:

```text
source_type = MANUAL_APPROVAL
```

Reconciliation SHALL distinguish legitimate manual override from accidental drift.

---

# 82. Override Policy

Manual override SHALL be explicit and time-bound where possible.

Permanent undocumented override SHALL be discouraged.

---

# 83. Provisioning Transactions

Where operations span several systems, provisioning cannot always be one database transaction.

Therefore CP SHALL use:

```text
local transactions
outbox events
idempotent provider operations
reconciliation
compensating actions
```

rather than pretending a distributed transaction exists.

---

# 84. Failure Model

Provisioning failures SHALL be resumable.

Example:

```text
grants created
Trade configured
ERP configuration failed
```

Result SHALL be:

```text
PROVISIONING / FAILED
```

with enough state to retry safely.

CP SHALL NOT silently roll back successful external operations unless a defined compensating action exists.

---

# 85. Provider Provisioning Adapter

Each provider MAY expose operations such as:

```text
PlanProvisioning
ApplyProvisioning
InspectProvisioning
Deprovision
```

Exact interfaces SHALL be provider/domain-specific but governed by canonical platform semantics.

---

# 86. Provider Provisioning SHALL NOT Execute Business Transactions

Provisioning adapters MAY create configuration.

They SHALL NOT create fake business operations merely to signal readiness.

---

# 87. Product Activation Event

Recommended lifecycle events:

```text
product.subscription.created
product.subscription.provisioning
product.subscription.activated
product.subscription.suspended
product.subscription.cancelled

product.provisioning.started
product.provisioning.completed
product.provisioning.failed

product.readiness.changed
```

---

# 88. Grant Events

Entitlement changes SHALL continue using capability grant events defined under previous ADRs.

Product events SHALL not replace grant events.

---

# 89. Estate Events

Digital Estate provisioning MAY emit:

```text
digital-estate.provisioning.started
digital-estate.ready
digital-estate.degraded
digital-estate.suspended
```

Exact canonical names SHALL be defined in Shared.

---

# 90. Audit

Provisioning audit SHALL capture:

```text
actor
tenant
product
version
profiles
estate
plan
grants created
grants revoked
bindings affected
provider operations
readiness outcome
correlation ID
```

---

# 91. Product Provisioning Security

Only authorised principals/workloads SHALL be able to:

```text
create subscriptions
change profiles
activate products
suspend products
upgrade products
deprovision products
```

These are platform administrative operations.

They SHALL not be inferred from ordinary domain permissions.

---

# 92. Separation from IAM

IAM MAY authenticate a tenant administrator.

CP SHALL determine whether that principal has authority to alter subscription/provisioning state.

Product profile selections SHALL NOT be encoded as Keycloak roles.

---

# 93. Separation from Domain Authorization

A subscription granting:

```text
finance.invoice.manage
```

does NOT mean every user can issue invoices.

It means the tenant has the platform capability.

ERP/domain authorization still decides which principals may perform the business action.

---

# 94. Separation from Provider Topology

Subscription SHALL never directly contain:

```text
Medusa instance ID
iDempiere endpoint
Payload database
```

Those are implementation/topology concerns.

---

# 95. Storage Model

The product schema SHOULD include concepts equivalent to:

```text
product.product
product.product_version
product.subscription
product.subscription_profile
product.provisioning_state
product.provisioning_plan
product.readiness
estate.capability_requirement
```

Capability grants remain in the capability schema.

---

# 96. Suggested Product Table

Conceptual PostgreSQL:

```sql
CREATE TABLE product.product (
    id                  uuid PRIMARY KEY,
    product_key         text NOT NULL UNIQUE,
    name                text NOT NULL,
    description         text,
    product_type        text NOT NULL,
    lifecycle_status    text NOT NULL,
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,
    version             bigint NOT NULL DEFAULT 1,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL
);
```

---

# 97. Suggested Product Version Table

```sql
CREATE TABLE product.product_version (
    id                  uuid PRIMARY KEY,
    product_id          uuid NOT NULL,
    version             text NOT NULL,
    composition_key     text NOT NULL,
    composition_version text NOT NULL,
    status              text NOT NULL,
    effective_from      timestamptz NOT NULL,
    effective_to        timestamptz,
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,

    UNIQUE(product_id, version),

    FOREIGN KEY(product_id)
        REFERENCES product.product(id),

    CHECK(
      effective_to IS NULL OR
      effective_to > effective_from
    )
);
```

---

# 98. Suggested Subscription Table

```sql
CREATE TABLE product.subscription (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL,
    product_version_id  uuid NOT NULL,

    subscription_type   text NOT NULL,
    status              text NOT NULL,

    effective_from      timestamptz NOT NULL,
    effective_to        timestamptz,

    contract_reference  text,
    billing_reference   text,

    configuration       jsonb NOT NULL DEFAULT '{}'::jsonb,

    version             bigint NOT NULL DEFAULT 1,

    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,

    FOREIGN KEY(product_version_id)
        REFERENCES product.product_version(id),

    CHECK(
      effective_to IS NULL OR
      effective_to > effective_from
    )
);
```

---

# 99. Suggested Subscription Profile Table

```sql
CREATE TABLE product.subscription_profile (
    subscription_id     uuid NOT NULL,
    composition_key     text NOT NULL,
    composition_version text NOT NULL,
    status              text NOT NULL,
    configuration       jsonb NOT NULL DEFAULT '{}'::jsonb,

    PRIMARY KEY (
      subscription_id,
      composition_key
    )
);
```

---

# 100. Suggested Provisioning State

```sql
CREATE TABLE product.provisioning_state (
    id                      uuid PRIMARY KEY,
    subscription_id         uuid NOT NULL,
    digital_estate_id       uuid,

    desired_state_version   bigint NOT NULL,
    observed_state_version  bigint,

    status                  text NOT NULL,
    failure_reason_code     text,

    last_reconciled_at      timestamptz,

    metadata                jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_at              timestamptz NOT NULL,
    updated_at              timestamptz NOT NULL
);
```

---

# 101. Product APIs

Candidate API surface:

```text
GET    /v1/products
GET    /v1/products/{id}

POST   /v1/subscriptions
GET    /v1/subscriptions/{id}
PATCH  /v1/subscriptions/{id}

POST   /v1/subscriptions/{id}/plan
POST   /v1/subscriptions/{id}/apply
POST   /v1/subscriptions/{id}/suspend
POST   /v1/subscriptions/{id}/resume
POST   /v1/subscriptions/{id}/upgrade
POST   /v1/subscriptions/{id}/cancel

GET    /v1/subscriptions/{id}/readiness
GET    /v1/subscriptions/{id}/grants
```

Exact APIs SHALL be contract-first in Shared.

---

# 102. Plan API

A planning endpoint SHOULD return:

```text
product
version
profiles
capabilities to grant
capabilities to revoke
required bindings
provider changes
readiness blockers
impact
warnings
```

without applying mutation.

---

# 103. Dry Run

Administrative product changes SHOULD support dry-run planning.

This is especially important for:

```text
upgrades
profile changes
deprovisioning
```

---

# 104. Product Upgrade Example

```text
Current:
XBT v1
B2B + Cross-Border

Target:
XBT v2
B2B + Cross-Border + Advanced Intelligence
```

Plan:

```text
+ intelligence.risk.assess
+ intelligence.opportunity.discover

Provider requirement:
baobab-pulse compatible contract required

Readiness:
Pulse binding found

Result:
READY_TO_APPLY
```

---

# 105. Product Removal Example

Removing:

```text
profile.crossborder
```

SHOULD compute which grants become unnecessary.

CP SHALL not revoke a capability if another active source still grants it.

---

# 106. Multi-Source Grants

Example:

```text
Capability A
granted by:
Product Subscription X
AND
Manual Contract Y
```

Removing subscription X SHALL NOT remove effective entitlement if Y remains valid.

Grant provenance makes this deterministic.

---

# 107. Reference Counting by Provenance

The implementation SHALL avoid simplistic logic such as:

```text
delete capability grant when profile removed
```

Instead, each grant source SHALL be represented independently or reconciled in a way that preserves multiple entitlement sources.

---

# 108. Product and Estate Independence

A tenant MAY have multiple Digital Estates consuming the same subscribed product.

Example:

```text
Tenant
│
├── Buyer Portal
├── Supplier Portal
└── Operations Portal
```

Each MAY require a different capability subset.

---

# 109. Estate-Specific Grants

Where required, grants MAY be scoped to one estate.

Example:

```text
supplier.onboarding.manage
only supplier portal
```

That SHALL be represented through CapabilityScope.

---

# 110. Cross-Estate Shared Capabilities

Capabilities MAY also be tenant-wide.

Example:

```text
intelligence.fx.query
```

could be consumed by several Digital Estates if granted broadly.

---

# 111. Product Provisioning and Legal Entities

Subscriptions MAY operate across one or more legal entities under a tenant where policy permits.

Capabilities MAY then be scoped more specifically.

No product SHALL assume:

```text
one subscription = one legal entity
```

as a universal rule.

---

# 112. Product Provisioning and Markets

Products SHALL not need duplicate country-specific SKUs solely because provider routing differs.

Example:

```text
XBT
```

may serve both:

```text
UG
ZA
```

through different bindings.

Market differences belong primarily in scope/context/provider resolution.

---

# 113. Market-Specific Product Variants

A genuinely different regulated/commercial offering MAY have a different product version or product.

This SHALL be a deliberate product decision, not a workaround for poor capability routing.

---

# 114. Subscription Validation

Before creation, CP SHALL validate:

```text
tenant active
product active
product version active
requested profiles valid
profile compatibility
subscription dates valid
scope valid
```

---

# 115. Subscription Activation Validation

Before `ACTIVE`:

```text
composition expands successfully
all required capabilities exist
required grants materialised
required providers compatible
mandatory bindings resolvable
residency/isolation valid
mandatory estate requirements satisfied
```

---

# 116. Failure Codes

Canonical reason codes SHOULD include:

```text
PRODUCT_UNKNOWN
PRODUCT_INACTIVE
PRODUCT_VERSION_UNKNOWN
PRODUCT_VERSION_INACTIVE

SUBSCRIPTION_INVALID
SUBSCRIPTION_SUSPENDED
SUBSCRIPTION_EXPIRED

COMPOSITION_UNKNOWN
COMPOSITION_INVALID
COMPOSITION_DEPENDENCY_CYCLE
PROFILE_INCOMPATIBLE

ENTITLEMENT_EXPANSION_FAILED

MANDATORY_CAPABILITY_MISSING
MANDATORY_CAPABILITY_NOT_READY
MANDATORY_PROVIDER_UNAVAILABLE

PROVISIONING_FAILED
PROVISIONING_DRIFT

ESTATE_REQUIREMENT_UNSATISFIED
```

---

# 117. Observability

Recommended metrics:

```text
product_subscription_total
product_subscription_activation_total
product_subscription_failure_total

product_provisioning_duration_seconds
product_provisioning_failure_total

product_readiness_total
product_readiness_transition_total

entitlement_expansion_duration_seconds
entitlement_drift_total
```

---

# 118. Product Readiness Dashboard

Operators SHOULD be able to see:

```text
Tenant
Product
Version
Profiles
Subscription state
Provisioning state
Required capabilities
Readiness
Blocking reason
```

Example:

```text
ZuriBeans
XBT v1
B2B + Cross-Border + Commodity

Subscription: ACTIVE
Provisioning: DEGRADED

commerce.order.manage       READY
finance.invoice.manage      READY
documents.trade.manage      BLOCKED
```

---

# 119. Impact Analysis

Before changing a product definition, tooling SHALL be able to answer:

```text
Which subscriptions use this version?
Which tenants are affected?
Which capabilities will change?
Which Digital Estates consume them?
Which providers must be available?
Which grants will change?
```

---

# 120. Product Definition Immutability

Published ProductVersions SHOULD be treated as immutable.

If capability composition changes materially:

```text
create new ProductVersion
```

rather than silently editing the version already subscribed by tenants.

---

# 121. Composition Immutability

Published composition versions SHALL similarly remain immutable.

This is essential for reproducibility.

---

# 122. Historical Reproducibility

The platform SHALL be able to determine:

```text
On date X,
Tenant Y was subscribed to Product version Z,
which expanded to capability set Q.
```

This SHALL remain possible after future product evolution.

---

# 123. Security Boundary

Subscription administration SHALL be highly privileged.

The following are security-sensitive:

```text
adding profiles
activating product
granting capabilities
suspending subscription
upgrading product
deprovisioning
```

All SHALL be audited.

---

# 124. Tenant Administrator Role

A tenant administrator MAY be permitted to manage approved product settings.

They SHALL not automatically be allowed to:

```text
grant arbitrary capabilities
override provider topology
bypass residency
override isolation
```

Platform-admin authority remains separate.

---

# 125. Platform Administrator Role

Platform administrators MAY manage:

```text
product catalog
provider topology
subscription overrides
capability grants
provisioning recovery
```

subject to separation-of-duty policy.

---

# 126. Domain Admin Roles

Domain administrators SHALL not automatically receive product-management authority.

Again:

```text
domain authorization
≠
platform provisioning authorization
```

---

# 127. Product Composition Governance

Changes to:

```text
solution.baobab-xbt
profile.b2b
profile.crossborder
profile.logistics
```

SHALL undergo architecture review because they may affect many tenants.

---

# 128. Capability Additions

Adding a capability to a composition SHALL declare whether it is:

```text
MANDATORY
OPTIONAL
CONDITIONAL
```

and its contract requirement.

---

# 129. Capability Removal

Removing a capability SHALL include:

```text
replacement guidance
migration impact
affected products
affected estates
deprecation timeline
```

where applicable.

---

# 130. Product Evolution Principle

Products SHOULD evolve primarily by recomposing stable capabilities.

They SHOULD NOT require frequent creation of entirely new platform primitives.

---

# 131. Reference Scenario Validation

The architecture SHALL be validated against:

```text
ZuriBeans:
XBT + B2B + Cross-Border + Commodity + Supplier

Thamani:
Commerce/XBT subset + B2C + Supplier

Coal Haulier:
XBT + B2B + Cross-Border + Logistics

Petroleum Trader:
XBT + B2B + Cross-Border + Commodity
```

If these require tenant-specific product code, the architecture SHALL be reconsidered.

---

# 132. Implementation Gates

## Gate 0 — Audit Current Product Entitlement Model

Inspect:

```text
product subscriptions
current entitlement semantics
Capability
CapabilityBinding
DigitalEstate
tenant lifecycle
seed data
APIs
tests
```

---

## Gate 1 — Canonical Product Contracts

Add/update Shared contracts for:

```text
ProductRef
ProductVersionRef
Subscription
SubscriptionProfile
ProvisioningPlan
ProvisioningState
Readiness
EstateCapabilityRequirement
```

---

## Gate 2 — Product Registry

Implement Product and ProductVersion runtime registry.

---

## Gate 3 — Subscription Model

Implement subscription lifecycle.

---

## Gate 4 — Composition Expansion

Implement deterministic flattening and dependency validation.

---

## Gate 5 — Grant Materialisation

Transform requirements into provenance-preserving grants.

---

## Gate 6 — Estate Requirements

Implement Digital Estate capability requirement model.

---

## Gate 7 — Provisioning Planner

Implement:

```text
desired state
observed state
diff
blockers
impact
```

---

## Gate 8 — Reconciliation

Implement idempotent reconciliation.

---

## Gate 9 — Readiness

Implement capability/product/estate readiness.

---

## Gate 10 — Provider Provisioning Integration

Integrate domain provider adapters safely.

---

## Gate 11 — Upgrade/Downgrade

Implement version/profile change planning.

---

## Gate 12 — Suspension/Deprovisioning

Implement controlled entitlement withdrawal.

---

## Gate 13 — APIs

Implement contract-first subscription/provisioning APIs.

---

## Gate 14 — Audit/Events/Observability

Implement operational governance.

---

## Gate 15 — Reference Product Fixtures

Validate all four reference operating models.

---

## Gate 16 — Security and Failure Tests

Test:

```text
unauthorised product activation
cross-tenant subscription access
invalid profile combination
missing provider
partial provider provisioning
grant drift
stale composition
subscription suspension
multi-source entitlement removal
```

---

# 133. Definition of Done

ADR-BCP-005 SHALL be considered implemented when:

1. Product is distinct from capability.
2. ProductVersion is explicit.
3. CapabilityComposition is canonical.
4. Profiles are composable.
5. B2B/B2C are not tenant types.
6. Cross-border is not a separate platform.
7. Subscription is distinct from grant.
8. Runtime resolution uses grants rather than subscriptions.
9. Grants preserve entitlement provenance.
10. Multiple entitlement sources are safe.
11. Digital Estate requirements are explicit.
12. Provisioning is declarative.
13. Provisioning is idempotent.
14. Readiness is measurable.
15. Mandatory capabilities block activation when unavailable.
16. Product upgrades are planned as diffs.
17. Published product versions are reproducible.
18. Deprovisioning does not destroy domain data blindly.
19. Provider topology remains separate from product definition.
20. ZuriBeans, Thamani, coal-haulage and petroleum-trading models work without tenant-specific product code.

---

# 134. Rejected Alternatives

## Product equals engine

Rejected.

## Subscription directly authorizes APIs

Rejected.

## One product per tenant

Rejected.

## One product per country

Rejected as a default strategy.

## One product per Digital Estate

Rejected.

## B2B and B2C as separate Control Planes

Rejected.

## XBT as another orchestration engine

Rejected.

## Provider details inside product contracts

Rejected.

## Destructive deprovisioning

Rejected.

## Silent product-version mutation

Rejected.

---

# 135. Final Target Architecture

```text
                         PRODUCT CATALOG
                              │
                              ▼
                        ProductVersion
                              │
                              ▼
                   CapabilityComposition
                              │
               ┌──────────────┼──────────────┐
               ▼              ▼              ▼
            Core           Profiles        Add-ons
                              │
                              ▼
                        Subscription
                              │
                              ▼
                    Entitlement Expansion
                              │
                              ▼
                     Capability Grants
                              │
                              ▼
                   Capability Resolution
                              │
               ┌──────────────┼──────────────┐
               ▼              ▼              ▼
             Trade           ERP            Pulse
                              │
                              ▼
                       Estate Readiness
                              │
                              ▼
                        DIGITAL ESTATE
```

---

# 136. Decision

**ACCEPTED TARGET PRODUCT AND PROVISIONING ARCHITECTURE, subject to formal approval.**

Upon approval:

1. product packaging SHALL be separated from engine topology;
2. capability compositions SHALL define products and profiles;
3. subscriptions SHALL materialise capability grants;
4. runtime entitlement SHALL rely on grants rather than product names;
5. Digital Estates SHALL declare capability requirements;
6. product/estate readiness SHALL be first-class;
7. product lifecycle SHALL include safe provisioning, upgrade, suspension and deprovisioning;
8. no tenant-specific product fork SHALL be introduced without genuine commercial justification;
9. Baobab XBT SHALL remain a composable product rather than a new orchestration runtime;
10. implementation SHALL proceed through explicit planning and reconciliation.

---

# 137. Architectural Maxim

> **A Baobab product is a promise of capabilities, not a bundle of engines.**

And operationally:

> **Subscribe to a product, expand its composition, materialise its grants, resolve its providers, verify readiness, then activate the Digital Estate.**