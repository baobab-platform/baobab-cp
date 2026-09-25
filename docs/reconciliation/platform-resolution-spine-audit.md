# Platform resolution spine: implementation baseline

**Date:** 2026-09-08  
**Authority:** `baobab-platform/baobab-cp` (runtime), `baobab-platform/shared` (contracts)  
**Repositories inspected:** `baobab-cp`, `shared`, `baobab-erp`, `baobab-trade`,
`baobab-pulse`

## Outcome

The repositories contain useful foundations, but the complete resolution spine is not
production-ready. The Control Plane already has the physical canonical tables, temporal
mapping and primary-binding constraints, resolver packages, RFC 9457 errors, OIDC
verification, contract checks and a transactional outbox. These are foundations to keep.

The current composed resolver is nevertheless unsafe for production: it accepts caller-
assembled candidate lists, hard-codes `baobab_trade`, resolves a mapping using the tenant
ID as the canonical entity ID, silently breaks equal-ranked ties, selects topology
independently of the chosen binding, and does not enforce temporal validity, Digital
Estate scope, instance isolation profile or authorization within the resolution decision.
These are P0/P1 correctness defects, not documentation gaps.

## Repository findings

| Repository | Existing strengths | Material gaps | Priority |
|---|---|---|---|
| `baobab-cp` | Go runtime; migrations 000001–000023; schema-qualified canonical registry, topology, capability and mapping tables; OIDC; outbox; repository and resolver layers | Runtime resolver types duplicate canonical concepts; request supplies candidate records; hard-coded capability; incomplete Context; ties do not fail closed; topology is not constrained to selected binding; no reverse-resolution/quarantine path; incomplete stable error taxonomy | P0/P1 |
| `shared` | Accepted Baobab ADRs; JSON Schema/OpenAPI/AsyncAPI assets; TypeScript context and mapping reference code | Reference resolver contains deferred engine/capability compatibility and risks becoming a second runtime authority; no complete shared schemas for the richer binding/instance/full-resolution response required by this programme | P1 |
| `baobab-erp` | iDempiere-only target, contract lock, conformance file, reconciliation and ERPNext-removal evidence; local validation passes | Must consume resolved Context, instance and mappings as contracts; must not accept tenant/native identity from untrusted headers; end-to-end Control Plane contract scenario is absent | P1 |
| `baobab-trade` | Mature tenant/estate tests, ERP projection and mapping client tests, production-control evidence | One unrelated PR (#45) is open; full trusted resolution-envelope consumption and version compatibility across the new spine remain to be proven | P1 |
| `baobab-pulse` | Haystack/Qdrant architecture, migrations and contract fixtures | Supplier-research routing is not proven through a capability binding without exposing pipeline IDs; Control Plane contract version lock/e2e acceptance is incomplete | P1 |

## Confirmed Control Plane defects

1. `internal/resolver/pipeline.go` hard-codes capability key `baobab_trade`; callers cannot
   request `erp.sales-accounting`, `intelligence.supplier-research`, or future capabilities.
2. The pipeline passes `req.TenantID` as `CanonicalEntityID`. Tenant and canonical entity
   are explicitly distinct identities.
3. Capability and mapping resolvers sort deterministic tie-breaker IDs and choose one.
   Equal effective precedence must instead return an ambiguity error.
4. `CapabilityBinding` has no effective interval in the Go resolver model and its resolver
   does not evaluate `effective_from`/`effective_to` or scope compatibility.
5. The topology resolver may return any active instance in the supplied slice; it does not
   require the instance selected by the binding and has no health, draining, residency or
   isolation-profile eligibility decision.
6. Context omits Principal, Digital Estate, property/channel, deployment region,
   environment, isolation profile, authorization decision and immutable resolution time.
7. The handler accepts mappings, bindings and engine instances in the request payload.
   A consumer can therefore influence routing state that must come from authoritative
   Control Plane repositories.
8. Forward mapping scope is inferred by comparing one `scope_id` string independently
   with tenant/legal-entity/market/country values. It does not load and evaluate the
   multi-dimensional `MappingScope` record.
9. No reverse mapping service quarantines unresolved engine events.
10. Resolver errors are plain strings. They cannot reliably drive RFC 9457 codes,
    retryability, metrics or operator diagnostics.

## Existing invariants to preserve

- Existing canonical UUIDs and temporal history must never be regenerated.
- `Tenant`, `LegalEntity`, `DigitalEstate`, `Market`, jurisdiction and deployment region
  remain distinct.
- PostgreSQL exclusion constraints remain the last line of defence against overlapping
  active authoritative mappings/bindings.
- `baobab-platform/shared` owns wire contracts; `baobab-cp` owns resolution behaviour and state.
- Peer engines use authenticated APIs/events and never query Control Plane tables.
- ERPNext references may remain only in historical migration evidence and prohibition
  tests; ERPNext must never be a live engine, binding or fallback.

## Remediation sequence and merge gates

| Gate | Change set | Exit evidence |
|---|---|---|
| 1 | Consolidate canonical Go model and shared wire schemas | No competing runtime model; schema/Go compatibility tests |
| 2 | Complete PostgreSQL status, scope, FK and temporal invariants | PostgreSQL 17 migration and concurrency tests |
| 3 | Trusted immutable Context and authorization linkage | spoofing, precedence and worker-propagation tests |
| 4 | Scoped temporal `CapabilityBinding` resolver | missing/ambiguous/inactive tests fail closed |
| 5 | Binding-constrained eligible `EngineInstance` selection | region, isolation, health, draining and retired tests |
| 6 | Forward and reverse Mapping resolution | temporal, ambiguity, cross-context and quarantine tests |
| 7 | Tenant/legal-entity/estate isolation | Zuribeans, Thamani and Nabhold negative tests |
| 8–9 | Instance relocation and capability extraction | stable canonical IDs and unchanged consumer contracts |
| 10 | Cross-repository contract compatibility | all lock files agree and compatibility CI passes |
| 11–14 | Performance, resilience, observability and security | measured report plus automated fitness tests |
| 15 | Production-readiness evidence | every control is PASS or explicitly BLOCKED |

## Validation baseline

- `baobab-erp/scripts/validate.sh`: **PASS** (51 tests plus JSON validation).
- Local `baobab-cp` validation: **environment blocked** because the execution image does
  not contain Go or Docker. GitHub Actions remains authoritative for this audit-only PR.
- Local `baobab-trade` validation: **dependency blocked** because node modules are not
  installed; existing open PR #45 is left untouched.
- Local `baobab-pulse` validation: **dependency blocked** because pytest is not installed.
- No open PR existed in `baobab-cp`, `shared`, `baobab-erp` or `baobab-pulse` at baseline.

## Decision

Proceed gate-by-gate. Do not expose or integrate the current composed `/v1/resolve`
behaviour as a production contract until Gates 1–7 pass. Each gate must include code,
negative tests, contract/migration evidence where applicable, and an updated conformance
record. A green unit-test suite alone is insufficient evidence of readiness.

---

## Remediation record (2026-09-16)

**Status update:** all 10 "Confirmed Control Plane defects" listed above are resolved on
`main` as of this date, verified by direct inspection of `internal/resolver/pipeline.go`,
`capability.go`, `topology.go`, `mapping.go`, `trace.go`, `internal/domain/resolution.go`
and `api/capability_resolve_handler.go` — not asserted from a changelog. Left as originally
written above as the record of what was found; this section is the record of what has
since changed. Most of this landed in a single commit, `2e4b706 fix(resolution): resolve
requested capability keys (#105)`, between this audit's baseline and the check performed
here (confirmed via `git log 597cac2..main -- internal/resolver/ internal/repository/`).

| # | Original defect | Current state |
|---|---|---|
| 1 | `internal/resolver/pipeline.go` hard-codes capability key `baobab_trade` | Fixed — `ResolutionPipeline.Resolve` uses `req.CapabilityKey` throughout; only remaining `"baobab_trade"` occurrences repo-wide are test fixture values. |
| 2 | Pipeline passes `req.TenantID` as `CanonicalEntityID` | Fixed — the two remain distinct fields end-to-end; `pipeline.go`'s own header comment now documents this as a fixed historical defect. |
| 3 | Equal-precedence bindings silently pick one instead of failing closed | Fixed — `CapabilityResolverImpl.Resolve` explicitly detects a tie (same specificity, same binding-mode rank, same priority) after sorting and returns `"capability binding is ambiguous"` rather than returning `active[0]`. |
| 4 | `CapabilityBinding` has no effective interval evaluated | Fixed — `EffectiveFrom`/`EffectiveTo` are checked against `at` before a binding is considered a candidate. |
| 5 | Topology resolver returns any active instance, not the one the binding selected; no health/residency/isolation eligibility | Fixed — `TopologyResolverImpl.Resolve` requires an exact match on `SelectedEngineInstanceID` and additionally checks `Status`, `HealthStatus`, effective interval, `Environment`, `DeploymentRegion`/`Region`, `IsolationProfileID` and `ResidencyRegion`. |
| 6 | Context omits Principal, Digital Estate, property/channel, deployment region, environment, isolation profile, resolution time | Fixed — `domain.Context` (`internal/domain/resolution.go`) carries all of these (`PrincipalID`, `DigitalEstateID`, `DigitalPropertyID`, `ChannelID`, `DeploymentRegion`, `Environment`, `IsolationProfileID`, `ResolvedAt`, plus `ExpiresAt`/`Provenance`). Authorization decision remains separate (`ResolutionResult.Policy`), which is a reasonable separation of concerns rather than an omission. |
| 7 | Handler accepts mappings/bindings/instances in the request payload | Fixed — `CapabilityResolveHandler`'s request shape is `{context_id, canonical_entity_id, capability_key}` only; it redeems a server-resolved `Context` by `context_id` (with a tenant-mismatch check) rather than accepting any inline routing state. |
| 8 | Forward mapping scope inferred from one `scope_id` string, not the multi-dimensional `MappingScope` record | Fixed (opt-in) — `resolveMapping` calls `DefaultScopeMatcher{}.Match` against real `domain.MappingScope` records when a caller supplies `Scopes`; `legacyScopeSpecificity`'s string comparison remains only as the fallback when no scope map is supplied, mirroring the same nil-means-skip rollout pattern already used for `Grants`/`Capability`. |
| 9 | No reverse-mapping/quarantine path | Fixed — `MappingResolverImpl.ResolveReverse` exists, returns a `QuarantineDecision` on failure, backed by `ErrReverseMappingUnresolved`. |
| 10 | Resolver errors are plain strings | Fixed — `resolutionFailure` returns a structured `*ResolutionError` carrying the full `ResolutionTrace` (correlation/tenant/capability/mapping/grant/binding/instance IDs, outcome, reason) and supports `errors.Unwrap()`. |

Verified together: `go build ./...`, `go vet ./...`, `gofmt -l cmd internal api` (pre-existing
formatting drift in `api/capability_explain_handler.go` noted, unrelated to this remediation
and not fixed here), and `go test ./... -count=1` all pass against a real local PostgreSQL 16
and a real `baobab-platform/shared` checkout (`SHARED_CONTRACTS_DIR`).

**Update**: both items Gate P0's own classification
(`docs/reconciliation/phase-0-architecture-inventory-and-lock.md`) tracked as open here are
now resolved: `CapabilityBinding.scope_id` is repointed at `capability.capability_scope`
(`000034`, `#74`), and the legacy `capability.tenant_capability` table is dropped
(`000035`, `#72`). Neither was in this document's original 10-item list; both are recorded
here for continuity since this document previously cited them as open.
