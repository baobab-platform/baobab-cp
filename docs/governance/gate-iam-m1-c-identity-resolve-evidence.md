# Gate IAM-M1-C — CP identity resolve evidence

**Status:** Evidence for Phase A / Gate IAM-M1  
**Date:** 2026-09-30  
**Repo:** `baobab-platform/baobab-cp`  
**Related:** ADR-IAM-0019, ADR-IAM-0020, ADR-0004; `shared` PR #147 (M1-B); `baobab-iam` PR #42 (M1-A)

---

## 1. Criteria

| # | Criterion | Result |
|---|-----------|--------|
| 1 | Resolve Principal by `(issuer, subject)` only | **Pass** — `IdentityService.Resolve` and `ResolveIdentity` (in-memory + Postgres) join/lookup on issuer+subject and ACTIVE status; `provider_type` is never in the WHERE clause |
| 2 | No authorization branch on `provider_type == "keycloak"` for identity resolution | **Pass** — no such branch in identity resolve/provision path |
| 3 | At least one non-Keycloak fixture | **Pass** — Ory issuer/subject tests + `provider_type: "ory"` dual-run shape in repository tests; domain schema compatibility for ory |

---

## 2. Code map

| Component | Path | Behaviour |
|-----------|------|-----------|
| Domain | `internal/domain/identity.go` | `ExternalIdentity` requires issuer+subject; `ProviderType` optional |
| Service | `internal/service/identity_service.go` | `Resolve(ctx, issuer, subject, actorType)` — no provider_type parameter |
| In-memory repo | `internal/repository/repository.go` | Key = issuer+subject |
| Postgres | `internal/repository/postgres.go` | `WHERE e.issuer = $1 AND e.subject = $2 AND e.status = 'ACTIVE'` |

---

## 3. Explicit non-claims

- **IamOrganisation** (`organisation/v1` `iamProvider` enum `keycloak` only) is **out of scope** for M1-C Principal resolve. Keycloak Organizations are classified **REPLACE** under migration (M7 path); dual-run of organisation claims is not enabled here.
- No dual-issuer production IssuerTrust configuration in this change.
- No production traffic cutover.

---

## 4. Tests added

- `TestIdentityServiceResolveOryIssuerSubject`
- `TestIdentityServiceProvisionDoesNotRequireProviderType`
- `TestInMemoryResolveIdentityOryProviderType` (ory + dual keycloak binding same principal)
- `TestExternalIdentityOryMatchesSharedSchema`

---

## 5. Document control

| Version | Date | Change |
|---------|------|--------|
| 0.1 | 2026-09-30 | Initial M1-C evidence |
