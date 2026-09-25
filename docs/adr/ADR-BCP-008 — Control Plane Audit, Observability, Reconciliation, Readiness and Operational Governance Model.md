# ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model

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
- ADR-BCP-007 — Control Plane APIs, Capability Resolution Contracts, Caching, Resolution Assertions and Service-to-Service Consumption Model
- ADR-SHARED-007 — Canonical Capability Contracts, Composition Registry and Cross-Engine Provider Model

**Applies To:** Audit, event history, observability, readiness, reconciliation, drift detection, provisioning state, operational diagnostics, impact analysis, runbooks, SLOs, operator workflows and production governance.

**Architecture Style:** Observable, explainable, auditable, reconciled, fail-safe, operationally governed.

**Decision Type:** Foundational production-operations architecture.

---

# 1. Executive Decision

The Baobab Control Plane SHALL treat:

```text id="ru1d0a"
Audit
Observability
Reconciliation
Readiness
Operational Governance
```

as first-class platform capabilities.

They SHALL NOT be treated as optional logging concerns added after feature implementation.

The Control Plane SHALL continuously answer five operational questions:

```text id="dkxvz3"
1. What was supposed to exist?
2. What actually exists?
3. What changed?
4. Why did the platform make a decision?
5. Is the platform currently able to deliver the promised capability?
```

The governing principle is:

> **If the Control Plane can make a consequential decision, it must be able to explain, observe, audit and reconcile that decision.**

---

# 2. Why This ADR Is Required

Previous ADRs define:

```text id="3cxaf2"
tenants
legal entities
Digital Estates
products
subscriptions
capability grants
bindings
providers
engine instances
contexts
resolution
```

Those models are insufficient for production if operators cannot determine:

```text id="7z4wj3"
why a capability failed
whether a tenant is ready
whether provider state drifted
whether a grant was revoked
which estate is impacted
which provider is degraded
which change caused an outage
```

Operational opacity is itself an architectural defect.

---

# 3. Core Operational Model

The target operational loop SHALL be:

```text id="kv0q6p"
Desired State
     │
     ▼
Observed State
     │
     ▼
Comparison
     │
     ▼
Drift Detection
     │
     ▼
Reconciliation
     │
     ▼
Readiness Evaluation
     │
     ▼
Operational Decision
     │
     ▼
Audit + Metrics + Traces + Events
```

---

# 4. Five Distinct Concerns

The following SHALL remain distinct:

```text id="d48tjk"
Audit
≠
Logs
≠
Metrics
≠
Traces
≠
Readiness
≠
Reconciliation
```

Each has a different purpose.

---

# 5. Audit

Audit answers:

> Who changed what, when, under which authority, and with what result?

Audit SHALL be durable and suitable for security, support and governance investigation.

---

# 6. Logs

Logs answer:

> What happened inside a component while it was executing?

Logs are operational diagnostics.

They SHALL NOT substitute for structured audit history.

---

# 7. Metrics

Metrics answer:

> How often, how fast, how many, and how healthy?

They support:

```text id="30b6qp"
dashboards
alerts
capacity planning
SLOs
trend analysis
```

---

# 8. Traces

Traces answer:

> Which distributed path did this operation take?

Tracing SHALL connect:

```text id="ijz521"
Digital Estate
→ IAM
→ CP
→ provider
→ downstream services
```

---

# 9. Readiness

Readiness answers:

> Can this platform configuration currently deliver the required capability?

Readiness SHALL aggregate dependencies.

---

# 10. Reconciliation

Reconciliation answers:

> Does observed runtime state match declared desired state?

If not:

```text id="v55kr9"
drift exists.
```

---

# 11. Desired State

The Control Plane SHALL maintain desired state for platform-owned configuration.

Examples:

```text id="cz2trj"
Tenant T is active.
Tenant T has product X.
Product X requires capabilities A, B and C.
Capability B must resolve through provider P.
Provider P requires engine instance E.
Estate Z requires capabilities A and B.
```

Desired state SHALL be explicit and versioned where practical.

---

# 12. Observed State

Observed state reflects what is currently known to exist.

Examples:

```text id="6k5dxf"
grant exists
binding exists
provider healthy
engine instance registered
provider tenant configuration exists
estate available
```

Observed state MAY come from:

```text id="4gmlta"
CP database
provider adapters
health probes
infrastructure systems
event streams
```

---

# 13. Desired vs Observed

Conceptually:

```text id="1u20hw"
Desired:
commerce.order.create
must be available to ZuriBeans

Observed:
grant exists
binding exists
Medusa provider healthy

Result:
READY
```

Alternative:

```text id="z8hbkk"
Desired:
finance.invoice.issue
must be available

Observed:
grant exists
binding missing

Result:
DRIFT + NOT_READY
```

---

# 14. Drift

Drift SHALL be a first-class operational concept.

Conceptually:

```text id="h2lggr"
Drift
├── id
├── resource_type
├── resource_id
├── tenant_id
├── desired_state
├── observed_state
├── drift_type
├── severity
├── detected_at
├── status
└── remediation
```

---

# 15. Drift Types

Initial drift types SHOULD include:

```text id="mbs47c"
MISSING_RESOURCE
UNEXPECTED_RESOURCE
STATE_MISMATCH
VERSION_MISMATCH
CONFIGURATION_MISMATCH
PROVIDER_MISMATCH
CONTRACT_MISMATCH
SCOPE_MISMATCH
HEALTH_MISMATCH
ISOLATION_MISMATCH
RESIDENCY_MISMATCH
```

---

# 16. Drift Severity

Recommended levels:

```text id="ishio5"
INFO
WARNING
DEGRADED
CRITICAL
```

Severity SHALL reflect impact, not only technical difference.

---

# 17. Reconciliation Loop

The canonical reconciliation loop SHALL be:

```text id="f8ld8e"
Read Desired State
        │
        ▼
Observe Current State
        │
        ▼
Compute Diff
        │
        ▼
Classify Drift
        │
        ▼
Safe to Auto-Repair?
       / \
     YES  NO
      │    │
      ▼    ▼
  Reconcile  Raise Operational Action
      │
      ▼
Re-observe
      │
      ▼
Evaluate Readiness
```

---

# 18. Reconciliation Is Idempotent

Running the reconciler repeatedly against an already-correct state SHALL produce no harmful change.

The desired property is convergence.

---

# 19. Reconciliation Is Not Arbitrary Mutation

The reconciler SHALL only manage resources within explicitly declared platform authority.

For example:

```text id="9av6nj"
CP may reconcile:
grant
binding
provider registration
tenant-specific provider configuration
```

It SHALL NOT blindly mutate:

```text id="j6oa7a"
orders
invoices
shipments
supplier approvals
```

because those belong to business domains.

---

# 20. Auto-Repair Classification

Not all drift is safe to repair automatically.

Example:

```text id="2bq72t"
missing derived grant
```

may be safely recreated.

But:

```text id="40raob"
unexpected ERP ledger state
```

is not CP reconciliation territory.

---

# 21. Reconciliation Policy

Each drift class SHOULD define:

```text id="4gkpa3"
detectable
auto-repairable
manual-review-required
blocking
non-blocking
```

---

# 22. Readiness Hierarchy

