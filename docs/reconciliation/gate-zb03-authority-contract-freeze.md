# Gate ZB-03.0 — Authority Contract Freeze

**Purpose:** the mandatory first step of Gate ZB-03 (IAM and Isolation) before any
implementation begins. Establishes, with evidence, which repository owns each
identity/organisation/authorization concern today, which repositories may only consume a
projection of it, and which concerns have **no owner yet** — so no ZB-03 slice is built on
an assumed authority that does not actually exist.
**Date:** 2026-09-17
**Method:** direct, read-only inspection of `nabhold/baobab-iam`, `nabhold/baobab-cp`,
`nabhold/baobab-trade`, `nabhold/baobab-erp`, `nabhold/zuribeans` and `nabhold/shared` as
checked out locally — ADRs, Go/TypeScript/Python/Java source, SQL migrations, JSON Schema
contracts, and existing tests. Every claim below cites a file. Nothing is asserted from ADR
prose alone where code could be read directly.
**Scope:** this document freezes *authority* (who decides / who is the system of record).
It does not itself implement anything — see `docs/reconciliation/gate-zb03-*` follow-ups and
this repository's task list (ZB-03.1 onward) for the implementation slices this freeze
unblocks.

---

## 1. Required authority table

| Concern | Authoritative owner | Projection consumers | Forbidden duplicate owner | Status |
|---|---|---|---|---|
| Human authentication (password/passkey/OTP, session issuance) | `baobab-iam` (Keycloak) | all engines | Trade / ZuriBeans / ERP / CMS rolling their own login | **Real.** Keycloak realm `baobab`, PKCE-only public clients, dedicated workforce/admin clients (`baobab-iam/config/clients/*.json`, `tests/integration/run.sh` §7/§11-12). |
| Canonical human identity (issuer+subject → durable Principal) | `baobab-cp` | all engines that need a stable identity key across logins/engines | any engine minting its own "canonical" identity | **Real and wired.** `Principal`/`ExternalIdentity`/`IdentityReference` (`baobab-cp/internal/domain/identity.go`), consumed today by `requireAdminRole` (`baobab-cp/api/router.go:187-234`) — the only place in the whole platform this chain is live end-to-end right now. |
| Workload (machine-to-machine) authentication | `baobab-iam` | resource servers (baobab-cp, baobab-erp, …) | local API keys, shared service accounts, network-location trust | **Real.** 6 dedicated OAuth client-credentials clients, one per workload boundary (`baobab-iam/config/clients/*-workload.json`), independent-revocation proven live (`tests/integration/run.sh` §8). Consumed correctly by `baobab-erp`'s JWKS-validated workload-token middleware (`baobab-erp/modules/security/workload_auth.py`) and `baobab-cp`'s `WorkloadVerifier` (`baobab-cp/api/router.go:87-95`). |
| Workload lifecycle state (beyond issuer-level enable/disable) | **unowned** | — | — | **Gap.** ADR-0007 (`baobab-iam`) specifies `PROVISIONED→ACTIVE→SUSPENDED→REVOKED→RETIRED`; nothing implements it — revocation today is binary Keycloak client `enabled` toggling only (`baobab-iam` gate-iam-4 doc). |
| Tenant lifecycle (PLAN/APPLY/RECONCILE/READY/ACTIVE, suspend/decommission) | `baobab-cp` | all engines via `/v1/context/resolve` and `/v1/tenants/*` | any engine treating its own local tenant flag as authoritative | **Real.** `baobab-cp/internal/domain/tenant.go`, `internal/store/postgres/store.go`, Gate ZB-02 orchestration (`internal/provisioning/`). |
| Legal entity identity | `baobab-cp` (bare identifier) / `baobab-erp` (ERP-side `AD_Client`/`AD_Org` mapping) | Trade, ZuriBeans, ERP | any engine treating legal entity as a Tenant field with business meaning | **Partial.** `baobab-cp` models `LegalEntityID` only as a referenced string, no struct (`docs/adr/0003-multi-tenant-control-plane-architecture.md` §4.1). `baobab-erp` has a real, tested, fail-closed Tenant/LegalEntity → `AD_Client`/`AD_Org` mapping table and resolver (`baobab-erp/db/migrations/0002_create_tenant_mapping.sql`, `modules/context/resolver.py`) — but **no concrete Uganda/South Africa legal entity has actually been provisioned** in either repo yet (no fixture, migration, or test names them; `baobab-erp` tests use `NABHOLD`/`THAMANI` only). |
| **Canonical Organisation identity (buyer or supplier org as a first-class entity)** | **unowned** | — | — | **The central gap. See §2.** |
| Buyer organisation commercial model (membership, roles, spend/approval/credit authority) | `baobab-trade` | ZuriBeans (read-only), ERP (via `erp_business_partner_id` seam) | `baobab-iam` (a Keycloak Organization is identity-side affiliation only, never commercial authority) | **Real, but unreachable.** `b2b_organisation`/`buyer_membership`/`buyer_role` and separate commercial-authority models (spend limit, approval policy, credit terms) all exist (`baobab-trade/src/modules/b2b/models/*.ts`) with strong policy tests — but **zero HTTP route** exists in `baobab-trade` to reach any of it (`src/api/` has only health/readiness), and buyer-facing OIDC is explicitly deferred ("Gate IAM-7", `baobab-trade/medusa-config.ts:57-64`). |
| Supplier qualification / lifecycle | `zuribeans` (estate-owned, per accepted ADR) | ERP/CP projections once wired | `baobab-iam` (identity ≠ approval) | **Real.** `supplier_organisations`/`supplier_capabilities`/`supplier_certifications`/`supplier_status_events`, full 11-state lifecycle with transition guard (`zuribeans/src/lib/db/schema.ts`, `src/lib/supplier/lifecycle.ts`), `canonical_organisation_id` reserved and unpopulated (`schema.ts` line 50). No supplier-representative/membership model exists yet (`supplier_contacts` is inert metadata, not authorization). |
| Market participation / trade lane authority | `baobab-cp` | Trade, ERP, ZuriBeans | frontend market switcher deciding eligibility itself | **Real.** Gate ZB-02 (`MarketAssignment`, `TradeLane`), already isolation-tested. `zuribeans`'s own market-context resolver explicitly does **not** gate eligibility, only presentation (`zuribeans/docs/adr/0004-market-context-resolution.md:56-58`) — this is correct today only because nothing downstream yet relies on it for authorization; ZB-03.6 must not accidentally start trusting it. |
| ERP-native authorization (`AD_Role`, `AD_Client`/`AD_Org` grants) | `baobab-erp` | ERP only | IAM mirroring every role into iDempiere | **Decided, not implemented.** ADR-ERP-002 §50 / ADR-ERP-010 §37-39 (both Accepted) explicitly reject 1:1 role mirroring — but no mapping/policy code exists yet to translate an IAM role/scope into an `AD_Role` grant (`baobab-erp/architecture/conformance.yaml`). |
| Revocation propagation (identity/session/membership/workload/tenant/grant → downstream reject) | **unowned as a cross-repo signal**; each repo owns rejecting its own stale authority | all engines | — | **Gap. See §3.** `nabhold/shared` already defines the event schemas (`identity.disabled`, `session.revoked`, `workload.revoked`, `membership.revoked`, `entitlement.revoked`, `credential.compromised` — `shared/contracts/identity-events/v1/`); nothing emits them yet. |
| Cross-cutting event/error/idempotency contract shape | `shared` | all engines | any engine inventing its own envelope/error/idempotency-key shape | **Real and mature.** `contracts/events/v1/envelope.schema.json`, `contracts/errors/v1/problem-details.schema.json`, `contracts/idempotency/v1/policy.yaml`, `contracts/authorization/v1/{scope-registry,reason-code-registry,delegation}.yaml` — all Accepted, all reusable as-is. |

