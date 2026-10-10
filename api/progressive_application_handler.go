// PEO-03 progressive applicant v2 API. Applicant-owned, never admission.
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

type progressiveApplicationWriter interface {
	CreateProgressiveApplication(context.Context, basestore.RequestMetadata, string, string, []byte) (postgres.ProgressiveApplicantDraft, error)
	GetProgressiveApplication(context.Context, string, string) (postgres.ProgressiveApplicantDraft, error)
	ChangeProgressiveApplication(context.Context, basestore.RequestMetadata, string, string, []byte, bool) (postgres.ProgressiveApplicantDraft, error)
}
type progressiveApplicantHandler struct {
	repo progressiveApplicationWriter
	api  *API
}

func (h progressiveApplicantHandler) caller(w http.ResponseWriter, r *http.Request) (string, basestore.RequestMetadata, bool) {
	actor, _, ok := resolveActor(w, r, h.api.identities, true)
	if !ok {
		return "", basestore.RequestMetadata{}, false
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "human applicant identity required", false)
		return "", basestore.RequestMetadata{}, false
	}
	return actor, requestMetadata(r, principal), true
}
func (h progressiveApplicantHandler) failed(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, postgres.ErrProgressiveApplicationNotFound):
		problem(w, r, http.StatusNotFound, "APPLICATION_NOT_FOUND", "no such applicant-owned application", false)
	case errors.Is(err, postgres.ErrProgressiveApplicationConflict):
		problem(w, r, http.StatusConflict, "APPLICATION_VERSION_CONFLICT", "this draft or idempotency key cannot be reused", false)
	default:
		// Contract errors are applicant mistakes; internal persistence failures
		// MUST NOT disclose another principal's application or database details.
		problem(w, r, http.StatusUnprocessableEntity, "PROGRESSIVE_APPLICATION_REJECTED",
			"application requires an eligible business identity and valid v2 fields", false)
	}
}
func (h progressiveApplicantHandler) create(w http.ResponseWriter, r *http.Request) {
	id, meta, ok := h.caller(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	app, err := h.repo.CreateProgressiveApplication(r.Context(), meta, id, key, raw)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, app)
}
func (h progressiveApplicantHandler) get(w http.ResponseWriter, r *http.Request) {
	id, _, ok := h.caller(w, r)
	if !ok {
		return
	}
	app, err := h.repo.GetProgressiveApplication(r.Context(), id, chi.URLParam(r, "applicationID"))
	if err != nil {
		h.failed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}
func (h progressiveApplicantHandler) update(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, false)
}
func (h progressiveApplicantHandler) submit(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, true)
}
func (h progressiveApplicantHandler) mutate(w http.ResponseWriter, r *http.Request, submit bool) {
	id, meta, ok := h.caller(w, r)
	if !ok {
		return
	}
	raw := []byte("{}")
	if !submit {
		var yes bool
		raw, yes = readBody(w, r)
		if !yes {
			return
		}
	}
	app, err := h.repo.ChangeProgressiveApplication(r.Context(), meta, id, chi.URLParam(r, "applicationID"), raw, submit)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}
