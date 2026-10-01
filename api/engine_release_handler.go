package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

var engineReleaseRecordSchema = contracts.MustSchema("topology/v1/release.schema.json#/$defs/EngineReleaseRecordRequest")

// engineReleaseHandler serves the ADR-BCP-025 gate ER-02 routes: recording
// an immutable engine release and reading releases. Recording never
// approves, desires or deploys a release.
type engineReleaseHandler struct {
	repo       repository.EngineReleaseRepository
	identities repository.IdentityRepository
	now        func() time.Time
}

func (h engineReleaseHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC().Truncate(time.Microsecond)
}

// recorder is who records: release tooling as "workload:<client>", a
// platform administrator as their registered Control Plane principal.
func (h engineReleaseHandler) recorder(w http.ResponseWriter, r *http.Request) (string, repository.AuditActor, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "a verified caller is required", false)
		return "", repository.AuditActor{}, false
	}
	if principal.ActorType == "workload" {
		if principal.ClientID == "" {
			problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified workload identity is required", false)
			return "", repository.AuditActor{}, false
		}
		actor, _ := iamAuditActor(r)
		return "workload:" + principal.ClientID, actor, true
	}
	return resolveActor(w, r, h.identities, false)
}

func (h engineReleaseHandler) record(w http.ResponseWriter, r *http.Request) {
	recordedBy, actor, ok := h.recorder(w, r)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req release.RecordRequest
	if !decodeRaw(w, r, engineReleaseRecordSchema, raw, &req) {
		return
	}
	recorded, replay, err := h.repo.RecordEngineRelease(r.Context(), req, recordedBy, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/engine-releases/"+recorded.ReleaseID)
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(w, status, recorded)
}

func (h engineReleaseHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repository.EngineReleaseFilter{EngineID: q.Get("engine_id"), Status: q.Get("status"), PageToken: q.Get("page_token")}
	if f.EngineID != "" && !domain.ValidEngineID(f.EngineID) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "engine_id is not an engine identifier", false)
		return
	}
	switch f.Status {
	case "", release.StatusCandidate, release.StatusApproved, release.StatusDeprecated, release.StatusRevoked:
	default:
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "status is not an engine release status", false)
		return
	}
	items, next, err := h.repo.ListEngineReleases(r.Context(), f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page := release.Page{Items: items}
	if next != "" {
		page.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, page)
}

func (h engineReleaseHandler) get(w http.ResponseWriter, r *http.Request) {
	rel, err := h.repo.GetEngineRelease(r.Context(), chi.URLParam(r, "releaseID"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

// fail maps a repository error to its problem: the engine_release reason
// code where one is registered.
func (h engineReleaseHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *release.ErrInvalid
	switch {
	case errors.As(err, &invalid):
		problem(w, r, http.StatusUnprocessableEntity, invalid.Code, invalid.Detail, false)
	case errors.Is(err, release.ErrDuplicateDigest), errors.Is(err, release.ErrDuplicateSupport):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
	case errors.Is(err, repository.ErrEngineReleaseEngineUnknown):
		problem(w, r, http.StatusUnprocessableEntity, "ENGINE_NOT_REGISTERED", err.Error(), false)
	case errors.Is(err, repository.ErrEngineReleaseVersionConflict):
		problem(w, r, http.StatusConflict, release.ReasonVersionConflict, err.Error(), false)
	case errors.Is(err, repository.ErrEngineReleaseDigestConflict):
		problem(w, r, http.StatusConflict, release.ReasonDigestConflict, err.Error(), false)
	case errors.Is(err, repository.ErrEngineReleaseNotFound):
		problem(w, r, http.StatusNotFound, "ENGINE_RELEASE_NOT_FOUND", "no engine release has that id", false)
	case errors.Is(err, repository.ErrEngineReleaseMalformedPageToken):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "page_token is malformed", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "ENGINE_RELEASE_UNAVAILABLE", "the engine release could not be processed", true)
	}
}
