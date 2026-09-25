# ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model

**Status:** Proposed — Normative Target Architecture  
**Date:** 2026-09-10  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Repository:** `baobab-platform/baobab-cp`  
**Primary Runtime Owner:** `baobab-platform/baobab-cp`  
**Contract Authority:** `baobab-platform/shared`  
**Identity Authority:** `baobab-platform/baobab-iam`  
**Architecture Style:** Capability-centric, multi-tenant, context-resolved, headless, contract-driven platform control plane  
**Target Runtime:** Go  
**Authoritative Runtime Store:** PostgreSQL 17  
**Applies To:** Baobab Control Plane, all Baobab engines, Digital Estates, platform contracts, tenant provisioning, capability entitlements and capability resolution  
**Depends On:** ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts  
**Related Decisions:** Baobab IAM ADR-0001 through ADR-0018; `baobab-platform/shared` ADRs governing Digital Estates, tenancy, legal entities, canonical mappings, ERP boundaries and cross-engine contracts  
**Supersedes:** Any interpretation of `Product`, `Engine`, `EngineInstance`, `Capability`, `CapabilityBinding`, entitlement or Digital Estate that requires Digital Estates to consume vendor-specific engines directly  
**Migration Posture:** Remodel before production; compatibility with non-production data is subordinate to architectural correctness  
**Decision Type:** Foundational platform architecture

---

# 1. Executive Decision

Baobab SHALL become a **capability-centric digital business platform**.

The Baobab Control Plane SHALL become the authoritative runtime responsible for determining:

```text
WHO is operating
FOR WHICH tenant
FOR WHICH legal entity
THROUGH WHICH Digital Estate
IN WHICH business / market / geographic context
TO CONSUME WHICH capability
UNDER WHICH entitlement
USING WHICH permitted capability provider
ON WHICH engine instance
UNDER WHICH isolation, residency and deployment constraints
AT WHICH point in time.
```

Digital Estates SHALL consume **business capabilities**, not engines.

Engines SHALL implement capabilities.

Products and solutions SHALL package capabilities.

`baobab-cp` SHALL resolve capabilities to providers and provider instances according to context, entitlement, policy, topology, temporal validity, health, isolation and contract compatibility.

`baobab-platform/shared` SHALL define the canonical language and contracts for capabilities, compositions, contexts, provider declarations, events and interoperability.

`baobab-iam` SHALL remain authoritative for identity, authentication, credential/session security, authentication assurance and coarse OAuth/OIDC access.

Domain engines SHALL remain authoritative for domain business logic and fine-grained business authorization.

This ADR establishes the following governing principle:

> **Digital Estates consume capabilities. Products compose capabilities. The Control Plane grants and resolves capabilities. Engines provide capabilities. Shared defines their contracts. IAM establishes identity. Domain engines own business behaviour.**

Baobab SHALL NOT be architected around any single engine implementation, vendor, industry, business model, market or Digital Estate.

---

# 2. Strategic Intent

Baobab exists to support heterogeneous businesses and Digital Estates without requiring each Digital Estate to become an integration project.

Examples include:

- ZuriBeans conducting B2B commerce and cross-border commodity trade;
- Thamani conducting B2C commerce;
- a domestic B2B distributor operating in a single market;
- a coal-haulage company servicing cross-border corridors without necessarily maintaining legal entities in every transit or destination market;
- a crude-oil trader managing B2B counterparties, contracts, execution, settlement and intelligence;
- future logistics, wholesale, commodity, services, property or commerce businesses not yet known.

These businesses differ materially.

Their common requirement is not that they all use MedusaJS, iDempiere, Payload, Haystack or Keycloak.

Their common requirement is that they need **capabilities**.

Examples:

```text
supplier onboarding
buyer onboarding
customer identity
catalogue management
negotiated pricing
quotation management
order management
inventory availability
invoice management
shipment management
document management
payment processing
settlement
market intelligence
FX intelligence
commodity intelligence
authentication
audit
```

The architecture SHALL therefore optimise for **stable business capabilities over replaceable implementation technologies**.

---

# 3. Problem Statement

The existing Baobab architecture already contains the essential foundations:

```text
Tenant
LegalEntity
DigitalEstate
Market
Context
Capability
CapabilityBinding
Engine
EngineInstance
CanonicalEntity
ExternalReference
Mapping
MappingScope
IsolationProfile
```

The Control Plane already asserts responsibility for tenant lifecycle, entitlement and context resolution while excluding commerce, ERP and intelligence business logic.

However, without a stronger capability-centric architecture, several failure modes remain possible.

## 3.1 Engine-centric coupling

A Digital Estate may become coupled to:

```text
Medusa
iDempiere
Payload
Haystack
Keycloak
```

rather than to stable Baobab business interfaces.

This would make engine replacement expensive and make Baobab little more than an integration façade.

## 3.2 Product and capability conflation

Commercial products, runtime entitlements and implementation providers may become synonymous.

For example:

```text
"Subscribed to Baobab Trade"
```

must not automatically mean:

```text
"entitled to every Medusa function"
```

## 3.3 Entitlement and binding conflation

The statement:

```text
Tenant may use capability X
```

is fundamentally different from:

```text
Engine instance Y provides capability X in context Z.
```

The architecture SHALL keep those decisions separate.

## 3.4 Digital Estate specialisation

Hard-coded notions such as:

```text
ZuriBeans mode
Thamani mode
coal mode
oil mode
```

would eventually fragment the platform.

The target architecture SHALL use context and capability composition instead.

## 3.5 Keycloak role explosion

Business capabilities SHALL NOT become thousands of Keycloak roles.

IAM SHALL identify and authenticate the actor; CP and domain engines SHALL determine platform and business authority.

## 3.6 Cross-border context overloading

The Control Plane must understand enough geography and jurisdiction to resolve capabilities, but SHALL NOT become the source of truth for shipments, trucks, customs declarations, product inventory, orders or trade transactions.

---

# 4. Architectural North Star

The target platform SHALL be:

```text
┌──────────────────────────────────────────────────────────────┐
│                    DIGITAL EXPERIENCES                       │
│                                                              │
│ ZuriBeans   Thamani   Coal Haulier   Oil Trader   Future    │
│    B2B        B2C         B2B            B2B                 │
└──────────────────────────────┬───────────────────────────────┘
                               │
                         consume capabilities
                               │
                               ▼
┌──────────────────────────────────────────────────────────────┐
│                    BAOBAB CONTROL PLANE                       │
│                                                              │
│                      baobab-platform/baobab-cp                        │
│                                                              │
│ Identity Context          Tenant / Legal Entity              │
│ Digital Estate           Market / Geography                  │
│ Entitlements             Capability Grants                  │
│ Capability Resolution    Provider Selection                 │
│ Engine Topology          Isolation / Residency              │
│ Mappings                 Desired State                       │
│ Auditing                 Reconciliation                      │
└─────────────┬─────────────────────────────┬──────────────────┘
              │                             │
      contracts / schemas              authentication
              │                             │
              ▼                             ▼
┌───────────────────────────┐   ┌──────────────────────────────┐
│     baobab-platform/shared        │   │      baobab-platform/baobab-iam     │
│                           │   │          Keycloak            │
│ Capability contracts     │   │                              │
│ Context contracts        │   │ Identity                     │
│ API contracts            │   │ Authentication               │
│ Event contracts          │   │ MFA / Passkeys               │
│ Canonical schemas        │   │ Workload identity            │
│ Solution definitions     │   │ Sessions                     │
└──────────────┬────────────┘   │ Coarse OAuth scopes          │
               │                └──────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────────┐
│                    CAPABILITY PROVIDERS                       │
│                                                              │
│ baobab-trade      baobab-erp       baobab-pulse             │
│ MedusaJS          iDempiere        Haystack                  │
│                                                              │
│ baobab-cms        future engines   approved external APIs    │
│ Payload                                                      │
└──────────────────────────────────────────────────────────────┘
```

