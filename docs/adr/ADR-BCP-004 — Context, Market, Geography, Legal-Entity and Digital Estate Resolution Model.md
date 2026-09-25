# ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model

**Status:** Proposed — Normative Platform Architecture  
**Date:** 2026-09-10  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Repository:** `baobab-platform/baobab-cp`  
**Runtime Authority:** `baobab-platform/baobab-cp`  
**Contract Authority:** `baobab-platform/shared`  
**Identity Authority:** `baobab-platform/baobab-iam`  
**Depends On:**
- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model

**Applies To:** Tenant context, legal entities, organisations, Digital Estates, markets, jurisdictions, countries, channels, transaction geography, operating footprint, deployment regions, currencies, isolation profiles and capability-resolution context  
**Architecture Style:** Explicit-context, multi-tenant, multi-entity, multi-market, multi-region, multi-channel, geography-aware, fail-closed  
**Decision Type:** Foundational runtime context architecture

---

# 1. Executive Decision

Baobab SHALL implement an explicit **Platform Context Model** that represents the business, organisational, geographic and technical circumstances under which a Digital Estate requests a capability.

The Control Plane SHALL NOT reduce context to:

```text
tenant_id
```

or:

```text
tenant_id + market_id
```

because those identifiers are insufficient for the operating models Baobab must support.

The canonical runtime context SHALL be capable of distinguishing:

```text
Tenant
Legal Entity
Organisation
Business Unit
Digital Estate
Digital Property
Channel
Market
Jurisdiction
Legal Footprint
Operating Footprint
Commercial Footprint
Tax Footprint
Transaction Geography
Origin
Destination
Transit Geography
Currency
Deployment Region
Data Residency Region
Environment
Isolation Profile
Principal
Capability
```

These concepts SHALL remain independently modelled even where they happen to have identical values in a simple deployment.

The fundamental rule is:

> **Context describes the circumstances of an operation. It SHALL NOT infer organisational, geographic or legal facts merely because another contextual dimension happens to correlate with them.**

---

# 2. Why This ADR Is Foundational

ADR-BCP-003 defines deterministic capability resolution.

But capability resolution cannot be deterministic if the context supplied to it is ambiguous.

Consider:

```text
ZuriBeans
   │
   ├── incorporated in Uganda
   ├── operates in Uganda
   ├── sells to South Africa
   ├── sources in Uganda
   ├── may transact in USD
   └── may execute through a South African Digital Estate
```

Which of those is:

```text
market?
jurisdiction?
legal entity?
operating region?
transaction geography?
currency?
deployment region?
```

They are not interchangeable.

Likewise, consider a coal haulier:

```text
Legal entity: South Africa

Transport:
South Africa
     │
     ▼
Botswana
     │
     ▼
Zambia
```

The business may:

- be legally incorporated only in South Africa;
- operate vehicles across multiple countries;
- transit Botswana;
- deliver in Zambia;
- invoice a South African customer;
- price the contract in USD;
- consume Baobab from infrastructure hosted in South Africa.

None of those facts SHALL cause CP to invent:

```text
Botswana legal entity
```

or:

```text
Zambia tenant
```

or:

```text
USD market
```

The context model therefore forms the semantic foundation beneath capability resolution.

---

# 3. The Core Separation

The following SHALL be architectural invariants:

```text
Tenant
   ≠
Legal Entity
   ≠
Organisation
   ≠
Digital Estate
   ≠
Market
   ≠
Country
   ≠
Jurisdiction
   ≠
Channel
   ≠
Operating Footprint
   ≠
Transaction Geography
   ≠
Deployment Region
```

They MAY coincide.

They SHALL never be assumed equivalent.

---

# 4. Canonical Context Model

The target conceptual model is:

```text
                    Principal
                        │
                        ▼
                      Tenant
                        │
              ┌─────────┼─────────┐
              │         │         │
              ▼         ▼         ▼
        Legal Entity   Estate   Organisation
              │         │
              │         ▼
              │       Channel
              │
              ▼
        Market Participation
              │
     ┌────────┼────────────┐
     │        │            │
     ▼        ▼            ▼
   Market  Jurisdiction  Footprint
                         │
             ┌───────────┼────────────┐
             │           │            │
             ▼           ▼            ▼
           Legal      Operating    Commercial
                                     │
                                     ▼
                                Transaction
                                 Geography

                        +
               Technical Context
                        │
              ┌─────────┼─────────┐
              ▼         ▼         ▼
           Region   Environment  Isolation
```

All relevant dimensions combine into:

```text
PlatformContext
```

---

# 5. PlatformContext

`PlatformContext` SHALL be the immutable result of context resolution.

Conceptually:

```text
PlatformContext
├── context_id
├── principal
├── tenant
├── legal_entity
├── organisation
├── business_unit
├── digital_estate
├── digital_property
├── channel
├── market
├── jurisdiction
├── market_participation
├── transaction_geography
├── currency
├── deployment_region
├── data_residency
├── environment
├── isolation_profile
├── requested_capability
├── operation_scope
├── resolved_at
├── expires_at
├── correlation_id
└── provenance
```

Not every operation SHALL populate every field.

However, every field that influences capability resolution SHALL be explicit.

---

# 6. Context Is Resolved, Not Trusted

Digital Estates MAY submit contextual hints.

They SHALL NOT unilaterally establish authoritative context.

The resolution pipeline SHALL be:

```text
Digital Estate Request
        │
        ▼
Requested Context
        │
        ▼
Authenticated Principal
        │
        ▼
Tenant Validation
        │
        ▼
Legal Entity Validation
        │
        ▼
Estate Validation
        │
        ▼
Market / Channel Validation
        │
        ▼
Footprint / Participation Validation
        │
        ▼
Region / Isolation / Residency Validation
        │
        ▼
Authoritative PlatformContext
```

---

# 7. Requested Context vs Resolved Context

The API SHALL distinguish:

```text
ContextRequest
```

from:

```text
PlatformContext
```

This distinction is mandatory.

Example request:

```json
{
  "tenant_id": "tn_zuribeans",
  "legal_entity_id": "le_zuribeans_uganda",
  "digital_estate_id": "estate_zuribeans",
  "channel_id": "b2b",
  "market_id": "market_za"
}
```

The client is asking CP to establish whether that combination is valid.

It is NOT declaring that combination authoritative.

---

# 8. Tenant

Tenant SHALL represent the primary Baobab isolation and consumption boundary.

A tenant MAY represent a legal entity by default.

However:

> **Tenant and legal entity SHALL remain separate concepts.**

The current Baobab principle remains:

```text
Legal entity is the default tenant boundary.
```

It does NOT become:

