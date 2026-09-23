// Target path: baobab-platform/baobab-cp/internal/domain/tenant_mapping_compat.go
//
// ADR-BCP-018 — Compatibility projection for Tenant.LegalEntityID.
//
// The singular Tenant.LegalEntityID field is retained as a compatibility
// projection of the default TenantLegalEntityMapping (IsDefault=true).
// New code SHOULD prefer explicit mapping APIs; this helper keeps the
// projection consistent when mappings are written or backfilled.
//
// Rules:
//   - Exactly one default TenantLegalEntityMapping SHOULD exist for every
//     active tenant.
//   - Writing a new default mapping MUST update Tenant.LegalEntityID to match.
//   - Clearing the default mapping without a replacement is a data-integrity
//     incident (fail closed).
//   - Historical migrations are not rewritten; backfill is forward-only.

package domain

import (
	"fmt"
	"time"
)

// DefaultLegalEntityIDFromMappings returns the legal_entity_id of the active
// default mapping at the given time, or empty string if none.
// Callers that require a default MUST treat empty as a fail-closed condition.
func DefaultLegalEntityIDFromMappings(mappings []TenantLegalEntityMapping, at time.Time) string {
	for _, m := range mappings {
		if !m.IsDefault {
			continue
		}
		if m.Status != "ACTIVE" {
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

// AssertDefaultLegalEntityProjection checks that tenant.LegalEntityID matches
// the default mapping projection. Returns an error if conflicted or missing
// when a default is required.
//
// This is intentionally strict: consequential tenancy decisions fail closed.
func AssertDefaultLegalEntityProjection(legalEntityID string, mappings []TenantLegalEntityMapping, at time.Time) error {
	projected := DefaultLegalEntityIDFromMappings(mappings, at)
	if projected == "" {
		if legalEntityID == "" {
			return fmt.Errorf("tenant has no default TenantLegalEntityMapping and empty LegalEntityID")
		}
		// Singular field present but no mapping row: transitional state during
		// backfill is allowed only if callers treat it as compatibility-only.
		return nil
	}
	if legalEntityID != "" && legalEntityID != projected {
		return fmt.Errorf(
			"Tenant.LegalEntityID %q conflicts with default TenantLegalEntityMapping %q",
			legalEntityID, projected,
		)
	}
	return nil
}

// NewDefaultTenantLegalEntityMapping constructs a default mapping row for
// backfill from an existing Tenant.LegalEntityID.
func NewDefaultTenantLegalEntityMapping(id, tenantID, legalEntityID string, from time.Time) TenantLegalEntityMapping {
	return TenantLegalEntityMapping{
		ID:            id,
		TenantID:      tenantID,
		LegalEntityID: legalEntityID,
		IsDefault:     true,
		Status:        "ACTIVE",
		EffectiveFrom: from,
	}
}
