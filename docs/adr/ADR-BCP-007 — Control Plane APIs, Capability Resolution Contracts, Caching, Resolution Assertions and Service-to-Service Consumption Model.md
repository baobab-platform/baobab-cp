# ADR-BCP-007 — Control Plane APIs, Capability Resolution Contracts, Caching, Resolution Assertions and Service-to-Service Consumption Model

**Status:** Proposed — Normative Platform Architecture  
**Date:** 2026-09-11  
**Decision Owners:** NABHOLD / Baobab Platform Architecture  
**Repository:** `baobab-platform/baobab-cp`  
**Runtime Authority:** `baobab-platform/baobab-cp`  
**Contract Authority:** `baobab-platform/shared`  
**Identity Authority:** `baobab-platform/baobab-iam`

**Depends On:**

- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
- ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
- ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model
- ADR-BCP-006 — Capability Provider Lifecycle, Engine Topology, Health, Failover and Migration Model
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model
- Applicable Baobab IAM ADRs

**Applies To:** Control Plane APIs, context resolution, capability resolution, batch resolution, authorization boundaries, workload identity, caching, invalidation, resolution assertions, service-to-service calls, Digital Estate consumption and provider invocation.

**Architecture Style:** Contract-first, capability-centric, fail-closed, context-aware, stateless API tier, bounded caching, zero-trust service communication.

**Decision Type:** Foundational runtime interaction architecture

---

# 1. Executive Decision

`baobab-cp` SHALL expose stable platform APIs that answer:

> **Given an authenticated principal or workload, a tenant/legal-entity/Digital-Estate context, and a requested capability, is that capability available, and if so, which provider is authorised to fulfil it under the current context?**

The Control Plane SHALL return a **resolution decision**.

It SHALL NOT normally proxy the resulting business request.

The governing runtime pattern SHALL therefore be:

```text
Consumer
   │
   │ 1. authenticate
   ▼
Baobab IAM
   │
   │ identity / workload token
   ▼
Consumer
   │
   │ 2. request capability resolution
   ▼
Baobab CP
   │
   │ 3. authoritative resolution decision
   ▼
Consumer / Domain Boundary
   │
   │ 4. invoke selected capability provider
   ▼
Capability Provider
   │
   │ 5. domain authorization
   ▼
Business Operation
```

The architectural rule is:

> **The Control Plane decides whether and where a capability may execute; the data plane executes it.**

---

# 2. Why This ADR Is Necessary

The preceding ADRs establish:

```text
Capability
Grant
Scope
Binding
Provider
EngineInstance
Context
Product
Digital Estate
```

However, those concepts require a precise runtime contract.

Without one, different repositories could independently invent:

```text
tenant headers
estate headers
engine URLs
provider selection
authorization conventions
caching
fallback behaviour
resolution payloads
```

That would destroy the capability-centric architecture.

This ADR therefore establishes one canonical resolution boundary.

---

# 3. Fundamental Runtime Separation

Baobab SHALL distinguish:

```text
CONTROL PLANE
     │
     │ decides
     ▼
DATA PLANE
     │
     │ executes
     ▼
BUSINESS DOMAIN
```

The Control Plane is responsible for:

```text
context
entitlement
capability
scope
provider resolution
topology selection
platform-level authorization
```

The data plane is responsible for carrying the actual business request.

The domain provider is responsible for business behaviour and domain authorization.

---

# 4. CP SHALL NOT Become the Universal Proxy

The following architecture is rejected:

```text
Digital Estate
     │
     ▼
Baobab CP
     │
     ▼
Every business API
     │
     ▼
Every engine
```

If all business traffic flows through CP, CP becomes:

```text
API gateway
ESB
reverse proxy
integration hub
business bottleneck
```

That is not its role.

---

# 5. Preferred Runtime Architecture

```text
                    ┌─────────────────────┐
                    │    Baobab IAM       │
                    │     Keycloak        │
                    └─────────┬───────────┘
                              │
                              │ token
                              ▼
┌────────────────┐     ┌─────────────────────┐
│ Digital Estate │────►│ Estate API / BFF    │
└────────────────┘     └─────────┬───────────┘
                                 │
                    ┌────────────┴────────────┐
                    │                         │
                    ▼                         ▼
             ┌──────────────┐        ┌─────────────────┐
             │  Baobab CP   │        │ Domain Provider │
             │              │        │                 │
             │ context      │        │ Trade / ERP /   │
             │ grants       │        │ CMS / Pulse     │
             │ bindings     │        │                 │
             │ resolution   │        │ business work   │
             └──────────────┘        └─────────────────┘
```

The Estate API/BFF or domain service SHALL normally mediate browser-originating interactions.

---

# 6. Browser Trust Boundary

Browsers SHALL NOT be trusted to declare authoritative:

```text
tenant_id
legal_entity_id
provider_id
engine_instance_id
isolation_profile
capability grant
```

A browser MAY supply contextual hints such as requested estate/market/channel where appropriate.

Those values SHALL be validated and canonicalised.

---

# 7. Digital Estate Consumption Principle

Digital Estates SHALL consume Baobab through stable domain/capability-facing interfaces.

They SHOULD NOT contain knowledge such as:

```text
"call Medusa"
"call iDempiere"
"call Haystack"
"call Payload"
```

Instead:

```text
commerce.order.create
finance.invoice.read
intelligence.fx.query
content.page.read
```

represent stable platform intent.

---

# 8. API Families

The Control Plane SHOULD expose logically distinct API families:

```text
Identity/Context
Capability Registry
Capability Resolution
Product/Entitlement
Topology
Administration
Diagnostics
Readiness
```

Runtime consumers SHALL receive only the API access necessary for their role.

---

# 9. Runtime APIs vs Administrative APIs

These SHALL be separated.

Runtime examples:

```text
POST /v1/context/resolve
POST /v1/capabilities/resolve
POST /v1/capabilities/resolve-batch
```

Administrative examples:

```text
POST /v1/capability-grants
POST /v1/capability-bindings
POST /v1/providers
POST /v1/subscriptions
```

Administrative APIs SHALL require stronger privileges.

---

# 10. Context Resolution API

Conceptually:

```text
POST /v1/context/resolve
```

The caller supplies the minimum necessary contextual request.

CP combines it with authenticated identity and canonical platform state.

---

# 11. Context Request

Conceptual contract:

```text
ContextResolutionRequest
├── tenant_hint
├── legal_entity_hint
├── digital_estate_hint
├── digital_property_hint
├── channel_hint
├── market_hint
├── country_hint
├── currency_hint
├── locale_hint
├── operation_scope
└── correlation_id
```

The word `hint` is intentional.

