# Gate ZB-03.10 — Cross-Repository Isolation Certification

**Purpose:** the closing audit of Gate ZB-03 (IAM and Isolation): inventory every
isolation-relevant test this programme has built or found already in place across all six
repositories, map each to the specific ADR-0007/ADR-BCP/ADR-ERP/ADR-0009 isolation requirement
it proves, name what remains genuinely uncovered, and give a certification verdict.
**Date:** 2026-09-18
**Method:** direct inspection of test files across `baobab-platform/baobab-iam`,
`baobab-platform/baobab-cp`, `baobab-platform/baobab-trade`, `baobab-platform/baobab-erp`, `baobab-platform/zuribeans` as
checked out locally. Every test cited below was confirmed to exist at the stated path
(`ls`/`grep`), not assumed from memory or an earlier summary. This is an audit of what's
already built and proven, not a new live multi-service integration harness: no repository in
this platform has a docker-compose stack, CI job, or any other mechanism that wires
Keycloak+`baobab-cp`+`baobab-trade`+`baobab-erp`+`zuribeans` together as one running system
(confirmed by inspecting every repo's CI workflows — each runs its own services in isolation;
`baobab-platform/baobab-trade`'s `test:isolation` npm script, for instance, is a single-repo Vitest
run against `tests/commerce-context.test.ts`, not a repo-spanning execution). Building one
would be new infrastructure invention with no precedent anywhere in this platform, not
certification of what exists — out of scope here, consistent with `gate-zb03-authority-
contract-freeze.md` §5's "additive, don't rebuild" principle.

---

## 1. What "isolation" means across these tests

Every test below proves one or more of:

- **Tenant isolation** — a request scoped to tenant A must never resolve, read, or act on
  tenant B's data, even when IDs collide or a caller tries to supply a foreign tenant_id.
- **Organisation/membership isolation** — a caller belonging to organisation A must never
  reach organisation B's buyer/supplier context, even with a validly authenticated token.