The Control Plane SHALL NOT become a business-process monolith.

It SHALL become the **platform capability resolution authority**.

---

# 5. Fundamental Architectural Definitions

## 5.1 Capability

A **Capability** is an implementation-neutral description of something the Baobab platform can perform or make available.

Examples:

```text
commerce.order.create
commerce.catalogue.read
commerce.pricing.negotiated
supplier.onboarding.manage
finance.invoice.manage
finance.receivable.read
logistics.shipment.manage
identity.customer.authenticate
intelligence.fx.query
intelligence.commodity.query
```

A capability SHALL describe **what**, not **how**.

Therefore this is valid:

```text
commerce.inventory.query
```

This is invalid:

```text
medusa.inventory.query
```

for a canonical platform capability.

---

## 5.2 Capability Definition

The canonical metadata describing a capability.

Conceptually:

```text
CapabilityDefinition
├── capability_id
├── capability_key
├── name
├── description
├── domain
├── version
├── maturity
├── status
├── contract_reference
├── dependencies
├── compatibility
├── data_classification
└── metadata
```

---

## 5.3 Capability Provider

A **Capability Provider** declares that a platform component can implement a capability.

Examples:

```text
commerce.order.manage
       │
       ▼
baobab-trade
       │
       ▼
MedusaJS
```

```text
finance.invoice.manage
       │
       ▼
baobab-erp
       │
       ▼
iDempiere
```

```text
intelligence.commodity.query
       │
       ▼
baobab-pulse
       │
       ▼
Haystack + approved data sources
```

Provider identity SHALL remain distinct from capability identity.

---

## 5.4 Engine

An **Engine** is an implementation technology or independently deployable provider family.

Examples:

```text
MedusaJS
iDempiere
Payload
Haystack
Keycloak
```

Engines SHALL NOT define Baobab product identity.

---

## 5.5 Engine Instance

An **EngineInstance** is an environment-, region-, deployment- and isolation-specific runtime instance of an Engine.

Examples:

```text
medusa-prod-za-01
idempiere-prod-ug-01
pulse-prod-af-south-01
```

Capability resolution SHALL ultimately resolve to a permitted provider instance.

---

## 5.6 Capability Grant

A **CapabilityGrant** states that a tenant or scoped tenant context is entitled to consume a capability.

A grant answers:

```text
May this tenant consume this capability?
```

A grant SHALL NOT determine which engine instance provides it.

---

## 5.7 Capability Binding

A **CapabilityBinding** associates a capability with a provider / engine instance under a particular scope and configuration.

A binding answers:

```text
If capability C is permitted in context X,
which provider instance SHALL satisfy it?
```

Binding SHALL remain separate from entitlement.

---

## 5.8 Capability Scope

A **CapabilityScope** defines the dimensions under which a grant or provider binding is applicable.

Conceptual dimensions MAY include:

```text
Tenant
LegalEntity
Organisation
BusinessUnit
DigitalEstate
DigitalProperty
Channel
Market
Country
Jurisdiction
Currency
CustomerSegment
Catalogue
OperatingRegion
GeographicRegion
DeploymentRegion
Environment
IsolationProfile
```

A future implementation MAY extend these dimensions through versioned contracts.

Arbitrary ungoverned JSON SHALL NOT replace first-class dimensions merely for convenience.

---

## 5.9 Capability Composition

A **CapabilityComposition** is a declarative grouping of capabilities.

Examples:

```text
solution.baobab-xbt
profile.b2b
profile.b2c
profile.crossborder
profile.logistics
profile.commodity-trade
```

A composition SHALL NOT execute business logic.

It SHALL describe required or optional capabilities and dependency relationships.

---

## 5.10 Solution / Product

A **Solution** or **Product** is a commercial or customer-facing package.

Example:

```text
Baobab XBT
```

A product MAY map to one or more CapabilityCompositions.

The architecture SHALL preserve:

```text
Product
    ≠
Capability
    ≠
Provider
    ≠
Engine
    ≠
EngineInstance
```

---

# 6. Product, Capability and Provider Separation

The canonical relationship SHALL be:

```text
COMMERCIAL PRODUCT
        │
        │ packages
        ▼
CAPABILITY COMPOSITION
        │
        │ contains
        ▼
CAPABILITY
        │
        │ implemented by
        ▼
CAPABILITY PROVIDER
        │
        │ deployed as
        ▼
ENGINE INSTANCE
```

Example:

```text
Baobab XBT
   │
   ▼
XBT B2B Cross-Border Composition
   │
   ▼
commerce.pricing.negotiated
   │
   ▼
baobab-trade provider
   │
   ▼
Medusa production instance
```

If the implementation engine changes, Digital Estates SHALL NOT require conceptual redesign.

---

# 7. Digital Estate Consumption Model

A Digital Estate SHALL be treated as a **consumer of Baobab capabilities**.

It SHALL NOT be treated as a private collection of direct engine integrations.

Target model:

```text
DigitalEstate
│
├── identity
├── tenant
├── legal_entity context
├── channels
├── markets
├── properties
├── experience configuration
└── capability requirements
```

A Digital Estate MAY consume different capabilities depending on:

- channel;
- user type;
- market;
- legal entity;
- operating geography;
- customer segment;
- currency;
- deployment region;
- business model;
- transaction context.

Digital Estates SHALL NOT normally call `baobab-cp` directly from the browser.

The normal flow SHALL be:

```text
User
 │
 ▼
Digital Estate
 │
 ▼
Estate API / BFF / Domain API
 │
 ├────────► Baobab IAM
 │           authenticate/token verification
 │
 ├────────► Baobab CP
 │           context + entitlement + capability resolution
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

# 8. Context Is a First-Class Resolution Input

The current `Context` model SHALL remain foundational.

The target immutable server-resolved context SHALL continue to distinguish at minimum:

```text
Principal
Tenant
LegalEntity
DigitalEstate
DigitalProperty
Channel
Market
Jurisdiction
Country
Currency
Locale
DeploymentRegion
Environment
IsolationProfile
CorrelationID
ResolvedAt
Provenance
```

These SHALL NOT be aliases for each other.

In particular:

```text
Tenant ≠ LegalEntity
LegalEntity ≠ DigitalEstate
Market ≠ Country
Market ≠ Legal presence
DigitalEstate ≠ Tenant
```

---

# 9. Business Geography and Cross-Border Context

Baobab SHALL explicitly recognise that several geographic concepts are different.

A business may have:

```text
Legal footprint
Operating footprint
Commercial footprint
Transaction footprint
Transit footprint
Destination footprint
```

These SHALL NOT be conflated.

Example:

```text
Coal Haulier

Legal presence:
    South Africa

Origin:
    South Africa

Transit:
    Botswana

Destination:
    Zambia

Settlement:
    USD
