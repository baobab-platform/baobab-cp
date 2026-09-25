package api

// resolveWorkloadTenant reconciles a workload token's own tenant_id claim
// (principalTenant) against a caller-supplied tenant (requestedTenant --
// a request body field, or a previously-resolved Context's own TenantID).
//
// No workload client in baobab-platform/baobab-iam mints a tenant_id claim today
// (client-credentials tokens are static per-client, and ADR-0007 SS88
// explicitly allows one workload identity, e.g. baobab-trade, to serve many
// tenants -- a static claim can't express that), so principalTenant is
// empty for every real workload request. Treating that as "no tenant
// authorized" and 403-ing every workload call, as api/router.go's
// authorize() and every handler using this used to, made these endpoints
// unusable outside of tests that set a synthetic principal.TenantID by
// hand.
//
// ADR-0007 SS91 ("No Scope-to-Tenant Shortcut") still governs: scope alone
// SHALL NOT authorize the requested tenant, so requestedTenant is never
// trusted unconditionally. When the token DOES carry its own tenant_id,
// requestedTenant SHALL still equal it -- this preserves the existing
// cross-tenant-spoofing protection unchanged for any token that has one.
// Only when the token carries none does requestedTenant become the tenant
// to act on, still subject to whatever check already gates that value
// downstream (tenant existence/active state via ContextResolutionService,
// and CapabilityGrant entitlement once enforced).
func resolveWorkloadTenant(principalTenant, requestedTenant string) (tenant string, ok bool) {
	if principalTenant == "" {
		return requestedTenant, requestedTenant != ""
	}
	if requestedTenant != "" && requestedTenant != principalTenant {
		return "", false
	}
	return principalTenant, true
}
