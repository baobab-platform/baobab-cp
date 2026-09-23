# ADR-BCP-021 — Changeset, Impact Analysis, Approval and Controlled Mutation Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-23  
**Decision Owners:** Baobab Platform Architecture / Security / Platform Operations  
**Primary Repository:** `baobab-platform/baobab-cp`  
**Runtime Authority:** Baobab Control Plane  
**Canonical Contract Authority:** `baobab-platform/shared`  
**Identity / Authentication Authority:** `baobab-platform/baobab-iam`  
**Administrative Authority:** ADR-BCP-020  
**Administrative Human Interface:** ADR-BCP-019 — Baobab Control Plane Console  
**Decision Type:** Foundational platform change-governance, impact-analysis, approval, execution, recovery and reconciliation architecture

**Depends On / Reconciles With:**

- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
- ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
- ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model
- ADR-BCP-006 — Capability Provider Lifecycle, Engine Topology, Health, Failover and Migration Model
- ADR-BCP-007 — Control Plane APIs, Capability Resolution Contracts, Caching, Resolution Assertions and Service-to-Service Consumption Model
- ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model
- ADR-BCP-009 — Capability-Centric Security, Isolation, Residency, Revocation and Failure Semantics
- ADR-BCP-010 — Modular Control Plane Architecture, Governance Boundaries and Evolution Model
- ADR-BCP-011 — Market Participation, Trade Lanes and Cross-Market Trading Model
- ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model
- ADR-BCP-013 — Canonical Inventory Ownership, Custody, Location and In-Transit Model
- ADR-BCP-014 — Canonical Counterparty Identity, Roles and Relationships Model
- ADR-BCP-016 — Buyer Organisation Canonical Entity Registration and OrganisationID Context Resolution
- ADR-BCP-017 — Organisation Admission, Subscription Classification and Tenant Onboarding Lifecycle Model
- ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account and Tenant Relationship Model
- ADR-BCP-019 — Control Plane Administrative Frontend, Organisation Onboarding Experience and Repository Composition Model
- ADR-BCP-020 — Administrative Authority, Delegated Administration, Privileged Access and Separation-of-Duties Model
- `BCP-TS-ONBOARDING-001` — Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification
- Applicable contracts and canonical schemas in `baobab-platform/shared`
- Applicable Baobab IAM ADRs and security controls

**External Validation References:**

- NIST SP 800-53 Rev. 5 — CM-3 Configuration Change Control
- NIST SP 800-53 Rev. 5 — CM-4 Impact Analyses
- NIST SP 800-53 Rev. 5 — CM-5 Access Restrictions for Change
- NIST SP 800-128 — Guide for Security-Focused Configuration Management
- RFC 9110 — HTTP Semantics and conditional requests
- OWASP ASVS 5.0 — Authorization and high-value business-operation controls
- Terraform plan/apply model — reviewed executable plans
- Saga / compensating-transaction patterns for distributed operations
- ADR-BCP-008 desired-state, reconciliation and readiness model

---

# 1. Executive Decision

Baobab SHALL establish a first-class **Changeset and Controlled Mutation Model** for consequential Control Plane state changes.

A consequential platform change SHALL NOT ordinarily be modelled as:

```text
administrator
     │
     ▼
PATCH resource
     │
     ▼
database changed
```

Instead, the canonical model SHALL be:

```text
Business / Administrative Intent
               │
               ▼
           Changeset
               │
               ▼
            Validate
               │
               ▼
        Impact Analysis
               │
               ▼
              Plan
               │
               ▼
         Risk Evaluation
               │
               ▼
           Approval
               │
               ▼
        Execution Operation
               │
               ▼
         Desired State
               │
               ▼
        Reconciliation
               │
               ▼
           Readiness
               │
               ▼
          Verification
               │
               ▼
            Complete
```

The governing principle is:

> **A consequential Control Plane change SHALL be reviewed as intent, analysed as impact, approved as an exact plan, executed under controlled authority, reconciled against desired state, verified against observed state, and preserved as an auditable historical record.**

A second governing principle is:

> **Approval SHALL attach to the exact material plan being executed, not merely to a vague request or mutable ticket.**

A third governing principle is:

> **Distributed platform change SHALL not pretend to be one database transaction. Baobab SHALL use idempotent execution, reconciliation, forward recovery and explicit compensation rather than unsafe assumptions of universal atomic rollback.**

A fourth governing principle is:

> **Applied does not mean ready, and ready does not necessarily mean active.**

---

# 2. Why This ADR Is Required

The Control Plane already governs:

```text
organisations
corporate relationships
PlatformAccounts
tenants
markets
Digital Estates
subscriptions
capability grants
capability bindings
providers
engine instances
isolation
residency
administrative authority
provisioning
readiness
reconciliation
```

Many changes to those objects can have consequences beyond one database row.

For example:

```text
Enable ERP for Tenant A
```

may require:

```text
subscription update
capability expansion
grant creation
provider resolution
engine-instance selection
isolation validation
residency validation
IAM configuration
provider provisioning
mapping creation
reconciliation
readiness verification
```

Likewise:

```text
Add Uganda market
```

may affect:

```text
MarketParticipation
LegalEntity applicability
Digital Estates
currency context
provider eligibility
residency
capability bindings
regional topology
```

Similarly:

```text
Move ACME Uganda under ACME Holdings
```

may affect:

```text
corporate structure
group reporting
dynamic administrative scope
PlatformAccount relationships
future delegated administration
```

A generic CRUD interface is therefore inadequate.

---

# 3. Security and Change-Control Basis

NIST SP 800-128 describes configuration change control as a systematic process covering:

```text
request
record
impact analysis
testing
approval
implementation
verification
```

rather than treating implementation as the first stage.

NIST SP 800-53 CM-4 similarly requires analysing proposed changes for security and privacy effects before implementation, while CM-5 requires restrictions around who may initiate and apply changes.

Baobab SHALL adapt these principles to a SaaS Control Plane.

---

# 4. Conceptual Separation

The following concepts SHALL remain distinct:

```text
Intent
    != Changeset

Changeset
    != Plan

Plan
    != Approval

Approval
    != Execution

Execution
    != Reconciliation

Reconciliation
    != Readiness

Readiness
    != Activation

Rollback
    != Compensation

Failure
    != Partial Application

Audit
    != Operational Log
```

---

# 5. Core Change Objects

The target domain SHALL distinguish at least:

```text
ChangeIntent
Changeset
ImpactAnalysis
ChangePlan
ApprovalDecision
ExecutionOperation
ExecutionStep
CompensationAction
VerificationResult
ChangeOutcome
```

Not every concept must require a separate database table.

Their semantics SHALL remain separate.

---

# 6. ChangeIntent

`ChangeIntent` expresses:

> What outcome does the requesting actor want?

Examples:

```text
Enable ERP for ZuriBeans South Africa.

Add Uganda as an operating market.

Create production tenant for ACME Foods.

Change tenant isolation to dedicated database.

Suspend Thamani production tenant.

Move organisation into another corporate group.

Grant a new organisation administrator.

Migrate Trade capability from Provider A to Provider B.
```

Intent SHOULD be expressed primarily in canonical business/platform concepts.

It SHALL NOT ordinarily contain low-level provider commands.

---

# 7. Changeset

A `Changeset` SHALL be the canonical governed unit representing one coherent proposed change to Control Plane-owned desired state.

Conceptually:

```text
Changeset
├── id
├── reference
├── type
├── title
├── description
├── requested_by
├── requested_at
├── source
├── target_scope
├── base_revision
├── desired_change
├── reason
├── business_justification?
├── risk_class
├── lifecycle_state
├── current_plan_id?
├── correlation_id
├── created_at
├── updated_at
└── version
```

---

# 8. Changeset Is Not a Ticket

A Changeset SHALL NOT merely be:

```text
free-text Jira-style ticket
```

It must be sufficiently structured that CP can determine:

```text
target resources
requested desired state
authorization scope
conflicts
impact
plan
execution
verification
```

Free-text reasoning MAY supplement structured data.

It SHALL not replace it.

---

# 9. Changeset as Governance Boundary

The Changeset SHALL become the boundary for:

```text
authorization
impact analysis
approval
execution
audit
correlation
recovery
```

Every high-impact administrative mutation SHOULD trace back to a Changeset.

---

# 10. Routine Low-Risk Exceptions

Not every harmless metadata edit requires human approval.

Baobab MAY classify some operations as:

```text
PRE_APPROVED
```

or:

```text
LOW_RISK_SELF_SERVICE
```

Examples might include:

```text
changing a non-authoritative display label
updating a contact preference
adding a harmless description
```

provided policy explicitly permits this.

Such actions SHALL still be:

```text
authorized
validated
audited
version-aware
```

The absence of a human approval requirement does not remove change governance.

---

# 11. Change Sources

A Changeset MAY originate from:

```text
CP Console
approved external API
system automation
migration tooling
reconciliation remediation
security response
platform operator
customer organisation administrator
future partner integration
```

The source SHALL be recorded.

Conceptually:

```text
source =
HUMAN_CONSOLE
API
AUTOMATION
MIGRATION
RECONCILIATION
SECURITY_RESPONSE
```

---

# 12. Human and Machine Changes

Machine-initiated changes SHALL NOT bypass governance merely because no human clicked a button.

Automation SHALL operate under:

```text
workload identity
explicit administrative/service authority
defined scope
change policy
audit
```

---

# 13. Changeset Types

Initial conceptual types MAY include:

```text
ONBOARD
MODIFY
EXPAND
REDUCE
SUSPEND
REINSTATE
MIGRATE
DECOMMISSION
SECURITY_REMEDIATION
EMERGENCY
RECONCILIATION_REPAIR
```

Exact canonical vocabulary SHALL be defined during contract design.

---

# 14. Initial Onboarding Is a Changeset

ADR-BCP-019 established the principle:

```text
Initial Onboarding
      =
first major Changeset
```

This ADR makes that normative.

The same governance model SHALL subsequently support:

```text
add market
add product
remove capability
change isolation
register estate
change provider
suspend tenant
offboard tenant
```

---

# 15. Changeset Lifecycle

The canonical lifecycle SHOULD support:

```text
DRAFT
  │
  ▼
VALIDATING
  │
  ├────────► INVALID
  │
  ▼
PLANNING
  │
  ├────────► BLOCKED
  │
  ▼
PLANNED
  │
  ▼
AWAITING_APPROVAL
  │
  ├────────► REJECTED
  │
  ├────────► CHANGES_REQUESTED
  │
  ▼
APPROVED
  │
  ▼
SCHEDULED
  │
  ▼
APPLYING
  │
  ├────────► FAILED
  │
  ├────────► PARTIALLY_APPLIED
  │
  ▼
VERIFYING
  │
  ├────────► VERIFICATION_FAILED
  │
  ▼
COMPLETED
```

