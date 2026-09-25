# ADR-BCP-009 — Capability-Centric Security, Isolation, Residency, Revocation and Failure Semantics

**Status:** Proposed — Normative Platform Security Architecture  
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
- ADR-BCP-007 — Control Plane APIs, Capability Resolution Contracts, Caching, Resolution Assertions and Service-to-Service Consumption Model
- ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model
- Applicable `baobab-platform/baobab-iam` ADRs, particularly ADR-IAM-0008 and identity lifecycle/revocation/resilience decisions

**Applies To:** Tenant isolation, legal-entity isolation, Digital Estate boundaries, capability authorization, provider access, engine topology, regional placement, data residency, IAM integration, revocation, service identity, privileged administration, failure behaviour, caching, audit and incident containment.

**Architecture Style:** Zero-trust, deny-by-default, fail-closed, least privilege, capability-centric, context-aware, tenant-isolated, residency-aware, defence-in-depth.

**Decision Type:** Foundational security architecture.

---

# 1. Executive Decision

Baobab SHALL enforce security through independent but composable trust layers:

```text id="n60uv7"
IDENTITY
    │
    ▼
CONTEXT
    │
    ▼
ENTITLEMENT
    │
    ▼
CAPABILITY
    │
    ▼
PROVIDER
    │
    ▼
DOMAIN AUTHORIZATION
    │
    ▼
BUSINESS OPERATION
```

No layer SHALL assume that successful validation at an earlier layer implies authorization at a later layer.

The fundamental rule is:

> **Identity establishes who is acting. Context establishes where and for whom they are acting. Capability grants establish what the platform permits them to consume. Bindings determine where that capability may execute. Domain authorization determines whether the requested business action may actually occur.**

Baobab SHALL deny an operation whenever a security-critical fact cannot be established authoritatively.

---

# 2. Security Objective

The Control Plane security architecture SHALL protect against at least:

```text id="g5z58n"
cross-tenant access
cross-legal-entity access
Digital Estate impersonation
context spoofing
capability escalation
provider bypass
binding manipulation
stale entitlement reuse
revocation failure
credential compromise
workload impersonation
region bypass
residency violation
isolation downgrade
administrative abuse
confused-deputy attacks
unsafe failover
unsafe caching
```

---

# 3. Primary Security Invariant

For every capability invocation:

```text id="ry0r9c"
Authenticated
AND
ContextValid
AND
TenantAllowed
AND
LegalEntityAllowed
AND
CapabilityGranted
AND
ScopeApplicable
AND
ProviderEligible
AND
IsolationSatisfied
AND
ResidencySatisfied
AND
DomainAuthorized
```

must hold.

Otherwise:

```text id="z2b50s"
DENY
```

---

# 4. Defence in Depth

Baobab SHALL NOT rely on one enforcement point.

Conceptually:

```text id="o9snkl"
┌─────────────────────────────────────┐
│ Edge / Gateway                      │
│ Authentication + coarse protection  │
└──────────────────┬──────────────────┘
                   ▼
┌─────────────────────────────────────┐
│ IAM                                 │
│ Identity + assurance + workload ID  │
└──────────────────┬──────────────────┘
                   ▼
┌─────────────────────────────────────┐
│ Control Plane                       │
│ Context + entitlement + resolution  │
└──────────────────┬──────────────────┘
                   ▼
┌─────────────────────────────────────┐
│ Provider Boundary                   │
│ Workload + context validation       │
└──────────────────┬──────────────────┘
                   ▼
┌─────────────────────────────────────┐
│ Domain Engine                       │
│ Business authorization              │
└─────────────────────────────────────┘
```

Compromise or failure of one layer SHALL not automatically remove all other controls.

---

# 5. IAM Boundary

`baobab-iam` SHALL remain authoritative for:

```text id="iynl94"
authentication
credentials
MFA
passkeys
sessions
OIDC/OAuth
federation
workload identity
authentication assurance
identity lifecycle
credential revocation
```

It SHALL NOT become the authority for Baobab business capability entitlement.

---

# 6. Control Plane Boundary

`baobab-cp` SHALL remain authoritative for:

```text id="bfshkg"
canonical tenant context
legal-entity context
Digital Estate context
market participation context
capability grants
capability scope
provider binding
provider eligibility
isolation requirements
residency requirements
platform entitlement
```

---

# 7. Domain Boundary

Domain engines SHALL remain authoritative for business decisions such as:

```text id="g3h3ab"
buyer approval
supplier approval
refund approval
payment execution
credit limit
invoice posting
accounting-period controls
shipment dispatch
inventory reservation
contract approval
CMS publishing
```

These SHALL NOT be moved into Keycloak or CP merely because they involve authorization.

---

# 8. Three-Layer Authorization

The security model therefore remains:

| Layer | Question | Authority |
|---|---|---|
| IAM | Who are you and how strongly were you authenticated? | `baobab-iam` |
| Control Plane | In which context may you consume which platform capability? | `baobab-cp` |
| Domain | May you perform this business operation on this resource now? | Domain provider |

No layer replaces another.

---

# 9. Deny by Default

All capability access SHALL default to:

```text id="n3qpjg"
DENY
```

until explicitly established otherwise.

Absence of a denial rule SHALL NOT imply permission.

---

# 10. Fail Closed

When Baobab cannot safely establish:

```text id="xufhbl"
identity
tenant
legal entity
context
grant
scope
binding
provider
isolation
residency
```

the result SHALL be:

```text id="6zydfp"
DENY
```

or:

```text id="h95b67"
NOT_READY
```

depending on whether the question is authorization or operational readiness.

---

# 11. No Security Guessing

The following behaviour is prohibited:

```text id="vd2ntg"
if tenant missing:
    choose default tenant

if market ambiguous:
    choose first market

if binding missing:
    use default provider

if legal entity missing:
    infer from country

if residency unknown:
    use nearest region
```

Security-sensitive ambiguity SHALL fail.

---

# 12. Tenant Isolation

Tenant SHALL be a primary isolation dimension.

Every tenant-scoped repository query SHALL constrain tenant context.

Unsafe:

```sql id="y8y3r4"
SELECT *
FROM capability_grant
WHERE id = $1;
```

Preferred conceptual pattern:

```sql id="4c0jtb"
SELECT *
FROM capability_grant
WHERE tenant_id = $1
  AND id = $2;
```

---

# 13. Legal Entity Isolation

Legal entity SHALL remain independent from tenant.

A tenant relationship SHALL not automatically authorize access across legal entities.

This is especially important for NABHOLD.

```text id="x5zeb9"
NABHOLD GROUP AFRICA
       │
       ├── ZuriBeans
       │
       └── Thamani Global
```

Corporate relationship does not imply shared operational authority.

---

# 14. ZuriBeans and Thamani

The following SHALL remain independently isolated:

```text id="rwqhgu"
customers
buyers
suppliers
catalogues where separately owned
inventory
orders
payments
contracts
accounting
capability grants
provider configuration
IAM business context
audit
operational workflows
```

They MAY share platform infrastructure.

They SHALL NOT share business state merely because infrastructure is shared.

---

# 15. Infrastructure Sharing vs Data Sharing

Baobab SHALL distinguish:

```text id="msysbd"
Shared Infrastructure
```

from:

```text id="bjqzxk"
Shared Business Data
```

Example:

```text id="p2bpr8"
One PostgreSQL cluster
```

does not imply:

```text id="fv6a0q"
one tenant data namespace.
```

