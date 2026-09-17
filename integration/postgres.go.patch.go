// Target path: internal/repository/postgres.go
package integration

/*
Implement TenantProvisioningRepository against provisioning.tenant_provisioning
created by migration 000038.

Update shape:

result, err := r.pool.Exec(ctx, `
 UPDATE provisioning.tenant_provisioning
 SET status=$2,
     desired_state_version=$3,
     observed_state_version=$4,
     product_requests=$5::jsonb,
     market_requests=$6::jsonb,
     isolation_requirement=NULLIF($7,''),
     residency_requirement=NULLIF($8,''),
     blocking_reasons=$9::jsonb,
     attempt_count=$10,
     last_error=NULLIF($11,''),
     completed_at=$12,
     version=$13,
     metadata=$14::jsonb,
     updated_at=now()
 WHERE tenant_provisioning_id=$1::uuid AND version=$15`,
 ...)
if err != nil { return err }
if result.RowsAffected()!=1 {
    return ErrOptimisticLock
}

Get/Create MUST marshal/unmarshal product_requests, market_requests,
blocking_reasons and metadata. The existing UNIQUE(tenant_id,idempotency_key)
is the authoritative duplicate-operation guard. If the same key arrives with
a different request_hash, reject it as an idempotency conflict rather than
replaying a different desired state.
*/