---

## 2. The central gap: no canonical Organisation authority exists

Every repository independently reserved a seam for this and explicitly deferred it —
confirmed by direct inspection, not inference:

- `shared`: no `Organisation`/`CanonicalEntity` resource schema, no `BUYER_ORGANISATION`/
  `SUPPLIER_ORGANISATION` entity-type enum anywhere in `contracts/`. `contracts/erp/v1/
  system-of-record.yaml`'s own `Organisation` concept is `canonical_owner: unassigned`.
- `baobab-cp`: `CanonicalEntity`/`Mapping`/`MappingScope`/`ExternalReference` all exist as
  real Go types and are the platform's actual identity spine (`internal/domain/canonical.go`,
  `internal/domain/resolution.go`). `docs/adr/0006-supplier-organisation-canonical-entity.md`
  (Accepted) registers `SUPPLIER_ORGANISATION` as an `EntityType` **name only** — no
  `Mapping`, no `MappingScope`, no HTTP endpoint, no migration, "no other control-plane code
  branches on this constant" (`internal/domain/entity_types.go:14-22`). `BUYER_ORGANISATION`
  does not exist anywhere in this repo (grep-confirmed, zero matches).
- `baobab-cp`'s own `ADR-BCP-014` (Accepted) *does* specify a general Organisation/
  `CounterpartyProfile`/`CounterpartyRole` model (`BUYER`, `SUPPLIER`, `CUSTOMER`, `VENDOR`,
  … as *roles* on a generic Organisation, not as `EntityType` values) and explicitly states
  a Keycloak Organization must never be canonical directly (§18-19: reachable only via
  `Canonical Organisation → ExternalReference → Keycloak Organization`) — but this remains
  prose/diagram only: no Go struct, no table, for `Organisation`, `CounterpartyProfile`,
  `CounterpartyRole`, or `CounterpartyRelationship` exists anywhere in the repo.
