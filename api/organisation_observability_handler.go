// ADR-BCP-018 gate ORG-15 — relationship drift report and organisation
// audit lineage (sections 127-131).
// Contract: baobab-platform/shared contracts/organisation/v1/observability.schema.json.
package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

type organisationObservabilityHandler struct {
	repo repository.OrganisationObservabilityRepository
	now  func() time.Time
}

type organisationAuditPage struct {
	Items []domain.OrganisationAuditEntry `json:"items"`
	// Next is the cursor for the following (older) page; empty on the last.
	Next string `json:"next,omitempty"`
}

func (h organisationObservabilityHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

// pageSize reads ?limit=, defaulting to def and refusing values above max.
func pageSize(r *http.Request, def, max int) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > max {
		return 0, false
	}
	return n, true
}

// drift reports current relationship drift. It changes nothing: drift is
// resolved by review, never by cascade (section 129).
func (h organisationObservabilityHandler) drift(w http.ResponseWriter, r *http.Request) {
	limit, ok := pageSize(r, 200, 1000)
	if !ok {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit must be between 1 and 1000", false)
		return
	}
	report, err := h.repo.DetectRelationshipDrift(r.Context(), h.clock(), limit)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "DRIFT_DETECTION_FAILED", "relationship drift could not be evaluated", true)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// audit returns an organisation's audit lineage, newest first.
func (h organisationObservabilityHandler) audit(w http.ResponseWriter, r *http.Request) {
	limit, ok := pageSize(r, 100, 500)
	if !ok {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit must be between 1 and 500", false)
		return
	}
	items, err := h.repo.ListOrganisationAudit(r.Context(), chi.URLParam(r, "organisationID"), limit, r.URL.Query().Get("before"))
	switch {
	case errors.Is(err, repository.ErrCanonicalEntityNotFound):
		problem(w, r, http.StatusNotFound, "CANONICAL_ENTITY_NOT_FOUND", "no organisation has that id", false)
		return
	case err != nil:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	page := organisationAuditPage{Items: items}
	if page.Items == nil {
		page.Items = []domain.OrganisationAuditEntry{}
	}
	if len(items) == limit {
		page.Next = items[len(items)-1].AuditID
	}
	writeJSON(w, http.StatusOK, page)
}
