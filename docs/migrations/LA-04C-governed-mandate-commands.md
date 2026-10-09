# LA-04C — Governed operating legal-actor mandate proposal and independent decision

**Authority:** Accepted ADR-BCP-027 §6 and LA-04; Shared pinned at `c47df3512aab759b7914899efef429d73a01ffde`.
**State:** Manual, privileged **non-activating** maker/checker commands. Migration `000104` continues to **prohibit ACTIVE mandates**.

## Privileged workflows

1. With `LEGAL_ACTOR_MANDATE_COMMANDS_ENABLED=true` (default disabled), a platform administrator with human OIDC token and `legal-actor-mandate:propose` scope may `POST /v2/legal-actor-mandates` with an `Idempotency-Key` and an exact Shared `ProposeMandateRequest`. Maker may not supply identity, actor, approval or status beyond scoped business intent. CP verifies a live PRIMARY mapping, confirms human principal, mints UUID and writes inert PENDING mandate, audit, outbox and replay ledger in **one transaction**.
2. A different human platform administrator with `legal-actor-mandate:decide` scope may `POST /v2/legal-actor-mandates/{mandateID}/decision` with `Idempotency-Key` and Shared `DecideMandateRequest`. `REJECT` is a decision record; `APPROVE` additionally requires a CURRENT independently VERIFIED, ACTIVE LegalEntityProfile and an exact matching evidence reference from CP's evidence-backed legal profile. Both commands preserve mandate `PENDING`. The database repeats SoD and verification checks for direct SQL writers.
3. Both routes return the canonical nonactivating `MandateCommandReceipt`, with decision ID on checker outcomes. Identical retries return the exact durable receipt without new audit/outbox events. Different commands/principals using the same key are refused. Decision history is insert-once, immutable and distinct from mandate lifecycle status.
4. The transactional outbox publishes versioned `MandateProposed` / `MandateDecided` payloads under Shared `organisation/v2/legal-actor-mandate-events.schema.json`; event data contains **no** evidence contents, registration identifiers, personal data, or legal actor claims. Audit records (restricted) carry reviewed evidence references.

## Security boundaries

- The feature is **disabled by default** and cannot override the SQL activation gate.
- OAuth scopes are distinct for maker and checker, with platform-only role checks. Their issuance must be governed by IAM (not inferred from a tenant-admin or Nabhold group role); without such scopes the API fails closed.
- Tenant/Organisation linkage is resolved from CP canonical mappings, not a display name, legal-entity DEFAULT, or first-party relationship.
- Approval does **not** establish provider eligibility, statutory permits, invoicing authority, banking status, corporate ownership, access to another tenant, legal verification itself or an operational mandate.
- Actual active legal-actor resolution is still subject to LA-04D activation, revocation/expiry, overlap conflict checks and LA-05 consumer integration.
- No Nabhold-to-ZuriBeans or Nabhold-to-Equator mandate is created by this migration.

## Next step: LA-04D

Implement fully governed prospective activation, suspension, revocation, supersession, non-overlap acceptance, event publication, rollback and current-authority invalidation. **Do not remove `operating_legal_actor_mandate_activation_gate` until this complete lifecycle is independently reviewed and accepted.**