- `baobab-trade`: `b2b_organisation.canonical_organisation_id` (nullable, indexed, unpopulated
  — `src/modules/b2b/models/b2b-organisation.ts:13`) and a sibling `erp_business_partner_id`
  follow the identical reserved-but-unwired pattern.
- `zuribeans`: `supplier_organisations.canonical_organisation_id` (nullable, unpopulated —
  `src/lib/db/schema.ts:50`, comment: "Never populated by this increment's code").
- `baobab-iam`: Keycloak Organizations feature is enabled (`organizationsEnabled: true`,
  `config/realm/baobab-realm.json`) and proven to work at the Keycloak layer alone (create/
  delete via Admin API, `tests/integration/run.sh` §13) — but nothing links a Keycloak
  Organization to a `CanonicalEntity`, and member add/remove is entirely untested from this
  repo's side.

**Conflict to resolve, not silently pick a side on:** `ADR-0006` (Accepted, `baobab-cp`)
models supplier-org-ness as a `CanonicalEntity.EntityType` value (`SUPPLIER_ORGANISATION`).
`ADR-BCP-014` (Accepted, `baobab-cp`) models it as a `CounterpartyRole` on a generic
`Organisation` layered on top of `CanonicalEntity`. Both are Accepted; neither explicitly
supersedes or reconciles the other. ZB-03.2 (canonical identity/organisation spine) must
resolve this **as an architecture decision inside `baobab-cp`, citing both ADRs**, before
writing the `Organisation`/`BUYER_ORGANISATION` implementation — not invent a third shape.
The two are not necessarily incompatible (an `Organisation` `CanonicalEntity` with
`EntityType=ORGANISATION` and a `CounterpartyRole` sub-record of `BUYER`/`SUPPLIER` would
satisfy both), but that reconciliation has to be written down as an ADR amendment or a new
`ADR-BCP-01x`, not assumed.

The intended chain, once this is resolved (per `ADR-0010` in `baobab-iam`, cited by the
`baobab-iam` `gate-iam-6` governance doc, and consistent with `ADR-BCP-014` §18-19):

```
Keycloak Organization ID  (baobab-iam — identity-side affiliation only)
        │
        ▼  (ExternalReference)
CanonicalEntity / Organisation  (baobab-cp — canonical authority, ZB-03.2's job)
        │
        ├──▶ Trade b2b_organisation.canonical_organisation_id   (buyer commercial authority)
        │
        └──▶ ZuriBeans supplier_organisations.canonical_organisation_id  (supplier lifecycle authority)
```

No repository may treat a Keycloak Organization membership, alone, as buyer purchasing
authority or supplier approval — this is already independently and correctly enforced by
`baobab-trade`'s commercial-authority separation (§1 above) and `zuribeans`'s ADR-0006, and
must remain true once the canonical Organisation link is wired.

---

## 3. Revocation signal propagation — a second cross-cutting gap

`shared` already defines the full canonical event vocabulary for this
(`contracts/identity-events/v1/`: `identity-disabled`, `identity-suspended`,
`identity-reactivated`, `session-revoked`, `workload-revoked`, `membership-revoked`,
`entitlement-revoked`, `credential-compromised`, all on the standard envelope). What's
missing is universal: **no repository in this platform currently emits any of them.**

- `baobab-iam`: Keycloak's only configured event listener is `jboss-logging` (writes to its
  own server log). The Admin Events API exists and is proven to capture the right actions
  (`tests/integration/run.sh` §16-17) but nothing polls it. Framed in `baobab-iam`'s own
  governance docs as an "open architectural fork" (custom Event Listener SPI vs. downstream
  polling) — not resolved either way.
