// Target path: internal/repository/repository.go
package integration

/*
Add the orchestration persistence contract without replacing the existing
RegisterTenant idempotency path:

type TenantProvisioningRepository interface {
    GetTenantProvisioning(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error)
    GetTenantProvisioningByIdempotencyKey(ctx context.Context, tenantID, key string) (provisioningdomain.TenantProvisioning, error)
    CreateTenantProvisioning(ctx context.Context, operation provisioningdomain.TenantProvisioning) error
    UpdateTenantProvisioning(ctx context.Context, operation provisioningdomain.TenantProvisioning, expectedVersion int64) error
}

UpdateTenantProvisioning MUST use:
    WHERE tenant_provisioning_id=$id AND version=$expectedVersion
and MUST fail if RowsAffected()!=1. Never permit two workers to advance the
same orchestration record concurrently without optimistic-lock detection.
*/
