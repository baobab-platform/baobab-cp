# ADR-BCP-014 — Canonical Counterparty Identity, Roles and Relationships Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-12  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Primary Repository:** `nabhold/baobab-cp`  
**Canonical Contracts:** `nabhold/shared`  
**Consuming Engines:** `baobab-trade`, `baobab-erp`, `baobab-iam`, `baobab-cms`, future Baobab engines  
**Reference Tenant:** ZuriBeans

## Related Decisions

- ADR-BCP-011 — Market Participation, Trade Lanes and Cross-Market Trading Model
- ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model
- ADR-BCP-013 — Canonical Inventory Ownership, Custody, Location and In-Transit Model
- ADR-0017 — Customer, B2B Organisation, Buyer Identity and Authorization
- ADR-0019 — B2B Procurement, Supplier Commercial Workflow and Trade-to-ERP Boundary Model
- ADR-0021 — Customs, Trade Compliance and Regulatory Provider Architecture
- ADR-0022 — Shipping, Logistics, Freight and Transport Provider Abstraction

---

# 1. Context

Baobab increasingly interacts with organisations that cannot be represented safely as merely:

```text
Customer
```

or:

```text
Supplier
```

A single organisation may simultaneously be:

```text
supplier
customer
buyer
distributor
carrier
freight forwarder
customs broker
warehouse operator
affiliate
intercompany counterparty
service provider
```

For example:

```text
Organisation A
├── supplies coffee to ZuriBeans Uganda
├── buys another product from ZuriBeans Uganda
├── distributes ZuriBeans products in Kenya
└── operates logistics services on another trade lane
```

Creating four unrelated records would fragment the organisation's identity.

Conversely, making `Supplier`, `Customer`, `Carrier`, `BusinessPartner` or Keycloak `Organization` the canonical identity would couple Baobab to a particular engine.

A platform-level counterparty architecture is therefore required.

---

# 2. Decision

Baobab SHALL establish a canonical counterparty model based on the principle:

> **Identity, role, relationship, account, legal entity, tenant and IAM organisation are distinct concepts and SHALL NOT be conflated.**

The canonical model SHALL conceptually follow:

```text
CanonicalEntity
      │
      ▼
Organisation / Person
      │
      ▼
CounterpartyProfile
      │
      ├── CounterpartyRole
      │
      ├── CounterpartyRelationship
      │
      ├── Identifier
      │
      ├── Address
      │
      └── ExternalReference
      │
      ▼
Engine Projections
 ┌────┼───────────┬──────────┐
 ▼    ▼           ▼          ▼
Trade ERP         IAM      Logistics
```

No downstream engine identifier SHALL become the universal Baobab counterparty identifier.

---

# 3. Core Identity Principle

Baobab SHALL maintain one canonical identity for the same known organisation wherever reasonable and verified.

Example:

```text
Canonical Organisation
        │
        ├── CUSTOMER
        ├── SUPPLIER
        └── DISTRIBUTOR
```

rather than:

```text
Customer #123
Supplier #987
Distributor #445
```

with no canonical relationship between them.

---

# 4. Counterparty Is Not Necessarily External

A counterparty may be:

```text
EXTERNAL
RELATED_PARTY
INTERNAL_LEGAL_ENTITY
AFFILIATE
JOINT_VENTURE
```

Therefore:

```text
Counterparty
≠
External Organisation
```

---

# 5. Counterparty Is a Contextual Concept

An organisation becomes a counterparty because it participates in a relationship or transaction.

The architecture SHALL therefore avoid treating `Counterparty` as an isolated duplicate master identity.

Preferred structure:

```text
CanonicalEntity
      │
      ▼
Organisation
      │
      ▼
CounterpartyProfile
```

The profile adds commercial/relationship semantics to the canonical organisation.

---

# 6. CanonicalEntity Remains the Identity Spine

Existing `CanonicalEntity` SHALL remain the platform identity abstraction.

This ADR SHALL extend/generalise its use rather than create a competing universal identity system.

Conceptually:

```text
CanonicalEntity
├── id
├── entity_type
├── status
├── canonical_name
├── created_at
└── updated_at
```

Organisation-specific information sits in the corresponding organisation/profile model.

---

# 7. Organisation

Conceptually:

```text
Organisation
├── canonical_entity_id
├── legal_name
├── trading_names[]
├── organisation_type
├── country_of_registration?
├── incorporation_date?
├── identifiers[]
├── addresses[]
└── status
```

---

# 8. Organisation Type

Suggested extensible types include:

```text
COMPANY
SOLE_PROPRIETOR
PARTNERSHIP
COOPERATIVE
NON_PROFIT
GOVERNMENT_ENTITY
PUBLIC_BODY
FINANCIAL_INSTITUTION
OTHER
```

These describe organisational form.

They SHALL NOT encode commercial role.

---

# 9. Organisation Type ≠ Counterparty Role

A:

```text
COMPANY
```

may simultaneously be:

```text
SUPPLIER
CUSTOMER
CARRIER
```

Therefore organisational form and commercial role SHALL remain independent.

---

# 10. Counterparty Profile

