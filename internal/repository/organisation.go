// Target path: internal/repository/organisation.go
//
// OrganisationRepository is implemented by PostgresRepository (pgx pool).

package repository

import (
	"context"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
)

// OrganisationRepository persists ADR-BCP-018 organisation-domain records.
type OrganisationRepository interface {
	UpsertOrganisation(ctx context.Context, org domain.Organisation) error
	GetOrganisation(ctx context.Context, canonicalEntityID string) (*domain.Organisation, error)

	UpsertLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile) error
	GetLegalEntityProfile(ctx context.Context, legalEntityID string) (*domain.LegalEntityProfile, error)
	ListLegalEntityProfilesByOrganisation(ctx context.Context, organisationID string) ([]domain.LegalEntityProfile, error)

	UpsertCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship) error
	ListCorporateRelationshipsByOrganisation(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error)

	UpsertCorporateGroup(ctx context.Context, g domain.CorporateGroup) error
	UpsertCorporateGroupMembership(ctx context.Context, m domain.CorporateGroupMembership) error
	ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error)

	UpsertPlatformRelationship(ctx context.Context, rel domain.PlatformRelationship) error
	// ListPlatformRelationships returns all active relationships for the organisation at at.
	// Concurrent types (e.g. PARTNER + EXTERNAL_CLIENT) are allowed.
	ListPlatformRelationships(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error)

	UpsertPlatformAccount(ctx context.Context, acct domain.PlatformAccount) error
	UpsertPlatformAccountMembership(ctx context.Context, m domain.PlatformAccountMembership) error
	ListPlatformAccountMemberships(ctx context.Context, accountID string, at time.Time) ([]domain.PlatformAccountMembership, error)

	UpsertTenantOrganisationMapping(ctx context.Context, m domain.TenantOrganisationMapping) error
	ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error)
	UpsertTenantLegalEntityMapping(ctx context.Context, m domain.TenantLegalEntityMapping) error
	ListTenantLegalEntityMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantLegalEntityMapping, error)

	// EnsureDefaultTenantLegalEntityMapping upserts the default mapping and
	// keeps tenants.legal_entity_id as the compatibility projection (same tx).
	EnsureDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, legalEntityID, provenance string, at time.Time) error
}