Additional terminal states:

```text
CANCELLED
SUPERSEDED
EXPIRED
COMPENSATED
```

---

# 16. DRAFT

`DRAFT` represents intent still being prepared.

Draft changes:

```text
may be edited
may be deleted according to retention policy
are not executable
are not approved
```

---

# 17. VALIDATING

Validation SHALL determine whether the requested intent is structurally and semantically acceptable.

Validation includes:

```text
schema validity
resource existence
canonical identifier validity
actor authority
scope validity
lifecycle compatibility
contract compatibility
required fields
policy constraints
```

---

# 18. INVALID

An invalid Changeset SHALL not proceed to planning.

It SHOULD provide structured reasons.

Example:

```text
TARGET_TENANT_NOT_FOUND
MARKET_NOT_REGISTERED
SUBSCRIPTION_COMBINATION_INVALID
RESIDENCY_REQUIREMENT_UNSATISFIABLE
```

---

# 19. PLANNING

Planning derives the concrete Control Plane changes required to satisfy the intent.

Planning SHALL be side-effect free.

---

# 20. Side-Effect-Free Planning

A planner MAY:

```text
read state
resolve providers
calculate dependencies
perform compatibility checks
perform impact analysis
query health/readiness
```

but SHALL NOT:

```text
create production resources
modify grants
change IAM membership
mutate providers
change database state beyond planning metadata
```

---

# 21. ChangePlan

`ChangePlan` SHALL describe the exact material operations currently proposed.

Conceptually:

```text
ChangePlan
├── id
├── changeset_id
├── plan_version
├── plan_digest
├── base_revision
├── generated_at
├── generated_by
├── expires_at?
├── risk_class
├── affected_resources[]
├── steps[]
├── dependencies[]
├── preconditions[]
├── impact_analysis_id
├── rollout_strategy?
├── verification_strategy
├── compensation_strategy
├── irreversible_steps[]
└── status
```

---

# 22. Plan as Reviewed Artifact

The plan SHALL be treated as an immutable review artifact once submitted for approval.

Terraform's saved-plan workflow demonstrates the useful principle that the reviewed plan can be the same plan subsequently executed rather than silently recomputing materially different actions during apply.

Baobab SHALL adopt the architectural principle, not Terraform's implementation format.

---

# 23. Plan Digest

Every approvable plan SHALL have a stable digest calculated from its material execution semantics.

Conceptually:

```text
plan_digest =
hash(
    plan_version
    + material steps
    + affected scope
    + critical preconditions
    + rollout strategy
)
```

The exact canonicalisation and hashing specification SHALL be defined by implementation contract.

---

# 24. Approval Binds to Plan Digest

An approval SHALL reference:

```text
changeset_id
plan_id
plan_version
plan_digest
```

Therefore:

```text
approved Changeset
```

does not mean:

```text
any future plan for that Changeset is approved.
```

---

# 25. Material Plan Mutation

If an approved plan changes materially:

```text
Plan v1
   │
   X
   ▼
Plan v2
```

existing approvals SHALL become invalid for v2.

The system SHALL require:

```text
re-analysis
and
re-approval
```

according to policy.

---

# 26. Non-Material Presentation Changes

Changes that affect only:

```text
formatting
UI presentation
non-semantic display description
```

MAY avoid invalidating plan approval.

Materiality SHALL be determined by canonical plan semantics, not frontend appearance.

---

# 27. No Mutable Approved Plan

The following is prohibited:

```text
approve Plan A
      │
      ▼
edit Plan A silently
      │
      ▼
execute modified Plan A
```

Approved plans SHALL be immutable.

Create a new version instead.

---

# 28. Plan Expiry

Plans SHOULD be able to expire.

Reasons include:

```text
platform state changed
provider topology changed
security policy changed
market context changed
another conflicting change completed
```

High-risk plans SHOULD generally have shorter validity windows.

---

# 29. Stale Plan

A plan SHALL be considered stale when its assumptions no longer safely match authoritative current state.

Examples:

```text
base resource version changed
tenant suspended
provider became unavailable
isolation policy changed
administrative authority revoked
corporate relationship changed
```

---

# 30. Stale Plan Handling

A stale plan SHALL NOT simply execute because it was once approved.

Result:

```text
PLAN_STALE
```

The system SHALL require:

```text
re-plan
re-evaluate impact
and potentially
re-approve
```

---

# 31. Base Revision

A plan SHALL record the authoritative revision or version assumptions used during planning.

This MAY include:

```text
tenant version
organisation version
subscription version
binding version
administrative-policy version
```

as relevant.

---

# 32. Optimistic Concurrency

Mutation APIs SHALL use optimistic concurrency where appropriate.

RFC 9110 defines `If-Match` specifically to prevent state-changing methods from overwriting a representation that has changed since the caller observed it.

Baobab MAY use:

```text
ETag / If-Match
resource version
expected_version
base_revision
```

according to API design.

---

# 33. Lost Update Prevention

The following scenario SHALL fail safely:

```text
Admin A reads Tenant v7

Admin B updates Tenant to v8

Admin A submits change based on v7
```

Result:

```text
CONFLICT
```

rather than silently replacing v8.

---

# 34. ImpactAnalysis

Every non-trivial Changeset SHALL produce an explicit `ImpactAnalysis`.

Conceptually:

```text
ImpactAnalysis
├── id
├── changeset_id
├── plan_id
├── analysed_at
├── risk_class
├── affected_resources[]
├── security_impacts[]
├── tenancy_impacts[]
├── organisation_impacts[]
├── capability_impacts[]
├── provider_impacts[]
├── operational_impacts[]
├── availability_impacts[]
├── residency_impacts[]
├── isolation_impacts[]
├── identity_impacts[]
├── data_impacts[]
├── customer_impacts[]
├── compliance_impacts[]
├── irreversible_effects[]
├── rollback_complexity
├── blast_radius
└── recommendations[]
```

---

# 35. Impact Dimensions

Impact analysis SHOULD consider at least:

| Dimension | Example Question |
|---|---|
| Organisation | Does corporate structure change? |
| Tenant | Which tenant boundaries change? |
| Platform Account | Does commercial/admin grouping change? |
| Legal Entity | Does legal representation change? |
| Market | Which operating markets change? |
| Digital Estate | Which portals/sites are affected? |
| Subscription | Which products change? |
| Capability | Which grants change? |
| Provider | Which provider or engine instance changes? |
| Security | Does privilege or exposure increase? |
| Isolation | Does technical isolation weaken/strengthen? |
| Residency | Does storage/processing location change? |
| IAM | Are memberships or projections affected? |
| Administration | Does administrative scope expand? |
| Availability | Will service be interrupted? |
| Data | Is migration/transformation required? |
| Compliance | Are policy obligations affected? |
| Cost | Does material platform consumption change? |
| Customer | Is customer-facing behaviour affected? |
| Recovery | Can the change be compensated? |

---

# 36. Security Impact Analysis

Changes affecting:

```text
authorization
administrative authority
isolation
residency
identity
network boundary
provider security
```

SHALL receive explicit security impact analysis.

NIST CM-4 requires analysing changes before implementation to determine security/privacy impacts and verifying after implementation that security requirements remain satisfied.

---

# 37. Administrative Scope Impact

ADR-BCP-020 introduces dynamic administrative scopes.

Impact analysis SHALL therefore identify whether a change:

```text
expands administrator authority
reduces administrator authority
invalidates delegation
creates new group descendants
changes tenant ownership context
```

---

# 38. Example — Corporate Group Change

Proposed:

```text
Add NewCo as subsidiary of ACME Holdings
```

Impact analysis might report:

```text
Corporate relationships:
+1

Dynamic group administrators affected:
3

Administrators gaining NewCo visibility:
2

Administrators gaining NewCo mutation authority:
1

Static-scope administrators:
unchanged
```

This is a security impact.

---

# 39. Isolation Impact

Changing:

```text
DEDICATED_DATABASE
```

to:

```text
SHARED_LOGICAL
```

SHALL be classified as a potentially significant security change.

The impact analysis SHALL identify:

```text
affected data
affected provider
contractual restrictions
residency implications
customer commitments
```

---

# 40. Residency Impact

A change SHALL NOT silently move processing or storage into a new region.

Impact analysis SHALL compare:

```text
current residency policy
proposed topology
provider processing regions
backup/replication constraints
```

---

# 41. Availability Impact

The plan SHOULD classify expected operational effect:

```text
NO_INTERRUPTION
DEGRADED_SERVICE
PARTIAL_OUTAGE
MAINTENANCE_REQUIRED
FULL_OUTAGE
UNKNOWN
```

---

# 42. Blast Radius

Every material Changeset SHOULD calculate or classify blast radius.

Possible dimensions:

```text
one resource
one tenant
one organisation
one corporate group
one market
multiple customers
platform-wide
```

---

# 43. Risk Classification

ADR-BCP-020 defines:

```text
LOW
MODERATE
HIGH
CRITICAL
```

The same vocabulary SHOULD govern Changesets.

---

# 44. Risk Inputs

Risk classification SHOULD consider:

```text
blast radius
security impact
isolation impact
residency impact
irreversibility
customer-facing impact
production scope
data migration
privilege expansion
provider migration
historical failure rate
automation maturity
```

---

# 45. Example Risk Classification

| Change | Illustrative Risk |
|---|---|
| Change display label | Low |
| Add staging Digital Estate | Moderate |
| Add production market | High |
| Activate new production tenant | High |
| Change provider for critical capability | High |
| Isolation downgrade | Critical |
| Residency relaxation | Critical |
| Decommission production tenant | Critical |
| Platform-wide binding change | Critical |

The matrix SHALL remain configurable policy.

---

# 46. Risk Is Not User-Selected

A requester MAY provide context.

The requester SHALL NOT be authoritative for:

```text
risk = LOW
```

CP policy SHALL calculate or validate the classification.

---

# 47. Approval Requirement

The approval requirement SHALL derive from:

```text
change type
risk
scope
environment
affected resources
administrative policy
separation-of-duties policy
```

---

# 48. Approval Policy

Conceptually:

```text
ApprovalPolicy
├── id
├── applies_to
├── risk_threshold
├── required_permissions[]
├── required_approvers
├── distinct_actor_requirement
├── required_assurance
├── required_specialists[]
└── conditions[]
```

---

# 49. Approval Outcomes

An approver SHALL be able to issue:

```text
APPROVE
REJECT
REQUEST_CHANGES
```

A future policy MAY support:

```text
ABSTAIN
```

where committee-style governance is introduced.

---

# 50. Approval Is a Decision Record

Conceptually:

```text
ApprovalDecision
├── id
├── changeset_id
├── plan_id
├── plan_digest
├── approver_principal_id
├── authority_reference
├── decision
├── reason?
├── authentication_assurance
├── decided_at
└── correlation_id
```

---

# 51. Approval Authority

Approval SHALL be evaluated using ADR-BCP-020.

The existence of an approval UI button SHALL not establish authority.

---

# 52. Separation of Duties

Where policy requires:

```text
Requester != Approver
```

the backend SHALL enforce it.

The frontend MAY explain the rule.

It SHALL not be the enforcement boundary.

---

# 53. Multiple Approvals

High-risk changes MAY require:

```text
2 distinct approvers
```

or distinct expertise such as:

```text
Platform Approver
+
Security Approver
```

---

# 54. Approval Quorum

Future policy MAY require quorum.

Example:

```text
2 of 3 authorised Platform Approvers
```

The architecture SHALL not assume all approval is single-user.

---

# 55. Conditional Approval

An approval SHALL NOT rely on an unstructured condition such as:

```text
Approved if you remember to change X.
```

If a condition changes execution semantics, it SHALL be represented in the plan.

The plan SHALL then be regenerated and approved.

---

# 56. Approval Expiry

Approvals MAY expire.

Expiry SHOULD be supported for:

```text
critical changes
plans with volatile assumptions
emergency changes
provider migrations
```

---

# 57. Revoked Approver Authority

If an approver's administrative authority is revoked after approval but before execution, policy SHALL determine whether the approval remains valid.

For high-risk changes, Baobab SHOULD ordinarily invalidate or re-evaluate the approval before apply.

---

# 58. Changeset Modification After Rejection

A rejected or change-requested Changeset MAY be revised.

Revision SHALL produce a new plan version.

Previous rejection/approval history SHALL remain immutable.

---

# 59. ExecutionOperation

Applying an approved plan SHALL create an `ExecutionOperation`.

Conceptually:

```text
ExecutionOperation
├── id
├── changeset_id
├── plan_id
├── plan_digest
├── requested_by
├── started_by
├── started_at
├── status
├── current_step
├── execution_attempt
├── execution_context
├── correlation_id
├── completed_at?
└── outcome?
```

---

# 60. ExecutionOperation Is Long-Running

Execution SHALL be modelled as asynchronous.

The caller SHOULD receive:

```text
operation_id
```

instead of holding a browser request open across the entire process.

---

# 61. Execution States

Recommended states:

```text
QUEUED
PREPARING
RUNNING
WAITING
VERIFYING
SUCCEEDED
FAILED
PARTIALLY_APPLIED
COMPENSATING
COMPENSATED
COMPENSATION_FAILED
CANCEL_REQUESTED
CANCELLED
BLOCKED
```

---

# 62. Execution Steps

A plan SHALL compile into ordered or dependency-aware execution steps.

Conceptually:

```text
ExecutionStep
├── id
├── operation_id
├── step_type
├── target
├── dependency_ids[]
├── state
├── attempt
├── idempotency_key
├── started_at?
├── completed_at?
├── result_reference?
├── compensation_step?
└── failure_reason?
```

---

# 63. Step Dependency Graph

Execution MAY use a DAG rather than a purely sequential list.

Example:

```text
              Validate Preconditions
                       │
        ┌──────────────┴──────────────┐
        ▼                             ▼
 Configure IAM                 Create Subscription
        │                             │
        └──────────────┬──────────────┘
                       ▼
                Materialise Grants
                       │
             ┌─────────┴─────────┐
             ▼                   ▼
        Bind Trade            Bind ERP
             │                   │
             └─────────┬─────────┘
                       ▼
                  Reconcile
                       │
                       ▼
                   Readiness
```

Independent steps MAY execute in parallel where safe.

---

# 64. No Unsafe Parallelism

Parallelism SHALL be determined from declared dependencies.

The executor SHALL NOT parallelise merely for speed if doing so can violate:

```text
ordering
scope isolation
provider consistency
security invariants
```

---

# 65. Execution Locking

Baobab SHALL prevent unsafe concurrent Changesets over overlapping controlled scope.

This does not require one global platform lock.

---

# 66. Semantic Lock Scope

A Changeset SHOULD define concurrency keys.

Examples:

```text
tenant:T1
organisation:ORG1
binding:CAP-X:T1
market:T1:UG
administrative-grant:AG1
```

---

# 67. Disjoint Changes

Changes with genuinely independent scope MAY proceed concurrently.

Example:

```text
Tenant A add CMS
```

and:

```text
Tenant B update contact metadata
```

need not block one another.

---

# 68. Overlapping Changes

Potentially conflicting changes SHALL:

```text
serialize
reject
or
require re-plan
```

according to policy.

---

# 69. Example Concurrency Conflict

Changeset A:

```text
Change T1 Trade provider:
Provider A → Provider B
```

Changeset B:

```text
Change T1 Trade provider:
Provider A → Provider C
```

These SHALL not execute concurrently.

---

# 70. Execution Preconditions

Immediately before apply, CP SHALL re-evaluate critical preconditions.

Examples:

```text
plan not stale
approvals valid
actor authority valid
tenant state compatible
resource versions match
provider eligible
maintenance window valid
security policy unchanged materially
```

---

# 71. Apply-Time Revalidation

Planning does not guarantee apply-time validity.

Therefore:

```text
PLAN VALID
```

at 10:00 does not necessarily mean:

```text
PLAN SAFE
```

at 16:00.

---

# 72. Exact Plan Execution

The executor SHALL apply the approved plan.

It SHALL NOT silently derive materially new steps at apply time.

If current state requires materially different actions:

```text
PLAN_STALE
```

and re-plan.

---

# 73. Minor Runtime Resolution

This rule does not prohibit execution-time resolution of non-material operational details such as:

```text
connection lease
ephemeral operation ID
retry timing
```

provided those details do not alter the approved change semantics or risk.

---

# 74. Desired State First

For CP-owned configuration, execution SHOULD primarily establish or modify authoritative desired state.

Conceptually:

```text
Approved Plan
      │
      ▼
Commit Desired State
      │
      ▼
Reconciliation
      │
      ▼
Observed State Converges
```

---

# 75. Reconciliation Remains Authoritative for Convergence

The changeset executor SHALL NOT become a second reconciler.

ADR-BCP-008 remains authoritative.

Execution tells the platform:

```text
what should now be true
```

Reconciliation determines:

```text
whether reality matches it.
```

---

# 76. Provider-Side Actions

Some changes require explicit provider operations.

Examples:

```text
provision tenant configuration
establish provider mapping
configure IAM projection
create provider-native organisational object
```

These SHALL occur through governed adapters and orchestration.

The CP Console SHALL not perform them directly.

---

# 77. Distributed Mutation

Cross-system provisioning may involve:

```text
CP
IAM
Trade
ERP
CMS
Pulse
Infrastructure
```

These systems do not share one ACID transaction.

Baobab SHALL therefore not pretend that:

```text
BEGIN DISTRIBUTED TRANSACTION
...
COMMIT EVERYTHING
```

is the platform architecture.

---

# 78. Orchestrated Saga Principle

For complex multi-participant changes, CP orchestration SHOULD follow saga-like principles.

A saga coordinates local transactions and uses retries or compensating actions when a distributed operation fails.

Because the Control Plane already owns provisioning orchestration and desired state, **orchestration** is preferred to uncontrolled choreography for governed platform Changesets.

---

# 79. Why Orchestration

A central Control Plane orchestrator can maintain:

```text
plan
step status
timeouts
retries
compensation
approval provenance
audit
current operation
```

This is preferable to hiding a critical tenant-provisioning workflow across loosely connected event handlers.

---

# 80. Events Still Matter

Orchestration does NOT eliminate events.

Events remain important for:

```text
state propagation
observability
notifications
consumer reactions
audit integration
```

But the canonical control flow for an approved Changeset SHALL remain understandable from CP operation state.

---

# 81. Local Atomicity

Each participant SHOULD commit its own local change atomically where possible.

Example:

```text
CP local transaction:
desired-state row
+
audit record
+
outbox event
```

---

# 82. Transactional Outbox

Where CP must:

```text
change authoritative database state
+
publish canonical event
```

it SHOULD use the transactional outbox architecture already anticipated by the Control Plane design.

This avoids unsafe dual writes.

---

# 83. Idempotency

Every retryable execution step SHALL be idempotent or protected through an equivalent idempotency mechanism.

AWS guidance for distributed sagas likewise identifies idempotency as important because participants may execute more than once after transient failures.

---

# 84. Step Idempotency Key

Conceptually:

```text
idempotency_key =
operation_id
+
step_id
+
plan_version
```

or another canonical collision-resistant representation.

---

# 85. Retry Classes

Failures SHALL be classified.

Example:

```text
TRANSIENT
POLICY
VALIDATION
DEPENDENCY
CONFLICT
PERMANENT
UNKNOWN
```

---

# 86. Transient Failure

Examples:

```text
network timeout
temporary provider unavailable
rate limit
short dependency outage
```

These MAY be automatically retried under bounded policy.

---

# 87. Permanent Failure

Examples:

```text
provider does not support required isolation
requested market prohibited
invalid legal-entity mapping
required resource deleted
```

These SHALL not loop indefinitely.

---

# 88. Retry Policy

Retry behaviour SHALL define:

```text
maximum attempts
backoff
maximum duration
retryable failure classes
```

The policy MAY vary by step type.

---

# 89. Infinite Retry Is Prohibited

A broken configuration SHALL not remain:

```text
retrying forever
```

without becoming visible operationally.

Persistent failure SHALL transition to a state requiring intervention.

---

# 90. Compensation

A `CompensationAction` SHALL represent an intentional operation designed to reduce or reverse the effect of a previously completed execution step.

---

# 91. Compensation Is Not Rollback

The term:

```text
rollback
```

can dangerously imply perfect time reversal.

In distributed systems this is often impossible.

Baobab SHALL distinguish:

```text
Database rollback
```

from:

```text
Compensation
```

and:

```text
Forward recovery
```

---

# 92. Example

Suppose:

```text
1. Subscription created
2. Grants created
3. ERP tenant provisioned
4. IAM projection fails
```

Potential compensation might be:

```text
disable ERP tenant configuration
revoke grants
cancel subscription provisioning
```

rather than pretending those systems participated in one transaction.

---

# 93. Compensation Classification

Each execution step SHOULD declare one of:

```text
COMPENSABLE
RETRYABLE
IRREVERSIBLE
NO_COMPENSATION_REQUIRED
```

---

# 94. Irreversible Steps

Examples MAY include:

```text
destructive external purge
irreversible provider migration action
permanent key destruction
legally mandated deletion
```

Such steps require stronger planning.

---

# 95. Irreversible-Step Visibility

A plan SHALL explicitly identify irreversible effects before approval.

