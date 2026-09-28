// ADR-BCP-021 gates CCM-02 and CCM-03, ADR-BCP-022 section 44: the
// generic changeset. Contract: baobab-platform/shared
// contracts/control-plane/v1/changeset.schema.json and
// changeset-lifecycle.yaml.
//
// Platform administrators only for now. The approver is never the
// requester; apply executes exactly the approved, non-stale plan.
package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

var (
	changesetCreateSchema   = contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangesetCreateRequest")
	changesetCancelSchema   = contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangesetCancelRequest")
	changesetDecisionSchema = contracts.MustSchema("control-plane/v1/approval-decision.schema.json#/$defs/ApprovalDecisionRequest")
	changesetIDPattern      = regexp.MustCompile(`^cs_[a-z0-9]+$`)
)

type changesetHandler struct {
	repo       repository.ChangesetRepository
	identities repository.IdentityRepository
	now        func() time.Time
}

func (h changesetHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

type changesetPage struct {
	Items         []changeset.Changeset `json:"items"`
	NextPageToken string                `json:"next_page_token,omitempty"`
}

// fail maps repository errors to the contract's problems.
func (h changesetHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrChangesetNotFound):
		problem(w, r, http.StatusNotFound, "CHANGESET_NOT_FOUND", "no changeset has that id", false)
	case errors.Is(err, repository.ErrChangesetRevisionMismatch):
		problem(w, r, http.StatusPreconditionFailed, "CHANGESET_REVISION_MISMATCH", "the changeset changed since it was read", false)
	case errors.Is(err, repository.ErrChangesetStateConflict):
		problem(w, r, http.StatusConflict, "CHANGESET_STATE_CONFLICT", err.Error(), false)
	case errors.Is(err, repository.ErrChangesetSelfApproval):
		problem(w, r, http.StatusForbidden, "CHANGESET_SELF_APPROVAL", "a changeset is never decided by its requester", false)
	case errors.Is(err, repository.ErrChangesetPlanMismatch):
		problem(w, r, http.StatusConflict, "PLAN_DIGEST_MISMATCH", "the decision names another plan or digest than the current one", false)
	case errors.Is(err, repository.ErrChangesetPlanStale):
		problem(w, r, http.StatusConflict, "PLAN_STALE", "the plan no longer matches the target; submit the changeset again", false)
	case errors.Is(err, repository.ErrChangesetPlanNotApproved):
		problem(w, r, http.StatusConflict, "PLAN_NOT_APPROVED", "the current plan has no approval", false)
	case errors.Is(err, repository.ErrPlanAlreadyDecided):
		problem(w, r, http.StatusConflict, "PLAN_ALREADY_DECIDED", "the plan already has a decision", false)
	case errors.Is(err, repository.ErrOperationKeyReused):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
	case errors.Is(err, repository.ErrChangeOutcomeNotFound):
		problem(w, r, http.StatusNotFound, "CHANGE_OUTCOME_NOT_FOUND", "the changeset has not ended", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "CHANGESET_UNAVAILABLE", "the changeset could not be processed", true)
	}
}

func (h changesetHandler) id(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "changesetID")
	if len(id) > 63 || !changesetIDPattern.MatchString(id) {
		problem(w, r, http.StatusNotFound, "CHANGESET_NOT_FOUND", "no changeset has that id", false)
		return "", false
	}
	return id, true
}

func (h changesetHandler) write(w http.ResponseWriter, status int, c changeset.Changeset) {
	w.Header().Set("ETag", entityTag(c.Revision))
	writeJSON(w, status, c)
}

