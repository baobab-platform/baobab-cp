package repository

import (
	"context"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/jackc/pgx/v5"
)

// ErrOperationNotFound is returned for an unknown operation.
var ErrOperationNotFound = errors.New("operation not found")

// OperationRepository reads durable operations (ADR-BCP-022 section 58).
type OperationRepository interface {
	GetOperation(ctx context.Context, id string) (operations.Operation, error)
}

var _ OperationRepository = (*PostgresRepository)(nil)

const operationColumns = `operation_id, operation_type, status, subject_type, subject_id, COALESCE(tenant_id, ''),
	COALESCE(plan_id, ''), COALESCE(plan_digest, ''), COALESCE(approval_id, ''), requested_by, COALESCE(current_phase, ''),
	completed_steps, total_steps, COALESCE(current_step, ''), execution_attempt, retryable, result, problem, revision,
	COALESCE(correlation_id::text, ''), created_at, started_at, updated_at, completed_at`

func scanOperation(row pgx.Row) (operations.Operation, error) {
	var op operations.Operation
	var status string
	var result, problem []byte
	var created, updated time.Time
	err := row.Scan(&op.ID, &op.Type, &status, &op.SubjectType, &op.SubjectID, &op.TenantID, &op.PlanID, &op.PlanDigest,
		&op.ApprovalID, &op.RequestedBy, &op.CurrentPhase, &op.CompletedSteps, &op.TotalSteps, &op.CurrentStep,
		&op.ExecutionAttempt, &op.Retryable, &result, &problem, &op.Revision, &op.CorrelationID, &created, &op.StartedAt,
		&updated, &op.CompletedAt)
	if err != nil {
		return operations.Operation{}, err
	}
	op.Status, op.Result, op.Problem = operations.Status(status), result, problem
	op.CreatedAt, op.UpdatedAt = created.UTC(), updated.UTC()
	return op, nil
}

func (r *PostgresRepository) GetOperation(ctx context.Context, id string) (operations.Operation, error) {
	if !operations.ValidID(id) {
		return operations.Operation{}, ErrOperationNotFound
	}
	op, err := scanOperation(r.pool.QueryRow(ctx, `SELECT `+operationColumns+` FROM operations.execution_operation WHERE operation_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.Operation{}, ErrOperationNotFound
	}
	return op, err
}