```text
Tenant == LegalEntity
```

as a universal identity rule.

---

# 9. Tenant Responsibilities

Tenant SHALL anchor:

```text
isolation
subscriptions
capability grants
configuration
usage
audit
data ownership
service boundaries
```

Tenant SHALL NOT automatically determine:

```text
market
country
currency
channel
legal footprint
transaction geography
```

---

# 10. Legal Entity

A LegalEntity SHALL represent an incorporated or otherwise legally recognised organisational person relevant to Baobab operations.

Conceptually:

```text
LegalEntity
├── id
├── tenant_id
├── canonical_entity_id
├── legal_name
├── registration_number
├── entity_type
├── incorporation_country
├── incorporation_jurisdiction
├── tax_identifiers
├── status
├── effective_from
├── effective_to
└── metadata
```

Legal identity SHALL not be inferred from Digital Estate branding.

---

# 11. Parent/Subsidiary Relationships

Corporate relationships SHALL be explicit.

Example:

```text
NABHOLD GROUP AFRICA
        │
        ├───────────────┐
        ▼               ▼
   ZuriBeans         Thamani
```

This relationship SHALL NOT allow:

```text
ZuriBeans capability grant
```

to satisfy:

```text
Thamani request
```

unless an explicit platform mechanism authorises such sharing.

Corporate affiliation SHALL not weaken tenant isolation.

---

# 12. Organisation

`Organisation` SHALL represent a broader business organisational construct where needed.

A LegalEntity MAY be an Organisation.

But Organisation MAY also represent:

```text
group
division
business organisation
partner organisation
customer organisation
supplier organisation
```

The precise canonical contract SHALL be governed in Shared.

---

# 13. Business Unit

Business units SHALL be optional organisational subdivisions.

Example:

```text
ZuriBeans
├── Procurement
├── Export
├── Wholesale
└── Finance
```

Business units SHALL NOT become tenants merely because they require separate capability configuration.

---

# 14. Digital Estate

A Digital Estate SHALL represent a Baobab-consuming digital business experience or digital operating environment.

Examples:

```text
ZuriBeans B2B estate
Thamani commerce estate
Nabhold corporate estate
future customer portal
supplier portal
operations portal
```

A Digital Estate SHALL NOT be assumed to equal:

```text
tenant
legal entity
market
channel
```

---

# 15. Digital Property

A Digital Estate MAY contain multiple Digital Properties.

Example:

```text
ZuriBeans Digital Estate
│
├── public website
├── buyer portal
├── supplier portal
├── operations console
└── mobile application
```

These MAY consume different capability subsets while belonging to the same Digital Estate.

---

# 16. Channel

Channel describes the commercial or interaction channel through which an operation occurs.

Initial canonical channel families MAY include:

```text
B2B
B2C
B2B2C
INTERNAL
PARTNER
SUPPLIER
MARKETPLACE
```

Channel SHALL be contextual.

It SHALL NOT define tenant type.

---

# 17. B2B/B2C Rule

The following SHALL be prohibited:

```text
tenant.business_model = B2B
```

as a rigid platform assumption.

Instead:

```text
Tenant
   │
   ├── Estate A
   │      └── B2B
   │
   └── Estate B
          └── B2C
```

SHALL be valid.

A single legal entity MAY eventually operate both B2B and B2C channels.

---

# 18. Market

A Market SHALL represent a commercial market context.

It MAY frequently correspond to a country.

But Market SHALL remain a business concept.

Conceptually:

```text
Market
├── id
├── key
├── name
├── country_code
├── default_currency
├── timezone
├── status
└── metadata
```

Examples:

```text
South Africa
Uganda
regional market
special commercial zone
```

---

# 19. Country vs Market

Country SHALL describe geography/political territory.

Market SHALL describe commercial participation.

Therefore:

```text
Country = ZA
```

does not necessarily mean:

```text
Market = South Africa retail market
```

for every operation.

---

# 20. Jurisdiction

Jurisdiction SHALL represent the legal or regulatory authority relevant to a context.

A jurisdiction MAY correspond to:

```text
country
province/state
customs territory
economic zone
regulatory jurisdiction
```

Jurisdiction SHALL NOT be inferred solely from deployment location.

---

# 21. Market Participation

The Control Plane SHALL introduce or formalise `MarketParticipation`.

This is a critical Baobab concept.

Conceptually:

```text
MarketParticipation
├── id
├── tenant_id
├── legal_entity_id
├── market_id
├── participation_type
├── legal_presence
├── operating_presence
├── tax_presence
├── commercial_presence
├── effective_from
├── effective_to
├── status
└── metadata
```

---

# 22. Participation Types

Canonical participation types SHOULD support:

```text
LEGAL_PRESENCE
DOMESTIC_OPERATION
SELLING
BUYING
SOURCING
EXPORTING
IMPORTING
TRANSITING
DELIVERING
WAREHOUSING
TRANSPORTING
SERVICE_PROVISION
```

Multiple participation types MAY apply simultaneously.

---

# 23. Footprint Model

Baobab SHALL explicitly distinguish:

```text
Legal Footprint
Operating Footprint
Tax Footprint
Commercial Footprint
Transaction Footprint
```

These dimensions SHALL not collapse into one `market` flag.

---

# 24. Legal Footprint

Legal footprint answers:

> Where does the organisation possess relevant legal presence?

Examples:

```text
incorporated entity
registered branch
recognised establishment
```

This is different from merely selling into a country.

---

# 25. Operating Footprint

Operating footprint answers:

> Where does the organisation maintain meaningful operations?

Examples MAY include:

```text
office
warehouse
fleet base
staff
operational facility
distribution facility
```

The precise business/legal meaning SHALL be configuration/domain driven.

---

# 26. Tax Footprint

Tax footprint represents known tax registrations or obligations relevant to platform configuration.

It SHALL NOT be treated as automated legal determination.

Baobab MAY record:

```text
tax registration
tax identifier
tax configuration
```

It SHALL NOT claim that recorded configuration proves complete tax compliance.

---

# 27. Commercial Footprint

Commercial footprint answers:

> In which markets does the organisation intentionally buy, sell or provide services?

A company MAY have commercial participation without local incorporation.

---

# 28. Transaction Footprint

Transaction footprint describes where a particular transaction actually occurs or passes.

This is operation-specific.

It SHALL not automatically modify the organisation's permanent footprint.

---

# 29. Critical Distinction

Consider:

```text
Company incorporated: ZA
Customer: ZM
Transit: BW
Currency: USD
Infrastructure: ZA
```

The platform SHALL understand:

```text
Legal footprint         ZA
Transaction origin      ZA
Transit                 BW
Destination             ZM
Transaction currency    USD
Deployment region       ZA-compatible region
```

