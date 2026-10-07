# Changelog

> **Strong Roots. Inspired Growth.**

This document records the notable changes made to BAOBAB throughout its development.

The changelog provides a clear and concise history of the platform's evolution, enabling users, contributors, maintainers, and other stakeholders to understand what has changed between releases and why those changes matter.

BAOBAB follows the principles of **Keep a Changelog** while adopting **Semantic Versioning (SemVer)** to ensure releases are communicated consistently and transparently.

---

# Purpose

The objectives of this changelog are to:

- Document significant changes introduced in each release.
- Improve transparency between releases.
- Help users understand the impact of upgrading.
- Provide a historical record of the platform's evolution.
- Support release planning and maintenance.
- Promote consistent release documentation across the project.

Only changes that are meaningful to users, contributors, maintainers, or system operators should be included.

---

# Release Philosophy

BAOBAB treats every release as an engineering milestone rather than simply a collection of commits.

Each release should:

- Deliver measurable value.
- Maintain platform stability.
- Preserve backwards compatibility wherever practical.
- Clearly communicate breaking changes.
- Highlight security-related improvements.
- Provide sufficient context for users to understand the release.

A release should tell the story of how the platform has evolved—not merely list technical changes.

---

# Versioning Policy

BAOBAB follows **Semantic Versioning (SemVer)**.

Version numbers use the format:

```text
MAJOR.MINOR.PATCH
```

For example:

```text
1.4.2
```

Where:

| Component | Description |
|-----------|-------------|
| **MAJOR** | Introduces incompatible or breaking changes. |
| **MINOR** | Adds new functionality while maintaining backwards compatibility. |
| **PATCH** | Delivers backwards-compatible bug fixes and maintenance improvements. |

Examples:

| Version | Meaning |
|---------|---------|
| `0.1.0` | Initial development release. |
| `0.2.0` | New functionality added. |
| `0.2.3` | Maintenance release with bug fixes. |
| `1.0.0` | First stable production release. |
| `2.0.0` | Major release containing breaking changes. |

---

# Pre-release Versions

Before reaching a stable production release, BAOBAB may publish pre-release versions.

Examples include:

| Identifier | Purpose |
|------------|---------|
| `alpha` | Early development builds with active feature development. |
| `beta` | Feature-complete builds intended for wider evaluation and testing. |
| `rc` (Release Candidate) | Final validation before a stable release. |

Examples:

```text
0.1.0-alpha

0.5.0-beta

0.9.0-rc.1
```

Pre-release versions should not be considered production-ready unless explicitly stated.

---

# Release Principles

Every release should strive to be:

## Predictable

Users should understand what to expect from a release based on its version number.

---

## Transparent

Important changes should be documented clearly and honestly.

---

## Reliable

Releases should represent stable, tested, and review-approved milestones.

---

## Traceable

Changes should be linked to Pull Requests, Issues, discussions, or other relevant project records where appropriate.

---

## Reproducible

Each tagged release should correspond to a specific state of the repository and be reproducible through the documented build process.

---

# What Belongs in the Changelog

The changelog should include notable changes such as:

- New features.
- User-visible improvements.
- Significant architectural changes.
- Security fixes.
- Breaking changes.
- Performance improvements.
- Deprecations.
- Removed functionality.
- Operational changes affecting deployment or configuration.

Entries should be concise, factual, and written from the perspective of someone using or maintaining the platform.

---

# What Does Not Belong

The changelog is **not** intended to document every repository activity.

The following should generally be excluded unless they have broader significance:

- Individual commits.
- Minor formatting changes.
- Routine code refactoring with no observable impact.
- Temporary experimental work.
- Internal development notes.
- Routine dependency updates that have no functional or security impact.
- Minor documentation corrections.

Keeping the changelog focused improves readability and usefulness.

---

# Relationship to Other Documents

This changelog complements the following governance documents:

- `README.md`
- `CONTRIBUTING.md`
- `SECURITY.md`
- `CODE_OF_CONDUCT.md`

Together, these documents support BAOBAB's commitment to transparency, quality, and sustainable software engineering.

---

---

# Changelog Structure

Every release should follow a consistent structure to improve readability and make it easier for users, contributors, and maintainers to understand what has changed.

Each release entry should begin with:

- Version number
- Release status (where applicable)
- Release date
- Brief release summary

For example:

```markdown
## [1.2.0] - 2027-03-15

### Summary

This release introduces multi-tenant reporting, improves API performance, and strengthens authentication controls.
```

A concise summary helps readers quickly understand the focus of the release.

---

# Release Categories

BAOBAB adopts the categories recommended by **Keep a Changelog**.

Only categories that contain entries need to appear in a release.

## Added

New functionality introduced in the release.

Examples include:

- New platform features.
- New APIs.
- New integrations.
- New documentation.
- New infrastructure capabilities.

---

## Changed

Modifications to existing functionality.

Examples include:

- Behaviour improvements.
- User experience enhancements.
- Architecture refinements.
- Performance improvements.
- Configuration updates.

---

## Deprecated

Features or functionality that remain available but are scheduled for removal in a future release.

Deprecation entries should include:

- What is being deprecated.
- Why it is being deprecated.
- Recommended replacement.
- Expected removal version where known.

---

## Removed

Functionality that has been permanently removed.

Entries should clearly explain:

- What was removed.
- Why it was removed.
- Migration guidance where appropriate.

---

## Fixed

Corrections to defects affecting functionality, reliability, or usability.

Examples include:

- Bug fixes.
- Stability improvements.
- Compatibility fixes.
- Error handling improvements.

---

## Security

Security-related improvements that users should be aware of.

Examples include:

- Vulnerability remediation.
- Authentication improvements.
- Authorisation enhancements.
- Dependency security updates.
- Encryption improvements.
- Security hardening.

Where appropriate, reference related security advisories.

---

# Writing Guidelines

Release notes should be written from the perspective of users and maintainers rather than individual developers.

Entries should be:

- Clear.
- Concise.
- Accurate.
- Objective.
- User-focused.
- Written in plain language.

Prefer:

> Improved authentication reliability and simplified session management.

Instead of:

> Refactored authentication middleware.

The emphasis should be on the value delivered rather than the implementation details.

---

# Writing Style

Each entry should:

- Begin with an action verb where practical.
- Describe the outcome before the implementation.
- Avoid unnecessary technical jargon.
- Explain breaking changes clearly.
- Highlight operational impact where relevant.

Good examples:

- Added support for multi-tenant reporting.
- Improved API response performance.
- Fixed an issue affecting tenant authentication.
- Strengthened password validation.
- Updated deployment documentation.

Avoid vague descriptions such as:

- Miscellaneous fixes.
- Various improvements.
- Minor updates.
- General cleanup.

Every entry should communicate meaningful information.

---

# Referencing Project Records

