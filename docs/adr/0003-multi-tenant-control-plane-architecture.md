# ADR-0003: Multi-Tenant, Production-Ready Control Plane Architecture

**Status:** Proposed
**Date:** 2026-08-31
**Repository:** `baobab-platform/baobab-cp`
**Series:** Local ADR register (follows `0001-go-control-plane-runtime.md`, `0002-adopting-zensical.md`)
**Related:** Platform ADR-020 (Control Plane / Product Plane / Digital Estate architecture), `baobab-platform/shared` (canonical contracts), `baobab-platform/infrastructure` (environment provisioning)

---

## 1. Context

`baobab-cp` is narrowing from its original monolith scaffold to a purpose-built Go control plane. ADR-0001 fixed the runtime (Go). This ADR fixes *what the control plane actually is*: its tenancy model, its API contract, its consistency and failure behaviour, its event model, and its security posture — the decisions a production-ready, multi-tenant control plane cannot defer.

Constraints already established elsewhere in the ecosystem, which this ADR treats as fixed inputs rather than open questions:

- No product engine shares a database with another, or with the control plane.
- `baobab-trade` already implements a control-plane client and **fails closed** if tenant context cannot be resolved — any design here must honour that contract, not redefine it.
- `baobab-platform/infrastructure` provisions PostgreSQL, RabbitMQ (with DLQs), APISIX, and Redis; PostgreSQL is authoritative, Redis holds only rebuildable projections; the control plane reconciles APISIX configuration and owns PostgreSQL desired state.
- Target production region is AWS `af-south-1` (Cape Town).
- Canonical contracts live in `baobab-platform/shared`, referenced, not redefined.

## 2. Decision Summary

`baobab-cp` is a **declarative, reconciling control plane** in the same architectural family as a Kubernetes controller: consumers submit *desired state* for tenants and entitlements; the control plane reconciles that desired state against reality (databases, gateway routes, downstream engine registrations) and exposes *resolved, authoritative context* to product engines on every request that needs it.

It is **not** a request-routing proxy, **not** a business-data store, and **not** a place where product logic lives.

## 3. Responsibilities and Non-Responsibilities

### Owns
- Tenant lifecycle (create, suspend, reinstate, decommission) and lifecycle status.
- The tenancy hierarchy metadata: Tenant Group → Tenant → Business Unit → Function → Team (structure only — no business data).
- Product entitlements: which tenants may use which product engines (`baobab-trade`, `baobab-erp`, `baobab-pulse`), at what tier.
- Desired-state records for provisioned infrastructure surfaces it is responsible for reconciling (APISIX routes, per-tenant PostgreSQL boundaries).
- Auditable provisioning history (append-only).
- Publication of lifecycle and entitlement change events.
- Authenticated tenant-context resolution for product engines.

### Explicitly does not own
- Business data of any kind (orders, invoices, research artefacts — these live in `baobab-trade`, `baobab-erp`, `baobab-pulse` respectively).
- UI or end-user-facing surfaces.
- Infrastructure provisioning mechanics (Terraform, Compose topologies) — that is `baobab-platform/infrastructure`'s job; the control plane declares *desired state*, infrastructure tooling and the reconciler realise it.
- Canonical contract definitions — those are authored in `baobab-platform/shared`; `baobab-cp` implements against them.

## 4. Tenancy Model

### 4.1 Resource model

Desired-state resources, versioned and stored in PostgreSQL as the single source of truth:

```
TenantGroup
  └── Tenant (has LifecycleStatus: Provisioning | Active | Suspended | Decommissioning | Decommissioned)
        ├── BusinessUnit
        │     └── Function
        │           └── Team
        └── Entitlement[] (ProductID, Tier, EffectiveFrom, EffectiveTo, Status)
```

This mirrors the platform tenancy hierarchy (Platform → Products → Tenant Group → Tenant → Business Unit → Function → Team) established elsewhere in the architecture, with the control plane as its single authoritative owner. Corporate/legal entity identity (e.g. "ZuriBeans (Pty) Ltd") is a *reference field* on Tenant, never a foreign key into a legal-entity system — the corporate hierarchy and the tenancy hierarchy remain decoupled, consistent with existing platform principle.

### 4.2 Isolation strategy

The control plane's own data is platform-owned, not tenant-owned, and is **not** multi-tenant in the row-per-tenant-business-data sense. A single schema with `tenant_id` as an indexed foreign key is sufficient and appropriate for tenant *metadata*; this is a deliberate departure from (and should not be confused with) whatever data-isolation strategy individual product engines choose for their own business data (e.g. `baobab-trade`'s own Postgres, `baobab-erp`'s MariaDB).

