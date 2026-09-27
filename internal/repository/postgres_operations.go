package repository

import (
	"context"
	"encoding/json"
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

// ClaimOperation takes the oldest runnable operation of type opType under
// a lease: a QUEUED one, or one whose executor's lease expired, which is
// resumed as a new execution attempt. It returns false when none is
// runnable.
func (r *PostgresRepository) ClaimOperation(ctx context.Context, opType string, lease time.Duration) (operations.Operation, bool, error) {
	op, err := scanOperation(r.pool.QueryRow(ctx, `
		UPDATE operations.execution_operation o
		SET status = 'RUNNING', lease_expires_at = now() + $2::interval, started_at = COALESCE(o.started_at, now()),
			execution_attempt = o.execution_attempt + CASE WHEN o.status = 'QUEUED' THEN 0 ELSE 1 END,
			revision = o.revision + 1, updated_at = now()
		WHERE o.operation_id = (
			SELECT operation_id FROM operations.execution_operation
			WHERE operation_type = $1 AND (status = 'QUEUED'
				OR (status IN ('PREPARING', 'RUNNING') AND lease_expires_at < now()))
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1)
		RETURNING `+operationColumns, opType, lease.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.Operation{}, false, nil
	}
	return op, err == nil, err
}

// CompleteOperation records the outcome and releases the lease.
func (r *PostgresRepository) CompleteOperation(ctx context.Context, id string, outcome operations.Outcome) error {
	terminal := outcome.Status == operations.StatusSucceeded || outcome.Status == operations.StatusFailed
	_, err := r.pool.Exec(ctx, `
		UPDATE operations.execution_operation
		SET status = $2, current_phase = NULLIF($3, ''), retryable = $4, result = $5, problem = $6,
			completed_at = CASE WHEN $7 THEN now() END, lease_expires_at = NULL,
			revision = revision + 1, updated_at = now()
		WHERE operation_id = $1`,
		id, string(outcome.Status), outcome.CurrentPhase, outcome.Retryable, nullJSON(outcome.Result), nullJSON(outcome.Problem), terminal)
	return err
}

func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}

// ReplayOperation returns the operation a principal created with an
// idempotency key, or ErrOperationNotFound.
func (r *PostgresRepository) ReplayOperation(ctx context.Context, requestedBy, opType, key string) (operations.Operation, error) {
	op, err := scanOperation(r.pool.QueryRow(ctx, `SELECT `+operationColumns+` FROM operations.execution_operation
		WHERE requested_by = $1 AND operation_type = $2 AND idempotency_key = $3`, requestedBy, opType, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.Operation{}, ErrOperationNotFound
	}
	return op, err
}

// OperationRequestHash is the hash of the request that created an
// operation; empty when it has none or cannot be read, which never matches.
func (r *PostgresRepository) OperationRequestHash(ctx context.Context, id string) string {
	var hash *string
	if err := r.pool.QueryRow(ctx, `SELECT request_hash FROM operations.execution_operation WHERE operation_id = $1`, id).Scan(&hash); err != nil || hash == nil {
		return ""
	}
	return *hash
}
