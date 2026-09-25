# ADR-BCP-022 — Administrative API, Command/Query, Long-Running Operation and Error Contract Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-23  
**Decision Owners:** Baobab Platform Architecture / Platform Security / Platform Operations  
**Primary Repository:** `baobab-platform/baobab-cp`  
**Runtime Authority:** Baobab Control Plane  
**Canonical Contract Authority:** `baobab-platform/shared`  
**Identity / Authentication Authority:** `baobab-platform/baobab-iam`  
**Administrative Human Interface:** Baobab Control Plane Console  
**Primary API Description:** OpenAPI contract maintained under `baobab-platform/shared`  
**Decision Type:** Foundational administrative API, command/query, asynchronous operation, concurrency, idempotency, pagination and error-contract architecture

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
- ADR-BCP-017 — Organisation Admission, Subscription Classification and Tenant Onboarding Lifecycle Model
- ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account and Tenant Relationship Model
- ADR-BCP-019 — Control Plane Administrative Frontend, Organisation Onboarding Experience and Repository Composition Model
- ADR-BCP-020 — Administrative Authority, Delegated Administration, Privileged Access and Separation-of-Duties Model
- ADR-BCP-021 — Changeset, Impact Analysis, Approval and Controlled Mutation Model
- `BCP-TS-ONBOARDING-001` — Tenant Onboarding & Provisioning Technical Specification
- `contracts/control-plane/v1/openapi.yaml`
- `contracts/errors/v1/problem-details.schema.json`
- `contracts/idempotency/v1/policy.yaml`
- `contracts/control-plane/v1/security-policy.yaml`
- Applicable canonical contracts in `baobab-platform/shared`

**External Standards / Validation References:**

- RFC 9110 — HTTP Semantics
- RFC 9457 — Problem Details for HTTP APIs
- RFC 6585 — Additional HTTP Status Codes
- W3C Trace Context
- OpenAPI Specification
- OAuth / OIDC security profile defined by Baobab IAM ADRs
- Google AIP-151 — Long-Running Operations, as non-normative comparative guidance

---

# 1. Executive Decision

Baobab SHALL establish a dedicated, contract-first **Administrative API family** for human and automated administration of the Control Plane.

The administrative surface SHALL be logically distinct from the existing runtime resolution APIs.

The target API architecture SHALL be:

```text
                         BAOBAB CONTROL PLANE
                                  │
             ┌────────────────────┼─────────────────────┐
             │                    │                     │
             ▼                    ▼                     ▼
      RUNTIME APIs         ADMINISTRATIVE APIs     HEALTH / OPS
             │                    │                     │
             │                    │                     │
      Context resolution     Organisations          healthz
      Capability resolve     Applications           readyz
      Mapping resolve        Tenants
      Runtime assertions     Changesets
             │               Operations
             │               Approvals
             │               Readiness
             │               Audit
             │
     high-frequency           human / automation
     deterministic            governance-oriented
```

New human-administration APIs SHALL normally be rooted under:

```text
/v1/admin/
```

Existing runtime APIs SHALL remain under their existing stable runtime routes unless separately versioned or formally deprecated.

The governing principles are:

> **Runtime APIs resolve platform context and capabilities; Administrative APIs govern platform state. They share one Control Plane domain but SHALL NOT share indistinguishable API semantics.**

> **Administrative APIs SHALL expose domain commands and read models, not database CRUD.**

> **Long-running work SHALL return durable operation resources rather than keeping browser requests open while distributed orchestration completes.**

> **The API contract SHALL express concurrency, idempotency, authorization, errors, asynchronous progress and audit correlation consistently across all administrative resources.**

---

# 2. Why This ADR Is Required

The current Control Plane API grew around runtime and initial provisioning requirements.

Existing routes include concepts such as:

```text
/v1/context/resolve
/v1/platform-context/resolve
/v1/capabilities/resolve
/v1/capabilities/resolve-batch

/v1/tenants
/v1/tenants/{tenantID}
/v1/tenants/{tenantID}/activate
/v1/tenants/{tenantID}/suspend
/v1/tenants/{tenantID}/decommission

/v1/canonical-entities
/v1/canonical-entities/{entityID}

 /v1/tenants/{tenantID}/provisioning
```

This was suitable while most Control Plane consumers were:

```text
engine workloads
developers
platform engineers
early provisioning automation
```

ADR-BCP-019 introduces a non-technical administrative Console.

ADR-BCP-020 introduces fine-grained administrative authority.

ADR-BCP-021 introduces governed Changesets, impact analysis, approvals and asynchronous controlled mutation.

Those decisions require a richer API contract.

---

# 3. Current API Strengths to Preserve

The existing API already contains several desirable patterns that SHALL be retained and generalised:

```text
OpenAPI contract
RFC 9457-style problem details
Idempotency-Key
correlation IDs
causation IDs
Trace Context
ETags
If-Match support
tenant-safe not-found behaviour
explicit runtime/admin OAuth scopes
202 Accepted for provisioning
Location headers
durable provisioning identifiers
```

This ADR SHALL evolve those foundations rather than replace them.

---

# 4. Current Gaps to Resolve

The repository audit identifies architectural gaps that this ADR SHALL close.

Current concerns include:

```text
runtime and administrative concerns mixed under one API namespace

broad platform-admin / tenant-admin role interpretation

some lifecycle actions exposed as direct mutation

long provisioning work sometimes driven synchronously by HTTP handlers

inconsistent concurrency behaviour

If-Match optional in some contracts but mandatory in newer handlers

unpaginated administrative collections

actor identity occasionally accepted from request payload

mixed error-code casing in implementation

incomplete Administrative API coverage in shared OpenAPI

no canonical generic long-running Operation model

no uniform command response model

no Changeset API family yet
```

These are expected evolutionary gaps.

They are not grounds for discarding the current API.

---

# 5. API Families

Baobab SHALL distinguish four API classes:

| API Family | Purpose |
|---|---|
| Runtime | Context/capability resolution for workloads |
| Administrative | Human or administrative automation controlling CP state |
| Diagnostic | Privileged explanation, readiness and operational diagnostics |
| Health | Process health/readiness for infrastructure |

---

# 6. Runtime APIs

Runtime APIs answer questions such as:

```text
Who is this workload acting for?

Which tenant context is valid?

Is capability C available?

Which provider may fulfil it?
```

Examples remain:

```text
POST /v1/context/resolve
POST /v1/platform-context/resolve
POST /v1/capabilities/resolve
POST /v1/capabilities/resolve-batch
POST /v1/resolution/mappings
```

These APIs SHALL remain optimised for:

```text
low latency
deterministic resolution
small payloads
bounded caching
workload identity
fail-closed authorization
```

---

# 7. Administrative APIs

Administrative APIs answer questions and accept commands such as:

```text
Show organisation ACME.

Create an application.

Submit this onboarding request.

Show the impact of this Changeset.

Approve this exact plan.

Apply this approved plan.

Show operation progress.

Suspend tenant T.

Show readiness.

Delegate administration.

List current administrators.
```

The administrative API is not a business-domain API.

---

# 8. Administrative Namespace

New administrative APIs SHOULD use:

```text
/v1/admin/
```

Examples:

```text
/v1/admin/applications
/v1/admin/organisations
/v1/admin/platform-accounts
/v1/admin/tenants
/v1/admin/changesets
/v1/admin/operations
/v1/admin/approvals
/v1/admin/audit-events
```

This produces an explicit trust and semantics boundary.

---

# 9. Existing Administrative Routes

Existing routes such as:

```text
/v1/tenants
/v1/canonical-entities
/v1/markets
/v1/mappings
```

SHALL NOT be broken merely to achieve naming symmetry.

They MAY:

```text
remain supported
be internally redirected through common application services
be gradually superseded
be formally deprecated later
```

according to compatibility policy.

---

# 10. No Flag-Day API Reorganisation

The Control Plane SHALL NOT require every existing consumer to migrate simultaneously.

Evolution SHOULD be:

```text
Existing API
    │
    ├── preserved where stable
    │
    ▼
New /v1/admin API
    │
    ▼
Consumers migrate where appropriate
    │
    ▼
Old route formally deprecated only if justified
```

---

# 11. Administrative API Consumers

The API SHALL support at least:

```text
CP Console BFF

future authorised administrative automation

migration tooling

platform operational tooling

approved partner administration clients
```

The browser itself SHALL ordinarily communicate with the CP Console BFF rather than directly holding privileged CP access tokens.

---

# 12. BFF Boundary

The preferred chain remains:

```text
Browser
   │
   ▼
CP Console / BFF
   │
   ▼
Administrative API
   │
   ▼
CP Application / Domain
```

The BFF SHALL NOT become a second domain authority.

---

# 13. No Trusted User Headers

The Administrative API SHALL NOT trust headers such as:

```text
X-User-ID
X-Admin-ID
X-Organisation-Admin
X-Role
```

as authoritative identity merely because the BFF supplied them.

Identity SHALL derive from cryptographically validated IAM credentials and canonical identity resolution.

---

# 14. Actor Identity Comes From Authentication

Administrative request payloads SHALL NOT ordinarily contain fields such as:

```text
approved_by
created_by
retired_by
suspended_by
```

when those values identify the authenticated actor.

Instead:

```text
authenticated principal
        │
        ▼
canonical principal
        │
        ▼
audit actor
```

SHALL determine actor identity.

---

# 15. Why Client-Supplied Actor Identity Is Rejected

This is unsafe:

```json
{
  "approved_by": "CEO"
}
```

because the request body is caller-controlled input.

The body MAY contain:

```text
reason
comment
decision rationale
business justification
```

but not authoritative identity of the actor performing the action.

---

# 16. Delegated Actor Context

Where one service acts on behalf of a human, audit SHALL preserve both:

```text
subject / initiating principal
```

and:

```text
executing workload
```

according to IAM and audit ADRs.

---

# 17. Contract-First Development

Administrative API behaviour SHALL be defined in canonical contracts before implementation is considered complete.

The preferred lifecycle is:

