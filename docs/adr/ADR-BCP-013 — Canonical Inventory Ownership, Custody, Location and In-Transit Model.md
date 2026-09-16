# ADR-BCP-013 — Canonical Inventory Ownership, Custody, Location and In-Transit Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-12  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Repository:** `nabhold/baobab-cp`  
**Runtime Context Authority:** `nabhold/baobab-cp`  
**Contract Authority:** `nabhold/shared`  
**Commercial Inventory Authority:** `nabhold/baobab-trade`  
**Financial Inventory Authority:** `nabhold/baobab-erp`  
**Identity Authority:** `nabhold/baobab-iam`  
**Reference Tenant:** ZuriBeans

## Depends On

- ADR-BCP-001 through ADR-BCP-010
- ADR-BCP-011 — Market Participation, Trade Lanes and Cross-Market Trading Model
- ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model
- Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification

## Applies To

Inventory identity, legal ownership, physical custody, stock location, warehouse context, market context, lots, batches, serialisation where applicable, reservations, quality status, customs status, title transfer, goods in transit, inter-warehouse movement, inter-branch movement, intercompany movement, external logistics custody, availability, valuation, reconciliation and traceability.

## Architecture Style

Canonical identity, explicit ownership, explicit custody, multi-location, multi-market, multi-legal-entity, event-driven, provider-neutral, temporal and fail-closed.

## Decision Type

Foundational inventory and cross-market stock architecture.

---

# 1. Executive Decision

Baobab SHALL model inventory as a multidimensional business state rather than equating inventory with a quantity stored at a warehouse.

At minimum, the platform SHALL distinguish:

```text
WHAT
Canonical Product / SKU / Variant

WHO OWNS IT
Legal Owner

WHO CONTROLS OR HOLDS IT
Custodian

WHERE IT PHYSICALLY IS
Stock Location

WHERE IT OPERATES COMMERCIALLY
Market

WHAT UNIT OF STOCK IT BELONGS TO
Lot / Batch / Serial where applicable

WHAT CONDITION IT IS IN
Quality Status

WHAT CUSTOMS STATE IT IS IN
Customs Status

WHAT OWNERSHIP STATE IT IS IN
Ownership / Title Status

WHAT LOGISTICS STATE IT IS IN
Stock / Transit State

HOW MUCH EXISTS
Quantity

HOW MUCH CAN BE PROMISED
Availability / Reservation State

WHAT IT IS WORTH
Financial Inventory Value
```

Accordingly:

> **Inventory location does not determine inventory ownership; ownership does not determine custody; custody does not determine commercial availability; physical movement does not necessarily determine title transfer.**

Baobab SHALL support inventory that is:

- physically stored;
- reserved;
- allocated;
- quality-held;
- customs-held;
- quarantined;
- externally warehoused;
- in transit;
- partially received;
- damaged;
- returned;
- owned by one legal entity but held by another party;
- moving between branches;
- moving between related legal entities;
- moving across markets;
- awaiting title transfer.

No domain SHALL infer these dimensions from warehouse or market alone.

---

# 2. Problem

A simplistic inventory model might represent:

```text
SKU-COFFEE-001
Johannesburg Warehouse
1,000 kg
```

This is inadequate for Baobab.

The platform must answer questions such as:

```text
Who legally owns the 1,000 kg?

Who physically has custody?

Is it available for sale?

Which market owns the commercial availability?

Has customs released it?

Has quality approved it?

Is it already reserved?

Was it imported?

Which batch is it?

Where did it originate?

What did it cost?

Is it still owned by ZuriBeans Uganda?

Did title transfer to ZuriBeans South Africa?

Is it physically in Johannesburg or merely destined there?
```

These are different questions.

---

# 3. Why This Matters to ZuriBeans

Consider coffee moving:

```text
Ugandan Supplier
      ↓
ZuriBeans Uganda
      ↓
Export
      ↓
Carrier
      ↓
Customs
      ↓
South Africa
      ↓
ZuriBeans South Africa
      ↓
B2B Buyer
```

At different points the same goods may be:

| Stage | Owner | Custodian | Physical State | Commercial State |
|---|---|---|---|---|
| Supplier warehouse | Supplier | Supplier | Stored | Supplier stock |
| After purchase | ZuriBeans UG | Supplier/ZuriBeans | Stored | Acquired |
| Kampala warehouse | ZuriBeans UG | Warehouse | Stored | Available/held |
| Export dispatched | ZuriBeans UG | Carrier | In transit | Not warehouse stock |
| Border/customs | Depends on title rule | Carrier/customs | In transit/held | Restricted |
| ZA warehouse receipt | Depends on legal structure/title | ZA warehouse | Stored | Pending release |
| Quality cleared | Resolved owner | ZA warehouse | Stored | Potentially available |
| Reserved for buyer | Resolved owner | ZA warehouse | Stored | Reserved |
| Customer delivery | Buyer after title event | Carrier/customer | Delivered | Sold |

A single `warehouse_id + quantity` model cannot represent this correctly.

---

# 4. Architectural Invariants

The following SHALL remain independent:

```text
Inventory
≠
Inventory Position
≠
Product
≠
Warehouse
≠
Stock Location
≠
Market
≠
Legal Owner
≠
Custodian
≠
Title Holder
≠
Commercial Seller
≠
Lot
≠
Shipment
≠
Financial Valuation
```

They may coincide.

They SHALL NOT be assumed to coincide.

---

# 5. Canonical Inventory Identity

Baobab SHALL use canonical product identity across inventory providers.

Conceptually:

```text
CanonicalProduct
      │
      ▼
ProductVariant / SKU
      │
      ▼
InventoryPosition
```

Provider-specific IDs SHALL be mappings.

For example:

```text
Canonical SKU
    │
    ├── Medusa inventory item
    └── iDempiere product
```

Neither provider-specific ID becomes the universal Baobab inventory identity.

---

# 6. Canonical Inventory Position

A canonical inventory position SHALL conceptually support:

```text
InventoryPosition
├── id
├── tenant_id
├── canonical_product_id
├── variant_id?
├── legal_owner_id
├── custodian_id?
├── market_id
├── stock_location_id?
├── warehouse_id?
├── country_id?
├── lot_id?
├── batch_id?
├── serial_id?
├── origin
├── quality_status
├── customs_status
├── ownership_status
├── logistics_status
├── available_quantity
├── reserved_quantity
├── allocated_quantity
├── blocked_quantity
├── in_transit_quantity
├── damaged_quantity
├── uom
├── valuation_reference?
├── effective_at
└── external_references[]
```

This is a canonical semantic model.

It does not require CP to persist every operational inventory row.

---

# 7. Authority Boundary

The authoritative ownership model SHALL be:

```text
CP / Shared
    │
    │ canonical identity,
    │ legal context,
    │ provider resolution
    ▼
Trade
    │
    │ commercial inventory,
    │ ATP, reservation,
    │ allocation
    ▼
ERP
    │
    │ financial inventory,
    │ valuation,
    │ accounting
    ▼
Warehouse / Logistics Providers
    │
    │ physical execution
    ▼
Reconciliation
```

---

# 8. Control Plane Responsibility

CP SHALL own or resolve:

- tenant;
- legal entity;
- market;
- stock-location canonical identity where platform-governed;
- warehouse/provider relationship;
- capability entitlement;
- provider binding;
- trade lane;
- legal relationship;
- isolation;
- readiness.

CP SHALL NOT become a WMS.

CP SHALL NOT maintain operational bin-level stock quantities as its primary responsibility.

---

# 9. Baobab Trade Responsibility

Trade SHALL own the commercial inventory projection required for commerce, including:

```text
sellable inventory
available-to-promise
reservations
allocations
sales-channel availability
market availability
commercial stock visibility
```

Medusa inventory capabilities SHOULD be used where they correctly satisfy these responsibilities.

---

# 10. Baobab ERP Responsibility

ERP SHALL own financial inventory consequences including:

```text
inventory valuation
goods receipt
goods issue
stock transfer accounting
landed cost
inventory adjustments
costing
financial ownership
GL consequences
period-end valuation
```

iDempiere SHALL not be reduced to an invoice sink.

---

# 11. Warehouse Execution Responsibility

A warehouse/WMS/provider MAY own:

```text
bin
zone
putaway
pick
pack
load
cycle count
physical movement
scanner events
```

Baobab SHALL integrate these through provider-neutral contracts where required.

---

# 12. Legal Ownership

Every material inventory position SHALL have a determinable legal owner.

Conceptually:

```text
inventory.legal_owner_id
```

The owner may be:

- ZuriBeans legal entity;
- supplier;
- customer;
- external principal;
- another authorised entity.

A warehouse SHALL NOT implicitly own inventory merely because the goods are located there.

---

# 13. Custody

Custody SHALL be modelled independently from ownership.

Example:

```text
Owner:
ZuriBeans Uganda

Custodian:
DHL / freight forwarder / warehouse operator
```

Therefore:

```text
Owner ≠ Custodian
```

is a normal state.

---

# 14. Custody Examples

```text
ZuriBeans-owned coffee
held by external 3PL
```

and:

```text
supplier-owned consignment stock
held in ZuriBeans warehouse
```

are both representable.

---

# 15. Stock Location

`StockLocation` SHALL identify the physical or logical place at which inventory is managed.

Examples:

```text
warehouse
store
processing facility
port facility
bonded warehouse
3PL warehouse
virtual transit location
quality quarantine area
```

A stock location SHALL not automatically imply legal ownership.

---

# 16. Warehouse

A warehouse is an operational facility.

Conceptually:

```text
Warehouse
├── canonical_id
├── tenant_id
├── operator
├── market
├── country
├── legal_entity_relationship
├── provider_reference
└── stock_locations[]
```

Warehouses may be:

```text
OWNED
LEASED
3PL
BONDED
SHARED
```

where required.

---

# 17. Warehouse Is Not Legal Entity

The following inference is prohibited:

```text
warehouse = Kampala
therefore
owner = ZuriBeans Uganda
```

Likewise:

```text
warehouse = Johannesburg
therefore
owner = ZuriBeans South Africa
```

Ownership must be explicit.

---

# 18. Market Is Not Ownership

The following is also prohibited:

```text
inventory market = ZA
therefore
legal owner = ZA legal entity
```

A Ugandan entity may own inventory physically located in South Africa depending on transaction/title state.

---

# 19. Lot and Batch Traceability

For commodities and regulated products, Baobab SHALL support lot/batch traceability.

Conceptually:

```text
Product
  ↓
Lot
  ↓
Inventory Position
  ↓
Shipment
  ↓
Customer
```

Traceability SHALL support backward and forward queries.

---

# 20. Lot Attributes

A lot MAY include:

```text
lot number
origin
producer/supplier
harvest/production date
processing date
grade
quality characteristics
certifications
expiry/best-before
regulatory evidence
```

Exact product-specific fields SHALL be extensible.

---

# 21. Commodity Traceability

For ZuriBeans coffee:

```text
Supplier
  ↓
Farm / Cooperative where available
  ↓
Origin
  ↓
Lot
  ↓
Quality Result
  ↓
Warehouse
  ↓
Shipment
  ↓
Buyer
```

The platform SHALL not hard-code coffee-specific attributes into the generic inventory core.

---

# 22. Quality State