Conceptually:

```text
CounterpartyProfile
├── id
├── canonical_entity_id
├── tenant_id
├── status
├── relationship_class
├── risk_state?
├── onboarding_state?
├── created_at
└── effective_period
```

---

# 11. Global Identity, Tenant-Scoped Relationship

Baobab SHOULD distinguish:

```text
canonical organisation identity
```

from:

```text
tenant-specific counterparty relationship
```

Example:

```text
Acme Coffee Ltd
       │
       ├── ZuriBeans relationship
       │      └── SUPPLIER
       │
       └── Thamani relationship
              └── SUPPLIER
```

ZuriBeans and Thamani SHALL NOT automatically share commercial terms, approvals, risk assessments or private relationship data merely because they interact with the same canonical organisation.

---

# 12. Tenant Isolation

The canonical identity layer MAY know:

```text
Organisation X exists
```

while tenant-specific projections govern:

```text
what ZuriBeans knows about Organisation X
```

and separately:

```text
what Thamani knows about Organisation X.
```

This distinction is mandatory.

---

# 13. Counterparty Roles

Initial canonical roles SHOULD support:

```text
CUSTOMER
BUYER
SUPPLIER
VENDOR
DISTRIBUTOR
RESELLER
CARRIER
FREIGHT_FORWARDER
CUSTOMS_BROKER
WAREHOUSE_OPERATOR
3PL
INSURER
BANK
PAYMENT_PROVIDER
AGENT
SERVICE_PROVIDER
AFFILIATE
INTERCOMPANY_COUNTERPARTY
```

The enumeration SHALL be extensible.

---

# 14. Multiple Roles

An organisation MAY hold multiple roles simultaneously.

```text
Organisation X
├── SUPPLIER
├── CUSTOMER
└── DISTRIBUTOR
```

This SHALL NOT require duplicate canonical identities.

---

# 15. Effective-Dated Roles

Roles SHALL support:

```text
effective_from
effective_to
status
scope
```

because relationships change.

---

# 16. Role Scope

A role MAY be scoped by:

```text
tenant
legal entity
market
product category
trade lane
business relationship
```

where required.

Example:

```text
Organisation X

SUPPLIER:
Uganda / Coffee

CUSTOMER:
South Africa / Vanilla
```

---

# 17. Role ≠ Authorization

Being assigned:

```text
SUPPLIER
```

does not automatically grant a user access to the supplier portal.

IAM governs authentication and authorization.

---

# 18. Organisation ≠ IAM Organization

A Keycloak Organization SHALL NOT be the canonical business organisation.

Instead:

```text
Canonical Organisation
        │
        ▼
ExternalReference
        │
        ▼
Keycloak Organization
```

where IAM projection is required.

---

# 19. IAM Organisation Is a Projection

Keycloak may represent:

```text
portal membership
federated users
organisation-level access
```

but SHALL not become the platform's legal/commercial master record.

---

# 20. Organisation ≠ Medusa Customer

Likewise:

```text
Canonical Organisation
        │
        ▼
Trade Projection
        │
        ▼
Medusa Customer / B2B Organisation
```

Medusa identifiers SHALL remain provider-specific.

---

# 21. Organisation ≠ ERP Business Partner

iDempiere:

```text
C_BPartner
```

is an ERP projection.

It SHALL NOT become Baobab's universal organisation identifier.

Mapping:

```text
Canonical Organisation
       │
       ▼
ExternalReference
       │
       ▼
iDempiere C_BPartner
```

---

# 22. Business Partner Dual Role

This architecture aligns naturally with ERP systems in which a Business Partner may function as both customer and vendor.

However, ERP semantics SHALL not dictate the canonical model.

---

# 23. Counterparty Relationship

Roles answer:

> What capacity can this organisation act in?

Relationships answer:

> How is this organisation related to another organisation?

These SHALL be distinct.

---

# 24. Relationship Model

Conceptually:

```text
CounterpartyRelationship
├── id
├── tenant_id
├── source_entity_id
├── target_entity_id
├── relationship_type
├── direction
├── scope
├── status
├── effective_from
└── effective_to?
```

---

# 25. Relationship Direction

Relationships SHALL be explicitly directional where semantics require it.

Example:

```text
ZuriBeans
   ──SUPPLIER_OF──▶ Customer X
```

differs from:

```text
Customer X
   ──SUPPLIER_OF──▶ ZuriBeans
```

---

# 26. Relationship Types

Potential relationship types include:

```text
SUPPLIER_OF
CUSTOMER_OF
DISTRIBUTOR_FOR
CARRIER_FOR
BROKER_FOR
WAREHOUSE_OPERATOR_FOR
AGENT_FOR
AFFILIATE_OF
OWNED_BY
CONTROLS
RELATED_TO
```

They SHALL remain extensible.

---

# 27. Legal Relationships

Corporate/legal relationships SHALL defer to ADR-BCP-012 where appropriate.

This ADR SHALL NOT duplicate:

```text
PARENT_SUBSIDIARY
SISTER_SUBSIDIARY
BRANCH_OF
```

semantics already governed there.

