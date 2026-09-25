// ADR-BCP-017 — the client application workflow API (gates OA-01, OA-03,
// OA-04). Contract: baobab-platform/shared contracts/admission/v1.
//
// Applicants (application:read / application:write) reach only their own
// applications: another applicant's application is reported as not found.
// Reviewers (admission:review) and deciders (admission:decide) must be
// platform administrators with a registered Control Plane principal; a
// decider can never decide their own application.
package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service"
	"github.com/nabhold/baobab-cp/internal/service/application"
)

type clientApplicationHandler struct {
	svc        *application.Service
	identities repository.IdentityRepository
}

type clientApplicationPage struct {
	Items []domain.ClientApplication `json:"items"`
	// Next is the cursor for the following (older) page; empty on the last.
	Next string `json:"next,omitempty"`
}

// openStatuses is the review queue's default: applications awaiting the platform.
var openStatuses = []domain.ApplicationStatus{domain.ApplicationSubmitted, domain.ApplicationValidating,
	domain.ApplicationInformationRequired, domain.ApplicationUnderReview}

// actor resolves the caller's Control Plane principal. An applicant's first
// request provisions one (ADR-BCP-017 section 5); platform staff must
// already have one, so every review and decision is attributable to a
// canonical principal, never to an IAM-internal id (section 58).
func (h clientApplicationHandler) actor(w http.ResponseWriter, r *http.Request, applicant bool) (application.Actor, bool) {
	principalID, audit, ok := resolveActor(w, r, h.identities, applicant)
	return application.Actor{PrincipalID: principalID, Audit: audit}, ok
}

// resolveActor resolves the authenticated caller to its Control Plane
// principal and audit identity. provision lets an applicant's first request
// create its principal; privileged staff are never provisioned here.
func resolveActor(w http.ResponseWriter, r *http.Request, identities repository.IdentityRepository, provision bool) (string, repository.AuditActor, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || identities == nil {
		problem(w, r, http.StatusServiceUnavailable, "AUTH_VERIFIER_UNAVAILABLE", "authorization is temporarily unavailable", true)
		return "", repository.AuditActor{}, false
	}
	var (
		resolved domain.Principal
		err      error
	)
	if provision {
		resolved, err = service.IdentityService{Repository: identities, Provision: service.ApplicantProvisioningPolicy}.
			Resolve(r.Context(), principal.Issuer, principal.Subject, principal.ActorType)
	} else {
		resolved, err = identities.ResolveIdentity(r.Context(), principal.Issuer, principal.Subject)
	}
	switch {
	case errors.Is(err, repository.ErrIdentityNotFound) || errors.Is(err, service.ErrProvisioningNotAllowed):
		problem(w, r, http.StatusForbidden, "PRINCIPAL_NOT_REGISTERED", "the caller has no Control Plane principal", false)
		return "", repository.AuditActor{}, false
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "the caller's identity could not be resolved", true)
		return "", repository.AuditActor{}, false
	}
	return resolved.ID, repository.AuditActor{ActorID: resolved.ID, ActorType: principal.ActorType,
		ClientID: principal.ClientID, TokenID: principal.TokenID, CorrelationID: correlationID(r)}, true
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		problem(w, r, http.StatusRequestEntityTooLarge, "INVALID_REQUEST", "the request body is too large", false)
		return nil, false
	}
	return raw, true
}

// fail maps a service error to a problem response.
func (h clientApplicationHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		invalid    *application.InvalidError
		transition *application.TransitionError
	)
	switch {
	case errors.As(err, &invalid) && invalid.Application:
		problem(w, r, http.StatusUnprocessableEntity, "APPLICATION_INCOMPLETE", strings.Join(invalid.Problems, "; "), false)
	case errors.As(err, &invalid):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", strings.Join(invalid.Problems, "; "), false)
	case errors.As(err, &transition):
		problem(w, r, http.StatusConflict, "INVALID_TRANSITION", transition.Error(), false)
	case errors.Is(err, application.ErrNotFound):
		problem(w, r, http.StatusNotFound, "CLIENT_APPLICATION_NOT_FOUND", "no such client application", false)
	case errors.Is(err, application.ErrVersionConflict):
		problem(w, r, http.StatusConflict, "VERSION_CONFLICT", err.Error(), false)
	case errors.Is(err, application.ErrIdempotencyConflict):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
	case errors.Is(err, application.ErrSelfDecision):
		problem(w, r, http.StatusForbidden, "SELF_DECISION_FORBIDDEN", err.Error(), false)
	case errors.Is(err, application.ErrMarketScope):
		problem(w, r, http.StatusUnprocessableEntity, "MARKET_SCOPE_EXCEEDED", err.Error(), false)
	case errors.Is(err, application.ErrNotInternalEligible):
		problem(w, r, http.StatusUnprocessableEntity, "NOT_INTERNAL_ELIGIBLE", err.Error(), false)
	default:
		problem(w, r, http.StatusInternalServerError, "ADMISSION_FAILED", "the client application could not be processed", true)
	}
}

func (h clientApplicationHandler) respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, status, v)
}

// --- Applicant -------------------------------------------------------------