Where practical, release entries should reference supporting project records such as:

- GitHub Issues
- Pull Requests
- Security Advisories
- Architectural Decision Records (ADRs)
- Documentation updates

These references improve traceability and provide additional technical context for contributors and maintainers.

---

# Unreleased Section

The changelog should always begin with an **Unreleased** section.

This section captures notable changes that have been merged into the main development branch but have not yet been included in a tagged release.

Example:

```markdown
## [Unreleased]

### Added

- Initial support for tenant-aware reporting.

### Changed

- Improved deployment workflow for container builds.

### Fixed

- Corrected validation of tenant configuration settings.
```

Once a release is published, the entries from the **Unreleased** section should be moved into the corresponding versioned release.

---

# Maintaining the Changelog

Maintainers should update the changelog as part of the release preparation process.

Contributors are encouraged to include proposed changelog entries in Pull Requests whenever they introduce changes that are:

- User-visible.
- Operationally significant.
- Security-related.
- Architecturally important.

Reviewers should ensure that changelog entries accurately reflect the completed work before approving a release.

---

# Consistency Across Releases

To maintain a professional and predictable release history:

- Use the same section order for every release.
- Keep language consistent.
- Avoid duplicate entries.
- Group related changes together.
- Record only completed work.
- Verify accuracy before publication.

Consistency improves readability and helps users compare releases over time.

---

---

# Release Lifecycle

Every BAOBAB release progresses through a structured lifecycle designed to promote quality, stability, and transparency.

```text
Planning
    │
    ▼
Development
    │
    ▼
Alpha Release
    │
    ▼
Beta Release
    │
    ▼
Release Candidate
    │
    ▼
Stable Release
    │
    ▼
Maintenance
    │
    ▼
End of Support
```

Each stage serves a distinct purpose and should be completed before progressing to the next.

---

# Release Stages

## Planning

During the planning stage, the scope of the release is defined.

Typical activities include:

- Defining release objectives.
- Prioritising features and improvements.
- Identifying breaking changes.
- Assessing technical risks.
- Updating the project roadmap.

Planning establishes clear expectations for the release.

---

## Development

Features, enhancements, fixes, documentation, and infrastructure updates are implemented during active development.

Development activities should follow the project's:

- Coding Standards
- Testing Standards
- Documentation Standards
- Security Policy
- Contribution Guidelines

Only completed and reviewed work should be considered for release.

---

## Alpha Releases

Alpha releases represent early development milestones.

They may include:

- Experimental functionality.
- Incomplete features.
- Architectural changes.
- Early platform capabilities.

Alpha releases are intended primarily for project contributors and early adopters.

Example:

```text
v0.1.0-alpha
```

---

## Beta Releases

Beta releases indicate that planned functionality is substantially complete.

The focus shifts towards:

- Stability.
- Integration testing.
- Performance improvements.
- Documentation.
- User feedback.

Breaking changes should become less frequent during the beta phase.

Example:

```text
v0.5.0-beta
```

---

## Release Candidates (RC)

Release Candidates are intended to become the next stable release unless significant issues are discovered.

Typical activities include:

- Final regression testing.
- Security verification.
- Documentation review.
- Performance validation.
- Release approval.

Example:

```text
v0.9.0-rc.1
```

---

## Stable Releases

Stable releases are recommended for production environments.

Before publication, stable releases should satisfy the project's quality expectations, including:

- Successful automated tests.
- Security validation.
- Documentation updates.
- Approved release notes.
- Review by project maintainers.

Example:

```text
v1.0.0
```

---

## Maintenance Releases

Maintenance releases provide improvements without introducing significant new functionality.

Examples include:

- Bug fixes.
- Security updates.
- Dependency updates.
- Performance improvements.
- Compatibility enhancements.

Maintenance releases generally increment the PATCH version number.

Example:

```text
v1.0.3
```

---

# Release Governance

Every release should follow an appropriate review and approval process.

Typical responsibilities include:

| Role | Responsibility |
|------|----------------|
| Contributors | Implement approved changes and update documentation where required. |
| Reviewers | Review code quality, testing, documentation, and architectural consistency. |
| Maintainers | Approve releases, validate readiness, and coordinate publication. |
| Security Reviewers | Assess security-sensitive changes where applicable. |

Releases represent collective engineering decisions rather than individual contributions.

---

# Release Readiness Checklist

Before publishing a release, maintainers should confirm that:

- All planned work has been completed or intentionally deferred.
- Automated tests have passed.
- Critical defects have been resolved.
- Security reviews have been completed where appropriate.
- Documentation has been updated.
- The changelog has been reviewed.
- Version numbers have been updated consistently.
- Release artefacts have been successfully generated.
- Deployment procedures have been validated.

Completing this checklist helps ensure reliable and predictable releases.

---

# Version Support Policy

BAOBAB aims to provide clear guidance regarding supported versions.

In general:

- The latest stable release receives full support.
- Security updates are prioritised for supported releases.
- Older releases may reach End of Support (EoS) as the platform evolves.

Support expectations may vary depending on project maturity and available maintenance resources.

---

# End of Support (EoS)

A release may reach End of Support when:

- A newer supported version becomes available.
- Continued maintenance is no longer practical.
- Critical dependencies are no longer supported.
- Significant architectural changes require migration.

Where practical, users will be encouraged to upgrade to a supported version.

---

# Release Communication

Each published release should include:

- Version number.
- Release date.
- Summary of notable changes.
- Upgrade guidance where required.
- Known limitations (if applicable).
- Links to relevant documentation.

Clear communication enables users to adopt new releases with confidence.

---

# Continuous Improvement

The release process will continue to evolve as BAOBAB matures.

Feedback from contributors, maintainers, and users will help improve:

- Release planning.
- Automation.
- Quality assurance.
- Documentation.
- Deployment processes.
- Governance practices.

Continuous refinement ensures that BAOBAB's release management remains efficient, transparent, and aligned with industry best practices.

---

---

# Official Release Record

This changelog serves as BAOBAB's official record of released versions.

Each release documents the most significant changes introduced to the platform and should be considered the authoritative summary of release milestones.

Detailed implementation history remains available through:

- Git history
- Pull Requests
- Issues
- GitHub Releases
- Architectural Decision Records (ADRs)

The changelog focuses on changes that are meaningful to users, contributors, maintainers, and system operators.

---

# Unreleased

> The following entries represent completed work that has been merged into the main development branch but has not yet been included in an official tagged release.

## Added

