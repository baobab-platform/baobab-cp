// Target path: baobab-platform/baobab-cp/internal/service/organisation/provision.go
//
// ADR-BCP-018 CP-4 — Provisioning / admission wiring for Organisation + mappings.
//
// Purpose
// -------
// When a tenant is registered (first-party or external), ensure:
//   1. Organisation profile exists (anchored to CanonicalEntity)
//   2. LegalEntityProfile exists
//   3. Default TenantLegalEntityMapping exists and projects to Tenant.LegalEntityID
//   4. Optional TenantOrganisationMapping (default)
//   5. PlatformRelationship is set server-side (never from applicant self-claim
//      for privileged types)
//
// Rules
// -----
//   - First-party legal_entity_ids (NABHOLD, ZURIBEANS, THAMANI-GLOBAL,
//     EQUATOR-ESTATE) SHOULD be validated against Shared registry by the
//     caller before invoking ProvisionTenantOrganisation.
//   - External customers do NOT require a Shared registry entry (tenancy v1.1).
//   - Does not grant cross-tenant access or administration rights.
//   - Fail closed on missing required identifiers.
//
// Integration
// -----------
// Call from the existing RegisterTenant / admission success path after the
// Tenant row and CanonicalEntity are created. Inject OrganisationRepository
// and any CanonicalEntity factory the project already uses.

package organisation

import (
	"context"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// FirstPartyLegalEntityIDs are governed by Shared contracts/legal-entity/registry.yaml.
// Callers SHOULD reject unknown first-party claims that are not in this set or
// the live Shared registry snapshot.
var FirstPartyLegalEntityIDs = map[string]struct{}{
	"NABHOLD":        {},
	"ZURIBEANS":      {},
	"THAMANI-GLOBAL": {},
	"EQUATOR-ESTATE": {},
}

// IsFirstPartyLegalEntity reports whether id is a known first-party registry id.
func IsFirstPartyLegalEntity(id string) bool {
	_, ok := FirstPartyLegalEntityIDs[id]
	return ok
}

// ProvisionRequest is the minimum input to attach organisation structure to a tenant.
type ProvisionRequest struct {
	TenantID              string
	CanonicalEntityID     string // Organisation identity spine
	LegalEntityID         string
	DisplayName           string
	OfficialName          string
	OrganisationForm      domain.OrganisationForm
	Jurisdiction          string
	LegalName             string
	LegalStatus           string
	SourceAuthority       string // e.g. shared-registry | control-plane-admission
	PlatformRelType       domain.PlatformRelationshipType
	PlatformRelAuthority  string // must be server-side for privileged types
	EffectiveFrom         time.Time
	// SkipPlatformRelationship when true does not write a PlatformRelationship
	// (e.g. deferred until commercial activation).
	SkipPlatformRelationship bool
}

// Provisioner wires Organisation + LegalEntity + default mappings for a tenant.
type Provisioner struct {
	Orgs repository.OrganisationRepository
	// NewID generates stable opaque ids for mapping/relationship rows.
	// Inject the project's existing id generator (e.g. ulid / resource-id helper).
	NewID func() string
	// SyncTenantLegalEntityID updates Tenant.LegalEntityID for compatibility.
	// If nil, SetDefaultTenantLegalEntityMapping alone is relied upon (repo path).
	SyncTenantLegalEntityID func(ctx context.Context, tenantID, legalEntityID string) error
}

// ProvisionTenantOrganisation creates Organisation profile, LegalEntityProfile,
// default TenantLegalEntityMapping, default TenantOrganisationMapping, and
// optionally PlatformRelationship. Idempotent upserts where the repository supports them.
func (p *Provisioner) ProvisionTenantOrganisation(ctx context.Context, req ProvisionRequest) error {
	if req.TenantID == "" || req.CanonicalEntityID == "" || req.LegalEntityID == "" {
		return fmt.Errorf("organisation provision: tenant_id, canonical_entity_id and legal_entity_id are required")
	}
	if req.DisplayName == "" {
		return fmt.Errorf("organisation provision: display_name is required")
	}
	if req.SourceAuthority == "" {
		return fmt.Errorf("organisation provision: source_authority is required")
	}
	if req.EffectiveFrom.IsZero() {
		req.EffectiveFrom = time.Now().UTC()
	}
	if p.NewID == nil {
		return fmt.Errorf("organisation provision: NewID generator is required")
	}

	// Privileged platform relationship types must not be applicant-assigned.
	if !req.SkipPlatformRelationship {
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
		VerificationState: domain.VerificationVerified, // admission path only after review
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
		LegalStatus:       defaultString(req.LegalStatus, "ACTIVE"),
		SourceAuthority:   req.SourceAuthority,
		VerificationState: domain.VerificationVerified,
		EffectiveFrom:     req.EffectiveFrom,
	}
	if err := p.Orgs.UpsertLegalEntityProfile(ctx, lep); err != nil {
		return fmt.Errorf("upsert legal entity profile: %w", err)
	}

	tlemID := p.NewID()
	tlem := domain.TenantLegalEntityMapping{
		ID:            tlemID,
		TenantID:      req.TenantID,
		LegalEntityID: req.LegalEntityID,
		IsDefault:     true,
		Status:        "ACTIVE",
		EffectiveFrom: req.EffectiveFrom,
	}
	if err := p.Orgs.UpsertTenantLegalEntityMapping(ctx, tlem); err != nil {
		return fmt.Errorf("upsert tenant legal entity mapping: %w", err)
	}
	// Ensure singular default + Tenant.LegalEntityID projection (transactional in repo).
	if err := p.Orgs.SetDefaultTenantLegalEntityMapping(ctx, req.TenantID, tlemID); err != nil {
		return fmt.Errorf("set default tenant legal entity mapping: %w", err)
	}
	if p.SyncTenantLegalEntityID != nil {
		if err := p.SyncTenantLegalEntityID(ctx, req.TenantID, req.LegalEntityID); err != nil {
			return fmt.Errorf("sync tenant legal_entity_id: %w", err)
		}
	}

	tom := domain.TenantOrganisationMapping{
		ID:             p.NewID(),
		TenantID:       req.TenantID,
		OrganisationID: req.CanonicalEntityID,
		IsDefault:      true,
		Status:         "ACTIVE",
		EffectiveFrom:  req.EffectiveFrom,
	}
	if err := p.Orgs.UpsertTenantOrganisationMapping(ctx, tom); err != nil {
		return fmt.Errorf("upsert tenant organisation mapping: %w", err)
	}

	if !req.SkipPlatformRelationship {
		pr := domain.PlatformRelationship{
			ID:                p.NewID(),
			OrganisationID:    req.CanonicalEntityID,
			RelationshipType:  req.PlatformRelType,
			VerificationState: domain.VerificationVerified,
			Status:            "ACTIVE",
			EffectiveFrom:     req.EffectiveFrom,
			SourceAuthority:   req.PlatformRelAuthority,
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
		return fmt.Errorf("platform relationship type is required unless SkipPlatformRelationship is set")
	}
	return nil
}

func defaultString(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