The CP Console SHALL display them prominently.

---

# 96. Pivot / Point of No Return

A complex plan MAY identify a:

```text
pivot point
```

after which backward compensation is no longer safe or meaningful.

Before pivot:

```text
compensation may restore previous desired condition
```

After pivot:

```text
forward recovery becomes preferred
```

This aligns with saga patterns where some steps are compensable while later steps are retryable or irreversible.

---

# 97. Decommissioning Example

A tenant decommission plan might be:

```text
Suspend tenant
      │
      ▼
Revoke runtime access
      │
      ▼
Final export/retention checks
      │
      ▼
Disable provider configuration
      │
      ▼
Retention period
      │
      ▼
Irreversible data deletion
```

The deletion step is materially different from suspension.

---

# 98. Forward Recovery

After a pivot, the preferred strategy MAY be:

```text
retry until desired safe state achieved
```

rather than attempting to reconstruct an obsolete prior state.

---

# 99. Compensation Failure

Compensation itself can fail.

Baobab SHALL model:

```text
COMPENSATION_FAILED
```

as a first-class operational condition.

It SHALL require:

```text
operator visibility
diagnostics
audit
manual remediation or controlled retry
```

---

# 100. No Hidden Partial Failure

A distributed Changeset SHALL never be reported as:

```text
FAILED
```

without distinguishing whether:

```text
nothing changed
```

or:

```text
some changes succeeded
```

---

# 101. Failure Outcomes

Execution SHOULD distinguish:

```text
NO_CHANGE_FAILURE
PARTIAL_FAILURE
VERIFICATION_FAILURE
COMPENSATED_FAILURE
UNCOMPENSATED_FAILURE
```

---

# 102. PARTIALLY_APPLIED

`PARTIALLY_APPLIED` means:

> At least one material planned change occurred, but the complete desired outcome was not achieved.

This SHALL receive elevated operational visibility.

---

# 103. Change Cancellation Before Apply

A Changeset MAY ordinarily be cancelled before execution begins.

Cancellation SHALL preserve:

```text
history
reason
requester
time
```

---

# 104. Cancellation During Execution

Cancellation during execution SHALL NOT mean:

```text
kill process immediately
```

The orchestrator SHALL determine whether a safe cancellation boundary exists.

---

# 105. Cancel Request

During execution:

```text
CANCEL_REQUESTED
```

SHOULD mean:

> stop at the next safe orchestration point if policy permits.

---

# 106. Readiness

Readiness remains governed by ADR-BCP-008.

A successfully executed mutation SHALL NOT automatically imply:

```text
READY
```

---

# 107. Applied vs Reconciled vs Ready

The canonical relationship is:

```text
APPLIED
   │
   ▼
Desired state recorded

RECONCILED
   │
   ▼
Observed state matches desired state

READY
   │
   ▼
Required service can safely operate
```

---

# 108. Ready vs Active

For certain lifecycles:

```text
READY
```

MAY still require:

```text
activation approval
scheduled activation
commercial effective date
```

before:

```text
ACTIVE
```

---

# 109. Verification

After execution and reconciliation, the Changeset SHALL undergo verification according to plan.

---

# 110. Verification Strategy

Conceptually:

```text
VerificationStrategy
├── required_readiness_checks[]
├── security_checks[]
├── contract_checks[]
├── health_checks[]
├── functional_checks[]
├── observation_period?
└── success_criteria[]
```

---

# 111. Security Verification

For high-risk security changes, post-change verification SHOULD ensure that expected controls remain effective.

This reflects NIST guidance that change control includes post-implementation verification of security effects.

---

# 112. Stabilisation Window

Some high-impact changes SHOULD support an observation period.

Example:

```text
Provider migration completed
        │
        ▼
30-minute stability observation
        │
        ├── healthy ───► COMPLETE
        └── degraded ──► RECOVERY ACTION
```

Exact windows are policy/configuration, not ADR constants.

---

# 113. Completion

A Changeset SHALL become `COMPLETED` only when its completion criteria are satisfied.

For a simple change this may mean:

```text
authoritative state changed
```

For a complex change it may mean:

```text
execution succeeded
+
reconciliation converged
+
required readiness achieved
+
verification passed
```

---

# 114. Outcome Record

Conceptually:

```text
ChangeOutcome
├── changeset_id
├── operation_id
├── final_state
├── applied_plan_digest
├── started_at
├── completed_at
├── affected_resources[]
├── verification_result
├── compensation_summary?
├── residual_risks[]
└── correlation_id
```

---

# 115. Rollback to Prior Desired State

Where business intent is to restore a previous desired configuration, the preferred model SHALL ordinarily be:

```text
create new Changeset
```

representing the desired restoration.

---

# 116. No History Rewriting

The following is prohibited:

```text
delete Change 42
because we reversed it
```

Instead:

```text
Change 42
Provider A → Provider B

Change 43
Provider B → Provider A
```

Both remain historically true.

---

# 117. Reversion Changeset

A reversion Changeset SHOULD reference:

```text
reverts_changeset_id
```

or an equivalent relationship.

---

# 118. Change Supersession

A Changeset MAY be superseded by another before execution.

Example:

```text
Change A:
Add Uganda + Kenya

replaced before approval by:

Change B:
Add Uganda only
```

Change A becomes:

```text
SUPERSEDED
```

not deleted.

---

# 119. Plan Reuse

Plans SHALL ordinarily be specific to:

```text
environment
current authoritative state
current topology
current policies
```

Therefore a production plan SHALL not simply be copied from staging and applied unchanged.

---

# 120. Environment Promotion

Baobab MAY support:

```text
promote intent
```

from:

```text
development
→ staging
→ production
```

but each environment SHALL generate its own plan and impact analysis.

---

# 121. Why Plans Are Environment-Specific

Because:

```text
providers differ
resource IDs differ
current state differs
security policy may differ
capacity differs
region differs
```

---

# 122. Change Scheduling

Changes MAY be scheduled.

Conceptually:

```text
Schedule
├── not_before
├── deadline?
├── maintenance_window?
└── blackout_constraints?
```

---

# 123. Scheduling Is Not Approval

A scheduled change still requires whatever approval policy applies.

Similarly:

```text
approved
```

does not mean:

```text
execute immediately
```

---

# 124. Maintenance Windows

High-impact changes MAY require approved maintenance windows.

The plan SHOULD state expected service effect.

---

# 125. Blackout Periods

The platform MAY define periods in which certain changes are prohibited except through emergency policy.

Examples:

```text
financial close
major customer event
critical trading period
platform migration
```

---

# 126. Progressive Rollout

Broad-impact changes SHOULD be capable of progressive rollout.

Conceptually:

```text
Pilot
  │
  ▼
Wave 1
  │
  ▼
Verify
  │
  ▼
Wave 2
  │
  ▼
Verify
  │
  ▼
Full Rollout
```

---

# 127. RolloutStrategy

Conceptually:

```text
RolloutStrategy
├── mode
├── target_groups[]
├── batch_size?
├── pause_between_waves?
├── verification_per_wave
├── stop_conditions[]
└── rollback_or_compensation_policy
```

---

# 128. Rollout Modes

Future canonical modes MAY include:

```text
IMMEDIATE
SCHEDULED
CANARY
BATCHED
MANUAL_WAVES
```

---

# 129. Canary Changes

Canary rollout is most relevant for:

```text
provider migrations
platform-wide capability changes
shared infrastructure configuration
new engine versions
```

rather than ordinary single-tenant metadata changes.

---

# 130. Stop Conditions

A progressive rollout SHALL stop automatically when configured safety criteria fail.

Examples:

```text
readiness degrades
error rate exceeds limit
provider health fails
security verification fails
unexpected drift detected
```

---

# 131. Bulk Changes

Bulk administration SHALL not be implemented as:

```text
browser loops over 5,000 PATCH requests
```

Instead it SHALL produce a governed bulk Changeset.

---

# 132. Bulk Plan

The plan SHALL expose:

```text
number of organisations
number of tenants
number of grants
estimated blast radius
batch strategy
failures
```

---

# 133. Bulk Failure Policy

Bulk execution SHALL define whether failure semantics are:

```text
STOP_ON_FIRST_FAILURE
CONTINUE_INDEPENDENT_TARGETS
BATCH_ATOMIC_WITHIN_SCOPE
```

according to change type.

---

# 134. No Global Atomicity Claim

Even bulk operations SHALL NOT claim global ACID atomicity across independent engines.

---

# 135. Emergency Changes

Baobab SHALL support emergency Changesets.

Emergency does not mean:

```text
untracked
unauthorized
unaudited
```

---

# 136. Emergency Change Flow

Conceptually:

```text
Incident
   │
   ▼
Emergency Change
   │
   ▼
Emergency Authority
   │
   ▼
Reduced Pre-Approval Where Policy Permits
   │
   ▼
Execute
   │
   ▼
Verify
   │
   ▼
Mandatory Retrospective Review
```

---

# 137. Emergency Reason

Emergency execution SHALL require:

```text
incident reference
reason
target
scope
actor
```

---

# 138. Emergency Is Exceptional

Routine operational convenience SHALL NOT be grounds for:

```text
EMERGENCY
```

classification.

Emergency use SHOULD be measured and reviewed.

---

# 139. Break-Glass Integration

ADR-BCP-020 break-glass authority MAY be used for emergency Changesets where ordinary administrative paths are unavailable.

The two concepts remain distinct:

```text
BreakGlass
=
authority mechanism

Emergency Changeset
=
change-governance mechanism
```

---

# 140. Automated Remediation

Reconciliation may discover safe, auto-repairable drift.

Such remediation MAY execute under a pre-authorised policy.

---

# 141. Auto-Repair Changes

Auto-repair SHALL remain:

```text
defined
bounded
audited
idempotent
```

It SHALL NOT become:

```text
reconciler may mutate anything
```

---

# 142. Auto-Repair Policy

Conceptually:

```text
AutoRepairPolicy
├── drift_type
├── permitted_action
├── maximum_scope
├── risk_class
├── required_preconditions
└── enabled
```

---

# 143. Manual Drift Remediation

Drift that is not safe for auto-repair SHALL produce an operational action or Changeset.

---

# 144. Direct Database Mutation

Production changes SHALL NOT ordinarily be performed through:

```text
manual SQL UPDATE
```

against CP authoritative tables.

---

# 145. Exceptional Database Repair

If emergency database repair becomes unavoidable, it SHALL be treated as an exceptional governed operation with:

```text
incident
backup/recovery plan
authorised operator
audit
post-reconciliation
verification
```

It SHALL not become a routine administrative interface.

---

# 146. Direct Provider Mutation

Similarly, platform configuration managed by CP SHALL not ordinarily be changed manually in:

```text
Keycloak
Medusa
iDempiere
Payload
provider infrastructure
```

when CP owns the corresponding desired configuration.

---

# 147. Out-of-Band Change

If an authorised external operator changes managed provider state directly:

```text
desired != observed
```

ADR-BCP-008 drift detection SHALL identify the divergence.

---

# 148. Reconciliation Response

Depending on policy CP may:

```text
auto-repair
raise change action
mark not ready
escalate
```

It SHALL not silently accept arbitrary out-of-band state as new desired truth.

---

# 149. Change Communication

High-impact Changesets SHOULD support stakeholder communication metadata.

Examples:

```text
customer notice required
operations notice required
security notice required
maintenance announcement required
```

---

# 150. Notification Is Not Governance

Sending an email or message SHALL NOT constitute:

```text
approval
```

unless a future formally integrated approval channel satisfies the approval contract.

---

# 151. Human-Readable Plan

ADR-BCP-019 requires non-technical administration.

The plan therefore SHALL support a human-readable projection.

Example:

```text
Requested change
────────────────────────
Enable ERP for ACME Foods — Uganda

What will change
────────────────────────
1 subscription will be updated
7 capability grants will be created
1 ERP provider binding will be created
1 IAM organisation projection will be updated
1 ERP tenant configuration will be provisioned

Security impact
────────────────────────
No isolation downgrade
No residency change

Availability
────────────────────────
No expected outage

Risk
────────────────────────
HIGH

Approval required
────────────────────────
1 Platform Approver
```

---

# 152. Technical Plan View

Authorised operators MAY expand:

```text
resource identifiers
provider IDs
engine instance
contract versions
preconditions
dependency graph
step sequence
compensation
```

---

# 153. Diff Representation

The Console SHOULD present meaningful changes as:

```text
BEFORE
→
AFTER
```

Example:

```text
Markets

Before:
South Africa

After:
South Africa
Uganda
```

---

# 154. Sensitive Plan Data

Change plans SHALL not contain raw secrets merely because they are operationally convenient.

Use:

```text
SecretReference
```

rather than:

```text
secret_value
```

Terraform documentation warns that execution plans can contain sensitive values; Baobab SHALL design its plan model to minimise such exposure by default.

---

# 155. Approval UX

The approver SHALL be able to understand:

```text
what is changing
why
scope
risk
impact
irreversible actions
availability effect
who requested it
which exact plan is approved
```

---

# 156. Blind Approval Is Prohibited

An approval interface SHALL NOT reduce high-risk changes to:

```text
Approve
Reject
```

without providing sufficient plan context.

---

# 157. Plan Explainability

The system SHOULD be able to explain why a plan contains a derived action.

Example:

```text
Why is ERP provider provisioning required?

Because:
ProductSubscription "Baobab ERP"
requires capability finance.ledger.manage,
which resolves to provider idempiere
for Tenant T / Market UG.
```

---

# 158. Derived-Change Provenance

Derived steps SHOULD retain provenance.

Conceptually:

```text
Requested:
Enable Product X

Derived:
Capability A
  source = Product X composition

Derived:
Binding B
  source = capability A provider resolution
```

---

# 159. No Hidden Side Effects

Material derived effects SHALL be visible in the plan.

An administrator requesting:

```text
Enable service
```

should not discover after execution that it silently:

```text
changed residency
changed isolation
added global administrator
```

---

# 160. Policy Obligations

Planning MAY produce obligations.

Examples:

```text
SECURITY_APPROVAL_REQUIRED
MAINTENANCE_WINDOW_REQUIRED
CUSTOMER_NOTICE_REQUIRED
STEP_UP_REQUIRED
DATA_MIGRATION_REQUIRED
```

---

# 161. Obligations Must Be Satisfied

A plan SHALL not apply until mandatory obligations are satisfied.

---

# 162. External Dependencies

Plans SHOULD record critical dependencies.

Example:

```text
IAM available
provider ready
engine instance healthy
required region available
contract version compatible
```

---

# 163. Dependency Failure Before Apply

If a critical dependency is unavailable before execution:

```text
BLOCKED
```

may be preferable to starting a likely partial failure.

---

# 164. Dependency Failure During Apply

The orchestrator SHALL use:

```text
retry
pause
forward recovery
compensation
operator intervention
```

according to step policy.

---

# 165. Change Events

Canonical events SHOULD eventually include:

```text
baobab.control-plane.changeset.created.v1
baobab.control-plane.changeset.submitted.v1
baobab.control-plane.changeset.planned.v1
baobab.control-plane.changeset.approved.v1
baobab.control-plane.changeset.rejected.v1
baobab.control-plane.changeset.execution-started.v1
baobab.control-plane.changeset.execution-failed.v1
baobab.control-plane.changeset.completed.v1
baobab.control-plane.changeset.compensated.v1
```

Final naming SHALL conform to the canonical event namespace defined by `baobab-platform/shared`.

No local repository SHALL independently override the shared event convention.

---

# 166. Audit

Every consequential stage SHALL be auditable.

At minimum:

```text
who requested
who planned/system planned
what plan was produced
what impacts were found
who approved
what exact plan digest was approved
who initiated execution
what steps executed
what failed
what was compensated
what readiness resulted
what final outcome occurred
```

---

# 167. Actor vs System

Audit SHALL preserve:

```text
human actor
```

and:

```text
executing workload
```

where the human initiated an operation executed by CP.

---

# 168. Correlation

The entire lifecycle SHOULD share:

```text
changeset_id
operation_id
correlation_id
trace_id where available
```

---

# 169. Decision References

Security/approval decisions MAY additionally use:

```text
decision_id
```

consistent with Baobab audit architecture.

---

# 170. Audit Immutability

Replanning SHALL not erase previous:

```text
plan
impact analysis
approval
rejection
```

records.

They form part of the historical decision chain.

---

# 171. Change Timeline

The CP Console SHOULD be able to present:

```text
09:14  Change requested by Jane
09:15  Validation passed
09:15  Plan v1 generated
09:16  Risk classified HIGH
09:43  Approved by Peter
09:44  Execution started
09:45  IAM configuration succeeded
09:46  ERP provisioning succeeded
09:47  Reconciliation started
09:48  Readiness READY
09:48  Change completed
```

---

# 172. Observability

Changeset execution SHALL emit operational telemetry.

Useful measures include:

```text
changesets created
planning latency
approval latency
execution latency
success rate
partial failure rate
compensation rate
compensation failure rate
stale plan rate
emergency change rate
verification failure rate
```

---

# 173. Change Failure Rate

Baobab SHOULD eventually monitor:

```text
percentage of production changes
that cause failure, degradation or corrective action
```

as an operational quality signal.

---

# 174. Mean Recovery

Operations SHOULD measure recovery time from failed high-impact Changesets.

---

# 175. Approval Latency

Approval delay SHOULD also be observable because governance can itself become an operational bottleneck.

This information SHALL inform process improvement.

It SHALL NOT automatically weaken controls.

---

# 176. Change Volume

Metrics SHOULD distinguish:

```text
routine
moderate
high
critical
emergency
automated
```

changes.

---

# 177. Retention

Changeset, approval and execution records SHALL be retained according to platform audit/compliance policy.

They SHALL outlive short-lived frontend sessions.

---

# 178. API Behaviour

The administrative API SHALL expose Changeset semantics rather than forcing the Console to orchestrate low-level mutations.

Conceptually:

```text
Create Changeset
       │
       ▼
Submit
       │
       ▼
Plan
       │
       ▼
Approve
       │
       ▼
Apply
       │
       ▼
Observe Operation
```

Exact endpoint design belongs to ADR-BCP-022 and OpenAPI.

---

# 179. Command vs Query

Changing state SHALL remain semantically distinct from querying:

```text
plan
impact
readiness
status
```

The API SHOULD make this distinction explicit.

---

# 180. Idempotent Command Submission

Administrative commands SHALL use idempotency mechanisms where repeated submission could produce duplicate operations.

---

# 181. Duplicate Apply

Applying the same approved plan twice SHALL not accidentally create:

```text
duplicate tenant
duplicate grants
duplicate provider tenant
duplicate administrator
```

---

# 182. Execution Attempt

Retries of an operation SHALL increment or otherwise record:

```text
execution_attempt
```

without producing a new logical Changeset unless semantics change.

---

# 183. Replan vs Retry

The system SHALL distinguish:

```text
RETRY
=
same approved semantic operation after transient failure
```

from:

```text
REPLAN
=
derive materially new plan due to changed assumptions
```

A replan may require new approval.

---

# 184. Retry Does Not Change Plan

A retry SHALL not quietly modify the planned semantics.

---

# 185. Customer-Initiated Change

An Organisation Administrator MAY create a Changeset such as:

```text
Request Kenya market
```

even if that administrator cannot approve or execute it.

The flow may be:

```text
Customer Request
       │
       ▼
Changeset
       │
       ▼
Platform Review
       │
       ▼
Plan
       │
       ▼
Approval
       │
       ▼
Execution
```

---

# 186. Self-Service Change

Future self-service MAY allow:

```text
requester
```

to also cause execution where policy pre-approves the class of change.

This SHALL still use the Changeset architecture.

---

# 187. Self-Service Does Not Mean Direct Mutation

Even fully automated self-service SHOULD follow:

```text
Intent
→ Validate
→ Plan
→ Policy
→ Execute
→ Verify
```

The approval stage may be:

```text
POLICY_APPROVED
```

rather than human-approved.

---

# 188. Policy Approval

Low-risk changes MAY be approved by a deterministic pre-authorised policy.

Conceptually:

```text
approval_type = POLICY
```

Audit SHALL identify the policy/version.

---

# 189. Automated Approval Safety

Policy approval SHALL only apply to defined change classes.

Unknown or materially novel changes SHALL not default to automated approval.

---

# 190. Provider Migration

Provider migration is a prime Changeset use case.

Example:

```text
Capability:
commerce.order.manage

Current:
Provider A / Instance A

Target:
Provider B / Instance B
```

The plan may include:

```text
validate target
provision target
shadow validation
migration binding
controlled cutover
readiness
decommission source later
```

---

# 191. Migration Source Decommission

Source decommission SHOULD ordinarily occur in a separate stage or Changeset after target stability is proven.

This reduces irreversible cutover risk.

---

# 192. Isolation Upgrade

Changing:

```text
SHARED_LOGICAL
→ DEDICATED_DATABASE
```

may involve:

```text
new database
data migration
provider reconfiguration
binding update
readiness checks
source retirement
```

This SHALL be planned as a distributed operation.

---

# 193. Isolation Downgrade

The reverse change deserves stronger scrutiny because it may weaken tenant isolation.

It SHOULD normally receive:

```text
security impact analysis
high/critical risk
security approval
contract validation
```

---

