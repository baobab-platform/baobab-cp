# ADR-BCP-020 — Administrative Authority, Delegated Administration, Privileged Access and Separation-of-Duties Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-23  
**Decision Owners:** Baobab Platform Architecture / Security  
**Primary Repository:** `baobab-platform/baobab-cp`  
**Runtime Authority:** Baobab Control Plane  
**Canonical Contract Authority:** `baobab-platform/shared`  
**Identity / Authentication Authority:** `baobab-platform/baobab-iam`  
**Administrative Human Interface:** Baobab Control Plane Console  
**Decision Type:** Foundational administrative authorization, delegation, privilege-governance and separation-of-duties architecture

**Depends On / Reconciles With:**

- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
- ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
- ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model
- ADR-BCP-007 — Control Plane APIs, Capability Resolution Contracts, Caching, Resolution Assertions and Service-to-Service Consumption Model
- ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model
- ADR-BCP-009 — Capability-Centric Security, Isolation, Residency, Revocation and Failure Semantics
- ADR-BCP-010 — Modular Control Plane Architecture, Governance Boundaries and Evolution Model
- ADR-BCP-017 — Organisation Admission, Subscription Classification and Tenant Onboarding Lifecycle Model
- ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account and Tenant Relationship Model
- ADR-BCP-019 — Control Plane Administrative Frontend, Organisation Onboarding Experience and Repository Composition Model
- `BCP-TS-ONBOARDING-001` — Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification
- Baobab IAM ADR-0005 — Realm, Organization, Tenant and Legal-Entity Model
- Baobab IAM ADR-0006 — OIDC, OAuth and Token Profile
- Baobab IAM ADR-0008 — Platform Authorization Architecture
- Baobab IAM ADR-0009 — Workforce SSO and Privileged Access
- Baobab IAM ADR-0015 — Credential Security, MFA, Passkeys and Account Recovery
- Baobab IAM ADR-0016 — Identity Lifecycle, Revocation and Deprovisioning
- Baobab IAM ADR-0017 — IAM Audit, Security Events and Observability

**External Validation References:**

- NIST SP 800-53 Rev. 5 — AC-5 Separation of Duties; AC-6 Least Privilege
- NIST SP 800-207 — Zero Trust Architecture
- NIST SP 800-171 Rev. 3 — Least Privilege and privilege review
- OWASP Application Security Verification Standard 5.0 — Authorization
- CISA guidance on privileged access and Just-in-Time administrative access
- Keycloak Fine-Grained Admin Permissions and Organizations administration

---

# 1. Executive Decision

Baobab SHALL introduce a first-class **Administrative Authority Model** within the Control Plane.

Administrative privilege SHALL NOT be represented merely as:

```text
admin = true
```

or:

```text
role = admin
```

or:

```text
Keycloak role = platform-admin
```

Administrative authority SHALL instead be an explicit, scoped, auditable, lifecycle-managed relationship between:

```text
Principal
    │
    ▼
Administrative Grant
    │
    ├── permitted administrative action
    ├── resource/context scope
    ├── conditions
    ├── validity period
    ├── delegation constraints
    ├── risk classification
    ├── approval provenance
    └── lifecycle state
```

The governing principle is:

> **Authentication establishes who the administrator is. The Control Plane establishes what that administrator may administer, for whom, within which scope, under which conditions, and for how long.**

A second governing principle is:

> **Administrative authority is a relationship, not an identity type.**

A third governing principle is:

> **Corporate hierarchy, organisation membership, platform affiliation, employment status, executive position and IAM organisation membership SHALL NOT independently imply administrative authority.**

A fourth governing principle is:

> **Administrative authority SHALL be least-privileged, explicitly scoped, revocable, auditable and incapable of silently expanding through organisational relationships.**

---

# 2. Why This ADR Is Required

ADR-BCP-019 establishes a human-facing Control Plane Console supporting:

```text
Platform Operators
Admission Reviewers
Platform Approvers
Security Administrators
Organisation Administrators
Organisation Auditors
Support Operators
Platform Auditors
```

However, those labels alone do not answer the security questions necessary for implementation.

Baobab must determine:

```text
Who may make someone an administrator?

Administrator of what?

For which organisation?

For which tenant?

For which market?

For which PlatformAccount?

For which administrative actions?

Does authority extend to subsidiaries?

Can an administrator delegate that authority?

Can the delegate delegate again?

When does authority expire?

Who may revoke it?

Does a corporate-group administrator automatically control newly acquired subsidiaries?

Can support staff inspect customer configuration?

Can support staff act as the customer?

May the person requesting a production change approve the same change?

What happens when an administrator leaves their employer?

What happens when a tenant is suspended?

How quickly does revocation become effective?

How is emergency access granted?

How do auditors reconstruct the authority chain?
```

These are not frontend concerns.

They are Control Plane authorization concerns.

---

# 3. External Security Basis

NIST SP 800-53 distinguishes:

```text
AC-5 — Separation of Duties
AC-6 — Least Privilege
```

and requires access authorizations to support those principles.

NIST Zero Trust similarly calls for least-privilege, per-request access decisions rather than relying on implicit trust derived from network location or prior authentication.

OWASP ASVS 5 requires authorization rules to be documented and explicitly addresses function-level, data-specific, cross-tenant and administrative access controls.

CISA recommends time-bounded or Just-in-Time privileged access where appropriate instead of unnecessarily persistent administrative rights.

Current Keycloak releases support organisation-scoped delegated administration through Fine-Grained Admin Permissions, demonstrating that identity-side organisation administration can be scoped without granting realm-wide control.

Baobab SHALL adopt these principles while preserving its existing architectural boundary:

```text
Keycloak administration
        !=
Baobab Control Plane administration
```

---

# 4. Authority Boundaries

The canonical administrative model remains layered.

```text
                      HUMAN
                        │
                        ▼
                  BAOBAB IAM
              Who is this person?
                        │
                        ▼
             Authentication Assurance
                        │
                        ▼
                BAOBAB CONTROL PLANE
                        │
       What may this person administer?
                        │
                        ▼
           Administrative Authority
                        │
               ┌────────┴────────┐
               ▼                 ▼
       CP Administration    Domain Context
                                  │
                                  ▼
                          Domain Engine
                         Business Authority
```

The responsibilities are:

| Authority | Owns |
|---|---|
| Baobab IAM | Identity, authentication, credentials, MFA/passkeys, sessions, coarse OAuth access |
| Baobab CP | Administrative authority over Control Plane resources and platform context |
| Domain Engine | Domain-specific administrative/business authorization |
| CP Console | Human interface only |

---

# 5. Fundamental Invariants

The following SHALL remain binding:

```text
Authenticated User
    != Administrator

IAM Organisation Member
    != Organisation Administrator

Employee
    != Platform Administrator

Executive
    != Superuser

Corporate Parent
    != Administrator of Subsidiary

PlatformAccount Member
    != Tenant Administrator

Organisation Administrator
    != Domain Administrator

Control Plane Administrator
    != IAM Administrator

Control Plane Administrator
    != ERP Administrator

Control Plane Administrator
    != Trade Administrator

Support Operator
    != Customer Administrator

Application Reviewer
    != Application Approver

Requester
    != Approver
where separation-of-duties policy requires independence

Role Name
    != Authority

Frontend Visibility
    != Authorization
```

---

# 6. Administrative Authority as a First-Class Domain Concept

Baobab SHALL introduce a canonical administrative authority aggregate or equivalent domain model.

Conceptually:

```text
AdministrativeGrant
├── id
├── principal_id
├── permission_id
├── scope
├── conditions
├── grant_type
├── delegation_policy?
├── source
├── risk_class
├── valid_from
├── valid_until?
├── status
├── granted_by
├── approval_reference?
├── reason
├── created_at
├── updated_at
├── revoked_at?
├── revoked_by?
├── revocation_reason?
└── version
```

