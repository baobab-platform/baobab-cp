# ADR-BCP-017 — Organisation Admission, Subscription Classification and Tenant Onboarding Lifecycle Model

**Status:** Accepted
**Date:** 2026-09-19
**Decision Owners:** NABHOLD / Baobab Platform Architecture
**Repository:** `baobab-platform/baobab-cp`
**Runtime Authority:** Baobab Control Plane
**Contract Authority:** Baobab Shared Contracts
**Identity Authority:** Baobab IAM
**Decision Type:** Foundational organisation admission, subscription-classification and tenant-onboarding architecture

**Depends On:**

* ADR-BCP-001 — Baobab Control Plane — Parent Implementation Contract and Derived Artefacts
* ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
* ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
* ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
* ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model
* ADR-BCP-006 — Capability Provider Lifecycle, Engine Topology, Health, Failover and Migration Model
* ADR-BCP-007 — Control Plane APIs, Capability Resolution Contracts, Caching, Resolution Assertions and Service-to-Service Consumption Model
* ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model
* ADR-BCP-009 — Capability-Centric Security, Isolation, Residency, Revocation and Failure Semantics
* ADR-BCP-010 — Modular Control Plane Architecture, Governance Boundaries and Evolution Model
* ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model
* ADR-BCP-014 — Canonical Counterparty Identity, Roles and Relationships Model
* ADR-BCP-016 — Buyer Organisation Canonical Entity Registration and OrganisationID Context Resolution
* `BCP-TS-ONBOARDING-001` — Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification

**Applies To:** Prospective organisations, Nabhold Group entities, external customers, applicant identities, organisation admission, subscription classification, tenant-onboarding requests, provisioning plans, activation, suspension, reclassification and offboarding.

---

# 1. Executive Decision

Baobab SHALL introduce a formal **Organisation Admission** lifecycle before tenant provisioning.

A person registering to apply for Baobab SHALL NOT automatically become:

```text
Tenant
LegalEntity
Organisation member
Product subscriber
Capability holder
Digital Estate administrator
Engine-native user
```

Registration establishes only sufficient identity to create, maintain and track an application.

The canonical lifecycle SHALL be:

```text
Applicant Registration
        │
        ▼
ClientApplication
        │
        ▼
Organisation / Legal Evidence
        │
        ▼
Requirements Assessment
        │
        ▼
Admission Review
        │
   ┌────┴────┐
   ▼         ▼
REJECTED   APPROVED
             │
             ▼
    Subscription Classification
             │
             ▼
      TenantOnboardingRequest
             │
             ▼
      TenantProvisioningPlan
             │
             ▼
        Provisioning
             │
             ▼
       Reconciliation
             │
             ▼
          Readiness
             │
             ▼
           ACTIVE
```

The governing principle is:

> **Baobab admits an organisation before it provisions a tenant; provisioning creates desired platform state, not commercial eligibility.**

A second governing principle is:

> **Registration, application, admission, subscription, entitlement, tenant provisioning and runtime authorization are distinct concerns and SHALL NOT be collapsed.**

---

# 2. Decision Scope

This ADR determines:

```text
Applicant registration
ClientApplication lifecycle
Application-scoped organisation evidence
Admission review
Approval and rejection
Subscription classification
Nabhold internal subscription policy
External commercial onboarding
Transition into TenantOnboardingRequest
TenantProvisioningPlan initiation
Sandbox/UAT activation
Production activation
Subscription reclassification
Suspension
Offboarding
Audit requirements
Security boundaries
```

This ADR does NOT redefine:

```text
Tenant
LegalEntity
Market
DigitalEstate
Capability
CapabilityGrant
CapabilityBinding
Engine
EngineInstance
CanonicalEntity
Mapping
ExternalReference
IsolationProfile
```

Those remain governed by their existing ADRs.

---

# 3. Architectural Gap Being Resolved

`BCP-TS-ONBOARDING-001` correctly defines the tenant provisioning lifecycle beginning with a declarative desired-state onboarding request:

```text
REQUEST
   │
VALIDATE
   │
PLAN
   │
IMPACT ANALYSIS
   │
APPLY
   │
RECONCILE
   │
VERIFY READINESS
   │
ACTIVATE
```

That specification answers:

> How does an authorised organisation become an operational Baobab tenant?

It does not fully answer:

> How does an organisation become authorised to request that provisioning in the first place?

Nor does it fully establish the governance rule for:

```text
Nabhold Group entities
        versus
external commercial organisations
```

This ADR closes that gap.

It does not replace `BCP-TS-ONBOARDING-001`.

Instead:

```text
ADR-BCP-017
Organisation Admission
        │
        ▼
Approved TenantOnboardingRequest
        │
        ▼
BCP-TS-ONBOARDING-001
Tenant Provisioning
```

---

# 4. Non-Negotiable Conceptual Distinctions

The following SHALL remain true:

```text
User Registration
    != ClientApplication

ClientApplication
    != Tenant

ApplicantOrganisationProfile
    != Canonical Organisation

Tenant
    != LegalEntity

Tenant
    != DigitalEstate

Subscription
    != CapabilityGrant

CapabilityGrant
    != CapabilityBinding

CapabilityBinding
    != EngineInstance

Product
    != Engine

Market
    != DeploymentRegion

Canonical identity
    != Native engine identity
```

Approval of an application SHALL NOT by itself authorize runtime capabilities.

Similarly:

```text
subscription_type = INTERNAL
```

SHALL NOT mean:

```text
bypass authorization
bypass capability grants
bypass isolation
bypass metering
bypass audit
bypass readiness
```

