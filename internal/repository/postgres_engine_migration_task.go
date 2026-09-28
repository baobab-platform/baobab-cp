package repository

// ADR-SHARED-016 sections 3-4: engine migration tasks. The stateful steps
// of a STATEFUL_CUTOVER cohort are assigned to exactly one engine instance
// each, in a role; the instance's attested workload claims a task under a
// lease and reports its outcome, and each report resumes the advance that
// waits on it. A task names contexts by opaque identifiers only and never
// carries credentials, endpoints or business data.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

var (
	ErrEngineMigrationTaskNotFound      = errors.New("engine migration task not found")
	ErrEngineMigrationTaskRevision      = errors.New("engine migration task revision mismatch")
	ErrEngineMigrationTaskClaimed       = errors.New("the task is claimed under another live lease")
	ErrEngineMigrationTaskFinal         = errors.New("the task is final")
	ErrEngineMigrationTaskLeaseExpired  = errors.New("only the claimant reports, while its lease holds")
	ErrEngineMigrationTaskReportInvalid = errors.New("the report does not satisfy the task's step")
	engineMigrationTaskPageSize         = 100
	taskFailureCodesOnce                sync.Once
	taskFailureCodes                    map[string]bool
)

// EngineMigrationTaskRepository is the workload side of engine migration
// tasks. clientID is the verified workload client; the instances it may
// act for are those whose registration names it as their attested
// workload, never an instance the request names.
type EngineMigrationTaskRepository interface {
	ListEngineMigrationTasks(ctx context.Context, clientID, pageToken string, now time.Time) ([]migration.Task, string, error)
	ClaimEngineMigrationTask(ctx context.Context, taskID string, expectedRevision int64, clientID string, policy *health.Policy,
		now time.Time, actor AuditActor) (migration.Task, error)
	ReportEngineMigrationTask(ctx context.Context, taskID string, expectedRevision int64, clientID string, report migration.TaskReport,
		reportHash string, policy *health.Policy, now time.Time, actor AuditActor) (migration.Task, error)
}

var _ EngineMigrationTaskRepository = (*PostgresRepository)(nil)

// registeredTaskFailure reports whether code is a provider_migration_task_failure
// reason code in the pinned Shared registry.
func registeredTaskFailure(code string) bool {
	taskFailureCodesOnce.Do(func() {
		taskFailureCodes = map[string]bool{}
		raw, err := contracts.ReadEmbedded("authorization/v1/reason-code-registry.yaml")
		if err != nil {
			return
		}
		var doc struct {
			ReasonCodes []struct {
				Code     string `yaml:"code"`
				Category string `yaml:"category"`
			} `yaml:"reason_codes"`
		}
		if yaml.Unmarshal(raw, &doc) != nil {
			return
		}
		for _, c := range doc.ReasonCodes {
			if c.Category == "provider_migration_task_failure" {
				taskFailureCodes[c.Code] = true
			}
		}
	})
	return taskFailureCodes[code]
}

const engineMigrationTaskColumns = `t.task_id, t.provider_migration_id, t.operation_id, t.step_id, t.operation, t.role, t.direction,
	ei.engine_instance_key, t.provider_key, t.cohort_key, t.capabilities, t.contexts, t.counterpart, t.status, t.attempt,
	COALESCE(t.claimed_by, ''), t.lease_expires_at, t.deadline_at, t.result, t.reported_at, t.created_at, t.updated_at, t.revision`

const engineMigrationTaskFrom = ` FROM topology.engine_migration_task t JOIN topology.engine_instance ei ON ei.engine_instance_id = t.engine_instance_id`

