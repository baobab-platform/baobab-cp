# ZB-02 Tenant Provisioning — Operator Guide

Describes the actual merged Gate ZB-02 implementation as of `main`, not planned behaviour.
See `docs/reconciliation/gate-zb02-completion-report.md` for the evidence matrix this guide
is a companion to, and `docs/architecture/zb02-tenant-manifest-reference.md` for the input
manifest format.

**No HTTP endpoint exists for any of this yet.** Everything below is driven through the two
Go entry points described in §2; wiring either into `api/router.go` is future work. This
guide is for engineers operating provisioning through those Go APIs (directly, via a REPL/
script, or via this package's own tests) and for whoever writes the HTTP layer next.

---

## 1. State machine

```
PLAN --------> APPLY --------> RECONCILE -------> READY --------> ACTIVE
  |              |                  |    ^ \          |
  |              |                  |    |  \(retry APPLY on drift)
  v              v                  v    |   v
CANCELLED     CANCELLED          CANCELLED|  RECONCILE
  |              |                  |     |
  v              v                  v     |
FAILED <---------+------------------+-----+
  |
  v (Retry)
APPLY
```

- `PLAN → APPLY → RECONCILE → READY → ACTIVE` is the success path. Every non-terminal state
  may also transition to `CANCELLED` or `FAILED` (`internal/provisioning/domain/tenant_provisioning.go`'s
  `provisioningTransitions`).
- `RECONCILE` may loop back to itself: `Orchestrator.Run` remains in `RECONCILE` (returns
  without advancing) whenever `observed_state_version != desired_state_version` or any
  blocking reason exists. A later `Run` call re-attempts reconciliation — nothing is
  automatically retried on a timer.
- `READY` behaves the same way: if the readiness evaluator reports non-convergence or
  blockers, the operation stays in `READY` (persisted with updated evidence) rather than
  advancing to `ACTIVE`.
- `FAILED` is **not** terminal. It may only be retried back into `APPLY` (never directly into
  `RECONCILE`/`READY`/`ACTIVE` — a failure invalidates any progress claimed past `APPLY`) or
  explicitly cancelled. `ACTIVE` and `CANCELLED` have no outbound edges.
- `AttemptCount` increments only on a transition into `FAILED`; retrying does not reset it, so
  it is an audit trail of total failures, not a live retry counter.

## 2. Two entry points — know which one you're using

| | `internal/service.TenantProvisioningService` | `internal/provisioning.Orchestrator` + `BuildZB02Pipeline` |
|---|---|---|
| Drives | One phase transition per call, caller-initiated | The whole `PLAN→ACTIVE` run per `Orchestrator.Run` call, advancing as far as deterministic results allow |
| Business logic | None — records the transition only | Real APPLY/RECONCILE/READY logic materialising `MarketAssignment`/`CapabilityGrant`/`CapabilityBinding`/`TradeLane` from a `TenantManifest` |
| Idempotency | Enforces `idempotency_key`/`request_hash` on `Plan()` | N/A (works over an already-`PLAN`ned operation) |
| Wired into HTTP today | No | No |
| Use for | Recording/inspecting provisioning state transitions manually | Actually provisioning a tenant's ZB-02 resources end-to-end |

In production use these compose: `TenantProvisioningService.Plan()` creates the row (with
idempotency protection), then `BuildZB02Pipeline(...).Run(ctx, provisioning.ID)` drives it to
`ACTIVE`. Nothing in this codebase currently performs that composition automatically — see
`internal/provisioning/zuribeans_e2e_test.go` for the closest thing to a worked example.

## 3. Running a provisioning operation

```go
// 1. Resolve the manifest: ResolveManifest validates it (TenantManifest.
//    Validate()) and resolves its symbolic references (market codes,
//    capability keys) against authoritative CP registries. Unknown or
//    inactive references fail closed here, not later.
manifest := ... // TenantManifest — see the manifest reference doc
resolved, err := provisioning.ResolveManifest(ctx, repo, manifest)

// 2. Ensure the tenant has a default CapabilityScope.
scopeID, err := provisioning.EnsureDefaultCapabilityScope(ctx, repo, resolved.TenantID)

// 3. Build the pipeline for this resolved manifest.
orch, err := provisioning.BuildZB02Pipeline(provisioning.ZB02Dependencies{
    Tenants: tenantStore, Repo: repo, Provisioning: repo,
}, resolved, scopeID)

// 4. Seed the TenantProvisioning row (PLAN), then run it to completion (or as far as it goes).
err = repo.CreateTenantProvisioning(ctx, provisioningdomain.TenantProvisioning{
    ID: id, TenantID: manifest.Metadata.TenantID,
    IdempotencyKey: key, RequestHash: hash,
    Status: provisioningdomain.ProvisioningStatusPlan,
    DesiredStateVersion: manifest.Metadata.DesiredStateVersion,
    StartedAt: time.Now().UTC(), Version: 1,
})
final, err := orch.Run(ctx, id)
```

`Run` is safe to call repeatedly: it re-reads current state from the store on every
invocation and only ever moves forward along legal transitions (or stays put pending
convergence) — calling it again after a `RECONCILE`/`READY` block is exactly how you retry
after fixing whatever the blocking reason names.

### Retrying a FAILED operation

```go
final, err := orch.Retry(ctx, id) // FAILED -> APPLY, then re-runs Run() to completion
```

`Retry` refuses anything not currently `FAILED`.

## 4. Reading the logs

Every phase transition, APPLY step and RECONCILE drift now emits a structured `log/slog`
line (`internal/provisioning/orchestrator.go`, `workers.go`, `reconciliation.go`), matching
this codebase's existing `slog` convention (`api/router.go`'s per-request logging).

