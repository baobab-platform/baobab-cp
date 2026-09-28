// ADR-SHARED-016 section 4: engine migration tasks. An engine workload
// lists, claims and reports the tasks assigned to the engine instances
// whose Control Plane registration names its client as their attested
// workload. The instance is never taken from the request. Contract:
// baobab-platform/shared contracts/control-plane/v1/engine-migration-task.schema.json.
package api

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

var (
	engineMigrationTaskReportSchema = contracts.MustSchema("control-plane/v1/engine-migration-task.schema.json#/$defs/EngineMigrationTaskReport")
	engineMigrationTaskIDPattern    = regexp.MustCompile(`^emt_[a-z0-9]+$`)
)

type engineMigrationTaskHandler struct {
	repo   repository.EngineMigrationTaskRepository
	policy *health.Policy
	now    func() time.Time
}

func (h engineMigrationTaskHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

type engineMigrationTaskPage struct {
	Items         []migration.Task `json:"items"`
	NextPageToken string           `json:"next_page_token,omitempty"`
}

func (h engineMigrationTaskHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrEngineMigrationTaskNotFound):
		problem(w, r, http.StatusNotFound, "ENGINE_MIGRATION_TASK_NOT_FOUND", "no engine migration task has that id", false)
	case errors.Is(err, repository.ErrEngineMigrationTaskRevision):
		problem(w, r, http.StatusPreconditionFailed, "ENGINE_MIGRATION_TASK_REVISION_MISMATCH", "the task changed since it was read", false)
	case errors.Is(err, repository.ErrEngineMigrationTaskClaimed):
		problem(w, r, http.StatusConflict, "ENGINE_MIGRATION_TASK_CLAIMED", "the task is claimed under another live lease", true)
	case errors.Is(err, repository.ErrEngineMigrationTaskFinal):
		problem(w, r, http.StatusConflict, "ENGINE_MIGRATION_TASK_FINAL", "the task is final", false)
	case errors.Is(err, repository.ErrEngineMigrationTaskLeaseExpired):
		problem(w, r, http.StatusConflict, "ENGINE_MIGRATION_TASK_LEASE_EXPIRED", "only the claimant reports, while its lease holds", false)
	case errors.Is(err, repository.ErrEngineMigrationTaskReportInvalid):
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "ENGINE_MIGRATION_TASK_UNAVAILABLE", "the task could not be processed", true)
	}
}

// caller is the verified workload client and its audit identity.
func (h engineMigrationTaskHandler) caller(w http.ResponseWriter, r *http.Request) (string, repository.AuditActor, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ClientID == "" {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified workload identity is required", false)
		return "", repository.AuditActor{}, false
	}
	actor, _ := iamAuditActor(r)
	return principal.ClientID, actor, true
}

func (h engineMigrationTaskHandler) id(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "taskID")
	if len(id) > 63 || !engineMigrationTaskIDPattern.MatchString(id) {
		problem(w, r, http.StatusNotFound, "ENGINE_MIGRATION_TASK_NOT_FOUND", "no engine migration task has that id", false)
		return "", false
	}
	return id, true
}

func (h engineMigrationTaskHandler) list(w http.ResponseWriter, r *http.Request) {
	clientID, _, ok := h.caller(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("page_token")
	if token != "" && (len(token) > 512 || !engineMigrationTaskIDPattern.MatchString(token)) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "page_token is not a token this route issued", false)
		return
	}
	tasks, next, err := h.repo.ListEngineMigrationTasks(r.Context(), clientID, token, h.clock())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, engineMigrationTaskPage{Items: tasks, NextPageToken: next})
}

func (h engineMigrationTaskHandler) write(w http.ResponseWriter, t migration.Task) {
	w.Header().Set("ETag", entityTag(t.Revision))
	writeJSON(w, http.StatusOK, t)
}

func (h engineMigrationTaskHandler) claim(w http.ResponseWriter, r *http.Request) {
	clientID, actor, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	revision, ok := ifMatchRevision(w, r, "task")
	if !ok {
		return
	}
	t, err := h.repo.ClaimEngineMigrationTask(r.Context(), id, revision, clientID, h.policy, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.write(w, t)
}

func (h engineMigrationTaskHandler) report(w http.ResponseWriter, r *http.Request) {
	clientID, actor, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	revision, ok := ifMatchRevision(w, r, "task")
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var report migration.TaskReport
	if !decodeRaw(w, r, engineMigrationTaskReportSchema, raw, &report) {
		return
	}
	t, err := h.repo.ReportEngineMigrationTask(r.Context(), id, revision, clientID, report, sha256Hex(raw), h.policy, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.write(w, t)
}
