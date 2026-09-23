// Target path: baobab-platform/baobab-cp/internal/repository/organisation.go
//
// ADR-BCP-018 — Repository interfaces for Organisation, relationships and
// tenant mappings.
//
// Implementation notes:
//   - Prefer the existing postgres registry package patterns (see
//     internal/repository/postgres.go and registry.canonical_entity access).
//   - Do not introduce a second DB connection or identity store.
//   - All read paths used for consequential decisions MUST treat missing
//     rows as fail-closed (return empty + nil error, or explicit not-found
//     that callers interpret as deny).
//   - Writes to default TenantLegalEntityMapping MUST keep Tenant.LegalEntityID
//     in sync (see domain.AssertDefaultLegalEntityProjection).

package repository

import (
	"context"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// OrganisationRepository is the runtime persistence port for ADR-BCP-018 concepts.
// Concrete type lives next to existing registry repositories.
type OrganisationRepository interface {
	// --- Organisation profile ---
	UpsertOrganisation(ctx context.Context, org domain.Organisation) error
	GetOrganisation(ctx context.Context, canonicalEntityID string) (*domain.Organisation, error)

	// --- Legal entity profile ---
	UpsertLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile) error
	GetLegalEntityProfile(ctx context.Context, legalEntityID string) (*domain.LegalEntityProfile, error)
	ListLegalEntityProfilesByOrganisation(ctx context.Context, organisationID string) ([]domain.LegalEntityProfile, error)

	// --- Corporate relationships ---
	UpsertCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship) error
	ListCorporateRelationshipsByOrganisation(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error)

	// --- Corporate group ---
	UpsertCorporateGroup(ctx context.Context, g domain.CorporateGroup) error
	UpsertCorporateGroupMembership(ctx context.Context, m domain.CorporateGroupMembership) error
	ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error)

	// --- Platform relationship / account ---
	UpsertPlatformRelationship(ctx context.Context, rel domain.PlatformRelationship) error
	GetPlatformRelationship(ctx context.Context, organisationID string, at time.Time) (*domain.PlatformRelationship, error)
	UpsertPlatformAccount(ctx context.Context, acct domain.PlatformAccount) error
	UpsertPlatformAccountMembership(ctx context.Context, m domain.PlatformAccountMembership) error
	ListPlatformAccountMemberships(ctx context.Context, accountID string, at time.Time) ([]domain.PlatformAccountMembership, error)

	// --- Tenant mappings ---
	UpsertTenantOrganisationMapping(ctx context.Context, m domain.TenantOrganisationMapping) error
	ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error)
	UpsertTenantLegalEntityMapping(ctx context.Context, m domain.TenantLegalEntityMapping) error
	ListTenantLegalEntityMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantLegalEntityMapping, error)

	// SetDefaultTenantLegalEntityMapping sets is_default for the given mapping,
	// clears other defaults for the tenant, and updates Tenant.LegalEntityID
	// to the compatibility projection. Must be transactional.
	SetDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, mappingID string) error
}