Exact physical representation SHALL be determined during implementation.

The semantic model defined by this ADR SHALL remain stable.

---

# 7. Principal

The subject of an administrative grant SHALL be a canonical principal.

For human administration this will normally resolve to:

```text
CanonicalIdentity
```

through the IAM mapping model.

A principal SHALL NOT be identified authoritatively by:

```text
email address
display name
username alone
```

because those may change.

Canonical identifiers SHALL be used.

---

# 8. Administrative Permission

An `AdministrativePermission` SHALL represent an administrative action over Control Plane-owned resources.

Examples may include:

```text
application.review
application.request-information
application.decide

organisation.view
organisation.manage
organisation.relationship.manage

platform-account.view
platform-account.manage

tenant.view
tenant.provision
tenant.activate
tenant.suspend
tenant.reinstate
tenant.decommission

market.view
market.request
market.activate
market.deactivate

subscription.view
subscription.manage

digital-estate.view
digital-estate.manage

administrator.view
administrator.grant
administrator.revoke
administrator.delegate

security.isolation.view
security.isolation.manage

security.residency.view
security.residency.manage

readiness.view
reconciliation.request

audit.view

support.diagnostics.view

changeset.submit
changeset.review
changeset.approve
changeset.apply

breakglass.request
breakglass.approve
```

The exact canonical vocabulary SHALL be defined in `baobab-platform/shared`.

---

# 9. Administrative Permissions Are Not Product Capabilities

Baobab SHALL distinguish:

```text
AdministrativePermission
```

from:

```text
Capability
```

A capability answers:

> What platform business capability may a tenant or estate consume?

An administrative permission answers:

> What Control Plane administrative action may this administrator perform?

For example:

```text
commerce.order.manage
```

may be a business capability.

Whereas:

```text
subscription.manage
```

is a Control Plane administrative permission.

These vocabularies SHALL NOT be conflated.

---

# 10. Administrative Action Categories

Administrative permissions SHOULD be organised into policy domains.

| Domain | Example Actions |
|---|---|
| Admission | Review or decide applications |
| Organisation | Manage organisation identity/relationships |
| Account | Manage PlatformAccount configuration |
| Tenant | Provision, suspend, activate, decommission |
| Market | Activate/deactivate market participation |
| Subscription | Manage product subscription state |
| Digital Estate | Register or manage estates |
| Administration | Grant/revoke delegated administration |
| Security | Isolation/residency/security controls |
| Operations | Readiness/reconciliation/diagnostics |
| Audit | Audit visibility/export |
| Change Control | Submit/review/approve/apply changes |
| Emergency | Break-glass administration |

---

# 11. Administrative Scope

Every administrative grant SHALL carry explicit scope.

Conceptually:

```text
AdministrativeScope
├── platform?
├── platform_account_id?
├── corporate_group_id?
├── organisation_id?
├── tenant_id?
├── legal_entity_id?
├── market_id?
├── digital_estate_id?
├── environment?
├── resource_type?
├── resource_id?
└── scope_mode
```

Not every field applies to every permission.

---

# 12. Scope Is Multidimensional

Administrative scope SHALL not be reduced to:

```text
tenant_id
```

because administration may occur:

```text
before a tenant exists
```

or at:

```text
organisation
corporate group
PlatformAccount
application
market
estate
platform
```

levels.

Examples:

```text
Admission Reviewer
scope = application queue

Organisation Administrator
scope = Organisation A

Tenant Administrator
scope = Tenant T1

Market Administrator
scope = Organisation A + Uganda

Digital Estate Administrator
scope = Estate E1

Platform Auditor
scope = platform-wide read-only
```

---

# 13. Scope Matching

Authorization SHALL require an explicit match between:

```text
requested action
requested resource/context
effective administrative grant
```

Conceptually:

```text
Principal
   │
   ▼
AdministrativeGrant
   │
   ├── permission matches?
   ├── scope matches?
   ├── grant active?
   ├── time valid?
   ├── conditions satisfied?
   ├── principal active?
   └── policy satisfied?
          │
          ▼
        ALLOW
```

Any required failure SHALL produce:

```text
DENY
```

or an explicit obligation such as:

```text
STEP_UP_REQUIRED
APPROVAL_REQUIRED
```

where policy allows continued workflow.

---

# 14. Deny by Default

Administrative access SHALL follow:

```text
DEFAULT = DENY
```

Absence of a grant SHALL NOT mean permission.

Unknown scope SHALL NOT mean global scope.

Ambiguous context SHALL NOT be guessed.

---

# 15. No Implicit Administrative Inheritance

The following is prohibited:

```text
Parent organisation
        │
        ▼
automatically administer subsidiaries
```

Likewise:

```text
PlatformAccount owner
        │
        ▼
automatically administer every tenant
```

and:

```text
CorporateGroup membership
        │
        ▼
automatic administration of all members
```

Corporate topology describes organisation relationships.

It does not independently grant authority.

---

# 16. Group Administration Is Explicit

Baobab MAY grant group-wide administration.

But it SHALL be explicit.

Example:

```text
AdministrativeGrant
principal = Jane
permission = organisation.view
scope = CorporateGroup ACME
scope_mode = GROUP_DESCENDANTS
```

This is materially different from inferring authority because Jane administers the parent company.

---

# 17. Group Scope Expansion Risk

Group-based scope introduces an important security concern.

Suppose:

```text
Jane administers ACME Group
```

and later:

```text
ACME acquires NewCo Ltd
```

If `NewCo` enters the corporate group, Jane's effective administrative scope might expand.

Baobab SHALL therefore treat relationship-driven scope expansion as consequential.

The system SHALL support explicit policies such as:

```text
STATIC_MEMBERSHIP
```

or:

```text
DYNAMIC_GROUP_DESCENDANTS
```

---

# 18. Static Group Scope

`STATIC_MEMBERSHIP` SHALL mean:

```text
grant applies only to the organisations
explicitly included at grant time
```

Adding an organisation to the corporate group does not automatically expand the grant.

---

# 19. Dynamic Group Scope

`DYNAMIC_GROUP_DESCENDANTS` MAY be supported where business governance requires it.

The grant SHALL explicitly state that authority follows the current corporate graph.

Changes to corporate relationships capable of expanding privileged scope SHALL trigger:

```text
impact analysis
audit
and, where policy requires,
approval
```

---

# 20. Default Group Behaviour

The safer default SHALL be:

```text
NO AUTOMATIC EXPANSION
```

unless a dynamic group delegation is explicitly configured.

---

# 21. Administrative Profiles

For usability Baobab MAY define named administrative profiles such as:

```text
Platform Operator
Admission Reviewer
Admission Approver
Organisation Administrator
Group Administrator
Tenant Administrator
Market Administrator
Integration Administrator
Security Administrator
Support Operator
Organisation Auditor
Platform Auditor
```

A profile SHALL be:

```text
a policy template
```

not the authoritative grant itself.

---

# 22. Profile Expansion

Conceptually:

```text
Organisation Administrator
        │
        ▼
Administrative Profile
        │
        ├── organisation.view
        ├── organisation.manage
        ├── tenant.view
        ├── market.view
        ├── subscription.view
        ├── digital-estate.view
        └── administrator.view
        │
        ▼
Scoped Administrative Grants
```

The resulting permissions SHALL always retain resource scope.

---

# 23. No Universal `admin`

Baobab SHALL avoid introducing a routine application role named simply:

```text
admin
```

because it fails to communicate:

```text
admin of what?
for whom?
in which environment?
with which permissions?
for how long?
```

Where bootstrap or emergency infrastructure requires extremely broad authority, that authority SHALL be exceptional and separately governed.

---

# 24. Platform Administrator

A Platform Administrator MAY possess broad CP permissions.

However:

```text
Platform Administrator
```

