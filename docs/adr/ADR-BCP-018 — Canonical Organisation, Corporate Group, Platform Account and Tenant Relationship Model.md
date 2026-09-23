# ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account and Tenant Relationship Model

**Status:** Proposed — Normative Platform Architecture  
**Date:** 2026-09-22  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Primary Repository:** `baobab-platform/baobab-cp`  
**Contract Authority:** `baobab-platform/shared`  
**Runtime Authority:** `baobab-platform/baobab-cp`  
**Identity / Authentication Authority:** `baobab-platform/baobab-iam`  
**First-Party Corporate Governance Source:** Nabhold Group Africa governance records published through `baobab-platform/shared`  
**Decision Type:** Foundational organisation, enterprise-group, platform-affiliation, customer-account and tenancy architecture

**Depends On / Reconciles:**

- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
- ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
- ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model
- ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model
- ADR-BCP-009 — Capability-Centric Security, Isolation, Residency, Revocation and Failure Semantics
- ADR-BCP-010 — Modular Control Plane Architecture, Governance Boundaries and Evolution Model
- ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model
- ADR-BCP-014 — Canonical Counterparty Identity, Roles and Relationships Model
- ADR-BCP-015 — Gate ZB-00 Capability Vocabulary Alignment, Quality-State Ownership and Roadmap Conflict Resolution
- ADR-BCP-016 — Buyer Organisation Canonical Entity Registration and OrganisationID Context Resolution
- ADR-BCP-017 — Organisation Admission, Subscription Classification and Tenant Onboarding Lifecycle Model
- ADR-SHARED-0003 — Tenant, Legal Entity and Digital Estate identity separation
- `contracts/tenancy/tenancy.yaml`
- `contracts/legal-entity/registry.yaml`
- `contracts/erp/v1/system-of-record.yaml`
- `contracts/control-plane/v1/domain.schema.json`
- `BCP-TS-ONBOARDING-001` — Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification
- Applicable Baobab IAM ADRs, including the Keycloak Organisation projection and B2B organisation-access decisions

**Non-Normative External Validation References:**

- NIST SP 800-207 — Zero Trust Architecture
- NIST SP 800-207A — Zero Trust Architecture for cloud-native/multi-location environments
- AWS SaaS Architecture Fundamentals — SaaS identity and tenant isolation
- Keycloak Organizations — realm-scoped identity and membership projection

---

# 1. Executive Decision

Baobab SHALL establish a platform-wide **Canonical Organisation and Relationship Model** that distinguishes:

```text
Real-World Organisation Identity

Corporate / Ownership Relationship

Corporate Group

Relationship to Baobab Platform

Commercial / Administrative Platform Account

Legal Entity

Tenant

Product Subscription

Capability Entitlement

IAM Organisation / Membership
```

These concepts SHALL remain independently modelled.

The principal architectural invariant is:

> **Organisation identity, legal-entity identity, corporate ownership, corporate-group membership, platform affiliation, platform account membership, tenancy, subscription classification and authorization are independent concerns and SHALL NOT be conflated.**

The target architecture is:

```text
                         REAL-WORLD DOMAIN
                                │
                                ▼
                         CanonicalEntity
                                │
                                ▼
                           Organisation
                                │
              ┌─────────────────┼──────────────────┐
              │                 │                  │
              ▼                 ▼                  ▼
       LegalEntityProfile   Corporate          Counterparty
                            Relationship        Relationships
                                │              (ADR-BCP-014)
                                ▼
                         Corporate Group
                         / Relationship Graph


                         PLATFORM DOMAIN
                                │
                                ▼
                       PlatformRelationship
                                │
        ┌───────────────────────┼────────────────────────┐
        │                       │                        │
        ▼                       ▼                        ▼
 PLATFORM_OWNER       PLATFORM_GROUP_AFFILIATE    EXTERNAL_CLIENT
 PLATFORM_OPERATOR    PLATFORM_PARTNER            MANAGED_ENTITY
                                │
                                ▼
                         PlatformAccount
                                │
                     Commercial / Contractual
                     Administrative Grouping
                                │
                   ┌────────────┼────────────┐
                   ▼            ▼            ▼
                 Tenant       Tenant       Tenant
                   │            │            │
                   ▼            ▼            ▼
             ProductSubscription(s)
                   │
                   ▼
              CapabilityGrant
                   │
                   ▼
             CapabilityBinding
```

The model SHALL apply equally to:

```text
Nabhold Group Africa
and its subsidiaries
```

and:

```text
any external customer
and that customer's subsidiaries,
affiliates or corporate group.
```

There SHALL NOT be a Nabhold-specific tenancy architecture.

Nabhold SHALL be represented as one valid instance of the generic Baobab organisation and corporate-group model.

---

# 2. Problem Statement

Baobab began with a small and known set of first-party organisations:

```text
Nabhold Group Africa
├── ZuriBeans
├── Thamani Global
└── Equator & Estate Co.
```

A static legal-entity registry was sufficient during this phase.

Baobab is, however, intended to become a platform serving organisations outside Nabhold Group.

A future external customer may itself be a complex enterprise:

```text
Acme Holdings Ltd
├── Acme Foods Ltd
├── Acme Logistics Ltd
├── Acme Retail Ltd
└── Acme Uganda Ltd
```

Any combination of these organisations may become Baobab clients.

For example:

```text
Acme Holdings        → Baobab client
Acme Foods           → Baobab client
Acme Logistics       → Baobab client
Acme Retail          → not a Baobab client
Acme Uganda          → Baobab client
```

Baobab therefore needs to answer independently:

```text
Who is this organisation?

Is it legally incorporated?

Who owns or controls it?

Which corporate group does it belong to?

What relationship does it have with Baobab?

Which commercial account covers it?

Does it have a tenant?

Which legal entities does that tenant represent?

Which products has the tenant subscribed to?

Which capabilities are actually granted?

Who may access which tenant or resource?
```

No single existing object answers all of these questions safely.

---

# 3. Existing Architecture Is Retained

This ADR SHALL NOT discard the existing architecture.

It preserves the following decisions:

```text
Tenant != LegalEntity
```

```text
Tenant != Organisation
```

```text
Tenant != DigitalEstate
```

```text
Subscription != CapabilityGrant
```

```text
IAM Organisation != Canonical Organisation
```

```text
Counterparty relationship != identity
```

```text
Corporate ownership != authorization
```

```text
Market != LegalEntity
```

```text
Platform affiliation != subscription classification
```

The purpose of this ADR is to complete the missing relationship layer.

---

# 4. Architectural Invariants

The following are normative:

```text
Organisation
    != LegalEntity

Organisation
    != Tenant

Organisation
    != IAM Organisation

CorporateGroup
    != LegalEntity

CorporateGroup
    != Tenant

CorporateRelationship
    != CounterpartyRelationship

CorporateRelationship
    != ADR-BCP-012 operational LegalEntityRelationship

CorporateRelationship
    != PlatformRelationship

PlatformRelationship
    != PlatformAccount

PlatformRelationship
    != ProductSubscription

PlatformAccount
    != Tenant

PlatformAccount
    != Authorization Boundary

Tenant
    != ProductSubscription

ProductSubscription
    != CapabilityGrant

Corporate Ownership
    != Authorization

Common Parent
    != Shared Data Access

Common Platform Account
    != Shared Data Access

Common Subscription Type
    != Shared Data Access
```

---

# 5. Core Concept Matrix

| Concept | Question Answered | Scope | Security Boundary? | Primary Authority |
|---|---|---|---:|---|
| CanonicalEntity | What canonical thing is this? | Platform | No | Control Plane |
| Organisation | Which real-world organisation is this? | Platform | No | Control Plane |
| LegalEntityProfile | What legally recognised person does this organisation represent? | Platform | No | Control Plane runtime; governed source retained |
| CorporateRelationship | Who owns, controls or is corporately related to whom? | Platform | No | Control Plane |
| CorporateGroup | Which organisations form a governed economic/corporate group? | Platform | No | Control Plane projection |
| PlatformRelationship | How does this organisation relate to Baobab Platform? | Platform | No | Control Plane |
| PlatformAccount | Which organisations/tenants share commercial or administrative arrangements with Baobab? | Platform | No | Control Plane |
| Tenant | What is the Baobab isolation and consumption boundary? | Platform | **Yes** | Control Plane |
| ProductSubscription | What product has this tenant contracted for or been assigned? | Tenant | No | Control Plane |
| CapabilityGrant | What platform capability is actually entitled? | Tenant/context | Authorization input | Control Plane |
| IAM Organisation | Which identity-provider organisation/member projection applies? | IAM realm | Authentication context only | Baobab IAM |
| CounterpartyRelationship | How does one tenant commercially relate to an organisation? | Tenant | No | ADR-BCP-014 model |

---

# 6. CanonicalEntity Remains the Identity Spine

ADR-BCP-014 established:

```text
CanonicalEntity
```

as the universal platform identity abstraction.

This ADR SHALL NOT introduce a second universal identity system.

Instead:

```text
CanonicalEntity
      │
      ▼
OrganisationProfile
```

SHALL become the canonical representation of a real-world organisation.

Conceptually:

```text
CanonicalEntity
├── canonical_entity_id
├── entity_type
├── canonical_key
├── status
├── classification
├── authority
├── effective_from
├── effective_to?
└── metadata
        │
        ▼
OrganisationProfile
```

An organisation's canonical platform identity SHALL therefore remain anchored to the existing `CanonicalEntity`.

---

# 7. Generic Organisation Model

Baobab SHALL introduce a platform-wide generic Organisation model.

Conceptually:

```text
Organisation
├── canonical_entity_id
├── display_name
├── official_name?
├── trading_names[]
├── organisation_form?
├── jurisdiction?
├── verification_state
├── source_authority
├── status
├── effective_from
├── effective_to?
├── identifiers[]
├── addresses[]
└── metadata
```

The model SHALL describe identity and organisational form.

It SHALL NOT encode commercial role.

Therefore:

```text
OrganisationType = SUPPLIER
```

is prohibited.

Likewise:

```text
OrganisationType = CUSTOMER
```

is prohibited.

Those are relationships or roles, not identities.

---

# 8. Organisation Form Is Not Commercial Role

Possible organisational forms may include:

```text
COMPANY
PARTNERSHIP
TRUST
ASSOCIATION
PUBLIC_BODY
NONPROFIT
UNINCORPORATED_ORGANISATION
OTHER
```

Exact values SHALL be extensible and contract-governed.

They answer:

> What sort of organisation is this?

They do not answer:

> What does this organisation do for this tenant?

That remains the responsibility of:

```text
CounterpartyRole
CounterpartyRelationship
domain accounts
```

per ADR-BCP-014.

---

# 9. Organisation and Legal Entity

An Organisation MAY have a legally recognised identity.

Where it does, Baobab SHALL attach a:

```text
LegalEntityProfile
```

Conceptually:

```text
LegalEntityProfile
├── legal_entity_id
├── organisation_id
├── legal_name
├── jurisdiction_of_incorporation
├── registration_identifiers[]
├── incorporation_date?
├── legal_status
├── source_authority
├── verification_state
├── effective_from
├── effective_to?
└── evidence_references[]
```

The LegalEntityProfile SHALL NOT become a replacement Organisation identity.

Therefore:

```text
organisation_id != legal_entity_id
```

even where the relationship is one-to-one.

---

# 10. One Real Legal Person, One Canonical Organisation

Where possible and verified, one real-world legal person SHALL correspond to one canonical Organisation.

Different appearances of the same organisation as:

```text
customer
supplier
carrier
partner
tenant
contracting party
ERP Business Partner
Keycloak Organization
Medusa B2B organisation
```

SHALL NOT create independent canonical identities.

Instead:

```text
                    Canonical Organisation
                             │
           ┌─────────────────┼─────────────────┐
           ▼                 ▼                 ▼
        Customer          Supplier          Carrier
         Role              Role              Role
           │                 │                 │
           ▼                 ▼                 ▼
        Trade             ERP             Logistics
      Projection        Projection         Projection
```

---

# 11. Existing Buyer and Supplier Organisation Types

ADR-BCP-016 currently recognises:

```text
BUYER_ORGANISATION
SUPPLIER_ORGANISATION
```

as bounded canonical entity types.

Those records SHALL remain valid.

This ADR SHALL NOT require destructive replacement of their identifiers.

However, the target architecture SHALL generalise them.

Conceptually:

