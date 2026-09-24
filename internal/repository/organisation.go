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

// Evidence is the input to an explicit verification transition. Who
// verified is never part of it: verified_by is recorded from the
// AuditActor of the authenticated caller.
type Evidence struct {
	References []string
	VerifiedAt time.Time
	// Reason is recorded in the audit trail.
	Reason string
}

// OrganisationRepository persists ADR-BCP-018 organisation-domain records.
//
// Ensure* operations are idempotent on the record's natural key: a retry
// returns the existing live row unchanged instead of inserting a duplicate
// or overwriting it. Verify* operations are the only way a record becomes
// VERIFIED, and they require evidence.
//
// Every mutation takes the authenticated AuditActor. A mutation that
// changes state writes an audit_events row and, for ADR-BCP-018 section
// 124 transitions, a messaging.outbox event in the same transaction; a
// replay that changes nothing writes neither (ADR-BCP-018 sections 124-131).
type OrganisationRepository interface {
	// EnsureOrganisation creates the profile if absent; an existing profile
	// (including its verification state) is left untouched.
	EnsureOrganisation(ctx context.Context, org domain.Organisation, actor AuditActor) (created bool, err error)
	GetOrganisation(ctx context.Context, canonicalEntityID string) (*domain.Organisation, error)

	// EnsureLegalEntityProfile creates the profile if absent. It returns
	// ErrOrganisationConflict when the legal entity already belongs to a
	// different organisation.
	EnsureLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile, actor AuditActor) (created bool, err error)
	GetLegalEntityProfile(ctx context.Context, legalEntityID string) (*domain.LegalEntityProfile, error)
	ListLegalEntityProfilesByOrganisation(ctx context.Context, organisationID string) ([]domain.LegalEntityProfile, error)
	VerifyLegalEntityProfile(ctx context.Context, legalEntityID string, ev Evidence, actor AuditActor) error

	// EnsureCorporateRelationship is keyed by (source, target, type).
	EnsureCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship, actor AuditActor) (id string, err error)
	VerifyCorporateRelationship(ctx context.Context, id string, ev Evidence, actor AuditActor) error
	ListCorporateRelationshipsByOrganisation(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error)
	// ListCorporateControlAncestry returns every consequential (VERIFIED,
	// ACTIVE, in-window) OWNS/CONTROLS edge on a directed path into
	// organisationID, bounded by domain.MaxCorporateControlDepth.
	ListCorporateControlAncestry(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error)

	// CreateCorporateGroup is idempotent on the caller-supplied group id.
	CreateCorporateGroup(ctx context.Context, g domain.CorporateGroup, actor AuditActor) error
	// EnsureCorporateGroupMembership is keyed by (group, organisation).
	EnsureCorporateGroupMembership(ctx context.Context, m domain.CorporateGroupMembership, actor AuditActor) (id string, err error)
	ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error)

	// EnsurePlatformRelationship is keyed by (organisation, platform, type).
	EnsurePlatformRelationship(ctx context.Context, rel domain.PlatformRelationship, actor AuditActor) (id string, err error)
	VerifyPlatformRelationship(ctx context.Context, id string, ev Evidence, actor AuditActor) error
	// ListPlatformRelationships returns the relationships in their effective
	// window at at, whatever their status. Callers decide consequence with
	// PlatformRelationship.IsConsequential.
	ListPlatformRelationships(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error)
	// ListPlatformOwnerOrganisations returns organisations holding a
	// consequential PLATFORM_OWNER relationship with platformID at at.
	ListPlatformOwnerOrganisations(ctx context.Context, platformID string, at time.Time) (map[string]struct{}, error)

	// CreatePlatformAccount is idempotent on the caller-supplied account id.
	CreatePlatformAccount(ctx context.Context, acct domain.PlatformAccount, actor AuditActor) error
	// EnsurePlatformAccountMembership is keyed by (account, organisation, role).
	EnsurePlatformAccountMembership(ctx context.Context, m domain.PlatformAccountMembership, actor AuditActor) (id string, err error)
	ListPlatformAccountMemberships(ctx context.Context, accountID string, at time.Time) ([]domain.PlatformAccountMembership, error)

	// EnsureTenantOrganisationMapping is keyed by (tenant, organisation, role).
	EnsureTenantOrganisationMapping(ctx context.Context, m domain.TenantOrganisationMapping, actor AuditActor) (id string, err error)
	ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error)
	// ListLiveTenantOrganisationMappings returns the tenant's PENDING, ACTIVE
	// and SUSPENDED mappings whatever their effective window: the rows the
	// natural-key uniqueness rules apply to.
	ListLiveTenantOrganisationMappings(ctx context.Context, tenantID string) ([]domain.TenantOrganisationMapping, error)
	ListTenantLegalEntityMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantLegalEntityMapping, error)
	// EnsureDefaultTenantLegalEntityMapping makes legalEntityID the tenant's
	// live DEFAULT mapping and keeps tenants.legal_entity_id as its
	// compatibility projection, in one transaction. Replaying it for the
	// current default is a no-op; a different legal entity ends the previous
	// default (history is kept) before the new one is recorded.
	EnsureDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, legalEntityID, provenance string, at time.Time, actor AuditActor) (id string, err error)
	// ListTenantsByDefaultLegalEntity returns the tenants whose live DEFAULT
	// legal-entity mapping names legalEntityID.
	ListTenantsByDefaultLegalEntity(ctx context.Context, legalEntityID string) ([]string, error)

	// ApplyFirstPartyGovernance reconciles one first-party legal entity to
	// its Shared governance record (ADR-BCP-018 section 13): it creates the
	// Organisation and LegalEntityProfile when absent, and otherwise corrects
	// the legal name and verifies both from the registry evidence. Shared
	// wins for these identities; every correction is returned as drift and
	// audited with its previous value. A REJECTED or EXPIRED record is never
	// overturned: it is returned as blocking drift and left untouched.
	// Reconciling an entity that already matches changes and records nothing.
	ApplyFirstPartyGovernance(ctx context.Context, g FirstPartyGovernance, actor AuditActor) (GovernanceOutcome, error)
}

// FirstPartyGovernance is one Shared first-party registry record together
// with the evidence reference that cites the registry revision.
type FirstPartyGovernance struct {
	LegalEntityID     string
	LegalName         string
	EvidenceReference string
	At                time.Time
}

// GovernanceOutcome reports what reconciling one first-party entity did.
type GovernanceOutcome struct {
	OrganisationID string            `json:"organisation_id"`
	Created        bool              `json:"created"`
	Changes        []string          `json:"changes,omitempty"`
	Drift          []GovernanceDrift `json:"drift,omitempty"`
}

// GovernanceDrift is a difference between the Control Plane and Shared
// governance (ADR-BCP-018 section 128). Blocking drift was not corrected
// and needs a governed human decision.
type GovernanceDrift struct {
	Field    string `json:"field"`
	Observed string `json:"observed"`
	Governed string `json:"governed"`
	Blocking bool   `json:"blocking"`
	Reason   string `json:"reason"`
}
