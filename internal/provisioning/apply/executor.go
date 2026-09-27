package apply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
)

// ExecutorStore is the persistence the executor needs.
type ExecutorStore interface {
	ClaimExecution(ctx context.Context, lease time.Duration) (operations.Operation, bool, error)
	FinishAbandonedCancellations(ctx context.Context) (int, error)
	LoadExecuted(ctx context.Context, key string) (convergence.ExecutedProvisioning, error)
	SaveExecutionManifest(ctx context.Context, m provisioningdomain.TenantManifestRecord) error
	MarkProvisioningBlocked(ctx context.Context, id, reason string) error
	MarkExecutionFailed(ctx context.Context, id, code string, retryable bool) error
	CompleteOperation(ctx context.Context, id string, attempt int, outcome operations.Outcome) (operations.Status, error)
	LegalEntityOf(ctx context.Context, tenantID string) (string, error)
}

// Pipeline builds the orchestrator that executes a manifest for a
// provisioning.
type Pipeline func(ctx context.Context, tenantID string, manifest provisioning.ResolvedManifest) (*provisioning.Orchestrator, error)

// maxAttempts bounds how often an operation whose executor keeps dying is
// resumed before it is failed.
const maxAttempts = 5

// Executor applies approved plans as durable operations (ADR-BCP-022
// sections 54-67). Each claimed operation runs the approved plan's execution
// manifest through the provisioning pipeline once, from wherever a previous
// attempt left it: the pipeline's phases are idempotent.
type Executor struct {
	Store    ExecutorStore
	Registry convergence.ExecutionRegistry
	// Planner re-plans the desired state before execution: a plan whose
	// assumptions no longer hold is never executed (ADR-BCP-021 section 30).
	Planner  convergence.Planner
	Now      func() time.Time
	Pipeline Pipeline
	Lease    time.Duration
}

// RunOnce executes one runnable operation, if any, and reports whether it
// found one.
func (e Executor) RunOnce(ctx context.Context) (bool, error) {
	lease := e.Lease
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	if _, err := e.Store.FinishAbandonedCancellations(ctx); err != nil {
		return false, err
	}
	op, found, err := e.Store.ClaimExecution(ctx, lease)
	if err != nil || !found {
		return false, err
	}
	outcome := e.execute(ctx, op)
	recorded, err := e.Store.CompleteOperation(ctx, op.ID, op.ExecutionAttempt, outcome)
	slog.InfoContext(ctx, "provisioning operation executed", "operation_id", op.ID, "tenant_provisioning_id", op.SubjectID,
		"tenant_id", op.TenantID, "operation_type", op.Type, "attempt", op.ExecutionAttempt, "status", string(recorded))
	if err != nil {
		return true, err
	}
	id, err := provisioningRowID(op.SubjectID)
	if err != nil {
		return true, nil
	}
	switch recorded {
	case operations.StatusCancelled:
		// Asked to stop, execution halted at this safe point.
		return true, e.Store.MarkProvisioningBlocked(ctx, id, "EXECUTION_CANCELLED")
	case operations.StatusFailed:
		// A failure before the pipeline recorded one must not leave the
		// provisioning APPLYING or REMEDIATING, where no command reaches it.
		return true, e.Store.MarkExecutionFailed(ctx, id, outcomeCode(outcome), outcome.Retryable)
	}
	return true, nil
}