---

# 5. Registration Versus Application

Baobab SHALL distinguish applicant registration from organisation application.

## 5.1 Registration

Registration establishes an authenticated applicant principal.

Conceptually:

```text
Person
  │
  ▼
Baobab IAM
  │
  ▼
Applicant Principal
```

The applicant principal MAY:

```text
create application
edit draft application
submit application
upload requested evidence
respond to information requests
review application status
withdraw application
```

The applicant principal SHALL NOT automatically receive:

```text
tenant membership
tenant roles
organisation administration
capability access
Digital Estate access
engine credentials
provider-native accounts
```

---

# 6. ClientApplication

The Control Plane SHALL introduce or formalise an application aggregate conceptually named:

```text
ClientApplication
```

It represents a request by an organisation to use Baobab.

Conceptually:

```text
ClientApplication
├── id
├── reference
├── status
├── applicant_principal_id
├── application_channel
├── submitted_at
├── reviewed_at
├── decision_at
├── assigned_reviewer
├── requested_markets
├── requested_business_profiles
├── requested_capabilities
├── evidence_references
├── decision_reference
├── created_at
└── updated_at
```

Exact physical fields SHALL be determined during implementation and contract design.

The canonical model SHALL avoid embedding credentials, document binaries or provider-native identifiers directly into this aggregate.

---

# 7. Applicant Organisation Information Is Pre-Canonical

ADR-BCP-016 explicitly leaves the broader platform-wide Organisation ownership question unresolved.

Therefore this ADR SHALL NOT silently establish `ClientApplication` as canonical Organisation authority.

During admission, the application MAY hold an application-scoped representation conceptually called:

```text
ApplicantOrganisationProfile
```

containing submitted evidence such as:

```text
registered name
trading name
registration number
country of incorporation
principal jurisdiction
business address
website
industry
business model
authorised representative
requested markets
declared legal entities
ownership evidence
tax identifiers where appropriate
```

This information is:

```text
application evidence
```

not automatically:

```text
canonical Organisation state
```

Approved information SHALL be reconciled against the authoritative canonical registries before tenant provisioning.

---

# 8. Application Lifecycle

The application lifecycle SHALL be explicit.

Recommended states are:

```text
DRAFT
  │
  ▼
SUBMITTED
  │
  ▼
VALIDATING
  │
  ├──────────► INFORMATION_REQUIRED
  │                    │
  │                    └────────► VALIDATING
  │
  ▼
UNDER_REVIEW
  │
  ├──────────► REJECTED
  │
  └──────────► APPROVED
```

Additional terminal states MAY include:

```text
WITHDRAWN
EXPIRED
CANCELLED
```

Invalid state transitions SHALL be rejected.

An application SHALL NOT transition directly from:

```text
DRAFT
```

to:

```text
ACTIVE TENANT
```

---

# 9. Admission Channels

Baobab SHALL support the same admission model through multiple channels.

Conceptually:

```text
                    Organisation Admission
                              │
           ┌──────────────────┼──────────────────┐
           │                  │                  │
           ▼                  ▼                  ▼
     Self-Service         Assisted          Internal Group
     Application          Enterprise          Admission
           │                  │                  │
           └──────────────────┼──────────────────┘
                              ▼
                       ClientApplication
                              │
                              ▼
                       Admission Decision
```

The channel MAY influence workflow.

It SHALL NOT weaken the canonical provisioning, isolation, audit or readiness model.

---

# 10. Internal Nabhold Group Admission

Nabhold Group Africa and its recognised subsidiaries SHALL participate in Baobab using the same architectural subscription and provisioning model as external customers.

They SHALL NOT be hard-coded as exceptions throughout the platform.

Instead, their commercial treatment SHALL be represented using the subscription model already established by ADR-BCP-005.

The applicable subscription type SHALL be:

```text
INTERNAL
```

not a parallel invented type such as:

```text
INTERNAL_GROUP
```

This preserves the ADR-BCP-005 subscription vocabulary:

```text
COMMERCIAL
INTERNAL
TRIAL
PARTNER
MANUAL
MIGRATION
```

---

# 11. Nabhold Internal Subscription Policy

For an eligible Nabhold Group legal entity:

```text
subscription_type   = INTERNAL
monetary_charge     = 0
billing_required    = false
usage_metering      = true
entitlement_control = true
audit                = true
readiness_control    = true
isolation_control    = true
```

This means:

> **Nabhold Group entities receive Baobab platform subscriptions free of charge, while remaining normal governed tenants for platform architecture purposes.**

The following SHALL still exist:

```text
ProductSubscription
CapabilityComposition
CapabilityGrant
CapabilityBinding
ProvisioningState
Readiness
Usage measurement
Audit evidence
Engine mappings
Isolation policy
```

Zero charge SHALL NOT equal zero governance.

---

# 12. Eligible Internal Entities

The internal subscription rule SHALL apply to:

```text
Nabhold Group Africa
and
legal entities authoritatively recognised as its subsidiaries
```

Eligibility SHALL derive from governed legal-entity/group relationship information.

Eligibility SHALL NOT derive from:

```text
email domain
user-selected checkbox
promo code
frontend parameter
JWT claim supplied by applicant
organisation name matching
hard-coded tenant ID
```

An external applicant SHALL never be able to self-assign:

```text
subscription_type = INTERNAL
```

Subscription classification is server-authoritative.

---

# 13. Internal Eligibility Evidence

An internal subscription SHALL preserve evidence explaining why it was granted.

