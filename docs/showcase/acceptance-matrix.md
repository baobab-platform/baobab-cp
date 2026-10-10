# NBO Acceptance Matrix — implementation evidence

**As of 2026-10-09; Shared #255 and CP #289 have since merged (2026-10-09).** Outcomes below indicate only evidence actually produced. Test names refer to code under [CP #289](https://github.com/baobab-platform/baobab-cp/pull/289), not a completed live showcase run.

| ID | Scenario | Implementation evidence | Current outcome |
|---|---|---|---|
| NBO-T01 | Incomplete evidence request | staff DRAFT route and applicant draft tests | PARTIAL — DRAFT only; submission requires registration identifier |
| NBO-T02 | Unverified registration number stays unverified | applicant JSON schema forbids `verified` on registration identifier | CODE_TEST; external verification unexecuted |
| NBO-T03 | ZuriBeans and Equator & Estate not incorporated | fixture claim `NOT_INCORPORATED`, omitted ID and jurisdiction | FIXTURE_TEST |
| NBO-T04 | Claimed parent ownership grants no verified control | fixture creates no CorporateRelationship | FIXTURE_TEST; live cross-tenant not run |
| NBO-T05 | Ineligible INTERNAL request refused | existing `TestInternalClassificationIsServerAuthoritative` | EXISTING_CODE_TEST; no Nabhold authoritative classification |
| NBO-T06 | Eligible INTERNAL zero charge with metering | existing billing subsystem | BLOCKED_INTEGRATION |
| NBO-T07 | Applicant approves own request | existing application Decide + NBO staff test | CODE_TEST |
| NBO-T08 | Onboarding requester authorises self | existing onboarding SoD tests | EXISTING_CODE_TEST; live IAM not run |
| NBO-T09 | Showcase exception expires | no runtime exception in NBO-01 | UNEXECUTED |
| NBO-T10 | Production bypass refused | no override in fixture or staff route | PARTIAL — production activation not tested |
| NBO-T11 | Subsidiary admin reads sibling | no live subsidiary tenants | BLOCKED_INTEGRATION |
| NBO-T12 | Repeat founding intake | stable idempotency keys and integration replay test | CODE_TEST; live multi-tenant execution unexecuted |
| NBO-T13 | Missing provider | existing CP readiness machinery | BLOCKED_INTEGRATION |
| NBO-T14 | Stale plan refused | existing provisioning tests | EXISTING_CODE_TEST; live provider test outstanding |
| NBO-T15 | Identity access revoked | IAM runtime not linked in showcase | BLOCKED_INTEGRATION |
| NBO-T16 | Synthetic settlement isolation | no financial scenario executed | UNEXECUTED |
| NBO-T17 | Corporate exit triggers eligibility reassessment | existing classification/reconciliation tests | EXISTING_CODE_TEST; Nabhold scenario unexecuted |
| NBO-T18 | UG expansion without legal evidence | market interest in fixture; no rights granted | FIXTURE_TEST only |
| NBO-T19 | Visitor invokes admin command | no invited visitor role provisioned | BLOCKED_INTEGRATION |
| NBO-T20 | Showcase teardown | AWS showcase not deployed | BLOCKED_EXTERNAL_DEPENDENCY |

### CI proof boundaries

- PostgreSQL-backed tests in `internal/service/application/staff_test.go` use `TEST_DATABASE_URL`. Their passing in Go CI must be confirmed from actual run, not inferred from existence.
- HTTP denial tests in `api/client_application_handler_test.go` establish scope/role rejection.
- `scripts/showcase/test_nbo_apply.py` and default dry-run are executed in CP Go CI.
- Shared NBO schema validator `scripts/validate-admission-contracts.py` and OpenAPI validator are green on Shared commit `90b4dd7033471b2fa1189c7a98ce74b40c5a3a16`.
- No deployment, external settlement, production IAM or cross-engine certification evidence exists in this increment.
