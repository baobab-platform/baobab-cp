# PEO-02C — Transactional founding lifecycle events and consuming-PDP/PEP

**Contracts:** Shared admission/v2 lifecycle event schema + AsyncAPI + registry (Shared PR #272).
**Stack:** Control Plane #312 (000111) → #315 (000112) → this PR.
**Activation:** Synthetic, controlled nonproduction only. No production or real grant activation.

## Event authority

Control Plane is the only producer of the following seven canonical past-tense facts:
- `com.baobab-platform.control-plane.founding-sponsorship.activated.v1`
- `com.baobab-platform.control-plane.founding-sponsorship.suspended.v1`
- `com.baobab-platform.control-plane.founding-sponsorship.revoked.v1`
- `com.baobab-platform.control-plane.founding-sponsorship.expired.v1`
- `com.baobab-platform.control-plane.founding-documentary-deferral.activated.v1`
- `com.baobab-platform.control-plane.founding-documentary-deferral.revoked.v1`
- `com.baobab-platform.control-plane.founding-documentary-deferral.expired.v1`

An independent grant approval, manual revocation/suspension, or scheduled expiry writes its canonical CloudEvents envelope into `messaging.outbox` **inside the same PostgreSQL transaction** as the governance state transition and audit. The envelope is platform-scoped, with a CP-generated correlation ID, canonical schema URI and only grant/Organisation identifiers and status. A grant that fails its checker, audit or outbox insertion cannot commit.

The expiry sweep reads a bounded batch of due ACTIVE/SUSPENDED sponsorships and ACTIVE deferrals under `FOR UPDATE SKIP LOCKED`, updates their durable state and writes audit/outbox atomically. A retry sees no due row after successful commit. The sweep is mounted only when `PEO_FOUNDING_GOVERNANCE_ENABLED=true` in an explicitly permitted nonproduction environment.

## Current-time consuming policy

A lifecycle event invalidates any consumer cache; it **never becomes an entitlement token**. An INTERNAL classification must be rechecked at point of use through Control Plane. The live CP `EligibilityResolver` now queries the authoritative sponsor verification and current grant scope, status and effective period for registered first-party OPERATING_BUSINESS Organisations before admitting INTERNAL classification or reporting current eligibility.

Separately, PEO-03B's `bridgeInternalEligible` rechecks the same founding permission before REVIEW, DECIDE, REQUEST and AUTHORISE. A relationship, self-declared INTERNAL classification or emitted activated event is not sufficient.

**Important bounded limitation:** existing subscription/billing-engine consumers need their own authoritative current-time PEP before posting financial consequences. A CP classification's historical INTERNAL status and an emitted event cannot be trusted as current. This PR does not silently rewrite historical classifications or create provider-facing grants.

## Acceptance invariants

1. Approved grant → one correct canonical activation event, exact idempotent replay → no duplicate.
2. Suspended/revoked → immediate denial by current policy even before the outbox dispatcher publishes.
3. Expired at current time → denial before expiry sweep; sweeping records EXPIRED and one event.
4. Concurrency `SKIP LOCKED`, retry and rollback preserve single state transition and event atomicity.
5. Scope is `INTERNAL_GROUP_ADMISSION`; independently verified sponsor must be current; a claimed registration or linked group is insufficient.
6. Unrecognised environment/production → routes and scheduler absent. All real Nabhold/ZuriBeans legal and finance actions remain blocked.
7. Shared #272 must pass CI and be accepted before contract lock/production rollout. Consumers must validate against the versioned Shared event schema.

This programme still requires end-to-end broker delivery, subscription-engine current-authority enforcement, production operational certification and independent founding-group decisions before LA-06/07.