```text
Domain Decision
     │
     ▼
Canonical Contract
     │
     ▼
OpenAPI
     │
     ▼
Go Handler / Service
     │
     ▼
Generated / Typed Frontend Client
     │
     ▼
Contract Tests
```

---

# 18. Contract Authority

Where a contract is cross-repository or consumer-facing:

```text
baobab-platform/shared
```

SHALL remain authoritative.

`baobab-cp` implements the contract.

It SHALL NOT silently fork it.

---

# 19. OpenAPI Baseline

The existing Control Plane contract uses:

```text
OpenAPI 3.1
```

This ADR SHALL retain the existing contract family.

Migration to a newer OpenAPI minor line, if desired, SHALL be performed deliberately as contract/tooling work rather than being coupled to this architecture decision.

---

# 20. One API Description, Logical Families

Baobab MAY maintain one composed OpenAPI document or modular OpenAPI source files.

The contract SHOULD nevertheless clearly identify tags/families such as:

```text
Runtime
Applications
Organisations
Tenants
Changesets
Approvals
Operations
Readiness
Audit
Diagnostics
```

---

# 21. API Versioning

The public contract family SHALL continue using path-major versioning:

```text
/v1/
```

Breaking semantic changes require deliberate versioning.

---

# 22. What Constitutes a Breaking Change

Examples include:

```text
removing a required response field
changing field meaning
changing identifier semantics
changing state-machine meaning
changing enum value semantics
changing error interpretation
changing authorization assumption
changing resource identity
```

---

# 23. Additive Evolution

Within `/v1`, Baobab SHOULD prefer:

```text
new optional fields
new endpoints
new optional filters
new non-breaking response metadata
```

where clients can safely ignore unknown fields according to their contract.

---

# 24. Enum Evolution

Enums require care.

Clients SHALL NOT be designed under the assumption:

```text
the current enum list can never grow
```

where extension is architecturally expected.

---

# 25. API Version ≠ Domain Version

The following remain distinct:

```text
API major version
Product version
Capability contract version
Changeset plan version
resource revision
```

They SHALL not be conflated.

---

# 26. Command / Query Separation

Administrative APIs SHALL distinguish conceptually:

```text
QUERY
```

from:

```text
COMMAND
```

A query asks:

> What is true?

A command asks:

> Please attempt to make this authorised state transition or action occur.

---

# 27. Query Invariant

Queries SHALL NOT intentionally alter authoritative business/platform state.

Allowed incidental effects include:

```text
access logging
metrics
tracing
cache activity
```

but not domain mutation.

---

# 28. Command Invariant

A command SHALL explicitly represent intent to change state or trigger a consequential process.

Commands SHALL be:

```text
authorised
validated
audited
idempotent where applicable
concurrency-aware
```

---

# 29. CQRS Without Premature Infrastructure

This ADR adopts **semantic command/query separation**.

It DOES NOT mandate:

```text
separate databases
separate message buses
separate query service
event sourcing
```

The Go Control Plane MAY implement both against the same PostgreSQL system.

---

# 30. Purpose-Built Read Models

Administrative queries MAY return projections designed for human administration.

Examples:

```text
OrganisationSummary
OrganisationDashboard
ApplicationSummary
TenantSummary
ChangesetSummary
OperationSummary
ReadinessSummary
AuditTimeline
```

These are projections.

They are not new sources of truth.

---

# 31. Resource APIs

Stable domain resources SHOULD use resource-oriented paths.

Examples:

```text
GET /v1/admin/organisations/{organisation_id}

GET /v1/admin/tenants/{tenant_id}

GET /v1/admin/changesets/{changeset_id}

GET /v1/admin/operations/{operation_id}
```

---

# 32. Command Subresources

Actions with meaningful domain semantics SHOULD use explicit command subresources.

Examples:

```text
POST /v1/admin/changesets/{id}/submit

POST /v1/admin/changesets/{id}/approve

POST /v1/admin/changesets/{id}/apply

POST /v1/admin/operations/{id}/cancel

POST /v1/admin/tenants/{id}/suspend
```

---

# 33. Why Explicit Commands

The API SHOULD prefer:

```text
POST /changesets/{id}/approve
```

over a generic:

```text
PATCH /changesets/{id}
{
  "status": "approved"
}
```

because approval is a governed domain action, not arbitrary field mutation.

---

# 34. State Machines Are Not Editable Fields

Lifecycle state SHALL be controlled by domain commands.

Prohibited:

```json
PATCH /tenant/123

{
  "status": "ACTIVE"
}
```

where activation requires readiness or approval.

---

# 35. PATCH Usage

`PATCH` MAY be used for genuinely editable resource attributes.

Examples:

```text
draft application fields
non-authoritative description
allowed contact metadata
draft Changeset intent
```

provided domain invariants are preserved.

---

# 36. Lifecycle Changes Use Commands

Examples:

```text
submit
approve
activate
suspend
reinstate
decommission
retire
cancel
```

SHOULD ordinarily be explicit commands.

---

# 37. DELETE Usage

`DELETE` SHALL be used sparingly for canonical administrative resources.

Where lifecycle/history matters, prefer:

```text
retire
decommission
revoke
cancel
archive
```

instead of physical deletion.

---

# 38. Physical Deletion

Physical deletion MAY remain appropriate for:

```text
short-lived drafts
ephemeral non-audit resources
records explicitly governed by deletion policy
```

but not as a substitute for lifecycle management.

---

# 39. Administrative API Concept Map

```text
Administrative API
│
├── Applications
├── Organisations
│   ├── relationships
│   └── verification
│
├── Platform Accounts
├── Tenants
├── Markets
├── Digital Estates
├── Subscriptions
├── Administrative Grants
├── Changesets
│   ├── plan
│   ├── impact
│   └── approvals
│
├── Operations
├── Readiness
├── Drift
└── Audit
```

---

# 40. Application APIs

Conceptual routes MAY include:

```text
POST /v1/admin/applications
GET  /v1/admin/applications
GET  /v1/admin/applications/{id}

POST /v1/admin/applications/{id}/submit
POST /v1/admin/applications/{id}/request-information
POST /v1/admin/applications/{id}/approve
POST /v1/admin/applications/{id}/reject
POST /v1/admin/applications/{id}/withdraw
```

Exact OpenAPI shape SHALL follow ADR-BCP-017.

---

# 41. Organisation APIs

Conceptual routes MAY include:

```text
GET /v1/admin/organisations
GET /v1/admin/organisations/{id}

GET /v1/admin/organisations/{id}/relationships
GET /v1/admin/organisations/{id}/tenants
GET /v1/admin/organisations/{id}/administrators
GET /v1/admin/organisations/{id}/readiness
GET /v1/admin/organisations/{id}/activity
```

Mutations with consequential effects SHOULD use Changesets.

---

# 42. Platform Account APIs

Conceptually:

```text
GET /v1/admin/platform-accounts
GET /v1/admin/platform-accounts/{id}
GET /v1/admin/platform-accounts/{id}/organisations
GET /v1/admin/platform-accounts/{id}/tenants
```

---

# 43. Tenant APIs

Conceptually:

```text
GET /v1/admin/tenants
GET /v1/admin/tenants/{id}
GET /v1/admin/tenants/{id}/markets
GET /v1/admin/tenants/{id}/subscriptions
GET /v1/admin/tenants/{id}/digital-estates
GET /v1/admin/tenants/{id}/readiness
```

High-impact lifecycle actions SHOULD flow through Changesets.

---

# 44. Changeset APIs

Conceptually:

```text
POST /v1/admin/changesets
GET  /v1/admin/changesets
GET  /v1/admin/changesets/{id}

PATCH /v1/admin/changesets/{id}

POST /v1/admin/changesets/{id}/submit

GET  /v1/admin/changesets/{id}/plan
GET  /v1/admin/changesets/{id}/impact

POST /v1/admin/changesets/{id}/approve
POST /v1/admin/changesets/{id}/reject
POST /v1/admin/changesets/{id}/request-changes

POST /v1/admin/changesets/{id}/apply
POST /v1/admin/changesets/{id}/cancel
```

---

# 45. Operation APIs

Conceptually:

```text
GET  /v1/admin/operations
GET  /v1/admin/operations/{id}
POST /v1/admin/operations/{id}/cancel
POST /v1/admin/operations/{id}/retry
```

where those actions are valid for the operation class.

---

# 46. Audit APIs

Conceptually:

```text
GET /v1/admin/audit-events
GET /v1/admin/organisations/{id}/audit-events
GET /v1/admin/tenants/{id}/audit-events
GET /v1/admin/changesets/{id}/timeline
```

Audit queries SHALL remain read-only.

---

# 47. Diagnostics

Privileged diagnostics SHOULD be explicitly separated.

Examples:

```text
/v1/admin/diagnostics/...
```

or clearly privileged diagnostic routes.

Diagnostic authority SHALL be stronger than ordinary organisation administration where internal topology is exposed.

---

# 48. Administrative Authorization Layers

Every Administrative API request SHALL normally pass:

```text
Token Validation
      │
      ▼
Client / Audience / Coarse Scope
      │
      ▼
Canonical Principal Resolution
      │
      ▼
ADR-BCP-020 Administrative Authority
      │
      ▼
Resource / Context Policy
      │
      ▼
Command-Specific Policy
```

---

# 49. IAM Coarse Scope Is Not Final Authorization

A token may permit invocation of the administrative API class.

It SHALL NOT imply:

```text
platform-wide administrative authority.
```

---

# 50. Remove Dependency on Broad Realm Roles

The current implementation's:

```text
cp:platform-admin
cp:tenant-admin
```

model MAY remain temporarily during migration.

The target model SHALL use ADR-BCP-020's:

```text
AdministrativeGrant
+
AdministrativePermission
+
AdministrativeScope
```

as the authoritative CP decision.

---

# 51. Migration Safety

Existing broad roles SHALL not be removed before equivalent or narrower effective authority has been verified.

---

# 52. Resource Existence Confidentiality

Cross-tenant or cross-organisation resources SHALL not leak existence unnecessarily.

