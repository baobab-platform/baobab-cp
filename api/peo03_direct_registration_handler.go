package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/go-chi/chi/v5"
)

// PEO-03C is an explicit, feature-gated entrypoint; it must never accept
// a legacy v1 onboarding authority or permit an applicant to register.
type progressiveTenantRegistrar interface {
	RegisterProgressiveTenantV2(context.Context, string, store.RequestMetadata,
		domain.RegisterTenantV2) (domain.Operation, error)
}

func (a *API) registerProgressiveV2(w http.ResponseWriter, r *http.Request) {
	db, ok := a.store.(progressiveTenantRegistrar)
	if !ok {
		problem(w, r, http.StatusServiceUnavailable, "PROGRESSIVE_REGISTRATION_UNAVAILABLE",
			"v2 admission registration is unavailable", true)
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var command domain.RegisterTenantV2
	if !decodeContract(w, r, registrationV2Schema, &command) {
		return
	}
	if command.TenantOnboardingRequestID != chi.URLParam(r, "requestID") {
		problem(w, r, http.StatusConflict, "PROGRESSIVE_REQUEST_MISMATCH",
			"registration must consume the exact authorised v2 request", false)
		return
	}
	command.Basis = domain.RegistrationOnboarding
	command.TenantID = domain.NewTenantID()
	if err := command.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	_, _, valid := resolveActor(w, r, a.identities, false)
	if !valid {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	op, err := db.RegisterProgressiveTenantV2(r.Context(), key,
		requestMetadata(r, principal), command)
	switch {
	case err == nil:
		w.Header().Set("Location", "/v1/operations/"+op.OperationID)
		writeJSON(w, http.StatusAccepted, op)
	case errors.Is(err, store.ErrIdempotencyConflict):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED",
			"idempotency key references another command", false)
	default:
		problem(w, r, http.StatusConflict, "PROGRESSIVE_AUTHORITY_NOT_SATISFIED",
			"independently authorised v2 request, current sponsorship, desired state or reviewed Organisation not satisfied", false)
	}
}