---

# 16. Isolation Profiles

`IsolationProfile` SHALL explicitly describe technical isolation requirements.

Conceptually:

```text id="lgwr2q"
IsolationProfile
├── id
├── level
├── database_mode
├── schema_mode
├── namespace_mode
├── compute_mode
├── network_mode
├── encryption_mode
├── secret_scope
├── region_constraints
└── policy_version
```

---

# 17. Isolation Levels

Baobab MAY support several isolation levels, for example:

```text id="s06yya"
SHARED_LOGICAL
DEDICATED_SCHEMA
DEDICATED_DATABASE
DEDICATED_INSTANCE
DEDICATED_DEPLOYMENT
```

The exact names SHALL be canonicalised.

---

# 18. Isolation Is Policy

Isolation level SHALL be selected according to:

```text id="j5z3m9"
tenant requirements
data classification
regulatory obligations
contractual requirements
risk
provider capability
```

It SHALL not be inferred solely from tenant size.

---

# 19. No Silent Isolation Downgrade

If a tenant requires:

```text id="4x6k1m"
DEDICATED_DATABASE
```

and only:

```text id="bmltnv"
SHARED_LOGICAL
```

is available:

```text id="x6rxsz"
resolution SHALL fail.
```

Availability SHALL not override isolation policy.

---

# 20. Isolation Compatibility

A provider binding is eligible only if:

```text id="2m6cqn"
provider isolation capability
>=
required isolation policy
```

under the platform's explicit compatibility model.

---

# 21. Provider Sharing

A provider MAY serve multiple tenants only where its architecture supports required isolation.

Provider registration SHALL declare supported isolation modes.

---

# 22. Engine Instance Isolation

Two capability resolutions MAY use the same provider but different engine instances because of:

```text id="akpdyb"
tenant isolation
region
residency
capacity
environment
```

---

# 23. Environment Isolation

Production SHALL be isolated from:

```text id="l87sfr"
development
test
staging
```

Production credentials and data SHALL not be reused in non-production environments except under explicitly governed sanitised procedures.

---

# 24. Environment SHALL Be Contextual

`environment` SHALL be part of provider eligibility.

A production resolution SHALL never select a staging engine instance.

---

# 25. Digital Estate Isolation

A Digital Estate is not automatically an authorization boundary by itself.

However, estate context SHALL constrain capabilities where grants or bindings are estate-scoped.

---

# 26. Estate Spoofing

A user of:

```text id="2aqs0i"
Thamani Customer Storefront
```

SHALL not obtain:

```text id="qck1bv"
ZuriBeans Supplier Portal
```

context by modifying an estate identifier.

CP SHALL validate principal-to-estate/context relationships.

---

# 27. Channel Isolation

Channel context MAY affect:

```text id="u08c4c"
catalogue
pricing
workflow
provider binding
capability availability
```

A B2C channel SHALL not inherit B2B capabilities merely because the same legal entity owns both channels.

---

# 28. B2B/B2C Are Not Security Roles

The values:

```text id="79l8xq"
B2B
B2C
```

describe channel/business context.

They SHALL not substitute for actual identity, grants or domain roles.

---

# 29. Market Security

Market participation SHALL be validated against authoritative CP state.

A request SHALL not gain market capability merely by supplying:

```text id="4wz6ko"
country_code = ZA
```

---

# 30. Geography Is Not Identity

The platform SHALL never assume:

```text id="9k0f3e"
destination country
=
authorised legal entity
```

or:

```text id="79f0y2"
IP country
=
operating market
```

---

# 31. Residency

Data residency SHALL be explicitly modelled.

Conceptually:

```text id="02f5c9"
DataResidencyPolicy
├── id
├── applicable_scope
├── allowed_regions[]
├── prohibited_regions[]
├── storage_constraints
├── processing_constraints
├── replication_constraints
├── backup_constraints
└── policy_version
```

---

# 32. Residency vs Business Geography

These are distinct:

```text id="vd1rm3"
customer country
transaction origin
transaction destination
legal jurisdiction
deployment region
data residency region
```

No one dimension SHALL silently determine the others.

---

# 33. Residency Resolution

Provider eligibility SHALL include:

```text id="3hvjra"
required residency
      │
      ▼
provider deployment region
      │
      ▼
storage/processing policy
      │
      ▼
compatible?
```

If incompatible:

```text id="0izflf"
DENY / NOT_READY
```

---

# 34. Cross-Border Transactions

A cross-border transaction does NOT automatically permit cross-border data movement.

Example:

```text id="im2iub"
Uganda → South Africa goods movement
```

does not imply:

```text id="mjpx0f"
all personal or commercial data may be replicated anywhere.
```

---

# 35. Transit Countries

A transit country SHALL not automatically become:

```text id="br92e0"
tenant operating market
legal presence
data residency region
```

Transit is transaction geography.

---

# 36. Data Classification

Baobab SHALL classify data sufficiently to drive security policy.

The existing broad taxonomy:

```text id="8e6w7j"
Public
Baobab-owned
Tenant-specific
Confidential
```

SHOULD be refined where necessary for security enforcement.

---

# 37. Suggested Security Classification

A future canonical classification MAY include:

```text id="zvrch9"
PUBLIC
INTERNAL
TENANT_CONFIDENTIAL
RESTRICTED
HIGHLY_RESTRICTED
```

Mapping from existing classification SHALL be documented rather than silently changed.

---

# 38. Classification Is Metadata

Classification SHOULD influence:

```text id="h9i5ou"
encryption
logging
retention
access control
residency
backup
export
```

---

# 39. Encryption in Transit

All production service-to-service communication SHALL use encrypted transport.

Internal network placement SHALL not remove this requirement.

---

# 40. Workload Authentication

Encryption without workload authentication is insufficient.

Baobab SHOULD use authenticated workload identities in addition to encrypted transport.

---

# 41. Encryption at Rest

Sensitive platform and tenant data SHALL be encrypted at rest according to infrastructure and classification policy.

---

# 42. Key Management

Encryption keys SHALL be:

```text id="6czwqe"
managed
rotatable
access-controlled
audited
```

Application repositories SHALL not contain production private keys.

---

# 43. Secret Management

Secrets SHALL NOT be stored in:

```text id="i90p5k"
Git
Docker images
source code
plain configuration files
logs
resolution responses
```

Production secrets SHALL use an approved secret-management mechanism.

---

# 44. Secret Scope

Secrets SHOULD be scoped to the smallest practical boundary:

```text id="3q6ogk"
service
environment
provider
tenant where required
```

---

# 45. Secret Rotation

Critical secrets SHALL support rotation without uncontrolled downtime.

---

# 46. Service Identity

Every Baobab service SHOULD have a distinct workload identity.

Examples:

```text id="9sgcnc"
baobab-cp
baobab-trade
baobab-erp
baobab-cms
baobab-pulse
```

They SHALL not all authenticate as one generic:

```text id="b9a40r"
baobab-service
```

identity.

---

# 47. Least Privilege

Each workload SHALL receive only the scopes necessary for its function.

Example:

```text id="n0nh8l"
baobab-trade
```

should not automatically receive administrative authority over:

```text id="ntj2ct"
tenant lifecycle
IAM administration
ERP configuration
```

---

# 48. Audience Restriction

Tokens SHALL be audience restricted as established in ADR-BCP-007.

This mitigates token replay between services.

---

# 49. Token Lifetime

Workload and user tokens SHOULD be short-lived.

