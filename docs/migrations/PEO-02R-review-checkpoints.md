# PEO-02R — Durable 6/12/18/24-month documentary reviews

**Authority:** Accepted ADR-BCP-026 §§6, 14–15, including the 24-calendar-month amendment.
**Dependencies:** Shared admission/v2, governed founding sponsorship/deferral, CP migrations 000107–000113.
**Delivery:** CP migration 000114, `SweepFoundingDocumentaryReviews` and disabled-by-default staging scheduler.

A documentary deferral is a narrowly scoped **platform evidence-process decision**. It is
not incorporation, licence verification, statutory exemption, provider entitlement
or a legal-actor mandate. An operator must be prompted to reassess named
requirements before relying on the deferral indefinitely.

## What this increment implements

- A durable checkpoint ledger with **four fixed milestones**: months 6, 12, 18
  and 24, calculated by calendar-month UTC anniversary of the *original*
  effective provisional approval. Month-end and leap-day dates clamp.
- A PostgreSQL constraint and immutable trigger bind each checkpoint to the
  original deferral ID, Organisation, enumerated requirement IDs, policy
  reference and due date. No caller can invent a new starting date or mark
  an item VERIFIED/NOT_APPLICABLE by writing a checkpoint.
- A bounded scheduler, controlled by
  `PEO_FOUNDING_GOVERNANCE_ENABLED=true` **and**
  `PEO_FOUNDING_REVIEW_SCHEDULER_ENABLED=true`, allowed only in existing
  nonproduction environments. It materialises only due checkpoint work.
  Concurrent/retried runs use the `(deferral_id,milestone_month)` unique
  identity and `ON CONFLICT DO NOTHING`.
- Each newly created checkpoint emits an **internal restricted audit entry** in
  the same transaction. This is not a Shared cross-engine event: do not
  publish an invented event type or treat an audit record as a grant.
- The ledger retains pending 24-month remediation work even after existing
  expiry reconciliation changes a deferral to EXPIRED. Revoked/fulfilled
  deferrals do not acquire new reliance on grace.
- Existing policy and consuming PEPs independently deny expired or revoked
  grace irrespective of scheduler cadence. No tenant deletion, global
  suspension, default legal actor, or retroactive legal-status mutation.

## Work intentionally not authorised by this PR

**A review checkpoint is not a completed review.** A subsequent Shared
contract and CP authorised API must accept versioned, independently reviewed
evidence-applicability decisions and requirement-specific outcomes, record
provenance and support remediation. Human or certified-machine approval must
be explicit before changing restrictions, reclassifying eligibility or
marking evidence approved. The scheduler cannot automatically extend a
grace period or restrict unrelated capabilities. Missing downstream
Regulations/Trade/ERP/Payments provider integrations remain closed.

## Tests and rollout

PostgreSQL 17 migration journal, UTC across time zones, leap-day and
month-end equivalence to the existing progressive Go policy, duplicate
checkpoint rejection, no fabricated positive status, and no changes to the
underlying sponsor/deferral records. A green PR is not a staging run.
Use reviewed synthetic data and retain audit evidence before activating
the scheduler in staging.
