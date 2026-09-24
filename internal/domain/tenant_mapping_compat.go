// Target path: internal/domain/tenant_mapping_compat.go
//
// Compatibility projection of Tenant.LegalEntityID from TenantLegalEntityMapping.

package domain

import "time"

// ProjectTenantLegalEntityID is the singular compatibility projection used
// while consumers still read Tenant.LegalEntityID. Empty when no default mapping.
func ProjectTenantLegalEntityID(mappings []TenantLegalEntityMapping, at time.Time) string {
	return DefaultTenantLegalEntityID(mappings, at)
}
