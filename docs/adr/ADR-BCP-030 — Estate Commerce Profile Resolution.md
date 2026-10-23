# ADR-BCP-030 — Estate Commerce Profile Resolution

**Status:** Proposed — the Control Plane's implementation record for ADR-SHARED-042 §14 Decision 8 (CP registry and attestation step)  
**Governing decision:** Shared ADR-SHARED-042 §14 Decision 8, Platform Owner ruling of 22 October 2026. Where this record and ADR-SHARED-042 differ, ADR-SHARED-042 governs.  
**Date:** 2026-10-22  
**Canonical contracts:** `baobab-platform/shared` at `742295e2b4f67a3de6d985e7da6bd89ed0be39cb`:
- `control-plane/v1/estate-commerce-profile.schema.json` and `.openapi.yaml`;
- scope `estate:commerce-profile:resolve` (registered, unallocated).

**Implementation state:** Source only, behind a default-off switch, in development and staging only. No workload holds `estate:commerce-profile:resolve`, and no profile is registered: the intended ZuriBeans (B2B) and Thamani (B2C) profiles are governed CP onboarding steps, not part of this change. There is no connected acceptance. This is not `REAL_COMPONENT_SYNTHETIC_DATA`, `PROTECTED_STAGING` or production acceptance (ADR-SHARED-042 §12), and it does not satisfy ZB-G03 or ZB-G07 on its own.

---

## 1. Scope

Trade must classify every native commitment as B2B or B2C (Decision 6). A missing B2B classification denies and never falls through to B2C. The classification must come from trusted, persistent evidence. Sales-channel names, publishable keys and storefront configuration do not count.

CP is the sole owner of that evidence: the governed **EstateCommerceProfile**. One profile fixes the commerce model of one Digital Estate, on one Trade engine instance, for a set of Markets. There is no MIXED model and no default.

Under this record, CP attests the profile read-only. It does not decide Trade's classification. Trade owns the immutable NativeCommitmentClassification and must agree with CP. The attestation classifies the estate only. It is never seller, buyer, Market or purchasing authority.

## 2. The profile record — `registry.estate_commerce_profile` (migration 000131)

**Contents.** One governed row per profile, holding:
- the `digital_estate_id` (Shared `resourceId` grammar), the estate's `tenant_id` and the `trade_engine_instance_id`;
- the `commerce_model` (`B2B` or `B2C`) and the `market_scope` (one or more distinct canonical Market ids);
- the status (ACTIVE, SUSPENDED or REVOKED), a revision and an effective interval;
- an optional `supersedes` link, a mandatory `approval_reference` and an `evidence_reference`.

**Guards.** A trigger enforces the governance rules:
- the scope (estate, Tenant, engine instance, model, Markets, supersedes) is immutable, so a change of model is a new, superseding profile;
- a profile starts at revision 1, and every change increments it by exactly one;
- REVOKED is terminal;
- nothing is deleted or truncated;
- at most one ACTIVE profile covers any (estate, engine instance, Market) over an overlapping effective interval. The check runs under a per-estate advisory lock, so two concurrent activations cannot both pass.

**Writes.** Profiles change only through the reviewed CP registry process. This increment adds no write API.

## 3. Resolution — `POST /internal/estate/v1/commerce-profiles/resolve`

CP checks, in order:

1. **Caller.** The caller is an ACTIVE workload whose registry entry allows `estate:commerce-profile:resolve`.
2. **Request shape.** The body is the closed Shared request schema, and its `correlation_id` must equal `X-Correlation-ID`. The caller names the estate, engine instance and Market. It cannot name a Tenant, model, profile or revision.
3. **Estate provenance.** CP never classifies an estate the caller has not shown it is acting for. Two kinds of provenance are defined:
   - **`ESTATE_CLIENT_BINDING`** (for BFF flows). CP accepts it only if its own record holds an ATTESTED estate client binding resolution (ADR-BCP-029) with all of the following:
     - requested by the same workload;
     - for the referenced binding, which is ACTIVE at the attested revision;
     - for the same estate and engine instance;
     - made within the last 60 seconds.
   - **`GOVERNED_RESOURCE_ASSIGNMENT`** (for non-BFF flows). No governed assignment registry exists yet, so CP refuses this kind rather than trusting it.

   Both refusals are `ESTATE_PROVENANCE_INVALID`.
4. **Selection.** Only after provenance is verified are the estate's profiles read. They are filtered in this order:

   | Filter | Denial when nothing is left |
   | --- | --- |
   | Estate has any profile | `PROFILE_NOT_REGISTERED` (404) |
   | Engine instance | `ENGINE_INSTANCE_NOT_PERMITTED` (403) |
   | Market in scope | `PROFILE_NOT_REGISTERED` (404) |
   | ACTIVE and in effect | `PROFILE_NOT_ACTIVE` (403) |

   More than one remaining profile is `PROFILE_AMBIGUOUS` (409). It is never resolved by order.
5. **Attestation.**
   - It names the profile, revision, model, Tenant and Market.
   - It expires at the earlier of 60 seconds and the profile's `effective_until`.
   - It is validated against the Shared schema before it is returned.
   - It is `private, no-store`.

**Unavailability.** A store or provenance outage is `RESOLUTION_UNAVAILABLE` (503). Trade must treat it as UNAVAILABLE and deny, never as B2C.

**Evidence.** Every answer is appended to the immutable `registry.estate_commerce_profile_resolution`. The database requires an ATTESTED answer to name its profile, revision, model, attestation id and an expiry after the resolution time. It requires every other outcome to name none of them. An attestation that cannot be recorded is not returned.

## 4. Configuration

| Variable | Meaning |
| --- | --- |
| `ESTATE_COMMERCE_PROFILE_ENABLED` | Mounts the route. CP refuses to start if it is set outside `development` or `staging`, or without `ESTATE_CLIENT_BINDING_ENABLED` (provenance depends on ADR-BCP-029) |

## 5. Outstanding before activation

- **Registry allocation:** granting the Trade workload `estate:commerce-profile:resolve` through the reviewed Shared registry process. The canonical registry activation hold remains in place.
- **Governed onboarding:** registering the ZuriBeans B2B and Thamani B2C profiles, with real approval references, through the reviewed CP process.
- **Governed write path:** a command for creating, suspending, superseding and revoking profiles.
- **Resource assignment:** a governed registry for non-BFF provenance (jobs, workflows, callbacks).
- **Market mapping:** reconciling `market_scope` ids with CP's Market catalogue.
- **Trade:** the NativeCommitmentClassification and the CAA-06 commit fence that consumes this attestation.
- **Real-component acceptance:** suspended, revoked or superseded profiles, CP and Trade disagreement, and CP unavailability (CAA-08).
