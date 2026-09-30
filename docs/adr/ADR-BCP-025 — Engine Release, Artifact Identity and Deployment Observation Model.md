# ADR-BCP-025 — Engine Release, Artifact Identity and Deployment Observation Model

**Status:** Accepted — Normative Platform Architecture (2026-09-30, platform architecture owner). Accepted with amendments A1–A4, incorporated below.
**Date:** 2026-09-27 (proposed); 2026-09-30 (accepted)
**Repository:** `baobab-platform/baobab-cp`
**Depends on:** ADR-BCP-006, ADR-BCP-008, ADR-BCP-009, ADR-BCP-021, ADR-SHARED-007, ADR-SHARED-012, ADR-SHARED-017
**Refines:** ADR-BCP-006 §13 (engine contract compatibility), §59–61 (engine upgrade, instance replacement, rolling upgrade), §92–95 (topology reconciliation, infrastructure boundary, instance registration, attestation)
**Gate:** EA-03 (runtime topology)

## 1. Context

ADR-BCP-006 gives the Control Plane an engine topology:
- `Engine`, `CapabilityProvider` and `EngineInstance`;
- a time-bounded `HealthObservation`;
- the rule that the Control Plane declares desired topology, while infrastructure tooling provisions it and the Control Plane then "observes/reconciles it" (§92).

It says registration verifies a `version` (§94), that engine upgrades are distinct from provider migration (§59), and that instances may later attest their identity (§95). It does not say:
- what a version *is*;
- what makes it immutable;
- who reports what is actually running;
- how the Control Plane tells the running software from the software it expects.

Today that gap is concrete:
- `topology.engine_instance` has no version at all;
- the capability contract versions an engine supports are declared at registration (`EngineRegistration.contract_versions`) and never tied to the software actually deployed.

A binding can therefore resolve to an instance running software that no longer implements the contract version the binding requires (§12–13), and nothing notices.

A mutable tag such as `latest` or `v2` is not an identity. The same tag can name different software on different days, and the platform rule is already never to pin `latest`. Release identity must rest on content.

## 2. Decision

### 2.1 EngineRelease

An **EngineRelease** is the Control Plane's immutable record of one version of one engine. It contains:

| Field | Meaning |
|---|---|
| `release_id` | Control Plane identifier (`erl_…`). |
| `engine_id` | The engine it is a release of. |
| `release_version` | The engine's semantic version (`MAJOR.MINOR.PATCH`, optional pre-release). |
| `artifacts` | One or more artifacts (§2.2). There is at least one. |
| `provider_support` | For each provider the release contains, each capability it supports and the capability contract **major** versions, as integers (§2.1.1, §2.1.2). *(A1)* |
| `capability_provider_declaration_digest` | `sha256:` digest of the repository's `.baobab/capability-provider.yaml` at `source_revision` (§2.1.3). *(A2)* |
| `source_revision` | The source commit the release was built from (40-hex git SHA). |
| `provenance` | Optional reference to a build provenance attestation (§2.3). |
| `status` | `CANDIDATE`, `APPROVED`, `DEPRECATED` or `REVOKED` (§2.4). |
| `recorded_by`, `recorded_at`, `reason` | Who recorded it, when, and why. |

**Rules:**

1. **Immutable identity.** Once recorded, a release's `engine_id`, `release_version`, `artifacts`, `provider_support`, `capability_provider_declaration_digest` and `source_revision` never change. Only `status` moves, with an audited reason. Correcting a release means recording a new version.
2. **One identity per version.** Recording an existing `(engine_id, release_version)` again is accepted only if it is byte-identical, which makes it a replay. Different content is refused (`RELEASE_VERSION_CONFLICT`).
3. **One owner per digest.** An artifact digest belongs to at most one release of one engine. Recording it under another release is refused, so an observed digest always identifies a single release.
4. **Contract support is the release's, not the engine's.** A provider's supported contract versions are the union over the engine's releases that are `APPROVED` and deployed. §13 compatibility is checked against the release an instance runs (§2.6), not against the registration, and always for the binding's provider.


#### 2.1.1 Contract version representation

Compatibility needs one representation on both sides. Today:
- `EngineRegistration.contract_versions` and `ProviderMigrationRequest.capabilities[].contract_version` are integer **major** versions;
- a binding's `required_contract_version` is an integer in Shared, but the Control Plane stores it as text holding values such as `v1` and `1.0.0`.

