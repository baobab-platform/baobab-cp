# P0-04B-04 — ERP posting-authority resolution

**Authority:** ADR-SHARED-034, ADR-BCP-027 (§6.1, §8.3, §10.2, LA-05, LA-07). The Shared `erp/v2` contract is pinned in `contracts.lock.yaml`. Migration `000125`.

`POST /internal/erp/v1/posting-authority/resolve` is a private, opt-in Control Plane read for the ERP posting PEP. It joins CP legal-actor authority to an ERP-owned native placement for **one** accounting operation:

```json
{"context_id":"<CP-issued RUNTIME context>","engine_instance_id":"ei_…","operation_class":"erp.gl-journal.post",
 "native_placement_reference":{"placement_id":"<uuid>","placement_version":2},"operation_reference":"erp/…/trx-…"}
```

The route is mounted only when **all** of the following hold:
- `ERP_POSTING_AUTHORITY_ENABLED=true`;
- the environment is `development`, `test`, `integration`, `sandbox` or `staging`;
- `ERP_POSTING_AUTHORITY_CLIENT_IDS` names at least one workload client.

Production and an unset environment never register it. The caller also needs `context:resolve` and `erp-posting-authority:resolve` in its actual IAM token and in the ACTIVE workload registry. Shared grants the scope only to the PROVISIONED ERP staging assessor.

## Enforcement

1. Authenticate a workload that is active, registered, scope-allowed and on the explicit client allowlist.
2. The request is schema-closed. It cannot carry a tenant, Organisation, market, legal entity, mandate, role, activity, capability, time or native ERP identifier (LA-T23).
3. Redeem the RUNTIME context for its owning canonical identity, using `redeemContext` with `runtimeOnly`.
   - A context that redeems but is not bounded and current, has no UUID Organisation or market, or belongs to an inactive tenant gives `CONTEXT_NOT_USABLE`, without scope.
4. Derive role, activity and capability **only** from the pinned `erp/v2/posting-operations.yaml` catalogue. Derive tenant, Organisation and market only from the context.
5. Require the `engine_instance_id` to be a live `baobab-erp` instance (`ACTIVE`, in effect) with an ACTIVE, in-effect capability binding in a scope the tenant owns. Otherwise return `ENGINE_INSTANCE_NOT_BOUND`, before any mandate is read. The approved provisioning plan is not authority for posting.
6. Re-evaluate the LA-04 trusted resolver on every call. `AMBIGUOUS`, revoked, expired, suspended and unverified all fail closed, as in LA-05A.
7. An `AUTHORIZED` decision carries `mandate_id`, `supersedes_mandate_id`, the mandate window, the responsible legal entity and evidence. Its `expires_at` is at most 30 seconds and never later than the context or the mandate. A resolution missing any lineage field is downgraded to `ACTOR_NOT_VERIFIED`.
8. Every decision, including denials, is recorded in `registry.erp_posting_authority_decision` **before** it is returned.
   - That table is immutable (no UPDATE or DELETE), stores no native ERP identifiers, and checks that `AUTHORIZED` rows are complete with a lease of 30 seconds or less, and that denials disclose no actor.
   - A decision that cannot be recorded is never disclosed.
   - Responses are validated against the pinned schema, are `no-store`, and always carry `provider_permissions_granted: false`.

## What ERP must still do

The decision is a CP fact and grants nothing. ERP must, separately:
- check placement and provenance;
- check its own finance readiness (period, tax, baseline);
- store `decision_id`, `mandate_id` and the placement reference with the posting in the same transaction;
- deny at its database COMMIT once `expires_at` has passed.

A revocation takes effect within the lease.

## Acceptance state

This increment implements the CP authority source and its decision history only. Not included:
- ERP enforcement (P0-04B ERP PRs)
- a real iDempiere posting run (EA v2.1 Gate D)
- IAM workload activation
- real mandates or placements
- production enablement

These all remain open. Passing CP tests is not ERP acceptance. No Nabhold, ZuriBeans, Equator & Estate or Thamani authority record is created.