---

# 28. Internal Legal Entity as Counterparty

A Nabhold legal entity MAY participate as a counterparty.

Example:

```text
ZuriBeans Uganda operation
       ↓
related legal entity
       ↓
intercompany transaction
```

The canonical counterparty model SHALL reference the canonical LegalEntity rather than create a duplicate external company.

---

# 29. Legal Entity ≠ Organisation Duplicate

Where an internal LegalEntity already represents the organisation:

```text
CounterpartyProfile
      │
      ▼
LegalEntity reference
```

SHOULD be used.

No duplicate identity SHALL be created merely because the entity participates in Trade.

---

# 30. Counterparty Account

Commercial accounts SHALL remain distinct from identity.

Example:

```text
Canonical Organisation
        │
        ├── ZA customer account
        ├── UG supplier account
        └── KE distributor account
```

---

# 31. Account ≠ Identity

Account-specific data may include:

```text
credit terms
currency
price list
payment terms
tax profile
sales representative
supplier terms
```

These SHALL NOT be placed in the universal organisation identity.

---

# 32. Commercial Terms

ADR-0012 and ADR-0020 govern pricing/commercial terms.

Counterparty identity merely supplies the party context.

---

# 33. Supplier Commercial State

ADR-0019 governs supplier qualification/procurement lifecycle.

This ADR governs:

```text
who the supplier organisation is.
```

ADR-0019 governs:

```text
whether and how ZuriBeans may procure from it.
```

---

# 34. Supplier Registration ≠ Supplier Identity

A registration submission may create:

```text
CounterpartyCandidate
```

before canonical identity is confirmed.

The platform SHALL not immediately create a verified canonical organisation from untrusted form data.

---

# 35. Candidate Identity

Conceptually:

```text
CounterpartyCandidate
├── submitted_name
├── claimed_identifiers[]
├── claimed_addresses[]
├── contact_details[]
├── source
├── tenant
└── verification_state
```

---

# 36. Candidate Resolution

```text
Registration
     ↓
Candidate
     ↓
Identity Resolution
    / \
Existing New
   │     │
   ▼     ▼
Link   Create
   │     │
   └──┬──┘
      ▼
Canonical Organisation
```

---

# 37. Duplicate Detection

Potential duplicates SHOULD be detected using signals such as:

```text
registration number
tax identifier
legal name
country
address
verified domain
other authoritative identifiers
```

---

# 38. No Fuzzy Auto-Merge

The platform SHALL NOT automatically merge organisations solely because:

```text
names are similar.
```

Example:

```text
ABC Trading Ltd
ABC Traders Ltd
```

may be different legal organisations.

---

# 39. Merge Requires Evidence

Canonical merges SHALL require:

```text
verified identifiers
authoritative evidence
or
authorised human decision
```

---

# 40. Merge Audit

A merge SHALL record:

```text
source entities
surviving entity
actor
reason
evidence
timestamp
```

---

# 41. Merge Does Not Destroy History

Historical transaction references SHALL remain resolvable after a merge.

---

# 42. Duplicate Split

Architecture SHOULD allow erroneous merges to be corrected through controlled identity remediation.

---

# 43. Legal Name

Canonical legal name SHOULD come from the strongest available verified source.

User-entered display names SHALL not automatically overwrite it.

---

# 44. Trading Names

An organisation MAY have multiple:

```text
trading names
brands
aliases
```

These SHALL not create new canonical organisations unless legally separate entities exist.

---

# 45. Legal Identifiers

Identifiers SHALL be jurisdiction-aware.

Conceptually:

```text
OrganisationIdentifier
├── organisation_id
├── type
├── value
├── issuing_jurisdiction
├── issuer?
├── verified
├── valid_from?
└── valid_to?
```

---

# 46. Identifier Types

Examples MAY include:

```text
COMPANY_REGISTRATION
TAX_IDENTIFIER
VAT_IDENTIFIER
CUSTOMS_IDENTIFIER
IMPORTER_IDENTIFIER
EXPORTER_IDENTIFIER
LEI
OTHER
```

---

# 47. Identifier Is Not Globally Unique Without Context

The platform SHALL NOT assume:

```text
identifier value
```

alone is universally unique.

Uniqueness may require:

```text
identifier type
+
jurisdiction
+
issuer
+
value
```

---

# 48. Tax Identity

Tax identifiers SHALL remain identifiers.

Tax treatment remains governed by the tax architecture.

---

# 49. Customs Identity

Importer/exporter/customs registrations SHALL remain regulatory identifiers and evidence.

ADR-0021 consumes them.

---

# 50. Addresses

Organisations MAY have multiple addresses.

Suggested types:

```text
REGISTERED
BILLING
SHIPPING
WAREHOUSE
OPERATING
CORRESPONDENCE
```

---

# 51. Address ≠ Market Participation

Having an address in South Africa does not automatically mean:

```text
approved supplier in South Africa.
```

---

# 52. Address Verification

Address verification state SHOULD be explicit where important.

---

# 53. Contacts

A business contact SHALL not automatically become an IAM identity.

