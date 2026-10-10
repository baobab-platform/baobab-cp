# PEO-02B — Founding sponsorship and documentary-grace deactivation

**Scope:** implementation increment, not PEO-02 or founding-group onboarding acceptance.
**Authority:** ADR-BCP-026/A1; preserves the accepted 24-calendar-month, no-reset policy.
**Runtime:** `PEO_FOUNDING_GOVERNANCE_ENABLED=true` and `BAOBAB_ENVIRONMENT=staging` (or an explicitly permitted development/test environment); production is never included.

This increment adds an authenticated human checker command, a one-way PostgreSQL status transition and an append-only idempotency ledger. Existing proposal/independent-decision flows are unchanged. A reviewer must possess `admission:decide`, be a registered CP platform administrator and not be the **original grant proposer**. Every command requires a 16–128-character `Idempotency-Key`, a specific evidence reference, a meaningful reason and an exact expected state. The same checker/key/payload returns the recorded receipt; a changed payload is denied.

| Endpoint | Expected state | Result |
| --- | --- | --- |
| `POST /v2/founding-governance/sponsorships/{grantID}/suspend` | `ACTIVE` | `SUSPENDED` |
| `POST /v2/founding-governance/sponsorships/{grantID}/revoke` | `ACTIVE` or `SUSPENDED` | `REVOKED` |
| `POST /v2/founding-governance/documentary-deferrals/{grantID}/revoke` | `ACTIVE` | `REVOKED` |

Example **synthetic-only** command body (never place a real identity into a test):

```json
{
  "reason": "Independent reviewer withdrew the evidentiary basis after a documented check",
  "evidence_reference": "synthetic-review/withdrawal-001",
  "expected_status": "ACTIVE"
}
```

The authenticated reviewer identity comes from the server, never this body. Grant identity, original decision, dates and scope cannot change. The current-time `admission.active_founding_documentary_deferral` view immediately ceases to return a revoked/suspended sponsorship or revoked deferral. The 24-month limit remains measured from the **original** provisional approval; nothing in this change resets or extends it. A concurrent reviewer seeing a stale state gets a conflict, and a successful decision and its audit record/command receipt commit together.

## What this increment does *not* authorise

- It cannot create a sponsor, decide a proposal, verify Nabhold, grant an INTERNAL subscription or prove a separate ZuriBeans company.
- It cannot fulfil documentary evidence. `FULFILLED` needs an independently verified satisfaction record and is deliberately not reachable via these routes.
- It cannot activate a suspended grant or renew an expired/revoked grant.
- It does not publish a new canonical event: Shared must approve the schema and versioned outbox mapping first. **Downstream consuming-policy enforcement is still required** before a revoked grant can be considered platform-wide closed.
- It cannot register or provision a tenant, activate an operating legal-actor mandate, or post finance.
- It does not mark PEO-03, LA-06 or LA-07 complete.

## Acceptance checklist

1. Confirm migration `000111` applies on PostgreSQL 17 without changing existing sponsorship/grace records.
2. Run `go test ./internal/store/postgres ./api` and all existing CI, including database-backed migration/tests.
3. Verify unauthenticated calls, unknown environments and production all fail closed.
4. With synthetic authorised staging records, verify ACTIVE → SUSPENDED → REVOKED, ACTIVE → REVOKED, deferral ACTIVE → REVOKED, stale expected-status rejection, replay consistency and audit provenance.
5. Add Shared-approved lifecycle event envelopes, transactional outbox, consuming PEPs, expiration reconciliation and independent identity review before claiming PEO-02 closure.
