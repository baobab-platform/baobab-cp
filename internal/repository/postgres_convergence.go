package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/market"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Errors of provisioning as desired-state convergence (ADR-SHARED-015).
var (
	ErrProvisioningNotFound = errors.New("tenant provisioning not found")
	ErrProvisioningLive     = errors.New("the tenant already has a live provisioning")
	ErrPlanAlreadyDecided   = errors.New("the plan already has a decision")
	ErrOperationInProgress  = errors.New("an operation on the provisioning is already in progress")
	ErrOperationKeyReused   = errors.New("the idempotency key was used for a different request")
	// ErrOperationNotRetryable: only a retryable FAILED or BLOCKED operation is retried.
	ErrOperationNotRetryable = errors.New("the operation cannot be retried")
	// ErrOperationNotCancellable: a finished operation cannot be cancelled.
	ErrOperationNotCancellable = errors.New("the operation has finished and cannot be cancelled")
)

// ConvergenceRepository persists provisioning planned from desired state.
type ConvergenceRepository interface {
	convergence.Registry
	convergence.ExecutionRegistry
	CreateConvergedProvisioning(ctx context.Context, p NewConvergedProvisioning, actor AuditActor) error
	GetConvergedProvisioning(ctx context.Context, id string) (ConvergedProvisioning, error)
	GetConvergedProvisioningByIdempotencyKey(ctx context.Context, tenantID, key string) (ConvergedProvisioning, error)
	ListConvergedProvisionings(ctx context.Context, tenantID string, limit int, after string) ([]ConvergedProvisioning, error)
	GetDesiredState(ctx context.Context, id string, version int64) (convergence.DesiredState, error)
	RecordPlanDecision(ctx context.Context, id string, expectedRevision int64, decision PlanDecision, actor AuditActor) error
	AcceptApply(ctx context.Context, id string, expectedRevision int64, op operations.Operation, key, requestHash string, actor AuditActor) (operations.Operation, bool, error)
	AcceptRemediation(ctx context.Context, id string, expectedRevision int64, op operations.Operation, key, requestHash string, actor AuditActor) (operations.Operation, bool, error)
	Replan(ctx context.Context, id string, expectedRevision int64, plan convergence.Plan, key, requestHash, reason string, actor AuditActor) error
	PlanByIdempotencyKey(ctx context.Context, id, key string) (planID, requestHash string, err error)
	Withdraw(ctx context.Context, id string, expectedRevision int64, reason string, actor AuditActor) error
	RetryOperation(ctx context.Context, operationID, key, reason string, actor AuditActor) (operations.Operation, error)
	CancelOperation(ctx context.Context, operationID, reason string, actor AuditActor) (operations.Operation, error)
	GetOperation(ctx context.Context, id string) (operations.Operation, error)
	ReplayOperation(ctx context.Context, requestedBy, opType, key string) (operations.Operation, error)
	OperationRequestHash(ctx context.Context, id string) string
	GetTenantOnboardingRequest(ctx context.Context, id string) (domain.TenantOnboardingRequest, error)
}

// NewConvergedProvisioning is a provisioning, its frozen desired state and
// its first plan, created together.
type NewConvergedProvisioning struct {
	ID, TenantID, IdempotencyKey, RequestHash, CreatedBy string
	Desired                                              convergence.DesiredState
	Plan                                                 convergence.Plan
	StartedAt                                            time.Time
}

// PlanDecision is a decision bound to one plan's id, version and digest.
type PlanDecision struct {
	ApprovalID, PlanID, PlanDigest, Decision, Reason, DecidedBy, CorrelationID string
	PlanVersion                                                                int
	DecidedAt                                                                  time.Time
}

// ConvergedProvisioning is a provisioning planned from desired state, with
// its current plan, the decision on that plan and its latest operation.
type ConvergedProvisioning struct {
	ID, Key, TenantID, State, CreatedBy, ReadinessStatus string
	TenantOnboardingRequestID, AdmissionDecisionID       string
	DesiredStateVersion                                  int64
	DesiredStateDigest                                   string
	LegacyBlockingReasons                                []string
	Revision                                             int64
	CreatedAt, UpdatedAt                                 time.Time
	Plan                                                 *convergence.Plan
	Decision                                             *PlanDecision
	OperationID                                          string
	// LegacyStatus is the orchestrator's status; BlockedReason why the
	// provisioning is BLOCKED when no finding says; OperationActive whether
	// an operation on it is still in progress.
	LegacyStatus, BlockedReason string
	OperationActive             bool
}