func scanEngineMigrationTask(row pgx.Row, extra ...any) (migration.Task, error) {
	var t migration.Task
	var contexts, counterpart, result []byte
	dest := append([]any{&t.TaskID, &t.ProviderMigrationID, &t.OperationID, &t.StepID, &t.Operation, &t.Role, &t.Direction,
		&t.EngineInstanceID, &t.ProviderKey, &t.CohortKey, &t.Capabilities, &contexts, &counterpart, &t.Status, &t.Attempt,
		&t.ClaimedBy, &t.LeaseExpiresAt, &t.DeadlineAt, &result, &t.ReportedAt, &t.CreatedAt, &t.UpdatedAt, &t.Revision}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return t, ErrEngineMigrationTaskNotFound
		}
		return t, err
	}
	if err := json.Unmarshal(contexts, &t.Contexts); err != nil {
		return t, err
	}
	if len(counterpart) > 0 {
		t.Counterpart = &migration.TaskCounterpart{}
		if err := json.Unmarshal(counterpart, t.Counterpart); err != nil {
			return t, err
		}
	}
	if len(result) > 0 {
		t.Result = &migration.TaskResult{}
		if err := json.Unmarshal(result, t.Result); err != nil {
			return t, err
		}
	}
	t.DeadlineAt, t.CreatedAt, t.UpdatedAt = t.DeadlineAt.UTC(), t.CreatedAt.UTC(), t.UpdatedAt.UTC()
	for _, p := range []*time.Time{t.LeaseExpiresAt, t.ReportedAt} {
		if p != nil {
			*p = p.UTC()
		}
	}
	if t.Status != migration.TaskClaimed {
		t.ClaimedBy, t.LeaseExpiresAt = "", nil
	}
	return t, nil
}

// engineStep advances one engine step of the run: it issues the step's
// tasks the first time, then reports done once every task succeeded (and,
// for RECONCILE_COHORT_DATA, both sides match), waiting otherwise. A failed
// task fails the step with its reason code.
func (x *migrationExecutor) engineStep(ctx context.Context, step migration.Step) (bool, error) {
	rows, err := x.tx.Query(ctx, `SELECT `+engineMigrationTaskColumns+engineMigrationTaskFrom+`
		WHERE t.operation_id = $1 AND t.step_id = $2 ORDER BY t.task_id`, x.operationID, step.StepID)
	if err != nil {
		return false, err
	}
	tasks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (migration.Task, error) { return scanEngineMigrationTask(row) })
	if err != nil {
		return false, err
	}
	if len(tasks) == 0 {
		issued, err := x.issueTasks(ctx, step)
		return issued == 0, err
	}
	done := true
	for _, t := range tasks {
		switch t.Status {
		case migration.TaskFailed:
			code, detail := migration.FailureTaskTimeout, "the task failed"
			if t.Result != nil {
				code, detail = t.Result.ReasonCode, t.Result.Detail
			}
			if detail == "" {
				detail = fmt.Sprintf("%s on %s failed", t.Operation, t.EngineInstanceID)
			}
			return false, stepFailure(code, "%s", detail)
		case migration.TaskPending, migration.TaskClaimed:
			done = false
		}
	}
	if done && step.Operation == migration.OpReconcileCohortData {
		if err := reconciled(tasks); err != nil {
			return false, err
		}
	}
	return done, nil
}

// reconciled compares the SOURCE and TARGET reports of a reconciliation:
// equal total record counts always, and equal digests where each side
// served the cohort from one instance (ADR-SHARED-016 section 3).
func reconciled(tasks []migration.Task) error {
	var counts [2]int64
	var digests [2][]string
	for _, t := range tasks {
		if t.Status != migration.TaskSucceeded || t.Result == nil || t.Result.RecordCount == nil {
			continue
		}
		side := 0
		if t.Role == migration.RoleTarget {
			side = 1
		}
		counts[side] += *t.Result.RecordCount
		digests[side] = append(digests[side], t.Result.ContentDigest)
	}
	if counts[0] != counts[1] {
		return stepFailure(migration.FailureReconciliationMismatch, "the source reported %d record(s), the target %d", counts[0], counts[1])
	}
	if len(digests[0]) == 1 && len(digests[1]) == 1 && digests[0][0] != digests[1][0] {
		return stepFailure(migration.FailureReconciliationMismatch, "the source and target content digests differ")
	}
	return nil
}

