// ADR-BCP-017 sections 22-24, 39 — admin API for the TenantOnboardingRequest
// handoff from an APPROVED AdmissionDecision to provisioning. Contract:
// baobab-platform/shared contracts/admission/v1/onboarding.schema.json.
//
// Platform administrators with a registered Control Plane principal only.
// onboarding:request creates, cancels and fulfils; onboarding:authorise
// authorises. Separation of duties is enforced per principal by the service.
package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
	"github.com/go-chi/chi/v5"
)

type tenantOnboardingHandler struct {
	svc        *onboarding.Service
	identities repository.IdentityRepository
}

type tenantOnboardingRequestList struct {
	Items []domain.TenantOnboardingRequest `json:"items"`
}

func (h tenantOnboardingHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *onboarding.InvalidError
	switch {
	case errors.As(err, &invalid):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", strings.Join(invalid.Problems, "; "), false)
	case errors.Is(err, onboarding.ErrNotFound):
		problem(w, r, http.StatusNotFound, "TENANT_ONBOARDING_REQUEST_NOT_FOUND", "no tenant onboarding request has that id", false)
	case errors.Is(err, onboarding.ErrDecisionMissing), errors.Is(err, repository.ErrClientApplicationNotFound):
		problem(w, r, http.StatusUnprocessableEntity, "ADMISSION_DECISION_NOT_FOUND", "no admission decision has that id", false)
	case errors.Is(err, onboarding.ErrDecisionNotApproved):
		problem(w, r, http.StatusUnprocessableEntity, "ADMISSION_DECISION_NOT_APPROVED", "only an APPROVED decision can be onboarded", false)
	case errors.Is(err, onboarding.ErrSeparationOfDuties):
		problem(w, r, http.StatusForbidden, "SEPARATION_OF_DUTIES", err.Error(), false)
	case errors.Is(err, onboarding.ErrIsolationDecided):
		problem(w, r, http.StatusUnprocessableEntity, "ISOLATION_SET_BY_DECISION", err.Error(), false)
	case errors.Is(err, onboarding.ErrIsolationRequired):
		problem(w, r, http.StatusUnprocessableEntity, "ISOLATION_REQUIRED", err.Error(), false)
	case errors.Is(err, onboarding.ErrTransition):
		problem(w, r, http.StatusConflict, "TENANT_ONBOARDING_TRANSITION_NOT_ALLOWED", err.Error(), false)
	case errors.Is(err, onboarding.ErrTenantNotFound):
		problem(w, r, http.StatusUnprocessableEntity, "TENANT_NOT_FOUND", "no tenant has that id", false)
	case errors.Is(err, onboarding.ErrTenantAlreadyOnboarded):
		problem(w, r, http.StatusConflict, "TENANT_ALREADY_ONBOARDED", err.Error(), false)
	case errors.Is(err, onboarding.ErrDesiredStateMismatch):
		problem(w, r, http.StatusUnprocessableEntity, "DESIRED_STATE_MISMATCH", err.Error(), false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "TENANT_ONBOARDING_UNAVAILABLE", "the onboarding request could not be processed", true)
	}
}

func (h tenantOnboardingHandler) command(run func(*onboarding.Service, *http.Request, onboarding.Actor, string, []byte) (domain.TenantOnboardingRequest, bool, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principalID, audit, ok := resolveActor(w, r, h.identities, false)
		if !ok {
			return
		}
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		out, created, err := run(h.svc, r, onboarding.Actor{PrincipalID: principalID, Audit: audit}, chi.URLParam(r, "requestID"), raw)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, out)
	}
}

func (h tenantOnboardingHandler) request() http.HandlerFunc {
	return h.command(func(s *onboarding.Service, r *http.Request, a onboarding.Actor, _ string, raw []byte) (domain.TenantOnboardingRequest, bool, error) {
		return s.Request(r.Context(), a, raw)
	})
}

func (h tenantOnboardingHandler) transition(step func(*onboarding.Service, *http.Request, onboarding.Actor, string, []byte) (domain.TenantOnboardingRequest, error)) http.HandlerFunc {
	return h.command(func(s *onboarding.Service, r *http.Request, a onboarding.Actor, id string, raw []byte) (domain.TenantOnboardingRequest, bool, error) {
		out, err := step(s, r, a, id, raw)
		return out, false, err
	})
}

func (h tenantOnboardingHandler) authorise() http.HandlerFunc {
	return h.transition(func(s *onboarding.Service, r *http.Request, a onboarding.Actor, id string, raw []byte) (domain.TenantOnboardingRequest, error) {
		return s.Authorise(r.Context(), a, id, raw)
	})
}

func (h tenantOnboardingHandler) cancel() http.HandlerFunc {
	return h.transition(func(s *onboarding.Service, r *http.Request, a onboarding.Actor, id string, raw []byte) (domain.TenantOnboardingRequest, error) {
		return s.Cancel(r.Context(), a, id, raw)
	})
}

func (h tenantOnboardingHandler) fulfil() http.HandlerFunc {
	return h.transition(func(s *onboarding.Service, r *http.Request, a onboarding.Actor, id string, raw []byte) (domain.TenantOnboardingRequest, error) {
		return s.Fulfil(r.Context(), a, id, raw)
	})
}

func (h tenantOnboardingHandler) get(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := resolveActor(w, r, h.identities, false); !ok {
		return
	}
	out, err := h.svc.Get(r.Context(), chi.URLParam(r, "requestID"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantOnboardingHandler) list(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := resolveActor(w, r, h.identities, false); !ok {
		return
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "", domain.OnboardingRequested, domain.OnboardingAuthorised, domain.OnboardingFulfilled, domain.OnboardingCancelled:
	default:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "status must be REQUESTED, AUTHORISED, FULFILLED or CANCELLED", false)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.svc.List(r.Context(), status, limit)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantOnboardingRequestList{Items: out})
}
