# ADR-BCP-027 — Organisation-First Tenancy, Operating Business, Trading Style and Legal-Actor Responsibility Model

**Status:** Accepted — Normative Platform Architecture (2026-10-09)  
**Decision approval:** Accepted at the platform decision owner's direction on 2026-10-09. Independent approval of each real operating mandate and implementation/security certification remain required.  
**Date:** 2026-10-09  
**Decision owners:** Baobab Platform Architecture / Nabhold Group Governance / Control Plane / Enterprise Integration / Security  
**Primary repository:** \`baobab-platform/baobab-cp\`  
**Canonical contract authority:** \`baobab-platform/shared\`  
**Runtime authority:** Baobab Control Plane  
**Legal identity/evidence authority:** CP's canonical Organisation and evidence model, using independently verified sources and authorised business declarations  
**Identity authority:** \`baobab-platform/baobab-iam\`  
**Finance, tax-document and posting authority:** \`baobab-platform/baobab-erp\` (iDempiere), within approved legal-actor context  
**Commerce authority:** \`baobab-platform/baobab-trade\` (MedusaJS), within approved seller/contracting context  
**Initial markets:** South Africa, Uganda  
**Decision type:** Foundational amendment and compatibility migration for \`legal_entity_id\`, trading businesses and independent tenants  
**Refines:** ADR-BCP-004, ADR-BCP-012, ADR-BCP-017, ADR-BCP-018, ADR-BCP-019, ADR-BCP-023, ADR-BCP-024 and BCP-TS-ONBOARDING-001; Shared tenancy and registration contracts  
**Related accepted decision:** ADR-BCP-026 — Progressive Enterprise Onboarding, Founding-Group Exemption and Capability-Specific Evidence Governance. Both accepted decisions SHALL be read together, with Shared contract changes and runtime certification still outstanding.  
**Retains:** ADR-BCP-003/005/009/011, ADR-BCP-020/021/022, ADR-SHARED-012/013/014/015, ADR-IAM-0033; existing isolation, explicit entitlement, governance and approval controls.  
**Implementation status:** Accepted design decision only; implementing contracts, migrations, code, real operating mandates and live tenant changes are pending. This ADR does not itself modify runtime state.

---

## 1. Executive decision

Baobab SHALL make **the canonical Organisation the required primary business identity of every Tenant**, while treating legally responsible actors and \`legal_entity_id\` as independently governed, operation-specific relationships.

A tenant MAY represent:
- An incorporated legal enterprise trading in its own name;
- An independently administered operating business, division or trading style acting under a different, established legal person;
- A lawful sole-proprietor or other recognised business form using the appropriate real legal actor;
- A pre-incorporation venture which is admitted for permitted capabilities but has no falsely manufactured company identity.

A founding operating business such as **ZuriBeans** may retain its own Organisation, Tenant, digital estate, administrators, product configuration, markets and operations while **Nabhold Group Africa** acts as the legally responsible seller, importer, contracting party or accounting entity for those particular activities that Nabhold is demonstrably authorised and legally capable of undertaking.

**Operational autonomy does not create separate legal personality. A common legal person does not combine tenants or grant access. A trading name is not a LegalEntity.**

The old \`Tenant.LegalEntityID\` and \`tenants.legal_entity_id\` SHALL be evolved into a nullable, explicitly derived compatibility projection of a valid in-effect DEFAULT \`TenantLegalEntityMapping\` where one exists. They SHALL NOT define the Tenant's primary identity, SHALL NOT be guessed, and SHALL NOT be universally mandatory for platform admission.

Capabilities requiring a legal actor SHALL resolve a **purpose-, role-, market- and time-bound legal responsibility mapping** (the \`OperatingLegalActorMandate\` conceptual boundary below) in addition to tenant/organisation context. Ambiguous, expired, unevidenced or unauthorised mappings SHALL fail closed for the affected action, not automatically for unrelated tenant capabilities.

This ADR changes **identity and responsibility modelling**, not statutory legal duties, commercial subscription prices or provider readiness requirements.

### 1.1 Founding-group declared incorporation and legal actor status (2026-10-09)

The current first-party onboarding programme SHALL use the following **group-governance declarations**. They are not independent legal-verification decisions or automatically valid operating mandates.

| Business | Separately incorporated today? | Identity and group relationship | Responsibility during transition |
|---|---|---|---|
| Nabhold Group Africa | Group declares South African incorporation | Holding company/founding principal | Own legal obligations; may assume specific authorised operating-business roles |
| ZuriBeans | **No** | Independently operating trade business formerly Nabhold cross-border trade unit | Nabhold is proposed responsible legal actor only for documented authorised role/market/activity combinations |
| Equator & Estate Co. | **No** | Independently operating property/construction/hospitality business awaiting incorporation | Nabhold is proposed responsible legal actor only for documented authorised role/market/activity combinations |
| Thamani Global | Group declares CIPC registration | Independently incorporated Nabhold subsidiary; former logistics-unit history | Ordinarily its own legal actor for new business; Nabhold affiliation does not transfer legal liability automatically |

**Terminology:** ZuriBeans and Equator & Estate Co. SHALL be treated as *founding operating businesses* rather than represented as legally incorporated subsidiaries. Thamani may be described as a declared incorporated subsidiary; official registration and corporate ownership/control SHALL be independently supported before being marked VERIFIED.

**Specific, not blanket responsibility:** The group intends Nabhold to assume "most responsibility" for the two unincorporated businesses. That intention SHALL become a matrix of separately governed mandates for each role (e.g. contracting party, legal issuer, importer/exporter, employer, property owner, seller, payment beneficiary), activity, market, effective window, representative authority, supporting evidence, restrictions and revocation. No authority for property title, building permits, import/export permissions or third-party settlement is inferred from sponsorship or a default legal-entity mapping.

**Operational autonomy:** Each business retains its own PRIMARY Organisation, justified tenant, digital estate, IAM scope and market choices. Sharing the Nabhold LegalEntity for approved roles SHALL NOT share tenant data access, automatically make intercompany transactions, or reassign an independent operating business's primary Organisation.

**Evidence and migration:** The stable first-party IDs in Shared remain intact for compatibility. Audit old LegalEntityProfiles and VERIFIED records against the actual claims/evidence. Correct status only through governed, logged and history-preserving transitions. Never generate a fictitious incorporated person for ZuriBeans or Equator & Estate.

## 2. Historical and commercial context

Nabhold originally planned independently operated **business units** under the Nabhold legal name. Its cross-border trade business unit became **ZuriBeans**; logistics operations evolved into **Thamani**, which now has a broader independent business identity. These are meaningful stages in the same business evolution:

~~~text
Nabhold Group Africa — legal person / parent
    |
    +-- Cross-border trade business unit -> ZuriBeans trading/operating business
    |
    +-- Logistics business unit -> Thamani operating business
    |
    +-- Other operating units and later subsidiaries
~~~

Subsequent corporate incorporation does not rewrite the earlier legal nature of transactions. At time T1, business unit A may operate for legal person X; at time T2, a newly incorporated company Y may become legally responsible for new transactions after valid authorisation and handover.

The platform needs both **continuity of business identity** and **truthful attribution of legal accountability**. It must not force every distinct digital estate to be a distinct legal person, nor force a business operating under another legal person to share that person's tenant.

This is central to the original first-party mission and to an African enterprise platform capable of serving multiple business-formation stages without unnecessary friction.

## 3. Existing architecture and exact incompatibilities

Current repository evidence (inspect fresh commits before implementing):

| Current element | Observed contract/code | Required correction |
|---|---|---|
| Shared \`contracts/control-plane/v1/tenant-registration.schema.json\` | Requires \`legal_entity_id\` on every tenant command | Require authoritative \`organisation_id\`; permit absent \`legal_entity_id\` when no DEFAULT legal actor is justified |
| Shared \`contracts/tenancy/tenancy.yaml\` | Requires default legal-entity mapping at all times and reports its loss as integrity failure | Require in-effect PRIMARY Organisation; require legal-actor mapping only when needed for selected operations |
| CP \`internal/domain/tenant.go\` | \`RegisterTenant.Validate\` requires LegalEntityID | Versioned Organisation-first validation; fail closed on falsely asserted legal actor |
| CP \`internal/store/postgres/store.go\` | Inserts into \`legal_entities\` and persists \`tenants.legal_entity_id\` on registration | Only persist a real legal actor when supplied and authorised; atomic primary Organisation mapping |
| CP \`internal/store/postgres/organisation_register.go\` | Can create unverified Organisation AND LegalEntityProfile from registration input | Never automatically create a legal-person profile merely because a tenant is requested |
| CP \`internal/domain/relationship_resolution.go\` | DEFAULT legal entity is the compatibility projection | Retain this derivation; allow absence when policy permits, detect ambiguity |
| CP \`internal/repository/postgres_organisation_integrity.go\` | Enforces projection equality with DEFAULT mapping | Extend to primary-Organisation integrity and nullable/no-default semantics |
| CP \`internal/service/organisation/admission.go\` | Ordinary external admission expects at least one registration identifier, one legal entity and verification claims around incorporation | Support lawful unincorporated organisations and a purpose-specific legal actor without fictitious identifiers |
| CP \`internal/service/organisation/provision.go\` | Requires LegalEntityID and writes default legal-entity mapping | Separate organisation provisioning from optional legal-actor attachment |
| CP \`internal/service/organisation/firstparty_reconciler.go\` | Infers primary Organisation from the tenant's DEFAULT legal entity | Prohibit this inference: a ZuriBeans tenant may legitimately use Nabhold's legal entity |
| CP \`internal/repository/postgres_organisation_governance.go\` | Can mark registry-listed first-party legal profiles VERIFIED from the Shared registry digest | Registry inclusion verifies first-party governance identity, not by itself legal incorporation or company registration |
| CP \`internal/service/context_resolution_service.go\` | Treats \`Tenant.LegalEntityID\` as always present | Resolve tenant and PRIMARY Organisation first; attach legal actor only with scoped, authorised mapping |
| CP \`internal/erpprovisioning/client.go\` | Finance baselines and ERP assignments identify LegalEntityIDs | LegalEntity remains mandatory for postings/company provisioning; no synthetic ERP legal person |
| Shared first-party \`contracts/legal-entity/registry.yaml\` | Lists NABHOLD, ZURIBEANS, THAMANI-GLOBAL, EQUATOR-ESTATE as legal-entity IDs, but subsidiary evidence is incomplete | Distinguish stable first-party business identity from independently established legal-person status, without casually renaming existing IDs |

The current accepted ADR-BCP-012 already states \`Tenant != LegalEntity != Branch != BusinessUnit\` and requires distinguishing same-entity operations from intercompany transactions. This ADR **extends that separation to tenant identity, trading style and incorporation transitions**; it does not supersede the accounting classification rules.

Existing ADR-BCP-004 defines optional BusinessUnits and says they should not become tenants **merely because** they need separate capability configuration. This ADR clarifies that a deliberate standalone tenant is allowed when an independently governed operating business genuinely requires its own isolation, administration, lifecycle and digital estates. It is not an automatic tenant-per-department rule.

## 4. Canonical concepts and hard invariants

### 4.1 Identity dimensions

| Concept | Answers | Canonical relation |
|---|---|---|
| Organisation | Who is this operational/business organisation? | Primary business identity |
| BusinessUnit | Which internal operating subdivision/cost or management centre? | May belong to an Organisation or legal actor; not automatically a tenant |
| TradingStyle / OperatingBrand | What public/business name does the business use? | Associated with operating Organisation; evidence of permission/ownership when necessary |
| LegalEntity | Which established legal person bears a particular obligation? | May serve several Organisations and Tenants |
| OperatingLegalActorMandate | Why and for which actions may that legal person act for this operating business? | Independently governed relationship |
| Tenant | What is the canonical technical/administrative isolation boundary? | Has one PRIMARY Organisation; may have multiple legal actor mappings |
| PlatformAccount | Who covers the commercial/administrative platform relationship? | Not an IAM boundary |
| CorporateRelationship | Who owns/controls whom? | Not permission to issue an invoice or access a tenant |
| ProductSubscription | Which product rights exist? | Not proof of legal capacity |
| Business transaction | Who legally sold/bought/imported/invoiced at the time? | Immutable/reconstructible effective-date attribution |

### 4.2 Normative invariants

1. **Every non-migrating operational Tenant SHALL resolve exactly one active, in-effect PRIMARY Organisation.**
2. An Organisation MAY exist before a separate corporate LegalEntityProfile exists.
3. One LegalEntity MAY legitimately be responsible for operations conducted by multiple independently isolated tenants; this fact creates no cross-tenant membership or read privilege.
4. \`organisation_id\`, \`tenant_id\`, \`legal_entity_id\`, \`business_unit_id\` and trading-style identifiers are NOT interchangeable.
5. A \`TradingStyle\` is NOT an independent legal person; its relationship to the legally responsible actor must be explainable to counterparties wherever applicable.
6. An operating mandate SHALL be scoped by principal/approved authority, legal actor, operating Organisation, permissible activity/role, jurisdiction/market, validity window and supporting evidence.
7. A corporate \`OWNS\`/\`CONTROLS\` relationship or Shared first-party registry entry alone is NOT an operational mandate.
8. \`legal_entity_id\` in a financial or legal operation MUST resolve to a real, properly identified legal person, not a placeholder.
9. A LegalEntityProfile MAY be UNVERIFIED as a claim but SHALL NOT imply official incorporation. A profile for a non-existent separate company SHALL NOT be created merely to satisfy an API.
10. An operational business may have an isolated tenant even where its seller/invoice issuer is another legal entity.
11. ERP financial postings require an actual responsible legal actor and valid finance baseline; a business unit/tenant never substitutes for a legal entity.
12. Previous legal responsibility is not retroactively changed by subsequent incorporation, rebranding or revised mappings.
13. No universal 12-month legal waiver exists: ADR-BCP-026 defers specific *platform evidence*, not law or provider conditions.
14. The zero-priced founding-group INTERNAL classification remains subject to its own authoritative policy; trading under Nabhold does not create a shortcut to INTERNAL eligibility.
15. Provider health, subscription, IAM and tenant readiness remain separate from legal-person status.

## 5. Reference topology — Nabhold trading as ZuriBeans

~~~mermaid
flowchart TD
 N["Nabhold Group Africa<br/>LegalEntity (when independently verified)"]
 Z["ZuriBeans<br/>Operating Organisation / TradingStyle"]
 T["Thamani Global<br/>Operating Organisation"]
 NT["Nabhold Tenant"]
 ZT["ZuriBeans Tenant"]
 TT["Thamani Tenant"]
 M["Time-bound OperatingLegalActorMandate<br/>Scope: market + activity + legal role"]
 N --> NT
 Z --> ZT
 T --> TT
 N --> M
 M --> Z
~~~

**Example, conditional rather than automatically authorised:** Nabhold Group Africa carries on business under the public trading name “ZuriBeans” and authorises ZuriBeans as its operating division for specified B2B transactions while ZuriBeans' own incorporation remains pending.

The parties must confirm that this trading arrangement, brand use, contracting authority, tax identity, goods/permits and market-specific activities are legally supportable. This ADR is not itself legal advice and creates no statutory entitlement.

The separate ZuriBeans Tenant hosts its operating data, users, digital estate and permissions. The Nabhold LegalEntity may serve as legal issuer or contracting party for the *specific* permitted transaction. **Nabhold tenant administrators receive no automatic access to ZuriBeans tenant data**; only explicit scoped IAM/CP delegation can confer any such access.

### 5.1 Trade display and legal disclosure

When an operational document, checkout page or signed contract must identify the actual legal person, the integration SHALL resolve the legal actor and use the jurisdiction-appropriate legal identity particulars. A possible style, **only after approved legal review of the exact wording**, is:

> ZuriBeans — a trading business of Nabhold Group Africa (Pty) Ltd.

Contracts and invoices SHALL identify the actual seller/supplier/issuer required by applicable law and provider contract, not simply display “ZuriBeans (Pty) Ltd” while that company does not exist. TradingStyle visibility shall never conceal the real liable entity where identification is required.

South African SARS guidance recognises that a trading name may differ from a business's legal name and that VAT invoices require prescribed supplier/recipient particulars when applicable. See:
- https://www.sars.gov.za/guide-to-completing-the-value-added-tax-vat201-return/
- https://www.sars.gov.za/types-of-tax/value-added-tax/obligations-of-a-vat-vendor/
- https://www.sars.gov.za/wp-content/uploads/Ops/Guides/Legal-Pub-Guide-VAT404-VAT-404-Guide-for-Vendors.pdf

These are technical design references, not a legal ruling on whether Nabhold may conduct a specific cross-border trade. Confirm trade/export/import, licences, tax and supplier-of-record requirements per transaction/corridor before live execution.

## 6. The legal actor is an *operating mandate*, not the tenant owner

Define a conceptual **OperatingLegalActorMandate** (final name/contracts determined by Shared gap analysis):

~~~text
OperatingLegalActorMandate
  id                        -- opaque minted identifier
  operating_organisation_id -- ZuriBeans
  responsible_legal_entity_id -- Nabhold, if valid and verified for required purpose
  legal_actor_organisation_id -- Nabhold Organisation reference where applicable
  authority_basis_reference -- reviewed resolution/mandate/contract
  legal_capacity/role        -- seller, supplier, importer, exporter, contract party, employer, invoicing party etc.
  permitted_activities[]    -- only actual approved activities
  market_scope[]            -- ZA / UG only as supported
  product_capability_scope[] -- bounded optional scopes
  effective_from
  effective_to
  status
  verification_assessment_reference
  approved_by / approved_at
  supersedes_id
  audit/correlation references
~~~

The names are conceptual; no new status enums or capability keys shall be invented before validating Shared contracts. This record must not duplicate the actual corporate ownership graph, PlatformRelationship or accounting authority.

A legal actor can have distinct mandates for B2B sale, tax invoicing, import, export and contracting, and may be eligible for some but not all of them. The same-market/same-activity role MUST resolve uniquely for an operation or fail closed with an actionable conflict; never silently pick the newest or lexically first row.

### 6.1 Responsibility matrix

| Domain action | Operating context | Legal responsibility needed? | Rule |
|---|---|---|---|
| Identity/account creation | ZuriBeans Organisation | Not necessarily a separate incorporated company | Minimum validated applicant and authorised sponsor |
| Catalogue drafting | ZuriBeans Tenant | Not inherently a company registration | Tenant IAM + grants |
| Market intelligence | ZuriBeans Tenant | Contract/licence restrictions as relevant | Product/provider scope |
| Supplier or buyer contracting | ZuriBeans Tenant | Yes, for contracting legal party | Resolve valid mandate and actor |
| Tax invoice | ZuriBeans commercial action | Yes, actual issuer/vendor as applicable | ERP/provider must validate tax identity/issuer requirements |
| Bank/payment settlement | ZuriBeans commercial action | Yes, approved merchant/beneficiary of record | Provider policy + exact legal actor |
| Cross-border import/export | ZuriBeans market activity | Yes, importer/exporter of record and applicable conditions | Evaluate corridor/activity-specific legal actor |
| ERP general ledger posting | ZuriBeans cost/profit centre | Yes, accounting legal person | ERP uses Nabhold legal-entity company while mandate active, if approved |
| Employee/payroll operations | Responsible employer | Yes, employment actor and applicable payroll requirements | Do not infer employer solely from tenant owner |
| Internal transfers | Legal entities at each end | Yes for legal/accounting classification | Same actor vs distinct actors under ADR-BCP-012 |

A business operation's *public trading name* and *legal issuer* may be different values and both must remain reconstructible in issued records.

## 7. BusinessUnit versus independently isolated Tenant

Two valid deployment/business arrangements SHALL remain available:

### 7.1 Business unit inside the parent's tenant

~~~text
Nabhold Tenant
   |
   +-- Cross-border Trade BusinessUnit
   +-- Logistics BusinessUnit
~~~

Use where only internal cost/profit management, delegated workflows or configuration separation is required. ADR-BCP-004 remains authoritative: a BusinessUnit does not automatically need a new tenant.

### 7.2 Independent operating-business tenant under a parent legal actor

~~~text
Nabhold LegalEntity
      |
      +-- legally responsible for scoped business activities
             |
             +-- ZuriBeans Organisation -> ZuriBeans Tenant
             +-- [other authorised operating business] -> its own Tenant
~~~

Use where operating autonomy, brand, digital estate, security boundary, administration, partner relationships, market footprint or future incorporation requires stronger separation.

**Decision criterion:** Distinct Tenant only where justified by governance and isolation, not merely by department count or corporate hierarchy.

A tenant is not a legal-person boundary. Two isolated tenant datasets must not be aggregated as two legal corporate persons in consolidated finance merely because they are different tenants.

## 8. Enhanced tenant and relationship contracts

### 8.1 Canonical tenant identity

The future Shared tenant-registration contract SHALL accept a canonical \`organisation_id\` naming a live, approved PRIMARY Organisation and SHALL not universally require \`legal_entity_id\`.

Conceptual, not yet a valid contract:

~~~json
{
  "tenant_onboarding_request_id": "tor_<opaque>",
  "organisation_id": "<canonical-organisation-uuid>",
  "display_name": "ZuriBeans",
  "isolation_strategy": "schema_per_tenant",
  "residency_region": "af-south-1",
  "requested_products": []
}
~~~

If a legally responsible actor is selected for the whole tenant, CP SHALL establish an explicit DEFAULT legal-entity mapping to a real LegalEntity, while preserving a distinct PRIMARY Organisation. More specific or different actor roles for individual transactions SHALL be governed by mandate records, not by overloading the DEFAULT mapping.

The original \`Tenant.LegalEntityID\` is a **nullable backwards-compatibility projection**, not the authoritative source of operating identity and not a globally correct seller-of-record for every transaction.

### 8.2 Mapping invariants

The following SHALL be supported without identity collision:

~~~text
TenantOrganisationMapping:
  tenant_id = ZuriBeans Tenant
  organisation_id = ZuriBeans Organisation
  mapping_role = PRIMARY_ORGANISATION

TenantLegalEntityMapping:     # if default legally authorised
  tenant_id = ZuriBeans Tenant
  legal_entity_id = NABHOLD
  mapping_role = DEFAULT

OperatingLegalActorMandate:
  operating_organisation_id = ZuriBeans Organisation
  responsible_legal_entity_id = NABHOLD
  activities = specifically authorised
  effective window = approved window
~~~

The active PRIMARY Organisation SHALL **not** be looked up by assuming that a LegalEntityProfile's owning Organisation equals the tenant's operating Organisation. That existing inference is unsafe for this use case and must be amended in registration, first-party reconciliation and attestation paths.

### 8.3 Scope resolution and Context

Resolver ordering:

~~~mermaid
flowchart TD
 A[Authenticated principal] --> B[Resolve tenant and membership]
 B --> C[Resolve one PRIMARY operating Organisation]
 C --> D[Resolve market, DigitalEstate and entitlement]
 D --> E{Does action require a legal actor?}
 E -->|No| F[Execute ordinary eligible capability]
 E -->|Yes| G[Find approved mandate by purpose/role/market/time]
 G --> H{Exactly one valid responsible actor?}
 H -->|No| I[Fail closed: missing/ambiguous/expired actor]
 H -->|Yes| J[Resolve legal entity and provider rules]
 J --> K{Authorised and ready?}
 K -->|No| L[Fail closed for this operation]
 K -->|Yes| M[Execute with legal actor and provenance]
~~~

Contexts SHALL carry separately:
- canonical \`tenant_id\`;
- PRIMARY \`organisation_id\`;
- requested and resolved market/estate/capability;
- **optional** resolved \`legal_entity_id\` where required;
- immutable/reconstructible legal-actor mandate and decision provenance for consequential actions.

The client SHALL NOT provide a legal actor as an unrestricted override. A requested actor must be attested to the authorised mandate and context, with fail-closed ambiguity and no cross-tenant leakage.

## 9. Financial, tax, inventory and contractual boundaries

**ERP remains the finance authority.** For ZuriBeans operating legally under Nabhold, where such an arrangement is valid, the ERP may configure:
- one Nabhold legal-entity/company ledger and its approved finance baseline;
- separate ZuriBeans BusinessUnit, cost centre, profit centre, analytics or other supported management dimensions;
- a ZuriBeans operational tenant/external-reference mapping without fabricating a separate statutory company ledger.

Do not assume a capability exists in iDempiere solely because the architecture describes it. Provider support must be declared, live, tested and appropriately published.

The ERP and Trade adapters SHALL preserve:
- legal seller/purchaser/issuer of record;
- TradingStyle/operating brand and business unit;
- VAT/tax identifier of the actual applicable person when required;
- bank/merchant beneficiary, ownership/title and transport/document references where relevant;
- currency and jurisdiction/corridor context;
- source mandate version/effective timestamp;
- traceability to the operation, tenant, commercial document and audit trail.

If ZuriBeans and another division both act under the same Nabhold LegalEntity, an exchange between those divisions is not automatically an **intercompany** sale. Apply ADR-BCP-012 classification by the *actual legal entities at both sides*, and ensure transfers, allocations and eliminations are treated appropriately. Never count same-company cross-tenant movements as independent company revenue by default.

A customer-facing or statutory document must disclose legally required true particulars. The policy shall not automatically assert that any specific public trading-style wording meets all South African, Ugandan or cross-border disclosure laws.

## 10. Transition from operating business to independent company

**Business identity continuity without retroactive legal-person rewriting** is the migration goal.

~~~mermaid
flowchart TD
 A["Stage 1: ZuriBeans Organisation + own Tenant"] --> B["Nabhold mandated for selected roles/markets"]
 B --> C["Stage 2: incorporation and evidence verification"]
 C --> D["Create/verify ZuriBeans LegalEntityProfile"]
 D --> E["Approve new mandates and future effective date"]
 E --> F["ERP/Trade/Payment provider readiness and cutover"]
 F --> G["New transactions use ZuriBeans legal actor where authorised"]
 B -.-> H["Historic obligations remain attributed to Nabhold"]
 G --> I["Same Organisation ID and Tenant ID"]
~~~

### 10.1 Mandatory transition procedure

1. Register the new legal entity from independently verified legal evidence; preserve any pre-existing Organisation, trading names and stable business references.
2. Establish a legally reviewed effective date, scope and assignment/novation plan for contracts, assets, licences, obligations, inventory and counterparties as applicable. **Incorporation itself does not automatically transfer existing contracts, debts, permits or invoices.**
3. Provision ERP legal-entity company/finance baselines and other provider-native resources through approved CP desired-state convergence.
4. Obtain provider confirmations (including merchant/payments and market-specific trade permissions) before enabling the new actor for an operation.
5. Create the new dated legal-actor mandate and end/supersede the old mandate for future operations as appropriate.
6. Reconcile open orders, returns, credits, tax invoices, payables, inventory title, customers, contracts and legally required historical records. Assign special treatment where a legal novation or additional consent is required; do not silently mutate them.
7. Run cutover with reversible operational features where feasible, dual-read/reference mapping as needed, and signed-off reconciliation.
8. Preserve canonical \`organisation_id\` and \`tenant_id\`, historical mandates and documents, full audit and provenance.
9. Review founding-group INTERNAL classification and the continuing parent relationship separately; independent incorporation does not imply an automatic loss of group affiliation or entitlements, nor automatic eligibility.
10. Keep existing tenants isolated throughout.

### 10.2 Legal-entity changes are not CRUD renames

An update of a legal actor SHALL NOT simply overwrite \`tenants.legal_entity_id\`, regenerate tenant IDs, rewrite historic invoices or re-issue provider IDs. It is a governed, effective-dated relationship transition supported by explicit reconciliation.

Historical queries must be capable of answering, **for transaction date T**, which operating Organisation, Tenant, trading style, seller/issuer and legal actor applied *then*, using the stored decision reference and historical relationship records—not only today's mapping.

## 11. Founding-group evidence and registry corrective action

Shared \`contracts/legal-entity/registry.yaml\` currently lists founding businesses using established first-party identifiers. These stable IDs are used by existing consumers and SHALL NOT be abruptly removed or renamed.

However, **an approved registry identity or product-consumption intent is not by itself independent proof that a separately incorporated LegalEntity exists**.

Implementation SHALL:
1. Inventory every Shared first-party ID and its evidential basis.
2. Introduce an explicit, contract-defined distinction between **governed first-party Organisation identity**, **legal-person claim**, and **independently verified legal-person status**.
3. Make first-party reconciliation create/reconcile a business Organisation without automatically manufacturing or marking a separately incorporated LegalEntityProfile VERIFIED from registry membership alone.
4. Preserve actual official evidence of Nabhold or Thamani incorporation where available and sufficiently verified; do not downgrade correctly verified claims by blanket script.
5. Identify any existing subsidiary LegalEntityProfiles whose verification relies **only** on the legacy Shared registry digest and flag them for governed review/correction, with events and historical audit; do not quietly delete or falsify evidence.
6. Correct the current code path that derives a tenant's PRIMARY Organisation from the DEFAULT LegalEntity for first-party records.
7. Support a ZuriBeans-specific PRIMARY Organisation plus valid Nabhold default/legal mandate without first-party reconciler "fixing" it to the Nabhold Organisation.
8. Update contract locks, all consumers, fixtures and formal decision logs by governed versioned migration.

The registry may continue to declare product intent, approved names/aliases and first-party onboarding policy, but it must not double as a legal-incorporation verification engine.

## 12. Progressive migration, avoiding a dangerous flag-day

### LA-00 — Evidence and compatibility baseline
Audit live Shared, CP, IAM, Trade, ERP, digital estates and each affected SQL constraint, OpenAPI client, event, mapping, context, tests and snapshot. Record existing first-party records and questionable verification provenance. Make no mass update from assumptions.

### LA-01 — Shared normative contracts
Agree Organisation-first Tenant invariant and revised \`tenancy.yaml\`, tenant-registration and onboarding contract with:
- canonical required \`organisation_id\`;
- optional compatible \`legal_entity_id\`;
- stable \`tenant_id\`;
- explicit mandate/mapping provenance;
- lifecycle, decision and event semantics;
- backward-compatible old request readers or a deliberate new contract version.

Avoid simply accepting either arbitrary ID without authoritative attestation. Open Shared PR first.

### LA-02 — PostgreSQL and domain compatibility
Provide additive migrations, constraints and backfill:
- \`tenants.legal_entity_id\` nullable after consumers are adapted;
- PRIMARY TenantOrganisationMapping mandatory for ordinary operational tenants;
- DEFAULT TenantLegalEntityMapping optional where permitted;
- nullable projection equals the sole valid DEFAULT mapping or NULL if none;
- valid relationship rows, referential integrity and effective-dated mandate constraints;
- existing tenants retain their real primary Organisation and legal-entity mappings;
- no automatically generated fake legal profiles.

Install transitional read/write paths and migration gates so old consumers are never accidentally given an empty legal actor where their action needs one.

### LA-03 — Control Plane registration, attestation and governance
Refactor \`RegisterTenant\`, \`insertOrganisationOnRegister\`, \`ProvisionTenantOrganisation\`, \`AdmissionOnboarder\`, \`FirstPartyReconciler\`, context resolution, integrity checks and controlled mapping change handlers. Enforce idempotent authorised onboarding requests and immutable audit/event provenance. Resolve identity through the named primary Organisation, never from display-name matching.

### LA-04 — Mandate authority and API
Implement reuse-first operating mandate policy under Shared/CP, with signed/approved authority, verified legal-actor identity when an operation requires it, role/market/time scope, revocation, history and conflict rules. Add API and audit/outbox events only after domain vocabulary is reviewed. Implement tests for authoriser privilege, multi-tenant shared legal actors, gaps, overlaps and expiry.

### LA-05 — Provider-consumer integration
Update IAM projection, Trade seller/merchant contexts, ERP company/finance baselines, document issuance, payments where present, digital estates and legal-person enforcement. Capability-by-capability readiness; do not pretend unavailable provider integrations are live.

### LA-06 — Founding-group registration and demonstration
Onboard Nabhold, ZuriBeans, Thamani and Equator & Estate Co. from the declared 2026-10-09 operating/legal-status baseline, independently verifying claims as required. **ZuriBeans and Equator & Estate are not separately incorporated**: register each as its own operating Organisation (and justified Tenant) without an invented incorporated LegalEntity. Nabhold may be selected for specifically authorised activities of each operating business only with an explicit, scoped operating mandate and actual legal/provider permission. **Thamani is declared CIPC-registered and a Nabhold subsidiary**: preserve its separate incorporated identity and responsibility, verifying the two claims separately; do not route its new transactions through Nabhold merely because it was historically a logistics unit. Keep founding sponsorship, INTERNAL eligibility and statutory identity evidence distinct.

### LA-07 — Cutover, rollback and operational acceptance
Certify historical/legal attribution, RLS/tenant isolation, multi-provider operational behavior, reconciliation, expiry, restore, failures and cross-market scenarios. Change/suspend only impacted capabilities on a missing legal actor; never destroy a tenant to correct a legal reference.

## 13. Acceptance test matrix

| Test | Scenario | Expected result |
|---|---|---|
| LA-T01 | Register an authorised Organisation-only tenant | Succeeds where the minimum admission policy permits; no fake LegalEntityProfile |
| LA-T02 | Try tenant without PRIMARY Organisation | Denied |
| LA-T03 | ZuriBeans Tenant uses verified/authorised Nabhold legal actor | ZuriBeans remains PRIMARY Organisation; only selected operations resolve Nabhold |
| LA-T04 | Nabhold admin attempts ZuriBeans tenant data access | Denied without explicit tenant-specific grant |
| LA-T05 | Same legal actor appears in two independent tenants | Permitted with separate mandates; tenant isolation intact |
| LA-T06 | Legal actor missing for invoice issuance | Deny only affected action, with actionable reason |
| LA-T07 | Attempt to issue as an unincorporated ZuriBeans (Pty) Ltd | Denied |
| LA-T08 | BusinessUnit / tenant mistaken for company ledger | Rejected by ERP boundary |
| LA-T09 | Two operating units use same legal actor | No automatic intercompany sale or double revenue |
| LA-T10 | Valid authority in ZA but not UG | UG-scoped operation denied unless separately authorised |
| LA-T11 | Expired/revoked operating mandate | New restricted action fails closed; historic evidence remains |
| LA-T12 | Overlapping conflicting legal actor mandates | Deterministic ambiguity error, no silent actor selection |
| LA-T13 | ZuriBeans incorporated later | New LegalEntity linked; original OrganisationID/TenantID unchanged |
| LA-T14 | Open old contract/invoice after new incorporation | Historic Nabhold legal liability/issuer not rewritten |
| LA-T15 | Migrate legacy tenant with real DEFAULT legal entity | Compatible projection and access preserved |
| LA-T16 | Replay registration or mandate | Idempotent; no duplicate mappings or outbox mutations |
| LA-T17 | First-party registry includes unregistered organisation | Internal identity recognised; no automatic incorporation VERIFIED |
| LA-T18 | Official proof verifies existing Nabhold/Thamani legal facts | Preserved with correct evidence source |
| LA-T19 | First-party reconciler sees ZuriBeans tenant with Nabhold DEFAULT actor | Does not reassign PRIMARY Organisation to Nabhold |
| LA-T20 | Finance baseline requested for operation without responsible actor | Fail closed; no fake ERP company |
| LA-T21 | Onboard a lawful sole proprietor | Uses supported real person/legal actor boundary; no dummy corporation |
| LA-T22 | Subsidiary changes market or exits group | Tenancy stable; mandates, sponsorship and eligibility reviewed |
| LA-T23 | Attempt client-supplied unbound legal_entity_id override | Rejected |
| LA-T24 | Missing legal-actor evidence on an unrelated catalogue draft | Draft may proceed if otherwise eligible |
| LA-T25 | Contract/source lock incompatible with changed schema | CI contract-drift gate fails until corrected |

| LA-T26 | Equator & Estate uses approved Nabhold legal actor for one scoped property/service activity | Its own Organisation remains PRIMARY; unrelated roles and markets remain restricted |
| LA-T27 | Thamani declares CIPC registration and Nabhold control | Both status claims retain independent evidence and review paths |
| LA-T28 | Nabhold legal actor serves ZuriBeans and Equator & Estate tenants | Two separate mandate scopes; no IAM or data crossover; correct accounting attribution |
| LA-T29 | First-party registry names unincorporated business | Registry identity recognised without automatic company-incorporation VERIFIED |
| LA-T30 | Required property/permit or cross-border authority absent | Block only dependent activity; preserve unrelated permitted tenant capabilities |

Include PostgreSQL migration/rollback, Go unit/integration/concurrency, Shared JSON Schema, OpenAPI generation/drift, IAM audience/context/isolation, ERP/Trade contract and digital-estate browser tests. A green mock-only test is not live regulatory or provider acceptance.

## 14. Authority, legal review and non-goals

This ADR sets software architecture for accountable business operations. It does not:
- Establish that Nabhold has all licences, capacity, trademark/trading-name rights or registrations needed to trade as ZuriBeans in every market;
- Transfer ZuriBeans contracts automatically to a later incorporated company;
- Permit fake company/invoice/merchant records;
- Convert the Baobab Regulations engine into a government enforcement authority;
- Grant exceptions to statutory or provider obligations;
- Merge Nabhold and ZuriBeans tenant security;
- Decide external subscription prices, billing tiers or future monetisation (reserved for a later ADR).

For any proposed live trading-as arrangement, obtain recorded organisational authority and a review of applicable legal disclosures, market/product permits, taxation, contracting, import/export and payment-provider conditions. Controlled non-production demonstrations may use clearly synthetic transactions and never present them as real commercial authorisation.

## 15. Alternatives considered

| Alternative | Decision | Reason |
|---|---|---|
| Require ZuriBeans to incorporate before any platform tenant | Rejected | Needlessly excludes legitimate operating-business use |
| Assign all ZuriBeans users and data to Nabhold tenant | Rejected | Undermines operating autonomy, isolated digital estate and later transition |
| Give ZuriBeans an invented legal_entity_id as if already incorporated | Rejected | Falsifies identity and pollutes ERP/contracting |
| Assign Nabhold legal_entity_id to ZuriBeans without independent PRIMARY Organisation and mandate | Rejected | Conflates identities and permits wrongful invoices/authorization inference |
| Remove legal_entity_id from every engine and transaction | Rejected | Destroys legal, accounting and settlement attribution |
| **Organisation-first Tenant plus scoped legal-actor mandate and nullable compatibility projection** | **Chosen** | Preserves operating autonomy, genuine legal responsibility, lifecycle continuity and cross-engine integrity |

## 16. Accepted architectural amendments (implementation pending)

1. **ADR-BCP-004:** Clarify that BusinessUnits are usually subdivisions, but an independently governed operating Organisation can have a separate Tenant even while a parent LegalEntity is legally responsible.
2. **ADR-BCP-012:** Preserve transaction classification based on actual legal actors, not tenants/brands, and require provenance for cross-tenant same-legal-entity transactions.
3. **ADR-BCP-017:** Permit an authorised Tenant onboarding request from an Organisation without requiring separate corporate incorporation universally; legal actor remains activity dependent.
4. **ADR-BCP-018:** PRIMARY Organisation is tenant identity; DEFAULT LegalEntity is optional and a compatibility projection. Corporate relationships never automatically establish operating mandate or access.
5. **ADR-BCP-019:** Represent operating entity, trading name, legal actor and documentary standing separately to applicants and administrators.
6. **ADR-BCP-023:** First-party governance evidence verifies only the specific assertion it supports. Correct inappropriate automatic promotion of unrelated legal incorporation claims.
7. **ADR-BCP-024:** Organisation kind and tenant attestation remain authoritative; legal-actor attestation is additive rather than a way around them.
8. **ADR-BCP-026:** Implement its progressive and founding grace via real Organisation-first tenants, without treating a first-party registry entry as incorporation proof.
9. **Shared tenancy and registration contracts:** Replace perpetual tenant-to-default-legal-entity obligation with mandatory primary Organisation, conditional legal-actor relationships and versioned compatibility.
10. **ADR-SHARED-015:** Desired-state plans, approvals, provider selection, readiness, observed state and drift remain authoritative, not replaced by a special-case registration.

**Implementation prerequisites:** Architecture and Security SHALL review and approve the detailed identity/mapping migration and affected consumer integration before deploying changes. Nabhold legal/corporate governance SHALL approve each real trading-as/legal-actor mandate with the required market/activity evidence. **Acceptance of this ADR does not certify, deploy or authorise an operating mandate, legal incorporation, tenant access or production transaction.**

---

## 17. Final principle

> **The business that uses Baobab, the tenant that isolates its data, the name under which it trades, and the legal person responsible for its obligations are distinct but explicitly related concepts. Baobab SHALL model all four faithfully.**

This preserves the original Nabhold business-unit vision, supports an autonomous ZuriBeans trading business before separate incorporation where legally permitted, and gives emerging African enterprises a durable path from inception to established corporate identity without losing their operational platform history.