- **Pre-activation provisioning context** (Shared control-plane/v1 1.34.0, erp/v1 1.2.0; pin `05db746`; `docs/architecture/context-authority-for-workloads.md` section 13, owner ruling 2026-10-07). The canonical lifecycle activates a tenant only after provider provisioning and readiness (Technical Specification section 22; ADR-BCP-017 sections 22 and 45), so the ERP provisioning that justifies activation could not be authorised by a context that requires an ACTIVE tenant. Activation is not reordered and the ACTIVE rule is not relaxed for ordinary contexts; instead every stored context states its purpose.
  - **Storage** (migration `000094`): `authority_purpose` (`RUNTIME` by default, or `TENANT_PROVISIONING`) and `provisioning_authority` (the approved plan tuple). The database itself refuses a provisioning context that is unbounded, longer than 15 minutes or without its tuple, a RUNTIME context carrying one, and any later change of purpose, tuple, tenant or owner (a resolved context is immutable, ADR-BCP-004 section 71); deleting for tenant invalidation still works. `domain.Context.Validate` enforces the same, and also that a provisioning context carries no business dimension.
  - **Validation** (`POST /v1/platform-context/validate`): the response always states `authority_purpose`, with `provisioning_authority` exactly for a provisioning context. A RUNTIME context is unchanged (tenant must be ACTIVE). A provisioning context is valid only while all hold, judged from authoritative state on every call (ADR-BCP-009 section 61): its owner's workload is **ACTIVE** in the registry and lists `TENANT_PROVISIONING` in the new `context_purposes` (otherwise answered like an unknown context); the tenant is pending, provisioning or active (suspended, decommissioning, decommissioned or unknown is `TENANT_NOT_ACTIVE`); the TenantProvisioning it names belongs to that tenant and is in `PROVISIONING_PROVIDERS`, `VERIFYING_READINESS` or `REMEDIATING`; and it has an approved, current plan whose id, version and digest equal the context's (`403 PROVISIONING_AUTHORITY_NOT_CURRENT` otherwise). "Approved, current plan" is one function (`approvedPlanProblem`) shared with the ERP assignment projection, so ERP's assignment read and this validation cannot disagree. Without the registry and provisioning sources configured a provisioning context is simply never valid.
  - **Never runtime authority:** capability resolution, batch resolution, mapping resolution and capability explanation read contexts through `runtimeOnly`, so a provisioning context is as absent to them as an unknown one; a test pins that no consumer other than the validator reads the unfiltered store.
  - **Issuer:** `internal/erpprovisioning.ContextIssuer` now records a `TENANT_PROVISIONING` context bound to the approved tuple (TTL at most 15 minutes).
  - **Registry:** `context_purposes` is parsed; an unknown purpose refuses the snapshot; purposes are honoured only for ACTIVE workloads, so while `baobab-cp-provisioning-workload` is PROVISIONED the Control Plane honours none of it.
  - **Re-pin** Shared `363e0ea` -> `05db746`: the catalogue gains `inventory.availability.query` (ERP), the Console client is regenerated, and the duplicate `source-map-js` override left by two concurrent security fixes is removed.
  - **Not included:** no scope allocation, no activation, the provisioning worker is still unwired, and ERP does not yet consume the new response members (it must re-pin before this is deployed).

- ERP provisioning now resolves the Finance baseline from ERP instead of stopping at the missing one (Shared erp/v1 1.3.0, re-pinned `dcd01ab` -> `78b4e5e`; FB-03). **Deployment gate: the ERP release containing baobab-erp `3433036` must be deployed before this Control Plane release**, because ERP rejects the old request shape and the new one is rejected by an older ERP.
- ERP provisioning records the Finance baseline references it will send as a durable, immutable intent (migration `000098`) before the request is first sent, and a retry for an approved plan continues the recorded operation or resends the recorded intent. A baseline superseded between a crash and its retry can no longer produce other references under another idempotency key, or an orphaned second ERP operation (follow-up to FB-03).
  - **Resolution.** For every legal entity of the frozen approved plan the worker calls ERP's `getEffectiveFinanceBaseline` as the provisioner, under the same `TENANT_PROVISIONING` context bound to the approved plan tuple. The reference and `functional_currency` ERP answers are taken verbatim: no market-currency fallback, default, country inference or Control Plane accounting rule. It does not ask for the exact version a second time; ERP proves the references exact when it provisions.
  - **Request.** The references are sent as `finance_baselines` (one per legal entity) and `functional_currencies` is the set of ERP's answers, each currency once. The reference set is part of the submission's idempotency key: the same set in another order replays, an altered one does not.
  - **Fail closed.** No approved baseline (404), an unavailable ERP, a rejected context, an answer that does not conform to erp/v1, names another entity or another owner, a repeated legal entity, or a version that is not `EFFECTIVE`: nothing is sent or recorded. ERP's `FINANCE_BASELINE_MISMATCH`, `FINANCE_BASELINE_NOT_USABLE` and `PLAN_AUTHORITY_MISMATCH` stay three distinct, non-retryable errors.
  - **Ledger.** Migration `000097` stores the complete reference set with the submission (fixed once recorded, like the plan tuple), so an audit can answer which Finance baseline caused which provisioning; a replay naming other references is a conflict, never a rewrite.
  - **Removed.** `FinanceBaselines`, `PlanSource.Finance` and `Authorised.Currencies`: the Control Plane no longer has any path that produces a currency itself. Nothing is activated, no scope is allocated and the provisioner stays PROVISIONED.