```text
Existing BUYER_ORGANISATION
          │
          ▼
OrganisationProfile
          │
          ▼
Buyer Role / CounterpartyProfile
```

and:

```text
Existing SUPPLIER_ORGANISATION
          │
          ▼
OrganisationProfile
          │
          ▼
Supplier Role / CounterpartyProfile
```

Future generic organisations SHOULD use a generic Organisation representation rather than encode one commercial role in `EntityType`.

Where existing buyer and supplier records are later discovered to represent the same real organisation, they SHALL be reconciled through governed identity resolution.

They SHALL NOT be silently merged.

---

# 12. Legal Entity Identifier Strategy

Existing first-party identifiers such as:

```text
NABHOLD
ZURIBEANS
THAMANI-GLOBAL
EQUATOR-ESTATE
```

SHALL remain stable.

They SHALL NOT be renamed merely because the GitHub organisation, repository namespace, commercial brand or platform name changes.

For newly admitted external legal entities, Baobab SHOULD mint opaque legal-entity identifiers rather than embed:

```text
company name
country
market
brand
subsidiary role
```

in identity.

A compatible form may be:

```text
LE-<opaque-token>
```

provided it conforms to the versioned Shared contract.

Human-readable identifiers SHALL be represented as:

```text
aliases
external references
registration identifiers
display names
```

rather than the immutable runtime identity.

---

# 13. Shared Legal-Entity Registry Reconciliation

The current Shared legal-entity registry is appropriate as a governance source for known first-party Nabhold entities.

It is not suitable as Baobab's universal runtime customer database.

Baobab SHALL therefore distinguish:

```text
Shared Governance Source
```

from:

```text
Control Plane Runtime Registry
```

The target authority model SHALL be:

```text
             baobab-platform/shared
          Contract + First-Party Governance
                        │
                        │ seed / reconcile
                        ▼
                Baobab Control Plane
          Canonical Runtime Organisation
             and Legal Entity Registry
                        │
        ┌───────────────┼───────────────┐
        ▼               ▼               ▼
     Trade             ERP             IAM
   projection        projection       projection
```

For first-party Nabhold entities:

```text
Shared governance record
        │
        ▼
CP canonical runtime record
```

The Control Plane SHALL NOT independently alter authoritative Nabhold governance facts.

For external organisations:

```text
Admission / Verified Evidence
        │
        ▼
CP canonical runtime record
```

A source-code pull request SHALL NOT be required for every new Baobab customer.

---

# 14. Amendment to Shared Tenancy Semantics

The current Shared tenancy contract requires the default legal entity to exist in the source-controlled Shared registry.

That universal requirement SHALL be superseded.

The target rule SHALL be:

> **A tenant's legal-entity mappings SHALL reference active canonical legal entities known to the Control Plane. First-party Nabhold legal entities SHALL additionally reconcile to Shared governance records.**

Therefore:

```text
External customer
    ↓
does NOT require
    ↓
editing shared/contracts/legal-entity/registry.yaml
```

before onboarding.

Shared SHALL remain:

```text
contract authority
schema authority
first-party governance source
```

rather than becoming:

```text
the transactional customer master database.
```

---

# 15. Organisation Authority Decision

The unresolved Organisation authority identified by ADR-BCP-016 is settled by this ADR.

The broader platform-wide:

```text
Organisation
```

runtime authority SHALL be:

```text
baobab-platform/baobab-cp
```

`baobab-platform/shared` SHALL remain the contract/schema authority.

Therefore `contracts/erp/v1/system-of-record.yaml` SHALL eventually be reconciled from:

```text
Organisation:
canonical_owner: unassigned
```

to:

```text
Organisation:
canonical_runtime_owner: control-plane
contract_authority: shared
```

with provider engines remaining projections.

---

# 16. Legal Entity Authority Reconciliation

ADR-BCP-012 currently states that CP owns canonical legal-entity identity, while Shared currently declares Legal Entity canonical ownership.

This ADR resolves the apparent conflict.

The target model is:

```text
Shared
    ↓
governance / contract / first-party source authority

Control Plane
    ↓
canonical runtime registry and relationship resolution

External authoritative evidence
    ↓
source facts for admitted external legal entities
```

The Control Plane SHALL retain provenance explaining where each legal fact came from.

It SHALL NOT pretend to be the legal authority that incorporated the company.

---

# 17. CorporateRelationship

Baobab SHALL introduce an explicit platform-wide:

```text
CorporateRelationship
```

This relationship SHALL NOT contain a `tenant_id`.

Conceptually:

```text
CorporateRelationship
├── relationship_id
├── source_organisation_id
├── target_organisation_id
├── relationship_type
├── ownership_percentage?
├── control_basis?
├── direct_or_derived
├── status
├── verification_state
├── effective_from
├── effective_to?
├── evidence_references[]
├── source_authority
├── verified_by?
├── verified_at?
├── classification
└── metadata
```

Its scope is the real-world organisation graph.

---

# 18. Initial Corporate Relationship Types

Initial canonical relationship types SHOULD include:

```text
OWNS
CONTROLS
BRANCH_OF
AFFILIATE_OF
JOINT_VENTURE_WITH
SUCCESSOR_OF
```

The vocabulary SHALL remain extensible.

A relationship such as:

```text
Acme Holdings OWNS Acme Foods
```

is different from:

```text
Acme Foods SUPPLIER_OF ZuriBeans
```

The first is corporate.

The second is counterparty/commercial.

---

# 19. Do Not Store Redundant Inverse Facts

Where:

```text
A OWNS B
```

is canonical, Baobab SHOULD derive:

```text
B is owned by A
```

rather than store a second independent relationship unless required by the contract.

Likewise:

```text
PARENT_SUBSIDIARY
SISTER_SUBSIDIARY
COMMONLY_CONTROLLED
```

MAY often be derived classifications.

Derived facts SHALL retain lineage to the source relationships.

---

# 20. Corporate Graph, Not Corporate Tree

Baobab SHALL model corporate structures as graphs rather than assuming a simple tree.

Real corporate structures may contain:

```text
multiple shareholders
joint ventures
cross-holdings
indirect ownership
multiple controllers
non-equity control
branches
affiliates
reorganisations
```

Therefore the model SHALL NOT assume:

```text
one organisation = exactly one parent
```

or:

```text
every corporate group has exactly one ultimate parent
```

Where a unique parent cannot be established safely, the result SHALL remain ambiguous.

Security or internal-subscription decisions SHALL fail closed rather than guess.

---

# 21. Ownership Is Not Control

The following SHALL remain distinct:

```text
ownership
```

and:

```text
control
```

A percentage shareholding SHALL NOT automatically be interpreted by generic platform code as legal control.

Likewise:

```text
50% ownership
```

SHALL NOT universally mean:

```text
joint control
```

or:

```text
no control.
```

Jurisdictional, contractual and governance policy may differ.

The relationship shall record facts and evidence.

Policy determines consequences.

---

# 22. Corporate Relationship Effective Dating

All corporate relationships SHALL be effective-dated.

Example:

```text
2026-01-01
Nabhold OWNS ZuriBeans

2028-06-30
relationship ends
```

The historical fact SHALL remain queryable.

Baobab SHALL support:

```text
relationship as of now
relationship as of transaction date
relationship as of reporting date
relationship as of admission date
```

This is necessary for:

```text
audit
transfer pricing
intercompany classification
historical reporting
subscription eligibility
acquisitions
divestitures
```

---

# 23. CorporateRelationship vs ADR-BCP-012

ADR-BCP-012 defines an operational:

```text
LegalEntityRelationship
```

containing `tenant_id`.

That model remains useful for internal-trade classification.

However:

> **ADR-BCP-012 LegalEntityRelationship SHALL NOT be treated as the canonical corporate ownership graph.**

Instead:

```text
CorporateRelationship
      │
      ▼
operational projection / classification
      │
      ▼
ADR-BCP-012 LegalEntityRelationship
```

For example:

```text
Nabhold OWNS ZuriBeans
```

may help ADR-BCP-012 determine:

```text
PARENT_SUBSIDIARY
```

for a particular transaction.

ADR-BCP-012 remains authoritative for the transaction consequence.

ADR-BCP-018 remains authoritative for the corporate fact.

---

# 24. `EXTERNAL` Is Not a Corporate Relationship Fact

ADR-BCP-012 includes an operational classification:

```text
EXTERNAL
```

This SHALL NOT require an explicit global:

```text
Organisation A EXTERNAL_TO Organisation B
```

relationship for every unrelated pair in the world.

Instead, `EXTERNAL` MAY be derived when no qualifying related-party relationship exists under the applicable policy.

---

# 25. CorporateRelationship vs CounterpartyRelationship

| Dimension | CorporateRelationship | CounterpartyRelationship |
|---|---|---|
| Purpose | Ownership/control/group structure | Tenant-specific business relationship |
| Scope | Platform-wide | Tenant-scoped |
| `tenant_id` | **Forbidden** | Required |
| Examples | OWNS, CONTROLS, AFFILIATE_OF | SUPPLIER_OF, CUSTOMER_OF, CARRIER_FOR |
| Can exist without a tenant? | Yes | Normally no |
| Changes tenant authorization? | No | No, by itself |
| Authority | Control Plane | ADR-BCP-014 model |
| Effective dated | Yes | Yes |
| Evidence | Corporate/legal evidence | Commercial/domain evidence |

---

# 26. CorporateGroup

Baobab SHALL support a first-class conceptual:

```text
CorporateGroup
```

where grouping is useful.

A CorporateGroup represents an economic or governance grouping of organisations.

It SHALL NOT automatically be treated as a LegalEntity.

Conceptually:

```text
CorporateGroup
├── group_id
├── display_name
├── root_organisation_id?
├── status
├── grouping_policy
├── effective_from
├── effective_to?
└── metadata
```

Example:

```text
CorporateGroup: ACME GLOBAL GROUP

Acme Holdings Ltd
├── Acme Foods Ltd
├── Acme Logistics Ltd
└── Acme Retail Ltd
```

---

# 27. Corporate Group Is Not Necessarily the Holding Company

The following SHALL NOT be assumed equivalent:

```text
CorporateGroup
        =
Holding Company LegalEntity
```

Example:

```text
"Acme Global Group"
```

may be an economic grouping.

The actual legal parent may be:

```text
Acme Holdings Ltd
```

They may have similar names.

They remain separate concepts.

---

# 28. Corporate Group Membership Is Derived or Evidenced

CorporateGroup membership SHALL NOT silently become a second ownership database.

Membership SHOULD be backed by:

```text
CorporateRelationship references
```

and may be materialised for efficient querying.

Conceptually:

```text
CorporateGroupMembership
├── group_id
├── organisation_id
├── group_role?
├── basis_relationship_ids[]
├── effective_from
├── effective_to?
├── derived_at
└── derivation_version
```

Where membership cannot be derived automatically, an explicitly governed manual basis MAY be recorded.

---

# 29. Corporate Group Is Not an Authorization Boundary

This is a critical invariant.

```text
Organisation A
and
Organisation B
belong to the same CorporateGroup
```

does not mean:

```text
Organisation A may access Organisation B's tenant.
```

Similarly:

```text
parent company
```

does not automatically mean:

```text
tenant administrator of subsidiary.
```

Any such authority requires a separate explicit authorization mechanism.

---

# 30. Baobab Platform Is Not a Legal Entity

The platform itself SHALL have a canonical platform identity, conceptually:

```text
platform:baobab
```

or equivalent governed identifier.

The Baobab Platform SHALL NOT be conflated with:

```text
Nabhold Group Africa
```

or:

```text
baobab-platform GitHub organisation
```

The relationship is:

```text
Nabhold Group Africa
        │
        │ PLATFORM_OWNER
        ▼
   Baobab Platform
```

rather than:

```text
Nabhold Group Africa = Baobab Platform
```

---

# 31. PlatformRelationship

Baobab SHALL introduce:

```text
PlatformRelationship
```

to describe the durable relationship between an Organisation and the Baobab Platform.

Conceptually:

```text
PlatformRelationship
├── platform_relationship_id
├── platform_id
├── organisation_id
├── relationship_type
├── status
├── basis_relationship_id?
├── admission_decision_id?
├── effective_from
├── effective_to?
├── evidence_references[]
├── verified_by?
├── verified_at?
└── metadata
```

It SHALL NOT carry runtime authorization.

---

# 32. Initial PlatformRelationship Types

