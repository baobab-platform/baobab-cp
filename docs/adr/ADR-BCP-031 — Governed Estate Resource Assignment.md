# ADR-BCP-031 — Governed Estate Resource Assignment

**Status:** Proposed. This is the Control Plane's implementation record for ADR-SHARED-042 §14 Decision 9, covering CP registration, approval, revocation, trusted resolution and immutable attestation.
**Governing decision:** Shared ADR-SHARED-042 §14 Decision 9, the Platform Owner ruling of 23 October 2026. Where this record and ADR-SHARED-042 differ, ADR-SHARED-042 governs.
**Date:** 2026-10-23
**Canonical contracts:** `baobab-platform/shared` at `1517429bcae601a1b0288ca0279e524ce48a3d77`:
- `control-plane/v1/estate-resource-assignment.schema.json` and `.openapi.yaml`;
- `control-plane/v1/estate-commerce-profile.schema.json` (`GOVERNED_RESOURCE_ASSIGNMENT` provenance);
- four scopes, each registered and allocated to nobody:
  - `estate:resource-assignment:resolve`
  - `estate:resource-assignment:write`
  - `estate:resource-assignment:approve`
  - `trade:native-commitment:submit`

**Implementation state:** Source only. The feature sits behind a default-off switch (`ESTATE_RESOURCE_ASSIGNMENT_ENABLED`) and runs only in development and staging. The process exits if the switch is on in any other environment.

Nothing is allocated, registered or activated:
- no workload holds `estate:resource-assignment:resolve` or `trade:native-commitment:submit`;
- no human role holds `:write` or `:approve`;
- no assignment is registered;
- the Trade enforcement gate stays inactive.

There is no connected acceptance. Existing TA-08 evidence is a historical baseline only. This is not `REAL_COMPONENT_SYNTHETIC_DATA`, `PROTECTED_STAGING` or production acceptance (ADR-SHARED-042 §12). It does not satisfy ZB-G03 or ZB-G07 on its own.

---

## 1. Scope

The estate-client binding (ADR-BCP-029) proves the Digital Estate of a commitment only when it arrives through a governed BFF. Decision 9 requires the same proof for native commitments that do not come through a BFF:
- authenticated server applications;
- native operations, jobs, callbacks and replays.

If neither provenance exists once the fence is active, the commitment is refused. It never falls back to B2C, to implicit authority or to a weaker fence.

CP is the only registry for this. Trade neither keeps a second registry nor certifies itself.

## 2. The assignment record (migration 000132)

### `registry.estate_resource_assignment`

Each assignment binds exactly one subject:
- **WORKLOAD:** an IAM workload `client_id`. A Medusa publishable key is never a subject.
- **NATIVE_RESOURCE:** one Trade native seller configuration, by its reference and digest exactly as Trade attested it under Trade-0 (`registry.trade_native_configuration_attestation`).

The subject is bound to an estate, a Tenant, an engine instance, a Market scope, permitted commitment operations and an effective interval. The row also carries:
- the proposer and the approver, both `identity.principal`;
- proposal, approval and evidence references;
- a status and a revision.

### Guard trigger

The trigger enforces these rules on every write, whatever path it comes from:
- **Insert.** An insert must be PROPOSED at revision 1, by an ACTIVE human Principal. The Tenant must be the estate's single governed commerce-profile Tenant. An estate with no profile, or with profiles under two Tenants, cannot be assigned.
- **Immutable scope.** The subject, estate, Tenant, engine instance, Markets, operations, interval and proposer never change. Once set, the approver never changes either. A different scope is a new assignment.
- **Transitions.** Every update moves the revision up by exactly one. Each update must be backed by an immutable `estate_resource_assignment_transition` row, performed by an ACTIVE human, that this same transaction inserted. The row's `xmin` proves that, not its writer-supplied timestamp, so a transition committed ahead of time never counts. The allowed transitions are:
  - APPROVE: PROPOSED → ACTIVE
  - SUSPEND: ACTIVE → SUSPENDED
  - REINSTATE: SUSPENDED → ACTIVE
  - REVOKE: any status → REVOKED
- **Four-eyes.** The proposer can never perform APPROVE or REINSTATE. APPROVE records the performer as `approved_by`.
- **Terminal and permanent.** REVOKED is terminal. Rows are never deleted or truncated; all four tables carry delete and truncate guards.
- **No overlap.** Two ACTIVE assignments never cover the same subject, engine instance, operation and Market over overlapping intervals. This is checked under an advisory lock and surfaces as `ASSIGNMENT_OVERLAP`.

### Supporting tables

- **`estate_resource_assignment_command`** makes propose and transition idempotent. It is keyed by the actor and the `Idempotency-Key`. Reusing a key with a different request is refused. A replay answers with the exact result the command recorded, never the assignment's later state.
- **`estate_resource_assignment_resolution`** stores every resolve answer as immutable evidence. An ATTESTED answer, and only an ATTESTED answer, names the assignment, its revision and its Tenant, and expires no more than 60 s after it was resolved.

