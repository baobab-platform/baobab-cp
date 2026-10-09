# LA-05A — Context-owned legal actor assessment for resource-server PEPs

**Authority:** ADR-BCP-027 LA-05; Shared `c42bfebaa3a6edfe8aa3fe56ed3504af3328a73e` (Shared PR #261), on top of merged LA-04A–LA-04D.

`POST /internal/legal-actor/v1/assess` is a private, opt-in Control Plane read:
```json
{"context_id":"<previously CP-issued context_id>","role":"SELLER_OF_RECORD","activity":"B2B_COFFEE_SALE","market":"ZA","capability":"commerce.order.create","operation_reference":"order/opaque-id"}
```

The assessor is currently **off** unless `LEGAL_ACTOR_ASSESSMENT_ENABLED=true` *and* environment is `development`, `test`, `integration`, `sandbox`, or `staging`. Production and unset environment never register the route. Configure both `context:resolve` and `legal-actor:assess` in the authenticated workload's **actual** IAM token and active canonical workload scope registry. A scope registered in Shared is only vocabulary; it is not automatically issued.

## Enforcement

1. Authenticate the workload token with audience `baobab-control-plane`, workload type, `legal-actor:assess` scope, active workload registry and allowed canonical scope.
2. Reuse CP's `redeemContext` principal-owner proof: the issued runtime context belongs to the current **canonical identity** (issuer+subject), never simply to whoever knows its identifier; an unknown, expired or foreign handle is denied.
3. Require bounded, still-current `RUNTIME` context, active tenant, canonical UUID Organisation, exact context country. Reject provisioning purpose and any request that attempts tenant, Organisation, LegalEntity, mandate or time selection via schema `additionalProperties=false`.
4. Re-evaluate the LA-04B/D trusted PostgreSQL resolver on every call. No cached `ACTIVE` event or v1 DEFAULT LegalEntity fallback. The resolvers' current PRIMARY mapping, verified actor evidence, independent checker, role/activity/market/capability and revocation all still apply.
5. Respond in canonical Shared `AssessLegalActorResponse`: `legal_actor_resolution` carries current outcome and a max-30-second `valid_until`, capped at context expiry. `provider_permissions_granted` is always `false`; denied outcomes never disclose responsible actor or mandate.
6. Require every *consuming* Trade/ERP/Payments/Trade Docs PEP to check operation-scoped `AUTHORIZED`, exact capability/market, provider readiness, entitlements and legal-form requirements **separately**, and re-resolve just before irreversible activity. Legal actor assessment alone never issues invoice or settles a payment.

## Current acceptance state

This increment implements the **CP authority source and contract** only. External consumer enforcement, current IAM token issuance and revocation propagation, and production commissioning are independently gated in LA-05B–F. Do not mark those providers ACCEPTED based on passing CP tests; do not issue unrestricted `legal-actor:assess` to a generic shared client. Deny on unavailable CP and do not use a recent mandate event as an offline fallback.

## Operator examples

- CP `AUTHORIZED` seller + Trade seller readiness FAILED → Trade must deny order completion.
- CP `REVOKED_OR_EXPIRED` + prior ERP legal entity mapping → ERP must deny the new posting.
- CP `AUTHORIZED` invoice issuer + unsigned company document → Trade Docs must deny issue.
- CP `AUTHORIZED` payment beneficiary + HyperSwitch merchant unverified → Payments must deny payout.

No Nabhold, ZuriBeans, Equator & Estate or Thamani real authorisation record is created.