# 194. Residency Change

Moving processing/storage region SHALL require explicit plan visibility.

No provider placement algorithm SHALL silently override residency constraints during execution.

---

# 195. Market Activation

Example:

```text
Add Uganda market
```

may derive:

```text
MarketParticipation
Digital Estate scope
provider eligibility
engine placement
currency context
capability scope
```

The plan SHALL explain these derived changes.

---

# 196. Market Deactivation

Market removal SHALL identify dependencies such as:

```text
active estate
active capability grants
provider configuration
outstanding migrations
```

It SHALL not simply delete `MarketParticipation`.

---

# 197. Administrative Authority Change

ADR-BCP-020 grant/delegation changes MAY themselves use Changesets where risk requires it.

Example:

```text
Grant platform-wide Security Administrator
```

should normally be a governed Changeset.

---

# 198. Emergency Security Suspension

Some defensive actions may need very fast execution.

Example:

```text
Suspend compromised tenant
```

Policy MAY permit:

```text
emergency authorised actor
→ immediate change
→ retrospective approval/review
```

Security containment SHALL not be blocked unnecessarily by ordinary business approval latency.

---

# 199. Destructive Changes

Destructive changes SHALL expose:

```text
what is destroyed
what can be restored
what cannot be restored
retention implications
dependencies
customer impact
```

---

# 200. Two-Step Destruction

Critical destructive operations SHOULD consider:

```text
SUSPEND / MARK FOR DECOMMISSION
          │
          ▼
retention / review period
          │
          ▼
FINAL DESTRUCTION
```

rather than immediate irreversible deletion.

---

# 201. Data Retention

Change execution SHALL obey domain/data retention requirements.

A `DECOMMISSION` Changeset SHALL NOT imply:

```text
delete all data immediately
```

unless retention policy explicitly requires that outcome.

---

# 202. Changeset and Business Domain Boundary

This ADR governs Control Plane-owned configuration changes.

It SHALL NOT turn CP into a workflow engine for ordinary domain business transactions.

Not CP Changesets:

```text
approve purchase order
post journal
refund customer
approve supplier price
publish CMS article
dispatch shipment
```

Those remain domain workflows.

---

# 203. Platform Configuration vs Business Operation

Use a Changeset when the intent changes:

```text
platform configuration
tenancy
capability availability
topology
platform security
administrative authority
```

Not simply because an operation is important.

---

# 204. No Universal Workflow Engine

The Changeset subsystem SHALL NOT become:

```text
generic BPMN platform
enterprise workflow replacement
business process engine for all Baobab domains
```

Its scope is Control Plane governance.

---

# 205. No Infrastructure-as-Code Clone

Although the plan/apply concept resembles infrastructure-as-code systems, Baobab SHALL not recreate Terraform.

CP Changesets govern Baobab canonical platform state.

Infrastructure tooling remains authoritative for infrastructure resources it owns.

---

# 206. Infrastructure Changes

When a CP Changeset requires infrastructure work outside CP's authority, the plan SHALL reference or invoke the approved infrastructure boundary.

CP SHALL not absorb Terraform/Kubernetes ownership merely because orchestration is convenient.

---

# 207. Contract Ownership

Cross-repository canonical schemas SHOULD eventually include:

```text
Changeset
ChangePlan
ImpactAnalysis
ApprovalDecision
ExecutionOperation
ChangeOutcome
```

where interoperability requires them.

Authority:

```text
baobab-platform/shared
```

for contract shape.

Runtime authority:

```text
baobab-platform/baobab-cp
```

---

# 208. Persistence

CP SHALL own authoritative persistence for:

```text
Changeset
plan metadata
impact analysis
approval references
execution state
change outcome
```

subject to final physical model.

---

# 209. Immutability Strategy

Immutable historical objects SHOULD include:

```text
approved plans
approval decisions
execution outcomes
historical impact analyses
```

Updates SHOULD create new versions rather than rewriting decision history.

---

# 210. Mutable Working Objects

The following MAY remain mutable until submission:

```text
draft Changeset
draft justification
draft requested scope
```

Once submitted/planned, controlled versioning applies.

---

# 211. State Machine Enforcement

The backend SHALL enforce legal transitions.

Example prohibited:

```text
DRAFT
→
COMPLETED
```

Likewise:

```text
REJECTED
→
APPLYING
```

without appropriate reactivation/revision semantics.

---

# 212. Example Canonical Flow

```text
DRAFT
   │
   ▼
SUBMITTED
   │
   ▼
VALIDATING
   │
   ▼
PLANNING
   │
   ▼
PLANNED
   │
   ▼
AWAITING_APPROVAL
   │
   ▼
APPROVED
   │
   ▼
APPLYING
   │
   ▼
RECONCILING
   │
   ▼
VERIFYING
   │
   ▼
COMPLETED
```

---

# 213. Failure Branch

```text
APPLYING
   │
   ├── transient ──► RETRY
   │
   ├── compensable failure
   │        │
   │        ▼
   │   COMPENSATING
   │        │
   │        ▼
   │   COMPENSATED
   │
   └── unresolved
            │
            ▼
     PARTIALLY_APPLIED
            │
            ▼
      OPERATOR ACTION
```

---

# 214. Planning Flow

```text
Intent
  │
  ▼
Load Current State
  │
  ▼
Resolve Dependencies
  │
  ▼
Calculate Desired Delta
  │
  ▼
Evaluate Policy
  │
  ▼
Impact Analysis
  │
  ▼
Risk Classification
  │
  ▼
Generate Execution DAG
  │
  ▼
Generate Verification Plan
  │
  ▼
Generate Compensation Metadata
  │
  ▼
Hash Plan
```

---

# 215. Apply Flow

```text
Approved Plan
     │
     ▼
Validate Plan Digest
     │
     ▼
Validate Approval
     │
     ▼
Validate Authority
     │
     ▼
Validate Base Revision
     │
     ▼
Acquire Semantic Locks
     │
     ▼
Execute Steps
     │
     ▼
Commit Desired State
     │
     ▼
Reconcile
     │
     ▼
Verify
     │
     ▼
Release Locks
     │
     ▼
Outcome + Audit
```

---

# 216. Compensation Flow

```text
Step Failure
    │
    ▼
Classify Failure
    │
    ├── Retryable
    │      │
    │      ▼
    │    Retry
    │
    └── Non-Retryable
           │
           ▼
     Before Pivot?
       /       \
     YES        NO
      │          │
      ▼          ▼
Compensate   Forward Recovery
      │          │
      └────┬─────┘
           ▼
        Verify
```

---

# 217. UI Responsibility

ADR-BCP-019's Console SHALL present and operate Changesets.

It SHALL NOT:

```text
derive authoritative plan logic
calculate authorization
mutate provider resources directly
decide readiness
invent compensation
```

---

# 218. Console Areas

The CP Console SHOULD eventually expose:

```text
Changes
Approvals
Scheduled Changes
Active Operations
Failed Operations
Change History
Emergency Changes
```

according to actor authority.

---

# 219. My Changes

An organisation administrator SHOULD be able to see:

```text
requested changes
current status
approval state
execution progress
final outcome
```

within permitted scope.

---

# 220. Approval Inbox

Approvers SHOULD see:

```text
risk
requester
scope
plan
impact
irreversible effects
conflicts
required assurance
```

not merely the request title.

---

# 221. Operations View

Platform operators SHOULD see:

```text
running operation
current step
elapsed time
dependencies
retries
readiness
drift
safe available actions
```

---

# 222. Safe Operator Actions

Possible operational actions MAY include:

```text
retry step
resume
pause at safe boundary
request cancellation
start approved compensation
escalate
```

only where backend policy permits.

---

# 223. Manual Step Completion

If some plan requires authorised manual work, the system MAY support a governed manual step.

Such a step SHALL record:

```text
responsible actor
instructions
evidence/reference
completion time
verification
```

---

# 224. No Invisible Manual Work

Critical manual commands performed outside the system SHALL not be represented merely as:

```text
checkbox = done
```

without sufficient evidence where policy requires it.

---

# 225. Change Attachments

A Changeset MAY reference:

```text
architecture review
customer approval
contract amendment
migration evidence
security assessment
```

through governed references.

Raw binaries need not live in CP.

---

# 226. Change Comments

Discussion MAY be supported.

Comments SHALL not alter the executable plan.

---

# 227. Decision vs Discussion

This distinction SHALL remain:

```text
Comment
    != Approval

Comment
    != Plan

Comment
    != Execution Instruction
```

---

# 228. Search and Reporting

The platform SHOULD support querying Changesets by:

```text
organisation
tenant
requester
approver
change type
risk
status
date
market
resource
operation outcome
```

---

# 229. Impact Queries

A future query SHOULD answer:

> Which production changes affected ZuriBeans Uganda during this incident window?

This requires structured scope/audit, not free-text tickets.

---

# 230. Incident Correlation

Incident tooling SHOULD be able to correlate:

```text
degradation began
        │
        ▼
recent Changesets
        │
        ▼
potential causal candidates
```

The system SHALL not automatically claim causation merely because timestamps correlate.

---

# 231. Change Freeze During Incident

Policy MAY temporarily prohibit unrelated high-risk changes during:

```text
major incident
```

to reduce diagnostic noise.

Emergency remediation remains possible.

---

# 232. Change Freeze Scope

Freeze may apply to:

```text
platform
tenant
provider
region
capability
```

rather than necessarily globally.

---

# 233. Change Windows

Some regulated or critical organisations MAY configure narrower permitted production-change windows.

Such requirements SHOULD eventually be expressed as policy.

---

# 234. Customer Approval

Future enterprise contracts MAY require customer approval for certain provider/platform changes.

The approval model SHALL be extensible enough to represent:

```text
customer approver
```

without granting that customer global CP administration.

---

# 235. Cross-Organisation Change

A Changeset affecting several independent customer organisations SHALL be considered higher blast radius.

The plan SHALL enumerate each affected organisation.

---

# 236. Shared Provider Change

Changing one shared engine instance may affect many tenants.

Impact analysis SHALL therefore traverse:

```text
EngineInstance
       │
       ▼
Bindings
       │
       ▼
Capabilities
       │
       ▼
Tenants / Estates
```

before execution.

---

# 237. Provider Maintenance

Provider maintenance SHOULD be represented differently from tenant-specific configuration when appropriate.

The shared Changeset framework still applies.

---

# 238. Cascading Effects

Impact analysis SHALL detect canonical cascading relationships rather than relying only on directly targeted objects.

Example:

```text
EngineInstance
→ CapabilityBinding
→ CapabilityGrant
→ Product
→ Digital Estate
→ Tenant
```

---

# 239. Impact Traversal Must Be Bounded

Impact analysis SHALL use explicit canonical relationships.

