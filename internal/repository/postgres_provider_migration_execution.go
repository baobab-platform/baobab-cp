package repository

// ADR-SHARED-016: provider migration execution. One approval binds the
// migration's current plan; each lifecycle transition is an explicit
// advance that runs exactly the approved plan's steps as a durable
// PROVIDER_MIGRATION_ADVANCE operation. The Control Plane runs the local
// steps on its own state and never calls a provider.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

var (
	ErrProviderMigrationRevisionMismatch = errors.New("provider migration revision mismatch")
	ErrProviderMigrationStageConflict    = errors.New("the provider migration's stage does not allow this")
	ErrProviderMigrationSelfApproval     = errors.New("a provider migration is never approved by its creator")
	ErrProviderMigrationPlanMismatch     = errors.New("the decision names another plan or digest than the current one")
	ErrProviderMigrationPlanStale        = errors.New("the provider migration's plan is stale")
	ErrProviderMigrationBlocked          = errors.New("the provider migration's plan has blockers")
	ErrProviderMigrationNotApproved      = errors.New("the provider migration's current plan is not approved")
	ErrProviderMigrationOperationRunning = errors.New("another operation of the provider migration is running")
	ErrProviderMigrationWindowClosed     = errors.New("the migration's cutover window is not open")
	ErrProviderMigrationNotReversible    = errors.New("the migration cannot return a shifted cohort to the source")
	ErrProviderMigrationPlanDecided      = errors.New("the plan already has a decision")
	ErrProviderMigrationNotExecutable    = errors.New("the transition includes steps the Control Plane does not execute yet")
	ErrProviderMigrationSelfExecution    = errors.New("a provider migration is never advanced by the approver of its plan")
)

// errMigrationStep is a step that failed on authoritative state: the
// operation is recorded FAILED and the migration stays in its stage.
type errMigrationStep struct {
	code, detail string
}

func (e errMigrationStep) Error() string { return e.code + ": " + e.detail }

func stepFailure(code, format string, args ...any) error {
	return errMigrationStep{code: code, detail: fmt.Sprintf(format, args...)}
}

// MigrationAdvance is one advance command.
type MigrationAdvance struct {
	ProviderMigrationID string
	ExpectedRevision    int64
	Transition          string
	Reason              string
	OperationID         string
	IdempotencyKey      string
	RequestHash         string
	RequestedBy         string
	// Planner re-plans against authoritative state inside the command's
	// transaction to detect staleness; its Facts are supplied here.
	Planner migration.Planner
	Now     time.Time
	Actor   AuditActor
}

// ProviderMigrationExecution is the execution half of the provider
// migration repository.
type ProviderMigrationExecution interface {
	DecideProviderMigration(ctx context.Context, id string, expectedRevision int64, req changeset.DecisionRequest, approvalID, approver string,
		planner migration.Planner, now time.Time, actor AuditActor) (changeset.Approval, error)
	AdvanceProviderMigration(ctx context.Context, in MigrationAdvance) (operations.Operation, error)
}

var _ ProviderMigrationExecution = (*PostgresRepository)(nil)

// lockProviderMigration reads the migration for update at the expected
// revision.
func lockProviderMigration(ctx context.Context, tx pgx.Tx, id string, expected int64) (migration.Migration, error) {
	m, err := scanProviderMigration(tx.QueryRow(ctx, `SELECT `+providerMigrationColumns+`
		FROM topology.provider_migration WHERE provider_migration_id = $1 FOR UPDATE`, id))
	if err != nil {
		return m, err
	}
	if m.Revision != expected {
		return m, ErrProviderMigrationRevisionMismatch
	}
	return m, nil
}

func loadProviderMigrationPlan(ctx context.Context, q migrationQuerier, planID string) (migration.Plan, error) {
	var document []byte
	if err := q.QueryRow(ctx, `SELECT document FROM topology.provider_migration_plan WHERE plan_id = $1`, planID).Scan(&document); err != nil {
		return migration.Plan{}, fmt.Errorf("load provider migration plan: %w", err)
	}
	var plan migration.Plan
	return plan, json.Unmarshal(document, &plan)
}

// planStale re-plans the migration inside tx, reading authoritative state
// as the migration's own execution left it (migrationFacts), and reports
// whether the approved plan no longer holds. Expiry counts only before
// execution has started (ADR-SHARED-016 section 2).
func planStale(ctx context.Context, tx pgx.Tx, planner migration.Planner, m migration.Migration, plan migration.Plan, now time.Time) (bool, error) {
	if m.Stage == migration.StagePlan && !now.Before(plan.ExpiresAt) {
		return true, nil
	}
	planner.Facts = migrationFacts{q: tx, migrationID: m.ProviderMigrationID}
	fresh, err := planner.Plan(ctx, migration.Input{Request: plan.Request, ProviderMigrationID: m.ProviderMigrationID,
		PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, BaseRevision: plan.BaseRevision, Now: now})
	if err != nil {
		return false, err
	}
	return migration.Material(fresh) != migration.Material(plan), nil
}