```

The existence of a transaction or route through Zambia SHALL NOT imply the existence of a Zambian legal entity.

The Control Plane SHALL model only the dimensions necessary for:

- entitlement;
- capability selection;
- provider selection;
- market policy;
- jurisdictional applicability;
- residency;
- routing;
- isolation;
- authorization.

It SHALL NOT become the operational source of truth for shipment legs, customs declarations, vehicles, inventory, trades or deliveries.

---

# 10. Platform Context vs Domain Operation Scope

To prevent `Context` from becoming a business object, Baobab SHALL distinguish:

```text
Platform Context
        +
Domain Operation Scope
```

Conceptual example:

```text
ResolvedPlatformContext
├── principal
├── tenant
├── legal_entity
├── digital_estate
├── channel
├── market
├── deployment_region
├── environment
└── isolation_profile
```

Domain request:

```text
ShipmentOperation
├── origin_country
├── transit_countries
├── destination_country
├── route
├── carrier
└── shipment_id
```

The Control Plane MAY consume selected operation attributes during resolution when contractually required.

It SHALL NOT take ownership of the domain operation.

---

# 11. Capability Taxonomy

Baobab SHALL maintain a governed capability namespace.

Recommended high-level namespaces include:

```text
identity.*
tenant.*
organisation.*
counterparty.*
supplier.*
customer.*
commerce.*
pricing.*
catalogue.*
inventory.*
procurement.*
commercial.*
contract.*
trade.*
logistics.*
fulfilment.*
documents.*
finance.*
payment.*
settlement.*
content.*
intelligence.*
audit.*
integration.*
```

Capabilities SHALL use implementation-neutral names.

Names SHALL be stable enough to survive provider changes.

A capability MAY be subdivided:

```text
domain.resource.action
```

Examples:

```text
supplier.onboarding.manage
commerce.order.create
commerce.order.cancel
commerce.pricing.negotiated
inventory.availability.read
finance.invoice.issue
finance.receivable.read
logistics.shipment.track
documents.trade.verify
intelligence.fx.query
```

The exact namespace registry SHALL be owned by `baobab-platform/shared`.

---

# 12. Capability Composition and Baobab XBT

Baobab XBT SHALL be modelled as a capability composition or family of compositions, not as another orchestration engine.

Conceptually:

```text
BAOBAB XBT
│
├── Core
│   ├── identity
│   ├── tenant context
│   ├── organisations
│   ├── counterparties
│   ├── commercial operations
│   ├── orders
│   ├── finance
│   └── intelligence
│
├── B2B Profile
│   ├── organisation accounts
│   ├── buyer onboarding
│   ├── supplier onboarding
│   ├── RFQ
│   ├── quotations
│   ├── negotiated pricing
│   ├── contracts
│   └── credit/payment terms
│
├── B2C Profile
│   ├── customer identity
│   ├── catalogue
│   ├── cart
│   ├── checkout
│   ├── promotions
│   ├── payments
│   └── returns
│
├── Cross-Border Profile
│   ├── multiple markets
│   ├── multiple currencies
│   ├── trade documents
│   ├── origin/destination context
│   ├── regulatory intelligence
│   └── cross-border logistics
│
├── Logistics Profile
│   ├── transport orders
│   ├── loads
│   ├── dispatch
│   ├── routes
│   ├── milestones
│   └── POD
│
└── Commodity Profile
    ├── commodity pricing
    ├── contracts
    ├── inspection
    ├── settlement
    ├── market intelligence
    └── trade profitability
```

This SHALL NOT require all customers to enable every profile.

---

# 13. Illustrative Digital Estate Compositions

## 13.1 ZuriBeans

```text
ZURIBEANS

Core
+
B2B
+
Cross-Border
+
Commodity Trade
+
Supplier Management
```

Potential capabilities:

```text
identity.organisation-access
supplier.onboarding.manage
customer.organisation.onboard
commercial.rfq.manage
commercial.quotation.manage
commerce.pricing.negotiated
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

## 13.2 Thamani

```text
THAMANI

Core
+
B2C
+
Supplier Management
+
Domestic and/or Cross-Border profiles as required
```

Potential capabilities:

```text
identity.customer.authenticate
customer.profile.manage
commerce.catalogue.read
commerce.cart.manage
commerce.checkout.execute
commerce.promotions.apply
payment.authorize
fulfilment.order.manage
returns.manage
supplier.onboarding.manage
content.catalogue.publish
intelligence.market.query
```

---

## 13.3 Coal Haulier

```text
COAL HAULIER

Core
+
B2B
+
Cross-Border
+
Logistics
```

Potential capabilities:

```text
customer.organisation.onboard
commercial.contract.manage
commercial.pricing.manage
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
intelligence.fuel.query
intelligence.route-risk.assess
```

---

## 13.4 Petroleum Trader

```text
PETROLEUM TRADER

Core
+
B2B
+
Cross-Border
+
Commodity Trade
```

Potential capabilities:

```text
counterparty.onboarding.manage
counterparty.due-diligence.manage
commercial.opportunity.manage
commercial.offer.manage
commercial.contract.manage
commodity.pricing.query
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

These examples SHALL inform capability design but SHALL NOT create tenant-specific platform primitives.

---

# 14. Capability Grant Model

A CapabilityGrant SHALL answer whether a capability may be consumed.

Conceptual schema:

```text
CapabilityGrant
├── id
├── tenant_id
├── capability_id
├── scope_id
├── source
├── source_reference
├── status
├── effective_from
├── effective_to
├── constraints
├── granted_by
├── created_at
└── version
```

Possible grant sources:

```text
PRODUCT_SUBSCRIPTION
PLATFORM_BASELINE
MANUAL_APPROVAL
CONTRACT
TRIAL
INTERNAL_POLICY
MIGRATION
```

A product subscription SHALL typically expand into one or more CapabilityGrants.

Example:

```text
Subscribe:
    Baobab XBT B2B

          │
          ▼

Grant:
    supplier.onboarding.manage
    commercial.rfq.manage
    commercial.quotation.manage
    commerce.pricing.negotiated
    commerce.order.manage
    finance.invoice.manage
    ...
```

---

# 15. Capability Binding Model

A CapabilityBinding SHALL describe how a granted capability is fulfilled.

Conceptual target:

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
└── version
```

The current Control Plane binding model SHALL be remodelled where necessary to support this target unambiguously.

A binding SHALL NOT itself grant entitlement.

---

# 16. Binding Modes

The architecture SHOULD support explicit binding modes.

At minimum:

```text
PRIMARY
FALLBACK
SHADOW
MIGRATION
DISABLED
```

Future modes MAY include:

```text
READ_ONLY
WRITE_PRIMARY
REGION_PREFERRED
CANARY
```

Meaning SHALL be contractually defined.

A resolver SHALL never guess binding semantics from arbitrary metadata.

---

# 17. Capability Provider Registry

The Control Plane SHALL maintain runtime provider topology.

Conceptual:

```text
CapabilityProvider
├── provider_id
├── provider_key
├── engine_id
├── capability_id
├── contract_versions
├── status
├── configuration_schema
├── health_requirements
├── supported_regions
├── supported_markets
├── supported_isolation_profiles
└── metadata
```

An Engine MAY provide many capabilities.

A capability MAY have several providers.

Example:

```text
                 finance.payment.authorize
                           │
           ┌───────────────┼───────────────┐
           ▼               ▼               ▼
     Provider A        Provider B       Provider C
```

This SHALL permit migration and provider substitution without redefining Digital Estate contracts.

---

# 18. Capability Dependency Model

Capabilities MAY depend on other capabilities.

Example:

```text
commerce.checkout.execute
│
├── commerce.cart.read
├── pricing.resolve
├── inventory.availability.read
├── payment.authorize
└── fulfilment.option.resolve
```

