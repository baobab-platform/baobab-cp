package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/go-chi/chi/v5"
)

// convergenceHandler serves tenant provisioning as desired-state
// convergence (ADR-SHARED-015): plan from an authorised onboarding request,
// a decision bound to the plan's digest, and asynchronous apply.
type convergenceHandler struct {
	repo        ProvisioningRepository
	identities  repository.IdentityRepository
	environment string
	now         func() time.Time
}

func (h convergenceHandler) clock() time.Time {
	if h.now != nil {
		return h.now().UTC()
	}
	return time.Now().UTC()
}

func (h convergenceHandler) planner() convergence.Planner {
	return convergence.Planner{Registry: h.repo, Environment: h.environment}
}

// provisioningResponse is TenantProvisioning (tenant-provisioning.schema.json).
type provisioningResponse struct {
	TenantProvisioningID string                             `json:"tenant_provisioning_id"`
	TenantID             string                             `json:"tenant_id"`
	State                string                             `json:"state"`
	LegacyState          string                             `json:"legacy_state"`
	Provenance           convergence.DesiredStateProvenance `json:"provenance"`
	DesiredStateDigest   string                             `json:"desired_state_digest"`
	CurrentPlan          *currentPlanResponse               `json:"current_plan,omitempty"`
	Approval             *approvalSummary                   `json:"approval,omitempty"`
	OperationID          string                             `json:"operation_id,omitempty"`
	ReadinessStatus      string                             `json:"readiness_status"`
	BlockingReasons      []blockingReason                   `json:"blocking_reasons"`
	Revision             int64                              `json:"revision"`
	CreatedBy            string                             `json:"created_by"`
	CreatedAt            time.Time                          `json:"created_at"`
	UpdatedAt            time.Time                          `json:"updated_at"`
}

type currentPlanResponse struct {
	PlanID      string    `json:"plan_id"`
	PlanVersion int       `json:"plan_version"`
	PlanDigest  string    `json:"plan_digest"`
	GeneratedAt time.Time `json:"generated_at"`
	Stale       bool      `json:"stale"`
}

type approvalSummary struct {
	ApprovalID string    `json:"approval_id"`
	Decision   string    `json:"decision"`
	PlanDigest string    `json:"plan_digest"`
	DecidedBy  string    `json:"decided_by"`
	DecidedAt  time.Time `json:"decided_at"`
}