Baobab SHALL evaluate readiness at multiple levels:

```text id="cs9qg2"
Provider Readiness
Capability Readiness
Composition Readiness
Product Readiness
Digital Estate Readiness
Tenant Readiness
Platform Readiness
```

These SHALL not be conflated.

---

# 23. Provider Readiness

Provider readiness answers:

> Is this provider able to serve a capability for the specified scope?

It may depend on:

```text id="e2otmd"
provider lifecycle
engine instance lifecycle
health
contract compatibility
tenant configuration
residency
isolation
region
credentials
```

---

# 24. Capability Readiness

Capability readiness answers:

> Can capability C resolve now for context X?

Conceptually:

```text id="bgpm0n"
Capability Ready
=
active capability
+
effective grant
+
satisfied dependencies
+
valid binding
+
eligible provider
+
eligible engine instance
```

---

# 25. Product Readiness

Product readiness SHALL aggregate mandatory capabilities from ADR-BCP-005.

Example:

```text id="p5884h"
XBT
├── commercial.contract.manage    READY
├── finance.invoice.manage        READY
├── logistics.shipment.manage     NOT_READY
└── intelligence.fx.query         READY
```

If shipment is mandatory:

```text id="cl4ka0"
Product = NOT_READY
```

---

# 26. Estate Readiness

A Digital Estate SHALL be READY only when its mandatory platform dependencies are ready.

Conceptually:

```text id="8byy2e"
EstateReady
=
context ready
AND
identity integration ready
AND
mandatory subscriptions ready
AND
mandatory capabilities ready
AND
provider topology ready
```

---

# 27. Tenant Readiness

Tenant readiness SHALL be a summary.

It MAY report:

```text id="2500l1"
READY
DEGRADED
BLOCKED
```

but SHALL always permit drill-down.

---

# 28. Recommended Readiness States

```text id="e7ddst"
UNKNOWN
PROVISIONING
READY
DEGRADED
NOT_READY
BLOCKED
SUSPENDED
```

---

# 29. Readiness Reason Codes

Every non-READY state SHOULD include structured reasons.

Examples:

```text id="6hk5bn"
MISSING_GRANT
MISSING_BINDING
PROVIDER_UNAVAILABLE
CONTRACT_INCOMPATIBLE
MARKET_CONTEXT_INVALID
RESIDENCY_MISMATCH
ISOLATION_MISMATCH
PROVISIONING_FAILED
RECONCILIATION_PENDING
```

---

# 30. Readiness Is Scope-Sensitive

A capability may be:

```text id="ew2nkt"
READY in Uganda
NOT_READY in South Africa
```

or:

```text id="wrvgp7"
READY for B2B
NOT_READY for B2C
```

Therefore readiness SHALL be context-aware.

---

# 31. Health vs Readiness

This remains an architectural invariant:

```text id="036mg3"
Health
≠
Readiness
```

Example:

```text id="g8lqcx"
Provider healthy
but tenant has no valid binding
```

Result:

```text id="x8d950"
health = HEALTHY
readiness = NOT_READY
```

---

# 32. Audit Event Model

Audit records SHALL capture security- and governance-relevant state changes.

Conceptually:

```text id="t06u7g"
AuditRecord
├── audit_id
├── occurred_at
├── actor
├── actor_type
├── tenant_id
├── legal_entity_id
├── resource_type
├── resource_id
├── action
├── previous_state
├── new_state
├── result
├── reason_code
├── correlation_id
├── causation_id
├── request_id
├── source_service
└── metadata
```

---

# 33. Audit Actors

Actor types MAY include:

```text id="ij0ysp"
USER
WORKLOAD
SYSTEM
RECONCILER
ADMINISTRATOR
MIGRATION
AUTOMATION
```

---

# 34. Audit Actions

At minimum, audit SHALL capture changes to:

```text id="rkcw5a"
tenant
legal entity
Digital Estate
market participation
subscription
capability grant
capability binding
provider
engine instance
isolation profile
residency policy
product provisioning
provider migration
```

---

# 35. Resolution Audit

Not every high-volume resolution must produce a full transactional audit record.

However security-relevant decisions SHALL be reconstructable.

The platform MAY separate:

```text id="s35jmm"
configuration audit
resolution audit
operational telemetry
```

to avoid overloading one storage system.

---

# 36. Security-Sensitive Resolution Audit

Capabilities involving:

```text id="ejkhtf"
administration
payments
financial posting
tenant management
privileged identity operations
```

MAY require stronger durable resolution audit.

---

# 37. Audit Immutability

Audit records SHALL be append-oriented.

They SHALL not be silently rewritten.

Corrections SHALL produce new records where appropriate.

---

# 38. Audit Retention

Retention SHALL be policy-driven according to:

```text id="8pzqu5"
security
legal obligations
operational requirements
tenant contracts
data classification
```

---

# 39. Audit Data Minimisation

Audit SHALL not become an uncontrolled store of:

```text id="hl0l14"
passwords
tokens
API keys
payment details
sensitive business payloads
```

Secrets SHALL never be logged.

---

# 40. Correlation

Every consequential platform request SHOULD carry:

```text id="zekbtm"
correlation_id
```

and, where applicable:

```text id="z2p8b1"
causation_id
request_id
resolution_id
```

---

# 41. Correlation Chain

Example:

```text id="o2m7iu"
Digital Estate Request
      │
      ▼
correlation_id = C1
      │
      ▼
Context Resolution
      │
      ▼
Capability Resolution R1
      │
      ▼
Provider Invocation
      │
      ▼
Domain Event
```

All should remain traceable to C1.

---

# 42. Distributed Tracing

The platform SHOULD implement distributed tracing across:

```text id="y6rhli"
gateway
estate/BFF
IAM interactions
CP
provider adapters
domain engines
event publication
```

---

# 43. Trace Attributes

Useful attributes MAY include:

```text id="brphzs"
tenant.id
legal_entity.id
digital_estate.id
capability.key
provider.key
engine.instance
resolution.result
reason_code
```

Sensitive attributes SHALL be omitted or appropriately controlled.

---

# 44. High-Cardinality Warning

Identifiers such as:

```text id="1h9wpu"
tenant_id
user_id
order_id
```

SHALL not be attached indiscriminately to metrics labels.

They MAY appear in traces/logs/audit where appropriate.

---

# 45. Metrics Model

Metrics SHOULD cover five major dimensions:

```text id="yrmzc9"
Resolution
Readiness
Reconciliation
Provider Topology
Platform Operations
```

---

# 46. Resolution Metrics

Recommended:

```text id="8uo0df"
capability_resolution_total
capability_resolution_duration_seconds
capability_resolution_denied_total
capability_resolution_ambiguous_total
capability_resolution_cache_hit_total
capability_resolution_cache_miss_total
```

---

# 47. Readiness Metrics

Recommended:

```text id="rvnuuz"
capability_readiness_status
product_readiness_status
estate_readiness_status
tenant_readiness_status
readiness_transition_total
```

---

# 48. Reconciliation Metrics

Recommended:

```text id="x4zkvx"
reconciliation_run_total
reconciliation_duration_seconds
reconciliation_failure_total
drift_detected_total
drift_repaired_total
drift_unresolved_total
```