func (h clientApplicationHandler) create(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key != "" && (len(key) < 16 || len(key) > 128) {
		problem(w, r, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must contain 16 to 128 characters", false)
		return
	}
	actor, ok := h.actor(w, r, true)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	app, replayed, err := h.svc.Create(r.Context(), actor, raw, key)
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	h.respond(w, r, status, app, err)
}

func (h clientApplicationHandler) listMine(w http.ResponseWriter, r *http.Request) {
	limit, ok := pageSize(r, 50, 200)
	if !ok {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit must be between 1 and 200", false)
		return
	}
	actor, ok := h.actor(w, r, true)
	if !ok {
		return
	}
	apps, err := h.svc.ListForApplicant(r.Context(), actor, limit, r.URL.Query().Get("before"))
	h.page(w, r, apps, limit, err)
}

func (h clientApplicationHandler) getMine(w http.ResponseWriter, r *http.Request) {
	if actor, ok := h.actor(w, r, true); ok {
		app, err := h.svc.GetForApplicant(r.Context(), actor, chi.URLParam(r, "applicationID"))
		h.respond(w, r, http.StatusOK, app, err)
	}
}

// applicantCommand runs an applicant command that takes the request body.
func (h clientApplicationHandler) applicantCommand(run func(*application.Service, *http.Request, application.Actor, string, []byte) (domain.ClientApplication, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.actor(w, r, true)
		if !ok {
			return
		}
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		app, err := run(h.svc, r, actor, chi.URLParam(r, "applicationID"), raw)
		h.respond(w, r, http.StatusOK, app, err)
	}
}

func (h clientApplicationHandler) update() http.HandlerFunc {
	return h.applicantCommand(func(s *application.Service, r *http.Request, a application.Actor, id string, raw []byte) (domain.ClientApplication, error) {
		return s.Update(r.Context(), a, id, raw)
	})
}

func (h clientApplicationHandler) submit() http.HandlerFunc {
	return h.applicantCommand(func(s *application.Service, r *http.Request, a application.Actor, id string, _ []byte) (domain.ClientApplication, error) {
		return s.Submit(r.Context(), a, id)
	})
}

func (h clientApplicationHandler) respondToRequest() http.HandlerFunc {
	return h.applicantCommand(func(s *application.Service, r *http.Request, a application.Actor, id string, raw []byte) (domain.ClientApplication, error) {
		return s.Respond(r.Context(), a, id, raw)
	})
}

func (h clientApplicationHandler) withdraw() http.HandlerFunc {
	return h.applicantCommand(func(s *application.Service, r *http.Request, a application.Actor, id string, raw []byte) (domain.ClientApplication, error) {
		return s.Withdraw(r.Context(), a, id, raw)
	})
}

// --- Platform --------------------------------------------------------------

func (h clientApplicationHandler) queue(w http.ResponseWriter, r *http.Request) {
	limit, ok := pageSize(r, 50, 200)
	if !ok {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit must be between 1 and 200", false)
		return
	}
	statuses := openStatuses
	if raw := r.URL.Query().Get("status"); raw != "" {
		statuses = nil
		for _, s := range strings.Split(raw, ",") {
			status := domain.ApplicationStatus(s)
			if !status.Valid() {
				problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "unknown application status "+s, false)
				return
			}
			statuses = append(statuses, status)
		}
	}
	if _, ok := h.actor(w, r, false); !ok {
		return
	}
	apps, err := h.svc.List(r.Context(), statuses, limit, r.URL.Query().Get("before"))
	h.page(w, r, apps, limit, err)
}

func (h clientApplicationHandler) page(w http.ResponseWriter, r *http.Request, apps []domain.ClientApplication, limit int, err error) {
	if errors.Is(err, repository.ErrInvalidApplicationCursor) {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "before is not a client application id", false)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page := clientApplicationPage{Items: apps}
	if len(apps) == limit {
		page.Next = apps[len(apps)-1].ID
	}
	writeJSON(w, http.StatusOK, page)
}

func (h clientApplicationHandler) get(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.actor(w, r, false); ok {
		app, err := h.svc.Get(r.Context(), chi.URLParam(r, "applicationID"))
		h.respond(w, r, http.StatusOK, app, err)
	}
}

func (h clientApplicationHandler) getDecision(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.actor(w, r, false); ok {
		decision, err := h.svc.GetDecision(r.Context(), chi.URLParam(r, "applicationID"))
		h.respond(w, r, http.StatusOK, decision, err)
	}
}

// staffCommand runs a reviewer, operator or decider command.
func (h clientApplicationHandler) staffCommand(status int, run func(*application.Service, *http.Request, application.Actor, string, []byte) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.actor(w, r, false)
		if !ok {
			return
		}
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		out, err := run(h.svc, r, actor, chi.URLParam(r, "applicationID"), raw)
		h.respond(w, r, status, out, err)
	}
}

func (h clientApplicationHandler) beginValidation() http.HandlerFunc {
	return h.staffCommand(http.StatusOK, func(s *application.Service, r *http.Request, a application.Actor, id string, _ []byte) (any, error) {
		return s.BeginValidation(r.Context(), a, id)
	})
}

func (h clientApplicationHandler) requestInformation() http.HandlerFunc {
	return h.staffCommand(http.StatusOK, func(s *application.Service, r *http.Request, a application.Actor, id string, raw []byte) (any, error) {
		return s.RequestInformation(r.Context(), a, id, raw)
	})
}

func (h clientApplicationHandler) beginReview() http.HandlerFunc {
	return h.staffCommand(http.StatusOK, func(s *application.Service, r *http.Request, a application.Actor, id string, _ []byte) (any, error) {
		return s.BeginReview(r.Context(), a, id)
	})
}

func (h clientApplicationHandler) cancel() http.HandlerFunc {
	return h.staffCommand(http.StatusOK, func(s *application.Service, r *http.Request, a application.Actor, id string, raw []byte) (any, error) {
		return s.Cancel(r.Context(), a, id, raw)
	})
}

func (h clientApplicationHandler) decide() http.HandlerFunc {
	return h.staffCommand(http.StatusCreated, func(s *application.Service, r *http.Request, a application.Actor, id string, raw []byte) (any, error) {
		return s.Decide(r.Context(), a, id, raw)
	})
}