Inventory SHALL support explicit quality status.

Initial states SHOULD include:

```text
PENDING_INSPECTION
APPROVED
REJECTED
QUARANTINED
CONDITIONALLY_RELEASED
DAMAGED
```

Additional states may be domain-specific.

---

# 23. Quality Availability Rule

Inventory SHALL NOT automatically become commercially available merely because it was received.

Example:

```text
Goods Receipt
    ↓
PENDING_INSPECTION
    ↓
Quality Test
   /     \
PASS     FAIL
 │        │
 ▼        ▼
APPROVED QUARANTINED
```

Only approved states may contribute to ATP where policy requires.

---

# 24. Customs State

Cross-border inventory SHALL support explicit customs status.

Initial conceptual states:

```text
NOT_APPLICABLE
PRE_EXPORT
EXPORT_PENDING
EXPORT_CLEARED
INTERNATIONAL_TRANSIT
IMPORT_PENDING
CUSTOMS_HELD
IMPORT_CLEARED
BONDED
RELEASED
```

Exact state machine SHALL be refined with the customs ADR.

---

# 25. Customs State Is Not Logistics State

For example:

```text
Logistics:
ARRIVED_AT_DESTINATION

Customs:
IMPORT_PENDING
```

Goods are physically present but not necessarily commercially releasable.

---

# 26. Ownership Status

Ownership/title status SHOULD support:

```text
SUPPLIER_OWNED
TENANT_OWNED
INTERNAL_TRANSFER_PENDING
TITLE_TRANSFER_PENDING
CUSTOMER_OWNED
CONSIGNMENT
```

The exact model SHALL integrate with ADR-BCP-022 on Incoterms/title transfer.

---

# 27. Logistics State

Inventory logistics state SHOULD distinguish:

```text
AT_REST
ALLOCATED_FOR_TRANSFER
PICKED
DISPATCHED
IN_TRANSIT
ARRIVED
RECEIVING
RECEIVED
DELIVERED
LOST
DAMAGED
```

Shipment remains the primary logistics transaction.

Inventory references shipment state rather than replacing it.

---

# 28. In-Transit Inventory

Baobab SHALL treat in-transit inventory as a first-class state.

The architecture SHALL NOT model dispatch as:

```text
source quantity - 100
destination quantity + 100
```

in one atomic conceptual step.

Instead:

```text
Source Stock
     ↓
Dispatch
     ↓
IN-TRANSIT STOCK
     ↓
Receipt
     ↓
Destination Stock
```

---

# 29. Why In-Transit Is Mandatory

Without explicit in-transit state, the system cannot reliably answer:

```text
Where are the goods?

Who owns them?

What is the financial exposure?

What is delayed?

What is insured?

What is expected at destination?

What quantity is missing?

What should customs reconcile?
```

---

# 30. In-Transit Inventory Position

Conceptually:

```text
InTransitInventory
├── inventory_position_id
├── shipment_id
├── origin_location_id
├── destination_location_id
├── legal_owner_id
├── custodian_id
├── quantity
├── uom
├── dispatched_at
├── expected_arrival_at?
├── title_status
├── customs_status
└── logistics_status
```

---

# 31. Cross-Market Inventory Movement

Generic flow:

```text
Origin Inventory
      │
      ▼
Reservation for Transfer
      │
      ▼
Pick / Pack
      │
      ▼
Dispatch
      │
      ▼
In Transit
      │
      ▼
Customs / Border / Port
      │
      ▼
Destination Receipt
      │
      ▼
Quality / Customs Release
      │
      ▼
Destination Availability
```

---

# 32. Inter-Branch Inventory Movement

Same legal entity:

```text
UG Warehouse
    │
    ▼
Inventory Transfer
    │
    ▼
In Transit
    │
    ▼
ZA Warehouse
```

Legal ownership may remain unchanged throughout.

Financial inventory may move between:

```text
Inventory — Uganda
      ↓
Inventory In Transit
      ↓
Inventory — South Africa
```

subject to ERP accounting policy.

---

# 33. Intercompany Inventory Movement

Different legal entities:

```text
Entity A Inventory
      │
      ▼
Shipment
      │
      ▼
In Transit
      │
      │ Title Transfer Event
      ▼
Entity B Inventory
```

Ownership may change before, during or after physical transit.

Therefore physical receipt SHALL NOT be the universal ownership-transfer event.

---

# 34. Title Transfer Event

Baobab SHALL support an explicit title-transfer event or reference.

Conceptually:

```text
baobab.inventory.title-transferred.v1
```

with:

```text
inventory
from_owner
to_owner
effective_at
reason
contract_reference
shipment_reference
incoterm_reference
evidence_reference
```

---

# 35. Physical Possession vs Legal Title

Example:

```text
Goods physically:
Port of Durban

Custodian:
Port / customs operator

Legal owner:
ZuriBeans Uganda

Commercial destination:
ZuriBeans South Africa

Customs:
IMPORT_PENDING
```

This is valid.

---

# 36. Available-to-Promise

ATP SHALL be derived, not blindly equated with on-hand quantity.

Conceptually:

```text
ATP =
Eligible On-Hand
- Reserved
- Allocated
- Blocked
- Quality Hold
- Customs Hold
- Safety Stock
+ Eligible Expected Supply
```

Exact calculation remains Trade-domain policy.

---

# 37. On-Hand vs Available

The following SHALL remain separate:

```text
ON_HAND
AVAILABLE
RESERVED
ALLOCATED
BLOCKED
IN_TRANSIT
EXPECTED
```

A quantity may be physically on hand but commercially unavailable.

---

# 38. Reservations

Reservation SHALL bind inventory to commercial demand without changing legal ownership.

Conceptually:

```text
Inventory
  1,000 kg

Reserved
    300 kg

Available
    700 kg
```

---

# 39. Allocation

Allocation MAY bind a specific:

```text
lot
location
quantity
```

to an order or shipment.

Reservation and allocation SHALL not necessarily be synonymous.

---

# 40. Overselling

Trade SHALL prevent overselling according to inventory policy.

Concurrency controls SHALL ensure two simultaneous orders cannot consume the same availability.

---

# 41. Expected Inventory

The platform MAY expose expected supply separately from on-hand inventory.

Examples:

```text
confirmed purchase order
confirmed internal transfer
confirmed inbound shipment
```

Expected inventory SHALL not automatically be sellable.

---

# 42. Supplier-Owned Inventory

Consignment scenarios SHALL be supportable.

Example:

```text
Location:
ZuriBeans warehouse

Owner:
Supplier

Custodian:
ZuriBeans

Commercial status:
Consignment
```

---

# 43. Customer-Owned Inventory

Post-title-transfer goods may remain physically in a ZuriBeans-controlled warehouse.

Example:

```text
Owner:
Customer

Custodian:
ZuriBeans warehouse

Status:
Awaiting collection
```

The inventory model SHALL represent this without pretending ZuriBeans still owns the stock.

---

# 44. 3PL Inventory

External logistics providers SHALL be represented through custody/provider references.

Baobab SHALL not duplicate the entire 3PL WMS internally.

Instead:

```text
3PL Operational State
        ↓
Provider Adapter
        ↓
Canonical Inventory Projection
```

---

# 45. Inventory Capability Family

Initial capabilities SHOULD include:

```text
inventory.position.read
inventory.availability.read

inventory.receive
inventory.reserve
inventory.release
inventory.allocate
inventory.deallocate

inventory.transfer.create
inventory.transfer.dispatch
inventory.transfer.receive

inventory.in-transit.read
inventory.in-transit.manage

inventory.owner.resolve
inventory.custodian.resolve
inventory.title-transfer.record

inventory.quality.hold
inventory.quality.release

inventory.customs.hold
inventory.customs.release

inventory.adjust
inventory.reconcile
```

---

# 46. Capability Ownership Matrix

| Capability | Primary Owner |
|---|---|
| Canonical product identity | Shared/CP |
| Provider resolution | CP |
| Legal-owner context | CP/ERP |
| Commercial availability | Trade |
| Reservation | Trade |
| Allocation | Trade |
| Financial inventory | ERP |
| Valuation | ERP |
| Physical movement | Warehouse/Trade/ERP provider |
| In-transit projection | Trade + ERP |
| Quality state | ERP/quality provider |
| Customs state | Trade/compliance provider |
| Reconciliation | Cross-domain |

---

# 47. Inventory Provider Resolution

Different capabilities MAY resolve to different providers.

Example:

```text
inventory.availability.read
        ↓
Baobab Trade / Medusa

inventory.value.read
        ↓
Baobab ERP / iDempiere

inventory.physical-bin.read
        ↓
External WMS
```

This is intentional.

---

# 48. No Single Inventory God Object

Baobab SHALL NOT attempt to make:

```text
Medusa
```

or:

```text
iDempiere
```

or:

```text
Control Plane
```

the universal owner of every inventory concern.

The canonical model reconciles specialised authorities.

---

# 49. Commercial vs Financial Inventory

A critical distinction is:

```text
Commercial Inventory
        ≠
Financial Inventory
```

Commercial inventory asks:

> Can I promise this product to this customer?

Financial inventory asks:

> What inventory asset does this legal entity own and at what value?

Both must reconcile.

---

# 50. Example

Trade:

```text
SKU A
available = 900 kg
reserved = 100 kg
```

ERP:

```text
SKU A
on hand = 1,000 kg
value = ZAR X
```

This may be valid.

A mismatch is not automatically an error.

The reconciliation model must understand semantic differences.

---

# 51. Reconciliation Dimensions

Inventory reconciliation SHALL compare:

```text
product
legal owner
location
lot/batch
on-hand
reserved
allocated
in-transit
quality state
customs state
financial value where applicable
```

---

# 52. Reconciliation Outcomes

Suggested states:

```text
MATCHED
EXPECTED_DIFFERENCE
MISMATCH
STALE
UNRESOLVED
```

---

# 53. Reconciliation Example

```text
Trade:
On-hand = 1,000
Reserved = 200
Available = 800

ERP:
Financial on-hand = 1,000

Result:
MATCHED
```

But:

```text
Trade:
On-hand = 950

ERP:
On-hand = 1,000

Result:
MISMATCH
```

unless an explicit known timing condition explains it.

---

# 54. Inventory Events

Canonical events SHOULD include:

```text
baobab.inventory.received.v1
baobab.inventory.reserved.v1
baobab.inventory.reservation-released.v1
baobab.inventory.allocated.v1
baobab.inventory.transfer-created.v1
baobab.inventory.dispatched.v1
baobab.inventory.in-transit.v1
baobab.inventory.arrived.v1
baobab.inventory.title-transferred.v1
baobab.inventory.quality-held.v1
baobab.inventory.quality-released.v1
baobab.inventory.customs-held.v1
baobab.inventory.customs-released.v1
baobab.inventory.adjusted.v1
baobab.inventory.reconciled.v1
```

---

# 55. Event Identity

Inventory events SHALL carry:

```text
tenant
canonical product
inventory position
legal owner
market
location
quantity
uom
lot/batch where applicable
correlation ID
causation ID
event time
```

plus relevant contextual dimensions.

---

# 56. Idempotency

Inventory events SHALL be idempotently consumed.

Repeated:

```text
inventory.received
```

must not increase stock twice.

This is a P0 correctness requirement.

---

