# ADR-BCP-029 — Estate Client Binding Resolution

**Status:** Proposed — the Control Plane's implementation record for ADR-SHARED-042 work package CAA-04  
**Governing decision:** Shared ADR-SHARED-042 §14, Platform Owner ruling of 22 October 2026. Where this record and ADR-SHARED-042 differ, ADR-SHARED-042 governs.  
**Date:** 2026-10-22  
**Canonical contracts:** `baobab-platform/shared`:
- `control-plane/v1/estate-client-binding.schema.json` and `.openapi.yaml`;
- `identity/v1/buyer-identity-handoff` v1.1 (`BffCallerTokenClaims`).

**Implementation state:** Source only, behind a default-off switch, in development and staging only. No workload holds `estate:client-binding:resolve`, no binding is registered, and there is no connected acceptance. This is not `REAL_COMPONENT_SYNTHETIC_DATA`, `PROTECTED_STAGING` or production acceptance (ADR-SHARED-042 §12).

---

## 1. Scope

A buyer token's `azp` names the OAuth client of the confidential estate BFF that obtained it. It never establishes which Digital Estate, Tenant, environment or Trade instance a request belongs to.

CP is the authority for that relationship. Under this record, CP attests it for one buyer request, from the BFF's own authenticated evidence.

## 2. The binding record — `registry.estate_client_binding` (migration 000129)

**Contents.** One governed row per binding, holding:
- the issuer and `bff_client_id`;
- the `digital_estate_id`, the estate's `tenant_id`, and the permitted Trade `engine_instance_id`;
- the `environment` and `deployment_reference`;
- the status (ACTIVE, SUSPENDED or REVOKED), a revision, an effective interval and an `evidence_reference` for the approval.

**Guards.** A trigger enforces the governance rules:
- the scope columns are immutable;
- every change increments the revision by exactly one;
- REVOKED is terminal;
- nothing is deleted.

The full scope tuple is unique, so a binding cannot be registered twice.

**Identifiers.** `digital_estate_id` follows the Shared `estateId` grammar (`resourceId`). The legacy `estate.digital_estate` table keys estates by UUID; reconciling the two is outside this record.

**Writes.** Bindings change only through the reviewed CP registry process. This increment adds no write API. Bindings are registered by a reviewed migration or operator change until a governed command exists.

## 3. Resolution — `POST /internal/estate/v1/client-bindings/resolve`

CP checks, in order:

1. **Caller.** The caller is an ACTIVE workload whose registry entry allows `estate:client-binding:resolve` and lists `baobab-trade` in `subject_principal_audiences`.
2. **Request shape.** The body is the closed, purpose-bound (`BUYER_OPERATION`) request schema, and its `correlation_id` must equal `X-Correlation-ID`. The caller cannot name a tenant, binding, deployment or revision. Validation errors never quote the token.
3. **The BFF's evidence.** CP verifies the client evidence (the BFF's own IAM workload token) against the configured buyer issuer for audience `baobab-trade`. It must be a workload token carrying `trade:buyer-handoff`, from a client on `BUYER_SUBJECT_AUTHORISED_PARTIES`, with a token ID, and unexpired. Otherwise the request is `CLIENT_EVIDENCE_INVALID`.
4. **Same client as the buyer token.** The client must equal the buyer token's `azp` as Trade verified it (`subject_authorised_party`). Otherwise the request is `CLIENT_NOT_FOR_SUBJECT`, so a stolen buyer token cannot be presented by another client.
5. **Current status.** RFC 7662 introspection at the issuer must confirm the evidence is active, for the same client, subject, audience and scope. An outage is `RESOLUTION_UNAVAILABLE`.
6. **Environment.** The stated environment must equal CP's own (`ENVIRONMENT_MISMATCH`).
7. **Selection.** Only after steps 1–6 are bindings read, for the exact (issuer, client). They are filtered in this order:

   | Filter | Denial when nothing is left |
   | --- | --- |
   | Optional estate narrowing | `BINDING_NOT_FOUND` |
   | Environment | `ENVIRONMENT_MISMATCH` |
   | Engine instance | `ENGINE_INSTANCE_NOT_PERMITTED` |
   | ACTIVE and in effect | `BINDING_NOT_ACTIVE` |

   More than one remaining binding is `BINDING_AMBIGUOUS` (409). It is never resolved by order.
8. **Attestation.** It lasts at most 60 seconds and never outlives the client evidence. It is validated against the Shared schema before it is returned.

Every answer is appended to the immutable `registry.estate_client_binding_resolution`; only an ATTESTED answer names a binding. An attestation that cannot be recorded is not returned.

## 4. Configuration

| Variable | Meaning |
| --- | --- |
| `ESTATE_CLIENT_BINDING_ENABLED` | Mounts the route. CP refuses to start if it is set outside `development` or `staging` |
| `BUYER_SUBJECT_ISSUER`, `BUYER_SUBJECT_INTROSPECTION_*`, `BUYER_SUBJECT_AUTHORISED_PARTIES` | Shared with ADR-BCP-028. Incomplete configuration refuses to start |

## 5. Outstanding before activation

- **Registry allocation:** granting the Trade workload `estate:client-binding:resolve` through the reviewed Shared registry process.
- **IAM:** issuing `trade:buyer-handoff` workload tokens to the ZuriBeans BFF, and registering the BFF client (CAA-03).
- **Governed write path:** a command for creating and changing bindings.
- **Real-component acceptance:** wrong estate or environment, revoked binding, ambiguous binding (CAA-08).