It SHALL NOT infer:

```text
legal footprint = ZA + BW + ZM
```

---

# 30. Transaction Geography

Baobab SHALL model transaction geography explicitly where relevant.

Conceptually:

```text
TransactionGeography
├── origin_country
├── origin_location
├── destination_country
├── destination_location
├── transit_countries[]
├── ports[]
├── border_points[]
├── customs_territories[]
└── route_reference
```

Not every capability SHALL require all fields.

---

# 31. Origin and Destination

Origin and destination SHALL be first-class contextual dimensions for cross-border-capable operations.

Example:

```text
Coffee:
UG → ZA

Coal:
ZA → ZM

Consumer order:
ZA → ZA
```

The same capability may resolve differently according to origin/destination.

---

# 32. Transit

Transit SHALL remain distinct from:

```text
origin
destination
market participation
legal presence
```

A business transiting a country SHALL not automatically be considered commercially operating there.

---

# 33. Domestic vs Cross-Border

Baobab SHALL derive domestic/cross-border transaction context.

Conceptually:

```text
origin.country == destination.country
       │
       ▼
    DOMESTIC
```

and:

```text
origin.country != destination.country
       │
       ▼
  CROSS_BORDER
```

where such comparison is semantically applicable.

However, callers SHALL not hard-code platform architecture around this binary.

---

# 34. Cross-Border Is Context, Not Product Identity

Baobab XBT MAY compose cross-border capabilities.

But:

```text
cross-border
```

SHALL remain a transaction/geography characteristic.

A tenant MAY execute:

```text
domestic transactions
+
cross-border transactions
```

through the same platform.

---

# 35. Currency

Currency SHALL be explicit.

The platform SHALL distinguish:

```text
tenant reporting currency
legal entity functional currency
market default currency
catalogue currency
pricing currency
transaction currency
settlement currency
```

These SHALL NOT automatically be identical.

---

# 36. Currency Example

ZuriBeans may have:

```text
Legal entity functional currency: UGX
Market currency: ZAR
Quotation currency: USD
Settlement currency: USD
```

This SHALL be representable without changing tenant or legal entity identity.

---

# 37. Currency Does Not Define Market

The following inference is prohibited:

```text
USD transaction
   ↓
United States market
```

Currency SHALL never independently determine market.

---

# 38. Deployment Region

DeploymentRegion SHALL describe where a Baobab engine instance is deployed.

Examples:

```text
africa-south
africa-east
eu-west
```

Exact names SHALL follow infrastructure conventions.

DeploymentRegion is a technical topology concept.

---

# 39. Deployment Region ≠ Business Geography

This is mandatory:

```text
Deployment Region
       ≠
Market
       ≠
Legal Jurisdiction
       ≠
Transaction Geography
```

A South African customer MAY transact with Zambia while the relevant service remains deployed in an approved South African region.

---

# 40. Data Residency

Data residency SHALL be explicitly represented as policy.

Conceptually:

```text
DataResidencyPolicy
├── id
├── tenant_id
├── data_classification
├── allowed_regions[]
├── prohibited_regions[]
├── replication_policy
└── status
```

Capability resolution SHALL evaluate applicable residency policy before selecting provider instances.

---

# 41. Isolation Profile

Context SHALL include the resolved isolation profile.

Examples MAY include:

```text
SHARED
LOGICAL_TENANT
DEDICATED_SCHEMA
DEDICATED_DATABASE
DEDICATED_INSTANCE
DEDICATED_REGION
```

Exact canonical profiles SHALL remain governed separately.

---

# 42. Isolation Is Contextual

A tenant MAY have:

```text
general capabilities -> shared infrastructure

restricted finance capability -> dedicated instance
```

Therefore isolation SHALL be capable of participating in capability resolution.

---

# 43. Environment

Environment SHALL be explicit.

At minimum:

```text
development
test
staging
production
```

A production context SHALL never resolve accidentally to a development provider.

---

# 44. Principal Context

PlatformContext SHALL retain canonical principal identity.

Conceptually:

```text
PrincipalContext
├── principal_id
├── principal_type
├── subject
├── client_id
├── authentication_assurance
├── workload_identity
└── token_provenance
```

IAM provides identity facts.

CP validates contextual applicability.

---

# 45. Principal Types

Potential principal types:

```text
USER
SERVICE
WORKLOAD
SYSTEM
AGENT
```

An AI agent SHALL not receive implicit capability authority merely because it acts on behalf of a user.

Delegation SHALL be explicit.

---

# 46. Context Provenance

Each resolved dimension SHOULD retain provenance.

Example:

```text
tenant:
  source = authenticated relationship

market:
  source = validated request

deployment_region:
  source = control-plane topology

legal_entity:
  source = tenant configuration

principal:
  source = IAM
```

This provides explainability.

---

# 47. Context Confidence

The Control Plane SHOULD distinguish:

```text
AUTHORITATIVE
VALIDATED
DERIVED
```

context values.

Security-critical dimensions SHALL not depend on untrusted inference.

---

# 48. Derived Context

Some fields MAY safely be derived.

Example:

```text
origin = ZA
destination = ZM

transaction_geography_type = CROSS_BORDER
```

This is acceptable because the derivation is deterministic.

---

# 49. Dangerous Inference

The following SHALL NOT occur:

```text
destination = ZM
       │
       ▼
legal_entity = Zambia subsidiary
```

unless explicit entity configuration supports it.

Similarly:

```text
estate = zuribeans.co.za
       │
       ▼
market = ZA
```

SHALL not be authoritative merely because of hostname.

---

# 50. Context Resolution Request

Conceptual contract:

```json
{
  "tenant_id": "tn_zuribeans",
  "legal_entity_id": "le_zuribeans_uganda",
  "digital_estate_id": "estate_zuribeans",
  "digital_property_id": "buyer_portal",
  "channel_id": "b2b",
  "market_id": "za",
  "currency": "USD",
  "operation_scope": {
    "origin_country": "UG",
    "destination_country": "ZA"
  }
}
```

---

# 51. Context Resolution Result

Conceptually:

```json
{
  "context_id": "ctx_...",
  "tenant": "...",
  "legal_entity": "...",
  "digital_estate": "...",
  "channel": "B2B",
  "market": "ZA",
  "transaction_geography": {
    "origin": "UG",
    "destination": "ZA",
    "type": "CROSS_BORDER"
  },
  "currency": "USD",
  "deployment_region": "africa-south",
  "environment": "production",
  "isolation_profile": "...",
  "resolved_at": "...",
  "expires_at": "..."
}
```

---

# 52. Context Resolution Algorithm