type blockingReason struct {
	Code          string `json:"code"`
	CapabilityKey string `json:"capability_key,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

// approvalResponse is ApprovalDecision (approval-decision.schema.json).
type approvalResponse struct {
	ApprovalID    string    `json:"approval_id"`
	SubjectType   string    `json:"subject_type"`
	SubjectID     string    `json:"subject_id"`
	PlanID        string    `json:"plan_id"`
	PlanVersion   int       `json:"plan_version"`
	PlanDigest    string    `json:"plan_digest"`
	Decision      string    `json:"decision"`
	Reason        string    `json:"reason,omitempty"`
	DecidedBy     string    `json:"decided_by"`
	DecidedAt     time.Time `json:"decided_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

// legacyProjection is the deprecated coarse state of each canonical state
// (Shared tenant-provisioning-lifecycle.yaml legacy_projection).
var legacyProjection = map[string]string{
	"DRAFT": "accepted", "VALIDATING": "accepted", "PLANNED": "accepted",
	"REGISTERING": "provisioning", "CONFIGURING_CONTEXT": "provisioning", "PROVISIONING_ENTITLEMENTS": "provisioning",
	"PROVISIONING_PROVIDERS": "provisioning", "VALIDATING_SECURITY": "provisioning", "REMEDIATING": "provisioning",
	"VERIFYING_READINESS": "reconciling", "BLOCKED": "reconciling", "READY": "reconciling",
	"ACTIVE": "active", "FAILED": "failed", "CANCELLED": "cancelled", "DEPROVISIONED": "cancelled",
}

func (h convergenceHandler) response(c repository.ConvergedProvisioning, stale bool) provisioningResponse {
	out := provisioningResponse{
		TenantProvisioningID: c.Key, TenantID: c.TenantID, State: c.State, LegacyState: legacyProjection[c.State],
		Provenance: convergence.DesiredStateProvenance{TenantOnboardingRequestID: c.TenantOnboardingRequestID,
			AdmissionDecisionID: c.AdmissionDecisionID, DesiredStateVersion: c.DesiredStateVersion},
		DesiredStateDigest: c.DesiredStateDigest, OperationID: c.OperationID, ReadinessStatus: c.ReadinessStatus,
		BlockingReasons: []blockingReason{}, Revision: c.Revision, CreatedBy: c.CreatedBy,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if c.Plan != nil {
		out.CurrentPlan = &currentPlanResponse{PlanID: c.Plan.PlanID, PlanVersion: c.Plan.PlanVersion,
			PlanDigest: c.Plan.PlanDigest, GeneratedAt: c.Plan.GeneratedAt.UTC(), Stale: stale}
	}
	if c.Decision != nil {
		out.Approval = &approvalSummary{ApprovalID: c.Decision.ApprovalID, Decision: c.Decision.Decision,
			PlanDigest: c.Decision.PlanDigest, DecidedBy: c.Decision.DecidedBy, DecidedAt: c.Decision.DecidedAt}
	}
	if c.State == "BLOCKED" {
		out.BlockingReasons = blockingReasons(c)
	}
	return out
}

// blockingReasons are why the provisioning is BLOCKED: a cancelled
// execution, the plan's blockers or, when execution stopped short of
// readiness, what the pipeline reported.
func blockingReasons(c repository.ConvergedProvisioning) []blockingReason {
	var out []blockingReason
	if c.BlockedReason != "" {
		out = append(out, blockingReason{Code: c.BlockedReason})
	}
	if c.Plan != nil {
		for _, b := range c.Plan.Blockers {
			reason := blockingReason{Code: b.Code, Detail: b.Message}
			for _, s := range c.Plan.Steps {
				if s.StepID == b.StepID {
					reason.CapabilityKey = s.Resources.CapabilityKey
				}
			}
			out = append(out, reason)
		}
	}
	for _, detail := range c.LegacyBlockingReasons {
		if len(detail) > 500 {
			detail = detail[:500]
		}
		out = append(out, blockingReason{Code: "READINESS_NOT_MET", Detail: detail})
	}
	if len(out) == 0 {
		out = append(out, blockingReason{Code: "READINESS_NOT_MET"})
	}
	return out
}

// stale reports whether c's current plan can no longer be approved or
// applied: it expired, or planning the same desired state against current
// authoritative state yields other material (ADR-BCP-021 sections 28-30).
func (h convergenceHandler) stale(ctx context.Context, c repository.ConvergedProvisioning) (bool, error) {
	if c.Plan == nil {
		return true, nil
	}
	if c.Plan.Expired(h.clock()) {
		return true, nil
	}
	desired, err := h.repo.GetDesiredState(ctx, c.ID, c.DesiredStateVersion)
	if err != nil {
		return false, err
	}
	again, err := h.planner().Plan(ctx, convergence.Input{Desired: desired, TenantProvisioningID: c.Key,
		PlanID: c.Plan.PlanID, PlanVersion: c.Plan.PlanVersion, BaseRevision: c.Plan.BaseRevision, Now: c.Plan.GeneratedAt})
	if err != nil {
		return false, err
	}
	return convergence.Material(again) != convergence.Material(*c.Plan), nil
}

// load reads the {tenantProvisioningID} of the {tenantID} in the path; one
// of another tenant, a malformed id and a legacy manifest run are all
// PROVISIONING_NOT_FOUND.
func (h convergenceHandler) load(w http.ResponseWriter, r *http.Request) (repository.ConvergedProvisioning, bool) {
	tenantID := chi.URLParam(r, "tenantID")
	if !domain.ValidTenantID(tenantID) {
		problem(w, r, http.StatusBadRequest, "INVALID_TENANT_ID", "tenant_id is invalid", false)
		return repository.ConvergedProvisioning{}, false
	}
	c, err := repository.ConvergedProvisioning{}, repository.ErrProvisioningNotFound
	if id, parseErr := repository.ProvisioningUUID(chi.URLParam(r, "provisioningID")); parseErr == nil {
		c, err = h.repo.GetConvergedProvisioning(r.Context(), id)
	}
	if err == nil && c.TenantID != tenantID {
		err = repository.ErrProvisioningNotFound
	}
	switch {
	case errors.Is(err, repository.ErrProvisioningNotFound):
		problem(w, r, http.StatusNotFound, "PROVISIONING_NOT_FOUND", "no provisioning of this tenant has that id", false)
		return repository.ConvergedProvisioning{}, false
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning could not be read", true)
		return repository.ConvergedProvisioning{}, false
	}
	return c, true
}

func (h convergenceHandler) writeProvisioning(w http.ResponseWriter, r *http.Request, status int, c repository.ConvergedProvisioning) {
	stale, err := h.stale(r.Context(), c)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning's plan could not be evaluated", true)
		return
	}
	w.Header().Set("ETag", entityTag(c.Revision))
	writeJSON(w, status, h.response(c, stale))
}

// create plans a provisioning from the tenant's authorised onboarding
// request. Nothing is applied.
func (h convergenceHandler) create(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	if !domain.ValidTenantID(tenantID) {
		problem(w, r, http.StatusBadRequest, "INVALID_TENANT_ID", "tenant_id is invalid", false)
		return
	}
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
	var body struct {
		TenantOnboardingRequestID string `json:"tenant_onboarding_request_id"`
		DesiredStateVersion       *int64 `json:"desired_state_version"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || !torPattern.MatchString(body.TenantOnboardingRequestID) ||
		(body.DesiredStateVersion != nil && *body.DesiredStateVersion < 1) {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "the body names a tenant_onboarding_request_id and, optionally, the desired_state_version reviewed; never a manifest", false)
		return
	}
	requestHash := sha256Hex(raw)
	ctx := r.Context()

	if existing, err := h.repo.GetConvergedProvisioningByIdempotencyKey(ctx, tenantID, key); err == nil {
		h.replay(w, r, existing, requestHash)
		return
	} else if !errors.Is(err, repository.ErrProvisioningNotFound) {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning could not be read", true)
		return
	}

	request, err := h.repo.GetTenantOnboardingRequest(ctx, body.TenantOnboardingRequestID)
	if errors.Is(err, repository.ErrOnboardingRequestNotFound) {
		problem(w, r, http.StatusNotFound, "TENANT_ONBOARDING_REQUEST_NOT_FOUND", "no tenant onboarding request has that id", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the onboarding request could not be read", true)
		return
	}
	const version = int64(1)
	if body.DesiredStateVersion != nil && *body.DesiredStateVersion != version {
		problem(w, r, http.StatusConflict, "DESIRED_STATE_VERSION_MISMATCH", "the desired-state version reviewed is not the current one", false)
		return
	}
	now := h.clock()
	desired, err := convergence.FreezeDesiredState(request, tenantID, version, now)
	if err != nil {
		problem(w, r, http.StatusConflict, "TENANT_ONBOARDING_NOT_AUTHORISED", "the onboarding request was not authorised and fulfilled for this tenant", false)
		return
	}
	id := domain.NewUUIDv7()
	tpKey, _ := domain.FormatResourceID("tp", id)
	plan, err := h.planner().Plan(ctx, convergence.Input{Desired: desired, TenantProvisioningID: tpKey,
		PlanID: domain.NewResourceID("plan"), PlanVersion: 1, BaseRevision: 1, Now: now})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PLANNING_UNAVAILABLE", "authoritative state could not be read for planning", true)
		return
	}
	err = h.repo.CreateConvergedProvisioning(ctx, repository.NewConvergedProvisioning{ID: id, TenantID: tenantID,
		IdempotencyKey: key, RequestHash: requestHash, CreatedBy: principalID, Desired: desired, Plan: plan, StartedAt: now}, actor)
	switch {
	case errors.Is(err, repository.ErrTenantProvisioningAlreadyExists):
		existing, getErr := h.repo.GetConvergedProvisioningByIdempotencyKey(ctx, tenantID, key)
		if errors.Is(getErr, repository.ErrProvisioningNotFound) {
			// The key identifies a legacy manifest run of this tenant.
			problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
			return
		}
		if getErr != nil {
			problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning could not be read", true)
			return
		}
		h.replay(w, r, existing, requestHash)
		return
	case errors.Is(err, repository.ErrProvisioningLive):
		problem(w, r, http.StatusConflict, "PROVISIONING_IN_PROGRESS", "the tenant already has a live provisioning", false)
		return
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning could not be created", true)
		return
	}
	created, err := h.repo.GetConvergedProvisioning(ctx, id)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning could not be read back", true)
		return
	}
	w.Header().Set("Location", "/v1/tenants/"+tenantID+"/provisioning/"+created.Key)
	h.writeProvisioning(w, r, http.StatusCreated, created)
}

func (h convergenceHandler) replay(w http.ResponseWriter, r *http.Request, existing repository.ConvergedProvisioning, requestHash string) {
	var hash string
	if row, err := h.repo.GetTenantProvisioning(r.Context(), existing.ID); err == nil {
		hash = row.RequestHash
	}
	if hash != requestHash {
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
		return
	}
	w.Header().Set("Location", "/v1/tenants/"+existing.TenantID+"/provisioning/"+existing.Key)
	h.writeProvisioning(w, r, http.StatusCreated, existing)
}

func (h convergenceHandler) get(w http.ResponseWriter, r *http.Request) {
	if c, ok := h.load(w, r); ok {
		h.writeProvisioning(w, r, http.StatusOK, c)
	}
}

func (h convergenceHandler) list(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	if !domain.ValidTenantID(tenantID) {
		problem(w, r, http.StatusBadRequest, "INVALID_TENANT_ID", "tenant_id is invalid", false)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			problem(w, r, http.StatusBadRequest, "INVALID_LIMIT", "limit must be between 1 and 200", false)
			return
		}
		limit = n
	}
	after := ""
	if token := r.URL.Query().Get("page_token"); token != "" {
		id, err := repository.ProvisioningUUID(token)
		if err != nil {
			problem(w, r, http.StatusBadRequest, "INVALID_PAGE_TOKEN", "page_token is not one this API issued", false)
			return
		}
		after = id
	}
	items, err := h.repo.ListConvergedProvisionings(r.Context(), tenantID, limit+1, after)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "provisionings could not be listed", true)
		return
	}
	page := struct {
		Items         []provisioningResponse `json:"items"`
		NextPageToken string                 `json:"next_page_token,omitempty"`
	}{Items: []provisioningResponse{}}
	if len(items) > limit {
		items = items[:limit]
		page.NextPageToken = items[limit-1].Key
	}
	for _, c := range items {
		stale, err := h.stale(r.Context(), c)
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "a provisioning's plan could not be evaluated", true)
			return
		}
		page.Items = append(page.Items, h.response(c, stale))
	}
	writeJSON(w, http.StatusOK, page)
}

// plan returns the immutable current plan; its ETag is the plan digest.
func (h convergenceHandler) plan(w http.ResponseWriter, r *http.Request) {
	c, ok := h.load(w, r)
	if !ok {
		return
	}
	if c.Plan == nil {
		problem(w, r, http.StatusNotFound, "PLAN_NOT_FOUND", "the provisioning has no current plan", false)
		return
	}
	w.Header().Set("ETag", `"`+c.Plan.PlanDigest+`"`)
	writeJSON(w, http.StatusOK, c.Plan)
}

// approve records a decision on exactly the plan the approver reviewed.
func (h convergenceHandler) approve(w http.ResponseWriter, r *http.Request) {
	revision, ok := provisioningRevision(w, r)
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
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var body struct {
		PlanID      string `json:"plan_id"`
		PlanVersion int    `json:"plan_version"`
		PlanDigest  string `json:"plan_digest"`
		Decision    string `json:"decision"`
		Reason      string `json:"reason"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.PlanID == "" || body.PlanVersion < 1 || body.PlanDigest == "" ||
		(body.Decision != "APPROVED" && body.Decision != "REJECTED" && body.Decision != "CHANGES_REQUESTED") ||
		(body.Decision != "APPROVED" && strings.TrimSpace(body.Reason) == "") || len(body.Reason) > 1000 {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "the body names the plan_id, plan_version and plan_digest reviewed and a decision; REJECTED and CHANGES_REQUESTED give a reason", false)
		return
	}
	if principalID == c.CreatedBy {
		problem(w, r, http.StatusForbidden, "PROVISIONING_SELF_APPROVAL", "the principal who requested the provisioning cannot decide its plan", false)
		return
	}
	switch {
	case c.State != "PLANNED" && c.State != "BLOCKED":
		problem(w, r, http.StatusConflict, "PROVISIONING_STATE_CONFLICT", "only a planned provisioning's plan is decided", false)
		return
	case c.Plan == nil || body.PlanID != c.Plan.PlanID || body.PlanVersion != c.Plan.PlanVersion || body.PlanDigest != c.Plan.PlanDigest:
		problem(w, r, http.StatusConflict, "PLAN_DIGEST_MISMATCH", "the decision names another plan than the current one", false)
		return
	case body.Decision == "APPROVED" && len(c.Plan.Blockers) > 0:
		problem(w, r, http.StatusConflict, "PLAN_BLOCKED", "a plan with blockers cannot be approved", false)
		return
	}
	if body.Decision == "APPROVED" {
		stale, err := h.stale(r.Context(), c)
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the plan could not be evaluated", true)
			return
		}
		if stale {
			problem(w, r, http.StatusConflict, "PLAN_STALE", "authoritative state changed since the plan was generated, or it expired; replan", false)
			return
		}
	}
	decision := repository.PlanDecision{ApprovalID: domain.NewResourceID("apd"), PlanID: c.Plan.PlanID,
		PlanVersion: c.Plan.PlanVersion, PlanDigest: c.Plan.PlanDigest, Decision: body.Decision,
		Reason: strings.TrimSpace(body.Reason), DecidedBy: principalID, DecidedAt: h.clock(), CorrelationID: actor.CorrelationID}
	switch err := h.repo.RecordPlanDecision(r.Context(), c.ID, revision, decision, actor); {
	case errors.Is(err, repository.ErrTenantProvisioningVersionConflict):
		problem(w, r, http.StatusPreconditionFailed, "PROVISIONING_REVISION_MISMATCH", "the provisioning changed since the revision given in If-Match", false)
		return
	case errors.Is(err, repository.ErrPlanAlreadyDecided):
		problem(w, r, http.StatusConflict, "PLAN_ALREADY_DECIDED", "the plan already has a decision; a changed plan is replanned", false)
		return
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the decision could not be recorded", true)
		return
	}
	writeJSON(w, http.StatusOK, approvalResponse{ApprovalID: decision.ApprovalID, SubjectType: "TENANT_PROVISIONING",
		SubjectID: c.Key, PlanID: decision.PlanID, PlanVersion: decision.PlanVersion, PlanDigest: decision.PlanDigest,
		Decision: decision.Decision, Reason: decision.Reason, DecidedBy: decision.DecidedBy, DecidedAt: decision.DecidedAt,
		CorrelationID: decision.CorrelationID})
}