This ADR fixes the representation: **a contract version is the capability contract's major version, a positive integer.** Minor and patch versions are backward-compatible by the capability contract rules, so they never decide compatibility.
- ER-01 declares the canonical form in Shared.
- ER-02 migrates stored binding versions to it. `v1`, `1` and `1.x.y` all normalise to `1`; a value that cannot be normalised is a data defect, reported and never guessed.
- Compatibility is set membership: the binding's major version is in the release's `provider_support` contract versions for the binding's provider and capability.

#### 2.1.2 Provider dimension of release support *(amendment A1)*

ADR-SHARED-017 places support on **provider + capability + contract major**, not on the engine. One engine release can contain several providers, for example `baobab-iam.keycloak` and `baobab-iam.ory`. A release therefore records its support per provider:

```text
EngineRelease
    └── provider_support[]
            ├── provider_key        (<engine-id>.<provider-name>)
            ├── capability_key      (a catalogued capability)
            └── contract_versions[] (majors)
```

Every `provider_key` belongs to the release's engine, and every `capability_key` is in the Shared catalogue. Only `IMPLEMENTED` declared support is recorded; `PARTIAL` and planned capabilities never are. EA-09 certification can then name provider, capability, contract major and release without reconstructing which providers a release contained.

#### 2.1.3 Declaration digest *(amendment A2)*

`capability_provider_declaration_digest` binds the release to the provider declaration it was built from: `sha256` over the exact bytes of `.baobab/capability-provider.yaml` at `source_revision`. `provider_support` must equal what that declaration's `IMPLEMENTED` support says. The release therefore proves what the source declared when it was built. It is never reinterpreted from a later `main`.
### 2.2 Artifact identity

An **artifact** is identified by content, never by name:

| Field | Meaning |
|---|---|
| `artifact_type` | `OCI_IMAGE` first. Other types are added by Shared contract change only. |
| `repository` | Where it is published, e.g. `registry.example/baobab/trade-engine`. This is a location, not an identity. |
| `digest` | `sha256:` followed by 64 lowercase hex characters. **This is the identity.** |
| `platform` | Optional, e.g. `linux/amd64`, for multi-platform releases. |

A tag may be recorded as `display_tag` for readability. It is never compared, resolved or trusted. A reference with a tag but no digest is refused.

### 2.3 Provenance

`provenance` optionally records:
- a reference to a build attestation, such as an in-toto/SLSA provenance statement, with its digest;
- the builder identity.

The Control Plane records and exposes the reference. It does not verify signatures itself: that stays with the build and admission tooling that owns keys and policy (ADR-BCP-006 §93).

A production environment MAY require `provenance` before a release can be `APPROVED`. That requirement is a Shared policy value, not code.

### 2.4 Release lifecycle

```text
CANDIDATE ──approve──▶ APPROVED ──deprecate──▶ DEPRECATED
    │                     │                         │
    └──────── revoke ─────┴────────── revoke ───────┘──▶ REVOKED
```

| Status | Meaning |
|---|---|
| `CANDIDATE` | Recorded, not yet approved for desired state. |
| `APPROVED` | This immutable release may be used in desired-state operations: it may be named as an instance's desired release (§2.5). Approval is a controlled mutation under ADR-BCP-021, and in production is subject to the maker/checker rules of ADR-BCP-020. It is **not** certification (A3). |
| `DEPRECATED` | May still run, but may no longer be newly desired. |
| `REVOKED` | Must not run: a security or correctness withdrawal. It is terminal. |

**Revocation and desired state.** Revoking a release never leaves it desired:
- Revocation is one transaction. Every instance whose `desired_release_id` names the revoked release gets a replacement desired release named in the revocation, or has its desired release cleared. Revocation is refused unless every such instance is covered.
- Reads of desired state also refuse to return a release that is not `APPROVED` or `DEPRECATED`. This is a second guard, so a desired pointer to a revoked release can never reach deployment tooling.

An instance observed running a `REVOKED` release is critical drift (§2.7).

**Approval is not certification** *(amendment A3)*. `APPROVED` says the artifact may be deployed. Whether a specific provider, capability and contract major in that release passed qualification is a separate record: EA-09's `ProviderCapabilityCertification(provider, capability, contract major, engine release, …)`. A release can be `APPROVED` while some of its provider–capability pairs are uncertified. Environment policy (a Shared policy value) decides whether desired-state eligibility or provider activation also requires certification. Neither status implies the other.

### 2.5 Desired release