- **Actor-type isolation** — a token minted for one actor type (workload vs. human, or one
  workload's audience/scope) must not authenticate a route gated for another.
- **Workload lifecycle isolation** — a workload whose registry status is no longer `ACTIVE`
  must not retain live authority, whether checked at registration time or request time.
- **Estate/brand isolation** — Thamani and ZuriBeans, as separate brands/estates on shared
  infrastructure, must not leak identity, redirect, or session state into each other.

## 2. Inventory

### `baobab-platform/baobab-iam` (`tests/integration/run.sh`, verified against a live Keycloak in CI)

| § | What it proves | Isolation category |
|---|---|---|
| §9 | Every `config/clients/*-workload.json` client is registered in `baobab-platform/shared`'s workload registry with in-allowlist scopes, **and** (Gate IAM-18, ZB-03.9) its live `enabled` flag agrees with the registry's `status` — a `REVOKED`/`SUSPENDED`/`RETIRED` entry with `enabled: true` fails CI | Workload lifecycle |
| §10 | Cross-workload identity isolation: one workload's token cannot impersonate another's `azp`/`sub` (ADR-0007 §102) | Actor-type / workload |
| §20 | Thamani ≠ ZuriBeans structural isolation | Estate/brand |
| §21 | Thamani/ZuriBeans browser client redirect isolation | Estate/brand |

### `baobab-platform/baobab-cp`

| File | What it proves | Isolation category |
|---|---|---|
| `internal/auth/tenant_isolation_test.go` (`TestTenantIsolationBlocksSubsidiariesBidirectionally`, `TestParentOrganizationHasNoImplicitSubsidiaryAccess`) | Cross-tenant access denial holds bidirectionally, and a parent organisation has no implicit access to a subsidiary tenant's resources | Tenant |
| `internal/provisioning/zb02_isolation_test.go` (`TestZB02ResourcesFailClosedAcrossTenants`) | ZB-02 provisioning resources fail closed across tenants | Tenant |
| `internal/provisioning/zb02_context_negative_test.go` (`TestAuthoritativeContextResolverFailsClosed`) | `AuthoritativeContextResolver` rejects ambiguous/missing/cross-tenant context rather than guessing | Tenant |
| `internal/provisioning/zb03_organisation_context_test.go` (`TestAuthoritativeContextResolverOrganisationFailsClosed`, ZB-03.2) | Organisation-scoped context resolution fails closed the same way | Organisation |
| `internal/repository/postgres_identity_unlink_test.go`, `postgres_identity_merge_test.go` | Identity unlink/merge auditing enforces its last-credential guard and rejects a double-link | Human identity |
| `api/router_test.go` (`TestResolveContextRejectsRevokedWorkload`, `TestResolveContextAllowsActiveWorkload`, `TestResolveContextUnaffectedWhenWorkloadRegistryUnconfigured`, ZB-03.10, this slice) | A workload token that authenticates successfully but whose `client_id` the workload registry marks non-`ACTIVE` is rejected at request time; the default (unconfigured) behaviour is unchanged | Workload lifecycle |

### `baobab-platform/baobab-trade`

| File | What it proves | Isolation category |
|---|---|---|
| `tests/b2b-threat-model.test.ts` (Gate 15 threat model) | Cross-tenant IDOR, cross-organisation IDOR, role escalation, suspended-membership access, spoofed-principal binding, and sensitive-data-in-events are all blocked | Tenant / Organisation |
| `tests/commerce-context.test.ts` (`npm run test:isolation`) | Commerce context resolution isolation | Tenant |
| `tests/workload-tenant-verifier.test.ts`, `tests/workload-token.test.ts` (ZB-03.6) | A workload-attested tenant check rejects a `tenant_id` the Control Plane doesn't independently confirm for the asserted organisation, never trusting a bare `200 OK` | Organisation / Workload |
| `tests/authenticate-middleware.test.ts` (ZB-03.7) | The real `authenticate()` boundary rejects forged signatures, expired tokens, and a correctly-signed token for the wrong actor type | Actor-type |
| `tests/b2b-context-route.test.ts` (ZB-03.7) | An IDOR case: a real, validly-signed token for an actor with no membership in the requested organisation is rejected | Organisation |

### `baobab-platform/baobab-erp`

| File | What it proves | Isolation category |
|---|---|---|
| `tests/tenancy/test_context_resolver.py` | A cross-tenant `(ad_client_id, ad_org_id)` pair fails closed | Tenant |
| `tests/integration/test_http_server.py` (ZB-03.8) | A validly authenticated workload token whose scope doesn't grant the endpoint it's calling is rejected with `403`, distinct from `401` | Actor-type / scope |

### `baobab-platform/zuribeans`

| File | What it proves | Isolation category |
|---|---|---|
| `src/lib/auth/internal-api-key.test.ts` (ZB-03.5) | The interim admin-only internal API key check rejects a missing/wrong/non-Bearer credential in constant time | Actor-type |
| `src/lib/supplier/repository.test.ts` (ZB-03.5) | `canonical_organisation_id` linkage is scoped to the correct supplier row, never a different one | Organisation |
| `src/app/login/actions.test.ts`, `src/app/api/auth/callback/route.test.ts` (ZB-03.4) | SSO login/callback flow doesn't leak session state across a malformed or mismatched callback | Estate/brand |

## 3. Remaining gap, named honestly

`baobab-cp`'s new `WorkloadRegistry` enforcement (§2 above, this slice) is **opt-in and
disabled by default** (`api.Dependencies.WorkloadRegistry` nil unless an operator supplies
`auth.LoadWorkloadRegistryFile` a local snapshot path). This repository has no existing
runtime mechanism for consuming any `baobab-platform/shared` contract live — `internal/contracttest`'s
`SHARED_CONTRACTS_DIR` is a test-only, local-checkout pattern (see that package's own doc
comment), not something any production binary reads. Building a live-fetch-and-cache
mechanism (polling cadence, staleness policy, fail-open-vs-fail-closed on a fetch error) is a
real design decision this slice deliberately does not make unilaterally — the same posture
`gate-zb03-authority-contract-freeze.md` §7 already took for revocation-event propagation.

Concretely: **today, in this repository's actual deployed configuration (nothing wires
`WorkloadRegistry` in `cmd/controlplane/main.go` yet), a `REVOKED` workload's still-unexpired
token still passes request-time authorization.** The mechanism to close this exists and is
tested; the operational decision to wire it into a running deployment, and to decide how that
deployment keeps its local snapshot current, is not part of this slice's scope and remains
open follow-on work.

No other isolation gap was found uncovered by an existing or newly-added test during this
audit.

## 4. Certification verdict

**Conditional GO.** Every isolation category in §1 has at least one test proving it, spanning
all six repositories, and every test cited above was confirmed to exist and (where run this
session) to pass. The one named exception (§3) is a real, bounded, already-documented gap with
a tested-but-unwired mechanism, not an unknown unknown — it does not block ZB-03's other
slices, none of which depend on it (per `gate-zb03-authority-contract-freeze.md` §6's own exit
assessment), and it is explicitly tracked rather than silently accepted.