- ERP provisioning is wired into the APPLY and READY phases, off unless configured (`ERP_PROVISIONING_URL`). Nothing is activated: the provisioner is still PROVISIONED with no IAM client or credential.
  - **Ledger:** migration `000095` adds `provisioning.erp_submission`, the only link from an ERP operation back to its provisioning and exact approved plan (ERP's state carries no provisioning id). One operation per approved plan; the link is immutable and progress only moves forward, enforced by the database (`PostgresLedger`, proved against real PostgreSQL).
  - **Source:** `PlanSource` takes the tenant, plan tuple, legal entities and countries from the approved plan and its frozen desired state, through `repository.ApprovedPlanProblem`, now the one definition of "approved plan" shared by ERP's assignment read, provisioning-context validation and this worker (moved out of `api`, behaviour unchanged).
  - **Functional currencies are not derived by the Control Plane.** A market's registry currency is a market fact, not a legal entity's accounting fact, and neither the plan nor the desired state carries one. They come from a `FinanceBaselines` resolver; none exists until Shared defines a versioned Finance baseline reference and ERP exposes it, so every submission stops at the source with `ErrFinanceBaselineUnavailable` (fail closed; ERP would answer `409 PLAN_AUTHORITY_MISMATCH` for a guess anyway).
  - **Admissibility:** APPLY leaves the canonical state at `REGISTERING`, which Control Plane does not honour a provisioning context in, so before anything is sent the step moves the provisioning to `PROVISIONING_PROVIDERS` (`EnterProviderProvisioning`: only from the states before it, a resumed execution is left alone, anything else is refused, and the revision the orchestrator holds is not touched).
  - **Pipeline:** with ERP configured, APPLY gains an `erp-provisioning` step after the capability bindings and READY gains an `erp-provisioning` check that holds the tenant until ERP reports `active` (ERP's 202 is only "accepted"). Unset, the pipeline is unchanged.
  - **Config:** `ERP_PROVISIONING_URL` (HTTPS), `ERP_PROVISIONER_TOKEN_FILE`, `ERP_PROVISIONER_ISSUER`, `ERP_PROVISIONER_SUBJECT` (default `baobab-cp-provisioning-workload`), `ERP_PROVISIONING_CONTEXT_TTL` (1m-15m, default 10m).
  - **Still to do:** the inbound `provisioning.changed` transport (Control Plane has no event consumer; until then progress is read through `Worker.Reconcile`, which nothing yet schedules), the Finance baseline contract and resolver, and the end-to-end evidence before the provisioner may be promoted to ACTIVE.

- `internal/erpprovisioning`: the Control Plane's caller side of the ERP provisioning boundary (Shared erp/v1, pin unchanged at `6ae8d9f`; the provisioning request/state schemas are the same at the newer erp/v1 1.1.1, whose only change is the GET scope in the OpenAPI). It is a library with no runtime wiring: nothing starts it, nothing is configured, and no identity is allocated or activated.
  - **Caller:** the dedicated provisioner workload (`baobab-cp-provisioning-workload`, still PROVISIONED in Shared's registry, no IAM client yet): token `aud=baobab-erp`, `scope=erp:provision`, read per request. The approving human is provenance carried by the plan, never the runtime caller.
  - **Context:** created internally (`ContextIssuer`) under the provisioner's own canonical principal, resolved by (issuer, subject) and looked up, never created (`ErrProvisionerIdentityNotRegistered` otherwise). It does not use `POST /v1/platform-context`, so the provisioner needs no `context:resolve` and no Control Plane audience. Contexts are bounded (TTL required) because ERP validates them through `POST /v1/platform-context/validate` for the caller it sees.
  - **Request:** the approved plan tuple (provisioning id, plan id, version, digest) plus legal entities, countries and currencies, validated against the pinned `erp/v1` request schema before sending; a deterministic `Idempotency-Key` from the tenant, the tuple and the legal-entity set (not `context_id`, which ERP excludes from request identity), so a replay returns ERP's prior operation and a replan is a different request.
  - **State:** ERP's 202 means accepted, not provisioned. `provisioning.changed` event data and reads are validated against `erp/v1` `ProvisioningState`, tied to the recorded submission (ERP's state carries no provisioning id), refused if tenant or legal entities disagree, and applied only if newer (late, duplicate and foreign events are ignored). The operation GET is only for reconciliation of a known operation.
  - **Refusals** keep only status, a well-formed code, retryability and `Retry-After`; `409 PLAN_AUTHORITY_MISMATCH` means replan, never retry.
  - **Not included here:** see the wiring entry above for the ledger, source, APPLY step and config that followed.

- The ERP assignment projection now names the exact approved-plan tuple (control-plane/v1 OpenAPI 1.32.0, Shared `daddc4f`): `GET .../erp-assignments/{legalEntityID}` emits `plan_id` and `plan_version` beside `plan_digest`, the same tuple an ERP provisioning request names in `control_plane_authority` (erp/v1 1.0.2), so ERP can compare tenant, provisioning, plan id, version and digest member by member and answer `409 PLAN_AUTHORITY_MISMATCH` to any difference. An approval binds the plan id, version and digest together (ADR-BCP-021), so the handler now also refuses (409) an approval recorded for another plan version, not only another id or digest. Re-pins `contracts.lock.yaml` `6c6bd17` -> `daddc4f` and regenerates the Console client.
- `GET /v1/tenants/{tenantID}/provisioning/{provisioningID}/erp-assignments/{legalEntityID}` serves Shared's ERP assignment projection (control-plane/v1 OpenAPI 1.31.0, `erp-assignment.schema.json`; Shared pin `6c6bd17`). It answers only what Control Plane has already determined ERP is assigned for one legal entity of one provisioning execution; it is not a planning API and never repairs a disagreement between its sources. Workload read under the new `erp-assignment:read` scope, which this change **grants to no client** (IAM assigns it separately). The scope is an invocation permission, not resource authority: the token's `tenant_id` must be present and equal the path tenant (403 otherwise, a missing claim is not "any tenant").
  - **Sources:** `isolation_requirement`, the markets and legal-entity membership come from the provisioning's frozen desired state (not the tenant's current isolation configuration); `engine_instance_id` and `capabilities[]` come only from the approved plan's `CREATE_CAPABILITY_BINDING` steps for `baobab-erp` (deduplicated, sorted; never the live registry); the legal entity's identity comes from its Control Plane profile.
  - **Status mapping:** 404 for an absent provisioning (including another tenant's or a malformed id) or a legal entity outside it; 409 when the sources disagree or are not executable: no APPROVED decision for the current plan digest, a withdrawn provisioning, a stale or expired plan (judged only before apply, since the registry legitimately moves on during execution), desired state and plan disagreeing on tenant or digest, ERP steps resolving to no or conflicting engine instances, a missing isolation requirement, or a legal entity with no profile, not VERIFIED, or without a jurisdiction; 503 when a source cannot be read. Every response is validated against the pinned schema before it is served.
  - **Never emitted:** `capability_binding_id`, `capability_bindings`, `isolation_profile_id`, native `AD_Client`/`AD_Org` placement, `legal_entity_code`, or any UUID standing in for a canonical identifier. Control Plane's registry UUIDs stay internal; a canonical public id for bindings or isolation profiles would have to be minted first.
  - Re-pins `contracts.lock.yaml` from `a503649` to `6c6bd17` (the consumed `control-plane/v1/openapi.yaml` changed; `erp-assignment.schema.json` is newly consumed and embedded).
- `POST /v1/capabilities/resolve` and `/resolve-batch` now answer Shared `capability/v1` resolutions (control-plane/v1 OpenAPI 1.21.0, EA-01 phase 3). **Breaking** for callers of these two routes. With this, every Control Plane route is described.
  - **Request:** `resolutionRequest` or `batchResolutionRequest`, validated against the schema. `correlation_id` is required and no canonical entity is accepted. The `context_id` is redeemed as before and stays bound to the caller's tenant.
  - **Decision order:** a new `CapabilityResolutionService` checks, in turn:
    1. the capability is registered (else `CAPABILITY_UNKNOWN`) and ACTIVE (else `CAPABILITY_INACTIVE`);
    2. an effective grant's scope covers the context; otherwise `GRANT_SUSPENDED`, `GRANT_REVOKED`, `GRANT_EXPIRED` or `GRANT_NOT_FOUND`. Grant enforcement applies to these routes only; `/v1/resolve` is unchanged;
    3. `required_contract_version`, else `CONTRACT_VERSION_UNSUPPORTED`;
    4. binding selection by the binding's own scope, mode, priority and health: `BINDING_NOT_FOUND`, `BINDING_AMBIGUOUS`, or the health codes;
    5. the engine instance's eligibility;
    6. residency: a context without a market is `RESIDENCY_POLICY_MISMATCH`;
    7. the provider's registered invocation reference, else `PROVIDER_INVOCATION_UNDECLARED`.
  - **RESOLVED** names `grant_…`, `bind_…` and an invocation (service reference, protocol, contract version, `provider_…`, `ei_…`). It expires when the health or the context it relied on does.
  - **Recording:** migration 000079 adds `capability.capability_resolution`, which stores every decision under its `res_` id, including each member of a batch. A RESOLVED record must name its grant, binding, provider, instance and reference, and any other record must name its reason.
  - **Grant report:** the view `capability.tenant_binding_without_grant` lists ACTIVE bindings whose tenant holds no effective grant. Those bindings resolve to `GRANT_NOT_FOUND` until a grant is issued.
  - `resolver` exports `ErrBindingNotFound`, `ErrBindingAmbiguous`, `ErrCapabilityNotResolvable` and `ScopeCompatible`.
- A resolved context carries the tenant's market participation (Shared control-plane/v1 OpenAPI 1.20.0, EA-01 phase 2.5).
  - `ContextResolutionService` fills `country_code`, `market_id` and `currency_code`:
    - the country comes from the tenant's ACTIVE, effective market assignment in a country an available registry market covers;
    - `market_id` is that country's primary registry market (`market.market.registry_market_id`), and `currency_code` is that market's currency.
  - `POST /v1/platform-context/resolve`:
    - takes an optional `country_code` to choose among the tenant's own participations;
    - answers `MARKET_CONTEXT_AMBIGUOUS` (403) when there are several and none was chosen, and `MARKET_CONTEXT_NOT_PARTICIPATING` (403) for a country the tenant doesn't participate in;
    - returns the three fields.
  - A tenant that participates nowhere keeps a context without a market, which resolution policy still denies.
  - Resolution policy has always required a market or country in the context. Resolved contexts now carry one, so the deprecated `/v1/resolve` reaches a decision again.
- Engine registration records a provider's logical invocation reference (Shared `capability/v1` `registration.schema.json` `provider.invocation`). Migration 000078 adds `service_reference` and `invocation_protocol` to `capability.capability_provider`.
  - The store accepts only `service://` references, never hosts, and the two columns must be set together.
  - A registration without an invocation clears any reference recorded earlier.
  - `ProviderInvocationByID` reads the reference for capability resolution. It becomes the invocation descriptor when the capability routes conform to `capability/v1` (EA-01 phase 3).
- `POST /v1/platform-context/resolve` and the deprecated `POST /v1/resolve` are now described by Shared control-plane/v1 OpenAPI 1.19.0 (`platform-context.schema.json`), and the drift test no longer lists them as undescribed.
  - `TestContextResolutionRoutesConformToShared` validates their requests and responses against the Shared schemas.
  - Both routes now build the composed decision body with one function, `composedResolution`, which `/v1/capabilities/resolve` also uses, adding its `context_id`.
  - `/v1/resolve` builds its context without a market or country, so its pipeline answers `RESOLUTION_FAILED`. It stays deprecated until its callers move to platform-context resolution.
  - `/v1/capabilities/resolve` and `/resolve-batch` stay undescribed until they conform to `capability/v1`.
- Capability bindings name their provider (Shared `capability/v1` `binding.schema.json`; baobab-cp#76, #82).
  - **Eligible provider.** A binding's provider is an ACTIVE provider on the binding's engine that ACTIVELY supports the binding's capability.
  - **Creating bindings.** `CreateBinding`, used by provisioning, takes the provider the binding names when it is eligible; otherwise it takes the engine's only eligible provider.
    - It refuses an ACTIVE binding whose provider is missing or ambiguous with `ErrBindingProviderUnresolved`.
    - It refuses a named provider belonging to another engine with the same error.
    - `SaveBinding` cannot make a binding without a provider ACTIVE.
  - **Migration 000077.**
    - Backfills existing bindings whose provider is unambiguous.
    - Adds `capability_binding_active_provider_check` as `NOT VALID`: rows that couldn't be backfilled are not rejected, but a row without a provider can no longer become ACTIVE or change while it is ACTIVE.
    - Reports the ACTIVE bindings still without a provider in `capability.binding_without_provider`, with reason `NO_PROVIDER` or `AMBIGUOUS_PROVIDER`. A later migration validates the constraint once that view is empty.
- One market authority: the registry drives participation (Shared `market-lifecycle.yaml` `participation`).
  - **Coverage.** A country is covered by every registry market naming it as `default_country` or listing it in `countries`. The view `market.country_coverage` derives this from the registry; nothing stores it separately.
  - **Projection.** Activating a market, whether through the direct route or a `MARKET_ACTIVATION` changeset, projects each country the market covers into `market.market` in the same transaction.
    - The row is created if missing, marked active, and linked to the country's primary market through `registry_market_id`, from which it also takes its name, currency and region.
    - The primary market is the earliest-activated available market whose `default_country` is the country; failing that, it is the earliest-activated one that lists the country.
  - **Planning.** Provisioning plans participation in a country only while an ACTIVE registry market covers it. A `market.market` row on its own no longer counts, and a country without coverage is `MARKET_NOT_AVAILABLE`. Existing assignments are unaffected.
  - **Legacy rows.** The view `market.uncovered_country_market` lists the country rows that no ACTIVE registry market covers.
  - Migration 000076.
- Provider migration execution in both modes, with engine migration tasks (ADR-SHARED-016; Shared control-plane OpenAPI 1.18.0).
  - **Approval.** `POST /v1/provider-migrations/{id}/approve` (`provider-migration:approve`, If-Match) records one decision on the current plan's digest.
    - The creator never approves.
    - Approving needs a plan that is unblocked, unexpired and not stale; staleness is re-planned inside the transaction.
    - One approval authorises the plan's whole sequence.
  - **Advancing.** `POST /v1/provider-migrations/{id}/advance` (`provider-migration:execute`, If-Match, Idempotency-Key) runs one lifecycle transition as a `PROVIDER_MIGRATION_ADVANCE` operation.
    - It runs exactly the approved plan's steps named by the Shared `stage_steps`.
    - The plan's approver never advances it forward (`PROVIDER_MIGRATION_SELF_EXECUTION`, 403); `cancel` and `roll_back` stay open to any executor.
    - Staleness ignores the migration's own effects: the ledger's source bindings count as they were.
    - `canary` and `shift` run only inside the cutover window.
  - **Local steps.**
    - `prepare` binds each source binding's scope to the target instance the plan fixed, in MIGRATION mode, which resolution ranks lowest.
    - `SHIFT_COHORT` steps the source down to MIGRATION before the target takes the source's mode, so each scope holds at most one authoritative binding.
    - `VALIDATE_COHORT` checks the modes and the target's health.
    - `RETIRE_SOURCE_BINDING` retires the source.
    - Cohort steps now name their `binding_ids`, so execution never re-derives membership.
  - **Engine migration tasks.** STATEFUL_CUTOVER cohorts issue FREEZE (source), MIGRATE (target, with the source as counterpart), RECONCILE (both sides) and UNFREEZE (target), each after the previous step's reports.
    - The advance waits RUNNING, and each report resumes it.
    - Reconciliation needs equal total counts on both sides, and equal digests when each side runs one instance.
    - The workload routes `GET /v1/engine-migration-tasks`, `POST .../{taskID}/claim` and `POST .../{taskID}/report` (`provider-migration:task`) serve only the attested workload of a task's instance (`topology.engine_instance.workload_client_id`).
    - Claims hold a lease, and a lapsed lease starts a new attempt. Reports are final and replayable.
    - An overdue task fails with `MIGRATION_TASK_TIMEOUT`, leaving its cohort frozen.
  - **Rollback and cancel.**
    - `roll_back` releases a cohort a failed advance left frozen, then returns each shifted cohort by its strategy, using reverse tasks for stateful cohorts, then removes the target bindings.
    - `FORWARD_FIX_ONLY` refuses once a cohort has shifted.
    - `cancel` removes the target bindings before any shift.
    - An advance cancelled through `/admin/operations/{id}/cancel` ends CANCELLED when next settled.
  - **Migration `000075_provider_migration_execution.sql`:**
    - migration execution columns, and the approval, binding-ledger and run tables;
    - `topology.engine_migration_task`;
    - the attested workload column on engine instances;
    - the new operation type and subject.

- Market and mapping activation as Changesets (ADR-BCP-021 adoption; Shared control-plane OpenAPI 1.17.0).
  - The change kinds `MARKET_ACTIVATION` and `MAPPING_ACTIVATION` are MODIFY changesets at PLATFORM scope, from VALIDATED to ACTIVE.
  - This is an optional governed path. `POST /v1/markets/{marketID}/activate` and `POST /v1/mappings/{mappingID}/activate` stay as they are.
  - **Planning:**
    - the plan binds the target's reviewed revision (`target_revision`);
    - a target edited after planning makes the plan `PLAN_STALE`;
    - an ACTIVE target blocks the plan, and an unknown target is INVALID.
  - **Approval:**
    - the approver needs the kind's `approval_scope` (`market:approve` or `mapping:approve`) as well as `changeset:approve`;
    - the approver must meet the direct route's maker-checker rule: `MARKET_SELF_ACTIVATION` when they are the market's creator or last editor, `MAPPING_SELF_APPROVAL` when they are the mapping's creator.
  - **Apply:**
    - runs exactly the direct routes' activation rules (`activateLockedRegistryMarket`, `transitionLockedMapping`) in the changeset's transaction;
    - records the approver as the market's activator or the mapping's approver;
    - verifies the target by reading it back.
  - Migration `000074_changeset_targets.sql`:
    - changesets carry `target_type` and `target_id`, which hold the semantic lock;
    - `target_tenant_id` is now set for tenant changesets only;
    - approvals record the approver's verified subject.

- Applicant claims on client applications (ADR-BCP-023 §7, §9, §191-192; Shared control-plane OpenAPI 1.16.0).
  - `POST`/`GET /v1/client-applications/{applicationID}/claims` and `POST .../claims/{claimID}/withdraw` (If-Match):
    - an applicant asserts a claim type, jurisdiction and value on their own application, only while it is DRAFT or INFORMATION_REQUIRED;
    - the claim is SELF_ASSERTED and about the application, in the application's one `ORGANISATION_ADMISSION` VerificationCase, which the first claim opens (serialised per application);
    - an applicant never names a subject or case, and never reaches another applicant's application or claim.
    - creating a claim is replayable: an `Idempotency-Key` replay answers 200 with the original claim (migration `000073_applicant_claim_idempotency.sql`).
  - Concluding a case VERIFIED now requires at least one claim, and every claim that still stands to be VERIFIED. Withdrawn and superseded claims neither block nor count.
  - `POST /v1/admin/verification-cases/{caseID}/claims/{claimID}/open-verification` (`verification:write`, If-Match; permission `verification.review`) takes a claim under verification. It changes no standing.
  - Organisation admission now also accepts the admitted application's own case (subject `APPLICATION`). That is the usual path, since the case is worked before approval, and only its VERIFIED claims are promoted. The case counts only when the admission's decision is APPROVED and is that application's decision; the request's `application_id` alone is never trusted.
  - A claim is added only while the application is editable, rechecked under the application's row lock in the claim's own transaction, so it can never race a submission.
- The verification workflow (ADR-BCP-023 gate OEV-03; Shared `evidence/v1` and the control-plane `openapi.yaml` 1.14.0 Verification operations). Eighteen `/v1/admin` routes serve:
  - verification cases: open, list, read, transition and conclude;
  - a case's claims, checks, results and discrepancies;
  - discrepancy transitions and resolution;
  - evidence registration and metadata;
  - the evidence source registry.
  - `internal/verification` loads the lifecycle and source registry from the embedded Shared files and enforces the evidence chain:
    - nobody checks or decides a claim they asserted (section 169), also refused by a database trigger;
    - VERIFIED rests on a positive check from a source trusted for the claim type in the claim's jurisdiction;
    - a positive check cites only evidence reviewers may open;
    - a result cites only its own claim's checks, and discrepancies about its own claim;
    - a claim's standing is its current result's outcome.
  - A CONFLICTED result stops a VERIFYING case until a reviewer resolves the discrepancy and resumes it.
  - `complete_verified` requires every claim VERIFIED.
  - Recording results, concluding a case and closing a discrepancy require `verification:decide`.
  - Evidence registration accepts source records and credentials only, never uploads, and GET serves metadata only.
  - Migration 000071 adds the `evidence` schema.
  - Every route is mapped for shadow evaluation, anchored by the case's organisation.
- The market registry routes (ADR-BCP-004 section 18; Shared `control-plane/v1/market.schema.json` and `market-lifecycle.yaml`, `openapi.yaml` 1.13.0). The Control Plane now serves every operation its pinned OpenAPI describes.
  - `POST /v1/markets` registers a market:
    - the Control Plane mints `mkt_` ids, derives `created_by`, and replays by Idempotency-Key;
    - an unknown owner tenant is refused, and so is a duplicate `canonical_key`.
  - `GET /v1/markets/{id}`: administrators read every market; workloads holding `market:read` read ACTIVE markets only.
  - `PATCH /v1/markets/{id}` is a JSON merge patch at `If-Match`. It is allowed on DRAFT and VALIDATED markets; an ACTIVE market is refused (`MARKET_NOT_EDITABLE`).
  - `POST /v1/markets/{id}/activate`: VALIDATED to ACTIVE at `If-Match`, under `market:approve`.
    - The activator is the caller, and never the market's creator or last editor (`MARKET_SELF_ACTIVATION`). The database also refuses a creator as activator.
  - `internal/market` evaluates the lifecycle's ten validation rules, loaded from the embedded Shared file. Every write re-evaluates them in its transaction, so status and `validation_findings` always agree.
  - Migration 000070 adds `market.registry`.
    - The configuration is kept as the validated Shared document, beside the derived columns.
    - `market.market`, the country-keyed market provisioning plans against, is unchanged; linking the two is a follow-up.
  - The routes are mapped to `market.request`, `market.view` and `market.activate` for shadow evaluation.
  - The OpenAPI unimplemented list, and the Console client's list, are now empty.
- Generic changesets (ADR-BCP-021 CCM-02 and CCM-03; Shared `control-plane/v1/changeset.schema.json`). The first change kinds are tenant suspension and reinstatement.
  - `internal/changeset`:
    - drafts derive their type, scope, source, requester and base revision from the change and the verified caller;
    - the lifecycle is loaded from Shared `changeset-lifecycle.yaml`, and illegal transitions are refused;
    - validation separates INVALID (the target does not exist) from BLOCKED (a state conflict, or an earlier open changeset on the same tenant);
    - planning is deterministic and digest-bound, and a changed tenant makes the plan stale.
  - Migration 000069 adds `changeset.changeset`, `plan`, `approval` and `outcome`, and admits the `CHANGESET_APPLY` operation type.
  - Each lifecycle command is one transaction at the changeset's revision:
    - approval is refused for the requester, for another plan or digest, and for a stale plan;
    - apply re-validates under a tenant row lock, changes the tenant, verifies it by reading it back, and records the operation, the COMPLETED changeset and its outcome atomically;
    - a replayed apply returns the same operation.
  - Nine `/v1/admin/changesets` routes: platform administrators only, with approval under its own scope. The routes are shadow-evaluated against the `changeset.*` permissions.
- Provider migration planning (ADR-BCP-006 Gate 8; Shared
  `control-plane/v1/provider-migration.schema.json`).
  - `internal/topology/migration`: a side-effect-free planner. Discovery
    covers the source provider's live PRIMARY and FALLBACK bindings and the
    contexts their scopes name. Each context is assigned to exactly one
    deterministic cohort. Blockers cover every unmet section 48
    precondition: target registration, support, contract version, region
    and environment eligibility, and CRITICAL-level health. Shadow is refused
    until a capability declares itself shadow-safe. Stateful cohorts freeze,
    migrate, reconcile, shift and unfreeze, in that order.
  - Migration 000068 adds `topology.provider_migration` and
    `topology.provider_migration_plan`. Creates for one source provider are
    serialised, so a competing migration is always seen and blocked.
  - Routes (platform administrators, `topology:read` or `topology:write`):
    - `POST /v1/provider-migrations/plan`, the preview;
    - `POST /v1/provider-migrations`, which is idempotent;
    - `GET /v1/provider-migrations/{id}`;
    - `GET /v1/provider-migrations/{id}/plan`.
  - Advancing a migration past PLAN is left to the generic changeset
    (ADR-BCP-021).
- Shadow evaluation of administrative authority (ADR-BCP-020 section 144).
  Every role-guarded route maps to the permission it performs. Grants are
  evaluated beside the legacy role decision and counted in
  `administrative_authority_shadow_total`, without changing any response.
- Administrative grants and the effective-authority read model (ADR-BCP-020
  gates ADA-01 to ADA-03, FE-00 G5; Shared `administration/v1`).
  - `internal/administration`: the permission and profile catalogue, scope
    coverage, deny-by-default evaluation that never combines grants across
    scope, delegation validation, and effective authority.
  - Migration 000067 adds `policy.administrative_grant`.
  - `GET /v1/admin/effective-authority` (scope `authority:self`) reports the
    caller's own usable grants, derived from grants only.
  - `cmd/admin-bootstrap` creates bounded, audited initial platform
    authority.
  - `docs/reconciliation/ada-00-administrative-authority-inventory.md`
    records today's role checks and their migration.
- Health gates eligibility (ADR-BCP-006 sections 18-22 and 72-73, Shared
  `capability/v1` `health.schema.json` and `health-policy.yaml`).
  - Health is a time-bounded observation. A missing, expired or
    future-dated observation is UNKNOWN.
  - A capability declares `health_criticality`: `CRITICAL` accepts only
    HEALTHY; `STANDARD` (the default) accepts HEALTHY or UNKNOWN.
  - Capability resolution, engine relocation, binding provisioning and the
    provisioning planner all decide through `internal/health`. A plan with
    no health-eligible candidate is blocked with `NO_HEALTHY_PROVIDER`.
  - Migration 000066 adds `capability.health_criticality` and
    `topology.health_observation`, and drops the unused
    `topology.engine_instance.health_status`.
- Executable `POST /v1/context/resolve` with workload OIDC, authoritative
  lifecycle/entitlement checks, 15-second bounded success caching and
  fail-closed behaviour.
- Append-only audit provenance for context decisions, including service,
  token, tenant/product target, result and policy decision.
- Canonical RFC 9457 problem responses, context contract tests, database
  migration 000003, API documentation and ADR-0004.
- Continued development of the enterprise platform architecture.
- Additional governance documentation.
- Repository improvements and engineering standards.
- Ongoing documentation enhancements.

## Fixed

- `cmd/migrate` no longer fails against a fresh database: migration
  `000017_indexes_and_integrity.sql` referenced columns and a
  `mapping.mapping` table that were never created by the migrations ahead of
  it (`registry.canonical_entity.owner_tenant_id`,
  `registry.canonical_relationship.source_entity_id`/`target_entity_id`/
  `status`/`valid_period`, four `mapping.mapping_scope` columns, and
  `mapping.mapping` itself). Verified end to end against PostgreSQL 16/17
  from an empty database. See ADR-0005.
- `capability.capability_binding` writes made through
  `PostgresRepository.CreateBinding`/`SaveBinding` no longer silently defeat
  the `capability_binding_primary_excl` exclusion constraint. `status` was
  written lower-cased while the constraint's predicate (and
  `binding_mode`'s `CHECK`) expect upper case, so two `PRIMARY` bindings for
  the same capability and scope with overlapping validity could both be
  created without error — the invariant BCP-DB-001 relies on to keep
  capability resolution deterministic was inert. Added a PostgreSQL
  integration test that reproduces the conflict. See ADR-0005.
- `POST /v1/canonical-entities` now mints entity IDs as UUIDv7
  (`domain.NewUUIDv7`) instead of UUIDv4, per BCP-DB-001's identifier
  contract for first-class control-plane resources. See ADR-0005.

## Security

- Every role-guarded administrative route now maps to a registered AdministrativePermission (ADR-BCP-020 §143; Shared `administration/v1` with `mapping.view`, `mapping.manage` and `mapping.approve`). The external-reference and canonical-mapping routes were the last blank entries in the grant shadow. `TestEveryGuardedRouteIsMapped` now refuses a blank one. Legacy roles still decide; this only completes the comparison ahead of the grant cutover.
- **Breaking: admission verifies legal identity only through a VerificationCase (ADR-BCP-023 §191-193; Shared control-plane OpenAPI 1.15.0).**
  - `POST /v1/tenants/{tenantID}/organisation-admission` no longer accepts `legal_verification`, the reviewer's one-call evidence and reason.
  - It takes `verification_case_id` instead. The case must:
    - be VERIFIED, with purpose `ORGANISATION_ADMISSION`;
    - be about the admitted organisation or its legal entity;
    - have VERIFIED REGISTRATION_IDENTIFIER and LEGAL_NAME claims whose values match the applicant's submission (whitespace and case normalised).
  - The case is checked before anything is written. An unknown case answers 404 `VERIFICATION_NOT_FOUND`; any other unusable case answers 409 `VERIFICATION_CASE_STATE_CONFLICT`.
  - The legal entity and organisation are verified citing `verification-case:` and `verification-result:` references.
  - Their `legal-entity.verified` and `organisation.verified` events carry the case, results, evidence ids and reason codes by opaque identifier (§143).
- An organisation profile records who verified it and when, and cannot be VERIFIED without evidence, a verifier and a time (ADR-BCP-023 OEV-03; migration `000072_organisation_verification_provenance.sql`). The legal-entity, corporate-relationship and platform-relationship tables already had these guarantees.
  - `VerifyOrganisation` and first-party governance write `verified_by` and `verified_at`.
  - The migration backfills existing VERIFIED profiles from the audit record of the transition that verified them. It never invents provenance: a VERIFIED profile without evidence or without that audit record fails the new constraint, and the migration stops for review.
- No organisation record can be created VERIFIED (ADR-BCP-023; OEV-00 inventory, "Verification writers"). `EnsureOrganisation`, `EnsureLegalEntityProfile`, `EnsureCorporateRelationship` and `EnsurePlatformRelationship` refuse `VERIFIED` with `ErrVerifiedAtCreation`, so verification is only ever a transition: `Verify*`, a verification-case outcome, or first-party governance. No production caller created VERIFIED records, so this closes a latent bypass without changing behaviour.
- Upgraded `golang.org/x/text` to `v0.39.0` to remediate
  `CVE-2026-56852`; the dependency upgrade also advances
  `golang.org/x/sync` to `v0.21.0`.

---

# [0.1.0-alpha] - 2026-07-11

## Summary

The inaugural alpha release establishes the governance, engineering standards, repository structure, and architectural foundations of the BAOBAB platform.

This release focuses on preparing the project for collaborative development rather than delivering production-ready functionality.

### Added

#### Repository

- Established the BAOBAB Git repository.
- Adopted an enterprise-ready polyglot repository structure.
- Organised project documentation and governance resources.
- Added development container support.
- Added GitHub Actions workflow definitions.
- Established infrastructure and shared component directories.

#### Governance

- Added `README.md`.
- Added `CONTRIBUTING.md`.
- Added `SECURITY.md`.
- Added `CODE_OF_CONDUCT.md`.
- Introduced project governance framework.

#### Architecture

- Defined enterprise platform architecture.
- Established support for a polyglot service architecture.
- Organised backend, frontend, mobile, AI, and worker applications.
- Defined shared contracts, events, schemas, and reusable components.
- Established infrastructure organisation for AWS, Docker, Kubernetes, monitoring, and automation.

#### Development Environment

- Added Dev Container configuration.
- Added Docker development support.
- Added project configuration files.
- Added repository development standards.

#### DevOps

- Added Continuous Integration workflow definitions.
- Planned Continuous Deployment automation.
- Established release workflow structure.
- Added dependency update workflow.

#### Documentation

- Established documentation hierarchy.
- Organised architecture documentation.
- Created ADR structure.
- Added runbook framework.
- Added documentation standards framework.

---

### Changed

- Adopted Apache License 2.0 as the project's open-source licence.
- Standardised repository organisation across platform components.
- Adopted Semantic Versioning for future releases.
- Adopted the Keep a Changelog format for release documentation.

---

### Security

- Established responsible vulnerability disclosure process.
- Adopted secure software development lifecycle (SSDLC) principles.
- Defined secure coding expectations.
- Added software supply chain security guidance.
- Added AI security guidance.
- Added infrastructure security principles.

---

### Notes

This alpha release represents the beginning of the BAOBAB platform.

The emphasis is on establishing strong engineering governance, project standards, documentation, and architectural direction before feature development accelerates.

Future releases will progressively introduce platform functionality while maintaining the engineering principles established in this initial release.

---

# Future Releases

Subsequent releases will continue documenting the evolution of BAOBAB using the structure defined in this changelog.

Each release should include:

- Release summary.
- Version number.
- Release date.
- Relevant change categories.
- Upgrade guidance where applicable.
- Links to supporting documentation.

Maintaining a consistent format ensures clarity and traceability throughout the project's lifecycle.

---

# References

This changelog complements the following project documents:

| Document | Purpose |
|----------|---------|
| `README.md` | Project overview and vision. |
| `CONTRIBUTING.md` | Contribution workflow and engineering practices. |
| `SECURITY.md` | Security policy and vulnerability disclosure. |
| `CODE_OF_CONDUCT.md` | Community standards and expected behaviour. |
| `LICENSE` | Apache License 2.0 governing the use and distribution of BAOBAB. |
| `docs/Coding-Standards.md` | Software engineering and coding conventions. |
| `docs/Testing-Standards.md` | Testing strategy and quality expectations. |
| `docs/Documentation-Standards.md` | Documentation principles and style guidance. |

Together, these documents provide the governance and operational framework supporting BAOBAB's continued development.

---

# Closing Statement

The BAOBAB changelog reflects the platform's ongoing journey from architectural vision to production-ready enterprise software.

Each release represents not only technical progress but also the continued commitment of contributors, maintainers, and the wider community to building secure, reliable, maintainable, and well-governed software.

By documenting meaningful changes with clarity and consistency, we preserve the history of the project while helping users and contributors confidently navigate its future.

---

<div align="center">

## Strong Roots. Inspired Growth.

**Every release strengthens the platform. Every contribution shapes its future.**

</div>
