# ADR-BCP-015 — Gate ZB-00 Capability Vocabulary Alignment, Quality-State Ownership and Roadmap Conflict Resolution

**Status:** Accepted — Normative Erratum and Conflict Resolution
**Date:** 2026-09-13
**Decision Owners:** NABHOLD / Baobab Platform Architecture
**Primary Repository:** `nabhold/baobab-cp`
**Affected Repositories:** `nabhold/baobab-cp`, `nabhold/baobab-trade`, `nabhold/zuribeans`, `nabhold/shared`
**Reference Tenant:** ZuriBeans
**Trigger:** ZuriBeans Go-Live Implementation Plan, **Gate ZB-00 — Architectural Decision Freeze**, exit criterion "No P0 architectural ambiguity remains."

**Depends On / Amends:**

- Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification (source of the CR-001…CR-005 conflict-resolution pattern this ADR continues)
- ADR-BCP-011 — Market Participation, Trade Lanes and Cross-Market Trading Model
- ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model
- ADR-BCP-013 — Canonical Inventory Ownership, Custody, Location and In-Transit Model
- ADR-BCP-014 — Canonical Counterparty Identity, Roles and Relationships Model
- `baobab-trade` ADR-0018 (Accepted) and its Accepted Addendum — Multi-Jurisdiction, Cross-Border and Enterprise Tax Architecture
- `baobab-trade` ADR-0019 — B2B Procurement, Supplier Commercial Workflow and Trade-to-ERP Boundary Model
- `baobab-trade` ADR-0020 — B2B Landed Cost, Margin and Commercial Price Resolution Model
- `baobab-trade` ADR-0021 — Customs, Trade Compliance and Regulatory Provider Architecture
- `baobab-trade` ADR-0022 — Shipping, Logistics, Freight and Transport Provider Abstraction
- `zuribeans` Go-Live Implementation Plan §10 (canonical capability taxonomy)
- **Corrected by:** `shared` ADR-SHARED-008 — Gate ZB-01 Capability Domain Registration and Canonical Event-Type Convention Confirmation (registers the `market`/`internal-trade`/`customs`/`tax` domains this ADR's CR-006 relies on, and corrects this ADR's event-naming target from `baobab.*` to `com.nabhold.*`; see §2.1/§2.2 below)

---

## 1. Purpose

The masterplan's **Gate ZB-00** requires all ten P0 architecture questions to be resolved before further implementation. An audit of the nine ADRs listed above — all of which already exist as drafts covering those ten P0 items — found that the *content* is substantively coherent (ownership boundaries, transfer pricing, procurement boundary, and customs/tax/logistics domain separation all agree with each other and with the Technical Specification), but three cross-cutting defects remain that block the exit criterion:

1. Eight independently-authored documents use eight different, mutually incompatible conventions for capability IDs and event names, instead of the one shared vocabulary the Capability Registry (ADR-BCP-003) requires.
2. ADR-BCP-013's capability-ownership matrix internally contradicts its own availability model on who raises and clears a quality hold.
3. ADR-BCP-011's forward roadmap no longer matches where its dependent ADRs actually landed, which risks a reviewer concluding a required ADR is missing when it exists under a different number and repository.

This ADR is **not** a new canonical model. It is a conflict-resolution erratum, in the same spirit as the Technical Specification's CR-001 through CR-005, and it deliberately does **not** rewrite ADR-BCP-011/012/013/014 or `baobab-trade` ADR-0018–0022 in place. Instead it is the single authoritative mapping those documents are amended-by-reference to follow. CR numbering continues from the Technical Specification (CR-001–CR-005) as **CR-006, CR-007, CR-008**.

---

## 2. CR-006 — Canonical Capability and Event Vocabulary Alignment

### 2.1 Problem

The `zuribeans` masterplan §10 defines the target capability taxonomy (`domain.object.verb`, plain dotted segments, 2–3 levels deep) that every engine's capability grants must ultimately resolve against via ADR-BCP-003's Capability Registry. In practice, each ADR that introduces new capabilities invented its own local convention rather than the masterplan's registered strings:

| Domain | Masterplan §10 canonical form | As drafted | Source |
|---|---|---|---|
| Market participation | `market.source`, `market.procure`, `market.sell`, `market.import`, `market.export`, `market.warehouse`, `market.distribute`, `market.fulfil` (§10.2) | Bare enum `MarketParticipationCapability` = `SOURCING`, `PROCUREMENT`, `SELLING`, `IMPORTING`, `EXPORTING`, `WAREHOUSING`, `DISTRIBUTION`, `FULFILMENT` (plus undocumented `LEGAL_PRESENCE`, `PROCESSING`, `TRANSIT`) | ADR-BCP-011 §6/§53 |
| Internal trade | `internal-trade.mirror-document.create` (§10.20) | `internal-trade.document-pair.create`, plus undocumented `settlement.manage`, `transfer-price.approve` | ADR-BCP-012 §42 |
| Trade compliance | `trade.sanctions.screen`, `trade.restricted-party.screen`, `trade.hs-classification.manage` (§10.32) | New `trade-compliance.*` namespace: `trade-compliance.party-screen`, `trade-compliance.product-classify` | ADR-0021 §62 |
| Customs | `customs.customs-value.calculate`, `customs.duty.calculate` (§10.33) | `customs.value`, `customs.duty-assess` | ADR-0021 §62 |
| Shipping vs. logistics | Two distinct families: `shipping.*` (§10.26) and `logistics.*` (§10.27) | Collapsed into one `logistics.*` namespace (`logistics.quote.request`, `logistics.booking.create`, `logistics.shipment.track`) | ADR-0022 §84 |
| Counterparty | `counterparty.role.assign`, `counterparty.relationship.manage` (§10.6) | Hyphenated compound events `counterparty-role.assigned`, `counterparty-relationship.created` | ADR-BCP-014 §88 |
| Inventory | `inventory.dispatch`, `inventory.quality-hold` (§10.21, 2-segment) | 3-segment `inventory.transfer.dispatch`, `inventory.quality.hold` | ADR-BCP-013 §45 |
| Procurement | `procurement.requisition.create`, `procurement.rfq.create`, `procurement.purchase-order.create`, … (§10.11) | No `procurement.*` capability ID used anywhere; modelled purely as domain objects (`SupplierRFQ`, `PurchaseOrder`) with no registry mapping | ADR-0019 (entire document) |

