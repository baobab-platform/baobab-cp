# ADR-BCP-011 — Market Participation, Trade Lanes and Cross-Market Trading Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-12  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Repository:** `nabhold/baobab-cp`  
**Runtime Authority:** `nabhold/baobab-cp`  
**Contract Authority:** `nabhold/shared`  
**Identity Authority:** `nabhold/baobab-iam`  
**Primary Domain Providers:** `nabhold/baobab-trade`, `nabhold/baobab-erp`  
**Reference Tenant:** ZuriBeans  

**Depends On:**

- ADR-BCP-001 — Baobab Control Plane — Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
- ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
- ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model
- ADR-BCP-006 — Capability Provider Lifecycle, Engine Topology, Health, Failover and Migration Model
- ADR-BCP-007 — Control Plane APIs, Capability Resolution Contracts, Caching, Resolution Assertions and Service-to-Service Consumption Model
- ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model
- ADR-BCP-009 — Capability-Centric Security, Isolation, Residency, Revocation and Failure Semantics
- ADR-BCP-010 — Modular Control Plane Architecture, Governance Boundaries and Evolution Model
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model
- Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification

**Applies To:** Markets, market participation, operating footprints, sourcing, procurement, selling, importing, exporting, warehousing, distribution, fulfilment, trade lanes, origin/destination relationships, cross-market trading, internal trading, external trading, intercompany routing, capability resolution, provisioning and readiness.

**Architecture Style:** Explicit-context, capability-driven, multi-market, multi-jurisdiction, bidirectional trade, N-to-N trade-lane architecture, provider-neutral, fail-closed.

**Decision Type:** Foundational multi-market and cross-border trading architecture.

---

# 1. Executive Decision

Baobab SHALL model a tenant's participation in a market as an explicit, effective-dated set of authorised **Market Participation Capabilities**.

A market SHALL NOT be assigned a permanent architectural role such as:

```text
SOURCE MARKET
IMPORT MARKET
EXPORT MARKET
SALES MARKET
```

Instead, the same market MAY independently participate in one or more capabilities including:

```text
SOURCING
PROCUREMENT
SELLING
IMPORTING
EXPORTING
WAREHOUSING
DISTRIBUTION
FULFILMENT
```

subject to:

- the tenant;
- legal entity;
- legal footprint;
- operating authority;
- product class;
- regulatory eligibility;
- applicable licences;
- capability grants;
- effective dates;
- provider readiness.

Baobab SHALL additionally introduce **Trade Lane** as a first-class canonical platform concept representing an authorised origin-to-destination trading relationship.

Trade lanes SHALL support:

```text
Market A → Market B
Market B → Market A
Market A → Market C
Market B → Market C
...
Market N → Market M
```

without requiring:

- both markets to have the same capabilities;
- the tenant to have legal presence in both markets;
- the same legal entity to act at both ends;
- the same currency;
- the same provider topology;
- the same product assortment;
- reciprocal trade-lane enablement.

The platform SHALL distinguish at least:

```text
LOCAL TRADE

EXTERNAL CROSS-BORDER TRADE

INTERNAL CROSS-MARKET TRADE
```

and SHALL NOT infer the legal or accounting treatment of internal cross-market trade merely from common tenant ownership.

The principal rule is:

> **Markets describe where business may occur; Market Participation describes what the tenant or legal entity is authorised to do there; Trade Lanes describe permitted directional relationships between markets; domain engines execute the resulting transactions.**

---

# 2. Context

ADR-BCP-004 deliberately separates:

```text
Tenant
Legal Entity
Organisation
Digital Estate
Market
Country
Jurisdiction
Operating Footprint
Commercial Footprint
Transaction Geography
Deployment Region
```

That separation is necessary but not sufficient for a trading enterprise.

The platform must also represent:

> **What may this tenant or legal entity actually do in this market?**

Consider ZuriBeans.

A simplistic implementation might define:

```text
Uganda
    = sourcing
    = export

South Africa
    = import
    = selling
```

That may describe one initial commercial flow:

```text
Ugandan Coffee Supplier
          ↓
   ZuriBeans Uganda
          ↓
       Export
          ↓
   ZuriBeans South Africa
          ↓
        Sale
```

but it fails as soon as the operating model expands.

ZuriBeans Uganda may also:

```text
source locally
procure locally
hold inventory
sell locally
import products
distribute imported products
export to South Africa
export directly to third markets
```

ZuriBeans South Africa may independently:

```text
source wine
procure wine
hold inventory
sell wine locally
import Ugandan coffee
export wine to Uganda
export wine to third countries
distribute locally
```

Therefore:

```text
Market
≠
Business Role
```

and:

```text
Country
≠
Sourcing Role
≠
Selling Role
≠
Import Role
≠
Export Role
```

The role is contextual and capability-based.

---

# 3. Business Requirement

Baobab must support a business operating model such as:

```text
                              ZURIBEANS
                                  │
             ┌────────────────────┼────────────────────┐
             │                    │                    │
             ▼                    ▼                    ▼
           UGANDA           SOUTH AFRICA          MARKET N
             │                    │                    │
       ┌─────┼─────┐        ┌─────┼─────┐        ┌─────┼─────┐
       │     │     │        │     │     │        │     │     │
      BUY   SELL  STOCK     BUY   SELL  STOCK     BUY   SELL STOCK
       │     │     │        │     │     │        │     │     │
       └─────┼─────┘        └─────┼─────┘        └─────┼─────┘
             │                    │                    │
             └────────────────────┼────────────────────┘
                                  │
                                  ▼
                         CROSS-MARKET TRADE
```

The architecture must not require code changes simply because ZuriBeans:

- enters Kenya;
- begins selling coffee locally in Uganda;
- begins importing wine into Uganda;
- sources wine in South Africa;
- exports directly from Uganda to UAE;
- exports South African wine to Uganda;
- starts sourcing the same commodity in several countries;
- adds another warehouse;
- changes its fulfilment model.

These changes should principally be:

```text
configuration
+
capability grants
+
market participation
+
trade-lane policy
+
provider bindings
+
domain master data
```

rather than architectural rewrites.

---

# 4. Architectural Invariants

The following SHALL be treated as independent concepts:

```text
Market
        ≠
Country
        ≠
Jurisdiction
        ≠
Legal Presence
        ≠
Market Participation
        ≠
Trade Lane
        ≠
Transaction Geography
        ≠
Inventory Location
        ≠
Inventory Owner
        ≠
Customer Market
        ≠
Supplier Market
        ≠
Deployment Region
```

They MAY align in a simple case.

Baobab SHALL NOT assume that they do.

