# Client application API (ADR-BCP-017)

An organisation applies to use Baobab before any tenant exists. These routes cover gates OA-01, OA-03 and OA-04: taking an application from draft to an explicit AdmissionDecision. The bodies and responses are the Shared contracts in `contracts/admission/v1`, which the Control Plane validates at runtime against the embedded, pinned schemas (`internal/contracts`).

Approval activates nothing (§22). The approved decision's `admission_decision_id` keys organisation admission: `POST /v1/tenants/{tenantID}/organisation-admission`.

## Authorization

Every route takes a human bearer token from the Baobab IAM human issuer (`ADMIN_OIDC_ISSUER`).

| Scope | Who | Additional requirement |
|---|---|---|
| `application:read` / `application:write` | the applicant | none. The first request registers the caller's Control Plane principal (ADR-BCP-017 §5) and nothing else: no tenant, membership, role or capability. An applicant reaches only their own applications; any other application is reported as not found. |
| `admission:review` | an admission reviewer | `cp:platform-admin` and a registered Control Plane principal |
| `admission:decide` | an admission approver | `cp:platform-admin`, a registered principal, and never the application's applicant (ADR-BCP-020 §39) |

## Applicant routes

| Route | Scope | Body | Effect |
|---|---|---|---|
| `POST /v1/client-applications` | `application:write` | `ClientApplicationDraft` without `version` | Creates a SELF_SERVICE DRAFT. An optional `Idempotency-Key` (16-128 characters) replays the original with 200; the same key with another body returns 409. |
| `GET /v1/client-applications[?limit=&before=]` | `application:read` | none | Lists the caller's applications, newest first |
| `GET /v1/client-applications/{id}` | `application:read` | none | Returns the caller's application |
| `PATCH /v1/client-applications/{id}` | `application:write` | `ClientApplicationDraft` with `version` | Replaces each supplied section. Allowed only in DRAFT or INFORMATION_REQUIRED. |
| `POST /v1/client-applications/{id}/submit` | `application:write` | none | DRAFT → SUBMITTED. The whole application must be complete (422 `APPLICATION_INCOMPLETE` lists what is missing). |
| `POST /v1/client-applications/{id}/response` | `application:write` | `ApplicantResponseCommand` | Answers the open information request: INFORMATION_REQUIRED → VALIDATING |
| `POST /v1/client-applications/{id}/withdraw` | `application:write` | `ClosureCommand` | Any open state → WITHDRAWN |

## Platform routes

| Route | Scope | Body | Effect |
|---|---|---|---|
| `GET /v1/admission/applications[?status=A,B&limit=&before=]` | `admission:review` | none | The review queue. Defaults to open statuses. |
| `GET /v1/admission/applications/{id}` | `admission:review` | none | The platform view, including `assigned_reviewer` |
| `GET /v1/admission/applications/{id}/decision` | `admission:review` | none | The AdmissionDecision |
| `POST /v1/admission/applications/{id}/begin-validation` | `admission:review` | none | SUBMITTED → VALIDATING; assigns the reviewer |
| `POST /v1/admission/applications/{id}/information-request` | `admission:review` | `InformationRequestCommand` | VALIDATING or UNDER_REVIEW → INFORMATION_REQUIRED |
| `POST /v1/admission/applications/{id}/begin-review` | `admission:review` | none | VALIDATING → UNDER_REVIEW |
| `POST /v1/admission/applications/{id}/cancel` | `admission:review` | `ClosureCommand` | Any open, submitted state → CANCELLED |
| `POST /v1/admission/applications/{id}/decision` | `admission:decide` | `AdmissionDecisionRequest` | UNDER_REVIEW → APPROVED or REJECTED; returns the AdmissionDecision (201) |

A decision is refused in these cases:

- the decider is the applicant (403 `SELF_DECISION_FORBIDDEN`);
- the approved market scope includes a market the application did not request (422 `MARKET_SCOPE_EXCEEDED`);
- the decision is INTERNAL and the named organisation is not INTERNAL-eligible now (422 `NOT_INTERNAL_ELIGIBLE`).

For INTERNAL, the Control Plane evaluates eligibility from verified platform and corporate relationships and records the qualifying relationships in `internal_eligibility`. A request can never supply that evidence.

## Errors

| Status | Code | Meaning |
|---|---|---|
| 400 | `VALIDATION_FAILED` | The body does not satisfy the contract (the detail lists JSON pointers) |
| 404 | `CLIENT_APPLICATION_NOT_FOUND` | No such application, or not the caller's |
| 409 | `INVALID_TRANSITION` | The command is not permitted from the current status (`lifecycle.yaml`) |
| 409 | `VERSION_CONFLICT` | The application changed since the caller read it |
| 409 | `IDEMPOTENCY_KEY_REUSED` | The Idempotency-Key was used for a different request |
| 422 | `APPLICATION_INCOMPLETE` | The resulting application would not satisfy the contract |
| 403 | `PRINCIPAL_NOT_REGISTERED` | Platform staff without a Control Plane principal |

## Audit and events

Every change writes an audit record to `audit_events` (target `client-application/<id>`, actor = the Control Plane principal) in the same transaction.

Creation, submission, information requests, withdrawal, approval and rejection also publish the §40 events (`com.baobab-platform.control-plane.client-application.*.v1`) through the outbox, ordered by the application's version. Validation, review and cancellation are audited only.

AdmissionDecisions are immutable: a database trigger refuses UPDATE, DELETE and TRUNCATE.
