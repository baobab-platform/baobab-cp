// CX-04: bounded, caller-authorised Organisation 360 source projection.
// The registry is authoritative for its own facts only. In particular,
// a VERIFIED registration is distinct from an operating legal-actor mandate.
package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/go-chi/chi/v5"
)

// A narrow interface permits deterministic isolation tests without a database.
type organisationOverviewReader interface {
	GetOrganisation(context.Context, string) (*domain.Organisation, error)
	ListLegalEntityProfilesByOrganisation(context.Context, string) ([]domain.LegalEntityProfile, error)
	ListCorporateRelationshipsByOrganisation(context.Context, string, time.Time) ([]domain.CorporateRelationship, error)
	ListCorporateGroupMemberships(context.Context, string, time.Time) ([]domain.CorporateGroupMembership, error)
	ListPlatformRelationships(context.Context, string, time.Time) ([]domain.PlatformRelationship, error)
}

type organisationOverviewHandler struct {
	reader      organisationOverviewReader
	grants      repository.AdministrativeGrantReader
	identities  repository.IdentityRepository
	now         func() time.Time
	environment string
}

type legalEntityOverview struct {
	ID                string                   `json:"legal_entity_id"`
	Name              string                   `json:"legal_name"`
	Jurisdiction      string                   `json:"jurisdiction_of_incorporation,omitempty"`
	LegalStatus       string                   `json:"legal_status"`
	VerificationState domain.VerificationState `json:"verification_state"`
	SourceAuthority   string                   `json:"source_authority"`
	VerifiedAt        *time.Time               `json:"verified_at,omitempty"`
	EvidenceCount     int                      `json:"evidence_count"`
}
type corporateRelationshipOverview struct {
	ID                   string                           `json:"id"`
	SourceOrganisationID string                           `json:"source_organisation_id"`
	TargetOrganisationID string                           `json:"target_organisation_id"`
	Kind                 domain.CorporateRelationshipType `json:"relationship_type"`
	VerificationState    domain.VerificationState         `json:"verification_state"`
	Status               string                           `json:"status"`
	DirectOrDerived      string                           `json:"direct_or_derived"`
	SourceAuthority      string                           `json:"source_authority"`
	EffectiveFrom        time.Time                        `json:"effective_from"`
	EffectiveTo          *time.Time                       `json:"effective_to,omitempty"`
}
type groupMembershipOverview struct {
	ID        string    `json:"id"`
	GroupID   string    `json:"corporate_group_id"`
	GroupRole string    `json:"group_role,omitempty"`
	Status    string    `json:"status"`
	DerivedAt time.Time `json:"derived_at"`
}
type platformRelationshipOverview struct {
	ID                string                          `json:"id"`
	PlatformID        string                          `json:"platform_id"`
	Kind              domain.PlatformRelationshipType `json:"relationship_type"`
	VerificationState domain.VerificationState        `json:"verification_state"`
	Status            string                          `json:"status"`
	SourceAuthority   string                          `json:"source_authority"`
	EffectiveFrom     time.Time                       `json:"effective_from"`
	EffectiveTo       *time.Time                      `json:"effective_to,omitempty"`
}
type organisationOverview struct {
	OrganisationID            string                          `json:"organisation_id"`
	EvaluatedAt               time.Time                       `json:"evaluated_at"`
	Profile                   *domain.Organisation            `json:"profile"`
	LegalEntities             []legalEntityOverview           `json:"legal_entities"`
	CorporateRelationships    []corporateRelationshipOverview `json:"corporate_relationships"`
	CorporateGroupMemberships []groupMembershipOverview       `json:"corporate_group_memberships"`
	PlatformRelationships     []platformRelationshipOverview  `json:"platform_relationships"`
	// Other domains require separate permission-filtered, paginated
	// contracts. These are not disguised as empty collections.
}

// A single count limit bounds payload growth without pretending the list is
// complete. The caller must seek a separately designed paginated endpoint
// if this bound is exceeded.
const maxOverviewRows = 100