- `baobab-cp`: `RevokeGrant`, `UpdateTenantLifecycle` (backing tenant suspend/activate/
  decommission), `UnlinkExternalIdentityAudited`, and `MergePrincipalsAudited` all mutate
  authoritative state but write **no** outbox event (`internal/repository/outbox.go` only
  has the 6 Gate-ZB-02-scoped event types — confirmed by direct grep, no revocation-shaped
  event exists). `ADR-BCP-009` §59-60 (Proposed, not Accepted) already specifies the intended
  pipeline (`Transactional Commit → Audit + Outbox Event → Cache Invalidation → Readiness
  Recompute → Dependent Services`) — it has not been implemented, and its own ADR has not
  even been formally accepted yet despite being cited as authoritative elsewhere.
- Every downstream engine (`baobab-trade`, `baobab-erp`, `zuribeans`) therefore has **nothing
  to consume** for "a stale allow decision must become deny promptly" today. Each currently
  achieves fail-closed behaviour only by re-validating on every request (e.g. `baobab-erp`'s
  synchronous context lookup, `baobab-trade`'s `assertActiveBuyerContext` against live-fetched
  state) rather than via any push signal — which is *safe* (no stale ALLOW), but means a
  suspension takes effect only as fast as each engine's own re-validation cadence, not
  instantly. ZB-03.9 is where this needs a real decision: keep pure synchronous
  re-validation (simplest, already fail-closed, but couples every protected call to a live
  round trip) vs. add outbox-based revocation events as a **cache-invalidation accelerant**
  on top of (never instead of) synchronous re-validation for the highest-risk paths.

---

## 4. ADR governance hygiene — flagged, not blocking

Several repositories' shipped code already implements against specific section numbers of
ADRs that are **not** marked `Status: Accepted`:

- `baobab-iam`: **all 18 ADRs are `Status: Proposed`** — including ADR-0004 (canonical
  identity mapping), ADR-0007 (workload identity), ADR-0015 (MFA), ADR-0016 (revocation),
  which every other repo's code and ADRs cite as settled fact.
- `baobab-cp`: `ADR-BCP-004` (context/market/legal-entity resolution) and `ADR-BCP-009`
  (security/isolation/revocation) are `Proposed`, yet `internal/provisioning/
  context_resolver.go`, `internal/resolver/context.go` and `api/router.go` already cite
  their section numbers as authoritative in code comments. `docs/adr/index.md` is stale — it
  lists only 5 of this repo's 19 ADR files, omitting `ADR-BCP-002` through `015` entirely,
  several of which (`011`–`015`) are actually `Accepted`.

