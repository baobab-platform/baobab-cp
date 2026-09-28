// ADR-BCP-006 Gate 8 — provider migration planning (sections 44-58,
// 119-122). Contract: baobab-platform/shared
// contracts/control-plane/v1/provider-migration.schema.json.
//
// Platform administrators only. Planning and creation bind nothing: a
// migration is created in PLAN, and advancing it is a controlled mutation
// under ADR-BCP-021.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

var (
	providerMigrationRequestSchema = contracts.MustSchema("control-plane/v1/provider-migration.schema.json#/$defs/ProviderMigrationRequest")
	providerMigrationIDPattern     = regexp.MustCompile(`^pmg_[a-z0-9]+$`)
)

type providerMigrationHandler struct {
	repo       repository.ProviderMigrationRepository
	identities repository.IdentityRepository
	policy     *health.Policy
	now        func() time.Time
}

func (h providerMigrationHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

func (h providerMigrationHandler) planner() migration.Planner {
	return migration.Planner{Facts: h.repo, Policy: h.policy}
}

// decode validates the body against ProviderMigrationRequest and the rules
// its schema cannot express.
func (h providerMigrationHandler) decode(w http.ResponseWriter, r *http.Request) (migration.Request, []byte, bool) {
	var request migration.Request
	raw, ok := readBody(w, r)
	if !ok {
		return request, nil, false
	}
	if !decodeRaw(w, r, providerMigrationRequestSchema, raw, &request) {
		return request, nil, false
	}
	request.Normalize()
	if err := migration.Validate(request); err != nil {
		problem(w, r, http.StatusUnprocessableEntity, "PROVIDER_MIGRATION_INVALID", err.Error(), false)
		return request, nil, false
	}
	return request, raw, true
}

// preview is the side-effect-free administrative preview (section 122).
func (h providerMigrationHandler) preview(w http.ResponseWriter, r *http.Request) {
	request, _, ok := h.decode(w, r)
	if !ok {
		return
	}
	plan, err := h.planner().Plan(r.Context(), migration.Input{Request: request, PlanID: domain.NewResourceID("plan"),
		PlanVersion: 1, BaseRevision: 1, Now: h.clock()})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVIDER_MIGRATION_UNAVAILABLE", "the migration could not be planned", true)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// create records the migration in PLAN with its first plan.
func (h providerMigrationHandler) create(w http.ResponseWriter, r *http.Request) {
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	request, raw, ok := h.decode(w, r)
	if !ok {
		return
	}
	requestHash := sha256Hex(raw)
	ctx := r.Context()
	if existing, hash, err := h.repo.GetProviderMigrationByIdempotencyKey(ctx, key); err == nil {
		h.replay(w, r, existing, hash, requestHash)
		return
	} else if !errors.Is(err, repository.ErrProviderMigrationNotFound) {
		problem(w, r, http.StatusServiceUnavailable, "PROVIDER_MIGRATION_UNAVAILABLE", "the migration could not be read", true)
		return
	}
	now := h.clock()
	id := domain.NewResourceID("pmg")
	created, err := h.repo.CreateProviderMigration(ctx, request.SourceProviderKey, key, requestHash,
		func(ctx2 context.Context) (migration.Migration, migration.Plan, error) {
			plan, err := h.planner().Plan(ctx2, migration.Input{Request: request, ProviderMigrationID: id,
				PlanID: domain.NewResourceID("plan"), PlanVersion: 1, BaseRevision: 1, Now: now})
			if err != nil {
				return migration.Migration{}, migration.Plan{}, err
			}
			return migration.Migration{ProviderMigrationID: id, Request: request, Stage: migration.StagePlan,
				PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Blocked: len(plan.Blockers) > 0,
				CreatedBy: principalID, CreatedAt: now, UpdatedAt: now, Revision: 1}, plan, nil
		}, actor)
	if errors.Is(err, repository.ErrProviderMigrationIdempotencyConflict) {
		existing, hash, getErr := h.repo.GetProviderMigrationByIdempotencyKey(ctx, key)
		if getErr != nil {
			problem(w, r, http.StatusServiceUnavailable, "PROVIDER_MIGRATION_UNAVAILABLE", "the migration could not be read", true)
			return
		}
		h.replay(w, r, existing, hash, requestHash)
		return
	}
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVIDER_MIGRATION_UNAVAILABLE", "the migration could not be created", true)
		return
	}
	w.Header().Set("Location", "/v1/provider-migrations/"+created.ProviderMigrationID)
	w.Header().Set("ETag", entityTag(created.Revision))
	writeJSON(w, http.StatusCreated, created)
}

func (h providerMigrationHandler) replay(w http.ResponseWriter, r *http.Request, existing migration.Migration, hash, requestHash string) {
	if hash != requestHash {
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
		return
	}
	w.Header().Set("Location", "/v1/provider-migrations/"+existing.ProviderMigrationID)
	w.Header().Set("ETag", entityTag(existing.Revision))
	writeJSON(w, http.StatusCreated, existing)
}

func (h providerMigrationHandler) id(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "providerMigrationID")
	if !providerMigrationIDPattern.MatchString(id) || len(id) > 63 {
		problem(w, r, http.StatusNotFound, "PROVIDER_MIGRATION_NOT_FOUND", "no provider migration has that id", false)
		return "", false
	}
	return id, true
}

