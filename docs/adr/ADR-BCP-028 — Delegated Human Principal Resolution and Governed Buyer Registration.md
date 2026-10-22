# ADR-BCP-028 — Delegated Human Principal Resolution and Governed Buyer Registration

**Status:** Proposed — the Control Plane's implementation record for ADR-SHARED-042 work package CAA-02  
**Governing decision:** Shared ADR-SHARED-042 — Canonical Commercial Actor Authority and Buyer Execution Context (Accepted 2026-10-21). Where this record and ADR-SHARED-042 differ, ADR-SHARED-042 governs.  
**Date:** 2026-10-21  
**Primary repository:** `baobab-platform/baobab-cp`  
**Canonical contracts:** `baobab-platform/shared`:
- `identity/v1/principal-resolution.openapi.yaml` (v1.1) and `principal-resolution.schema.json`;
- `identity/v1/workload-registry.yaml` field `subject_principal_audiences`;
- `identity-events/v1` `identity.created.v1`.

**Refines:** ADR-0004 §11–13 and §56  
**Implementation state:** Source only, behind default-off, nonproduction-only switches. No workload holds the scopes or a subject audience, and there is no connected acceptance. Per ADR-SHARED-042 §12, this is not `REAL_COMPONENT_SYNTHETIC_DATA`, `PROTECTED_STAGING` or production acceptance.

---

## 1. Scope

ADR-SHARED-042 §3–4 require CP to:
- resolve a verified human `(issuer, subject)` to an ACTIVE Principal and ExternalIdentity for a registered Trade workload, read-only;
- separately, register an eligible first-time buyer atomically, creating identity only.

This record describes how CP does that.

## 2. Resolution — `POST /internal/identity/v1/principals/resolve`

CP checks, in order:

1. **Caller.** The caller is an ACTIVE workload whose registry entry allows `identity:principal:resolve` and lists `baobab-trade` in `subject_principal_audiences`. That entry is the registered validator relationship. There is no separate operator allowlist; the registry is the source.
2. **Request shape.** The body is the closed request schema. Its `correlation_id` must equal `X-Correlation-ID`, and validation errors never quote the token.
3. **Local verification.** The subject token is verified against the configured buyer issuer's own discovery and keys, for audience `baobab-trade`. It must be human, carry `trade:buy`, and carry an `azp` on `BUYER_SUBJECT_AUTHORISED_PARTIES`. It must also have a token ID and be unexpired.
4. **Current status.** CP introspects the token (RFC 7662) at the issuer's origin before any identity read. Any disagreement denies, and an outage is `RESOLUTION_UNAVAILABLE`.
5. **Lookup.** An exact `(issuer, subject)` lookup that returns the row whatever its status:
   - unknown is `IDENTITY_NOT_RESOLVED` and creates nothing;
   - a non-human or non-ACTIVE Principal, or a non-ACTIVE ExternalIdentity, is `PRINCIPAL_NOT_ACTIVE`.
6. **Response.** `evidence_expires_at` never outlives the token.

The resolution carries no tenant, Organisation, membership, role or market. Trade binds those (ADR-SHARED-042 §5–7).

## 3. Registration — `POST /internal/identity/v1/principals/register-buyer`

Enabled only with `BUYER_REGISTRATION_ENABLED=true`, on top of resolution.

1. **Caller and evidence.** The same checks as §2 steps 1–4, with scope `identity:principal:register-buyer`. Holding the resolve scope never implies registration.
2. **Declared purpose.** The request declares `purpose: BUYER_ONBOARDING`, enforced by the schema. The token's BFF client must be approved for that purpose (`BUYER_REGISTRATION_AUTHORISED_PARTIES`, intersected with the resolution allowlist). Otherwise the request is `REGISTRATION_NOT_ELIGIBLE`.
3. **Existing identity.** An existing human identity returns `RESOLVED`. An identity linked to a non-human Principal is `IDENTITY_LINK_CONFLICT`. A stopped identity is `PRINCIPAL_NOT_ACTIVE` and is never revived. A link is never resolved by email.
4. **Abuse limits.** A per-process fixed window, `BUYER_REGISTRATION_PER_MINUTE` (default 30), applies per caller workload, BFF client and issuer. Only attempts that passed every evidence check count. A breach is `REGISTRATION_RATE_LIMITED` (429), and nothing is created. An aggregate or distributed limit is a deployment control on top of this.
5. **Atomic create.** `LinkedIdentityCreator.CreateLinkedIdentity` writes the Principal, its ACTIVE ExternalIdentity, an `identity.principal.created` audit record and a `com.baobab-platform.identity.created.v1` outbox event in one transaction. The event carries the issuer and never the subject.
   - A concurrent loser waits on `UNIQUE(issuer, subject)`, inserts nothing and rolls back its Principal, audit record and event. It then re-reads the winner. The result is one winner, no orphan Principal and no duplicate event; the Postgres test proves this.

## 4. Correction of `IdentityService.Resolve` (ADR-SHARED-042 §4)

The existing workload and applicant provisioning path created a Principal and then linked it in a separate step, which could leave an orphan under concurrency (old ADR-0004 §56).

`IdentityService.Resolve` now uses `CreateLinkedIdentity` whenever the repository provides it, as both the in-memory and Postgres repositories do. Two consequences:
- first authentication is atomic and audited, and emits its event;
- an existing stopped identity resolves to `ErrIdentityNotFound` instead of being re-created.

## 5. Configuration

| Variable | Meaning |
| --- | --- |
| `BUYER_PRINCIPAL_RESOLUTION_ENABLED` | Mounts resolution in nonproduction environments only |
| `BUYER_SUBJECT_ISSUER` | The only issuer whose buyer tokens are verified |
| `BUYER_SUBJECT_INTROSPECTION_ENDPOINT`, `_CLIENT_ID`, `_CLIENT_SECRET` | CP's introspection client, on the issuer's origin |
| `BUYER_SUBJECT_AUTHORISED_PARTIES` | The BFF clients whose buyer tokens are accepted |
| `BUYER_REGISTRATION_ENABLED` | Mounts registration |
| `BUYER_REGISTRATION_AUTHORISED_PARTIES` | The BFF clients approved for `BUYER_ONBOARDING` |
| `BUYER_REGISTRATION_PER_MINUTE` | The per-process abuse limit |

An incomplete configuration refuses to start.

## 6. Outstanding before activation

- **Registry allocation:** granting the Trade workload the scopes and `subject_principal_audiences` through the reviewed Shared registry process.
- **IAM clients:** CP's introspection client and the ZuriBeans confidential BFF client.
- **Hydra:** a deployed login/consent provider.
- **Provider-migration conflicts:** account-recovery and provider-migration link evidence, which would extend `IDENTITY_LINK_CONFLICT` beyond the non-human case.
- **Real-component acceptance:** the ADR-SHARED-042 §12 scenarios under CAA-08.