Conceptually:

```text
Internal Eligibility
├── legal_entity_id
├── relationship_reference
├── eligibility_status
├── effective_from
├── effective_to
├── evidence_reference
├── verified_by
└── verified_at
```

This does not require a new canonical ownership authority if an accepted legal-entity relationship source already exists.

The implementation SHALL reference the governing source rather than duplicate ownership facts.

---

# 14. Group Ownership Changes

Internal entitlement SHALL be conditional on continuing eligibility.

If a Nabhold subsidiary ceases to qualify as an internal Group entity:

```text
INTERNAL
   │
   ▼
Eligibility Review
   │
   ├── remains INTERNAL
   │
   ├── transitions to COMMERCIAL
   │
   └── enters OFFBOARDING
```

A commercial reclassification SHALL NOT require:

```text
new Tenant ID
new canonical identities
new engine-native records
destructive migration
```

The tenant remains the same tenant.

Only the applicable commercial/subscription policy changes, subject to new grants or product terms where required.

---

# 15. Usage Metering for Internal Entities

Internal subscriptions SHALL remain metered.

At minimum Baobab SHOULD be capable of attributing:

```text
API utilisation
engine utilisation
compute consumption
storage consumption
event volume
AI/intelligence consumption
integration utilisation
user counts
transaction volumes where appropriate
```

to the applicable tenant.

Internal metering exists for:

```text
capacity planning
cost attribution
platform economics
budgeting
performance analysis
future pricing design
operational governance
```

It is not dependent on issuing an invoice.

---

# 16. External Commercial Classification

An approved external commercial customer SHALL normally receive:

```text
subscription_type = COMMERCIAL
```

Commercial terms remain outside runtime capability semantics.

In accordance with ADR-BCP-005:

```text
Subscription
     │
     ▼
Capability Grants
     │
     ▼
Capability Bindings
```

Therefore:

```text
higher price
lower price
zero price
partner agreement
internal allocation
```

SHALL NOT directly modify capability semantics.

Capabilities remain governed by explicit grants.

---

# 17. Other Subscription Types

The existing ADR-BCP-005 vocabulary SHALL remain valid.

```text
TRIAL
PARTNER
MANUAL
MIGRATION
```

Examples:

```text
TRIAL
→ time-bound evaluation

PARTNER
→ governed partner arrangement

MANUAL
→ explicitly assigned administrative subscription

MIGRATION
→ transitional subscription used during platform migration
```

This ADR SHALL NOT redefine those types unnecessarily.

---

# 18. Business Requirement Discovery

Applicants SHALL describe business requirements primarily in business language.

They SHALL NOT need to understand provider technologies.

The admission experience SHOULD ask questions such as:

```text
Do you operate B2B?
Do you operate B2C?
Do you sell online?
Do you require accounting?
Do you require procurement?
Do you manage inventory?
Do you require supplier management?
Do you trade across borders?
Do you require content management?
Do you require intelligence capabilities?
In which markets do you operate?
```

It SHOULD NOT primarily ask:

```text
Do you want MedusaJS?
Do you want iDempiere?
Do you want Payload CMS?
Do you want Haystack?
```

Provider implementation is a platform concern.

---

# 19. Requirement-to-Product Mapping

Application requirements SHALL ultimately be mapped to Baobab Products, ProductVersions, profiles and capability compositions.

Example:

```text
Applicant Requirement
        │
        ▼
Cross-border B2B wholesaler
        │
        ▼
Product / Profile Selection
        │
        ├── B2B
        ├── Cross-Border
        ├── Commerce
        ├── ERP
        ├── Supplier Management
        └── Intelligence
        │
        ▼
Capability Composition
```

This mapping MAY initially require human review.

It MAY later become recommendation-assisted.

It SHALL remain reviewable and auditable.

---

# 20. Market Discovery

Application review SHALL establish intended business participation by Market.

Examples:

```text
South Africa
Uganda
future supported markets
```

The platform SHALL continue to maintain:

```text
Market != Jurisdiction
Market != DeploymentRegion
```

Applicant declarations SHALL not directly determine deployment topology.

Residency and policy resolution determine eligible provider locations.

---

# 21. Admission Decision

Every application SHALL conclude with an explicit decision.

Conceptually:

```text
AdmissionDecision
├── application_id
├── decision
├── decision_reason
├── decided_by
├── decided_at
├── approved_subscription_type
├── approved_product_requirements
├── approved_market_scope
├── approved_legal_structure_reference
├── approved_isolation_requirements
├── conditions
└── evidence
```

Decision values SHALL include at minimum:

```text
APPROVED
REJECTED
```

`INFORMATION_REQUIRED` belongs to application workflow, not the final decision.

---

# 22. Approval Is Not Activation

The following SHALL be prohibited:

```text
Application APPROVED
        =
Tenant ACTIVE
```

Instead:

```text
Application APPROVED
        │
        ▼
TenantOnboardingRequest
        │
        ▼
TenantProvisioningPlan
        │
        ▼
Provisioning
        │
        ▼
Readiness
        │
        ▼
Activation
```

Approval establishes permission to begin governed onboarding.

It does not establish operational readiness.

---

# 23. TenantOnboardingRequest

An approved application SHALL be transformed into a declarative tenant onboarding request consistent with `BCP-TS-ONBOARDING-001`.

The request SHOULD contain or reference:

```text
tenant desired identity
legal-entity structure
Digital Estates
market participation
ProductSubscriptions
product/profile selections
isolation requirements
residency requirements
IAM requirements
integration requirements
```