// DecideProviderMigration records one decision on the migration's exact
// current plan (ADR-SHARED-016 section 1). The creator never decides; an
// approval needs a plan without blockers that is neither expired nor
// stale. One APPROVED decision authorises the plan's whole sequence.
func (r *PostgresRepository) DecideProviderMigration(ctx context.Context, id string, expectedRevision int64, req changeset.DecisionRequest,
	approvalID, approver string, planner migration.Planner, now time.Time, actor AuditActor) (changeset.Approval, error) {
	if err := validateActor(actor); err != nil {
		return changeset.Approval{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return changeset.Approval{}, err
	}
	defer tx.Rollback(ctx)
	m, err := lockProviderMigration(ctx, tx, id, expectedRevision)
	if err != nil {
		return changeset.Approval{}, err
	}
	if m.Stage != migration.StagePlan || m.ApprovalID != "" {
		return changeset.Approval{}, fmt.Errorf("%w: a migration is decided once, in PLAN; it is %s", ErrProviderMigrationStageConflict, m.Stage)
	}
	if approver == m.CreatedBy {
		return changeset.Approval{}, ErrProviderMigrationSelfApproval
	}
	plan, err := loadProviderMigrationPlan(ctx, tx, m.PlanID)
	if err != nil {
		return changeset.Approval{}, err
	}
	if req.PlanID != plan.PlanID || req.PlanVersion != plan.PlanVersion || req.PlanDigest != plan.PlanDigest {
		return changeset.Approval{}, ErrProviderMigrationPlanMismatch
	}
	if req.Decision == changeset.DecisionApproved {
		if m.Blocked || len(plan.Blockers) > 0 {
			return changeset.Approval{}, ErrProviderMigrationBlocked
		}
		stale, err := planStale(ctx, tx, planner, m, plan, now)
		if err != nil {
			return changeset.Approval{}, err
		}
		if stale {
			return changeset.Approval{}, ErrProviderMigrationPlanStale
		}
	}
	a := changeset.Approval{ApprovalID: approvalID, SubjectType: "PROVIDER_MIGRATION", SubjectID: m.ProviderMigrationID,
		PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: req.Decision, Reason: req.Reason,
		DecidedBy: approver, DecidedAt: now, CorrelationID: actor.CorrelationID}
	if _, err := tx.Exec(ctx, `INSERT INTO topology.provider_migration_approval (approval_id, provider_migration_id, plan_id,
		plan_version, plan_digest, decision, reason, decided_by, decided_at, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, NULLIF($10, '')::uuid)`,
		a.ApprovalID, a.SubjectID, a.PlanID, a.PlanVersion, a.PlanDigest, a.Decision, a.Reason, a.DecidedBy, a.DecidedAt, a.CorrelationID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return changeset.Approval{}, ErrProviderMigrationPlanDecided
		}
		return changeset.Approval{}, fmt.Errorf("record provider migration decision: %w", err)
	}
	if req.Decision == changeset.DecisionApproved {
		if _, err := tx.Exec(ctx, `UPDATE topology.provider_migration SET approval_id = $2, updated_at = $3, revision = revision + 1
			WHERE provider_migration_id = $1`, m.ProviderMigrationID, a.ApprovalID, now); err != nil {
			return changeset.Approval{}, fmt.Errorf("approve provider migration: %w", err)
		}
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "provider_migration.decided", m.ProviderMigrationID, map[string]any{
		"provider_migration_id": m.ProviderMigrationID, "approval_id": a.ApprovalID, "plan_id": a.PlanID,
		"plan_digest": a.PlanDigest, "decision": a.Decision}); err != nil {
		return changeset.Approval{}, err
	}
	return a, tx.Commit(ctx)
}

// priorAdvance finds the operation an earlier advance with the same
// idempotency key created, refusing a key reused for another request.
func priorAdvance(ctx context.Context, q migrationQuerier, requester, key, requestHash string) (operations.Operation, bool, error) {
	var id, hash string
	err := q.QueryRow(ctx, `SELECT operation_id, request_hash FROM operations.execution_operation
		WHERE requested_by = $1 AND operation_type = 'PROVIDER_MIGRATION_ADVANCE' AND idempotency_key = $2`, requester, key).Scan(&id, &hash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return operations.Operation{}, false, nil
	case err != nil:
		return operations.Operation{}, false, err
	case hash != requestHash:
		return operations.Operation{}, false, ErrOperationKeyReused
	}
	op, err := scanOperation(q.QueryRow(ctx, `SELECT `+operationColumns+` FROM operations.execution_operation WHERE operation_id = $1`, id))
	return op, err == nil, err
}

