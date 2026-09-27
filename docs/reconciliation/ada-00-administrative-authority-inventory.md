# ADA-00: Administrative authority inventory

**ADR:** ADR-BCP-020 §141, gate ADA-00 ("No blind migration") and §143 (migration from broad roles).
**Date:** 2026-09-27.
**Scope:** how the Control Plane authorises administration today, and what each mechanism becomes under AdministrativeGrants.

## What exists today

| Mechanism | Where | What it decides | Classification |
|---|---|---|---|
| `cp:platform-admin` Keycloak realm role | `api/router.go` `requireAdminRole`, `api/operation_handler.go` | Authorises every administrative route for every tenant. | **REPLACE.** It becomes platform-scoped grants, starting from the `platform-administrator` profile. It is not removed until shadow evaluation shows grants decide equally or more narrowly (§143–144). |
| `cp:tenant-admin` realm role plus an ACTIVE `WorkforceMembership` for the tenant | `requireAdminRole`, `tenantAdminOf` | Authorises tenant-scoped routes for that tenant only. | **REMODEL.** It becomes `tenant-administrator` profile grants at TENANT scope. The membership stays an identity fact, not authority (§15, §28). |
| OAuth scopes (`tenant:write`, `admission:decide`, `operation:control`, …) | `authorize()` on every route; `contracts/authorization/v1/scope-registry.yaml` | Coarse permission to call a route; necessary, never sufficient. | **KEEP.** Scopes stay a coarse client capability (§76). Grants add the per-principal, per-scope decision. |
| Registered Control Plane principal required for commands | `resolveActor` | Every administrative command is attributable to a canonical principal. | **KEEP.** Grants key on the same canonical principal id (§7). |

Route coverage: 53 routes run `requireAdminRole`.

- **37 are platform-only** (`requireAdminRole(nil, true)`): tenant registration, canonical registry, capability diagnostics, onboarding, admission decisions and similar.
- **14 are tenant-scoped by path** (`tenantIDFromPath`).
- **1 is tenant-scoped by query** (`/v1/entitlements`).

## What this change adds (ADA-01 to ADA-03, partial ADA-10)

- **Contracts:** the Shared `administration/v1` contracts, pinned.
- **`internal/administration`:** the permission and profile catalogue, scope coverage, deny-by-default evaluation with no union across scope (§111), delegation validation (§43–48), and the effective authority read model.
- **Migration 000067:** `policy.administrative_grant`, with self-grants, standing bootstrap grants and incomplete revocations refused in the database too.
- **`GET /v1/admin/effective-authority`:** the caller's own usable grants, derived from grants only (G5).
- **`cmd/admin-bootstrap`:** the controlled initial authority procedure (§128–129). Grants are platform-scoped and TIME_BOUND for at most 30 days, the operator is audited, and CRITICAL or EMERGENCY permissions are never granted.

## Shadow evaluation (§144)

Every role-guarded route is mapped to the permission it performs in `api/admin_shadow.go`'s `adminRoutePermissions`. `TestEveryGuardedRouteIsMapped` walks the router so no guarded route goes unmapped and no mapping goes stale.

After the legacy decision, `requireAdminRole` evaluates the caller's grants for the same permission and for the resource the route names: tenant, organisation or platform account, else platform-level. It counts the comparison in `administrative_authority_shadow_total`:

| Label | Values |
|---|---|
| `permission` | a registered permission, or `unregistered` |
| `legacy` | `allow`, `deny` |
| `grants` | `allow`, `deny`, `step_up`, `approval_required`, `not_ready`, `unresolved`, `unmapped`, `error` |
| `agreement` | `agree`, `grants_broader`, `grants_narrower`, `not_evaluated` |

All label values come from closed sets. The response is never changed, and evaluation has a 250 ms budget. `grants_broader` also logs a warning.

- **`unresolved` and `error`:** only a principal who is not found or not ACTIVE is `unresolved`. An identity-store or grant-store failure, including the budget running out, is `error`.
- **Targets named in the body or a stored record:** the resource is the route's path and query identifiers. The two platform-account binding routes also resolve their account: from the request body when binding (the body is restored for the handler), and from the active binding when ending.
- **`not_evaluated`:** when grants do not allow and the caller holds a live grant of the permission at a level the route left unresolved, the comparison is `not_evaluated`, never `grants_narrower`. An example is an organisation grant checked on a tenant route. Resolving each route's organisation, account and group ancestry would turn these into real comparisons.

**Criteria for moving enforcement to grants:**
- `grants_broader` stays at zero over a representative period;
- every `grants_narrower` is explained, as a principal who still needs a grant or a role that was broader than intended;
- the eleven canonical-mapping routes have Shared permissions (`mapping.view`, `mapping.manage`, `mapping.approve`) instead of `unregistered`.

**The decision to flip enforcement, and to retire the realm roles, is the user's.** It is not made on a green build.

Two mappings are approximations to revisit once the vocabulary grows:
- tenant onboarding and provisioning approval map to `changeset.approve`, ahead of ADR-BCP-021 changesets;
- canonical entities map to `organisation.*`.

## Not yet (next gates)

- **Enforcement on grants.** See the criteria above.
- **Operations routes.** They authorise inside `operationHandler` with the platform-admin role, not through `requireAdminRole`, so they are not yet shadowed.
- **Grant administration routes** (`administrator.grant`, `.revoke`, `.delegate`), SoD and approval (ADA-05, ADA-06), JIT (ADA-07), support and break-glass (ADA-08, ADA-09).
- **An expiry sweeper.** Evaluation already treats an elapsed window as expired, so it is housekeeping, not a security gap.
- **IAM issuance of `authority:self`.** Keycloak configuration stays untouched pending its ADRs; until the scope is issued, the route answers 403.