Caller-supplied identifiers SHALL not automatically become authoritative context.

---

# 12. Authenticated Principal

Principal identity SHALL come from validated IAM credentials.

Conceptually:

```text
Principal
├── subject
├── client_id / workload_id
├── authentication assurance
├── coarse scopes
└── token provenance
```

CP SHALL map this into canonical identity/context.

---

# 13. Resolved Context

Conceptually:

```text
ResolvedContext
├── context_id
├── principal_id
├── tenant_id
├── legal_entity_id
├── digital_estate_id
├── digital_property_id
├── channel_id
├── market_id
├── jurisdiction
├── country_code
├── currency_code
├── locale
├── deployment_region
├── environment
├── isolation_profile_id
├── correlation_id
├── resolved_at
├── expires_at
└── provenance
```

Not every field must be populated for every request.

---

# 14. Context Immutability

Once returned for a resolution decision, context SHALL be treated as immutable.

A new context produces a new resolution.

The consumer SHALL NOT modify:

```text
tenant
legal entity
market
estate
isolation
```

inside a previously resolved context.

---

# 15. Context Provenance

Each important context field SHOULD retain provenance.

Examples:

```text
IAM_CLAIM
CANONICAL_MAPPING
TENANT_CONFIGURATION
ESTATE_CONFIGURATION
REQUEST_HINT
POLICY_DEFAULT
```

This supports diagnostics and security review.

---

# 16. Capability Resolution API

Primary runtime API:

```text
POST /v1/capabilities/resolve
```

It SHALL answer whether one capability can be consumed in the requested context.

---

# 17. Capability Resolution Request

Conceptually:

```text
CapabilityResolutionRequest
├── capability_key
├── required_contract_version
├── context_request
├── operation_scope
├── invocation_mode
├── correlation_id
└── request_metadata
```

The request SHALL not select the provider.

---

# 18. Forbidden Caller Selection

Normal consumers SHALL NOT send:

```text
provider_id = medusa
engine_instance_id = xyz
```

to force routing.

That would bypass the Control Plane.

Privileged diagnostics or migration APIs MAY explicitly inspect providers but are separate.

---

# 19. Capability Resolution Output

Conceptually:

```text
CapabilityResolution
├── resolution_id
├── decision
├── reason_code
├── context
├── capability
├── capability_contract_version
├── grant_reference
├── binding_reference
├── provider_reference
├── engine_reference
├── engine_instance_reference
├── invocation
├── isolation
├── residency
├── resolved_at
├── expires_at
├── correlation_id
└── provenance
```

---

# 20. Resolution Decision

Initial values SHOULD be:

```text
ALLOW
DENY
```

Operational diagnostics MAY additionally expose:

```text
DEGRADED
```

as readiness information, but the authorization decision itself SHOULD remain unambiguous.

---

# 21. Reason Codes

Resolution SHALL use machine-readable reason codes.

Examples:

```text
CAPABILITY_ALLOWED

AUTHENTICATION_REQUIRED
PRINCIPAL_DISABLED
TENANT_UNKNOWN
TENANT_INACTIVE
LEGAL_ENTITY_INVALID
ESTATE_INVALID

CAPABILITY_UNKNOWN
CAPABILITY_INACTIVE
CAPABILITY_NOT_GRANTED
CAPABILITY_DEPENDENCY_UNSATISFIED

BINDING_NOT_FOUND
BINDING_AMBIGUOUS

PROVIDER_UNAVAILABLE
PROVIDER_INCOMPATIBLE
CONTRACT_VERSION_UNSUPPORTED

ISOLATION_MISMATCH
RESIDENCY_MISMATCH
REGION_UNAVAILABLE

CONTEXT_INVALID
CONTEXT_AMBIGUOUS
```

---

# 22. Do Not Leak Sensitive Diagnostics

External consumers SHOULD receive enough reason information to behave correctly.

Detailed internal topology SHALL not be exposed unnecessarily.

For example:

```text
PROVIDER_UNAVAILABLE
```

may be appropriate externally.

Internal diagnostics may additionally know:

```text
engine instance X failed health check Y
```

---

# 23. Invocation Descriptor

A successful resolution MAY contain an invocation descriptor.

Conceptually:

```text
InvocationDescriptor
├── service_reference
├── route_reference
├── protocol
├── contract_version
├── provider_id
├── engine_instance_id
└── metadata
```

It SHALL not contain secrets.

---

# 24. Prefer Logical Service References

Where practical:

```text
service://baobab-trade/orders
```

or an equivalent routing abstraction is preferable to exposing physical infrastructure addresses.

The exact mechanism MAY be implemented by:

```text
API gateway
service discovery
service mesh
internal DNS
```

without changing the capability contract.

---

# 25. CP Does Not Carry Provider Credentials

Resolution output SHALL NOT include:

```text
passwords
API keys
database credentials
client secrets
private keys
```

Workloads obtain credentials through IAM/workload identity or secret-management infrastructure.

---

# 26. Service-to-Service Authentication

All Baobab service-to-service communication SHALL use authenticated workload identity.

Conceptually:

```text
Service A
   │
   │ workload credential
   ▼
Baobab IAM
   │
   ▼
short-lived service token
   │
   ▼
Service B
```

Static long-lived shared secrets SHOULD be avoided.

---

# 27. Service Identity

A workload token SHOULD identify:

```text
workload subject
client identity
audience
coarse scopes
authentication mechanism
issuer
expiry
```

It SHALL not attempt to encode all business permissions.

---

# 28. Audience Restriction

Tokens SHALL be audience-restricted.

A token issued for:

```text
baobab-cp
```

SHALL not automatically be valid for:

```text
baobab-erp
```

unless the IAM design explicitly permits it.

---

# 29. User Delegation

Some service-to-service calls occur on behalf of a user.

The platform SHALL distinguish:

```text
CALLER WORKLOAD
```

from:

```text
END-USER PRINCIPAL
```

where both matter.

---

# 30. Delegated Context

Conceptually:

```text
Request
├── authenticated workload
└── delegated principal
```

The provider SHALL know enough to enforce domain authorization without trusting arbitrary forwarded headers.

---

# 31. No Identity Header Trust

Headers such as:

```text
X-User-ID
X-Tenant-ID
X-Legal-Entity-ID
```

SHALL not become trusted merely because they exist.

If such headers are used internally, they SHALL be cryptographically or network-bound to a trusted intermediary and reconstructed from validated context.

---

# 32. Capability Invocation Sequence

