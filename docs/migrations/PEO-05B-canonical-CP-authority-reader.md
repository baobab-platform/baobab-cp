# PEO-05B — current Control Plane provisioning and product authority

**Accepted:** ADR-BCP-026 PEO-05, ADR-BCP-027 LA-06/LA-07, ADR-SHARED-015. Depends on PEO-05A policy gate (CP PR #342).

This increment implements `AuthoritativePEO05Reader`, a read-only composition over existing CP repository interfaces. There is no new parallel persistence or invented tenant entitlement.

## Sources and checks

The reader requires exactly one current tenant provisioning record; canonical `READY` plus readiness `READY`; no active execution; the frozen current desired state with a freshly calculated digest; the exact current, approved, unexpired plan whose calculated digest and plan approval match; original fulfilled TenantOnboardingRequest with distinct requester, authoriser and plan approver; and each product independently ACTIVE and classified in a ProductSubscription for the exact tenant.

It resolves the actual current ACTIVE product version and composition for **every desired product**, collecting the union of MANDATORY canonical capabilities. Multiple products cannot borrow or reuse one another's subscription. An unavailable catalog, missing classification, duplicate desired product, unsatisfied mandatory composition, altered digest or stale/superseded plan fails closed.

Only after CP checks does it consult `PEO05CurrentIAM` and `PEO05CurrentProviders` to retrieve independently authenticated, current tenant-bound IAM and provider proofs. It reuses the PEO-05A evaluator to deny absent or revoked membership, stale health, uncertified providers, wrong market residency and sibling-tenant evidence.

## Acceptance boundary

The CP source is a real `PostgresRepository`-compatible interface; **the IAM and provider readers are explicit ports and are not yet installed as live adapters**. A nil adapter always denies. The unit tests use synthetic IAM and provider evidence and are not real runtime certification.

The next Package 5 source integration must:

- Implement an IAM reader that retrieves provider-neutral, audience-bound issuer/principal/Organisation/Tenant membership and revocation evidence from governed IAM runtime, with proof of authentication and TTL.
- Implement a provider reader from CP's actual tenant grants/bindings, approved release/provider-support certification and bounded fresh HealthObservation, cross-checking exact contract major, residency and plan.
- Wire both into an IAM-protected runtime command or read-only operator surface; never read proof objects from untrusted HTTP.
- Prove cross-tenant denial and stale/revoked transitions against protected PostgreSQL 17 staging, then independently review.

Run: `go test ./internal/service/organisation`. Project CI additionally executes race, full Go suite, PostgreSQL resolution readiness, Foundation and Security.

Never mark a FoundingBusiness, incorporated person, subsidiary, Payment merchant or PSP READY solely from this reader. This increments PEO-05 implementation, not operational acceptance.
