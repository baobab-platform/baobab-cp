// Helpers for effective-dated organisation relationship filtering.
// All consequential use goes through IsConsequential (fail closed).

package domain

import "time"

// FilterConsequentialCorporateRelationships returns edges usable at at.
func FilterConsequentialCorporateRelationships(rels []CorporateRelationship, at time.Time) []CorporateRelationship {
	out := make([]CorporateRelationship, 0, len(rels))
	for _, r := range rels {
		if r.IsConsequential(at) {
			out = append(out, r)
		}
	}
	return out
}

// ProjectTenantLegalEntityID is the singular Tenant.LegalEntityID
// compatibility projection: the legal entity of the active DEFAULT mapping
// in force at at. Empty when there is none or when more than one qualifies,
// so an ambiguous projection fails closed instead of picking one.
func ProjectTenantLegalEntityID(mappings []TenantLegalEntityMapping, at time.Time) string {
	found := ""
	for _, m := range mappings {
		if m.MappingRole != TenantLegalEntityRoleDefault || m.Status != RelationshipStatusActive {
			continue
		}
		if !inWindow(at, m.EffectiveFrom, m.EffectiveTo) {
			continue
		}
		if found != "" && found != m.LegalEntityID {
			return ""
		}
		found = m.LegalEntityID
	}
	return found
}
