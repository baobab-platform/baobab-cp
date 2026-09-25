// ADR-BCP-018 — organisation structure for a tenant.
//
// Provisioning records claims, never verified facts: the Organisation and
// LegalEntityProfile it creates are UNVERIFIED and the PlatformRelationship
// is PENDING / PENDING_REVIEW. Only the repository's Verify* transitions,
// driven by evidence review (ADR-BCP-023), make any of them VERIFIED.
//
// Every write is idempotent on its natural key, so a retried or replayed
// provisioning request converges on the same rows.

package organisation

import (
	"context"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// DefaultPlatformID is the Baobab Platform instance relationships attach to
// when the caller does not name one.
const DefaultPlatformID = "baobab-platform"

// ProvisionRequest is the input to attach organisation structure to a tenant.
type ProvisionRequest struct {
	TenantID          string
	CanonicalEntityID string // uuid of an existing organisation CanonicalEntity
	LegalEntityID     string
	DisplayName       string
	OfficialName      string
	OrganisationForm  domain.OrganisationForm
	Jurisdiction      string
	LegalName         string
	SourceAuthority   string
	PlatformID        string
	PlatformRelType   domain.PlatformRelationshipType
	// PlatformRelAuthority must be a server-side authority for privileged
	// types (PLATFORM_OWNER, PLATFORM_OPERATOR, PLATFORM_GROUP_AFFILIATE).
	PlatformRelAuthority string
	// BasisRelationshipID is required for PLATFORM_GROUP_AFFILIATE.
	BasisRelationshipID string
	AdmissionDecisionID string
	EffectiveFrom       time.Time
	SkipPlatformRel     bool
	// Actor is the authenticated principal the change is audited against
	// (ADR-BCP-018 section 131). Required.
	Actor repository.AuditActor
}

// ProvisionResult names the rows provisioning converged on.
type ProvisionResult struct {
	OrganisationCreated        bool
	LegalEntityProfileCreated  bool
	TenantLegalEntityMappingID string
	TenantOrganisationMapping  string
	PlatformRelationshipID     string
}

// Provisioner writes organisation profile, legal entity profile, mappings,
// and an optional pending platform relationship.
type Provisioner struct {
	Orgs repository.OrganisationRepository
}

// ProvisionTenantOrganisation is idempotent: replaying the same request
// returns the same mapping and relationship ids and changes nothing.
func (p *Provisioner) ProvisionTenantOrganisation(ctx context.Context, req ProvisionRequest) (ProvisionResult, error) {
	var res ProvisionResult
	if req.TenantID == "" || req.CanonicalEntityID == "" || req.LegalEntityID == "" {
		return res, fmt.Errorf("organisation provision: tenant_id, canonical_entity_id and legal_entity_id are required")
	}
	if req.DisplayName == "" || req.SourceAuthority == "" {
		return res, fmt.Errorf("organisation provision: display_name and source_authority are required")
	}
	if req.EffectiveFrom.IsZero() {
		req.EffectiveFrom = time.Now().UTC()
	}
	if req.PlatformID == "" {
		req.PlatformID = DefaultPlatformID
	}
	if !req.SkipPlatformRel {
		if err := assertPlatformRelAuthority(req.PlatformRelType, req.PlatformRelAuthority); err != nil {
			return res, err
		}
	}

	var err error
	res.OrganisationCreated, err = p.Orgs.EnsureOrganisation(ctx, domain.Organisation{
		CanonicalEntityID: req.CanonicalEntityID,
		DisplayName:       req.DisplayName,
		OfficialName:      req.OfficialName,
		OrganisationForm:  req.OrganisationForm,
		Jurisdiction:      req.Jurisdiction,
		VerificationState: domain.VerificationUnverified,
		SourceAuthority:   req.SourceAuthority,
		Status:            "ACTIVE",
		EffectiveFrom:     req.EffectiveFrom,
	}, req.Actor)
	if err != nil {
		return res, fmt.Errorf("ensure organisation: %w", err)
	}

	legalName := firstNonEmpty(req.LegalName, req.OfficialName, req.DisplayName)
	res.LegalEntityProfileCreated, err = p.Orgs.EnsureLegalEntityProfile(ctx, domain.LegalEntityProfile{
		LegalEntityID:               req.LegalEntityID,
		OrganisationID:              req.CanonicalEntityID,
		LegalName:                   legalName,
		JurisdictionOfIncorporation: req.Jurisdiction,
		LegalStatus:                 domain.LegalStatusUnknown,
		SourceAuthority:             req.SourceAuthority,
		VerificationState:           domain.VerificationUnverified,
		EffectiveFrom:               req.EffectiveFrom,
	}, req.Actor)
	if err != nil {
		return res, fmt.Errorf("ensure legal entity profile: %w", err)
	}

	if res.TenantLegalEntityMappingID, err = p.Orgs.EnsureDefaultTenantLegalEntityMapping(ctx, req.TenantID, req.LegalEntityID, req.SourceAuthority, req.EffectiveFrom, req.Actor); err != nil {
		return res, fmt.Errorf("default tenant legal entity mapping: %w", err)
	}

	if res.TenantOrganisationMapping, err = p.Orgs.EnsureTenantOrganisationMapping(ctx, domain.TenantOrganisationMapping{
		TenantID:       req.TenantID,
		OrganisationID: req.CanonicalEntityID,
		MappingRole:    domain.TenantOrgRolePrimary,
		Status:         domain.RelationshipStatusActive,
		EffectiveFrom:  req.EffectiveFrom,
		Provenance:     req.SourceAuthority,
	}, req.Actor); err != nil {
		return res, fmt.Errorf("tenant organisation mapping: %w", err)
	}

	if !req.SkipPlatformRel {
		if res.PlatformRelationshipID, err = p.Orgs.EnsurePlatformRelationship(ctx, domain.PlatformRelationship{
			PlatformID:          req.PlatformID,
			OrganisationID:      req.CanonicalEntityID,
			RelationshipType:    req.PlatformRelType,
			VerificationState:   domain.VerificationPendingReview,
			Status:              domain.RelationshipStatusPending,
			EffectiveFrom:       req.EffectiveFrom,
			BasisRelationshipID: req.BasisRelationshipID,
			AdmissionDecisionID: req.AdmissionDecisionID,
			SourceAuthority:     req.PlatformRelAuthority,
		}, req.Actor); err != nil {
			return res, fmt.Errorf("platform relationship: %w", err)
		}
	}
	return res, nil
}

// applicantAuthorities are sources that may never establish first-party status.
var applicantAuthorities = map[string]bool{"": true, "applicant-submission": true}

func assertPlatformRelAuthority(t domain.PlatformRelationshipType, authority string) error {
	if t == "" {
		return fmt.Errorf("platform relationship type is required unless SkipPlatformRel is set")
	}
	if t.IsPrivileged() && applicantAuthorities[authority] {
		return fmt.Errorf("platform relationship type %s requires a server-side source_authority", t)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
