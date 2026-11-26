# Baobab Platform — Organisation Onboarding and Provisioning Plan

## 1. Objective

Create a controlled, auditable and repeatable mechanism for onboarding any organisation onto Baobab, from first application through production activation and eventual suspension/offboarding.

The onboarding process must support:

* Nabhold Group Africa;
* existing and future Nabhold subsidiaries;
* external B2B customers;
* external B2C customers;
* organisations operating in one market;
* organisations operating across several markets;
* customers requiring only selected Baobab capabilities;
* customers requiring dedicated or regulated isolation;
* sandbox/evaluation customers;
* future partners and implementation customers.

The onboarding system must **not assume that every organisation looks like ZuriBeans or Thamani**.

---

# 2. Fundamental architectural rule

Organisation onboarding should work as:

```text
Applicant
   │
   ▼
Client Application
   │
   ▼
Assessment / Approval
   │
   ▼
Desired Platform State
   │
   ▼
BAOBAB CONTROL PLANE
   │
   ├── Tenant
   ├── Legal Entity context
   ├── Markets
   ├── Digital Estates
   ├── Subscription / Entitlements
   ├── IsolationProfile
   ├── Capabilities
   ├── CapabilityBindings
   ├── EngineInstances
   ├── Canonical identities
   ├── Mappings
   └── Audit / lifecycle
   │
   ▼
Provisioning Orchestration
   │
   ├────► Baobab IAM
   ├────► Baobab Trade
   ├────► Baobab ERP
   ├────► Baobab CMS
   └────► Baobab Pulse
           │
           ▼
       Sandbox / UAT
           │
           ▼
       Production
```

The Control Plane does not become ERP, commerce, CMS or IAM.

It decides **what should exist, under which context, with which capabilities, and where those capabilities resolve**.

The existing Baobab architecture already defines a resolution chain from trusted Context through CapabilityBinding and eligible EngineInstance to canonical/native representations.

---

# 3. Concepts that must remain separate

The onboarding implementation must never collapse these:

```text
Applicant
    != User

ClientApplication
    != Tenant

Organisation
    != Tenant

Tenant
    != LegalEntity

Tenant
    != DigitalEstate

Market
    != DeploymentRegion

Capability
    != CapabilityBinding

Engine
    != EngineInstance

CanonicalEntity
    != ExternalReference

Canonical ID
    != Engine-native ID

Subscription
    != CapabilityBinding

Subscription
    != EngineInstance
```

An application is simply a request to become a Baobab customer.

A subscription describes commercial/platform entitlement.

A tenant describes platform isolation/context.

A legal entity represents a juridical organisation.

A CapabilityBinding determines where an entitled capability is actually served.

---

# 4. Proposed onboarding lifecycle

I recommend the following lifecycle:

```text
DRAFT
  │
  ▼
SUBMITTED
  │
  ▼
VALIDATING
  │
  ├────► INFORMATION_REQUIRED
  │              │
  │              └────► VALIDATING
  │
  ▼
UNDER_REVIEW
  │
  ├────► REJECTED
  │
  └────► APPROVED
           │
           ▼
      CONTRACTING
           │
           ▼
    READY_TO_PROVISION
           │
           ▼
      PROVISIONING
           │
      ┌────┴────┐
      ▼         ▼
   FAILED     SANDBOX_READY
                │
                ▼
               UAT
                │
                ▼
       PRODUCTION_APPROVED
                │
                ▼
             ACTIVE
                │
       ┌────────┼─────────┐
       ▼        ▼         ▼
   SUSPENDED  CLOSED   OFFBOARDING
```

Provisioning failures must not silently convert an application into an active tenant.

---

# 5. Onboarding authority model

