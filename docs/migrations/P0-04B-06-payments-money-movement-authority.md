# P0-04B-06 — Payments money-movement authority resolution

**Authority:** ADR-SHARED-035, ADR-BCP-027 (§6.1, §8.3, §10.2), ADR-PAY-0002, ADR-PAY-0016. The Shared `payments/v2` contract is pinned in `contracts.lock.yaml`. Migration `000126`.

`POST /internal/payments/v1/money-movement-authority/resolve` is a private, opt-in Control Plane read for the Payments execution PEP. It answers one question: may this operating Organisation undertake **one** catalogued money movement, for **one** canonical instruction fingerprint and attempt? A request looks like this:

```json
{"context_id":"<CP-issued RUNTIME context>","engine_instance_id":"ei_…","operation_class":"payments.capture.execute",
 "operation_reference":"payments/capture/pay_…/key-…","attempt":2,
 "instruction":{"tenant_id":"…","operating_organisation_id":"…","market":"UG","currency":"UGX","amount_minor":150000,
  "payment_reference":"pay_…","parent_reference":"pay_…","merchant_profile_reference":"mprof_…","destination_reference":null},
 "instruction_fingerprint":"<sha256 hex>"}
```

It is one of two independent layers. CP decides legal authority; Payments decides financial validity, approval, provider readiness and execution. Both must permit, and neither overrides a denial by the other.

## Mounting

The route is mounted only when **all** of these hold:
- `PAYMENTS_MONEY_MOVEMENT_AUTHORITY_ENABLED=true`;
- the environment is `development`, `test`, `integration`, `sandbox` or `staging`;
- `PAYMENTS_MONEY_MOVEMENT_AUTHORITY_CLIENT_IDS` names at least one workload client.

Production, and an unset environment, never register the route. The caller also needs `context:resolve` and `payments-money-movement-authority:resolve`, both in its actual IAM token and in the ACTIVE workload registry. Shared grants this scope only to the PROVISIONED Payments staging assessor.

## Enforcement

1. Authenticate a workload that is active, registered, scope-allowed and on the explicit allowlist.
2. The request is schema-closed. It cannot carry a legal entity, mandate, role, activity, capability, approval, provider permission or time. Its instruction shape is fixed per class: a refund may only return funds to `ORIGINAL_PAYMENT_METHOD`.
3. **Payout is never activated.** `payments.payout.execute` returns `OPERATION_NOT_ACTIVATED` before any context, binding or mandate is read. Migration 000126 also refuses to record a payout with any other outcome, role or capability. Collection authority is never disbursement authority.
4. Redeem the RUNTIME context for its owning canonical identity, using `redeemContext` with `runtimeOnly`. If the context is not usable, the result is `CONTEXT_NOT_USABLE`, without scope.
5. The instruction's tenant, Organisation and market must echo the context exactly, and the fingerprint must recompute under the canonical encoding (Shared README and golden vectors). Otherwise the result is `INSTRUCTION_MISMATCH`, before any binding or mandate is read. Any change to amount, currency, merchant profile, parent payment, destination, class or reference invalidates the decision.
6. Require a live `baobab-payments` engine instance with an ACTIVE, in-effect binding in a scope the tenant owns. Otherwise the result is `ENGINE_INSTANCE_NOT_BOUND`. An ERP instance never qualifies.
7. Derive role (`PAYMENT_BENEFICIARY`), activity and capability **only** from the pinned `payments/v2/money-movement-operations.yaml`. Re-evaluate the LA-04 trusted resolver on every call. Ambiguous, revoked, expired, suspended and unverified mandates all fail closed.
8. `AUTHORIZED` carries the mandate lineage and the capability key. Its lease is at most 30 seconds, and never later than the context or the mandate.
9. Every decision, including each denial, is recorded in the immutable `registry.payments_money_movement_decision` **before** it is returned. That table stores the class, reference, attempt and fingerprint. A decision that cannot be recorded is never disclosed. Responses are schema-validated, `no-store`, and always carry `provider_permissions_granted: false`.

## What Payments must still do

The decision is a CP fact and grants nothing. Payments must, separately:
- apply its own financial checks (refundable balance, capture limit, state);
- check independent provider readiness;
- run the reserve, re-authorise, dispatch, observe and reconcile protocol;
- keep its own COMMIT fences;
- store `decision_id`, `mandate_id` and the fingerprint with the reservation and the dispatch;
- refuse to dispatch once `min(expires_at, receipt + 30 s)` has passed.

## Acceptance state

This increment implements only the CP authority source and its decision history. Still open:
- Payments enforcement;
- Gate D1 (a real Payments, CP and HyperSwitch run);
- Gate D2 (protected staging with an approved PSP sandbox);
- IAM workload activation;
- real mandates and merchant certification;
- payout activation;
- production enablement.

Passing CP tests is not Payments acceptance. No Nabhold, ZuriBeans, Equator & Estate or Thamani authority record is created.
