// Target path: internal/service/organisation/provision.go
//
// ADR-BCP-018 — Idempotent organisation structure for a tenant.
// First-party identity is not hard-coded here; callers pass SourceAuthority
// and PlatformRelType from admission policy / Shared governance reconciliation.

package organisation

import (
	"context"
	"fmt"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// ProvisionRequest is the input to attach organisation structure to a tenant.
type ProvisionRequest struct {
	TenantID             string
	CanonicalEntityID    string // uuid string of CanonicalEntity
	LegalEntityID        string
	DisplayName          string
	OfficialName         string
	OrganisationForm     domain.OrganisationForm
	Jurisdiction         string
	LegalName            string
	SourceAuthority      string
	PlatformID           string
	PlatformRelType      domain.PlatformRelationshipType
	PlatformRelAuthority string
	AdmissionDecisionID  string
	EffectiveFrom        time.Time
	SkipPlatformRel      bool
}

// Provisioner writes organisation profile, legal entity profile, mappings,
// and optional platform relationship via OrganisationRepository.
type Provisioner struct {
	Orgs repository.OrganisationRepository
}

// ProvisionTenantOrganisation is idempotent: profiles upsert; default
// legal-entity mapping uses EnsureDefaultTenantLegalEntityMapping.
func (p *Provisioner) ProvisionTenantOrganisation(ctx context.Context, req ProvisionRequest) error {
	if req.TenantID == "" || req.CanonicalEntityID == "" || req.LegalEntityID == "" {
		return fmt.Errorf("organisation provision: tenant_id, canonical_entity_id and legal_entity_id are required")
	}
	if req.DisplayName == "" || req.SourceAuthority == "" {
		return fmt.Errorf("organisation provision: display_name and source_authority are required")
	}
	if req.EffectiveFrom.IsZero() {
		req.EffectiveFrom = time.Now().UTC()
	}
	if req.PlatformID == "" {
		req.PlatformID = "baobab-platform"
	}
	if !req.SkipPlatformRel {
		if err := assertPlatformRelAuthority(req.PlatformRelType, req.PlatformRelAuthority); err != nil {
			return err
		}
	}

	org := domain.Organisation{
		CanonicalEntityID: req.CanonicalEntityID,
		DisplayName:       req.DisplayName,
		OfficialName:      req.OfficialName,
		OrganisationForm:  req.OrganisationForm,
		Jurisdiction:      req.Jurisdiction,
		VerificationState: domain.VerificationVerified,
		SourceAuthority:   req.SourceAuthority,
		Status:            "ACTIVE",
		EffectiveFrom:     req.EffectiveFrom,
	}
	if err := p.Orgs.UpsertOrganisation(ctx, org); err != nil {
		return fmt.Errorf("upsert organisation: %w", err)
	}

	legalName := req.LegalName
	if legalName == "" {
		legalName = req.OfficialName
	}
	if legalName == "" {
		legalName = req.DisplayName
	}
	lep := domain.LegalEntityProfile{
		LegalEntityID:     req.LegalEntityID,
		OrganisationID:    req.CanonicalEntityID,
		LegalName:         legalName,
		JurisdictionOfIncorporation: req.Jurisdiction,
		LegalStatus:       "ACTIVE",
		SourceAuthority:   req.SourceAuthority,
		VerificationState: domain.VerificationVerified,
		EffectiveFrom:     req.EffectiveFrom,
	}
	if err := p.Orgs.UpsertLegalEntityProfile(ctx, lep); err != nil {
		return fmt.Errorf("upsert legal entity profile: %w", err)
	}

	if err := p.Orgs.EnsureDefaultTenantLegalEntityMapping(ctx, req.TenantID, req.LegalEntityID, req.SourceAuthority, req.EffectiveFrom); err != nil {
		return fmt.Errorf("default tenant legal entity mapping: %w", err)
	}

	tom := domain.TenantOrganisationMapping{
		TenantID:       req.TenantID,
		OrganisationID: req.CanonicalEntityID,
		MappingRole:    "PRIMARY_ORGANISATION",
		IsDefault:      true,
		Status:         "ACTIVE",
		EffectiveFrom:  req.EffectiveFrom,
		Provenance:     req.SourceAuthority,
	}
	if err := p.Orgs.UpsertTenantOrganisationMapping(ctx, tom); err != nil {
		return fmt.Errorf("upsert tenant organisation mapping: %w", err)
	}

	if !req.SkipPlatformRel {
		pr := domain.PlatformRelationship{
			PlatformID:          req.PlatformID,
			OrganisationID:      req.CanonicalEntityID,
			RelationshipType:    req.PlatformRelType,
			VerificationState:   domain.VerificationVerified,
			Status:              "ACTIVE",
			EffectiveFrom:       req.EffectiveFrom,
			AdmissionDecisionID: req.AdmissionDecisionID,
			SourceAuthority:     req.PlatformRelAuthority,
		}
		if err := p.Orgs.UpsertPlatformRelationship(ctx, pr); err != nil {
			return fmt.Errorf("upsert platform relationship: %w", err)
		}
	}
	return nil
}

func assertPlatformRelAuthority(t domain.PlatformRelationshipType, authority string) error {
	switch t {
	case domain.PlatformRelOwner, domain.PlatformRelOperator, domain.PlatformRelGroupAffiliate:
		if authority == "" || authority == "applicant-submission" {
			return fmt.Errorf("platform relationship type %s requires server-side source_authority", t)
		}
	case "":
		return fmt.Errorf("platform relationship type is required unless SkipPlatformRel is set")
	}
	return nil
}
