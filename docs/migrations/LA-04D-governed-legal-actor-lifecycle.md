# LA-04D — Guarded Operating Legal Actor Lifecycle

Authority: Accepted ADR-BCP-027, Shared commit `86d317175bbb2728b38e77ae86b951d04f3219b6`. Complements merged LA-04C and replaces only its blanket SQL activation prohibition with a fully conditional transition guard.

## Boundary and rollout

- `LEGAL_ACTOR_MANDATE_LIFECYCLE_ENABLED` is **false by default**. Routes are available only in `development`, `test`, `integration`, `sandbox`, or `staging` when explicitly enabled and backed by PostgreSQL. `production` or unspecified environments are **hard-disabled**. Enabling LA-04C proposal and decision routes is separate (`LEGAL_ACTOR_MANDATE_COMMANDS_ENABLED`).
- All commands use a separate, registered, human-only OAuth scope and require platform-administrator authority and registered CP principal. `legal-actor-mandate:activate` may only activate; `legal-actor-mandate:terminate` cannot activate.
- `POST /v2/legal-actor-mandates/{mandateID}/activate` requires `Idempotency-Key` (16–128 ASCII tokens) and a Shared `MandateLifecycleRequest` with `action: ACTIVATE`, authority basis and evidence references.
- `POST /v2/legal-actor-mandates/{mandateID}/terminate` uses the same schema with `SUSPEND`, `REVOKE`, or `EXPIRE`; no actor ID may be selected in the request.
- Server returns the exact Shared `MandateLifecycleReceipt`. Same principal/action/key replay returns the original committed receipt; a different payload, mandate or principal conflicts.
- `ACTIVE` requires a prior immutable LA-04C `APPROVE` decision, distinct maker, checker and **third human operator**, a currently ACTIVE verified LegalEntityProfile with evidence reference matching the checker decision, and the exact LIVE PRIMARY Organisation mapping. Effective windows must be current; the activation is rejected if another ACTIVE mandate overlaps in Tenant, Organisation, role, activity, market, capability (empty means wildcard) **and time**. Advisory locks serialize competing activations. Parent/subsidiary affiliation never grants authority.
- `SUSPENDED` cannot be reactivated; `REVOKED` and `EXPIRED` are terminal. Reinstatement requires a *new*, independently approved record (optionally `supersedes_mandate_id`), and the old mandate must terminate before a successor sharing its scope activates.
- `EXPIRE` is a recorded transition only after `effective_to`; **runtime resolution already rejects an ACTIVE row after expiry**, independently of an operator sweep. LA-04D does not yet provide an autonomous sweeper.
- Transition record, state change, restricted audit record, canonical versioned CloudEvents outbox record and immutable idempotency response are one transaction. State is never patched as a compensating side effect; failed updates roll back evidence.

## Explicit limitations and go/no-go

An active CP mandate is *only* a CP legal-actor fact for a specific scope, not a payment authorization, corporate power of attorney, market license, Trade settlement entitlement, ERP posting permission or cross-tenant access. Provider-dependent roles require separate certifications, context enforcement and acceptance under LA-05.

Before production enablement: obtain independent security approval for database owner privileges and the lifecycle trigger model; verify registered human OAuth scopes and live IAM attestation; complete staging/PostgreSQL concurrency and revocation proof; prove consumer-side re-resolution for every restricted action (the current decision lease is at most 30 seconds); validate legal/market evidence; review monitoring, outbox delivery, rollback and disaster recovery. No Nabhold/ZuriBeans/Equator real mandate is created by this code.

## Incident rollback

Disable `LEGAL_ACTOR_MANDATE_LIFECYCLE_ENABLED` immediately to prohibit *new* lifecycle commands. **This does not revoke existing ACTIVE authority.** Revoke affected mandates through the authorized `terminate` route before disabling, or use a reviewed emergency database runbook and evidence trail; consumers must independently deny transactions when CP cannot be reached. Suspend or revoke suspect verification/PRIMARY mappings through existing governance, and verify resolution returns non-AUTHORIZED. Never directly delete mandate history, rewind to an older SQL schema or relabel a revoked row ACTIVE.
