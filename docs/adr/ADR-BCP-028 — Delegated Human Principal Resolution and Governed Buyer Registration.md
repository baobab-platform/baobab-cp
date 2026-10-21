# ADR-BCP-028 — Delegated Human Principal Resolution and Governed Buyer Registration

**Status:** Accepted — Normative Platform Architecture (2026-10-21)  
**Decision approval:** Accepted at the platform decision owner's direction on 2026-10-21 (ZuriBeans ZB-G03 step 3). Issuer trust, scope allocation, connected acceptance and production activation each remain separately required.  
**Date:** 2026-10-21  
**Decision owners:** Baobab Platform Architecture / Control Plane / Identity / Commerce / Security  
**Primary repository:** `baobab-platform/baobab-cp`  
**Canonical contract authority:** `baobab-platform/shared` — `identity/v1/principal-resolution.openapi.yaml`, `principal-resolution.schema.json`, `buyer-identity-handoff.schema.json`  
**Identity authority:** `baobab-platform/baobab-iam` (Ory Kratos authenticates, Ory Hydra issues tokens; ADR-IAM-0033)  
**Purchasing authority:** `baobab-platform/baobab-trade`  
**Refines:** ADR-0004 §11–13 (resolution and provisioning policy), ADR-BCP-004, IAM ADR-0010 §0  
**Retains:** ADR-0004 §7/§54 (one Principal per issuer and subject), ADR-BCP-009, ADR-BCP-020, ADR-IAM-0023 §43–45  
**Implementation status:** Increment 1 (read-only resolution) is implemented behind a default-off, nonproduction-only switch. Increment 2 (governed buyer registration) is pending.

---

## 1. Context

A ZuriBeans buyer signs in through the confidential BFF (ZuriBeans ADR 0015). Trade receives an IAM access token for audience `baobab-trade` with scope `trade:buy`, and must then know which canonical Principal that person is before it can check a buyer membership.

Until now Trade had no sanctioned way to learn this:

- `/v1/platform-context/resolve` is workload-only. It resolves the caller's own Principal, not a person the caller acts for.
- `/v1/platform-context/validate` checks a subject token, but only to confirm ownership of a stored workload context.
- Trade's onboarding read a Principal identifier from Medusa auth metadata. That makes the commerce engine an identity source, which IAM ADR-0010 §0 rules out.

## 2. Decision

### 2.1 Two credentials, never merged

Trade calls the Control Plane with **its own** `baobab-control-plane` workload token. That token authenticates the caller and nothing else.

The buyer's IAM access token is carried in the request body as **subject evidence**. It is never accepted as the Control Plane caller credential. A buyer remains a human Principal and Trade remains a workload Principal. Trade acts for the buyer, but CP verifies each identity independently.

### 2.2 `identity.principal.resolve@1` — read-only resolution

`POST /internal/identity/v1/principals/resolve` with scope `identity:principal:resolve`. CP runs these checks in this order, and any failure denies:

1. **Caller.** The caller is an ACTIVE registered workload whose registry entry currently allows the scope, and it is on the operator's explicit caller allowlist.
2. **Request shape.** The body is the closed `PrincipalResolveRequest`. It carries the subject token, the RFC 8693 access-token type and a correlation ID that must equal `X-Correlation-ID`. No issuer, subject, Principal, email or commerce-customer identifier can be asserted.
3. **Local verification.** CP verifies the subject token against the configured buyer issuer's own discovery and keys, never a caller-supplied URL or key. The token's audience must be `baobab-trade`. CP then requires:
   - `actor_type` `human`;
   - scope `trade:buy`;
   - an authorised party (`azp`) on the allowlist of confidential BFF clients;
   - an unexpired token.
4. **Current status.** CP introspects the token (RFC 7662) at the issuer's own origin, before reading any identity. Inactive, or any disagreement with the verified token (subject, client, issuer, audience, scope, expiry), denies. An introspection outage is `RESOLUTION_UNAVAILABLE` and never a rejection or an allowance.
5. **Lookup.** CP resolves the exact verified (issuer, subject):
   - unknown is `IDENTITY_NOT_RESOLVED`, and nothing is created;
   - a non-human or non-ACTIVE Principal, or a non-ACTIVE ExternalIdentity, is `PRINCIPAL_NOT_ACTIVE`.
6. **Response.** `PrincipalResolution` carries the `principal_id`, the `external_identity_id` and `evidence_expires_at`. The expiry is no later than the token's own exp or the introspected exp.