For example:

```text
ZuriBeans Uganda exports coffee to Kenya
```

does not imply:

```text
ZuriBeans has a Kenyan legal entity
```

Likewise:

```text
ZuriBeans South Africa sources wine locally
```

does not imply:

```text
South Africa is exclusively a sourcing market
```

Likewise:

```text
goods transit Kenya
```

does not imply:

```text
Kenya is the commercial destination market
```

---

# 5. Decision: Market Participation

A canonical `MarketParticipation` SHALL represent an authorised relationship between:

```text
Tenant
+
Legal Entity where applicable
+
Market
+
Participation Capability
+
Effective Period
+
Governance State
```

Conceptually:

```text
MarketParticipation
├── id
├── tenant_id
├── legal_entity_id?
├── market_id
├── status
├── valid_from
├── valid_to?
├── source
├── policy_version
└── participation_capabilities[]
```

The resource SHALL be effective-dated.

Historical market-participation records SHALL NOT be overwritten in a manner that destroys historical resolution.

---

# 6. Market Participation Capability Taxonomy

The initial canonical capability taxonomy SHALL include at least:

| Capability | Meaning |
|---|---|
| `LEGAL_PRESENCE` | Tenant/legal entity has recognised legal presence applicable to the market |
| `SOURCING` | May identify and engage supply sources |
| `PROCUREMENT` | May execute purchasing/procurement |
| `SELLING` | May execute commercial sales |
| `IMPORTING` | May act in an authorised import role |
| `EXPORTING` | May act in an authorised export role |
| `WAREHOUSING` | May maintain inventory/storage footprint |
| `DISTRIBUTION` | May perform or arrange local distribution |
| `FULFILMENT` | May fulfil customer orders |
| `PROCESSING` | May perform authorised processing/production where applicable |
| `TRANSIT` | May participate as a transit geography without implying commercial presence |

Additional capabilities MAY be introduced through controlled registry evolution.

They SHALL NOT be introduced as arbitrary ungoverned strings.

---

# 7. Participation Capability Semantics

A participation capability SHALL answer:

> **Is this organisational context authorised to perform this class of activity in this market?**

It SHALL NOT itself perform:

- procurement;
- order creation;
- inventory reservation;
- customs calculation;
- tax calculation;
- shipment execution;
- accounting.

Those remain business-domain responsibilities.

For example:

```text
CP:
ZuriBeans / Uganda / EXPORTING = authorised

Trade:
create export commercial workflow

ERP:
post financial consequences

Customs provider:
prepare or submit customs declaration
```

---

# 8. Example — ZuriBeans Initial Market Participation

The intended configuration is conceptually:

```yaml
tenant: zuribeans

market_participations:

  - market: UG
    capabilities:
      - LEGAL_PRESENCE
      - SOURCING
      - PROCUREMENT
      - SELLING
      - IMPORTING
      - EXPORTING
      - WAREHOUSING
      - DISTRIBUTION
      - FULFILMENT

  - market: ZA
    capabilities:
      - LEGAL_PRESENCE
      - SOURCING
      - PROCUREMENT
      - SELLING
      - IMPORTING
      - EXPORTING
      - WAREHOUSING
      - DISTRIBUTION
      - FULFILMENT
```

This configuration says nothing about:

- what products may be sold;
- which products may be imported;
- applicable tax;
- applicable customs requirements;
- which warehouses exist;
- which counterparties may trade;
- which providers execute operations.

Those are resolved elsewhere.

---

# 9. Product Eligibility Is Separate

A market participation grant SHALL NOT mean every product may be sourced, imported, exported or sold.

The full resolution is closer to:

```text
Market Participation
        +
Product Regulatory Classification
        +
Market Assortment
        +
Trade Lane
        +
Legal Entity
        +
Capability Grant
        +
Provider Readiness
        =
Operation Eligibility
```

Example:

```text
ZuriBeans Uganda
SELLING = true
```

does not mean:

```text
every wine SKU may legally be sold in Uganda
```

Product eligibility remains separately governed.

---

# 10. Decision: Trade Lane

Baobab SHALL introduce `TradeLane` as a canonical, directional relationship describing a permitted trading path between an origin and destination market.

Conceptually:

```text
TradeLane
├── id
├── code
├── tenant_id?
├── origin_market_id
├── destination_market_id
├── status
├── effective_from
├── effective_to?
├── allowed_transaction_classes
├── product_scope?
├── legal_entity_scope?
├── allowed_transport_modes?
├── allowed_incoterms?
├── settlement_currency_scope?
├── policy_version
└── metadata
```

A trade lane is directional.

Therefore:

```text
UG → ZA
```

and:

```text
ZA → UG
```

are two distinct trade lanes.

Enabling one SHALL NOT automatically enable the other.

---

# 11. Trade Lane Directionality

The platform SHALL implement:

```text
Origin Market
       │
       ▼
   Trade Lane
       │
       ▼
Destination Market
```

For example:

```text
UG ───────────► ZA
coffee
```

is distinct from:

```text
ZA ───────────► UG
wine
```

because they may differ in:

- regulations;
- product eligibility;
- customs;
- tariffs;
- permits;
- tax;
- logistics;
- providers;
- currency;
- documentation;
- insurance;
- commercial terms.

---

# 12. N-to-N Trade-Lane Model

Baobab SHALL support N-to-N trade relationships.

Conceptually:

```text
        UG
       /  \
      ▼    ▼
     ZA    KE
     │ \   ▲
     │  \  │
     ▼   ▼ │
     UG   AE
```

There SHALL be no architectural requirement that:

```text
N markets = N² active trade lanes
```

Only required lanes are provisioned.

This prevents unnecessary configuration explosion.

---

# 13. Trade Lane Does Not Equal Legal Footprint

A tenant MAY possess a trade lane into a market without possessing legal presence there.

Example:

```text
ZuriBeans Uganda
      │
      ▼
Uganda → Kenya trade lane
      │
      ▼
Kenyan independent customer
```

This does not create:

```text
ZuriBeans Kenya
```

or:

```text
Kenya LEGAL_PRESENCE
```

The destination market may therefore represent:

```text
commercial destination
```

without:

```text
organisational footprint
```

This distinction SHALL remain explicit.

---

# 14. Trade Lane Does Not Equal Logistics Route

The following SHALL remain independent:

```text
Trade Lane
≠
Transport Route
```

A commercial trade lane may be:

```text
UG → ZA
```

while the physical transport route may be:

```text
Uganda
  ↓
Kenya
  ↓
Tanzania
  ↓
Mozambique
  ↓
South Africa
```