| Responsibility               | Authority                                 |
| ---------------------------- | ----------------------------------------- |
| Applicant UX                 | Baobab Platform/Corporate Estate          |
| Application state            | `baobab-cp`                               |
| Canonical contracts          | `shared`                                  |
| Tenant/platform context      | `baobab-cp`                               |
| Legal-entity relationship    | Control Plane/governed canonical registry |
| Authentication               | `baobab-iam`                              |
| Authorization context        | IAM + Control Plane                       |
| Subscription classification  | Control Plane                             |
| Capabilities                 | Control Plane                             |
| CapabilityBinding            | Control Plane                             |
| IsolationProfile             | Control Plane                             |
| EngineInstance selection     | Control Plane                             |
| Commerce native provisioning | `baobab-trade`                            |
| ERP native provisioning      | `baobab-erp`                              |
| CMS native provisioning      | `baobab-cms`                              |
| Intelligence provisioning    | `baobab-pulse`                            |
| Infrastructure resources     | `infrastructure`                          |
| Native engine business data  | Relevant engine                           |

The existing platform specification already assigns Control Plane ownership of Context, Capability, CapabilityBinding, EngineInstance, Mapping, ExternalReference and IsolationProfile, while engines retain native state.

---

# 6. Gate 0 — Onboarding architecture and governance

Before accepting customers, define the onboarding domain formally.

## Required decisions

Create or extend an accepted ADR covering:

```text
Client Application
Organisation onboarding
Approval lifecycle
Subscription classification
Internal Nabhold entitlement
Provisioning orchestration
Failure/rollback model
Engine provisioning contracts
Offboarding
```

The ADR should explicitly state:

> An organisation does not become an operational Baobab tenant merely by registering a user account.

Also establish authoritative ownership for each onboarding object.

## Deliverables

* onboarding ADR;
* onboarding OpenAPI contract;
* canonical onboarding event definitions;
* state-transition matrix;
* role/permission matrix;
* provisioning contract;
* audit requirements;
* data-retention rules;
* threat model.

## Exit criterion

No implementation starts until responsibility boundaries are unambiguous.

---

# 7. Gate 1 — Applicant identity and application creation

The public Baobab estate provides:

```text
Apply to Baobab
```

rather than immediately:

```text
Create Tenant
```

An applicant creates an identity through Baobab IAM sufficient to access their application.

This identity is **not yet tenant membership**.

## Application information

Capture:

### Organisation information

```text
registered name
trading name
registration number
organisation type
country of incorporation
principal place of business
website
industry
```

### Authorised contact

```text
name
position
email
telephone
authority to represent organisation
```

### Operational profile

```text
B2B / B2C / mixed
products/services
customer model
supplier model
approximate users
approximate transactions
expected order volumes
expected storage/data volumes
```

### Existing technology

```text
ERP
commerce
CMS
CRM
identity provider
accounting systems
existing APIs
data migration requirements
```

Applications should be saveable as drafts.

## Exit criterion

Application reaches `SUBMITTED`.

---

# 8. Gate 2 — Organisation and legal-entity verification

The platform validates the organisation before canonical tenant creation.

Potential checks include:

```text
legal existence
registration details
registered jurisdiction
authorised representative
ownership structure
subsidiaries
operational markets
tax identifiers where applicable
contracting entity
```

This stage is especially important because one customer organisation may ultimately contain:

```text
1 Tenant
   │
   ├── Legal Entity A
   ├── Legal Entity B
   └── Legal Entity C
```

or another approved topology.

Do not create arbitrary relationships from matching company names.

## Internal Nabhold path

Nabhold-owned entities receive a special verification path.

The Control Plane verifies that the organisation belongs to the governed Nabhold Group structure.

That classification produces:

```text
CustomerClass = INTERNAL_GROUP
```

It must **not** rely on:

```text
@nabh... email address
promo code
user self-declaration
```

Group status must derive from canonical ownership/governance data.

---

# 9. Gate 3 — Business capability discovery

Applicants should describe business requirements rather than selecting implementation technologies.

Do not ask:

```text
Do you want Medusa?
Do you want iDempiere?
Do you want Payload?
```

Ask:

```text
Do you sell products online?
Do you sell B2B or B2C?
Do you require accounting?
Do you manage inventory?
Do you need supplier management?
Do you require procurement?
Do you need content management?
Do you require intelligence/market analysis?
Do you operate across borders?
```

The resulting requirements map to Baobab capabilities.

Example:

```text
Organisation requirement
        │
        ▼
Wholesale cross-border commerce
        │
        ├── commerce.catalogue
        ├── commerce.b2b
        ├── commerce.orders
        ├── inventory.*
        ├── erp.accounting
        ├── erp.procurement
        ├── iam.organisation
        ├── cms.content
        └── intelligence.market
```