Initial types SHOULD include:

```text
PLATFORM_OWNER
PLATFORM_OPERATOR
PLATFORM_GROUP_AFFILIATE
EXTERNAL_CLIENT
PLATFORM_PARTNER
MANAGED_ENTITY
```

The set SHALL remain extensible.

---

# 33. PLATFORM_OWNER

`PLATFORM_OWNER` identifies the organisation that owns the Baobab Platform or the relevant platform business interest.

Initially this may be:

```text
Nabhold Group Africa
```

The architecture SHALL NOT hard-code that assumption permanently.

Ownership may change through:

```text
restructuring
spin-off
joint venture
sale
new operating entity
```

without replacing tenant IDs.

---

# 34. PLATFORM_OPERATOR

`PLATFORM_OPERATOR` identifies the organisation operationally responsible for Baobab.

The owner and operator MAY be the same organisation.

They SHALL NOT be required to remain the same forever.

Example:

```text
Nabhold Group Africa
     PLATFORM_OWNER

Baobab Platform Services Ltd
     PLATFORM_OPERATOR
```

would be valid if such an operating structure were adopted later.

---

# 35. PLATFORM_GROUP_AFFILIATE

`PLATFORM_GROUP_AFFILIATE` identifies an organisation whose governed corporate relationship places it within the applicable first-party platform-owner group.

For example:

```text
ZuriBeans
Thamani Global
Equator & Estate Co.
```

may carry this relationship while qualifying.

The status SHALL be evidence-backed and effective-dated.

It SHALL NOT be self-assigned.

---

# 36. EXTERNAL_CLIENT

An unrelated external Baobab customer SHALL normally carry:

```text
EXTERNAL_CLIENT
```

This describes its relationship to Baobab.

It does not describe its own internal corporate hierarchy.

Thus:

```text
Acme Holdings
    EXTERNAL_CLIENT

Acme Foods
    EXTERNAL_CLIENT

Acme Logistics
    EXTERNAL_CLIENT
```

may simultaneously be:

```text
Acme Holdings OWNS Acme Foods
Acme Holdings OWNS Acme Logistics
```

The corporate and platform relationship planes remain independent.

---

# 37. A Corporate Group Does Not Become First-Party Merely Because It Has Subsidiaries

An external customer's subsidiaries SHALL remain external with respect to Baobab unless an independent governed relationship says otherwise.

Therefore:

```text
Acme Holdings
     └── subsidiary → Acme Foods
```

does NOT cause:

```text
Acme Foods
→ PLATFORM_GROUP_AFFILIATE
```

`PLATFORM_GROUP_AFFILIATE` refers specifically to the platform owner's governed first-party group.

---

# 38. PlatformRelationship Is Not Subscription Type

This ADR formally establishes:

```text
PlatformRelationship
        !=
ProductSubscription.subscription_type
```

Examples:

```text
Organisation:
Acme Ltd

PlatformRelationship:
EXTERNAL_CLIENT

Subscription:
TRIAL
```

Later:

```text
PlatformRelationship:
EXTERNAL_CLIENT

Subscription:
COMMERCIAL
```

No platform relationship changed.

Likewise:

```text
Organisation:
ZuriBeans

PlatformRelationship:
PLATFORM_GROUP_AFFILIATE

Subscription:
INTERNAL
```

If ZuriBeans were divested:

```text
PlatformRelationship:
EXTERNAL_CLIENT

Subscription:
COMMERCIAL
```

after governed transition.

These are separate lifecycle changes.

---

# 39. PlatformRelationship and Subscription Compatibility

Illustrative policy relationships include:

| Platform Relationship | Possible Subscription Types |
|---|---|
| PLATFORM_OWNER | INTERNAL, MANUAL |
| PLATFORM_OPERATOR | INTERNAL, MANUAL |
| PLATFORM_GROUP_AFFILIATE | INTERNAL, MANUAL |
| EXTERNAL_CLIENT | COMMERCIAL, TRIAL, MIGRATION, MANUAL |
| PLATFORM_PARTNER | PARTNER, COMMERCIAL, TRIAL |
| MANAGED_ENTITY | COMMERCIAL, PARTNER, MANUAL |

This table is not a one-to-one mapping.

The actual subscription SHALL remain an explicit object.

---

# 40. Internal Subscription Eligibility

ADR-BCP-017's internal-eligibility mechanism SHALL use authoritative relationship evidence defined by this ADR.

A typical evaluation SHALL be:

```text
Organisation
      │
      ▼
Active verified CorporateRelationship?
      │
      ▼
Eligible membership in platform-owner group?
      │
      ▼
Active PLATFORM_GROUP_AFFILIATE?
      │
      ▼
Internal subscription policy satisfied?
      │
      ├── NO → deny INTERNAL classification
      │
      └── YES
             │
             ▼
       subscription_type = INTERNAL
```

`PLATFORM_GROUP_AFFILIATE` alone SHOULD NOT be accepted when its underlying evidence has become invalid.

---

# 41. PlatformAccount

Baobab SHALL support a commercial and administrative grouping object:

```text
PlatformAccount
```

A PlatformAccount represents:

```text
master service relationship
commercial account
enterprise customer account
contract grouping
billing grouping
support grouping
```

It SHALL NOT represent:

```text
legal identity
corporate ownership
tenant identity
authorization
```

---

# 42. PlatformAccount Model

Conceptually:

```text
PlatformAccount
├── platform_account_id
├── display_name
├── status
├── primary_organisation_id?
├── contract_references[]
├── billing_profile_reference?
├── support_profile_reference?
├── effective_from
├── effective_to?
└── metadata
```

Organisations SHALL participate through explicit memberships.

---

# 43. PlatformAccountMembership

Conceptually:

```text
PlatformAccountMembership
├── platform_account_id
├── organisation_id
├── account_role
├── status
├── effective_from
├── effective_to?
└── evidence_reference?
```

Potential account roles MAY include:

```text
CONTRACTING_PARTY
BILLING_PARTY
SERVICE_RECIPIENT
ACCOUNT_MEMBER
PRIMARY_ACCOUNT_ORGANISATION
```

These are commercial/administrative roles.

They SHALL NOT become runtime permissions.

---

# 44. Enterprise Customer Example

An external enterprise may sign one master agreement:

```text
                    Acme Global Group
                           │
             Corporate Relationship Graph
                           │
        ┌──────────────────┼───────────────────┐
        ▼                  ▼                   ▼
 Acme Holdings        Acme Foods       Acme Logistics
        │                  │                   │
        └──────────────────┼───────────────────┘
                           │
                           ▼
                   PlatformAccount
                    ACME ENTERPRISE
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
         Tenant-H      Tenant-F      Tenant-L
```

Possible roles:

```text
Acme Holdings
→ CONTRACTING_PARTY
→ BILLING_PARTY

Acme Foods
→ SERVICE_RECIPIENT

Acme Logistics
→ SERVICE_RECIPIENT
```

This permits:

```text
one master agreement
multiple legal entities
multiple tenants
multiple subscriptions
separate data isolation
```

---

# 45. PlatformAccount Does Not Collapse Tenants

The following is prohibited:

```text
same PlatformAccount
therefore
same tenant
```

and:

```text
same PlatformAccount
therefore
cross-tenant access.
```

A PlatformAccount MAY contain:

```text
1 tenant
10 tenants
100 tenants
```

without changing tenant isolation.

---

# 46. One Corporate Group May Have Multiple Platform Accounts

Baobab SHALL support:

```text
Acme Global Group
├── PlatformAccount: ACME-GLOBAL-MSA
├── PlatformAccount: ACME-AFRICA
└── PlatformAccount: ACME-RETAIL
```

if separate commercial arrangements require it.

Corporate structure SHALL NOT dictate commercial account structure.

---

# 47. One Platform Account May Span Organisations from Different Structures

Where a valid contract requires it, a PlatformAccount MAY include organisations that are not part of one traditional parent-subsidiary tree.

Examples may include:

```text
joint venture
consortium
managed enterprise arrangement
strategic partnership
```

Such membership SHALL NOT cause Baobab to invent corporate ownership between those organisations.

---

# 48. PlatformAccount and ProductSubscription

ProductSubscription remains tenant-owned.

A subscription MAY reference:

```text
platform_account_id
contract_reference
billing_reference
```

for provenance.

Conceptually:

```text
PlatformAccount
       │
       ▼
Commercial Terms
       │
       ▼
Tenant
       │
       ▼
ProductSubscription
       │
       ▼
CapabilityGrant
```

PlatformAccount SHALL NOT replace ProductSubscription.

---

# 49. Tenant Remains the Primary Isolation Boundary

Nothing in this ADR weakens the current tenant model.

Tenant SHALL continue to anchor:

```text
data isolation
platform consumption
capability grants
usage attribution
audit
configuration
provider context
residency
readiness
```

A corporate group SHALL NOT replace Tenant.

A PlatformAccount SHALL NOT replace Tenant.

An Organisation SHALL NOT replace Tenant.

---

# 50. TenantOrganisationMapping

Baobab SHALL support explicit organisation-to-tenant mappings.

Conceptually:

```text
TenantOrganisationMapping
├── mapping_id
├── tenant_id
├── organisation_id
├── mapping_role
├── status
├── effective_from
├── effective_to?
└── provenance
```

Potential roles MAY include:

```text
PRIMARY_ORGANISATION
OPERATING_ORGANISATION
ADDITIONAL_ORGANISATION
```

The exact vocabulary SHALL be contract-governed.

---

# 51. TenantLegalEntityMapping

Legal-entity mappings SHALL remain explicit.

Conceptually:

```text
TenantLegalEntityMapping
├── mapping_id
├── tenant_id
├── legal_entity_id
├── mapping_role
├── status
├── effective_from
├── effective_to?
└── provenance
```

At least one mapping MAY be marked:

```text
DEFAULT
```

for version-1 compatibility.

---

# 52. Default Legal Entity Remains a Compatibility Concept

The current runtime model contains:

```text
Tenant.LegalEntityID
```

as a singular field.

This ADR SHALL NOT require destructive replacement.

Instead:

```text
Tenant.LegalEntityID
```

SHALL temporarily represent:

```text
default active legal-entity mapping
```

while the explicit mapping model is introduced.

The long-term source of truth SHALL be the mapping set.

---

# 53. Tenant / Legal Entity Cardinality

The existing Shared rule remains:

```text
Tenant → one or more legal entities
```

where explicitly approved.

Likewise:

```text
Legal Entity → one or more tenants
```

where isolation or regional topology requires it.

The default remains:

```text
one legal entity
≈
one tenant boundary
```

but this is a default operating topology, not an identity equation.

---

# 54. External Enterprise Example — All Subsidiaries Are Clients

Baobab SHALL support:

```text
Acme Holdings Ltd
├── Acme Foods Ltd
├── Acme Logistics Ltd
└── Acme Retail Ltd
```

Corporate graph:

```text
Acme Holdings
    │
    ├── OWNS ──► Acme Foods
    ├── OWNS ──► Acme Logistics
    └── OWNS ──► Acme Retail
```

Platform relationships:

```text
Acme Holdings
→ EXTERNAL_CLIENT

Acme Foods
→ EXTERNAL_CLIENT

Acme Logistics
→ EXTERNAL_CLIENT

Acme Retail
→ EXTERNAL_CLIENT
```

Tenants:

```text
Acme Holdings
→ tn_holding

Acme Foods
→ tn_foods

Acme Logistics
→ tn_logistics

Acme Retail
→ tn_retail
```

Subscriptions:

```text
tn_holding
→ COMMERCIAL

tn_foods
→ COMMERCIAL

tn_logistics
→ COMMERCIAL

tn_retail
→ COMMERCIAL
```

Nothing about common ownership weakens isolation.

---

# 55. External Enterprise Example — Parent Is Not a Tenant

Baobab SHALL also support:

```text
Acme Holdings
    │
    ├── Acme Foods      → Baobab client
    ├── Acme Logistics  → Baobab client
    └── Acme Retail     → not a client
```

Acme Holdings MAY exist in CP only because its identity is necessary to establish the corporate structure.

It may have:

```text
Organisation record
CorporateRelationships
PlatformAccount membership
```

without having:

```text
Tenant
ProductSubscription
CapabilityGrant
```

This is valid.

---

# 56. Reference-Only Related Organisations

Baobab MAY record an organisation that is not itself a customer when necessary to establish:

```text
parent ownership
group membership
related-party status
internal eligibility
contracting authority
compliance
group reporting scope
```

Such a record SHALL be data-minimised.

Baobab SHALL NOT ingest an entire corporate family tree merely because one customer exists.

---

# 57. Data-Minimisation Principle

Corporate information SHALL be collected only when it serves an explicit platform, legal, operational, contractual, security or compliance purpose.

Baobab SHOULD NOT collect:

```text
irrelevant remote affiliates
unnecessary beneficial-owner personal information
unrelated directors
unrelated subsidiaries
historic entities with no platform relevance
```

without a defined requirement.

Where personal information accompanies corporate evidence, applicable data-protection controls SHALL apply.

---

# 58. Nabhold Example

The same generic architecture SHALL model Nabhold:

```text
                     CorporateGroup
                  NABHOLD GROUP AFRICA
                           │
                           ▼
                 Nabhold Group Africa
                           │
       ┌───────────────────┼────────────────────┐
       │                   │                    │
       ▼                   ▼                    ▼
   ZuriBeans        Thamani Global      Equator & Estate
```

Corporate relationships:

```text
Nabhold OWNS ZuriBeans
Nabhold OWNS Thamani Global
Nabhold OWNS Equator & Estate
```

Platform relationships:

```text
Nabhold
→ PLATFORM_OWNER

ZuriBeans
→ PLATFORM_GROUP_AFFILIATE

Thamani Global
→ PLATFORM_GROUP_AFFILIATE

Equator & Estate
→ PLATFORM_GROUP_AFFILIATE
```

Subscriptions may be:

```text
INTERNAL
```

subject to ADR-BCP-017 eligibility policy.

---

# 59. Nabhold Must Not Be Special-Cased in Runtime Logic

The following is prohibited:

```go
if legalEntityID == "ZURIBEANS" {
    subscriptionType = "INTERNAL"
}
```

Likewise:

```go
if strings.HasSuffix(email, "@nabhold...") {
    allowInternalAccess()
}
```

Instead:

```text
Canonical Organisation
       +
Verified Corporate Relationship
       +
Platform Relationship
       +
Internal Eligibility Policy
       =
Internal Classification Decision
```

---

# 60. Corporate Affiliation Does Not Imply Authorization

This is a security invariant.

The following SHALL be false:

```text
parent organisation
    => subsidiary access
```

```text
sister subsidiaries
    => mutual access
```

```text
same CorporateGroup
    => shared authorization
```

```text
same PlatformAccount
    => shared authorization
```

```text
PLATFORM_GROUP_AFFILIATE
    => platform-admin permission
```

---

# 61. Cross-Tenant Group Administration

Where an enterprise legitimately requires central administration:

```text
Acme Holdings administrator
        │
        ▼
manage selected settings
        │
        ▼
Acme Foods tenant
Acme Logistics tenant
```

Baobab SHALL require an explicit delegated-authorization object or equivalent governed grant.

CorporateRelationship SHALL only supply context.

PlatformAccount SHALL only supply commercial/administrative grouping.

Neither SHALL themselves authorize the action.

---

# 62. Cross-Tenant Reporting

The same rule applies to group reporting.

A parent organisation MAY require:

```text
consolidated spend
group revenue
group inventory
group risk
executive dashboards
```

across subsidiaries.

Such access SHALL use an explicit:

```text
reporting scope
cross-tenant aggregation capability
delegated grant
or governed analytics projection
```

It SHALL NOT query subsidiary data merely because the corporate graph says they share a parent.

---

# 63. Nabhold Executive Visibility

Nabhold executive visibility into subsidiaries SHALL follow the same principle.

The corporate graph MAY establish:

```text
Nabhold owns ZuriBeans
```

but access to:

```text
ZuriBeans operations
Thamani operations
Equator & Estate operations
```

SHALL still require explicit capability and authorization.

Group ownership is not a database bypass.

---

# 64. IAM Organisation Remains a Projection

Keycloak Organization SHALL remain an IAM-native projection.

Conceptually:

```text
Canonical Organisation
        │
        ▼
ExternalReference
        │
        ▼
Keycloak Organization
```

Keycloak SHALL remain authoritative for:

```text
authentication
identity federation
membership authentication context
sessions
credentials
MFA
OIDC/OAuth
```

It SHALL NOT become authoritative for:

```text
corporate ownership
legal identity
platform customer classification
tenant isolation
product subscription
capability entitlement
```

---

# 65. IAM Organisation Cardinality

A canonical Organisation MAY map to:

```text
one
or
multiple
```

IAM-native organisation representations where required by:

```text
environment
realm
region
identity federation topology
customer identity architecture
```

The mapping SHALL be explicit through `ExternalReference` or canonical Mapping.

Provider IDs SHALL never replace canonical Organisation identity.

---

# 66. IAM Organisation Claim Is Context Evidence, Not Truth

Where an IAM token contains an organisation claim:

```text
organization = <provider-native-id>
```

the Control Plane SHALL resolve that identifier to a canonical Organisation.

It SHALL NOT simply echo the token value into trusted business context.

The flow SHALL remain:

```text
Authenticated Principal
        │
        ▼
IAM Organisation Claim / Membership
        │
        ▼
ExternalReference lookup
        │
        ▼
Canonical Organisation
        │
        ▼
Tenant relationship validation
        │
        ▼
Authoritative PlatformContext
```

---

# 67. Corporate Parent Claims Shall Not Be Carried as Trust Shortcuts

IAM SHALL NOT mint claims such as:

```text
ultimate_parent = ACME
allow_all_subsidiaries = true
group_admin = automatic
```

and expect downstream systems to infer authorization.

If group administration is required, the authorization SHALL be explicit, revocable and audited.

---

# 68. Admission Integration

ADR-BCP-017 SHALL be extended by this model.

The target external admission lifecycle becomes:

```text
Applicant Registration
        │
        ▼
ClientApplication
        │
        ▼
ApplicantOrganisationProfile
        │
        ▼
Identity Resolution
   ┌────┴────┐
   │         │
Existing     New
Org          Org
   │         │
   └────┬────┘
        ▼
Organisation Verification
        │
        ▼
LegalEntityProfile
        │
        ▼
Corporate Relationship Assessment
        │
        ▼
PlatformRelationship
        │
        ▼
PlatformAccount Assignment
        │
        ▼
Admission Decision
        │
        ▼
TenantOnboardingRequest
        │
        ▼
Tenant + Explicit Mappings
        │
        ▼
ProductSubscription
        │
        ▼
Capability Grants
```

---

# 69. Admission Shall Not Create Corporate Facts from Applicant Claims Alone

An applicant MAY state:

```text
"We are a subsidiary of Acme Holdings."
```

This is evidence.

It is not automatically:

```text
verified CorporateRelationship.
```

Baobab SHALL require appropriate verification before the relationship becomes authoritative for:

```text
internal eligibility
corporate reporting
related-party treatment
delegated administration
billing consolidation
```

---

# 70. External Clients Cannot Self-Classify as First-Party

An external applicant SHALL NOT be able to submit:

```text
platform_relationship = PLATFORM_GROUP_AFFILIATE
```

as an authoritative value.

Likewise they SHALL NOT self-assign:

```text
PLATFORM_OWNER
PLATFORM_OPERATOR
subscription_type = INTERNAL
```

Such values SHALL be server-authoritative.

---

# 71. Relationship Verification

Corporate relationships SHALL support verification state.

Suggested states:

```text
UNVERIFIED
PENDING_REVIEW
VERIFIED
CONFLICTED
REJECTED
EXPIRED
```

Only relationships meeting policy requirements SHALL be used for consequential decisions.

---

# 72. Relationship Evidence

Evidence MAY include references to:

```text
company registry
corporate filing
shareholder register
board/governance record
contract
verified customer documentation
legal review
trusted data provider
internal company-secretarial record
```

Evidence binaries SHOULD live in an appropriate document system.

CorporateRelationship SHOULD store references, not uncontrolled document blobs.

---

# 73. Conflicting Evidence

If authoritative sources disagree:

```text
Source A: Acme Holdings owns 70%
Source B: Acme Holdings owns 40%
```

Baobab SHALL NOT silently choose the convenient value.

The relationship SHALL enter:

```text
CONFLICTED
```

or equivalent review state.

Consequential policy SHALL fail closed where required.

---

# 74. Corporate Changes and Divestiture

A change in ownership SHALL NOT replace canonical Organisation identity.

Example:

```text
Before:
Nabhold OWNS ZuriBeans

After:
relationship ends
```

The Organisation remains:

```text
same organisation_id
same legal_entity_id
same tenant_id
```

unless independent legal events require otherwise.

Instead:

```text
CorporateRelationship changes
        │
        ▼
PlatformRelationship review
        │
        ▼
Subscription eligibility review
        │
        ▼
Commercial reclassification plan
```

---

# 75. Divestiture Flow

Conceptually:

```text
CorporateRelationship ENDS
          │
          ▼
PLATFORM_GROUP_AFFILIATE review
          │
      ┌───┴────┐
      │        │
   Retain    Terminate
      │        │
      │        ▼
      │   EXTERNAL_CLIENT?
      │        │
      └───┬────┘
          ▼
Subscription Review
          │
    ┌─────┼─────┐
    ▼     ▼     ▼
INTERNAL COMMERCIAL OFFBOARD
          │
          ▼
No Tenant ID replacement
```

---

# 76. Acquisition

If one Baobab customer acquires another:

```text
Acme Holdings
      acquires
Beta Logistics
```

Baobab SHALL normally record:

```text
new CorporateRelationship
```

rather than merge:

```text
Acme organisation_id
and
Beta organisation_id.
```

Acquisition does not erase the acquired legal identity.

---

# 77. Merger and Successor Identity

A true legal merger MAY require:

```text
SUCCESSOR_OF
```

and retirement of a predecessor legal identity.

This SHALL be handled through governed canonical identity lifecycle and successor mappings.

Historical transactions SHALL continue to resolve to the historical legal person.

---

# 78. Renaming

A company name change SHALL NOT create a new Organisation.

Example:

```text
Acme Foods Ltd
        ↓
Acme Consumer Foods Ltd
```

should normally update:

```text
official_name
trading_names
identifiers/evidence
```

while preserving canonical identity.

---

# 79. Branches

A branch SHALL NOT automatically be treated as a separate legal entity.

Where a branch lacks separate legal personality:

```text
Branch Organisation / Operating Unit
        │
        ▼
BRANCH_OF
        │
        ▼
Parent Legal Organisation
```

may be used.

Where jurisdictional law gives the branch a distinct relevant legal identity, the legal model MAY differ.

Baobab SHALL rely on verified legal context rather than naming convention.

---

# 80. Joint Ventures

The model SHALL support:

```text
Company A
      \
       \
      Joint Venture
       /
      /
Company B
```

without requiring one parent.

Joint venture relationships SHALL NOT be forced into:

```text
PARENT_SUBSIDIARY
```

when that is not accurate.

---

# 81. Derived Corporate Facts

The Control Plane MAY compute:

```text
ultimate_parent
same_group
sister_entity
indirect_owner
ownership_chain
related_party
```

as derived projections.

Every derived fact SHALL preserve:

```text
basis_relationship_ids[]
derived_at
algorithm_version
as_of
```

Derived results SHALL NOT silently become source facts.

---

# 82. Graph Ambiguity

Where the corporate graph contains:

```text
cross-holdings
conflicting control
multiple parents
insufficient evidence
```

Baobab SHALL not invent a clean hierarchy merely for UI convenience.

The UI MAY display:

```text
relationship requires review
multiple controlling interests
ultimate parent unresolved
```

rather than fabricate certainty.

---

# 83. Platform Account Lifecycle

Recommended PlatformAccount lifecycle:

```text
PENDING
   │
   ▼
ACTIVE
   │
   ├────► SUSPENDED
   │          │
   │          └────► ACTIVE
   │
   └────► CLOSED
```

Closing an account SHALL NOT automatically delete:

```text
tenants
organisations
relationships
audit history
```

Those have their own lifecycles.

---

# 84. PlatformRelationship Lifecycle

Recommended lifecycle:

```text
PROPOSED
    │
    ▼
VERIFIED
    │
    ▼
ACTIVE
    │
    ├────► SUSPENDED
    │
    └────► ENDED
```

Invalid transitions SHALL be rejected.

---

# 85. CorporateRelationship Lifecycle

Recommended lifecycle:

```text
DRAFT
  │
  ▼
PENDING_VERIFICATION
  │
  ├────► REJECTED
  │
  ▼
VERIFIED
  │
  ▼
ACTIVE
  │
  ├────► CONFLICTED
  │           │
  │           └────► ACTIVE
  │
  ├────► SUPERSEDED
  │
  └────► ENDED
```

Historical records SHALL not be physically overwritten simply because a relationship changes.

---

# 86. Current-Time and Historical Resolution

Relationship resolution APIs SHOULD support:

```text
as_of
```

semantics.

Example:

```http
GET /v1/organisations/{id}/corporate-relationships?as_of=2028-03-31T23:59:59Z
```

Exact APIs SHALL be contract-first in Shared.

---

# 87. PlatformContext Integration

ADR-BCP-004's `PlatformContext` SHALL remain the runtime context.

This ADR enriches the authority behind:

```text
organisation
legal_entity
tenant
```

It SHALL NOT require every request to carry:

```text
corporate_group
platform_account
corporate_parent
```

unless those dimensions are material to that capability.

Runtime context SHALL remain minimal and purpose-specific.

---

# 88. Corporate Facts Are Usually Resolution Inputs, Not Request Inputs

Digital Estates SHOULD NOT routinely submit:

```text
corporate_group_id
parent_company_id
platform_relationship
```

as trusted request context.

When relevant, CP SHALL resolve those facts from canonical state.

---

# 89. No Hidden Tenant Inheritance

The following logic is prohibited:

```text
if organisation.parent has tenant:
    use parent tenant
```

and:

```text
if subsidiary has no tenant:
    use group tenant
```

unless a valid explicit TenantOrganisationMapping says so.

---

# 90. One Parent Contract Does Not Mean One Tenant

An enterprise master contract may cover:

```text
20 legal entities
```

while Baobab operates:

```text
20 tenant boundaries.
```

Commercial consolidation and security consolidation are independent decisions.

---

# 91. One Tenant Serving Multiple Legal Entities

Where approved, one tenant MAY serve multiple legal entities.

This SHALL require:

```text
explicit TenantLegalEntityMappings
appropriate isolation policy
legal/accounting review
capability scopes
audit
```

Common corporate ownership alone SHALL NOT justify it.

---

# 92. Multiple Tenants for One Legal Entity

A legal entity MAY require several tenants for:

```text
regional isolation
regulated business units
residency
acquisition transition
dedicated products
high-risk separation
```

The same legal entity identity SHALL be preserved.

---

# 93. Platform Relationship Does Not Own Business Data

`PlatformRelationship` SHALL contain only affiliation/governance state.

It SHALL NOT accumulate:

```text
orders
invoices
customer pricing
supplier qualification
credit limits
employee roles
purchase approvals
```

Those remain in the appropriate domain.

---

# 94. Platform Account Does Not Become CRM

PlatformAccount MAY store references to:

```text
commercial agreement
billing arrangement
support plan
account manager
```

but SHALL NOT evolve into an uncontrolled CRM database.

A future dedicated CRM or billing service MAY own richer commercial data.

CP SHALL retain only what is required for platform governance and routing.

---

# 95. ERP Projection

iDempiere SHALL remain a projection/consumer of canonical organisation and legal-entity identities where applicable.

Conceptually:

```text
Canonical Organisation
        │
        ▼
ExternalReference
        │
        ▼
iDempiere C_BPartner
```

and:

```text
Canonical Legal Entity
        │
        ▼
Explicit ERP Organisation Mapping
```

ERP identifiers SHALL not become the platform master.

---

# 96. Trade Projection

Medusa/Baobab Trade MAY maintain:

```text
buyer organisation
customer account
supplier account
company account
```

These SHALL map to canonical Organisations.

Trade MAY own commercial state.

It SHALL NOT own the global corporate graph.

---

# 97. Identity Projection

Keycloak may maintain:

```text
Organization
membership
federation
invitation
```

These are identity projections.

They SHALL map to canonical Organisation through:

```text
ExternalReference
```

and SHALL remain provider-replaceable.

---

# 98. Same Organisation Across Tenants

Baobab MAY know globally that:

```text
Organisation X
```

is the same legal organisation interacting with:

```text
ZuriBeans
Thamani
another external tenant
```

However tenant-private state SHALL remain isolated.

Example:

```text
Canonical Organisation:
Acme Ltd
```

may be global.

But:

```text
ZuriBeans credit terms for Acme
```

shall remain isolated from:

```text
Thamani commercial terms for Acme.
```

ADR-BCP-014 remains authoritative for that separation.

---

# 99. Identity Resolution

Organisation identity reconciliation SHALL use governed identifiers and evidence.

Potential inputs include:

```text
legal registration number
jurisdiction
tax identifier
trusted registry identifier
verified existing canonical mapping
```

Name-only matching SHALL NOT establish identity.

---

# 100. No Fuzzy Auto-Merge

The platform SHALL NOT automatically merge:

```text
Acme Holdings Ltd
```

with:

```text
Acme Holdings Africa Ltd
```

because their names appear similar.

Ambiguous identities SHALL be quarantined for review.

---

# 101. Duplicate Resolution

Where duplicate canonical records are confirmed:

```text
Organisation A
Organisation B
```

the merge process SHALL preserve:

```text
old identifiers
ExternalReferences
audit
relationship history
source evidence
successor mapping
```

No engine-native reference SHALL silently become orphaned.

---

# 102. Relationship Security Classification

Corporate and platform relationships MAY carry classifications such as:

```text
PUBLIC
INTERNAL
TENANT_CONFIDENTIAL
RESTRICTED
```

An organisation being globally canonical does NOT mean every tenant may enumerate its relationships.

---

# 103. No Corporate Graph Enumeration by Default

External tenant users SHALL NOT be able to enumerate:

```text
all Baobab customers
all parent companies
all platform accounts
all Nabhold subsidiaries
all external corporate relationships
```

unless explicitly authorized for a legitimate purpose.

The canonical graph is a platform resource.

---

# 104. Platform Administrators

Modification of:

```text
Organisation
LegalEntityProfile
CorporateRelationship
PlatformRelationship
PlatformAccount
TenantOrganisationMapping
TenantLegalEntityMapping
```

SHALL require privileged platform authority or an explicitly delegated workflow.

All changes SHALL be audited.

---

# 105. Separation of Duties

High-consequence relationship changes SHOULD support separation of duties.

Examples:

```text
mark organisation as PLATFORM_GROUP_AFFILIATE
change PLATFORM_OWNER
change legal identity
record acquisition/divestiture
bind external enterprise account
grant cross-tenant administration
```

SHOULD require stronger review than ordinary profile editing.

---

# 106. Threat Model

At minimum Baobab SHALL defend against:

| Threat | Required Behaviour |
|---|---|
| External applicant self-claims Nabhold affiliation | Reject / verify server-side |
| Applicant self-assigns INTERNAL subscription | Reject |
| Forged parent/subsidiary evidence | Review; do not activate relationship |
| User changes JWT organisation claim | CP resolves provider ID; fail closed |
| Parent company user queries subsidiary tenant | Deny absent explicit authorization |
| Same PlatformAccount used to cross tenant boundary | Deny |
| Stale group membership after divestiture | Reconcile and review eligibility |
| Similar company names auto-merged | Prohibited |
| Cross-tenant graph enumeration | Deny unless authorized |
| Platform admin silently rewrites ownership | Audit / approval controls |
| Missing corporate evidence | Do not infer |
| Same email domain interpreted as same company | Prohibited |
| Same website domain interpreted as ownership | Prohibited |
| Platform partner assumed trusted everywhere | Prohibited |
| Corporate owner assumed tenant admin | Prohibited |

---

# 107. Zero-Trust Principle

Corporate affiliation SHALL be treated as descriptive context.

It SHALL NOT establish implicit trust.

Conceptually:

```text
Corporate Ownership
      +
Platform Affiliation
      +
Authentication
```

still does NOT equal:

```text
Authorization
```

Authorization requires its own explicit policy and grant.

---

# 108. External Customer Group Onboarding Flow

```text
External Enterprise Application
            │
            ▼
Identify Applying Organisation
            │
            ▼
Existing Canonical Organisation?
       ┌────┴────┐
       │         │
      YES        NO
       │         │
       │         ▼
       │    Candidate Organisation
       │         │
       └────┬────┘
            ▼
      Verify Legal Identity
            │
            ▼
   Discover Relevant Group Context
            │
            ▼
 Verify Parent/Subsidiary Relationships
            │
            ▼
 Create / Reconcile Corporate Graph
            │
            ▼
 Establish EXTERNAL_CLIENT Relationship
            │
            ▼
 Determine PlatformAccount
            │
            ▼
 Approve Admission
            │
            ▼
 TenantOnboardingRequest
            │
            ▼
 Tenant Mapping
            │
            ▼
 ProductSubscription
            │
            ▼
 Capability Grants
```

---

# 109. Nabhold First-Party Onboarding Flow

```text
Shared First-Party Registry
            │
            ▼
Reconcile Canonical Organisation
            │
            ▼
Reconcile LegalEntityProfile
            │
            ▼
Verify CorporateRelationship
            │
            ▼
PLATFORM_GROUP_AFFILIATE
            │
            ▼
Internal Eligibility Review
            │
            ▼
TenantOnboardingRequest
            │
            ▼
Tenant
            │
            ▼
INTERNAL ProductSubscription
            │
            ▼
Capability Grants
```

---

# 110. Existing Tenant Migration

Existing Nabhold, ZuriBeans and Thamani tenants SHALL NOT be destroyed.

Migration SHALL preserve:

```text
tenant_id
provider-native mappings
canonical entity references
subscriptions
grants
engine state
audit history
```

New relationship records SHALL be backfilled around them.

---

# 111. Legacy `Tenant.LegalEntityID`

The singular field SHALL remain operational during migration.

The migration sequence SHOULD be:

```text
Existing Tenant.LegalEntityID
        │
        ▼
Create TenantLegalEntityMapping(DEFAULT)
        │
        ▼
Verify relationship
        │
        ▼
Make mapping repository authoritative
        │
        ▼
Retain singular field as compatibility projection
        │
        ▼
Deprecate only after all consumers migrate
```

---

# 112. Buyer/Supplier Organisation Migration

Existing:

```text
BUYER_ORGANISATION
SUPPLIER_ORGANISATION
```

records SHALL be handled conservatively.

Phase 1:

```text
attach OrganisationProfile
preserve existing CanonicalEntity ID
```

Phase 2:

```text
materialise explicit CounterpartyRole/Profile
```

Phase 3:

```text
reconcile duplicates where evidence proves same real organisation
```

No destructive bulk rewriting SHALL occur.

---

# 113. Shared Contract Changes Required

`baobab-platform/shared` SHOULD eventually publish versioned contracts for:

```text
OrganisationProfile
LegalEntityProfile
CorporateRelationship
CorporateRelationshipType
CorporateGroup
CorporateGroupMembership
PlatformRelationship
PlatformRelationshipType
PlatformAccount
PlatformAccountMembership
TenantOrganisationMapping
TenantLegalEntityMapping
```

These SHALL use canonical identifiers and effective dating.

---

# 114. Existing Shared Tenancy Contract Changes

The tenancy contract SHALL be amended so that:

```text
requires_default_legal_entity_registry_entry
```

no longer means:

```text
every external customer must appear in
shared/contracts/legal-entity/registry.yaml.
```

Instead it SHALL mean:

```text
tenant must resolve to an active canonical
LegalEntity in the Control Plane.
```

The Shared Nabhold registry remains an upstream authority for first-party records.

---

# 115. Control Plane Domain Changes

The target Control Plane model SHOULD include persistence for:

```text
organisation_profile
legal_entity
corporate_relationship
corporate_group
corporate_group_membership_projection
platform_relationship
platform_account
platform_account_membership
tenant_organisation_mapping
tenant_legal_entity_mapping
```

Exact table and package names remain implementation decisions.

---

# 116. Illustrative Relational Model

The following is non-normative:

```text
canonical_entity
    │
    └── organisation_profile
             │
             └── legal_entity
             │
             ├── corporate_relationship ──► organisation_profile
             │
             ├── platform_relationship
             │
             ├── platform_account_membership
             │          │
             │          ▼
             │     platform_account
             │
             └── tenant_organisation_mapping
                         │
                         ▼
                       tenant
                         ▲
                         │
               tenant_legal_entity_mapping
```