---

# 49. Provider Metrics

Recommended:

```text id="ju7s2m"
provider_health_status
engine_instance_health_status
provider_failover_total
provider_migration_total
provider_configuration_drift_total
```

---

# 50. Platform Metrics

Recommended:

```text id="0i763i"
http_request_duration_seconds
http_request_total
database_query_duration_seconds
outbox_pending_total
event_publish_failure_total
cache_operation_failure_total
```

---

# 51. SLOs

Production SHALL define measurable SLOs for critical platform paths.

Examples SHOULD include:

```text id="dj7zv5"
context resolution availability
capability resolution availability
capability resolution latency
reconciliation freshness
event publication latency
readiness evaluation freshness
```

Exact targets SHALL be established during production-readiness planning.

---

# 52. SLIs

SLIs SHALL measure actual behaviour.

Example:

```text id="gdtb7v"
percentage of valid resolution requests completed successfully
within target latency
```

rather than merely:

```text id="6ve14m"
CP process is running
```

---

# 53. Error Budgets

The platform SHOULD use error budgets for critical services where operational maturity warrants it.

Error budgets MAY guide:

```text id="3jxt8o"
release pace
change freezes
reliability work
capacity work
```

---

# 54. Alerts

Alerts SHALL be actionable.

Do NOT page operators merely because:

```text id="ao5nxj"
a metric exists.
```

Alerts SHOULD indicate conditions requiring intervention.

---

# 55. Example Critical Alerts

```text id="uu5sqt"
capability resolution failure rate above threshold
binding ambiguity detected
critical provider unavailable
tenant isolation violation detected
outbox backlog growing
reconciliation stale
mandatory capability not ready
database unavailable
```

---

# 56. Alert Severity

Recommended:

```text id="zavfxd"
P1 CRITICAL
P2 HIGH
P3 WARNING
P4 INFORMATIONAL
```

Exact mapping SHALL align with operational governance.

---

# 57. Alert Deduplication

One provider outage affecting 50 capabilities SHALL not necessarily produce 50 independent pages.

Alerts SHOULD aggregate shared root causes.

---

# 58. Root-Cause Relationships

Operational models SHOULD retain dependency relationships.

Example:

```text id="uf2yjh"
Engine Instance Down
      │
      ▼
Provider Unavailable
      │
      ▼
3 Capability Bindings Broken
      │
      ▼
2 Estates Degraded
```

The operator should see both cause and impact.

---

# 59. Impact Analysis

The Control Plane SHALL support impact queries.

Examples:

```text id="0wnq05"
If provider P fails, which tenants are affected?
If capability C is deprecated, which products depend on it?
If market M is suspended, which estates lose readiness?
If binding B is removed, which contexts fail?
```

---

# 60. Reverse Dependency Graph

Conceptually:

```text id="9ih9gt"
EngineInstance
      │
      ▼
Provider
      │
      ▼
Binding
      │
      ▼
Capability
      │
      ▼
Grant
      │
      ▼
Subscription
      │
      ▼
Digital Estate
      │
      ▼
Tenant
```

The system SHOULD be able to traverse this graph in both directions.

---

# 61. Operational Explainability

An operator SHALL be able to ask:

> Why is ZuriBeans not ready in South Africa?

and receive a structured answer such as:

```text id="xq16ll"
Tenant: READY
Estate: READY
Market participation: READY
Capability commerce.order.create: READY
Capability finance.invoice.issue: NOT_READY
Reason:
BINDING_NOT_FOUND
```

---

# 62. Resolution Explainability

An operator SHALL be able to ask:

> Why did capability X resolve to provider Y?

and receive:

```text id="rdly73"
grant matched
scope matched
binding A rejected: wrong market
binding B matched
provider healthy
contract compatible
binding B most specific
```

---

# 63. Explainability Shall Be Privileged

Detailed resolution diagnostics may reveal:

```text id="q2bgmr"
topology
tenant policy
provider structure
```

and SHALL therefore require appropriate authorization.

---

# 64. Tenant-Facing Diagnostics

A reduced tenant-facing diagnostics view MAY expose:

```text id="phoj0e"
capability unavailable
configuration incomplete
service degraded
```

without exposing internal topology.

---

# 65. Reconciliation Categories

The Control Plane SHOULD distinguish:

```text id="zjwmtv"
CONFIGURATION_RECONCILIATION
ENTITLEMENT_RECONCILIATION
PROVIDER_RECONCILIATION
TOPOLOGY_RECONCILIATION
READINESS_RECONCILIATION
```

---

# 66. Configuration Reconciliation

Validates control-plane internal state.

Examples:

```text id="1u2r73"
Digital Estate references active tenant
binding references valid provider
scope references existing market
```

---

# 67. Entitlement Reconciliation

Validates:

```text id="fzlp4e"
subscriptions
compositions
grants
```

Example:

```text id="ib1v3y"
subscription expects capability A
grant missing
```

---

# 68. Provider Reconciliation

Validates provider-specific required state.

Example:

```text id="gj1y7v"
CP expects Medusa sales channel mapping
provider adapter reports missing
```

---

# 69. Topology Reconciliation

Validates:

```text id="nbf802"
engine instance registration
region
isolation
residency
version
health probe configuration
```

---

# 70. Readiness Reconciliation

Re-evaluates readiness after:

```text id="mnn3qe"
grant change
binding change
provider health change
subscription change
market change
tenant change
```

---

# 71. Event-Driven Reconciliation

Relevant changes SHOULD trigger reconciliation.

Example:

```text id="vi9pnl"
provider.health.changed
      │
      ▼
affected bindings
      │
      ▼
affected capabilities
      │
      ▼
readiness recomputation
```

---

# 72. Periodic Reconciliation

Event-driven processing alone SHALL not be trusted indefinitely.

Periodic reconciliation SHALL detect:

```text id="yl7ixg"
lost events
manual provider changes
data drift
partial failures
```

---

# 73. Event + Periodic Model

Preferred:

```text id="87jkx1"
Events
  → fast convergence

Periodic reconciliation
  → eventual correctness
```

---

# 74. Reconciliation Scheduling

Frequency MAY depend on resource type.

Examples:

```text id="inzzvb"
critical provider health     frequent
subscription drift          moderate
static product definition   infrequent
```

Exact schedules belong to operational configuration.

---

# 75. Concurrency Control

Only one reconciler SHOULD mutate a given reconciliation key concurrently.

Possible keys:

```text id="fkb2s8"
tenant
subscription
provider
engine instance
Digital Estate
```

The implementation MAY use:

```text id="89vpnh"
database advisory locks
leases
work queues
```

provided correctness is maintained.

---

# 76. Reconciliation Idempotency

Every reconciler action SHALL be safe to retry.

A timeout SHALL not leave the operator unable to determine whether the mutation succeeded.

---

# 77. Reconciliation State

Conceptually:

```text id="9pjpye"
ReconciliationRun
├── id
├── resource_type
├── resource_id
├── desired_version
├── observed_version
├── started_at
├── completed_at
├── result
├── changes_applied
├── drift_detected
├── retry_count
├── reason_code
└── correlation_id
```

---

# 78. Reconciliation Results

Recommended:

```text id="p2lwra"
IN_SYNC
CHANGED
DEGRADED
BLOCKED
FAILED
```

---

# 79. Dead-Letter Handling

Repeatedly failing reconciliation work SHALL not retry forever without visibility.

After policy-defined attempts:

```text id="dx3f26"
move to operational intervention
```

with full diagnostics.

---

# 80. Readiness Snapshot

Readiness SHOULD be queryable as a snapshot.

Conceptually:

```text id="b5f7hq"
ReadinessSnapshot
├── subject_type
├── subject_id
├── context_scope
├── status
├── reasons[]
├── dependency_statuses[]
├── evaluated_at
├── expires_at
└── version
```

---

# 81. Readiness Freshness

Readiness snapshots SHALL have freshness semantics.

Stale readiness SHALL not be displayed as current indefinitely.

---

# 82. Computed vs Persisted Readiness

Readiness MAY be:

```text id="a50qyb"
computed on demand
cached
materialised
```

depending on performance needs.

The authoritative basis remains underlying platform state.

---

# 83. Product Activation Gate

ADR-BCP-005 requires product readiness before activation.

ADR-BCP-008 formalises that gate.

Conceptually:

```text id="exvw52"
Provisioning
    │
    ▼
Reconcile
    │
    ▼
Evaluate mandatory capabilities
    │
    ▼
READY?
   / \
YES   NO
 │     │
 ▼     ▼
ACTIVE BLOCKED
```

---

# 84. Degraded Operation

Some products MAY permit degraded operation.

Example:

```text id="jep7fr"
core commerce ready
advanced intelligence unavailable
```

If intelligence is optional:

```text id="ld2ezs"
Product = DEGRADED or READY_WITH_WARNINGS
```

depending on canonical status policy.

---

# 85. Mandatory Dependency Failure

Mandatory capability failure SHALL never be masked by an overall `READY`.

---

# 86. Provider Failure Propagation

Example:

```text id="to5ubf"
Haystack unavailable
      │
      ▼
intelligence.market.query NOT_READY
      │
      ▼
XBT intelligence profile DEGRADED
      │
      ▼
ZuriBeans estate DEGRADED
```

The impact chain SHALL be computable.

---

# 87. ZuriBeans Example

Operational view:

```text id="3zc5o4"
Tenant: ZuriBeans

Estate:
B2B Portal              READY

Product:
Baobab XBT              READY

Capabilities:
buyer.onboard           READY
supplier.onboard        READY
commerce.order.create   READY
finance.invoice.issue   READY
intelligence.fx.query   READY

Providers:
Trade                   HEALTHY
ERP                     HEALTHY
Pulse                   HEALTHY
```

---

# 88. ZuriBeans Degraded Example

```text id="7o7mx0"
Pulse Provider          UNAVAILABLE

Affected:
intelligence.fx.query
intelligence.market.query

Product:
XBT                     DEGRADED

Core B2B order flow:
READY
```

This distinction prevents unnecessary total outage.

---

# 89. Thamani Isolation Example

If Thamani and ZuriBeans share provider technology:

```text id="d491vv"
Medusa instance degraded
```

impact analysis SHALL still distinguish:

```text id="hnl1d7"
ZuriBeans bindings
Thamani bindings
```

and their different readiness states.

---

# 90. External Tenant Example

A coal-haulage tenant may use a separate logistics provider.

Failure of that provider SHALL NOT incorrectly mark:

```text id="6m9fdk"
Thamani
ZuriBeans
```

as degraded unless they actually depend on it.

---

# 91. Change Management

High-impact control-plane changes SHOULD follow:

```text id="3z7e6b"
PLAN
   │
   ▼
IMPACT ANALYSIS
   │
   ▼
CHANGE
   │
   ▼
OBSERVE
   │
   ▼
RECONCILE
   │
   ▼
VERIFY READINESS
```

---

# 92. Change Risk Classification

Changes SHOULD be classified.

Example:

```text id="za40wx"
LOW
MEDIUM
HIGH
CRITICAL
```

Potentially critical changes include:

```text id="2xcfss"
tenant suspension
global binding change
provider retirement
isolation policy change
residency policy change
product composition breaking change
```

---

# 93. Approval Policy

Certain critical changes MAY require:

```text id="ag1fog"
dual approval
separation of duties
change window
```

The platform SHALL support such governance without forcing it onto every low-risk change.

---

# 94. Emergency Change

Emergency changes SHALL be possible.

They SHALL always generate enhanced audit.

Example:

```text id="6enfgk"
emergency provider disable
```

SHALL record:

```text id="1scabb"
actor
reason
incident reference
affected resources
timestamp
```

---

# 95. Operational Runbooks

Production readiness SHALL include runbooks for at least:

```text id="201gw6"
CP unavailable
PostgreSQL unavailable
provider unavailable
binding ambiguity
grant corruption
cache invalidation failure
outbox backlog
reconciliation failure
tenant suspension
provider migration rollback
```

---

# 96. Runbook Ownership

Every critical alert SHOULD link to:

```text id="0a0in3"
runbook
owner
escalation path
```

where operational tooling permits.

---

# 97. Operational Ownership

Each capability/provider SHOULD identify an owning team or operational owner.

Example:

```text id="eb1s5u"
commerce.*     Trade team
finance.*      ERP team
content.*      CMS team
intelligence.* Pulse team
```

This prevents orphaned platform dependencies.

---

# 98. Incident Model

The platform SHOULD support incident correlation with:

```text id="ik45sm"
provider
capability
tenant
estate
market
region
```

without copying full incident-management functionality into CP.

---

# 99. Incident Blast Radius

Operators SHOULD be able to determine:

```text id="qpfnp3"
How many tenants?
Which estates?
Which legal entities?
Which capabilities?
Which products?
Which markets?
```

are affected.

---

# 100. Audit and Event Separation

Audit events and integration events SHALL not automatically be the same thing.

Example:

```text id="p9lr4u"
capability.grant.revoked
```

may be an integration event.

The corresponding audit record contains richer:

```text id="fiq72m"
actor
previous state
new state
administrative reason
```

---

# 101. Transactional Outbox

State changes that require canonical event publication SHALL use transactional outbox.

Conceptually:

```text id="tf8r0e"
BEGIN

UPDATE capability_grant ...

INSERT outbox_event ...

COMMIT
```

This pattern SHALL be used wherever event consistency matters.

---

# 102. Outbox State

Outbox SHOULD include:

```text id="smh1nc"
event_id
event_type
aggregate_type
aggregate_id
payload
created_at
publish_status
attempt_count
last_error
published_at
```

---

# 103. Outbox Retrying

Event publication SHALL be retryable.

Duplicate publication MAY occur.

Consumers SHALL therefore be idempotent.

---

# 104. Poison Events

Repeatedly unpublishable events SHALL move to controlled failure handling.

They SHALL not silently block the entire outbox.

---

# 105. Event Lag

Operators SHALL monitor:

```text id="6weuop"
oldest unpublished event age
pending event count
failed event count
```

---

# 106. Reconciliation Event Consumers

Reconciliation consumers SHALL store sufficient idempotency state to handle duplicate events safely.

---

# 107. Observability Data Classification

Operational telemetry SHALL respect data classification.