The customer buys capabilities.

Baobab decides which engine currently supplies those capabilities.

---

# 10. Gate 4 — Market, jurisdiction and residency discovery

For every proposed operation establish:

```text
Market
Jurisdiction
Currency
Tax context
Data residency
Deployment constraints
Languages
Regulatory requirements
```

Never derive deployment region directly from market.

The current architecture explicitly distinguishes `Market` from `DeploymentRegion`.

Example:

```text
Customer Market
    │
    ▼
South Africa
    │
    ▼
Residency / policy evaluation
    │
    ▼
Approved deployment topology
```

Possible markets initially include:

```text
ZA
UG
```

with the architecture remaining extensible.

---

# 11. Gate 5 — Subscription and commercial classification

Every approved organisation receives a `Subscription`.

Do not bypass subscription records for internal companies.

Recommended classifications:

```text
INTERNAL_GROUP
EXTERNAL_COMMERCIAL
PARTNER
TRIAL
DEVELOPER_SANDBOX
```

## Nabhold Group rule

For Nabhold and eligible subsidiaries:

```text
SubscriptionClass = INTERNAL_GROUP
Billable           = false
RecurringCharge    = 0
UsageMetering      = true
Entitlements       = explicit
Audit              = enabled
LifecycleControls  = enabled
```

Therefore:

```text
ZuriBeans
    subscription = ACTIVE
    price         = R0

Thamani Global
    subscription = ACTIVE
    price         = R0

Equator & Estate Co.
    subscription = ACTIVE
    price         = R0
```

Free does **not** mean unmanaged.

Capabilities remain explicitly enabled.

Usage remains measured.

Infrastructure costs remain attributable internally.

This enables future cost accounting such as:

```text
ZuriBeans
Platform fee:       R0
Actual compute:     tracked
Storage:            tracked
API utilisation:    tracked
Engine utilisation: tracked
```

---

# 12. Gate 6 — Formal approval

Approval converts an assessed application into authorised desired platform state.

Approval should capture:

```text
approved organisation
approved legal entities
approved markets
approved subscription class
approved capabilities
approved isolation profile
approved data residency
approved environments
approved integrations
approved administrator
```

An approval must be immutable/auditable.

Subsequent changes occur through amendments, not silent edits.

## Approval record

Conceptually:

```text
ApprovalDecision
├── application_id
├── decision
├── approver
├── approved_at
├── commercial_class
├── capability_set
├── market_scope
├── isolation_requirement
├── conditions
└── evidence
```

---

# 13. Gate 7 — Build the Provisioning Plan

Do **not** start engine provisioning immediately from an approval handler.

First generate a declarative `ProvisioningPlan`.

Example:

```text
ProvisioningPlan
│
├── Create Tenant
├── Create legal-entity relationships
├── Create markets
├── Create subscription
├── Apply isolation policy
├── Provision IAM context
├── Invite organisation administrator
├── Provision Trade
├── Provision ERP
├── Provision CMS
├── Provision Pulse
├── Establish mappings
├── Establish CapabilityBindings
├── Configure Digital Estate
├── Run reconciliation
└── Activate sandbox
```

The plan becomes inspectable before execution.

That greatly reduces expensive surprises.

---

# 14. Gate 8 — Canonical Control Plane provisioning

The Control Plane creates authoritative platform records first.

Typical order:

```text
Application
    │
    ▼
Tenant
    │
    ├── LegalEntity relationship(s)
    ├── Market configuration
    ├── DigitalEstate(s)
    ├── Subscription
    ├── Entitlements
    └── IsolationProfile
```

Only after canonical state exists may native engine provisioning begin.

The system must not allow engines to invent canonical identities independently.

---

# 15. Gate 9 — IAM provisioning

Baobab IAM receives the approved tenant/organisation context.

Provision:

```text
organisation identity context
initial administrator
roles
group membership
authentication policy
MFA requirements
service identities
client/application identities
```

The first organisation administrator receives an invitation.

Suggested starting roles:

```text
ORGANISATION_ADMIN
SECURITY_ADMIN
BILLING_ADMIN
DEVELOPER_ADMIN
OPERATIONS_ADMIN
AUDITOR
MEMBER
```

Exact Keycloak representation must conform to the accepted `baobab-iam` ADRs rather than being hard-coded into Control Plane domain models.

## Important

Authentication and authorization remain separate from routing.

The platform architecture already requires:

```text
Authentication
    ↓
Authorization
    ↓
Context
    ↓
Resolution
```

Do not treat a mapping or engine identity as authorization.

---

# 16. Gate 10 — Isolation and deployment topology

Before provisioning any engine, resolve the customer's `IsolationProfile`.

Existing architecture permits policy dimensions including runtime, database, tenant/client, network, credential, encryption, backup, region, residency, administration and event boundaries.

Possible profiles might include:

```text
SHARED_STANDARD
SHARED_DEDICATED_NATIVE_TENANT
DEDICATED_INSTANCE
DEDICATED_REGIONAL
REGULATED
```

The generic Control Plane profile must not contain engine-specific semantics.

Example:

```text
IsolationProfile
    │
    ▼
Engine adapter
    │
    ├── iDempiere → appropriate AD_Client topology
    ├── Medusa    → approved tenant isolation mechanism
    ├── Payload   → approved content isolation
    └── Pulse     → approved pipeline/index/data isolation
```

---

# 17. Gate 11 — Engine provisioning

Provision only entitled capabilities.

## Baobab IAM

```text
tenant/organisation context
administrators
roles
clients
workload identities
```

## Baobab Trade

Potential native provisioning:

```text
sales channels
markets/regions as appropriate
price configuration
customer/company structures
inventory context
fulfilment configuration
payment configuration
tax configuration
API credentials
```

## Baobab ERP

Potential native provisioning:

```text
iDempiere client/organisation topology
accounting structures
warehouses
business partners
tax configuration
currencies
financial periods
document sequences
roles
```

Canonical `Tenant` must never simply become `AD_Client_ID`; the architecture specifically prohibits that conflation.

## Baobab CMS

Provision:

```text
tenant/estate content scope
collections/configuration
roles
localisation
market content context
```

## Baobab Pulse

Provision:

```text
tenant intelligence context
permitted datasets
market scopes
retrieval boundaries
pipelines
indexes/stores where applicable
access policy
```

The current platform stack remains Go Control Plane, MedusaJS Trade, iDempiere ERP, Payload CMS, Headless Haystack intelligence and PostgreSQL canonical/control storage.

---

# 18. Gate 12 — External references and mappings

After an engine successfully provisions its native representation:

```text
Canonical Identity
        │
        ▼
Mapping
        │
        ▼
ExternalReference
        │
        ▼
Native Engine Object
```

Example:

```text
Tenant: ZURIBEANS
        │
        ▼
ERP provisioning
        │
        ▼
iDempiere AD_Client_ID = 1000037
        │
        ▼
ExternalReference
        │
        ▼
Mapping retained by Control Plane
```

`1000037` does not become the canonical tenant ID.

Likewise Medusa, Payload and Pulse native identifiers never replace Baobab canonical identities.

---

# 19. Gate 13 — CapabilityBinding activation

Once an engine's provisioned representation is healthy, the Control Plane may bind capabilities.

Example:

```text
Context
Tenant      = CUSTOMER-A
LegalEntity = CUSTOMER-A-ZA
Market      = ZA

Capability
erp.receivables

        │
        ▼

CapabilityBinding
ERP-AF-SOUTH-01
```

CapabilityBindings should initially be:

```text
pending
```

and only move to:

```text
active
```

after engine provisioning, mapping verification and health checks succeed.

The existing platform lifecycle explicitly distinguishes pending, active, suspended, superseded and retired bindings.

---

# 20. Gate 14 — Integration configuration

Configure external dependencies only after canonical platform identity exists.

Examples:

```text
payment providers
shipping providers
banking integrations
email
SMS
tax providers
object storage
external accounting
external identity federation
external APIs
webhooks
```

Credentials belong in the approved secrets infrastructure.

Never persist raw credentials in canonical Control Plane business records.