The normative conceptual algorithm SHALL be:

```text
START
  │
  ▼
Authenticate caller
  │
  ├── invalid → DENY
  ▼
Resolve principal
  │
  ├── inactive/revoked → DENY
  ▼
Resolve tenant
  │
  ├── unknown/inactive → DENY
  ▼
Validate principal ↔ tenant relationship
  │
  ├── invalid → DENY
  ▼
Resolve requested legal entity
  │
  ├── not under permitted tenant context → DENY
  ▼
Resolve Digital Estate
  │
  ├── not associated → DENY
  ▼
Resolve Digital Property
  │
  ▼
Validate channel
  │
  ▼
Resolve market
  │
  ▼
Validate MarketParticipation
  │
  ▼
Resolve jurisdiction
  │
  ▼
Resolve operation geography
  │
  ▼
Derive domestic/cross-border where applicable
  │
  ▼
Resolve currency context
  │
  ▼
Resolve residency requirements
  │
  ▼
Resolve isolation profile
  │
  ▼
Resolve eligible deployment region
  │
  ▼
Construct immutable PlatformContext
  │
  ▼
Audit
  │
  ▼
RETURN
```

---

# 53. Fail-Closed Context Resolution

If a security-relevant contextual fact cannot be established:

```text
resolution SHALL fail
```

rather than silently selecting a default.

Examples:

```text
unknown tenant
unknown legal entity
unrecognised estate
ambiguous market
invalid tenant/entity relationship
invalid estate/entity relationship
residency conflict
```

---

# 54. Defaults

Defaults MAY be used only where:

1. they are explicitly configured;
2. they do not weaken security;
3. they do not alter legal meaning;
4. the caller may override them where permitted;
5. the resulting value is visible in resolved context.

Example:

```text
market default currency
```

may be reasonable.

Example:

```text
unknown legal entity -> tenant's first legal entity
```

is prohibited.

---

# 55. Context Hierarchy

A useful conceptual hierarchy is:

```text
Tenant
 │
 ├── Legal Entities
 │
 ├── Organisations
 │
 ├── Digital Estates
 │     │
 │     └── Digital Properties
 │
 ├── Market Participations
 │
 ├── Isolation Policies
 │
 └── Residency Policies
```

This is NOT an ownership hierarchy for all business data.

It is a context-resolution relationship model.

---

# 56. Market Participation Example — ZuriBeans

Illustrative:

| Dimension | Uganda | South Africa |
|---|---:|---:|
| Legal presence | Yes | Depends on actual entity configuration |
| Operating presence | Yes | Configured fact |
| Selling | Yes | Yes |
| Buying | Yes | Possible |
| Sourcing | Yes | Possible |
| Exporting | Yes | Context dependent |
| Importing | Context dependent | Yes |
| Commercial presence | Yes | Yes |

The model SHALL store known facts rather than infer them from transactions.

---

# 57. ZuriBeans Cross-Border Example

```text
Tenant:
ZuriBeans

Legal Entity:
ZuriBeans Uganda

Channel:
B2B

Commercial Market:
South Africa

Origin:
Uganda

Destination:
South Africa

Product:
Coffee

Pricing Currency:
USD

Transaction Type:
CROSS_BORDER
```

This SHALL resolve without requiring:

```text
South African legal entity
```

unless actual platform/legal configuration requires one.

---

# 58. ZuriBeans Domestic Example

The same tenant may execute:

```text
Origin: UG
Destination: UG
Market: Uganda
Channel: B2B
```

Result:

```text
DOMESTIC
```

No different tenant or platform product is required.

---

# 59. Coal Haulier Example

```text
Tenant:
Coal Haulier

Legal Entity:
South Africa

Channel:
B2B

Service:
Road Freight

Origin:
South Africa

Transit:
Botswana

Destination:
Zambia

Invoice Currency:
USD
```

Context SHALL preserve all dimensions independently.

---

# 60. Coal Transit Illustration

```text
          LEGAL ENTITY
               │
               ▼
         SOUTH AFRICA
               │
               │ dispatch
               ▼
        ┌─────────────┐
        │     ZA      │
        └──────┬──────┘
               │
               ▼
        ┌─────────────┐
        │     BW      │  ← TRANSIT
        └──────┬──────┘
               │
               ▼
        ┌─────────────┐
        │     ZM      │  ← DESTINATION
        └─────────────┘
```

Neither transit nor destination automatically creates legal presence.

---

# 61. Petroleum Trading Example

A petroleum trader MAY have:

```text
Tenant: Trader A
Legal entity: Country X
Seller: Country Y
Commodity origin: Country Z
Buyer: Country A
Destination port: Country B
Settlement currency: USD
```

The model SHALL not attempt to compress this into one `market_country`.

That simplification would make Baobab unsuitable for serious trade operations.

---

# 62. Thamani Domestic B2C Example

```text
Tenant:
Thamani

Legal Entity:
Thamani South Africa

Estate:
Thamani Storefront

Channel:
B2C

Market:
South Africa

Origin:
South Africa

Destination:
South Africa

Currency:
ZAR
```

Result:

```text
B2C + DOMESTIC
```

using the same platform context model.

---

# 63. Future Thamani Cross-Border Example

If Thamani later sells:

```text
South Africa → Namibia
```

the platform SHALL not require redesign.

Context becomes:

```text
Channel: B2C
Geography: CROSS_BORDER
Origin: ZA
Destination: NA
```

Cross-border capabilities MAY then become applicable.

---

# 64. Hybrid Business Example

A future tenant MAY operate:

```text
Tenant X
│
├── B2C Store
│    └── ZA
│
├── B2B Wholesale
│    ├── ZA
│    └── BW
│
└── Supplier Portal
     ├── UG
     └── KE
```

The tenant SHALL remain one tenant where legal/isolation policy permits.

Context and capability composition SHALL handle differences.

---

# 65. Four-Axis Business Context

Baobab SHALL explicitly recognise four independent business dimensions:

```text
              BUSINESS CONTEXT

      ┌─────────────────────────┐
      │ 1. CHANNEL              │
      │ B2B / B2C / partner     │
      └────────────┬────────────┘
                   │
      ┌────────────▼────────────┐
      │ 2. GEOGRAPHY            │
      │ domestic / cross-border │
      └────────────┬────────────┘
                   │
      ┌────────────▼────────────┐
      │ 3. FOOTPRINT            │
      │ legal/ops/tax/commercial│
      └────────────┬────────────┘
                   │
      ┌────────────▼────────────┐
      │ 4. TRANSACTION TYPE     │
      │ goods/service/logistics │
      └─────────────────────────┘
```