```text
Contact Person
≠
Authenticated User
```

---

# 54. Contact Model

Conceptually:

```text
OrganisationContact
├── organisation_id
├── name
├── role/title
├── email?
├── phone?
├── purpose
└── status
```

---

# 55. User Linking

When a contact becomes a portal user:

```text
Organisation Contact
       │
       ▼
IAM Identity Link
```

SHALL be explicit.

---

# 56. Person Counterparties

The architecture SHOULD permit individual counterparties where legitimate business models require them.

However, B2B organisation identity remains the Release 1 priority.

---

# 57. Verification

Organisation identity SHALL support verification states.

Suggested:

```text
UNVERIFIED
PENDING_VERIFICATION
PARTIALLY_VERIFIED
VERIFIED
REJECTED
SUSPENDED
```

---

# 58. Verification ≠ Commercial Approval

An organisation may be:

```text
VERIFIED
```

but:

```text
NOT APPROVED AS SUPPLIER
```

These SHALL remain separate states.

---

# 59. Verification Sources

Verification MAY use:

```text
company registry
tax authority
bank evidence
trade licence
manual review
trusted data provider
```

depending on market/provider availability.

---

# 60. Source Provenance

Verified fields SHOULD retain:

```text
source
verified_at
verification_method
evidence_reference
```

---

# 61. Field-Level Provenance

Where practical, high-value identity attributes SHOULD support field-level provenance.

Example:

```text
legal_name
   ↓
Uganda Registry Provider
```

while:

```text
warehouse_address
   ↓
Supplier Submission
```

---

# 62. Conflicting Sources

If authoritative sources disagree, the platform SHALL create a review condition rather than silently choose a convenient value.

---

# 63. Counterparty Status

Generic lifecycle MAY include:

```text
PROSPECT
ACTIVE
SUSPENDED
BLOCKED
DORMANT
DEACTIVATED
```

This describes relationship usability.

It SHALL not replace role-specific lifecycle.

---

# 64. Role-Specific Lifecycle

Example:

```text
Organisation = ACTIVE

Supplier Role = SUSPENDED

Customer Role = ACTIVE
```

This SHALL be valid.

---

# 65. Blocking

A block MAY apply at:

```text
organisation
role
relationship
market
legal entity
trade lane
```

scope.

---

# 66. Risk

Counterparty risk SHALL be represented as an assessment/projection, not immutable identity.

Risk changes over time.

---

# 67. Risk Sources

Future risk signals MAY come from:

```text
Trade
ERP
Pulse
compliance providers
credit providers
manual assessment
```

---

# 68. Compliance Screening

ADR-0021 may screen the canonical organisation.

This is preferable to screening disconnected Customer and Supplier duplicates independently.

---

# 69. Screening Identity

Compliance requests SHOULD include verified:

```text
legal name
aliases
registration identifiers
jurisdiction
addresses
```

where appropriate.

---

# 70. Screening Result ≠ Identity

A sanctions or compliance result is an assessment of an identity.

It SHALL not be stored as an identity attribute.

---

# 71. Logistics Counterparty

ADR-0022 may use the same canonical organisation as:

```text
carrier
freight forwarder
customs broker
3PL
```

through roles.

---

# 72. Provider vs Counterparty

An organisation MAY simultaneously be:

```text
Counterparty
+
Capability Provider
```

Example:

```text
Freight Forwarder Ltd
```

may have a commercial relationship with ZuriBeans and also satisfy a platform logistics capability.

These concepts SHALL remain separable.

---

# 73. Provider Binding

CP CapabilityBinding may reference:

```text
provider organisation
+
provider implementation
```

without turning every counterparty into an engine provider.

---

# 74. Banks and Financial Institutions

Banks MAY participate as counterparties/providers for future:

```text
settlement
trade finance
letters of credit
guarantees
```

The identity model SHALL accommodate them.

---

# 75. Counterparty Groups

Commercial grouping MAY be required.

Example:

```text
Global Group
├── Subsidiary A
├── Subsidiary B
└── Subsidiary C
```

The platform SHALL model actual organisational relationships rather than pretending all subsidiaries are one legal organisation.

---

# 76. Parent Company ≠ Subsidiary

This SHALL be explicit.

A group relationship does not collapse separate legal identities.

---

# 77. Related-Party Detection

Where organisational relationships indicate related parties, ADR-BCP-012 may use that context for:

```text
intercompany
transfer pricing
customs valuation
consolidation
```

---

# 78. Relationship Graph

Canonical organisations and relationships conceptually form:

```text
              Parent Group
              /          \
             ▼            ▼
       Company A       Company B
          │               │
          │               ├── CUSTOMER_OF
          │               │
          └── SUPPLIER_OF ─┘
```

The graph SHALL remain queryable without replacing transactional models.

---

# 79. Engine Projection Architecture

```text
                 Canonical Organisation
                          │
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
        Trade            ERP             IAM
          │               │               │
          ▼               ▼               ▼
 B2B Organisation     C_BPartner     Keycloak Org
```

Mappings SHALL be explicit.

---

# 80. ExternalReference

Existing ExternalReference infrastructure SHALL be reused.