Every external integration should carry:

```text
tenant scope
legal entity scope
market scope
environment
credential reference
status
effective period
audit information
```

---

# 21. Gate 15 — Data migration and initial data

Customers may have:

```text
products
customers
suppliers
inventory
orders
opening balances
content
documents
users
price lists
```

Migration must be handled as an explicit onboarding workstream.

Recommended sequence:

```text
Discover
   ↓
Extract
   ↓
Validate
   ↓
Transform
   ↓
Canonicalise
   ↓
Load into authoritative engine
   ↓
Create canonical mappings
   ↓
Reconcile
   ↓
Business validation
```

Never import records independently into several engines and later attempt to infer which records correspond.

---

# 22. Gate 16 — Sandbox activation

The first operational environment should normally be sandbox/UAT.

```text
Approved Tenant
     │
     ▼
Sandbox
     │
     ├── IAM login
     ├── capability resolution
     ├── Trade
     ├── ERP
     ├── CMS
     ├── Pulse
     ├── integrations
     └── sample workflows
```

No production routing should occur yet.

The platform specification already states that provisioning engine instances must not receive normal traffic until they become eligible.

---

# 23. Gate 17 — Conformance and isolation testing

Before production activation, test:

## Identity

```text
correct user
wrong tenant
expired user
suspended user
service account
administrator
```

## Isolation

```text
Tenant A → Tenant A        ALLOW
Tenant A → Tenant B        DENY
Tenant B → Tenant B        ALLOW
Unknown tenant             DENY
```

Baobab's existing test specification explicitly requires cross-tenant denial through APIs, resolver services, caches, events, workers and reverse mappings.

## Capability resolution

Verify:

```text
Context
→ Capability
→ CapabilityBinding
→ EngineInstance
```

## Mappings

Verify:

```text
CanonicalEntity
→ Mapping
→ ExternalReference
→ Native Object
```

## Markets

Verify jurisdiction/market boundaries.

## Caches

Ensure tenant-scoped resolution cannot leak between customers.

---

# 24. Gate 18 — Business UAT

The customer's administrators execute approved scenarios.

Examples:

```text
User login
Create/invite employee
Catalogue access
Create customer
Create supplier
Place order
Generate invoice
Inventory transaction
Content publication
Pulse query
Market switching
Document retrieval
Audit review
```

The exact UAT suite derives from purchased capabilities.

An ERP-only customer should not be required to test ecommerce.

---

# 25. Gate 19 — Production-readiness review

Production activation requires signed-off evidence covering:

| Area               | Requirement |
| ------------------ | ----------- |
| Identity           | Passed      |
| Tenant isolation   | Passed      |
| Capability routing | Passed      |
| Engine health      | Passed      |
| Canonical mappings | Reconciled  |
| Backups            | Verified    |
| Restore process    | Verified    |
| Secrets            | Valid       |
| Monitoring         | Enabled     |
| Alerts             | Enabled     |
| Audit              | Enabled     |
| Data residency     | Compliant   |
| Integrations       | Verified    |
| UAT                | Signed off  |
| Support owner      | Assigned    |
| Rollback           | Documented  |

No single operator should be able to click “production” around unresolved P0/P1 issues.

---

# 26. Gate 20 — Controlled production activation

Activation should be transactional at the platform-state level.

Conceptually:

```text
Production Approval
       │
       ▼
Verify Provisioning Graph
       │
       ├── Tenant active?
       ├── IAM healthy?
       ├── Engine instances eligible?
       ├── Mappings valid?
       ├── Bindings valid?
       ├── Isolation valid?
       └── Health checks pass?
                │
                ▼
       Activate Bindings
                │
                ▼
        Production Enabled
```

Activation events should propagate through canonical events.

The existing architecture already expects canonical lifecycle events and transaction/outbox-style publication for binding, engine-instance and mapping changes.

---

# 27. Provisioning must be idempotent

Every provisioning command should use a stable desired-state identity.

Running:

```text
Provision Tenant X → ERP
```

twice must not create:

```text
Tenant X ERP Client #1
Tenant X ERP Client #2
```

Instead:

```text
Desired State
      │
      ▼
Existing state?
   │       │
  yes      no
   │       │
 reconcile create
```

The existing Control Plane specification already requires idempotent provisioning for bindings/mappings and reconciliation of the platform graph.

---

# 28. Provisioning execution model

Introduce a durable orchestration construct:

```text
ProvisioningRun
│
├── Step 01 Canonical Tenant
├── Step 02 Legal Entity
├── Step 03 Subscription
├── Step 04 IAM
├── Step 05 Isolation
├── Step 06 Trade
├── Step 07 ERP
├── Step 08 CMS
├── Step 09 Pulse
├── Step 10 Mappings
├── Step 11 Bindings
├── Step 12 Reconciliation
└── Step 13 Sandbox Activation
```

Each step records:

```text
PENDING
RUNNING
SUCCEEDED
FAILED
SKIPPED
COMPENSATING
COMPENSATED
```

Also capture:

```text
started_at
finished_at
attempt
correlation_id
idempotency_key
error_code
operator
engine response reference
```

---

# 29. Failure strategy

Do not assume distributed provisioning can behave like one database transaction.

Use controlled compensation.

Example:

```text
Tenant created        ✓
IAM provisioned       ✓
Trade provisioned     ✓
ERP provisioning      ✗
CMS                    -
Pulse                  -
```

The correct result is:

```text
ProvisioningRun = FAILED
Tenant           = NOT_ACTIVE
Bindings         = NOT_ACTIVE
```

not:

```text
partly usable customer
```

Operations can then:

```text
Retry ERP
```

or:

```text
Compensate provisioning
```

No engine should receive production traffic while provisioning remains incomplete.

---

# 30. Reconciliation

A scheduled and event-triggered reconciler should compare desired state with actual engine state.

```text
CONTROL PLANE DESIRED STATE
          │
          ▼
      Reconciler
          │
      ┌───┼────┬────┐
      ▼   ▼    ▼    ▼
     IAM Trade ERP  CMS/Pulse
          │
          ▼
ACTUAL ENGINE STATE
```

Detect:

```text
missing tenant
missing engine representation
stale mapping
duplicate mapping
inactive engine
invalid binding
incorrect market
incorrect isolation
orphaned account
credential drift
```

Never repair ambiguous identity automatically.

Quarantine and escalate instead.

---

# 31. Organisation Portal after onboarding

After activation, the customer's administrator uses a Baobab organisation console.

Suggested areas:

```text
Overview

Organisation
├── Profile
├── Legal Entities
├── Markets
└── Digital Estates

People & Access
├── Users
├── Teams
├── Roles
└── Invitations

Capabilities
├── Trade
├── ERP
├── CMS
├── Pulse
└── Future Engines

Integrations
├── APIs
├── Webhooks
├── Credentials
└── External Services

Subscription
├── Plan
├── Entitlements
├── Usage
└── Billing

Security
├── Authentication
├── Sessions
├── Audit
└── Policies

Operations
├── Health
├── Incidents
└── Support
```

For `INTERNAL_GROUP` tenants, Subscription may show:

```text
Baobab Internal Group
Charge: R0
Usage: tracked
```

---

# 32. Changes after onboarding

Onboarding must naturally transition into tenant lifecycle management.

A customer later requesting Uganda operations should not be “re-onboarded.”

Instead:

```text
Active Tenant
    │
    ▼
Change Request
    │
    ├── Add Market
    ├── Add Legal Entity
    ├── Add Capability
    ├── Change Isolation
    ├── Add Digital Estate
    └── Add Integration
```

The same provisioning machinery executes the approved delta.

This is crucial.

The onboarding system should really become a **desired-state tenant lifecycle orchestrator**, with onboarding simply being the initial transition from zero state.

---

# 33. Suspension and offboarding

Eventually an organisation may leave.

Define:

```text
ACTIVE
  │
  ▼
SUSPENDED
  │
  ▼
OFFBOARDING
  │
  ▼
TERMINATED
  │
  ▼
RETAINED / PURGED
```

Offboarding must coordinate:

```text
disable access
revoke credentials
stop new transactions
drain operations
export customer data
retain statutory data
retire bindings
retire mappings where appropriate
release resources
revoke integrations
archive audit records
terminate subscription
```

