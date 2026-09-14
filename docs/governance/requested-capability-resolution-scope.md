# Requested capability resolution scope

## Accepted decisions applied

The accepted capability architecture and Shared capability contract require a consumer to request an implementation-neutral capability key such as `commerce.order.create` or `commercial.quotation.manage`. Control Plane selects a provider and engine binding for that capability. A product name, provider name, or engine name cannot silently substitute for the requested capability.

The previous resolver accepted no capability key and hardcoded `baobab_trade` throughout binding, grant, registry, trace, and cache resolution. That made different commercial capabilities indistinguishable and allowed a cached routing decision to be reused across capability boundaries.

## Implementation

- `/v1/resolve`, `/v1/capabilities/resolve`, `/v1/capabilities/resolve-batch`, and `/v1/capabilities/explain` now require `capability_key`.
- The service and pipeline pass that exact key to binding, grant, lifecycle, policy, and trace resolution.
- Resolution caches include the requested capability key.
- Capability keys must satisfy Shared's implementation-neutral grammar.
- Batch resolution keeps independent decisions per canonical entity under the explicitly requested capability.

No Digital Estate or domain provider chooses the engine instance. The caller requests a business capability; Control Plane remains the routing and platform-entitlement authority, and the selected domain engine remains responsible for business authorization.

## Rollout dependency

The existing `EnforceEntitlement` rollout switch remains fail-closed when enabled but is not enabled globally until capability and grant provisioning is backfilled. This increment removes the hardcoded-key blocker; it does not fabricate entitlement rows. Production readiness for a capability still requires its definition, grant, binding, provider, conformance, and provisioning evidence.