It SHALL NOT scan arbitrary databases attempting to infer unknown dependencies.

---

# 240. Unknown Impact

If the platform cannot determine required impact safely:

```text
IMPACT_UNKNOWN
```

SHOULD raise risk or block execution.

Unknown shall not automatically be treated as safe.

---

# 241. Policy Evolution

A plan generated under:

```text
policy version 10
```

may become invalid if:

```text
policy version 11
```

introduces materially stricter requirements before apply.

---

# 242. Policy Version

Plans SHOULD record relevant policy versions.

---

# 243. Contract Evolution

Plans involving provider contracts SHOULD identify:

```text
required contract version
provider support version
```

where applicable.

---

# 244. Contract Drift

If contract compatibility changes before apply:

```text
PLAN_STALE
```

or:

```text
BLOCKED
```

may be appropriate.

---

# 245. Secrets

Secrets SHALL never be embedded into:

```text
approval comments
plan JSON
audit event
Changeset description
```

unless encrypted and explicitly designed for such storage—which SHOULD generally be avoided.

---

# 246. PII Minimisation

Changeset records SHOULD store canonical identifiers rather than unnecessary duplicated personal data.

---

# 247. Audit Redaction

Sensitive metadata MAY be redacted from routine user-facing audit views while preserving authoritative evidence.

---

# 248. Retrying Authentication

Long-running Changesets SHALL not depend on a browser session remaining alive.

Once validly authorised and accepted, execution SHALL proceed under controlled server-side operation identity and preserved initiating-principal attribution.

---

# 249. User Logout

If the initiating human logs out after operation start:

```text
execution need not abort
```

unless policy explicitly requires it.

---

# 250. Authority Revocation During Execution

If the requester's authority is revoked after execution starts, policy SHALL determine whether the already-authorised operation:

```text
continues
pauses
or aborts
```

based on risk.

For critical operations, re-evaluation MAY be required at safe boundaries.

---

# 251. Service Identity

Execution SHALL use a dedicated workload identity with only the permissions necessary for orchestration.

It SHALL NOT execute downstream actions using the human user's raw OAuth token throughout the long-running workflow.

---

# 252. Attribution

Audit SHALL retain:

```text
initiating human
+
executing service
```

---

# 253. Operation Recovery After CP Restart

Execution state SHALL be durable.

A Control Plane restart SHALL not lose:

```text
which step completed
which step is pending
which idempotency keys were used
which compensation is required
```

---

# 254. Resume

After restart the orchestrator SHOULD safely resume from durable operation state.

---

# 255. At-Least-Once Reality

Cross-system commands may effectively be delivered at least once.

Therefore idempotency is required.

The system SHALL not assume exactly-once networks.

---

# 256. Step Results

Step results SHOULD store references to created/modified canonical/external resources.

This aids:

```text
verification
compensation
audit
diagnostics
```

---

# 257. Timeouts

Each external execution step SHOULD have a bounded timeout policy.

Unknown completion after timeout SHALL be treated carefully.

---

# 258. Timeout Ambiguity

A network timeout does not prove:

```text
operation did not occur.
```

Before retrying a non-trivial external step, CP SHOULD query observed state where possible.

---

# 259. Reconciliation as Recovery Tool

When command result is ambiguous:

```text
observe
compare
reconcile
```

is safer than blindly issuing duplicate mutations.

---

# 260. Provider Idempotency

Baobab provider adapters SHOULD expose idempotent provisioning semantics where possible.

---

# 261. Operation Heartbeats

Long-running operations MAY emit heartbeat/progress metadata.

Failure to receive progress SHALL not automatically imply failure unless timeout policy says so.

---

# 262. Manual Intervention State

If safe automated recovery is impossible:

```text
BLOCKED
```

with:

```text
MANUAL_INTERVENTION_REQUIRED
```

shall be explicit.

---

# 263. Operator Remediation

Manual remediation SHOULD itself be:

```text
authorised
recorded
scoped
```

and may require a follow-up Changeset.

---

# 264. No Silent State Editing

Operators SHALL not be offered a generic:

```text
mark operation succeeded
```

button that bypasses verification.

---

# 265. Administrative Override

If an override is necessary, it SHALL require:

```text
explicit permission
reason
high assurance
audit
```

and SHOULD not rewrite actual execution evidence.

---

# 266. Verification Override

A human MAY acknowledge:

```text
accepted degraded state
```

only where policy explicitly permits.

The system SHALL preserve that readiness remains degraded.

---

# 267. Ready Is Evidence-Based

A human administrator SHALL not simply choose:

```text
READY
```

from a dropdown for provider readiness.

Readiness derives from authoritative readiness evaluation.

---

# 268. Completion Override

Similarly:

```text
COMPLETED
```

SHALL not ordinarily be a manually assigned cosmetic status.

---

# 269. Change Quality Gates

Different change classes MAY require:

```text
contract tests
security tests
provider preflight
capacity check
backup verification
migration rehearsal
```

before approval or apply.

---

# 270. Preflight

A plan MAY include non-mutating preflight checks.

Example:

```text
target engine reachable
capacity available
required region available
schema compatible
IAM mapping available
```

---

# 271. Preflight Freshness

A preflight result may expire.

Critical assumptions SHOULD be rechecked before execution.

---

# 272. Backups

Where a change can affect recoverable persistent state, the plan SHOULD identify whether backup/snapshot validation is required.

CP does not necessarily perform the backup itself.

---

# 273. Backup Reference

The plan MAY carry:

```text
backup_reference
```

or:

```text
recovery_point_reference
```

where required.

---

# 274. Migration Rehearsal

High-risk data migrations SHOULD be rehearsed in an appropriate non-production environment where feasible.

---

# 275. Production State Differences

Successful staging rehearsal SHALL reduce risk.

It SHALL NOT prove production impact is identical.

A production plan remains environment-specific.

---

# 276. Change Policy

Baobab SHOULD introduce a policy layer conceptually capable of answering:

```text
Is this change permitted?

Which risk class applies?

Which approvals are required?

Which assurance is required?

May it auto-apply?

Which maintenance constraints apply?

Which verification is mandatory?
```

---

# 277. Policy Is Not Hardcoded UI

Change policy SHALL live in backend-governed rules/configuration.

The CP Console merely displays the result.

---

# 278. Policy as Code Trajectory

The architecture MAY later support policy-as-code.

This ADR does not mandate a particular policy engine.

---

# 279. No Premature Policy Engine

Baobab SHALL not introduce OPA, Cedar or another policy engine solely because this ADR mentions policy.

Initial explicit Go domain policy may be entirely appropriate.

A separate policy runtime requires demonstrated need and its own decision.

---

# 280. Approval Matrix

The platform SHOULD be capable of an approval matrix similar to:

| Risk | Example | Human Approval |
|---|---|---|
| Low | benign metadata | May be policy-approved |
| Moderate | staging configuration | 0–1 depending on policy |
| High | production capability/market change | Normally ≥1 |
| Critical | isolation/residency/decommission | Enhanced/multi-party |

Exact rules SHALL remain policy-controlled.

---

# 281. Administrative Authority Matrix

A Changeset action SHALL map to ADR-BCP-020 administrative permissions.

Example:

| Action | Permission |
|---|---|
| Create change | `changeset.create` |
| Submit | `changeset.submit` |
| View impact | `changeset.view` |
| Approve | `changeset.approve` |
| Schedule | `changeset.schedule` |
| Apply | `changeset.apply` |
| Request cancel | `changeset.cancel` |
| Invoke compensation | `changeset.compensate` |
| Emergency apply | `changeset.emergency.apply` |

Final vocabulary SHALL be canonicalised.

---

# 282. Requester vs Executor

The person creating the Changeset does not necessarily execute it.

For example:

```text
Organisation Administrator
        │
        ▼
requests market activation

Platform Operator
        │
        ▼
applies approved plan
```

---

# 283. Approver vs Executor

Likewise approval and execution may be separate responsibilities.

---

# 284. Automation Executor

Once approved, a trusted workload SHOULD normally perform the actual technical execution.

This improves:

```text
repeatability
least privilege
audit
resumability
```

---

# 285. No Human SSH Requirement

Ordinary Control Plane changes SHOULD NOT require an engineer to:

```text
SSH into production
```

and manually execute configuration.

---

# 286. Support for Manual Platforms

Where a downstream system cannot yet be automated, a controlled manual step MAY temporarily bridge the gap.

The architecture SHOULD gradually remove such steps.

---

# 287. Change Governance Maturity

Baobab may evolve:

```text
Manual Changeset + Manual Steps
            │
            ▼
Automated Planning
            │
            ▼
Automated Execution
            │
            ▼
Policy-Based Self-Service
```

without changing the core governance model.

---

# 288. Definition of Change Success

Success SHALL be defined per change type.

Examples:

```text
Market activation:
MarketParticipation ACTIVE
+ mandatory capabilities READY

Provider migration:
target binding PRIMARY
+ readiness stable
+ source no longer serving authoritative traffic

Administrative grant:
new grant ACTIVE
+ audit emitted
```

---

# 289. Success Is Not HTTP 200

A successful API response indicating operation acceptance SHALL not be interpreted as final change success.

---

# 290. Asynchronous API Semantics

A mutating administrative API may return:

```text
202 Accepted
```

with operation reference when execution is asynchronous.

Exact HTTP semantics belong to ADR-BCP-022/OpenAPI.

---

# 291. Problem Details

Errors SHOULD use structured canonical problem details.

Reason codes SHALL distinguish:

```text
PLAN_STALE
APPROVAL_REQUIRED
CONFLICT
NOT_READY
POLICY_DENIED
OPERATION_IN_PROGRESS
```

---

# 292. Contract Compatibility

Changeset schemas SHALL be versioned.

The Console and API SHALL maintain compatibility during rolling deployments.

---

# 293. Planner Version

Plans SHOULD record:

```text
planner_version
```

or equivalent.

This allows investigation of why two plans generated at different times differ.

---

# 294. Executor Version

Execution SHOULD similarly record relevant executor/software revision.

---

# 295. Git Commit Traceability

Deployment metadata SHOULD make it possible to associate operation behaviour with:

```text
baobab-cp release
git SHA
```

where operationally appropriate.

---

# 296. Reproducibility

Planning SHOULD be deterministic for equivalent:

```text
intent
state snapshot
policy
contracts
planner version
```

as far as practical.

---

# 297. Non-Deterministic Inputs

Where dynamic inputs influence planning:

```text
provider health
capacity
regional availability
```

they SHALL be recorded or referenced.

---

# 298. Planning Snapshot

The platform MAY persist a planning-state summary sufficient for audit and staleness detection.

It SHALL avoid duplicating entire databases.

---

# 299. Plan Size

Large bulk plans MAY require paginated or referenced detail.