func (h providerMigrationHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	m, err := h.repo.GetProviderMigration(r.Context(), id)
	if errors.Is(err, repository.ErrProviderMigrationNotFound) {
		problem(w, r, http.StatusNotFound, "PROVIDER_MIGRATION_NOT_FOUND", "no provider migration has that id", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVIDER_MIGRATION_UNAVAILABLE", "the migration could not be read", true)
		return
	}
	w.Header().Set("ETag", entityTag(m.Revision))
	writeJSON(w, http.StatusOK, m)
}

// plan returns the migration's current immutable plan; its ETag is the
// plan digest.
func (h providerMigrationHandler) plan(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	plan, err := h.repo.CurrentProviderMigrationPlan(r.Context(), id)
	if errors.Is(err, repository.ErrProviderMigrationNotFound) {
		problem(w, r, http.StatusNotFound, "PROVIDER_MIGRATION_NOT_FOUND", "no provider migration has that id", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVIDER_MIGRATION_UNAVAILABLE", "the plan could not be read", true)
		return
	}
	w.Header().Set("ETag", `"`+plan.PlanDigest+`"`)
	writeJSON(w, http.StatusOK, plan)
}

// decodeRaw validates raw against schema and decodes it, writing 400 when
// it does not conform.
func decodeRaw(w http.ResponseWriter, r *http.Request, schema *contracts.Schema, raw []byte, into any) bool {
	var invalid *contracts.ValidationError
	if err := contracts.Validate(schema, raw); errors.As(err, &invalid) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", strings.Join(invalid.Problems, "; "), false)
		return false
	} else if err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not valid JSON", false)
		return false
	}
	if err := json.Unmarshal(raw, into); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body could not be decoded", false)
		return false
	}
	return true
}

var (
	providerMigrationDecisionSchema = contracts.MustSchema("control-plane/v1/approval-decision.schema.json#/$defs/ApprovalDecisionRequest")
	providerMigrationAdvanceSchema  = contracts.MustSchema("control-plane/v1/provider-migration.schema.json#/$defs/ProviderMigrationAdvanceRequest")
)

// execution is the repository's execution half (ADR-SHARED-016), or false
// when the configured store does not execute migrations.
func (h providerMigrationHandler) execution(w http.ResponseWriter, r *http.Request) (repository.ProviderMigrationExecution, bool) {
	exec, ok := h.repo.(repository.ProviderMigrationExecution)
	if !ok {
		problem(w, r, http.StatusServiceUnavailable, "PROVIDER_MIGRATION_UNAVAILABLE", "migration execution is not available", true)
	}
	return exec, ok
}