or some other route.

Transit countries SHALL be modelled as transaction/logistics geography.

They SHALL NOT automatically become commercial markets or market participations.

---

# 15. Trade Lane Does Not Equal Supply Chain

A trade lane describes directional eligibility and context.

It SHALL NOT become a universal supply-chain object containing:

- every shipment;
- inventory;
- every supplier;
- every customer;
- invoices;
- customs declarations.

Domain records reference the trade lane where appropriate.

Example:

```text
TradeLane: UG-ZA

Shipment 123 ──────┐
Sales Order 456 ───┼──► trade_lane_id = UG-ZA
Customs Case 789 ──┘
```

The trade lane remains stable platform metadata.

---

# 16. Transaction Classification

Every material trading transaction SHALL resolve to one of the following initial classes:

```text
LOCAL_TRADE

EXTERNAL_CROSS_BORDER

INTERNAL_CROSS_MARKET
```

Additional transaction classes MAY later be introduced.

---

# 17. Local Trade

`LOCAL_TRADE` applies where commercial origin and destination are within the same relevant market context.

Example:

```text
Ugandan Supplier
      ↓
ZuriBeans Uganda
      ↓
Ugandan B2B Buyer
```

Typical characteristics:

```text
domestic procurement
domestic sales
domestic tax
domestic logistics
no international import/export customs leg
```

This does not mean the product itself was originally produced domestically.

Transaction classification concerns the transaction being executed.

---

# 18. External Cross-Border Trade

`EXTERNAL_CROSS_BORDER` applies where a ZuriBeans trading context transacts across markets with an independent external counterparty.

Example:

```text
ZuriBeans Uganda
       ↓
Kenyan Customer
```

or:

```text
South African Supplier
       ↓
ZuriBeans Uganda
```

This class normally introduces:

```text
origin
destination
trade lane
customs
trade documents
cross-border logistics
potential FX
import/export regulation
```

---

# 19. Internal Cross-Market Trade

`INTERNAL_CROSS_MARKET` applies where goods, services or value move between two operating contexts belonging to the same tenant or corporate group relationship.

Example:

```text
ZuriBeans Uganda
       ↓
ZuriBeans South Africa
```

This classification SHALL NOT determine whether the movement is:

```text
stock transfer
inter-branch movement
intercompany sale
```

That determination depends upon:

```text
Legal Entity A
+
Legal Entity B
+
Relationship
+
Jurisdiction
+
Accounting Policy
```

---

# 20. Internal Trade Legal Relationship Resolver

Internal cross-market operations SHALL resolve legal relationship before execution.

Conceptually:

```text
Cross-Market Requirement
          │
          ▼
Resolve Origin Legal Entity
          │
          ▼
Resolve Destination Legal Entity
          │
          ▼
Compare Legal Identity
          │
      ┌───┴────┐
      │        │
     SAME   DIFFERENT
      │        │
      ▼        ▼
 Internal    Intercompany
 Movement    Commercial Flow
```

CP SHALL provide the canonical identities and relationship context.

ERP SHALL determine and execute the accounting treatment according to approved policy.

Trade SHALL execute the appropriate commercial workflow.

---

# 21. Internal Trade Must Not Be Special-Cased for ZuriBeans

No implementation SHALL contain business logic equivalent to:

```go
if tenant == "zuribeans" &&
   origin == "UG" &&
   destination == "ZA" {
    ...
}
```

Nor:

```typescript
if (origin === "UG") {
  exporter = true
}
```

Nor:

```java
if (destination.equals("ZA")) {
    createIntercompanyInvoice();
}
```

Behaviour SHALL derive from canonical context and domain policy.

---

# 22. Market Participation Resolution

The runtime decision flow SHALL be:

```text
Incoming Operation
       │
       ▼
Resolve Tenant
       │
       ▼
Resolve Legal Entity
       │
       ▼
Resolve Market
       │
       ▼
Resolve Requested Participation Capability
       │
       ▼
Participation Exists?
       │
   ┌───┴────┐
   │        │
  NO       YES
   │        │
   ▼        ▼
 DENY    Continue
```

Resolution SHALL be fail-closed.

Absence of participation SHALL mean:

```text
NOT AUTHORISED
```

not:

```text
ASSUME ALLOWED
```

---

# 23. Trade-Lane Resolution

For cross-market activity:

```text
Operation
   │
   ▼
Resolve Origin Market
   │
   ▼
Resolve Destination Market
   │
   ▼
Origin ≠ Destination?
   │
   ├── NO → local transaction path
   │
   └── YES
        │
        ▼
Resolve Trade Lane
        │
        ▼
Active?
        │
    ┌───┴────┐
    │        │
   NO       YES
    │        │
    ▼        ▼
 BLOCK    Validate
          Product /
          Legal /
          Capability Scope
```

---

# 24. Full Eligibility Resolution

Cross-border execution SHOULD conceptually resolve:

```text
Tenant
   +
Principal
   +
Legal Entity
   +
Market Participation
   +
Origin
   +
Destination
   +
Trade Lane
   +
Product Scope
   +
Product Regulatory Eligibility
   +
Capability Grants
   +
Provider Bindings
   +
Provider Health
   +
Isolation Policy
   +
Residency Policy
   =
Execution Eligibility
```

Failure of a mandatory dimension SHALL block execution.

---

# 25. Platform Context Extension

`PlatformContext` SHALL be extended where necessary to represent:

```text
origin_market
destination_market
trade_lane
transaction_class
origin_legal_entity
destination_legal_entity
market_participation_refs
```

Not every operation requires every field.

For example:

```text
content.page.read
```

may require no trade lane.

However:

```text
trade.export.execute
```

normally does.

---

# 26. Context Example — Local Uganda Sale

```yaml
tenant: zuribeans
legal_entity: ZURIBEANS_UG
market: UG
origin_market: UG
destination_market: UG
transaction_class: LOCAL_TRADE
participation:
  - SELLING
  - FULFILMENT
trade_lane: null
```

---

# 27. Context Example — Uganda to South Africa

```yaml
tenant: zuribeans

origin:
  market: UG
  legal_entity: ZURIBEANS_UG

destination:
  market: ZA
  legal_entity: ZURIBEANS_ZA

transaction_class: INTERNAL_CROSS_MARKET

trade_lane: UG-ZA

required_participation:
  origin:
    - EXPORTING
  destination:
    - IMPORTING
```

The eventual financial classification MAY be:

```text
INTERCOMPANY
```

or:

```text
INTER_BRANCH
```

depending on legal structure.

---

# 28. Context Example — Uganda to External Kenyan Customer