The onboarding request SHALL preserve:

```text
application_id
admission_decision_id
approval evidence
correlation_id
```

so the resulting tenant can always be traced back to its admission authority.

---

# 24. Desired-State Boundary

The transformation shall be:

```text
Application Evidence
        │
        ▼
Approved Admission Decision
        │
        ▼
Validated Desired State
        │
        ▼
TenantOnboardingRequest
```

Applicant-supplied information SHALL NOT be copied blindly into authoritative Control Plane state.

Every authoritative field SHALL pass its normal validation.

---

# 25. TenantProvisioningPlan

This ADR adopts the existing name:

```text
TenantProvisioningPlan
```

defined by `BCP-TS-ONBOARDING-001`.

No parallel generic `ProvisioningPlan` aggregate SHALL be created merely for this ADR.

The plan includes, conceptually:

```text
TenantProvisioningPlan
├── tenant_operations[]
├── legal_entity_operations[]
├── estate_operations[]
├── market_operations[]
├── subscription_operations[]
├── capability_grant_operations[]
├── binding_operations[]
├── provider_operations[]
├── mapping_operations[]
├── IAM_requirements[]
├── security_checks[]
├── readiness_requirements[]
├── blockers[]
├── warnings[]
└── plan_hash
```

The existing properties remain mandatory:

```text
deterministic
versioned
auditable
dry-run capable
idempotent when applied
```

---

# 26. Plan Before Apply

The complete combined lifecycle SHALL be:

```text
REGISTER
   │
   ▼
APPLY
   │
   ▼
REVIEW
   │
   ▼
APPROVE
   │
   ▼
CLASSIFY SUBSCRIPTION
   │
   ▼
CREATE ONBOARDING REQUEST
   │
   ▼
VALIDATE
   │
   ▼
PLAN
   │
   ▼
IMPACT ANALYSIS
   │
   ▼
APPLY
   │
   ▼
RECONCILE
   │
   ▼
VERIFY READINESS
   │
   ▼
ACTIVATE
```

No complex provisioning SHALL occur before deterministic planning.

---

# 27. Canonical Provisioning Order

Once admission completes, the existing tenant provisioning programme remains authoritative.

Conceptually:

```text
1. Validate canonical contracts
2. Register/create tenant
3. Register/reconcile legal entities
4. Register Digital Estates
5. Register market participation
6. Apply isolation/residency requirements
7. Create ProductSubscriptions
8. Expand capability compositions
9. Materialise CapabilityGrants
10. Resolve provider requirements
11. Create CapabilityBindings
12. Provision provider configuration
13. Establish IAM mappings/context
14. Reconcile desired vs observed state
15. Evaluate readiness
16. Activate tenant
```

This ADR does not replace that sequence.

It defines the authority that allows step 1 to begin.

---

# 28. IAM Admission Boundary

Baobab IAM SHALL distinguish an applicant identity from an operational tenant identity context.

Conceptually:

```text
Applicant
   │
   ▼
Application Identity
   │
   ▼
Admission Approval
   │
   ▼
Tenant Provisioning
   │
   ▼
Organisation Administrator Membership
```

Approval MAY trigger an invitation for the approved organisation administrator.

That invitation SHALL NOT become effective operational access until required platform state is ready.

---

# 29. Initial Organisation Administrator

The approved organisation SHALL identify an initial administrator.

That administrator SHALL pass the applicable IAM provisioning and security policy.

Potential administrative roles MAY include:

```text
ORGANISATION_ADMIN
SECURITY_ADMIN
BILLING_ADMIN
OPERATIONS_ADMIN
DEVELOPER_ADMIN
AUDITOR
```

Exact role vocabulary remains governed by Baobab IAM.

This ADR SHALL NOT duplicate Keycloak-native roles into Control Plane canonical authorization vocabulary without an accepted IAM contract.

---

# 30. Capability Entitlement

Subscriptions SHALL continue to materialise explicit runtime entitlement.

The chain remains:

```text
Product
   │
   ▼
ProductVersion
   │
   ▼
Subscription
   │
   ▼
Capability Composition
   │
   ▼
CapabilityGrant
   │
   ▼
CapabilityBinding
   │
   ▼
Provider / EngineInstance
```

Neither:

```text
APPROVED application
```

nor:

```text
INTERNAL subscription
```

is sufficient runtime authorization.

---

# 31. Provider Neutrality

No application workflow SHALL contain business logic equivalent to:

```text
if applicant wants accounting:
    create iDempiere AD_Client
```

Instead:

```text
Approved Product / Capabilities
        │
        ▼
Control Plane Desired State
        │
        ▼
Capability Resolution
        │
        ▼
Provider Requirements
        │
        ▼
Provider Adapter
```

This ensures Baobab can change providers without redesigning organisation admission.

---

# 32. Provider Provisioning

Native provisioning SHALL remain owned by the applicable provider/engine integration.

Examples may include:

```text
Baobab IAM
→ identity configuration

Baobab Trade
→ commerce provider bootstrap

Baobab ERP
→ ERP provider bootstrap

Baobab CMS
→ content provider bootstrap

Baobab Pulse
→ intelligence provider bootstrap
```

The Control Plane SHALL maintain desired state and provider mappings.

It SHALL NOT promote provider-native identifiers into canonical identity.

---

# 33. Isolation

Every approved onboarding SHALL resolve an `IsolationProfile` through existing Control Plane policy.

Possible policies include:

```text
shared runtime
dedicated native tenant
dedicated engine instance
dedicated regional deployment
regulated isolation
```

