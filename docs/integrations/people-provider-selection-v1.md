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