```text
Estate API
   │
   │ token + requested operation
   ▼
Resolve Context
   │
   ▼
Resolve Capability
   │
   ├── DENY ─────────────► stop
   │
   ▼
Receive provider decision
   │
   ▼
Acquire/forward appropriate workload identity
   │
   ▼
Invoke provider
   │
   ▼
Provider validates identity
   │
   ▼
Provider performs domain authorization
   │
   ▼
Business operation
```

---

# 33. CP Resolution Is Platform Authorization

A successful CP resolution means:

> The platform permits this principal/workload to attempt this capability in this context through this provider.

It does NOT mean:

> The business operation itself is approved.

---

# 34. Domain Authorization Example

CP:

```text
finance.invoice.issue
→ ALLOW
→ iDempiere provider
```

ERP may still deny because:

```text
user lacks accounting role
period is closed
invoice state invalid
approval missing
```

This is correct.

---

# 35. Batch Resolution

A Digital Estate often needs several capabilities.

Therefore CP SHOULD support:

```text
POST /v1/capabilities/resolve-batch
```

---

# 36. Batch Request

Conceptually:

```text
BatchCapabilityResolutionRequest
├── context_request
├── capabilities[]
└── correlation_id
```

Each requested capability SHALL produce an independent decision.

---

# 37. Batch Result

```text
BatchCapabilityResolution
├── context
├── decisions[]
└── resolved_at
```

Example:

```text
commerce.catalogue.read   ALLOW
commerce.order.create     ALLOW
finance.invoice.issue     DENY
intelligence.fx.query     ALLOW
```

---

# 38. Batch Atomicity

Batch resolution SHALL NOT imply transactional atomicity.

It is an efficient decision query.

Business operations remain separate.

---

# 39. Estate Bootstrap Resolution

A Digital Estate MAY use batch resolution during bootstrap to determine available experiences.

Example:

```text
Supplier Portal
   │
   ▼
resolve:
 supplier.profile.read
 supplier.application.create
 supplier.document.upload
 supplier.approval.manage
```

UI features may then be rendered appropriately.

---

# 40. UI Hiding Is Not Authorization

Even if a UI hides unavailable features:

```text
backend authorization remains mandatory
```

A malicious caller must not gain access by manually invoking hidden endpoints.

---

# 41. Resolution Cache

CP MAY cache successful resolution decisions.

Caching is necessary for:

```text
latency
availability
database load
high-frequency capability checks
```

but it is security-sensitive.

---

# 42. Cache Principle

> **A cache may accelerate an authorization decision; it may never broaden one.**

---

# 43. Cache Key

A resolution cache key SHALL contain every dimension that can alter the result.

Conceptually:

```text
principal/workload identity
tenant
legal entity
Digital Estate
channel
market
jurisdiction
currency where relevant
environment
deployment region
isolation profile
capability
contract version
operation-scope hash
policy/configuration version
```

---

# 44. Cache Key Omission Is a Security Defect

If market affects a binding but is omitted from the key:

```text
ZA resolution
```

could incorrectly be reused for:

```text
UG resolution
```

This SHALL be treated as a security defect.

---

# 45. Positive Cache

Successful:

```text
ALLOW
```

decisions MAY be cached for a bounded period.

TTL SHALL reflect revocation requirements.

---

# 46. Negative Cache

Certain denials MAY also be cached briefly.

Examples:

```text
CAPABILITY_NOT_GRANTED
CAPABILITY_UNKNOWN
```

Negative caching SHALL have shorter or carefully governed TTLs to avoid delaying legitimate activation.

---

# 47. Cache TTL

No universal TTL SHALL be hard-coded into architecture.

TTL MAY depend on:

```text
capability criticality
grant volatility
provider health volatility
tenant lifecycle
security policy
```

---

# 48. Security-Sensitive Capabilities

Capabilities such as:

```text
payment authorization
financial posting
privileged administration
```

MAY require:

```text
short TTL
or
no reusable resolution cache
```

depending on risk analysis.

---

# 49. Cache Invalidation Sources

Affected cache entries SHALL be invalidated or safely expire when relevant state changes:

```text
principal revoked
membership revoked
tenant suspended
legal entity disabled
estate suspended

capability disabled
grant revoked
grant changed

binding changed
provider suspended
provider health invalidated
engine instance drained

isolation changed
residency policy changed
contract compatibility changed
```

---

# 50. Event-Driven Invalidation

Canonical lifecycle events SHOULD drive distributed invalidation.

Conceptually:

```text
Grant Revoked
     │
     ▼
Canonical Event
     │
     ├──► CP cache
     ├──► gateway cache
     └──► participating service caches
```

---

# 51. Event Delivery Is Not Instantaneous

Because distributed events are not instantaneous, TTL SHALL remain bounded.

Critical revocation MAY require synchronous invalidation mechanisms.

---

# 52. Revocation Latency Budget

Security policy SHALL define maximum acceptable stale-authorisation windows for capability classes.

Example conceptual classes:

```text
CRITICAL
HIGH
STANDARD
LOW
```

The exact values belong to security policy.

---

# 53. Cache Failure

Cache outage SHALL not automatically make authorization permissive.

The resolver SHOULD fall back to authoritative evaluation where possible.

If authoritative evaluation cannot be performed for a security-critical operation:

```text
FAIL CLOSED
```

---

# 54. Cache Poisoning Protection

Cache content SHALL not be derived from unvalidated caller-provided tenant/provider information.

Cache namespaces SHALL be protected against cross-tenant collisions.

---

# 55. Local vs Distributed Cache

The implementation MAY use:

```text
in-process cache
distributed cache
or both
```

provided consistency and revocation requirements are met.

Architecture SHALL not depend on a specific cache product.

---

# 56. Context Cache

Resolved contexts MAY be cached separately from capability resolutions.

However:

```text
Context Cache
≠
Capability Cache
```

Context validity does not imply capability entitlement validity.

---

# 57. Provider Health Cache

Provider health SHOULD have separate freshness semantics.

A long-lived capability resolution SHALL not continue selecting a provider based on indefinitely stale health.

---

# 58. Resolution Versioning

Each resolution SHOULD carry enough version/provenance information to determine which control-plane state produced it.

Examples:

```text
grant version
binding version
policy version
capability contract version
context version
```

---

# 59. Resolution ID

Every successful or denied resolution SHOULD receive a unique:

```text
resolution_id
```

for audit and diagnostics.

---

# 60. Correlation ID

Correlation SHALL propagate:

```text
Digital Estate
→ Estate API
→ CP
→ Provider
→ downstream domain calls
```

This enables end-to-end tracing.

---

# 61. Resolution Assertions

Baobab MAY eventually support a short-lived cryptographically signed representation of a capability resolution.

This is called a:

```text
Resolution Assertion
```

It SHALL NOT be implemented casually.

---