Do not expose tenant-confidential details in:

```text id="0o3rkw"
public dashboards
unrestricted logs
metrics labels
```

---

# 108. Sensitive Logging

The following SHALL be redacted or prohibited:

```text id="yebtka"
access tokens
refresh tokens
passwords
private keys
client secrets
full credential payloads
```

---

# 109. Structured Logging

Logs SHOULD be structured.

Recommended common fields:

```text id="fa3i37"
timestamp
level
service
environment
region
correlation_id
trace_id
tenant_context where appropriate
operation
reason_code
```

---

# 110. Stable Reason Codes

Operators SHALL rely on canonical reason codes rather than parsing arbitrary log messages.

---

# 111. Operational APIs

The Control Plane SHOULD expose privileged operational APIs such as:

```text id="epdzg9"
GET /v1/readiness/tenants/{id}
GET /v1/readiness/estates/{id}
GET /v1/readiness/capabilities/{key}

POST /v1/reconciliation/tenants/{id}
POST /v1/reconciliation/subscriptions/{id}

GET /v1/drift
GET /v1/impact/providers/{id}
GET /v1/impact/capabilities/{key}
```

Exact contracts SHALL originate in Shared.

---

# 112. Explain API

A privileged endpoint MAY provide:

```text id="uhhq6o"
POST /v1/operations/explain
```

for structured diagnostics.

---

# 113. Readiness API Shall Not Mutate

Readiness inspection SHALL be side-effect free.

Explicit reconciliation endpoints SHALL perform mutations where authorised.

---

# 114. Reconciliation APIs Are Privileged

Manual reconciliation may cause provider mutations.

It SHALL require strong administrative authorization.

---

# 115. Control Plane Self-Readiness

`baobab-cp` SHALL expose its own operational readiness.

At minimum:

```text id="4ojf36"
process alive
PostgreSQL connectivity
critical migrations applied
required configuration loaded
dependency readiness where mandatory
```

---

# 116. Liveness vs Readiness

These SHALL remain distinct.

```text id="m6p9ve"
Liveness:
Should this process be restarted?

Readiness:
Should this process receive traffic?
```

---

# 117. Dependency Failure and CP Readiness

Not every external provider outage SHALL make CP itself unready.

For example:

```text id="0xbs2p"
Pulse unavailable
```

should not necessarily remove CP from service.

CP can still resolve/report:

```text id="h27q6o"
PROVIDER_UNAVAILABLE
```

---

# 118. Database Failure

Loss of authoritative PostgreSQL access MAY make relevant CP instances unready because authoritative resolution cannot safely proceed.

---

# 119. Event Bus Failure

Event bus failure SHOULD degrade event publication/reconciliation freshness but SHALL not automatically make synchronous authoritative resolution impossible.

---

# 120. Cache Failure

Cache failure SHOULD degrade performance, not correctness.

CP SHALL be capable of authoritative uncached resolution where dependencies permit.

---

# 121. Graceful Degradation

The platform SHOULD distinguish:

```text id="d3u7wi"
functional degradation
```

from:

```text id="bdh1pd"
security degradation
```

Security controls SHALL not be relaxed to preserve availability.

---

# 122. Operational Fail-Closed Rule

If a security-relevant state cannot be established:

```text id="ikqovr"
deny
```

or mark:

```text id="9hgt2u"
NOT_READY
```

rather than fabricate readiness.

---

# 123. Migration Observability

Provider migrations from ADR-BCP-006 SHALL expose:

```text id="517h75"
migration stage
source provider
target provider
affected capabilities
affected tenants
canary scope
error rate
readiness
rollback state
```

---

# 124. Product Upgrade Observability

Product upgrades SHALL expose:

```text id="g15p8h"
current version
target version
capability diff
provider requirements
readiness blockers
migration progress
```

---

# 125. Tenant Onboarding Dashboard

Operators SHOULD be able to view onboarding as:

```text id="hb88v3"
Tenant              READY
Legal Entity        READY
IAM                 READY
Digital Estate      READY
Market              READY
Subscription        READY
Capability Grants   READY
Bindings            READY
Providers           READY
Overall             READY
```

---

# 126. Tenant Onboarding Failure

Example:

```text id="e0wpdn"
Tenant              READY
Legal Entity        READY
IAM                 READY
Digital Estate      READY
Subscription        READY
Capability Grants   READY
Bindings            NOT_READY
Provider            READY

Overall             BLOCKED
Reason:
MANDATORY_BINDING_MISSING
```

---

# 127. No Boolean-Only Readiness

The platform SHALL not expose only:

```text id="kpyvcb"
ready = false
```

without explanation.

Structured reasons are mandatory.

---

# 128. Historical Readiness

The platform SHOULD preserve enough history to answer:

> When did this estate become degraded, and why?

This may be represented through events/time-series telemetry rather than a full relational history table.

---

# 129. Change Correlation

Readiness transitions SHOULD be correlatable to recent changes.

Example:

```text id="x51xol"
10:02 binding updated
10:03 capability readiness -> NOT_READY
10:03 estate readiness -> DEGRADED
```

This dramatically improves incident diagnosis.

---

# 130. Audit Persistence

Transactional audit metadata MAY reside in PostgreSQL initially.

High-volume logs/traces/metrics SHOULD use dedicated observability systems in production.

CP SHALL not force all telemetry into PostgreSQL.

---

# 131. PostgreSQL Responsibilities

PostgreSQL SHOULD retain authoritative/control-plane state such as:

```text id="itb18i"
desired state
grants
bindings
subscriptions
provisioning
audit index/reference
outbox
reconciliation state
drift state where needed
```

---

# 132. Observability Backend Independence

The ADR SHALL not mandate a single:

```text id="8bq9py"
metrics vendor
logging vendor
tracing vendor
```

The architecture SHOULD use open, portable telemetry conventions where practical.

---

# 133. OpenTelemetry

Baobab SHOULD adopt OpenTelemetry-compatible tracing/metrics/log correlation where practical to reduce polyglot instrumentation divergence.

Language-specific implementation MAY differ across Go, Java and Node services.

---

# 134. Correlation Across Polyglot Repositories

Common propagation conventions SHALL be contractually defined so that:

```text id="cm0j7f"
Go CP
Node Trade
Java ERP
Node CMS
Python/other Pulse components
```

remain traceable within one operation.

---

# 135. Trace Context

Standard distributed trace-context propagation SHOULD be preferred over custom incompatible tracing headers.

---

# 136. Business IDs vs Trace IDs

Business identifiers and trace identifiers SHALL remain distinct.

Example:

```text id="patsjf"
Order ID
≠
Trace ID
≠
Correlation ID
```

They MAY be linked.

---

# 137. Audit Integrity

Future high-assurance deployments MAY add tamper-evidence for critical audit streams.

Possible mechanisms MAY include:

```text id="bnni5f"
append-only object storage
cryptographic chaining
external audit sink
```

This is optional unless required by later policy.

---

# 138. Operational Governance Roles

The platform SHOULD recognise governance roles such as:

```text id="2lmbpr"
Platform Operator
Tenant Administrator
Security Administrator
Provider Operator
Auditor
Support Operator
```

They SHALL have appropriately scoped access.

---

# 139. Auditor Role