# 57. Concurrency

Reservation and allocation operations SHALL provide appropriate concurrency protection.

The platform SHALL prevent:

```text
Order A reserves last 100 units

and simultaneously

Order B reserves same 100 units
```

from both succeeding.

---

# 58. Units of Measure

Inventory SHALL preserve explicit UOM.

For example:

```text
kg
g
tonne
bag
case
bottle
pallet
```

Conversion SHALL require governed conversion rules.

---

# 59. UOM Invariant

The system SHALL NOT add:

```text
100 kg
+
100 bags
```

without a defined conversion.

---

# 60. Packaging

Commercial packaging MAY differ from inventory UOM.

Example:

```text
Inventory:
1,000 kg coffee

Commercial:
20 × 50 kg bags
```

The relationship SHALL be explicit.

---

# 61. Inventory Origin

Inventory SHOULD retain origin information where required.

Origin may include:

```text
country of origin
producer
production site
farm/cooperative
supplier
lot
```

Origin SHALL not be overwritten merely because goods move to another market.

---

# 62. Origin vs Current Location

Example:

```text
Origin:
Uganda

Current Location:
South Africa
```

is normal.

Therefore:

```text
Origin ≠ Location
```

---

# 63. Customs Origin

Customs origin may require specialised rules beyond physical production location.

The inventory model SHALL preserve references required by the trade-compliance domain rather than independently deciding origin law.

---

# 64. Landed Cost

Inventory SHALL support financial association with landed-cost calculations.

Conceptually:

```text
Purchase Cost
+ Freight
+ Insurance
+ Duty
+ Brokerage
+ Port Charges
+ Other Allocable Costs
=
Landed Cost
```

ERP SHALL own the financial consequence.

---

# 65. Inventory Valuation

Valuation methods MAY include, subject to ERP configuration:

```text
standard cost
average cost
FIFO
other supported accounting policy
```

Baobab SHALL not hard-code a single method at platform level.

---

# 66. Valuation Currency

Inventory may have:

```text
transaction cost currency
functional currency value
reporting currency value
```

These SHALL remain distinct.

---

# 67. Damaged Inventory

Damage SHALL not simply delete quantity.

The system SHOULD support:

```text
Available
   ↓
Damaged
   ↓
Inspection
  /   \
Recover Scrap
```

with accounting consequences.

---

# 68. Lost Inventory

In-transit loss SHALL be explicitly recorded and reconciled against:

```text
shipment
carrier
insurance
financial inventory
claim
```

---

# 69. Returns

Returned goods SHALL re-enter inventory only through a controlled state.

Example:

```text
Customer Return
      ↓
Received
      ↓
Quarantine
      ↓
Inspection
     /   \
Resell  Reject
```

---

# 70. Recall

Lot/batch traceability SHALL support identifying:

```text
all locations
all shipments
all customers
all remaining stock
```

associated with an affected lot.

---

# 71. Stock Adjustment

Manual adjustment SHALL require:

```text
reason
actor
before
after
evidence/reference
timestamp
```

High-risk adjustments SHOULD require approval.

---

# 72. Negative Inventory

Negative physical inventory SHOULD be prohibited by default.

If a provider permits it for accounting reasons, it SHALL not automatically become valid commercial availability.

---

# 73. Market Availability

A product physically present in a market SHALL not automatically be sellable there.

Resolution may require:

```text
Market Participation
+
Market Assortment
+
Product Eligibility
+
Inventory Availability
+
Quality Release
+
Customs Release
+
Pricing
```

---

# 74. Inventory and Trade Lanes

Trade-lane context SHALL accompany cross-market stock movements.

Example:

```text
UG Inventory
     ↓
UG → ZA
     ↓
In Transit
     ↓
ZA Inventory
```

The trade lane SHALL not replace the inventory-transfer transaction.

---

# 75. Inventory and Legal Relationships

Cross-market transfer classification SHALL consume ADR-BCP-012.

```text
Origin owner
      +
Destination context
      ↓
Legal Relationship Resolver
      ↓
Inter-Branch
or
Intercompany
```

---

# 76. Inventory Transfer Object

A canonical transfer SHOULD conceptually contain:

```text
InventoryTransfer
├── id
├── tenant_id
├── origin_location_id
├── destination_location_id
├── origin_market_id
├── destination_market_id
├── trade_lane_id?
├── origin_legal_owner_id
├── expected_destination_owner_id
├── transfer_type
├── shipment_id?
├── lines[]
├── status
└── external_references[]
```

---

# 77. Transfer Types

Initial types SHOULD include:

```text
WAREHOUSE_TRANSFER
INTER_BRANCH_TRANSFER
INTERCOMPANY_TRANSFER
CUSTOMER_RETURN
SUPPLIER_RETURN
QUALITY_TRANSFER
```

---

# 78. Transfer State Machine

```text
DRAFT
  ↓
APPROVED
  ↓
RESERVED
  ↓
PICKED
  ↓
DISPATCHED
  ↓
IN_TRANSIT
  ↓
ARRIVED
  ↓
RECEIVING
  ↓
RECEIVED
  ↓
RECONCILED
  ↓
CLOSED
```

Exceptional:

```text
CANCELLED
BLOCKED
PARTIALLY_RECEIVED
DAMAGED
LOST
DISPUTED
```

---

# 79. Partial Shipment

Transfers SHALL support:

```text
ordered: 1,000 kg
dispatched: 1,000 kg
received: 980 kg
```

The missing:

```text
20 kg
```

must remain an exception, not disappear.

---

# 80. Partial Receipt

A partial receipt SHALL preserve remaining in-transit quantity where appropriate.

```text
In transit: 1,000
Received: 600
Remaining in transit: 400
```

---

# 81. Split Shipments