Example:

```text
GET Organisation B resource
by Organisation A administrator
```

MAY return:

```text
404 Not Found
```

rather than revealing:

```text
resource exists but belongs to another customer
```

where confidentiality requires this posture.

---

# 53. Administrative Lists Must Be Scope-Filtered

The following is prohibited:

```text
GET /organisations
returns every organisation
then frontend hides unauthorised rows
```

The backend SHALL scope the query before returning data.

---

# 54. Long-Running Operations

Any command that may materially outlive a normal HTTP request SHALL use a durable long-running operation.

Examples:

```text
tenant provisioning
Changeset apply
provider migration
bulk mutation
decommissioning
large export
complex reconciliation
```

---

# 55. HTTP Acceptance

When work is accepted but not complete, the API SHOULD return:

```text
202 Accepted
```

with a representation of the durable operation.

---

# 56. Operation Location

The response SHALL identify the operation resource.

Conceptually:

```http
Location: /v1/admin/operations/{operation_id}
```

and the response body SHOULD contain the same canonical operation identifier.

---

# 57. 202 Does Not Mean Success

A `202 Accepted` response means:

```text
request accepted for processing
```

not:

```text
requested business/platform outcome succeeded.
```

The caller SHALL inspect the operation.

---

# 58. Durable Operation Resource

Conceptually:

```text
Operation
├── id
├── operation_type
├── status
├── requested_by
├── changeset_id?
├── target_scope
├── current_phase?
├── progress?
├── started_at?
├── updated_at
├── completed_at?
├── result?
├── problem?
├── retryable
├── revision
├── correlation_id
└── links
```

---

# 59. Operation Status

The common operation vocabulary SHOULD support:

```text
QUEUED
PREPARING
RUNNING
WAITING
VERIFYING
BLOCKED
SUCCEEDED
FAILED
PARTIALLY_APPLIED
COMPENSATING
COMPENSATED
COMPENSATION_FAILED
CANCEL_REQUESTED
CANCELLED
```

Not every operation class uses every state.

---

# 60. Domain Status vs Operation Status

Do not conflate:

```text
Operation = SUCCEEDED
```

with:

```text
Tenant = ACTIVE
```

The operation result SHALL identify the resulting resource state.

---

# 61. Progress

Progress MAY be represented as:

```text
current_phase
completed_steps
total_steps
percentage?
```

where meaningful.

---

# 62. Percentage Is Optional

The API SHALL NOT fabricate:

```text
73%
```

when work cannot meaningfully be estimated.

Phase/status information is preferable to false precision.

---

# 63. Operation Result

Successful completion MAY expose:

```text
result_resource
result_uri
result_summary
```

rather than copying an arbitrarily large final resource into the operation forever.

---

# 64. Operation Failure

A failed operation SHOULD contain or reference a canonical RFC 9457 problem representation.

---

# 65. Polling

The baseline operation-consumption mechanism SHALL be polling:

```text
GET /v1/admin/operations/{id}
```

because it is simple, robust and restart-safe.

---

# 66. Retry Guidance

The API MAY return:

```text
Retry-After
```

where it can reasonably suggest a polling interval or retry time.

---

# 67. Conditional Operation Polling

Operation resources SHOULD support:

```text
ETag
If-None-Match
```

where practical.

This allows repeated polling to return:

```text
304 Not Modified
```

without resending identical payloads.

---

# 68. Streaming Is Optional

Future versions MAY support:

```text
Server-Sent Events
```

for operation updates.

Polling remains authoritative and sufficient.

---

# 69. WebSocket Is Not Required

The Administrative API SHALL NOT introduce WebSockets merely for visual progress indicators.

---

# 70. Operation Events

If SSE is later introduced, it SHALL represent updates to the same durable operation.

The stream SHALL NOT become the only source of truth.

---

# 71. Browser Disconnection

Disconnecting the browser SHALL NOT cancel an accepted operation.

---

# 72. BFF Restart

Restarting the Console/BFF SHALL NOT lose operation state.

The operation lives in the Control Plane.

---

# 73. CP Restart

The operation model SHALL be durably recoverable after Control Plane restart, as required by ADR-BCP-021.

---

# 74. Operation Cancellation

Cancellation SHALL be an explicit command.

Conceptually:

```text
POST /v1/admin/operations/{id}/cancel
```

---

# 75. Cancel Request ≠ Immediate Cancellation

If the operation is currently in a non-interruptible step, the response may indicate:

```text
CANCEL_REQUESTED
```

while the orchestrator reaches a safe boundary.

---

# 76. Cancellation Conflict

Attempting to cancel an already completed or irreversible operation SHOULD return a domain conflict rather than pretending cancellation occurred.

---

# 77. Operation Retry

Retry SHALL be supported only when the operation's failure state declares retry safe.

---

# 78. Retry vs New Changeset

A retry means:

```text
same approved semantic plan
```

A materially different recovery action requires:

```text
new or revised Changeset
```

---

# 79. Commands That Complete Synchronously

A small low-risk command MAY return its final resource immediately.

Examples might include:

```text
save draft
update harmless metadata
```

provided no long-running external consequence exists.

---

# 80. Created Resource

A synchronous successful create SHOULD use:

```text
201 Created
```

with:

```text
Location
```

identifying the created resource.

---

# 81. Successful Query

A successful query ordinarily returns:

```text
200 OK
```

---

# 82. Successful Command With Representation

A synchronous domain command that completes and returns updated representation MAY return:

```text
200 OK
```

---

# 83. Successful Command Without Representation

Where no representation is useful:

```text
204 No Content
```

MAY be appropriate.

---

# 84. Do Not Overload HTTP Status

HTTP status conveys transport/application-level semantics.

Detailed domain outcome SHALL remain in:

```text
resource representation
operation state
problem details
```

rather than inventing custom HTTP codes.

---

# 85. Canonical Error Model

All Administrative API errors SHALL use:

```text
application/problem+json
```

following the existing Baobab RFC 9457-based contract.

---

# 86. Existing Problem Details Contract

The existing contract already defines:

```text
type
title
status
detail
instance
code
correlation_id
trace_id
retryable
errors[]
```

This structure SHALL be retained.

---

# 87. Problem `type`

`type` SHALL identify stable problem semantics.

It SHALL NOT identify only the individual occurrence.

---

# 88. Problem Namespace

Problem-type URIs SHALL use an approved Baobab-controlled namespace.

Stale pre-renaming references such as:

```text
docs.nabhold.com
```

SHALL be removed during migration.

The final public problem namespace SHALL be governed centrally.

---

# 89. Problem `code`

`code` SHALL be the stable machine-readable Baobab code.

Canonical style SHALL remain:

```text
UPPER_SNAKE_CASE
```

Examples:

```text
AUTHORIZATION_DENIED
VALIDATION_FAILED
PLAN_STALE
VERSION_CONFLICT
APPROVAL_REQUIRED
```

---

# 90. Code Casing Must Be Consistent

Existing lowercase variants such as:

```text
validation_failed
internal_error
```

SHALL be migrated to the canonical contract vocabulary.

Clients SHALL use canonical codes, not free-form detail text.

---

# 91. Problem `detail`

`detail` SHALL be safe human-readable explanation.

It SHALL NOT contain:

```text
stack trace
secret
access token
database credentials
unnecessary PII
raw provider exception
```

---

# 92. Problem Extensions

Domain-specific extensions MAY be added through versioned schema evolution when machine clients need structured additional context.

---

# 93. Validation Errors

Field-specific validation issues SHOULD appear in:

```text
errors[]
```

with:

```text
code
field
message
```

---

# 94. Client Must Not Parse Human Text

Clients SHALL act on:

```text
status
code
structured fields
```

not by parsing:

```text
detail
```

strings.

---

# 95. Error Mapping

Administrative APIs SHOULD use the following semantic mapping.

| Status | Meaning |
|---:|---|
| 400 | Malformed or structurally invalid request |
| 401 | Authentication required/invalid |
| 403 | Authenticated but insufficient authority |
| 404 | Resource unavailable/not found, including concealed cross-scope resources |
| 409 | Domain/lifecycle/idempotency conflict |
| 412 | Conditional request precondition failed |
| 422 | Structurally valid but semantically invalid input |
| 428 | Required concurrency precondition missing |
| 429 | Rate limited |
| 500 | Unexpected server failure |
| 503 | Required service/dependency temporarily unavailable |
| 504 | Required downstream operation timed out where synchronous boundary applies |

---

# 96. 400 Bad Request

Use when the server cannot correctly interpret the request.

Examples:

```text
invalid JSON
invalid header syntax
unsupported request shape
malformed UUID
```

---

# 97. 422 Unprocessable Content

Use when:

```text
syntax valid
schema structurally acceptable
but domain semantics invalid
```

Examples:

```text
market unsupported
illegal state transition input
invalid organisation relationship
```

where another status such as 409 is not more appropriate.

---

# 98. 409 Conflict

Use for conflicts with current domain state.

Examples:

```text
IDEMPOTENCY_KEY_REUSED
INVALID_STATE_TRANSITION
OVERLAPPING_CHANGESET
PLAN_STATE_CONFLICT
```

---

# 99. 412 Precondition Failed

Use when:

```text
If-Match supplied
but resource version changed
```

for standard conditional-request semantics.

---

# 100. 428 Precondition Required

Use when:

```text
If-Match is mandatory
but missing
```

for a mutation requiring optimistic concurrency.

---

# 101. 403 vs 404

Where disclosing existence would create a cross-tenant or security leak, the API MAY intentionally return:

```text
404
```

rather than:

```text
403
```

after appropriate authorization processing.

---

# 102. Retryable Flag

`retryable` SHALL describe whether retry is potentially appropriate under documented retry policy.

---

# 103. Retryable Does Not Mean Retry Immediately

When:

```json
"retryable": true
```

the caller SHALL still obey:

```text
Retry-After
backoff
operation-specific policy
```

where applicable.

---

# 104. Dependency Failure