```yaml
tenant: zuribeans

origin:
  market: UG
  legal_entity: ZURIBEANS_UG

destination:
  market: KE
  external_counterparty: buyer-ke-001

transaction_class: EXTERNAL_CROSS_BORDER

trade_lane: UG-KE

required_participation:
  origin:
    - EXPORTING
```

No `LEGAL_PRESENCE` in Kenya is implied.

---

# 29. Context Example — South African Wine into Uganda

```yaml
tenant: zuribeans

origin:
  market: ZA
  legal_entity: ZURIBEANS_ZA

destination:
  market: UG
  legal_entity: ZURIBEANS_UG

trade_lane: ZA-UG

transaction_class: INTERNAL_CROSS_MARKET

product:
  regulatory_class: ALCOHOL

required_participation:
  origin:
    - EXPORTING

  destination:
    - IMPORTING
```

Additional wine-specific regulatory policy SHALL be resolved outside this ADR.

---

# 30. Capability Registry Interaction

Market participation SHALL integrate with the existing capability-centric model.

The Control Plane SHALL distinguish:

```text
Platform Capability Grant
```

from:

```text
Market Participation Capability
```

For example:

```text
CapabilityGrant:
trade.shipment.create

MarketParticipation:
ZA / EXPORTING
```

Both may be necessary.

The former answers:

> Is this tenant entitled to use the shipment capability?

The latter answers:

> Is this organisational context authorised to export from South Africa?

These concepts SHALL NOT be collapsed.

---

# 31. Product Subscription Interaction

A product subscription such as:

```text
solution.baobab-xbt
```

may compose capabilities such as:

```text
trade.order
trade.shipment
trade.customs
trade.document
```

but subscription SHALL NOT automatically create legal market authority.

Therefore:

```text
Product Subscription
        ≠
Market Participation
```

The onboarding workflow may provision both from desired state, but they remain independent resources.

---

# 32. Trade-Lane Product Scope

Trade lanes MAY include optional product scope.

Example:

```yaml
trade_lane: UG-ZA

allowed_product_classes:
  - COFFEE
  - VANILLA
```

However, absence of product scope SHALL NOT automatically mean all products are legally tradable.

Final product eligibility remains subject to regulatory and assortment resolution.

---

# 33. Trade-Lane Legal-Entity Scope

A trade lane MAY optionally be restricted to legal entities.

Example:

```yaml
trade_lane: UG-ZA

origin_legal_entities:
  - ZURIBEANS_UG

destination_legal_entities:
  - ZURIBEANS_ZA
```

This is useful where several legal entities participate in the same geographical market.

---

# 34. Trade-Lane Currency Scope

Trade lanes MAY specify supported settlement or commercial currencies.

Example:

```yaml
trade_lane: UG-ZA

supported_currencies:
  - USD
  - ZAR
  - UGX
```

This does not imply that FX conversion occurs in CP.

CP governs allowed configuration.

Pricing/Trade/ERP execute commercial and accounting behaviour.

---

# 35. Trade-Lane Incoterm Scope

Trade lanes MAY specify allowed Incoterms.

Example:

```yaml
allowed_incoterms:
  - FCA
  - FOB
  - CIF
  - CIP
  - DAP
```

Incoterm interpretation, named place, title/risk treatment and transaction execution remain domain concerns.

---

# 36. Trade-Lane Transport Scope

Trade lane policy MAY identify permissible transport modes:

```text
ROAD
AIR
SEA
RAIL
MULTIMODAL
```

However:

```text
Trade Lane
≠
Transport Booking
```

CP SHALL NOT dispatch vehicles, book vessels or plan loads.

---

# 37. Provider Resolution

The trade lane and market context MAY participate in provider resolution.

Example:

```text
trade.customs.submit
       │
       ▼
Context:
tenant = zuribeans
origin = UG
destination = ZA
trade_lane = UG-ZA
       │
       ▼
Capability Resolver
       │
       ▼
Customs Provider X
```

A different lane may resolve:

```text
ZA → UG
```

to another provider.

This is intentional.

---

# 38. Capability Provider Example

```text
                    Capability
              trade.customs.submit
                       │
                       ▼
                  Scope Match
                       │
          ┌────────────┴────────────┐
          │                         │
      UG → ZA                   ZA → UG
          │                         │
          ▼                         ▼
   Provider A                  Provider B
```

This architecture enables provider neutrality.

---

# 39. Market Capability and Provider Readiness

A market participation may be authorised while no provider is currently ready.

Example:

```text
ZA / EXPORTING = authorised

but

trade.customs.submit provider = unavailable
```

Then:

```text
Market Participation = VALID

Capability Readiness = NOT_READY
```

The concepts SHALL remain separate.

---

# 40. Readiness Semantics

A cross-market capability may become `READY` only when all mandatory dependencies are satisfied.

Conceptually:

```text
Market Participation
        +
Trade Lane
        +
Capability Grant
        +
Provider Binding
        +
Provider Health
        +
Required Legal Context
        +
Required Product Eligibility
        =
READY
```

Missing mandatory dependency:

```text
NOT_READY
```

Missing optional dependency:

```text
DEGRADED
```

where policy permits.

---

# 41. Desired-State Provisioning

Tenant onboarding SHALL allow desired-state declaration of market participation and trade lanes.

Conceptually:

```yaml
tenant: zuribeans

market_participations:

  - market: UG
    capabilities:
      - SOURCING
      - PROCUREMENT
      - SELLING
      - EXPORTING
      - IMPORTING
      - WAREHOUSING

  - market: ZA
    capabilities:
      - SOURCING
      - PROCUREMENT
      - SELLING
      - EXPORTING
      - IMPORTING
      - WAREHOUSING

trade_lanes:

  - origin: UG
    destination: ZA

  - origin: ZA
    destination: UG
```

Provisioning SHALL be declarative and idempotent.

---

# 42. Provisioning Lifecycle

Market participation and trade lanes SHALL participate in:

```text
DESIRED STATE
      │
      ▼
VALIDATE
      │
      ▼
PLAN
      │
      ▼
APPLY
      │
      ▼
RECONCILE
      │
      ▼
READINESS
```

Manual production database changes SHALL NOT substitute for this lifecycle.

---

# 43. Validation

Validation SHALL detect at least:

- unknown market;
- unknown legal entity;
- invalid capability name;
- unsupported transaction class;
- origin equal to destination for a cross-border-only lane;
- invalid effective dates;
- overlapping contradictory records;
- illegal self-reference;
- references outside tenant scope;
- inactive legal entity;
- invalid product scope;
- invalid provider scope;
- capability requiring absent legal footprint where policy mandates it.