No one axis SHALL be derived from another unless deterministic semantics permit it.

---

# 66. Transaction Type

Context MAY include a high-level transaction classification.

Examples:

```text
GOODS
SERVICE
LOGISTICS
COMMODITY_TRADE
DIGITAL_SERVICE
```

This MAY influence capability resolution.

It SHALL not become an uncontrolled substitute for domain modelling.

---

# 67. Context and Capability Composition

Context SHALL influence capability selection.

Example:

```text
Tenant
  +
Estate
  +
B2B
  +
Cross-Border
  +
Commodity Trade
        │
        ▼
Capability Composition
```

Possible resulting capabilities:

```text
commercial.rfq.manage
commercial.contract.manage
pricing.negotiated.resolve
trade.execution.manage
documents.trade.manage
logistics.shipment.manage
finance.settlement.manage
intelligence.fx.query
```

---

# 68. Context Does Not Equal Capability

The existence of:

```text
channel = B2B
```

does NOT automatically grant:

```text
commercial.rfq.manage
```

Context determines applicability.

Grant determines entitlement.

Binding determines implementation.

---

# 69. Context Resolution + Capability Resolution

The complete CP path SHALL become:

```text
Request
   │
   ▼
Identity Validation
   │
   ▼
Context Resolution
   │
   ▼
Immutable PlatformContext
   │
   ▼
Capability Resolution
   │
   ├── Grant
   ├── Scope
   ├── Provider
   ├── Binding
   └── Policy
   │
   ▼
Engine Instance
```

---

# 70. Context Identifier

Every successfully resolved context SHALL receive:

```text
context_id
```

Downstream operations SHOULD propagate this identifier.

This enables:

```text
audit
traceability
resolution correlation
incident investigation
cross-engine observability
```

---

# 71. Context Immutability

A resolved context SHALL be immutable.

If:

```text
market changes
legal entity changes
channel changes
origin changes
destination changes
```

a new context SHALL be resolved.

Historical contexts SHALL not be rewritten.

---

# 72. Context Lifetime

Context MAY have bounded lifetime.

Example:

```text
resolved_at
expires_at
```

A long-running transaction SHALL not assume that a stale context remains valid forever.

---

# 73. Context Cache

CP MAY cache resolved contexts.

Cache keys SHALL include all authoritative input dimensions relevant to resolution.

At minimum:

```text
principal
tenant
legal entity
estate
property
channel
market
operation geography
environment
```

where applicable.

---

# 74. Context Cache Invalidation

Events such as:

```text
tenant.suspended
legal_entity.suspended
estate.suspended
market_participation.revoked
identity.revoked
isolation_profile.changed
residency_policy.changed
```

SHALL invalidate affected contexts.

---

# 75. Context API

Recommended boundary:

```text
POST /v1/context/resolve
```

Optional:

```text
POST /v1/context/resolve-with-capabilities
```

for efficient batch resolution.

The latter SHALL internally preserve the context/capability separation.

---

# 76. Context Inspection

Administrative API MAY provide:

```text
GET /v1/contexts/{context_id}
```

subject to strict authorization.

This SHALL be primarily for:

```text
support
audit
debugging
operations
```

---

# 77. Context Explainability

Privileged diagnostics SHOULD explain:

```text
Tenant resolved from authenticated relationship.
Legal entity validated under tenant.
Estate associated with legal entity.
Market participation permits selling in ZA.
Origin UG and destination ZA derive CROSS_BORDER.
USD accepted as transaction currency.
Residency policy requires africa-south.
Isolation profile requires tenant isolation.
```

This will be invaluable when onboarding external customers.

---

# 78. Market Participation Persistence

Recommended conceptual table:

```sql
CREATE TABLE context.market_participation (
    id                    uuid PRIMARY KEY,
    tenant_id             uuid NOT NULL,
    legal_entity_id       uuid,
    market_id             uuid NOT NULL,

    participation_type    text NOT NULL,

    legal_presence        boolean NOT NULL DEFAULT false,
    operating_presence    boolean NOT NULL DEFAULT false,
    tax_presence          boolean NOT NULL DEFAULT false,
    commercial_presence   boolean NOT NULL DEFAULT false,

    status                text NOT NULL,

    effective_from        timestamptz NOT NULL,
    effective_to          timestamptz,

    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,

    CHECK (
      effective_to IS NULL OR
      effective_to > effective_from
    )
);
```

Exact types SHALL conform to canonical Shared contracts.

---

# 79. Market Persistence

Conceptually:

```sql
CREATE TABLE context.market (
    id                uuid PRIMARY KEY,
    market_key        text NOT NULL UNIQUE,
    name              text NOT NULL,
    country_code      char(2),
    default_currency  char(3),
    timezone          text,
    status            text NOT NULL,
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL
);
```

---

# 80. Legal Entity Persistence

Conceptually:

```sql
CREATE TABLE context.legal_entity (
    id                         uuid PRIMARY KEY,
    tenant_id                  uuid NOT NULL,
    canonical_entity_id        uuid,
    legal_name                 text NOT NULL,
    registration_number        text,
    incorporation_country      char(2),
    incorporation_jurisdiction text,
    status                     text NOT NULL,
    metadata                   jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at                 timestamptz NOT NULL,
    updated_at                 timestamptz NOT NULL
);
```

---

# 81. Digital Estate Persistence

Conceptually:

```sql
CREATE TABLE context.digital_estate (
    id                uuid PRIMARY KEY,
    tenant_id         uuid NOT NULL,
    estate_key        text NOT NULL,
    name              text NOT NULL,
    status            text NOT NULL,
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL,

    UNIQUE (tenant_id, estate_key)
);
```

---

# 82. Estate-to-Legal-Entity Relationship

The model SHOULD explicitly represent allowed estate/legal-entity relationships.

Conceptually:

```text
DigitalEstateLegalEntity
├── estate_id
├── legal_entity_id
├── relationship_type
├── effective_from
├── effective_to
└── status
```

This avoids assuming one estate always maps permanently to exactly one legal entity.

---

# 83. Estate-to-Market Relationship

Likewise:

```text
DigitalEstateMarket
```

MAY represent markets an estate is configured to serve.

This is configuration.

It does NOT itself establish legal presence.

---

# 84. Channel Relationship

Digital Estates MAY support multiple channels.

Conceptually:

```text
DigitalEstateChannel
├── estate_id
├── channel_id
├── status
└── configuration
```

---

# 85. Context Persistence Strategy

The Control Plane SHOULD persist:

```text
stable contextual entities
relationships
configuration
policy
```

It MAY persist resolved context snapshots for audit.

It SHALL NOT attempt to store all business transactions merely because those transactions have context.

