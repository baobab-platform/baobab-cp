# DEV-06 current grant and binding enforcement

The normal capability resolver re-reads provenance before entitlement and
provider selection. A PRODUCT_SUBSCRIPTION grant needs its exact MATERIALIZED
projection (same subscription, tenant, capability and grant), an ACTIVE source
subscription and its current effective window. Raw UUID source references from
the existing composition producer and canonical sub_ references are supported.
A withdrawn source does not delete the historical grant or projection. Other
independent grant sources retain their existing effectiveness/scope rules.

Provider activation at binding creation is insufficient: each invocation now
checks the selected provider remains ACTIVE, on the selected instance's engine,
and ACTIVELY supports that contract major within its effective window. Existing
scope, binding retirement, topology, health, isolation, residency and invocation
registration checks still apply. Historical ListBindings/ListGrants behaviour is
unchanged so governance and recovery can inspect withdrawn records.

No stored positive decision or provider result may replace these current reads.
Errors are unavailable, never permission. Decisions still go to the normal
capability_resolution audit. These sequential current reads do not claim a
serializable fence across an external provider invocation; the resource server
must independently enforce the fresh decision and record the resource effect.

Owner PG17 tests exercise native composition-projected source grants, suspension,
future/expired source dates, projection withdrawal and source absence, plus
provider/support suspension and support expiry. API regressions retain the
absent-subscription, unentitled scoped-resource and retired-binding denials.
They are synthetic owner evidence, not independent legal, provider certification,
INT-07, protected or production acceptance. No activation, grant or source is
invented by this runtime fix. DEV-10 remains paused.