Applicant preference MAY influence assessment.

It SHALL NOT override security, residency or platform policy.

---

# 34. Sandbox Before Production

Where applicable, newly provisioned customers SHOULD initially pass through:

```text
PROVISIONING
    │
    ▼
SANDBOX / UAT
    │
    ▼
PRODUCTION_READY
    │
    ▼
ACTIVE
```

A successful application review SHALL not bypass UAT where UAT is required.

---

# 35. Readiness Gate

Activation SHALL remain contingent on readiness.

At minimum applicable readiness evaluation SHALL verify:

```text
Tenant state
IAM readiness
required ProductSubscriptions
required CapabilityGrants
valid CapabilityBindings
eligible EngineInstances
mapping integrity
isolation requirements
residency requirements
provider health
integration readiness
reconciliation state
audit readiness
```

A missing mandatory provider SHALL produce:

```text
NOT_READY
```

not silent degraded activation.

---

# 36. Internal Tenants Use the Same Readiness Gate

Nabhold entities SHALL NOT bypass readiness merely because their subscription is free.

Thus:

```text
ZuriBeans INTERNAL R0
        │
        ▼
same readiness requirements

Thamani INTERNAL R0
        │
        ▼
same readiness requirements

External Customer COMMERCIAL
        │
        ▼
same capability/readiness semantics
```

Commercial classification changes money.

It does not weaken architecture.

---

# 37. Billing and Entitlement Separation

The platform SHALL maintain:

```text
Billing Policy
    !=
Entitlement Policy
```

For Nabhold internal subscriptions:

```text
Billing Policy
→ Zero monetary charge

Entitlement Policy
→ Explicit ProductSubscription
→ Explicit CapabilityGrants
→ Explicit scopes
```

This prevents free subscriptions from becoming implicit superuser grants.

---

# 38. Application Security

The admission subsystem SHALL threat-model at minimum:

```text
fake organisation applications
applicant impersonation
stolen applicant identity
unauthorised application viewing
unauthorised approval
approval forgery
subscription classification tampering
INTERNAL eligibility fraud
tenant ID injection
legal-entity spoofing
market spoofing
provider ID injection
capability escalation
replayed approval
replayed provisioning request
duplicate provisioning
cross-tenant access
document leakage
credential leakage
```

Sensitive actions SHALL fail closed.

---

# 39. Separation of Duties

Production admission SHOULD support separation between:

```text
Applicant
Reviewer
Approver
Provisioning Operator
Security Administrator
```

At minimum:

> An external applicant SHALL NOT approve their own application.

And:

> An applicant SHALL NOT assign their own subscription classification.

High-risk administrative override SHALL be audited.

---

# 40. Audit

The Control Plane SHALL produce immutable or append-oriented audit evidence for material transitions including:

```text
application.created
application.submitted
application.information_requested
application.approved
application.rejected
subscription.classified
tenant_onboarding.requested
tenant_provisioning.planned
tenant_provisioning.applied
tenant.readiness_changed
tenant.activated
tenant.suspended
tenant.offboarding_started
subscription.reclassified
```

Exact canonical event names SHALL be registered through Shared and follow its canonical event namespace.

This ADR does not create a competing event vocabulary.

---

# 41. Correlation

The admission-to-activation journey SHALL remain traceable through stable correlation metadata.

Conceptually:

```text
application_id
admission_decision_id
tenant_onboarding_request_id
tenant_id
subscription_id
provisioning_state_id
correlation_id
trace_id
```

Operators SHALL be able to answer:

> Which application caused this tenant to exist?

and:

> Which decision granted this subscription classification?

---

# 42. Application Documents

Supporting documents SHALL be stored through the approved document/object-storage mechanism.

The application aggregate SHOULD retain references rather than document binaries.

Documents MAY include:

```text
registration evidence
tax evidence
ownership evidence
authorisation evidence
contracts
security questionnaires
migration inventories
compliance evidence
```

Access SHALL be least privilege.

Retention SHALL follow applicable policy.

---

# 43. Data Minimisation

Application intake SHALL collect information required for:

```text
admission
legal verification
product assessment
security assessment
market assessment
provisioning
commercial administration
```

It SHALL NOT become an uncontrolled repository of arbitrary corporate information.

Sensitive data SHALL be minimised.

---

# 44. Reconciliation

Admission state and operational tenant state SHALL remain reconcilable but separate.

Example:

```text
ClientApplication
      APPROVED
         │
         ▼
TenantOnboardingRequest
         │
         ▼
TenantProvisioningPlan
         │
         ▼
Observed State
```

An `APPROVED` application with failed provisioning is:

```text
Application = APPROVED
Tenant      = NOT ACTIVE
```

The platform SHALL NOT rewrite the admission decision merely because provider provisioning fails.

---

# 45. Failure Semantics

Example:

```text
Application approved        ✓
Tenant registered           ✓
IAM provisioned             ✓
Trade provisioned           ✓
ERP provisioning            ✗
```

The result SHALL be:

```text
Application = APPROVED
Provisioning = FAILED / BLOCKED
Tenant readiness = NOT_READY
Production activation = DENIED
```

It SHALL NOT become a partially active production tenant.

Recovery SHALL use controlled retry, reconciliation or compensation.

---

# 46. Idempotency

Transforming the same approved decision into desired state repeatedly SHALL NOT create duplicate:

```text
Tenants
LegalEntities
ProductSubscriptions
CapabilityGrants
CapabilityBindings
IAM organisations
engine-native tenant representations
```

The transition:

```text
AdmissionDecision
→ TenantOnboardingRequest
→ TenantProvisioningPlan
```

SHALL therefore use stable idempotency and correlation semantics.

---

# 47. Change After Activation

After activation, new requirements SHALL be treated as tenant lifecycle changes rather than fresh organisation applications unless commercial/legal policy requires otherwise.

Examples:

```text
Add Market
Add Product
Add Capability
Add Digital Estate
Add Legal Entity
Change IsolationProfile
Add Integration
Upgrade ProductVersion
```

The lifecycle becomes:

```text
Active Tenant
     │
     ▼
Approved Change
     │
     ▼
Desired-State Delta
     │
     ▼
TenantProvisioningPlan
     │
     ▼
Reconcile
```

---

# 48. Subscription Reclassification

Subscription type changes SHALL be explicit and auditable.

Example:

```text
INTERNAL
    │
    ▼
COMMERCIAL
```

or:

```text
TRIAL
    │
    ▼
COMMERCIAL
```

Reclassification SHALL NOT automatically change runtime capability semantics unless the resulting ProductSubscription or grant policy explicitly requires it.

---

# 49. Suspension

Suspension SHALL preserve historical identity and audit evidence.

Conceptually:

```text
ACTIVE
   │
   ▼
SUSPENDED
```

Suspension MAY cause:

```text
derived grants ineffective
new runtime requests denied
new provider operations blocked
readiness blocked
```

according to existing entitlement and failure semantics.

Records SHALL not simply be deleted.

---

# 50. Offboarding

Offboarding SHALL be a controlled lifecycle.

```text
ACTIVE
  │
  ▼
OFFBOARDING
  │
  ├── revoke access
  ├── disable new operations
  ├── drain required workflows
  ├── revoke credentials
  ├── suspend grants
  ├── retire bindings
  ├── export required data
  ├── preserve statutory records
  ├── reconcile provider state
  └── retain audit evidence
  │
  ▼
TERMINATED
```

Tenant deletion SHALL NOT be the normal offboarding mechanism.

---

# 51. Customer-Facing Application Experience

The Baobab public experience SHOULD expose:

```text
Apply to Baobab
```

for prospective organisations.

It SHOULD expose:

```text
Sign In
```

for existing users.

The primary call to action SHOULD NOT imply that public signup creates an operational tenant instantly.

---

# 52. Applicant Portal

An authenticated applicant MAY be provided a restricted application workspace containing:

```text
Application Overview
Organisation Details
Requirements
Markets
Documents
Messages / Information Requests
Application Status
Decision
```

This workspace remains outside operational tenant administration until activation.

---

# 53. Organisation Console After Activation

After successful onboarding, authorised administrators MAY access the Baobab organisation/control-plane experience covering:

```text
Organisation / Legal Context
Markets
Digital Estates
Users and Access
Products
Capabilities
Integrations
Subscription
Usage
Security
Audit
Operational Health
Support
```

Engine-native administration interfaces remain provider concerns where applicable.

---

# 54. Nabhold Group Presentation

An eligible internal tenant SHOULD clearly display:

```text
Subscription Type: Internal
Platform Charge: R0
Usage Metering: Enabled
```

It SHALL NOT misleadingly show:

```text
No subscription
```

because a ProductSubscription still exists.

---

# 55. Anti-Hard-Coding Rule

The implementation FAILS architectural acceptance if core code contains patterns equivalent to:

```go
if tenant == "zuribeans" {
    price = 0
}

if tenant == "thamani" {
    bypassBilling()
}
```

or:

```go
if strings.HasSuffix(email, "@nabhold...") {
    subscriptionType = "INTERNAL"
}
```

Internal eligibility SHALL derive from governed relationship data and policy.

---

# 56. API Boundary

The Control Plane SHALL expose explicit application/admission operations through its approved API conventions.

Conceptually required operations include:

```text
create application
update draft application
submit application
request additional information
record additional evidence
approve application
reject application
withdraw application
derive onboarding request
retrieve application status
```

Exact HTTP paths SHALL be defined in the Control Plane OpenAPI specification.

This ADR intentionally does not freeze arbitrary URI naming before contract design.

---

# 57. Shared Contract Requirements

Baobab Shared Contracts SHALL own or define the appropriate machine-readable contracts for cross-repository concepts introduced by this ADR.

At minimum contract review SHALL determine whether schemas are required for:

```text
ClientApplication
AdmissionDecision
ApplicationStatus
subscription classification evidence
TenantOnboardingRequest
admission lifecycle events
```

Control Plane physical persistence SHALL remain a Control Plane responsibility.

---

# 58. IAM Contract Requirements

Baobab IAM integration SHALL support:

```text
applicant authentication
applicant-session security
administrator invitation
tenant membership after approval
service/workload identities for provisioning
role-aware administration
revocation
```

The Control Plane SHALL consume IAM contracts.

It SHALL NOT make Keycloak's internal IDs canonical business identities.

---

# 59. Observability

Onboarding observability SHOULD include:

```text
applications created
applications submitted
applications awaiting review
information requests
approval/rejection counts
time to admission decision
time from approval to plan
time from plan to sandbox
time from sandbox to production
provisioning failures
failure by provider
reconciliation drift
manual intervention count
internal vs commercial tenant count
subscription type distribution
```

Sensitive organisation information SHALL not become uncontrolled metric labels.

---

# 60. Required Test Matrix

At minimum tests SHALL prove:

| Scenario                                              | Expected                           |
| ----------------------------------------------------- | ---------------------------------- |
| Unregistered visitor requests tenant                  | deny                               |
| Applicant creates draft                               | allow                              |
| Applicant edits own draft                             | allow                              |
| Applicant edits another application                   | deny                               |
| Applicant self-assigns `INTERNAL`                     | deny                               |
| External reviewer assigns `INTERNAL` without evidence | deny                               |
| Verified Nabhold entity receives `INTERNAL`           | allow                              |
| Internal subscription receives R0 charge              | allow                              |
| Internal subscription bypasses grants                 | deny                               |
| Approved application immediately calls engine         | deny                               |
| Approved application generates onboarding request     | allow                              |
| Replayed onboarding request                           | idempotent                         |
| Failed provider provisioning                          | tenant remains not ready           |
| Cross-tenant access after activation                  | deny                               |
| Internal tenant loses eligibility                     | controlled review/reclassification |
| Subscription reclassification changes Tenant ID       | deny                               |

---

# 61. Required Nabhold Acceptance Scenario

Prove the following scenario:

```text
Nabhold Group Africa
        │
        ▼
authoritative Group eligibility
        │
        ▼
ClientApplication / administrative admission
        │
        ▼
APPROVED
        │
        ▼
ProductSubscription
subscription_type = INTERNAL
charge = R0
        │
        ▼
Capability Grants
        │
        ▼
Bindings
        │
        ▼
Provider Provisioning
        │
        ▼
Readiness
        │
        ▼
ACTIVE
```

The test SHALL prove that Nabhold receives no platform subscription charge while all entitlement, isolation, security and readiness controls remain active.

---

# 62. Required Subsidiary Acceptance Scenario

Prove with at least:

```text
ZuriBeans
Thamani Global
```

that both can independently hold:

```text
their own Tenant context
their own legal-entity context
their own Digital Estates
their own Markets
their own ProductSubscriptions
their own CapabilityGrants
their own mappings
their own isolation boundaries
```

while both subscriptions are:

```text
subscription_type = INTERNAL
monetary_charge = 0
```

No cross-subsidiary access SHALL arise from common Nabhold ownership.

---

# 63. External Customer Acceptance Scenario

Prove an unrelated external organisation can follow:

```text
Public Application
      │
      ▼
Admission Review
      │
      ▼
APPROVED
      │
      ▼
COMMERCIAL ProductSubscription
      │
      ▼
same provisioning algorithm
      │
      ▼
ACTIVE
```

Core provisioning code SHALL not require an industry-specific or customer-specific branch.

---

# 64. Migration of Existing Nabhold Tenants

Existing Nabhold, ZuriBeans and Thamani tenant state SHALL NOT be destroyed merely to adopt this ADR.

Where existing ProductSubscriptions are absent, migration SHALL create or reconcile them.

Where existing subscriptions exist but are not classified consistently, migration SHALL normalize them to:

```text
subscription_type = INTERNAL
```

when authoritative Group eligibility is established.

Canonical Tenant IDs, mappings and provider-native references SHALL be preserved.

---

# 65. Relationship With ADR-BCP-005

ADR-BCP-005 remains authoritative for:

```text
Product
ProductVersion
CapabilityComposition
ProductSubscription
subscription_type
CapabilityGrant
Digital Estate provisioning
ProvisioningState
Readiness
```

This ADR adds:

```text
how an organisation becomes eligible for such a subscription
how INTERNAL eligibility is governed
how an approved application enters the existing provisioning programme
```

No duplicate subscription architecture is introduced.

---

# 66. Relationship With ADR-BCP-016

ADR-BCP-016 remains authoritative for its bounded buyer/supplier organisation context-resolution model.

This ADR SHALL NOT reinterpret:

```text
BUYER_ORGANISATION
SUPPLIER_ORGANISATION
```

as the platform-wide organisation master.

`ApplicantOrganisationProfile` is explicitly pre-canonical.

A future accepted platform-wide Organisation model may replace or enrich portions of admission reconciliation without invalidating the ClientApplication lifecycle.

---

# 67. Relationship With BCP-TS-ONBOARDING-001

`BCP-TS-ONBOARDING-001` remains the normative integrated tenant provisioning programme.

Its statement:

> onboarding SHALL begin with a declarative desired-state request

shall be interpreted as:

> **tenant provisioning begins with a declarative desired-state request after organisation admission has authorised that request.**

Therefore:

```text
Organisation Admission
```

precedes:

```text
Tenant Provisioning
```

without contradicting the technical specification.

---

# 68. Consequences

## Positive Consequences

Baobab gains:

```text
controlled customer admission
clean registration boundary
explicit commercial classification
auditable internal Group entitlement
zero-charge Nabhold subscriptions without architectural exceptions
one provisioning model for internal and external tenants
clear transition from applicant to tenant
better security
better lifecycle management
better cost attribution
future self-service onboarding capability
```

## Costs

The platform must implement:

```text
application persistence
review workflows
admission authorization
evidence handling
internal eligibility verification
subscription classification controls
application audit
application-to-onboarding transformation
```

These are deliberate platform costs.

They prevent significantly more expensive tenant-specific onboarding logic later.

---

# 69. Rejected Alternatives

## 69.1 Public signup directly creates Tenant

Rejected.

It provides insufficient governance for an enterprise platform.

---

## 69.2 Nabhold entities bypass ProductSubscriptions

Rejected.

It creates separate internal and external architectures.

---

## 69.3 New `INTERNAL_GROUP` subscription type

Rejected.

ADR-BCP-005 already defines:

```text
INTERNAL
```

