// ADR-BCP-018 gate ORG-13 — admin API for tenant-scoped counterparty roles,
// the legacy buyer/supplier reconciliation run and the review of possible
// duplicate Organisations.
// Contract: baobab-platform/shared contracts/organisation/v1/counterparty.schema.json.
package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	svcorg "github.com/baobab-platform/baobab-cp/internal/service/organisation"
	"github.com/go-chi/chi/v5"
)

// counterpartyRoleSourceAuthority records that an administrator assigned the
// role through this API; it is never taken from the request.
const counterpartyRoleSourceAuthority = "control-plane-admin"

type counterpartyHandler struct {
	repo repository.CounterpartyRepository
	now  func() time.Time
}

type assignCounterpartyRoleRequest struct {
	OrganisationID string     `json:"organisation_id"`
	Role           string     `json:"role"`
	Status         string     `json:"status,omitempty"`
	EffectiveFrom  *time.Time `json:"effective_from,omitempty"`
}

type endCounterpartyRoleRequest struct {
	Reason      string     `json:"reason"`
	EffectiveTo *time.Time `json:"effective_to,omitempty"`
}

type counterpartyRoleList struct {
	Items []domain.CounterpartyRole `json:"items"`
}

type resolutionCandidatePage struct {
	Items []domain.OrganisationResolutionCandidate `json:"items"`
	// Next is the cursor for the following page; empty on the last page.
	Next string `json:"next,omitempty"`
}

func (h counterpartyHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

// assign records a live role. Replaying an existing live role returns it
// with 200.
func (h counterpartyHandler) assign(w http.ResponseWriter, r *http.Request) {
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	var req assignCounterpartyRoleRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	from := h.clock()
	if req.EffectiveFrom != nil {
		from = req.EffectiveFrom.UTC()
	}
	role := domain.CounterpartyRole{OrganisationID: req.OrganisationID, TenantID: chi.URLParam(r, "tenantID"), Role: req.Role,
		Status: req.Status, EffectiveFrom: from, SourceAuthority: counterpartyRoleSourceAuthority}
	id, created, err := h.repo.EnsureCounterpartyRole(r.Context(), role, actor)
	switch {
	case err == nil:
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		h.respondRole(w, r, status, id)
	case errors.Is(err, repository.ErrCanonicalEntityNotFound):
		problem(w, r, http.StatusNotFound, "CANONICAL_ENTITY_NOT_FOUND", "no canonical entity has that id", false)
	case errors.Is(err, repository.ErrTenantNotRegistered):
		problem(w, r, http.StatusNotFound, "TENANT_NOT_FOUND", "no tenant has that id", false)
	case errors.Is(err, repository.ErrNotAnOrganisation):
		problem(w, r, http.StatusUnprocessableEntity, "NOT_AN_ORGANISATION", "only organisation entities can hold a counterparty role", false)
	default:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
	}
}

// respondRole answers with the stored role, so a replay shows the role as
// it is rather than as requested.
func (h counterpartyHandler) respondRole(w http.ResponseWriter, r *http.Request, status int, id string) {
	role, err := h.repo.GetCounterpartyRole(r.Context(), id)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "COUNTERPARTY_ROLE_READ_FAILED", "the role was recorded but could not be read back", true)
		return
	}
	writeJSON(w, status, role)
}

func (h counterpartyHandler) list(w http.ResponseWriter, r *http.Request) {
	roles, err := h.repo.ListCounterpartyRoles(r.Context(), chi.URLParam(r, "tenantID"), r.URL.Query().Get("organisation_id"))
	if err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the tenant or organisation id is not valid", false)
		return
	}
	if roles == nil {
		roles = []domain.CounterpartyRole{}
	}
	writeJSON(w, http.StatusOK, counterpartyRoleList{Items: roles})
}

func (h counterpartyHandler) end(w http.ResponseWriter, r *http.Request) {
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	var req endCounterpartyRoleRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	at := h.clock()
	if req.EffectiveTo != nil {
		at = req.EffectiveTo.UTC()
	}
	err := h.repo.EndCounterpartyRole(r.Context(), chi.URLParam(r, "roleID"), at, req.Reason, actor)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, repository.ErrCounterpartyRoleNotFound):
		problem(w, r, http.StatusNotFound, "COUNTERPARTY_ROLE_NOT_FOUND", "no counterparty role has that id", false)
	default:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
	}
}

// reconcile runs the ORG-13 migration and duplicate detection. It is safe
// to rerun and merges nothing.
func (h counterpartyHandler) reconcile(w http.ResponseWriter, r *http.Request) {
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	report, err := (&svcorg.CounterpartyReconciler{Repo: h.repo, Now: h.clock}).Run(r.Context(), actor)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "ORGANISATION_RECONCILIATION_FAILED", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h counterpartyHandler) listCandidates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit must be an integer", false)
			return
		}
		limit = n
	}
	items, err := h.repo.ListResolutionCandidates(r.Context(), q.Get("status"), limit, q.Get("after"))
	if err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	page := resolutionCandidatePage{Items: items}
	if page.Items == nil {
		page.Items = []domain.OrganisationResolutionCandidate{}
	}
	if len(items) == limit {
		page.Next = items[len(items)-1].ID
	}
	writeJSON(w, http.StatusOK, page)
}

func (h counterpartyHandler) getCandidate(w http.ResponseWriter, r *http.Request) {
	c, err := h.repo.GetResolutionCandidate(r.Context(), chi.URLParam(r, "candidateID"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, c)
	case errors.Is(err, repository.ErrResolutionCandidateNotFound):
		problem(w, r, http.StatusNotFound, "RESOLUTION_CANDIDATE_NOT_FOUND", "no resolution candidate has that id", false)
	default:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
	}
}

// decide records the authenticated reviewer's decision. The body is decoded
// strictly, so it cannot name its own reviewer. A decision merges nothing
// (ADR-BCP-023 section 52).
func (h counterpartyHandler) decide(w http.ResponseWriter, r *http.Request) {
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	var d domain.ResolutionCandidateDecision
	if !decodeStrict(w, r, &d) {
		return
	}
	if err := d.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	c, _, err := h.repo.DecideResolutionCandidate(r.Context(), chi.URLParam(r, "candidateID"), d, h.clock(), actor)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, c)
	case errors.Is(err, repository.ErrResolutionCandidateNotFound):
		problem(w, r, http.StatusNotFound, "RESOLUTION_CANDIDATE_NOT_FOUND", "no resolution candidate has that id", false)
	case errors.Is(err, repository.ErrResolutionCandidateDecided):
		problem(w, r, http.StatusConflict, "RESOLUTION_CANDIDATE_DECIDED", "the candidate was already decided differently", false)
	case errors.Is(err, repository.ErrResolutionDecisionInvalid):
		problem(w, r, http.StatusUnprocessableEntity, "RESOLUTION_DECISION_INVALID", err.Error(), false)
	default:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
	}
}