# 62. Purpose of Resolution Assertions

A resolution assertion MAY reduce repeated synchronous CP calls.

Conceptually:

```text
CP
 │
 │ signed short-lived decision
 ▼
Consumer
 │
 ▼
Provider
```

The provider validates the assertion without contacting CP for every invocation.

---

# 63. Resolution Assertion Is Not an IAM Token

This distinction is mandatory.

```text
IAM Token
    proves identity/authentication

Resolution Assertion
    proves a recent CP capability decision
```

Neither replaces the other.

---

# 64. Resolution Assertion Contents

A future assertion MAY contain:

```text
issuer
resolution_id
principal/workload reference
tenant_id
legal_entity_id
estate_id
capability
contract_version
provider_id
engine_instance_id
scope digest
issued_at
expires_at
correlation_id
policy version
```

It SHALL contain only necessary information.

---

# 65. Signed Assertion Security

If implemented, assertions SHALL require:

```text
asymmetric signing
key rotation
audience restriction
short lifetime
replay analysis
revocation analysis
clock-skew policy
issuer verification
```

---

# 66. Separate Security ADR Requirement

Signed resolution assertions SHALL NOT become production-authoritative until their security model is approved under ADR-BCP-009 or a dedicated child ADR.

---

# 67. Assertions SHALL Not Become Role Containers

The following is prohibited:

```text
Resolution Assertion
├── thousands of permissions
├── every business role
└── every workflow privilege
```

That would reproduce IAM token inflation.

Assertions SHALL remain narrow capability decisions.

---

# 68. Assertion Audience

An assertion for:

```text
baobab-trade
```

SHALL not automatically authorize:

```text
baobab-erp
```

Audience/provider restriction SHALL be explicit.

---

# 69. Assertion Expiry

Assertions SHALL be short-lived.

Long-lived assertions would undermine:

```text
grant revocation
tenant suspension
provider migration
binding changes
```

---

# 70. Provider Verification

A provider receiving an assertion SHALL verify:

```text
signature
issuer
audience
expiry
capability
provider identity
scope
```

before considering it.

---

# 71. Provider Still Performs Domain Authorization

Even with a valid assertion:

```text
Domain authorization remains mandatory.
```

---

# 72. Service-to-Service Consumption Modes

Baobab SHALL support three conceptual consumption modes.

### Mode A — Synchronous resolution

```text
Service
  │
  ▼
CP
  │
  ▼
Provider
```

Default and simplest.

### Mode B — Cached resolution

```text
Service
  │
  ▼
Safe local/distributed cache
  │
  ├── hit → Provider
  └── miss → CP
```

### Mode C — Signed resolution assertion

```text
Service
  │
  ▼
CP assertion
  │
  ▼
Provider verifies assertion
```

Mode C is future/conditional.

---

# 73. Default Mode

Initial production implementation SHOULD prefer:

```text
Synchronous resolution
+
bounded safe caching
```

before introducing signed assertions.

This keeps the security model simpler.

---

# 74. Availability Concern

CP SHALL be highly available because it is a critical control-plane dependency.

However, the architecture SHALL avoid making every business operation require multiple CP round trips.

Batching and safe caching are therefore first-class.

---

# 75. CP Outage Behaviour

Outage behaviour SHALL depend on capability risk.

Potential policy:

```text
Low-risk read capability
→ previously cached unexpired resolution may continue

High-risk write capability
→ no valid decision = deny
```

The exact policy SHALL be explicit.

---

# 76. No Indefinite Offline Authorization

A Digital Estate SHALL not continue using an old capability decision indefinitely because CP is unavailable.

All cached decisions require expiry.

---

# 77. Read vs Write Capabilities

Capability definitions SHOULD distinguish operational characteristics where useful:

```text
READ
WRITE
ADMINISTRATIVE
```

This MAY influence caching and outage policy.

It SHALL not replace domain semantics.

---

# 78. Idempotency

CP mutation APIs SHALL support idempotency where duplicate requests are dangerous.

Examples:

```text
create subscription
create grant
create binding
start provisioning
```

---

# 79. Idempotency Key

Clients MAY provide:

```text
Idempotency-Key
```

or equivalent canonical contract.

Keys SHALL be scoped appropriately to caller and operation.

---

# 80. Resolution Requests Are Naturally Idempotent

Given the same:

```text
authoritative state
context
capability
resolution time
```

resolution SHOULD produce semantically equivalent decisions.

This is another reason resolution must be deterministic.

---

# 81. Concurrency

Administrative resources SHALL use optimistic concurrency.

For example:

```text
version
ETag
If-Match
```

may prevent lost updates.

---

# 82. Binding Race Example

Two administrators attempt to make different providers PRIMARY.

Without concurrency control:

```text
ambiguous bindings
```

could result.

CP SHALL detect and reject invalid concurrent state.

---

# 83. Contract-First APIs

All externally consumed CP APIs SHALL be defined contract-first in `baobab-platform/shared`.

Runtime implementation SHALL conform to those contracts.

---

# 84. OpenAPI

HTTP APIs SHALL be defined using the project's authoritative OpenAPI standard.

Generated clients MAY be produced from the canonical contract.

---

# 85. AsyncAPI

Lifecycle/event contracts SHALL be defined through AsyncAPI where applicable.

Examples:

```text
capability.grant.revoked
capability.binding.changed
provider.health.changed
tenant.suspended
subscription.changed
```

---

# 86. API Versioning

Breaking API changes SHALL require explicit version evolution.

The platform SHALL avoid uncontrolled:

```text
/v2
/v3
/v4
```

proliferation by maintaining backward-compatible contracts where reasonable.

---

# 87. Capability Contract Version vs CP API Version

These are distinct:

```text
CP API version
≠
Capability contract version
```

Example:

```text
/v1/capabilities/resolve
```

may resolve:

```text
commerce.order.create@2.0
```

---

# 88. Error Envelope

Shared SHALL define a canonical CP error envelope.

Conceptually:

```text
Error
├── code
├── message
├── correlation_id
├── resolution_id
├── retryable
└── details
```

Sensitive internal details SHALL be omitted from external responses.

---

# 89. Retry Semantics

Errors SHALL indicate whether retry is appropriate.

Examples:

```text
PROVIDER_TEMPORARILY_UNAVAILABLE
retryable = true

CAPABILITY_NOT_GRANTED
retryable = false
```

---

# 90. Retry Safety

Clients SHALL not retry mutating business operations blindly merely because provider invocation failed.

Domain APIs must define idempotency.

CP retryability refers to resolution, not automatically to business execution.

---

# 91. Timeouts

All synchronous CP calls SHALL have bounded timeouts.