// AdvanceProviderMigration runs one lifecycle transition (ADR-SHARED-016
// section 2) as a PROVIDER_MIGRATION_ADVANCE operation. Local steps run in
// this transaction; an engine step issues engine migration tasks and the
// operation waits RUNNING until their reports resume it. A forward
// transition moves the stage when accepted; cancel and roll_back move it
// when they finish. A step that fails before any engine task was issued
// undoes the transition and leaves the stage as it was. A replay of the
// same Idempotency-Key returns the same operation.
func (r *PostgresRepository) AdvanceProviderMigration(ctx context.Context, in MigrationAdvance) (operations.Operation, error) {
	if err := validateActor(in.Actor); err != nil {
		return operations.Operation{}, err
	}
	if op, found, err := priorAdvance(ctx, r.pool, in.RequestedBy, in.IdempotencyKey, in.RequestHash); found || err != nil {
		return op, err
	}
	// Settle the running advance first, in its own transaction, so overdue
	// tasks and cancel requests reach it even when this advance is then
	// refused.
	if err := r.settleProviderMigration(ctx, in.ProviderMigrationID, in.Planner.Policy, in.Now, in.Actor); err != nil {
		return operations.Operation{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return operations.Operation{}, err
	}
	defer tx.Rollback(ctx)
	m, err := scanProviderMigration(tx.QueryRow(ctx, `SELECT `+providerMigrationColumns+`
		FROM topology.provider_migration WHERE provider_migration_id = $1 FOR UPDATE`, in.ProviderMigrationID))
	if err != nil {
		return operations.Operation{}, err
	}
	// A concurrent request with the same key may have advanced while this
	// one waited for the lock: it is that request's replay.
	if op, found, err := priorAdvance(ctx, tx, in.RequestedBy, in.IdempotencyKey, in.RequestHash); found || err != nil {
		return op, err
	}
	if m.Revision != in.ExpectedRevision {
		return operations.Operation{}, ErrProviderMigrationRevisionMismatch
	}
	next, err := migration.Next(m.Stage, in.Transition)
	if err != nil {
		return operations.Operation{}, fmt.Errorf("%w: %v", ErrProviderMigrationStageConflict, err)
	}
	if m.OperationID != "" {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM operations.execution_operation WHERE operation_id = $1`, m.OperationID).Scan(&status); err != nil {
			return operations.Operation{}, fmt.Errorf("read the migration's operation: %w", err)
		}
		if !operationEnded(operations.Status(status)) {
			return operations.Operation{}, ErrProviderMigrationOperationRunning
		}
	}
	plan, err := loadProviderMigrationPlan(ctx, tx, m.PlanID)
	if err != nil {
		return operations.Operation{}, err
	}
	if migration.Forward(in.Transition) {
		if err := r.checkAdvanceAllowed(ctx, tx, in, m, plan); err != nil {
			return operations.Operation{}, err
		}
	}

	var steps []migration.Step
	cohort := m.CurrentCohortKey
	switch in.Transition {
	case migration.TransitionCancel:
		if len(m.ShiftedCohortKeys) > 0 {
			return operations.Operation{}, fmt.Errorf("%w: a cohort's authority has moved; roll back instead", ErrProviderMigrationStageConflict)
		}
		steps = compensationSteps(migration.Lifecycle().Compensation[migration.TransitionCancel])
	case migration.TransitionRollBack:
		frozen, err := frozenCohort(ctx, tx, m)
		if err != nil {
			return operations.Operation{}, err
		}
		if steps, err = rollbackSteps(m, plan, frozen); err != nil {
			return operations.Operation{}, err
		}
	default:
		selection, err := migration.Select(plan, m, in.Transition)
		if err != nil {
			return operations.Operation{}, fmt.Errorf("%w: %v", ErrProviderMigrationStageConflict, err)
		}
		if err := checkCohortOrder(in.Transition, m, plan, selection); err != nil {
			return operations.Operation{}, err
		}
		steps, cohort = selection.Steps, selection.CohortKey
	}

	// The operation row comes first: the run and any tasks reference it.
	if _, err := tx.Exec(ctx, `
		INSERT INTO operations.execution_operation (operation_id, operation_type, status, subject_type, subject_id, plan_id,
			plan_digest, approval_id, requested_by, idempotency_key, request_hash, current_phase, completed_steps, total_steps,
			retryable, correlation_id, created_at, started_at, updated_at)
		VALUES ($1, 'PROVIDER_MIGRATION_ADVANCE', 'RUNNING', 'PROVIDER_MIGRATION', $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, 0, $10,
			false, NULLIF($11, '')::uuid, $12, $12, $12)`,
		in.OperationID, m.ProviderMigrationID, plan.PlanID, plan.PlanDigest, m.ApprovalID, in.RequestedBy, in.IdempotencyKey,
		in.RequestHash, next, len(steps), in.Actor.CorrelationID, in.Now); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "execution_operation_idempotency_uq" {
			return operations.Operation{}, errOperationKeyRace
		}
		return operations.Operation{}, fmt.Errorf("record provider migration operation: %w", err)
	}
	document, err := json.Marshal(steps)
	if err != nil {
		return operations.Operation{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO topology.provider_migration_run (operation_id, provider_migration_id, transition, cohort_key,
		steps, next_step, target_stage, updated_at) VALUES ($1, $2, $3, NULLIF($4, ''), $5::jsonb, 0, $6, $7)`,
		in.OperationID, m.ProviderMigrationID, in.Transition, cohort, document, next, in.Now); err != nil {
		return operations.Operation{}, fmt.Errorf("record provider migration run: %w", err)
	}
	accepted := m
	accepted.OperationID, accepted.CurrentCohortKey = in.OperationID, cohort
	if in.Transition == migration.TransitionPrepare && accepted.StartedAt == nil {
		accepted.StartedAt = &in.Now
	}
	if _, err := r.drive(ctx, tx, accepted, plan, migrationRun{operationID: in.OperationID, transition: in.Transition, cohort: cohort,
		steps: steps, targetStage: next, previousStage: m.Stage, previousCohort: m.CurrentCohortKey}, in.Planner.Policy, in.Now, in.Actor); err != nil {
		return operations.Operation{}, err
	}
	if err := insertProvisioningAudit(ctx, tx, in.Actor, "", "provider_migration.advanced", m.ProviderMigrationID, map[string]any{
		"provider_migration_id": m.ProviderMigrationID, "transition": in.Transition, "from": m.Stage, "to": next,
		"operation_id": in.OperationID, "cohort_key": cohort, "reason": in.Reason}); err != nil {
		return operations.Operation{}, err
	}
	op, err := scanOperation(tx.QueryRow(ctx, `SELECT `+operationColumns+` FROM operations.execution_operation WHERE operation_id = $1`, in.OperationID))
	if err != nil {
		return operations.Operation{}, err
	}
	return op, tx.Commit(ctx)
}

// settleProviderMigration fails the migration's overdue tasks and resumes
// its running advance, committing what that changed.
func (r *PostgresRepository) settleProviderMigration(ctx context.Context, id string, policy *health.Policy, now time.Time, actor AuditActor) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	m, err := scanProviderMigration(tx.QueryRow(ctx, `SELECT `+providerMigrationColumns+`
		FROM topology.provider_migration WHERE provider_migration_id = $1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if m.OperationID == "" {
		return nil
	}
	if err := r.expireAndResume(ctx, tx, m, policy, now, actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// migrationRun is one advance's steps and where it has got to.
type migrationRun struct {
	operationID, transition, cohort, targetStage string
	steps                                        []migration.Step
	next                                         int
	// previousStage and previousCohort are where the migration was before
	// the transition was accepted; "" when resuming.
	previousStage, previousCohort string
}

// resume continues the migration's running advance, if any, from where it
// waits, and returns the migration as it is afterwards.
func (r *PostgresRepository) resume(ctx context.Context, tx pgx.Tx, m migration.Migration, policy *health.Policy, now time.Time, actor AuditActor) (migration.Migration, error) {
	var run migrationRun
	var document []byte
	var cohort *string
	var status string
	err := tx.QueryRow(ctx, `SELECT r.operation_id, r.transition, r.cohort_key, r.steps, r.next_step, r.target_stage, o.status
		FROM topology.provider_migration_run r JOIN operations.execution_operation o ON o.operation_id = r.operation_id
		WHERE r.operation_id = $1 AND o.status IN ('RUNNING', 'CANCEL_REQUESTED') FOR UPDATE OF r`, m.OperationID).
		Scan(&run.operationID, &run.transition, &cohort, &document, &run.next, &run.targetStage, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return m, fmt.Errorf("load provider migration run: %w", err)
	}
	if status == string(operations.StatusCancelRequested) {
		// Asked to stop (/admin/operations/{id}/cancel): the open tasks are
		// cancelled and the advance ends CANCELLED, leaving its cohort in
		// its safe state for the operator to roll back.
		if err := cancelOpenTasks(ctx, tx, run.operationID, now); err != nil {
			return m, err
		}
		if _, err := tx.Exec(ctx, `UPDATE operations.execution_operation SET status = 'CANCELLED', completed_at = $2, updated_at = $2,
			revision = revision + 1 WHERE operation_id = $1`, run.operationID, now); err != nil {
			return m, err
		}
		m.FailureReason = "The advance was cancelled before it finished."
		if _, err := tx.Exec(ctx, `UPDATE topology.provider_migration SET failure_reason = $2, updated_at = $3, revision = revision + 1
			WHERE provider_migration_id = $1`, m.ProviderMigrationID, m.FailureReason, now); err != nil {
			return m, err
		}
		return m, nil
	}
	if cohort != nil {
		run.cohort = *cohort
	}
	if err := json.Unmarshal(document, &run.steps); err != nil {
		return m, err
	}
	plan, err := loadProviderMigrationPlan(ctx, tx, m.PlanID)
	if err != nil {
		return m, err
	}
	return r.drive(ctx, tx, m, plan, run, policy, now, actor)
}

// drive runs the run's steps from run.next until they are all done, an
// engine step waits for its tasks, or a step fails, then records the
// operation, the run and the migration accordingly. Local steps run under a
// savepoint, so a failing step leaves nothing half-applied.
func (r *PostgresRepository) drive(ctx context.Context, tx pgx.Tx, m migration.Migration, plan migration.Plan, run migrationRun,
	policy *health.Policy, now time.Time, actor AuditActor) (migration.Migration, error) {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return m, err
	}
	x := &migrationExecutor{tx: savepoint, m: m, plan: plan, now: now, actor: actor, policy: policy, operationID: run.operationID,
		direction: "FORWARD"}
	if run.transition == migration.TransitionRollBack {
		x.direction = "REVERSE"
	}
	var failed *errMigrationStep
	waiting := false
	for run.next < len(run.steps) {
		step := run.steps[run.next]
		if migration.EngineOperation(step.Operation) {
			done, err := x.engineStep(ctx, step)
			if err != nil {
				var sf errMigrationStep
				if !errors.As(err, &sf) {
					return m, err
				}
				failed = &sf
				break
			}
			if !done {
				waiting = true
				break
			}
		} else if err := x.run(ctx, step, run.transition); err != nil {
			var sf errMigrationStep
			if !errors.As(err, &sf) {
				return m, err
			}
			failed = &sf
			break
		}
		run.next++
	}
	// A failed step undoes this call's local effects; tasks are only ever
	// issued as the last thing a call does, so none are lost.
	if failed != nil {
		if err := savepoint.Rollback(ctx); err != nil {
			return m, err
		}
		x.shifted = nil
	} else if err := savepoint.Commit(ctx); err != nil {
		return m, err
	}
	x.tx = tx
	issued, err := x.tasksIssued(ctx)
	if err != nil {
		return m, err
	}

	stage, status := m.Stage, operations.StatusRunning
	failure, completedAt := "", m.CompletedAt
	cohort := run.cohort
	var result, problem []byte
	currentStep := ""
	switch {
	case failed != nil:
		status, failure = operations.StatusFailed, failed.detail
		if !issued && run.previousStage != "" {
			// Nothing left the Control Plane: the transition is undone, and
			// the operator may issue it again.
			stage, cohort = run.previousStage, run.previousCohort
		} else if migration.Forward(run.transition) {
			stage = run.targetStage
		}
		if run.next < len(run.steps) {
			currentStep = run.steps[run.next].StepID
		}
		correlation := actor.CorrelationID
		if correlation == "" {
			correlation = run.operationID
		}
		problem, _ = json.Marshal(map[string]any{"type": "https://docs.nabhold.com/problems/" + strings.ToLower(failed.code),
			"title": "Provider migration step failed", "status": http.StatusConflict, "code": failed.code, "detail": failed.detail,
			"correlation_id": correlation, "retryable": false})
		if err := cancelOpenTasks(ctx, tx, run.operationID, now); err != nil {
			return m, err
		}
	case waiting:
		if migration.Forward(run.transition) {
			stage = run.targetStage
		}
		currentStep = run.steps[run.next].StepID
	default:
		status, stage = operations.StatusSucceeded, run.targetStage
		if migration.Terminal(stage) {
			completedAt = &now
		}
		result, _ = json.Marshal(map[string]string{"resource_type": "PROVIDER_MIGRATION", "resource_id": m.ProviderMigrationID,
			"resource_state": stage, "summary": fmt.Sprintf("Ran %s: %d step(s); the migration is %s.", run.transition, len(run.steps), stage)})
	}
	shifted := x.shiftedKeys()
	if _, err := tx.Exec(ctx, `UPDATE operations.execution_operation SET status = $2, current_phase = $3, completed_steps = $4,
		current_step = NULLIF($5, ''), result = $6::jsonb, problem = $7::jsonb,
		completed_at = CASE WHEN $2 IN ('SUCCEEDED', 'FAILED') THEN $8::timestamptz END, updated_at = $8, revision = revision + 1
		WHERE operation_id = $1`, run.operationID, string(status), stage, run.next, currentStep, nullJSON(result), nullJSON(problem), now); err != nil {
		return m, fmt.Errorf("record provider migration operation: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE topology.provider_migration_run SET next_step = $2, updated_at = $3 WHERE operation_id = $1`,
		run.operationID, run.next, now); err != nil {
		return m, err
	}
	startedAt := m.StartedAt
	if _, err := tx.Exec(ctx, `UPDATE topology.provider_migration SET stage = $2, current_cohort_key = NULLIF($3, ''),
		operation_id = $4, shifted_cohort_keys = $5, failure_reason = NULLIF($6, ''), started_at = $7, completed_at = $8,
		updated_at = $9, revision = revision + 1
		WHERE provider_migration_id = $1`, m.ProviderMigrationID, stage, cohort, run.operationID, shifted, failure,
		startedAt, completedAt, now); err != nil {
		return m, fmt.Errorf("advance provider migration: %w", err)
	}
	if status != operations.StatusRunning && run.previousStage == "" {
		if err := insertProvisioningAudit(ctx, tx, actor, "", "provider_migration.operation_ended", m.ProviderMigrationID, map[string]any{
			"provider_migration_id": m.ProviderMigrationID, "operation_id": run.operationID, "status": string(status), "stage": stage}); err != nil {
			return m, err
		}
	}
	m.Stage, m.CurrentCohortKey, m.ShiftedCohortKeys, m.FailureReason, m.CompletedAt = stage, cohort, shifted, failure, completedAt
	m.OperationID = run.operationID
	return m, nil
}

// checkAdvanceAllowed applies the forward-transition rules: the current
// plan is approved, has no blockers, is neither expired nor stale, and a
// transition that moves authority runs inside an open cutover window.
func (r *PostgresRepository) checkAdvanceAllowed(ctx context.Context, tx pgx.Tx, in MigrationAdvance, m migration.Migration, plan migration.Plan) error {
	if m.ApprovalID == "" {
		return ErrProviderMigrationNotApproved
	}
	var decision, digest, approver string
	if err := tx.QueryRow(ctx, `SELECT decision, plan_digest, decided_by FROM topology.provider_migration_approval WHERE approval_id = $1`,
		m.ApprovalID).Scan(&decision, &digest, &approver); err != nil || decision != changeset.DecisionApproved || digest != plan.PlanDigest {
		return ErrProviderMigrationNotApproved
	}
	// Approval and execution are held by different people for each
	// migration (ADR-SHARED-016 sections 2 and 6).
	if approver == in.RequestedBy {
		return ErrProviderMigrationSelfExecution
	}
	if m.Blocked || len(plan.Blockers) > 0 {
		return ErrProviderMigrationBlocked
	}
	stale, err := planStale(ctx, tx, in.Planner, m, plan, in.Now)
	if err != nil {
		return err
	}
	if stale {
		return ErrProviderMigrationPlanStale
	}
	if w := plan.Request.CutoverWindow; migration.MovesAuthority(in.Transition) && w != nil &&
		(in.Now.Before(w.StartsAt) || !in.Now.Before(w.EndsAt)) {
		return ErrProviderMigrationWindowClosed
	}
	return nil
}

// checkCohortOrder keeps the cohorts in order: validate follows a shifted
// cohort, and retire_old follows the last cohort's validation.
func checkCohortOrder(transition string, m migration.Migration, plan migration.Plan, selection migration.Selection) error {
	switch transition {
	case migration.TransitionValidate:
		if !slices.Contains(m.ShiftedCohortKeys, m.CurrentCohortKey) {
			return fmt.Errorf("%w: cohort %s has not shifted", ErrProviderMigrationStageConflict, m.CurrentCohortKey)
		}
	case migration.TransitionRetireOld:
		if !migration.LastCohort(plan, m.CurrentCohortKey) {
			return fmt.Errorf("%w: cohorts after %s have not moved", ErrProviderMigrationStageConflict, m.CurrentCohortKey)
		}
	}
	return nil
}

// operationEnded reports whether an operation's status is final, so the
// migration may run another.
func operationEnded(status operations.Status) bool {
	switch status {
	case operations.StatusSucceeded, operations.StatusFailed, operations.StatusCompensated, operations.StatusCompensationFailed,
		operations.StatusCancelled:
		return true
	}
	return false
}

func compensationSteps(ops []string) []migration.Step {
	steps := make([]migration.Step, 0, len(ops))
	for _, op := range ops {
		steps = append(steps, migration.Step{StepID: "compensate-" + strings.ToLower(strings.ReplaceAll(op, "_", "-")), Operation: op})
	}
	return steps
}

// rollbackSteps is what roll_back runs (ADR-SHARED-016 section 5): the
// release of a cohort frozen mid-step, then each shifted cohort, most
// recent first, through its strategy's steps, then the removal of the
// migration's target bindings. A STATELESS_REBIND migration runs only the
// steps that are not engine operations.
func rollbackSteps(m migration.Migration, plan migration.Plan, frozen string) ([]migration.Step, error) {
	strategy := plan.Request.RollbackStrategy
	if strategy == migration.RollbackForwardFixOnly && len(m.ShiftedCohortKeys) > 0 {
		return nil, ErrProviderMigrationNotReversible
	}
	step := func(cohort, op string) migration.Step {
		return migration.Step{StepID: stepKey("rollback-"+cohort, op), Operation: op, Resources: migration.StepResources{CohortKey: cohort}}
	}
	var steps []migration.Step
	if frozen != "" {
		for _, op := range migration.Lifecycle().RollbackRelease {
			steps = append(steps, step(frozen, op))
		}
	}
	for i := len(m.ShiftedCohortKeys) - 1; i >= 0; i-- {
		cohort := m.ShiftedCohortKeys[i]
		for _, op := range migration.Lifecycle().RollbackSteps[strategy] {
			if plan.MigrationMode == migration.ModeStatelessRebind && migration.EngineOperation(op) {
				continue
			}
			steps = append(steps, step(cohort, op))
		}
	}
	return append(steps, compensationSteps([]string{migration.OpRemoveMigrationBinding})...), nil
}

// stepKey is a contract step id from a prefix and an operation, at most 64
// characters.
func stepKey(prefix, op string) string {
	id := prefix + "-" + strings.ToLower(strings.ReplaceAll(op, "_", "-"))
	if len(id) > 64 {
		id = strings.TrimRight(id[:64], "-")
	}
	return id
}

// frozenCohort is the cohort left frozen at the source by a failed forward
// advance: its writes were frozen and it never shifted, so rollback must
// release it first. "" when there is none.
func frozenCohort(ctx context.Context, tx pgx.Tx, m migration.Migration) (string, error) {
	if m.CurrentCohortKey == "" || slices.Contains(m.ShiftedCohortKeys, m.CurrentCohortKey) {
		return "", nil
	}
	var frozen bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM topology.engine_migration_task
		WHERE provider_migration_id = $1 AND cohort_key = $2 AND operation = 'FREEZE_COHORT_WRITES' AND direction = 'FORWARD'
			AND status = 'SUCCEEDED')
		AND NOT EXISTS (SELECT 1 FROM topology.engine_migration_task
		WHERE provider_migration_id = $1 AND cohort_key = $2 AND operation = 'UNFREEZE_COHORT_WRITES' AND direction = 'REVERSE'
			AND status = 'SUCCEEDED')`, m.ProviderMigrationID, m.CurrentCohortKey).Scan(&frozen)
	if err != nil || !frozen {
		return "", err
	}
	return m.CurrentCohortKey, nil
}

// migrationExecutor runs a run's steps: local steps on the Control
// Plane's own state, engine steps as engine migration tasks.
type migrationExecutor struct {
	tx          pgx.Tx
	m           migration.Migration
	plan        migration.Plan
	now         time.Time
	actor       AuditActor
	policy      *health.Policy
	operationID string
	direction   string
	shifted     []string
}

// shiftedKeys is the migration's shifted cohorts after this run, never nil.
func (x *migrationExecutor) shiftedKeys() []string {
	out := x.shifted
	if out == nil {
		out = x.m.ShiftedCohortKeys
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// tasksIssued reports whether the run has issued any engine migration task.
func (x *migrationExecutor) tasksIssued(ctx context.Context) (bool, error) {
	var issued bool
	err := x.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM topology.engine_migration_task WHERE operation_id = $1)`, x.operationID).Scan(&issued)
	return issued, err
}

