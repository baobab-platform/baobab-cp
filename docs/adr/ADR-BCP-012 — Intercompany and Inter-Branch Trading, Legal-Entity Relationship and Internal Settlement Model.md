# ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-12  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Repository:** `nabhold/baobab-cp`  
**Runtime Authority:** `nabhold/baobab-cp`  
**Contract Authority:** `nabhold/shared`  
**Accounting Authority:** `nabhold/baobab-erp`  
**Commercial Execution Authority:** `nabhold/baobab-trade`  
**Identity Authority:** `nabhold/baobab-iam`  
**Reference Tenant:** ZuriBeans  

**Depends On:**

- ADR-BCP-001 through ADR-BCP-010
- ADR-BCP-011 — Market Participation, Trade Lanes and Cross-Market Trading Model
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model
- Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification

**Applies To:** Legal-entity relationships, internal trade, intercompany sales, inter-branch transfers, mirrored documents, stock ownership, transfer pricing, due-to/due-from balances, FX, settlement, consolidation, elimination, cross-market movements, ERP postings and reconciliation.

**Architecture Style:** Explicit legal-entity context, accounting-aware, cross-market, multi-entity, provider-neutral, fail-closed.

**Decision Type:** Foundational internal-trade and legal-entity architecture.

---

# 1. Executive Decision

Baobab SHALL distinguish between:

```text
INTERNAL CROSS-MARKET MOVEMENT
```

and:

```text
INTERCOMPANY COMMERCIAL TRANSACTION
```

based on the resolved legal entities at the origin and destination of the operation.

The platform SHALL NOT assume that a movement between two ZuriBeans markets is automatically:

```text
a stock transfer
```

or:

```text
an intercompany sale
```

The classification SHALL be derived from:

```text
Origin Legal Entity
+
Destination Legal Entity
+
Legal-Entity Relationship
+
Market Context
+
Transaction Type
+
Applicable Accounting / Tax Policy
```

The primary rule is:

> **Common tenant ownership does not determine legal or accounting treatment. Legal-entity identity and relationship determine whether an internal movement is inter-branch, intercompany, or another governed internal-trade form.**

---

# 2. Why This ADR Is Required

ADR-BCP-011 establishes three transaction classes:

```text
LOCAL_TRADE
EXTERNAL_CROSS_BORDER
INTERNAL_CROSS_MARKET
```

However, `INTERNAL_CROSS_MARKET` is not itself an accounting classification.

Consider:

```text
ZuriBeans Uganda
      ↓
ZuriBeans South Africa
```

This movement could represent:

```text
same legal entity
→ internal branch transfer
```

or:

```text
different legal entities
→ intercompany sale and purchase
```

The consequences are materially different.

Therefore, Baobab requires an explicit legal-entity relationship model.

---

# 3. Architectural Invariant

The following SHALL remain independent:

```text
Tenant
≠
Legal Entity
≠
Branch
≠
Business Unit
≠
Market
≠
Warehouse
≠
Intercompany Relationship
```

A tenant may contain:

```text
one legal entity
```

or:

```text
multiple legal entities
```

A legal entity may operate in:

```text
one market
```

or:

```text
multiple markets
```

A branch may exist in a market without being a separate legal entity.

---

# 4. Legal-Entity Relationship Model

Baobab SHALL maintain explicit relationships between legal entities.

Conceptually:

```text
LegalEntityRelationship
├── id
├── tenant_id
├── source_legal_entity_id
├── target_legal_entity_id
├── relationship_type
├── effective_from
├── effective_to?
├── status
├── policy_reference?
└── metadata
```

Initial relationship types SHALL include at least:

```text
SAME_ENTITY
PARENT_SUBSIDIARY
SISTER_SUBSIDIARY
BRANCH_OF
OPERATING_UNIT_OF
AFFILIATE
JOINT_VENTURE
EXTERNAL
```

Exact registry values SHALL be canonicalised in `shared`.

---

# 5. Internal Trade Classification

When a cross-market transaction is internal to the tenant, the platform SHALL resolve:

```text
Origin Legal Entity
       │
       ▼
Destination Legal Entity
       │
       ▼
Same legal identity?
       │
   ┌───┴───┐
   │       │
  YES      NO
   │       │
   ▼       ▼
Inter-   Intercompany
branch   commercial
movement transaction
```

---

# 6. Inter-Branch Movement

An inter-branch or same-entity movement SHALL generally represent:

```text
inventory movement
```

rather than:

```text
external revenue
```

Typical consequences may include:

- inventory transfer;
- in-transit stock;
- freight allocation;
- customs;
- import tax;
- internal cost allocation;
- warehouse receipt;
- no external AR/AP pair.

The exact accounting treatment remains an ERP policy concern.

---

# 7. Intercompany Transaction

A movement between distinct legal entities SHALL generally require an intercompany commercial relationship.

Typical flow:

```text
Origin Entity
   │
   ├── Sales Order
   ├── Shipment
   ├── Intercompany Invoice
   └── AR
          │
          ▼
Destination Entity
   ├── Purchase Order
   ├── Goods Receipt
   ├── Supplier Invoice
   └── AP
```

The transactions SHALL be linked by canonical internal-trade identity.

---

# 8. Canonical Internal Trade Object

Baobab SHOULD introduce a canonical `InternalTradeTransaction` or equivalent contract.

Conceptually:

```text
InternalTradeTransaction
├── id
├── tenant_id
├── origin_legal_entity_id
├── destination_legal_entity_id
├── origin_market_id
├── destination_market_id
├── trade_lane_id?
├── relationship_type
├── transaction_mode
├── pricing_policy_id?
├── settlement_policy_id?
├── currency?
├── status
└── external_references[]
```

Initial transaction modes:

```text
INTER_BRANCH_TRANSFER
INTERCOMPANY_SALE_PURCHASE
INTERNAL_SERVICE
INTERNAL_COST_ALLOCATION
```

---

# 9. Mirrored Commercial Documents

For `INTERCOMPANY_SALE_PURCHASE`, Baobab SHALL support paired documents.

Conceptually:

```text
Origin Entity
Sales Order
      │
      │ canonical pair
      ▼
Destination Entity
Purchase Order
```

and:

```text
Origin Invoice
      │
      │ canonical pair
      ▼
Destination Supplier Invoice
```

The relationship SHALL be explicit.

Matching by:

```text
amount
date
reference
```

alone is insufficient.

---

# 10. Mirror Document Invariant

Every intercompany pair SHOULD share:

```text
internal_trade_id
correlation_id
origin_document_id
destination_document_id
legal_entity_pair
currency
pricing_basis
```

This supports:

- reconciliation;
- audit;
- elimination;
- exception handling.

---

# 11. Commercial Ownership Boundary

`baobab-trade` SHALL orchestrate the commercial workflow.

It MAY create or project:

```text
Sales Order
Purchase Order relationship
Shipment
Quotation
Contract
```

but it SHALL NOT become the accounting authority.

`baobab-erp` SHALL own financial consequence.

---

# 12. ERP Responsibility

iDempiere SHALL be authoritative for:

```text
AR
AP
GL
Intercompany Accounts
Due To
Due From
FX
Transfer Pricing Accounting
Settlement
Elimination
Consolidation
```

where implemented.

---

# 13. Control Plane Responsibility

CP SHALL own:

- canonical legal-entity identity;
- legal-entity relationship;
- market context;
- trade-lane context;
- capability grants;
- provider resolution;
- readiness;
- policy references.

CP SHALL NOT calculate accounting entries.

---

# 14. Legal-Entity Relationship Resolution

Runtime flow:

```text
Internal Cross-Market Request
          │
          ▼
Resolve Origin Legal Entity
          │
          ▼
Resolve Destination Legal Entity
          │
          ▼
Resolve Relationship
          │
     ┌────┴────┐
     │         │
   SAME     DISTINCT
     │         │
     ▼         ▼
Inter-branch Intercompany
```

If relationship cannot be determined:

```text
BLOCK
```

for mandatory internal-trade execution.

Fail-closed.

---

# 15. No Tenant-Level Shortcuts

This logic is prohibited:

```text
same tenant
=
same legal entity
```

Likewise:

```text
same brand
=
same legal entity
```

and:

```text
same warehouse network
=
same accounting unit
```

---

# 16. Inventory Ownership

Internal trade SHALL explicitly track legal inventory ownership.

For example:

```text
Coffee lot
Location: Kampala
Owner: ZuriBeans Uganda Ltd
```

During movement:

```text
Location: In Transit
Owner: ZuriBeans Uganda Ltd
```

After title transfer:

```text
Location: Johannesburg
Owner: ZuriBeans South Africa Ltd
```

or, for same legal entity:

```text
Owner remains unchanged
```

Inventory ownership is governed further by ADR-BCP-013.

---

# 17. Title Transfer

Intercompany commercial movement SHALL record a title-transfer rule.

Title transfer MAY depend on:

- Incoterm;
- contract;
- shipment milestone;
- customs clearance;
- receipt;
- explicit acceptance.

The platform SHALL NOT assume:

```text
dispatch = ownership transfer
```

or:

```text
delivery = ownership transfer
```

without policy.

---

# 18. Transfer Pricing

Where distinct related legal entities trade, the platform SHALL support transfer pricing.

Capabilities may include:

```text
internal-trade.transfer-price.resolve
internal-trade.transfer-price.calculate
internal-trade.transfer-price.approve
internal-trade.transfer-price.audit
```

The actual policy may depend on:

```text
cost-plus
market price
resale-minus
comparable uncontrolled price
contracted transfer price
```

Baobab SHALL store policy reference and resulting basis.

It SHALL NOT hard-code tax-transfer-pricing law into CP.

---

# 19. Transfer Pricing Ownership

Suggested ownership:

```text
CP
→ policy reference / legal relationship / entitlement

Trade
→ commercial price execution

ERP
→ accounting and financial posting

Pulse
→ optional benchmarking intelligence
```

---

# 20. Intercompany Pricing Flow

```text
Product Cost
    +
Landed Cost
    +
Transfer Pricing Policy
    =
Internal Selling Price
```

Then:

```text
Origin Entity Revenue / AR
Destination Entity Cost / AP
```

subject to policy and jurisdiction.

---

# 21. Due-To / Due-From

Where intercompany settlement is not immediate, ERP SHALL maintain reciprocal balances.

Conceptually:

```text
Entity A
Due From Entity B

Entity B
Due To Entity A
```

These balances SHALL reconcile.

---

# 22. Intercompany Settlement

Settlement modes MAY include:

```text
direct bank settlement
netting
periodic clearing
parent settlement
multilateral netting
```

Release 1 MAY support only direct settlement plus manual netting references.

The architecture SHALL not prevent future netting.

---

# 23. FX Treatment

Intercompany transactions may be denominated in a currency different from either entity's functional currency.

Example:

```text
UG entity books: UGX
ZA entity books: ZAR
Intercompany invoice: USD
```

ERP must support:

```text
invoice FX rate
settlement FX rate
realised gain/loss
unrealised gain/loss
```

---

# 24. Currency Invariant

The following SHALL remain separate:

```text
transaction currency
functional currency
settlement currency
reporting currency
```

No currency SHALL be inferred from market.

---

# 25. Tax Consequences

Intercompany status SHALL NOT imply tax exemption.

The relevant tax engine/policy SHALL still resolve:

```text
seller
buyer
origin
destination
product
transaction class
legal entities
tax registrations
effective date
```

Internal corporate relationship does not erase tax obligations.

---

# 26. Customs Consequences

Likewise:

```text
same corporate group
```

does not mean:

```text
no customs
```

If goods cross customs borders, customs rules still apply.

Therefore:

```text
internal cross-market
+
cross-border geography
=
customs may be required
```

---

# 27. Intercompany Customs Value

Customs valuation may differ from accounting transfer price.

The system SHALL therefore preserve separate concepts:

```text
Commercial Transfer Price
≠
Customs Value
```

where jurisdictional rules require different treatment.

---

# 28. Intercompany Documents

Potential documents include:

```text
Intercompany Purchase Order
Intercompany Sales Order
Commercial Invoice
Packing List
Transfer Pricing Evidence
Customs Declaration
Certificate of Origin
Goods Receipt
Shipment Document
```