Never simply delete the tenant.

---

# 34. Internal-group ownership changes

The Nabhold zero-price entitlement should follow ownership state.

Example:

```text
Equator & Estate Co.
        │
        ▼
Nabhold controlled
        │
        ▼
INTERNAL_GROUP / R0
```

If control changes:

```text
Ownership change
       │
       ▼
Entitlement Review
       │
       ├── Remain INTERNAL_GROUP
       │
       └── Convert EXTERNAL_COMMERCIAL
```

That transition should not require a new Tenant.

Only subscription/commercial classification changes.

---

# 35. Observability

Every onboarding operation should carry:

```text
application_id
tenant_id
legal_entity_id
market_id
subscription_id
provisioning_run_id
capability_id
binding_id
engine_id
engine_instance_id
principal_id
correlation_id
trace_id
```

The existing Control Plane design already calls for structured telemetry covering tenant, legal entity, market, estate, capability, binding, engine instance, principal and correlation data.

Create dashboards for:

```text
applications submitted
approval rate
time to approval
time to sandbox
time to production
provisioning failures
failure by engine
manual interventions
reconciliation drift
tenant count
capabilities enabled
active engine bindings
```

---

# 36. Security requirements

Threat-model the onboarding system for:

```text
fake organisation
applicant impersonation
unauthorised approval
tenant spoofing
legal-entity spoofing
entitlement escalation
INTERNAL_GROUP fraud
capability escalation
cross-tenant access
provisioning replay
duplicate provisioning
credential leakage
mapping poisoning
engine-native ID injection
event replay
stale bindings
compromised service identity
```

Particularly:

> An external customer must never be able to set `SubscriptionClass=INTERNAL_GROUP`.

That property must be server-authoritative.

---

# 37. Proposed implementation objects

These are **new onboarding-domain proposals**, not existing canonical Baobab objects and should therefore be reconciled with the accepted Control Plane ADRs before implementation:

```text
ClientApplication
ApplicationContact
ApplicationOrganisation
ApplicationLegalEntity
ApplicationMarket
CapabilityRequirement
IntegrationRequirement
ResidencyRequirement
ApprovalDecision
SubscriptionClassification
ProvisioningPlan
ProvisioningRun
ProvisioningStep
OnboardingDocument
OnboardingComment
GoLiveApproval
OffboardingPlan
```

After approval, these should reference—not duplicate—the existing canonical platform objects:

```text
Tenant
LegalEntity
Market
DigitalEstate
Context
Capability
CapabilityBinding
Engine
EngineInstance
IsolationProfile
CanonicalEntity
Mapping
ExternalReference
```

---

# 38. Proposed API shape

Conceptually:

```text
POST   /applications
GET    /applications/{id}
PATCH  /applications/{id}
POST   /applications/{id}/submit

POST   /applications/{id}/request-information
POST   /applications/{id}/approve
POST   /applications/{id}/reject

POST   /applications/{id}/provisioning-plan
GET    /provisioning-plans/{id}

POST   /provisioning-plans/{id}/execute
GET    /provisioning-runs/{id}
POST   /provisioning-runs/{id}/retry

POST   /tenants/{id}/activate
POST   /tenants/{id}/suspend
POST   /tenants/{id}/offboard
```

Exact routes must follow existing `baobab-cp` API conventions rather than being adopted verbatim.

---

# 39. Events

Potential event families:

```text
client-application.submitted
client-application.information-requested
client-application.approved
client-application.rejected

subscription.created
subscription.activated
subscription.changed
subscription.suspended

tenant.provisioning-requested
tenant.provisioning-started
tenant.provisioning-failed
tenant.provisioned
tenant.activated
tenant.suspended

market.enabled
legal-entity.attached
digital-estate.enabled

engine-provisioning.requested
engine-provisioning.succeeded
engine-provisioning.failed
```

Names and envelope structures must conform to `baobab-platform/shared`.

Do not create a second event convention just for onboarding.

---

# 40. Implementation sequence

I would implement this through the following engineering gates:

```text
Gate 0   Architecture / ADR
Gate 1   Application domain model
Gate 2   Application API
Gate 3   Applicant IAM integration
Gate 4   Review / approval workflow
Gate 5   Subscription classification
Gate 6   INTERNAL_GROUP entitlement
Gate 7   ProvisioningPlan
Gate 8   ProvisioningRun orchestration
Gate 9   IAM provisioning adapter
Gate 10  Trade provisioning adapter
Gate 11  ERP provisioning adapter
Gate 12  CMS provisioning adapter
Gate 13  Pulse provisioning adapter
Gate 14  Mapping / ExternalReference creation
Gate 15  CapabilityBinding activation
Gate 16  Reconciliation
Gate 17  Sandbox / UAT workflow
Gate 18  Production activation
Gate 19  Organisation console
Gate 20  Change management
Gate 21  Suspension / offboarding
Gate 22  Security hardening
Gate 23  Load / failure / chaos testing
Gate 24  Production rollout
```

Each gate should result in a narrow PR.

Do not implement all engine adapters in one giant PR.

---

# 41. Definition of done for a newly onboarded organisation

An organisation is considered successfully onboarded only when all applicable conditions are true:

```text
✓ Application approved
✓ Organisation verified
✓ Subscription active
✓ Tenant canonical identity exists
✓ Legal-entity relationships valid
✓ Markets configured
✓ IsolationProfile assigned
✓ Organisation administrator can authenticate
✓ IAM authorization works
✓ Required engine representations provisioned
✓ ExternalReferences exist
✓ Canonical mappings reconcile
✓ Required CapabilityBindings active
✓ Cross-tenant isolation tests pass
✓ Sandbox UAT passed
✓ Data migration reconciled
✓ Integrations tested
✓ Monitoring active
✓ Audit active
✓ Backups verified
✓ Production approval recorded
✓ Production bindings activated
✓ Customer handover complete
```

Anything less is:

```text
PROVISIONING
```

or:

```text
PARTIALLY_PROVISIONED
```

but never silently `ACTIVE`.

---

# 42. Target end-state

The entire organisation onboarding journey should ultimately look like:

```text
                     BAOBAB PLATFORM
                           │
                           ▼
                    APPLY TO BAOBAB
                           │
                           ▼
                  ClientApplication
                           │
                ┌──────────┴──────────┐
                │                     │
             Reject                Approve
                                      │
                                      ▼
                              Subscription Policy
                                      │
                         ┌────────────┴────────────┐
                         │                         │
                  INTERNAL_GROUP            COMMERCIAL
                      R0                        billed
                         │                         │
                         └────────────┬────────────┘
                                      ▼
                               ProvisioningPlan
                                      │
                                      ▼
                                BAOBAB CONTROL
                                    PLANE
                                      │
               ┌──────────────────────┼─────────────────────┐
               │                      │                     │
               ▼                      ▼                     ▼
             IAM                   Tenant                Context
               │                      │                     │
               └──────────────────────┼─────────────────────┘
                                      │
                           Capability Entitlements
                                      │
                                      ▼
                              CapabilityBindings
                                      │
            ┌─────────────────────────┼─────────────────────────┐
            │             │           │            │            │
            ▼             ▼           ▼            ▼            ▼
           IAM          Trade        ERP           CMS         Pulse
        Keycloak       Medusa     iDempiere      Payload      Haystack
            │             │           │            │            │
            └─────────────┴───────────┴────────────┴────────────┘
                                      │
                                      ▼
                           Canonical Mappings
                                      │
                                      ▼
                               Reconciliation
                                      │
                                      ▼
                                  Sandbox
                                      │
                                      ▼
                                     UAT
                                      │
                                      ▼
                                Go-Live Gate
                                      │
                                      ▼
                                  ACTIVE
```

## Final architectural principle

**Baobab onboarding should not be “create accounts in five products.”**

It should be:

> **Approve an organisation once, define its desired Baobab platform state once, and let the Control Plane provision and continuously reconcile that state across every authorised engine.**

That approach turns onboarding from a one-off setup procedure into a reusable platform capability. It also gives Baobab a clean path from onboarding Nabhold and its subsidiaries today to onboarding independent enterprise customers later without maintaining two architectures.