// issueTasks creates the step's tasks, one per assigned role and engine
// instance of the cohort, and returns how many. A cohort with no bindings
// has nothing to move and issues none.
func (x *migrationExecutor) issueTasks(ctx context.Context, step migration.Step) (int, error) {
	rows, err := x.tx.Query(ctx, `
		SELECT l.capability_key, src.engine_instance_id::text, srcei.engine_instance_key, l.target_engine_instance_id::text,
			tgtei.engine_instance_key, s.tenant_id, COALESCE(s.digital_estate_id, '')
		FROM topology.provider_migration_binding l
		JOIN capability.capability_binding src ON src.id = l.source_binding_id
		JOIN topology.engine_instance srcei ON srcei.engine_instance_id = src.engine_instance_id
		JOIN topology.engine_instance tgtei ON tgtei.engine_instance_id = l.target_engine_instance_id
		JOIN capability.capability_scope s ON s.scope_id = src.scope_id
		WHERE l.provider_migration_id = $1 AND l.cohort_key = $2`, x.m.ProviderMigrationID, step.Resources.CohortKey)
	if err != nil {
		return 0, err
	}
	type member struct{ capability, source, sourceKey, target, targetKey, tenant, estate string }
	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (member, error) {
		var m member
		err := row.Scan(&m.capability, &m.source, &m.sourceKey, &m.target, &m.targetKey, &m.tenant, &m.estate)
		return m, err
	})
	if err != nil || len(members) == 0 {
		return 0, err
	}
	instances := map[string]map[string]string{migration.RoleSource: {}, migration.RoleTarget: {}} // role -> row id -> key
	capabilities, tenants, estates := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, m := range members {
		instances[migration.RoleSource][m.source] = m.sourceKey
		instances[migration.RoleTarget][m.target] = m.targetKey
		capabilities[m.capability], tenants[m.tenant] = true, true
		if m.estate != "" {
			estates[m.estate] = true
		}
	}
	providers := map[string]string{migration.RoleSource: x.plan.SourceProviderKey, migration.RoleTarget: x.plan.TargetProviderKey}
	contexts, err := json.Marshal(migration.TaskContexts{TenantIDs: sortedSet(tenants), EstateIDs: sortedSet(estates)})
	if err != nil {
		return 0, err
	}
	deadline := x.now.Add(time.Duration(migration.Lifecycle().TaskDefaultDeadline) * time.Hour)
	if w := x.plan.Request.CutoverWindow; w != nil && w.EndsAt.After(x.now) {
		deadline = w.EndsAt
	}
	issued := 0
	for _, role := range migration.Roles(step.Operation, x.direction) {
		var counterpart []byte
		if step.Operation == migration.OpMigrateCohortData {
			other := migration.RoleSource
			if role == migration.RoleSource {
				other = migration.RoleTarget
			}
			keys := make([]string, 0, len(instances[other]))
			for _, key := range instances[other] {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if counterpart, err = json.Marshal(migration.TaskCounterpart{ProviderKey: providers[other], EngineInstanceIDs: keys}); err != nil {
				return issued, err
			}
		}
		ids := make([]string, 0, len(instances[role]))
		for id := range instances[role] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, instance := range ids {
			if _, err := x.tx.Exec(ctx, `INSERT INTO topology.engine_migration_task (task_id, provider_migration_id, operation_id, step_id,
				operation, role, direction, engine_instance_id, provider_key, cohort_key, capabilities, contexts, counterpart, status,
				deadline_at, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8::uuid, $9, $10, $11, $12::jsonb, $13::jsonb, 'PENDING', $14, $15, $15)`,
				domain.NewResourceID("emt"), x.m.ProviderMigrationID, x.operationID, step.StepID, step.Operation, role, x.direction,
				instance, providers[role], step.Resources.CohortKey, sortedSet(capabilities), contexts, nullJSON(counterpart),
				deadline, x.now); err != nil {
				return issued, fmt.Errorf("issue engine migration task: %w", err)
			}
			issued++
		}
	}
	return issued, nil
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// expireMigrationTasks fails the migration's open tasks whose deadline has
// passed with MIGRATION_TASK_TIMEOUT. The cohort stays in its safe state.
func expireMigrationTasks(ctx context.Context, tx pgx.Tx, migrationID string, now time.Time) error {
	result, _ := json.Marshal(migration.TaskResult{ReasonCode: migration.FailureTaskTimeout,
		Detail: "The assigned engine instance did not report the task before its deadline."})
	_, err := tx.Exec(ctx, `UPDATE topology.engine_migration_task SET status = 'FAILED', result = $3::jsonb, reported_at = $2,
		updated_at = $2, revision = revision + 1
		WHERE provider_migration_id = $1 AND status IN ('PENDING', 'CLAIMED') AND deadline_at <= $2`, migrationID, now, result)
	return err
}

// cancelOpenTasks cancels a failed operation's tasks still open.
func cancelOpenTasks(ctx context.Context, tx pgx.Tx, operationID string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE topology.engine_migration_task SET status = 'CANCELLED', updated_at = $2, revision = revision + 1
		WHERE operation_id = $1 AND status IN ('PENDING', 'CLAIMED')`, operationID, now)
	return err
}

// ListEngineMigrationTasks lists the open tasks of the instances whose
// registration names clientID as their attested workload, by task id. A
// lapsed lease returns its task to PENDING for the next attempt; a task
// past its deadline is no longer offered.
func (r *PostgresRepository) ListEngineMigrationTasks(ctx context.Context, clientID, pageToken string, now time.Time) ([]migration.Task, string, error) {
	if clientID == "" {
		return []migration.Task{}, "", nil
	}
	if _, err := r.pool.Exec(ctx, `UPDATE topology.engine_migration_task t SET status = 'PENDING', claimed_by = NULL,
		lease_expires_at = NULL, attempt = attempt + 1, updated_at = $2, revision = revision + 1
		FROM topology.engine_instance ei
		WHERE ei.engine_instance_id = t.engine_instance_id AND ei.workload_client_id = $1
			AND t.status = 'CLAIMED' AND t.lease_expires_at <= $2`, clientID, now); err != nil {
		return nil, "", fmt.Errorf("release lapsed leases: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT `+engineMigrationTaskColumns+engineMigrationTaskFrom+`
		WHERE ei.workload_client_id = $1 AND t.status IN ('PENDING', 'CLAIMED') AND t.deadline_at > $2 AND t.task_id > $3
		ORDER BY t.task_id LIMIT $4`, clientID, now, pageToken, engineMigrationTaskPageSize+1)
	if err != nil {
		return nil, "", err
	}
	tasks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (migration.Task, error) { return scanEngineMigrationTask(row) })
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(tasks) > engineMigrationTaskPageSize {
		tasks = tasks[:engineMigrationTaskPageSize]
		next = tasks[len(tasks)-1].TaskID
	}
	if tasks == nil {
		tasks = []migration.Task{}
	}
	return tasks, next, nil
}