---

# 86. Context Snapshot

A resolution audit snapshot MAY contain:

```text
context_id
tenant_id
legal_entity_id
estate_id
channel_id
market_id
jurisdiction
currency
transaction_geography
deployment_region
isolation_profile
principal_id
resolved_at
provenance
```

This SHALL be sufficient to reconstruct the decision environment.

---

# 87. CP SHALL Not Become Master of Business Geography

CP MAY understand:

```text
origin
destination
transit
market
jurisdiction
```

for routing and policy.

But detailed domain objects such as:

```text
Shipment
RoutePlan
PortCall
TruckJourney
CustomsDeclaration
```

belong in appropriate domain engines.

CP stores only what is necessary to establish platform context.

---

# 88. CP SHALL Not Become Master of Corporate ERP Data

Legal entity context in CP SHALL contain enough canonical identity for platform resolution.

Detailed:

```text
chart of accounts
tax accounting
financial periods
ledger
AR/AP
```

remain ERP responsibilities.

---

# 89. Canonical Entity Integration

Where applicable:

```text
LegalEntity
Market
Organisation
DigitalEstate
```

SHOULD link to CP's existing canonical-entity/reference/mapping architecture.

The remodel SHALL not create duplicate identity systems.

---

# 90. External References

Engine-native identifiers SHALL continue to use:

```text
ExternalReference
Mapping
MappingScope
```

where appropriate.

Example:

```text
CP LegalEntity
       │
       ├── iDempiere organisation ID
       └── Medusa sales-channel reference
```

The canonical context SHALL not expose those as primary platform identity.

---

# 91. MappingScope Alignment

`MappingScope` and `CapabilityScope` SHALL share canonical scope vocabulary where sensible.

They SHALL remain different domain concepts.

Example:

```text
MappingScope
answers:
"When does this external identifier mapping apply?"

CapabilityScope
answers:
"When does this capability grant/binding apply?"
```

---

# 92. IAM Relationship

IAM SHALL NOT determine:

```text
market
legal entity
transaction geography
capability provider
```

from Keycloak roles.

IAM provides authenticated identity.

CP resolves authoritative business/platform context.

---

# 93. Security Invariant

A caller SHALL not gain access merely by changing:

```text
tenant_id
legal_entity_id
estate_id
market_id
```

in a request.

Every requested dimension SHALL be validated against authoritative relationships.

---

# 94. IDOR Protection

Context APIs SHALL explicitly defend against insecure direct object reference attacks.

For every requested object:

```text
exists?
belongs to correct boundary?
principal permitted?
active?
relationship valid?
```

All SHALL be checked.

---

# 95. Cross-Tenant Queries

Repository methods SHALL require tenant context where tenant-scoped data is involved.

The following pattern is prohibited:

```text
FindLegalEntityByID(id)
```

if it permits tenant-independent lookup during a tenant operation.

Prefer semantics equivalent to:

```text
FindLegalEntity(tenantID, legalEntityID)
```

---

# 96. Database Constraints

Where feasible, composite keys, foreign keys or constraints SHOULD reinforce tenant ownership relationships.

Application checks alone SHALL not be the only isolation layer.

---

# 97. Temporal Context

Market participation and estate/entity relationships SHALL support:

```text
effective_from
effective_to
```

because organisational footprints change over time.

---

# 98. Historical Correctness

If ZuriBeans begins operating in a new market in 2027, that SHALL not make a 2026 historical context appear as though the market participation already existed.

Temporal correctness is required.

---

# 99. Context and Data Residency

Resolution example:

```text
Tenant
  │
  ▼
Restricted data
  │
  ▼
Residency policy
  │
  ▼
Allowed deployment regions
  │
  ▼
Capability binding candidates
```

The context resolver and capability resolver SHALL cooperate without merging responsibilities.

---

# 100. Context and Isolation

Similarly:

```text
Tenant
  │
  ▼
Isolation Profile
  │
  ▼
Required topology characteristics
  │
  ▼
Eligible Engine Instances
```

---

# 101. Context and Product Composition

ProductSubscription MAY establish baseline composition.

Context determines which conditional capabilities are relevant.

Example:

```text
Baobab XBT
   │
   ├── domestic operation
   │      └── no customs capability required
   │
   └── cross-border operation
          └── trade-document/customs capabilities applicable
```

Entitlement SHALL still be validated.

---

# 102. Context and Pulse

Pulse MAY use context such as:

```text
market
origin
destination
commodity
currency
jurisdiction
```

to generate relevant intelligence.

CP SHALL provide canonical context.

Pulse SHALL perform intelligence.

---

# 103. Context and ERP

ERP capability resolution MAY depend upon:

```text
legal entity
organisation
market
currency
```

CP SHALL resolve the correct ERP provider/instance.

iDempiere SHALL remain authoritative for ERP business state.

---

# 104. Context and Trade

Trade capabilities MAY depend upon:

```text
estate
channel
market
customer segment
currency
```

CP SHALL resolve capability provider.

Medusa or future provider SHALL perform the business operation.

---

# 105. Context and IAM

IAM MAY authenticate:

```text
buyer employee
supplier employee
tenant administrator
platform administrator
service workload
```

CP SHALL establish which contextual boundaries the principal may enter.

---

# 106. Context Resolution Test Matrix

The implementation SHALL include at least:

| Scenario | Expected |
|---|---|
| Valid tenant/entity/estate | Resolve |
| Entity belongs to another tenant | Deny |
| Estate belongs to another tenant | Deny |
| Estate not permitted for entity | Deny |
| Market participation active | Resolve |
| Market participation expired | Deny where required |
| B2B domestic | Resolve |
| B2B cross-border | Resolve |
| B2C domestic | Resolve |
| B2C cross-border | Resolve if configured |
| Transit without legal presence | Resolve where permitted |
| Destination without local entity | Resolve where permitted |
| Invalid residency region | Deny |
| Invalid isolation topology | Deny |
| Suspended tenant | Deny |
| Suspended legal entity | Deny |
| Suspended estate | Deny |
| Invalid currency | Deny or reject context |
| Ambiguous legal entity | Fail closed |
| Unknown market | Fail closed |

---

# 107. Required Reference Scenarios

No context architecture SHALL be considered complete until it passes these fixtures.

## Fixture A — ZuriBeans Domestic B2B

```text
B2B
UG → UG
```

## Fixture B — ZuriBeans Cross-Border B2B

```text
B2B
UG → ZA
```

## Fixture C — Thamani Domestic B2C

```text
B2C
ZA → ZA
```

## Fixture D — Thamani Cross-Border B2C

```text
B2C
ZA → NA
```

## Fixture E — Coal Haulier

