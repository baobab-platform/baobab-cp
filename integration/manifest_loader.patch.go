// Target path: internal/provisioning/manifest_loader.go
package integration

/*
Production integration requirements:

1. Parse YAML/JSON into provisioning.TenantManifest.
2. Validate schema and semantic references.
3. Resolve symbolic values through authoritative CP registries:
   - tenant_id / legal_entity_id
   - market_code -> Market.ID
   - digital_estate -> DigitalEstate.ID
   - capability_key -> Capability.ID
   - engine -> Engine.ID
   - engine_instance -> EngineInstance.ID
4. Reject unknown, inactive or cross-tenant references.
5. Canonicalize the validated manifest and compute request_hash.
6. Create/reuse TenantProvisioning using (tenant_id,idempotency_key).
7. If the key exists with a different request_hash, reject the request.
8. Increment desired_state_version only through an explicit desired-state
   update; never infer a new version from retries.
9. Feed the resolved declaration into APPLY materializers and the independent
   desired readers used by reconciliation.
10. Persist or otherwise retain the exact versioned desired declaration so
    reconciliation can reproduce what version N meant.

Do not let the manifest mint canonical IDs or redefine Shared contracts.
*/