// lockTaskForClient locks the migration a task belongs to, then the task,
// for the workload attested for its instance. Another instance's task does
// not exist for the caller.
func lockTaskForClient(ctx context.Context, tx pgx.Tx, taskID, clientID string) (migration.Migration, migration.Task, string, error) {
	var migrationID string
	err := tx.QueryRow(ctx, `SELECT t.provider_migration_id FROM topology.engine_migration_task t
		JOIN topology.engine_instance ei ON ei.engine_instance_id = t.engine_instance_id
		WHERE t.task_id = $1 AND ei.workload_client_id = $2`, taskID, clientID).Scan(&migrationID)
	if errors.Is(err, pgx.ErrNoRows) || clientID == "" {
		return migration.Migration{}, migration.Task{}, "", ErrEngineMigrationTaskNotFound
	}
	if err != nil {
		return migration.Migration{}, migration.Task{}, "", err
	}
	m, err := scanProviderMigration(tx.QueryRow(ctx, `SELECT `+providerMigrationColumns+`
		FROM topology.provider_migration WHERE provider_migration_id = $1 FOR UPDATE`, migrationID))
	if err != nil {
		return m, migration.Task{}, "", err
	}
	var hash string
	t, err := scanEngineMigrationTask(tx.QueryRow(ctx, `SELECT `+engineMigrationTaskColumns+`, COALESCE(t.report_hash, '')`+
		engineMigrationTaskFrom+` WHERE t.task_id = $1 FOR UPDATE OF t`, taskID), &hash)
	return m, t, hash, err
}