Internal provider failure SHALL not expose provider internals unnecessarily.

External response might be:

```text
DEPENDENCY_UNAVAILABLE
```

while detailed provider diagnostics remain internal.

---

# 105. Correlation ID

Every Administrative API interaction SHALL have:

```text
correlation_id
```

The existing:

```text
X-Correlation-ID
```

contract SHALL remain.

---

# 106. Caller-Supplied Correlation

A valid caller correlation ID MAY be accepted.

If absent, CP SHALL mint one.

---

# 107. Invalid Correlation ID

Malformed correlation IDs SHALL produce a safe client error.

The server SHALL not silently accept invalid arbitrary text.

---

# 108. Causation ID

The existing:

```text
X-Causation-ID
```

SHOULD be preserved for chained commands/events.

---

# 109. Trace Context

The Administrative API SHALL continue supporting W3C:

```text
traceparent
tracestate
```

for distributed tracing where applicable.

---

# 110. Correlation vs Trace

These SHALL remain distinct:

```text
correlation_id
    business/workflow correlation

trace_id
    distributed technical trace
```

One may outlive the other.

---

# 111. Changeset Correlation

All API interactions involved in one Changeset execution SHOULD be traceable back to:

```text
changeset_id
operation_id
correlation_id
```

---

# 112. Idempotency

Commands capable of:

```text
creating resources
changing state
triggering external effects
starting an operation
```

SHALL support idempotency according to the canonical shared policy.

---

# 113. Idempotency-Key

The existing header remains:

```text
Idempotency-Key
```

---

# 114. Idempotency Key Properties

Keys SHALL:

```text
be sufficiently unique
contain no secrets
contain no personal data
not encode business payload
```

---

# 115. Idempotency Request Fingerprint

The key SHALL be bound to canonical request semantics.

Fingerprint inputs SHOULD include:

```text
effective principal
operation
target scope
canonical request body
relevant command version
```

---

# 116. Idempotency Scope Generalisation

The current shared policy includes:

```text
tenant_id
```

as an idempotency scope component.

Administrative commands may occur:

```text
before a tenant exists
at organisation scope
at PlatformAccount scope
platform-wide
```

Therefore the policy SHALL evolve conceptually from:

```text
service
operation
tenant_id
idempotency_key
```

to:

```text
service
operation
effective_administrative_scope
idempotency_key
```

where tenant ID remains one possible scope dimension.

---

# 117. Same Key, Same Request

Replaying the same command with the same valid key SHALL return the same logical result without repeating the consequential effect.

---

# 118. Same Key, Different Request

The API SHALL return:

```text
409 Conflict
IDEMPOTENCY_KEY_REUSED
```

and SHALL NOT execute the changed request.

---

# 119. Concurrent Duplicate Requests

Concurrent requests using the same scoped idempotency key SHALL be:

```text
serialized
or
atomically arbitrated
```

according to the existing policy.

---

# 120. Idempotency Retention

The server SHALL retain idempotency records longer than the maximum documented client retry window for that command class.

---

# 121. Idempotency and Operations

If a command starts:

```text
Operation OP-123
```

an idempotent replay SHALL resolve to:

```text
OP-123
```

rather than creating:

```text
OP-124
```

for the same logical request.

---

# 122. Idempotency Does Not Replace Concurrency

These solve different problems:

```text
Idempotency
    protects duplicate intent

Optimistic concurrency
    protects stale intent
```

Both may be required.

---

# 123. Optimistic Concurrency

Mutable versioned administrative resources SHALL expose:

```text
ETag
```

or an equivalent canonical version validator.

---

# 124. Strong Validators

For mutation protection, version validators SHALL represent the version of authoritative state sufficiently strongly to detect stale updates.

---

# 125. If-Match

Consequential direct updates to versioned resources SHALL require:

```text
If-Match
```

where stale writes are possible.

---

# 126. Current Contract Migration

The current shared OpenAPI marks `If-Match` optional on some routes while newer Go handlers already require it.

Target behaviour SHALL be consistent:

```text
mutation requires concurrency protection
        │
        ▼
If-Match required
```

---

# 127. Missing If-Match

If required and absent:

```text
428 Precondition Required
```

SHALL be returned.

---

# 128. Mismatched If-Match

If supplied but stale:

```text
412 Precondition Failed
```

SHOULD be returned.

---

# 129. Domain Conflict Is Different

A valid current version may still fail because:

```text
transition prohibited
business policy blocks change
```

That remains:

```text
409 Conflict
```

or another appropriate domain status.

---

# 130. ETag on GET

Versioned resources SHOULD return:

```text
ETag
```

on successful retrieval.

---

# 131. Frontend Concurrency UX

The CP Console SHOULD respond to `412` by telling the user:

```text
This resource changed after you opened it.
Reload the latest version before applying your changes.
```

It SHALL not automatically overwrite.

---

# 132. Changeset Concurrency

ADR-BCP-021's:

```text
base_revision
plan digest
semantic locks
```

provide higher-level concurrency in addition to HTTP resource concurrency.

---

# 133. Collection Pagination

Administrative collection endpoints SHALL be paginated.

Examples:

```text
organisations
applications
changesets
operations
audit events
administrative grants
```

---

# 134. Cursor-Based Pagination

The preferred default SHALL be opaque cursor pagination.

Conceptually:

```text
GET /v1/admin/organisations?page_size=50&page_token=...
```

---

# 135. Why Cursor Pagination

Administrative datasets are mutable.

Cursor-based pagination better avoids:

```text
duplicate rows
skipped rows
expensive large offsets
```

during concurrent changes.

---

# 136. Opaque Tokens

Clients SHALL treat:

```text
page_token
```

as opaque.

They SHALL NOT parse or manufacture it.

---

# 137. Page Size

The initial baseline SHOULD be:

```text
default = 50
maximum = 200
```

unless a resource-specific contract defines otherwise.

---

# 138. Server May Reduce Page Size

The server MAY return fewer items than requested.

Clients SHALL follow:

```text
next_page_token
```

rather than assuming page length indicates completion.

---

# 139. Collection Response

Conceptually:

```json
{
  "items": [],
  "page": {
    "next_page_token": "...",
    "has_more": true
  }
}
```

Additional metadata MAY be added as required.

---

# 140. Total Count

Exact total counts SHALL NOT be mandatory for every endpoint.

They can be expensive and misleading under concurrent mutation.

---

# 141. Estimated or Exact Counts

Where a UI genuinely requires count information, the contract SHALL make its semantics explicit:

```text
exact
estimated
snapshot-relative
```

---

# 142. Stable Ordering

Every paginated collection SHALL define deterministic ordering.

Example:

```text
created_at DESC
id DESC
```

---

# 143. Cursor Includes Ordering Context

The cursor SHALL encode or reference sufficient ordering state to resume safely.

Implementation details remain opaque.

---

# 144. Filtering

Administrative collections MAY support explicit filters.

Examples:

```text
status
organisation_id
tenant_id
market_id
risk
requested_by
created_after
created_before
```

---

# 145. No Arbitrary Query Language

The API SHALL NOT expose unrestricted client-controlled SQL-like filtering.

Allowed filters SHALL be explicitly contracted.

---

# 146. Sorting

Sort fields SHALL be allow-listed.

Clients SHALL NOT submit arbitrary column or expression names.

---

# 147. Search

Free-text search MAY be provided for resources where useful.

Search semantics SHALL be explicitly documented.

---

# 148. Search Is Not Authorization

Search SHALL execute only across resources already visible to the caller.

---

# 149. Pagination and Authorization

Authorization filtering SHALL occur before or as part of query execution.

It SHALL NOT produce:

```text
page of 50
then remove 47 unauthorized rows
```

if that leaks counts or produces broken pagination.

---

# 150. Bulk Reads

Where a UI needs multiple known resources, an explicit bounded batch query MAY be provided.

---

# 151. Batch Limits

Batch APIs SHALL have documented maximum sizes to prevent:

```text
unbounded database queries
memory abuse
authorization amplification
```

---

# 152. Bulk Mutation

Bulk mutation SHALL use ADR-BCP-021 Changesets.

It SHALL NOT ordinarily be:

```text
PATCH 5,000 resources independently
```

from the browser.

---

# 153. Response Envelopes

Single-resource responses SHOULD generally return the resource representation directly.

Collection responses MAY use:

```text
items
page
```

metadata.

The API SHALL avoid deeply redundant envelopes without purpose.

---

# 154. Resource Identity

Canonical resources SHALL expose stable IDs.

User-facing names SHALL not serve as primary API identity.

---

# 155. ID Minting

Where CP owns resource identity, CP SHALL mint canonical IDs according to the relevant domain conventions.

UUIDv7 SHOULD continue to be used where already established by Control Plane architecture.

---

# 156. External IDs

Provider-native IDs SHALL appear only through:

```text
ExternalReference
Mapping
diagnostic projection
```

where appropriate.

They SHALL not replace canonical IDs.

---

# 157. JSON Naming

Canonical JSON SHOULD continue using:

```text
snake_case
```

consistent with current shared contracts.

---

# 158. Timestamps

API timestamps SHALL use timezone-aware RFC 3339-compatible representations.

Authoritative server time SHOULD be UTC.

---

# 159. Display Timezone

Display localisation belongs to the Console.

The API does not rewrite historical timestamps into browser-local time.

---

# 160. Null vs Absent

Contracts SHALL distinguish where relevant:

```text
field absent
```

from:

```text
field explicitly null
```

particularly for partial updates.

---

# 161. Unknown Input Fields

Administrative command payloads SHOULD fail on unexpected fields where doing so protects against:

```text
client typos
stale contracts
mass-assignment vulnerabilities
```

The current Go practice of rejecting unknown fields SHOULD generally be retained.

---

# 162. Unknown Response Fields

Clients SHOULD be tolerant of additive response evolution where the contract permits it.

---

# 163. Request Body Limits

Administrative APIs SHALL enforce bounded request sizes.

Limits MAY vary for:

```text
ordinary JSON
bulk commands
metadata
```

---