Dependencies SHALL be declared, versioned and validated.

Circular mandatory dependencies SHALL be prohibited.

The Control Plane SHALL be able to detect an unsatisfied dependency graph before activating a product or composition.

---

# 19. Capability Resolution Pipeline

The normative resolution sequence SHALL be:

```text
REQUEST
  │
  ▼
Validate authenticated principal
  │
  ├── invalid ─────────────────────────► DENY
  ▼
Resolve canonical identity
  │
  ├── disabled/revoked ─────────────────► DENY
  ▼
Resolve tenant
  │
  ├── unknown/inactive ─────────────────► DENY
  ▼
Resolve legal entity / representation
  │
  ├── invalid ──────────────────────────► DENY
  ▼
Resolve Digital Estate / channel
  │
  ├── unauthorized ─────────────────────► DENY
  ▼
Resolve platform context
  │
  ▼
Resolve requested capability
  │
  ├── unknown/inactive ─────────────────► DENY
  ▼
Evaluate CapabilityGrant
  │
  ├── no effective grant ───────────────► DENY
  ▼
Evaluate capability dependencies
  │
  ├── unsatisfied ──────────────────────► FAIL CLOSED
  ▼
Find effective CapabilityBindings
  │
  ▼
Apply scope compatibility
  │
  ├── tenant
  │
  ├── legal entity
  │
  ├── estate
  │
  ├── channel
  │
  ├── market
  │
  ├── jurisdiction
  │
  ├── currency
  │
  ├── region
  │
  └── environment
  ▼
Filter provider eligibility
  │
  ├── lifecycle
  ├── health
  ├── contract compatibility
  ├── isolation
  ├── residency
  └── effective period
  ▼
Apply deterministic precedence
  │
  ├── none ─────────────────────────────► FAIL CLOSED
  │
  ├── unresolved tie ───────────────────► AMBIGUITY ERROR
  ▼
Resolve exactly one provider
  │
  ▼
Resolve EngineInstance
  │
  ▼
Return immutable resolution decision
```

The resolver SHALL NOT silently select arbitrarily among equally valid bindings.

Ambiguity SHALL be an error.

---

# 20. Resolution Output

A capability-resolution decision SHOULD conceptually contain:

```text
CapabilityResolution
├── resolution_id
├── context_id
├── principal_id
├── tenant_id
├── legal_entity_id
├── digital_estate_id
├── capability_key
├── capability_version
├── grant_id
├── binding_id
├── provider_id
├── engine_id
├── engine_instance_id
├── contract_version
├── endpoint_reference
├── effective_configuration
├── isolation_profile
├── resolved_at
├── expires_at
├── correlation_id
└── provenance
```

Secrets SHALL NOT be embedded directly in ordinary resolution responses.

Credential material SHALL be referenced through approved secret-management mechanisms.

---

# 21. Fail-Closed Requirement

Capability resolution SHALL be fail-closed.

The following conditions SHALL fail closed:

```text
unknown tenant
inactive tenant
invalid legal entity
invalid representation
disabled capability
no effective grant
ambiguous grant
missing required dependency
no compatible binding
ambiguous binding
unsupported contract
unhealthy mandatory provider
residency mismatch
isolation mismatch
expired binding
invalid context provenance
authorization failure
```

The Control Plane SHALL NOT silently fall back to a global provider unless an explicit valid fallback binding exists.

---

# 22. Deterministic Precedence

Binding resolution SHALL be deterministic.

Resolution precedence SHALL be defined contractually.

A typical precedence hierarchy MAY be:

```text
LegalEntity + DigitalEstate + Market
        >
LegalEntity + Market
        >
Tenant + DigitalEstate + Market
        >
Tenant + Market
        >
Tenant + DigitalEstate
        >
Tenant
        >
Platform default
```

However, this exact hierarchy SHALL NOT be hard-coded until defined in the canonical Shared contract and migration specification.

At equal effective specificity and equal priority, resolution SHALL return ambiguity.

---

# 23. Temporal Semantics

Capability grants, providers and bindings SHALL be temporal.

At minimum:

```text
effective_from
effective_to
status
version
```

The resolver SHALL evaluate eligibility at a single immutable resolution time.

For request resolution:

```text
resolution_time = server-controlled UTC timestamp
```

Clients SHALL NOT be trusted to choose arbitrary historical resolution times without an explicitly authorised historical/audit endpoint.

---

# 24. Health and Lifecycle Separation

Provider lifecycle and provider health SHALL remain distinct.

Example:

```text
Lifecycle:
    ACTIVE

Health:
    DEGRADED
```

Lifecycle describes authoritative registry state.

Health describes current operational state.

A frequently changing health value SHALL NOT cause unnecessary mutation of authoritative topology records.

---

# 25. Isolation and Residency

Capability resolution SHALL enforce isolation and residency constraints.

A provider SHALL NOT be selected solely because it offers the requested capability.

It SHALL also be compatible with:

```text
Tenant isolation
Database isolation
Storage isolation
Cache isolation
Queue isolation
Network isolation
Secret isolation
Encryption requirements
Observability isolation
Backup requirements
Deployment isolation
Data residency
Jurisdiction
```

Provider selection SHALL fail closed when mandatory controls cannot be satisfied.

---

# 26. Canonical Mapping Relationship

Capabilities SHALL coexist with, not replace, canonical entity mappings.

Canonical mappings answer:

```text
What platform business entity corresponds to this native engine object?
```

Capability resolution answers:

```text
Which provider should perform this capability in this context?
```

They are separate concerns.

Example:

```text
Canonical Product
       │
       ├── ExternalReference → Medusa product ID
       └── ExternalReference → iDempiere product ID
```

while:

```text
commerce.catalogue.read
       │
       ▼
CapabilityBinding
       │
       ▼
Medusa instance
```

Both are required.

---

# 27. MappingScope and CapabilityScope

Existing `MappingScope` SHALL remain specialised for mapping applicability.

It SHALL NOT become the universal scope object for unrelated platform decisions.

A new reusable CapabilityScope or equivalent SHALL be introduced where necessary.

Shared vocabulary SHOULD factor common dimensions into reusable schema definitions rather than duplicating semantics.

Conceptually:

```text
CommonScopeDimensions
├── tenant
├── legal_entity
├── estate
├── property
├── channel
├── market
├── country
├── currency
├── region
└── environment

MappingScope
└── CommonScopeDimensions + mapping-specific dimensions

CapabilityScope
└── CommonScopeDimensions + capability-specific dimensions
```

---

# 28. Authorization Boundaries

Baobab SHALL retain three authorization layers.

```text
Authentication
      │
      ▼
BAOBAB IAM
identity + authentication assurance + coarse scopes
      │
      ▼
BAOBAB CONTROL PLANE
tenant + legal entity + estate + context
+ capability entitlement + provider resolution
      │
      ▼
DOMAIN ENGINE
business role + resource relationship
+ workflow state + domain policy
      │
      ▼
BUSINESS ACTION
```

## IAM SHALL own

```text
identity
authentication
credentials
MFA
passkeys
session security
OIDC
OAuth
workload identity
authentication assurance
coarse API scopes
```

## Control Plane SHALL own

```text
tenant state
legal-entity context
Digital Estate context
platform entitlement
CapabilityGrant
CapabilityBinding
provider selection
engine topology
isolation
market-aware resolution
```

## Domain engines SHALL own