One transfer MAY be executed through multiple shipments.

```text
Transfer T1
   ├── Shipment S1
   ├── Shipment S2
   └── Shipment S3
```

Therefore:

```text
Transfer ≠ Shipment
```

---

# 82. Consolidated Shipments

Conversely, one shipment MAY contain lines associated with several orders/transfers where policy permits.

Canonical references SHALL preserve lineage.

---

# 83. Reservation During Cross-Market Movement

Whether in-transit inventory may be promised to customers SHALL be policy-driven.

For example:

```text
ZA expected inventory:
500 kg

ATP policy:
allow pre-sale after export clearance
```

may expose part of expected inventory.

This SHALL not be assumed globally.

---

# 84. Cross-Market Inventory Flow Example — Coffee

```text
Supplier
   ↓
Goods Receipt UG
   ↓
Quality Hold
   ↓
Quality Release
   ↓
UG Available Stock
   ↓
Transfer Reservation
   ↓
Dispatch
   ↓
UG → ZA In Transit
   ↓
Import Customs Hold
   ↓
Customs Release
   ↓
ZA Receipt
   ↓
Destination Quality Check
   ↓
ZA Available Stock
```

---

# 85. Cross-Market Inventory Flow Example — Wine

```text
South African Supplier
       ↓
ZA Goods Receipt
       ↓
Regulatory / Quality Release
       ↓
ZA Inventory
       ↓
ZA → UG Transfer
       ↓
Export
       ↓
In Transit
       ↓
UG Import / Regulatory Hold
       ↓
UG Receipt
       ↓
UG Commercial Availability
```

The generic inventory architecture is unchanged.

Product-specific compliance differs.

---

# 86. No Product-Specific Inventory Core

This ADR SHALL NOT create:

```text
CoffeeInventory
WineInventory
VanillaInventory
```

as platform-core models.

Instead:

```text
Canonical Inventory
        +
Product Classification
        +
Extensible Attributes
```

---

# 87. Security

Inventory operations SHALL be tenant-, legal-entity-, market- and capability-aware where applicable.

A user who can read:

```text
ZA inventory
```

SHALL NOT automatically read:

```text
UG inventory
```

unless authorised.

---

# 88. Buyer Isolation

Buyer organisations SHALL not receive unrestricted inventory visibility.

They SHOULD receive commercial availability projections appropriate to:

```text
buyer
market
catalogue
contract
sales channel
```

rather than internal inventory topology.

---

# 89. Supplier Isolation

Suppliers SHALL only see inventory information explicitly exposed to their supplier workflow.

A supplier SHALL NOT gain visibility into unrelated suppliers' stock.

---

# 90. Cross-Tenant Isolation

This SHALL always fail:

```text
Thamani principal
      ↓
ZuriBeans inventory position
```

unless a deliberately authorised inter-tenant commercial interface exists.

Common corporate ownership is insufficient.

---

# 91. Audit

Material inventory mutations SHALL capture:

```text
actor
tenant
legal owner
product
location
quantity before
quantity after
reason
transaction reference
correlation ID
timestamp
```

---

# 92. Observability

Recommended metrics include:

```text
inventory_on_hand
inventory_available
inventory_reserved
inventory_allocated
inventory_blocked
inventory_in_transit
inventory_quality_hold
inventory_customs_hold

inventory_reconciliation_mismatch_total
inventory_transfer_duration
inventory_transfer_exception_total
inventory_negative_attempt_total
```

High-cardinality labels SHALL be controlled.

---

# 93. Readiness

Inventory capability SHALL not be READY unless required components exist.

Conceptually:

```text
Product Mapping
      +
Stock Locations
      +
Legal Owner Mapping
      +
Trade Inventory Provider
      +
ERP Inventory Provider
      +
Event Integration
      +
Reconciliation
      =
Inventory READY
```

---

# 94. Warehouse Readiness

A warehouse intended for production SHALL require:

- canonical location identity;
- market association;
- operator/custodian;
- legal-entity relationship;
- provider mapping;
- supported inventory capabilities;
- health/readiness;
- reconciliation path.

---

# 95. Provider Failure

If Trade inventory is unavailable:

```text
commercial order execution
```

may be blocked.

If ERP valuation is temporarily unavailable:

```text
financial posting
```

may be delayed or blocked according to policy.

The platform SHALL not fabricate inventory state.

---

# 96. Eventual Consistency

Trade and ERP inventory projections may be eventually consistent.

The architecture SHALL explicitly monitor:

```text
event lag
reconciliation lag
staleness
```

rather than pretending distributed state is instantaneously identical.

---

# 97. Staleness

Inventory responses SHOULD expose or internally track:

```text
observed_at
source
version
```

where relevant.

A stale availability projection SHOULD NOT be treated as authoritative indefinitely.

---

# 98. Shared Contract Requirements

`nabhold/shared` SHALL define canonical contracts for at least:

```text
InventoryPosition
InventoryTransfer
InventoryTransferLine
InventoryOwnership
InventoryCustody
InventoryStatus
QualityStatus
CustomsInventoryStatus
InventoryTransferStatus
LotReference
BatchReference
TitleTransferReference
```

---

# 99. API Contracts

The platform MAY expose capability-oriented APIs such as:

```text
GET  /inventory/availability
GET  /inventory/positions

POST /inventory/reservations
POST /inventory/transfers
POST /inventory/transfers/{id}/dispatch
POST /inventory/transfers/{id}/receive

POST /inventory/title-transfers
POST /inventory/reconciliation
```

The actual API location SHALL follow provider ownership rather than forcing all inventory APIs into CP.

---

# 100. Event-Driven Integration

Preferred cross-engine flow:

```text
Trade
  │
  │ transfer.created
  ▼
RabbitMQ
  │
  ├────► ERP
  │
  ├────► Logistics
  │
  └────► Reconciliation
```

with transactional outbox/inbox and idempotent consumers.

---

# 101. Transactional Outbox

Mandatory inventory events SHALL be committed through the provider's transactional outbox where state mutation and event publication must remain consistent.

Pattern:

```text
Database Transaction
    ├── mutate inventory state
    └── write outbox event
             ↓
         Publisher
             ↓
          RabbitMQ
```

---

# 102. No Distributed Database Transaction

Baobab SHALL NOT require one ACID transaction spanning:

```text
Medusa PostgreSQL
+
iDempiere PostgreSQL
+
Control Plane PostgreSQL
```

Cross-engine consistency SHALL use:

- canonical events;
- idempotency;
- workflow/saga semantics;
- reconciliation.

---

# 103. Failure Scenario — ERP Event Delayed

```text
Trade:
dispatch committed

ERP:
event delayed
```

Expected behaviour:

```text
Trade = dispatched
Integration = pending
Reconciliation = lagging
```

not:

```text
pretend ERP succeeded
```

---

# 104. Failure Scenario — Destination Receipt Mismatch

```text
Dispatched:
1,000 kg

Received:
975 kg
```

System SHALL create an exception for:

```text
25 kg
```

and preserve evidence.

---

# 105. Failure Scenario — Customs Hold

```text
Shipment arrived
      ↓
CUSTOMS_HELD
```

Destination ATP SHALL not expose inventory where policy requires customs release first.

---

# 106. Failure Scenario — Quality Rejection

```text
Received:
1,000 kg

Approved:
800 kg

Rejected:
200 kg
```

Only eligible quantity contributes to commercial availability.

---

# 107. Failure Scenario — Title Dispute

If title cannot be deterministically resolved:

```text
TITLE_TRANSFER_PENDING
```

or equivalent SHALL prevent incorrect financial ownership posting where mandatory.

---

# 108. Testing Strategy

## Unit Tests

- quantity calculations;
- ownership transitions;
- custody transitions;
- state transitions;
- UOM conversion;
- ATP policy;
- title transfer;
- partial receipts.

## Integration Tests

- Trade ↔ ERP;
- Trade ↔ CP;
- Trade ↔ logistics provider;
- ERP ↔ CP mappings;
- RabbitMQ events;
- reconciliation.

## Security Tests

- tenant isolation;
- legal-entity isolation;
- buyer isolation;
- supplier isolation;
- workload-token scopes.

## Concurrency Tests

- simultaneous reservations;
- simultaneous transfer receipt;
- duplicate event delivery.

---

# 109. Required ZuriBeans Tests

At minimum:

```text
UG local receipt → sale

ZA local receipt → sale

UG → ZA transfer

ZA → UG transfer

UG → third-country shipment

ZA → third-country shipment

partial cross-market receipt

customs hold

quality hold

damaged goods

in-transit loss

intercompany ownership transfer

inter-branch owner-preserving transfer
```

---

# 110. Bidirectional Golden Tenant Test

The staging environment SHALL prove:

```text
UG COFFEE
UG warehouse
   ↓
UG → ZA
   ↓
ZA warehouse
```

and independently:

```text
ZA WINE
ZA warehouse
   ↓
ZA → UG
   ↓
UG warehouse
```

using the same generic architecture.

No country-specific inventory code.

---

# 111. Rejected Alternative — Warehouse Equals Inventory

Rejected because it cannot represent:

- transit;
- external custody;
- title transfer;
- consignment;
- customs hold;
- ownership.

---

# 112. Rejected Alternative — ERP Is Sole Runtime Inventory Source

Rejected because commerce requires performant:

```text
availability
reservation
allocation
```

and Medusa provides appropriate commercial inventory capabilities.

ERP remains financial authority.

---

# 113. Rejected Alternative — Trade Is Sole Inventory Authority

Rejected because commercial inventory does not replace:

```text
financial valuation
accounting ownership
landed cost
GL
```

---

# 114. Rejected Alternative — Immediate Destination Receipt

Rejected:

```text
dispatch source
      ↓
immediately increment destination
```

because goods may spend days or weeks:

```text
in transit
at port
in customs
```

---

# 115. Rejected Alternative — Ownership Follows Location

Rejected because ownership and possession are legally distinct.

---

# 116. Rejected Alternative — Ownership Always Changes at Receipt

Rejected because title transfer depends on contract and transaction policy.

---

# 117. Rejected Alternative — Duplicate Full WMS in Baobab Trade

Rejected.

Baobab should build only the warehouse capabilities required by the platform and integrate specialised WMS/3PL systems when complexity warrants it.

---

# 118. Consequences — Positive

This decision enables:

- accurate cross-border stock;
- correct legal ownership;
- intercompany trade;
- inter-branch transfers;
- real in-transit visibility;
- customs holds;
- quality holds;
- external custody;
- consignment;
- lot traceability;
- proper ERP valuation;
- safe commerce availability;
- future WMS integration.

---

# 119. Consequences — Complexity

It requires:

- richer canonical contracts;
- cross-engine reconciliation;
- explicit state machines;
- more event processing;
- more master data;
- title/custody modelling;
- stronger testing.

This complexity reflects the actual business.

Removing it from the model would not remove it from ZuriBeans operations; it would merely hide it.

---

# 120. Repository Responsibilities

| Repository | Responsibility |
|---|---|
| `nabhold/shared` | Canonical inventory contracts/events |
| `nabhold/baobab-cp` | Context, legal owner identity, provider/capability resolution |
| `nabhold/baobab-trade` | ATP, reservations, allocations, commercial projection |
| `nabhold/baobab-erp` | Financial stock, valuation, receipts/issues, accounting |
| `nabhold/baobab-iam` | Identity and workload security |
| `nabhold/zuribeans` | Buyer/supplier/operations UI projections |
| `nabhold/infrastructure` | RabbitMQ, observability, storage/runtime |
| Logistics/WMS providers | Physical execution where bound |