---

# 44. Drift Reconciliation

CP SHALL reconcile desired and actual platform metadata.

Examples of drift:

```text
desired:
UG / EXPORTING = active

actual:
record absent
```

or:

```text
desired:
UG → ZA = ACTIVE

actual:
UG → ZA = SUSPENDED
```

or:

```text
trade lane expects customs provider binding

actual:
no provider binding
```

Drift SHALL be observable and SHALL influence readiness where material.

---

# 45. Market Participation State Model

Initial lifecycle:

```text
DRAFT
  ↓
PLANNED
  ↓
ACTIVE
  ↓
SUSPENDED
  ↓
RETIRED
```

Alternative transitions:

```text
DRAFT → CANCELLED
ACTIVE → SUSPENDED
SUSPENDED → ACTIVE
ACTIVE → RETIRED
```

Historical records SHALL remain auditable.

---

# 46. Trade Lane State Model

Initial lifecycle:

```text
DRAFT
  ↓
PLANNED
  ↓
ACTIVE
  ↓
SUSPENDED
  ↓
RETIRED
```

A lane may be suspended independently of either market.

For example:

```text
UG market = ACTIVE
ZA market = ACTIVE
UG → ZA lane = SUSPENDED
```

because of:

- regulatory change;
- provider outage;
- policy decision;
- trade restriction.

---

# 47. Effective Dating

Both `MarketParticipation` and `TradeLane` SHALL support:

```text
valid_from
valid_to
```

This is necessary because:

- legal authority changes;
- regulations change;
- operating strategy changes;
- routes open and close;
- product eligibility changes;
- providers change.

Runtime resolution SHALL evaluate the effective state at the relevant decision time.

---

# 48. Historical Reproducibility

A transaction created under an earlier policy SHALL remain explainable later.

The platform SHOULD preserve references such as:

```text
trade_lane_id
trade_lane_version
market_participation_refs
policy_version
resolved_at
```

This allows audit questions such as:

> Why was this transaction permitted on 15 March 2027?

to be answered deterministically.

---

# 49. Security Boundary

No caller SHALL be trusted merely because it supplies:

```text
origin_market=UG
destination_market=ZA
```

The Control Plane SHALL verify:

- tenant;
- principal;
- legal-entity membership/authority;
- permitted market;
- market participation;
- capability grant;
- trade-lane status.

Caller-provided context is input to resolution, not proof of authority.

---

# 50. Cross-Tenant Isolation

Trade lanes SHALL be tenant-scoped or explicitly platform-global where designed.

A ZuriBeans trade lane SHALL NOT become usable by Thamani merely because both use:

```text
UG → ZA
```

Conceptually:

```text
ZuriBeans / UG → ZA
                ≠
Thamani / UG → ZA
```

even if both reference a shared canonical geographical lane template.

---

# 51. Tenant-Specific vs Platform Trade Lanes

Baobab MAY distinguish:

```text
TradeLaneDefinition
```

from:

```text
TenantTradeLane
```

For example:

```text
Platform:
UG → ZA

Tenant configuration:
ZuriBeans / UG → ZA / ACTIVE
Thamani / UG → ZA / NOT_CONFIGURED
```

This separation is recommended where shared geographical metadata becomes useful.

It SHALL NOT weaken tenant isolation.

---

# 52. Shared Contract Requirements

`nabhold/shared` SHALL become contract authority for canonical DTO/event/schema definitions including, at minimum:

```text
MarketParticipation
MarketParticipationCapability
TradeLane
TradeLaneStatus
TransactionClass
CrossMarketContext
```

Contracts SHALL be versioned.

Providers SHALL NOT independently invent incompatible representations for these platform concepts.

---

# 53. Proposed Canonical Types

Conceptually:

```text
MarketParticipationCapability =
    LEGAL_PRESENCE
  | SOURCING
  | PROCUREMENT
  | SELLING
  | IMPORTING
  | EXPORTING
  | WAREHOUSING
  | DISTRIBUTION
  | FULFILMENT
  | PROCESSING
  | TRANSIT
```

and:

```text
TransactionClass =
    LOCAL_TRADE
  | EXTERNAL_CROSS_BORDER
  | INTERNAL_CROSS_MARKET
```

Exact schema formats SHALL be defined in Shared.

---

# 54. API Requirements

The Control Plane SHOULD expose resources conceptually equivalent to:

```text
GET    /v1/market-participations
POST   /v1/market-participations
GET    /v1/market-participations/{id}
PATCH  /v1/market-participations/{id}

GET    /v1/trade-lanes
POST   /v1/trade-lanes
GET    /v1/trade-lanes/{id}
PATCH  /v1/trade-lanes/{id}
```

and runtime resolution endpoints such as:

```text
POST /v1/context/resolve

POST /v1/trade-context/resolve

POST /v1/trade-lanes/resolve

POST /v1/market-participations/check
```

The final API surface SHALL conform to existing CP API conventions.

---

# 55. Example Resolution Request

```json
{
  "tenant": "zuribeans",
  "origin_market": "UG",
  "destination_market": "ZA",
  "origin_legal_entity": "ZURIBEANS_UG",
  "destination_legal_entity": "ZURIBEANS_ZA",
  "requested_capability": "trade.shipment.create",
  "product_class": "COFFEE"
}
```

Conceptual response:

```json
{
  "allowed": true,
  "transaction_class": "INTERNAL_CROSS_MARKET",
  "trade_lane": "UG-ZA",
  "origin_participation": [
    "EXPORTING"
  ],
  "destination_participation": [
    "IMPORTING"
  ],
  "resolution_assertion": "..."
}
```

---

# 56. Resolution Assertion

Where ADR-BCP-007 resolution assertions are used, the assertion SHOULD bind relevant trade context:

```text
tenant
legal_entity
origin_market
destination_market
trade_lane
transaction_class
capability
provider
expiry
policy_version
```

A service SHALL NOT reuse an assertion for a materially different trade lane.

---

# 57. Event Requirements

Material lifecycle changes SHALL emit canonical events.

Examples:

```text
baobab.control.market-participation.created.v1

baobab.control.market-participation.activated.v1

baobab.control.market-participation.suspended.v1

baobab.control.trade-lane.created.v1

baobab.control.trade-lane.activated.v1

baobab.control.trade-lane.suspended.v1

baobab.control.trade-lane.retired.v1
```

Events SHALL follow Baobab canonical event-envelope conventions.

---

# 58. Domain Events Are Separate

CP lifecycle events SHALL NOT replace domain transaction events.