```text
purchase approval limits
supplier qualification rules
order cancellation rules
refund authority
inventory rules
journal posting permission
shipment workflow rules
publication workflow
trade approval
```

No layer SHALL silently absorb another layer's authority.

---

# 29. IAM Scope Policy

IAM scopes SHALL authorise classes of platform access.

Examples MAY include:

```text
context:resolve
capability:resolve
identity:self
identity:admin
tenant:admin
```

IAM SHALL NOT become authoritative for business capabilities such as:

```text
supplier.approve
trade.approve
invoice.post
shipment.dispatch
refund.approve
commodity.purchase
```

Such business permissions belong to domain authorization.

---

# 30. Digital Estates SHALL Remain Engine-Agnostic

A Digital Estate SHALL NOT be required to know that:

```text
MedusaJS provides orders
iDempiere provides accounting
Payload provides content
Haystack provides intelligence
```

Digital Estate application code SHALL integrate against stable Baobab domain/capability interfaces.

Vendor-native APIs MAY exist behind domain services and adapters but SHALL NOT become the canonical external platform contract.

Exceptions require explicit architecture approval.

---

# 31. Capability Provider Adapters

Every provider integration SHOULD expose an adapter layer that translates:

```text
Canonical Capability Contract
        ↕
Provider-Native Interface
```

Example:

```text
commerce.order.create
        │
        ▼
baobab-trade adapter
        │
        ▼
MedusaJS workflow/API
```

The canonical interface SHALL remain stable when provider-native implementations evolve.

---

# 32. Source-of-Truth Rule

Capability-centric architecture SHALL NOT undermine domain ownership.

For every canonical business entity or state transition, one authoritative owner SHALL be identified.

Examples:

| Concern | Expected authority |
|---|---|
| Tenant lifecycle | `baobab-cp` |
| Capability grants | `baobab-cp` |
| Capability bindings | `baobab-cp` |
| Identity credentials | `baobab-iam` |
| Orders | commerce domain |
| Accounting documents | `baobab-erp` |
| Content | `baobab-cms` |
| Intelligence outputs | `baobab-pulse` |
| Shared contracts | `baobab-platform/shared` |

Replicas and projections SHALL not become competing sources of truth.

---

# 33. Product Subscription Model

Commercial subscriptions MAY remain in the Control Plane.

However, subscriptions SHALL become a **grant source**, not the final execution abstraction.

Target:

```text
ProductSubscription
       │
       ▼
CapabilityComposition
       │
       ▼
CapabilityGrants
       │
       ▼
CapabilityResolution
```

This SHALL permit:

- tiered products;
- trials;
- custom enterprise packages;
- tenant-specific contracts;
- optional add-ons;
- internal capabilities;
- future billing models.

---

# 34. Capability Composition Example

```text
solution.baobab-xbt
│
├── required
│   ├── tenant.context.resolve
│   ├── identity.authenticate
│   ├── counterparty.manage
│   ├── commercial.contract.manage
│   └── finance.invoice.manage
│
└── optional profiles
    ├── profile.b2b
    ├── profile.b2c
    ├── profile.crossborder
    ├── profile.logistics
    └── profile.commodity-trade
```

A composition SHALL support:

```text
required capabilities
optional capabilities
dependencies
incompatibilities
minimum versions
feature maturity
activation policy
```

---

# 35. Capability Maturity

Capabilities SHOULD expose maturity states.

Recommended:

```text
EXPERIMENTAL
PREVIEW
SUPPORTED
DEPRECATED
RETIRED
```

Production tenants SHOULD NOT receive experimental capabilities unless explicitly authorised.

Capability maturity SHALL be distinct from provider health.

---

# 36. Versioning

Capability contracts SHALL be explicitly versioned.

A capability identifier SHOULD remain stable while contract versions evolve.

Example:

```text
Capability:
    commerce.order.create

Supported contracts:
    v1
    v2
```

Bindings SHALL declare supported contract versions.

Resolution SHALL verify compatibility.

Breaking contract changes SHALL require a new major version.

---

# 37. Events

The Control Plane SHALL emit canonical events for capability lifecycle and resolution-relevant changes.

Examples:

```text
capability.registered
capability.updated
capability.deprecated

capability_grant.created
capability_grant.suspended
capability_grant.revoked

capability_binding.created
capability_binding.activated
capability_binding.suspended
capability_binding.retired

capability_provider.registered
capability_provider.degraded
capability_provider.retired

capability_composition.updated
```

Resolution operations themselves MAY emit auditable decision events subject to volume and privacy policy.

---

# 38. Auditability

Every control-plane decision materially affecting tenant capability access SHALL be auditable.

Audit records SHALL capture as appropriate:

```text
actor
principal
tenant
legal entity
Digital Estate
capability
grant
binding
provider
engine instance
resolution timestamp
decision
reason
correlation ID
policy version
contract version
```

Sensitive configuration and credentials SHALL be redacted.

---

# 39. Observability

Capability-centric observability SHALL permit operators to answer:

```text
Which tenants are using capability X?
Which provider serves capability X?
Which Digital Estates depend on provider Y?
Which capability resolutions are failing?
Where are ambiguities occurring?
Which provider is degraded?
Which markets rely on this engine instance?
Which capabilities would be affected by retiring an engine?
```

Recommended metric dimensions include:

```text
capability_key
provider_key
engine_key
engine_instance
tenant
market
deployment_region
result
reason_code
```

Cardinality SHALL be governed carefully.

---

# 40. Desired-State Reconciliation

Capability topology SHALL form part of Control Plane desired state.

For example:

```text
Desired:
Tenant T
must possess capability C
bound to provider P
within region R
using isolation profile I
```

The reconciler SHALL determine whether runtime infrastructure and provider configuration satisfy the declared target.

The Control Plane SHALL declare desired state.

Infrastructure tooling SHALL perform infrastructure provisioning.

The distinction SHALL remain:

```text
CP:
What should exist?

Infrastructure:
How is infrastructure created?

Provider:
How is domain functionality implemented?
```

---

# 41. API Direction

The Control Plane SHALL evolve toward explicit APIs such as:

```text
/v1/capabilities
/v1/capability-providers
/v1/capability-grants
/v1/capability-bindings
/v1/capability-compositions
/v1/context/resolve
/v1/capabilities/resolve
```

Exact resource naming SHALL be defined by the Shared OpenAPI contract.

A capability-resolution endpoint SHOULD accept:

```text
authenticated principal
tenant hint or requested tenant
legal-entity context
Digital Estate
channel
market
requested capability
operation context permitted by contract
correlation ID
```

and return a deterministic immutable resolution.

---

# 42. Persistence Target

Because no production data exists, the database SHALL be remodelled to represent the target architecture correctly rather than layering compatibility tables indefinitely.

Expected logical schemas MAY include:

```text
registry.*
tenant.*
estate.*
market.*
topology.*
capability.*
mapping.*
policy.*
audit.*
messaging.*
system.*
```

The capability schema SHOULD ultimately contain concepts equivalent to:

```text
capability.capability
capability.capability_dependency
capability.capability_provider
capability.capability_provider_contract
capability.capability_scope
capability.capability_grant
capability.capability_binding
capability.capability_composition
capability.capability_composition_member
```

Exact migration structure SHALL be derived after the companion Shared contract ADR is accepted.

---

# 43. Existing Schema Remodel

Existing pre-production migrations SHALL be reviewed rather than blindly preserved.

Because the platform has no production data:

> **Architectural correctness SHALL take precedence over compatibility with obsolete pre-production database structures.**