Conceptually:

```text
ExternalReference
├── canonical_entity_id
├── engine
├── engine_instance
├── external_type
├── external_id
└── scope
```

---

# 81. Engine Instance Matters

An iDempiere Business Partner ID:

```text
12345
```

is meaningless without the corresponding EngineInstance.

Therefore mapping SHALL include provider/instance scope.

---

# 82. Market Scope

Where engines create market-specific counterparty projections, mapping SHALL preserve market scope.

---

# 83. Legal Entity Scope

ERP Business Partner relationships may also be scoped differently by Client/Organisation.

Canonical mapping SHALL preserve this context.

---

# 84. Projection Creation

Engine projections SHOULD be created:

```text
on demand
or
through governed provisioning
```

rather than indiscriminately copying every canonical organisation into every engine.

---

# 85. Projection Ownership

The engine owns its operational projection.

Baobab owns the canonical identity and mapping.

---

# 86. Projection Failure

If:

```text
Canonical Organisation created
        ↓
ERP projection fails
```

the canonical identity SHALL remain valid while the required capability becomes:

```text
NOT_READY / DEGRADED
```

according to workflow.

---

# 87. No Distributed Transaction

Canonical identity creation and downstream projection SHALL NOT require a distributed ACID transaction across:

```text
CP DB
Trade DB
ERP DB
IAM DB
```

---

# 88. Event-Driven Projection

Canonical events SHOULD include:

```text
counterparty.created
counterparty.updated
counterparty.verified
counterparty.suspended

counterparty-role.assigned
counterparty-role.revoked

counterparty-relationship.created
counterparty-relationship.ended

counterparty-identifier.verified

counterparty.merged
```

---

# 89. Event Envelope

Events SHALL include:

```text
canonical_entity_id
tenant where applicable
correlation_id
causation_id
schema_version
occurred_at
```

---

# 90. Idempotency

Repeated:

```text
counterparty-role.assigned
```

SHALL NOT create duplicate role assignments.

---

# 91. Event Ordering

Consumers SHALL tolerate:

```text
duplicate
delayed
out-of-order
```

events.

---

# 92. Canonical Update Policy

Not every downstream engine update SHALL automatically modify canonical identity.

Example:

```text
ERP user changes Business Partner name
```

SHALL NOT silently overwrite verified canonical legal name.

---

# 93. Change Authority

Attributes SHOULD declare authoritative source or reconciliation policy.

---

# 94. Reconciliation

Baobab SHALL reconcile:

```text
Canonical Organisation
↕
Trade Organisation

Canonical Organisation
↕
ERP Business Partner

Canonical Organisation
↕
IAM Organisation
```

where projections are expected.

---

# 95. Reconciliation States

Suggested:

```text
MATCHED
PENDING
MISSING_PROJECTION
STALE
MISMATCH
CONFLICT
UNRESOLVED
```

---

# 96. Name Change

A verified legal name change SHALL propagate through governed projection workflows.

Historical transactions SHALL preserve historical representation where required.

---

# 97. Organisation Closure

Closing/deactivating an organisation SHALL not delete historical:

```text
orders
invoices
shipments
payments
compliance records
```

---

# 98. Hard Delete

Verified canonical counterparties referenced by transactions SHOULD NOT be hard-deleted.

Use lifecycle state.

---

# 99. GDPR/POPIA and Personal Data

Organisation identity may contain personal information through contacts and sole proprietors.

Data minimisation, retention and access controls SHALL therefore apply.

---

# 100. Tenant Privacy Boundary

If ZuriBeans adds private:

```text
supplier rating
commercial notes
credit decision
```

Thamani SHALL NOT receive them merely because the organisation identity is shared canonically.

---

# 101. Shared Canonical Data

Only appropriate identity attributes MAY be shared at canonical level.

Examples:

```text
verified legal name
verified company registration identifier
jurisdiction
```

subject to governance.

---

# 102. Tenant-Specific Data

Examples:

```text
supplier approval
customer credit limit
price agreement
internal risk notes
procurement performance
account manager
```

SHALL remain tenant-scoped.

---

# 103. Confidential Data

Sensitive commercial or compliance data SHALL use stricter classification.

---

# 104. Search

Canonical counterparty search SHOULD support:

```text
legal name
trading name
registration identifier
tax identifier
country
role
```

subject to authorization.

---

# 105. Search Isolation

Search results SHALL respect tenant/context visibility.

A global canonical registry SHALL NOT become a cross-tenant information leak.

---

# 106. Counterparty Resolution API

Conceptually:

```text
resolveCounterparty(context)
```

may accept:

```text
tenant
identifier
name?
jurisdiction?
role?
market?
```

and return an authorised canonical reference or ambiguity result.

---

# 107. Ambiguous Resolution

If multiple candidates exist:

```text
AMBIGUOUS
```

SHALL be returned.

The resolver SHALL NOT guess.

---

# 108. Fail Closed

Financial, regulatory or procurement transactions SHALL fail closed when required counterparty identity cannot be resolved reliably.

---

# 109. Supplier Onboarding Flow