A hung resolver SHALL not consume application resources indefinitely.

---

# 92. Deadline Propagation

Where appropriate, callers SHOULD propagate request deadlines.

CP SHOULD respect the remaining deadline budget.

---

# 93. Circuit Breaking

Consumers MAY use circuit breakers around CP and providers.

Circuit breakers SHALL not convert denial or policy failures into success.

---

# 94. Rate Limiting

CP SHALL support rate controls for:

```text
runtime resolution
administrative APIs
diagnostic APIs
```

with different policies.

---

# 95. Tenant Fairness

One tenant SHALL not be able to exhaust CP resolution capacity for all others.

Rate limits and resource isolation SHOULD support tenant fairness.

---

# 96. Abuse Protection

Administrative and resolution APIs SHALL be protected from:

```text
enumeration
brute-force context discovery
tenant probing
provider topology discovery
```

---

# 97. Internal API Does Not Mean Trusted API

Even internal network calls SHALL be authenticated and authorized.

The platform SHALL not rely solely on:

```text
"inside the VPC"
```

as trust.

---

# 98. Resolution Data Minimisation

CP SHALL return only the information required for invocation and audit.

It SHALL not expose the entire tenant, provider or topology record.

---

# 99. Tenant Isolation

A caller resolving a capability for Tenant A SHALL never receive:

```text
Tenant B grants
Tenant B bindings
Tenant B provider configuration
Tenant B estate information
```

even through diagnostics or errors.

---

# 100. Cross-Legal-Entity Isolation

Within a NABHOLD tenant structure or other future complex tenant:

```text
Legal Entity A
```

SHALL not automatically inherit:

```text
Legal Entity B
```

capability scope.

This is particularly important for:

```text
ZuriBeans
Thamani
```

which remain separate legal entities and operational contexts.

---

# 101. Shared Engine Does Not Mean Shared Authorization

If ZuriBeans and Thamani both resolve to the same physical Medusa cluster:

```text
their capability decisions remain independent.
```

---

# 102. Example — ZuriBeans Order

```text
ZuriBeans Buyer
      │
      ▼
ZuriBeans Digital Estate
      │
      ▼
Estate API
      │
      ├── IAM token validation
      │
      ▼
CP:
tenant = ZuriBeans
legal entity = ZuriBeans
estate = ZuriBeans B2B
market = ZA
capability = commerce.order.create
      │
      ▼
Grant?
YES
      │
      ▼
Binding?
baobab-trade.medusa
      │
      ▼
Provider eligible?
YES
      │
      ▼
ALLOW
      │
      ▼
Trade API
      │
      ▼
B2B business authorization
      │
      ▼
Create order
```

---

# 103. Example — Thamani

```text
Thamani Customer
      │
      ▼
Thamani Estate
      │
      ▼
commerce.checkout.execute
      │
      ▼
CP
      │
      ▼
Thamani-specific grant + scope
      │
      ▼
Medusa provider
```

Even if the provider technology is the same as ZuriBeans:

```text
tenant context
legal entity
channel
catalogue
inventory
pricing
orders
```

remain independent.

---

# 104. Example — XBT Coal Haulier

```text
Haulier Operations Estate
      │
      ▼
logistics.dispatch.manage
      │
      ▼
CP
      │
      ▼
future logistics provider
```

The estate SHALL not need to know whether the provider is:

```text
Baobab-native
external SaaS
future open-source engine
```

---

# 105. Example — Provider Migration

Before:

```text
commerce.order.create
      │
      ▼
Medusa Provider
```

After:

```text
commerce.order.create
      │
      ▼
Replacement Provider
```

The estate contract remains:

```text
commerce.order.create
```

---

# 106. Provider Direct Access

Provider APIs MAY physically be reachable internally.

But logical access SHALL require valid:

```text
workload identity
tenant context
platform entitlement evidence where required
domain authorization
```

A caller SHALL not bypass CP simply because it discovers an internal endpoint.

---

# 107. Enforcement Point

Depending on architecture, CP resolution evidence MAY be enforced by:

```text
Estate API
Domain API
API gateway
provider adapter
provider itself
```

The enforcement boundary SHALL be explicit for every capability.

---

# 108. Gateway Role

An API gateway MAY enforce:

```text
authentication
coarse scopes
routing
rate limiting
resolution assertion verification
```

but SHALL not become the authoritative capability registry.

CP remains authority.

---

# 109. Service Mesh Role

A service mesh MAY provide:

```text
mTLS
service identity
traffic policy
telemetry
```

but SHALL not decide commercial entitlement.

---

# 110. IAM Role

IAM answers:

```text
Who is this?
How did they authenticate?
What coarse scope does this token have?
```

CP answers:

```text
Which tenant/legal entity/estate?
Which capability?
Is it granted?
Which provider?
```

Domain answers:

```text
May this principal perform this business operation now?
```

---

# 111. Complete Authorization Chain

```text
IAM
 │
 │ authenticated identity
 ▼
CP
 │
 │ platform entitlement + provider
 ▼
Domain
 │
 │ business authorization
 ▼
Operation
```

All three layers SHALL remain independently enforceable.

---

# 112. Resolution Snapshot

For audit-sensitive operations, the provider or calling domain SHOULD preserve the:

```text
resolution_id
correlation_id
```

with the business transaction where appropriate.

This permits later reconstruction of platform routing decisions.

---

# 113. CP Does Not Need Business Payload

Capability resolution SHOULD normally not require:

```text
entire order
invoice contents
supplier application
shipment document
```

Only the operation-scope attributes necessary for resolution should be provided.

---

# 114. Sensitive Scope Attributes

If resolution requires sensitive attributes, they SHALL be minimised and classified.

CP SHALL not become an accidental copy of domain data.

---

# 115. Operation Scope

Conceptually:

```text
OperationScope
├── operating_market
├── origin_market
├── destination_market
├── jurisdictions[]
├── currency
├── customer_segment
├── attributes
└── scope_version
```

Only applicable dimensions SHALL be supplied.

---

# 116. Operation Scope Validation

Operation scope SHALL be validated against:

```text
ResolvedContext
CapabilityScope
BindingScope
```

Contradictory values SHALL fail.

---

# 117. Context vs Transaction

Example:

```text
ResolvedContext:
Tenant = ZuriBeans
Estate = B2B portal

OperationScope:
Origin = Uganda
Destination = South Africa
Currency = USD
```

CP uses this for provider resolution.

The actual shipment/trade remains owned by the domain.

---

# 118. Context Reuse

A resolved context MAY be reused for several related capability resolutions while still valid.

But changing:

```text
legal entity
estate
market
channel
```

requires re-resolution where relevant.

---