func (r *PostgresRepository) getTask(ctx context.Context, q migrationQuerier, taskID string) (migration.Task, error) {
	return scanEngineMigrationTask(q.QueryRow(ctx, `SELECT `+engineMigrationTaskColumns+engineMigrationTaskFrom+` WHERE t.task_id = $1`, taskID))
}

// expireAndResume fails the migration's overdue tasks and resumes its
// running advance so the failure reaches the operation.
func (r *PostgresRepository) expireAndResume(ctx context.Context, tx pgx.Tx, m migration.Migration, policy *health.Policy,
	now time.Time, actor AuditActor) error {
	if err := expireMigrationTasks(ctx, tx, m.ProviderMigrationID, now); err != nil {
		return err
	}
	_, err := r.resume(ctx, tx, m, policy, now, actor)
	return err
}

// ClaimEngineMigrationTask takes (or renews) the task's lease for its
// instance's attested workload.
func (r *PostgresRepository) ClaimEngineMigrationTask(ctx context.Context, taskID string, expectedRevision int64, clientID string,
	policy *health.Policy, now time.Time, actor AuditActor) (migration.Task, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return migration.Task{}, err
	}
	defer tx.Rollback(ctx)
	m, t, _, err := lockTaskForClient(ctx, tx, taskID, clientID)
	if err != nil {
		return t, err
	}
	if (t.Status == migration.TaskPending || t.Status == migration.TaskClaimed) && !now.Before(t.DeadlineAt) {
		if err := r.expireAndResume(ctx, tx, m, policy, now, actor); err != nil {
			return t, err
		}
		if err := tx.Commit(ctx); err != nil {
			return t, err
		}
		return t, ErrEngineMigrationTaskFinal
	}
	switch {
	case t.Status != migration.TaskPending && t.Status != migration.TaskClaimed:
		return t, ErrEngineMigrationTaskFinal
	case t.Revision != expectedRevision:
		return t, ErrEngineMigrationTaskRevision
	}
	var claimedBy string
	var lease *time.Time
	if err := tx.QueryRow(ctx, `SELECT COALESCE(claimed_by, ''), lease_expires_at FROM topology.engine_migration_task WHERE task_id = $1`,
		taskID).Scan(&claimedBy, &lease); err != nil {
		return t, err
	}
	attempt := t.Attempt
	if t.Status == migration.TaskClaimed && lease != nil && lease.After(now) && claimedBy != clientID {
		return t, ErrEngineMigrationTaskClaimed
	}
	if t.Status == migration.TaskClaimed && (lease == nil || !lease.After(now)) {
		attempt++ // the earlier claimant's lease lapsed: a new attempt
	}
	expires := now.Add(time.Duration(migration.Lifecycle().TaskLeaseSeconds) * time.Second)
	if _, err := tx.Exec(ctx, `UPDATE topology.engine_migration_task SET status = 'CLAIMED', claimed_by = $2, lease_expires_at = $3,
		attempt = $4, updated_at = $5, revision = revision + 1 WHERE task_id = $1`, taskID, clientID, expires, attempt, now); err != nil {
		return t, fmt.Errorf("claim engine migration task: %w", err)
	}
	claimed, err := r.getTask(ctx, tx, taskID)
	if err != nil {
		return claimed, err
	}
	return claimed, tx.Commit(ctx)
}