```text
Supplier Registration
        │
        ▼
Counterparty Candidate
        │
        ▼
Identity Resolution
       / \
      /   \
 Existing  New
    │       │
    └───┬───┘
        ▼
Canonical Organisation
        │
        ▼
SUPPLIER Role
        │
        ▼
Qualification
        │
        ▼
Trade Projection
        │
        ▼
ERP Business Partner
```

---

# 110. Buyer Onboarding Flow

```text
Buyer Registration
       │
       ▼
Counterparty Candidate
       │
       ▼
Identity Resolution
       │
       ▼
Canonical Organisation
       │
       ▼
CUSTOMER / BUYER Role
       │
       ▼
Trade B2B Organisation
       │
       ▼
IAM Organisation
       │
       ▼
ERP Business Partner
```

---

# 111. Dual-Role Scenario

A coffee producer may later purchase packaging products from ZuriBeans.

```text
Organisation X
     │
     ├── SUPPLIER
     └── CUSTOMER
```

No duplicate identity SHALL be required.

---

# 112. ZuriBeans Supplier Scenario

```text
Uganda Coffee Cooperative
        │
        ▼
Canonical Organisation
        │
        ├── SUPPLIER
        │
        ▼
ZuriBeans Supplier Relationship
        │
        ├── Trade Supplier Projection
        └── ERP Business Partner
```

---

# 113. ZuriBeans Buyer Scenario

```text
South African Coffee Distributor
        │
        ▼
Canonical Organisation
        │
        ├── CUSTOMER
        ├── BUYER
        └── DISTRIBUTOR
        │
        ▼
Trade B2B Organisation
        │
        ├── IAM Organisation
        └── ERP Business Partner
```

---

# 114. Logistics Scenario

```text
African Freight Ltd
       │
       ▼
Canonical Organisation
       │
       ├── CARRIER
       └── FREIGHT_FORWARDER
       │
       ▼
ADR-0022 Provider Projection
```

---

# 115. Customs Broker Scenario

```text
Customs Broker Ltd
       │
       ▼
Canonical Organisation
       │
       └── CUSTOMS_BROKER
       │
       ▼
ADR-0021 Provider / Relationship
```

---

# 116. Intercompany Scenario

```text
Nabhold Legal Entity A
       │
       ▼
Canonical Legal Entity
       │
       ▼
Counterparty Context
       │
       ▼
RELATED PARTY
```

ADR-BCP-012 resolves the legal relationship.

---

# 117. No Duplicate Internal Counterparty

Internal legal entities SHALL not be re-created as arbitrary external supplier/customer organisations.

---

# 118. Master Data Ownership Matrix

| Data | Authority |
|---|---|
| Canonical identity | CP / canonical platform |
| Legal name | Canonical identity authority |
| Legal identifiers | Canonical identity authority |
| Tenant relationship | CP/domain relationship layer |
| Supplier qualification | Trade |
| Customer commercial terms | Trade |
| IAM membership | IAM |
| ERP Business Partner | ERP |
| Credit balance | ERP/financial provider |
| Shipment role | Trade |
| Compliance screening | Compliance provider |
| Counterparty intelligence | Pulse |

---

# 119. API Boundary

Canonical APIs SHOULD expose concepts such as:

```text
POST /counterparties
GET /counterparties/{id}
POST /counterparties/{id}/roles
POST /counterparties/{id}/relationships
POST /counterparties/{id}/identifiers
POST /counterparty-candidates
POST /counterparty-resolution
```

Exact endpoint design remains implementation-specific.

---

# 120. API Security

Canonical identity APIs SHALL distinguish:

```text
read canonical identity
manage canonical identity
manage tenant relationship
verify identifier
merge entities
```

as separate privileges.

---

# 121. Merge Authorization

Entity merge SHALL be a privileged operation.

---

# 122. Audit

At minimum audit:

```text
creation
verification
identifier change
role assignment
relationship change
suspension
merge
unmerge/remediation
projection mapping
```

---

# 123. Observability

Recommended metrics include:

```text
counterparty_created_total
counterparty_candidate_total
counterparty_duplicate_candidate_total
counterparty_merge_total
counterparty_resolution_ambiguous_total
counterparty_projection_failure_total
counterparty_reconciliation_mismatch_total
```

---

# 124. Readiness

A business capability requiring a downstream projection SHALL not be READY merely because canonical identity exists.

Example:

```text
Supplier identity = READY
ERP Business Partner = MISSING
Procurement commitment requires ERP
```

Therefore:

```text
PROCUREMENT = NOT_READY
```

---

# 125. Capability Readiness Integration

Use the established progression:

```text
DECLARED
   ↓
CONTRACTED
   ↓
IMPLEMENTED
   ↓
PROVISIONED
   ↓
INTEGRATED
   ↓
TESTED
   ↓
READY
```

---

# 126. Test — Same Organisation, Multiple Roles

Create one organisation and prove:

```text
SUPPLIER
+
CUSTOMER
+
DISTRIBUTOR
```

without duplicate canonical identity.

---

# 127. Test — Cross-Tenant Relationship Isolation