# 119. Resolution Freshness

Consumers SHALL inspect:

```text
expires_at
```

and SHALL not use expired resolutions.

---

# 120. Clock Policy

All services SHALL use UTC for authoritative timestamps.

Clock synchronization SHALL be operationally maintained.

Clock skew tolerance SHALL be explicit for signed tokens/assertions.

---

# 121. Diagnostics API

Privileged operators SHOULD have access to explainability APIs.

Example:

```text
POST /v1/capabilities/explain
```

---

# 122. Explain Output

An explanation MAY show:

```text
context resolution
candidate grants
grant selected
candidate bindings
bindings rejected
provider eligibility
selected provider
reason codes
```

without changing runtime state.

---

# 123. Explainability Is Critical

When a tenant cannot use a capability, operators SHOULD be able to answer:

> Why?

without manually querying several databases.

---

# 124. Explain Security

Explain APIs expose sensitive topology.

They SHALL require privileged administrative access.

Tenant-scoped explain endpoints MAY expose a reduced view.

---

# 125. Dry-Run Resolution

Administrative tooling MAY support:

```text
What would resolve if context were X?
```

This SHALL be clearly marked non-authoritative and SHALL not permit arbitrary impersonation without privilege.

---

# 126. Historical Explain

Future tooling MAY permit:

```text
Why did resolution R choose provider P yesterday?
```

using immutable audit data.

This is highly desirable for regulated operations.

---

# 127. Availability Target

CP resolution is a critical platform function.

Production architecture SHALL therefore provide:

```text
multiple stateless CP instances
health checks
load balancing
PostgreSQL HA
bounded cache
safe deployment
backup/recovery
```

Exact infrastructure topology belongs to deployment architecture.

---

# 128. Stateless API Tier

CP HTTP/API instances SHOULD remain stateless apart from bounded caches.

Authoritative state remains in PostgreSQL and approved supporting stores.

---

# 129. Horizontal Scaling

Resolution endpoints SHALL support horizontal scaling.

No instance-local state may be required for correctness unless replicated or safely reconstructible.

---

# 130. Database Dependency

PostgreSQL remains authoritative.

Caches and projections SHALL not silently become alternate authorities.

---

# 131. Event Dependency

Runtime authorization SHALL not require an event bus to be currently available for every request.

Events support:

```text
invalidation
reconciliation
audit propagation
desired-state processing
```

but synchronous authoritative resolution must remain coherent independently.

---

# 132. Outbox Requirement

Control-plane state changes that emit canonical events SHALL use transactional outbox semantics.

Conceptually:

```text
DB transaction
├── state change
└── outbox record
       │
       ▼
event publisher
```

This prevents state/event divergence.

---

# 133. Event Consumer Idempotency

Consumers SHALL tolerate duplicate event delivery.

Canonical events SHALL have stable event identifiers.

---

# 134. Resolution Performance Budget

The implementation SHALL define and test explicit latency objectives for:

```text
context resolution
single capability resolution
batch resolution
cached resolution
```

Performance SHALL be measured, not assumed.

---

# 135. Avoid N+1 Resolution

A page requiring twenty capabilities SHOULD NOT make twenty sequential CP requests.

Use:

```text
batch resolution
```

or an appropriate safe cached estate capability manifest.

---

# 136. Capability Manifest

CP MAY expose a scoped capability manifest for an estate.

Conceptually:

```text
GET /v1/estates/{estate}/capabilities
```

It MAY return capabilities available to the current principal/context.

---

# 137. Manifest Is Not Permanent Entitlement

Capability manifests SHALL have:

```text
context
expiry
version
```

They SHALL not be cached indefinitely by clients.

---

# 138. Browser Capability Manifest

A browser MAY receive a reduced manifest for UX composition.

It SHALL not include:

```text
engine topology
binding IDs
sensitive provider information
internal policies
```

---

# 139. BFF Capability Manifest

A trusted BFF MAY receive richer information than a browser, subject to workload authorization.

---

# 140. Capability Discovery

Consumers SHOULD be able to discover supported canonical capability keys and contract versions through controlled APIs or generated SDKs.

Runtime discovery SHALL not replace compile-time contract governance.

---

# 141. SDKs

`baobab-platform/shared` MAY generate:

```text
Go SDK
TypeScript SDK
Java SDK
Python SDK where needed
```

from canonical contracts.

Repositories SHOULD prefer generated/approved SDKs over hand-written divergent DTOs.

---

# 142. No Shared Runtime Dependency

Generated contracts/SDKs do not turn `baobab-platform/shared` into a runtime service.

Shared remains contract authority.

---

# 143. API Compatibility Testing

CI SHALL verify that CP implementation conforms to Shared OpenAPI/AsyncAPI schemas.

Breaking divergence SHALL fail checks.

---

# 144. Consumer Contract Testing

Key consumers SHOULD maintain contract tests against:

```text
context resolution
capability resolution
error envelope
event contracts
```

---

# 145. Provider Contract Testing

Providers SHALL demonstrate conformance with capability contracts they advertise.

A provider SHALL not be registered as supporting a capability merely because its API looks similar.

---

# 146. Failure Semantics

Resolution failures SHALL be classified into:

```text
AUTHENTICATION
AUTHORIZATION
CONTEXT
ENTITLEMENT
CONFIGURATION
TOPOLOGY
PROVIDER
CONTRACT
TRANSIENT
INTERNAL
```

This supports correct handling and observability.

---

# 147. HTTP Semantics

Exact mappings SHALL be defined in Shared.

Conceptually:

```text
401 → authentication absent/invalid
403 → authenticated but not entitled/authorized
404 → resource intentionally undiscoverable
409 → ambiguous/conflicting control-plane state
422 → semantically invalid context/request
429 → rate limited
503 → required platform/provider state unavailable
```

Sensitive-resource enumeration concerns MAY alter specific mappings.

---

# 148. Fail Closed

The resolver SHALL deny or fail when:

```text
identity cannot be trusted
tenant cannot be established
grant cannot be established
binding cannot be uniquely resolved
required provider cannot be trusted
residency cannot be satisfied
isolation cannot be satisfied
```

No “best guess” routing is allowed.

---

# 149. No Hidden Default Tenant

There SHALL be no production behaviour equivalent to:

```text
if tenant missing:
    tenant = default
```

---

# 150. No Hidden Default Provider

Likewise:

```text
if binding missing:
    use Medusa
```

is prohibited.

---

# 151. No Ambiguous Fallback

If two equally eligible fallbacks remain after deterministic rules:

```text
FAIL
```

rather than choose arbitrarily.

---

# 152. Canonical Runtime Flow

