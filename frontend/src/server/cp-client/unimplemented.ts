// Operations the pinned Shared control-plane/v1 OpenAPI declares but the
// Control Plane does not serve (FE-00 gap G2), or serves but not yet to their
// description (the legacy provisioning routes until the ADR-SHARED-015
// migration conforms them). The typed client removes them, so Console code
// cannot call a route that does not exist or does not match its contract.
// api.TestOpenAPIDescribesTheRouter holds this list equal to the Control
// Plane's unimplemented and nonconforming lists; update both together.
export const UNIMPLEMENTED_OPERATIONS = [
  "GET /markets/{market_id}",
  "PATCH /markets/{market_id}",
  "POST /markets",
  "POST /markets/{market_id}/activate",
  // Described by ADR-SHARED-015, not yet served.
  "GET /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}/plan",
  "POST /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}/plan",
  "POST /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}/approve",
  "POST /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}/remediate",
  "POST /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}/withdraw",
  "POST /admin/operations/{operation_id}/cancel",
  "POST /admin/operations/{operation_id}/retry",
  // Served, but not yet to their ADR-SHARED-015 description.
  "GET /tenants/{tenant_id}/provisioning",
  "GET /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}",
  "GET /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}/drift",
  "GET /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}/readiness",
  "POST /tenants/{tenant_id}/provisioning",
  "POST /tenants/{tenant_id}/provisioning/{tenant_provisioning_id}/apply",
] as const;

export type UnimplementedOperation = (typeof UNIMPLEMENTED_OPERATIONS)[number];