# 164. Binary Uploads

Large evidence/document binaries SHALL NOT be sent through ordinary administrative JSON APIs.

Use the evidence-storage architecture defined elsewhere.

---

# 165. File Metadata

CP APIs may accept:

```text
EvidenceReference
```

after controlled upload.

---

# 166. Content Type

Normal administrative JSON requests SHALL use:

```text
application/json
```

unless a more specific standard media type is intentionally chosen.

---

# 167. Errors

Problem responses SHALL use:

```text
application/problem+json
```

---

# 168. Cache Defaults

Sensitive administrative responses SHALL default toward:

```text
Cache-Control: private, no-store
```

unless a specific endpoint has been reviewed for safe caching.

---

# 169. Public Caching Prohibited

Tenant/organisation administrative data SHALL not be publicly cacheable.

---

# 170. BFF Cache Safety

The Console/BFF SHALL not use shared framework caching across administrative principals or tenants without explicit context-safe keys and review.

---

# 171. Conditional GET

Stable administrative resources MAY use:

```text
ETag
If-None-Match
```

to reduce repeated transfer.

This does not authorize shared public caching.

---

# 172. Rate Limiting

Administrative endpoints SHALL support rate and abuse controls appropriate to risk.

Particularly sensitive:

```text
login-related operations
search/export
application submission
invitation
plan generation
bulk Changesets
operation polling
```

---

# 173. Rate Limit Response

When throttled:

```text
429 Too Many Requests
```

SHOULD be used.

`Retry-After` SHOULD be provided where meaningful.

---

# 174. Polling Rate Limits

Operation polling SHALL have sensible client guidance.

The Console SHALL not poll every few milliseconds.

---

# 175. Query Cost Protection

Potentially expensive queries SHALL use:

```text
pagination
bounded filters
time-window limits
rate controls
```

where required.

---

# 176. Audit Export

Large exports SHOULD be modelled as long-running operations.

Example:

```text
POST /v1/admin/audit-exports
       │
       ▼
202
       │
       ▼
Operation
       │
       ▼
controlled result reference
```

---

# 177. Export Result Security

Export results SHALL be:

```text
authorised
time-limited where appropriate
audited
scope-bound
```

---

# 178. Secrets

Administrative APIs SHALL never return raw stored secret values after creation unless an explicitly designed one-time mechanism requires it.

---

# 179. Secret Submission

Secret-bearing requests SHALL use approved secret-management flows.

They SHALL not be echoed in:

```text
response
problem detail
audit
trace
log
```

---

# 180. Integration Credential Response

Prefer:

```json
{
  "secret_reference": "...",
  "status": "configured"
}
```

over:

```json
{
  "secret": "plaintext"
}
```

---

# 181. Administrative Command Authorization

Before executing a command, CP SHALL establish:

```text
principal
administrative permission
administrative scope
resource scope
required assurance
required approval
resource state
```

---

# 182. Authorization Failure

If authority is absent:

```text
DENY
```

regardless of what controls the Console displayed.

---

# 183. Step-Up Required

Where stronger authentication is needed, the API SHALL return a structured problem/decision allowing the BFF to initiate the IAM step-up flow.

Example code:

```text
AUTHENTICATION_ASSURANCE_REQUIRED
```

---

# 184. Approval Required

A command that requires approval SHALL not be accepted as executable merely because the caller can submit it.

The API may return or represent:

```text
APPROVAL_REQUIRED
```

through Changeset state.

---

# 185. Read vs Modify Permissions

Read permissions SHALL be separable from mutation permissions.

Example:

```text
organisation.view
```

does not imply:

```text
organisation.manage
```

---

# 186. List vs View

Broad collection listing MAY require different authority from access to one known resource where security warrants it.

---

# 187. Audit Access

Audit access SHALL be separately authorised.

A tenant administrator SHALL not automatically receive platform-wide security audit.

---

# 188. Diagnostic Access

Diagnostic topology SHALL require explicit diagnostic authority.

---

# 189. API-Level Separation of Duties

Command handlers SHALL consult ADR-BCP-020 policy where:

```text
requester
approver
executor
```

must be distinct.

---

# 190. Changeset Apply

Conceptual request:

```text
POST /v1/admin/changesets/{id}/apply
```

SHALL NOT allow the caller to supply a different plan body.

It executes the approved plan attached to the Changeset.

---

# 191. Approval Command

The request MAY include:

```text
decision reason
comment
```

but authoritative:

```text
approver ID
approval time
plan digest
authority
```

shall be server-derived.

---

# 192. Plan Retrieval

Plans SHALL be read-only once frozen.

Conceptually:

```text
GET /v1/admin/changesets/{id}/plan
```

---

# 193. Impact Retrieval

Impact analysis SHOULD be queryable independently:

```text
GET /v1/admin/changesets/{id}/impact
```

---

# 194. Regeneration

If the plan becomes stale, a command MAY request re-planning.

Example:

```text
POST /v1/admin/changesets/{id}/replan
```

subject to authority and lifecycle.

---

# 195. Replan Produces New Version

Replan SHALL not overwrite the previously approved plan.

---

# 196. Long-Running Apply

`apply` SHOULD normally return:

```text
202 Accepted
Location: /v1/admin/operations/{operation_id}
```

---

# 197. Operation Linkage

The response SHOULD also identify:

```text
changeset_id
plan_id
operation_id
```

---

# 198. Operation Read Model

Example conceptual representation:

```json
{
  "id": "op_...",
  "type": "CHANGESET_APPLY",
  "status": "RUNNING",
  "changeset_id": "chg_...",
  "current_phase": "RECONCILING",
  "progress": {
    "completed_steps": 7,
    "total_steps": 9
  },
  "created_at": "...",
  "updated_at": "...",
  "correlation_id": "...",
  "links": {
    "self": "/v1/admin/operations/...",
    "changeset": "/v1/admin/changesets/..."
  }
}
```

---

# 199. No Secrets in Operation State

Operations SHALL expose enough state to administer safely without exposing:

```text
provider credentials
tokens
confidential payloads
```

---

# 200. Operation Ownership Scope

An administrator MAY retrieve an operation only if their current effective authority permits access to its target scope or a specific operational-audit policy grants access.

---

# 201. Historical Operation Access

Revoking current mutation authority need not erase historical operation visibility if audit policy allows continued read access.

---

# 202. BFF API Client

The CP Console SHALL use a typed API client generated from, or rigorously validated against, the canonical Administrative OpenAPI contract.

---

# 203. No Handwritten Duplicate DTO Universe

The frontend SHALL not independently invent types such as:

```text
FrontendTenant
FrontendOrganisation
FrontendChangeset
```

whose semantics drift from shared contracts.

---

# 204. UI-Specific View Models

The frontend MAY create UI view models derived from API contracts.

Those view models SHALL not become canonical domain definitions.

---

# 205. BFF Endpoint Design

The BFF MAY expose internal same-origin routes to the browser.

It SHALL NOT expose an unrestricted proxy.

---

# 206. Explicit BFF Methods

Preferred:

```text
/api/changesets/create
/api/operations/{id}
```

or framework-equivalent explicit server actions.

Rejected:

```text
/api/proxy?url=http://internal-cp/anything
```

---

# 207. BFF Input Validation

The BFF MAY perform usability validation.

The CP API SHALL independently validate every request.

---

# 208. BFF Authorization

The BFF MAY suppress controls based on known authority.

The CP remains authoritative.

---

# 209. Browser Error Projection

The BFF SHOULD translate canonical problem details into user-safe UX while retaining:

```text
code
correlation_id
retryable
```

for support.

---

# 210. No Raw Internal Errors to Browser

Provider exceptions and stack traces SHALL not pass through the BFF.

---

# 211. Internal API Calls

The BFF-to-CP request SHALL preserve:

```text
trace context
correlation
authenticated principal/delegation context
```

using approved mechanisms.

---

# 212. Causation

When a BFF command triggers a Changeset which later triggers operations/events, causation identifiers SHOULD allow the chain to be reconstructed.

---

# 213. Machine Administrative Clients

Future administrative automation SHALL call the Administrative API through workload identity.

It SHALL not pretend to be a human principal.

---

# 214. Automation Authority

Automation SHALL receive explicit ADR-BCP-020-compatible authority or service-specific administrative permission.

---

# 215. Automation Approval

An automation client SHALL not circumvent Changeset approvals.

It may create or execute only according to change policy.

---

# 216. API Clients Must Not Infer Internal Provider Topology

Ordinary administrative clients SHOULD operate on:

```text
organisation
tenant
market
service
capability
```

rather than hard-coded engine internals.

---

# 217. Advanced Diagnostics

Provider/engine IDs MAY be available to suitably privileged platform operators.

---

# 218. Provider Neutrality

Administrative API contracts SHALL not unnecessarily encode:

```text
Medusa-specific field
iDempiere-specific field
Payload-specific field
```

into generic tenant/platform APIs.

---

# 219. Provider Configuration

Where provider-specific configuration is unavoidable, it SHALL be isolated behind:

```text
provider-specific typed configuration
```

or provider adapter contracts.

---

# 220. Domain Data Boundary

Administrative APIs SHALL NOT expose:

```text
orders
invoices
journal entries
inventory records
CMS content bodies
```

merely because CP knows which engine owns them.

---

# 221. CP Metadata Only

CP Administrative APIs expose platform-governance metadata.

Business-domain data remains with domain APIs.

---

# 222. Readiness APIs

Readiness SHALL be exposed as structured projection.

Conceptually:

```text
GET /v1/admin/tenants/{id}/readiness
```

or:

```text
GET /v1/admin/changesets/{id}/readiness
```

where meaningful.

---

# 223. Readiness Representation

It SHOULD include:

```text
state
required checks
blocking reasons
degraded reasons
evaluated_at
```

without pretending health and readiness are identical.

---

# 224. Drift APIs

Platform operators MAY query:

```text
GET /v1/admin/tenants/{id}/drift
```

or appropriate equivalent.

---

# 225. Drift Is Read Model

The API SHALL not let a caller:

```text
PATCH drift = resolved
```

without actual reconciliation/remediation.

---

# 226. Reconcile Command

Where manual reconciliation is permitted:

```text
POST .../reconcile
```

SHOULD launch a governed operation.

---

# 227. Reconciliation Result

The operation SHALL expose:

```text
observed state
repaired resources
remaining drift
readiness
```

through controlled projection.

---

# 228. Administrative Grant APIs

ADR-BCP-020 MAY result in:

```text
GET /v1/admin/administrative-grants
GET /v1/admin/administrative-grants/{id}
```

and governed grant/delegation commands.

---

# 229. Privilege Granting as Changeset

High-risk administrative grants SHOULD normally be changed through ADR-BCP-021 Changesets rather than direct mutable CRUD.

---

# 230. Low-Risk Delegation

Where policy permits lower-risk customer delegation, the API MAY expose direct governed commands.

The same authorization and audit principles still apply.

---

# 231. Support Session API

Future conceptual routes MAY include:

```text
POST /v1/admin/support-sessions
GET  /v1/admin/support-sessions/{id}
POST /v1/admin/support-sessions/{id}/terminate
```

according to ADR-BCP-020.

---

# 232. Break-Glass API

Emergency privilege activation SHALL have a clearly distinct command family.

It SHALL NOT be hidden inside ordinary role update endpoints.

---

# 233. Break-Glass Visibility

Responses SHALL indicate when:

```text
elevated / break-glass authority
```

is active for the principal where necessary for the Console.

---

# 234. API Security Headers

Administrative HTTP responses SHOULD retain appropriate defensive headers already present in CP.

The BFF additionally governs browser-specific security policy.

---

# 235. CORS

The CP Administrative API SHOULD not become broadly cross-origin accessible.

Preferred production architecture uses BFF-mediated same-origin browser access.

---

# 236. Direct Browser CORS

If direct browser API use is introduced in the future, it requires explicit security review.

It SHALL not emerge accidentally from:

```text
Access-Control-Allow-Origin: *
```

---

# 237. CSRF

CSRF protection primarily belongs at the cookie/session BFF boundary.

The CP API still SHALL validate authenticated authorization independently.

---

# 238. Token Audience

Administrative access tokens SHALL have an audience appropriate for the Control Plane.

Tokens intended for unrelated engines SHALL not automatically authorize CP administration.

---

# 239. Token Lifetime

The API SHALL follow IAM policy for short-lived access credentials.

The Administrative API SHALL not define competing credential lifetime policy.

---

# 240. Authentication Failure

Expired or invalid credentials SHALL return:

```text
401
```

rather than disguising authentication failure as a domain error.

---

# 241. Authorization Failure

Valid identity without sufficient authority normally returns:

```text
403
```

unless existence concealment requires `404`.

---

# 242. Session Expiry During Console Use

The BFF SHALL reauthenticate/refresh according to IAM policy.

The CP API does not retain browser session authority.

---

# 243. Long Operation Authentication

A long-running operation does not need the user's browser token to remain valid throughout execution.

Execution proceeds under server-side workload authority while preserving initiating-principal attribution.

---

# 244. Operation Commands Still Require Current Authority

Later commands such as:

```text
cancel
retry
```

require current authority at the time they are invoked.

---

# 245. API Audit

Every consequential command SHALL emit or persist structured audit evidence.

---

# 246. Audit Metadata

Audit SHOULD include:

```text
principal
actor type
client
permission decision
target
command
idempotency key reference/hash
correlation
causation
result
resource revision
Changeset
Operation
```

as appropriate.

---

# 247. Do Not Log Raw Idempotency Keys

The existing shared security policy says:

```text
log_keys: false
```

This SHALL remain.

Use safe references or hashes where operational correlation is required.

---

# 248. Request Logging

Administrative request logs SHALL avoid complete request bodies by default because they may contain:

```text
organisation evidence
personal information
integration metadata
```

---

# 249. Audit vs Request Log

A request log is not sufficient evidence that a business/domain command succeeded.

Audit SHALL record authoritative outcome.

---

# 250. API Telemetry

Metrics SHOULD include:

```text
request count
latency
status code
problem code
operation type
operation duration
rate limiting
stale precondition rate
idempotent replay rate
```

without high-cardinality unsafe labels.

---

# 251. Tenant IDs in Metrics

Tenant/organisation identifiers SHOULD not automatically become high-cardinality metric dimensions.

Use logs/traces/audit for detailed attribution.

---

# 252. Tracing

Distributed traces MAY contain canonical IDs where lawful and operationally justified.

Secrets SHALL never be trace attributes.

---

# 253. API SLO Separation

Runtime and Administrative APIs SHOULD have separate service objectives.

Reason:

```text
runtime resolution affects live business traffic

administrative API affects change/control workflows
```

---

# 254. Console Outage

A Console outage SHALL not imply runtime API outage.

---

# 255. Administrative API Outage

An Administrative API outage SHOULD not break existing tenant runtime capability resolution.

This preserves ADR-BCP-019's availability boundary.

---

# 256. Runtime API Priority

Infrastructure MAY prioritise runtime resolution traffic differently from administrative work under severe load.

---

# 257. Expensive Administrative Jobs

Large administrative operations SHOULD execute asynchronously rather than consume HTTP worker capacity.

---

# 258. Operation Queue Pressure

If the system cannot safely accept more asynchronous work:

```text
503
```

or an appropriate throttling response SHOULD be returned instead of silently accepting work that cannot be serviced.

---

# 259. Backpressure

Administrative clients SHALL honour backpressure.

---

# 260. API Documentation

Every administrative operation SHALL document:

```text
purpose
authorization
scope semantics
idempotency
concurrency
possible asynchronous behaviour
success response
problem codes
```

---

# 261. Problem Code Registry

Baobab SHOULD maintain a canonical problem-code registry.

Examples:

```text
AUTH_TOKEN_REQUIRED
AUTH_TOKEN_INVALID
AUTHORIZATION_DENIED

VALIDATION_FAILED
RESOURCE_NOT_FOUND

IF_MATCH_REQUIRED
VERSION_CONFLICT

IDEMPOTENCY_KEY_REUSED

APPROVAL_REQUIRED
SEPARATION_OF_DUTIES_VIOLATION

PLAN_STALE
PLAN_DIGEST_MISMATCH

OPERATION_NOT_CANCELLABLE
OPERATION_FAILED

DEPENDENCY_UNAVAILABLE
RATE_LIMITED
```

---

# 262. Problem Types Are Stable Contracts

Problem codes SHALL not be casually renamed because frontend/client logic may depend on them.

---

# 263. Internal Error

Unknown internal failure SHOULD produce:

```text
INTERNAL_ERROR
```

or a more precise safe code.

It SHALL not expose implementation details.

---

# 264. Service Unavailable

Known temporary dependency failure SHOULD produce a specific safe code where possible:

```text
IAM_UNAVAILABLE
PROVIDER_UNAVAILABLE
DATABASE_UNAVAILABLE
```

if exposure is operationally safe.

---

# 265. Validation Vocabulary

Validation errors SHOULD be sufficiently stable for frontend field mapping.

---

# 266. Field Pointer

Validation `field` SHOULD use a consistent field path or JSON Pointer convention.

---

# 267. Localisation

Machine-readable codes remain language-neutral.

Human-facing detail MAY later be localised by the Console.

---

# 268. API Language

The API SHOULD avoid deeply UI-specific prose.

Return semantic data and stable codes.

The Console decides wording appropriate to non-technical users.

---

# 269. Change Planning Response

A query retrieving a plan SHALL include structured impact capable of being rendered into:

```text
Business View
Administrative Detail
Technical Detail
```

as required by ADR-BCP-019.

---

# 270. Hyperlinks

Administrative representations MAY include canonical links where they improve navigation.

Example:

```json
{
  "links": {
    "self": "...",
    "operation": "...",
    "changeset": "..."
  }
}
```

---

# 271. No Full HATEOAS Requirement

This ADR does not mandate full hypermedia-driven REST architecture.

Links are pragmatic navigational metadata.

---

# 272. Resource Names

Path segments SHALL use stable business/platform nouns.

Avoid leaking Go package/internal implementation names.

---

# 273. Query Parameter Naming

Query parameters SHOULD follow consistent:

```text
snake_case
```

or the established canonical contract convention.

Consistency SHALL take precedence over stylistic preference.

---

# 274. Header Naming

Standard HTTP headers SHALL be preferred where semantics already exist.

Examples:

```text
If-Match
ETag
Location
Retry-After
```

Baobab-specific headers SHALL be limited to genuine platform requirements such as correlation/causation.

---

# 275. No Custom Version Header for Concurrency

Do not invent:

```text
X-Baobab-Version
```

where standard:

```text
ETag / If-Match
```

is sufficient.

---

# 276. No Custom Async Status Header

Long-running state SHALL live in the Operation resource, not:

```text
X-Job-Status
```

headers.

---

# 277. Health Endpoints

Process health remains:

```text
/healthz
/readyz
```

outside ordinary administrative resources.

---

# 278. Health Endpoints Are Infrastructure Signals

They SHALL NOT expose customer data or detailed internal topology.

---

# 279. Platform Readiness ≠ Process Readiness

These SHALL remain distinct:

```text
/readyz
```

means:

> Is this CP process ready to serve?

Whereas:

```text
/v1/admin/tenants/{id}/readiness
```

means:

> Is this tenant/platform configuration ready?

---

# 280. Contract Tests

`baobab-cp` SHALL maintain tests demonstrating implementation compatibility with `shared`.

---

# 281. Generated Client Tests

The frontend's generated/typed client SHOULD be regenerated or validated in CI whenever Administrative OpenAPI changes.

---

# 282. Breaking Contract Detection

CI SHALL detect accidental incompatible contract changes.

---

# 283. Schema Drift

Go structs and frontend types SHALL not silently diverge from shared schemas.

---

# 284. Consumer-Driven Tests

Where valuable, critical consumers MAY maintain compatibility fixtures or contract tests.

