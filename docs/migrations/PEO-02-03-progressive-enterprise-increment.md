# PEO-02 / PEO-03 — progressive admission policy and bounded founding documentary grace

**Decision:** Accepted ADR-BCP-026 with 9 October 2026 Amendment A1 (24 calendar months). **Deployment state:** repository candidate, runtime policy + guarded database foundation; **NOT an enabled founding sponsorship API, no self-service v2 route, no applicant UX release, no production certification**.

## Implemented in this increment

- `internal/service/progressive/policy.go`: strict exact **24 calendar month** window from the original effective provisional admission approval; UTC month-end clamping, independent maker/checker, current founding sponsorship and unique named `PLATFORM_DOCUMENTARY` requirements. Other authority classes are never waived. A rename, replay or tenant re-provision does not create a new grace origin.
- `internal/store/postgres/migrations/000107_peo02_founding_documentary_grace.sql`: PostgreSQL 17 tables with real Principal/Organisation/AdmissionDecision references, human SoD, check against approved INTERNAL_GROUP application, immutable sponsorship/grace identity and **one deferral ever per operating Organisation**. Exact `decided_at + interval '24 months'`, no extension or renewal. Live positive DB view filters expired grace and revoked/expired sponsorship. Inserts create no company, tenant or entitlement; no fixture creates a grant.
- `EvaluateProgressiveEligibility`: requirement-level decisions distinguish `NOT_APPLICABLE`, `REQUIRED`, and `NEEDS_ASSESSMENT`. Unverified claims do not satisfy a requirement. Approved platform documentary grace can only defer *that requirement*; merchant/provider and statutory requirements remain outstanding. Stale/missing policy facts fail closed.
- Unit tests address PEO-T06/T07/T08/T08A/T08C/T09. Live DB lifecycle and v2 admission/UX tests are still required.

## Explicit limitations and critical follow-up

This is **not complete PEO-02 or PEO-03**. The new Go policy is a reusable enforcement primitive, but no existing HTTP route currently invokes it and the DB migration cannot by itself establish the trustworthiness of an external policy's classification. A `FoundingDocumentaryDeferral` row must be issued by a **future** authenticated governed maker/checker command using verified CP policy provenance, with idempotency/audit/outbox, and then consumed from the exact current DB view by the admission/capability PEP. An inert row is NOT a sponsor attestation or activation grant.

**PEO-02 next:** add server-only governed sponsorship propose/decide/suspend/revoke and deferral issuance/fulfilment/revocation API/SQL commands, independently authorised typed policy inputs, repeatable idempotency with digest, audited outbox and No-Reset event. Test PostgreSQL trigger cases, concurrent duplicate issuance, expiry/leap-day and changing corporate affiliation. Register canonical Shared API schema/OpenAPI and IAM scope. Explicitly stop support for deferrals whose legal/provider requirements may apply. No dummy Companies or internal classification.

**PEO-03 next:** implement an authenticated application v2 draft/update/submit flow based on canonical Shared `admission/v2/application.schema.json` and `business-identity.schema.json`. Preserve applicant ownership, channel, optimistic `version` updates, PEO requirements applicability and the v1 maker/checker admission decision. Replace universal registration/PAYE/UIF requirements with **versioned, purpose-specific actual policy results**; do NOT program tax thresholds. Integrate CP Console progressive UX via generated OpenAPI types only after API acceptance. No applicant-selected LegalEntity/tenant/engine or auto-grant.

## Safety boundaries

1. A registered Organisation can be unincorporated; no verified LegalEntityProfile is minted.
2. Human sponsorship or deferral approval is **not** independent legal actor authority or an INTERNAL subscription classification.
3. First-party sponsorship is not proof of parent ownership or permission across tenants.
4. Founding grace is *platform documentary* only and never delays existing statutory or provider obligations.
5. Unproven provider, IAM, market or business capability readiness blocks the affected operation, not unrelated account admission.
6. Deferral status is not equivalent to evidence VERIFIED, and expired grace never silently creates a fresh window.

The permanent `UNIQUE(organisation_id)` history forbids policy reissuance in this initial model. If authorised corrections to a flawed record are needed, implement a separately audited forward-only adjustment with the **original** deadline, not a delete/reinsert exception.