The Group relationship determines eligibility; a new subscription type adds no capability semantics.

---

## 69.4 Hard-code ZuriBeans and Thamani as free

Rejected.

Future subsidiaries would require code changes and ownership changes would become dangerous.

---

## 69.5 Subscription directly grants all APIs

Rejected.

Runtime entitlement remains represented through CapabilityGrants and scopes.

---

## 69.6 Applicant declares tenant context

Rejected.

Tenant, legal-entity, market and organisation context must remain server-authoritative and validated.

---

## 69.7 Build provider-specific onboarding forms

Rejected.

Applicants request business outcomes.

Provider resolution remains a Control Plane concern.

---

# 70. Implementation Gates

Implementation SHOULD proceed through controlled gates:

```text
Gate OA-00
Architecture and contract reconciliation

Gate OA-01
ClientApplication model and lifecycle

Gate OA-02
Applicant IAM boundary

Gate OA-03
Application API and evidence model

Gate OA-04
Review and AdmissionDecision

Gate OA-05
Subscription classification policy

Gate OA-06
Nabhold INTERNAL eligibility policy

Gate OA-07
Application → TenantOnboardingRequest transformation

Gate OA-08
TenantProvisioningPlan integration

Gate OA-09
Audit and canonical events

Gate OA-10
Applicant portal / administrative workflow

Gate OA-11
Security and isolation tests

Gate OA-12
Nabhold/ZuriBeans/Thamani migration and acceptance

Gate OA-13
External commercial customer acceptance

Gate OA-14
Production readiness and documentation
```

Each gate SHALL remain bounded.

Implementation SHALL not refactor unrelated Control Plane domains merely because onboarding touches them.

---

# 71. Definition of Done

This ADR is implemented when Baobab can prove:

```text
✓ A person may register without creating a Tenant
✓ An organisation may submit an application
✓ Application state is governed and auditable
✓ Organisation evidence is not silently made canonical
✓ An authorised reviewer may approve/reject
✓ External applicants cannot self-grant INTERNAL status
✓ Nabhold Group eligibility is server-authoritative
✓ Nabhold Group entities receive INTERNAL subscriptions
✓ INTERNAL subscriptions carry R0 platform charge
✓ Internal usage remains metered
✓ Subscriptions still materialise explicit CapabilityGrants
✓ Approved applications produce TenantOnboardingRequests
✓ TenantProvisioningPlan remains the canonical planner
✓ Provider provisioning remains idempotent
✓ Failed provisioning cannot activate a tenant
✓ Readiness gates apply equally to internal and external tenants
✓ ZuriBeans and Thamani remain tenant-isolated
✓ External commercial customers use the same provisioning algorithm
✓ Subscription type can change without replacing canonical Tenant identity
✓ Suspension/offboarding preserve audit evidence
✓ No provider-specific identity becomes canonical
```

---

# 72. Final Target Architecture

```text
                        BAOBAB PLATFORM

                     Prospective Organisation
                               │
                               ▼
                         Register Identity
                               │
                               ▼
                        ClientApplication
                               │
                   ┌───────────┴───────────┐
                   │                       │
                REJECTED                APPROVED
                                           │
                                           ▼
                                Subscription Classification
                                           │
                        ┌──────────────────┼──────────────────┐
                        │                  │                  │
                        ▼                  ▼                  ▼
                    INTERNAL          COMMERCIAL            OTHER
                 Nabhold Group         External        Trial/Partner/etc.
                    Charge R0          Contracted
                        │                  │                  │
                        └──────────────────┼──────────────────┘
                                           │
                                           ▼
                               TenantOnboardingRequest
                                           │
                                           ▼
                                TenantProvisioningPlan
                                           │
                              ┌────────────┼────────────┐
                              │            │            │
                              ▼            ▼            ▼
                           Context      Products      Security
                              │            │            │
                              ▼            ▼            ▼
                         Tenant/Legal   Subscription   Isolation
                         Entity/Market       │         Residency
                              │             ▼            │
                              │       CapabilityGrants    │
                              │             │             │
                              └─────────────┼─────────────┘
                                            ▼
                                  CapabilityBindings
                                            │
                                            ▼
                                    Provider Resolution
                                            │
                   ┌─────────────┬──────────┼──────────┬─────────────┐
                   ▼             ▼          ▼          ▼             ▼
                  IAM          Trade       ERP        CMS          Pulse
                Keycloak       Medusa    iDempiere   Payload      Haystack
                   │             │          │          │             │
                   └─────────────┴──────────┼──────────┴─────────────┘
                                            ▼
                                      Reconciliation
                                            │
                                            ▼
                                         Readiness
                                            │
                               ┌────────────┴────────────┐
                               ▼                         ▼
                            BLOCKED                    READY
                                                         │
                                                         ▼
                                                       ACTIVE
```

---

# 73. Final Decision Principle

Baobab SHALL operate according to the following rule:

> **Register people, admit organisations, subscribe tenants, grant capabilities, bind providers, verify readiness, and only then activate operations.**

For Nabhold Group:

> **Nabhold Group Africa and every authoritatively recognised subsidiary SHALL receive Baobab ProductSubscriptions of type `INTERNAL` at zero monetary platform charge while remaining subject to the same entitlement, isolation, metering, audit, reconciliation and readiness controls as any other tenant.**

And finally:

> **A customer is not onboarded because an application was approved, a user account exists, or a Tenant row was created. A customer is onboarded only when its authorised desired state has been provisioned, reconciled, proven ready and activated through the Baobab Control Plane.**