Auditors MAY inspect:

```text id="ijdnvj"
audit history
resolution evidence
configuration changes
```

without necessarily having mutation authority.

---

# 140. Support Role

Support operators MAY receive:

```text id="pgrdi3"
tenant-scoped diagnostics
readiness
resolution reason
```

without access to unrelated tenants.

---

# 141. Separation of Duties

Sensitive operations MAY require separation between:

```text id="n55cqz"
person proposing change
person approving change
person auditing change
```

where business/compliance requirements demand it.

---

# 142. Tenant Isolation in Operations

Operational tooling SHALL preserve tenant boundaries.

A tenant administrator SHALL never inspect:

```text id="9s545a"
another tenant's grants
bindings
providers
audit
readiness
```

unless explicitly authorised at platform level.

---

# 143. Platform-Wide Views

Platform administrators MAY need cross-tenant operational views.

Such access SHALL be:

```text id="48bx9o"
privileged
audited
minimal
```

---

# 144. Query Safety

High-volume operational queries SHALL be designed to avoid destabilising the Control Plane database.

Heavy analytics SHOULD eventually use appropriate projections or observability stores.

---

# 145. Reconciliation Data Ownership

The reconciler SHALL use CP authoritative state as desired-state authority.

External provider state SHALL be treated as observed state unless that provider owns the underlying domain truth.

---

# 146. Domain Authority Boundary

Example:

```text id="wr7cxe"
CP expects iDempiere tenant organisation mapping.
```

CP may reconcile the mapping/configuration.

But:

```text id="4vkhr4"
ERP ledger balance
```

is ERP domain truth and SHALL not be “corrected” by CP reconciliation.

---

# 147. Governance Over Provider Changes

Provider registration, deprecation, migration and retirement SHALL be fully auditable.

Retiring a provider with live dependencies SHALL require explicit override if not already migrated.

---

# 148. Provider Retirement Guard

Conceptually:

```text id="mviyf9"
Provider retirement requested
        │
        ▼
Impact analysis
        │
        ▼
Active bindings?
      /    \
    YES     NO
     │       │
     ▼       ▼
   BLOCK    RETIRE
```

Emergency override SHALL be audited.

---

# 149. Capability Retirement Guard

Likewise:

```text id="d0c4m6"
Capability retirement
        │
        ▼
Referenced by product compositions?
        │
        ▼
Active grants?
        │
        ▼
Estate requirements?
```

Unsafe retirement SHALL be blocked or require migration.

---

# 150. Operational Maturity Levels

Baobab MAY assess platform features against maturity stages:

```text id="x8c55g"
DEFINED
OBSERVABLE
ALERTED
RECONCILED
AUTOMATED
RESILIENT
```

This is optional but useful for production-readiness reviews.

---

# 151. Reconciliation Test Matrix

At minimum test:

| Scenario | Expected |
|---|---|
| Desired grant missing | Detect and recreate if safe |
| Unexpected derived grant | Detect and remove/suspend if safe |
| Binding missing | Detect, readiness blocked |
| Provider unhealthy | Readiness degraded/not ready |
| Provider restored | Readiness recovers |
| Subscription suspended | Derived grants ineffective |
| Provider config missing | Drift detected |
| Event lost | Periodic reconciliation catches drift |
| Duplicate event | No duplicate mutation |
| Reconciler retry | Idempotent |
| Manual override | Preserved if legitimate |
| Cross-tenant mismatch | Critical error / no repair across tenants |
| Residency mismatch | Block readiness |
| Isolation mismatch | Block readiness |

---

# 152. Audit Test Matrix

Test at minimum:

```text id="qm1j0q"
grant creation
grant revocation
binding creation
binding activation
provider migration
tenant suspension
subscription activation
market participation change
emergency provider disable
reconciliation auto-repair
```

Each SHALL produce appropriate audit evidence.

---

# 153. Observability Test Matrix

Verify:

```text id="63sjhc"
correlation propagates
trace spans connect
metrics increment correctly
sensitive fields redacted
high-cardinality metrics controlled
alerts trigger under intended conditions
```

---

# 154. Failure Injection

Production-readiness testing SHOULD deliberately inject failures.

Examples:

```text id="iyy2mx"
database unavailable
provider unavailable
cache unavailable
event bus unavailable
stale health
binding ambiguity
outbox backlog
reconciler crash
```

Observe whether:

```text id="ljim4h"
readiness
alerts
audit
reconciliation
```

behave correctly.

---

# 155. Chaos and Resilience Testing

Controlled resilience testing MAY later be introduced.

It SHALL not be performed recklessly against production business workflows.

---

# 156. Operational Documentation

Each control-plane bounded context SHALL document:

```text id="5k9c97"
owner
metrics
alerts
readiness
failure modes
reconciliation behaviour
runbooks
```

---

# 157. Repository Documentation

`baobab-cp` documentation SHALL include:

```text id="j2dvhl"
operations architecture
readiness model
audit model
metrics catalogue
reason-code catalogue
reconciliation model
runbook index
impact-analysis guide
```

---

# 158. Shared Contract Requirements

`baobab-platform/shared` SHOULD define canonical contracts for:

```text id="q8sksz"
AuditEnvelope
ReadinessStatus
ReadinessReason
DriftType
ReconciliationStatus
ImpactReference
OperationalReasonCode
LifecycleEventEnvelope
```

---

# 159. Reason-Code Governance

Operational reason codes SHALL be versioned and documented.

The following is prohibited:

```text id="nhd67k"
reason = "something went wrong"
```

where structured diagnosis is possible.

---

# 160. Reason-Code Families

Recommended families:

```text id="9t5wj5"
CTX_*
CAP_*
GRANT_*
BINDING_*
PROVIDER_*
ENGINE_*
PRODUCT_*
SUBSCRIPTION_*
ESTATE_*
TENANT_*
RECONCILIATION_*
READINESS_*
SECURITY_*
SYSTEM_*
```

Exact format SHALL be standardised in Shared.

---

# 161. Persistence Model

A potential schema organisation MAY include:

```text id="mhtx3q"
audit.audit_record

operations.reconciliation_run
operations.drift
operations.readiness_snapshot
operations.impact_snapshot

messaging.outbox
```

Existing `audit` and `messaging` schemas from the parent architecture SHOULD be retained where appropriate.

---

# 162. Suggested Reconciliation Table

Conceptual:

```sql id="gy08k3"
CREATE TABLE operations.reconciliation_run (
    id                  uuid PRIMARY KEY,
    resource_type       text NOT NULL,
    resource_id         text NOT NULL,
    tenant_id           uuid,

    desired_version     bigint,
    observed_version    bigint,

    status              text NOT NULL,
    reason_code         text,

    drift_detected      boolean NOT NULL DEFAULT false,
    changes_applied     jsonb NOT NULL DEFAULT '[]'::jsonb,

    started_at          timestamptz NOT NULL,
    completed_at        timestamptz,

    correlation_id      text
);
```

---

# 163. Suggested Drift Table

```sql id="bbbt20"
CREATE TABLE operations.drift (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid,

    resource_type       text NOT NULL,
    resource_id         text NOT NULL,

    drift_type          text NOT NULL,
    severity            text NOT NULL,

    desired_state       jsonb,
    observed_state      jsonb,

    status              text NOT NULL,
    detected_at         timestamptz NOT NULL,
    resolved_at         timestamptz,

    reason_code         text
);
```