For example:

```text
baobab.control.trade-lane.activated.v1
```

is different from:

```text
baobab.trade.shipment.created.v1
```

The former describes platform configuration.

The latter describes business execution.

---

# 59. ERP Responsibility

`baobab-erp` SHALL remain responsible for:

- purchase-order accounting;
- sales-order accounting;
- inventory valuation;
- AP;
- AR;
- GL;
- intercompany accounting;
- due-to/due-from;
- transfer pricing effects;
- FX accounting;
- financial consolidation;
- accounting reconciliation.

CP SHALL NOT post accounting entries.

---

# 60. Trade Responsibility

`baobab-trade` SHALL remain responsible for relevant commercial/trade orchestration including:

- RFQ;
- quotation;
- order;
- customer-facing commercial workflow;
- shipment orchestration;
- trade documentation orchestration;
- fulfilment;
- trade/compliance provider invocation where assigned;
- customer/supplier-facing status projections.

CP SHALL NOT become an order or shipment engine.

---

# 61. IAM Responsibility

`baobab-iam` SHALL determine authenticated identity.

CP SHALL determine resolved platform authority/context in conjunction with canonical grants and relationships.

A valid IAM identity does not itself imply:

```text
permission to export from Uganda
```

or:

```text
permission to trade on UG → ZA
```

---

# 62. CMS Responsibility

CMS MAY provide:

- market-specific content;
- trade information;
- regulatory guidance;
- document templates;
- product content.

It SHALL NOT become authority for market participation or trade-lane permission.

---

# 63. Pulse Responsibility

Pulse MAY provide intelligence related to:

```text
market demand
commodity prices
FX
trade statistics
regulatory signals
route risk
```

Pulse SHALL NOT determine final transactional authority.

Intelligence can inform a decision.

It does not override canonical policy.

---

# 64. Infrastructure Responsibility

Infrastructure MAY provide regional execution constraints and provider availability.

For example:

```text
customs provider unavailable
```

may affect readiness.

It SHALL NOT redefine market participation.

---

# 65. ZuriBeans Reference Scenario A — Coffee Uganda to South Africa

```text
Ugandan Supplier
      │
      ▼
ZuriBeans Uganda
      │
      │ PROCUREMENT
      ▼
Uganda Inventory
      │
      │ EXPORTING
      ▼
Trade Lane UG → ZA
      │
      ▼
Shipment / Customs
      │
      ▼
South Africa
      │
      │ IMPORTING
      ▼
ZA Inventory
      │
      │ SELLING
      ▼
South African Buyer
```

Required platform context includes:

```text
UG PROCUREMENT
UG EXPORTING
UG → ZA lane
ZA IMPORTING
ZA SELLING
```

---

# 66. ZuriBeans Reference Scenario B — Wine South Africa to Uganda

```text
South African Winery
      │
      ▼
ZuriBeans South Africa
      │
      │ PROCUREMENT
      ▼
ZA Inventory
      │
      │ EXPORTING
      ▼
Trade Lane ZA → UG
      │
      ▼
Shipment / Customs
      │
      ▼
Uganda
      │
      │ IMPORTING
      ▼
UG Inventory
      │
      │ SELLING
      ▼
Ugandan Buyer
```

The reverse lane is independently governed.

---

# 67. ZuriBeans Reference Scenario C — Local Uganda Sale

```text
Ugandan Supplier
       ↓
ZuriBeans Uganda
       ↓
UG Inventory
       ↓
Ugandan Buyer
```

Required participation may include:

```text
PROCUREMENT
WAREHOUSING
SELLING
FULFILMENT
```

No international trade lane is required.

---

# 68. ZuriBeans Reference Scenario D — Uganda to Kenya Without Legal Presence

```text
ZuriBeans Uganda
       ↓
UG → KE
       ↓
Kenyan Customer
```

Expected facts:

```text
UG EXPORTING = true

UG → KE trade lane = active

Kenya LEGAL_PRESENCE = false or absent

External cross-border transaction = permitted
```

provided all other required policies are satisfied.

---

# 69. ZuriBeans Reference Scenario E — Cross-Market Internal Movement

```text
ZuriBeans Uganda
       ↓
ZuriBeans South Africa
```

CP determines:

```text
tenant relationship
origin context
destination context
trade lane
legal entities
transaction class
```

ERP/Trade determine:

```text
stock transfer
or
intercompany sale/purchase
```

according to the legal relationship.

---

# 70. Failure Semantics

Fail-closed behaviour SHALL apply.

Examples:

| Condition | Result |
|---|---|
| Origin participation absent | DENY |
| Destination required participation absent | DENY |
| Trade lane absent | DENY cross-market execution |
| Lane suspended | DENY |
| Legal entity inactive | DENY |
| Mandatory provider unresolved | NOT_READY / DENY execution |
| Product outside permitted scope | DENY |
| Capability grant absent | DENY |
| Context ambiguous | DENY |
| Policy evaluation unavailable where mandatory | DENY |

---

# 71. No Implicit Reciprocal Authority

The following SHALL be prohibited:

```text
UG → ZA exists
therefore
ZA → UG exists
```

Likewise:

```text
UG EXPORTING
therefore
UG IMPORTING
```

and:

```text
ZA SELLING
therefore
ZA PROCUREMENT
```

Every capability is explicit.

---

# 72. No Capability Inference From Historical Behaviour

The fact that:

```text
ZuriBeans previously exported coffee from Uganda
```

SHALL NOT be accepted as evidence that:

```text
UG EXPORTING = currently authorised
```

Runtime decisions rely on current effective policy, not historical transactions.

---

# 73. No Market Inference From Currency

The following SHALL remain invalid:

```text
transaction currency = ZAR
therefore
market = ZA
```

or:

```text
currency = USD
therefore
market = US
```

Currency and market remain independent context dimensions.

---

# 74. No Market Inference From Warehouse

The following SHALL also remain invalid:

```text
warehouse country = South Africa
therefore
customer market = South Africa
```

Inventory location and commercial destination are different facts.

---

# 75. Audit

Every material Market Participation or Trade Lane mutation SHALL record:

```text
actor
tenant
resource
before state
after state
reason
approval reference where applicable
timestamp
correlation ID
policy version
```

Changes affecting active production trade SHOULD require appropriate governance approval.

---

# 76. Observability

Recommended metrics include:

```text
cp_market_participation_total

cp_market_participation_active_total

cp_trade_lane_total

cp_trade_lane_active_total

cp_trade_lane_resolution_total

cp_trade_lane_resolution_denied_total

cp_market_participation_resolution_denied_total

cp_trade_context_resolution_latency

cp_trade_lane_readiness_failures_total
```