var _ ConvergenceRepository = (*PostgresRepository)(nil)

// PlanningProduct returns the product's highest ACTIVE version.
func (r *PostgresRepository) PlanningProduct(ctx context.Context, productID string) (convergence.Product, bool, error) {
	var p convergence.Product
	err := r.pool.QueryRow(ctx, `
		SELECT v.product_id, v.composition_key
		FROM product.product_version v JOIN product.product p ON p.product_id = v.product_id
		WHERE v.product_id = $1 AND p.status = 'ACTIVE' AND v.status = 'ACTIVE'
		ORDER BY string_to_array(v.version, '.')::int[] DESC
		LIMIT 1`, productID).Scan(&p.ProductID, &p.CompositionKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return convergence.Product{}, false, nil
	}
	return p, err == nil, err
}

func (r *PostgresRepository) PlanningComposition(ctx context.Context, compositionKey string) ([]convergence.Member, bool, error) {
	composition, err := r.GetActiveComposition(ctx, compositionKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	members := make([]convergence.Member, 0, len(composition.Members))
	for _, m := range composition.Members {
		members = append(members, convergence.Member{CapabilityKey: m.CapabilityKey, Criticality: string(m.Criticality)})
	}
	return members, true, nil
}

func (r *PostgresRepository) PlanningMarket(ctx context.Context, code string) (bool, error) {
	// The registry is the market authority (market-lifecycle.yaml
	// participation): a country is plannable only while a registry market
	// in an available status covers it. A country row alone is not enough.
	var found bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM market.market m
			JOIN market.country_coverage c ON c.country_code = m.code
			WHERE m.code = $1 AND m.is_active AND c.status = ANY($2))`,
		code, market.AvailableStatuses()).Scan(&found)
	return found, err
}

func (r *PostgresRepository) PlanningCapability(ctx context.Context, capabilityKey string) (bool, error) {
	capability, err := r.GetCapability(ctx, capabilityKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && capability.IsResolvable(), err
}

// PlanningCandidates lists the active engine instances of ACTIVE providers
// currently supporting the capability. Only engines whose code is a
// conforming ADR-SHARED-012 engineId are candidates: a plan never names a
// nonconforming engine. Each carries the capability's health criticality
// and the health held for the instance, the provider and the provider's
// capability, which the planner judges as resolution does.
func (r *PostgresRepository) PlanningCandidates(ctx context.Context, capabilityKey string) ([]convergence.Candidate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.provider_key, e.code, ei.engine_instance_key, ei.region, ei.environment,
			COALESCE(p.metadata->>'production_permitted', '') = 'true',
			c.health_criticality, ei.engine_instance_id::text, p.provider_id::text
		FROM capability.capability_provider p
		JOIN capability.provider_capability_support s ON s.provider_id = p.provider_id
		JOIN capability.capability c ON c.capability_id = s.capability_id
		JOIN topology.engine e ON e.engine_id = p.engine_id
		JOIN topology.engine_instance ei ON ei.engine_id = e.engine_id
		WHERE c.code = $1 AND p.status = 'ACTIVE' AND s.status = 'ACTIVE'
			AND s.effective_from <= now() AND (s.effective_to IS NULL OR s.effective_to > now())
			AND lower(ei.status) = 'active'
			AND length(e.code) BETWEEN 3 AND 63 AND e.code ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'
		ORDER BY p.provider_key, ei.engine_instance_key`, capabilityKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		candidate                  convergence.Candidate
		instanceRowID, providerRow string
	}
	var found []row
	for rows.Next() {
		var f row
		c := &f.candidate
		if err := rows.Scan(&c.ProviderKey, &c.EngineID, &c.EngineInstanceID, &c.Region, &c.Environment, &c.ProductionPermitted,
			&c.HealthCriticality, &f.instanceRowID, &f.providerRow); err != nil {
			return nil, err
		}
		found = append(found, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	out := make([]convergence.Candidate, 0, len(found))
	for _, f := range found {
		levels, err := r.HealthLevels(ctx, f.instanceRowID, f.providerRow, capabilityKey)
		if err != nil {
			return nil, err
		}
		f.candidate.Health = levels
		out = append(out, f.candidate)
	}
	return out, nil
}

// EngineRowIDByCode returns the row id of the engine whose engineId is code.
func (r *PostgresRepository) EngineRowIDByCode(ctx context.Context, code string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code = $1`, code).Scan(&id)
	return id, err
}

// CreateConvergedProvisioning creates the provisioning, its frozen desired
// state and its first plan in one transaction, with the audit record. A plan
// with blockers leaves the provisioning BLOCKED.
func (r *PostgresRepository) CreateConvergedProvisioning(ctx context.Context, p NewConvergedProvisioning, actor AuditActor) error {
	if err := validateActor(actor); err != nil {
		return err
	}
	desired, err := json.Marshal(p.Desired)
	if err != nil {
		return fmt.Errorf("marshal desired state: %w", err)
	}
	plan, err := json.Marshal(p.Plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	state := "PLANNED"
	if len(p.Plan.Blockers) > 0 {
		state = "BLOCKED"
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	row := provisioningdomain.TenantProvisioning{
		ID: p.ID, TenantID: p.TenantID, IdempotencyKey: p.IdempotencyKey, RequestHash: p.RequestHash,
		Status: provisioningdomain.ProvisioningStatusPlan, DesiredStateVersion: p.Desired.Provenance.DesiredStateVersion,
		IsolationRequirement: p.Desired.IsolationRequirement, ResidencyRequirement: p.Desired.ResidencyRequirement,
		StartedAt: p.StartedAt, Version: 1,
	}
	if err := execInsertTenantProvisioning(ctx, tx, row); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provisioning.tenant_provisioning
		SET tenant_onboarding_request_id = $2, admission_decision_id = $3, created_by = $4, state = $5
		WHERE tenant_provisioning_id = $1::uuid`,
		p.ID, p.Desired.Provenance.TenantOnboardingRequestID, p.Desired.Provenance.AdmissionDecisionID, p.CreatedBy, state); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "tenant_provisioning_live_uq" {
			return ErrProvisioningLive
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provisioning.desired_state (tenant_provisioning_id, desired_state_version, document, desired_state_digest, frozen_at)
		VALUES ($1::uuid, $2, $3, $4, $5)`,
		p.ID, p.Desired.Provenance.DesiredStateVersion, desired, p.Desired.DesiredStateDigest, p.Desired.FrozenAt); err != nil {
		return fmt.Errorf("insert desired state: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provisioning.plan (plan_id, tenant_provisioning_id, plan_version, plan_digest, base_revision,
			desired_state_version, document, generated_at, expires_at)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9)`,
		p.Plan.PlanID, p.ID, p.Plan.PlanVersion, p.Plan.PlanDigest, p.Plan.BaseRevision,
		p.Plan.DesiredStateVersion, plan, p.Plan.GeneratedAt, p.Plan.ExpiresAt); err != nil {
		return fmt.Errorf("insert plan: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, p.TenantID, "tenant_provisioning.planned", p.Plan.TenantProvisioningID, map[string]any{
		"tenant_onboarding_request_id": p.Desired.Provenance.TenantOnboardingRequestID, "plan_id": p.Plan.PlanID,
		"plan_digest": p.Plan.PlanDigest, "state": state, "blockers": len(p.Plan.Blockers)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const convergedColumns = `tp.tenant_provisioning_id::text, tp.tenant_provisioning_key, tp.tenant_id, tp.state,
	COALESCE(tp.created_by, ''), tp.readiness_status, tp.tenant_onboarding_request_id, tp.admission_decision_id,
	tp.desired_state_version, ds.desired_state_digest, tp.blocking_reasons, tp.version, tp.created_at, tp.updated_at,
	pl.document, ap.approval_id, ap.plan_id, ap.plan_version, ap.plan_digest, ap.decision, ap.reason, ap.decided_by,
	ap.decided_at, ap.correlation_id::text,
	(SELECT o.operation_id FROM operations.execution_operation o
		WHERE o.subject_type = 'TENANT_PROVISIONING' AND o.subject_id = tp.tenant_provisioning_key
		ORDER BY o.created_at DESC LIMIT 1),
	tp.status, COALESCE(tp.blocked_reason, ''),
	EXISTS (SELECT 1 FROM operations.execution_operation o
		WHERE o.subject_type = 'TENANT_PROVISIONING' AND o.subject_id = tp.tenant_provisioning_key AND o.status IN (` + activeOperationStatuses + `))`

// activeOperationStatuses are the statuses of an operation still in
// progress; any other is finished, or waiting to be retried.
const activeOperationStatuses = `'QUEUED', 'PREPARING', 'RUNNING', 'WAITING', 'VERIFYING', 'COMPENSATING', 'CANCEL_REQUESTED'`

const convergedFrom = `
	FROM provisioning.tenant_provisioning tp
	JOIN provisioning.desired_state ds
		ON ds.tenant_provisioning_id = tp.tenant_provisioning_id AND ds.desired_state_version = tp.desired_state_version
	LEFT JOIN provisioning.plan pl ON pl.tenant_provisioning_id = tp.tenant_provisioning_id AND pl.superseded_at IS NULL
	LEFT JOIN provisioning.plan_approval ap ON ap.plan_id = pl.plan_id`

func scanConverged(row pgx.Row) (ConvergedProvisioning, error) {
	var c ConvergedProvisioning
	var legacy, plan []byte
	var approvalID, planID, planDigest, decision, reason, decidedBy, correlation, operationID *string
	var planVersion *int
	var decidedAt *time.Time
	err := row.Scan(&c.ID, &c.Key, &c.TenantID, &c.State, &c.CreatedBy, &c.ReadinessStatus, &c.TenantOnboardingRequestID,
		&c.AdmissionDecisionID, &c.DesiredStateVersion, &c.DesiredStateDigest, &legacy, &c.Revision, &c.CreatedAt, &c.UpdatedAt,
		&plan, &approvalID, &planID, &planVersion, &planDigest, &decision, &reason, &decidedBy, &decidedAt, &correlation, &operationID,
		&c.LegacyStatus, &c.BlockedReason, &c.OperationActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConvergedProvisioning{}, ErrProvisioningNotFound
	}
	if err != nil {
		return ConvergedProvisioning{}, err
	}
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	if err := json.Unmarshal(legacy, &c.LegacyBlockingReasons); err != nil {
		return ConvergedProvisioning{}, fmt.Errorf("unmarshal blocking reasons: %w", err)
	}
	if plan != nil {
		c.Plan = &convergence.Plan{}
		if err := json.Unmarshal(plan, c.Plan); err != nil {
			return ConvergedProvisioning{}, fmt.Errorf("unmarshal plan: %w", err)
		}
	}
	if approvalID != nil {
		c.Decision = &PlanDecision{ApprovalID: *approvalID, PlanID: *planID, PlanVersion: *planVersion, PlanDigest: *planDigest,
			Decision: *decision, Reason: deref(reason), DecidedBy: *decidedBy, DecidedAt: decidedAt.UTC(), CorrelationID: deref(correlation)}
	}
	c.OperationID = deref(operationID)
	return c, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// GetConvergedProvisioning reads a provisioning planned from desired state.
// A legacy manifest run is not one, and is ErrProvisioningNotFound.
func (r *PostgresRepository) GetConvergedProvisioning(ctx context.Context, id string) (ConvergedProvisioning, error) {
	return scanConverged(r.pool.QueryRow(ctx, `SELECT `+convergedColumns+convergedFrom+`
		WHERE tp.tenant_provisioning_id = $1::uuid`, id))
}

func (r *PostgresRepository) GetConvergedProvisioningByIdempotencyKey(ctx context.Context, tenantID, key string) (ConvergedProvisioning, error) {
	return scanConverged(r.pool.QueryRow(ctx, `SELECT `+convergedColumns+convergedFrom+`
		WHERE tp.tenant_id = $1 AND tp.idempotency_key = $2`, tenantID, key))
}

// ListConvergedProvisionings pages a tenant's provisionings, newest first,
// after the provisioning id given (keyset pagination).
func (r *PostgresRepository) ListConvergedProvisionings(ctx context.Context, tenantID string, limit int, after string) ([]ConvergedProvisioning, error) {
	args := []any{tenantID, limit}
	where := `WHERE tp.tenant_id = $1`
	if after != "" {
		where += ` AND (tp.created_at, tp.tenant_provisioning_id) < (SELECT created_at, tenant_provisioning_id
			FROM provisioning.tenant_provisioning WHERE tenant_provisioning_id = $3::uuid)`
		args = append(args, after)
	}
	rows, err := r.pool.Query(ctx, `SELECT `+convergedColumns+convergedFrom+` `+where+`
		ORDER BY tp.created_at DESC, tp.tenant_provisioning_id DESC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConvergedProvisioning
	for rows.Next() {
		c, err := scanConverged(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetDesiredState(ctx context.Context, id string, version int64) (convergence.DesiredState, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT document FROM provisioning.desired_state
		WHERE tenant_provisioning_id = $1::uuid AND desired_state_version = $2`, id, version).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return convergence.DesiredState{}, ErrProvisioningNotFound
	}
	if err != nil {
		return convergence.DesiredState{}, err
	}
	var d convergence.DesiredState
	return d, json.Unmarshal(raw, &d)
}

// lockRevision locks the provisioning and checks the caller's revision.
func lockRevision(ctx context.Context, tx pgx.Tx, id string, expected int64) (tenantID, key string, err error) {
	var version int64
	err = tx.QueryRow(ctx, `SELECT version, tenant_id, tenant_provisioning_key FROM provisioning.tenant_provisioning
		WHERE tenant_provisioning_id = $1::uuid FOR UPDATE`, id).Scan(&version, &tenantID, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrProvisioningNotFound
	}
	if err != nil {
		return "", "", err
	}
	if version != expected {
		return "", "", ErrTenantProvisioningVersionConflict
	}
	return tenantID, key, nil
}

func bumpRevision(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE provisioning.tenant_provisioning SET version = version + 1, updated_at = now()
		WHERE tenant_provisioning_id = $1::uuid`, id)
	return err
}

// RecordPlanDecision records the decision on the current plan, under the
// caller's revision.
func (r *PostgresRepository) RecordPlanDecision(ctx context.Context, id string, expectedRevision int64, d PlanDecision, actor AuditActor) error {
	if err := validateActor(actor); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tenantID, key, err := lockRevision(ctx, tx, id, expectedRevision)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provisioning.plan_approval (approval_id, tenant_provisioning_id, plan_id, plan_version, plan_digest,
			decision, reason, decided_by, decided_at, correlation_id)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10::uuid)`,
		d.ApprovalID, id, d.PlanID, d.PlanVersion, d.PlanDigest, d.Decision, d.Reason, d.DecidedBy, d.DecidedAt, d.CorrelationID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "plan_approval_plan_uq" {
			return ErrPlanAlreadyDecided
		}
		return fmt.Errorf("insert plan decision: %w", err)
	}
	if err := bumpRevision(ctx, tx, id); err != nil {
		return err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, tenantID, "tenant_provisioning.plan_decided", key, map[string]any{
		"approval_id": d.ApprovalID, "plan_id": d.PlanID, "plan_version": d.PlanVersion, "plan_digest": d.PlanDigest,
		"decision": d.Decision}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AcceptApply queues the operation applying the approved plan. A replay of
// the same idempotency key returns the operation it created (created is
// false); another operation still in progress on the provisioning refuses it.
func (r *PostgresRepository) AcceptApply(ctx context.Context, id string, expectedRevision int64, op operations.Operation, key, requestHash string, actor AuditActor) (operations.Operation, bool, error) {
	return r.acceptOperation(ctx, id, expectedRevision, op, key, requestHash, "", "tenant_provisioning.apply_accepted", actor)
}

// AcceptRemediation queues the remediation of a BLOCKED provisioning, which
// is REMEDIATING until it runs.
func (r *PostgresRepository) AcceptRemediation(ctx context.Context, id string, expectedRevision int64, op operations.Operation, key, requestHash string, actor AuditActor) (operations.Operation, bool, error) {
	return r.acceptOperation(ctx, id, expectedRevision, op, key, requestHash, "REMEDIATING", "tenant_provisioning.remediation_accepted", actor)
}

func (r *PostgresRepository) acceptOperation(ctx context.Context, id string, expectedRevision int64, op operations.Operation, key, requestHash, state, action string, actor AuditActor) (operations.Operation, bool, error) {
	out, created, err := r.acceptApply(ctx, id, expectedRevision, op, key, requestHash, state, action, actor)
	if errors.Is(err, errOperationKeyRace) {
		// A concurrent request with the same key won; this one is its replay.
		return r.acceptApply(ctx, id, expectedRevision, op, key, requestHash, state, action, actor)
	}
	return out, created, err
}

var errOperationKeyRace = errors.New("concurrent operation with the same idempotency key")

func (r *PostgresRepository) acceptApply(ctx context.Context, id string, expectedRevision int64, op operations.Operation, key, requestHash, state, action string, actor AuditActor) (operations.Operation, bool, error) {
	if err := validateActor(actor); err != nil {
		return operations.Operation{}, false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return operations.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	existing, err := scanOperation(tx.QueryRow(ctx, `SELECT `+operationColumns+` FROM operations.execution_operation
		WHERE requested_by = $1 AND operation_type = $2 AND idempotency_key = $3`, op.RequestedBy, op.Type, key))
	switch {
	case err == nil:
		var hash string
		if err := tx.QueryRow(ctx, `SELECT request_hash FROM operations.execution_operation WHERE operation_id = $1`, existing.ID).Scan(&hash); err != nil {
			return operations.Operation{}, false, err
		}
		if hash != requestHash || existing.SubjectID != op.SubjectID {
			return operations.Operation{}, false, ErrOperationKeyReused
		}
		return existing, false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return operations.Operation{}, false, err
	}
	tenantID, _, err := lockRevision(ctx, tx, id, expectedRevision)
	if err != nil {
		return operations.Operation{}, false, err
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM operations.execution_operation
		WHERE subject_type = 'TENANT_PROVISIONING' AND subject_id = $1
			AND status IN (`+activeOperationStatuses+`))`, op.SubjectID).Scan(&active); err != nil {
		return operations.Operation{}, false, err
	}
	if active {
		return operations.Operation{}, false, ErrOperationInProgress
	}
	created, err := scanOperation(tx.QueryRow(ctx, `
		INSERT INTO operations.execution_operation (operation_id, operation_type, status, subject_type, subject_id, tenant_id,
			plan_id, plan_digest, approval_id, requested_by, idempotency_key, request_hash, current_phase, retryable, correlation_id)
		VALUES ($1, $2, 'QUEUED', 'TENANT_PROVISIONING', $3, $4, $5, $6, $7, $8, $9, $10, $11, false, NULLIF($12, '')::uuid)
		RETURNING `+operationColumns,
		op.ID, op.Type, op.SubjectID, tenantID, op.PlanID, op.PlanDigest, op.ApprovalID, op.RequestedBy, key, requestHash,
		op.CurrentPhase, op.CorrelationID))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "execution_operation_idempotency_uq" {
			return operations.Operation{}, false, errOperationKeyRace
		}
		return operations.Operation{}, false, fmt.Errorf("insert operation: %w", err)
	}
	// A new execution supersedes any earlier one's outcome.
	if _, err := tx.Exec(ctx, `UPDATE provisioning.tenant_provisioning SET blocked_reason = NULL,
		state = COALESCE(NULLIF($2, ''), state) WHERE tenant_provisioning_id = $1::uuid`, id, state); err != nil {
		return operations.Operation{}, false, err
	}
	if err := bumpRevision(ctx, tx, id); err != nil {
		return operations.Operation{}, false, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, tenantID, action, op.SubjectID, map[string]any{
		"operation_id": op.ID, "plan_id": op.PlanID, "plan_digest": op.PlanDigest, "approval_id": op.ApprovalID}); err != nil {
		return operations.Operation{}, false, err
	}
	return created, true, tx.Commit(ctx)
}

// insertProvisioningAudit writes the audit record of a provisioning change
// on tx, so it commits with the change.
func insertProvisioningAudit(ctx context.Context, tx pgx.Tx, actor AuditActor, tenantID, action, target string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal audit payload for %s: %w", action, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, actor_id, actor_type, client_id, token_id, correlation_id, action, target, result, payload)
		VALUES ($1, $2, $3, $4, $5, $6::uuid, $7, $8, 'accepted', $9::jsonb)`,
		nullable(tenantID), actor.ActorID, actor.ActorType, nullable(actor.ClientID), nullable(actor.TokenID),
		actor.CorrelationID, action, target, raw); err != nil {
		return fmt.Errorf("audit %s: %w", action, err)
	}
	return nil
}

// ProvisioningUUID returns the row id behind a tp_ identifier.
func ProvisioningUUID(key string) (string, error) {
	return domain.ParseResourceID("tp", key)
}

// SaveExecutionManifest persists the manifest the approved plan executes
// as. A resumed execution of the same plan keeps the first one rather than
// re-deriving it; a replanned plan's execution replaces it.
func (r *PostgresRepository) SaveExecutionManifest(ctx context.Context, m provisioningdomain.TenantManifestRecord) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO provisioning.tenant_manifest (tenant_provisioning_id, tenant_id, schema_version, manifest_hash,
			desired_state_version, source, raw_manifest, resolved_manifest)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_provisioning_id) DO UPDATE SET schema_version = EXCLUDED.schema_version,
			manifest_hash = EXCLUDED.manifest_hash, desired_state_version = EXCLUDED.desired_state_version,
			source = EXCLUDED.source, raw_manifest = EXCLUDED.raw_manifest, resolved_manifest = EXCLUDED.resolved_manifest
		WHERE provisioning.tenant_manifest.source <> EXCLUDED.source`,
		m.TenantProvisioningID, m.TenantID, m.SchemaVersion, m.ManifestHash, m.DesiredStateVersion, m.Source,
		m.RawManifest, m.ResolvedManifest)
	return err
}

// MarkProvisioningBlocked records that execution stopped at a known,
// remediable condition, and why when no finding says (reason may be empty).
// The legacy status is left as the orchestrator set it.
func (r *PostgresRepository) MarkProvisioningBlocked(ctx context.Context, id, reason string) error {
	_, err := r.pool.Exec(ctx, `UPDATE provisioning.tenant_provisioning SET state = 'BLOCKED', blocked_reason = NULLIF($2, ''),
		version = version + 1, updated_at = now() WHERE tenant_provisioning_id = $1::uuid`, id, reason)
	return err
}

// MarkExecutionFailed leaves a provisioning whose execution failed
// recoverable: FAILED, to be retried, when the failure is retryable, and
// otherwise BLOCKED with the failure's code, to be replanned or withdrawn. It
// changes only a provisioning still APPLYING or REMEDIATING: one the pipeline
// already recorded as FAILED keeps that.
func (r *PostgresRepository) MarkExecutionFailed(ctx context.Context, id, code string, retryable bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE provisioning.tenant_provisioning
		SET state = CASE WHEN $3 THEN 'FAILED' ELSE 'BLOCKED' END,
			blocked_reason = CASE WHEN $3 THEN NULL ELSE NULLIF($2, '') END,
			version = version + 1, updated_at = now()
		WHERE tenant_provisioning_id = $1::uuid AND state IN ('APPLYING', 'REMEDIATING')`, id, code, retryable)
	return err
}

// ClaimExecution claims the next runnable provisioning apply or
// remediation.
func (r *PostgresRepository) ClaimExecution(ctx context.Context, lease time.Duration) (operations.Operation, bool, error) {
	return r.ClaimOperation(ctx, lease, operations.TypeTenantProvisioningApply, operations.TypeTenantProvisioningRemediate)
}

// LoadExecuted reads the provisioning an operation applies, by its tp_ id.
func (r *PostgresRepository) LoadExecuted(ctx context.Context, key string) (convergence.ExecutedProvisioning, error) {
	id, err := ProvisioningUUID(key)
	if err != nil {
		return convergence.ExecutedProvisioning{}, ErrProvisioningNotFound
	}
	c, err := r.GetConvergedProvisioning(ctx, id)
	if err != nil {
		return convergence.ExecutedProvisioning{}, err
	}
	desired, err := r.GetDesiredState(ctx, c.ID, c.DesiredStateVersion)
	if err != nil {
		return convergence.ExecutedProvisioning{}, err
	}
	out := convergence.ExecutedProvisioning{ID: c.ID, Key: c.Key, TenantID: c.TenantID, Plan: c.Plan, Desired: &desired,
		LegacyStatus: c.LegacyStatus}
	if c.Decision != nil {
		out.ApprovalID, out.ApprovedDigest = c.Decision.ApprovalID, c.Decision.PlanDigest
		out.Approved = c.Decision.Decision == "APPROVED"
	}
	return out, nil
}

// LegalEntityOf returns the tenant's legal entity.
func (r *PostgresRepository) LegalEntityOf(ctx context.Context, tenantID string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `SELECT legal_entity_id FROM tenants WHERE tenant_id = $1`, tenantID).Scan(&id)
	return id, err
}