These SHALL be linked to the same canonical internal-trade transaction.

---

# 29. Intercompany Shipment

Shipment execution SHALL remain a logistics/trade concern.

Example:

```text
Entity A
   ↓
Shipment
   ↓
Customs
   ↓
Entity B
```

ERP financial documents and logistics documents SHALL reconcile but remain separate domain objects.

---

# 30. Inter-Branch Transfer Flow

Same legal entity example:

```text
Kampala Warehouse
       ↓
Transfer Order
       ↓
Shipment
       ↓
Customs / Import
       ↓
Johannesburg Warehouse
       ↓
Inventory Receipt
```

Accounting may involve:

```text
inventory in transit
freight
duty
tax
landed cost
```

but not necessarily:

```text
sales revenue
AR
AP
```

---

# 31. Intercompany Flow

Different legal entity example:

```text
ZuriBeans Uganda Ltd
       ↓
Intercompany Sales Order
       ↓
Shipment
       ↓
Invoice
       ↓
AR

              ↕ linked

ZuriBeans South Africa Ltd
       ↓
Intercompany Purchase Order
       ↓
Goods Receipt
       ↓
Supplier Invoice
       ↓
AP
```

---

# 32. Canonical Pairing

Document pairing SHALL be deterministic.

Conceptually:

```text
internal_trade_id: ITX-001

seller_sales_order: SO-UG-1001
buyer_purchase_order: PO-ZA-8821

seller_invoice: INV-UG-344
buyer_supplier_invoice: APINV-ZA-991
```

---

# 33. Reconciliation

Intercompany reconciliation SHALL include at least:

```text
Order Pair
Shipment Pair
Quantity
Price
Currency
Invoice Pair
AR/AP Balance
Payment
FX
Tax
Customs
Inventory
GL
```

---

# 34. Reconciliation States

Suggested states:

```text
UNMATCHED
PARTIALLY_MATCHED
MATCHED
MISMATCH
RESOLVED
```

---

# 35. Exception Examples

Detected mismatch:

```text
Origin invoice:
USD 10,000

Destination supplier invoice:
USD 9,800
```

or:

```text
Origin shipment:
1,000 kg

Destination receipt:
980 kg
```

Such discrepancies SHALL not be silently ignored.

---

# 36. Intercompany Elimination

Where consolidated reporting is performed, ERP or reporting layer SHALL support elimination of:

```text
intercompany revenue
intercompany expense
intercompany receivables
intercompany payables
intercompany profit in inventory where required
```

The exact consolidation implementation may be phased.

---

# 37. Consolidation Architecture

Conceptually:

```text
Entity A Ledger
      │
      ├──────────────┐
      │              │
      ▼              ▼
Entity B Ledger   Elimination Entries
      │              │
      └──────┬───────┘
             ▼
      Consolidated View
```

---

# 38. Unrealised Intercompany Profit

Where goods remain unsold within the group, accounting policy may require elimination of unrealised profit.

The platform SHALL preserve sufficient lineage to support this.

For example:

```text
internal_trade_id
source cost
transfer price
destination inventory lot
remaining quantity
```

---

# 39. Legal-Entity Master Data

Each legal entity participating in internal trade SHOULD have:

```text
canonical ID
legal name
jurisdiction
registration number
tax registrations
functional currency
accounting schema
ERP organisation mapping
bank references
status
effective dates
```

---

# 40. Branch Master Data

A branch or operating unit SHALL NOT automatically become a legal entity.

Conceptually:

```text
Legal Entity
   │
   ├── Branch A
   ├── Branch B
   └── Operating Unit C
```

This distinction is critical to accounting classification.

---

# 41. Business Unit vs Legal Entity

The platform SHALL reject logic equivalent to:

```text
business_unit_id == legal_entity_id
```

unless the mapping is explicit.

---

# 42. Internal Trade Capability Family

Initial capability family:

```text
internal-trade.relationship.resolve

internal-trade.route.resolve

internal-trade.transfer.create

internal-trade.intercompany-sale.create

internal-trade.intercompany-purchase.create

internal-trade.document-pair.create

internal-trade.transfer-price.resolve

internal-trade.transfer-price.approve

internal-trade.inventory-owner.resolve

internal-trade.title-transfer.record

internal-trade.in-transit.manage

internal-trade.invoice.manage

internal-trade.receivable.manage

internal-trade.payable.manage

internal-trade.fx.manage

internal-trade.settlement.manage

internal-trade.reconcile

internal-trade.eliminate
```

---

# 43. Capability Ownership

| Capability | Primary owner |
|---|---|
| Relationship resolution | CP |
| Internal route context | CP |
| Intercompany order workflow | Trade + ERP |
| Transfer pricing execution | Trade/ERP |
| Title transfer | Trade/Inventory |
| Inventory ownership | Trade/ERP |
| AR/AP | ERP |
| FX | ERP |
| Settlement | ERP |
| Elimination | ERP/reporting |
| Readiness | CP |

---

# 44. Capability Resolution

Example:

```text
Request:
internal-trade.intercompany-sale.create

Context:
tenant = zuribeans
origin = ZURIBEANS_UG
destination = ZURIBEANS_ZA
lane = UG-ZA

CP resolves:
relationship
capability grant
provider
readiness
```

---

# 45. Fail-Closed Behaviour

Execution SHALL fail if:

- legal entity unresolved;
- destination legal entity unresolved;
- relationship unresolved;
- required intercompany capability absent;
- provider unavailable;
- ERP organisation mapping absent;
- mandatory accounting configuration absent.

---

# 46. ERP Mapping Requirement

Every legal entity executing accounting SHALL map to an ERP accounting context.

Conceptually:

```text
Canonical Legal Entity
        ↓
ExternalReference
        ↓
iDempiere AD_Client / AD_Org
```

The exact mapping SHALL be deterministic and auditable.

---

# 47. No Direct ERP IDs in Domain Contracts

Canonical business contracts SHALL NOT expose iDempiere-specific identifiers as primary identity.

Use:

```text
canonical_legal_entity_id
```

plus:

```text
ExternalReference
```

for ERP mapping.

---

# 48. Transaction Orchestration

Recommended orchestration:

```text
Internal Trade Request
        ↓
CP Context Resolution
        ↓
Legal Relationship Resolution
        ↓
Trade Workflow
        ↓
ERP Document Creation
        ↓
Shipment
        ↓
Receipt
        ↓
Invoices
        ↓
Settlement
        ↓
Reconciliation
```

---

# 49. Event Model

Potential canonical events:

```text
baobab.internal-trade.created.v1

baobab.internal-trade.classified.v1

baobab.internal-trade.sales-order-created.v1

baobab.internal-trade.purchase-order-created.v1

baobab.internal-trade.shipped.v1

baobab.internal-trade.received.v1

baobab.internal-trade.invoice-issued.v1

baobab.internal-trade.settled.v1

baobab.internal-trade.reconciled.v1
```

---

# 50. Idempotency

Intercompany mirrored document creation SHALL be idempotent.

Repeated delivery of:

```text
internal-trade.created
```

must not create duplicate:

```text
PO
SO
invoice
```

---

# 51. Saga / Workflow Failure

Distributed failure is expected.

Example:

```text
Origin SO created
Destination PO creation fails
```

The system SHALL surface:

```text
PARTIALLY_PROVISIONED / BLOCKED
```

or domain equivalent.

It SHALL NOT silently continue.

---

# 52. Compensation

Compensation rules SHALL be defined per workflow.

Possible actions:

```text
cancel source order
retry destination creation
place transaction on hold
manual intervention
```

Financial postings SHALL never be casually deleted to simulate rollback.

---

# 53. Audit

Internal trade SHALL capture:

```text
actor
origin entity
destination entity
relationship
pricing policy
transaction classification
documents
approvals
FX rates
title transfer
settlement
correlation ID
```

---

# 54. Approval

Intercompany transactions MAY require distinct approvals for:

```text
commercial terms
transfer price
credit
FX
shipment
customs
```

Approval policy SHALL be configurable.

---

# 55. Security

A principal authorised for:

```text
ZuriBeans Uganda
```

