# LA-04A — Scoped operating legal-actor mandate foundation

**Architectural authority:** Accepted ADR-BCP-027; ADR-BCP-026 does not waive legal-actor evidence.
**Contract authority:** `baobab-platform/shared` `contracts/organisation/v2/legal-actor-mandate.schema.json`, lock `5930dcf07d16cbb138fa98af0059d9a35e011114`.
**Depends on:** merged LA-03 CP PR #294, migration 000103.
**Status:** Persistence and pure resolution policy foundation, intentionally **not operational authority**.

## Scope

Migration `000104_operating_legal_actor_mandate.sql` creates a real, versioned
PostgreSQL persistence boundary for `OperatingLegalActorMandate`: separate
tenant, operating Organisation and responsible LegalEntity; role, activity,
market and optional capability scopes; decision/evidence references,
independent maker and checker, effective window and history. Registered
LegalEntityProfile identities are required even to record a mandate draft.

This increment **hard-blocks all `ACTIVE` mandates** using
`operating_legal_actor_mandate_activation_gate`. The `PENDING` state
stores inert reviewed-intent candidates; immutability prevents direct
reinterpretation of legal identity, scope, evidence or validity after
insertion. Revocation preserves history. Future LA-04B must introduce
authorised command APIs and an explicitly reviewed migration before
dropping this activation guard.

`internal/service/legalactor` provides a pure, fail-closed candidate
selector for **trusted backend inputs only**. It never accepts a browser
supplied legal actor, and is not wired into HTTP, IAM, ERP, Trade, payments
or document issuance. Non-unique active candidates cause `AMBIGUOUS`;
no lexical/recency tiebreaker exists. Expired or revoked matches cannot
be redeemed using a backdated transaction timestamp; current evaluation
time is independently enforced. A Shared first-party declaration or
corporate `OWNS` relationship is *not* an operating mandate.

## Separation of identity and legal responsibility

```text
ZuriBeans Organisation ──PRIMARY──► ZuriBeans Tenant
                                        │
                                        │ governed scoped mandate (future LA-04B)
                                        ▼
                              Nabhold LegalEntity
                             ZA · SELLER_OF_RECORD
                          B2B_COFFEE_SALE · time window
```

For the separate Equator & Estate Tenant, another mandate would be
required even if the responsible legal person were again Nabhold.
Thamani's corporate relationship must not imply the use of Nabhold
as its new contracting or accounting actor.

## Exit criteria for LA-04B before production activation

1. Reuse CP `AdministrativeGrant` / Changeset / Approval authority;
   validate independent human maker and checker, authority decision,
   audited evidence and approved scopes.
2. Verify the LegalEntityProfile against the authoritative legal source
   and ensure a correct in-effect PRIMARY TenantOrganisationMapping.
3. Implement idempotent commands, concurrent conflict handling,
   withdrawal/suspension/revocation, supersession and expiry.
4. Write immutable audit and CloudEvents outbox entries within the same
   transaction as any governed state change. Audit readers must retain
   historical legal responsibility after incorporation or mandate rotation.
5. Replace the migration activation gate deliberately **only after** the
   write/approval/verification APIs and fail-closed trusted resolver adapter
   are deployed and tested.
6. Integrate IAM, Trade, ERP, payments and legal document consumers
   capability by capability. Provider authorisation and market-specific
   licences are separate evidence gates; the CP mandate alone is not
   statutory permission.
7. Prove negative tests for absent, ambiguous, wrong-market, out-of-scope,
   unverified, expired and revoked actors. Prove Nabhold acting for two
   tenants does not merge data, identity or ledger attribution.

## Validation in LA-04A

- Migration ordering and PostgreSQL 17 DDL apply with CI's canonical journal.
- Integration test checks the database refuses `ACTIVE`, scope edits and
  deletes while allowing preservation of revoked history.
- Pure policy tests validate strict tenant/Organisation, market, role,
  activity and capability scopes; independent approval and verification;
  overlapping conflicts; validity; revocation and evidence provenance.
- No runtime API routes, provider entitlement or activation toggles added.

**Production state:** blocked by design until LA-04B and later LA-05/07.