A plan digest still covers the complete material semantics.

---

# 300. Final Architectural Invariants

The following SHALL remain non-negotiable:

```text
Intent
    != execution

Changeset
    != Plan

Plan
    != Approval

Approval
    binds to exact Plan

Modified Plan
    invalidates prior material approval

Stale Plan
    cannot silently apply

Requester
    != Approver
where SoD requires

Apply
    != Ready

Ready
    != Active

Failure
    must distinguish partial mutation

Rollback
    != Compensation

Compensation
    may fail

Distributed change
    != one ACID transaction

Reconciliation
    remains convergence authority

Frontend
    != planning authority

Frontend
    != execution authority

Provider
    cannot be mutated directly by browser

Emergency
    != unaudited

Automation
    != ungoverned

Self-service
    != direct CRUD

Corporate hierarchy
    cannot silently alter change authority

Current authorization
    must be evaluated server-side

High-impact change
    must be explainable

Irreversible change
    must be explicit

Historical plans and approvals
    must not be rewritten
```

---

# 301. Implementation Programme

Implementation SHALL proceed incrementally and preserve existing runtime behaviour.

## Gate CCM-00 — Existing Mutation Inventory

Audit:

```text
baobab-cp handlers
service-layer mutations
tenant provisioning
subscription mutation
grant mutation
binding mutation
provider migration
IAM integration
reconciliation
current manual workflows
```

Classify each existing mutation:

```text
DIRECT_LOW_RISK
CHANGESET_REQUIRED
SYSTEM_RECONCILIATION
DOMAIN_OUTSIDE_CP
DEPRECATED
```

---

## Gate CCM-01 — Canonical Change Contracts

Define:

```text
Changeset
ChangePlan
ImpactAnalysis
ApprovalDecision
ExecutionOperation
ChangeOutcome
```

in CP/shared according to cross-repository requirements.

---

## Gate CCM-02 — Changeset Persistence and State Machine

Implement:

```text
draft
submission
validation
state transitions
versioning
scope
risk
audit
```

No distributed execution yet.

---

## Gate CCM-03 — Planner

Implement deterministic planning over a limited first change class.

Recommended first candidate:

```text
tenant/product onboarding
```

because onboarding already has a defined provisioning model.

---

## Gate CCM-04 — Impact Analysis

Implement:

```text
affected-resource traversal
security impact
tenancy impact
residency/isolation impact
blast radius
risk classification
```

---

## Gate CCM-05 — Plan Digest and Staleness

Implement:

```text
immutable plan versions
plan hashing
base revisions
optimistic concurrency
staleness detection
```

---

## Gate CCM-06 — Approval Integration

Integrate ADR-BCP-020:

```text
approval authority
separation of duties
approval decision
plan digest binding
step-up requirements
```

---

## Gate CCM-07 — Execution Operations

Implement durable:

```text
operation
step
attempt
idempotency
dependencies
locks
resume
```

against limited CP-owned resources.

---

## Gate CCM-08 — Reconciliation Integration

Ensure execution commits desired state and hands convergence to the existing reconciliation architecture.

Avoid building a competing reconciler.

---

## Gate CCM-09 — Provider Orchestration

Introduce governed provider/IAM steps with:

```text
timeouts
retry
idempotency
observation
```

---

## Gate CCM-10 — Compensation

Implement explicit compensation metadata and failure handling.

Test partial failure thoroughly.

---

## Gate CCM-11 — Readiness and Verification

Integrate:

```text
readiness
verification
stabilisation window where required
completion criteria
```

---

## Gate CCM-12 — CP Console

Expose:

```text
My Changes
Plan
Impact
Approvals
Operations
Progress
Failure
Recovery
History
```

through ADR-BCP-019.

---

## Gate CCM-13 — Emergency and JIT Integration

Integrate:

```text
emergency Changesets
break-glass
JIT authority
retrospective review
```

with ADR-BCP-020.

---

## Gate CCM-14 — Progressive/Bulk Changes

Add:

```text
waves
canary
bulk operations
blast-radius controls
```

only after single-scope execution is proven.

---

## Gate CCM-15 — Production Hardening

Complete:

```text
failure injection
concurrency tests
restart recovery
stale-plan tests
approval invalidation tests
partial provider failure
compensation failure
cross-tenant isolation tests
event/outbox tests
observability
runbooks
```

before broad production use.

---

# 302. Required Test Scenarios

At minimum:

| Scenario | Expected Result |
|---|---|
| Apply unapproved high-risk plan | DENY |
| Apply plan with wrong digest | DENY |
| Edit approved plan | New plan version; approval invalid |
| Resource changes after planning | PLAN_STALE |
| Two conflicting Changesets | Conflict/serialization |
| Duplicate apply request | Same logical operation/no duplicates |
| Executor crashes mid-step | Safe resume |
| Provider times out after completing | Observe before unsafe retry |
| Retryable failure | Bounded retry |
| Permanent failure before mutation | FAILED / no material change |
| Failure after partial mutation | PARTIALLY_APPLIED or compensation |
| Compensation succeeds | COMPENSATED |
| Compensation fails | COMPENSATION_FAILED |
| Requester tries self-approval | DENY where SoD applies |
| Approval authority revoked | Policy re-evaluation |
| Readiness fails after apply | Not COMPLETED/ACTIVE |
| Browser closes mid-execution | Server operation continues |
| CP restarts | Durable operation resumes |
| Cross-tenant target tampering | DENY |
| Out-of-band provider mutation | Drift detected |
| Emergency change | Elevated audit + retrospective review |
| Bulk partial failure | Policy-consistent per-target result |
| Plan contains secret | Validation/security failure |

---

# 303. Alternatives Considered

## Alternative A — Direct CRUD Administration

**Rejected.**

It cannot adequately represent:

```text
derived effects
impact
approval
long-running execution
partial failure
recovery
```

---

## Alternative B — Approval on Free-Text Ticket Only

**Rejected.**

A person may approve one intent while a materially different technical plan is later executed.

Approval SHALL bind to the plan.

---

## Alternative C — Recalculate Plan During Apply

**Rejected for material plan semantics.**

This creates a gap between:

```text
what was reviewed
```

and:

```text
what was executed.
```

---

## Alternative D — Global Database Transaction

**Rejected.**

Baobab spans independent engines and authorities.

Global ACID semantics are neither realistic nor desirable.

---

## Alternative E — Two-Phase Commit Across Engines

**Rejected as the default architecture.**

It would tightly couple providers and create availability/operational complexity contrary to Baobab's decoupled engine model.

---

## Alternative F — Pure Event Choreography

**Rejected for governed Changesets.**

Complex approval/provisioning flows need an explicit coordinator and durable operation state.

Events remain part of the architecture but do not replace orchestration.

---

## Alternative G — Generic Workflow/BPM Engine Immediately

**Rejected for now.**

The required orchestration can initially live inside the modular Go Control Plane.

If complexity later justifies an external workflow runtime, that requires a separate ADR.

---

## Alternative H — Terraform as the Control Plane Change Engine

**Rejected.**

Terraform governs infrastructure resources.

Baobab Changesets govern canonical SaaS/platform state.

The plan/apply principle is useful; the authority boundary is different.

---

# 304. Positive Consequences

This decision provides:

```text
controlled platform mutation
human-readable change review
machine-executable exact plans
impact visibility
least-privileged approval
separation of duties
safe customer self-service trajectory
idempotent execution
distributed failure recovery
readiness-aware completion
provider migration governance
better incident correlation
strong audit history
progressive rollout capability
```

---

# 305. Negative Consequences

The model introduces complexity:

```text
planning engine
impact graph
approval model
operation state
step orchestration
locking
idempotency
compensation
verification
```

This complexity is accepted.

The platform already has distributed change complexity.

The decision merely makes it explicit, governable and observable instead of hiding it behind HTTP mutation endpoints.

---

# 306. Relationship With ADR-BCP-008

ADR-BCP-008 answers:

> What should exist, what exists, where is drift, and is the result ready?

This ADR answers:

> How does an authorised proposed change safely modify what should exist?

Relationship:

```text
Changeset
    │
    ▼
Desired State Change
    │
    ▼
ADR-BCP-008 Reconciliation
    │
    ▼
Observed State
    │
    ▼
Readiness
```

---

# 307. Relationship With ADR-BCP-019

ADR-BCP-019 provides the human Control Plane interface.

This ADR provides the controlled mutation semantics behind that interface.

The Console SHALL not bypass it.

---

# 308. Relationship With ADR-BCP-020

ADR-BCP-020 answers:

```text
Who may act?
```

ADR-BCP-021 answers:

```text
How does the authorised action become controlled platform change?
```

Together:

```text
Identity
   │
   ▼
Administrative Authority
   │
   ▼
Changeset
   │
   ▼
Plan
   │
   ▼
Approval
   │
   ▼
Execution
```

---

# 309. Relationship With ADR-BCP-022

ADR-BCP-022 SHALL define:

```text
Administrative API
Command/query semantics
Long-running operation API
Pagination
Problem Details
Idempotency headers/contracts
BFF consumption
```

ADR-BCP-022 SHALL expose this ADR's model.

It SHALL NOT redefine it.

---

# 310. Final Decision

Baobab SHALL treat consequential Control Plane mutation as a **governed lifecycle**, not direct CRUD.

The canonical chain SHALL be:

```text
Intent
   │
   ▼
Changeset
   │
   ▼
Validation
   │
   ▼
Impact Analysis
   │
   ▼
Immutable Change Plan
   │
   ▼
Risk / Policy
   │
   ▼
Exact-Plan Approval
   │
   ▼
Durable Execution Operation
   │
   ▼
Desired State
   │
   ▼
Reconciliation
   │
   ▼
Readiness
   │
   ▼
Verification
   │
   ▼
Audited Outcome
```

Distributed failures SHALL be managed through:

```text
idempotency
retry
observation
forward recovery
compensation
```

rather than false assumptions of cross-engine ACID atomicity.

High-risk changes SHALL be:

```text
impact analysed
explicitly scoped
properly authorised
reviewable before execution
bound to an immutable plan
recoverable where possible
verified after execution
fully auditable
```

The strategic outcome is:

> **Every consequential Baobab platform change can answer, before execution: what will change, why it will change, who requested it, who may approve it, what it affects, what risks it creates, what exact plan will execute, and how failure will be handled.**

And after execution it can answer:

> **What actually changed, which steps succeeded, which failed, what was compensated, whether desired and observed state converged, whether the platform became ready, who authorised the outcome, and what evidence remains for audit and recovery.**

The central architectural discipline is therefore:

> **Baobab does not mutate consequential platform state merely because an authorised person clicked a button. It converts authorised intent into an analysed, approved, executable and verifiable plan, then reconciles reality against the resulting desired state.**