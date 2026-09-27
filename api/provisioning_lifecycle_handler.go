package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/go-chi/chi/v5"
)

// The lifecycle commands of a provisioning (Shared
// tenant-provisioning-lifecycle.yaml triggers): replan, withdraw and
// remediate, and retry and cancel of the operation executing it.

// commandReason reads the {"reason"} body of a lifecycle command
// (ProvisioningCommandRequest, OperationCommandRequest), and optionally the
// desired-state version a replan names; allowVersion refuses it otherwise.
func commandReason(w http.ResponseWriter, r *http.Request, allowVersion bool) (string, *int64, bool) {
	raw, ok := readBody(w, r)
	if !ok {
		return "", nil, false
	}
	var body struct {
		Reason              string `json:"reason"`
		DesiredStateVersion *int64 `json:"desired_state_version"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	reason := ""
	if err := dec.Decode(&body); err == nil {
		reason = strings.TrimSpace(body.Reason)
	}
	if reason == "" || len(reason) > 500 || (body.DesiredStateVersion != nil && (!allowVersion || *body.DesiredStateVersion < 1)) {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "the body gives a reason of 1 to 500 characters", false)
		return "", nil, false
	}
	return reason, body.DesiredStateVersion, true
}

// replan supersedes the current plan with one generated from authoritative
// state. The new plan needs its own decision.
func (h convergenceHandler) replan(w http.ResponseWriter, r *http.Request) {
	revision, ok := provisioningRevision(w, r)
	if !ok {
		return
	}
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	c, ok := h.load(w, r)
	if !ok {
		return
	}
	_, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	reason, version, ok := commandReason(w, r, true)
	if !ok {
		return
	}
	requestHash := sha256Hex([]byte(reason + "|" + versionString(version)))
	if _, hash, err := h.repo.PlanByIdempotencyKey(r.Context(), c.ID, key); err == nil {
		if hash != requestHash {
			problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
			return
		}
		h.writeProvisioning(w, r, http.StatusOK, c)
		return
	}
	switch {
	case c.State != "PLANNED" && c.State != "BLOCKED" && c.State != "FAILED":
		problem(w, r, http.StatusConflict, "PROVISIONING_STATE_CONFLICT", "only a PLANNED, BLOCKED or FAILED provisioning is replanned", false)
		return
	case c.OperationActive:
		problem(w, r, http.StatusConflict, "OPERATION_IN_PROGRESS", "an operation on the provisioning is in progress; cancel it first", false)
		return
	case version != nil && *version != c.DesiredStateVersion:
		problem(w, r, http.StatusConflict, "DESIRED_STATE_VERSION_MISMATCH", "the desired-state version named is not the current one", false)
		return
	}
	desired, err := h.repo.GetDesiredState(r.Context(), c.ID, c.DesiredStateVersion)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the desired state could not be read", true)
		return
	}
	next := 1
	if c.Plan != nil {
		next = c.Plan.PlanVersion + 1
	}
	plan, err := h.planner().Plan(r.Context(), convergence.Input{Desired: desired, TenantProvisioningID: c.Key,
		PlanID: domain.NewResourceID("plan"), PlanVersion: next, BaseRevision: c.Revision, Now: h.clock()})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PLANNING_UNAVAILABLE", "authoritative state could not be read for planning", true)
		return
	}
	if !h.commandRecorded(w, r, h.repo.Replan(r.Context(), c.ID, revision, plan, key, requestHash, reason, actor)) {
		return
	}
	h.reload(w, r, c.ID, http.StatusOK)
}

// withdraw abandons a provisioning that is not executing.
func (h convergenceHandler) withdraw(w http.ResponseWriter, r *http.Request) {
	revision, ok := provisioningRevision(w, r)
	if !ok {
		return
	}
	c, ok := h.load(w, r)
	if !ok {
		return
	}
	_, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	reason, _, ok := commandReason(w, r, false)
	if !ok {
		return
	}
	switch {
	case c.State != "DRAFT" && c.State != "PLANNED" && c.State != "BLOCKED" && c.State != "FAILED":
		problem(w, r, http.StatusConflict, "PROVISIONING_STATE_CONFLICT", "only a DRAFT, PLANNED, BLOCKED or FAILED provisioning is withdrawn", false)
		return
	case c.OperationActive:
		problem(w, r, http.StatusConflict, "OPERATION_IN_PROGRESS", "an operation on the provisioning is in progress; cancel it first", false)
		return
	}
	if !h.commandRecorded(w, r, h.repo.Withdraw(r.Context(), c.ID, revision, reason, actor)) {
		return
	}
	h.reload(w, r, c.ID, http.StatusOK)
}

// remediate resolves a BLOCKED provisioning's blockers within its approved,
// current plan, as an operation.
func (h convergenceHandler) remediate(w http.ResponseWriter, r *http.Request) {
	revision, ok := provisioningRevision(w, r)
	if !ok {
		return
	}
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	c, ok := h.load(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	reason, _, ok := commandReason(w, r, false)
	if !ok {
		return
	}
	op := operations.Operation{ID: domain.NewResourceID("op"), Type: operations.TypeTenantProvisioningRemediate,
		SubjectID: c.Key, RequestedBy: principalID, CurrentPhase: "REMEDIATING", CorrelationID: actor.CorrelationID}
	if c.Plan != nil {
		op.PlanID, op.PlanDigest = c.Plan.PlanID, c.Plan.PlanDigest
	}
	if c.Decision != nil {
		op.ApprovalID = c.Decision.ApprovalID
	}
	requestHash := sha256Hex([]byte(c.Key + "|" + op.PlanDigest + "|" + reason))
	if existing, err := h.repo.ReplayOperation(r.Context(), principalID, op.Type, key); err == nil {
		h.accepted(w, r, existing, requestHash, c.Key)
		return
	}
	switch {
	case c.State != "BLOCKED":
		problem(w, r, http.StatusConflict, "PROVISIONING_STATE_CONFLICT", "only a BLOCKED provisioning is remediated", false)
		return
	case c.Plan == nil || len(c.Plan.Blockers) > 0:
		problem(w, r, http.StatusConflict, "PLAN_CHANGE_REQUIRED", "the blockers need another plan; replan", false)
		return
	case c.Decision == nil || c.Decision.Decision != "APPROVED" || c.Decision.PlanDigest != c.Plan.PlanDigest:
		problem(w, r, http.StatusConflict, "PLAN_NOT_APPROVED", "the current plan has no APPROVED decision", false)
		return
	}
	if stale, err := h.stale(r.Context(), c); err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the plan could not be evaluated", true)
		return
	} else if stale {
		problem(w, r, http.StatusConflict, "PLAN_STALE", "authoritative state changed since the plan was approved, or it expired; replan", false)
		return
	}
	created, _, err := h.repo.AcceptRemediation(r.Context(), c.ID, revision, op, key, requestHash, actor)
	if !h.commandRecorded(w, r, err) {
		return
	}
	h.accepted(w, r, created, requestHash, c.Key)
}

// commandRecorded writes the problem of a failed lifecycle command, and
// reports whether it was recorded.
func (h convergenceHandler) commandRecorded(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, repository.ErrTenantProvisioningVersionConflict):
		problem(w, r, http.StatusPreconditionFailed, "PROVISIONING_REVISION_MISMATCH", "the provisioning changed since the revision given in If-Match", false)
	case errors.Is(err, repository.ErrOperationInProgress):
		problem(w, r, http.StatusConflict, "OPERATION_IN_PROGRESS", "an operation on the provisioning is in progress; cancel it first", false)
	case errors.Is(err, repository.ErrOperationKeyReused):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the command could not be recorded", true)
	}
	return false
}

func (h convergenceHandler) reload(w http.ResponseWriter, r *http.Request, id string, status int) {
	c, err := h.repo.GetConvergedProvisioning(r.Context(), id)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning could not be read back", true)
		return
	}
	h.writeProvisioning(w, r, status, c)
}

func versionString(v *int64) string {
	if v == nil {
		return ""
	}
	raw, _ := json.Marshal(*v)
	return string(raw)
}

// operationCommands serves retry and cancel of an operation: platform
// administrators for every operation, a tenant administrator for its own
// tenants' (as reading one); any other is OPERATION_NOT_FOUND.
type operationCommands struct {
	read  operationHandler
	plans convergenceHandler
}

func (o operationCommands) load(w http.ResponseWriter, r *http.Request) (operations.Operation, bool) {
	op, err := o.plans.repo.GetOperation(r.Context(), chi.URLParam(r, "operationID"))
	if err == nil {
		if authority, ok := o.read.authorised(r, op); authority == adminUnavailable {
			problem(w, r, http.StatusServiceUnavailable, "AUTH_VERIFIER_UNAVAILABLE", "authorization is temporarily unavailable", true)
			return operations.Operation{}, false
		} else if !ok {
			err = repository.ErrOperationNotFound
		}
	}
	switch {
	case errors.Is(err, repository.ErrOperationNotFound):
		problem(w, r, http.StatusNotFound, "OPERATION_NOT_FOUND", "no such operation", false)
		return operations.Operation{}, false
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "OPERATION_UNAVAILABLE", "the operation could not be read", true)
		return operations.Operation{}, false
	}
	return op, true
}

// retry resumes a retryable operation while its approved plan is current
// and not stale; a stale or replanned plan is PLAN_STALE and is replanned
// and approved again.
func (o operationCommands) retry(w http.ResponseWriter, r *http.Request) {
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	op, ok := o.load(w, r)
	if !ok {
		return
	}
	_, actor, ok := resolveActor(w, r, o.plans.identities, false)
	if !ok {
		return
	}
	reason, _, ok := commandReason(w, r, false)
	if !ok {
		return
	}
	if op.Retryable && op.SubjectType == "TENANT_PROVISIONING" {
		id, err := repository.ProvisioningUUID(op.SubjectID)
		var c repository.ConvergedProvisioning
		if err == nil {
			c, err = o.plans.repo.GetConvergedProvisioning(r.Context(), id)
		}
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning could not be read", true)
			return
		}
		stale, err := o.plans.stale(r.Context(), c)
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the plan could not be evaluated", true)
			return
		}
		if c.State != "FAILED" && c.State != "BLOCKED" {
			problem(w, r, http.StatusConflict, "PROVISIONING_STATE_CONFLICT", "only a FAILED or BLOCKED provisioning's operation is retried", false)
			return
		}
		if stale || c.Plan.PlanDigest != op.PlanDigest {
			problem(w, r, http.StatusConflict, "PLAN_STALE", "the plan the operation applies is stale or was replanned; replan and approve again", false)
			return
		}
	}
	next, err := o.plans.repo.RetryOperation(r.Context(), op.ID, key, reason, actor)
	o.respond(w, r, next, err)
}

// cancel stops a queued operation at once and a running one at its next
// safe point.
func (o operationCommands) cancel(w http.ResponseWriter, r *http.Request) {
	op, ok := o.load(w, r)
	if !ok {
		return
	}
	_, actor, ok := resolveActor(w, r, o.plans.identities, false)
	if !ok {
		return
	}
	reason, _, ok := commandReason(w, r, false)
	if !ok {
		return
	}
	next, err := o.plans.repo.CancelOperation(r.Context(), op.ID, reason, actor)
	o.respond(w, r, next, err)
}

func (o operationCommands) respond(w http.ResponseWriter, r *http.Request, op operations.Operation, err error) {
	switch {
	case errors.Is(err, repository.ErrOperationNotRetryable):
		problem(w, r, http.StatusConflict, "OPERATION_NOT_RETRYABLE", "the operation is not retryable now", false)
	case errors.Is(err, repository.ErrOperationNotCancellable):
		problem(w, r, http.StatusConflict, "OPERATION_NOT_CANCELLABLE", "the operation has finished", false)
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "OPERATION_UNAVAILABLE", "the command could not be recorded", true)
	default:
		w.Header().Set("Location", "/v1/admin/operations/"+op.ID)
		w.Header().Set("ETag", entityTag(op.Revision))
		writeJSON(w, http.StatusAccepted, newOperationResponse(op))
	}
}
