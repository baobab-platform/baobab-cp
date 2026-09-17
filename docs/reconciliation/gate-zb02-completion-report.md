# Gate ZB-02 — Control Plane Completion Report

**Gate status:** PASSED for the scope defined below (PLAN→APPLY→RECONCILE→READY→ACTIVE
orchestration mechanism, proven end-to-end against real PostgreSQL); HTTP wiring and
persisted readiness/drift snapshots remain explicitly out of scope — see §6.
**Date:** 2026-09-17
**Authority:** ZuriBeans Go-Live — ZB-00 to ZB-02 Integration, Completion & Production
Hardening programme spec; `docs/reconciliation/phase-0-architecture-inventory-and-lock.md`
§2 (`MarketParticipation`, `ProvisioningState` rows); ADR-BCP-010 §18.1/§22/§23/§31,
ADR-BCP-011.
**Method:** Direct citation of merged Go source, SQL migrations and tests on `main` as of
this report. No behaviour is described from design intent alone — every row below cites the
file(s) and, where one exists, the test that proves it against a real PostgreSQL instance.

---

## 1. What Gate ZB-02 is

Gate ZB-02 implements the *orchestration mechanism* for the five-stage exit lifecycle
`PLAN → APPLY → RECONCILE → READY → ACTIVE` (plus `FAILED`/`CANCELLED` terminals) over the
`TenantProvisioning` process aggregate (`internal/provisioning/domain/tenant_provisioning.go`,
`provisioning.tenant_provisioning`, migration `000038`). It is deliberately **not** the full
thirteen-state Technical Specification §22 machine, and it does not itself decide business
policy (which grants/bindings/lanes a tenant should have) — that is supplied as an input
manifest. What Gate ZB-02 owns is durable, idempotent, retry-safe, fail-closed *orchestration*
of that manifest into real `MarketAssignment`/`CapabilityGrant`/`CapabilityBinding`/`TradeLane`
resources, with read-after-write reconciliation and evidence-backed readiness before a tenant
is marked `ACTIVE`.

---

## 2. Gate Evidence Matrix