Long-lived credentials increase revocation exposure.

---

# 50. Refresh Credentials

Where refresh credentials exist, they SHALL receive stronger protection than ordinary access tokens.

---

# 51. Authentication Assurance

Sensitive capabilities MAY require stronger authentication assurance.

Examples:

```text id="a1s8ae"
tenant administration
provider administration
financial approval
security policy change
```

---

# 52. Step-Up Authentication

Where supported by IAM, a domain or administrative operation MAY require step-up authentication.

CP SHALL consume assurance information where platform policy requires it.

---

# 53. Authentication Assurance Is Not Business Approval

MFA proves stronger authentication.

It does NOT prove:

```text id="2azk9u"
invoice approved
supplier approved
refund authorised
```

---

# 54. Capability Grants

A capability grant SHALL be treated as an explicit authorization object.

It SHALL have:

```text id="zrc45u"
subject scope
capability
effective time
source
status
version
```

---

# 55. Grant Provenance

Grant provenance SHALL be retained.

Possible sources:

```text id="w4c90r"
PRODUCT_SUBSCRIPTION
PLATFORM_BASELINE
CONTRACT
ADMINISTRATIVE_OVERRIDE
INTERNAL_PLATFORM
```

---

# 56. Multiple Grant Sources

A capability MAY be granted through multiple independent sources.

Revoking one source SHALL not automatically revoke another.

---

# 57. Source-Specific Revocation

Example:

```text id="s8hr9u"
Capability A
├── subscription X
└── platform baseline
```

Cancel subscription X:

```text id="7gljqs"
remove/suspend X contribution
```

but capability A may remain effective through the baseline.

---

# 58. Effective Entitlement

Conceptually:

```text id="i61whb"
EffectiveGrant(C)
=
at least one active,
applicable,
unexpired,
non-revoked grant source
```

subject to policy constraints.

---

# 59. Revocation

Revocation SHALL be first-class.

Revocation types include:

```text id="66wwrg"
identity revocation
session revocation
membership revocation
grant revocation
subscription suspension
tenant suspension
estate suspension
provider revocation
binding revocation
credential revocation
```

---

# 60. Revocation Propagation

Conceptually:

```text id="pcl5od"
Authoritative State Change
        │
        ▼
Transactional Commit
        │
        ├── Audit
        └── Outbox Event
                │
                ▼
        Cache Invalidation
                │
                ▼
        Readiness Recompute
                │
                ▼
        Dependent Services
```

---

# 61. Revocation Shall Not Depend Solely on Events

Events accelerate propagation.

Bounded TTL and authoritative checks provide correctness backstops.

---

# 62. Revocation Latency

Security policy SHALL define maximum tolerated revocation latency according to risk class.

---

# 63. Immediate Revocation

Critical events MAY require synchronous or near-immediate invalidation.

Examples:

```text id="2xg0mf"
tenant security suspension
compromised identity
compromised provider
administrative credential compromise
```

---

# 64. Tenant Suspension

Tenant suspension SHALL invalidate or deny tenant-scoped capability access.

It SHALL not require deleting historical records.

---

# 65. Legal Entity Suspension

Legal entity suspension SHALL affect only applicable legal-entity contexts unless policy explicitly requires broader tenant suspension.

---

# 66. Estate Suspension

Estate suspension SHALL prevent relevant estate-mediated capability consumption without necessarily suspending the entire tenant.

---

# 67. Capability Suspension

A capability MAY be globally or contextually suspended.

Global suspension SHALL be exceptional and audited.

---

# 68. Provider Suspension

Provider suspension SHALL remove the provider from eligible resolution.

Failover MAY occur only if an eligible alternative satisfies all security constraints.

---

# 69. Unsafe Failover Is Prohibited

Availability SHALL NOT justify:

```text id="fyubmx"
lower isolation
wrong region
unsupported contract
prohibited residency
unapproved provider
```

---

# 70. Failover Security Invariant

A fallback provider must satisfy at least the same mandatory security constraints as the primary.

---

# 71. Provider Compromise

A suspected provider compromise SHOULD support:

```text id="6qt57m"
provider quarantine
binding disablement
credential revocation
cache invalidation
impact analysis
readiness recomputation
audit
incident correlation
```

---

# 72. Quarantine

A provider or engine instance MAY enter:

```text id="z51zzr"
QUARANTINED
```

state.

Quarantined resources SHALL not receive normal traffic.

---

# 73. Recovery From Quarantine

Recovery SHALL require explicit validation and audited reactivation.

A healthy probe alone SHALL not automatically reverse a security quarantine.

---

# 74. Cache Security

Authorization caches SHALL obey ADR-BCP-007.

The cache key SHALL contain every dimension affecting authorization.

---

# 75. Cross-Tenant Cache Poisoning

Cache entries SHALL be namespaced or keyed so that:

```text id="m95hzw"
Tenant A
```

can never reuse:

```text id="r8lmre"
Tenant B
```

resolution.

---

# 76. Stale Grant Cache

A cached ALLOW SHALL expire.

A revoked grant SHALL not remain usable indefinitely.

---

# 77. Negative Cache

DENY decisions MAY be cached.

Care SHALL be taken not to delay legitimate entitlement activation excessively.

---

# 78. Security-Critical Cache Policy

Some capabilities MAY require:

```text id="ec83ws"
very short ALLOW TTL
```

or authoritative re-evaluation for every sensitive operation.

---

# 79. Resolution Assertions

If signed resolution assertions from ADR-BCP-007 are later implemented, they SHALL follow this ADR.

---

# 80. Resolution Assertion Threat Model

The design SHALL address:

```text id="7s4xxq"
forgery
replay
audience confusion
stale entitlement
provider migration
tenant suspension
key compromise
clock manipulation
scope tampering
```

---

# 81. Resolution Assertion Requirements

At minimum:

```text id="bxlr14"
asymmetric signatures
short lifetime
issuer validation
audience restriction
provider restriction
capability restriction
context digest
expiry
key rotation
```

---

# 82. Assertion Replay

Where replay creates material risk, assertions SHOULD include:

```text id="sgm08i"
nonce
operation binding
request binding
```

or an equivalent anti-replay mechanism.

---

# 83. Assertion Is Not Domain Authorization

Even a cryptographically valid assertion SHALL not bypass domain authorization.

---

# 84. Administrative Security

Control-plane administration represents a high-value attack surface.

Administrative APIs SHALL use:

```text id="g2r96s"
strong authentication
explicit administrative authorization
least privilege
audit
rate limiting
network controls where appropriate
```

---

# 85. Administrative Roles

Administrative access SHOULD distinguish:

```text id="oyux8n"
Tenant Administrator
Platform Administrator
Security Administrator
Provider Administrator
Auditor
Support Operator
```

---

# 86. No Universal Administrator by Default

A single permanent account with unrestricted authority over:

```text id="crdrwe"
IAM
CP
Trade
ERP
CMS
Pulse
```

SHOULD be avoided.

---

# 87. Separation of Duties

High-risk changes MAY require separate approval.

Examples:

```text id="vg24t4"
isolation downgrade
residency policy change
provider quarantine reversal
cross-tenant administrative action
privileged grant
```

---

# 88. Break-Glass Access

Emergency privileged access MAY exist.

It SHALL be:

```text id="tibf81"
rare
time-bounded
strongly authenticated
fully audited
alerted
reviewed afterward
```

---

# 89. No Permanent Break-Glass Session

Emergency access SHALL not become ordinary administrative practice.

