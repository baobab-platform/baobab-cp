// Target path: internal/domain/relationship_resolution.go
//
// Helpers for effective-dated corporate relationship filtering.
// All consequential use must go through IsConsequential (fail closed).

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

// ActivePlatformRelationships filters platform relationships in force at at
// with authoritative verification (or PENDING for non-consequential display).
func ActivePlatformRelationships(rels []PlatformRelationship, at time.Time) []PlatformRelationship {
	out := make([]PlatformRelationship, 0, len(rels))
	for _, r := range rels {
		if r.IsActive(at) {
			out = append(out, r)
		}
	}
	return out
}

// DefaultTenantLegalEntityID returns the legal_entity_id of the default mapping
// if present and active at at; otherwise empty string (fail closed for projection).
func DefaultTenantLegalEntityID(mappings []TenantLegalEntityMapping, at time.Time) string {
	for _, m := range mappings {
		if !m.IsDefault || m.Status != "ACTIVE" {
			continue
		}
		if at.Before(m.EffectiveFrom) {
			continue
		}
		if m.EffectiveTo != nil && !at.Before(*m.EffectiveTo) {
			continue
		}
		return m.LegalEntityID
	}
	return ""
}
