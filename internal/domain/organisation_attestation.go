package domain

import (
	"errors"
	"time"
)

// Organisation attestation errors. Each names why a caller-asserted
// organisation_id was refused; none of them is ever softened into success.
var (
	ErrOrganisationNotOrganisationKind = errors.New("requested organisation_id is not a canonical organisation entity")
	ErrOrganisationNotActive           = errors.New("requested organisation is not active")
	ErrOrganisationNotMappedToTenant   = errors.New("requested organisation is not mapped to the requesting tenant")
)

// AttestOrganisation decides whether tenantID may act in the context of the
// canonical organisation entity (ADR-BCP-016 organisation_id attestation,
// generalised by ADR-BCP-018 gate ORG-14).
//
//   - Generic ORGANISATION entities are platform-scoped and may serve many
//     tenants (section 98), so the requesting tenant must hold an ACTIVE,
//     in-effect TenantOrganisationMapping to the organisation. Which tenant
//     happened to register the entity grants nothing on its own, and an
//     ended mapping stops attesting even for that tenant.
//   - BUYER_ORGANISATION and SUPPLIER_ORGANISATION remain ADR-BCP-016's
//     tenant-owned domain records (section 11 keeps them valid): the owning
//     tenant attests as before, and an explicit mapping attests too.
//
// Corporate relationships, CorporateGroup membership and PlatformAccount
// membership are deliberately not inputs: a parent company, a sibling in the
// same group or a co-member of the same account gains no access to another
// organisation's context (sections 29, 41, 60, 89). mappings must be the
// requesting tenant's mappings.
func AttestOrganisation(entity CanonicalEntity, tenantID string, mappings []TenantOrganisationMapping, at time.Time) error {
	if !OrganisationEntityTypes[entity.EntityType] {
		return ErrOrganisationNotOrganisationKind
	}
	if entity.Status != "ACTIVE" {
		return ErrOrganisationNotActive
	}
	for _, m := range mappings {
		if m.TenantID == tenantID && m.OrganisationID == entity.ID &&
			m.Status == RelationshipStatusActive && inWindow(at, m.EffectiveFrom, m.EffectiveTo) {
			return nil
		}
	}
	if entity.EntityType != EntityTypeOrganisation && entity.OwnerTenantID == tenantID {
		return nil
	}
	return ErrOrganisationNotMappedToTenant
}
