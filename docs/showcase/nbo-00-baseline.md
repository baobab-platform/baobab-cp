# NBO-00 — Architecture and Readiness Lock

**Observed:** 2026-10-09. **Scope:** repository-state evidence, not deployed-service certification.
**Policy:** ADR-BCP-017/018/020–024 and ADR-SHARED-012–015 override earlier implementation plans. CP is Go + PostgreSQL; ERP is iDempiere; Trade is Medusa; IAM is Kratos/Hydra with permanent Keycloak federation.

## Locked live baselines (default branch)

| Repository | Observed main | Meaning |
|---|---|---|
| shared | `ec5d1b486b796f4403edbf051246ddc196ff20a4` | canonical contracts; NBO-01 changes under [PR #255](https://github.com/baobab-platform/shared/pull/255) |
| baobab-cp | `489c921898c00e8d5432a113d0432fbadb7ddfbe` | domain services, governance APIs, in-repo frontend; this change on CP [PR #289](https://github.com/baobab-platform/baobab-cp/pull/289) |
| baobab-iam | `ef7472023634b9324a724068bbe3b71c919f61a5` | governed identity runtime; [PR #106](https://github.com/baobab-platform/baobab-iam/pull/106) open |
| subscriptions | `b682650be7bfc31ee98ca40cbd4e363ae7536323` | separate subscription authority; no direct ERP provisioning |
| trade | `d19ba6ace4163ab84f38a5f53d0a114047c506e0` | Medusa-based provider |
| erp | `4c1848ba43b9602dd67339dea24143c974f1c172` | iDempiere-based provider |
| cms | `19deac119b3232ccbf29f1889b62cfabb43ceeb1` | Payload content provider |
| pulse | `6f4271d7b43db8d9c81e04d4f5dcc4cf50b27172` | intelligence engine |
| infrastructure | `48ff6272db06143fe7418453770a3730cf3061b7` | deployment definitions, not an operational showcase certificate |
| nabhold | `f23c681502f00d4c37ff4e2e274aae36ee899d85` | authoritative Nabhold corporate digital estate |
| thamani | `8dd76e97a358e3aeb27e7b9d27f6f2177a5bd2c3` | authoritative Thamani digital estate |
| zuribeans | `a03690116ce5cd4727e5fe8284996a97e479a3f3` | independent B2B estate |
| regulations | `4fab60ef52ab0661901db2814fa0ab661bdbc369` | regulatory authority integration requires separate proof |
| trade-docs | `e889b6524fab7ed5c0143834e247ccd3f174c803` | trade document boundary; not a showcase activation prerequisite |

## Implemented source boundaries

- **Admission:** `api/client_application_handler.go`, `internal/service/application/service.go`, `internal/repository/postgres_admission.go`, migration 000050; applicant draft, review, independent decision, events and idempotency. Prior to NBO-01 staff creation was absent.
- **Corporate governance:** `internal/service/organisation/admission.go`, `firstparty_reconciler.go`, `internal/repository/postgres_organisation*.go`; corporation/group/account/tenant are distinct. A governance registry identity is not a verified incorporation.
- **Evidence:** `api/verification_handler.go`, `docs/reconciliation/oev-00-evidence-inventory.md`; verification APIs exist, but statutory and source-specific evidence proof must be established case by case. No generic showcase exception runtime was demonstrated.
- **Administrative authority:** `api/grant_admin_handler.go`, `api/changeset_handler.go`; existence of code does not prove every permission integration.
- **Provisioning:** `api/provisioning_convergence_handler.go`, `internal/provisioning/convergence`, `internal/provisioning/apply`. Desired, plan, operation, readiness and health require runtime integration proof.
- **Market:** `api/market_handler.go`; intent is not legal presence or market activity permission.
- **Console:** `frontend/src/server/cp-client` and generated OpenAPI contract; source code does not establish deployed OIDC session or investor visitor access.

## Source-of-truth and execution constraints

1. Shared is contract authority; CP is runtime organisation, admission and provisioning authority. Subscriptions owns product subscriptions, IAM owns identity, native engines own operational resources.
2. **Verified corporate control** is distinct from claimed relationships. Derived CorporateGroup never conveys IAM authority.
3. A staff-assisted application can only be a draft until the normal governed transitions occur. No new default `INTERNAL_GROUP` subscription type.
4. NBO-01 is a reusable platform flow. Nabhold-specific configuration belongs in fixtures, not policy branching.
5. No production deployment, authenticated IAM integration, provider readiness or onboarding of a Nabhold tenant has been verified by this source audit.

## Existing work, avoid duplication

- Shared [#252](https://github.com/baobab-platform/shared/pull/252) proposes Nabhold CIPC claim values but explicitly awaits independent verification. Do **not** treat this unmerged PR as verified facts.
- Shared [#253](https://github.com/baobab-platform/shared/pull/253) proposes corporate finance namespace; [#254](https://github.com/baobab-platform/shared/pull/254) was merged to main at this snapshot.
- IAM [#106](https://github.com/baobab-platform/baobab-iam/pull/106) covers CP-dispatched governed operations.
- CP core already includes a first-party reconciler and organisation admission logic; do not add a parallel registry or legacy TenantManifest.

See `nbo-00-capability-matrix.md` and `nbo-00-dependencies.md` for proof limits.
