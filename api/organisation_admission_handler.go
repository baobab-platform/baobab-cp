// ADR-BCP-018 gate ORG-09 — onboard the organisation behind an approved
// admission decision. Contract: baobab-platform/shared
// contracts/organisation/v1/admission.schema.json.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nabhold/baobab-cp/internal/repository"
	svcorg "github.com/nabhold/baobab-cp/internal/service/organisation"
)

type organisationAdmissionHandler struct {
	onboarder *svcorg.AdmissionOnboarder
}

// onboard is platform-admin only: the admission reviewer issues it after an
// approved decision, never the applicant (ADR-BCP-017 section 39). The body
// is decoded strictly, so a request carrying a platform relationship or a
// verification flag the contract does not define is rejected (ADR-BCP-018
// sections 69-70). A quarantined identity answers 409 with the outcome and
// its candidates; nothing was written.
func (h organisationAdmissionHandler) onboard(w http.ResponseWriter, r *http.Request) {
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	var req svcorg.AdmissionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not a valid organisation admission request", false)
		return
	}
	if err := req.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	outcome, err := h.onboarder.Onboard(r.Context(), chi.URLParam(r, "tenantID"), req, actor)
	switch {
	case errors.Is(err, svcorg.ErrFirstPartyOrganisation):
		problem(w, r, http.StatusConflict, "FIRST_PARTY_ORGANISATION", err.Error(), false)
	case errors.Is(err, repository.ErrOrganisationConflict):
		problem(w, r, http.StatusConflict, "ORGANISATION_CONFLICT", err.Error(), false)
	case err != nil:
		problem(w, r, http.StatusUnprocessableEntity, "ADMISSION_ONBOARDING_FAILED", err.Error(), false)
	case outcome.IdentityResolution == svcorg.IdentityQuarantined:
		writeJSON(w, http.StatusConflict, outcome)
	default:
		writeJSON(w, http.StatusOK, outcome)
	}
}