They SHALL not redefine the canonical contract.

---

# 285. Mock API

A generated/mock administrative API MAY support frontend development.

Mocks SHALL be contract-derived.

---

# 286. Mock Does Not Define Reality

A frontend mock SHALL not become the place where missing backend semantics are invented.

---

# 287. Migration From Existing Routes

Migration SHALL be incremental.

Existing routes SHALL be classified:

```text
RUNTIME_STABLE
ADMIN_RETAIN
ADMIN_SUPERSEDE
DIAGNOSTIC
DEPRECATED
```

---

# 288. Existing Runtime Routes

Routes such as:

```text
/v1/platform-context/resolve
/v1/capabilities/resolve
```

SHOULD remain unchanged unless runtime architecture itself changes.

---

# 289. Existing Tenant Creation

Existing:

```text
POST /v1/tenants
```

MAY remain supported for internal provisioning compatibility.

New human onboarding SHOULD flow through:

```text
Application
→ Changeset
→ Plan
→ Operation
```

rather than directly calling tenant creation from the browser.

---

# 290. Existing Provisioning API

Existing tenant provisioning endpoints SHALL be reconciled with ADR-BCP-021.

The long-term model SHOULD convert:

```text
HTTP request drives orchestrator as far as possible
```

into:

```text
HTTP request creates/starts durable Operation
        │
        ▼
server-side worker/orchestrator proceeds
        │
        ▼
caller polls Operation
```

---

# 291. No Browser-Driven Orchestration

The browser or BFF SHALL NOT be required to repeatedly invoke:

```text
apply next phase
apply next phase
apply next phase
```

to keep server-side orchestration alive.

---

# 292. Existing Provisioning Readiness

Existing readiness and drift data remain valuable and SHOULD become projections behind the Administrative API.

---

# 293. Existing Canonical-Entity API

Canonical entity lifecycle endpoints MAY remain for platform/internal clients.

The Console SHOULD generally operate through organisation/application workflows defined by ADR-BCP-017/018.

---

# 294. Existing Market API

Direct market CRUD/lifecycle endpoints SHOULD be evaluated against Changeset governance.

High-impact production market activation SHOULD eventually require governed Changeset semantics.

---

# 295. Existing Mapping API

Low-level mapping administration SHOULD remain primarily platform-operator functionality.

It SHALL not become a normal customer-facing Console abstraction.

---

# 296. Migration of `approved_by`

Existing contract fields such as:

```text
approved_by
```

SHALL be reviewed.

Where they describe the actual authenticated actor, they SHALL be removed from client authority and derived server-side.

---

# 297. Migration of Error Codes

Implementation error codes SHALL be reconciled with the shared uppercase canonical schema.

---

# 298. Migration of Old Organisation References

The API/contract audit SHALL remove stale organisational naming such as:

```text
baobab-platform/shared
github.com/baobab-platform/...
docs.nabhold.com
```

where repository/domain renaming has made them obsolete.

This SHALL be done carefully because Go module-path migration may affect imports and release compatibility.

---

# 299. Migration Is Not Blind Search/Replace

Repository/module/package identifiers SHALL be corrected according to their technical semantics.

Do not rewrite historical references or immutable artefacts merely because the organisation was renamed.

---

# 300. Administrative API Security Tests

Required scenarios SHALL include:

| Scenario | Expected |
|---|---|
| Missing token | 401 |
| Valid token, no admin grant | 403/404 according to concealment policy |
| Cross-tenant known ID | No resource disclosure |
| Forged `approved_by` body | Ignored/rejected; actor server-derived |
| Missing required If-Match | 428 |
| Stale If-Match | 412 |
| Invalid state transition | 409 |
| Same idempotency key + same request | Original logical result |
| Same key + different request | 409 |
| Duplicate async apply | Same logical Operation |
| Invalid page token | 400 |
| Excessive page size | Bounded/rejected |
| Unauthorised search | No cross-scope results |
| Cancel completed operation | Conflict |
| Operation poll by wrong organisation | 404/deny without leakage |
| Provider error | Safe Problem Details |
| Stack trace attempted | Never returned |
| BFF changes organisation ID | CP reauthorises scope |
| Console hides action but API called directly | Backend denies |
| Rate limit exceeded | 429 |
| Dependency unavailable | 503 |
| Large unsafe body | Rejected |
| Unknown JSON field | Rejected where strict contract applies |

---

# 301. Administrative API Performance Tests

Testing SHOULD include:

```text
large organisation collections
audit pagination
operation polling
parallel authorised users
concurrent Changesets
idempotent replay contention
ETag contention
```

---

# 302. Runtime Isolation Tests

Administrative load SHALL not materially degrade capability-resolution SLOs.

---

# 303. API Fuzzing

Structured request parsers and sensitive command endpoints SHOULD undergo fuzz/property testing where useful.

---

# 304. Problem Contract Tests

Every documented problem response SHALL conform to:

```text
contracts/errors/v1/problem-details.schema.json
```

---

# 305. OpenAPI Completeness

Production administrative endpoints SHALL not remain undocumented indefinitely.

OpenAPI coverage SHALL be part of Definition of Done.

---

# 306. Idempotency Contract Tests

CI SHALL test:

```text
replay
collision
concurrency
fingerprint mismatch
retention assumptions
```

for consequential commands.

---

# 307. Concurrency Contract Tests

CI SHALL test:

```text
ETag emitted
If-Match required
stale update returns 412
missing precondition returns 428
```

where applicable.

---

# 308. Long-Running Operation Tests

CI SHALL test:

```text
202 acceptance
Location
durable operation
server restart
retry
cancellation
partial failure
compensation
terminal result
```

---

# 309. Frontend Contract Tests

The CP Console SHALL test:

```text
401 reauthentication
403 permission UX
404 concealed-resource UX
409 conflict UX
412 stale-resource UX
428 precondition UX
422 validation UX
429 backoff UX
503 temporary failure UX
operation polling
```

---

# 310. Operation Retention

Completed operations SHALL be retained long enough for:

```text
audit
support
customer status review
incident investigation
```

according to retention policy.

---

# 311. Operation Is Not Permanent Audit Store

Long-term authoritative audit remains ADR-BCP-008's responsibility.

Operation retention and audit retention MAY differ.

---

# 312. Operation Archival

Older operation detail MAY eventually be compacted or archived while retaining audit evidence.

---

# 313. API Deprecation

Deprecated endpoints SHALL receive:

```text
documentation
replacement path
migration guidance
reasonable transition window
```

before removal.

---

# 314. No Silent Semantic Deprecation

An endpoint SHALL not retain the same route while quietly changing its meaning incompatibly.

---

# 315. Internal Endpoints

Implementation-only endpoints MAY exist.

They SHALL not be accidentally published as stable public administrative contracts.

---

# 316. Debug Endpoints

Debug interfaces SHALL not be enabled broadly in production.

---

# 317. API Threat Model

The Administrative API threat model SHALL explicitly cover:

```text
BOLA / cross-tenant object access
privilege escalation
mass assignment
replay
stale update
idempotency collision
SSRF through BFF
excessive data exposure
resource enumeration
bulk abuse
operation hijacking
error leakage
```

---

# 318. BOLA Defence

Every resource lookup SHALL include or verify authorised scope.

Possessing a valid resource ID does not imply authority.

---

# 319. Mass Assignment Defence

Request DTOs SHALL expose only fields clients are permitted to control.

Do not decode arbitrary persisted domain objects directly when that allows modification of server-controlled fields.

---

# 320. Server-Controlled Fields

Examples include:

```text
created_by
approved_by
risk_class where policy-derived
readiness
revision
lifecycle state
canonical authority
audit metadata
```

---

# 321. Resource Enumeration

Predictability of IDs SHALL not substitute for authorization.

UUIDv7 or any other identifier remains only an identifier.

---

# 322. Error Timing

The implementation SHOULD avoid obvious cross-tenant information leaks through divergent errors where practical.

---

# 323. API Documentation Security

Examples in documentation SHALL use fictitious data.

They SHALL never include production credentials or real private customer data.

---

# 324. Future External Administrative API

If Baobab later exposes administrative APIs directly to customers or partners, the same contracts MAY be reused with:

```text
stricter scopes
rate limits
commercial policy
API management
```

rather than building an unrelated customer-management API.

---

# 325. Future SDKs

SDKs MAY be generated for:

```text
TypeScript
Go
Python
```

where customer/automation demand justifies them.

This ADR does not mandate SDK production.

---

# 326. Future GraphQL

GraphQL is NOT required for CP administration.

The existing contract-first HTTP API remains sufficient.

A GraphQL administrative API would require demonstrated need and a separate architecture decision.

---

# 327. Future gRPC

gRPC MAY remain suitable for selected internal service interactions.

It SHALL not replace the human/BFF Administrative HTTP API without a separate decision.

---

# 328. Future Event Interface

Administrative lifecycle events MAY complement the HTTP API.

Events SHALL NOT replace authoritative command authorization.

---

# 329. API Does Not Equal Event Bus

A Changeset command produces authoritative CP state.

Events communicate consequences.

Those responsibilities remain distinct.

---

# 330. Implementation Programme

Implementation SHALL proceed in controlled gates.

## Gate AAPI-00 — Current Surface Audit

Inventory:

```text
router routes
handlers
OpenAPI
shared contracts
error codes
authentication
role checks
idempotency
ETags
If-Match
pagination
provisioning
diagnostics
```

Classify each route:

```text
RUNTIME
ADMIN
DIAGNOSTIC
HEALTH
DEPRECATED
```

Also identify stale organisation-name references without blindly modifying historical artefacts.

---

## Gate AAPI-01 — Administrative Contract Foundation

Introduce canonical shared schemas for:

```text
Administrative pagination
Operation
Collection metadata
Administrative decision/problem extensions
```

and extend OpenAPI modularly.

---

## Gate AAPI-02 — Authorization Adapter

Replace new Administrative API handlers' dependency on broad realm roles with ADR-BCP-020 authority evaluation.