does not mean:

```text
Keycloak Realm Administrator
ERP Administrator
Trade Administrator
Database Administrator
Infrastructure Administrator
```

Those remain separate privilege domains.

---

# 25. Organisation Administrator

An Organisation Administrator SHALL administer authorised Control Plane configuration relating to a defined organisation.

Potential responsibilities MAY include:

```text
view organisation configuration
manage permitted organisation metadata
view associated tenants
request market changes
request service changes
manage permitted Digital Estate metadata
manage lower-risk organisation administrators
view readiness
view permitted audit history
```

Exact permissions SHALL be policy-driven.

---

# 26. Organisation Administrator Does Not Own the Organisation

Administrative authority SHALL NOT modify the canonical fact of:

```text
corporate ownership
```

or:

```text
legal ownership
```

merely because a person is an Organisation Administrator.

Administrative responsibility and corporate ownership remain independent.

---

# 27. Group Administrator

A Group Administrator MAY administer explicitly authorised organisations across a CorporateGroup.

For example:

```text
ACME GROUP ADMIN
       │
       ├── ACME FOODS
       ├── ACME LOGISTICS
       └── ACME UGANDA
```

This SHALL require explicit group-scoped authority.

The existence of the group alone does not create it.

---

# 28. Tenant Administrator

A Tenant Administrator MAY administer tenant-scoped platform configuration.

This SHALL NOT automatically grant:

```text
organisation-level authority
other tenant authority
corporate-group authority
```

---

# 29. Market Administrator

A Market Administrator MAY have authority constrained by market.

Example:

```text
Principal: Jane
Organisation: ACME
Market: Uganda
```

shall not imply authority over:

```text
South Africa
Kenya
Tanzania
```

---

# 30. Digital Estate Administrator

A Digital Estate administrator MAY administer CP-owned configuration for one or more Digital Estates.

That SHALL NOT automatically grant administration of the business application itself.

For example:

```text
CP:
may manage domain registration and estate capability requirements

Payload:
may not necessarily publish content

Trade:
may not necessarily manage products
```

---

# 31. Security Administrator

Security administration SHALL be separately assignable.

Sensitive actions include:

```text
isolation requirement changes
residency-policy changes
privileged administrator changes
break-glass review
security-policy overrides
```

Security administration SHOULD receive stronger assurance and approval requirements.

---

# 32. Auditor

Auditor authority SHALL be principally read-only.

An auditor MAY receive broad visibility without mutation rights.

Conceptually:

```text
VIEW
VIEW HISTORY
VIEW DECISIONS
EXPORT where approved
```

but not:

```text
MUTATE
APPROVE OWN CHANGE
DELETE AUDIT
```

---

# 33. Support Operator

Support access SHALL be narrower than general administration.

A Support Operator SHOULD normally receive:

```text
diagnostic read access
readiness visibility
operation status
safe support metadata
```

rather than:

```text
customer configuration mutation
privilege grants
secret retrieval
business-domain authority
```

---

# 34. Admission Reviewer

An Admission Reviewer MAY:

```text
inspect application
review evidence
request information
record assessment
```

but SHALL NOT automatically possess:

```text
final admission decision
tenant activation
platform administrator grant
```

---

# 35. Admission Approver

Admission approval MAY be assigned separately from review.

This supports:

```text
Reviewer
   │
   ▼
Recommendation
   │
   ▼
Approver
   │
   ▼
Decision
```

where policy requires independence.

---

# 36. Separation of Duties

Baobab SHALL support explicit Separation-of-Duties policies.

Conceptually:

```text
SeparationOfDutiesPolicy
├── id
├── action_or_permission_set
├── conflicting_permissions[]
├── minimum_distinct_actors
├── applies_to_scope
├── risk_threshold
└── status
```

---

# 37. Separation-of-Duties Examples

Policies MAY state:

```text
Requester cannot approve own high-risk change.
```

```text
Administrator granting privileged access
cannot be sole approver of that grant.
```

```text
Security administrator changing residency policy
cannot independently waive the associated control.
```

```text
Audit administrator
cannot modify the audit evidence under review.
```

NIST AC-5 explicitly identifies separation of duties as a control against abuse of authorised privilege.

---

# 38. Maker-Checker

High-impact administrative actions SHOULD support:

```text
MAKER
   │
   ▼
REQUEST
   │
   ▼
CHECKER
   │
   ▼
APPROVAL
   │
   ▼
EXECUTION
```

Exact workflows will be formalised further by ADR-BCP-021.

This ADR establishes that the authorization model SHALL support such separation.

---

# 39. Self-Approval

Self-approval SHALL default to prohibited for:

```text
high-risk privilege escalation
production activation
tenant decommissioning
isolation downgrade
residency relaxation
break-glass approval
```

where an independent approver is available and policy requires one.

---

# 40. Risk Classification

Administrative permissions SHOULD carry a risk classification.

Recommended conceptual levels:

```text
LOW
MODERATE
HIGH
CRITICAL
```

Risk may influence:

```text
authentication assurance
approval count
allowed delegation
duration
audit intensity
notification
review frequency
```

---

# 41. Example Risk Matrix

| Administrative Action | Illustrative Risk |
|---|---|
| View organisation profile | Low |
| View readiness | Low |
| Change organisation display metadata | Moderate |
| Invite ordinary organisation administrator | Moderate/High |
| Activate production tenant | High |
| Change isolation policy | High |
| Relax residency restriction | Critical |
| Decommission production tenant | Critical |
| Grant platform-wide administrator | Critical |
| Activate break-glass authority | Critical |

The exact classification SHALL be policy-defined.

---

# 42. Delegated Administration

Baobab SHALL support explicit administrative delegation.

A delegation answers:

> Under what authority may Principal A grant some portion of their administrative authority to Principal B?

Conceptually:

```text
Delegation
├── id
├── delegator_principal_id
├── delegatee_principal_id
├── permissions[]
├── scope
├── valid_from
├── valid_until?
├── delegation_depth
├── approval_reference?
├── reason
└── status
```

---

# 43. Delegation Does Not Create Privilege

A delegator SHALL NOT delegate authority they do not possess.

The invariant is:

```text
Delegated Authority
    ⊆
Delegator Effective Authority
```

never:

```text
Delegated Authority
    >
Delegator Effective Authority
```

---

# 44. Scope Cannot Expand Through Delegation

Example:

```text
Jane:
Organisation Administrator
Scope = ACME Uganda
```

Jane SHALL NOT delegate:

```text
ACME South Africa
```

or:

```text
Entire ACME Group
```

unless Jane independently possesses that authority.

---

# 45. Permission Cannot Expand Through Delegation

If Jane possesses:

```text
organisation.view
tenant.view
```

she SHALL NOT delegate:

```text
tenant.decommission
```

---

# 46. Delegation Duration Cannot Exceed Source Authority

If Jane's grant expires:

```text
2026-12-31
```

a delegated grant sourced solely from Jane SHALL NOT remain effective beyond that authority unless a separate authorised grant exists.

Conceptually:

```text
delegated_valid_until
<=
source_grant_valid_until
```

where the source grant is time-bound.

---

# 47. Delegation Chain

Delegation SHALL preserve provenance.

Example:

```text
Baobab Platform Authority
         │
         ▼
Alice — Group Administrator
         │
         ▼
Bob — Organisation Administrator
         │
         ▼
Carol — Tenant Viewer
```

The platform SHALL be able to reconstruct the complete authority chain.

---

# 48. Delegation Depth

Delegation depth SHOULD be constrained.

Default:

```text
delegation_depth = 1
```

unless policy explicitly permits further delegation.

Unlimited recursive delegation SHALL be prohibited.

---

# 49. Non-Delegable Permissions

Certain permissions SHALL be designated non-delegable.

Candidates include:

```text
platform-wide admin grant
break-glass approval
security-policy override
residency override
audit-governance administration
```

Exact policy SHALL be defined centrally.

---

# 50. Organisation Self-Administration

Baobab MAY allow an Organisation Administrator to create lower-level administrators.

Example:

```text
Organisation Administrator
        │
        ├── Tenant Administrator
        ├── Market Administrator
        └── Organisation Auditor
```

only where:

```text
delegation policy permits
scope remains inside organisation
permission set does not exceed delegator
risk policy permits
```

---

# 51. Platform-to-Customer Delegation

Initial customer administration will typically originate from Baobab during onboarding.

Conceptually:

```text
Baobab Platform Approver
        │
        ▼
Initial Organisation Administrator
        │
        ▼
Controlled Delegation
        │
        ▼
Additional Customer Administrators
```

The initial administrator SHALL be explicitly established.

Applicant status alone SHALL not create this authority.

---

# 52. Managed-Service Delegation

Future customers may delegate limited administration to third parties.

Example:

```text
ACME Holdings
     │
     ▼
Managed IT Provider
     │
     ▼
limited administrative authority
```

This SHALL use normal delegated authority.

It SHALL NOT require customers to share credentials.

---

# 53. External Consultant Scenario

Example:

```text
Consultant:
Principal = CI-0091

Scope:
ACME Uganda

Permissions:
market.view
digital-estate.view
integration.manage

Validity:
2026-10-01 → 2026-10-31
```

At expiry:

```text
authority = EXPIRED
```

without deleting the consultant's canonical identity.

---

# 54. Standing vs Time-Bound Privilege

Administrative grants SHALL distinguish:

```text
STANDING
```

from:

```text
TIME_BOUND
```

and future:

```text
JUST_IN_TIME
```

authority.

Standing high-risk privilege SHOULD be minimised.

---

# 55. Just-in-Time Administrative Authority

Baobab SHOULD support Just-in-Time privileged authority for selected high-risk operations.

The conceptual flow is:

```text
Ordinary Administrator
        │
        ▼
Request Elevated Authority
        │
        ▼
Policy Evaluation
        │
        ├── reason
        ├── target
        ├── requested duration
        ├── authentication assurance
        └── approvals
        │
        ▼
Temporary Administrative Grant
        │
        ▼
Use
        │
        ▼
Automatic Expiry
```

CISA recommends time-based/JIT access for administrative privileges as a least-privilege control.

---

# 56. JIT Is Not Mandatory Everywhere

Baobab SHALL NOT make low-risk routine administration unnecessarily cumbersome.

JIT SHOULD be focused on:

```text
platform-wide administration
security-critical configuration
break-glass operations
high-impact production actions
unusual support access
```

---

# 57. Administrative Grant Lifecycle

An administrative grant SHALL have an explicit lifecycle.

Recommended model:

```text
PENDING
   │
   ▼
ACTIVE
   │
   ├────► SUSPENDED
   │           │
   │           └────► ACTIVE
   │
   ├────► EXPIRED
   │
   └────► REVOKED
```

---

# 58. PENDING

`PENDING` means:

```text
administrative authority has been proposed
but is not yet effective
```

Possible reasons:

```text
approval pending
identity verification incomplete
start date in future
step-up enrollment incomplete
```

---

# 59. ACTIVE

`ACTIVE` means the grant is eligible for authorization evaluation.

It does not mean every action succeeds.

Other policies remain applicable.

---

# 60. SUSPENDED

`SUSPENDED` SHALL represent temporary disablement of that administrative grant.

It SHALL be reversible.

---

# 61. EXPIRED

A time-bounded grant SHALL transition to `EXPIRED` automatically after its validity window.

No manual clean-up SHALL be required to make expiry effective.

---

# 62. REVOKED

`REVOKED` SHALL indicate deliberate termination.

Revocation SHALL preserve:

```text
who revoked
when
why
authority chain
historical audit
```

---

# 63. Narrow Revocation

Consistent with IAM ADR-0016:

> revoke the narrowest authority that is no longer valid.

For example:

```text
remove ACME Uganda administration
```

SHALL NOT automatically:

```text
disable person's global identity
```

if that person retains legitimate other relationships.

---

# 64. Broad Security Revocation

Where compromise is suspected, broader controls MAY include:

```text
AdministrativeGrant suspension
all administrative grant suspension
session revocation
identity suspension
credential revocation
```

depending on incident severity.

IAM and CP SHALL coordinate according to their respective authority boundaries.

---

# 65. Joiner / Mover / Leaver

Administrative privilege SHALL participate in lifecycle governance.

## Joiner

```text
identity established
   ↓
relationship verified
   ↓
minimum required admin grant
   ↓
strong authentication enrolled
```

## Mover

```text
responsibilities change
   ↓
existing grants reviewed
   ↓
obsolete grants removed
   ↓
new minimum grants added
```

## Leaver

```text
employment/representation ends
   ↓
administrative grants revoked
   ↓
sessions revoked where required
   ↓
identity retained for historical attribution
```

---

# 66. Organisation Relationship Termination

If a person ceases representing an external organisation, organisation-derived administrative grants SHALL be reviewed or revoked.

The canonical identity MAY remain active if other legitimate relationships remain.

---

# 67. Tenant Suspension

If:

```text
Tenant = SUSPENDED
```

administrative permissions MAY remain available for:

```text
diagnosis
billing/commercial resolution
reinstatement
audit
```

where explicitly permitted.

Suspension SHALL not necessarily prevent all administration.

This is distinct from runtime tenant access.

---

# 68. Organisation Offboarding

Organisation offboarding SHALL include:

```text
administrative authority inventory
delegated-authority inventory
revocation plan
support-session termination
IAM projection changes
audit preservation
```

No orphaned customer-administration grants SHALL remain active after final decommissioning.

---

# 69. Privilege Review

Administrative privileges SHALL be reviewable.

NIST guidance specifically calls for periodic review of assigned privileges and removal/reassignment where no longer necessary.

Baobab SHOULD therefore support:

```text
periodic access review
event-triggered review
manager/owner recertification
security recertification
```

---

# 70. Event-Triggered Review

Privilege review SHOULD occur when relevant events happen.

Examples:

```text
organisation changes owner
corporate relationship changes
administrator changes employer
tenant becomes high risk
security incident occurs
PlatformAccount changes
new subsidiary joins dynamic group scope
role responsibilities change
```

---

# 71. Access Review Outcome

A review SHALL be capable of producing:

```text
RETAIN
REDUCE
SUSPEND
REVOKE
```

and SHALL be auditable.

---

# 72. Authentication Assurance

Administrative access SHALL be able to require stronger authentication assurance than ordinary use.

Potential policy:

```text
Read low-risk administration
        ↓
normal authenticated session

Privileged configuration
        ↓
MFA

Critical configuration
        ↓
phishing-resistant step-up
```

The exact assurance mechanism remains Baobab IAM responsibility.

---

# 73. Step-Up Authentication

A valid session MAY still be insufficient for a sensitive operation.

Example:

```text
Administrator authenticated
        │
        ▼
requests tenant decommission
        │
        ▼
CP determines
STEP_UP_REQUIRED
        │
        ▼
Baobab IAM
        │
        ▼
stronger assurance
        │
        ▼
operation continues
```

---

# 74. Authentication Assurance Is Not Permission

The invariant remains:

```text
strong MFA
    !=
administrative permission
```

A person cannot gain authority simply by authenticating more strongly.

Step-up proves assurance.

The grant proves authority.

---

# 75. Token Design

Access tokens SHALL NOT contain a complete persistent copy of all administrative grants.

Reasons include:

```text
staleness
token bloat
revocation delay
scope complexity
group-relationship changes
```

Tokens MAY carry:

```text
canonical principal reference
client
coarse scopes
organisation context where appropriate
authentication assurance
```