SHALL NOT automatically gain authority to:

```text
ZuriBeans South Africa
```

Internal trade requires authority across the relevant operation or a service workflow explicitly delegated for cross-entity execution.

---

# 56. Workload Identity

Service-to-service intercompany orchestration SHALL use workload identity.

No shared admin credentials.

---

# 57. Cross-Entity Isolation

Entity isolation tests SHALL prove:

```text
UG finance user cannot modify ZA ledger
```

unless specifically authorised.

Likewise:

```text
ZA operations user cannot manipulate UG supplier invoice
```

without permission.

---

# 58. Settlement Provider Abstraction

External bank/payment rails MAY be bound through capability providers.

The canonical settlement transaction SHALL remain provider-neutral.

---

# 59. Intercompany Netting

Future netting architecture:

```text
Entity A owes B
Entity B owes A
       ↓
Netting Engine / ERP
       ↓
Net Settlement
```

Release 1 MAY defer automation.

---

# 60. Transfer Pricing Policy Reference

The platform SHOULD reference rather than embed tax policy.

Example:

```text
transfer_pricing_policy_id:
TP-ZB-UG-ZA-2026
```

Policy detail may reside in:

```text
ERP
document management
governance repository
```

depending on design.

---

# 61. Temporal Validity

Legal-entity relationships SHALL be effective-dated.

A relationship may change due to:

- restructuring;
- acquisition;
- disposal;
- branch conversion;
- new subsidiary formation.

Historical transactions SHALL preserve historical classification.

---

# 62. Historical Reproducibility

A transaction from 2027 must still resolve:

```text
what relationship existed then?
what pricing policy applied?
what legal entities were involved?
what accounting route was used?
```

---

# 63. State Model

Suggested `InternalTradeTransaction` states:

```text
DRAFT
  ↓
CLASSIFIED
  ↓
APPROVED
  ↓
ORDERED
  ↓
IN_TRANSIT
  ↓
RECEIVED
  ↓
INVOICED
  ↓
SETTLED
  ↓
RECONCILED
  ↓
CLOSED
```

Exceptional:

```text
BLOCKED
DISPUTED
CANCELLED
FAILED
```

---

# 64. ZuriBeans Scenario A — Same Legal Entity

If ZuriBeans initially operates Uganda and South Africa under one legal entity:

```text
UG Warehouse
     ↓
Cross-market movement
     ↓
ZA Warehouse
```

classification:

```text
INTERNAL_CROSS_MARKET
+
SAME_ENTITY
=
INTER_BRANCH_TRANSFER
```

No artificial intercompany invoice SHALL be created solely because markets differ.

---

# 65. ZuriBeans Scenario B — Separate Legal Entities

If:

```text
ZuriBeans Uganda Ltd
```

and:

```text
ZuriBeans South Africa (Pty) Ltd
```

are separate entities:

```text
INTERNAL_CROSS_MARKET
+
DISTINCT_RELATED_ENTITIES
=
INTERCOMPANY_SALE_PURCHASE
```

---

# 66. ZuriBeans Scenario C — South African Wine to Uganda

```text
ZA entity
procures wine
     ↓
sells internally
     ↓
UG entity purchases
     ↓
cross-border shipment
     ↓
customs
     ↓
UG warehouse
```

Required:

```text
SO
PO
shipment
customs
invoice
AR
AP
FX if applicable
inventory ownership transition
```

---

# 67. ZuriBeans Scenario D — Ugandan Coffee to South Africa

The exact inverse must also work.

No directional assumptions.

---

# 68. Accounting Integrity Rule

No internal transaction is complete merely because:

```text
shipment delivered
```

Completion may additionally require:

```text
inventory reconciled
invoice pair matched
AR/AP matched
payment settled
GL posted
```

---

# 69. Intercompany Credit

Intercompany counterparties MAY have:

```text
credit limit
payment terms
settlement terms
```

but these should be governed separately from external customer credit.

---

# 70. Counterparty Representation

Each legal entity MAY project as a Business Partner in the other entity's ERP context.

Example:

```text
ZuriBeans Uganda Ltd
```

becomes supplier/customer Business Partner for:

```text
ZuriBeans South Africa Ltd
```

where required.

---

# 71. Business Partner Pairing

ERP mapping SHOULD preserve:

```text
canonical legal entity
↔
ERP Business Partner
```

for reciprocal relationships.

---

# 72. Rejected Alternative — Treat All Internal Movements as Stock Transfers

Rejected because separate legal entities require:

- accounting;
- tax;
- customs;
- AR/AP;
- transfer pricing.

---

# 73. Rejected Alternative — Treat All Internal Movements as Sales

Rejected because same-entity movements may not constitute external revenue or receivables/payables.

---

# 74. Rejected Alternative — Infer From Market

Rejected:

```text
UG → ZA
=
intercompany
```

because market geography does not determine legal identity.

---

# 75. Rejected Alternative — Implement Relationship Logic Only in ERP

Rejected because:

- Trade needs classification before orchestration;
- CP needs readiness and provider resolution;
- other engines need canonical context;
- ERP-specific logic would leak into platform semantics.

---

# 76. Rejected Alternative — Implement Relationship Logic Only in Trade

Rejected because legal-entity relationships are platform context, not merely commerce state.

---

# 77. Consequences — Positive

This decision enables:

- correct legal treatment;
- bidirectional internal trade;
- multiple corporate structures;
- inter-branch and intercompany support;
- auditable transfer pricing;
- proper AR/AP;
- FX treatment;
- consolidation;
- future restructuring without redesigning Trade.

---

# 78. Consequences — Complexity

It introduces:

- additional relationship metadata;
- mirrored documents;
- reconciliation;
- ERP integration;
- policy references;
- temporal modelling;
- additional testing.

This complexity is unavoidable in real multi-entity trade.

---

# 79. Shared Contract Requirements

`nabhold/shared` SHALL define canonical contracts for:

```text
LegalEntityRelationship
LegalEntityRelationshipType
InternalTradeTransaction
InternalTradeMode
IntercompanyDocumentLink
TransferPricingReference
InternalSettlementReference
```

---

# 80. API Requirements

CP SHOULD expose resources conceptually equivalent to:

```text
GET /v1/legal-entity-relationships
POST /v1/legal-entity-relationships

POST /v1/legal-entity-relationships/resolve

POST /v1/internal-trade/classify
```

Exact routes SHALL align with CP conventions.

---

# 81. Example Classification Request

```json
{
  "tenant": "zuribeans",
  "origin_legal_entity": "ZURIBEANS_UG",
  "destination_legal_entity": "ZURIBEANS_ZA",
  "origin_market": "UG",
  "destination_market": "ZA"
}
```

Conceptual response:

```json
{
  "transaction_class": "INTERNAL_CROSS_MARKET",
  "legal_relationship": "SISTER_SUBSIDIARY",
  "internal_trade_mode": "INTERCOMPANY_SALE_PURCHASE"
}
```

---

# 82. Provisioning

Legal-entity relationships SHALL be declaratively provisioned.

Example:

```yaml
legal_entity_relationships:

  - source: ZURIBEANS_UG
    target: ZURIBEANS_ZA
    type: SISTER_SUBSIDIARY
    status: ACTIVE
```

---

# 83. Readiness

Internal trade SHALL not become READY unless:

- legal entities exist;
- relationship exists;
- ERP mappings exist;
- required Trade capabilities exist;
- relevant trade lane exists;
- tax/customs dependencies are ready;
- accounting configuration is ready.

---

# 84. Drift

Reconciliation SHALL detect:

```text
relationship missing
relationship changed
ERP mapping missing
paired document missing
AR/AP mismatch
currency mismatch
quantity mismatch
```

---

# 85. Observability

Recommended metrics:

```text
internal_trade_transactions_total
internal_trade_intercompany_total
internal_trade_interbranch_total
internal_trade_reconciliation_failures_total
internal_trade_document_pair_mismatch_total
internal_trade_unsettled_total
```

---

# 86. Test Matrix