## 3. Resolution: `POST /internal/estate/v1/resource-assignments/resolve`

### Caller

The caller must be an admitted workload with `estate:resource-assignment:resolve`. The request names a scope, the subject's evidence, the estate, the engine instance, the Market and the operation. A request that asserts a Tenant, an assignment, a publishable key or any other authority fails schema validation.

### Verifying the subject

- **WORKLOAD.** The subject's access token is verified against the `baobab-trade` audience. It must be a workload token carrying `trade:native-commitment:submit`. It is then introspected, and the introspection must be active and for the same client. A missing scope, a human subject, an expired or forged token, a revoked token or another client's token gives `WORKLOAD_EVIDENCE_INVALID`. An issuer or introspection outage gives `RESOLUTION_UNAVAILABLE`.
- **NATIVE_RESOURCE.** The caller's latest Trade-0 attestation of that configuration on that engine instance must carry exactly this digest. Drift gives `NATIVE_RESOURCE_NOT_ATTESTED`. A candidate assignment counts only if it names the same digest.

### Selecting the assignment

Among the candidates for the subject and estate, checks run in this order:

| Order | Condition | Code |
|---|---|---|
| 1 | No candidate | `ASSIGNMENT_NOT_FOUND` |
| 2 | Another engine instance | `ENGINE_INSTANCE_NOT_PERMITTED` |
| 3 | Not ACTIVE or not yet effective | `ASSIGNMENT_NOT_ACTIVE` |
| 4 | Market outside scope | `MARKET_NOT_PERMITTED` |
| 5 | Operation not permitted | `OPERATION_NOT_PERMITTED` |
| 6 | More than one remains | `ASSIGNMENT_AMBIGUOUS` |

### The attestation

The attestation expires at the earliest of:
- 60 s after issue;
- the assignment's `effective_until`;
- the subject evidence's expiry.

It is validated against the Shared schema and recorded before it is returned. An attestation that cannot be recorded is never returned. Every denial is recorded too, and a denial names no assignment.

## 4. Administration (`/v1/estate/resource-assignments`)

- **Propose and read** need a human administrator with `estate:resource-assignment:write`.
- **APPROVE and REINSTATE** need `estate:resource-assignment:approve`.
- **SUSPEND and REVOKE** need `:write`.

The actor is always the caller's resolved canonical Principal, never a request field. The Tenant is derived and never accepted from the body. `SELF_APPROVAL`, `REVISION_CONFLICT`, `ASSIGNMENT_OVERLAP` and `ASSIGNMENT_TERMINAL` are problem codes.

## 5. Commerce profile provenance

`resolveEstateCommerceProfile` (ADR-BCP-030) now accepts `GOVERNED_RESOURCE_ASSIGNMENT` provenance. The reference must be the attestation id of an ATTESTED assignment resolution that meets all of these conditions:
- the same caller obtained it;
- it is for the same estate, engine instance and Market;
- it has not expired;
- its assignment is still ACTIVE at the attested revision.

CP re-verifies all of this from its own evidence. A suspension, reinstatement or revocation therefore invalidates any earlier attestation at once.

## 6. Evidence

- **`api/estate_resource_assignment_handler_test.go`** covers:
  - WORKLOAD and NATIVE_RESOURCE attestation;
  - lifetime caps;
  - every subject and assignment denial, each recorded without naming an assignment;
  - caller and request boundary refusals;
  - store and record outages;
  - routes that stay off when disabled or outside development and staging;
  - per-transition scopes.
- **`api/estate_commerce_profile_handler_test.go`** covers the assignment provenance cases.
- **`internal/store/postgres/estate_resource_assignment_test.go`** runs against PostgreSQL. It covers:
  - the ungoverned-estate refusal and idempotent replay;
  - self-approval, stale revision and illegal transitions;
  - direct-SQL bypass attempts: an ACTIVE insert, an untransitioned approval, a scope change and a delete;
  - overlap;
  - evidence constraints and immutability;
  - provenance by caller, Market and expiry, and revocation by revision;
  - terminal REVOKED.

## 7. Not done here

These steps follow the Decision 9 order. Each is a separate, reviewed step:
- **IAM:** a server-workload identity, independent of the Medusa key, issuing `trade:native-commitment:submit`.
- **Trade:** a verified native resource binding, the universal database fence behind an inactive enforcement gate, and a TA-08 rerun covering bypass, cross-tenant, revoked, replay, wrong-Market and conflicting-classification cases.
- **Activation:** this needs connected evidence, an approved scope allocation and rollback safeguards. The canonical registry activation hold stays in place.

## 8. Rollback

The switch defaults off. Turning it off unmounts every route. The migration only adds tables, functions and a trigger. Commerce-profile resolution with `ESTATE_CLIENT_BINDING` provenance is unchanged.
