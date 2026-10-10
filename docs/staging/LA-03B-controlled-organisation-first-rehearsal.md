# LA-03B controlled Organisation-first staging rehearsal

**Decision (10 October 2026): conditional GO for a synthetic technical rehearsal.**
This document is a reproducible acceptance procedure, **not proof of a deployed staging run**. Do not run against production or use real Nabhold/ZuriBeans identities to create any tenant.

## Preparation / non-escalation

- Deploy the authorised, version-pinned CP and Shared contracts on a controlled isolated staging environment with a separate PostgreSQL database and synthetic-only test records. Enforce log redaction and data-retention policy.
- Only on the CP staging deployment, set `BAOBAB_ENVIRONMENT=staging` and `ORGANISATION_FIRST_V2_ENABLED=true`.
- Keep the independent PEO-02/03 flags **off unless their own PRs and reviewer permissions are separately accepted**: `PEO_FOUNDING_GOVERNANCE_ENABLED`, `PEO_PROGRESSIVE_ADMISSION_ENABLED`.
- Do not modify `api.OrganisationFirstV2PermittedIn` or `peoRoutesPermittedIn` to admit production/unknown environments. These allow-lists are intentionally distinct from other production-like engine registration guards.
- Use synthetic immutable application IDs, pre-tenant Organisations, signed limited test administrator/reviewer identities and fake company registration strings clearly identified as synthetic. Never create a fake CIPC verification result for a real organisation.

## Evidence matrix

| Control | Expected result |
| --- | --- |
| Routes not enabled | `POST /v2/tenants` and `POST /v2/tenant-onboarding/{requestID}/primary-organisation` absent when feature off |
| Production/unknown env with flag on | Both routes still absent; no override via feature flag |
| No bearer, invalid issuer, wrong human scope or tenant admin | Denial with no state mutation |
| Workload principal posing as human platform administrator | Denial |
| Synthetic authorised request with reviewed first-party pre-tenant Organisation | Valid binding to the single requested Organisation, idempotent replay |
| Synthetic request missing/invalid/unauthorised | No tenant or binding created |
| Synthetic legitimate unincorporated operating business | An UNVERIFIED Organisation may exist without fabrication of a legal person |
| Synthetic v2 registration without a default legal actor | Registered tenant only following actual authorised request; product subscriptions remain PENDING |
| Client-supplied tenant ID or cross-tenant primary Organisation | Denial; CP remains ID authority |
| Reuse, duplicate, changed digest or stale request | Idempotent exact replay or conflict; no duplicate tenant, subscription or outbox record |
| ZuriBeans proposed ZA legal actor without real mandate | No `NABHOLD` ERP legal attribution, invoice, seller-of-record or finance posting |
| ZuriBeans Uganda without independent decision | No inferred ZA legal actor |
| Equator property-intelligence | Still `confirmed: false`; no entitlement/provision |

## Operator record

Capture and retain: staging release SHA and environment, exact route flag values, pinned `contracts.lock.yaml`, synthetic IDs only, authenticated actor grant evidence, request/decision status, response code, correlation ID, approved request digest, database invariants, raw CI runs and reviewer sign-off. Prove no production data is written. Destroy temporary synthetic data through the approved staging-data retention process, without deleting immutable audit records contrary to policy.

A passing rehearsal may establish **LA-03B runtime acceptance in staging only**. It does **not** establish PEO-02/03 governance closure, LA-06, LA-07, independent Nabhold legal verification, mandate activation or production go-live.

### Exit decision

Mark rehearsal as **PASS** only with signed technical/operator evidence of all applicable negative and positive cases, and as **BLOCKED** for an absent staging account, identity-authority proof or controlled deployment. Never convert a BLOCKED test into PASS by weakening a reviewer, environment or identity guard.