---

# 90. Privilege Escalation

Changes granting administrative capabilities to the actor making the change SHOULD be explicitly detected and audited.

---

# 91. Confused Deputy Protection

A trusted service SHALL not use its own broad privileges to perform an operation merely because an untrusted caller requested it.

Every delegated operation SHALL preserve sufficient caller/context information.

---

# 92. Example Confused Deputy Attack

```text id="eq2n99"
Thamani user
     │
     ▼
Estate BFF
     │
     │ BFF has broad CP access
     ▼
request ZuriBeans capability
```

The BFF SHALL NOT use its workload privilege to bypass the user's valid context.

---

# 93. Delegation

Where a workload acts on behalf of a user, the platform SHALL distinguish:

```text id="8ogtpn"
actor workload
delegated principal
tenant context
```

---

# 94. Machine-to-Machine Operations

Some operations have no end-user principal.

Example:

```text id="yxlf1k"
reconciliation worker
```

In that case the workload itself is the actor and SHALL have explicitly scoped machine authority.

---

# 95. AI/Agent Identity

Future AI agents SHALL not be treated as implicitly trusted.

An AI agent SHALL operate through:

```text id="3s9rda"
workload identity
delegated authority
bounded capabilities
audit
```

---

# 96. AI Delegation

An agent SHALL not gain broader capability access than the principal or automation policy authorising it.

---

# 97. Canonical IDs

Canonical identifiers SHALL be opaque and non-authoritative by themselves.

Knowing:

```text id="wj1zva"
tenant UUID
```

does not grant access to that tenant.

---

# 98. IDOR Protection

Every identifier-based lookup SHALL enforce the relevant contextual boundary.

This applies to:

```text id="ntxj6e"
tenant
legal entity
estate
grant
binding
provider
subscription
mapping
context
```

---

# 99. Enumeration

APIs SHOULD minimise the ability of one tenant to enumerate identifiers belonging to another tenant.

---

# 100. Error Leakage

Errors SHALL not reveal unnecessary cross-tenant existence information.

---

# 101. Database Security

PostgreSQL access SHALL use:

```text id="mh2mx8"
least-privilege database roles
encrypted transport
credential rotation
restricted administrative access
auditing where appropriate
```

---

# 102. Application DB Role

Normal CP runtime SHALL not use an unrestricted database superuser.

---

# 103. Migration DB Role

Database migrations MAY require elevated privileges.

Migration credentials SHOULD be separate from normal runtime credentials.

---

# 104. Row-Level Security

PostgreSQL Row-Level Security MAY be used as an additional defence for appropriate tenant-scoped tables.

It SHALL NOT substitute for application-layer authorization.

---

# 105. RLS Decision

If adopted:

```text id="mmh9qb"
Application isolation
+
Database RLS
```

provides defence in depth.

A dedicated implementation review SHALL determine which tables benefit from RLS without creating unsafe operational complexity.

---

# 106. Database Constraints

Security invariants SHOULD be enforced by database constraints where feasible.

Examples:

```text id="32ghz9"
tenant-bound foreign keys
unique active bindings
valid lifecycle state
non-overlapping temporal ranges
```

---

# 107. Cross-Tenant Foreign Keys

Schema design SHALL avoid relationships that allow Tenant A resources to reference Tenant B resources unintentionally.

---

# 108. Canonical Mapping Security

Mappings SHALL never be resolved globally without applicable MappingScope.

External IDs are not globally trusted.

---

# 109. ExternalReference

External references SHALL always be interpreted with:

```text id="z4wbci"
engine
tenant/context
mapping scope
```

where required.

---

# 110. Provider Adapters

Provider adapters SHALL treat provider data as external-domain input.

They SHALL validate:

```text id="3f7s7k"
identity mapping
tenant mapping
resource mapping
contract version
```

before trusting it.

---

# 111. Webhooks

Provider webhooks SHALL require:

```text id="vuh3zr"
authenticity verification
replay protection where applicable
tenant/context resolution
idempotency
```

---

# 112. Event Security

Canonical events SHALL be authenticated at the messaging infrastructure and validated against Shared schemas.

---

# 113. Event Payload Trust

Receiving an event does not mean every field in its payload is authoritative.

Consumers SHALL know which service owns each fact.

---

# 114. Event Tenant Context

Tenant-scoped events SHALL carry sufficient canonical tenant/legal-entity context to prevent cross-tenant processing.

---

# 115. Event Replay

Consumers SHALL be idempotent.

Security-sensitive events SHALL not become harmful when delivered more than once.

---

# 116. Event Ordering

Consumers SHALL not assume perfect global ordering.

Versioning or aggregate sequencing SHOULD protect against stale state overwriting newer state.

---

# 117. Outbox Security

Transactional outbox payloads SHALL not contain secrets unless absolutely necessary and specifically protected.

---

# 118. Logs

Security logs SHALL be structured and protected from tenant leakage.

---

# 119. No Token Logging

The following SHALL never be logged:

```text id="v8q6ad"
access token
refresh token
password
private key
client secret
```

---

# 120. Audit Security

Audit records SHALL be protected against unauthorised modification and deletion.

---

# 121. Audit Actor Integrity

Where possible, audit actor identity SHALL come from authenticated context rather than caller-provided fields.

---

# 122. Security Events

Important security events SHOULD include:

```text id="g0swp9"
authentication failure
context spoof attempt
cross-tenant access attempt
grant escalation attempt
provider-forcing attempt
residency violation
isolation mismatch
administrative privilege change
provider quarantine
break-glass use
```

---

# 123. Security Monitoring

Security events SHOULD feed appropriate monitoring and incident-response processes.

---

# 124. Security Severity

Not every denial is an incident.

For example:

```text id="34y1af"
ordinary user lacks capability
```

may be expected.

But:

```text id="wvs3xv"
repeated cross-tenant ID probing
```

may indicate attack.

---

# 125. Rate Limiting

Rate limiting SHALL apply at appropriate boundaries.

Possible dimensions:

```text id="xms99d"
IP where relevant
principal
workload
tenant
endpoint
capability
```

---

# 126. Rate Limiting Is Not Authorization

Rate limiting SHALL never substitute for entitlement checks.

---

# 127. Denial-of-Service Isolation

One tenant SHOULD not be able to exhaust CP resources for all tenants.

Controls MAY include:

```text id="wmv3ko"
tenant-aware quotas
connection limits
request limits
worker isolation
backpressure
```

---

# 128. Resource Exhaustion

Resolution algorithms SHALL have bounded computational complexity.

Untrusted callers SHALL not be able to construct arbitrarily expensive scope queries.

---

# 129. Input Validation

All API inputs SHALL be validated.

This includes:

```text id="jmsu79"
identifier format
enum values
scope structure
array sizes
string lengths
timestamps
pagination
filters
```

---

# 130. Query Safety

Dynamic filtering SHALL use safe query construction.

Raw untrusted SQL fragments are prohibited.

---

# 131. Output Encoding

Administrative UIs and Digital Estates SHALL correctly encode untrusted output to mitigate injection attacks.

---

# 132. Provider Configuration Validation

Configuration passed to provider adapters SHALL be schema validated before activation.

---

# 133. Configuration Secrets

Provider configuration SHALL distinguish:

```text id="kic9dl"
ordinary configuration
```

from:

```text id="16z3hh"
secret reference
```

Secrets SHOULD be referenced, not embedded.

---

# 134. Supply-Chain Security