| Area | Status | Evidence |
|---|---|---|
| **Declarative manifest input** | PASS | `internal/provisioning/manifest.go`'s `TenantManifest`/`TenantManifestSpec` (markets, capability grants, capability bindings, trade lanes), validated by `TenantManifest.Validate()` and resolved to canonical CP identifiers by `manifest_loader.go`'s `ResolveManifest` (fails closed on unknown/inactive references). See `docs/architecture/zb02-tenant-manifest-reference.md`. |
| **Orchestration state machine** | PASS | `internal/provisioning/domain/tenant_provisioning.go`'s `provisioningTransitions` table; `internal/provisioning/orchestrator.go`'s `Orchestrator.Run`/`Retry`. `TestOrchestratorActivatesOnlyAfterConvergence`, `TestOrchestratorStopsAtReconcileWhenDriftExists`. |
| **APPLY materialisation** | PASS | `internal/provisioning/composition_root.go`'s `marketParticipationApplyStep`/`capabilityGrantApplyStep`/`capabilityBindingApplyStep`/`tradeLaneApplyStep`, composed by `BuildZB02Pipeline`. Each step is idempotent (re-running APPLY against unchanged desired state is a no-op — see the `sameGrantWindow`/`timeEqualAtDBPrecision` fix in §4). |
| **RECONCILE (read-after-write)** | PASS | `internal/provisioning/reconciliation.go`'s `DesiredObservedReconciler`/`HashResourceReconciler`, comparing independently-read observed state against desired-state hashes for all four resource families. Never declares convergence from APPLY success alone. |
| **READY (evidence-backed)** | PASS | `internal/provisioning/readiness.go`'s `ReadinessEvaluatorImpl` over 7 required probes (`market-participation`, `capability-grants`, `capability-bindings`, `engine-instances`, `context-resolution`, `trade-lanes`, `isolation-and-residency`), wired in `BuildZB02Pipeline`. |
| **Idempotency-conflict detection** | PASS | `internal/service/tenant_provisioning_service.go`'s `Plan()` rejects a reused `idempotency_key` whose `request_hash` differs (`ErrTenantProvisioningIdempotencyConflict`), on both the normal read path and the concurrent-create race path. `TestTenantProvisioningServicePlanRejectsIdempotencyConflict`. |
| **Optimistic-locked concurrent updates** | PASS | `internal/repository/postgres.go`'s `UpdateTenantProvisioning` (`UPDATE ... WHERE version = $N`). Proven under genuine goroutine concurrency (not simulated staleness) by `TestPostgresTenantProvisioningConcurrentUpdatesOnlyOneWins` (`internal/repository/postgres_tenant_provisioning_concurrency_test.go`), stable across `-race -count=20`. |
| **Crash/restart recovery** | PASS | `TestZB02OrchestrationRecoversAfterSimulatedCrash` (`internal/provisioning/zb02_crash_recovery_test.go`): simulates a crash immediately after APPLY commits but before the RECONCILE transition is recorded, then proves a second independent `Orchestrator` resumes to `ACTIVE` with no duplicated resources. |
| **Bidirectional trade lanes** | PASS | `TestZuriBeansUGZAManifestReachesActive` declares both `UG→ZA` and `ZA→UG` `CROSS_MARKET` lanes and asserts both reach `ACTIVE`/usable. |
| **Multi-tenant / legal-entity isolation** | PASS | `TestZB02ResourcesFailClosedAcrossTenants` (`internal/provisioning/zb02_isolation_test.go`): cross-tenant reads/writes against `GetEffectiveMarketAssignment`, `CapabilityGrantProvisioner`, `CapabilityBindingProvisioner`, `GetTradeLane`, `GetCapabilityScope` all fail closed. |
| **Context resolution — negative/failure-closed paths** | PASS | `TestAuthoritativeContextResolverFailsClosed` (`internal/provisioning/zb02_context_negative_test.go`): 11 subtests covering missing identifiers, wrong legal entity, inactive/unparticipated/expired market participation, cross-tenant market and digital-estate references, unknown isolation profile, unknown tenant. |
| **Migration upgrade path (pre-ZB-02 → latest)** | PASS | `TestApplyMigrationsUpgradesExistingMarketAssignmentSafely` (`internal/store/postgres/migrate_upgrade_test.go`): applies migrations only through `000038`, seeds a representative pre-`000039` row, then upgrades to latest and proves the legacy-row backfill hazard (§4) is corrected and idempotent on re-run. |
| **Outbox event publishing** | PASS | `internal/repository/outbox.go` + `internal/repository/postgres.go`: `market-participation-created`/`-updated`, `trade-lane-activated`, `tenant-provisioning-ready`/`-active`/`-failed`, each written in the same transaction as the domain write (`messaging.outbox`, migration `000015`; envelope per ADR-0004/`internal/events.New`). Scoped deliberately to ZB-02's own write paths only — see §6. Counts asserted by `TestZuriBeansUGZAManifestReachesActive`. |
| **Structured observability** | PASS | `internal/provisioning/orchestrator.go`, `workers.go`, `reconciliation.go`: `log/slog`-based structured logs per phase transition, per APPLY step and per RECONCILE drift, per ADR-BCP-010 §31. See `docs/runbooks/zb02-provisioning-operator-guide.md` §4. |
| **HTTP API surface** | NOT IMPLEMENTED | Neither `internal/service.TenantProvisioningService` nor `internal/provisioning.BuildZB02Pipeline`/`Orchestrator` is called from any `api/`or `cmd/` production entry point today — both are exercised only by this package's own tests. This is a pre-existing gap the Phase-0 inventory already flags (`docs/reconciliation/phase-0-architecture-inventory-and-lock.md` §2, `MarketParticipation` row: "Still open: HTTP handlers"), not introduced or closed by this pass. See §6. |
| **Persisted readiness/drift snapshots** | NOT IMPLEMENTED | `ReadinessReport`/`ReconciliationReport` are computed on demand, not persisted as queryable rows. Tracked as Programme Gate P10 scope in the Phase-0 inventory. |
| **Manifest rehydration across process restarts** | NOT IMPLEMENTED | `BuildZB02Pipeline` composes one `Orchestrator` per already-resolved manifest held in closures; a resumed `Orchestrator.Run` after a real process restart needs the manifest re-resolved by its caller, not read back from `TenantProvisioning.Metadata`. Documented as a deliberate simplification in `composition_root.go`'s own doc comment. |