```text
B2B Logistics
ZA → BW transit → ZM
```

## Fixture F — Petroleum Trader

```text
B2B Commodity Trade
multi-party
multi-jurisdiction
cross-border
USD settlement
```

---

# 108. Context Invariant Matrix

| Concept | Must Not Imply |
|---|---|
| Tenant | Legal entity |
| Legal entity | Market |
| Market | Legal presence |
| Selling market | Local incorporation |
| Transit country | Operating entity |
| Destination country | Legal presence |
| Currency | Market |
| Digital Estate | Tenant |
| Digital Estate | Legal entity |
| Channel | Tenant type |
| B2B | Cross-border |
| B2C | Domestic |
| Deployment region | Market |
| Deployment region | Jurisdiction |
| Product subscription | Capability entitlement without grant |
| Context | Business authorization |

---

# 109. Anti-Patterns

The following SHALL be rejected.

### Tenant-as-everything

```text
tenant = company = country = market
```

### Country-driven entity inference

```text
destination ZA -> South African legal entity
```

### Estate-driven tenancy inference

```text
hostname -> tenant authority
```

### Currency-driven market inference

```text
USD -> US market
```

### Deployment-driven jurisdiction inference

```text
server in ZA -> transaction jurisdiction ZA
```

### Channel-driven tenant classification

```text
tenant = B2B tenant
```

### Cross-border service explosion

```text
domestic service
cross-border service
```

when context can govern the difference.

---

# 110. Context Readiness

CP SHOULD expose context configuration readiness.

Example:

```text
ZuriBeans
├── tenant                    READY
├── legal entity              READY
├── estate                    READY
├── market UG                 READY
├── market ZA                 READY
├── market participation      READY
├── isolation                 READY
└── residency                 READY
```

This SHALL support provisioning and operations.

---

# 111. Administrative Context Preview

CP SHOULD support privileged simulation:

```text
Given:
tenant = ZuriBeans
entity = Uganda
estate = ZuriBeans
market = ZA
origin = UG
destination = ZA

What context would resolve?
```

The preview SHALL not execute business operations.

---

# 112. Impact Analysis

The architecture SHALL support reverse queries such as:

```text
If MarketParticipation ZA is suspended,
which:
  Digital Estates
  capabilities
  bindings
  subscriptions
  contexts
are affected?
```

This is necessary for production operations.

---

# 113. Observability

Recommended metrics:

```text
context_resolution_total
context_resolution_duration_seconds
context_resolution_denied_total
context_resolution_ambiguous_total
context_cache_hit_total
context_cache_miss_total
market_participation_validation_total
```

---

# 114. Audit

Context resolution SHALL record enough information to determine:

```text
who requested
which tenant
which legal entity
which estate
which market
which operation geography
which principal
what was derived
what was validated
why resolution succeeded/failed
```

---

# 115. Shared Contract Requirements

`baobab-platform/shared` SHALL define canonical contracts for at least:

```text
PlatformContext
ContextRequest
PrincipalContext
TenantRef
LegalEntityRef
OrganisationRef
DigitalEstateRef
DigitalPropertyRef
ChannelRef
MarketRef
JurisdictionRef
MarketParticipationRef
TransactionGeography
CurrencyContext
DeploymentRegionRef
DataResidencyRef
IsolationProfileRef
ContextResolutionResult
ContextReasonCode
```

CP SHALL consume these contracts rather than create incompatible local equivalents.

---

# 116. Reason Codes

Shared SHOULD define reason codes including:

```text
CONTEXT_INVALID
CONTEXT_AMBIGUOUS

TENANT_UNKNOWN
TENANT_INACTIVE

LEGAL_ENTITY_UNKNOWN
LEGAL_ENTITY_INACTIVE
LEGAL_ENTITY_TENANT_MISMATCH

DIGITAL_ESTATE_UNKNOWN
DIGITAL_ESTATE_INACTIVE
DIGITAL_ESTATE_TENANT_MISMATCH
DIGITAL_ESTATE_ENTITY_MISMATCH

CHANNEL_NOT_ALLOWED

MARKET_UNKNOWN
MARKET_INACTIVE
MARKET_PARTICIPATION_NOT_FOUND
MARKET_PARTICIPATION_INACTIVE

JURISDICTION_INVALID

CURRENCY_UNSUPPORTED

RESIDENCY_POLICY_MISMATCH
ISOLATION_POLICY_MISMATCH
DEPLOYMENT_REGION_UNAVAILABLE
```

---

# 117. Context Events

Recommended events:

```text
context.market.created
context.market.updated

context.market-participation.created
context.market-participation.updated
context.market-participation.suspended

context.legal-entity.created
context.legal-entity.updated
context.legal-entity.suspended

context.digital-estate.created
context.digital-estate.updated
context.digital-estate.suspended

context.residency-policy.updated
context.isolation-profile.updated
```

Resolved contexts themselves SHOULD normally be audit records rather than globally broadcast domain events.

---

# 118. Remodel Strategy

Because Baobab does not yet carry production data, implementation SHALL favour semantic correctness over preserving accidental pre-production structures.

Existing CP concepts SHALL be reviewed:

```text
Context
Market
DigitalEstate
IsolationProfile
MappingScope
CanonicalEntity
ExternalReference
Mapping
EngineInstance
CapabilityBinding
```

Each SHALL be classified:

```text
KEEP
REMODEL
SPLIT
MERGE
RENAME
REMOVE
```

---

# 119. Migration Principle

Do not create:

```text
ContextV2
MarketV2
ScopeV2
```

merely to avoid correcting existing pre-production models.

Where safe:

```text
remodel the original architecture
```

and remove obsolete semantics.

---

# 120. Implementation Gates

## Gate 0 — Audit Existing Context Model

Inspect:

```text
domain models
migrations
repositories
services
OpenAPI
tests
seed data
ADRs
```

Map current state against this ADR.

---

## Gate 1 — Canonical Shared Contracts

Implement missing context contracts in Shared.

---

## Gate 2 — Tenant/Legal Entity Model

Separate tenant and legal-entity semantics explicitly.

---

## Gate 3 — Digital Estate Model

Formalise:

```text
DigitalEstate
DigitalProperty
EstateLegalEntity
EstateMarket
EstateChannel
```

---

## Gate 4 — Market Model

Implement canonical market and jurisdiction concepts.

---

## Gate 5 — Market Participation

Implement explicit footprint and participation model.

---

## Gate 6 — Transaction Geography

Implement canonical operation-scope geography.

---

## Gate 7 — Currency Context

Separate market/default/transaction/settlement currency semantics.

---

## Gate 8 — Technical Context

Integrate:

```text
deployment region
residency
environment
isolation
```

---

## Gate 9 — Context Resolver

Implement deterministic validation and resolution.

---

## Gate 10 — Persistence

Remodel PostgreSQL schema and invariants.

---

## Gate 11 — APIs

Implement context resolution API.

---

## Gate 12 — Cache and Invalidation

Implement bounded context caching and event invalidation.

---

## Gate 13 — Audit and Explainability

Implement context snapshots, diagnostics and provenance.

---

## Gate 14 — Capability Integration

Connect resolved PlatformContext to ADR-BCP-003 capability resolution.

---

## Gate 15 — Reference Fixtures

Implement all six mandatory reference scenarios.

---

## Gate 16 — Security Hardening

Test:

```text
cross-tenant access
entity spoofing
estate spoofing
market spoofing
IDOR
stale context
revoked principal
revoked market participation
residency mismatch
isolation mismatch
```

---

## Gate 17 — Documentation

Update:

```text
OpenAPI
architecture diagrams
data model
runbooks
onboarding documentation
developer documentation
```

---

# 121. Definition of Done

ADR-BCP-004 SHALL be considered implemented when:

1. Tenant and LegalEntity are explicitly distinct.
2. DigitalEstate is explicitly distinct from Tenant.
3. Market is distinct from Country.
4. Jurisdiction is explicit.
5. Channel is contextual rather than tenant classification.
6. MarketParticipation is first-class.
7. Legal footprint is distinct from commercial footprint.
8. Operating footprint is distinct from transaction geography.
9. Transit does not imply legal presence.
10. Destination does not imply legal presence.
11. Currency does not imply market.
12. Deployment region does not imply jurisdiction.
13. Transaction geography supports origin, destination and transit.
14. Domestic and cross-border operations use the same platform context model.
15. B2B and B2C use the same platform context model.
16. Context resolution is fail-closed.
17. Context is immutable after resolution.
18. Context has provenance.
19. Tenant relationships are validated.
20. Estate/entity relationships are validated.
21. Market participation is temporally valid.
22. Residency participates in context.
23. Isolation participates in context.
24. Context feeds deterministic capability resolution.
25. ZuriBeans domestic and cross-border fixtures pass.
26. Thamani domestic and cross-border fixtures pass.
27. Coal-haulage fixture passes.
28. Petroleum-trading fixture passes.
29. No tenant-specific resolver branches are required.
30. Shared remains canonical contract authority.

---

# 122. Consequences

## Positive

This architecture gives Baobab:

- genuine multi-market capability;
- genuine multi-region capability;
- correct legal-entity separation;
- support for domestic and cross-border transactions;
- support for B2B and B2C simultaneously;
- support for businesses without legal footprint in every transaction country;
- safer tenant isolation;
- more accurate provider routing;
- cleaner ERP integration;
- cleaner Trade integration;
- cleaner Pulse intelligence;
- clearer data residency;
- better SaaS onboarding;
- better auditability.

## Costs

It introduces additional explicit modelling.

That complexity is accepted because the underlying business reality is complex.

The alternative is not simplicity.

The alternative is hidden complexity encoded as incorrect assumptions.

---

# 123. Rejected Alternatives

## One market per tenant

Rejected.

Real businesses operate across multiple markets.

## One legal entity per Digital Estate

Rejected as a universal constraint.

It may be configuration, not architecture.

## Market equals country

Rejected as universal semantics.

## Country equals jurisdiction

Rejected.

## Destination determines legal entity

Rejected.

## Separate domestic and cross-border platforms

Rejected.

## Separate B2B and B2C control planes

Rejected.

## Digital Estate determines authoritative context

Rejected.

The Estate requests context; CP validates it.

## IAM token contains all business context

Rejected.

Tokens would become stale, oversized and semantically overloaded.

---

# 124. Final Target Architecture

```text
                     DIGITAL ESTATE
                           │
                           ▼
                  AUTHENTICATED REQUEST
                           │
                           ▼
                    BAOBAB IAM
                           │
                           ▼
                    PRINCIPAL CONTEXT
                           │
                           ▼
                 BAOBAB CONTROL PLANE
                           │
                           ▼
                    CONTEXT RESOLVER
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
      Tenant          Legal Entity       Digital Estate
        │                  │                  │
        └──────────────┬───┴──────────────────┘
                       ▼
                    Channel
                       │
                       ▼
                     Market
                       │
                       ▼
              Market Participation
                       │
          ┌────────────┼─────────────┐
          ▼            ▼             ▼
        Legal       Operating     Commercial
       Footprint     Footprint     Footprint
                       │
                       ▼
              Transaction Geography
              │        │         │
              ▼        ▼         ▼
            Origin   Transit  Destination
                       │
                       ▼
                    Currency
                       │
                       ▼
               Residency / Region
                       │
                       ▼
                  Isolation
                       │
                       ▼
               PLATFORM CONTEXT
                       │
                       ▼
              CAPABILITY RESOLVER
                       │
           ┌───────────┼───────────┐
           ▼           ▼           ▼
         Grant       Binding     Provider
                       │
                       ▼
                 Engine Instance
                       │
                       ▼
                 DOMAIN ENGINE
```

---

# 125. Architectural Rule of Thumb

When modelling a new contextual field, architects SHALL ask:

> Is this describing **who the organisation is**, **where it legally exists**, **where it commercially participates**, **where this particular transaction occurs**, or **where Baobab technically executes the capability**?

If the answer belongs to different categories, the fields SHALL remain different.

---

# 126. Decision

**ACCEPTED TARGET CONTEXT ARCHITECTURE, subject to formal approval.**

Upon approval:

1. the existing CP context model SHALL be audited against this ADR;
2. tenant/legal-entity conflation SHALL be removed;
3. Digital Estate SHALL become a first-class consumption context;
4. MarketParticipation SHALL become a first-class runtime concept;
5. organisational footprint and transaction geography SHALL be separated;
6. channel SHALL be contextual;
7. domestic/cross-border SHALL be derived contextual characteristics rather than platform silos;
8. residency, deployment and isolation SHALL remain distinct from commercial geography;
9. Shared SHALL define the corresponding canonical contracts;
10. ADR-BCP-003 capability resolution SHALL consume the resulting immutable PlatformContext.

---

# 127. Architectural Maxim

> **Baobab shall model the business as it actually exists: who is acting, for which legal entity, through which Digital Estate, in which commercial context, across which geography, under which technical and isolation constraints — without inventing relationships merely because two dimensions happen to coincide.**

And, most importantly:

> **Legal footprint, operating footprint, commercial footprint and transaction footprint are different facts. Baobab shall never pretend otherwise.**