The team MAY:

- rewrite non-production migrations;
- collapse superseded migrations;
- rename incorrect columns;
- replace UUID/slug inconsistencies;
- delete unused transitional structures;
- redesign product-subscription tables;
- introduce new capability tables;
- remove redundant schemas;
- regenerate test fixtures and development seed data.

However:

- ADR history SHALL remain;
- migration intent SHALL be documented;
- CI SHALL recreate the database from empty state;
- integration tests SHALL validate the final model.

---

# 44. What `baobab-cp` SHALL Become

At target state, `baobab-cp` SHALL be:

> **The authoritative Baobab platform control system for tenant lifecycle, Digital Estate context, legal-entity context, product entitlement, capability grants, capability topology, provider resolution, engine-instance selection, isolation, residency, canonical cross-engine references, desired-state reconciliation and auditable platform-level authorization.**

It SHALL be responsible for answering:

```text
Who?
Which tenant?
Which legal entity?
Which estate?
Which market/context?
Which capability?
Is it granted?
Which provider can satisfy it?
Which provider is allowed here?
Which instance should serve it?
Which contract version applies?
Which isolation/residency policy applies?
```

It SHALL NOT answer:

```text
How should a purchase order be approved?
How should inventory be allocated?
How should a truck be dispatched?
How should a journal be posted?
How should a customer refund be calculated?
How should commodity risk be calculated?
```

Those remain domain responsibilities.

---

# 45. What `baobab-cp` SHALL NOT Become

The following are explicitly rejected:

## 45.1 Enterprise service bus

CP SHALL NOT become a generic message router for all business traffic.

## 45.2 Workflow engine

CP SHALL NOT model entire order, trade, logistics, ERP or content workflows.

## 45.3 API gateway

CP MAY reconcile gateway desired state, but SHALL NOT replace the gateway.

## 45.4 Identity provider

Authentication remains IAM.

## 45.5 Business authorization monolith

Fine-grained domain authorization remains domain-owned.

## 45.6 Integration dumping ground

Provider-specific translations belong in domain/provider adapters.

## 45.7 Master database for all business objects

Canonical references do not make CP the owner of every entity's business state.

---

# 46. `baobab-platform/shared` Contract Authority

This ADR requires a companion architecture decision in `baobab-platform/shared`.

The companion SHALL define the canonical contracts for:

```text
CapabilityDefinition
CapabilityScope
CapabilityGrant
CapabilityProvider
CapabilityProviderContract
CapabilityBinding
CapabilityDependency
CapabilityComposition
CapabilityCompositionMember
CapabilityResolution
Capability lifecycle events
Capability reason codes
```

`baobab-platform/shared` SHALL remain non-deployable.

It SHALL own:

```text
schemas
OpenAPI
AsyncAPI
event envelopes
registries
versioning rules
generated types/SDKs
validation tools
contract compatibility
```

It SHALL NOT own runtime business behaviour.

---

# 47. Required Companion ADR

The following ADR SHALL be created:

> **ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model**

Its purpose SHALL be to ensure that `shared` becomes the authoritative vocabulary for the capability-centric architecture defined here.

This companion ADR SHALL be approved before final implementation of new capability contracts in CP.

---

# 48. Supplier Domain Concern in Shared

The existing tendency to place reusable supplier-domain implementation code under `shared` SHALL be reviewed.

The governing rule SHALL be:

```text
Shared owns:
    supplier schemas
    supplier events
    supplier contracts
    generated types
    portable identifiers

Domain owner owns:
    supplier workflow
    qualification logic
    approval behaviour
    state transition implementation
```

Shared SHALL NOT evolve into an application-domain monolith.

---

# 49. Implementation Boundaries

Target responsibilities:

| Concern | Owner |
|---|---|
| Capability vocabulary | `baobab-platform/shared` |
| Capability schemas | `baobab-platform/shared` |
| Capability composition contracts | `baobab-platform/shared` |
| Runtime capability registry | `baobab-cp` |
| Capability grants | `baobab-cp` |
| Provider bindings | `baobab-cp` |
| Provider topology | `baobab-cp` |
| Runtime resolution | `baobab-cp` |
| Identity/authentication | `baobab-iam` |
| Business behaviour | Domain engines |
| Business authorization | Domain engines |
| Digital experience | Digital Estates |
| Physical infrastructure | `baobab-platform/infrastructure` |

---

# 50. Target Repository Structure

A target Control Plane structure MAY evolve toward:

```text
baobab-cp/
├── api/
│   ├── context/
│   ├── capabilities/
│   ├── grants/
│   ├── providers/
│   ├── bindings/
│   └── compositions/
│
├── internal/
│   ├── auth/
│   ├── config/
│   │
│   ├── domain/
│   │   ├── tenant/
│   │   ├── estate/
│   │   ├── capability/
│   │   ├── topology/
│   │   ├── mapping/
│   │   └── context/
│   │
│   ├── resolver/
│   │   ├── context/
│   │   ├── capability/
│   │   └── mapping/
│   │
│   ├── repository/
│   ├── reconcile/
│   ├── events/
│   ├── audit/
│   └── service/
│
├── migrations/
├── docs/
│   ├── adr/
│   ├── architecture/
│   └── reconciliation/
└── tests/
```

Package boundaries SHALL follow domain responsibility rather than database table layout.

---

# 51. Capability Resolution Sequence

```text
Digital Estate API
       │
       │ authenticated request
       ▼
Domain Service
       │
       │ capability=commerce.order.create
       ▼
BAOBAB CP
       │
       ├── Principal?
       ├── Tenant?
       ├── Legal entity?
       ├── Estate?
       ├── Market?
       ├── Grant?
       ├── Binding?
       ├── Provider?
       ├── Contract?
       ├── Isolation?
       └── Region?
       │
       ▼
Resolution Decision
       │
       ▼
Capability Provider
       │
       ▼
Domain Authorization
       │
       ▼
Operation
```

---

# 52. Engine Replacement Illustration

Before capability-centric architecture:

```text
ZuriBeans
   │
   ▼
Medusa-specific integration
```

Replacement:

```text
ZuriBeans
   │
   X
Major rewrite
```

Target:

```text
ZuriBeans
   │
   ▼
commerce.order.create
   │
   ▼
Control Plane
   │
   ├── current → Medusa provider
   │
   └── future  → Replacement provider
```

The Digital Estate's conceptual dependency remains unchanged.

---

# 53. Cross-Market Provider Example

Suppose one provider is used in Uganda and another in South Africa.

```text
                   commerce.payment.authorize
                             │
                ┌────────────┴────────────┐
                │                         │
          Uganda context           South Africa context
                │                         │
                ▼                         ▼
         Provider UG                 Provider ZA
                │                         │
                ▼                         ▼
       EngineInstance UG           EngineInstance ZA
```

This SHALL be a normal capability-binding scenario, not a Digital Estate code fork.

---

# 54. Multi-Region Resolution

Similarly:

```text
Tenant
  │
  ▼
Capability
  │
  ▼
Context
  │
  ├── residency = ZA
  ├── region = africa-south
  └── environment = production
  │
  ▼
Eligible providers
  │
  ▼
ZA-compatible EngineInstance
```

A globally available engine instance SHALL NOT automatically be eligible if residency or isolation constraints prohibit it.

---

# 55. Hybrid Business Model

A tenant SHALL be able to consume B2B and B2C capabilities simultaneously.

Example:

```text
Tenant: Future Company
│
├── B2B Estate / Channel
│   ├── RFQ
│   ├── negotiated pricing
│   ├── organisation accounts
│   └── credit terms
│
└── B2C Estate / Channel
    ├── cart
    ├── checkout
    ├── promotions
    └── consumer payments
```

Business model SHALL NOT be hard-coded at tenant level.

It SHALL be expressed through channel/context/capability composition.

---

# 56. Domestic vs Cross-Border

Likewise:

```text
Tenant
│
├── Domestic transaction
│   └── core commerce capabilities
│
└── Cross-border transaction
    └── additional cross-border capabilities
```

Cross-border SHALL be a composable capability/context dimension, not a separate Baobab platform.

---

# 57. Security Requirement

Every capability-resolution boundary SHALL enforce:

```text
authenticated caller
authorised workload scope
canonical tenant context
tenant lifecycle state
legal-entity validity
effective grant
scope compatibility
provider eligibility
contract compatibility
isolation policy
residency policy
deterministic resolution
audit correlation
```

No tenant-controlled header SHALL become authoritative solely because the client supplied it.

Context provenance SHALL remain explicit.

---

# 58. Caching

Capability-resolution caching MAY be permitted.

However:

- cache keys SHALL include all security-relevant resolution dimensions;
- success caches SHALL have bounded TTL;
- revocation SHALL invalidate or outlive caches safely;
- failures SHALL not become long-lived permission decisions;
- cache entries SHALL never widen scope;
- stale positive decisions SHALL fail safely.

Tenant, grant, binding and identity revocation events SHOULD support cache invalidation.

---

# 59. Performance Objective

The capability architecture SHALL not require dozens of synchronous network calls per business request.

The Control Plane SHOULD provide an efficient resolution boundary capable of returning sufficient immutable context for downstream processing.

Optimisation MAY include:

```text
bounded resolution cache
precompiled binding indexes
scope indexes
provider eligibility indexes
local verification of signed context
batch capability resolution
```

Security semantics SHALL NOT be sacrificed for latency.

---

# 60. Batch Resolution

A future API SHOULD support resolving several capabilities for one immutable context.

Example:

```text
Resolve:
    commerce.order.create
    inventory.availability.read
    payment.authorize
```

This MAY reduce repeated resolution overhead.

Batch resolution SHALL preserve per-capability decisions and reason codes.

---

# 61. Signed Context / Resolution Tokens

A future phase MAY introduce short-lived cryptographically protected resolution assertions.

Such assertions MAY allow domain engines to validate:

```text
tenant
legal entity
Digital Estate
capability
grant
provider
expiry
correlation
```

without re-calling CP for every internal hop.

This SHALL require a separate security ADR.

It SHALL NOT be implemented casually as arbitrary JWT business-role inflation.

---

# 62. Provisioning Model

Tenant onboarding SHALL evolve toward:

```text
Create Tenant
     │
     ▼
Associate Legal Entities
     │
     ▼
Register Digital Estates
     │
     ▼
Subscribe to Product / Solution
     │
     ▼
Expand Composition
     │
     ▼
Create Capability Grants
     │
     ▼
Resolve Provider Requirements
     │
     ▼
Create / validate Bindings
     │
     ▼
Reconcile Engine Instances / Infrastructure
     │
     ▼
Verify Capability Readiness
     │
     ▼
Activate Tenant
```

Tenant activation SHALL fail if mandatory product capabilities cannot be satisfied.

---

# 63. Capability Readiness

The Control Plane SHOULD expose readiness per tenant or Digital Estate.

Example:

```text
Baobab XBT / ZuriBeans

supplier.onboarding.manage      READY
commercial.rfq.manage           READY
commerce.pricing.negotiated     READY
commerce.order.manage           READY
finance.invoice.manage          READY
trade.documents.manage          NOT_READY
intelligence.fx.query           READY
```

A product SHALL not be considered fully provisioned while mandatory capabilities remain unresolved.

---

# 64. Impact Analysis

The architecture SHOULD permit:

```text
"If engine instance X is retired, what breaks?"
```

Resolution:

```text
EngineInstance X
   │
   ▼
CapabilityBindings
   │
   ▼
Capabilities
   │
   ▼
CapabilityGrants
   │
   ▼
Tenants / Estates / Products
```

This graph SHALL become valuable for operational governance.

---

# 65. Decommissioning

Capability provider retirement SHALL follow:

```text
Mark provider deprecated
       │
       ▼
Block new bindings
       │
       ▼
Identify affected bindings
       │
       ▼
Create replacement bindings
       │
       ▼
Validate compatibility
       │
       ▼
Shift provider precedence
       │
       ▼
Observe
       │
       ▼
Retire old provider
```

Digital Estates SHOULD not require code changes when canonical capability contracts remain compatible.

---

# 66. Migration Strategy

Because there is no production data, migration SHALL optimise for the final model rather than preserving avoidable debt.

The remodel SHALL occur in deliberate Gates.

## Gate 0 — Reconciliation

Audit:

```text
ADR-BCP-001
current CP Go domain
current migrations
Shared contracts
IAM ADRs
current tests
current consumers
```

Produce an explicit conflict matrix.

---

## Gate 1 — Shared Capability Contract ADR

Create and accept:

```text
ADR-SHARED-007
Canonical Capability Contracts,
Composition Registry and
Cross-Engine Provider Model
```

---

## Gate 2 — Canonical Contracts

Add to Shared:

```text
capability definition schema
scope schema
grant schema
provider schema
binding schema
composition schema
resolution schema
events
reason codes
OpenAPI fragments
AsyncAPI events
```

---

## Gate 3 — Control Plane Domain Remodel

Implement canonical Go domain types.

No API work shall precede stable domain semantics.

---

## Gate 4 — Persistence Remodel

Rewrite or replace pre-production migrations as needed.

Create strong integrity constraints.

---

## Gate 5 — Capability Registry

Implement CRUD and lifecycle services for capabilities and providers.

---

## Gate 6 — Grants

Implement product/composition expansion into CapabilityGrants.

---

## Gate 7 — Binding Resolver

Implement deterministic scoped binding resolution.

---

## Gate 8 — Provider Eligibility

Add contract compatibility, health, region, isolation and residency evaluation.

---

## Gate 9 — Context Integration

Integrate capability resolution into the existing fail-closed context pipeline.

---

## Gate 10 — IAM Integration

Verify authenticated principal and coarse scope boundary without moving business capabilities into Keycloak.

---

## Gate 11 — Digital Estate Consumption

Integrate one Digital Estate through capability-first interfaces.

ZuriBeans SHOULD be the principal B2B/cross-border reference case.

---

## Gate 12 — Second Reference Model

Validate architectural generality against a materially different profile:

```text
Thamani B2C
or
coal-haulage B2B logistics
```

No ZuriBeans-specific assumption SHALL survive if it cannot be justified as a platform invariant.

---

## Gate 13 — Operations

Implement:

```text
audit
metrics
tracing
resolution diagnostics
provider impact analysis
readiness reporting
```

---

## Gate 14 — Documentation and Production Readiness

Complete:

```text
ADRs
OpenAPI
AsyncAPI
runbooks
threat model
recovery model
migration documentation
load tests
security tests
multi-tenant isolation tests
failure tests
ambiguity tests
```

---

# 67. Testing Requirements

The following SHALL be mandatory.

## Capability isolation

A tenant SHALL never receive another tenant's grant or binding.

## Legal-entity isolation

A principal authorised for Legal Entity A SHALL not gain capability access for Legal Entity B without explicit representation.