Same canonical organisation:

```text
ZuriBeans → SUPPLIER
Thamani → SUPPLIER
```

Prove that:

```text
ZuriBeans commercial data
```

cannot be read by Thamani.

---

# 128. Test — Duplicate Registration

Register same verified company twice.

System SHALL:

```text
detect candidate match
```

rather than silently create duplicate canonical identity.

---

# 129. Test — Similar Name

Register:

```text
ABC Trading Ltd
ABC Traders Ltd
```

without matching legal identifiers.

System SHALL NOT automatically merge them.

---

# 130. Test — Dual ERP Role

One canonical organisation SHALL map to an ERP Business Partner capable of appropriate customer/vendor roles without duplicate canonical identity.

---

# 131. Test — IAM Projection

Buyer organisation:

```text
Canonical Organisation
      ↓
Trade Organisation
      ↓
Keycloak Organisation
```

shall preserve canonical mapping.

---

# 132. Test — Supplier IAM Projection

Supplier portal organisation SHALL map explicitly without making Keycloak the canonical supplier master.

---

# 133. Test — Counterparty Screening

Compliance provider SHALL screen the canonical organisation identity with verified identifiers.

---

# 134. Test — Intercompany Counterparty

An internal legal entity SHALL resolve through LegalEntity relationship without duplicate external organisation creation.

---

# 135. Test — Engine Failure

If ERP Business Partner provisioning fails:

```text
canonical organisation survives
mapping reports failure
procurement capability not READY
```

---

# 136. Test — Cross-Tenant Attack

Attempt:

```text
Thamani user
   ↓
ZuriBeans supplier relationship
```

SHALL fail.

---

# 137. Test — Merge History

Merge a verified duplicate and prove historical:

```text
orders
shipments
invoices
```

remain resolvable.

---

# 138. Rejected Alternative — Medusa Organisation as Canonical Identity

Rejected because:

- Trade is one engine;
- ERP and IAM need the identity independently;
- it couples canonical identity to Medusa.

---

# 139. Rejected Alternative — iDempiere Business Partner as Canonical Identity

Rejected because ERP is a provider engine, not the platform identity authority.

---

# 140. Rejected Alternative — Keycloak Organisation as Canonical Identity

Rejected because authentication/authorization is not business master-data management.

---

# 141. Rejected Alternative — Separate Supplier and Customer Masters

Rejected because one legal organisation may perform both roles.

---

# 142. Rejected Alternative — Counterparty as Another Universal Identity Table

Rejected where it duplicates CanonicalEntity.

Counterparty SHALL extend the canonical identity spine through profiles, roles and relationships.

---

# 143. Rejected Alternative — Tenant-Global Sharing of All Counterparty Data

Rejected because it violates legal-entity independence and tenant confidentiality.

---

# 144. Rejected Alternative — Completely Isolated Duplicate Identity Per Tenant

Rejected as the canonical platform model because it makes cross-platform identity resolution, screening and relationship mapping unnecessarily fragmented.

Tenant-specific relationships remain isolated while canonical identity is governed centrally.

---

# 145. Rejected Alternative — Automatic Fuzzy Matching and Merge

Rejected due to unacceptable identity corruption risk.

---

# 146. Positive Consequences

The decision enables:

- supplier/customer dual roles;
- consistent B2B organisation identity;
- ERP Business Partner mapping;
- IAM organisation mapping;
- logistics provider identity;
- customs broker identity;
- related-party recognition;
- cross-market trading;
- deduplication;
- stronger compliance screening;
- future trade finance counterparties.

---

# 147. Costs

The architecture requires:

- identity resolution;
- verification;
- role modelling;
- relationship modelling;
- provenance;
- mapping;
- reconciliation;
- merge governance;
- tenant privacy controls.

These costs are justified by the platform's multi-engine, multi-tenant architecture.

---

# 148. Implementation Ownership

```text
nabhold/shared
    │
    ├── canonical schemas
    └── canonical events

nabhold/baobab-cp
    │
    ├── canonical identity
    ├── roles
    ├── relationships
    ├── resolution
    └── mappings

nabhold/baobab-trade
    │
    ├── customer projection
    ├── supplier projection
    ├── logistics projection
    └── commercial relationships

nabhold/baobab-erp
    │
    └── C_BPartner projection

nabhold/baobab-iam
    │
    └── Keycloak Organisation projection
```

---

# 149. Implementation Sequence

```text
Existing CanonicalEntity Audit
          ↓
Organisation Contract
          ↓
Counterparty Profile
          ↓
Identifiers
          ↓
Roles
          ↓
Relationships
          ↓
Candidate / Resolution
          ↓
Verification
          ↓
External References
          ↓
Trade Projection
          ↓
ERP Projection
          ↓
IAM Projection
          ↓
Reconciliation
          ↓
Isolation Tests
          ↓
ZuriBeans Golden-Tenant Validation
```

---

# 150. Migration Requirement

Before implementation, existing:

```text
customer
supplier
organisation
business partner
IAM organisation
```

records SHALL be inventoried.

Migration SHALL identify:

```text
existing canonical mapping
duplicate candidates
unmapped records
conflicting identifiers
```

before attempting automated consolidation.

---

# 151. Legacy Supplier Canonical Decisions

Any earlier supplier-organisation ADR SHALL be reviewed against this decision.

Where it conflicts:

```text
ADR-BCP-014
```

SHALL establish the broader counterparty architecture, and the older supplier-specific decision SHOULD be amended or superseded rather than maintaining two competing canonical identity models.

---

# 152. Release 1 Scope

ZuriBeans Release 1 SHALL support:

- canonical B2B organisations;
- legal identifiers;
- tenant-specific counterparty profiles;
- supplier role;
- customer/buyer role;
- logistics-provider roles;
- effective-dated roles;
- relationships;
- identity candidates;
- duplicate detection;
- controlled merge;
- Trade projection;
- ERP Business Partner mapping;
- IAM Organisation mapping;
- compliance screening identity;
- tenant isolation;
- audit;
- reconciliation.

---

# 153. Deferred Enhancements

Release 1.1+ MAY add:

- corporate group graph intelligence;
- beneficial ownership;
- automated registry enrichment;
- advanced entity resolution;
- AI-assisted duplicate detection;
- counterparty risk scoring;
- bank/financial institution profiles;
- counterparty network analytics.

AI-assisted matching SHALL remain advisory unless explicitly approved through governed identity resolution.

---

# 154. Definition of Done

ADR-BCP-014 is implemented when:

- [ ] CanonicalEntity remains the identity spine;
- [ ] Organisation model exists;
- [ ] CounterpartyProfile exists;
- [ ] tenant relationship is separated from canonical identity;
- [ ] multiple roles per organisation work;
- [ ] roles are effective-dated;
- [ ] relationships are directional;
- [ ] legal identifiers are jurisdiction-aware;
- [ ] candidate identity workflow exists;
- [ ] duplicate detection exists;
- [ ] fuzzy auto-merge is prohibited;
- [ ] controlled merge exists;
- [ ] historical references survive merge;
- [ ] Trade projections map canonically;
- [ ] ERP Business Partners map canonically;
- [ ] IAM Organisations map canonically;
- [ ] internal LegalEntities are not duplicated as arbitrary counterparties;
- [ ] supplier and customer roles may coexist;
- [ ] tenant-specific data remains isolated;
- [ ] compliance can screen canonical identity;
- [ ] logistics can consume provider organisation identity;
- [ ] reconciliation exists;
- [ ] events are idempotent;
- [ ] audit exists;
- [ ] cross-tenant isolation passes;
- [ ] ZuriBeans supplier onboarding passes;
- [ ] ZuriBeans buyer onboarding passes;
- [ ] dual-role counterparty test passes;
- [ ] intercompany counterparty test passes.

---

# 155. Final Architecture

```text
                         CANONICAL ENTITY
                               │
                               ▼
                          ORGANISATION
                               │
                               ▼
                      COUNTERPARTY PROFILE
                               │
             ┌─────────────────┼─────────────────┐
             ▼                 ▼                 ▼
           ROLES          RELATIONSHIPS      IDENTIFIERS
             │                 │                 │
             └─────────────────┼─────────────────┘
                               ▼
                       CANONICAL IDENTITY
                               │
          ┌────────────────────┼─────────────────────┐
          ▼                    ▼                     ▼
        TRADE                  ERP                   IAM
          │                    │                     │
          ▼                    ▼                     ▼
 B2B Organisation        Business Partner      Keycloak Org
 Supplier Projection
 Logistics Projection
```

Tenant relationships remain isolated:

```text
                     Canonical Organisation X
                              │
                  ┌───────────┴───────────┐
                  ▼                       ▼
             ZuriBeans                Thamani
                  │                       │
           SUPPLIER role             CUSTOMER role
           approval data             commercial data
           private terms             private terms
                  │                       │
                  └────── ISOLATED ──────┘
```

Role composition:

```text
                     Organisation X
                          │
        ┌─────────────────┼─────────────────┐
        ▼                 ▼                 ▼
     SUPPLIER          CUSTOMER          CARRIER
        │                 │                 │
        ▼                 ▼                 ▼
   Procurement          Sales           Logistics
```

The fundamental invariant is:

> **An organisation is what the party is; a role describes the capacity in which it acts; a relationship describes how it relates to another party; an account contains commercial configuration; an IAM organisation controls access; and an engine projection exists only to serve an engine. None of these concepts may substitute for the canonical identity of the party.**

---

# Decision Outcome

**ACCEPTED WHEN APPROVED**

Implementation SHALL proceed:

```text
ADR-BCP-014
      ↓
CanonicalEntity Audit
      ↓
Organisation + Counterparty Contracts
      ↓
Roles + Relationships
      ↓
Candidate Resolution + Verification
      ↓
Trade / ERP / IAM Projections
      ↓
Reconciliation + Isolation
      ↓
ZuriBeans Golden-Tenant Validation
```

No Baobab engine SHALL establish its own customer, supplier, carrier, broker or Business Partner identifier as the universal identity of an organisation.