---

## 3. What "done" means here

Every PASS row above is proven against a real PostgreSQL instance (migrations applied via
`ApplyMigrations`, not an in-memory fake), verified in this pass via `go build ./...`,
`go vet ./...`, and `go test ./...`/`go test -race ./internal/provisioning/... ./internal/repository/...`
green against a freshly created database — matching how CI provisions its own database per
job. No PASS row is asserted from source reading alone without a passing test backing it,
except where the row is itself a static verification (e.g. the migration transition table).

---

## 4. Two latent defects found and fixed during this pass

1. **`CapabilityGrant` retry/idempotency precision bug**
   (`internal/provisioning/capability_grant_provisioner.go`). `sameGrantWindow` compared
   `time.Time` values with `.Equal()` at Go's nanosecond precision, but PostgreSQL's
   `timestamptz` truncates to microsecond precision — so a freshly computed
   `time.Now().UTC()` value never exactly equalled its own round-tripped value, silently
   breaking every idempotent re-APPLY of an unchanged grant (every crash-recovery or retry
   path would have re-created a "new" grant window instead of recognising the existing one).
   Fixed with `timeEqualAtDBPrecision`, truncating both sides to microsecond precision before
   comparing. Found by `TestZB02OrchestrationRecoversAfterSimulatedCrash`.
2. **Migration `000039` legacy-row backfill hazard**
   (`internal/store/postgres/migrations/000039_*.sql`). `ADD COLUMN status text NOT NULL
   DEFAULT 'PENDING'` retroactively set every pre-existing `market.market_assignment` row to
   `PENDING`, which `IsOperationalAt()`/`RequireOperational()` treat as non-operational —
   silently deactivating real production market participation on upgrade. Corrected via a new,
   forward-only migration `000041_market_assignment_legacy_status_backfill_fix.sql`
   (migration `000039` itself was already merged and is never edited in place, per the
   Phase-0 §3.5 migration-strategy lock). Found by
   `TestApplyMigrationsUpgradesExistingMarketAssignmentSafely`.

---

## 5. Non-canonical tenant IDs surfaced by outbox validation

Wiring outbox event publication ran `domain.ValidTenantID` (`^tn_[a-z0-9]+$`, ADR-0004)
against ZB-02 test fixtures for the first time and found three (`tn_zuribeans_zb02_e2e`,
`tn_zb02_<suffix>`, `tn_provisioning_concurrency_test`) using an underscore after the `tn_`
prefix — not itself a ZB-02 defect (real, `RegisterTenant`-minted tenant IDs are already
canonical), but a test-fixture gap nothing had previously exercised. Fixed the fixtures
rather than loosening validation.

---

## 6. Explicit non-goals of this pass

- **HTTP handlers for tenant provisioning.** Both entry points (`TenantProvisioningService`'s
  manual `Plan`/`Apply`/`Reconcile`/`MarkReady`/`Activate`/`Retry`/`Cancel`, and
  `BuildZB02Pipeline`/`Orchestrator.Run`'s automatic driver) are Go APIs only. Wiring either
  into `api/router.go` is a separate, later change.
- **Retrofitting the pre-existing `CapabilityGrant`/`CapabilityBinding` write paths
  (`CreateGrant`/`RevokeGrant`/`CreateBinding`) into the outbox.** These predate Gate ZB-02
  and are shared by callers well beyond it; only the write paths ZB-02 itself introduced
  (`MarketAssignment` governance, `TradeLane`, `TenantProvisioning` phase transitions) are
  wired. See `internal/repository/outbox.go`'s own scope comment.
- **Metrics.** No metrics stack exists in this codebase to plug into; inventing a parallel one
  was explicitly out of scope. Structured logs alone satisfy the observability dimensions this
  pass covers (tenant, operation, phase, resource, attempt, duration, outcome).
- **Full Technical Specification §22 thirteen-state machine and per-phase business logic**
  beyond what this pass wires (creating grants, resolving context, binding providers,
  evaluating readiness) is Programme Gate P8 (Provider Reconciliation) onward, per the
  Phase-0 inventory's `ProvisioningState` row.