---

# 121. Implementation Sequence

```text
ADR
 ↓
Shared Inventory Contracts
 ↓
Canonical Product/Location Mapping
 ↓
Legal Owner/Custody Model
 ↓
Trade Commercial Inventory
 ↓
ERP Financial Inventory
 ↓
Transfer Model
 ↓
In-Transit Model
 ↓
Title Transfer
 ↓
Quality / Customs States
 ↓
Events
 ↓
Reconciliation
 ↓
Golden Tenant Tests
```

---

# 122. Migration Requirements

Existing code SHALL be audited for assumptions equivalent to:

```text
warehouse = owner
market = owner
dispatch = destination receipt
receipt = title transfer
on_hand = available
shipment = transfer
```

Such assumptions SHALL be removed or explicitly justified by domain policy.

---

# 123. Go-Live Blockers

The following are P0 for ZuriBeans:

- canonical SKU mapping;
- UG and ZA stock locations;
- legal-owner context;
- Trade commercial inventory;
- ERP financial inventory;
- reservation correctness;
- in-transit inventory;
- cross-market transfer;
- receipt;
- lot/batch traceability for relevant products;
- quality hold/release;
- customs hold/release;
- event idempotency;
- inventory reconciliation;
- bidirectional UG↔ZA tests.

---

# 124. Definition of Done

ADR-BCP-013 is implemented when:

- [ ] inventory ownership is explicit;
- [ ] custody is explicit;
- [ ] physical location is independent of ownership;
- [ ] market is independent of ownership;
- [ ] canonical inventory contracts exist;
- [ ] Trade commercial inventory is implemented;
- [ ] ERP financial inventory is implemented;
- [ ] canonical provider mappings exist;
- [ ] reservation and allocation are concurrency-safe;
- [ ] in-transit inventory exists;
- [ ] partial receipts are supported;
- [ ] cross-market transfers exist;
- [ ] title-transfer references exist;
- [ ] quality status exists;
- [ ] customs status exists;
- [ ] lot/batch traceability exists where required;
- [ ] events are idempotent;
- [ ] reconciliation exists;
- [ ] cross-tenant isolation passes;
- [ ] UG→ZA test passes;
- [ ] ZA→UG test passes;
- [ ] no country-specific inventory logic exists.

---

# 125. Final Architecture

```text
                        CANONICAL PRODUCT
                               │
                               ▼
                       INVENTORY POSITION
                               │
          ┌────────────────────┼────────────────────┐
          │                    │                    │
          ▼                    ▼                    ▼
      LEGAL OWNER           CUSTODIAN            LOCATION
          │                    │                    │
          └────────────┬───────┴────────────┬───────┘
                       │                    │
                       ▼                    ▼
                 QUALITY STATE       CUSTOMS STATE
                       │                    │
                       └─────────┬──────────┘
                                 ▼
                           STOCK STATE
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
           ON HAND           IN TRANSIT          BLOCKED
              │
              ▼
     COMMERCIAL AVAILABILITY
              │
              ▼
         RESERVATION
              │
              ▼
          ALLOCATION
```

Cross-market movement:

```text
ORIGIN INVENTORY
       │
       ▼
TRANSFER
       │
       ▼
RESERVE / ALLOCATE
       │
       ▼
DISPATCH
       │
       ▼
IN-TRANSIT INVENTORY
       │
       ├────► CUSTODY CHANGES
       │
       ├────► CUSTOMS STATE CHANGES
       │
       └────► TITLE MAY TRANSFER
       │
       ▼
DESTINATION ARRIVAL
       │
       ▼
GOODS RECEIPT
       │
       ▼
QUALITY / CUSTOMS RELEASE
       │
       ▼
DESTINATION INVENTORY
       │
       ▼
COMMERCIAL AVAILABILITY
```

And across engines:

```text
                    BAOBAB CONTROL PLANE
                  Context / Identity / Policy
                            │
                 ┌──────────┴──────────┐
                 │                     │
                 ▼                     ▼
          BAOBAB TRADE             BAOBAB ERP
        Commercial Stock         Financial Stock
        ATP / Reservation       Value / Ownership
        Allocation              Accounting / GL
                 │                     │
                 └──────────┬──────────┘
                            │
                            ▼
                     CANONICAL EVENTS
                            │
                 ┌──────────┼──────────┐
                 ▼          ▼          ▼
             Logistics     WMS    Reconciliation
```

The governing principle is:

> **Baobab inventory represents the state of goods across ownership, custody, physical location, market, quality, customs, logistics and commercial availability. None of those dimensions may be silently inferred from another.**

For ZuriBeans specifically, this ensures that coffee, wine, vanilla and future products can move:

```text
UG → ZA
ZA → UG
UG → external market
ZA → external market
```

while preserving correct ownership, custody, stock, transit, customs, commercial availability and accounting state.

---

# Decision Outcome

**ACCEPTED WHEN APPROVED**

Implementation SHALL proceed:

```text
ADR-BCP-013
      ↓
Shared Canonical Contracts
      ↓
CP Context / Provider Resolution
      ↓
Trade Commercial Inventory
      ↓
ERP Financial Inventory
      ↓
Transfer / In-Transit
      ↓
Title / Quality / Customs State
      ↓
Events
      ↓
Reconciliation
      ↓
Bidirectional ZuriBeans Validation
```

No production implementation SHALL reduce inventory to `warehouse + SKU + quantity`.