func (h changesetHandler) create(w http.ResponseWriter, r *http.Request) {
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req changeset.CreateRequest
	if !decodeRaw(w, r, changesetCreateSchema, raw, &req) {
		return
	}
	hash := sha256Hex(raw)
	ctx := r.Context()
	if existing, prior, err := h.repo.GetChangesetByIdempotencyKey(ctx, principalID, key); err == nil {
		h.replay(w, r, existing, prior, hash)
		return
	} else if !errors.Is(err, repository.ErrChangesetNotFound) {
		h.fail(w, r, err)
		return
	}
	base, _, err := h.repo.TenantRevision(ctx, req.DesiredChange.TenantID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	now := h.clock()
	c, err := changeset.Draft(req, domain.NewResourceID("cs"), principalID, "API", actor.CorrelationID, base, now)
	if err != nil {
		problem(w, r, http.StatusUnprocessableEntity, "CHANGESET_INVALID", err.Error(), false)
		return
	}
	created, err := h.repo.CreateChangeset(ctx, c, key, hash, actor)
	if errors.Is(err, repository.ErrChangesetIdempotencyConflict) {
		existing, prior, getErr := h.repo.GetChangesetByIdempotencyKey(ctx, principalID, key)
		if getErr != nil {
			h.fail(w, r, getErr)
			return
		}
		h.replay(w, r, existing, prior, hash)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/admin/changesets/"+created.ChangesetID)
	h.write(w, http.StatusCreated, created)
}

func (h changesetHandler) replay(w http.ResponseWriter, r *http.Request, existing changeset.Changeset, prior, hash string) {
	if prior != hash {
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
		return
	}
	w.Header().Set("Location", "/v1/admin/changesets/"+existing.ChangesetID)
	h.write(w, http.StatusCreated, existing)
}

func (h changesetHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repository.ChangesetFilter{State: q.Get("state"), TenantID: q.Get("tenant_id"), PageToken: q.Get("page_token")}
	if f.State != "" && !changeset.Known(f.State) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "state is not a changeset state", false)
		return
	}
	if f.TenantID != "" && !domain.ValidTenantID(f.TenantID) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "tenant_id is invalid", false)
		return
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "limit must be between 1 and 100", false)
			return
		}
		f.Limit = n
	}
	items, next, err := h.repo.ListChangesets(r.Context(), f)
	if errors.Is(err, repository.ErrChangesetStateConflict) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "page_token is malformed", false)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if items == nil {
		items = []changeset.Changeset{}
	}
	writeJSON(w, http.StatusOK, changesetPage{Items: items, NextPageToken: next})
}

func (h changesetHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	c, err := h.repo.GetChangeset(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.write(w, http.StatusOK, c)
}

// command runs a lifecycle command that needs the changeset id, the
// current revision in If-Match and the verified caller.
func (h changesetHandler) command(w http.ResponseWriter, r *http.Request) (string, int64, string, repository.AuditActor, bool) {
	id, ok := h.id(w, r)
	if !ok {
		return "", 0, "", repository.AuditActor{}, false
	}
	revision, ok := changesetRevision(w, r)
	if !ok {
		return "", 0, "", repository.AuditActor{}, false
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	return id, revision, principalID, actor, ok
}

func (h changesetHandler) submit(w http.ResponseWriter, r *http.Request) {
	id, revision, _, actor, ok := h.command(w, r)
	if !ok {
		return
	}
	c, err := h.repo.SubmitChangeset(r.Context(), id, revision, domain.NewResourceID("plan"), h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.write(w, http.StatusOK, c)
}

func (h changesetHandler) plan(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	p, err := h.repo.CurrentChangesetPlan(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", `"`+p.PlanDigest+`"`)
	writeJSON(w, http.StatusOK, p)
}

func (h changesetHandler) approve(w http.ResponseWriter, r *http.Request) {
	id, revision, principalID, actor, ok := h.command(w, r)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req changeset.DecisionRequest
	if !decodeRaw(w, r, changesetDecisionSchema, raw, &req) {
		return
	}
	a, err := h.repo.DecideChangeset(r.Context(), id, revision, req, domain.NewResourceID("apd"), principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h changesetHandler) apply(w http.ResponseWriter, r *http.Request) {
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	id, revision, principalID, actor, ok := h.command(w, r)
	if !ok {
		return
	}
	hash := sha256Hex([]byte(id + "\n" + strconv.FormatInt(revision, 10)))
	op, _, err := h.repo.ApplyChangeset(r.Context(), id, revision, domain.NewResourceID("op"), key, hash, principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/admin/operations/"+op.ID)
	w.Header().Set("ETag", entityTag(op.Revision))
	writeJSON(w, http.StatusAccepted, newOperationResponse(op))
}

func (h changesetHandler) cancel(w http.ResponseWriter, r *http.Request) {
	id, revision, _, actor, ok := h.command(w, r)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decodeRaw(w, r, changesetCancelSchema, raw, &req) {
		return
	}
	c, err := h.repo.CancelChangeset(r.Context(), id, revision, req.Reason, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.write(w, http.StatusOK, c)
}

func (h changesetHandler) outcome(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	o, err := h.repo.GetChangeOutcome(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// changesetRevision reads the changeset's revision from If-Match.
func changesetRevision(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	if value == "" {
		problem(w, r, http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "If-Match with the changeset's current revision is required", false)
		return 0, false
	}
	if !strongRevisionTag.MatchString(value) {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the changeset's revision as a strong entity tag, e.g. \"3\"", false)
		return 0, false
	}
	revision, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the changeset's revision, e.g. \"3\"", false)
		return 0, false
	}
	return revision, true
}
