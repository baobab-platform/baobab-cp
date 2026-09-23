// Target path: baobab-platform/baobab-cp/internal/domain/organisation_entity_type.go
//
// ADR-BCP-018 — EntityType constant for generic Organisation.
//
// Adds EntityTypeOrganisation without removing or renaming existing
// EntityTypeBuyerOrganisation / EntityTypeSupplierOrganisation
// (ADR-BCP-016 / ADR-0006). Those specialisations remain valid and
// migrate forward without destructive rewrite.
//
// Integration note:
//   Merge the constant below into the existing entity_types.go (or equivalent)
//   EntityType block. Do not create a parallel EntityType registry.

package domain

// EntityTypeOrganisation is the generic real-world organisation EntityType.
// Prefer this for new Organisation CanonicalEntity records that are not
// specialised as buyer or supplier at creation time.
//
// Existing BUYER_ORGANISATION and SUPPLIER_ORGANISATION values remain valid.
// Merge this constant into the existing entity_types.go EntityType block;
// the names EntityTypeBuyerOrganisation / EntityTypeSupplierOrganisation
// below must match the live constants in that file (adjust on integration).
const EntityTypeOrganisation EntityType = "ORGANISATION"

// Organisation-shaped specialised types (ADR-BCP-016 / ADR-0006).
// These aliases document intent; on integration, replace with the real
// constants from entity_types.go if their identifiers differ.
const (
	entityTypeBuyerOrganisation    EntityType = "BUYER_ORGANISATION"
	entityTypeSupplierOrganisation EntityType = "SUPPLIER_ORGANISATION"
)

// IsOrganisationEntityType reports whether t is any organisation-shaped
// EntityType (generic or specialised).
func IsOrganisationEntityType(t EntityType) bool {
	switch t {
	case EntityTypeOrganisation,
		entityTypeBuyerOrganisation,
		entityTypeSupplierOrganisation:
		return true
	default:
		return false
	}
}