The CP SHALL evaluate authoritative administrative state.

---

# 76. Coarse IAM Scope

A token may have:

```text
scope = cp:admin
```

or equivalent.

That means:

> This client/principal may invoke administrative API classes.

It SHALL NOT mean:

> The principal is a platform-wide administrator.

---

# 77. Keycloak Organizations

Keycloak Organizations MAY support:

```text
organisation membership
member invitation
organisation-specific authentication
organisation groups
identity-side delegated management
```

and current Keycloak Fine-Grained Admin Permissions can restrict delegated administrators to particular organisations.

Baobab SHALL use these features selectively where they align with IAM architecture.

---

# 78. Keycloak Is a Projection, Not CP Authority

For Baobab administration:

```text
CP AdministrativeGrant
```

remains authoritative.

Where Keycloak requires corresponding identity administration configuration, CP/IAM orchestration MAY project suitable state into Keycloak.

Conceptually:

```text
CP Administrative Authority
        │
        ▼
IAM Integration Policy
        │
        ▼
Keycloak FGAP / Organisation Permission
```

not:

```text
Keycloak role
        │
        ▼
infer entire Baobab administrative authority
```

---

# 79. IAM Administrator Boundary

Someone authorised to manage Baobab organisation configuration SHALL NOT automatically receive:

```text
manage-realm
manage-clients
manage-identity-providers
manage-authentication
```

in Keycloak.

Identity-side privileges SHALL be independently scoped.

---

# 80. No Role Explosion

Baobab SHALL avoid generating roles such as:

```text
acme-uganda-market-admin
acme-uganda-tenant-admin
acme-za-market-admin
acme-za-tenant-admin
...
```

inside Keycloak for every possible CP scope combination.

That would reproduce CP's contextual authorization model inside IAM.

Instead:

```text
IAM = identity + coarse application access

CP = contextual administrative authority
```

---

# 81. Domain Administration

This ADR governs Control Plane administration.

It SHALL NOT move domain administrative authority into CP.

For example:

```text
CP Tenant Administrator
```

does not automatically become:

```text
iDempiere System Administrator
Medusa Store Administrator
Payload Publisher
```

---

# 82. Domain-Admin Access Enablement

CP MAY establish that a person is eligible to reach a domain administration environment.

The domain engine still determines what that person may do there.

Example:

```text
IAM:
Peter authenticated

CP:
Peter may access ACME ERP administration context

iDempiere:
Peter has Finance Manager role
```

All three layers remain necessary.

---

# 83. Support Access

Customer support presents special privilege risk.

Baobab SHALL distinguish:

```text
SUPPORT VISIBILITY
```

from:

```text
CUSTOMER ADMINISTRATION
```

A support operator SHOULD default to diagnostic visibility.

---

# 84. No Shared Credentials

Support personnel SHALL NOT ask customers to share administrator passwords.

Likewise customer administrators SHALL NOT share their credentials with managed-service providers.

Delegated access SHALL use explicit principal authority.

---

# 85. No Opaque Impersonation

Baobab SHALL NOT introduce hidden or unaudited customer impersonation.

If impersonation-like support capability is ever introduced, it SHALL use an explicit controlled construct such as:

```text
SupportAccessSession
```

---

# 86. Support Access Session

Conceptually:

```text
SupportAccessSession
├── id
├── support_principal_id
├── target_organisation_id
├── target_tenant_id?
├── requested_permissions[]
├── reason
├── case_reference?
├── approved_by?
├── valid_from
├── valid_until
├── status
└── correlation_id
```

---

# 87. Support Session UX

While a support session is active, the CP Console SHALL display an unmistakable indication such as:

```text
SUPPORT ACCESS ACTIVE

Organisation:
ACME Holdings

Expires:
14:30 UTC

Reason:
CASE-10482
```

Support context SHALL not look like the operator's ordinary platform context.

---

# 88. Support Mutation

Support mutation rights SHALL be separately granted.

Read-only diagnostic access SHALL NOT silently become write authority.

---

# 89. Break-Glass Administration

Baobab SHALL support an emergency-access model for exceptional incidents.

Break-glass authority is intended for:

```text
critical platform failure
identity-control failure
security incident
customer-impacting emergency
```

It SHALL NOT become a convenient shortcut around normal governance.

---

# 90. Break-Glass Properties

Break-glass access SHALL be:

```text
exceptional
strongly authenticated
time-bounded
reason-bound
highly visible
audited
automatically expiring
post-reviewed
```

where technically possible.

---

# 91. Break-Glass Flow

```text
Emergency
   │
   ▼
Break-Glass Request
   │
   ├── target scope
   ├── reason
   ├── requested duration
   └── incident reference
   │
   ▼
Strong Authentication
   │
   ▼
Approval if available
   │
   ▼
Temporary Authority
   │
   ▼
Action
   │
   ▼
Automatic Expiry
   │
   ▼
Mandatory Review
```

---

# 92. Emergency Approval Failure

If the normal approval authority is unavailable during a genuine incident, emergency policy MAY permit specially governed activation.

Such activation SHALL produce:

```text
immediate alert
elevated audit record
strict expiry
mandatory post-event review
```

The exact mechanism belongs in security operations implementation.

---

# 93. Break-Glass Is Not Permanent Superuser

A permanently active universal account such as:

```text
root-admin-everything
```

SHOULD be avoided as the ordinary break-glass mechanism.

Where underlying technologies require emergency superuser credentials, those credentials SHALL be separately protected and SHALL not become ordinary CP Console accounts.

---

# 94. Administrative Policy Evaluation

A Control Plane administrative decision SHOULD evaluate at least:

```text
principal identity state
authentication/session state
client/audience
requested permission
requested scope
AdministrativeGrant state
grant validity period
delegation provenance
resource lifecycle state
environment
risk classification
separation-of-duties rules
required authentication assurance
required approval state
explicit security restrictions
```

---

# 95. Decision Pipeline

```text
Administrative Request
        │
        ▼
Authenticate Principal
        │
        ├── invalid ─────────────► DENY
        ▼
Resolve Canonical Principal
        │
        ├── invalid ─────────────► DENY
        ▼
Resolve Requested Scope
        │
        ├── ambiguous ───────────► DENY
        ▼
Find Effective Administrative Grants
        │
        ├── none ────────────────► DENY
        ▼
Validate Time / Lifecycle / Delegation
        │
        ├── invalid ─────────────► DENY
        ▼
Evaluate SoD / Risk / Conditions
        │
        ├── blocked ─────────────► DENY
        ├── approval missing ────► APPROVAL_REQUIRED
        ├── assurance low ───────► STEP_UP_REQUIRED
        ▼
ALLOW
        │
        ▼
Execute Authoritative CP Command
        │
        ▼
AUDIT
```

---

# 96. Authorization Decision

A security-sensitive administrative decision SHOULD produce a structured result.

Conceptually:

```text
AdministrativeDecision
├── decision_id
├── principal_id
├── action
├── scope
├── outcome
├── matched_grants[]
├── obligations[]
├── reason_codes[]
├── evaluated_at
└── correlation_id
```

---

# 97. Decision Outcomes

Initial outcomes SHOULD include:

```text
ALLOW
DENY
STEP_UP_REQUIRED
APPROVAL_REQUIRED
NOT_READY
```

The exact contract SHALL be standardised in `shared`.

---

# 98. Explainability

A denial SHOULD have a safe, structured reason.

Examples:

```text
NO_ADMINISTRATIVE_GRANT
SCOPE_MISMATCH
GRANT_EXPIRED
GRANT_SUSPENDED
DELEGATION_INVALID
SEPARATION_OF_DUTIES_VIOLATION
AUTHENTICATION_ASSURANCE_INSUFFICIENT
APPROVAL_REQUIRED
ORGANISATION_INACTIVE
TENANT_STATE_PROHIBITS_ACTION
```

