# ORG-11 certification: classification to billing projection

**Gate:** ADR-BCP-018 ORG-11. The Control Plane classifies each ProductSubscription and projects it into billing. The flow is Control Plane → baobab-subscriptions → baobab-payments.

**Contracts:** Shared `product/v1`, `subscriptions/v1`, `payments/v1` and `capability/v1`, pinned by `contracts.lock.yaml`.

**Status:** Certified for development and integration.

**Production is deliberately not certified.** Both engines run simulated providers only:
- baobab-subscriptions runs the temporary provider;
- baobab-payments runs the sandbox provider.

Both providers are refused in production: by the engines at startup, and by Control Plane registration.

## Authority boundaries

| Concern | Owner | Evidence |
|---|---|---|
| ProductSubscription, its classification, INTERNAL eligibility, and entitlement | Control Plane | `product.subscription_classification` (immutable); `internal/service/subscription` |
| Billing projection, billing lifecycle, and readiness | baobab-subscriptions | Its own database. It is reached only through the Baobab Billing API. |
| Payment execution | baobab-payments | Its own database. Only billing whose policy is `payment_execution: REQUIRED` may consult it. |

The Control Plane stores only which revision each projection reflects (`product.billing_projection_sync`). It holds no billing amounts, provider data or payment data. Neither engine reads or writes the Control Plane database.

## Acceptance criteria

| # | Criterion | Evidence | Result |
|---|---|---|---|
| 1 | The ZuriBeans chain (group affiliate of the platform owner) is classified INTERNAL from its AdmissionDecision, with eligibility evidence. | `TestInternalClassificationAcceptance` | Pass |
| 2 | The INTERNAL projection has zero charge, `billing_required=false`, metering on, `payment_execution=NEVER`, and is ACTIVE/READY with no payment path. | `TestInternalBillingProjection`; engine `BillingScenarios` | Pass (fake engine and real engine) |
| 3 | Payments are never called for INTERNAL. | The fake payments endpoint receives 0 calls. The engine's INTERNAL path never consults `PaymentsPort` (engine tests). The Control Plane has no payments client. | Pass |
| 4 | Acme (EXTERNAL_CLIENT) is refused INTERNAL at admission and at reclassification, and is classified COMMERCIAL. | `TestExternalClientIsNeverInternal` | Pass |
| 5 | COMMERCIAL on a simulated provider is BLOCKED, never ACTIVE. Readiness is never faked. | `TestCommercialBillingProjectionIsBlocked`; the Shared `BillingProjection` rule; engine tests | Pass (fake engine and real engine) |
| 6 | Divestiture is reported as drift (`INTERNAL_CLASSIFICATION_BASIS_NOT_IN_FORCE`) and never changed silently. A governed reclassification to COMMERCIAL keeps the tenant, organisation and subscription. | `TestDivestitureDriftAndReclassification` | Pass |
| 7 | The reclassification re-projects the same billing projection at the next revision. The old revision cannot regress it (`409 STALE_AUTHORITATIVE_REVISION`). | `TestDivestitureReprojectsAndStaleRevisionIsRefused` | Pass (fake engine and real engine) |
| 8 | Failures close safely: an unavailable engine backs off and retries, and an answer outside the contract (such as INTERNAL with a charge) is never recorded. | `TestBillingProjectionFailsClosed`; `internal/billing` unit tests | Pass |
| 9 | Applicant and staff authentication. <br>• The same issuer is used for both. <br>• Applicant-only scopes cannot reach staff routes. <br>• A wrong audience or issuer is refused. <br>• An unregistered principal gets `PRINCIPAL_NOT_REGISTERED`. <br>• A reviewer cannot decide. | `applicant_staff_realm_test.go`, `subscription_classification_handler_test.go` | Pass |
| 10 | ASSISTED_ENTERPRISE and INTERNAL_GROUP channels do not regress. | The existing client application suite | Pass |
| 11 | Engines register through the normal capability registry, from their Shared `EngineRegistration`, with no special-casing by name. Providers not permitted in production are refused there. | `TestEnginesRegisterFromSharedRegistrations` | Pass |
| 12 | Workload identity is used, not static secrets. | The Control Plane reads a platform-projected token per call (`BILLING_WORKLOAD_TOKEN_FILE`). The engines verify issuer, audience, azp, scope and `actor_type=workload` against JWKS. | Pass |
| 13 | Tenant isolation and idempotency hold. | Every command names its tenant. Idempotency keys are per subscription, revision and action, and are tenant-scoped in the engine. | Pass |

## Real cross-repository run

On 2026-09-25 the acceptance tests ran against a real baobab-subscriptions:
- **Engine:** `claude/sub-03-lifecycle-alignment` at `1497fa2`, backed by PostgreSQL 17, in `BAOBAB_ENVIRONMENT=development`.
- **Authentication:** an ES256 workload token issued by a local JWKS.
- **Command:** `BILLING_E2E_URL=… BILLING_E2E_TOKEN_FILE=… go test ./internal/service/subscription -run 'TestInternalBillingProjection|TestCommercialBillingProjectionIsBlocked|TestDivestitureReprojectsAndStaleRevisionIsRefused'`

Engine-side results:

| Engine observation | Value |
|---|---|
| `POST /v1/billing-projections` | 3 × 201 (created), 2 × 200 (converged), 1 × 409 (stale revision) |
| Projections | INTERNAL ACTIVE/READY `NEVER` rev 2; COMMERCIAL PENDING_CONFIGURATION/BLOCKED `REQUIRED` rev 2 (Acme); COMMERCIAL BLOCKED rev 3 (divested) |
| Audit records | 3 `billing_projection.created`, 1 `billing_projection.reclassified` |
| Payment calls | none |

The same tests run in Control Plane CI against a fake engine, which validates every request and response against the pinned Shared schemas.

## Not certified (tracked)

These items are recorded in `docs/runbooks/adr-bcp-018-organisation-operations.md` §10 and in baobab-subscriptions `docs/adr-alignment.md`.

**Providers**
- **Kill Bill billing provider.** Commercial billing stays BLOCKED until it exists.
- **HyperSwitch payment provider.** Payments run on the sandbox only.

**Engine integration**
- **The subscriptions → payments client.** `PaymentsPort` reports that no payment path is configured.
- **Engine outbox relays**, and the Control Plane consuming billing lifecycle events.
- **Scheduled reconciliation between engine and provider**, as defined in ADR-SUB-0004 through ADR-SUB-0018.

**Control Plane routes and workers**
- **Classification sources.** There are no routes for `MIGRATION` and `MANUAL_GOVERNANCE`.
- **Staff-assisted applications.** There is no route to create them.
- **Application expiry.** There is no expiry worker.

**Lifecycle**
- **Status-driven lifecycle calls.** The projector already issues suspend, resume and terminate when a ProductSubscription's status changes. The Control Plane has no route that changes the status yet, so nothing currently triggers these calls.