func (x *migrationExecutor) run(ctx context.Context, step migration.Step, transition string) error {
	switch step.Operation {
	case migration.OpVerifyTargetReadiness:
		// Readiness is the plan's: the advance re-planned against current
		// authoritative state and found it neither blocked nor stale.
		return nil
	case migration.OpCreateMigrationBinding:
		return x.createMigrationBindings(ctx, step)
	case migration.OpStartShadow, migration.OpStopShadow:
		// Shadowing is refused at planning (MIGRATION_SHADOW_UNSAFE), so an
		// approved plan never starts it and there is nothing to stop.
		if step.Operation == migration.OpStartShadow {
			return fmt.Errorf("%w: shadowing is not executable", ErrProviderMigrationNotExecutable)
		}
		return nil
	case migration.OpShiftCohort:
		if transition == migration.TransitionRollBack {
			return x.unshiftCohort(ctx, step.Resources.CohortKey)
		}
		return x.shiftCohort(ctx, step)
	case migration.OpValidateCohort:
		return x.validateCohort(ctx, step.Resources.CohortKey)
	case migration.OpRetireSourceBinding:
		return x.retireSource(ctx, step.Resources.CapabilityKey)
	case migration.OpRemoveMigrationBinding:
		return x.removeMigrationBindings(ctx)
	}
	return fmt.Errorf("%w: %s", ErrProviderMigrationNotExecutable, step.Operation)
}

