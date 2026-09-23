// Target path: baobab-platform/baobab-cp/internal/admission/organisation_hooks.go
//
// ADR-BCP-018 CP-4 — Admission hooks for organisation provisioning.
//
// Purpose
// -------
// Thin adapter between the existing admission / RegisterTenant flow and the
// organisation Provisioner. Keeps admission code free of schema detail.
//
// Integration point (typical):
//   after Tenant + CanonicalEntity are persisted in the admission success path,
//   call AfterTenantAdmitted(...).
//
// External customers: LegalEntityID is Control-Plane-issued; Shared registry
// entry is NOT required (tenancy contract v1.1).
// First-party: caller validates LegalEntityID against Shared registry first.

package admission

import (
	"context"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	svcorg "github.com/baobab-platform/baobab-cp/internal/service/organisation"
)

// TenantAdmittedEvent is emitted by the admission pipeline after a tenant is
// created. Field names are intentionally generic so existing command types
// can map into this struct without a large refactor.
type TenantAdmittedEvent struct {
	TenantID          string
	CanonicalEntityID string
	LegalEntityID     string
	DisplayName       string
	OfficialName      string
	Jurisdiction      string
	LegalName         string
	SourceAuthority   string
	// PlatformRelType is chosen by the admission policy (server-side).
	// External applicants default to EXTERNAL_CLIENT.
	PlatformRelType      domain.PlatformRelationshipType
	PlatformRelAuthority string
	EffectiveFrom        time.Time
	IsFirstParty         bool
}

// OrganisationAdmissionHook runs organisation provisioning after tenant admission.
type OrganisationAdmissionHook struct {
	Provisioner *svcorg.Provisioner
}

// AfterTenantAdmitted provisions Organisation + mappings. Returns error to fail
// the admission transaction if organisation structure cannot be written
// (fail closed — do not leave a tenant without default legal-entity mapping).
func (h *OrganisationAdmissionHook) AfterTenantAdmitted(ctx context.Context, ev TenantAdmittedEvent) error {
	if h == nil || h.Provisioner == nil {
		return fmt.Errorf("organisation admission hook: provisioner not configured")
	}

	// First-party path: enforce known registry ids when flagged.
	if ev.IsFirstParty && !svcorg.IsFirstPartyLegalEntity(ev.LegalEntityID) {
		return fmt.Errorf("organisation admission: first-party legal_entity_id %q is not in the governed set", ev.LegalEntityID)
	}

	relType := ev.PlatformRelType
	authority := ev.PlatformRelAuthority
	if relType == "" {
		if ev.IsFirstParty {
			// Affiliates default; PLATFORM_OWNER is assigned only by governance.
			relType = domain.PlatformRelGroupAffiliate
			if authority == "" {
				authority = "platform-governance"
			}
		} else {
			relType = domain.PlatformRelExternalClient
			if authority == "" {
				authority = "control-plane-admission"
			}
		}
	}

	src := ev.SourceAuthority
	if src == "" {
		if ev.IsFirstParty {
			src = "shared-registry"
		} else {
			src = "control-plane-admission"
		}
	}

	req := svcorg.ProvisionRequest{
		TenantID:             ev.TenantID,
		CanonicalEntityID:    ev.CanonicalEntityID,
		LegalEntityID:        ev.LegalEntityID,
		DisplayName:          ev.DisplayName,
		OfficialName:         ev.OfficialName,
		Jurisdiction:         ev.Jurisdiction,
		LegalName:            ev.LegalName,
		SourceAuthority:      src,
		PlatformRelType:      relType,
		PlatformRelAuthority: authority,
		EffectiveFrom:        ev.EffectiveFrom,
	}
	return h.Provisioner.ProvisionTenantOrganisation(ctx, req)
}