```text
REQUEST
  │
  ▼
Authenticate workload/principal
  │
  ├── invalid ───────────────► DENY
  ▼
Resolve canonical identity
  │
  ▼
Resolve tenant
  │
  ▼
Resolve legal entity
  │
  ▼
Resolve Digital Estate/channel
  │
  ▼
Resolve platform context
  │
  ▼
Validate requested capability
  │
  ▼
Evaluate grant
  │
  ├── absent ────────────────► DENY
  ▼
Validate dependencies
  │
  ▼
Resolve bindings
  │
  ▼
Evaluate provider eligibility
  │
  ▼
Exactly one valid resolution?
  │
  ├── none ──────────────────► FAIL CLOSED
  │
  ├── ambiguous ─────────────► FAIL CLOSED
  ▼
Create resolution decision
  │
  ▼
Cache safely if permitted
  │
  ▼
Return invocation descriptor
  │
  ▼
Provider invocation
  │
  ▼
Domain authorization
  │
  ▼
BUSINESS RESULT
```

---

# 153. Trust Boundary Diagram

```text
┌───────────────────────────────────────────────┐
│                UNTRUSTED EDGE                 │
│                                               │
│ Browser / Mobile / External Client            │
└──────────────────────┬────────────────────────┘
                       │
                       ▼
┌───────────────────────────────────────────────┐
│             DIGITAL ESTATE BOUNDARY            │
│                                               │
│ BFF / Estate API                              │
│ validates user/session                        │
└───────────────┬───────────────────────────────┘
                │ authenticated workload
                ▼
┌───────────────────────────────────────────────┐
│               PLATFORM CONTROL                │
│                                               │
│ IAM → identity                                │
│ CP  → context + entitlement + resolution      │
└───────────────┬───────────────────────────────┘
                │ authorised invocation
                ▼
┌───────────────────────────────────────────────┐
│                DOMAIN BOUNDARY                │
│                                               │
│ Trade / ERP / CMS / Pulse / Future Providers  │
│ domain authorization + business execution     │
└───────────────────────────────────────────────┘
```

---

# 154. Responsibility Matrix

| Concern | IAM | CP | Estate/BFF | Provider/Domain | Shared |
|---|---|---|---|---|---|
| Authentication | Authority | Validate | Validate/use | Validate/use | Contract |
| Canonical tenant context | Input only | **Authority** | Request/use | Consume | Contract |
| Capability definition | No | Runtime registry | Consume | Implement | **Contract authority** |
| Capability entitlement | No | **Authority** | Consume | Verify evidence | Contract |
| Provider selection | No | **Authority** | Consume | No | Contract |
| Business authorization | No | No | Partial UX only | **Authority** | Contract |
| Business execution | No | No | Initiate | **Authority** | No |
| Provider topology | No | **Authority** | No | Register/observe | Contract |
| Runtime routing transport | No | Decision only | Invoke | Receive | Contract |
| Canonical schemas | No | Implement | Implement | Implement | **Authority** |

---

# 155. Resolution Contract Invariants

The following SHALL be true:

1. Every resolution has an authenticated caller.
2. Every resolution has an explicit capability.
3. Tenant context is canonical.
4. Legal-entity context is validated where applicable.
5. Digital Estate context is validated where applicable.
6. Entitlement is represented by grants.
7. Bindings do not create entitlement.
8. Provider selection is deterministic.
9. Provider selection is contract-compatible.
10. Provider selection respects isolation.
11. Provider selection respects residency.
12. Provider health/lifecycle are evaluated according to policy.
13. Ambiguity fails closed.
14. Resolution output contains no secrets.
15. Resolution is time-bounded.
16. Resolution is auditable.
17. Provider invocation does not eliminate domain authorization.
18. Digital Estates do not select engines.
19. Browser-provided tenant headers are not authoritative.
20. Cache reuse never broadens authorization.

---

# 156. Persistence Requirements

This ADR does not require CP to persist every runtime resolution permanently in the transactional database.

Instead:

```text
configuration state
→ PostgreSQL

resolution audit
→ appropriate audit/observability storage

cache
→ ephemeral

signed assertion
→ normally self-contained + auditable reference
```

---

# 157. Resolution Snapshot Persistence

Security-sensitive or regulated operations MAY require durable resolution snapshots.

This SHALL be policy-driven rather than applied indiscriminately to every read request.

---

# 158. Data Volume

Resolution APIs may become some of the highest-volume CP operations.

The implementation SHALL avoid storing excessive synchronous audit payloads in a way that makes PostgreSQL a performance bottleneck.

Audit design is addressed further by ADR-BCP-008.

---

# 159. Migration from Current APIs

Because Baobab does not yet have production data, existing CP APIs MAY be remodelled rather than permanently wrapped for compatibility.

The migration SHALL:

```text
inventory existing endpoints
identify consumers
map old contracts to canonical contracts
update Shared
update consumers
remove obsolete endpoints
remove provider-specific assumptions
regenerate SDKs
update tests
```

---

# 160. Compatibility Policy During Remodel

Temporary adapters MAY exist during migration.

They SHALL be:

```text
documented
time-bounded
deprecated
tested
removed after consumer migration
```

They SHALL not become permanent architecture accidentally.

---

# 161. Implementation Gates

## Gate 0 — API and Consumer Audit

Inspect current:

```text
CP routes
OpenAPI
Shared contracts
context APIs
capability APIs
tenant headers
consumer repositories
IAM integration
caches
provider calls
```

Produce a contract/conflict matrix.

---

## Gate 1 — Canonical Shared Runtime Contracts

Define:

```text
ContextResolutionRequest
ResolvedContext
CapabilityResolutionRequest
CapabilityResolution
BatchCapabilityResolution
InvocationDescriptor
OperationScope
ReasonCode
ErrorEnvelope
```

---

## Gate 2 — Context API Remodel

Implement canonical:

```text
POST /v1/context/resolve
```

with strict provenance and fail-closed behaviour.

---

## Gate 3 — Single Capability Resolution

Implement:

```text
POST /v1/capabilities/resolve
```

against the canonical grant/binding/provider model.

---

## Gate 4 — Batch Resolution

Implement efficient multi-capability resolution.

---

## Gate 5 — Invocation Descriptor

Remove direct engine assumptions and return stable provider invocation information.

---

## Gate 6 — IAM Workload Integration

Implement:

```text
token validation
audience
workload identity
coarse scopes
delegated principal handling
```

consistent with IAM ADRs.

---

## Gate 7 — Safe Resolution Cache

Implement:

```text
complete security-sensitive keys
bounded TTL
positive/negative policies
invalidation
metrics
```

---

## Gate 8 — Lifecycle Invalidation