Internal policy detail SHALL not be exposed where doing so would create security risk.

---

# 99. Frontend Behaviour

The CP Console MAY use administrative authority to:

```text
show relevant navigation
hide irrelevant actions
disable unavailable controls
explain missing privilege
```

But backend authorization SHALL remain mandatory.

Prohibited architecture:

```text
button hidden
therefore action secure
```

---

# 100. Context Visibility

The Console SHALL make administrative scope visible.

Example:

```text
Administrator:
Jane Smith

Current Scope:
ACME Uganda

Authority:
Tenant Administrator

Environment:
Production
```

Cross-scope actions SHALL not be visually ambiguous.

---

# 101. Scope Switching

Administrators with multiple authorised scopes MAY switch context.

Example:

```text
ACME Uganda
        │
        ▼
ACME South Africa
```

The Console SHALL re-resolve effective authority.

It SHALL NOT assume authority carries from the previous context.

---

# 102. Privilege Elevation Visibility

When JIT or break-glass authority is active, the interface SHALL display this clearly.

Example:

```text
ELEVATED ACCESS ACTIVE
Expires in 18 minutes
```

This reduces accidental use of high privilege.

---

# 103. Administrative Caching

Administrative authorization caching SHALL be conservative.

High-risk authority SHOULD use:

```text
short-lived cache
or
current-state evaluation
```

particularly for:

```text
privilege grants
revocation
security configuration
break-glass
```

---

# 104. Revocation Latency

Baobab SHALL define acceptable revocation latency by risk class.

Critical administrative revocation SHOULD take effect substantially faster than ordinary access-token expiry where technically feasible.

Current-state CP authorization is therefore preferred for consequential actions.

---

# 105. Optimistic Concurrency

Administrative grant modification SHALL use version-aware mutation.

For example:

```text
grant version = 7
```

If another administrator changes the grant before submission:

```text
expected version = 7
actual version = 8
```

the mutation SHALL fail safely rather than overwrite silently.

---

# 106. Administrative Grant Creation Is Consequential

Creating, changing or revoking administrative authority SHALL itself be considered an administrative action.

Therefore:

```text
administrator.grant
administrator.revoke
administrator.delegate
```

require authorization.

There is no out-of-band assumption that an existing administrator may grant arbitrary privileges.

---

# 107. Privilege-Escalation Prevention

A principal SHALL NOT be able to grant themselves authority they do not already possess or that requires independent approval.

The following SHALL fail:

```text
Organisation Administrator
        │
        ▼
grant self
Platform Administrator
```

---

# 108. Circular Delegation

Delegation graphs SHALL be checked for cycles where delegation provenance could become circular.

Prohibited:

```text
Alice delegates to Bob
Bob delegates same authority to Alice
```

in a way that defeats revocation/provenance semantics.

---

# 109. Orphaned Delegation

If a source grant is revoked:

```text
Source Grant
   │
   X
   │
Delegated Grant
```

dependent delegated grants SHALL become ineffective unless they have another independent valid authority source.

---

# 110. Effective Authority

A principal's effective administrative authority may be composed from several grants.

Example:

```text
Jane
├── Grant A
│   └── organisation.view — ACME Group
│
├── Grant B
│   └── tenant.manage — ACME Uganda
│
└── Grant C
    └── audit.view — ACME Uganda
```

The authorization engine SHALL evaluate the relevant grant for the requested action.

---

# 111. No Accidental Union Across Scope

Multiple grants SHALL NOT combine dimensions to manufacture a privilege that was never granted.

Example:

```text
Grant A:
tenant.manage
ACME Uganda

Grant B:
organisation.view
ACME South Africa
```

SHALL NOT result in:

```text
tenant.manage
ACME South Africa
```

---

# 112. PlatformAccount Administration

PlatformAccount administrative authority SHALL remain separate from organisation administration.

A commercial/account administrator may be permitted to:

```text
view account arrangement
view covered organisations
request subscription changes
```

without being permitted to:

```text
administer every tenant
alter corporate ownership
view domain business data
```

---

# 113. Corporate Relationship Administration

Changing:

```text
parent
subsidiary
control
group membership
```

can materially affect governance and dynamic administration scopes.

Such changes SHALL therefore be considered high-impact where they influence effective authority.

Impact analysis SHALL identify:

```text
which administrator scopes expand
which scopes shrink
which delegations become invalid
```

before activation.

---

# 114. New Subsidiary Scenario

Suppose:

```text
ACME Group Admin:
Jane

Current:
ACME Foods
ACME Logistics
```

and:

```text
NewCo
```

is added as a subsidiary.

If Jane has:

```text
STATIC_MEMBERSHIP
```

Jane does not gain NewCo access.

If Jane has:

```text
DYNAMIC_GROUP_DESCENDANTS
```

the platform SHALL recognise that Jane's authority expands and apply the relevant governance policy.

---

# 115. Corporate Separation Scenario

If:

```text
ACME Logistics
```

leaves the group, dynamically inherited group authority SHALL cease according to policy.

Historical administrative records SHALL remain.

---

# 116. Administrative Events

Baobab SHOULD define canonical security/governance events such as:

```text
baobab.administration.grant.created.v1
baobab.administration.grant.activated.v1
baobab.administration.grant.suspended.v1
baobab.administration.grant.revoked.v1
baobab.administration.grant.expired.v1

baobab.administration.delegation.created.v1
baobab.administration.delegation.revoked.v1

baobab.administration.jit.activated.v1
baobab.administration.jit.expired.v1

baobab.administration.breakglass.activated.v1
baobab.administration.breakglass.expired.v1

baobab.administration.support-session.started.v1
baobab.administration.support-session.ended.v1
```

Exact vocabulary SHALL align with the canonical event naming standard.

---

# 117. Event Authority

Only the authoritative owner SHALL emit canonical state-change events.

CP SHALL emit administrative grant events because CP owns administrative authority.

IAM SHALL continue emitting credential/session/authentication events.

---

# 118. Audit Requirements

Every administrative grant mutation SHALL record:

```text
actor
subject
permission
scope
previous state
new state
reason
source authority
approval reference
request ID
correlation ID
time
result
```

where applicable.

---

# 119. Delegation Audit

Delegation audit SHALL allow reconstruction of:

```text
who originally possessed authority
who delegated
who received it
what subset was delegated
when it became effective
when it ended
```

---

# 120. Break-Glass Audit

Break-glass audit SHALL receive enhanced visibility.

It SHOULD include:

```text
incident reference
reason
approver
authentication assurance
scope
actions performed
start
expiry
actual termination
post-review result
```

---

# 121. Audit Independence

Where possible:

```text
administrator of access
```

SHOULD NOT have unilateral ability to:

```text
erase corresponding audit evidence
```

This supports separation of duties.

---

# 122. Administrative Data Exposure

An administrator's permission to configure a tenant SHALL NOT automatically permit unrestricted access to tenant business data.

For example:

```text
CP Operator
can activate tenant
```

does not imply:

```text
CP Operator
can read customer orders
```

---

# 123. Metadata vs Business Data

CP administrators MAY necessarily access platform metadata such as:

```text
tenant status
organisation name
markets
capability state
readiness
```

This SHALL NOT be extended unnecessarily into:

```text
customer transactions
financial records
commercial secrets
personal content
```

owned by engines.

---

# 124. Zero-Trust Principle

Administrative authorization SHALL be evaluated based on current context and policy rather than relying on:

```text
internal network
VPN presence
employee status alone
previous successful action
```

NIST Zero Trust explicitly emphasises per-request, least-privilege access decisions and resource/context sensitivity.

---

# 125. Device and Environmental Signals

Future higher-assurance administrative policy MAY consider:

```text
device assurance
network risk
session age
authentication age
location anomaly
known security incident
```