func projectOrganisationOverview(id string, at time.Time, profile *domain.Organisation,
	legal []domain.LegalEntityProfile, corporate []domain.CorporateRelationship,
	groups []domain.CorporateGroupMembership, platform []domain.PlatformRelationship,
) (organisationOverview, bool) {
	out := organisationOverview{OrganisationID: id, EvaluatedAt: at, Profile: profile,
		LegalEntities: []legalEntityOverview{}, CorporateRelationships: []corporateRelationshipOverview{},
		CorporateGroupMemberships: []groupMembershipOverview{}, PlatformRelationships: []platformRelationshipOverview{}}
	if profile == nil || !strings.EqualFold(profile.CanonicalEntityID, id) ||
		len(legal) > maxOverviewRows || len(corporate) > maxOverviewRows ||
		len(groups) > maxOverviewRows || len(platform) > maxOverviewRows {
		return organisationOverview{}, false
	}
	// Project safe explicit fields; never leak identifiers, personal
	// addresses, uploaded evidence, metadata or arbitrary source payloads.
	sanitized := *profile
	sanitized.Identifiers = nil
	sanitized.Addresses = nil
	sanitized.EvidenceReferences = nil
	sanitized.Metadata = nil
	out.Profile = &sanitized
	for _, item := range legal {
		if !strings.EqualFold(item.OrganisationID, id) {
			return organisationOverview{}, false
		}
		out.LegalEntities = append(out.LegalEntities, legalEntityOverview{
			ID: item.LegalEntityID, Name: item.LegalName, Jurisdiction: item.JurisdictionOfIncorporation,
			LegalStatus: item.LegalStatus, VerificationState: item.VerificationState,
			SourceAuthority: item.SourceAuthority, VerifiedAt: item.VerifiedAt, EvidenceCount: len(item.EvidenceReferences),
		})
	}
	for _, item := range corporate {
		if !strings.EqualFold(item.SourceOrganisationID, id) && !strings.EqualFold(item.TargetOrganisationID, id) {
			return organisationOverview{}, false
		}
		out.CorporateRelationships = append(out.CorporateRelationships, corporateRelationshipOverview{
			ID: item.ID, SourceOrganisationID: item.SourceOrganisationID, TargetOrganisationID: item.TargetOrganisationID,
			Kind: item.RelationshipType, VerificationState: item.VerificationState, Status: item.Status,
			DirectOrDerived: item.DirectOrDerived, SourceAuthority: item.SourceAuthority,
			EffectiveFrom: item.EffectiveFrom, EffectiveTo: item.EffectiveTo,
		})
	}
	for _, item := range groups {
		if !strings.EqualFold(item.OrganisationID, id) {
			return organisationOverview{}, false
		}
		out.CorporateGroupMemberships = append(out.CorporateGroupMemberships, groupMembershipOverview{
			ID: item.ID, GroupID: item.CorporateGroupID, GroupRole: item.GroupRole,
			Status: item.Status, DerivedAt: item.DerivedAt,
		})
	}
	for _, item := range platform {
		if !strings.EqualFold(item.OrganisationID, id) {
			return organisationOverview{}, false
		}
		out.PlatformRelationships = append(out.PlatformRelationships, platformRelationshipOverview{
			ID: item.ID, PlatformID: item.PlatformID, Kind: item.RelationshipType,
			VerificationState: item.VerificationState, Status: item.Status,
			SourceAuthority: item.SourceAuthority, EffectiveFrom: item.EffectiveFrom, EffectiveTo: item.EffectiveTo,
		})
	}
	return out, true
}

func (h organisationOverviewHandler) get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id := chi.URLParam(r, "organisationID")
	if !domain.IsUUID(id) || len(r.URL.Query()) != 0 {
		problem(w, r, http.StatusBadRequest, "INVALID_ORGANISATION_REFERENCE", "invalid organisation identifier", false)
		return
	}
	if h.reader == nil || h.grants == nil || h.identities == nil {
		problem(w, r, http.StatusServiceUnavailable, "ORGANISATION_OVERVIEW_UNAVAILABLE", "organisation read model unavailable", true)
		return
	}
	actor, _, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	principal, err := h.identities.GetPrincipal(r.Context(), actor)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "identity status unavailable", true)
		return
	}
	if principal.Status != "ACTIVE" {
		problem(w, r, http.StatusForbidden, "PRINCIPAL_INACTIVE", "principal inactive", false)
		return
	}
	grants, sources, err := h.grants.AdministrativeGrantsOf(r.Context(), actor)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "AUTHORITY_UNAVAILABLE", "administrative authority unavailable", true)
		return
	}
	at := time.Now().UTC()
	if h.now != nil {
		at = h.now()
	}
	scopes := make([]administration.Scope, 0, len(grants)+len(sources))
	for _, grant := range grants {
		scopes = append(scopes, grant.Scope)
	}
	for _, grant := range sources {
		scopes = append(scopes, grant.Scope)
	}
	relations, err := h.grants.EffectiveRelations(r.Context(), administration.TenantsOf(scopes...), at)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "AUTHORITY_UNAVAILABLE", "administrative relation unavailable", true)
		return
	}
	authorised := false
	for _, grant := range administration.Effective(actor, grants, sources, at, relations).Grants {
		if grant.Permission == "organisation.view" && grant.Scope.Level == administration.LevelOrganisation &&
			(grant.Scope.Mode == "" || grant.Scope.Mode == administration.ModeExact) &&
			strings.EqualFold(grant.Scope.OrganisationID, id) &&
			(grant.Scope.Environment == "" || grant.Scope.Environment == h.environment) {
			authorised = true
			break
		}
	}
	if !authorised {
		// Do not disclose whether the organisation exists.
		problem(w, r, http.StatusNotFound, "ORGANISATION_NOT_FOUND", "organisation unavailable", false)
		return
	}
	profile, err := h.reader.GetOrganisation(r.Context(), id)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "ORGANISATION_OVERVIEW_UNAVAILABLE", "organisation source unavailable", true)
		return
	}
	if profile == nil {
		problem(w, r, http.StatusNotFound, "ORGANISATION_NOT_FOUND", "organisation unavailable", false)
		return
	}
	legal, err := h.reader.ListLegalEntityProfilesByOrganisation(r.Context(), id)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "ORGANISATION_OVERVIEW_UNAVAILABLE", "legal source unavailable", true)
		return
	}
	corporate, err := h.reader.ListCorporateRelationshipsByOrganisation(r.Context(), id, at)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "ORGANISATION_OVERVIEW_UNAVAILABLE", "corporate source unavailable", true)
		return
	}
	groups, err := h.reader.ListCorporateGroupMemberships(r.Context(), id, at)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "ORGANISATION_OVERVIEW_UNAVAILABLE", "group source unavailable", true)
		return
	}
	platform, err := h.reader.ListPlatformRelationships(r.Context(), id, at)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "ORGANISATION_OVERVIEW_UNAVAILABLE", "platform source unavailable", true)
		return
	}
	result, valid := projectOrganisationOverview(id, at, profile, legal, corporate, groups, platform)
	if !valid {
		problem(w, r, http.StatusServiceUnavailable, "ORGANISATION_OVERVIEW_INCOMPLETE", "inconsistent or oversized authority source", true)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
