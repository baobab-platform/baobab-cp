// Target path: internal/provisioning/composition_root.go
package integration

/*
Wire concrete ResourceReadinessProbe implementations to accepted repositories
and resolvers:

market-participation:
  every requested market has an effective participation for this tenant/legal
  entity; suspended/expired participation fails closed.

capability-grants:
  every Release-1 requested capability has at least one effective compatible
  CapabilityGrant.

capability-bindings:
  every required capability deterministically resolves to one authoritative
  ACTIVE binding; ambiguity is a blocker.

engine-instances:
  each selected binding resolves through TopologyResolverImpl to an eligible
  ACTIVE/healthy instance satisfying environment, region, isolation and
  residency.

context-resolution:
  authoritative Context can be resolved for each requested market with
  trusted provenance and without tenant/legal-entity mismatch.

trade-lanes:
  required cross-market routes are ACTIVE and their market participations are
  effective.

isolation-and-residency:
  tenant isolation profile and selected engine topology satisfy declared
  isolation_requirement/residency_requirement.

Never make readiness depend on frontend reachability alone. Provider-specific
health checks can supplement, but cannot replace, CP desired/observed-state
proof.
*/
