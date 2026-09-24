package domain

// Known first-class CanonicalEntity.EntityType values. EntityType itself
// remains a plain string (see canonical.go) — the control plane is
// intentionally data-driven and does not enforce a closed enum here.
// These constants exist so callers spell known kinds consistently rather
// than repeating string literals, and so a new kind's canonical vocabulary
// is documented in one place.
const (
	// EntityTypeProduct is the canonical entity kind for a product, e.g.
	// canonical_key "product:green-coffee:ethiopia-guji".
	EntityTypeProduct = "PRODUCT"

	// EntityTypeSupplierOrganisation is the canonical entity kind for a
	// prospective or approved supplier organisation, e.g. canonical_key
	// "supplier:<estate>:<estate-local-application-id>" (see ADR-0006).
	// Registration happens through the existing entity-type-agnostic
	// CanonicalEntityService.Create API once a hosting estate is ready to
	// call it.
	EntityTypeSupplierOrganisation = "SUPPLIER_ORGANISATION"

	// EntityTypeBuyerOrganisation is the canonical entity kind for a
	// buyer organisation, e.g. canonical_key "buyer:<estate>:<estate-
	// local-organisation-id>" (see ADR-BCP-016, which registers this
	// constant following ADR-0006's identical precedent and reconciles it
	// with ADR-BCP-014's broader Organisation/Counterparty model).
	// Registration happens through the same CanonicalEntityService.Create
	// API as EntityTypeSupplierOrganisation.
	EntityTypeBuyerOrganisation = "BUYER_ORGANISATION"

	// EntityTypeOrganisation is the generic ADR-BCP-018 organisation profile
	// on CanonicalEntity. BUYER_ORGANISATION and SUPPLIER_ORGANISATION remain
	// specialised kinds; both are organisation entity types for context resolution.
	EntityTypeOrganisation = "ORGANISATION"
)

// OrganisationEntityTypes lists every CanonicalEntity.EntityType value the
// control plane recognises as "an organisation" for context-resolution
// purposes (internal/provisioning.AuthoritativeContextResolver's
// OrganisationID stage, ADR-BCP-016). A canonical entity of any other
// EntityType can never be asserted as a request's organisation_id, even if
// its ID is otherwise well-formed.
var OrganisationEntityTypes = map[string]bool{
	EntityTypeOrganisation:         true,
	EntityTypeBuyerOrganisation:    true,
	EntityTypeSupplierOrganisation: true,
}