Production Baobab builds SHOULD enforce:

```text id="6dxqoc"
dependency scanning
container scanning
pinned critical dependencies
provenance
protected branches
reviewed changes
CI checks
```

according to repository production-readiness standards.

---

# 135. Container Images

Production images SHOULD be:

```text id="y2lksv"
minimal
non-root where practical
immutable
scanned
versioned
traceable to source
```

---

# 136. Image Tags

Mutable tags such as:

```text id="waf7xg"
latest
```

SHOULD NOT be the sole production deployment reference.

Immutable digests or equivalent controlled references SHOULD be used for critical production workloads.

---

# 137. IAM Engine Image

The existing Keycloak image-digest placeholder identified during IAM review SHALL be resolved before production deployment.

Placeholder security controls SHALL not be treated as completed controls.

---

# 138. CI/CD Identity

GitHub Actions or equivalent deployment automation SHALL use short-lived/federated credentials where supported rather than permanent cloud secrets.

---

# 139. Branch Protection

Production-affecting repositories SHOULD require:

```text id="4l9ipb"
protected main branch
review
passing checks
controlled merge
```

---

# 140. Security Testing

Security tests SHALL be part of CI/CD where practical.

---

# 141. Mandatory Isolation Tests

At minimum:

```text id="4xxvkn"
Tenant A cannot query Tenant B grants
Tenant A cannot use Tenant B context
Tenant A cannot force Tenant B provider
Tenant A cannot enumerate Tenant B estate
Legal Entity A cannot inherit Legal Entity B entitlement
B2C estate cannot invoke B2B-only grant
staging instance cannot satisfy production binding
wrong-region instance cannot satisfy residency
lower-isolation provider cannot satisfy higher-isolation requirement
```

---

# 142. Mandatory Revocation Tests

Test:

```text id="h42slr"
identity disabled
session revoked
grant revoked
subscription suspended
tenant suspended
estate suspended
provider suspended
provider quarantined
binding disabled
```

and verify bounded propagation.

---

# 143. Mandatory Cache Tests

Test:

```text id="d8e5af"
cross-tenant cache key collision
cross-market cache collision
stale ALLOW after grant revocation
stale provider after migration
expired context reuse
expired resolution reuse
```

---

# 144. Mandatory IAM Tests

Test:

```text id="dr32vn"
wrong issuer
wrong audience
expired token
invalid signature
insufficient scope
disabled identity
workload/user confusion
```

---

# 145. Mandatory API Tests

Test:

```text id="x0zq1n"
IDOR
mass assignment
oversized payload
invalid scope
provider forcing
tenant spoofing
legal-entity spoofing
estate spoofing
```

---

# 146. Mandatory Provider Tests

Test:

```text id="bs7zyu"
wrong tenant mapping
wrong provider contract
wrong region
wrong isolation
unhealthy provider
quarantined provider
```

---

# 147. Property-Based Tests

Critical resolver invariants SHOULD use property-based testing where appropriate.

Example invariant:

> For any two distinct tenants A and B, a grant belonging only to A SHALL never make a capability resolvable for B.

---

# 148. Fuzz Testing

Parsers and externally reachable APIs SHOULD be candidates for fuzz testing where practical.

---

# 149. Security Regression Tests

Every discovered cross-tenant, authorization or context vulnerability SHALL receive a regression test before closure.

---

# 150. Threat Modelling

Major architecture changes SHALL update the threat model.

Threat modelling SHOULD cover:

```text id="opn0w3"
assets
actors
trust boundaries
entry points
abuse cases
mitigations
residual risk
```

---

# 151. Trust Boundaries

At minimum:

```text id="m2vvuj"
Internet ↔ Digital Estate
Digital Estate ↔ BFF
BFF ↔ IAM
BFF/domain ↔ CP
CP ↔ PostgreSQL
CP ↔ provider
provider ↔ domain engine
service ↔ event bus
CI/CD ↔ production
operator ↔ administrative API
```

---

# 152. Security Architecture Diagram

```text id="g02o7m"
                    ┌────────────────────┐
                    │  USER / WORKLOAD   │
                    └─────────┬──────────┘
                              │
                              ▼
                    ┌────────────────────┐
                    │   BAOBAB IAM       │
                    │                    │
                    │ Authentication     │
                    │ MFA / Passkeys     │
                    │ Workload Identity  │
                    └─────────┬──────────┘
                              │
                       trusted identity
                              │
                              ▼
                ┌───────────────────────────┐
                │        BAOBAB CP          │
                │                           │
                │ Canonical Identity        │
                │       ↓                   │
                │ Context                   │
                │       ↓                   │
                │ Tenant / Legal Entity     │
                │       ↓                   │
                │ Capability Grant          │
                │       ↓                   │
                │ Scope                     │
                │       ↓                   │
                │ Isolation                 │
                │       ↓                   │
                │ Residency                 │
                │       ↓                   │
                │ Binding                   │
                │       ↓                   │
                │ Provider Eligibility      │
                └────────────┬──────────────┘
                             │
                       ALLOW / DENY
                             │
                             ▼
               ┌────────────────────────────┐
               │     CAPABILITY PROVIDER    │
               │                            │
               │ Workload Validation        │
               │ Context Validation         │
               │ Domain Authorization       │
               └─────────────┬──────────────┘
                             │
                             ▼
                    BUSINESS OPERATION
```

---

# 153. Security Decision Flow

```text id="3z6w3a"
REQUEST
   │
   ▼
Valid authentication?
   │
   ├── NO ───────────────► DENY
   ▼
Valid workload/user audience?
   │
   ├── NO ───────────────► DENY
   ▼
Canonical identity resolvable?
   │
   ├── NO ───────────────► DENY
   ▼
Tenant context valid?
   │
   ├── NO ───────────────► DENY
   ▼
Legal entity valid?
   │
   ├── NO ───────────────► DENY
   ▼
Estate/channel valid?
   │
   ├── NO ───────────────► DENY
   ▼
Capability exists?
   │
   ├── NO ───────────────► DENY
   ▼
Active applicable grant?
   │
   ├── NO ───────────────► DENY
   ▼
Isolation satisfied?
   │
   ├── NO ───────────────► DENY
   ▼
Residency satisfied?
   │
   ├── NO ───────────────► DENY
   ▼
Valid binding?
   │
   ├── NO ───────────────► DENY
   ▼
Eligible provider?
   │
   ├── NO ───────────────► DENY
   ▼
Platform ALLOW
   │
   ▼
Provider/domain authorization
   │
   ├── NO ───────────────► DENY
   ▼
EXECUTE
```

---

# 154. Security Failure Taxonomy

Canonical failure classes SHOULD include:

| Class | Meaning |
|---|---|
| `AUTHN_*` | Authentication failure |
| `IDENTITY_*` | Canonical identity failure |
| `CONTEXT_*` | Invalid/ambiguous context |
| `TENANT_*` | Tenant boundary failure |
| `LEGAL_ENTITY_*` | Legal-entity boundary failure |
| `ESTATE_*` | Digital Estate boundary failure |
| `CAPABILITY_*` | Capability failure |
| `GRANT_*` | Entitlement failure |
| `ISOLATION_*` | Isolation failure |
| `RESIDENCY_*` | Residency failure |
| `BINDING_*` | Provider-binding failure |
| `PROVIDER_*` | Provider security/availability failure |
| `DOMAIN_*` | Business authorization failure |
| `REVOCATION_*` | Revocation-related state |
| `SECURITY_*` | Broader security policy failure |