These signals SHALL be additional policy inputs.

They SHALL NOT replace explicit administrative grants.

---

# 126. Failure Semantics

If CP cannot establish:

```text
principal
permission
scope
grant validity
delegation validity
required approval
authentication assurance
```

administrative access SHALL fail closed.

No default administrator SHALL be guessed.

---

# 127. Degraded Dependency Behaviour

If IAM authentication validation is unavailable:

```text
new privileged administrative actions
```

SHALL fail safely.

If the CP cannot load current AdministrativeGrant state:

```text
privileged mutation
```

SHALL NOT proceed based solely on stale frontend state.

---

# 128. Bootstrap Administration

A new platform requires an initial authority bootstrap.

Bootstrap SHALL be treated separately from routine administration.

Initial platform administrative authority MAY be established through controlled deployment/configuration procedures.

Once ordinary governance is functional:

```text
routine administration
```

SHALL use normal AdministrativeGrant workflows.

---

# 129. Bootstrap Credentials

Bootstrap secrets or accounts SHALL NOT become permanent ordinary administration mechanisms.

Bootstrap procedures SHOULD include:

```text
controlled creation
strong authentication
limited personnel
audit
rotation/retirement
documented transition to normal governance
```

---

# 130. Contract Ownership

Canonical definitions for:

```text
AdministrativePermission
AdministrativeScope
AdministrativeGrant
Delegation
AdministrativeDecision
AdministrativeProfile
SupportAccessSession
```

SHOULD be standardised through:

```text
baobab-platform/shared
```

where cross-repository interoperability requires them.

---

# 131. Control Plane Runtime Ownership

Persistence and policy evaluation of Baobab Control Plane administrative authority SHALL remain:

```text
baobab-platform/baobab-cp
```

`shared` defines contracts.

It does not own runtime decisions.

---

# 132. Conceptual Data Relationship

```text
CanonicalIdentity
       │
       │ 1..*
       ▼
AdministrativeGrant
       │
       ├────────────► AdministrativePermission
       │
       ├────────────► AdministrativeScope
       │
       ├────────────► ApprovalReference
       │
       └────────────► DelegationSource?
                             │
                             ▼
                    AdministrativeGrant
```

---

# 133. Scope Relationship Diagram

```text
                           PLATFORM
                              │
                 ┌────────────┴────────────┐
                 │                         │
          PlatformAccount            Platform Scope
                 │
          ┌──────┴──────┐
          ▼             ▼
    Organisation    Organisation
          │
      ┌───┴────┐
      ▼        ▼
   Tenant    Tenant
      │
   ┌──┴─────┐
   ▼        ▼
 Market   Estate
```

This diagram depicts possible administrative scope.

It does NOT redefine canonical entity relationships.

---

# 134. Example — External Customer

```text
ACME HOLDINGS
     │
     ├── ACME FOODS
     │      ├── ZA Tenant
     │      └── UG Tenant
     │
     └── ACME LOGISTICS
```

Administrators:

```text
Alice:
Group Viewer
ACME Holdings

Bob:
Organisation Administrator
ACME Foods

Carol:
Tenant Administrator
ACME Foods / UG Tenant

David:
Market Administrator
ACME Foods / Uganda
```

These are separate authority grants.

---

# 135. Example — Nabhold

The same generic architecture applies to:

```text
NABHOLD GROUP AFRICA
       │
       ├── ZuriBeans
       ├── Thamani Global
       └── Equator & Estate Co.
```

There SHALL NOT be a special Nabhold-only administration architecture.

Example:

```text
Group executive:
cross-group read visibility

ZuriBeans platform administrator:
ZuriBeans only

Thamani administrator:
Thamani only
```

A shared parent does not grant cross-subsidiary write authority.

---

# 136. Example — Executive Read Visibility

An executive MAY receive:

```text
organisation.view
tenant.view
readiness.view
audit.view-summary
```

across a group.

That does not imply:

```text
tenant.decommission
administrator.grant
security.residency.manage
```

---

# 137. Example — Temporary Consultant

```text
Principal:
Consultant A

Permission:
digital-estate.manage

Scope:
ZuriBeans Supplier Portal

Valid:
2026-10-01 → 2026-10-21

Delegation:
none

Environment:
staging
```

The consultant SHALL not administer production or another estate.

---

# 138. Example — Support Incident

```text
Support Engineer
       │
       ▼
CASE-8721
       │
       ▼
Read-only SupportAccessSession
       │
       ▼
ACME Uganda
       │
       ▼
diagnostics/readiness only
```

At expiry:

```text
access ends automatically
```

---

# 139. Example — Privilege Escalation Attempt

Suppose Bob has:

```text
tenant.view
tenant.manage-low-risk
```

for:

```text
Tenant T1
```

Bob attempts:

```text
grant self tenant.decommission
```

Evaluation:

```text
requested permission exceeds delegable authority
        │
        ▼
DENY
```

Audit reason:

```text
PRIVILEGE_ESCALATION_PREVENTED
```

---

# 140. Example — SoD Failure

Alice submits:

```text
Production tenant decommission request
```

and attempts to approve it.

Policy:

```text
minimum_distinct_actors = 2
```

Result:

```text
SEPARATION_OF_DUTIES_VIOLATION
```

The request remains pending independent approval.

---

# 141. Implementation Programme

Implementation SHALL proceed incrementally.

---

## Gate ADA-00 — Existing Authority Inventory

Audit:

```text
baobab-cp
baobab-iam
shared
CP administrative APIs
Keycloak roles
current tenant memberships
current platform-admin mechanisms
existing onboarding roles
```

Classify:

```text
KEEP
REMODEL
REPLACE
REMOVE
```

No blind migration.

---

## Gate ADA-01 — Canonical Contracts

Define in `shared` as required:

```text
AdministrativePermission
AdministrativeScope
AdministrativeGrant
AdministrativeDecision
Delegation
AdministrativeProfile
```

Include validation and versioning.

---

## Gate ADA-02 — Persistence Model

Implement CP persistence for:

```text
administrative grants
grant lifecycle
scope
validity
delegation provenance
audit references
```

with tenant/organisation-safe queries.

---

## Gate ADA-03 — Policy Evaluation

Implement:

```text
permission matching
scope matching
grant lifecycle
time validity
deny-by-default
risk classification
delegation validation
```

with comprehensive negative tests.

---

## Gate ADA-04 — IAM Integration

Integrate:

```text
canonical identity
authentication assurance
session state
OIDC client/audience
```

without moving CP authorization into Keycloak.

---

## Gate ADA-05 — Delegation

Implement:

```text
delegation boundaries
scope subset validation
permission subset validation
duration validation
depth validation
source-grant dependency
revocation propagation
```

---

## Gate ADA-06 — Separation of Duties

Implement policy support for:

```text
incompatible actions
independent approvers
minimum actor count
self-approval prevention
```

in preparation for ADR-BCP-021.

---

## Gate ADA-07 — Time-Bound and JIT Access

Introduce:

```text
validity windows
automatic expiry
temporary grants
elevated access indication
```

before full break-glass.

---

## Gate ADA-08 — Support Access

Implement controlled:

```text
SupportAccessSession
reason
case reference
scope
duration
visibility
audit
```

read-only first.

---

## Gate ADA-09 — Break-Glass

Implement emergency administrative access with:

```text
strong assurance
scope
reason
expiry
alerts
audit
post-review
```

and test failure scenarios.

---

## Gate ADA-10 — CP Console Integration

Expose:

```text
People & Access
administrative authority
delegation
expiry
scope
support access
audit
```

through ADR-BCP-019's Console.

Frontend SHALL consume backend authority.

It SHALL not calculate it independently.

---

## Gate ADA-11 — Access Review

Implement:

```text
grant inventory
expiring grants
review workflow
recertification
revocation
```

for privileged authority.

---

