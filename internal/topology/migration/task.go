package migration

import "time"

// Engine migration task statuses, roles and directions
// (engine-migration-task.schema.json).
const (
	TaskPending   = "PENDING"
	TaskClaimed   = "CLAIMED"
	TaskSucceeded = "SUCCEEDED"
	TaskFailed    = "FAILED"
	TaskCancelled = "CANCELLED"

	RoleSource = "SOURCE"
	RoleTarget = "TARGET"

	DirectionForward = "FORWARD"
	DirectionReverse = "REVERSE"
)

// Task failure codes (provider_migration_task_failure).
const (
	FailureReconciliationMismatch = "MIGRATION_RECONCILIATION_MISMATCH"
	FailureTaskTimeout            = "MIGRATION_TASK_TIMEOUT"
)

// TaskCounterpart is the side a MIGRATE_COHORT_DATA task moves state from,
// by topology identifiers only.
type TaskCounterpart struct {
	ProviderKey       string   `json:"provider_key"`
	EngineInstanceIDs []string `json:"engine_instance_ids"`
}

// TaskContexts are a cohort's contexts by opaque identifiers only.
type TaskContexts struct {
	TenantIDs []string `json:"tenant_ids"`
	EstateIDs []string `json:"estate_ids,omitempty"`
}

// TaskResult is what the engine reported. Never business data.
type TaskResult struct {
	RecordCount   *int64 `json:"record_count,omitempty"`
	ContentDigest string `json:"content_digest,omitempty"`
	ReasonCode    string `json:"reason_code,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

// Task is EngineMigrationTask.
type Task struct {
	TaskID              string           `json:"task_id"`
	ProviderMigrationID string           `json:"provider_migration_id"`
	OperationID         string           `json:"operation_id"`
	StepID              string           `json:"step_id"`
	Operation           string           `json:"operation"`
	Role                string           `json:"role"`
	Direction           string           `json:"direction"`
	EngineInstanceID    string           `json:"engine_instance_id"`
	ProviderKey         string           `json:"provider_key"`
	CohortKey           string           `json:"cohort_key"`
	Capabilities        []string         `json:"capabilities"`
	Contexts            TaskContexts     `json:"contexts"`
	Counterpart         *TaskCounterpart `json:"counterpart,omitempty"`
	Status              string           `json:"status"`
	Attempt             int              `json:"attempt"`
	ClaimedBy           string           `json:"claimed_by,omitempty"`
	LeaseExpiresAt      *time.Time       `json:"lease_expires_at,omitempty"`
	DeadlineAt          time.Time        `json:"deadline_at"`
	Result              *TaskResult      `json:"result,omitempty"`
	ReportedAt          *time.Time       `json:"reported_at,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
	Revision            int64            `json:"revision"`
}

// TaskReport is EngineMigrationTaskReport.
type TaskReport struct {
	Outcome       string `json:"outcome"`
	RecordCount   *int64 `json:"record_count,omitempty"`
	ContentDigest string `json:"content_digest,omitempty"`
	ReasonCode    string `json:"reason_code,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

// Roles is who performs an engine operation in a direction (engine_steps).
func Roles(op, direction string) []string {
	step := Lifecycle().EngineSteps[op]
	if direction == DirectionReverse {
		return step.Reverse
	}
	return step.Forward
}