Dimensions SHOULD avoid high-cardinality misuse.

---

# 77. Reconciliation

CP reconciliation SHALL detect at least:

- desired participation absent;
- unwanted participation present;
- trade lane missing;
- suspended lane unexpectedly active;
- missing provider binding;
- invalid product scope reference;
- inactive legal entity reference;
- orphaned market reference;
- expired participation still considered active;
- capability resolution inconsistency.

---

# 78. Readiness

ZuriBeans readiness SHALL consider mandatory market participation and lanes.

For initial production, readiness may require at minimum:

```text
Uganda:
  PROCUREMENT
  SELLING
  EXPORTING
  IMPORTING
  WAREHOUSING

South Africa:
  PROCUREMENT
  SELLING
  EXPORTING
  IMPORTING
  WAREHOUSING

Trade lanes:
  UG → ZA
  ZA → UG
```

Exact mandatory capabilities SHALL be specified in the ZuriBeans desired-state manifest.

---

# 79. Testing Strategy

Tests SHALL cover:

## Unit

- state transitions;
- effective-date evaluation;
- capability validation;
- lane directionality;
- transaction classification;
- fail-closed behaviour.

## Repository

- uniqueness constraints;
- overlap constraints;
- tenant isolation;
- foreign-key validity;
- temporal validity.

## Integration

- CP context resolution;
- capability resolution;
- provider resolution;
- IAM principal context;
- Shared contracts;
- Trade consumption;
- ERP consumption.

## End-to-End

- UG local sale;
- ZA local sale;
- UG → ZA;
- ZA → UG;
- UG → external market;
- absent lane denial;
- suspended lane denial;
- expired participation denial.

---

# 80. Adversarial Tests

Explicitly attempt:

```text
Thamani using ZuriBeans trade lane

ZuriBeans user changing tenant header

ZA principal claiming UG EXPORTING

buyer principal invoking procurement authority

UG → ZA assertion reused for ZA → UG

inactive legal entity attempting cross-market trade

expired trade lane replay

manually supplied fake lane ID

cross-tenant market participation reference
```

All SHALL fail.

---

# 81. Database Considerations

The exact relational schema remains implementation-specific, but the model SHOULD support entities equivalent to:

```text
markets

market_participations

market_participation_capabilities

trade_lanes

trade_lane_product_scopes

trade_lane_legal_entity_scopes

trade_lane_currency_scopes

trade_lane_incoterm_scopes
```

Temporal validity and tenant isolation SHALL be enforced at the persistence layer where practical.

---

# 82. Example Relationship Model

```text
Tenant
  │
  ├───────────────┐
  │               │
  ▼               ▼
LegalEntity     Market
  │               │
  └──────┬────────┘
         ▼
 MarketParticipation
         │
         ▼
 ParticipationCapability


Market ───────┐
              │ origin
              ▼
           TradeLane
              ▲
              │ destination
Market ───────┘
```

---

# 83. Domain Relationship Model

```text
                 MarketParticipation
                         │
                         ▼
Tenant ──► PlatformContext ◄── TradeLane
                         │
                         ▼
                  Capability Resolver
                         │
                         ▼
                  Capability Provider
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
        Trade           ERP          Customs/
                                    Logistics
```

---

# 84. Rejected Alternative — Fixed Market Roles

Rejected:

```text
Uganda = source
South Africa = destination
```

Reason:

- immediately fails bidirectional trading;
- embeds one tenant's present strategy in platform architecture;
- cannot scale to future markets;
- conflates geography with business activity.

---

# 85. Rejected Alternative — Infer Capabilities From Transactions

Rejected:

```text
if sales exist in ZA
then ZA is selling market
```

Reason:

- historical state cannot define current authority;
- produces non-deterministic governance;
- makes suspension difficult;
- weakens auditability.

---

# 86. Rejected Alternative — One Global Trading Flag

Rejected:

```text
tenant.cross_border_enabled = true
```

Reason:

too coarse to express:

```text
which market?
which direction?
which product?
which legal entity?
which capability?
which provider?
which effective period?
```

---

# 87. Rejected Alternative — Treat Trade Lane as Shipment

Rejected because:

- lanes are stable configuration;
- shipments are transactions;
- one lane supports many shipments;
- shipment route may differ physically from commercial lane;
- provider choice may vary per shipment.

---

# 88. Rejected Alternative — Encode Everything in Medusa

Rejected.

Medusa may execute important Trade capabilities, but platform-level context cannot be owned by a single provider implementation.

Otherwise:

```text
replace Medusa
=
replace Baobab market model
```

which violates provider neutrality.

---

# 89. Rejected Alternative — Encode Everything in iDempiere

Rejected for the same reason.

ERP remains accounting/enterprise transaction authority, not universal Baobab platform-context authority.

---

# 90. Consequences — Positive

This decision enables:

- true bidirectional trade;
- independent market roles;
- N-market expansion;
- N-to-N trade lanes;
- local and cross-border operations;
- external-market sales without forced legal presence;
- provider-specific resolution by corridor;
- deterministic readiness;
- explicit suspension;
- future trade-lane profitability analytics;
- product-specific regulatory evolution;
- intercompany architecture;
- reusable Baobab XBT capabilities.

---

# 91. Consequences — Costs

This architecture introduces additional complexity:

- more canonical resources;
- more context dimensions;
- more policy evaluation;
- additional provisioning;
- additional reconciliation;
- explicit temporal modelling;
- more comprehensive testing.

This complexity is accepted because the alternative moves the complexity into implicit assumptions distributed across Trade, ERP, frontends and operational code.

Explicit complexity in platform governance is preferable to hidden complexity in business execution.

---

# 92. Implementation Boundaries

This ADR mandates:

```text
WHAT must be modelled
WHAT relationships must remain distinct
HOW resolution must behave semantically
WHERE authority belongs
```

It does not fully define:

- intercompany accounting;
- transfer pricing;
- customs calculation;
- tax calculation;
- inventory-title mechanics;
- logistics execution;
- Incoterm accounting;
- product regulatory rules.

Those shall be addressed by subsequent ADRs.

---

# 93. Required Follow-On ADRs

This ADR directly establishes the foundation for:

1. **ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model**
2. **ADR-BCP-013 — Canonical Inventory Ownership, Custody, Location and In-Transit Model**
3. **ADR-BCP-014 — Procurement Ownership, Supplier Commercial Workflow and Trade-to-ERP Boundary**
4. **ADR-BCP-015 — B2B Pricing, Landed Cost, Margin and Commercial Price Resolution Model**
5. **ADR-BCP-016 — Customs, Trade Compliance and Regulatory Provider Architecture**
6. **ADR-BCP-017 — Shipping, Logistics, Freight and Transport Provider Abstraction**
7. **ADR-BCP-018 — Multi-Jurisdiction Tax Context, Decision and Evidence Model**
8. **ADR-BCP-019 — Canonical Counterparty Identity, Roles and Relationship Model**
9. **ADR-BCP-020 — Product Regulatory Classification, Market Eligibility and Rules Resolution Model**
10. **ADR-BCP-021 — Canonical Trade Document, Evidence, Retention and Provenance Architecture**
11. **ADR-BCP-022 — Incoterms, Risk Transfer, Title Transfer and Commercial Responsibility Model**
12. **ADR-BCP-023 — Trade Finance Capability and External Financial Instrument Architecture**

Additionally, the original fourteen-ADR programme includes this ADR and the Market Participation dimension embedded here; therefore subsequent numbering SHALL continue sequentially without resetting the `ADR-BCP` series.

---

# 94. Implementation Requirements by Repository

| Repository | Required change |
|---|---|
| `nabhold/shared` | Canonical schemas, enums, events, OpenAPI/AsyncAPI contracts |
| `nabhold/baobab-cp` | MarketParticipation, TradeLane, resolution, provisioning, readiness |
| `nabhold/baobab-iam` | Principal/context integration; no trade-domain ownership |
| `nabhold/baobab-trade` | Consume resolved lane/participation context |
| `nabhold/baobab-erp` | Consume legal-entity/cross-market context for accounting |
| `nabhold/zuribeans` | Request/consume platform context; no duplicated lane authority |
| `nabhold/baobab-cms` | Optional market/lane content projection |
| `nabhold/baobab-pulse` | Optional lane intelligence and analytics |
| `nabhold/infrastructure` | Provider/environment readiness integration |

---

# 95. Migration Requirements

Existing assumptions such as:

```text
UG = source/export market

ZA = sell/import market
```

SHALL be identified and removed where encoded as architectural truth.

Migration SHALL inventory:

- database records;
- environment variables;
- seed data;
- enums;
- conditional code;
- tests;
- docs;
- frontend assumptions;
- ERP mappings;
- capability bindings.

Historical data SHALL not be rewritten without an explicit migration rationale.

---

# 96. ZuriBeans Golden Tenant Acceptance Tests

ZuriBeans SHALL prove at least:

```text
UG local procurement
UG local selling

ZA local procurement
ZA local selling

UG → ZA
ZA → UG

UG → external third market
ZA → external third market
```

And prove that:

```text
UG → ZA enabled
does not imply
ZA → UG enabled
```

when tested under a deliberately asymmetric fixture.

---

# 97. Control Plane Acceptance Criteria

ADR-BCP-011 is implemented when:

- [ ] `MarketParticipation` is canonical.
- [ ] participation capabilities are registered and validated.
- [ ] participation is effective-dated.
- [ ] `TradeLane` is canonical.
- [ ] trade lanes are directional.
- [ ] trade lanes are effective-dated.
- [ ] transaction classification is implemented.
- [ ] runtime resolution is fail-closed.
- [ ] capability resolution can consume lane context.
- [ ] provider resolution can consume lane context.
- [ ] desired-state provisioning can declare participations and lanes.
- [ ] reconciliation detects drift.
- [ ] readiness includes mandatory participation/lane state.
- [ ] Shared contracts are published.
- [ ] Trade consumes canonical contracts.
- [ ] ERP consumes relevant canonical contracts.
- [ ] cross-tenant isolation is proven.
- [ ] bidirectional ZuriBeans tests pass.
- [ ] no country-specific branch logic is required.

---

# 98. Go-Live Relationship

For ZuriBeans P13 readiness:

```text
Tenant Ready
      │
      ├── Required Market Participations Ready
      │
      ├── Required Trade Lanes Ready
      │
      ├── Required Capability Grants Ready
      │
      ├── Required Providers Ready
      │
      └── Required Context Resolution Ready
      │
      ▼
Cross-Market Trading Ready
```

A mandatory trade lane in `NOT_READY` state SHALL block ZuriBeans activation where the desired-state manifest declares that lane mandatory for launch.

---

# 99. Governance Rule

Future developers SHALL NOT introduce:

- country-specific business-role assumptions;
- implicit reciprocal lanes;
- implicit import/export rights;
- legal-entity inference from market;
- market inference from currency;
- market inference from warehouse;
- trade-lane inference from physical transport route;
- tenant-crossing lane reuse.

Any proposal requiring such behaviour SHALL first amend or supersede this ADR.

---

# 100. Final Decision

Baobab adopts the following canonical hierarchy:

```text
Tenant
  │
  ├── Legal Entities
  │
  ├── Markets
  │     │
  │     └── Market Participations
  │            │
  │            └── Participation Capabilities
  │
  └── Trade Lanes
         │
         ├── Origin Market
         ├── Destination Market
         ├── Scope
         └── Policy
```

Runtime cross-market execution then becomes:

```text
Identity
   ↓
Tenant
   ↓
Legal Entity
   ↓
Market Participation
   ↓
Trade Lane
   ↓
Product / Regulatory Eligibility
   ↓
Capability Grant
   ↓
Provider Resolution
   ↓
Domain Execution
   ↓
Accounting / Reconciliation
```

The architectural principle is:

> **A market is a place in which business may occur. Market Participation defines what an organisational context is authorised to do there. A Trade Lane defines where authorised trade may move. Neither concept executes the commercial transaction.**

For ZuriBeans this means Uganda and South Africa are not permanently classified as source and destination markets.

They are independently capable operating markets that may:

```text
BUY
SELL
SOURCE
PROCURE
IMPORT
EXPORT
WAREHOUSE
DISTRIBUTE
FULFIL
```

according to explicit authority.

This pattern SHALL generalise to future ZuriBeans markets and future Baobab B2B tenants without introducing tenant-specific or country-specific architecture.

---

# Decision Outcome

**ACCEPTED WHEN APPROVED**

Upon approval, implementation SHALL proceed through:

```text
ADR
 ↓
Shared Canonical Contracts
 ↓
Control Plane Schema
 ↓
Control Plane Domain Model
 ↓
Provisioning
 ↓
Resolution
 ↓
Readiness
 ↓
Trade / ERP Integration
 ↓
Conformance Tests
 ↓
ZuriBeans Golden Tenant Validation
```

No production implementation SHALL substitute implicit country-role logic for this model.