// ReportEngineMigrationTask records the claimant's final report and
// resumes the advance waiting on it. The same report replayed returns the
// task; a different one is refused.
func (r *PostgresRepository) ReportEngineMigrationTask(ctx context.Context, taskID string, expectedRevision int64, clientID string,
	report migration.TaskReport, reportHash string, policy *health.Policy, now time.Time, actor AuditActor) (migration.Task, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return migration.Task{}, err
	}
	defer tx.Rollback(ctx)
	m, t, priorHash, err := lockTaskForClient(ctx, tx, taskID, clientID)
	if err != nil {
		return t, err
	}
	if t.Status == migration.TaskSucceeded || t.Status == migration.TaskFailed || t.Status == migration.TaskCancelled {
		if priorHash != "" && priorHash == reportHash {
			return t, nil
		}
		return t, ErrEngineMigrationTaskFinal
	}
	if !now.Before(t.DeadlineAt) {
		if err := r.expireAndResume(ctx, tx, m, policy, now, actor); err != nil {
			return t, err
		}
		if err := tx.Commit(ctx); err != nil {
			return t, err
		}
		return t, ErrEngineMigrationTaskFinal
	}
	if t.Revision != expectedRevision {
		return t, ErrEngineMigrationTaskRevision
	}
	var claimedBy string
	var lease *time.Time
	if err := tx.QueryRow(ctx, `SELECT COALESCE(claimed_by, ''), lease_expires_at FROM topology.engine_migration_task WHERE task_id = $1`,
		taskID).Scan(&claimedBy, &lease); err != nil {
		return t, err
	}
	if t.Status != migration.TaskClaimed || claimedBy != clientID || lease == nil || !lease.After(now) {
		return t, ErrEngineMigrationTaskLeaseExpired
	}
	switch {
	case report.Outcome == migration.TaskFailed && !registeredTaskFailure(report.ReasonCode):
		return t, fmt.Errorf("%w: %s is not a provider_migration_task_failure code", ErrEngineMigrationTaskReportInvalid, report.ReasonCode)
	case report.Outcome == migration.TaskSucceeded && report.ReasonCode != "":
		return t, fmt.Errorf("%w: a SUCCEEDED report carries no reason code", ErrEngineMigrationTaskReportInvalid)
	case report.Outcome == migration.TaskSucceeded && t.Operation == migration.OpReconcileCohortData &&
		(report.RecordCount == nil || report.ContentDigest == ""):
		return t, fmt.Errorf("%w: a reconciliation reports its record_count and content_digest", ErrEngineMigrationTaskReportInvalid)
	}
	result, err := json.Marshal(migration.TaskResult{RecordCount: report.RecordCount, ContentDigest: report.ContentDigest,
		ReasonCode: report.ReasonCode, Detail: report.Detail})
	if err != nil {
		return t, err
	}
	if _, err := tx.Exec(ctx, `UPDATE topology.engine_migration_task SET status = $2, result = $3::jsonb, report_hash = $4,
		reported_at = $5, updated_at = $5, revision = revision + 1 WHERE task_id = $1`,
		taskID, report.Outcome, result, reportHash, now); err != nil {
		return t, fmt.Errorf("record engine migration task report: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "engine_migration_task.reported", m.ProviderMigrationID, map[string]any{
		"task_id": taskID, "operation": t.Operation, "role": t.Role, "engine_instance_id": t.EngineInstanceID,
		"outcome": report.Outcome, "reason_code": report.ReasonCode}); err != nil {
		return t, err
	}
	if _, err := r.resume(ctx, tx, m, policy, now, actor); err != nil {
		return t, err
	}
	reported, err := r.getTask(ctx, tx, taskID)
	if err != nil {
		return reported, err
	}
	return reported, tx.Commit(ctx)
}
