# PEO-03B — Independent admission decision → authorised onboarding (v2)

**Scope:** stacked on PEO-02B (CP #312), migration 000112 following its 000111. **Nonproduction only.** This is a real database and authenticated HTTP implementation, not v1 adaptation or a production cutover.

## Four independent human commands

| Action | Scope | Durable result |
| --- | --- | --- |
| POST `/v2/admission/applications/{applicationID}/review` | `admission:review` | Immutable scoped review of a SUBMITTED v2 applicant claim and independent pre-tenant Organisation |
| POST `/v2/admission/reviews/{reviewID}/decision` | `admission:decide` | Immutable APPROVED or REJECTED v2 decision; no tenant/entitlement |
| POST `/v2/admission/decisions/{decisionID}/onboarding-requests` | `onboarding:request` | Organisation-bound, Shared-contract-validated v2 REQUESTED record |
| POST `/v2/admission/onboarding-requests/{requestID}/authorisation` | `onboarding:authorise` | Independently AUTHORISED record; **tenant still not created** |

Each requires an authenticated registered human CP platform administrator, scope, 16–128-character `Idempotency-Key`, evidence/policy references and an exact, durable command digest. The applicant, reviewer, decider, requester and authoriser are distinct principals. Reviewer-selected markets are constrained by the applicant's submitted market interests; the requester cannot change classification, markets, product requirements or isolation strategy approved in the decision. Every command commits its audit evidence and replay receipt in the same transaction.

The v2 review and decision tables are append-only. Onboarding is `REQUESTED → AUTHORISED` only, with immutable desired state; there is no provisional `FULFILLED` shortcut.

## INTERNAL eligibility

Neither `subscription_type: INTERNAL` nor a reported first-party relationship is authority. On REVIEW, APPROVE, REQUEST and AUTHORISE, CP evaluates the **current** ACTIVE, in-effect founding sponsorship and sponsor's independently verified LegalEntityProfile. If any is absent, expired, suspended or revoked, the operation denies. No automatic `INTERNAL` entitlement is granted.

## Controlled staging activation

Only if both `PEO_PROGRESSIVE_ADMISSION_ENABLED=true` and `PEO_PROGRESSIVE_BRIDGE_ENABLED=true` are set and `BAOBAB_ENVIRONMENT=staging` (or an explicitly allow-listed nonproduction environment). Unknown, unset and production environments are denied, even if both flags are set.

## Critical remaining boundary

The existing `/v2/tenants` LA-03B registration path still consumes **v1** authorised onboarding requests. This PR does *not* bridge v2 rows into v1 records or allow a v2 AUTHORISED record to register a tenant. A separate reviewed v2 registration adapter is required, including comprehensive identity, SoD, event, outbox and provider tests. This increment closes the **decision-to-authorised-request** portion of PEO-03, not LA-06/07 or founding-group admission.

No real Nabhold/ZuriBeans approval, verification, mandate, ERP posting, subscription, tenant or production permission is created. Equator `property-intelligence` remains `confirmed: false`.