| Message | Level | When | Key fields |
|---|---|---|---|
| `provisioning phase completed` | Info | A phase advances, converges, or blocks | `tenant_id`, `provisioning_id`, `correlation_id` (= `provisioning_id`), `phase`, `desired_state_version`, `observed_state_version`, `attempt`, `blocking_reasons` (count), `duration_ms`, `outcome` (`advanced`/`converged`/`blocked`) |
| `provisioning phase failed` | Error | A phase worker returns an error | as above, plus `error` |
| `provisioning apply step completed` / `... failed` | Info / Error | Each individual APPLY materialiser (`ApplyStep`) runs | `resource_type` (= `ApplyStep.Key()`: `market-participation`, `capability-grant`, `capability-binding-engine-instance`, `trade-lane`), `duration_ms`, `outcome` |
| `provisioning resource drift detected` | Warn | RECONCILE finds a desired/observed mismatch for a resource | `resource_type`, `resource_id`, `drift_kind` (`MISSING`/`MISMATCH`/`UNEXPECTED`), `repairable`, `reason` |

`correlation_id` on every line is the same UUIDv7 `provisioning_id` that
`internal/repository/outbox.go` uses as the `CorrelationID` on this operation's
`tenant-provisioning-ready`/`-active`/`-failed` milestone events — join orchestration logs to
emitted events on that one field.

## 5. Outbox events

`messaging.outbox` receives one row, in the same transaction as the domain write, for:

| Event type | Emitted on |
|---|---|
| `com.nabhold.control-plane.market-participation-created.v1` | `CreateMarketAssignment` |
| `com.nabhold.control-plane.market-participation-updated.v1` | `UpdateMarketAssignmentGovernance` |
| `com.nabhold.control-plane.trade-lane-activated.v1` | `SaveTradeLane` when the resulting status is `ACTIVE` (not `SUSPENDED`/`RETIRED`) |
| `com.nabhold.control-plane.tenant-provisioning-ready.v1` / `-active.v1` / `-failed.v1` | `UpdateTenantProvisioning` transitioning into `READY`/`ACTIVE`/`FAILED` specifically (not `PLAN`/`APPLY`/`RECONCILE`/`CANCELLED` — those advance the state machine but aren't milestones) |

Nothing publishes these to a broker yet — they land in `messaging.outbox` for a relay this
codebase does not yet include, matching every other outbox row already written by
`internal/store/postgres.Store`.

## 6. Troubleshooting

| Symptom | Likely cause | Where to look |
|---|---|---|
| `Plan()` returns `ErrTenantProvisioningIdempotencyConflict` | The same `idempotency_key` was reused with a different `request_hash` — a genuinely different request, not a retry of the same one | Compare the caller's `request_hash` derivation against what was originally planned |
| `UpdateTenantProvisioning` returns `ErrTenantProvisioningVersionConflict` | Another writer (or another `Orchestrator.Run` invocation) already advanced this operation past the version this caller observed | Re-read the operation and retry from current state; this is optimistic-locking working as intended, not a bug — see `TestPostgresTenantProvisioningConcurrentUpdatesOnlyOneWins` |
| Operation stuck in `RECONCILE` | A resource's desired hash doesn't match its observed hash, or a required resource is missing | `provisioning resource drift detected` log lines name the exact `resource_type`/`resource_id`/`reason`; fix the underlying resource and call `Run` again |
| Operation stuck in `READY` | A readiness probe (`market-participation`, `capability-grants`, `capability-bindings`, `engine-instances`, `context-resolution`, `trade-lanes`, `isolation-and-residency`) is failing | `provisioning phase completed` with `phase=READY, outcome=blocked` gives `blocking_reasons` count; the operation's persisted `BlockingReasons` field names which probe(s) failed |
| Operation `FAILED` | An `ApplyStep`, the reconciler, or the readiness evaluator returned an error (not a business-logic block — an actual error) | `provisioning phase failed` log line's `error` field, and the operation's persisted `LastError` |
| Cross-tenant access unexpectedly denied | Working as intended — ZB-02 resources fail closed across tenants and legal entities by design | `TestZB02ResourcesFailClosedAcrossTenants`, `TestAuthoritativeContextResolverFailsClosed` document every enforced boundary |