Resolution grants **no** tenant, Organisation membership, role, market or purchasing authority. Trade must still bind the canonical buyer Organisation and check an active buyer membership on every operation, and it should re-resolve for each protected operation rather than cache past `evidence_expires_at`.

### 2.3 `identity.principal.register.buyer@1` — governed just-in-time registration

`POST /internal/identity/v1/principals/register-buyer` uses the separate scope `identity:principal:register-buyer`. A first-time buyer can therefore be recognised without opening universal human auto-provisioning (ADR-0004 §13).

- **Same evidence.** The checks in §2.2 steps 1–4 apply unchanged.
- **Eligibility.** A `BuyerPrincipalProvisioningPolicy` additionally requires the issuer and the authorised-party client to be registered for buyer registration. Otherwise the request is `REGISTRATION_NOT_ELIGIBLE`.
- **Atomic creation.** CP creates the human Principal and its ACTIVE ExternalIdentity in **one transaction**, keyed by the UNIQUE (issuer, subject) constraint.
- **Idempotent.** A repeat or concurrent request returns the same Principal (`RESOLVED`). No orphan Principal is left behind. This tightens ADR-0004 §56, which tolerated an unlinked Principal.
- **No revival.** An existing SUSPENDED, DISABLED or ARCHIVED Principal, or an UNLINKED, DISABLED or REVOKED ExternalIdentity, is `PRINCIPAL_NOT_ACTIVE` and is never re-created.
- **Identity only.** Registration creates no Organisation, membership, tenant, market, role, purchasing authority or commerce-customer mapping.

The existing `ApplicantProvisioningPolicy` (ADR-BCP-017) is not reused. Applicant registration does not authorise buyer registration.

### 2.4 Buyer outcomes

| Outcome | Next step |
| --- | --- |
| Resolved, with an active Trade buyer membership | Trade evaluates the requested operation under its own current authority checks. |
| Resolved or registered, with no membership | The estate offers company registration, invitation acceptance or an access request. Purchasing stays disabled. |
| Denied or unavailable | A controlled access-denied or unavailable response. Trade never falls back to a Medusa customer or any other identity. |

### 2.5 Deployment boundary

- **Default off.** The route is mounted only when `BUYER_PRINCIPAL_RESOLUTION_ENABLED=true` in a nonproduction environment.
- **Complete configuration required.** It also needs the buyer issuer (`BUYER_SUBJECT_ISSUER`), its same-origin introspection client (`BUYER_SUBJECT_INTROSPECTION_*`), the caller allowlist (`BUYER_PRINCIPAL_RESOLUTION_CLIENT_IDS`) and the BFF client allowlist (`BUYER_SUBJECT_AUTHORISED_PARTIES`). An incomplete configuration refuses to start.
- **No live allocation.** The Shared scopes are allocated to no workload. Allocation is a reviewed Shared registry change made together with connected acceptance.
- **No production profile** exists until protected acceptance passes.

### 2.6 Handling of the subject token

The subject token is a bearer credential. It is read from the body only, never persisted, logged, traced or echoed, and is sent only to the configured issuer's introspection endpoint. Validation errors never quote the request body. Audit records carry the caller, the resolved Principal, the decision, the reason and the correlation ID.

## 3. Consequences

- **Trade.** Trade stops reading `app_metadata.baobab_principal_id` and keys buyer membership on the CP-resolved Principal and the CP-verified canonical Organisation.
- **Increment 2.** It adds the atomic registration repository operation and the eligibility registry.
- **Live use is blocked on IAM.** It needs an introspection client for the Control Plane and the confidential ZuriBeans BFF client, both registered in IAM. It also needs a deployed Hydra login/consent provider; none exists yet.
- **Not acceptance evidence.** Synthetic tests prove the boundary only. They are not connected IAM, CP or Trade qualification, and not EA Gate D, L4 or go-live readiness.

## 4. Alternatives considered

- **The caller asserts (issuer, subject).** Rejected. A workload's word would become enough to name a person.
- **CP accepts the Trade-audience token as its own caller credential.** Rejected. It violates audience binding (ADR-IAM-0023 §43).
- **RFC 8693 token exchange to a CP-audience token.** Deferred, not rejected. It needs separate IAM adoption (IAM ADR-0010 §0.2). The contract's `subject_token_type` leaves room for it.
- **Universal human JIT provisioning on resolve.** Rejected in favour of the separately scoped, policy-governed registration in §2.3.
