package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

var deploymentObservationSubmissionSchema = contracts.MustSchema("topology/v1/deployment-observation.schema.json#/$defs/DeploymentObservationSubmission")

// deploymentObservationHandler serves the ADR-BCP-025 gate ER-04 routes:
// registered infrastructure tooling reports what is running on an engine
// instance, and administrators read what was reported. Observation never
// changes capability resolution (section 2.8, amendment A4).
type deploymentObservationHandler struct {
	repo repository.ObservationRepository
	// reporters says where each reporter may report. Nil refuses every
	// observation: with no registry the Control Plane cannot know who may
	// report, and it never stores what it cannot attribute (section 2.9).
	reporters auth.ReporterRegistry
	now       func() time.Time
}

func (h deploymentObservationHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC().Truncate(time.Microsecond)
}

func (h deploymentObservationHandler) submit(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType != "workload" || principal.ClientID == "" {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "a verified reporter workload is required", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req release.ObservationSubmission
	if !decodeRaw(w, r, deploymentObservationSubmissionSchema, raw, &req) {
		return
	}
	// The reporter's registration decides, not the request: an observation
	// for another environment or region is refused, never stored.
	var scope auth.ReporterScope
	registered := false
	if h.reporters != nil {
		scope, registered = h.reporters.Reporter(principal.ClientID)
	}
	if !registered || !scope.Allows(req.Environment, req.Region) {
		metrics.DeploymentObservationRejected.Inc(release.ReasonObservationOutOfScope)
		problem(w, r, http.StatusForbidden, release.ReasonObservationOutOfScope,
			"the reporter is not registered for the environment and region this observation names", false)
		return
	}
	recorded, err := h.repo.RecordDeploymentObservation(r.Context(), req, "workload:"+principal.ClientID, h.clock())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/engine-instances/"+recorded.EngineInstanceID+"/deployment-observations")
	writeJSON(w, http.StatusCreated, recorded)
}

func (h deploymentObservationHandler) list(w http.ResponseWriter, r *http.Request) {
	items, next, err := h.repo.ListDeploymentObservations(r.Context(), chi.URLParam(r, "engineInstanceID"), r.URL.Query().Get("page_token"), 0)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page := release.ObservationPage{Items: items}
	if next != "" {
		page.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, page)
}

func (h deploymentObservationHandler) observedRelease(w http.ResponseWriter, r *http.Request) {
	observed, err := h.repo.GetObservedRelease(r.Context(), chi.URLParam(r, "engineInstanceID"), h.clock())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, observed)
}

func (h deploymentObservationHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *release.ErrInvalid
	if errors.As(err, &invalid) && (invalid.Code == release.ReasonObservationInstanceUnknown || invalid.Code == release.ReasonObservationWindowInvalid) {
		metrics.DeploymentObservationRejected.Inc(invalid.Code)
	}
	switch {
	case errors.As(err, &invalid) && invalid.Code == release.ReasonObservationInstanceUnknown:
		problem(w, r, http.StatusNotFound, invalid.Code, invalid.Detail, false)
	case errors.As(err, &invalid):
		problem(w, r, http.StatusUnprocessableEntity, invalid.Code, invalid.Detail, false)
	case errors.Is(err, repository.ErrEngineInstanceNotFound):
		problem(w, r, http.StatusNotFound, "ENGINE_INSTANCE_NOT_FOUND", "no engine instance has that id", false)
	case errors.Is(err, repository.ErrObservationMalformedPageToken):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "page_token is malformed", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "DEPLOYMENT_OBSERVATION_UNAVAILABLE", "the deployment observation could not be processed", true)
	}
}
