// Target path: internal/admission/organisation_hooks.go
//
// Call AfterTenantAdmitted from the RegisterTenant success path once a
// CanonicalEntity for the organisation exists. First-party vs external is
// decided by the caller (Shared registry reconciliation), not hard-coded ids.

package admission

import (
	"context"
	"fmt"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	svcorg "github.com/nabhold/baobab-cp/internal/service/organisation"
)

// TenantAdmittedEvent carries post-registration organisation provision inputs.
type TenantAdmittedEvent struct {
	TenantID             string
	CanonicalEntityID    string
	LegalEntityID        string
	DisplayName          string
	OfficialName         string
	Jurisdiction         string
	LegalName            string
	SourceAuthority      string
	PlatformRelType      domain.PlatformRelationshipType
	PlatformRelAuthority string
	AdmissionDecisionID  string
	EffectiveFrom        time.Time
}

// OrganisationAdmissionHook runs organisation provisioning after tenant admission.
type OrganisationAdmissionHook struct {
	Provisioner *svcorg.Provisioner
}

// AfterTenantAdmitted provisions Organisation + mappings. Fail closed on error.
func (h *OrganisationAdmissionHook) AfterTenantAdmitted(ctx context.Context, ev TenantAdmittedEvent) error {
	if h == nil || h.Provisioner == nil {
		return fmt.Errorf("organisation admission hook: provisioner not configured")
	}
	relType := ev.PlatformRelType
	authority := ev.PlatformRelAuthority
	if relType == "" {
		relType = domain.PlatformRelExternalClient
		if authority == "" {
			authority = "control-plane-admission"
		}
	}
	src := ev.SourceAuthority
	if src == "" {
		src = "control-plane-admission"
	}
	return h.Provisioner.ProvisionTenantOrganisation(ctx, svcorg.ProvisionRequest{
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
		AdmissionDecisionID:  ev.AdmissionDecisionID,
		EffectiveFrom:        ev.EffectiveFrom,
	})
}
