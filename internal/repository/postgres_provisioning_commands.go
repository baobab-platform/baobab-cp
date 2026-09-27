package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/jackc/pgx/v5"
)

// PlanByIdempotencyKey returns the plan a replan with key created, and the
// hash of that request.
func (r *PostgresRepository) PlanByIdempotencyKey(ctx context.Context, id, key string) (string, string, error) {
	var planID, hash string
	err := r.pool.QueryRow(ctx, `SELECT plan_id, request_hash FROM provisioning.plan
		WHERE tenant_provisioning_id = $1::uuid AND idempotency_key = $2`, id, key).Scan(&planID, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrProvisioningNotFound
	}
	return planID, hash, err
}

// Replan supersedes the current plan with plan, under the caller's
// revision. A decision on the superseded plan lapses with it; a plan with
// blockers leaves the provisioning BLOCKED.
func (r *PostgresRepository) Replan(ctx context.Context, id string, expectedRevision int64, plan convergence.Plan, key, requestHash, reason string, actor AuditActor) error {
	if err := validateActor(actor); err != nil {
		return err
	}
	document, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	state := "PLANNED"
	if len(plan.Blockers) > 0 {
		state = "BLOCKED"
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tenantID, tpKey, err := lockRevision(ctx, tx, id, expectedRevision)
	if err != nil {
		return err
	}
	if err := refuseWhileActive(ctx, tx, tpKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE provisioning.plan SET superseded_at = now()
		WHERE tenant_provisioning_id = $1::uuid AND superseded_at IS NULL`, id); err != nil {
		return fmt.Errorf("supersede plan: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provisioning.plan (plan_id, tenant_provisioning_id, plan_version, plan_digest, base_revision,
			desired_state_version, document, generated_at, expires_at, idempotency_key, request_hash)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		plan.PlanID, id, plan.PlanVersion, plan.PlanDigest, plan.BaseRevision, plan.DesiredStateVersion, document,
		plan.GeneratedAt, plan.ExpiresAt, key, requestHash); err != nil {
		return fmt.Errorf("insert plan: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE provisioning.tenant_provisioning SET state = $2, blocked_reason = NULL
		WHERE tenant_provisioning_id = $1::uuid`, id, state); err != nil {
		return err
	}
	if err := bumpRevision(ctx, tx, id); err != nil {
		return err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, tenantID, "tenant_provisioning.replanned", tpKey, map[string]any{
		"plan_id": plan.PlanID, "plan_version": plan.PlanVersion, "plan_digest": plan.PlanDigest, "state": state,
		"reason": reason}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Withdraw abandons a provisioning that is not executing: it is CANCELLED,
// and no runtime state changes.
func (r *PostgresRepository) Withdraw(ctx context.Context, id string, expectedRevision int64, reason string, actor AuditActor) error {
	if err := validateActor(actor); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tenantID, tpKey, err := lockRevision(ctx, tx, id, expectedRevision)
	if err != nil {
		return err
	}
	if err := refuseWhileActive(ctx, tx, tpKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE provisioning.tenant_provisioning SET status = 'CANCELLED', state = 'CANCELLED',
		blocked_reason = NULL WHERE tenant_provisioning_id = $1::uuid`, id); err != nil {
		return err
	}
	if err := bumpRevision(ctx, tx, id); err != nil {
		return err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, tenantID, "tenant_provisioning.withdrawn", tpKey,
		map[string]any{"reason": reason}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RetryOperation queues a retryable FAILED or BLOCKED operation again, as a
// new execution attempt. A replay with the key of the retry that queued it
// returns the operation as it is now.
func (r *PostgresRepository) RetryOperation(ctx context.Context, operationID, key, reason string, actor AuditActor) (operations.Operation, error) {
	return r.operationCommand(ctx, operationID, reason, "operation.retried", actor, func(tx pgx.Tx, op operations.Operation) (operations.Operation, error) {
		var last *string
		if err := tx.QueryRow(ctx, `SELECT retry_idempotency_key FROM operations.execution_operation WHERE operation_id = $1`,
			op.ID).Scan(&last); err != nil {
			return operations.Operation{}, err
		}
		// A key queues one attempt: its replay returns the operation as it
		// is now, even once that attempt has failed and is retryable.
		if last != nil && *last == key {
			return op, nil
		}
		if !op.Retryable || (op.Status != operations.StatusFailed && op.Status != operations.StatusBlocked) {
			return operations.Operation{}, ErrOperationNotRetryable
		}
		next, err := scanOperation(tx.QueryRow(ctx, `
			UPDATE operations.execution_operation SET status = 'QUEUED', execution_attempt = execution_attempt + 1,
				retryable = false, result = NULL, problem = NULL, completed_at = NULL, lease_expires_at = NULL,
				retry_idempotency_key = $2, revision = revision + 1, updated_at = now()
			WHERE operation_id = $1 RETURNING `+operationColumns, op.ID, key))
		if err != nil {
			return operations.Operation{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE provisioning.tenant_provisioning SET blocked_reason = NULL, version = version + 1,
			updated_at = now() WHERE tenant_provisioning_key = $1`, op.SubjectID)
		return next, err
	})
}

// CancelOperation cancels a queued operation at once, and asks a running one
// to stop at its next safe point (CANCEL_REQUESTED). Either way the
// provisioning ends BLOCKED with EXECUTION_CANCELLED. Cancelling an
// operation already asked to stop returns it unchanged.
func (r *PostgresRepository) CancelOperation(ctx context.Context, operationID, reason string, actor AuditActor) (operations.Operation, error) {
	return r.operationCommand(ctx, operationID, reason, "operation.cancelled", actor, func(tx pgx.Tx, op operations.Operation) (operations.Operation, error) {
		switch op.Status {
		case operations.StatusQueued:
			next, err := scanOperation(tx.QueryRow(ctx, `
				UPDATE operations.execution_operation SET status = 'CANCELLED', completed_at = now(), lease_expires_at = NULL,
					retryable = false, revision = revision + 1, updated_at = now()
				WHERE operation_id = $1 RETURNING `+operationColumns, op.ID))
			if err != nil {
				return operations.Operation{}, err
			}
			return next, blockCancelled(ctx, tx, op.SubjectID)
		case operations.StatusPreparing, operations.StatusRunning:
			return scanOperation(tx.QueryRow(ctx, `
				UPDATE operations.execution_operation SET status = 'CANCEL_REQUESTED', revision = revision + 1, updated_at = now()
				WHERE operation_id = $1 RETURNING `+operationColumns, op.ID))
		case operations.StatusCancelRequested:
			return op, nil
		default:
			return operations.Operation{}, ErrOperationNotCancellable
		}
	})
}

// FinishAbandonedCancellations completes as CANCELLED the operations asked
// to stop whose executor's lease lapsed, so no executor will reach their
// safe point. Their provisionings end BLOCKED with EXECUTION_CANCELLED.
func (r *PostgresRepository) FinishAbandonedCancellations(ctx context.Context) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		UPDATE operations.execution_operation SET status = 'CANCELLED', completed_at = now(), lease_expires_at = NULL,
			retryable = false, revision = revision + 1, updated_at = now()
		WHERE status = 'CANCEL_REQUESTED' AND lease_expires_at < now()
		RETURNING subject_id`)
	if err != nil {
		return 0, err
	}
	var subjects []string
	for rows.Next() {
		var subject string
		if err := rows.Scan(&subject); err != nil {
			rows.Close()
			return 0, err
		}
		subjects = append(subjects, subject)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, subject := range subjects {
		if err := blockCancelled(ctx, tx, subject); err != nil {
			return 0, err
		}
	}
	return len(subjects), tx.Commit(ctx)
}

func blockCancelled(ctx context.Context, tx pgx.Tx, subject string) error {
	_, err := tx.Exec(ctx, `UPDATE provisioning.tenant_provisioning SET state = 'BLOCKED',
		blocked_reason = 'EXECUTION_CANCELLED', version = version + 1, updated_at = now()
		WHERE tenant_provisioning_key = $1`, subject)
	return err
}

// operationCommand runs command on the locked operation, with its audit
// record, in one transaction.
func (r *PostgresRepository) operationCommand(ctx context.Context, operationID, reason, action string, actor AuditActor,
	command func(pgx.Tx, operations.Operation) (operations.Operation, error)) (operations.Operation, error) {
	if err := validateActor(actor); err != nil {
		return operations.Operation{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return operations.Operation{}, err
	}
	defer tx.Rollback(ctx)
	op, err := scanOperation(tx.QueryRow(ctx, `SELECT `+operationColumns+` FROM operations.execution_operation
		WHERE operation_id = $1 FOR UPDATE`, operationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.Operation{}, ErrOperationNotFound
	}
	if err != nil {
		return operations.Operation{}, err
	}
	next, err := command(tx, op)
	if err != nil {
		return operations.Operation{}, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, op.TenantID, action, op.ID, map[string]any{
		"subject_id": op.SubjectID, "status": string(next.Status), "reason": reason}); err != nil {
		return operations.Operation{}, err
	}
	return next, tx.Commit(ctx)
}

// refuseWhileActive is ErrOperationInProgress while an operation on the
// provisioning is still in progress.
func refuseWhileActive(ctx context.Context, tx pgx.Tx, subject string) error {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM operations.execution_operation
		WHERE subject_type = 'TENANT_PROVISIONING' AND subject_id = $1 AND status IN (`+activeOperationStatuses+`))`,
		subject).Scan(&active); err != nil {
		return err
	}
	if active {
		return ErrOperationInProgress
	}
	return nil
}