| Scenario | Expected |
|---|---|
| Same entity, UG→ZA | Inter-branch |
| Different related entities, UG→ZA | Intercompany |
| Different related entities, ZA→UG | Intercompany |
| Unknown relationship | Block |
| Inactive legal entity | Block |
| Missing ERP mapping | Not ready |
| Duplicate event | No duplicate docs |
| Invoice mismatch | Reconciliation failure |
| Currency difference | FX handling |
| Same tenant, unrelated entity | Do not infer relationship |

---

# 87. Adversarial Tests

Attempt:

- fake destination legal entity;
- cross-tenant legal entity pairing;
- same-tenant shortcut;
- altered relationship type;
- replay intercompany creation;
- create duplicate mirrored PO;
- settle without authorised financial capability;
- manipulate transfer price after approval.

All SHALL fail or produce controlled exception workflows.

---

# 88. Repository Responsibilities

| Repository | Responsibility |
|---|---|
| `shared` | Canonical relationship/internal-trade contracts |
| `baobab-cp` | Legal-entity relationship, classification, readiness |
| `baobab-trade` | Commercial internal-trade workflow |
| `baobab-erp` | AR/AP/GL/FX/settlement/elimination |
| `baobab-iam` | Principal/workload identity |
| `zuribeans` | User-facing workflow only |
| `infrastructure` | Runtime/event/provider availability |

---

# 89. Implementation Sequence

```text
Shared contracts
      ↓
CP relationship model
      ↓
CP classification
      ↓
ERP entity mappings
      ↓
Trade intercompany orchestration
      ↓
ERP document pairing
      ↓
Shipment integration
      ↓
Invoice / AR / AP
      ↓
Settlement
      ↓
Reconciliation
      ↓
Integrated tests
```

---

# 90. Go-Live Requirement

For ZuriBeans go-live, at least one real cross-market internal scenario SHALL execute successfully through staging using the actual intended legal structure.

If ZuriBeans uses separate legal entities:

```text
intercompany path
```

must pass.

If it uses one legal entity with branches:

```text
inter-branch path
```

must pass.

The alternate path SHOULD still have conformance tests so the platform remains generic.

---

# 91. Migration Rule

Any existing logic treating:

```text
ZuriBeans Uganda
```

and:

```text
ZuriBeans South Africa
```

as implicitly the same or implicitly separate legal entities SHALL be identified and removed.

The relationship must become explicit.

---

# 92. Definition of Done

ADR-BCP-012 is implemented when:

- [ ] legal-entity relationships are canonical;
- [ ] relationships are effective-dated;
- [ ] same-entity vs distinct-entity classification is deterministic;
- [ ] internal trade has canonical identity;
- [ ] mirrored order/invoice relationships exist;
- [ ] ERP legal-entity mappings exist;
- [ ] AR/AP consequences are implemented where required;
- [ ] FX handling exists;
- [ ] title/inventory ownership references exist;
- [ ] intercompany reconciliation exists;
- [ ] duplicate event handling is idempotent;
- [ ] audit is complete;
- [ ] cross-entity security tests pass;
- [ ] ZuriBeans bidirectional internal scenarios pass.

---

# 93. Final Decision

Baobab adopts the following decision tree:

```text
Internal Cross-Market Operation
            │
            ▼
Resolve Origin Legal Entity
            │
            ▼
Resolve Destination Legal Entity
            │
            ▼
        Same Entity?
          /     \
        YES      NO
        /         \
Inter-Branch     Resolve Relationship
Transfer              │
                      ▼
                 Related Entities?
                   /        \
                 YES         NO
                 /            \
        Intercompany       External /
        Sale-Purchase      Invalid Context
```

The core principle is:

> **Internal trade is classified by legal identity and legal relationship, not by brand, tenant, market or geography alone.**

This rule SHALL govern ZuriBeans and future Baobab tenants.

---

# Decision Outcome

**ACCEPTED WHEN APPROVED**

Implementation sequence:

```text
ADR
 ↓
Shared Contracts
 ↓
Legal-Entity Relationship Model
 ↓
CP Classification
 ↓
ERP Mapping
 ↓
Trade Orchestration
 ↓
Accounting
 ↓
Reconciliation
 ↓
Golden Tenant Validation
```

No implementation SHALL infer intercompany or inter-branch treatment solely from market names or tenant membership.