// apply accepts asynchronous execution of exactly the approved plan and
// returns the operation; 202 does not mean the tenant is provisioned.
func (h convergenceHandler) apply(w http.ResponseWriter, r *http.Request) {
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
	op := operations.Operation{ID: domain.NewResourceID("op"), Type: operations.TypeTenantProvisioningApply,
		SubjectID: c.Key, RequestedBy: principalID, CurrentPhase: c.State, CorrelationID: actor.CorrelationID}
	if c.Plan != nil {
		op.PlanID, op.PlanDigest = c.Plan.PlanID, c.Plan.PlanDigest
	}
	if c.Decision != nil {
		op.ApprovalID = c.Decision.ApprovalID
	}
	// A replay returns the operation it created, whatever happened since.
	requestHash := sha256Hex([]byte(c.Key + "|" + op.PlanDigest))
	if existing, err := h.repo.ReplayOperation(r.Context(), principalID, op.Type, key); err == nil {
		h.accepted(w, r, existing, requestHash, c.Key)
		return
	}
	switch {
	case c.State != "PLANNED":
		problem(w, r, http.StatusConflict, "PROVISIONING_STATE_CONFLICT", "only a PLANNED provisioning is applied", false)
		return
	case c.Plan == nil || c.Decision == nil || c.Decision.Decision != "APPROVED":
		problem(w, r, http.StatusConflict, "PLAN_NOT_APPROVED", "the current plan has no APPROVED decision", false)
		return
	case c.Decision.PlanDigest != c.Plan.PlanDigest || c.Decision.PlanID != c.Plan.PlanID:
		problem(w, r, http.StatusConflict, "PLAN_DIGEST_MISMATCH", "the approval is for another plan than the current one", false)
		return
	}
	stale, err := h.stale(r.Context(), c)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the plan could not be evaluated", true)
		return
	}
	if stale {
		problem(w, r, http.StatusConflict, "PLAN_STALE", "authoritative state changed since the plan was approved, or it expired; replan", false)
		return
	}
	created, _, err := h.repo.AcceptApply(r.Context(), c.ID, revision, op, key, requestHash, actor)
	switch {
	case errors.Is(err, repository.ErrTenantProvisioningVersionConflict):
		problem(w, r, http.StatusPreconditionFailed, "PROVISIONING_REVISION_MISMATCH", "the provisioning changed since the revision given in If-Match", false)
		return
	case errors.Is(err, repository.ErrOperationKeyReused):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
		return
	case errors.Is(err, repository.ErrOperationInProgress):
		problem(w, r, http.StatusConflict, "OPERATION_IN_PROGRESS", "an operation on the provisioning is already in progress", false)
		return
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the apply could not be accepted", true)
		return
	}
	h.accepted(w, r, created, requestHash, c.Key)
}

