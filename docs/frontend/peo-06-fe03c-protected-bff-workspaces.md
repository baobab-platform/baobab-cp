# Priority 1 / PEO-06 — FE-03C, FE-06/07/09 protected Console integration

**Authority:** Accepted ADR-BCP-026 PEO-06, ADR-BCP-027 LA-03–07, ADR-BCP-019 FE-03/06/07/09 and provider-neutral ADR-IAM-0033. Builds on FE-03A PR #347 and FE-03B PR #348.

## Implemented in this increment

- Strict, **explicitly enabled nonproduction** auth configuration for an approved OIDC confidential client, exact callback origin, authenticated HTTPS introspection, 32-byte AES-GCM key and TLS Redis username/password.
- Dedicated Node runtime `/api/auth/login`, `/api/auth/callback`, `/api/auth/session` and `/api/auth/logout`. Login stores OIDC state server-side, redirects to trusted issuer, burns one-time state through atomic Redis GETDEL, validates the authorization code, PKCE, ID-token signature, issuer, audience, nonce and current access-token introspection **before** a session is issued. Browser cookies are `__Host-`, `Secure`, `HttpOnly`, path /, SameSite Lax only on the OIDC attempt and Strict for the application session. Session tokens are never sent to browser Javascript. Logout requires exact origin and a server-sourced CSRF token.
- An origin-checked, CSRF-protected Next.js `/api/workspace/v2` API invokes **only a fixed allowlist** of Control Plane v2 applicant and independent-review/authorisation commands and current `/v1/admin/operations/{id}`. Every invocation re-introspects the authenticated CP-audience token; the BFF does not relay any browser-provided bearer, tenant selector, issuer, URL, role or legal actor. Only actor-owned CP resources pass CP authorisation. Structured errors are sanitised and requests/responses are size bounded.
- Accessible server-rendered `/workspace` entry with separate applicant, reviewer and operations screens. Applicants can draft/edit/read/submit. The reviewer queue is `admission:review`-guarded in CP; different people/permissions are required for review, decision, onboarding request and authorisation. Registration uses the existing strictly guarded Organisation-first progressive v2 route. The operation view is read-only. Neither the UI nor its BFF can set legal verification or final activation.

## Exact backend authority and limits

`POST /v2/client-applications`, `GET/PATCH /v2/client-applications/{id}`, `POST /v2/client-applications/{id}/submit` are owned by the authenticated applicant. Privileged staff calls remain separate: `GET /v2/admin/client-applications`, `POST /v2/admission/applications/{id}/review`, `POST /v2/admission/reviews/{id}/decision`, `POST /v2/admission/decisions/{id}/onboarding-requests`, `POST /v2/admission/onboarding-requests/{id}/authorisation`, `POST /v2/admission/onboarding-requests/{id}/register-tenant`. Operation inspection is `GET /v1/admin/operations/{id}`. No generic v1 fallback.

These are *operationally gated*. CP still requires opt-in `ProgressiveApplicationsEnabled`, `ProgressiveBridgeEnabled`, `OrganisationFirstV2` and actual user/admin registration, scopes and source data. A privileged step cannot be performed by a self-appointed reviewer or by an applicant merely calling the endpoint.

## Required environment (not committed)

| Input | Purpose |
| --- | --- |
| `CONSOLE_ENVIRONMENT=staging` and `CP_CONSOLE_AUTH_ENABLED=true` | Deliberate nonproduction access; production hard-disabled |
| `CP_API_BASE_URL` | Approved HTTPS CP origin |
| `CP_OIDC_ISSUER`, `CP_OIDC_CLIENT_ID`, `CP_OIDC_CLIENT_SECRET`, `CP_OIDC_REDIRECT_URI` | Registered provider-neutral OIDC confidential client and exact HTTPS callback `/api/auth/callback` |
| `CP_OIDC_INTROSPECTION_URL` | Approved HTTPS token introspection returning `active`, exact issuer/subject and CP audience |
| `CP_REDIS_TLS_HOST`, `CP_REDIS_TLS_PORT`, `CP_REDIS_USERNAME`, `CP_REDIS_PASSWORD` | Durable authenticated Redis 6.2+ TLS store (not public Internet, configured ACL for Console-prefixed keys) |
| `CP_CONSOLE_SESSION_KEY_B64` | Independently generated 32-byte encryption key from a real protected secret provider; never `NEXT_PUBLIC_` |

**Security rollout:** secure cookie handling requires a public HTTPS console origin, trusted reverse-proxy host validation, TLS Redis certificate verification and short-lived tokens. There are no known deployment credentials or live signing keys in source control. A provider missing the required introspection fields must be rejected, not reclassified as active.

## Tests and remaining gates

Unit/security tests cover strict runtime configuration, no production enablement, CSRF/origin rejection, active/revoked token introspection, BFF path/action allowlist, forbidden client-selected fields, applicant isolation boundaries and bounded request bodies. CP backend tests separately cover maker/checker and governed tenant registration. Both test suites are synthetic. Live browser end-to-end in two replicas with real IAM and PostgreSQL is **not yet accepted**. A production toggle, enterprise UX accessibility acceptance, sign-out propagation to upstream SSO and real staged provider certification require separate deployment review and PRs; **PEO-06/Priority 1 must not be declared operationally complete** until then.

## Acceptance procedure

1. Provision approved protected AWS staging IAM client, introspection account, Redis ACL, encryption secrets and CP feature gates.
2. Exercise a new applicant (unincorporated) and a distinct reviewer/decider/requester/authoriser across two BFF replicas. Validate submit, stage transitions and a read-only operation result.
3. Try sibling application IDs, applicant self-approval, expired/revoked JWT, changed audience, tampered state, callback replay, CSRF, stale session, forged organisation/legal actor, non-approved subscription, stale plan/health and denied registration. All must fail closed.
4. Verify same-origin, cookie containment and token/PII-free audit logs, responsive keyboard/browser accessibility and restore/rollback. Do not promote CP or provider readiness based on UI success.

No real tenant, company registration, seller-of-record mandate or PSP integration is established by this increment.