// cohortOf maps each of the plan's source bindings to its cohort, from the
// cohort steps' binding_ids.
func (x *migrationExecutor) cohortOf() map[string]string {
	out := map[string]string{}
	for _, s := range x.plan.Steps {
		if s.Operation == migration.OpShiftCohort {
			for _, id := range s.Resources.BindingIDs {
				out[id] = s.Resources.CohortKey
			}
		}
	}
	return out
}

// createMigrationBindings binds each named source binding's capability and
// scope to the target instance the plan fixed, in MIGRATION mode, which
// resolution ranks below every authoritative mode.
func (x *migrationExecutor) createMigrationBindings(ctx context.Context, step migration.Step) error {
	cohorts := x.cohortOf()
	var targetInstance, targetProvider string
	if err := x.tx.QueryRow(ctx, `SELECT engine_instance_id::text FROM topology.engine_instance WHERE engine_instance_key = $1`,
		step.Resources.EngineInstanceID).Scan(&targetInstance); err != nil {
		return stepFailure("MIGRATION_TARGET_NOT_REGISTERED", "target instance %s is not registered", step.Resources.EngineInstanceID)
	}
	if err := x.tx.QueryRow(ctx, `SELECT provider_id::text FROM capability.capability_provider WHERE provider_key = $1`,
		x.plan.TargetProviderKey).Scan(&targetProvider); err != nil {
		return stepFailure("MIGRATION_TARGET_NOT_REGISTERED", "target provider %s is not registered", x.plan.TargetProviderKey)
	}
	for _, bindingID := range step.Resources.BindingIDs {
		source, err := domain.ParseResourceID("bind", bindingID)
		if err != nil {
			return err
		}
		var exists bool
		if err := x.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM topology.provider_migration_binding
			WHERE provider_migration_id = $1 AND source_binding_id = $2::uuid)`, x.m.ProviderMigrationID, source).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		var mode string
		err = x.tx.QueryRow(ctx, `SELECT binding_mode FROM capability.capability_binding
			WHERE id = $1::uuid AND UPPER(status) = 'ACTIVE' AND binding_mode IN ('PRIMARY', 'FALLBACK') AND valid_period @> $2::timestamptz
			FOR UPDATE`, source, x.now).Scan(&mode)
		if errors.Is(err, pgx.ErrNoRows) {
			return stepFailure("PLAN_STALE", "source binding %s is no longer an active authoritative binding", bindingID)
		}
		if err != nil {
			return err
		}
		var target string
		if err := x.tx.QueryRow(ctx, `
			INSERT INTO capability.capability_binding (capability_id, engine_instance_id, scope_id, binding_mode, priority, status,
				contract_version, effective_from, provider_id, configuration)
			SELECT capability_id, $2::uuid, scope_id, 'MIGRATION', priority, 'ACTIVE', contract_version, $3, $4::uuid, '{}'::jsonb
			FROM capability.capability_binding WHERE id = $1::uuid
			RETURNING id::text`, source, targetInstance, x.now, targetProvider).Scan(&target); err != nil {
			return fmt.Errorf("create migration binding for %s: %w", bindingID, err)
		}
		if _, err := x.tx.Exec(ctx, `INSERT INTO topology.provider_migration_binding (provider_migration_id, source_binding_id,
			source_mode, capability_key, cohort_key, target_binding_id, target_engine_instance_id, state, updated_at)
			VALUES ($1, $2::uuid, $3, $4, $5, $6::uuid, $7::uuid, 'PREPARED', $8)`,
			x.m.ProviderMigrationID, source, mode, step.Resources.CapabilityKey, cohorts[bindingID], target, targetInstance, x.now); err != nil {
			return fmt.Errorf("record migration binding for %s: %w", bindingID, err)
		}
	}
	return nil
}

type ledgerRow struct {
	source, target, mode, capability, cohort, state, instance string
}

func (x *migrationExecutor) ledger(ctx context.Context, where string, args ...any) ([]ledgerRow, error) {
	rows, err := x.tx.Query(ctx, `SELECT source_binding_id::text, target_binding_id::text, source_mode, capability_key, cohort_key, state,
		target_engine_instance_id::text
		FROM topology.provider_migration_binding WHERE provider_migration_id = $1 AND `+where+` ORDER BY source_binding_id FOR UPDATE`,
		append([]any{x.m.ProviderMigrationID}, args...)...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ledgerRow, error) {
		var l ledgerRow
		err := row.Scan(&l.source, &l.target, &l.mode, &l.capability, &l.cohort, &l.state, &l.instance)
		return l, err
	})
}

func (x *migrationExecutor) setLedger(ctx context.Context, source, state string) error {
	_, err := x.tx.Exec(ctx, `UPDATE topology.provider_migration_binding SET state = $3, updated_at = $4
		WHERE provider_migration_id = $1 AND source_binding_id = $2::uuid`, x.m.ProviderMigrationID, source, state, x.now)
	return err
}

func (x *migrationExecutor) setMode(ctx context.Context, binding, mode string) error {
	_, err := x.tx.Exec(ctx, `UPDATE capability.capability_binding SET binding_mode = $2, version = version + 1, updated_at = $3
		WHERE id = $1::uuid`, binding, mode, x.now)
	return err
}

func (x *migrationExecutor) addShifted(cohort string) {
	if x.shifted == nil {
		x.shifted = slices.Clone(x.m.ShiftedCohortKeys)
	}
	if !slices.Contains(x.shifted, cohort) {
		x.shifted = append(x.shifted, cohort)
	}
}

func (x *migrationExecutor) removeShifted(cohort string) {
	if x.shifted == nil {
		x.shifted = slices.Clone(x.m.ShiftedCohortKeys)
	}
	x.shifted = slices.DeleteFunc(x.shifted, func(k string) bool { return k == cohort })
	if x.shifted == nil {
		x.shifted = []string{}
	}
}

// shiftCohort moves the cohort's authority to the target: each source
// binding steps down to MIGRATION before its target binding takes the
// source's mode, so at most one authoritative binding holds the scope.
func (x *migrationExecutor) shiftCohort(ctx context.Context, step migration.Step) error {
	rows, err := x.ledger(ctx, `cohort_key = $2`, step.Resources.CohortKey)
	if err != nil {
		return err
	}
	if len(rows) != len(step.Resources.BindingIDs) {
		return stepFailure("PLAN_STALE", "cohort %s has %d prepared binding(s), the plan names %d", step.Resources.CohortKey, len(rows), len(step.Resources.BindingIDs))
	}
	for _, l := range rows {
		if l.state != "PREPARED" {
			return stepFailure("PROVIDER_MIGRATION_STAGE_CONFLICT", "binding %s is %s, not PREPARED", l.source, l.state)
		}
		if err := x.setMode(ctx, l.source, "MIGRATION"); err != nil {
			return err
		}
		if err := x.setMode(ctx, l.target, l.mode); err != nil {
			return fmt.Errorf("shift binding %s: %w", l.source, err)
		}
		if err := x.setLedger(ctx, l.source, "SHIFTED"); err != nil {
			return err
		}
	}
	x.addShifted(step.Resources.CohortKey)
	return nil
}

// unshiftCohort returns a shifted cohort's authority to the source: the
// target steps down to MIGRATION before the source takes its mode back.
func (x *migrationExecutor) unshiftCohort(ctx context.Context, cohort string) error {
	rows, err := x.ledger(ctx, `cohort_key = $2 AND state = 'SHIFTED'`, cohort)
	if err != nil {
		return err
	}
	for _, l := range rows {
		if err := x.setMode(ctx, l.target, "MIGRATION"); err != nil {
			return err
		}
		if err := x.setMode(ctx, l.source, l.mode); err != nil {
			return fmt.Errorf("return binding %s: %w", l.source, err)
		}
		if err := x.setLedger(ctx, l.source, "PREPARED"); err != nil {
			return err
		}
	}
	x.removeShifted(cohort)
	return nil
}

// validateCohort checks the cohort now resolves to the target: every
// target binding is ACTIVE in the source's mode, every source binding has
// stepped down, and every target instance is healthy enough to serve.
func (x *migrationExecutor) validateCohort(ctx context.Context, cohort string) error {
	rows, err := x.ledger(ctx, `cohort_key = $2`, cohort)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(x.plan.Request.Capabilities))
	for _, c := range x.plan.Request.Capabilities {
		keys = append(keys, c.CapabilityKey)
	}
	instances, err := migrationFacts{q: x.tx}.ProviderInstances(ctx, x.plan.TargetProviderKey, keys)
	if err != nil {
		return err
	}
	for _, l := range rows {
		var targetMode, targetStatus, sourceMode, instanceKey string
		if err := x.tx.QueryRow(ctx, `SELECT t.binding_mode, UPPER(t.status), s.binding_mode, ei.engine_instance_key
			FROM capability.capability_binding t, capability.capability_binding s, topology.engine_instance ei
			WHERE t.id = $1::uuid AND s.id = $2::uuid AND ei.engine_instance_id = t.engine_instance_id`,
			l.target, l.source).Scan(&targetMode, &targetStatus, &sourceMode, &instanceKey); err != nil {
			return err
		}
		if l.state != "SHIFTED" || targetMode != l.mode || targetStatus != "ACTIVE" || sourceMode != "MIGRATION" {
			return stepFailure("MIGRATION_VALIDATION_FAILED", "binding %s does not resolve to the target", l.source)
		}
		i := slices.IndexFunc(instances, func(in migration.Instance) bool { return in.EngineInstanceID == instanceKey })
		if i < 0 || !strings.EqualFold(instances[i].Status, "ACTIVE") ||
			(x.policy != nil && !x.policy.Evaluate(health.CriticalityCritical, instances[i].Health[l.capability], x.now).Eligible) {
			return stepFailure("MIGRATION_VALIDATION_FAILED", "target instance %s cannot serve %s", instanceKey, l.capability)
		}
	}
	return nil
}

// retireSource retires the source bindings of a capability once every
// cohort has moved.
func (x *migrationExecutor) retireSource(ctx context.Context, capability string) error {
	rows, err := x.ledger(ctx, `capability_key = $2`, capability)
	if err != nil {
		return err
	}
	for _, l := range rows {
		if l.state != "SHIFTED" {
			return stepFailure("PROVIDER_MIGRATION_STAGE_CONFLICT", "binding %s is %s, not SHIFTED", l.source, l.state)
		}
		if _, err := x.tx.Exec(ctx, `UPDATE capability.capability_binding SET status = 'RETIRED', effective_to = $2, version = version + 1,
			updated_at = $2 WHERE id = $1::uuid`, l.source, x.now); err != nil {
			return fmt.Errorf("retire source binding %s: %w", l.source, err)
		}
		if err := x.setLedger(ctx, l.source, "RETIRED"); err != nil {
			return err
		}
	}
	return nil
}

// removeMigrationBindings retires the target bindings of every cohort that
// has not shifted (cancel, and the end of a rollback).
func (x *migrationExecutor) removeMigrationBindings(ctx context.Context) error {
	rows, err := x.ledger(ctx, `state = 'PREPARED'`)
	if err != nil {
		return err
	}
	for _, l := range rows {
		if _, err := x.tx.Exec(ctx, `UPDATE capability.capability_binding SET status = 'RETIRED', effective_to = $2, version = version + 1,
			updated_at = $2 WHERE id = $1::uuid`, l.target, x.now); err != nil {
			return fmt.Errorf("remove migration binding %s: %w", l.target, err)
		}
		if err := x.setLedger(ctx, l.source, "REMOVED"); err != nil {
			return err
		}
	}
	return nil
}
