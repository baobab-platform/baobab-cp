// Target path: internal/repository/postgres.go
// Merge these corrections into existing binding persistence.
package integration

/*
CreateBinding currently has two production-critical gaps for provisioning:

1. It does not check RowsAffected after INSERT ... SELECT. An unknown capability
   or mismatched engine/instance can silently insert zero rows.
2. It does not persist caller EffectiveFrom/EffectiveTo; it always uses now()
   and omits effective_to.

Replace the final CreateBinding INSERT with the following shape:

result, err := r.pool.Exec(ctx, `
    INSERT INTO capability.capability_binding(
        id, capability_id, engine_instance_id, scope_id, binding_mode,
        priority, status, contract_version, effective_from, effective_to
    )
    SELECT $1::uuid, c.capability_id, ei.engine_instance_id, $5::uuid, UPPER($6),
           $7, UPPER($8), $9, $10, $11
    FROM capability.capability c
    JOIN topology.engine_instance ei
      ON ei.engine_instance_id=$4::uuid AND ei.engine_id=$3::uuid
    WHERE c.code=$2`,
    binding.ID, binding.CapabilityKey, binding.EngineID,
    binding.EngineInstanceID, binding.ScopeID, string(binding.BindingMode),
    binding.Priority, binding.Status, binding.ContractVersion,
    binding.EffectiveFrom, binding.EffectiveTo,
)
if err != nil { return err }
if result.RowsAffected() == 0 {
    return fmt.Errorf("capability or engine instance not found/mismatched for binding")
}
return nil

Also extend ListBindings SELECT/Scan to include:
    cb.effective_from, cb.effective_to, cb.version
so resolver temporal checks operate on persisted truth.

Do not add provider selection shortcuts here. Provider/engine-instance eligibility
belongs to provisioning/resolver policy, not raw SQL.
*/