---

# 155. Security vs Availability Failure

Baobab SHALL distinguish:

```text id="vd6odl"
PROVIDER_UNAVAILABLE
```

from:

```text id="s9jvnm"
PROVIDER_NOT_AUTHORISED
```

A security rejection SHALL not be automatically retried against progressively weaker providers.

---

# 156. Retry Semantics

Authentication and authorization denials SHOULD normally be non-retryable without changed credentials/context/state.

Transient infrastructure failures MAY be retryable.

---

# 157. No Security Fallback

The following pattern is prohibited:

```text id="02a20n"
secure provider failed
       │
       ▼
try less-secure provider
```

---

# 158. Failure Modes

Baobab SHALL explicitly define behaviour for:

```text id="zy12n3"
IAM unavailable
CP unavailable
PostgreSQL unavailable
cache unavailable
event bus unavailable
provider unavailable
region unavailable
secret manager unavailable
residency-compliant provider unavailable
```

---

# 159. IAM Unavailable

Existing unexpired tokens MAY continue to be validated according to IAM/security policy where cryptographic validation remains possible.

New authentication or required introspection may fail.

The system SHALL NOT fabricate identities.

---

# 160. CP Unavailable

Only valid, unexpired, policy-permitted cached decisions may continue where ADR-BCP-007 allows.

Sensitive operations MAY require live CP authority.

---

# 161. PostgreSQL Unavailable

Where authoritative state cannot be established and no valid security-approved cached decision exists:

```text id="e15w2f"
FAIL CLOSED
```

---

# 162. Cache Unavailable

Fall back to authoritative evaluation.

Security SHALL remain correct at reduced performance.

---

# 163. Event Bus Unavailable

Synchronous authoritative state changes may continue only if transactional outbox preserves event publication intent.

Revocation-sensitive operations SHALL consider propagation lag.

---

# 164. Provider Unavailable

Use only security-compatible configured fallback.

Otherwise:

```text id="o4by4h"
NOT_READY / unavailable
```

---

# 165. Region Unavailable

A provider SHALL NOT move processing to a prohibited region merely to maintain availability.

---

# 166. Secret Manager Unavailable

Services SHALL not fall back to hard-coded credentials.

Existing safely cached credentials MAY continue only according to secret-management policy and expiry.

---

# 167. Security Over Availability

Where a direct conflict exists:

> **Baobab SHALL prefer loss of availability over violation of tenant isolation, legal-entity isolation, identity integrity, residency or mandatory security policy.**

---

# 168. Multi-Region Security

Multi-region deployment SHALL preserve:

```text id="38br0l"
identity trust
tenant isolation
encryption
residency
audit
revocation
configuration consistency
```

---

# 169. Regional Control Plane

A regional CP instance MAY serve local requests.

It SHALL operate from authoritative or safely replicated state according to consistency requirements.

---

# 170. Replication Lag

Security-sensitive replicated state SHALL define acceptable lag.

Examples:

```text id="3d4k63"
tenant suspension
grant revocation
provider quarantine
```

require stricter propagation than low-risk catalogue metadata.

---

# 171. Multi-Region Split Brain

The architecture SHALL define which authority wins during conflicting regional state.

Security-critical mutable authority SHALL not use uncontrolled last-write-wins semantics.

---

# 172. Residency-Aware Backup

Backup location SHALL comply with applicable residency policy.

Primary data placement compliance does not excuse non-compliant backup placement.

---

# 173. Disaster Recovery

Recovery procedures SHALL preserve:

```text id="5fxyw9"
tenant boundaries
encryption
audit integrity
residency
revocation state
```

---

# 174. Backup Restore Testing

Security-sensitive restore procedures SHALL be tested.

An untested backup SHALL not be treated as sufficient resilience.

---

# 175. Incident Containment

Baobab SHALL support containment at multiple scopes:

```text id="27vm2d"
identity
session
tenant
legal entity
estate
capability
provider
engine instance
region
```

---

# 176. Narrowest Effective Containment

Where safe, operators SHOULD contain the narrowest affected scope.

Example:

```text id="vd74zh"
compromised Pulse engine instance
```

should not require suspending all Baobab tenants if the issue can be isolated.

---

# 177. Blast Radius

Architecture SHALL minimise blast radius through:

```text id="2a20hh"
separate workload identities
tenant scoping
legal-entity scoping
provider isolation
regional boundaries
least privilege
secret scoping
```

---

# 178. Incident Impact Analysis

Security incidents SHALL be traceable through the dependency graph defined by ADR-BCP-008.

Example:

```text id="s3b1m0"
Compromised EngineInstance
        │
        ▼
Provider
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

---

# 179. Security Audit Trail

Incident response SHOULD be able to reconstruct:

```text id="xtv4gu"
who changed the configuration
which credentials were used
which contexts resolved
which providers served requests
which tenants were affected
when revocation occurred
```

---

# 180. ZuriBeans Security Fixture

Reference scenario:

```text id="3st34a"
Principal:
ZuriBeans buyer

Tenant:
ZuriBeans

Legal Entity:
ZuriBeans

Estate:
ZuriBeans B2B

Capability:
commerce.order.create

Market:
South Africa

Provider:
Baobab Trade / Medusa
```

A Thamani grant SHALL NOT satisfy this request.

---

# 181. Thamani Security Fixture

Reference scenario:

```text id="iqwgb2"
Principal:
Thamani customer

Tenant:
Thamani

Legal Entity:
Thamani Global

Estate:
Thamani B2C

Capability:
commerce.checkout.execute
```

A ZuriBeans B2B buyer role SHALL NOT confer authority here.

---

# 182. Cross-Border ZuriBeans Fixture

```text id="g8p2lt"
Origin:
Uganda

Destination:
South Africa

Business model:
B2B

Legal Entity:
ZuriBeans

Transaction currency:
USD
```

The platform SHALL independently validate:

```text id="jrd81j"
identity
tenant
legal entity
market participation
capability grant
origin/destination scope
provider region
residency
```

---

# 183. Coal Haulier Fixture

```text id="shl3yh"
Legal entity:
External logistics tenant

Route:
South Africa → Botswana → Zambia

Capability:
logistics.dispatch.manage
```

Botswana transit SHALL not imply Botswana legal presence.

---

# 184. Petroleum Trader Fixture

```text id="yy3m4w"
Business model:
B2B

Transaction:
cross-border commodity trade

Settlement:
USD