---

# 117. Corporate Relationship Persistence Invariants

At minimum persistence SHALL enforce:

```text
source_organisation_id exists
target_organisation_id exists
source != target where relationship semantics require
relationship_type valid
effective_to >= effective_from
status valid
evidence required for consequential relationship types
no unauthorized mutation
historical versions preserved
```

Cycles SHALL NOT automatically be rejected because real corporate cross-holdings may exist.

However, cycles SHALL be visible to graph validation and SHALL prevent unsafe assumptions such as a unique ultimate parent.

---

# 118. PlatformRelationship Persistence Invariants

At minimum:

```text
organisation exists
platform exists
relationship type valid
effective dates valid
status valid
evidence/provenance retained
self-assigned first-party status prohibited
```

A `PLATFORM_GROUP_AFFILIATE` SHOULD preserve its corporate relationship or group-membership basis.

---

# 119. PlatformAccount Persistence Invariants

At minimum:

```text
account_id opaque
account status valid
memberships effective-dated
organisation references canonical
duplicate active memberships controlled
tenant binding explicit
```

No account row SHALL be interpreted as permission.

---

# 120. Tenant Registration Changes

Tenant creation SHALL validate more than identifier grammar.

Before a tenant becomes active, CP SHALL verify:

```text
Organisation exists
Organisation active
Legal entity exists where required
Legal entity active
Tenant mapping valid
Admission authority valid
PlatformRelationship valid
Subscription classification allowed
Isolation policy valid
Residency policy valid
```

The current format-only `legal_entity_id` validation is insufficient as the complete admission check.

---

# 121. Tenant Creation Remains Platform-Privileged

Public applicants SHALL NOT directly invoke arbitrary tenant creation.

The canonical path SHALL remain:

```text
approved admission
      │
      ▼
TenantOnboardingRequest
      │
      ▼
authorised provisioning
      │
      ▼
Tenant
```

Direct administrative APIs SHALL remain privileged and audited.

---

# 122. API Boundary

Conceptual Control Plane APIs MAY include:

```text
POST   /v1/organisations
GET    /v1/organisations/{organisationId}
PATCH  /v1/organisations/{organisationId}

POST   /v1/organisations/{organisationId}/legal-entity
GET    /v1/organisations/{organisationId}/legal-entity

POST   /v1/corporate-relationships
GET    /v1/corporate-relationships/{relationshipId}
GET    /v1/organisations/{organisationId}/corporate-relationships

POST   /v1/corporate-groups
GET    /v1/corporate-groups/{groupId}

POST   /v1/platform-relationships
GET    /v1/organisations/{organisationId}/platform-relationships

POST   /v1/platform-accounts
GET    /v1/platform-accounts/{accountId}
POST   /v1/platform-accounts/{accountId}/members

POST   /v1/tenants/{tenantId}/organisation-mappings
POST   /v1/tenants/{tenantId}/legal-entity-mappings
```

Exact contracts SHALL be defined in Shared before implementation.

---

# 123. API Security

Organisation graph APIs SHALL not be ordinary tenant-public CRUD endpoints.

Read/write authority SHALL depend on:

```text
platform administration
admission workflow
governance role
audited delegated authority
```

Tenant administrators SHALL receive only the relationship data required for their authorised context.

---

# 124. Events

The model SHOULD emit lifecycle events for:

```text
Organisation created
Organisation verified
Organisation suspended
Legal entity verified
Corporate relationship activated
Corporate relationship ended
Corporate relationship conflicted
Platform relationship activated
Platform relationship ended
Platform account created
Platform account membership changed
Tenant organisation mapping activated
Tenant legal-entity mapping activated
```

Actual event IDs SHALL be registered in Shared and SHALL conform to ADR-SHARED-008's canonical event naming convention.

No local repository SHALL invent a competing event namespace.

---

# 125. Event Payload Principle

Events SHALL carry identifiers and meaningful state changes.

They SHOULD NOT indiscriminately publish:

```text
full company documents
sensitive registration evidence
personal contacts
shareholder personal details
unnecessary metadata
```

Consumers SHOULD dereference authorized details when needed.

---

# 126. Idempotency

Organisation admission and relationship creation SHALL be idempotent.

Repeated processing of the same:

```text
AdmissionDecision
CorporateRelationship evidence
TenantOnboardingRequest
```

SHALL NOT create duplicate canonical organisations or duplicate active relationships.

---

# 127. Reconciliation

CP SHALL periodically reconcile:

```text
first-party Shared governance records
canonical Organisation records
legal-entity records
corporate relationships
platform relationships
platform accounts
tenant mappings
subscriptions affected by eligibility
IAM external references
engine projections
```

Drift SHALL be observable.

---

# 128. Relationship Drift

Examples:

```text
Shared says ZuriBeans is active subsidiary
CP has relationship ENDED
```

or:

```text
Corporate relationship ended
PLATFORM_GROUP_AFFILIATE remains active
```

or:

```text
PlatformAccount membership removed
billing reference still points to account
```

SHALL produce actionable drift.

---

# 129. Relationship Changes Shall Trigger Review, Not Blind Cascades

The following is unsafe:

```text
ownership changed
therefore instantly delete tenant
```

Instead:

```text
relationship change
      │
      ▼
policy impact assessment
      │
      ▼
planned changes
      │
      ▼
approval where required
      │
      ▼
reconciliation
```

This avoids destructive side effects from bad upstream data.

---

# 130. Observability

Recommended metrics include:

```text
organisation_total
organisation_verification_total
organisation_duplicate_candidate_total

corporate_relationship_total
corporate_relationship_conflict_total
corporate_relationship_expiry_total

platform_relationship_total
platform_relationship_reclassification_total

platform_account_total
platform_account_membership_total

tenant_organisation_mapping_total
tenant_legal_entity_mapping_total

internal_eligibility_review_total
relationship_drift_total
relationship_resolution_failure_total
cross_tenant_group_access_denied_total
```

Sensitive names and registration identifiers SHALL NOT become uncontrolled metric labels.

---

# 131. Audit

Audit SHALL answer:

```text
Who created this Organisation?

Who verified its legal identity?

Which evidence supported this parent/subsidiary relationship?

Who classified this organisation as PLATFORM_GROUP_AFFILIATE?

Why did this tenant receive INTERNAL subscription classification?

Who changed this PlatformAccount membership?

Which relationship change caused subscription reclassification?

Who approved cross-tenant delegated administration?
```

---

# 132. Privacy and Sensitive Corporate Data

Corporate master data MAY include confidential information.

Access SHALL observe classification.

Evidence containing personal information SHALL be stored and exposed according to applicable data-protection requirements.

The canonical relationship graph SHALL store sufficient evidence lineage without turning CP into an uncontrolled document archive.

---

# 133. Commercial Confidentiality

An external customer SHALL not infer that another organisation is also a Baobab client merely because both exist in the global canonical identity layer.

Therefore:

```text
canonical existence
```

and:

```text
customer relationship visibility
```

are independent.

---

# 134. Platform Account Billing

PlatformAccount MAY eventually support:

```text
consolidated billing
enterprise discounts
group invoice recipient
cost allocation
master contract
support entitlement
```

However:

```text
billing aggregation
```

SHALL NOT weaken:

```text
tenant isolation
capability entitlement
domain authorization.
```

---

# 135. Cost Attribution

Even where one external enterprise receives a consolidated invoice, Baobab SHOULD retain usage attribution by:

```text
tenant
product
capability
market
provider
```

where applicable.

This mirrors ADR-BCP-017's internal metering requirement.

---

# 136. Platform Partner Example

An organisation MAY simultaneously have:

```text
PlatformRelationship:
PLATFORM_PARTNER
```

and:

```text
PlatformRelationship:
EXTERNAL_CLIENT
```

if it both partners with Baobab and consumes Baobab products.

Relationships SHOULD be independently represented rather than forced into one overloaded enum value.

---

# 137. Multiple Platform Relationships

PlatformRelationship SHALL therefore support multiple concurrent valid relationship records where semantically legitimate.

Policy SHALL define incompatible combinations.

For example:

```text
PLATFORM_OWNER
+
PLATFORM_OPERATOR
```

may be legitimate.

Likewise:

```text
PLATFORM_PARTNER
+
EXTERNAL_CLIENT
```

may be legitimate.

---

# 138. No Relationship-by-Email

The following SHALL be prohibited:

```text
same email domain
= same organisation
```

```text
same email domain
= same corporate group
```

```text
same email domain
= internal Nabhold entity
```

Email domain MAY be evidence or IAM federation configuration.

It SHALL not establish corporate truth.

---

# 139. No Relationship-by-Website

Likewise:

```text
same website
same brand
similar company name
same trading name
same postal address
```

SHALL NOT independently establish corporate ownership.

---

# 140. No Relationship-by-Tenant

The following is prohibited:

```text
same tenant
=
same corporate group
```

A tenant is a platform boundary.

It is not evidence of legal ownership.

---

# 141. No Relationship-by-PlatformAccount

Likewise:

```text
same PlatformAccount
=
same legal group
```

is prohibited.

Commercial account membership may reflect a contract rather than ownership.

---

# 142. No Authorization-by-CorporateRelationship

The following code pattern is prohibited:

```go
if SameCorporateGroup(callerOrg, targetOrg) {
    allow()
}
```

A corporate relationship MAY be one input to authorization policy.

It SHALL never be sufficient by itself.

---

# 143. Explicit Cross-Tenant Grants

Where group access is required, a future or existing authorization model SHALL express at least:

```text
granting authority
principal / subject
source tenant
target tenant(s)
capability
scope
effective period
approval
revocation
audit provenance
```

This ADR does not redefine that authorization system.

It requires one.

---

# 144. Group Executive Analytics

Group analytics SHOULD preferably use:

```text
governed aggregation
data products
approved reporting projections
```

rather than broad direct tenant-database access.

This reduces coupling and preserves tenant boundaries.

---

# 145. First-Party and Third-Party Symmetry

Below tenancy, first-party and third-party consumers SHALL use the same architecture.

```text
Organisation
      │
      ▼
PlatformRelationship
      │
      ▼
Tenant
      │
      ▼
ProductSubscription
      │
      ▼
CapabilityGrant
      │
      ▼
CapabilityBinding
```

The value of relationships and subscriptions differs.

The machinery does not.

---

# 146. First-Party Example

```text
ZuriBeans
     │
     ├── CorporateRelationship:
     │     Nabhold OWNS ZuriBeans
     │
     ├── PlatformRelationship:
     │     PLATFORM_GROUP_AFFILIATE
     │
     ├── Tenant:
     │     tn_x...
     │
     └── ProductSubscription:
           INTERNAL
```

---

# 147. Third-Party Example

```text
Acme Foods
     │
     ├── CorporateRelationship:
     │     Acme Holdings OWNS Acme Foods
     │
     ├── PlatformRelationship:
     │     EXTERNAL_CLIENT
     │
     ├── Tenant:
     │     tn_y...
     │
     └── ProductSubscription:
           COMMERCIAL
```

The resolver below Tenant SHALL not require:

```text
if Nabhold then ...
else external ...
```

branches.

---

# 148. External Customer Subsidiaries

Where an external customer's subsidiary independently becomes a client:

```text
Corporate relationship:
Acme Holdings OWNS Acme Foods

Platform relationship:
Acme Holdings  → EXTERNAL_CLIENT
Acme Foods     → EXTERNAL_CLIENT
```

Baobab SHALL preserve both facts.

The subsidiary SHALL not be represented merely as a child row inside the parent's tenant.

---

# 149. Subsidiary Not Yet a Client

Where the subsidiary is relevant corporately but not a customer:

```text
Acme Holdings  → EXTERNAL_CLIENT
Acme Foods     → no PlatformRelationship
```

may be valid.

CorporateRelationship still exists.

No Tenant SHALL be created merely because the subsidiary exists.

---

# 150. Subsidiary Becomes a Client Later

The transition SHALL be:

```text
Existing Canonical Organisation
        │
        ▼
Admission
        │
        ▼
EXTERNAL_CLIENT
        │
        ▼
PlatformAccount membership
        │
        ▼
Tenant
        │
        ▼
Subscription
```

No duplicate organisation should be created.

---

# 151. Platform Account Without Tenant

A contracting parent may hold:

```text
PlatformAccount membership
```

without operating its own tenant.

This is valid.

Example:

```text
Acme Holdings
    CONTRACTING_PARTY

Acme Foods
    SERVICE_RECIPIENT
    + Tenant

Acme Logistics
    SERVICE_RECIPIENT
    + Tenant
```

---

# 152. Tenant Without Enterprise PlatformAccount

A simple client MAY have:

```text
one Organisation
one Tenant
one ProductSubscription
```

without requiring enterprise-group complexity.

The model SHALL scale down as well as up.

---

# 153. Progressive Complexity

Baobab SHALL avoid forcing enterprise complexity into simple cases.

Simple case:

```text
Organisation
    ↓
LegalEntity
    ↓
EXTERNAL_CLIENT
    ↓
Tenant
    ↓
COMMERCIAL Subscription
```

Complex case:

```text
CorporateGroup
    ↓
multiple Organisations
    ↓
multiple LegalEntities
    ↓
multiple PlatformRelationships
    ↓
one or more PlatformAccounts
    ↓
multiple Tenants
    ↓
multiple Subscriptions
```

Both use the same architecture.

---

# 154. Corporate Relationship Purpose

A relationship SHOULD preserve why Baobab needs it.

Potential purposes include:

```text
INTERNAL_ELIGIBILITY
RELATED_PARTY_CLASSIFICATION
PLATFORM_ACCOUNT
CONTRACT
BILLING
COMPLIANCE
GROUP_REPORTING
DELEGATED_ADMINISTRATION
INTERCOMPANY
REFERENCE
```

Purpose metadata SHALL not itself authorize actions.

---

# 155. Stale Relationship Handling

Corporate relationships MAY become stale.

High-consequence relationships SHOULD support:

```text
verification expiry
review date
source refresh
manual re-attestation
```

A stale relationship SHOULD NOT silently continue granting INTERNAL eligibility forever.

---

# 156. Reverification

Reverification MAY be triggered by:

```text
ownership change
registry update
new admission
contract renewal
internal audit
compliance event
manual governance review
```

The existing Organisation identity SHALL remain stable during reverification.

---

# 157. Platform Owner Change

The model SHALL tolerate:

```text
Baobab Platform
owner changes
```

without re-keying every customer.

Only PlatformRelationships and applicable policies should change.

This is a major reason Platform ownership SHALL not be encoded inside tenant identity.

---

# 158. Platform Operator Change

Likewise a new operator SHALL not require:

```text
tenant recreation
legal-entity recreation
subscription recreation
```

unless commercial or infrastructure policy independently requires it.

---

# 159. Multi-Region Behaviour

Organisation identity is platform-global unless legal/residency policy requires controlled replication.

Tenant, provider and data placement remain region-sensitive.

CorporateRelationship SHALL NOT imply deployment region.

PlatformAccount SHALL NOT imply residency region.

---

# 160. Markets Remain Independent

The following remain independent:

```text
CorporateGroup
Organisation
LegalEntity
Tenant
Market
Jurisdiction
DeploymentRegion
PlatformAccount
```

An organisation operating in:

```text
Uganda
South Africa
Kenya
```

SHALL NOT cause three Organisations or three tenants unless actual legal/isolation policy requires them.

---

# 161. Context Example

```text
Organisation:
Acme Foods Ltd

Legal Entity:
LE-...

Corporate Group:
Acme Global Group

Platform Relationship:
EXTERNAL_CLIENT

Platform Account:
ACME-GLOBAL-MSA

Tenant:
tn_...

Market:
ZA

Deployment Region:
af-south-1
```

Every value answers a different question.

---

# 162. Required Acceptance Scenario — Nabhold

Tests SHALL prove:

```text
Nabhold
→ PLATFORM_OWNER

ZuriBeans
→ PLATFORM_GROUP_AFFILIATE

Nabhold OWNS ZuriBeans

ZuriBeans Tenant
→ INTERNAL ProductSubscription
```

while:

```text
Nabhold ownership
```

does NOT automatically grant Nabhold users direct access to ZuriBeans tenant data.

---

# 163. Required Acceptance Scenario — External Standalone Customer

Tests SHALL prove:

```text
Acme Ltd
→ EXTERNAL_CLIENT
→ PlatformAccount
→ Tenant
→ COMMERCIAL Subscription
```

without any Shared source-code registry modification.

---

# 164. Required Acceptance Scenario — External Corporate Group

Tests SHALL prove:

```text
Acme Holdings
├── owns Acme Foods
├── owns Acme Logistics
└── owns Acme Retail
```

and all four MAY independently carry:

```text
EXTERNAL_CLIENT
```

while remaining isolated tenants.

---

# 165. Required Acceptance Scenario — Parent Without Tenant

Tests SHALL prove:

```text
Acme Holdings
→ canonical Organisation
→ contracting PlatformAccount member
```

can exist without:

```text
Tenant
ProductSubscription
```

while subsidiaries have tenants.

---

# 166. Required Acceptance Scenario — Shared Enterprise Account

Tests SHALL prove:

```text
Tenant A
Tenant B
Tenant C
```

can belong commercially to:

```text
PlatformAccount X
```

without either tenant gaining access to another.

---

# 167. Required Acceptance Scenario — Divestiture

Tests SHALL prove:

```text
CorporateRelationship ends
```

can cause:

```text
PLATFORM_GROUP_AFFILIATE review
INTERNAL eligibility review
COMMERCIAL reclassification
```

without changing:

```text
tenant_id
canonical organisation_id
provider-native IDs
historical transactions
```

---

# 168. Required Acceptance Scenario — Joint Venture

Tests SHALL prove a jointly controlled entity may have:

```text
multiple ownership/control relationships
```

without the graph requiring exactly one parent.

---

# 169. Required Acceptance Scenario — Same Organisation Multiple Commercial Roles

Tests SHALL prove one canonical organisation may be:

```text
supplier to ZuriBeans
customer of another tenant
platform external client itself
```

without duplicate global identity.

Tenant-specific relationship data SHALL remain isolated.

---

# 170. Required Security Test Matrix

| Scenario | Expected |
|---|---|
| External applicant claims `PLATFORM_GROUP_AFFILIATE` | deny |
| External applicant claims `INTERNAL` | deny |
| Shared Nabhold record reconciles to CP | allow |
| Same company name, different registration IDs | remain separate |
| Same registration evidence, duplicate candidate | review |
| Parent attempts subsidiary tenant API | deny |
| Sister subsidiary attempts sibling tenant API | deny |
| Same PlatformAccount cross-tenant query | deny |
| Explicit delegated cross-tenant grant | evaluate grant |
| Expired corporate relationship used for INTERNAL | deny/review |
| Conflicted ownership evidence | fail closed |
| IAM Organization not mapped to canonical Organisation | deny context resolution |
| IAM Organisation belongs to another tenant | deny |
| PlatformAccount closed | tenant not automatically deleted |
| Corporate owner changes | tenant ID unchanged |
| External subsidiary added to group | no tenant automatically created |
| Existing non-client subsidiary later admitted | reuse canonical identity |
| Corporate graph cycle encountered | no unsafe ultimate-parent inference |
| GitHub org renamed | no legal identity mutation |

---

# 171. Implementation Gates

Implementation SHALL proceed incrementally.

## Gate ORG-00 — Architecture and Contract Freeze

Audit:

```text
ADR-BCP-004
ADR-BCP-005
ADR-BCP-009
ADR-BCP-012
ADR-BCP-014
ADR-BCP-016
ADR-BCP-017
Shared tenancy
Shared legal entity registry
Shared system-of-record
current CP CanonicalEntity
current Tenant model
IAM Organisation projections
```

Exit:

```text
no unresolved ownership ambiguity
```

---

## Gate ORG-01 — Shared Contract Foundation

Publish versioned schemas for:

```text
Organisation
LegalEntity
CorporateRelationship
CorporateGroup
PlatformRelationship
PlatformAccount
tenant mappings
```

Update the system-of-record matrix.

---

## Gate ORG-02 — Generic Organisation Runtime

Implement:

```text
OrganisationProfile
generic Organisation entity type
lifecycle
repository
service
tests
```

Preserve existing buyer/supplier canonical IDs.

---

## Gate ORG-03 — Legal Entity Runtime Registry

Implement:

```text
LegalEntityProfile
verified identifiers
provenance
first-party reconciliation
external admission path
```

Do not require Shared PRs for external customers.

---

## Gate ORG-04 — Corporate Relationship Graph

Implement:

```text
CorporateRelationship
effective dating
verification
graph queries
conflict handling
derived relationship lineage
```

---

## Gate ORG-05 — Corporate Group Projection

Implement:

```text
CorporateGroup
derived membership
group queries
effective-date resolution
```

No authorization semantics.

---

## Gate ORG-06 — PlatformRelationship

Implement:

```text
PLATFORM_OWNER
PLATFORM_OPERATOR
PLATFORM_GROUP_AFFILIATE
EXTERNAL_CLIENT
PLATFORM_PARTNER
MANAGED_ENTITY
```

with governed transitions.

---

## Gate ORG-07 — PlatformAccount

Implement:

```text
PlatformAccount
memberships
roles
contract references
tenant bindings
```

No tenant-security shortcuts.

---

## Gate ORG-08 — Tenant Mapping Generalisation

Implement:

```text
TenantOrganisationMapping
TenantLegalEntityMapping
default legal entity compatibility
```

Preserve current `Tenant.LegalEntityID`.

---

## Gate ORG-09 — Admission Integration

Extend ADR-BCP-017 lifecycle to:

```text
identity resolution
legal verification
corporate relationship assessment
platform relationship
platform account
tenant onboarding
```

---

## Gate ORG-10 — IAM Projection Reconciliation

Implement:

```text
Canonical Organisation
↔ ExternalReference
↔ Keycloak Organization
```

with fail-closed organisation-context resolution.

---

## Gate ORG-11 — Subscription Eligibility Integration

Wire:

```text
CorporateRelationship
+
PlatformRelationship
+
policy
→ INTERNAL eligibility
```

without changing CapabilityGrant semantics.

---

## Gate ORG-12 — First-Party Migration

Backfill:

```text
NABHOLD
ZURIBEANS
THAMANI-GLOBAL
EQUATOR-ESTATE
```

and their existing tenants.

No canonical tenant ID replacement.

---

## Gate ORG-13 — Buyer/Supplier Reconciliation

Generalise existing:

```text
BUYER_ORGANISATION
SUPPLIER_ORGANISATION
```

into Organisation + roles/profiles.

Quarantine uncertain identity matches.

---

## Gate ORG-14 — Security and Isolation Hardening

Prove:

```text
parent != tenant admin
same group != cross-tenant access
same account != cross-tenant access
IAM claim != canonical truth
```

---

## Gate ORG-15 — Events, Audit and Observability

Implement:

```text
relationship events
audit lineage
drift
metrics
reconciliation
```

---

## Gate ORG-16 — Production Readiness

Validate:

```text
migration
rollback strategy
scale
indexes
graph query performance
tenant isolation
DR
backup
audit retention
documentation
runbooks
threat model
```

---

# 172. Migration Principle

All schema migrations SHALL be forward-only.

Existing historical migrations SHALL NOT be rewritten to make the architecture appear as though the new concepts always existed.

Migration history is architectural evidence.

---

# 173. Performance Considerations

Corporate graphs may become large.

The runtime SHOULD support efficient indexed queries for:

```text
direct relationships
current relationships
relationships as-of date
organisation → group
group → members
organisation → platform relationships
platform account → members
tenant → organisation/legal entity
```

Transitive graph closure MAY use a materialized projection or graph-optimized query strategy if needed.

Canonical facts SHALL remain relationally/auditably authoritative.

---

# 174. Caching

Derived corporate-group membership MAY be cached.

Caches SHALL include:

```text
version
as_of
source relationship version
expiry
```

Relationship changes that affect:

```text
internal eligibility
authorization input
related-party classification
```

SHALL invalidate or version relevant derived state.

---

# 175. Availability

Corporate relationship resolution used only for analytics MAY tolerate temporary degraded data according to policy.

Corporate relationship resolution used for:

```text
INTERNAL subscription eligibility
high-risk group administration
intercompany classification
```

SHALL fail closed when required facts cannot be established.

---

# 176. Disaster Recovery

Backup and recovery SHALL preserve:

```text
Organisation IDs
LegalEntity IDs
CorporateRelationships
effective dates
PlatformRelationships
PlatformAccounts
tenant mappings
audit provenance
ExternalReferences
```