Each `EngineInstance` gains a **desired release**, `desired_release_id`, set only through a controlled change (ADR-BCP-021). This is the "CP desired" half of §92:
- an engine upgrade (§59) is a change of desired release;
- instance replacement (§60) and rolling upgrade (§61) are infrastructure executions of it.

Only an `APPROVED` release may become desired.

The Control Plane never deploys anything. Infrastructure tooling reads the desired release (by API, or by event, §2.8) and executes it (§93).

### 2.6 DeploymentObservation

A **DeploymentObservation** is a time-bounded report of what is actually running on one engine instance. It follows the HealthObservation pattern:

| Field | Meaning |
|---|---|
| `observation_id` | `dob_…` |
| `engine_instance_id` | The instance observed. |
| `artifacts` | The artifact digests observed running, with `platform` where known. |
| `environment`, `region` | Where they were observed. Checked against the instance's own; a mismatch is drift, not an update. |
| `observed_at`, `expires_at` | As for health. Expired, missing or future-dated means **UNKNOWN**. |
| `recorded_at`, `ingestion_sequence` | Assigned by the Control Plane on acceptance; the ordering key for equal `observed_at`. |
| `source` | The reporting workload's canonical principal (§2.9). |

The Control Plane derives the **observed release** by resolving the observed digests. It is one of:
- **a release:** every digest belongs to one release of the instance's engine;
- **UNKNOWN_ARTIFACT:** a digest belongs to no recorded release;
- **FOREIGN_ARTIFACT:** a digest belongs to another engine's release;
- **MIXED:** the digests span several releases. This is expected briefly during a rolling upgrade, and is drift if it persists beyond policy.
- **UNKNOWN:** there is no current observation.

Observations are append-only; the current one is derived. The Control Plane stamps each accepted observation with `recorded_at` and a monotonically increasing `ingestion_sequence`, both assigned by the Control Plane and never by the reporter. The current observation is the one with the latest `observed_at`; at equal `observed_at`, the higher `ingestion_sequence` wins. Any replica or rebuilt projection therefore derives the same current observation.

### 2.7 Drift

The Control Plane compares desired against observed on every observation and on a periodic sweep. Drift uses the existing drift record contract (ADR-BCP-008), under a new drift object type `ENGINE_INSTANCE_RELEASE`. Reasons:

| Reason | When |
|---|---|
| `RELEASE_MISMATCH` | The observed release is not the desired release, past the rollout grace period. |
| `REVOKED_RELEASE_RUNNING` | The observed release is `REVOKED`. |
| `UNKNOWN_ARTIFACT_RUNNING` | UNKNOWN_ARTIFACT or FOREIGN_ARTIFACT. |
| `RELEASE_UNOBSERVED` | No current observation for an instance that has a desired release. |
| `DEPLOYMENT_LOCATION_MISMATCH` | The observed environment or region differs from the instance's. |

Grace periods and rollout windows are Shared policy values.

### 2.8 Consequences for resolution and readiness

In phases:

1. **Observe (this ADR's first gates).**
   - Drift and readiness only.
   - `ENGINE_INSTANCE_RELEASE` drift appears in provisioning readiness for the tenants bound to the instance (ADR-BCP-006 §67 reverse impact).
   - Resolution is unchanged.
2. **Enforce compatibility (a later gate, decided on phase 1 evidence).**
   - A binding is ineligible when its instance's observed release does not support the binding's `required_contract_version`.
   - A `REVOKED` or unknown artifact makes the instance ineligible for `CRITICAL` capabilities, like `UNKNOWN` health under ADR-BCP-006 §22.
   - This step changes resolution, so it needs its own acceptance, like the health criticality policy did.

### 2.9 Who reports, and how

1. Observations come from **infrastructure tooling**: the deploy controller or admission webhook of the environment. They never come from the engine describing itself. An engine cannot self-report its release, just as it cannot self-grant (§97) or self-assign a tenant (§98).
2. Reporters authenticate with **workload identity only**: a registered workload with a dedicated scope (`deployment:observe`). No static secrets.
3. A reporter is registered for the environments and regions it may report on. An observation outside them is refused, never stored.
4. Engines never touch the Control Plane database. The Control Plane never calls cluster APIs (§93).

### 2.10 Events and metrics

Events are in the `com.baobab-platform` namespace, through the transactional outbox:
- `com.baobab-platform.control-plane.engine-release.recorded.v1`;
- `com.baobab-platform.control-plane.engine-release.status-changed.v1`;
- `com.baobab-platform.control-plane.engine-instance.desired-release-changed.v1`, which infrastructure tooling may consume instead of polling;
- `com.baobab-platform.control-plane.engine-instance.release-drift-detected.v1`.

Each is registered in the Shared event registry with its payload schema before it is emitted (ER-01), following the envelope's versioned reverse-DNS naming.

Metrics use bounded labels only:
- the count of instances by `release_status` and `drift_reason`;
- the observation age;
- no digest, version, instance or tenant labels.

### 2.11 Relationship to ProviderMigration

These are separate concepts:
- **Release change:** a change of desired release on an instance, with the same provider and the same engine (§59).
- **ProviderMigration:** a change of the provider behind a capability (§44–52).

A ProviderMigration's plan (§120) may name the target provider's instances and require their observed release to support the migrated contract versions before the shift stage. It reads release state; it never changes it.

## 3. Contracts

Shared owns the contracts; the Control Plane implements them:

- `contracts/topology/v1/`, a new domain:
  - `release.schema.json`: EngineRelease (with `provider_support` and `capability_provider_declaration_digest`), Artifact, Provenance, the lifecycle enum;
  - `deployment-observation.schema.json`: DeploymentObservation and the derived observed release;
  - `release-policy.yaml`: grace periods, observation TTL bounds, production provenance requirement;
  - `openapi` routes for recording and reading releases, setting the desired release (through the changeset API once ADR-BCP-021 lands), and submitting and reading observations;
  - events and the metric catalogue.
- Additions elsewhere:
  - `engineReleaseId` and `deploymentObservationId` in `control-plane/v1/domain.schema.json`;
  - the `ENGINE_INSTANCE_RELEASE` drift object type in `drift.schema.json`;
  - the `deployment:observe` scope in the scope registry;
  - reason codes in the reason-code registry.

## 4. Gates

| Gate | Scope |
|---|---|
| ER-01 | Shared `topology/v1` contracts, validator and CI. |
| ER-02 | Control Plane persistence of releases, with immutability enforced in the database as well as in code; the record and read API. |
| ER-03 | Desired release on EngineInstance through a controlled change. |
| ER-04 | DeploymentObservation intake from registered reporters under workload identity; observed release derivation. |
| ER-05 | Drift and readiness integration; events; metrics. |
| ER-06 | Resolution compatibility enforcement (§2.8 phase 2). **Not accepted by this ADR** *(amendment A4)*: it needs its own explicit activation decision on phase-1 evidence. |

Acceptance authorises ER-01 to ER-05. Observation stays observation: nothing in ER-01 to ER-05 changes capability resolution. Turning an observed incompatibility into routing exclusion (ER-06) is a materially different operational consequence and is decided separately *(A4)*.

## 5. Non-goals

- The Control Plane is not a CD system. It never builds, pushes, deploys or rolls back software.
- The Control Plane does not verify signatures or scan images. It records references to what the tooling that does verified.
- A tag, branch or "latest" is never release identity.
- No per-tenant release pinning. Tenants reach releases only through the instances their bindings name.

## 6. Rejected alternatives

- **A version string on EngineInstance (§116 `software_version`).**
  - It is mutable and not content-addressed.
  - It cannot tell a rebuilt image from the original.
  - It says nothing about contract support.
- **Engines self-report their version through health probes.**
  - The engine would be attesting to itself, which §97–98 forbid in spirit.
  - A compromised engine could hide its release.
- **The Control Plane pulls state from cluster APIs.**
  - This breaks the infrastructure boundary (§93).
  - The Control Plane would need cluster credentials.
- **Tag plus digest as a composite identity.** The tag adds nothing trustworthy and invites comparing tags.
- **Engine-level contract versions** (the original `contract_versions` per capability). Superseded by A1: it cannot tell which of an engine's providers supports a contract, and EA-09 certification needs that.

## 7. Consequences

- **Engine upgrades become auditable.** It is recorded who approved which content, when it was desired, and when it was observed running.
- **Contract compatibility (§12–13) becomes checkable** against what actually runs, not what was once declared.
- **Infrastructure tooling gets one declarative input** (desired release) and one output (observations). Neither needs Control Plane database access or cluster credentials in the Control Plane.
- **New operational dependency.** Environments must run a reporter. Until they do, instances show `RELEASE_UNOBSERVED`, which is honest rather than silent.