This is a real governance gap (per this program's own instruction: "treat accepted ADRs as
authoritative" — but several load-bearing ADRs are not formally accepted anywhere), and it is
explicitly **not** this freeze's job to unilaterally flip a `Status:` line to `Accepted`.
Recorded here as a dependency risk for whichever team/process formally accepts ADRs in
`baobab-iam` and `baobab-cp`; ZB-03 implementation work proceeds against these ADRs' content
as the best available specification (consistent with how the rest of this platform already
treats them), while flagging that their formal status should be reconciled.

---

## 5. What IS already solid and must not be rebuilt

To keep ZB-03 additive rather than a rewrite, per this programme's own governing principle:

- **Human canonical identity** (`baobab-cp`'s `Principal`/`ExternalIdentity`, gating
  `requireAdminRole`) — reuse directly for ZB-03.3/.4/.5, do not reinvent.
- **Workload authentication** (`baobab-iam`'s 6 client-credentials clients + `baobab-cp`'s
  `WorkloadVerifier` + `baobab-erp`'s JWKS-validated middleware) — reuse the pattern for any
  new protected endpoint; do not add API keys or network-trust-only paths.
- **Buyer commercial-authority separation** (`baobab-trade`'s spend limit / approval policy /
  credit terms kept structurally apart from `buyer_role`) — this is exactly the "IAM must
  never own commercial authority" principle already correctly implemented; extend it, don't
  touch its shape.
- **Supplier lifecycle state machine** (`zuribeans`'s 11-state `supplier_organisations.status`
  with transition guard and audit trail) — reuse as the supplier domain authority; ZB-03.5
  adds identity/membership around it, does not replace it.
- **Fail-closed tenant/legal-entity context resolution** (`baobab-erp`'s `modules/context/
  resolver.py`, `baobab-cp`'s `AuthoritativeContextResolver`) — both already reject on
  missing/ambiguous/cross-tenant input; extend with organisation/membership dimensions
  (ZB-03.6), don't rewrite the resolution mechanics.
- **`shared`'s identity/authorization/event contracts** (§1 table, last row) — reuse
  `Principal`, `WorkloadIdentity`, `Session`, `AuthenticationAssurance`, the revocation event
  family, `access-token-claims.schema.json`, `scope-registry.yaml`,
  `reason-code-registry.yaml`, `delegation.schema.json`, `problem-details.schema.json`,
  `idempotency/v1/policy.yaml` verbatim. Do not define parallel versions of any of these.

---

## 6. Exit assessment against §11's criterion

> *"No ZB-03 implementation depends on an undefined authority boundary."*

**Met, with two explicit exceptions carried forward as tracked work, not silent gaps:**

1. Canonical Organisation authority (§2) — **must be resolved in ZB-03.2** before any buyer/
   supplier-organisation-aware authorization code is written elsewhere. Until then, no
   downstream slice may invent its own organisation-authority shortcut (e.g. trusting a raw
   Keycloak Organization claim as commercial authority) — the existing, correct posture in
   `baobab-trade`/`zuribeans`/`baobab-erp` (never trust identity-side org claims for business
   authority) remains the fallback default.
2. Revocation signal propagation (§3) — **must be decided in ZB-03.9**. Until then, every
   engine's existing synchronous re-validation is the fail-closed default and must not be
   weakened; ZB-03.6/.7/.8's context/authorization work must not assume a push-revocation
   signal exists yet. **Resolved — see §7**: synchronous re-validation stays the sole mechanism;
   this bullet's "must not be weakened" instruction is now the permanent posture, not just the
   interim one.

Every other concern in §1's table has a named, evidenced, single owner. ZB-03.1 (deferred
ZB-02 production surfaces) has no dependency on either open item and may proceed immediately.

---

## 7. ZB-03.9 resolution (2026-09-18)

Per §6 item 2's own instruction ("must be decided in ZB-03.9"), the revocation-propagation
question is now decided, and the §1 table's "Workload lifecycle state" gap is partially closed —
both scoped narrowly, consistent with §5's "additive, don't rebuild" principle.

**Revocation signal propagation (§3): keep pure synchronous re-validation.** No outbox-based
revocation events are being built in this slice. Every engine's existing fail-closed re-validation
(`baobab-erp`'s synchronous context lookup, `baobab-trade`'s `assertActiveBuyerContext` against
live-fetched state, this repo's own `AuthoritativeContextResolver`) is already correct and already
safe — it just isn't fast. `shared`'s revocation event schemas
(`contracts/identity-events/v1/*.schema.json`) remain defined but unconsumed. Building an
event producer with no consumer, or a consumer with no producer, would be speculative work against
a need nothing has demonstrated yet; §3's "cache-invalidation accelerant" framing stands as the
right shape for that work *if and when* re-validation latency becomes a real, measured problem —
not before. This does not weaken anything: it is the same synchronous-only posture §6 item 2
already required as the interim default, now made permanent-until-justified rather than merely
interim.

**Workload lifecycle state (§1 table, row 28): `baobab-iam`-side consistency enforcement shipped;
`baobab-cp`-side request-time enforcement remains unowned.** `baobab-iam`'s
`tests/integration/run.sh` §9 now asserts every workload client's live `enabled` flag agrees with
`nabhold/shared`'s `workload-registry.yaml` `status` field (Gate IAM-18) — a registry entry marked
`SUSPENDED`/`REVOKED`/`RETIRED` with its matching Keycloak client still `enabled: true` now fails
CI. This closes the *registration-consistency* half of the gap (an operator following the
registry's own documented "set status to RETIRED before disabling the client" ordering can no
longer silently skip the second step). It does **not** close the *request-time enforcement* half:
this repo's `WorkloadVerifier` (`api/router.go:87-95`) still only checks token signature/issuer/
audience/actor_type, never cross-references the caller's `client_id`/`azp` against the registry's
`status` at all — a `REVOKED` workload's already-issued, not-yet-expired token would still pass
verification here today. That remains **unowned** and is real follow-on work for a future slice
(this repo's own PR, reading the registry the same way `baobab-iam`'s `run.sh` §9 already does),
not folded into this decision.