Relationship history is not disposable configuration.

---

# 177. Consequences

## Positive Consequences

Baobab gains:

```text
generic enterprise onboarding
external corporate-group support
first-party/third-party architectural symmetry
clean separation of corporate and platform relationships
scalable external legal-entity onboarding
enterprise commercial accounts
multi-subsidiary customers
non-destructive M&A handling
explicit first-party eligibility
better auditability
better intercompany context
safer cross-tenant governance
provider-neutral IAM projections
future consolidated billing readiness
future group executive reporting readiness
```

## Costs

Baobab must implement:

```text
new canonical organisation contracts
runtime legal-entity registry
relationship graph
verification workflows
PlatformRelationship lifecycle
PlatformAccount model
tenant mapping generalisation
migration/backfill
reconciliation
security tests
graph-level audit
```

These costs are intentional.

They prevent significantly more expensive customer-specific tenancy exceptions later.

---

# 178. Rejected Alternative — Tenant Is the Organisation

Rejected.

It prevents:

```text
one organisation → multiple tenants
one tenant → multiple legal entities
organisation existence without tenant
corporate group representation
```

and conflates identity with platform topology.

---

# 179. Rejected Alternative — Shared Registry Contains Every Customer

Rejected.

It would require source-control changes to onboard customers and turn a governance repository into operational master data.

Shared remains the contract and first-party governance authority.

---

# 180. Rejected Alternative — Keycloak Organization Is Canonical Organisation

Rejected.

Keycloak Organization is an IAM provider-native representation.

Baobab must remain capable of changing identity-provider topology without changing corporate identity.

---

# 181. Rejected Alternative — Subscription Type Represents Platform Relationship

Rejected.

An external client may transition:

```text
TRIAL → COMMERCIAL
```

without ceasing to be an external client.

A first-party affiliate may change commercial treatment independently of canonical organisation identity.

---

# 182. Rejected Alternative — Corporate Relationship Grants Access

Rejected.

Ownership and affiliation are not security permissions.

They are context.

---

# 183. Rejected Alternative — PlatformAccount Is the Tenant

Rejected.

Commercial grouping and data isolation are different concerns.

Enterprise customers may deliberately require multiple isolated tenants under one commercial account.

---

# 184. Rejected Alternative — One Tenant for Every Corporate Group

Rejected.

It would collapse legally and operationally independent subsidiaries.

It would also make acquisitions and divestitures dangerous.

---

# 185. Rejected Alternative — One PlatformAccount per Legal Entity

Rejected.

Enterprise agreements commonly span multiple organisations, while some organisations may participate in multiple arrangements.

The cardinalities must remain explicit.

---

# 186. Rejected Alternative — Infer Group by Name or Email Domain

Rejected.

Names, brands, websites and email domains do not establish legal ownership.

---

# 187. Rejected Alternative — Hard-Code Nabhold as Internal

Rejected.

Baobab is intended to be a generic multi-organisation platform.

First-party status SHALL be data and policy, not product code.

---

# 188. Rejected Alternative — Store Only Currently Active Relationships

Rejected.

Historical corporate relationships are required for:

```text
audit
accounting
intercompany treatment
M&A
eligibility history
reporting
```

---

# 189. Rejected Alternative — Model Corporate Structure as a Strict Tree

Rejected.

Real corporate ownership may involve:

```text
joint control
multiple shareholders
cross-holdings
affiliates
non-equity control
```

The correct abstraction is a graph.

---

# 190. Deferred but Not Architecturally Unresolved

The following implementation details may be decided later without changing this ADR:

```text
specific corporate registry providers
specific company-verification vendors
billing-engine implementation
CRM implementation
graph database vs relational projection
exact cross-tenant delegation schema
group-reporting engine
UI visualisation
legal-document storage provider
beneficial-owner compliance workflow
```

The conceptual boundaries are decided here.

---

# 191. Required Documentation Changes

Acceptance of this ADR SHOULD cause explicit reconciliation notes in:

```text
ADR-BCP-004
ADR-BCP-012
ADR-BCP-014
ADR-BCP-016
ADR-BCP-017
Shared tenancy contract
Shared ERP system-of-record contract
IAM organisation projection documentation
```

---

# 192. Amendment to ADR-BCP-012

ADR-BCP-012 SHALL be interpreted as:

> **Its LegalEntityRelationship is an operational/internal-trade relationship model and SHALL NOT be treated as the platform-wide corporate ownership graph. Corporate facts are governed by ADR-BCP-018 and may be projected into ADR-BCP-012 classifications.**

---

# 193. Amendment to ADR-BCP-014

ADR-BCP-014 SHALL be interpreted as:

> **CounterpartyRelationship remains tenant-scoped commercial relationship state. Canonical Organisation lifecycle and platform-wide corporate relationships are governed by ADR-BCP-018.**

The broader CounterpartyProfile model remains valid.

---

# 194. Amendment to ADR-BCP-016

ADR-BCP-016's bounded:

```text
BUYER_ORGANISATION
SUPPLIER_ORGANISATION
```

model remains valid during migration.

ADR-BCP-018 resolves the Organisation authority question ADR-BCP-016 deliberately left open.

Existing IDs SHALL remain reusable in the generalized model.

---

# 195. Amendment to ADR-BCP-017

ADR-BCP-017 SHALL be interpreted as:

> **INTERNAL, COMMERCIAL, TRIAL, PARTNER, MANUAL and MIGRATION remain ProductSubscription classifications. They SHALL NOT represent the durable relationship between an Organisation and Baobab Platform.**

ADR-BCP-018 supplies the authoritative relationship evidence used by ADR-BCP-017's internal-eligibility policy.

---

# 196. Amendment to Shared System-of-Record

The target system-of-record semantics SHALL become:

| Concept | Runtime Canonical Authority | Contract Authority | Upstream Evidence |
|---|---|---|---|
| Tenant | Control Plane | Shared | Admission / CP |
| Organisation | Control Plane | Shared | Verified admission / governance |
| Legal Entity | Control Plane runtime | Shared | Shared first-party governance or verified external source |
| Corporate Relationship | Control Plane | Shared | Verified governance/legal evidence |
| Corporate Group | Control Plane projection | Shared | Corporate relationship graph |
| Platform Relationship | Control Plane | Shared | Admission/governance/policy |
| Platform Account | Control Plane | Shared | Commercial administration |
| IAM Organisation | IAM provider | IAM contracts | CP canonical Organisation mapping |
| ProductSubscription | Control Plane | Shared | Commercial/admin decision |
| CapabilityGrant | Control Plane | Shared | Subscription/manual provenance |

---

# 197. Required Definition of Done

This ADR is successfully implemented only when all of the following are true:

```text
✓ Canonical Organisation authority is no longer unassigned.

✓ Organisation is distinct from LegalEntity.

✓ CorporateRelationship is platform-wide and not tenant-scoped.

✓ Corporate structures can represent external customers and their subsidiaries.

✓ Corporate structures are graphs, not mandatory trees.

✓ CorporateGroup does not collapse legal identities.

✓ PlatformRelationship exists independently from ProductSubscription.

✓ Baobab distinguishes PLATFORM_OWNER, PLATFORM_OPERATOR,
  PLATFORM_GROUP_AFFILIATE and EXTERNAL_CLIENT.

✓ PlatformAccount exists independently from Tenant.

✓ A PlatformAccount can contain multiple organisations and tenants.

✓ Same PlatformAccount does not confer cross-tenant access.

✓ Same CorporateGroup does not confer cross-tenant access.

✓ External clients no longer require source-code registry changes.

✓ First-party Nabhold governance records continue to reconcile from Shared.

✓ Existing NABHOLD/ZURIBEANS/THAMANI/EQUATOR identities are preserved.

✓ Existing Tenant IDs are preserved.

✓ Existing BUYER_ORGANISATION and SUPPLIER_ORGANISATION identities
  can migrate without destructive replacement.

✓ Tenant-to-Organisation mapping is explicit.

✓ Tenant-to-LegalEntity mapping is explicit.

✓ Existing singular Tenant.LegalEntityID has a safe compatibility path.

✓ Keycloak Organization remains a projection, not corporate authority.

✓ IAM organisation claims are verified through canonical mapping.

✓ Corporate affiliation never automatically grants authorization.

✓ Internal subscription eligibility is based on verified relationship evidence.

✓ Divestiture can reclassify commercial treatment without replacing Tenant identity.

✓ Acquisition does not automatically merge organisations.

✓ Historical relationships remain queryable.

✓ Relationship changes are audited.

✓ Corporate graph access is security-controlled.

✓ External enterprise parent/subsidiary scenarios pass isolation tests.

✓ Nabhold uses the same architecture as an external enterprise group.
```

---

# 198. Final Architecture

The target architecture is:

```text
                               BAOBAB PLATFORM
                                     │
                                     │
                        ┌────────────┴────────────┐
                        │                         │
                        ▼                         ▼
                PlatformRelationship        PlatformAccount
                        │                         │
                        │              Commercial / Administrative
                        │                     Grouping
                        │                         │
                        └────────────┬────────────┘
                                     │
                                     ▼
                              Organisation
                                     │
                      ┌──────────────┼──────────────┐
                      │              │              │
                      ▼              ▼              ▼
                 LegalEntity    Corporate      Counterparty
                                Relationship    Relationship
                                     │          (tenant-scoped)
                                     ▼
                               CorporateGroup
                              / graph projection

                                     │
                                     │ explicit mappings
                                     ▼
                                   Tenant
                                     │
                      ┌──────────────┼───────────────┐
                      ▼              ▼               ▼
                 Legal Entity     Digital Estate   Market
                    Context          Context        Context
                                     │
                                     ▼
                            ProductSubscription
                                     │
                                     ▼
                              CapabilityGrant
                                     │
                                     ▼
                            CapabilityBinding
                                     │
                                     ▼
                              Domain Providers
```

The security interpretation is:

```text
Corporate relationship
        │
        └── describes reality

Platform relationship
        │
        └── describes affiliation with Baobab

Platform account
        │
        └── describes commercial administration

Tenant
        │
        └── defines isolation / consumption boundary

Product subscription
        │
        └── describes product activation

Capability grant
        │
        └── establishes platform entitlement

IAM + domain policy
        │
        └── establishes actor/business authorization
```

No layer substitutes for another.

---

# 199. Architectural Maxims

Baobab SHALL operate according to the following rules:

> **Know organisations globally; isolate their tenant-specific operations locally.**

> **Model ownership as fact, never as permission.**

> **Model corporate groups as relationships, not as collapsed legal identities.**

> **Model customer accounts as commercial groupings, not as security boundaries.**

> **Model platform affiliation independently from subscription classification.**

> **Let first-party and third-party organisations use the same platform architecture.**

> **A subsidiary may be related to its parent without sharing its parent's tenant, data, users, subscriptions or permissions.**

> **A customer may bring an entire corporate group to Baobab without Baobab turning that group into one giant tenant.**

> **Shared governs contracts and first-party identity; the Control Plane governs runtime organisations, relationships and tenancy.**

> **Keycloak authenticates organisations and people; it does not decide corporate truth.**

> **The Control Plane may know that two tenants are sisters. It SHALL still behave as though they are strangers until explicit authorization says otherwise.**

---

# 200. Final Decision

Baobab SHALL adopt a generic, platform-wide model in which:

```text
Canonical Organisation
        +
Legal Entity
        +
Corporate Relationship Graph
        +
Corporate Group
        +
Platform Relationship
        +
Platform Account
        +
Explicit Tenant Mappings
```

form the organisational foundation above:

```text
ProductSubscription
CapabilityGrant
CapabilityBinding
```

This foundation SHALL support equally:

```text
Nabhold Group Africa
and its subsidiaries
```

and:

```text
external enterprise customers
and their subsidiaries, affiliates,
joint ventures and corporate groups.
```

Corporate affiliation SHALL never implicitly weaken tenant isolation or runtime authorization.

The Baobab Control Plane SHALL become the platform runtime authority for canonical Organisation identity, legal-entity runtime representation, corporate relationships, corporate-group projections, platform relationships, platform accounts and their mappings to tenants, while Shared remains the versioned contract authority and authoritative governance source for Nabhold first-party legal identities.

This ADR therefore closes the organisation-authority gap deliberately left open by ADR-BCP-016 and supplies the missing organisational relationship layer required by ADR-BCP-017 for scalable first-party and external-client onboarding.