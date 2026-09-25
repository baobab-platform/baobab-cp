# Audit: `baobab-platform/shared` ↔ `baobab-platform/baobab-cp` Reconciliation

**Scope:** Verification that `baobab-cp`'s implementation, persistence, API and documentation
derive from and remain coherent with the canonical contracts and accepted ADRs owned by
`baobab-platform/shared`.
**Method:** Direct reading of `baobab-platform/shared` ADRs (0001–0005), governance contracts
(`tenancy.yaml`, `legal-entity/registry.yaml`), the `contracts/control-plane/v1/*` and
`contracts/events|errors|idempotency/v1/*` schemas, cross-referenced against `baobab-cp`'s
actual Go source, SQL migrations, tests and documentation as committed on
`origin/main` at the time of this audit. Every finding below cites the file(s) it is drawn
from; nothing here is inferred from the aspirational `Baobab Canonical Mapping Model.md`
document alone, since that document is explicitly `Status: Proposed`, not an accepted ADR.
**Not in scope:** `baobab-trade`, `baobab-erp`, `baobab-cms`, `baobab-pulse`, `infrastructure`.

**Status update:** the P0 finding in §2.1 and P1 backlog items 3–5 (§10) have since been
remediated in follow-up commits on this branch, each verified against a real PostgreSQL
instance and/or a real `baobab-platform/shared` checkout rather than asserted. §12 records what
changed, what is intentionally still open, and one additional defect (a column-type
mismatch in `messaging.outbox`) found while scoping item 6. The findings below are left as
originally written, as the record of what was found and why; §12 is the record of what was
then done about it.

---

## 1. Executive summary

`baobab-cp` correctly adopted the *shape* of the canonical mapping model described in
`baobab-platform/shared` — schema-qualified tables for canonical entities, mappings, markets,
capabilities and topology exist, and the repository has its own detailed (if only
"Proposed") internal elaboration of that model in `docs/adr/ADR-BCP-001-...md`. However,
the repository is currently **not internally coherent**: it contains two unreconciled
generations of its own persistence layer, and the generation that is actually wired into
the migration runner does not support the tenant-registration, context-resolution and
audit features that are simultaneously live in the HTTP API and covered by passing unit
tests (which exercise an in-memory/mocked store, not the real schema). Concretely:

- **P0 — the core product surface is non-functional against its own migrations.**
  `POST /v1/tenants`, `GET /v1/tenants/{id}`, the tenant lifecycle actions, `GET
  /v1/entitlements` and `POST /v1/context/resolve` are implemented in
  `internal/store/postgres/store.go` against tables (`tenants`, `legal_entities`,
  `product_subscriptions`, `provisioning_operations`, `outbox_events`, `audit_events`)
  that are defined **only** in migration files that the migration runner never applies.
  See §2.1.
- **P0 — a second family of `registry`/`mapping`/`capability`/`topology` tables (the ones
  actually created by `cmd/migrate`) is queried by `internal/repository/postgres.go` using
  column and table names, and an ID strategy, that diverge from `baobab-platform/shared`'s
  canonical schemas in ways that are individually correctable but currently untested
  against a real PostgreSQL instance (`postgres_integration_test.go` exists but does not
  cover these code paths — see §2.2/§4).
- **P1 — identifier grammar is not enforced anywhere in Go.** `baobab-platform/shared` fixes exact
  formats for `tenant_id` (`tn_[a-z0-9]+`), `mapping_id` (`map_[a-z0-9]+`),
  `external_reference_id` (`ref_[a-z0-9]+`) and `mapping_scope_id` (`scope_[a-z0-9]+`) in
  `contracts/control-plane/v1/domain.schema.json`. `baobab-cp` mints raw `uuid` values for
  all of these and validates only a generic `resourceID`/UUIDv4-shaped pattern. See §3.