Canonical tenant and business-entity identifiers minted here are the join key every product engine maps into its own native records — engines never redefine identity, only map to it (this mirrors `baobab-erp`'s existing "canonical identifiers mapped to ERPNext records, not replacing them" pattern, generalised to all engines).

## 5. Reconciliation Loop

A control loop, run on an interval and additionally triggerable on demand:

```
observe (load desired state + current APISIX/DB state)
   → diff
   → plan
   → apply (idempotent)
   → record status + emit events on transition
```

Every reconciliation pass is idempotent and safe to re-run; partial failures leave the resource in a recorded `Degraded` status rather than an ambiguous state. This is intentionally simpler than adopting a full Kubernetes CRD/operator model (rejected for v1 — see §11) while preserving the same declarative/reconciling discipline.

## 6. API Design

**Transport:** REST/JSON over HTTPS for all consumer-facing endpoints (tenant CRUD, context resolution, entitlement queries). Chosen over gRPC for v1 because every current consumer (`baobab-trade` on Node, `baobab-erp` on Python, digital-estate frontends) integrates more cheaply against REST/JSON, and the request volume does not yet justify gRPC's operational overhead. This is revisitable once traffic profiling justifies it.

**Contract ownership:** the OpenAPI specification for `baobab-cp`'s public API is authored in `baobab-platform/shared/contracts/openapi/baobab-cp/`, not in this repository, consistent with the platform's contract-ownership rule.

**Primary endpoints:**
- `POST /v1/context/resolve` — the endpoint `baobab-trade` already depends on. Given an authenticated caller, returns `tenantId`, `entityId`, `lifecycleStatus`, and the caller's product entitlement, or an explicit `403`/`409` if unresolved. **This endpoint must fail closed by contract, not by accident** — any change to it requires an explicit compatibility review under the platform's ADR discipline.
- `GET /v1/tenants/{tenantId}` / desired-state CRUD for tenant administration.
- `GET /v1/entitlements?tenantId=...&productId=...`
- `GET /healthz`, `GET /readyz` — liveness and readiness, the latter checking DB and message-broker connectivity, matching the pattern `baobab-trade` already expects of dependencies.

## 7. Security Model

- **Service-to-service (product engine → control plane):** mutual TLS at the network layer (terminated per `baobab-platform/infrastructure`'s APISIX gateway policy) plus a short-lived workload identity token. Context-resolution responses are signed by the control plane so a product engine can verify authenticity without a second round trip if it chooses to cache briefly (see §8).
- **Human/administrative access:** OIDC-backed authentication for the control plane's own admin API, distinct from the workload-identity path used by engines.
- **Secrets:** sourced exclusively from `baobab-platform/infrastructure`-managed facilities (never committed; `.env` is for local development defaults only, never committed with real values — see the equator-estate finding logged separately in ADR-020 as a cautionary precedent).
- **Audit:** every provisioning and lifecycle-transition action is written to an append-only audit table before being acknowledged to the caller, satisfying the platform's compliance/audit-schema direction.

## 8. Consistency and Failure Behaviour

Context resolution is a hot path — every `baobab-trade` request depends on it, and it fails closed today. A naive fail-closed-with-no-cache design means a control-plane outage becomes a full commerce outage. We accept a bounded trade-off:

- Successful resolutions may be cached by the calling engine for a short, explicitly-bounded TTL (seconds, not minutes) with the cache duration part of the published contract, not an implementation detail engines invent independently.
- On cache expiry with the control plane unreachable, engines **continue to fail closed** — we are not weakening the safety property `baobab-trade` already implements, only bounding how often it's exercised under normal operation.
- The control plane itself runs with standard availability practices (multiple replicas behind the gateway, PostgreSQL with a standby) rather than solving availability by weakening the client contract.

## 9. Event Model

- **Transport:** RabbitMQ, per `baobab-platform/infrastructure`'s provisioned topology, with dead-letter queues for poison messages.
- **Delivery guarantee:** transactional outbox pattern — state changes and their corresponding event records are written in the same PostgreSQL transaction; a separate publisher process delivers from the outbox to RabbitMQ, giving at-least-once delivery without dual-write risk.
- **Schema:** versioned AsyncAPI definitions in `baobab-platform/shared`, not redefined here. Consumers (e.g. `baobab-erp`'s webhook-based integration) version-check envelopes before processing.

## 10. Observability

- OpenTelemetry traces/metrics/logs throughout, exported per the shared observability contract (once established in `baobab-platform/shared`).
- A named SLO for `/v1/context/resolve` latency and availability, given its position as a hot-path dependency for `baobab-trade` — proposed starting point: p99 < 100ms, 99.9% availability, to be reviewed once real traffic exists.
- Structured audit logging distinct from operational logging (§7).

## 11. Alternatives Considered

- **Full Kubernetes CRD/operator model.** Rejected for v1: correct long-term shape, but adds operational machinery (CRDs, a real Kubernetes control loop, RBAC surface) ahead of proven need. `baobab-platform/infrastructure` already defers Kubernetes adoption until operational need justifies it; this ADR follows the same discipline.
- **Redis as source of truth for tenant state.** Rejected: contradicts `baobab-platform/infrastructure`'s own stated principle that PostgreSQL remains authoritative and Redis holds only rebuildable projections.
- **gRPC for all consumer-facing APIs.** Deferred, not rejected outright — revisit once traffic and consumer language mix justify the added complexity.
- **Weakening fail-closed to fail-open on control-plane outage.** Rejected: `baobab-trade` already depends on fail-closed semantics; changing this unilaterally from the control-plane side would be a breaking, safety-reducing change made without the consumer's consent.

## 12. Migration Path

Foundation 2 removed the non-executable legacy scaffold after the Go tenant-registration path and persistence boundary were established. Context resolution remains a **feature-parity bar** for production traffic: the Go control plane must be validated against real `baobab-trade` traffic before cutover.

## 13. Consequences

**Positive:** a single, coherent, testable model for tenancy that every current and future product engine can depend on without re-deriving it; explicit, bounded failure behaviour instead of an implicit one; alignment with infrastructure's own stated architectural discipline (Postgres-authoritative, Kubernetes-deferred).

**Costs:** the reconciliation loop, outbox pattern, and signed-context-resolution all add real implementation surface that a naive CRUD API would not have had. This is accepted as the necessary cost of "production-ready," per the brief.

## 14. Open Questions

1. Should `baobab-erp` adopt the same explicit control-plane client boundary `baobab-trade` already has? (Carried over from ADR-020; this ADR's `/v1/context/resolve` contract is designed to support it once decided.)
2. Exact cache TTL for context resolution (§8) — a specific number needs sign-off, not just the bounding principle.
3. Whether admin-API OIDC integrates with an existing NABHOLD identity provider or requires a new one — not yet established in this review.
