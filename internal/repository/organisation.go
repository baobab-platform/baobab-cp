// OrganisationRepository is implemented by PostgresRepository (pgx pool).

package repository

import (
	"context"
	"errors"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
)

// ErrOrganisationConflict reports that a write would silently change an
// existing identity fact (for example re-pointing a legal entity to another
// organisation). Such changes need a governed transition, never an upsert.
var ErrOrganisationConflict = errors.New("organisation identity conflict")

// Evidence is the input to an explicit verification transition. VerifiedBy
// must be derived from the authenticated principal, never from a payload.
type Evidence struct {
	References []string
	VerifiedBy string
	VerifiedAt time.Time
}

// OrganisationRepository persists ADR-BCP-018 organisation-domain records.
//
// Ensure* operations are idempotent on the record's natural key: a retry
// returns the existing live row unchanged instead of inserting a duplicate
// or overwriting it. Verify* operations are the only way a record becomes
// VERIFIED, and they require evidence.
type OrganisationRepository interface {
	// EnsureOrganisation creates the profile if absent; an existing profile
	// (including its verification state) is left untouched.
	EnsureOrganisation(ctx context.Context, org domain.Organisation) (created bool, err error)
	GetOrganisation(ctx context.Context, canonicalEntityID string) (*domain.Organisation, error)

	// EnsureLegalEntityProfile creates the profile if absent. It returns
	// ErrOrganisationConflict when the legal entity already belongs to a
	// different organisation.
	EnsureLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile) (created bool, err error)
	GetLegalEntityProfile(ctx context.Context, legalEntityID string) (*domain.LegalEntityProfile, error)
	ListLegalEntityProfilesByOrganisation(ctx context.Context, organisationID string) ([]domain.LegalEntityProfile, error)
	VerifyLegalEntityProfile(ctx context.Context, legalEntityID string, ev Evidence) error

	// EnsureCorporateRelationship is keyed by (source, target, type).
	EnsureCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship) (id string, err error)
	VerifyCorporateRelationship(ctx context.Context, id string, ev Evidence) error
	ListCorporateRelationshipsByOrganisation(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error)
	// ListCorporateControlAncestry returns every consequential (VERIFIED,
	// ACTIVE, in-window) OWNS/CONTROLS edge on a directed path into
	// organisationID, bounded by domain.MaxCorporateControlDepth.
	ListCorporateControlAncestry(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error)

	// CreateCorporateGroup is idempotent on the caller-supplied group id.
	CreateCorporateGroup(ctx context.Context, g domain.CorporateGroup) error
	// EnsureCorporateGroupMembership is keyed by (group, organisation).
	EnsureCorporateGroupMembership(ctx context.Context, m domain.CorporateGroupMembership) (id string, err error)
	ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error)

	// EnsurePlatformRelationship is keyed by (organisation, platform, type).
	EnsurePlatformRelationship(ctx context.Context, rel domain.PlatformRelationship) (id string, err error)
	VerifyPlatformRelationship(ctx context.Context, id string, ev Evidence) error
	// ListPlatformRelationships returns the relationships in their effective
	// window at at, whatever their status. Callers decide consequence with
	// PlatformRelationship.IsConsequential.
	ListPlatformRelationships(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error)
	// ListPlatformOwnerOrganisations returns organisations holding a
	// consequential PLATFORM_OWNER relationship with platformID at at.
	ListPlatformOwnerOrganisations(ctx context.Context, platformID string, at time.Time) (map[string]struct{}, error)

	// CreatePlatformAccount is idempotent on the caller-supplied account id.
	CreatePlatformAccount(ctx context.Context, acct domain.PlatformAccount) error
	// EnsurePlatformAccountMembership is keyed by (account, organisation, role).
	EnsurePlatformAccountMembership(ctx context.Context, m domain.PlatformAccountMembership) (id string, err error)
	ListPlatformAccountMemberships(ctx context.Context, accountID string, at time.Time) ([]domain.PlatformAccountMembership, error)

	// EnsureTenantOrganisationMapping is keyed by (tenant, organisation, role).
	EnsureTenantOrganisationMapping(ctx context.Context, m domain.TenantOrganisationMapping) (id string, err error)
	ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error)
	ListTenantLegalEntityMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantLegalEntityMapping, error)
	// EnsureDefaultTenantLegalEntityMapping makes legalEntityID the tenant's
	// live DEFAULT mapping and keeps tenants.legal_entity_id as its
	// compatibility projection, in one transaction. Replaying it for the
	// current default is a no-op; a different legal entity ends the previous
	// default (history is kept) before the new one is recorded.
	EnsureDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, legalEntityID, provenance string, at time.Time) (id string, err error)
}