// fail maps execution errors to the contract's problems.
func (h providerMigrationHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrProviderMigrationNotFound):
		problem(w, r, http.StatusNotFound, "PROVIDER_MIGRATION_NOT_FOUND", "no provider migration has that id", false)
	case errors.Is(err, repository.ErrProviderMigrationRevisionMismatch):
		problem(w, r, http.StatusPreconditionFailed, "PROVIDER_MIGRATION_REVISION_MISMATCH", "the migration changed since it was read", false)
	case errors.Is(err, repository.ErrProviderMigrationSelfApproval):
		problem(w, r, http.StatusForbidden, "PROVIDER_MIGRATION_SELF_APPROVAL", "a provider migration is never approved by its creator", false)
	case errors.Is(err, repository.ErrProviderMigrationPlanMismatch):
		problem(w, r, http.StatusConflict, "PLAN_DIGEST_MISMATCH", "the decision names another plan or digest than the current one", false)
	case errors.Is(err, repository.ErrProviderMigrationPlanStale):
		problem(w, r, http.StatusConflict, "PLAN_STALE", "the plan no longer matches authoritative state", false)
	case errors.Is(err, repository.ErrProviderMigrationBlocked):
		problem(w, r, http.StatusConflict, "PROVIDER_MIGRATION_BLOCKED", "the migration's plan has blockers", false)
	case errors.Is(err, repository.ErrProviderMigrationPlanDecided):
		problem(w, r, http.StatusConflict, "PLAN_ALREADY_DECIDED", "the plan already has a decision", false)
	case errors.Is(err, repository.ErrProviderMigrationNotApproved):
		problem(w, r, http.StatusConflict, "PLAN_NOT_APPROVED", "the migration's current plan has no approval", false)
	case errors.Is(err, repository.ErrProviderMigrationOperationRunning):
		problem(w, r, http.StatusConflict, "PROVIDER_MIGRATION_OPERATION_RUNNING", "another operation of the migration is running", true)
	case errors.Is(err, repository.ErrProviderMigrationWindowClosed):
		problem(w, r, http.StatusConflict, "MIGRATION_CUTOVER_WINDOW_CLOSED", "the migration's cutover window is not open", false)
	case errors.Is(err, repository.ErrProviderMigrationNotReversible):
		problem(w, r, http.StatusConflict, "MIGRATION_NOT_REVERSIBLE", "a shifted cohort cannot return to the source under FORWARD_FIX_ONLY", false)
	case errors.Is(err, repository.ErrProviderMigrationStageConflict), errors.Is(err, repository.ErrProviderMigrationNotExecutable):
		problem(w, r, http.StatusConflict, "PROVIDER_MIGRATION_STAGE_CONFLICT", err.Error(), false)
	case errors.Is(err, repository.ErrOperationKeyReused):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "PROVIDER_MIGRATION_UNAVAILABLE", "the migration could not be processed", true)
	}
}

// approve records a decision on the migration's current plan
// (ADR-SHARED-016 section 1).
func (h providerMigrationHandler) approve(w http.ResponseWriter, r *http.Request) {
	exec, ok := h.execution(w, r)
	if !ok {
		return
	}
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	revision, ok := ifMatchRevision(w, r, "migration")
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
	var req changeset.DecisionRequest
	if !decodeRaw(w, r, providerMigrationDecisionSchema, raw, &req) {
		return
	}
	a, err := exec.DecideProviderMigration(r.Context(), id, revision, req, domain.NewResourceID("apd"), principalID, h.planner(), h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// advance runs one lifecycle transition as a durable operation
// (ADR-SHARED-016 section 2).
func (h providerMigrationHandler) advance(w http.ResponseWriter, r *http.Request) {
	exec, ok := h.execution(w, r)
	if !ok {
		return
	}
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	revision, ok := ifMatchRevision(w, r, "migration")
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
	var req struct {
		Transition string `json:"transition"`
		Reason     string `json:"reason"`
	}
	if !decodeRaw(w, r, providerMigrationAdvanceSchema, raw, &req) {
		return
	}
	op, err := exec.AdvanceProviderMigration(r.Context(), repository.MigrationAdvance{ProviderMigrationID: id, ExpectedRevision: revision,
		Transition: req.Transition, Reason: req.Reason, OperationID: domain.NewResourceID("op"), IdempotencyKey: key,
		RequestHash: sha256Hex(append([]byte(id+"\n"), raw...)), RequestedBy: principalID, Planner: h.planner(), Now: h.clock(), Actor: actor})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/admin/operations/"+op.ID)
	w.Header().Set("ETag", entityTag(op.Revision))
	writeJSON(w, http.StatusAccepted, newOperationResponse(op))
}