## Gate ADA-12 — Hardening

Complete:

```text
cross-tenant privilege tests
scope escalation tests
delegation-cycle tests
revocation tests
JIT expiry tests
SoD tests
break-glass tests
audit completeness
concurrency tests
```

before declaring the model production ready.

---

# 142. Required Security Tests

At minimum:

| Test | Expected |
|---|---|
| User without admin grant calls admin endpoint | DENY |
| Organisation A admin requests Organisation B | DENY |
| Tenant admin requests parent organisation admin | DENY |
| Delegator grants greater permission than owned | DENY |
| Delegator grants wider scope than owned | DENY |
| Expired grant used | DENY |
| Revoked grant used | DENY |
| Source grant revoked | Dependent delegation ineffective |
| Self-approval when SoD required | DENY |
| Low assurance for critical action | STEP_UP_REQUIRED |
| Dynamic group expansion | Impact/governance policy applied |
| Support session expired | DENY |
| JIT period expired | DENY |
| Break-glass action performed | Enhanced audit generated |
| Stale frontend privilege | Backend denies |
| Modified organisation ID in request | Backend scope validation denies |

---

# 143. Migration From Broad Existing Roles

Existing roles such as:

```text
platform-admin
control-plane-admin
```

SHALL NOT necessarily disappear immediately.

Migration SHOULD follow:

```text
Inventory
   │
   ▼
Map Existing Role
   │
   ▼
Derive Explicit Permissions
   │
   ▼
Attach Explicit Scope
   │
   ▼
Shadow Evaluate
   │
   ▼
Compare Decisions
   │
   ▼
Enforce New Model
   │
   ▼
Retire Broad Role
```

A flag-day privilege migration SHOULD be avoided unless simplicity and testing make it demonstrably safer.

---

# 144. Shadow Evaluation

During migration, CP MAY evaluate:

```text
legacy decision
```

and:

```text
new AdministrativeGrant decision
```

in parallel.

Differences SHALL be observable but SHALL not silently broaden access.

The migration objective is:

```text
equal or narrower justified authority
```

not accidental expansion.

---

# 145. Alternatives Considered

## Alternative A — Store Everything as Keycloak Roles

**Rejected.**

This would create:

```text
role explosion
stale token authority
duplication of CP context
complex organisation/tenant scope encoding
cross-system semantic coupling
```

Keycloak remains authentication/IAM authority.

---

## Alternative B — Single Global Platform Administrator

**Rejected.**

Too broad for:

```text
multi-tenant SaaS
external customers
group structures
delegated customer administration
support access
future partners
```

---

## Alternative C — Organisation Membership Equals Administration

**Rejected.**

Most organisation members are not administrators.

Membership expresses affiliation.

Administration expresses authority.

---

## Alternative D — Corporate Parent Automatically Administers Subsidiaries

**Rejected.**

Corporate ownership is not authorization.

It would create dangerous automatic privilege expansion.

---

## Alternative E — Pure RBAC

**Rejected as insufficient by itself.**

Named profiles remain useful.

However Baobab requires contextual dimensions including:

```text
organisation
tenant
market
estate
environment
time
delegation
risk
```

Therefore the model is:

```text
permission/profile
+
scope
+
conditions
+
policy
```

rather than role name alone.

---

## Alternative F — Frontend-Enforced Administration

**Rejected.**

Frontend controls improve usability.

They cannot be trusted as authorization boundaries.

---

# 146. Positive Consequences

This decision provides:

```text
least-privileged administration
customer self-administration
multi-organisation delegation
corporate-group administration
time-bounded privilege
future JIT access
managed-service delegation
support governance
break-glass governance
separation of duties
better auditability
clean revocation
cross-tenant safety
```

---

# 147. Negative Consequences

The architecture introduces additional complexity:

```text
grant persistence
policy evaluation
delegation graphs
scope matching
access review
JIT lifecycle
approval integration
audit requirements
```

This complexity is accepted because the alternative is hidden and uncontrolled complexity distributed across:

```text
Keycloak roles
frontend conditionals
database flags
manual procedures
provider-specific administrator models
```

Explicit complexity is preferable to implicit privilege.

---

# 148. Relationship With ADR-BCP-021

This ADR answers:

> **Who has authority to request, review, approve and execute administrative actions?**

ADR-BCP-021 SHALL answer:

> **How is a proposed platform change represented, analysed, approved and executed safely?**

Conceptually:

```text
ADR-BCP-020
WHO MAY ACT?
      │
      ▼
ADR-BCP-021
HOW IS CHANGE GOVERNED?
```

The two SHALL remain complementary.

---

# 149. Relationship With ADR-BCP-019

ADR-BCP-019 defines the Control Plane Console.

This ADR defines the authority that drives its interface.

```text
AdministrativeGrant
        │
        ▼
CP Authorization
        │
        ▼
CP Console
        │
        ├── navigation
        ├── available actions
        ├── scope display
        ├── delegation controls
        └── elevated-access indication
```

The Console remains a presentation layer.

---

# 150. Relationship With ADR-BCP-018

ADR-BCP-018 defines:

```text
Organisation
CorporateGroup
PlatformAccount
Tenant
```

This ADR explicitly ensures those relationships do not become accidental authorization.

Instead:

```text
Organisational Structure
        │
        ▼
possible AdministrativeScope

PLUS

explicit AdministrativeGrant
        │
        ▼
Administrative Authority
```

---

# 151. Relationship With IAM

The final boundary is:

```text
                  BAOBAB IAM
                      │
              identity / assurance
                      │
                      ▼
             Canonical Principal
                      │
                      ▼
            BAOBAB CONTROL PLANE
                      │
        AdministrativeGrant + Policy
                      │
                      ▼
          Administrative Decision
                      │
                      ▼
                CP COMMAND
```

IAM establishes the actor.

CP establishes administrative authority.

---

# 152. Final Architectural Invariants

The following are non-negotiable:

```text
Identity
    != Administrative Authority

Authentication
    != Authorization

Organisation Membership
    != Administration

Corporate Ownership
    != Administration

PlatformAccount
    != Administration

Tenant Membership
    != Tenant Administration

Executive Position
    != Superuser

Role
    != Scope

Delegation
    cannot expand privilege

Corporate hierarchy
    cannot silently expand privilege

Support Access
    != Customer Identity

Break-Glass
    != Permanent Superuser

IAM Admin
    != CP Admin

CP Admin
    != Domain Admin

Administrative Grant
    must be revocable

Administrative Grant
    must be auditable

Administrative Grant
    must have explicit scope

Critical privilege
    must support stronger governance

Frontend visibility
    != authorization
```

---

# 153. Final Decision

Baobab SHALL establish **Administrative Authority** as a first-class Control Plane domain concern.

A person's ability to administer Baobab SHALL be represented through explicit:

```text
AdministrativePermission
        +
AdministrativeScope
        +
AdministrativeGrant
        +
Conditions
        +
Lifecycle
        +
Delegation Policy
        +
Risk / Approval Policy
```

rather than relying on broad roles or IAM membership alone.

The architecture SHALL support:

```text
platform administration
organisation administration
corporate-group administration
tenant administration
market-scoped administration
Digital Estate administration
security administration
audit access
delegated administration
managed-service administration
time-bound access
Just-in-Time privilege
support access
break-glass access
separation of duties
access review
```

while preserving the existing authority boundaries among:

```text
Baobab IAM
Baobab Control Plane
Domain Engines
Control Plane Console
```

The strategic outcome is:

> **Baobab can safely give customers, employees, partners and support personnel precisely the administrative authority they require without turning organisation membership, corporate hierarchy or authentication into implicit superuser access.**

And the governing security principle remains:

> **No administrator should possess more authority than necessary, over more resources than necessary, for longer than necessary, and no high-impact authority should exist without an explainable and auditable chain showing why it exists.**