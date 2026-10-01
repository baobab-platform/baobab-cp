# Moving a permission from roles to AdministrativeGrants

ADR-BCP-020 sections 143-144; roles-to-grants decision (Shared `docs/architecture/roles-to-grants-decision.md`), rulings 1 and 3-5.

**State today: no permission is enforced.** Realm roles decide every administrative request; grants are compared with them in shadow. Nothing in this runbook happens on its own, and a green build is not the decision.

## What the Control Plane does

| Mechanism | Behaviour |
|---|---|
| Shadow comparison | Every role-guarded request, and the durable-operation routes, compare the role decision with the grant decision. Kept per UTC day in `policy.administrative_shadow_daily` and counted in `administrative_authority_shadow_total`. |
| Readiness | `GET /v1/admin/authority-migration/readiness` (`administrator:read`, platform administrator) reports each permission against the exit criteria in Shared `enforcement-policy.yaml`. Evidence only. |
| Enforcement | A permission listed in the policy's `enforced` list is decided by the caller's grants alone. A role no longer suffices. Counted in `administrative_authority_enforced_total`. |
| Fail closed | Under enforcement an unresolved caller, a missing grant, a missing grant store and a store failure all refuse (403, or 503 retryable for a store failure). Roles are never the fallback. |
| CRITICAL | Never enforced while the policy prohibits it, whatever the list says. |

## Preconditions the owner checks before listing a permission

1. The reviewed population (Platform Security / Control Plane Governance) is issued through the grant administration API and each person's `effective-authority` is equal to or narrower than their role.
2. Readiness for the permission shows `ready: true` under **APPROVED** criteria: no `grants_broader`, narrower / not-evaluated / unresolved-or-error within the approved bounds, enough days and decisions.
3. Waves are respected: LOW, then MODERATE, then HIGH. CRITICAL last, and only after the owner lifts the prohibition.
4. The rollback below has been rehearsed for the permission.

## Enforcing a permission (the owner's act)

A reviewed change to Shared `contracts/administration/v1/enforcement-policy.yaml`: set `criteria.status: APPROVED` (once, with the approved numbers) and add an `enforced` entry with `permission`, optional `scope` (environments, tenants), `approved_by`, `approved_at` and `evidence_ref`. Then bump this repository's Shared pin. Canary by environment or tenant first.

## Rolling back

Immediately, without a release: set `ADMINISTRATIVE_ENFORCEMENT_ROLLBACK` on the Control Plane to the permission keys (comma separated), or `*`, and restart. Authority returns to roles for those permissions. The variable can only return authority to roles; it cannot enforce anything.

Durably: remove the entry from `enforced` in Shared and bump the pin, then clear the variable.

## Reading the signals

- `administrative_authority_shadow_total{agreement="grants_broader"}` must stay zero. It is logged at WARN with the route.
- `administrative_authority_enforced_total{result="unavailable"}` rising means the identity or grant store is failing under enforcement; roll the permission back while it is fixed.
- A permission whose readiness shows `NOT_EVALUATED` has routes whose target is named only indirectly; resolve their ancestry before enforcing it.
