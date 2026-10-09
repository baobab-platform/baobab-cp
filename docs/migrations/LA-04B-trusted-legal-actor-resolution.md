# LA-04B — Trusted, bounded legal-actor resolution (internal boundary)

**Status:** Implementation increment, not an operational approval or provider entitlement.
**Authority:** Accepted ADR-BCP-027 §6, §12 LA-04 and Shared LA-01 `organisation/v2/legal-actor-mandate.schema.json` at pin `5930dcf07d16cbb138fa98af0059d9a35e011114`.
**Predecessor:** LA-04A, CP PR #295, PostgreSQL migration 000104.

## Runtime decision boundary

`Store.ResolveOperatingLegalActor(ctx, request)` is a **read-only internal CP adapter**:

1. Derive the evaluation timestamp from CP's server clock, never an external caller-provided time that could resurrect expired authority. Select only mandates for exactly the requested Tenant and canonical operating Organisation; neither legal entity nor mandate may be chosen by the requester.
2. Require that Tenant's **live, in-effect PRIMARY_ORGANISATION mapping** to that exact Organisation. Historical/default LegalEntity mapping is not an identity substitute.
3. Independently attest the candidate LegalEntityProfile as in-effect, VERIFIED, ACTIVE and evidence-backed; exclude registration-only and legacy first-party digest provenance and explicitly unincorporated operating-business identities.
4. Apply LA-04A's role/activity/market/capability/time and approval separation-of-duties checks to CP persistence candidates. Multiple applicable ACTIVE records resolve to `AMBIGUOUS`, not a chosen issuer.
5. Issue `AUTHORIZED` results with the Shared-required `valid_until` at most **30 seconds** from evaluation, shortened further by the mandate end. This is an *upper bound on a decision's lease*, not a guarantee against intervening revocation; legally consequential operations must evaluate current authority again.

This adapter adds no public or workload route, caller-controlled legal actor parameter, default legal-person inference, subscription, legal verification, ERP company, invoice issuer, bank merchant or statutory-permission assertion.

## Remaining LA-04 delivery

- **LA-04C:** governed immutable intent/maker command, independent checker and authority/evidence decision, feature-gated versioned APIs, exact Shared contract validation, atomic audit/CloudEvents outbox, idempotency and concurrency discipline.
- **LA-04D:** controlled activation by a separately approved security migration; suspend, revoke, supersede, expiry, current-authority invalidation and conflicts. Until these are complete the DB `operating_legal_actor_mandate_activation_gate` remains present and forbids ACTIVE mandates.
- **LA-05:** integrate current CP decision/revocation through IAM, Trade, ERP and document/payment PEPs, with provider-specific readiness gates. Do not send `AUTHORIZED` to business consumers until these are independently accepted.

## Safety and tests

PostgreSQL adapter does not infer legal identity from owner relationship, same-group registry entries, trading style, v1 tenant default, or another tenant's mandate. Failures to read DB never produce an authorizing result. Policy tests verify an open-ended mandate cannot return an indefinite canonical `AUTHORIZED` response and approvals dated after evaluation cannot be used prematurely. PostgreSQL integration tests exercise no-mandate denial on the exact real datastore/migrations.

Neither ZuriBeans nor Equator & Estate Co. thereby receives a Nabhold mandate. No real founding-entity verification or issuance is recorded.
