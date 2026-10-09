# LA-03 — Governed Organisation-first admission and tenant registration

**Decision basis:** Accepted ADR-BCP-026/027 and Shared LA-01.
**Canonical Shared contracts:** `baobab-platform/shared` commit `5930dcf07d16cbb138fa98af0059d9a35e011114`.
**Control Plane migrations:** 000102 (nullable compatibility) and 000103 (reviewed pre-tenant identity).
**Runtime state:** v2 registration and preparation are feature-gated and **disabled by default**; not production acceptance, legal actor mandate or commerce authorization.

## Executable journey

1. A governed application and AdmissionDecision must already exist and an authorised request must have status `AUTHORISED` under the existing maker/checker lifecycle. The existing v1 application submission workflow remains unchanged: support for applicant-facing progressive submission is a separately necessary PEO-03 rollout, not implied by a v2 tenant-registration route.
2. An independent platform reviewer uses `POST /v2/tenant-onboarding/{requestID}/primary-organisation` with Idempotency-Key, identity_resolution_policy_reference and evidence_reference. The reviewer must differ from both the requester and the authoriser.
3. The reviewer **may supply `organisation_id`** only to reuse a stable Organisation recognised by the Shared first-party registry and matching the approved request's name; otherwise CP mints a pre-tenant Organisation of status UNVERIFIED with no associated company registration, legal-entity profile, tenant, membership, grants or subscription. The immutable request binding and audit/event are atomic.
4. A platform administrator registers through `POST /v2/tenants` with the exact Shared LA-01 schema. CP mints the tenant ID; the in-transaction registration checks the approved request, reviewed Organisation binding and desired state, assigns the canonical Organisation, creates exactly one PRIMARY TenantOrganisationMapping, subscribes requested products as PENDING and fulfils the request atomically. No automatic activation is inferred.
5. `legal_entity_id` is optional. Until LA-04 permits scoped cross-Organisation mandates, a DEFAULT is permitted **only** for a real independently VERIFIED LegalEntityProfile attached to the *same* operating Organisation. A Nabhold legal actor serving ZuriBeans or Equator & Estate MUST NOT be attached by this shortcut.
6. A Shared first-party registry entry annotated identity_class and incorporation_claim creates a durable operating Organisation reference. Inclusion NEVER verifies corporate registration, parent control, entitlement or ability to trade. The legacy first-party verifier is deliberately excluded for annotated records and its old DEFAULT LegalEntity -> PRIMARY Organisation inference is prohibited.
7. Historical, unannotated registry fixtures retain v1 parser semantics, while the actual file-loader used by live reconciliation requires all Shared LA-01 identity annotations. Existing mistaken legal verification is reported as blocking drift for independent review; it is not silently deleted or backdated.

## Security / rollout

Both v2 routes require the existing `tenant:write` human platform administrator authorization and role guard. The `OrganisationFirstV2` dependency flag defaults false and is not enabled by production config in this increment. A feature cutover needs reviewed versioned API publication, a live readiness/rollback decision, verification-case and corporate-record reconciliation, IAM integration and provider acceptance. This increment is intentionally **not** a route allowing self-service unincorporated applicants, a statutory documentary waiver or a payment/ERP company creation workflow.

## Tests / acceptance

- PostgreSQL 17 migration 000103, strict versioned schema pin, source registry identity, Go race, foundation, ERP and resolution-readiness checks.
- An approved but unincorporated applicant can obtain an UNVERIFIED Organisation without a fabricated CIPC profile.
- Exactly one immutable pre-tenant Organisation is bound to the approved request by an independent reviewer; replay converges and mismatches fail.
- Registration with no legal actor succeeds and fulfils the authorised request atomically, while wrong Organisation, absent mandate or invalid desired-state fail without a tenant.
- Identical v2 HTTP retries are idempotent despite different provisional tenant IDs; no duplicate operation, outbox or tenant.
- ZuriBeans, Equator & Estate and Thamani may be recognised as separate first-party business identities; unincorporated entities do not become VERIFIED legal persons.
- No route exists unless explicitly enabled, and an unauthenticated principal cannot invoke either route.

## Open downstream boundaries

**LA-04:** independently evidenced activity-, market-, time- and actor-scoped OperatingLegalActorMandate with revocation and resolution. Required before Nabhold acts as legal issuer for ZuriBeans and Equator & Estate.

**LA-05:** IAM entitlement/context and Trade/ERP/payment/document integration; provider observed readiness and actual legal actor provenance. Existing v1 context consumers still require a default LegalEntity and must fail closed for defaultless v2 tenants until they deliberately migrate.

**PEO-02/03:** founding sponsorship/Internal eligibility, 12-month named documentary deferral, legal-form-specific application UX and assisted/self-service submission. Unincorporated applicants cannot use existing strict v1 application submission unchanged.

**LA-06/07:** real first-party registration, operational/legal verification, provider certification and production rollout after review of all dependencies.