func (h convergenceHandler) accepted(w http.ResponseWriter, r *http.Request, op operations.Operation, requestHash, subject string) {
	if op.SubjectID != subject || h.repo.OperationRequestHash(r.Context(), op.ID) != requestHash {
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
		return
	}
	w.Header().Set("Location", "/v1/admin/operations/"+op.ID)
	w.Header().Set("ETag", entityTag(op.Revision))
	writeJSON(w, http.StatusAccepted, newOperationResponse(op))
}

// torPattern is Shared admission/v1 tenantOnboardingRequestId.
var torPattern = regexp.MustCompile(`^tor_[a-z0-9]+$`)

// provisioningIdempotencyKey reads Shared's IdempotencyKey, or writes 400.
func provisioningIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 16 || len(key) > 128 || !idempotencyKeyPattern.MatchString(key) {
		problem(w, r, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be 16 to 128 letters, digits, '.', '_', ':' or '-'", false)
		return "", false
	}
	return key, true
}

// provisioningRevision reads Shared's ProvisioningIfMatch, or writes 428 or
// 400.
func provisioningRevision(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	if value == "" {
		problem(w, r, http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "If-Match with the provisioning's current revision is required", false)
		return 0, false
	}
	if !strongRevisionTag.MatchString(value) {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the provisioning's revision as a strong entity tag, e.g. \"3\"", false)
		return 0, false
	}
	revision, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the provisioning's revision, e.g. \"3\"", false)
		return 0, false
	}
	return revision, true
}
