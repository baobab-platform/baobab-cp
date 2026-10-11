# ADR-BCP-026 — PEO-01/02S Governed documentary reassessment, remediation, and appeals

This implementation augments **PEO-02R** (due checkpoints from the original
24-calendar-month approval) and Shared **PEO-01** canonical
`admission/v2/documentary-review.schema.json`.

## Core authority boundaries

```text
Due checkpoint at 6 / 12 / 18 / 24 months
       |
       v
Platform human maker (admission:review)
  PROPOSE reviewed requirement assessments (only SOURCE CLAIMS)
       |
       v
Independent platform human checker (admission:decide)
  ASSESSMENT_INCOMPLETE -> durable negative/incomplete record only
  REMEDIATION_REQUIRED -> named scope(s):
        Organisation + Requirement + Capability + Market
        -> append-only negative restriction
       |
       v
A separate human may FILE appeal with evidence reference
       |
       v
Third independent human adjudicates appeal (admission:decide)
  UPHOLD -> no scope released
  RETURN_FOR_REASSESSMENT -> new review revision is permitted
    for the SAME original checkpoint, with the SAME original grace expiry.
```

**Neither an appeal nor a review is a legal/evidence verification service.**
Regulations remains authoritative for statutory requirement applicability;
the Evidence verification source/provider controls approved evidence and
provider KYB/KYC. CP does not acquire those powers through applicant JSON.
The service never automatically marks `APPROVED_EVIDENCE`, declares
`NOT_APPLICABLE`, clears an existing hold, renews documentary grace or
publishes an invented cross-engine event. No tenant, seller, legal-actor or
payments grant is issued. Existing current-time consumers continue to deny
expired/withdrawn founding grace independent of this scheduler.

## Default-off API

All routes require a registered and authenticated human platform admin,
independent IAM scope and Control Plane administrative role:

| Method | Endpoint | Required scope |
|---|---|---|
| POST | `/v2/documentary-reviews/deferrals/{deferralID}/milestones/{month}/propose` | `admission:review` |
| POST | `/v2/documentary-reviews/intents/{reviewID}/decision` | `admission:decide` |
| POST | `/v2/documentary-reviews/decisions/{decisionID}/appeals` | `admission:review` |
| POST | `/v2/documentary-reviews/appeals/{appealID}/decision` | `admission:decide` |

Every request must supply a nontrivial Idempotency-Key; the existing
authenticated principal and correlation metadata are server-derived.
Proposal body follows the exact Shared
`DocumentaryReviewAssessment` schema and must cover every original
deferral requirement with the original policy reference, exact Organisation,
single trusted context, unexpired assessments and actual caller identity.
A second proposal at the same milestone requires a previously approved
`RETURN_FOR_REASSESSMENT` appeal, and creates a bounded new revision.
A reviewer cannot decide their own proposal; the appeal reviewer must be
distinct from proposer, prior checker and appellant.

`PEO_DOCUMENTARY_REVIEW_DECISIONS_ENABLED=true` is required in addition
to a permitted nonproduction environment. A missing or revoked IAM
identity, unknown scope, production deployment, stale checkpoint or absent
independent review always denies or omits the routes.

## Persistent invariants

Migration 000115 appends `documentary_review_intent`,
`documentary_review_decision`, `documentary_remediation_restriction`,
`documentary_review_appeal` and `documentary_appeal_decision`.
Rows are immutable, except one-way `PENDING -> DECIDED` / `FILED -> DECIDED`
state transitions. Digests bind the exact request to its idempotency key;
independent reviewer IDs and the cited policy/evidence are historical
audit fields. `active_documentary_remediation_restriction` projects
**only negative** Organisation + Capability + Market scope holds;
`DocumentaryScopeBlocked` is the corresponding CP repository predicate.

## Remaining acceptance beyond this increment

1. Integrate source-verified Regulations/provider applicability/evidence
   decisions; a supplied assessment remains **untrusted for positive** purposes.
2. Publish a separately authorised Shared workload-only negative scope
   assessment and deploy current-time consumer PEPs in Trade, ERP, Payments
   and documentary issuance. The database view is not a remote capability.
3. Create an independent policy-bound restriction release mechanism only
   after verified remediation, with historical attribution preserved.
4. Complete the applicant-facing CP Console and reviewer work queues.
5. Run real IAM-authenticated synthetic staging with genuine maker/checker,
   appeal, expiry, revision, outbox/inbox and rollback/recovery evidence.
6. Never admit a real founding business, create an actual license claim, or
   certify provider readiness from these test fixtures.

This PR is an executable review and negative-remediation foundation,
**not** ADR-BCP-026 production completion by itself.