Multiple counterparties
Multiple jurisdictions
```

No IAM role explosion SHALL be introduced to model every trade dimension.

---

# 185. IAM Token Minimalism

IAM tokens SHOULD contain stable identity and coarse security information.

They SHOULD NOT contain:

```text id="dmlwza"
all markets
all estates
all capability grants
all supplier permissions
all pricing authorities
all trade workflows
```

---

# 186. Context Freshness

Business context SHALL be resolved from authoritative platform state rather than frozen indefinitely into tokens.

---

# 187. Tenant Membership Changes

If tenant membership changes, capability access SHALL cease within the applicable revocation latency.

---

# 188. Identity Deprovisioning

IAM lifecycle events SHALL invalidate affected platform access.

CP SHALL not continue authorizing a deprovisioned identity merely because historical mappings remain.

---

# 189. Mapping Retention

Historical identity mappings MAY be retained for audit.

Retention SHALL not imply active authorization.

---

# 190. Provider Identity Mapping

External engine identities SHALL map to canonical Baobab identity where required.

Provider-local IDs SHALL not become canonical identity authority.

---

# 191. Administrative Provider Accounts

Provider-native administrative accounts SHALL be tightly controlled.

Normal platform operations SHOULD use federated/workload identity where supported.

---

# 192. Emergency Provider-Native Access

Where an engine requires emergency native administration, credentials SHALL be:

```text id="sh3u5q"
protected
rotated
audited
rarely used
```

---

# 193. Security Headers and Edge Protection

Digital Estates and APIs SHOULD apply relevant web/API protections including:

```text id="tcfasv"
TLS
secure cookies
CSRF protection where applicable
CORS restrictions
content security controls
request-size limits
```

according to application type.

---

# 194. Browser Tokens

Browser applications SHALL follow secure OAuth/OIDC patterns defined by IAM architecture.

Tokens SHALL not be unnecessarily exposed to JavaScript where safer patterns are available.

---

# 195. BFF Preference

For security-sensitive Digital Estates, a BFF architecture SHOULD be preferred where it materially reduces token exposure and centralises trusted server-side context handling.

---

# 196. Supplier Portal

Supplier registration may initially involve unauthenticated or partially authenticated users.

Pre-approval identity SHALL not confer supplier business authority.

---

# 197. Supplier Approval

Supplier approval remains domain/business workflow state.

IAM identity activation and supplier business approval SHALL remain separate.

---

# 198. Buyer Approval

Likewise:

```text id="yzsxea"
authenticated buyer representative
```

does not automatically mean:

```text id="c80qwp"
approved commercial buyer
```

---

# 199. Security Contract Authority

`baobab-platform/shared` SHALL define canonical contracts for:

```text id="u8g3m5"
security reason codes
isolation profile
residency policy
security context
delegation context
capability authorization evidence
revocation events
provider quarantine events
security audit envelope
```

---

# 200. Shared SHALL Not Enforce Runtime Security

Shared defines contracts.

It SHALL not become a security runtime or policy server.

---

# 201. Policy Evaluation

CP MAY implement explicit policy evaluation modules.

However, policy evaluation SHALL remain understandable, testable and deterministic.

---

# 202. No Opaque Security Magic

A capability SHALL not be denied or allowed solely by an unexplainable policy mechanism without auditable reasons.

---

# 203. External Policy Engines

A future external policy engine MAY be adopted if justified.

It SHALL not displace:

```text id="vzcw5u"
CP authoritative context
capability grants
domain authorization
```

without a separate ADR.

---

# 204. Policy Versioning

Security-relevant policies SHALL be versioned.

Resolution evidence SHOULD identify applicable policy versions where useful.

---

# 205. Policy Change Impact

Before activating a high-impact security policy change, CP SHOULD determine affected:

```text id="8aprmj"
tenants
estates
capabilities
providers
regions
```

---

# 206. Security Configuration as Code

Security configuration SHOULD be reproducible and reviewable where practical.

However runtime tenant-specific state remains governed through authoritative CP workflows.

---

# 207. Manual Database Changes

Direct production database mutation SHALL NOT be the normal security-administration mechanism.

Emergency manual intervention SHALL be exceptional and documented.

---

# 208. Production Readiness Gate

No Baobab repository SHALL be considered production-ready merely because:

```text id="rjjpnx"
tests pass
container builds
application starts
```

Security readiness SHALL also cover:

```text id="gqu46n"
identity
authorization
tenant isolation
secrets
encryption
audit
revocation
failure behaviour
dependency security
incident response
```

---

# 209. Security Review Gates

## Gate 0 — Threat Model

Document:

```text id="86kmhm"
assets
actors
trust boundaries
attack surfaces
abuse cases
```

---

## Gate 1 — IAM Trust

Validate:

```text id="9f7e75"
issuer
audience
JWKS/key rotation
workload identity
user identity
assurance
```

---

## Gate 2 — Tenant Isolation

Audit every tenant-scoped repository/API path.

---

## Gate 3 — Legal Entity Isolation

Validate legal-entity boundaries independently of tenant boundaries.

---

## Gate 4 — Digital Estate and Channel Isolation

Test estate spoofing and channel escalation.

---

## Gate 5 — Capability Authorization

Validate grant source, scope, effective dates and revocation.

---

## Gate 6 — Provider Security

Validate provider eligibility, provider identity, quarantine and failover.

---

## Gate 7 — Isolation Profiles

Implement and test mandatory isolation matching.

---

## Gate 8 — Residency

Implement residency-aware provider resolution and readiness.

---

## Gate 9 — Secret Management

Remove embedded secrets and implement rotation-capable secret references.

---

## Gate 10 — Database Security

Validate:

```text id="8vpepi"
least privilege
encrypted connections
migration role separation
tenant constraints
RLS feasibility
```

---

## Gate 11 — Cache Security

Test authorization cache isolation and revocation.

---

## Gate 12 — Event Security

Validate:

```text id="1ymvmc"
event authentication
schema validation
tenant context
idempotency
ordering safeguards
```

---

## Gate 13 — Administrative Security

Implement least-privilege admin roles, audit and break-glass controls.

---

## Gate 14 — Supply-Chain Security

Validate:

```text id="6kqlkg"
dependencies
container images
CI identities
branch protection
build provenance
```

---

## Gate 15 — Security Telemetry

Implement security-relevant metrics, logs, audit and alerts.

---

## Gate 16 — Revocation Testing

Measure actual revocation propagation latency.

---

## Gate 17 — Failure Testing

Test every critical dependency failure with security invariants enabled.

---

## Gate 18 — Multi-Region Security

Validate region failure, replication lag, residency and DR.

---

## Gate 19 — ZuriBeans / Thamani Isolation

Prove that two NABHOLD subsidiaries sharing infrastructure cannot cross operational boundaries.

---

## Gate 20 — External Tenant Validation

Use coal-haulage or commodity-trading fixtures to ensure controls are generic.

---

## Gate 21 — Penetration and Abuse Testing

Perform focused testing against:

```text id="gppnqk"
IDOR
context spoofing
tenant hopping
provider forcing
privilege escalation
token confusion
cache poisoning
webhook replay
```

---

## Gate 22 — Production Security Review

No production approval until critical/high findings are resolved or formally risk-accepted.

---

# 210. Security Definition of Done

ADR-BCP-009 SHALL be considered implemented when:

1. IAM, CP and domain authorization responsibilities are separate.
2. Deny-by-default is enforced.
3. Security ambiguity fails closed.
4. Tenant isolation is tested.
5. Legal-entity isolation is independently tested.
6. Digital Estate spoofing is prevented.
7. Channel context cannot create entitlement.
8. Market context is validated.
9. Business geography and deployment geography remain separate.
10. Residency policy participates in provider eligibility.
11. Isolation profile participates in provider eligibility.
12. Isolation cannot silently downgrade.
13. Production cannot resolve to non-production engines.
14. ZuriBeans and Thamani remain isolated despite common ownership.
15. Shared infrastructure does not imply shared business data.
16. Workloads have distinct identities.
17. Service tokens are audience restricted.
18. Least privilege is enforced.
19. Secrets are externalised.
20. Secret rotation is supported.
21. Tokens and secrets are excluded from logs.
22. Capability grants retain provenance.
23. Multiple grant sources are handled correctly.
24. Source-specific revocation works.
25. Tenant suspension invalidates access.
26. Estate suspension is independently enforceable.
27. Provider quarantine works.
28. Failover cannot weaken security.
29. Authorization caches are tenant/context complete.
30. Cache revocation is bounded.
31. Resolution assertions, if implemented, are short-lived and cryptographically restricted.
32. Administrative APIs use stronger security.
33. Break-glass access is bounded and audited.
34. Confused-deputy protection exists.
35. AI/workload delegation is explicit.
36. IDOR protections exist.
37. Database runtime does not use superuser authority.
38. Cross-tenant relationships are constrained.
39. Webhooks are authenticated and replay-aware.
40. Events carry validated tenant context.
41. Security telemetry exists.
42. Critical revocation latency is measured.
43. Security-sensitive replication lag is bounded.
44. Backups respect residency.
45. DR preserves security state.
46. Incident containment can occur at multiple scopes.
47. Threat models are documented.
48. Security regression tests exist.
49. Supply-chain controls exist.
50. Production security review is mandatory.

---

# 211. Rejected Alternatives

## Keycloak owns all authorization

Rejected.

## CP owns all business authorization

Rejected.

## Domain engines independently determine tenant context

Rejected.

## Tenant equals legal entity universally

Rejected.

## Parent company ownership implies subsidiary access

Rejected.

## Destination country determines legal entity

Rejected.

## Cross-border trade implies unrestricted cross-border data movement

Rejected.

## Shared provider implies shared tenant data

Rejected.

## Shared infrastructure implies shared authorization

Rejected.

## Internal network equals trusted network

Rejected.

## Long-lived static service credentials

Rejected.

## Global generic service identity

Rejected.

## Default tenant fallback

Rejected.

## Default provider fallback

Rejected.

## Isolation downgrade during outage

Rejected.

## Residency bypass during outage

Rejected.

## Security controls disabled to preserve availability

Rejected.

## Events as sole revocation mechanism

Rejected.

## Unlimited authorization caching

Rejected.

## Business permissions embedded exhaustively in IAM tokens

Rejected.

## Permanent unrestricted platform administrator

Rejected.

## Provider health automatically clears security quarantine

Rejected.

## Direct production database edits as normal administration

Rejected.

---

# 212. Consolidated Security Architecture

```text id="gct1zn"
                             PRINCIPAL
                                 │
                                 ▼
                       ┌─────────────────┐
                       │   BAOBAB IAM    │
                       │                 │
                       │ Identity        │
                       │ Authentication  │
                       │ Assurance       │
                       │ Workload ID     │
                       └────────┬────────┘
                                │
                                ▼
                     ┌──────────────────────┐
                     │      BAOBAB CP       │
                     │                      │
                     │ Canonical Identity   │
                     │       ↓              │
                     │ Tenant               │
                     │       ↓              │
                     │ Legal Entity         │
                     │       ↓              │
                     │ Digital Estate       │
                     │       ↓              │
                     │ Market / Geography   │
                     │       ↓              │
                     │ Capability Grant     │
                     │       ↓              │
                     │ Capability Scope     │
                     │       ↓              │
                     │ Isolation Policy     │
                     │       ↓              │
                     │ Residency Policy     │
                     │       ↓              │
                     │ Binding              │
                     │       ↓              │
                     │ Provider Eligibility │
                     └──────────┬───────────┘
                                │
                         platform ALLOW
                                │
                                ▼
                 ┌────────────────────────────┐
                 │     CAPABILITY PROVIDER    │
                 │                            │
                 │ Workload authentication    │
                 │ Context validation         │
                 │ Domain authorization       │
                 │ Business invariants        │
                 └──────────────┬─────────────┘
                                │
                                ▼
                        BUSINESS OPERATION
