package api

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

var identityRuntimeProfileSchema = contracts.MustSchema("identity/v1/provider-runtime-profile.schema.json")

type identityRuntimeProfileHandler struct {
	repo      repository.IdentityRuntimeProfileRepository
	observers auth.IdentityRuntimeObserverRegistry
	clock     func() time.Time
}

func (h identityRuntimeProfileHandler) now() time.Time {
	if h.clock == nil {
		return time.Now().UTC()
	}
	return h.clock().UTC()
}

func (h identityRuntimeProfileHandler) publish(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType != "workload" || principal.ClientID == "" {
		problem(w, r, http.StatusForbidden, "IDENTITY_RUNTIME_OBSERVER_DENIED", "the workload is not an identity runtime observer", false)
		return
	}
	if h.repo == nil || h.observers == nil {
		problem(w, r, http.StatusServiceUnavailable, "IDENTITY_RUNTIME_OBSERVER_UNAVAILABLE", "identity runtime evidence publication is unavailable", true)
		return
	}
	scope, ok := h.observers.IdentityRuntimeObserver(principal.ClientID)
	if !ok {
		problem(w, r, http.StatusForbidden, "IDENTITY_RUNTIME_OBSERVER_DENIED", "the workload is not an identity runtime observer", false)
		return
	}

	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	if err := rejectDuplicateJSON(raw); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body contains duplicate or trailing JSON", false)
		return
	}
	var profile repository.IdentityRuntimeProfile
	if !decodeRaw(w, r, identityRuntimeProfileSchema, raw, &profile) {
		return
	}
	if err := profile.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the runtime profile violates identity contract invariants", false)
		return
	}

	replay, err := h.repo.RecordIdentityRuntimeProfile(
		r.Context(),
		profile,
		"workload:"+principal.ClientID,
		scope.Environment,
		scope.Regions,
		h.now(),
	)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrIdentityRuntimeProfileTargetNotFound):
			problem(w, r, http.StatusNotFound, "IDENTITY_RUNTIME_TARGET_NOT_FOUND", "the provider or engine instance is not registered", false)
		case errors.Is(err, repository.ErrIdentityRuntimeProfileOutOfScope):
			problem(w, r, http.StatusForbidden, "IDENTITY_RUNTIME_OBSERVER_OUT_OF_SCOPE", "the workload may not report this runtime target", false)
		case errors.Is(err, repository.ErrIdentityRuntimeProfileConflict):
			problem(w, r, http.StatusConflict, "IDENTITY_RUNTIME_PROFILE_CONFLICT", "the runtime profile conflicts with current authoritative evidence", false)
		default:
			problem(w, r, http.StatusServiceUnavailable, "IDENTITY_RUNTIME_PROFILE_UNAVAILABLE", "identity runtime evidence could not be recorded", true)
		}
		return
	}

	current, ok := h.observers.IdentityRuntimeObserver(principal.ClientID)
	if !ok || current.Environment != scope.Environment || !slices.Equal(current.Regions, scope.Regions) {
		problem(w, r, http.StatusForbidden, "IDENTITY_RUNTIME_OBSERVER_DENIED", "the workload no longer has the reporter authority used for this operation", false)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusCreated
	result := "RECORDED"
	if replay {
		status = http.StatusOK
		result = "REPLAY"
	}
	writeJSON(w, status, map[string]any{
		"status":             result,
		"provider_id":        profile.ProviderID,
		"engine_instance_id": profile.EngineInstanceID,
		"revision":           profile.Revision,
	})
}