---

# 164. Suggested Readiness Snapshot

```sql id="aylhpu"
CREATE TABLE operations.readiness_snapshot (
    id                  uuid PRIMARY KEY,

    subject_type        text NOT NULL,
    subject_id          text NOT NULL,
    tenant_id           uuid,

    context_hash        text,

    status              text NOT NULL,
    reasons             jsonb NOT NULL DEFAULT '[]'::jsonb,

    evaluated_at        timestamptz NOT NULL,
    expires_at          timestamptz,

    version             bigint NOT NULL DEFAULT 1
);
```

---

# 165. Audit Table Guidance

Audit persistence SHALL prefer structured columns for primary query dimensions and JSONB only for extensible detail.

Core fields SHOULD NOT disappear entirely into one unindexed JSON document.

---

# 166. Retention and Partitioning

High-volume audit/outbox tables MAY require:

```text id="5qypbs"
partitioning
archival
retention management
```

as operational volume grows.

These optimisations SHALL preserve correctness.

---

# 167. Reconciliation Worker Architecture

A conceptual architecture:

```text id="d27nek"
Lifecycle Events
      │
      ▼
Reconciliation Queue
      │
      ▼
Reconciliation Workers
      │
      ├── Tenant
      ├── Product
      ├── Provider
      ├── Estate
      └── Capability
      │
      ▼
PostgreSQL + Provider Observations
      │
      ▼
Readiness Evaluation
```

---

# 168. Modular Monolith Compatibility

This ADR does NOT require separate reconciliation microservices.

Within the current modular-monolith CP architecture:

```text id="ld2s5j"
reconciliation
audit
readiness
impact analysis
```

MAY exist as explicit modules within the Go service.

They SHALL retain bounded interfaces.

---

# 169. Background Workers

Background workers MAY run:

```text id="3pp8sz"
inside CP deployment
or
as separately scaled CP worker processes
```

provided they share the same domain contracts and authoritative store.

---

# 170. API/Worker Separation

Long-running reconciliation SHALL not block synchronous resolution APIs.

Worker concurrency and API capacity SHOULD be independently controllable.

---

# 171. Backpressure

Reconciliation queues SHALL support backpressure.

A provider outage affecting thousands of tenants SHALL not trigger an uncontrolled thundering herd.

---

# 172. Reconciliation Coalescing

Repeated events affecting the same resource MAY be coalesced.

Example:

```text id="i6v7yr"
provider degraded
provider unhealthy
provider unavailable
```

within a short interval may require one current-state reconciliation rather than three redundant full runs.

---

# 173. Priority Queues

Critical security/tenant-state reconciliation MAY be prioritised ahead of low-risk maintenance reconciliation.

---

# 174. Revocation Priority

Examples that SHOULD receive high priority:

```text id="zzyug8"
tenant suspended
grant revoked
identity disabled
provider security compromise
```

---

# 175. Operational Safety

Automatic reconciliation SHALL never “repair” a deliberate suspension.

Example:

```text id="zbvx0v"
administrator suspends provider
```

reconciler SHALL NOT reactivate it merely because desired topology once said active.

Administrative lifecycle state is part of desired state.

---

# 176. Reconciliation Provenance

Every automatic mutation SHALL record:

```text id="5tbplx"
reconciler identity
trigger
desired-state version
reason
before
after
```

---

# 177. Drift Acknowledgement

Some drift MAY be temporarily accepted.

The model MAY support:

```text id="6w6wjs"
ACKNOWLEDGED
```

with:

```text id="rm2esg"
owner
reason
expiry
```

Permanent unbounded drift acknowledgements SHOULD be avoided.

---

# 178. Maintenance Windows

Provider maintenance MAY temporarily alter readiness without generating unnecessary critical incidents if declared in advance.

Maintenance SHALL remain auditable.

---

# 179. Suppression vs Hiding

Alert suppression during maintenance SHALL not hide actual readiness state.

The system may suppress paging while still reporting:

```text id="lgrwwv"
NOT_READY_MAINTENANCE
```

or equivalent.

---

# 180. Platform Governance Dashboard

A platform dashboard SHOULD eventually present:

```text id="8ls3u5"
Platform
├── Tenant readiness
├── Estate readiness
├── Product readiness
├── Capability readiness
├── Provider health
├── Drift
├── Reconciliation backlog
├── Outbox backlog
├── Security-relevant failures
└── Active migrations
```

---

# 181. No Dashboard as Authority

Dashboard state is a projection.

It SHALL not become authoritative platform state.

---

# 182. GitOps Compatibility

Future desired-state workflows MAY integrate with GitOps-style configuration.

However:

```text id="sp3zf1"
Git repository
```

SHALL not automatically replace CP runtime authority.

The exact authority model must be explicit if GitOps is adopted.

---

# 183. Human vs Machine Changes

Every mutation SHALL indicate whether it originated from:

```text id="wg8l29"
human administrator
API client
automation
migration
reconciler
```

---

# 184. Production Change Traceability

Operators SHOULD be able to trace:

```text id="h6uxwe"
Git commit / deployment
→ configuration change
→ CP state mutation
→ readiness change
→ incident
```

where applicable.

---

# 185. Software Deployment Observability

CP deployments SHALL expose:

```text id="whs7ll"
build version
commit identifier
contract version
migration level
deployment time
```

for operational diagnostics.

---

# 186. Database Migration Observability

Startup/readiness SHALL verify expected migration state.

A binary expecting newer schema SHALL not quietly run against incompatible schema.

---

# 187. Contract Compatibility Observability

Operators SHOULD be able to see mismatches such as:

```text id="7i6xws"
CP expects provider contract 2.0
provider supports 1.0
```

before business traffic fails.

---

# 188. Pre-Deployment Validation

Production deployment SHOULD validate:

```text id="bp70ba"
database migration compatibility
Shared contract compatibility
provider compatibility
mandatory configuration
IAM trust configuration
```

before activation.

---

# 189. Post-Deployment Verification

Each deployment SHALL include automated verification of:

```text id="xwryq8"
liveness
readiness
context resolution
capability resolution
database connectivity
event publishing
critical provider integration
```

where safe.

---

# 190. Rollback Readiness

Deployments SHALL define rollback conditions.

Rollback SHALL consider:

```text id="coazq3"
database migrations
contract compatibility
event schema changes
provider configuration
```

not merely application binaries.

---

# 191. Operational Anti-Patterns

The following SHALL be rejected:

```text id="bg0d4y"
"Check the logs manually" as the only diagnostic model

ready = true because process is running

silent automatic repair with no audit

one giant platform-health boolean

metrics containing sensitive tenant data

reconciler with uncontrolled write authority

provider outage marking all tenants globally failed

readiness calculated without context

event-driven reconciliation with no periodic backstop

manual database edits as normal operations
```

---

# 192. Implementation Gates

## Gate 0 — Operational Baseline Audit

Inspect existing:

```text id="0t2drw"
logging
audit tables
metrics
tracing
health endpoints
readiness
outbox
reconciliation
operational docs
```

