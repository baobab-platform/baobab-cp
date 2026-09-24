// ADR-BCP-018 gate ORG-10 — admin API for the links between canonical
// Organisations and their IAM-native projections (Keycloak Organization).
// Contract: baobab-platform/shared contracts/organisation/v1/iam.schema.json.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// iamOrganisationSourceAuthority records that an administrator established
// the link through this API; it is never taken from the request.
const iamOrganisationSourceAuthority = "control-plane-admin"

type iamOrganisationHandler struct {
	repo repository.IamOrganisationRepository
	now  func() time.Time
}

type linkIamOrganisationRequest struct {
	Provider               string     `json:"provider"`
	Issuer                 string     `json:"issuer"`
	ProviderOrganisationID string     `json:"provider_organisation_id"`
	EffectiveFrom          *time.Time `json:"effective_from,omitempty"`
}

type retireIamOrganisationRequest struct {
	Reason      string     `json:"reason"`
	EffectiveTo *time.Time `json:"effective_to,omitempty"`
}

type iamOrganisationReferenceList struct {
	Items []domain.IamOrganisationReference `json:"items"`
}

type resolveIamOrganisationResponse struct {
	OrganisationID string `json:"organisation_id"`
}

func (h iamOrganisationHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

func iamAuditActor(r *http.Request) (repository.AuditActor, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return repository.AuditActor{}, false
	}
	return repository.AuditActor{ActorID: principal.Subject, ActorType: principal.ActorType, ClientID: principal.ClientID,
		TokenID: principal.TokenID, CorrelationID: correlationID(r)}, true
}

func decodeStrict(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not valid JSON for this operation", false)
		return false
	}
	return true
}

// link records an ACTIVE link. Replaying an existing link returns it with
// 200; a link held by another Organisation is a 409.
func (h iamOrganisationHandler) link(w http.ResponseWriter, r *http.Request) {
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	var req linkIamOrganisationRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	from := h.clock()
	if req.EffectiveFrom != nil {
		from = req.EffectiveFrom.UTC()
	}
	ref := domain.IamOrganisationReference{
		OrganisationID: chi.URLParam(r, "entityID"), Provider: req.Provider, Issuer: req.Issuer,
		ProviderOrganisationID: req.ProviderOrganisationID, Status: domain.IamReferenceActive,
		EffectiveFrom: from, SourceAuthority: iamOrganisationSourceAuthority,
	}
	id, created, err := h.repo.LinkIamOrganisation(r.Context(), ref, actor)
	switch {
	case err == nil:
		ref.ID = id
		status := http.StatusOK
		if created {
			status = http.StatusCreated
			w.Header().Set("Location", "/v1/iam-organisation-references/"+id)
		}
		writeJSON(w, status, ref)
	case errors.Is(err, repository.ErrIamOrganisationAlreadyLinked):
		problem(w, r, http.StatusConflict, "IAM_ORGANISATION_ALREADY_LINKED", "that IAM organisation is already linked to another canonical organisation", false)
	case errors.Is(err, repository.ErrCanonicalEntityNotFound):
		problem(w, r, http.StatusNotFound, "CANONICAL_ENTITY_NOT_FOUND", "no canonical entity has that id", false)
	case errors.Is(err, repository.ErrNotAnOrganisation):
		problem(w, r, http.StatusUnprocessableEntity, "NOT_AN_ORGANISATION", "only organisation entities can be linked to an IAM organisation", false)
	default:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
	}
}

func (h iamOrganisationHandler) list(w http.ResponseWriter, r *http.Request) {
	refs, err := h.repo.ListIamOrganisationReferences(r.Context(), chi.URLParam(r, "entityID"))
	if err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "canonical entity id is not valid", false)
		return
	}
	if refs == nil {
		refs = []domain.IamOrganisationReference{}
	}
	writeJSON(w, http.StatusOK, iamOrganisationReferenceList{Items: refs})
}

func (h iamOrganisationHandler) retire(w http.ResponseWriter, r *http.Request) {
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	var req retireIamOrganisationRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	at := h.clock()
	if req.EffectiveTo != nil {
		at = req.EffectiveTo.UTC()
	}
	if err := h.repo.RetireIamOrganisationReference(r.Context(), chi.URLParam(r, "referenceID"), at, req.Reason, actor); err != nil {
		problem(w, r, http.StatusConflict, "IAM_ORGANISATION_REFERENCE_NOT_RETIRABLE", err.Error(), false)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolve answers which canonical Organisation IAM evidence names now. It
// fails closed exactly like context resolution does.
func (h iamOrganisationHandler) resolve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ev := domain.IamOrganisationEvidence{Provider: q.Get("provider"), Issuer: q.Get("issuer"), ProviderOrganisationID: q.Get("provider_organisation_id")}
	if err := ev.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), false)
		return
	}
	id, err := h.repo.ResolveIamOrganisation(r.Context(), ev, h.clock())
	if err != nil {
		problem(w, r, http.StatusNotFound, "IAM_ORGANISATION_NOT_LINKED", "no active link resolves that IAM organisation", false)
		return
	}
	writeJSON(w, http.StatusOK, resolveIamOrganisationResponse{OrganisationID: id})
}