## Estate isolation

Capability availability MAY differ between Digital Estates.

## Market isolation

A capability valid in one market SHALL not automatically resolve in another.

## Region/residency

A prohibited engine region SHALL never be returned.

## Temporal validity

Expired grants/bindings SHALL not resolve.

## Ambiguity

Equal-precedence bindings SHALL fail.

## Revocation

Revoked grants SHALL stop resolving within the documented revocation window.

## Provider degradation

The resolver SHALL honour explicit failover semantics only where configured.

## Contract mismatch

Unsupported contract versions SHALL fail closed.

---

# 68. Non-Functional Requirements

Target architecture SHALL satisfy:

| Requirement | Principle |
|---|---|
| Multi-tenancy | Tenant context mandatory |
| Multi-legal-entity | Legal entity independent from tenant |
| Multi-estate | Estate is context, not tenant |
| Multi-market | Capabilities may vary by market |
| Multi-region | Provider placement is resolution-aware |
| Multi-currency | Currency is a resolution dimension where applicable |
| Isolation | Policy-driven and fail-closed |
| Residency | Explicitly enforced |
| Replaceability | Digital Estate not bound to provider vendor |
| Auditability | Every authority decision traceable |
| Versionability | Contracts explicitly versioned |
| Extensibility | New providers without platform rewrite |
| Security | Least privilege and deny by default |
| Observability | Capability/provider-centric visibility |
| Resilience | Explicit fallback only |
| Determinism | No arbitrary resolver choice |

---

# 69. Invariants

The following SHALL be architectural invariants.

1. Digital Estates consume capabilities, not engine identities.
2. Capability names are implementation-neutral.
3. Products compose capabilities.
4. Capability grants are separate from capability bindings.
5. Capabilities are separate from providers.
6. Providers are separate from engine instances.
7. IAM does not own business authorization.
8. CP does not own domain business rules.
9. Shared does not execute runtime business behaviour.
10. Domain engines remain systems of record for their business domains.
11. Resolution is deterministic.
12. Ambiguity fails closed.
13. Context is immutable for a resolution decision.
14. Tenant, legal entity and Digital Estate remain distinct.
15. Market involvement does not imply legal presence.
16. Cross-border is a context/capability composition, not a separate platform.
17. B2B and B2C are composable channel/business profiles, not tenant types.
18. No provider is selected without entitlement.
19. No entitlement implies provider selection.
20. No Digital Estate may bypass platform isolation merely because an engine supports direct APIs.

---

# 70. Rejected Alternatives

## Alternative A — Digital Estates integrate directly with engines

Rejected because:

```text
estate → Medusa
estate → iDempiere
estate → Payload
```

creates provider coupling and duplicated integration logic.

## Alternative B — Baobab XBT as another orchestration service

Rejected because CP already owns context and entitlement resolution.

A second orchestration layer would duplicate authority.

## Alternative C — Products equal engines

Rejected because commercial packaging must survive engine replacement.

## Alternative D — Keycloak owns all business permissions

Rejected because it creates role explosion and collapses domain boundaries.

## Alternative E — Put all business workflows in CP

Rejected because CP would become a central business monolith.

## Alternative F — Preserve current pre-production schemas regardless of correctness

Rejected because no production data exists and avoidable compatibility debt has no strategic value.

---

# 71. Consequences

## Positive

This decision enables:

- Digital Estate independence from vendors;
- reusable capabilities;
- product packaging;
- B2B/B2C composition;
- domestic/cross-border composition;
- multi-market resolution;
- engine replacement;
- provider failover;
- tenant-specific capability profiles;
- region-specific providers;
- stronger entitlement;
- clearer source-of-truth boundaries;
- SaaS packaging;
- future third-party providers;
- better audit and observability.

## Costs

The remodel introduces:

- additional domain concepts;
- more rigorous contract governance;
- capability registry maintenance;
- resolver complexity;
- dependency modelling;
- provider certification;
- stronger integration tests;
- migration work now.

These costs are accepted because they reduce significantly larger long-term coupling costs.

---

# 72. Final Target State

The intended Baobab architecture SHALL converge on:

```text
                         BAOBAB

             CAPABILITY-CENTRIC DIGITAL PLATFORM

                              │
                 ┌────────────┴────────────┐
                 │     DIGITAL ESTATES     │
                 │                         │
                 │ ZuriBeans               │
                 │ Thamani                 │
                 │ External B2B/B2C        │
                 │ Future Estates          │
                 └────────────┬────────────┘
                              │
                     consume capabilities
                              │
                              ▼
             ┌────────────────────────────────┐
             │         BAOBAB CONTROL PLANE   │
             │                                │
             │ Identity Context               │
             │ Tenant                         │
             │ Legal Entity                   │
             │ Estate                         │
             │ Market                         │
             │ Capability Grants              │
             │ Capability Composition         │
             │ Capability Bindings            │
             │ Provider Resolution            │
             │ Engine Topology                │
             │ Isolation / Residency          │
             │ Desired State                  │
             │ Audit                          │
             └───────────────┬────────────────┘
                             │
                ┌────────────┼────────────┐
                │            │            │
                ▼            ▼            ▼
          BAOBAB TRADE   BAOBAB ERP   BAOBAB PULSE
             │              │             │
           Medusa        iDempiere      Haystack

                ┌────────────┼────────────┐
                │            │            │
                ▼            ▼            ▼
          BAOBAB CMS     EXTERNAL      FUTURE
            Payload      PROVIDERS     ENGINES

                             ▲
                             │
                       BAOBAB IAM
                        Keycloak
                   identity/authentication

                             ▲
                             │
                      NABHOLD SHARED
                  canonical contract authority
```

The Control Plane SHALL therefore cease to be understood merely as:

> “the service that provisions tenants and knows which engines they use.”

Its final architectural role SHALL be:

> **Baobab's authoritative capability and context control system: the platform component that determines which business capabilities a tenant, legal entity or Digital Estate is entitled to consume, resolves those capabilities to valid providers and engine instances under market, geographic, security, contractual, isolation and residency constraints, and produces deterministic, auditable, fail-closed platform decisions without absorbing domain business logic.**

That is the destination.

All future Control Plane development SHALL be judged against whether it moves Baobab toward or away from that destination.

---

# 73. Decision

**ACCEPTED TARGET ARCHITECTURE, subject to formal approval.**

Upon approval:

1. `ADR-BCP-002` SHALL become the architectural authority for capability-centric platform consumption.
2. `ADR-BCP-001` SHALL remain authoritative where it does not conflict with this decision.
3. Conflicting pre-production implementation details SHALL be remodelled.
4. `ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model` SHALL be drafted next.
5. No new Digital Estate integration SHALL introduce direct provider coupling where a canonical Baobab capability should exist.
6. No new business capability SHALL be introduced as a Keycloak role solely for convenience.
7. No new CP domain behaviour SHALL duplicate domain-engine business logic.
8. The implementation SHALL proceed through explicit reconciliation and migration Gates.
9. ZuriBeans, Thamani and at least one materially different external B2B operating model SHALL be used as architectural validation scenarios.
10. Production readiness SHALL not be declared until capability resolution, entitlement, ambiguity, isolation, residency, revocation and multi-tenant tests pass.

---

# 74. Architectural Maxim

The architecture SHALL be remembered through one sentence:

> **The Digital Estate asks Baobab for a capability; the Control Plane determines whether it may have it, where it must come from, and under what context; the domain provider performs the business work.**