```

Cross-cutting every layer:

```text id="umvq1s"
┌────────────────────────────────────────────────────┐
│ Encryption                                         │
│ Least Privilege                                    │
│ Audit                                              │
│ Observability                                      │
│ Revocation                                         │
│ Isolation                                          │
│ Residency                                          │
│ Secret Management                                  │
│ Incident Containment                               │
│ Supply-Chain Security                              │
└────────────────────────────────────────────────────┘
```

---

# 213. Relationship to the Complete ADR Sequence

With ADR-BCP-009, the foundational capability-centric Control Plane architecture is:

```text id="e7kxe6"
ADR-BCP-001
Parent Implementation Contract
        │
        ▼
ADR-BCP-002
Capability-Centric Architecture
        │
        ▼
ADR-BCP-003
Capabilities / Grants / Scopes / Bindings / Resolution
        │
        ▼
ADR-BCP-004
Context / Markets / Geography / Legal Entities / Estates
        │
        ▼
ADR-BCP-005
Products / Compositions / Subscriptions / Provisioning
        │
        ▼
ADR-BCP-006
Providers / Engines / Health / Failover / Migration
        │
        ▼
ADR-BCP-007
Runtime APIs / Caching / Service Consumption
        │
        ▼
ADR-BCP-008
Audit / Observability / Reconciliation / Readiness
        │
        ▼
ADR-BCP-009
Security / Isolation / Residency / Revocation / Failure
```

Together they establish the platform chain:

```text id="wr97a2"
IDENTITY
   ↓
CONTEXT
   ↓
PRODUCT
   ↓
ENTITLEMENT
   ↓
CAPABILITY
   ↓
SCOPE
   ↓
BINDING
   ↓
PROVIDER
   ↓
ENGINE INSTANCE
   ↓
DOMAIN AUTHORIZATION
   ↓
BUSINESS OPERATION
```

with:

```text id="2d4nlm"
SECURITY
AUDIT
OBSERVABILITY
READINESS
RECONCILIATION
```

cross-cutting the entire system.

---

# 214. Decision

**ACCEPTED TARGET SECURITY ARCHITECTURE, subject to formal approval.**

Upon approval:

1. Baobab SHALL operate deny-by-default.
2. IAM, CP and domain authorization SHALL remain separate.
3. tenant and legal-entity isolation SHALL be independently enforced.
4. common corporate ownership SHALL confer no implicit operational authority.
5. Digital Estates SHALL not be trusted as identity or entitlement authorities.
6. capability grants SHALL remain explicit and revocable.
7. provider bindings SHALL not create entitlement.
8. provider eligibility SHALL enforce isolation and residency.
9. failover SHALL never weaken mandatory security requirements.
10. production and non-production SHALL remain isolated.
11. workloads SHALL use distinct, least-privilege identities.
12. secrets SHALL be externalised and rotatable.
13. revocation SHALL be propagated through authoritative state, events, invalidation and bounded TTL.
14. security-sensitive ambiguity SHALL fail closed.
15. cross-border business activity SHALL not imply unrestricted cross-border data movement.
16. administrative privilege SHALL be minimised and audited.
17. provider compromise SHALL support quarantine and impact analysis.
18. security controls SHALL remain effective during degraded operation.
19. availability SHALL never override mandatory isolation, residency or identity integrity.
20. production approval SHALL require demonstrable security tests rather than architectural assertions alone.

---

# 215. Final Architectural Maxims

> **Authentication is not entitlement. Entitlement is not routing. Routing is not business authorization.**

> **A shared platform may share infrastructure without sharing authority, identity, business state or trust.**

> **No corporate relationship, Digital Estate, market, country, provider or network location shall silently create authority. Authority must always be established explicitly.**

And the governing failure principle:

> **When Baobab must choose between being unavailable and violating identity integrity, tenant isolation, legal-entity isolation, mandatory residency or mandatory security policy, Baobab shall fail securely.**