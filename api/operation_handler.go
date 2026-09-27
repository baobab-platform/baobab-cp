package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/go-chi/chi/v5"
)

// operationHandler serves durable operations (ADR-BCP-022 sections 54-67,
// Shared execution-operation.schema.json).
type operationHandler struct {
	repo          repository.OperationRepository
	tenantAdminOf func(*http.Request, auth.Principal, string) adminAuthority
}

// authorised: platform administrators read every operation, a tenant
// administrator those of a tenant it administers. Anything else is
// OPERATION_NOT_FOUND, indistinguishable from an operation that does not
// exist.
func (h operationHandler) authorised(r *http.Request, op operations.Operation) (adminAuthority, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return adminDenied, false
	}
	if principal.HasRole(RolePlatformAdmin) {
		return adminAllowed, true
	}
	if op.TenantID == "" || h.tenantAdminOf == nil {
		return adminDenied, false
	}
	authority := h.tenantAdminOf(r, principal, op.TenantID)
	return authority, authority == adminAllowed
}

type operationSubject struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	TenantID string `json:"tenant_id,omitempty"`
}

type operationProgress struct {
	CompletedSteps int    `json:"completed_steps"`
	TotalSteps     int    `json:"total_steps"`
	CurrentStep    string `json:"current_step,omitempty"`
}

// operationResponse is ExecutionOperation.
type operationResponse struct {
	OperationID      string             `json:"operation_id"`
	OperationType    string             `json:"operation_type"`
	Status           string             `json:"status"`
	Subject          operationSubject   `json:"subject"`
	PlanID           string             `json:"plan_id,omitempty"`
	PlanDigest       string             `json:"plan_digest,omitempty"`
	ApprovalID       string             `json:"approval_id,omitempty"`
	RequestedBy      string             `json:"requested_by"`
	CurrentPhase     string             `json:"current_phase,omitempty"`
	Progress         *operationProgress `json:"progress,omitempty"`
	ExecutionAttempt int                `json:"execution_attempt"`
	Retryable        bool               `json:"retryable"`
	Result           json.RawMessage    `json:"result,omitempty"`
	Problem          json.RawMessage    `json:"problem,omitempty"`
	Revision         int64              `json:"revision"`
	CorrelationID    string             `json:"correlation_id,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
	StartedAt        *time.Time         `json:"started_at,omitempty"`
	UpdatedAt        time.Time          `json:"updated_at"`
	CompletedAt      *time.Time         `json:"completed_at,omitempty"`
}

func newOperationResponse(op operations.Operation) operationResponse {
	out := operationResponse{OperationID: op.ID, OperationType: op.Type, Status: string(op.Status),
		Subject: operationSubject{Type: op.SubjectType, ID: op.SubjectID, TenantID: op.TenantID},
		PlanID:  op.PlanID, PlanDigest: op.PlanDigest, ApprovalID: op.ApprovalID, RequestedBy: op.RequestedBy,
		CurrentPhase: op.CurrentPhase, ExecutionAttempt: op.ExecutionAttempt, Retryable: op.Retryable,
		Result: op.Result, Problem: op.Problem, Revision: op.Revision, CorrelationID: op.CorrelationID,
		CreatedAt: op.CreatedAt, StartedAt: utcTime(op.StartedAt), UpdatedAt: op.UpdatedAt, CompletedAt: utcTime(op.CompletedAt)}
	if op.CompletedSteps != nil && op.TotalSteps != nil {
		out.Progress = &operationProgress{CompletedSteps: *op.CompletedSteps, TotalSteps: *op.TotalSteps, CurrentStep: op.CurrentStep}
	}
	return out
}

// get polls an operation. Its ETag is its revision; a matching If-None-Match
// is 304, so polling does not resend an unchanged operation (ADR-BCP-022
// section 67).
func (h operationHandler) get(w http.ResponseWriter, r *http.Request) {
	op, err := h.repo.GetOperation(r.Context(), chi.URLParam(r, "operationID"))
	if err == nil {
		if authority, ok := h.authorised(r, op); authority == adminUnavailable {
			problem(w, r, http.StatusServiceUnavailable, "AUTH_VERIFIER_UNAVAILABLE", "authorization is temporarily unavailable", true)
			return
		} else if !ok {
			err = repository.ErrOperationNotFound
		}
	}
	if errors.Is(err, repository.ErrOperationNotFound) {
		problem(w, r, http.StatusNotFound, "OPERATION_NOT_FOUND", "no such operation", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "operation lookup failed", true)
		return
	}
	tag := entityTag(op.Revision)
	w.Header().Set("ETag", tag)
	if noneMatch(r.Header.Values("If-None-Match"), tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, newOperationResponse(op))
}

// noneMatch reports whether If-None-Match matches tag: "*" or any listed
// entity tag, compared weakly (RFC 9110 section 13.1.2).
func noneMatch(values []string, tag string) bool {
	for _, value := range values {
		for _, candidate := range strings.Split(value, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || strings.TrimPrefix(candidate, "W/") == tag {
				return true
			}
		}
	}
	return false
}

func utcTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
