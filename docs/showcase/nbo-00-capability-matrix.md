# NBO-00 — Capability Matrix

**As observed 2026-10-09.** These statuses report source-code/contract evidence, **not** deployment health.
Use exactly: `IMPLEMENTED_AND_VERIFIED`, `IMPLEMENTED_NOT_INTEGRATED`, `PARTIALLY_IMPLEMENTED`, `CONTRACT_ONLY`, `NOT_IMPLEMENTED`, `BLOCKED_EXTERNAL_DEPENDENCY`.

| Capability | Assessment | Evidence | Remaining proof |
|---|---|---|---|
| Applicant self-service, immutable decision lifecycle | IMPLEMENTED_AND_VERIFIED | CP `internal/service/application/service_test.go`, `api/client_application_handler_test.go`, migrations 000050 | Live IAM/DB staging smoke still outstanding |
| Staff-created INTERNAL_GROUP / assisted admission | PARTIALLY_IMPLEMENTED | Shared PR #255, CP PR #289; schema, API, maker tracking, migration 000101 and tests | Both PRs merged 2026-10-09; repin to current Shared main and run deployed smoke |
| Verified legal existence and source-backed claims | PARTIALLY_IMPLEMENTED | CP `api/verification_handler.go`, OEV inventory; Shared PR #252 | Independent registry verification and operational evidence chain |
| Time-bounded ComplianceException (maker/checker, revoke) | CONTRACT_ONLY | CP ADR-BCP-023 (ComplianceException); no demonstrated end-to-end runtime | Canonical boundary, durable policy, secure API, audited execution |
| Corporate registry, claim lifecycle, group derivation | IMPLEMENTED_AND_VERIFIED | CP `internal/service/organisation/`, `internal/domain/corporate_group.go`, migration 000045+ | Nabhold claims and real verification not yet onboarded |
| Founding-enterprise fixture with verified idempotent execution | PARTIALLY_IMPLEMENTED | Shared `contracts/legal-entity/registry.yaml`; CP `cmd/reconcile-first-party` | Admission-side fixture runner; no arbitrarily minted canonical IDs |
| External organisation onboarding | IMPLEMENTED_NOT_INTEGRATED | `api/tenant_onboarding_handler.go`, CP service / PostgreSQL tests | Full IAM + subscription + engine end-to-end run |
| INTERNAL eligibility / zero-charge governed metering | IMPLEMENTED_NOT_INTEGRATED | CP `internal/service/subscription`; billing projection report; Subscriptions engine | First-party verified-control evidence plus live subscription/metering proof |
| Desired-state provisioning planning + approval | IMPLEMENTED_NOT_INTEGRATED | CP `api/provisioning_convergence_handler.go`, `internal/provisioning/apply` | Stale plan/concurrency/provider certification in deployed environment |
| IAM tenant memberships and revocation | IMPLEMENTED_NOT_INTEGRATED | IAM main + open PR #106; CP identity and membership service | Full live issuer, membership, audience and revocation demonstration |
| Trade tenant-specific demo workflow | PARTIALLY_IMPLEMENTED | `baobab-trade`, `zuribeans` | Native provider certification, isolated synthetic order flow |
| ERP native company/organisation projection | PARTIALLY_IMPLEMENTED | `baobab-erp`; CP ERP provisioning adapter | Live event receipt, company tenancy, financial side-effect barrier |
| CMS / Pulse optional demonstration | PARTIALLY_IMPLEMENTED | `baobab-cms`, `baobab-pulse` | Certified runtime support, actual report/data provenance |
| CP Console corporate/tenant end-to-end journey | PARTIALLY_IMPLEMENTED | `baobab-cp/frontend`, generated contract client | Authenticated BFF, backend-backed screens, browser/accessibility proofs |
| Isolated AWS showcase stack | BLOCKED_EXTERNAL_DEPENDENCY | `infrastructure` staging definitions; region af-south-1 | Actual AWS account ID, OIDC roles, state bucket, approved secrets, tenancy + deployment evidence |
| Investor-ready, least-privilege hosted showcase | NOT_IMPLEMENTED | No verifiable invited-visitor deployment evidence | NBO-03 identity/provider vertical + NBO-04 integration |

`IMPLEMENTED_AND_VERIFIED` here refers to in-repository automated tests/inspection only; it does **not** claim production certification. NBO-T01 through T20 must remain UNEXECUTED or BLOCKED until evidenced.