Separately, canonical event naming was itself in dispute at the time this section was first drafted: this ADR originally treated `baobab.<bounded-context>.<aggregate>.<event>.v<major>` (per the Technical Specification's CR-003) as canonical and found ADR-BCP-011/013 conformant against it. **Correction (`shared` ADR-SHARED-008):** `nabhold/shared` — the actual contract authority — already ships and enforces `com.nabhold.<context>.<...>.v<N>` (`contracts/events/v1/envelope.schema.json`) across every real identity/ERP/supplier-onboarding event; the Technical Specification's CR-003 and this ADR's original item 5 below both specified the wrong format. See ADR-SHARED-008 §3 for the authoritative resolution. ADR-BCP-011 and ADR-BCP-013 are **not** conformant to the corrected convention either, despite matching the (incorrect) CR-003 format — all six of ADR-BCP-011/012/013/014 and `baobab-trade` ADR-0019/0020/0021/0022 need their event names expressed in `com.nabhold.*` form, not `baobab.*`.

### 2.2 Decision

1. **The masterplan §10 capability strings are canonical.** Where an ADR introduces a capability the masterplan already names, the ADR's local identifier is superseded by the §10 string. Where an ADR introduces a capability the masterplan does not name (e.g. `LEGAL_PRESENCE`, `PROCESSING`, `TRANSIT` market-participation states; `settlement.manage`; `transfer-price.approve`), the ADR's identifier is accepted into the canonical vocabulary and `nabhold/shared` SHALL add it to §10's registry on next revision, since these are legitimate gaps in the masterplan's enumeration rather than naming drift.
2. **`shipping.*` and `logistics.*` remain two distinct capability families**, per the masterplan. ADR-0022's collapse into a single `logistics.*` namespace is superseded: shipping (carrier/mode/booking/rate, customer- and shipment-facing) and logistics (dispatch/load/route/border/POD, execution-facing) SHALL be registered as separate families even where one provider adapter implements both.
3. **ADR-0019 SHALL register its procurement workflow against the masterplan's `procurement.*` capability set.** Every domain object the ADR defines (`SupplierRFQ`, `PurchaseOrder`, goods receipt, invoice match, etc.) maps onto an existing §10.11 capability (e.g. `procurement.rfq.issue`, `procurement.purchase-order.create`, `procurement.goods-receipt.manage`, `procurement.invoice-match`); none of these need new capability IDs, only the registry mapping the ADR currently omits.
4. **`nabhold/shared` becomes the single point of registration.** Every capability ID used by any Baobab engine SHALL be published in Shared's capability taxonomy before a `CapabilityGrant` referencing it may be issued (this is already implied by ADR-BCP-003 and the Technical Specification §13; this ADR makes it explicit that the vocabulary conflicts above are what that registration step must resolve, not something CapabilityGrant issuance can paper over).
5. ~~All events raised by ADR-BCP-012, ADR-BCP-014, and `baobab-trade` ADR-0019/0020/0021/0022 SHALL be renamed to the canonical `baobab.<bounded-context>.<aggregate>.<event>.v<major>` form before those ADRs are accepted.~~ **Superseded by ADR-SHARED-008 §3.** The canonical form is `com.nabhold.<context>.<...>.v<N>`, per Shared's already-shipped and enforced `contracts/events/v1/envelope.schema.json`. Corrected example renamings: `supplier.registered` → `com.nabhold.procurement.supplier.registered.v1`; `commercial-price.resolved` → `com.nabhold.commercial.price.resolved.v1`; `shipment.created` → `com.nabhold.trade.shipment.created.v1`; `counterparty.merged` → `com.nabhold.counterparty.counterparty.merged.v1`. ADR-BCP-011 and ADR-BCP-013's existing event names (previously believed conformant) require the same correction.

### 2.3 Remediation Mechanism

This ADR does not itself edit ADR-BCP-011/012/013/014 or ADR-0018–0022. Instead:

- Each of those ADRs SHALL carry a one-line pointer added at acceptance time: *"Capability IDs and event names in this document are superseded where they conflict with ADR-BCP-015 §2."*
- `nabhold/shared` owns producing the actual registry entries (capability taxonomy + AsyncAPI event definitions) reflecting the resolved names in the table above; no runtime repository resolves this itself.
- Implementation (migrations, resolver code, event publishers) SHALL be written against the canonical names from the outset — the naming drift SHALL NOT be encoded into schemas or contracts under the assumption it will be fixed later.

---

## 3. CR-007 — Quality-State Ownership

### 3.1 Problem

ADR-BCP-013's capability-ownership matrix (§46) assigns quality state to "ERP/quality provider." However, the same document's Available-to-Promise formula (§36) and its "Commercial vs. Financial Inventory" discussion (§49) both treat a cleared quality hold as a precondition for *commercial* availability — which every other document in this set (ADR-0019 §40/§125, ADR-0021 §53) treats as a Trade-side concern, since ATP/availability is Trade's runtime responsibility (per the Technical Specification's engine-ownership principle and the masterplan's Engine Ownership Matrix, §11: "Inventory availability → Trade"). As drafted, no document states unambiguously which engine raises or clears the hold that actually blocks a sale.

### 3.2 Decision

Quality-state ownership SHALL be split, not assigned wholesale to one engine:

```text
Quality Decision Authority          Availability Gating
(what the lab/inspector determined) (what blocks a sale)
        │                                    │
        ▼                                    ▼
   ERP / quality provider              Trade (Medusa / baobab-trade)
   - records inspection result         - raises inventory.quality-hold
   - records disposition               - clears inventory.quality-hold
     (accept/reject/rework)              only on receipt of a cleared
   - owns quality certificates           decision reference from ERP/
   - owns valuation consequence          the quality provider
     of a reject/write-down            - reflects the hold in ATP
                                          (§36) immediately
```

- **ERP or the designated quality provider is authoritative for the quality *decision*** — the inspection result, the disposition, the certificate, and any resulting valuation/costing consequence (write-down, quarantine cost allocation). This preserves ADR-BCP-013 §46 for the decision itself.
- **Trade is authoritative for quality-driven *availability gating*** — the same `inventory.quality-hold` / `inventory.quality-release` capability (§10.21) that already gates ATP everywhere else in ADR-BCP-013 and in ADR-0019/ADR-0021. Trade raises the hold when a lot enters an unqualified state and clears it only by reference to a decision recorded by ERP/quality provider; Trade never overrides or re-derives the decision itself.
- This is consistent with, and requires no change to, ADR-BCP-013 §36's ATP formula — it resolves the contradiction by making explicit what §36 already assumed.

### 3.3 Remediation Mechanism

ADR-BCP-013 §46's capability matrix SHALL be corrected at acceptance time to read: *"Quality decision → ERP/quality provider; Quality-driven availability gating → Trade (see ADR-BCP-015 §3)."* No other document in this set requires a change, since ADR-0019 and ADR-0021 already assumed this split without stating it.

---

## 4. CR-008 — ADR-BCP-011 Roadmap Erratum

### 4.1 Problem

ADR-BCP-011 §93 projects a roadmap in which ADR-BCP-012 through ADR-BCP-023 all land in `baobab-cp`, including a planned "ADR-BCP-014 — Procurement Ownership… Trade-to-ERP Boundary" and planned ADR-BCP-015/016/017/018 for pricing, customs, logistics, and tax. In practice:

- ADR-BCP-012 (Intercompany) and ADR-BCP-013 (Inventory) landed as planned.
- Procurement, pricing, customs, logistics, and the tax addendum instead landed in `baobab-trade` as ADR-0019, ADR-0020, ADR-0021, ADR-0022, and the ADR-0018 Addendum respectively — a different repository and numbering sequence than §93 states.
- The planned "canonical counterparty" ADR landed as ADR-BCP-014 in `baobab-cp` — correct repository, but not the number §93 anticipated.

None of this is a semantic contradiction; the content exists and is coherent. It is a documentation-tracking defect: a reviewer checking Gate ZB-00 against ADR-BCP-011 §93 literally will not find the ADRs where §93 says they will be, and may wrongly conclude a P0 item is unaddressed.

### 4.2 Decision

ADR-BCP-011 §93 SHALL receive an erratum at acceptance time reconciling its projected roadmap with actual placement:

```text
ADR-BCP-011 §93 ERRATUM:
Procurement Boundary  → landed as baobab-trade ADR-0019 (not baobab-cp ADR-BCP-014)
B2B Pricing/Landed Cost → landed as baobab-trade ADR-0020
Customs/Trade Compliance → landed as baobab-trade ADR-0021
Logistics Provider Model → landed as baobab-trade ADR-0022
Multi-Jurisdiction Tax → landed as baobab-trade ADR-0018 Addendum
Canonical Counterparty  → landed as baobab-cp ADR-BCP-014 (number reassigned)
```

This ADR (ADR-BCP-015) itself continues the `baobab-cp` numbering sequence from ADR-BCP-014, and should be treated by future ADRs as the authoritative index of where each Gate ZB-00 P0 item actually lives, superseding ADR-BCP-011 §93 for that purpose.

---

## 5. Gate ZB-00 Exit Assessment

With CR-006, CR-007, and CR-008 applied (as pointers/errata, not rewrites):

| Gate ZB-00 P0 item | ADR | Content ambiguity | Vocabulary conflict |
|---|---|---|---|
| Trade Lane Model | ADR-BCP-011 | none found | resolved by CR-006 |
| Market Participation Model | ADR-BCP-011 | none found | resolved by CR-006 |
| Intercompany/Inter-Branch Trading | ADR-BCP-012 | none found | resolved by CR-006 |
| Inventory Ownership/In-Transit | ADR-BCP-013 | resolved by CR-007 | resolved by CR-006 |
| Procurement Boundary | ADR-0019 | none found | resolved by CR-006 |
| B2B Pricing/Landed Cost | ADR-0020 | none found | none found |
| Customs/Trade Compliance | ADR-0021 | none found | resolved by CR-006 |
| Logistics Provider Model | ADR-0022 | none found | resolved by CR-006 |
| Multi-Jurisdiction Tax | ADR-0018 + Addendum | none found | none found |
| Counterparty Model | ADR-BCP-014 | none found | resolved by CR-006 |

No item retains unresolved architectural ambiguity, and the procedural prerequisite is also satisfied: ADR-BCP-011/012/013/014, this document, the ADR-0018 Addendum, and ADR-0019/0020/0021/0022 are all **Status: Accepted**. Gate ZB-00's exit criterion — "no P0 architectural ambiguity remains" — is met for all ten P0 items.

---

## 6. Consequences

- No schema, migration, or resolver code SHALL be written against the pre-CR-006 capability or event names in ADR-BCP-011/012/013/014 or ADR-0019/0020/0021/0022; implementers use the canonical forms from §2.2 directly.
- `nabhold/shared` carries an action item to publish the resolved capability taxonomy and event catalogue entries from §2.2 before Phase 1 (Technical Specification §88, "Programme Gate P1 — Shared Contract Foundation") schema work depending on them proceeds.
- ADR-BCP-013 carries an action item to correct §46 per §3.3 above at acceptance time.
- ADR-BCP-011 carries an action item to correct §93 per §4.2 above at acceptance time.
- This ADR and the nine it amends were accepted together; Gate ZB-00's architectural-freeze exit criterion is closed as of this document's acceptance.
