# CX-05C — bounded, permission-scoped enterprise resource tables

## Implemented acceptance slice

- New protected Organisation 360 → **Enterprise data tables** route at
  `/workspace/organisations/{organisationId}/records`.
- Eight distinct bounded table projections: linked tenants, independently
  visible markets, subscriptions, Digital Estates, redacted legal-entity
  profiles, corporate relationships, separately authorised legal-actor
  mandates and immutable provisioning-readiness summaries.
- Exact authenticated organisation scope is re-checked for every request.
  Server-only CP overview/governance reads are reconciled under CX-04D;
  individual tenant-derived domains only enter the table when the specific
  CP `markets_visible`, `subscriptions_visible`, `estates_visible`,
  `mandates_visible` or `readiness_visible` flag is true.
- Explicit `unavailable` and `restricted` states, excluded-section count,
  source, date and lifecycle states; no fake empty/ready/certified values.
- The URL permits **only** `domain,q,state,sort,page,columns`. Strictly
  validated, fixed page size 25, capped bounded source 10k records, sorted
  by displayed name with deterministic key tie-breaker. Compact/detailed
  presentation is a URL-only preference, not a saved authority claim.
- No browser bearer, direct CP transport, arbitrary tenant/principal ID,
  bulk mutation, downloads, CSV/Excel export or unredacted evidence.

## Required independent production gates

This is a bounded **Next server-rendered** table over CP's ≤100-per-source
authorised read models. It does not make CP's organisation directory into a
high-volume indexed server-side query. Genuine >500-authority scale requires a
new Shared-pinned snapshot/keyset contract, authority-joined SQL read model,
query-rate limits and audited performance/load tests. No safe single-hop
shortcut from the current bounded source to unrestricted enterprise search.

Separate export and bulk operations require resource-specific permissions,
redaction, privacy policy, audit and immutable approval where appropriate.
Those features remain fail-closed, rather than exposing a tempting ungoverned
button. Cross-engine provider/certification acceptance and live IAM/AWS
staging remain independent gates.