Integrate:

```text
grant
tenant
estate
provider
binding
identity
```

lifecycle changes with cache invalidation.

---

## Gate 9 — Estate/BFF Reference Integration

Implement one complete Digital Estate reference path.

ZuriBeans SHOULD be the principal B2B/cross-border reference.

---

## Gate 10 — Contrasting Reference Integration

Implement Thamani B2C or another materially different estate to expose hidden B2B assumptions.

---

## Gate 11 — Provider Enforcement

Ensure domain provider boundaries validate appropriate workload identity/context evidence.

---

## Gate 12 — Explainability

Implement privileged resolution explanation.

---

## Gate 13 — Contract and SDK Generation

Generate/update supported language contracts/SDKs from Shared.

---

## Gate 14 — Outbox and Invalidation Events

Complete transactional outbox/event publication required for lifecycle propagation.

---

## Gate 15 — Resilience

Test:

```text
CP instance loss
cache loss
database transient failure
event-bus outage
provider failure
stale health
binding change
grant revocation
tenant suspension
```

---

## Gate 16 — Security Testing

Test:

```text
tenant header spoofing
legal-entity spoofing
estate spoofing
provider forcing
cross-tenant cache collision
stale grant reuse
expired resolution reuse
audience confusion
workload impersonation
ambiguous binding
residency bypass
isolation bypass
```

---

## Gate 17 — Performance Testing

Measure:

```text
single resolution
batch resolution
cache hit
cache miss
high tenant cardinality
high capability cardinality
concurrent resolution
invalidation storms
```

---

## Gate 18 — Signed Resolution Assertion Decision

Only after the simpler architecture is production-ready:

```text
measure CP call volume
measure latency
measure cache effectiveness
assess threat model
```

Then decide whether signed assertions are justified.

No premature complexity.

---

# 162. Definition of Done

ADR-BCP-007 SHALL be considered implemented when:

1. CP exposes canonical context resolution.
2. CP exposes canonical capability resolution.
3. Batch resolution exists.
4. Consumers request capabilities rather than engines.
5. Consumers cannot force provider selection.
6. Browser tenant headers are non-authoritative.
7. IAM workload identity is enforced.
8. Audience restriction is enforced.
9. Delegated-user context is handled explicitly.
10. CP returns stable resolution decisions.
11. Resolution outputs contain no secrets.
12. Domain authorization remains mandatory.
13. Safe bounded caching exists.
14. Cache keys contain every authorization-sensitive dimension.
15. Revocation invalidates or safely expires cached decisions.
16. Cache failure does not broaden access.
17. Context and capability caches remain distinct.
18. Provider health freshness is respected.
19. Correlation and resolution IDs propagate.
20. API contracts live in Shared.
21. OpenAPI/AsyncAPI conformance is tested.
22. CP is horizontally scalable.
23. CP is not the universal data-plane proxy.
24. Gateway/service mesh responsibilities remain separate.
25. ZuriBeans and Thamani can share provider technology without sharing authorization or business state.
26. Resolution ambiguity fails closed.
27. Explainability exists.
28. Failure semantics are machine-readable.
29. CP outage behaviour is explicit.
30. Signed assertions are not introduced without security approval.

---

# 163. Rejected Alternatives

## Every request proxied through CP

Rejected.

## Digital Estates directly select engines

Rejected.

## Provider URL stored in frontend configuration

Rejected as canonical routing architecture.

## Tenant determined solely from HTTP headers

Rejected.

## Keycloak token contains every capability and business permission

Rejected.

## Capability resolution equals business authorization

Rejected.

## Unlimited resolution caching

Rejected.

## Cache key without full context

Rejected.

## Indefinitely valid offline authorization

Rejected.

## Signed resolution JWTs immediately

Rejected pending demonstrated need and security design.

## Event bus required synchronously for every authorization

Rejected.

## Internal network equals trusted network

Rejected.

---

# 164. Final Runtime Architecture

```text
                         USER / WORKLOAD
                               │
                               ▼
                      ┌─────────────────┐
                      │   BAOBAB IAM    │
                      │    Keycloak     │
                      └────────┬────────┘
                               │
                         identity token
                               │
                               ▼
                     ┌───────────────────┐
                     │ Estate API / BFF  │
                     └─────────┬─────────┘
                               │
                        requested capability
                               │
                               ▼
                 ┌──────────────────────────┐
                 │        BAOBAB CP         │
                 │                          │
                 │  Identity Mapping        │
                 │        ↓                 │
                 │  Context Resolution      │
                 │        ↓                 │
                 │  Capability Grant        │
                 │        ↓                 │
                 │  Binding Resolution      │
                 │        ↓                 │
                 │  Provider Eligibility    │
                 │        ↓                 │
                 │  Resolution Decision     │
                 └────────────┬─────────────┘
                              │
                       ALLOW + provider
                              │
                              ▼
                  ┌─────────────────────────┐
                  │ Capability Provider     │
                  │                         │
                  │ Trade / ERP / CMS /     │
                  │ Pulse / Future Engine   │
                  └────────────┬────────────┘
                               │
                      domain authorization
                               │
                               ▼
                        BUSINESS RESULT
```

The critical boundary is:

```text
CONTROL PLANE
     │
     │ determines
     ▼
"May capability C be consumed
in context X through provider P?"

DATA / DOMAIN PLANE
     │
     │ determines
     ▼
"May this specific business operation
be performed on this resource now?"
```

---

# 165. Decision

**ACCEPTED TARGET CONTROL-PLANE API AND CONSUMPTION ARCHITECTURE, subject to formal approval.**

Upon approval:

1. `baobab-cp` SHALL expose canonical context and capability-resolution APIs;
2. Digital Estates SHALL consume capabilities rather than engine identities;
3. provider selection SHALL remain a Control Plane responsibility;
4. CP SHALL return decisions rather than proxy normal business traffic;
5. IAM SHALL provide identity and workload authentication;
6. domain providers SHALL retain business authorization;
7. service-to-service calls SHALL use authenticated workload identity;
8. caching SHALL be bounded, context-complete and revocation-aware;
9. batch resolution SHALL prevent N+1 resolution patterns;
10. signed resolution assertions SHALL remain deferred until justified and security-approved;
11. all public platform contracts SHALL originate in `baobab-platform/shared`;
12. ambiguity, missing entitlement and untrusted context SHALL fail closed.

---

# 166. Architectural Maxim

> **Ask the Control Plane where an authorised capability lives; do not make the Control Plane perform the capability.**

And, more completely:

> **IAM establishes identity. The Control Plane establishes context, entitlement and provider resolution. The data plane carries the request. The domain provider establishes business authority and performs the work.**