// Run executes operations until ctx ends, polling every interval while none
// is runnable.
func (e Executor) Run(ctx context.Context, interval time.Duration) {
	for {
		found, err := e.RunOnce(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "provisioning operation executor", "error", err)
		}
		if found && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (e Executor) execute(ctx context.Context, op operations.Operation) operations.Outcome {
	failed := func(code, detail string, retryable bool) operations.Outcome {
		return failure(op, code, detail, retryable)
	}
	if op.ExecutionAttempt > maxAttempts {
		return failed("EXECUTION_ABANDONED", fmt.Sprintf("the operation was interrupted %d times", maxAttempts), true)
	}
	p, err := e.Store.LoadExecuted(ctx, op.SubjectID)
	if err != nil {
		return failed("PROVISIONING_UNAVAILABLE", "the provisioning could not be read", true)
	}
	// The approval authorises exactly this plan: re-check the binding the
	// apply command checked, in case anything changed since.
	if p.Plan == nil || p.Desired == nil || !p.Approved || p.Plan.PlanDigest != op.PlanDigest || p.ApprovedDigest != op.PlanDigest || p.ApprovalID != op.ApprovalID {
		return failed("PLAN_DIGEST_MISMATCH", "the plan being applied is no longer the approved plan", false)
	}
	// Authoritative state may have changed while the operation was queued
	// or interrupted: re-plan and execute only unchanged material. An
	// execution that has begun is not abandoned merely because the plan's
	// validity window closed while it ran.
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now()
	}
	if op.ExecutionAttempt == 1 && p.Plan.Expired(now) {
		return failed("PLAN_STALE", "the approved plan expired before execution; replan", false)
	}
	again, err := e.Planner.Plan(ctx, convergence.Input{Desired: *p.Desired, TenantProvisioningID: p.Key, PlanID: p.Plan.PlanID,
		PlanVersion: p.Plan.PlanVersion, BaseRevision: p.Plan.BaseRevision, Now: p.Plan.GeneratedAt})
	if err != nil {
		return failed("PROVISIONING_UNAVAILABLE", "authoritative state could not be read to revalidate the plan", true)
	}
	if convergence.Material(again) != convergence.Material(*p.Plan) {
		return failed("PLAN_STALE", "authoritative state changed since the plan was approved; replan", false)
	}
	legalEntity, err := e.Store.LegalEntityOf(ctx, p.TenantID)
	if err != nil {
		return failed("TENANT_UNAVAILABLE", "the tenant could not be read", true)
	}
	manifest, err := ExecutionManifest(ctx, e.Registry, *p.Plan, *p.Desired, legalEntity, p.ApprovalID)
	if err != nil {
		return failed("PLAN_NOT_EXECUTABLE", err.Error(), false)
	}
	record, err := executionRecord(p, manifest)
	if err != nil {
		return failed("PLAN_NOT_EXECUTABLE", err.Error(), false)
	}
	if err := e.Store.SaveExecutionManifest(ctx, record); err != nil {
		return failed("PROVISIONING_UNAVAILABLE", "the execution manifest could not be saved", true)
	}
	pipeline, err := e.Pipeline(ctx, p.TenantID, manifest)
	if err != nil {
		return failed("PROVISIONING_UNAVAILABLE", "the provisioning pipeline could not be built", true)
	}
	// A failed execution resumes the approved plan from where it failed.
	run := pipeline.Run
	if p.LegacyStatus == string(provisioningdomain.ProvisioningStatusFailed) {
		run = pipeline.Retry
	}
	final, runErr := run(ctx, p.ID)
	switch final.Status {
	case provisioningdomain.ProvisioningStatusActive:
		return succeeded(p.Key, "ACTIVE", "The approved plan was applied and the tenant is ACTIVE.")
	case provisioningdomain.ProvisioningStatusFailed:
		return failed("PROVISIONING_FAILED", final.LastError, true)
	case provisioningdomain.ProvisioningStatusReconcile, provisioningdomain.ProvisioningStatusReady:
		if runErr == nil {
			if err := e.Store.MarkProvisioningBlocked(ctx, p.ID, ""); err != nil {
				return failed("PROVISIONING_UNAVAILABLE", "the blocked provisioning could not be recorded", true)
			}
			return operations.Outcome{Status: operations.StatusBlocked, CurrentPhase: "BLOCKED", Retryable: true}
		}
	}
	if runErr == nil {
		runErr = errors.New("execution ended in status " + string(final.Status))
	}
	return failed("PROVISIONING_FAILED", runErr.Error(), true)
}

// executionRecord is the persisted form of the manifest: the approved plan
// as submitted and the manifest it resolved to.
func executionRecord(p convergence.ExecutedProvisioning, manifest provisioning.ResolvedManifest) (provisioningdomain.TenantManifestRecord, error) {
	raw, err := json.Marshal(p.Plan)
	if err != nil {
		return provisioningdomain.TenantManifestRecord{}, err
	}
	resolved, err := json.Marshal(manifest)
	if err != nil {
		return provisioningdomain.TenantManifestRecord{}, err
	}
	return provisioningdomain.TenantManifestRecord{
		TenantProvisioningID: p.ID, TenantID: p.TenantID, SchemaVersion: "control-plane/v1 ProvisioningPlan",
		ManifestHash: p.Plan.PlanDigest, DesiredStateVersion: p.Plan.DesiredStateVersion,
		Source: "plan:" + p.Plan.PlanID, RawManifest: raw, ResolvedManifest: resolved,
	}, nil
}

func succeeded(key, state, summary string) operations.Outcome {
	result, _ := json.Marshal(map[string]string{"resource_type": "TENANT_PROVISIONING", "resource_id": key,
		"resource_state": state, "summary": summary})
	return operations.Outcome{Status: operations.StatusSucceeded, CurrentPhase: state, Result: result}
}

// failure is an operation failure with its problem (errors/v1), under the
// operation's correlation id. detail comes from the Control Plane's own
// phase errors, never from request input.
func failure(op operations.Operation, code, detail string, retryable bool) operations.Outcome {
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	correlation := op.CorrelationID
	if correlation == "" {
		correlation = op.ID
	}
	status := http.StatusUnprocessableEntity
	if retryable {
		status = http.StatusServiceUnavailable
	}
	problem, _ := json.Marshal(map[string]any{"type": "https://docs.nabhold.com/problems/" + strings.ToLower(code),
		"title": "Provisioning operation failed", "status": status, "code": code, "detail": detail,
		"correlation_id": correlation, "retryable": retryable})
	return operations.Outcome{Status: operations.StatusFailed, CurrentPhase: "FAILED", Retryable: retryable, Problem: problem}
}

// outcomeCode is the code of a failed outcome's problem.
func outcomeCode(o operations.Outcome) string {
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(o.Problem, &p)
	return p.Code
}

// StandardPipeline is the provisioning pipeline an approved plan executes
// through: the existing ordered, idempotent phase workers over the tenant's
// default capability scope.
func StandardPipeline(deps provisioning.ZB02Dependencies) Pipeline {
	return func(ctx context.Context, tenantID string, manifest provisioning.ResolvedManifest) (*provisioning.Orchestrator, error) {
		scopeID, err := provisioning.EnsureDefaultCapabilityScope(ctx, deps.Repo, tenantID)
		if err != nil {
			return nil, err
		}
		return provisioning.BuildZB02Pipeline(deps, manifest, scopeID)
	}
}

// provisioningRowID returns the row id behind a tp_ identifier.
func provisioningRowID(key string) (string, error) {
	return domain.ParseResourceID("tp", key)
}