- **P1 — no event publication exists.** `messaging.outbox`/`messaging.inbox` tables are
  created by migration `000015`, and ADR-0001 (local) and the shared
  `control-plane-foundation.md` contract both commit to RabbitMQ + transactional outbox,
  but no Go code writes an outbox row, no Go code publishes to RabbitMQ, and no code
  constructs the CloudEvents-shaped envelope required by `contracts/events/v1/envelope.
  schema.json` (ADR-0004). The only outbox-adjacent write that exists
  (`store.go`'s `INSERT INTO outbox_events`) targets the dead legacy table from §2.1, in
  the legacy snake_case shape ADR-0004 explicitly retired. See §5.
- **P1 — `contracts.lock.yaml` under-declares what is actually implemented.** It pins 4 of
  the ~14 relevant `contracts/control-plane/v1` files and none of the organisation-wide
  `events`/`errors`/`idempotency` contracts, yet the codebase implements canonical
  entities, mappings, markets, capabilities and topology that come from schemas it does
  not declare a dependency on. See §6.
- **P2 — documentation contains extensive, actively misleading legacy content.**
  `docs/governance/readme.md` (987 lines) is a near-complete copy of the repository's
  pre-rewrite Django/Python/Celery/Wagtail README, contradicting the Go-only architecture
  ADR-0001 already accepted in this same repository. `docs/Coding-Standards.md` still
  lists Django/Wagtail as the current web framework/CMS. See §7.
- **P2 — a referenced ADR does not exist.** `docs/adr/index.md` and a code comment in
  `internal/repository/postgres.go` both cite
  `docs/adr/ADR-0005-bcp-db-001-conformance-gap.md` as the record of a known BCP-DB-001
  conformance gap; the file is not in the repository. The gap it was meant to document is
  real (see §2.2) but currently has no accepted remediation record. See §8.
- **P3 — root `README.md`'s "Repository structure" section does not match the actual tree**
  (describes `internal/api`, `internal/domain/tenant/`, `internal/gateway`,
  `internal/events`, `pkg/contracts`; none of these exist). See §9.

None of the above is a case of `baobab-cp` inventing a *competing* canonical definition —
the drift is internal (two generations of the same repository's own persistence layer left
unreconciled), which is actually the easier class of problem to fix. Sections 2–9 give the
evidence; §10 gives a prioritised remediation backlog; §11 records what this audit
deliberately did **not** attempt to fix in the same pass, and why.

---

## 2. Persistence layer: two unreconciled generations

### 2.1 Generation A ("tenant lifecycle") — referenced by live code, never migrated

`internal/store/postgres/migrations/` contains, alongside the 18-file canonical sequence
described in `docs/adr/ADR-BCP-001-...md` §5–24, three additional files that use the same
leading version numbers as the canonical sequence:

```
000001_control_plane.up.sql / .down.sql
000002_audit_identity.up.sql / .down.sql
000003_context_resolution_audit.up.sql / .down.sql
```

These three files, taken together, are the **only** place in the repository that defines
`tenants`, `legal_entities`, `product_subscriptions`, `provisioning_operations`,
`outbox_events` and `audit_events` (with the exact `actor_id`, `actor_type`, `client_id`,
`token_id`, `correlation_id`, `idempotency_key`, `target`, `result`, `policy_decision`
columns that later queries depend on).

`internal/store/postgres/store.go` — the store injected as `api.Dependencies.Store` and
wired to the router in `cmd/controlplane/main.go` — queries exactly these tables:
`RegisterTenant`, `GetTenant`, `GetEntitlement`, `UpdateTenantLifecycle` and
`ResolveContext` all issue raw SQL against `tenants`, `legal_entities`,
`product_subscriptions`, `provisioning_operations`, `outbox_events` and `audit_events`.

`internal/store/postgres/migrate.go`'s `canonicalMigrationNames` — the only list of files
`ApplyMigrations` (and therefore `cmd/migrate`) will ever execute — is:

```go
"000001_extensions_and_schemas.sql", "000002_canonical_registry.sql", ...,
"000018_canonical_entity_versions.sql"
```

It does not include `000001_control_plane.up.sql`, `000002_audit_identity.up.sql` or
`000003_context_resolution_audit.up.sql`. `LoadMigrations` reads only the names in that
slice; the three files are picked up by `//go:embed migrations/*.sql` (so they inflate the
binary) but are never executed.

**Consequence, stated plainly:** running `cmd/migrate` against an empty PostgreSQL 17
database and then starting `cmd/controlplane` produces a server whose `POST /v1/tenants`,
`GET /v1/tenants/{id}`, all three lifecycle-action endpoints, `GET /v1/entitlements` and
`POST /v1/context/resolve` fail on first use with `pgx` errors such as `relation "tenants"
does not exist`. This is the entire tenant-lifecycle and context-resolution surface the
root `README.md` describes as the repository's current milestone ("Status: A4 —
executable, fail-closed tenant context resolution"). The unit tests that exercise this
logic (`internal/store/postgres/context_policy_test.go`, `internal/domain/tenant_test.go`)
pass because they test pure functions and an in-process fixture, not the real schema; there
is no integration test that runs `ApplyMigrations` and then exercises
`store.RegisterTenant`/`store.ResolveContext` end-to-end (`postgres_integration_test.go`
covers the resolver/repository package instead — see §4).

This is not a naming or style issue. It is a data-integrity/architectural defect: the
system's advertised primary function does not work against its own tooling.

### 2.2 Generation B ("canonical mapping model") — migrated, but not what `internal/repository` assumes

The 18 canonical migrations create a second, schema-qualified persistence model
(`registry.canonical_entity`, `mapping.canonical_mapping`, `mapping.mapping_scope`,
`registry.external_reference`, `capability.capability`, `capability.capability_binding`,
`topology.engine`, `topology.engine_instance`, plus `policy.isolation_profile`,
`market.market`, `estate.digital_estate`, `audit.audit_record`, `audit.context_snapshot`,
`system.idempotency_record`, `system.registry_revision`). This is the model
`internal/repository/postgres.go` (`PostgresRepository`) actually queries, and it is
schema-consistent with itself and with what the migrations create — good.

However, comparing the **actual migration column names** (not the aspirational prose in
`ADR-BCP-001`, which describes a richer future version of the same tables — e.g. its
`registry.canonical_entity` has `id`, `owner_tenant_id`, `canonical_key`,
`effective_from`/`effective_to`, `authority`, `classification`, while the migration that is
actually committed, `000002_canonical_registry.sql`, has `canonical_entity_id`,
`tenant_id`, `legal_entity_id`, `external_key`, and no temporal columns at all) against
`baobab-platform/shared`'s `contracts/control-plane/v1/canonical-mapping.schema.json` and
`domain.schema.json` surfaces real, shared-contract-level gaps that are independent of the
ADR-BCP-001 aspiration:

| Table (as migrated) | Gap vs. `baobab-platform/shared` v1 contracts |
|---|---|
| `registry.canonical_entity` | No `effective_from`/`effective_to` (temporal validity is mandatory per the Mapping Model §25 and implied by `mapping.canonical_key` uniqueness semantics); `tenant_id`/`legal_entity_id` are unconstrained `text`, not validated against `tenant_id` (`^tn_[a-z0-9]+$`) or `legal_entity_id` (`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`) grammars from `domain.schema.json`. |
| `mapping.canonical_mapping` | Column/table name diverges from the shared schema's `Mapping` (fields `mapping_id`, `canonical_entity_id`, `external_reference_id`/`target_canonical_entity_id`, `scope_id`, `direction`, `cardinality`, `confidence`, `effective_from`/`effective_to`, `revision`) — the migrated table only has `source_entity_id`/`target_entity_id` and no scope, direction, cardinality, confidence or temporal columns at all, so the exclusion-constraint-based non-overlap guarantee `canonical-mapping.schema.json` implies (and `ADR-BCP-001` §16.3 specifies as "a core architectural invariant") is not present on this table. The temporal exclusion constraint that *does* exist is on `capability.capability_binding` only (`000012`). |
| `capability.capability` / `topology.engine` | Use `code` where the shared schema's equivalent concept (`Capability.capability_key`, `Engine.engine_key`) uses `*_key`; harmless in isolation, but exactly the kind of unreconciled local synonym the canonical-mapping model instructs against (§9.4 "no repository should invent a separate spelling"). |
| all of the above | Primary keys and every mapping/reference ID are raw `uuid`, not the prefixed opaque identifiers (`map_…`, `ref_…`, `scope_…`) `domain.schema.json` defines for exactly these concepts. Only `CanonicalEntity` IDs get the UUIDv7 treatment (`domain.NewUUIDv7()`, used in `api/canonical_handler.go`); `capability_binding`, `mapping_scope`, `external_reference` rows are created with `gen_random_uuid()` (UUIDv4) at the database default. |

None of this is unfixable — it is a normal "MVP schema was cut down from the full ADR-BCP-001
design and the cut-down version was never checked against the *shared* contracts it's
implementing" situation. It is listed as P1, not P0, because — unlike §2.1 — the code paths
that use it are internally consistent and do work end-to-end; they just don't yet match the
external contract they claim to implement.

---

## 3. Identifier and naming audit

| Concept | `baobab-platform/shared` canonical form | `baobab-cp` as committed | Status |
|---|---|---|---|
| `tenant_id` | `^tn_[a-z0-9]+$` (`domain.schema.json`) | `domain.ValidResource` accepts any `^[a-z][a-z0-9]*(_[a-z0-9]+)*$` 3–63 char string; no `tn_` prefix required or minted anywhere | **Non-conformant.** A caller can register a tenant with `tenant_id: "acme"` and it will be accepted. |
| `legal_entity_id` | `^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`, must exist in `contracts/legal-entity/registry.yaml` | Same generic lowercase `resourceID` regex is applied to `legal_entity_id` as to `tenant_id` in `domain.RegisterTenant.Validate()` — **the uppercase-kebab canonical form would be rejected**, and there is no check against the legal-entity registry at all | **Non-conformant**, and inverted: it accepts the *wrong* case and rejects the *canonical* one. |
| `mapping_id` | `^map_[a-z0-9]+$` | raw `uuid` (`canonical_mapping_id`) | Non-conformant |
| `external_reference_id` | `^ref_[a-z0-9]+$` | raw `uuid` (`external_reference_id` column, but UUID-typed) | Non-conformant |
| `mapping_scope_id` | `^scope_[a-z0-9]+$` | raw `uuid` (`mapping_scope_id`) | Non-conformant |
| Canonical entity ID | UUIDv7 recommended (Mapping Model §6); `ADR-BCP-001` §30 makes this a SHALL for "all first-class Control Plane resources" | `domain.NewUUIDv7()` implemented and used **only** for `CanonicalEntity` (`api/canonical_handler.go`); every other resource (`capability_binding`, `mapping_scope`, `external_reference`) gets `gen_random_uuid()` = UUIDv4 at the DB layer | Partially conformant — one correct implementation exists but is not applied consistently. |
| Event envelope field names | `baobabscope`, `correlationid`, `causationid`, `tenantid`, `idempotencykey` (CloudEvents-compatible, no underscores) — ADR-0004 | Not applicable yet: no event envelope is constructed anywhere in `baobab-cp` (see §5) | N/A — nothing to be non-conformant *with* yet, but also nothing conformant. |
| HTTP problem responses | RFC 9457 fields `type`, `title`, `status`, `code`, `correlation_id`, `retryable` (`errors/v1/problem-details.schema.json`) | `api/router.go`'s `problem()` helper emits exactly `type`, `title`, `status`, `detail`, `code`, `correlation_id`, `retryable` with `Content-Type: application/problem+json` | **Conformant.** This is the one place the repository's own ad-hoc implementation already matches the shared contract field-for-field. |
| Idempotency key handling | `Idempotency-Key` header, 16–128 chars, pattern `^[A-Za-z0-9][A-Za-z0-9._:-]*$`, scoped by `(service, operation, tenant_id, key)` (`idempotency/v1/policy.yaml`) | `api/router.go`'s `register()` checks length (16–128) but not the character pattern; `store.go`'s `RegisterTenant` scopes replay-detection by `idempotency_key` alone (global, not tenant-scoped) via a `UNIQUE` constraint on `provisioning_operations.idempotency_key` | Partially conformant — length check present, pattern check and per-tenant scoping missing. |

**Root cause common to the identifier rows above:** there is no shared `internal/idtype` (or
similar) package that owns the `tn_`/`map_`/`ref_`/`scope_` grammars as typed constructors +
validators once, the way `domain.NewUUIDv7()` already does for UUIDv7 generation. Each
handler and validator currently re-implements ID shape checking ad hoc
(`domain.ValidResource`, `productID` regexp in `internal/domain/context.go`, etc.), which is
exactly the "identifiers MUST NOT be checked five different ways in five different layers"
failure mode `baobab-platform/shared`'s ADRs are trying to prevent.

---

## 4. Testing gaps

- `internal/repository/postgres_integration_test.go` exists and (per its name) is intended
  to run against real PostgreSQL, but nothing in the repository exercises
  `internal/store/postgres/store.go` end-to-end against a migrated database — which is
  precisely the code path with the P0 defect in §2.1. A migration-to-integration-test gap
  of exactly this shape is how §2.1 went undetected.
- No test asserts that `ApplyMigrations` run against a clean PostgreSQL 17 produces a
  schema containing every table any repository/store package queries. This is a cheap,
  high-value test to add (a single query against `information_schema.tables` compared
  against a static allow-list derived from the SQL the Go code issues) and would have
  caught §2.1 immediately.
- `internal/store/postgres/migrations_test.go` asserts file *contents* for the 18 canonical
  migrations (good — this is real drift protection) but has no equivalent coverage for the
  three orphaned files, because they are, correctly, not supposed to be part of the
  canonical set at all (see remediation in §10).
- No contract test in this repository loads and validates against the actual JSON Schemas
  in `baobab-platform/shared/contracts/control-plane/v1/*.json` (e.g. via a JSON Schema validator
  library) — `contracts.lock.yaml` records a commit SHA to pin against but nothing in CI or
  `go test ./...` appears to fetch or validate against it.
- No test asserts the `tn_`/`map_`/`ref_`/`scope_` ID-prefix grammars from §3.

---

## 5. Event architecture

`docs/architecture/foundation-0.md`, the local ADR-0001, and `baobab-platform/shared`'s
`control-plane-foundation.md` all commit to: RabbitMQ with publisher confirms and DLQs, a
transactional outbox, and (per ADR-0004) a CloudEvents-shaped envelope with
`baobabscope`/`correlationid`/`causationid`/`tenantid`/`idempotencykey` extension
attributes. As committed:

- There is no RabbitMQ client dependency in `go.mod`.
- There is no code that writes a row into `messaging.outbox` (the schema-qualified,
  canonical outbox table created by migration `000015`).
- The only outbox-shaped write in the repository (`store.go`'s
  `INSERT INTO outbox_events(aggregate_id,event_type,payload)`) targets the legacy,
  non-applied `outbox_events` table from §2.1, and even if that table existed, the payload
  it constructs (`{"operation_id", "tenant_id", "revision", "correlation_id"}`, event type
  `tenant.provisioning.started`) is the exact snake_case, non-CloudEvents shape ADR-0004
  explicitly retired as "migration evidence, not another supported envelope"
  (`contracts/events/v1/compatibility/legacy-control-plane-event.json` is the canonical
  fixture proving this shape must be *rejected*).
- No publisher, consumer, or dead-letter handling exists anywhere in the module.

This is a scope gap, not a drift/inconsistency — the repository has not yet built this
part — but it is worth recording precisely because the *one* piece of event-shaped code
that does exist reproduces the exact legacy shape the shared contracts were written to
retire, which risks becoming a template a future contributor copies.

---

## 6. Shared contract consumption

`contracts.lock.yaml`:

```yaml
source:
  repository: baobab-platform/shared
  commit: 0bc19d2a74459f98e97ecd852d6dac94f0844483
contracts:
  - contracts/control-plane/v1/access-token-claims.schema.json
  - contracts/control-plane/v1/context-resolution.schema.json
  - contracts/control-plane/v1/openapi.yaml
  - contracts/control-plane/v1/security-policy.yaml
```

Missing from this list, despite being implemented against (per §2.2): `domain.schema.json`,
`canonical-mapping.schema.json`, `market.schema.json`, `audit-trail.schema.json`,
`tenant-registration.schema.json`, `provisioning-started.schema.json`,
`provisioning-state-changed.schema.json`, `provisioning-state-machine.yaml`,
`event-envelope.schema.json`, `asyncapi.yaml`, and the organisation-wide
`events/v1/envelope.schema.json`, `errors/v1/problem-details.schema.json`,
`idempotency/v1/policy.yaml`. There is also no OpenAPI or AsyncAPI document anywhere in
`baobab-cp` describing its own actual REST surface (`find . -iname 'openapi*' -o -iname
'asyncapi*'` returns nothing outside `.git`) — the pinned `openapi.yaml` is `shared`'s own
control-plane contract, not a description of what `baobab-cp` currently exposes, and the
two have already diverged (`shared`'s `tenant-registration.schema.json` requires
`isolation_strategy`/`residency_region`/`display_name`/`legal_entity_id`, which
`domain.RegisterTenant` does implement, but `shared`'s OpenAPI is not checked in CI against
the live router in `api/router.go`).

There is no CI job in `.github/workflows/` (not inspected file-by-file in this pass, see
§11) confirmed to validate `baobab-cp` against updates to the pinned `shared` commit, so a
breaking change in `baobab-platform/shared` would not be caught automatically.

---

## 7. Legacy Python/Django documentation

| File | Finding |
|---|---|
| `docs/governance/readme.md` (987 lines) | Near-complete duplicate of the pre-rewrite root README: badges Django 6.0, a "Technology Stack" table listing `Backend Services: Python 3.14 · Django 6.0`, `Background Workers: Python · Celery`, a "Backend Service" section prescribing Django + Wagtail CMS, and literal setup instructions (`cd services/backend`, `python manage.py migrate`, `python manage.py createsuperuser`, `python manage.py runserver`) for a `services/backend` directory that does not exist in this repository. This directly contradicts the accepted, same-repository ADR-0001 ("New control-plane runtime code will be written in Go"). |
| `docs/Coding-Standards.md` | Lines 174–175 list `Web Framework: Django 6` / `CMS: Wagtail` in a stack table, and line 199 lists "Django coding style and best practices" as an applicable standard. No Go-specific coding standard is listed at all in the same table. |
| `docs/architecture/foundation-0.md` | Line 77 refers to "the former Django, AI, ..." scaffold — this is *correctly* framed as history (past tense, "former") and is not a defect. |
| `docs/adr/0001-go-control-plane-runtime.md`, `docs/adr/0003-...md` | Correctly reference the historical Django monorepo and ERPNext-generalisation only as decision context/history. Not defects. |

The distinction matters: ADR-0001 and ADR-0003 (local) reference Python/Django/ERPNext
*as history*, which the task's own instructions correctly say to preserve. `governance/
readme.md` and `Coding-Standards.md` present it as **current, prescriptive** guidance,
which is the actual defect.

---

## 8. ADR governance gap

`docs/adr/index.md` lists five ADRs, including:

```
- [ADR-0005: BCP-DB-001/BCP-GO-001 conformance gap and remediation](0005-bcp-db-001-conformance-gap.md)
```

`docs/adr/0005-bcp-db-001-conformance-gap.md` does not exist anywhere in the repository.
It is also cited from a source-code comment in `internal/repository/postgres.go`
(`CreateBinding`, on the status/binding_mode upper-casing fix). The gap the comment
describes (a case-sensitivity bug that silently defeated the `capability_binding_primary_
excl` exclusion constraint) has evidently already been *fixed* in code, but the ADR meant
to record that class of conformance gap and its remediation was never committed, or was
lost. `docs/adr/index.md` additionally repeats its entire five-line list of ADRs twice
verbatim (lines 1–5 and 6–10 are identical before ADR-0005/ADR-BCP-001 are appended) — a
mechanical copy-paste defect independent of the missing-file issue.

This audit document and its companion matrix (`canonical-contract-matrix.md`) are written
so that a future ADR-0005 (or its equivalent) can cite them directly as the evidence base,
rather than needing to re-derive it.

---

## 9. README / implementation drift

Root `README.md`'s "Repository structure" section describes:

```
internal/
├── api/                   # HTTP layer: router, handlers, middleware
├── domain/
│   ├── tenant/
│   └── entitlement/
├── reconcile/
├── events/                # outbox + RabbitMQ publisher
├── store/
├── gateway/                # APISIX admin API client
└── config/
pkg/
└── contracts/              # generated clients from baobab-platform/shared
```

The actual tree has the HTTP layer at top-level `api/` (not `internal/api/`), a flat
`internal/domain/*.go` (no `tenant/`/`entitlement/` subpackages), no `internal/events`, no
`internal/gateway`, no `pkg/contracts`, and additionally has `internal/auth/`,
`internal/repository/`, `internal/resolver/`, `internal/service/` and `internal/reconcile/`
that the README's tree omits entirely. This is a documentation-only defect (P3) but is
exactly the kind of thing that costs a new contributor real time.

---

## 10. Remediation backlog (prioritised)

**P0 — must fix before any further feature work on tenant lifecycle or context resolution:**
1. Reconcile §2.1: either (a) bring the three orphaned migrations into the canonical,
   schema-qualified model (rename past `000018`, register them in
   `canonicalMigrationNames`, and — preferably — move their tables into an explicit schema
   such as `tenancy.*` for consistency with the rest of the schema, then update
   `store.go`'s SQL to match), or (b) fold tenant/legal-entity/provisioning persistence
   into the existing `registry`/`policy`/`audit`/`messaging` schemas from the canonical
   migration set and delete the orphaned files outright. Either is acceptable; leaving the
   current split is not. This audit applies the minimal-risk version of (a) — see the
   accompanying commit — as an immediate unblock, and records full schema-qualification
   and ID-grammar alignment as the follow-up in item 3 below.
2. Add an integration test that runs `ApplyMigrations` against a real PostgreSQL 17 and
   then exercises `store.RegisterTenant` → `store.GetTenant` → `store.ResolveContext`
   end-to-end, so this class of defect cannot recur silently.

**P1:**
3. Introduce a single `internal/idtype` (or equivalent) package owning the `tn_`/`map_`/
   `ref_`/`scope_` ID grammars from `domain.schema.json` as typed constructors and
   validators, and apply it consistently in place of the current per-file ad hoc regexes;
   correct `RegisterTenant.Validate()`'s inverted case-sensitivity bug on
   `legal_entity_id`.
4. Add `effective_from`/`effective_to` and the `mapping_single_authoritative_excl`-style
   exclusion constraint to `mapping.canonical_mapping`, matching the invariant
   `canonical-mapping.schema.json` and `ADR-BCP-001` §16.3 both specify.
5. Update `contracts.lock.yaml` to declare every `baobab-platform/shared` contract file actually
   implemented against, and add a CI job that fails when the pinned commit's schemas
   diverge from what `baobab-cp` emits/persists.
6. Design and implement the transactional outbox → RabbitMQ publisher using the canonical
   `messaging.outbox` table and the ADR-0004 CloudEvents envelope, once §10.1 lands (do not
   build it against the legacy `outbox_events` table).

**P2:**
7. Replace `docs/governance/readme.md`'s content with a short pointer to the current root
   `README.md` (done in the accompanying commit — the historical content remains
   recoverable from git history, consistent with ADR-0001's "not discarded blindly"
   instruction).
8. Fix the Django/Wagtail stack table and standards reference in `docs/Coding-Standards.md`
   (done in the accompanying commit).
9. Either author the missing `docs/adr/0005-bcp-db-001-conformance-gap.md` (this audit and
   the matrix are deliberately structured to serve as its evidence base) or remove the
   dangling reference from `docs/adr/index.md` until it is authored; de-duplicate
   `index.md`'s repeated ADR list (done in the accompanying commit — reference removed
   pending a real ADR-0005).

**P3:**
10. Correct root `README.md`'s "Repository structure" section to match the actual tree
    (done in the accompanying commit).
11. Reconcile `capability.code`/`engine.code` naming with the shared model's `*_key`
    convention once §10.1/§10.3 land, to avoid a second local synonym for the same
    concept.

---

## 11. Explicitly out of scope for this pass

Given the size of the original request (a full audit across identity, mapping, tenancy,
API, events, migrations, CI, containers, security and documentation), this pass
deliberately prioritised **finding and fixing the single defect that makes the product's
advertised core function non-operational** (§2.1) plus the mechanical, low-risk,
high-confidence documentation fixes (§7–§9), over attempting a blind rewrite of
persistence-layer SQL, ID formats, or event publication without a real PostgreSQL instance
to verify against in this environment. Items 3, 4, 5, 6 and 11 in §10 are real, verified
gaps, not speculation — but they involve schema and API changes wide enough that making
them without integration-test verification would trade one unverified state for another.
They are recorded here, with enough evidence to act on directly, as the next slice of work.

---

## 12. P1 remediation record

Applied in follow-up commits on this branch, each verified rather than asserted:

- **§10 item 1 (P0) / §2.1** — done in the initial pass, and confirmed here as still green:
  `cmd/migrate` run against a clean PostgreSQL 16/17 creates `tenants`, `legal_entities`,
  `product_subscriptions`, `provisioning_operations`, `outbox_events` and `audit_events`,
  and a full `RegisterTenant → GetTenant → ResolveContext → UpdateTenantLifecycle` sequence
  now passes end-to-end (`internal/store/postgres/store_integration_test.go`). Fixing this
  also surfaced two further defects, both fixed and covered by a regression test that fails
  against the pre-fix code: `ApplyMigrations` ran its `CREATE SCHEMA IF NOT EXISTS system`
  and per-migration DDL through `*pgxpool.Pool.Exec/Begin` *before*, and independently of,
  its `pg_advisory_lock` call — session-scoped advisory locks require one physical
  connection for their whole lifetime, but a pool hands out a connection per call, so two
  concurrent `ApplyMigrations` callers had no real mutual exclusion and could race on
  schema creation (`migrate_concurrency_test.go`); and `RegisterTenant`'s insert sent a nil
  Go map for an omitted `metadata` field as SQL `NULL` into a `jsonb NOT NULL DEFAULT '{}'`
  column, failing every registration that omitted metadata (fixed with `COALESCE`).
- **§10 item 3 (P1)** — `internal/domain/ids.go` now owns the `tn_`/`map_`/`ref_`/`scope_`
  grammars and the canonical/legacy-alias legal-entity grammar as the one place they are
  checked, replacing the inverted `legal_entity_id` check and the ad hoc regexes previously
  duplicated across `tenant.go`, `context.go` and `router.go`. Fixing this surfaced a
  further, contract-level defect: `tenant-registration.schema.json` does not accept
  `tenant_id` as input at all (it is Control Plane-minted per `tenancy.yaml`), but
  `RegisterTenant` required and trusted a client-supplied one; `tenant_id` is no longer a
  JSON field on the command, and the HTTP handler mints one via `domain.NewTenantID()`.
- **§10 item 4 (P1)** — migration `000022_canonical_mapping_temporal_integrity.sql` adds
  `effective_from`/`effective_to` and `canonical_mapping_source_type_active_excl`, a GiST
  exclusion constraint rejecting a second active mapping of the same type for the same
  source entity with an overlapping validity window.
  `internal/repository/postgres.go`'s `CreateMapping`/`GetMapping`/`ListMappings` now
  round-trip these fields instead of discarding them on write and fabricating them from
  `created_at` on read; a constraint violation surfaces as `repository.ErrMappingOverlap`.
  Verified with `postgres_mapping_test.go` against real PostgreSQL: rejects an overlapping
  insert, accepts a non-overlapping successor once the prior mapping is retired.
- **§10 item 5 (P1)** — `contracts.lock.yaml` now declares `domain.schema.json`,
  `tenant-registration.schema.json`, `canonical-mapping.schema.json`,
  `provisioning-state-machine.yaml`, `errors/v1/problem-details.schema.json` and
  `idempotency/v1/policy.yaml` alongside what was already listed. Its pinned commit
  (`0bc19d2a`) predated `canonical-mapping.schema.json` entirely and has been bumped to
  `baobab-platform/shared`'s current HEAD (`2da1a429`). The new `internal/contracttest` package
  compiles a schema from a local `baobab-platform/shared` checkout (`SHARED_CONTRACTS_DIR`) and
  validates a marshaled Go value against it; three tests now pass against the pinned
  commit — `RegisterTenant` against `tenant-registration.schema.json`, `ResolvedContext`
  against `context-resolution.schema.json`'s response definition, and the `problem()`
  error-response helper against the organisation-wide `problem-details.schema.json`
  (confirming this one was already field-for-field correct). `.github/workflows/ci.yml`
  now checks out `baobab-platform/shared` at exactly the pinned commit and adds a `postgres:17`
  service, so both this and the PostgreSQL integration tests run in CI instead of always
  skipping.
- **§10 item 6 (P1) — envelope + transactional outbox wiring done; publisher still open.**
  `internal/events` implements the ADR-0004 CloudEvents envelope as a validated, typed
  constructor (`events.New`), failing closed on a malformed type, a missing
  `correlationid`, or a `tenantid` that isn't a canonical Control Plane identifier, matching
  the "never invent a default tenant" requirement. `contract_compatibility_test.go`
  validates both a tenant-scoped and a platform-scoped constructed envelope against the
  real `contracts/events/v1/envelope.schema.json`.

  Wiring this into a transactional outbox write surfaced a further defect, now fixed:
  `messaging.outbox` (migration `000015`) typed `tenant_id` and `aggregate_id` as `uuid`,
  which cannot store the `tn_`-prefixed opaque strings that are the actual identifier
  grammar for tenant-provisioning aggregates (`domain.schema.json`'s `tenantId`). Migration
  `000023_outbox_tenant_identity.sql` widens both columns to `text` (aggregate identity is
  polymorphic across bounded contexts — a `uuid` for a canonical-entity/mapping aggregate,
  a `tn_`-prefixed string for a tenant aggregate — so neither type should be forced to fit
  the other). `store.go`'s `RegisterTenant` now constructs a real
  `com.nabhold.control-plane.tenant-provisioning-started.v1` envelope via `internal/events`
  and writes it into `messaging.outbox` atomically with the tenant row, replacing the
  legacy `outbox_events` insert and its ADR-0004-retired snake_case shape. Verified against
  real PostgreSQL: `TestTenantLifecycleEndToEnd` confirms exactly one outbox row is written
  per registration (and that an idempotent replay does not duplicate it), and the new
  `TestRegisterTenantOutboxEventMatchesSharedSchema` reads that row back and validates it
  against the real `contracts/events/v1/envelope.schema.json` — full round-trip proof, not
  an assertion.

  **What remains open, and why:** the RabbitMQ publisher itself (reading unpublished
  `messaging.outbox` rows, publisher confirms, dead-lettering) is still entirely unbuilt: no
  AMQP broker was available in this environment to verify a publisher against, and per this
  audit's own standard of not shipping unverified integration code, it was not built blind.
  The outbox now contains real, schema-valid events ready for a publisher to consume — that
  is the next and, as far as this audit found, final step for item 6.

Verified together, not just individually, at every stage of this remediation: `go build
./...`, `go vet ./...`, `gofmt -l cmd internal`, `go test ./...` and `go test -race ./...`
all pass with both `TEST_DATABASE_URL` (a real PostgreSQL instance) and
`SHARED_CONTRACTS_DIR` (a real `baobab-platform/shared` checkout at the pinned commit) set.