Legacy routes MAY retain compatibility logic temporarily.

---

## Gate AAPI-03 — Problem Contract Alignment

Standardise:

```text
UPPER_SNAKE_CASE codes
problem namespaces
status mapping
retryable semantics
validation error structure
```

across CP.

---

## Gate AAPI-04 — Concurrency

Standardise:

```text
ETag
If-Match
412
428
resource revisions
```

across mutable administrative resources.

---

## Gate AAPI-05 — Idempotency

Generalise shared idempotency scope beyond tenant-only operations and implement consistent command handling.

---

## Gate AAPI-06 — Pagination and Query Standards

Introduce:

```text
opaque cursor pagination
filter allow-lists
stable ordering
bounded page sizes
```

for collections.

---

## Gate AAPI-07 — Generic Operation Model

Introduce durable canonical:

```text
Operation
OperationStatus
OperationResult
OperationProblem
```

and API retrieval.

---

## Gate AAPI-08 — Async Provisioning Migration

Adapt existing provisioning so:

```text
HTTP handler accepts
        │
        ▼
durable operation starts
        │
        ▼
server orchestration proceeds independently
```

instead of requiring request-bound execution.

---

## Gate AAPI-09 — Changeset API

Expose ADR-BCP-021:

```text
create
submit
plan
impact
approve
reject
apply
cancel
observe
```

through Administrative API.

---

## Gate AAPI-10 — Organisation / Application Queries

Implement the read models required by ADR-BCP-019's applicant and organisation administration experiences.

---

## Gate AAPI-11 — Administration APIs

Expose ADR-BCP-020 administrative grant/delegation projections and permitted commands.

---

## Gate AAPI-12 — Audit / Readiness / Drift

Provide properly scoped query APIs over existing operational governance data.

---

## Gate AAPI-13 — Typed Frontend Client

Generate or validate the CP Console server client against canonical OpenAPI.

Remove handwritten semantic duplication.

---

## Gate AAPI-14 — Security Hardening

Complete:

```text
BOLA testing
scope tampering
mass assignment
rate limiting
problem leakage
idempotency abuse
pagination abuse
operation hijacking
cross-tenant enumeration
```

---

## Gate AAPI-15 — Compatibility and Deprecation

Identify legacy administrative routes eligible for:

```text
retain
delegate internally
deprecate
remove in future major version
```

without breaking runtime consumers.

---

# 331. Definition of Done

ADR-BCP-022 SHALL be considered implemented when:

| Requirement | Required |
|---|---:|
| Runtime/Admin families explicitly separated | Yes |
| `/v1/admin` contract family established | Yes |
| Existing runtime APIs preserved | Yes |
| ADR-BCP-020 authorization integrated | Yes |
| ADR-BCP-021 Changeset API available | Yes |
| Generic long-running Operation implemented | Yes |
| `202 + Location` used for async commands | Yes |
| Idempotency consistent | Yes |
| ETag/If-Match consistent | Yes |
| 412/428 semantics implemented | Yes |
| RFC 9457 Problem Details consistent | Yes |
| Uppercase canonical problem codes | Yes |
| Cursor pagination implemented | Yes |
| Cross-tenant query filtering server-side | Yes |
| Actor identity server-derived | Yes |
| BFF uses typed contract | Yes |
| Provisioning no longer request-bound | Yes |
| OpenAPI compatibility tests | Yes |
| Security negative tests | Yes |
| Operation restart recovery | Yes |
| Audit correlation | Yes |

---

# 332. Alternatives Considered

## Alternative A — Keep All APIs Mixed Under `/v1`

**Rejected for new administrative surfaces.**

It becomes increasingly difficult to distinguish:

```text
runtime contract
administrative contract
privilege expectations
latency/SLO expectations
```

Existing stable routes are preserved, but new administration receives an explicit namespace.

---

## Alternative B — Separate Administrative Microservice

**Rejected.**

The Administrative API operates over the same Control Plane bounded context and domain authority.

Creating:

```text
baobab-admin-service
```

would duplicate:

```text
authorization
domain semantics
persistence
Changeset logic
```

without justification.

---

## Alternative C — Let Next.js Access PostgreSQL

**Rejected by ADR-BCP-019.**

It would bypass:

```text
domain invariants
authorization
audit
idempotency
Changesets
```

---

## Alternative D — Browser Calls Go API Directly

**Rejected as the primary privileged Console architecture.**

ADR-BCP-019 establishes the BFF.

---

## Alternative E — Keep Long Operations Synchronous

**Rejected.**

Provisioning and distributed Changesets may outlive ordinary HTTP requests and survive process/browser restarts.

---

## Alternative F — Job IDs Without Resource Model

**Rejected.**

A bare:

```text
job_id
```

without canonical:

```text
state
scope
problem
result
audit
links
```

is insufficient.

---

## Alternative G — Offset Pagination Everywhere

**Rejected as the default.**

Mutable high-volume administrative collections benefit from opaque cursor semantics.

---

## Alternative H — Custom Error JSON

**Rejected.**

The repository already has an RFC 9457-aligned contract.

---

## Alternative I — Client-Supplied `approved_by`

**Rejected.**

Actor identity must come from authenticated authority.

---

## Alternative J — Broad Keycloak Roles as Final Authorization

**Rejected by ADR-BCP-020.**

IAM scopes/roles provide coarse access.

CP owns contextual administrative authority.

---

# 333. Positive Consequences

This architecture provides:

```text
clear runtime/admin separation
stronger security boundary
consistent Console integration
contract-generated clients
safe long-running operations
restart-safe administration
consistent errors
safe concurrency
consistent idempotency
server-side tenant scoping
scalable pagination
clean Changeset integration
future automation support
better auditability
```

---

# 334. Negative Consequences

It introduces additional implementation work:

```text
new Administrative routes
common Operation model
cursor infrastructure
contract migration
authorization adapter
problem-code cleanup
async worker/orchestrator changes
legacy compatibility
```

These costs are accepted because they eliminate a much more dangerous long-term cost:

```text
each screen inventing its own API semantics.
```

---

# 335. Relationship With ADR-BCP-007

ADR-BCP-007 remains authoritative for:

```text
runtime context resolution
capability resolution
workload consumption
provider selection
```

ADR-BCP-022 governs:

```text
human/administrative platform management
```

The two coexist:

```text
ADR-BCP-007
Runtime API

ADR-BCP-022
Administrative API
```

---

# 336. Relationship With ADR-BCP-019

ADR-BCP-019 defines:

```text
CP Console / BFF
```

ADR-BCP-022 defines:

```text
what the BFF is allowed to call
and
what semantics those APIs expose.
```

---

# 337. Relationship With ADR-BCP-020

ADR-BCP-020 answers:

```text
Who may administer what?
```

ADR-BCP-022 ensures every Administrative API endpoint evaluates that authority consistently.

---

# 338. Relationship With ADR-BCP-021

ADR-BCP-021 defines:

```text
Changeset
ImpactAnalysis
Plan
Approval
Execution
```

ADR-BCP-022 defines their external administrative contract.

---

# 339. Relationship With `shared`

The final contract chain SHALL be:

```text
Architecture Decision
        │
        ▼
baobab-platform/shared
        │
        ├── OpenAPI
        ├── JSON Schema
        ├── Problem Details
        ├── Idempotency Policy
        └── Security Contracts
        │
        ▼
baobab-platform/baobab-cp
        │
        ▼
CP Console typed client
        │
        ▼
Administrative UX
```

---

# 340. Final Architectural Invariants

The following SHALL remain non-negotiable:

```text
Runtime API
    != Administrative API

Administrative API
    != direct database access

BFF
    != Control Plane authority

Authenticated actor
    != authorised administrator

IAM scope
    != final CP authority

Query
    != Command

Command
    != arbitrary field mutation

Lifecycle state
    != freely editable property

202 Accepted
    != operation success

Operation success
    != readiness

Idempotency
    != concurrency control

ETag
    != authorization

Resource ID
    != authority

Cross-tenant known ID
    != data visibility

Frontend-hidden action
    != security control

Client-supplied actor
    != authoritative actor

Problem detail text
    != machine contract

Provider exception
    != public API error

Pagination
    must be bounded

Long-running operation
    must be durable

Administrative mutation
    must be auditable

Existing runtime compatibility
    must not be broken for cosmetic API symmetry
```

---

# 341. Final Decision

Baobab SHALL expose a dedicated, contract-first **Administrative API** from the existing Go Control Plane.

New administrative routes SHOULD use:

```text
/v1/admin/
```

while existing stable runtime resolution APIs remain compatible.

Administrative API design SHALL be built around:

```text
Resources
+
Queries
+
Domain Commands
+
Changesets
+
Durable Operations
+
Administrative Authority
+
Optimistic Concurrency
+
Idempotency
+
Problem Details
+
Audit Correlation
```

Long-running administrative work SHALL use:

```text
Command
   │
   ▼
202 Accepted
   │
   ▼
Durable Operation
   │
   ▼
Server-side Execution
   │
   ▼
Reconciliation / Verification
   │
   ▼
Terminal Result
```

Administrative collections SHALL be:

```text
scope-filtered
cursor-paginated
bounded
deterministically ordered
```

Mutable resources SHALL use:

```text
ETag
+
If-Match
```

where stale updates must be prevented.

Administrative errors SHALL consistently use the existing Baobab RFC 9457-compatible:

```text
application/problem+json
```

contract.

Actor identity SHALL be derived from authenticated canonical principals, never trusted from client-supplied fields such as:

```text
approved_by
```

The strategic outcome is:

> **The Control Plane gains one coherent administrative contract that can safely serve the CP Console, platform operators, customer administrators and future administrative automation without contaminating the runtime capability-resolution plane or exposing internal persistence and engine topology as the public programming model.**

The final API discipline is:

> **Queries explain platform state. Commands express authorised intent. Changesets govern consequential change. Operations track work that outlives the request. Reconciliation establishes whether reality matches desired state. The API exposes these concepts faithfully instead of making clients reconstruct them from low-level CRUD.**