Produce gap analysis.

---

## Gate 1 — Shared Operational Contracts

Define canonical:

```text id="mf6kj5"
AuditEnvelope
ReadinessStatus
ReadinessReason
Drift
ReconciliationResult
OperationalReasonCode
ImpactReference
```

---

## Gate 2 — Structured Audit

Implement audit for critical state mutations.

---

## Gate 3 — Correlation and Tracing

Implement common:

```text id="f1cyak"
correlation_id
request_id
trace propagation
resolution_id
```

---

## Gate 4 — Metrics Baseline

Implement CP, resolver, provider, reconciliation and outbox metrics.

---

## Gate 5 — Readiness Engine

Implement:

```text id="0dlv6j"
CapabilityReadiness
ProductReadiness
EstateReadiness
TenantReadiness
```

---

## Gate 6 — Drift Model

Implement drift detection and classification.

---

## Gate 7 — Reconciliation Framework

Implement idempotent reconciliation worker framework.

---

## Gate 8 — Entitlement Reconciliation

Reconcile:

```text id="u811kl"
subscriptions
compositions
grants
```

---

## Gate 9 — Provider Reconciliation

Reconcile provider configuration and bindings.

---

## Gate 10 — Event-Triggered Reconciliation

Integrate canonical lifecycle events.

---

## Gate 11 — Periodic Reconciliation

Add backstop reconciliation schedules.

---

## Gate 12 — Impact Analysis

Implement reverse dependency queries.

---

## Gate 13 — Explainability

Implement privileged diagnostic views.

---

## Gate 14 — Alerting

Define actionable alerts and severity.

---

## Gate 15 — Runbooks

Create runbooks for critical platform failure modes.

---

## Gate 16 — Failure Injection

Test operational behaviour under controlled failures.

---

## Gate 17 — ZuriBeans Operational Validation

Demonstrate:

```text id="h93s6t"
B2B estate readiness
cross-border capability readiness
Trade/ERP/Pulse provider readiness
impact analysis
reconciliation
```

---

## Gate 18 — Thamani Operational Validation

Demonstrate independent readiness and impact even where provider technology is shared.

---

## Gate 19 — External Tenant Validation

Use a logistics or commodity-trade fixture to prove the model is not hard-coded around NABHOLD entities.

---

## Gate 20 — Production Readiness Review

Review:

```text id="93at3v"
SLOs
alerts
dashboards
runbooks
audit
reconciliation
failure recovery
capacity
```

before production approval.

---

# 193. Definition of Done

ADR-BCP-008 SHALL be considered implemented when:

1. Critical mutations are auditable.
2. Audit and logs are distinct.
3. Metrics are structured and useful.
4. Distributed tracing exists.
5. Correlation propagates across services.
6. Sensitive data is excluded from telemetry.
7. Capability readiness is explicit.
8. Product readiness is explicit.
9. Estate readiness is explicit.
10. Tenant readiness is explicit.
11. Readiness includes structured reasons.
12. Health and readiness remain distinct.
13. Desired state is explicit.
14. Observed state is explicit.
15. Drift is first-class.
16. Reconciliation is idempotent.
17. Event-driven reconciliation exists.
18. Periodic reconciliation exists.
19. Automatic repair is bounded by authority.
20. Provider outage impact is computable.
21. Capability retirement impact is computable.
22. Operational explainability exists.
23. Outbox health is observable.
24. Alerts are actionable.
25. Critical alerts have runbooks.
26. CP self-readiness distinguishes liveness from readiness.
27. Cache failure does not break correctness.
28. Event-bus failure does not silently corrupt authority.
29. ZuriBeans and Thamani remain operationally independent.
30. Shared providers do not collapse tenant-specific readiness.
31. External tenant models work without custom observability logic.
32. Post-deployment verification exists.
33. Production incidents can be traced to configuration and topology changes.
34. Operators can answer "why?" without direct database archaeology.

---

# 194. Rejected Alternatives

## Logs-only operations

Rejected.

## Boolean health as complete readiness

Rejected.

## No reconciliation

Rejected.

## Event-only reconciliation

Rejected.

## Periodic-only reconciliation

Rejected for slow convergence.

## Automatically repair everything

Rejected.

## Store all observability in PostgreSQL

Rejected.

## Store no durable audit

Rejected.

## One global readiness flag

Rejected.

## Cross-tenant operational aggregation without strict authorization

Rejected.

## Manual database intervention as normal operating procedure

Rejected.

---

# 195. Final Operational Architecture

```text id="7wf78w"
                       DESIRED STATE
                            │
                            ▼
                    BAOBAB CONTROL PLANE
                            │
          ┌─────────────────┼──────────────────┐
          │                 │                  │
          ▼                 ▼                  ▼
     Subscriptions       Grants            Bindings
          │                 │                  │
          └─────────────────┼──────────────────┘
                            ▼
                       RECONCILER
                            │
             ┌──────────────┼──────────────┐
             ▼              ▼              ▼
         Providers       Engines      Digital Estates
             │              │              │
             └──────────────┼──────────────┘
                            ▼
                       OBSERVED STATE
                            │
                            ▼
                      DRIFT ANALYSIS
                            │
                     ┌──────┴──────┐
                     ▼             ▼
                  IN SYNC        DRIFT
                                   │
                                   ▼
                           SAFE RECONCILIATION
                                   │
                                   ▼
                          READINESS EVALUATION
                                   │
              ┌────────────────────┼───────────────────┐
              ▼                    ▼                   ▼
          Capability            Product              Estate
          Readiness             Readiness            Readiness
              │                    │                   │
              └────────────────────┼───────────────────┘
                                   ▼
                          OPERATIONAL GOVERNANCE
                                   │
        ┌──────────────────────────┼────────────────────────┐
        ▼                          ▼                        ▼
      AUDIT                     METRICS                  TRACES
        │                          │                        │
        └──────────────────────────┼────────────────────────┘
                                   ▼
                            ALERTS / RUNBOOKS
```

---

# 196. Operational Decision

**ACCEPTED TARGET AUDIT, OBSERVABILITY, RECONCILIATION AND READINESS ARCHITECTURE, subject to formal approval.**

Upon approval:

1. audit SHALL become first-class;
2. readiness SHALL become a hierarchical platform model;
3. desired and observed state SHALL be explicitly compared;
4. drift SHALL become detectable and queryable;
5. reconciliation SHALL be idempotent and bounded;
6. lifecycle events SHALL trigger reconciliation;
7. periodic reconciliation SHALL provide eventual correctness;
8. provider and capability impact analysis SHALL be available;
9. correlation, traces and metrics SHALL span polyglot Baobab services;
10. production alerts SHALL be actionable and linked to runbooks;
11. operational tooling SHALL preserve tenant/legal-entity isolation;
12. CP SHALL be able to explain why a tenant, estate, product or capability is or is not ready.

---

# 197. Architectural Maxim

> **Desired state tells Baobab what should be true. Observation tells Baobab what is true. Reconciliation closes the gap. Readiness tells Baobab whether the promise can currently be fulfilled. Audit tells us how we got there.**

And operationally:

> **Nothing consequential in the Control Plane should be invisible, unexplained, unreconciled or unauditable.**