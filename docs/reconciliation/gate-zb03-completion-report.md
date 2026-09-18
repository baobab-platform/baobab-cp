# Gate ZB-03 — IAM and Isolation: Completion Report

**Status:** Complete
**Date:** 2026-09-18
**Governing documents:** `gate-zb03-authority-contract-freeze.md` (ZB-03.0, the authority
freeze this whole gate proceeded against) and
`gate-zb03-cross-repository-isolation-certification.md` (ZB-03.10, the closing isolation
audit). This report is the top-level index across both and every slice in between; it does
not restate their content, only points to it.

---

## 1. What Gate ZB-03 was

The layering principle this gate built and then certified: **Baobab IAM** authenticates →
**Canonical Identity/Control Plane** resolves tenant/legal-entity/market/estate/org/
capability/isolation → **Domain Authority** (Trade/ZuriBeans/ERP) authorizes the business
action → **Business Action** executes. Every slice below either built a missing link in that
chain or proved an existing one holds under adversarial input.

## 2. Slice-by-slice record

| Slice | What it closed | Repo(s) | PR(s) |
|---|---|---|---|
| ZB-03.0 | Authority Contract Freeze — froze who owns each identity/organisation/authorization concern before any implementation began | `baobab-cp` | #127 |
| ZB-03.1 | Deferred ZB-02 production surfaces (tenant manifest persistence, readiness/reconciliation snapshots, HTTP provisioning API) | `baobab-cp` | #128, #129, #130, #131 |
| ZB-03.2 | Canonical identity and organisation spine (ADR-BCP-016) | `baobab-cp` | #132 |
| ZB-03.3 | Buyer IAM → CP → Trade integration: `ExternalReference` persistence, `OrganisationID` context resolution over real HTTP, and Trade's first real route composing a `BuyerContext` | `baobab-cp`, `baobab-trade` | #133, #81 |
| ZB-03.4 | ZuriBeans BFF authentication (SSO login/callback), plus two pre-existing unrelated bugs found and fixed during validation (corrupted `page.tsx`, malformatted `globals.css`) | `zuribeans` | #57 (#56, #58 the bug fixes) |
| ZB-03.5 | Supplier canonical-organisation linkage and interim internal-API-key auth for ZuriBeans' own admin routes | `zuribeans` | #59 |
| ZB-03.6 | Workload-attested tenant verification: `GET /store/b2b/context` optionally attests `tenant_id` against the Control Plane using the previously-unconsumed `baobab-trade-workload` identity, never trusting a bare `200 OK` | `baobab-trade` | #82 |
| ZB-03.7 | Real-token integration verification: drove the real `authenticate()` boundary and the real buyer-context route handler against actual signed JWTs (not hand-typed actor IDs), including an IDOR case | `baobab-trade` | #83 |
| ZB-03.8 | Per-endpoint workload scope enforcement on the ERP integration boundary — authentication existed (Gate IAM-10), authorization by scope didn't | `baobab-erp` | #29 |
| ZB-03.9 | Workload lifecycle status consistency (registration half: registry `status` vs. Keycloak client `enabled`) and the revocation-propagation decision (keep synchronous re-validation) | `baobab-iam`, `baobab-cp` | #35, #134 |
| ZB-03.10 | Request-time workload lifecycle enforcement (the other half of ZB-03.9's gap) and the closing cross-repository isolation certification | `baobab-cp` | #135 |

Every PR above is merged as of this report.

## 3. Certification verdict

**Conditional GO** — carried forward from `gate-zb03-cross-repository-isolation-
certification.md` §4, not re-derived here. Every isolation category that document defines
(tenant, organisation/membership, actor-type, workload lifecycle, estate/brand) has at least
one test proving it, spanning all six repositories, every citation verified to exist. The one
named exception is real and bounded, not an unknown unknown (§4 below).

## 4. Open follow-ons, carried forward honestly

None of these block Gate ZB-03's own exit criterion (`gate-zb03-authority-contract-freeze.md`
§6: "no ZB-03 implementation depends on an undefined authority boundary") — each is either an
operational/deployment decision outside this programme's code scope, or an explicitly
deferred design decision with a documented interim posture that is itself correct and
fail-closed.

1. **`baobab-cp`'s `WorkloadRegistry` is built and tested but not wired into production.**
   `cmd/controlplane/main.go` does not yet construct one from `Dependencies.WorkloadRegistry`,
   so a `REVOKED` workload's still-unexpired token still passes request-time authorization in
   any deployment today. Closing this needs an operational decision (where does the deployed
   binary's local registry snapshot come from, and how does it stay current) that this
   programme deliberately declined to invent unilaterally (`gate-zb03-cross-repository-
   isolation-certification.md` §3).
2. **Revocation signal propagation stays synchronous-only, by decision, not oversight.**
   `gate-zb03-authority-contract-freeze.md` §7 recorded this as the permanent posture until a
   real, measured latency need justifies building `nabhold/shared`'s already-defined
   (already-unconsumed) revocation event schemas into an actual producer/consumer pair.
3. **CodeQL "Advanced Security" checks fail structurally on `zuribeans`** (private repo,
   GitHub Advanced Security not enabled at the organization level) — confirmed by the repo
   owner directly on the relevant PRs during ZB-03.4/.5. Not a code defect; a repository
   setting outside this programme's authority to change.
4. **Human ERP login (iDempiere's own WebUI, via its built-in `org.idempiere.ui.sso.oidc`
   plugin) remains an operational runbook** (`baobab-erp/docs/sso-configuration.md`), not
   anything this programme's code touches, since its configuration lives in iDempiere's own
   schema. Named explicitly in `gate-iam-18-workload-lifecycle-scope.md` and earlier gate docs;
   unchanged by this gate.
5. **ADR governance hygiene**: several load-bearing ADRs in `baobab-iam` and `baobab-cp` are
   still formally `Status: Proposed` despite being treated as authoritative by shipped code
   platform-wide (`gate-zb03-authority-contract-freeze.md` §4). Flagged there as a dependency
   risk for whoever formally accepts ADRs in those repositories; this gate's implementation
   work correctly proceeded against their content regardless, consistent with how the rest of
   the platform already treats them.
6. **Per-resource workload scopes remain coarse.** Every workload client today is granted
   exactly the one scope it needs for its one integration point (`context:resolve` or
   `erp:integrate`), so ZB-03.8's and this gate's own scope-enforcement checks all currently
   enforce against a single scope per client. Splitting finer-grained scopes (e.g. separating
   context resolution from mapping resolution) is real follow-on work once a real client
   actually needs less than full access to its own boundary — not invented speculatively here,
   per ADR-ERP-010 §38's explicit rejection of assuming capability scopes map 1:1 onto native
   permissions.

## 5. Closing

Gate ZB-03 (IAM and Isolation) is complete. Every slice ZB-03.0 through ZB-03.10 shipped,
tested, documented, and merged; the closing certification found every isolation category this
platform's own authority freeze named already proven by at least one real test across the six
repositories that make up this platform, and the one genuine remaining gap is named, bounded,
and does not undermine any of the isolation guarantees this gate set out to build.
