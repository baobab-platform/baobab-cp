# People BYOP runtime boundary — CP adapter (ADR-SHARED-045)

This implementation introduces a **read-only, nonproduction-gated** CP endpoint:
`POST /internal/people/v1/provider-selection/resolve`. It accepts only
`context_id`, `capability_key`, `correlation_id` and
`required_contract_version: 1`. Client-supplied tenant, legal employer,
provider, approval, role, employee or selection fields are refused.

CP authenticates a current, ACTIVE and explicit allowlisted IAM workload with
**both** `people:provider:read` and `context:resolve` registered and present
in its token. It redeems a workload-owned RUNTIME Context, which must contain
both exact `legal_entity_id` and two-letter `jurisdiction`. CP joins one
approved employer-specific `people_provider_choice` with ACTIVE PRIMARY
capability binding, matching binding version/provider/scope, active provider
support, **current** EA-09 certification and approved release. Then the existing
capability resolver independently verifies current grant, binding, health and
invocation. Both readbacks must name the **same** provider; otherwise deny.

The choice table is **empty by default**, with no provisioning seed or automatic
native default. An ACTIVE row requires two different principals and qualification
evidence and cannot overlap another ACTIVE choice for the same exact employer,
jurisdiction, capability and effective period. No direct HTTP mutation exists.
The governed CP changeset/approval executor is not yet attached to the new
table; until then, there is **no supported operator activation procedure**.

The endpoint is registered only when both
`PEOPLE_PROVIDER_READ_ENABLED=true` and a nonempty
`PEOPLE_PROVIDER_READ_CLIENT_IDS` allowlist are configured, and never in
protected environments. No configuration is enabled by this PR. Responses use
the accepted `contracts/people/v1/provider-selection.schema.json` readback
and no-store, with a maximum 30-second lease. ERP must validate the result and
repeat a *current* CP resolution immediately before any consequential action.

**Not proven:** connected IAM issuance, real adapter, post-cutover replay,
qualified statutory packs or production availability. This is an execution
boundary, not a ready payroll system.

## Accepted source provenance and merge boundary — 9 November 2026

- Shared #434 (ADR-SHARED-045 People domain) and #436 (workload-only non-authorising scope) are accepted. CP's consumer lock is updated to the **merged** Shared #436 mainline SHA `162f90831ea9b99076391ddca3681b906fa28364`, not the PR head. All five changed CP-declared Shared contracts have been copied byte-for-byte, including the new `people:provider:read` scope and the four People capability catalogue entries. The canonical People domain, capabilities and provider-selection readback are embedded and locked for exact schema validation.
- The inert IAM-N02 candidate retains its older accepted Shared #430 SHA `7f547ccdd759208355ef9224d2177929f4bf224e` as historical provenance; no client status or authority is upgraded. Candidate source lineage is intentionally distinct from the **current** CP Shared lock and tested as such.
- CP and IAM must still execute a real employer-scoped active-workload/context/active-binding/grant/release/certification positive and corresponding revoked, foreign-employer, foreign-tenant, cutover and ambiguous-source denials. An existing PostgreSQL 17 negative test proves no default selection only; it cannot substitute for connected employer/provider acceptance.
- This PR proposes canonical migration `000136_people_provider_choice.sql`. Trade price-issuer PR #465 independently proposes **another 000136**. Before #465 merges, the Trade owner must reconcile it as the next unused canonical migration and rerun migrations from a clean PostgreSQL 17 database; duplicate migration versions are never acceptable.
- Route is disabled by default and entirely excluded from staging/production. No change in this PR enables an IAM token, grants an employer access or activates a provider. A safe source-level merge does not imply live provider-selection execution or payroll-readiness acceptance.
