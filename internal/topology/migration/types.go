// Package migration plans provider migrations (ADR-BCP-006 sections 44-58,
// 119-122, Gate 8): replacing the provider behind capabilities, cohort by
// cohort, without changing capability keys or canonical contracts.
//
// Planning is side-effect free (ADR-BCP-021 section 20): discovery reads
// authoritative state through Facts and nothing here writes. The types
// mirror Shared control-plane/v1 provider-migration.schema.json field for
// field.
package migration

import "time"

// Stages (provider-migration-lifecycle.yaml).
const (
	StagePlan       = "PLAN"
	StageComplete   = "COMPLETE"
	StageCancelled  = "CANCELLED"
	StageRolledBack = "ROLLED_BACK"
)

// Migration modes and data strategies.
const (
	ModeStatelessRebind = "STATELESS_REBIND"
	ModeStatefulCutover = "STATEFUL_CUTOVER"

	DataNone = "NONE"

	RollbackForwardFixOnly = "FORWARD_FIX_ONLY"
)

// Canonical migration operations (migrationOperation).
const (
	OpVerifyTargetReadiness  = "VERIFY_TARGET_READINESS"
	OpCreateMigrationBinding = "CREATE_MIGRATION_BINDING"
	OpStartShadow            = "START_SHADOW"
	OpStopShadow             = "STOP_SHADOW"
	OpFreezeCohortWrites     = "FREEZE_COHORT_WRITES"
	OpMigrateCohortData      = "MIGRATE_COHORT_DATA"
	OpReconcileCohortData    = "RECONCILE_COHORT_DATA"
	OpShiftCohort            = "SHIFT_COHORT"
	OpUnfreezeCohortWrites   = "UNFREEZE_COHORT_WRITES"
	OpValidateCohort         = "VALIDATE_COHORT"
	OpRetireSourceBinding    = "RETIRE_SOURCE_BINDING"
)

// statefulSequence keeps exactly one authoritative writer per cohort
// (sections 55-56; provider-migration-lifecycle.yaml stateful_cohort_sequence).
var statefulSequence = []string{OpFreezeCohortWrites, OpMigrateCohortData, OpReconcileCohortData, OpShiftCohort, OpUnfreezeCohortWrites}

// Blocking codes (provider_migration_blocker) and warning codes.
const (
	BlockSourceNotBound             = "MIGRATION_SOURCE_NOT_BOUND"
	BlockTargetNotRegistered        = "MIGRATION_TARGET_NOT_REGISTERED"
	BlockTargetNotSupported         = "MIGRATION_TARGET_NOT_SUPPORTED"
	BlockTargetContractIncompatible = "MIGRATION_TARGET_CONTRACT_INCOMPATIBLE"
	BlockTargetUnhealthy            = "MIGRATION_TARGET_UNHEALTHY"
	BlockTargetNotEligible          = "MIGRATION_TARGET_NOT_ELIGIBLE"
	BlockContextUnassigned          = "MIGRATION_CONTEXT_UNASSIGNED"
	BlockCohortOverlap              = "MIGRATION_COHORT_OVERLAP"
	BlockShadowUnsafe               = "MIGRATION_SHADOW_UNSAFE"
	BlockAlreadyInProgress          = "MIGRATION_ALREADY_IN_PROGRESS"

	WarnNotReversible = "MIGRATION_NOT_REVERSIBLE"
	WarnCohortEmpty   = "MIGRATION_COHORT_EMPTY"
)

type Capability struct {
	CapabilityKey   string `json:"capability_key"`
	ContractVersion int    `json:"contract_version"`
}

// Selector is a deterministic cohort: dimensions AND, values OR.
type Selector struct {
	TenantIDs      []string `json:"tenant_ids,omitempty"`
	LegalEntityIDs []string `json:"legal_entity_ids,omitempty"`
	EstateIDs      []string `json:"estate_ids,omitempty"`
	Markets        []string `json:"markets,omitempty"`
	Regions        []string `json:"regions,omitempty"`
}

type Cohort struct {
	CohortKey string    `json:"cohort_key"`
	Selector  *Selector `json:"selector,omitempty"`
}

type CutoverWindow struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type Check struct {
	Check       string `json:"check"`
	Description string `json:"description"`
}

// Request is ProviderMigrationRequest.
type Request struct {
	SourceProviderKey string         `json:"source_provider_key"`
	TargetProviderKey string         `json:"target_provider_key"`
	Capabilities      []Capability   `json:"capabilities"`
	MigrationMode     string         `json:"migration_mode"`
	DataStrategy      string         `json:"data_strategy"`
	RollbackStrategy  string         `json:"rollback_strategy"`
	Shadow            bool           `json:"shadow"`
	Cohorts           []Cohort       `json:"cohorts"`
	ValidationChecks  []Check        `json:"validation_checks"`
	CutoverWindow     *CutoverWindow `json:"cutover_window,omitempty"`
	Owners            []string       `json:"owners"`
	Reason            string         `json:"reason"`
}

type StepResources struct {
	CohortKey        string `json:"cohort_key,omitempty"`
	CapabilityKey    string `json:"capability_key,omitempty"`
	ProviderKey      string `json:"provider_key,omitempty"`
	BindingMode      string `json:"binding_mode,omitempty"`
	EngineInstanceID string `json:"engine_instance_id,omitempty"`
	BindingCount     *int   `json:"binding_count,omitempty"`
}

type Step struct {
	StepID       string        `json:"step_id"`
	Operation    string        `json:"operation"`
	DependsOn    []string      `json:"depends_on"`
	Resources    StepResources `json:"resources"`
	Irreversible bool          `json:"irreversible"`
}

type Finding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	StepID  string `json:"step_id,omitempty"`
}

type ImpactAnalysis struct {
	Summary            string `json:"summary"`
	ResourcesCreated   int    `json:"resources_created"`
	ResourcesChanged   int    `json:"resources_changed"`
	ResourcesRemoved   int    `json:"resources_removed"`
	AvailabilityImpact string `json:"availability_impact,omitempty"`
}

type CohortCount struct {
	CohortKey    string `json:"cohort_key"`
	BindingCount int    `json:"binding_count"`
	TenantCount  int    `json:"tenant_count"`
}

// Discovery is what the migration touches (section 46).
type Discovery struct {
	BindingCount int           `json:"binding_count"`
	TenantIDs    []string      `json:"tenant_ids"`
	EstateIDs    []string      `json:"estate_ids"`
	Markets      []string      `json:"markets"`
	Cohorts      []CohortCount `json:"cohorts"`
}

// Plan is ProviderMigrationPlan. A preview has no ProviderMigrationID.
type Plan struct {
	PlanID              string    `json:"plan_id"`
	PlanVersion         int       `json:"plan_version"`
	PlanDigest          string    `json:"plan_digest"`
	BaseRevision        int64     `json:"base_revision"`
	GeneratedAt         time.Time `json:"generated_at"`
	ExpiresAt           time.Time `json:"expires_at"`
	RiskClass           string    `json:"risk_class"`
	ProviderMigrationID string    `json:"provider_migration_id,omitempty"`
	// Request is the complete intent the plan executes; it is part of the
	// material an approval binds.
	Request               Request        `json:"request"`
	SourceProviderKey     string         `json:"source_provider_key"`
	TargetProviderKey     string         `json:"target_provider_key"`
	MigrationMode         string         `json:"migration_mode"`
	Discovery             Discovery      `json:"discovery"`
	Steps                 []Step         `json:"steps"`
	SecurityChecks        []Check        `json:"security_checks"`
	ReadinessRequirements []Check        `json:"readiness_requirements"`
	Blockers              []Finding      `json:"blockers"`
	Warnings              []Finding      `json:"warnings"`
	ImpactAnalysis        ImpactAnalysis `json:"impact_analysis"`
	VerificationStrategy  string         `json:"verification_strategy"`
	CompensationStrategy  string         `json:"compensation_strategy"`
}

// Migration is the ProviderMigration aggregate.
type Migration struct {
	ProviderMigrationID string     `json:"provider_migration_id"`
	Request             Request    `json:"request"`
	Stage               string     `json:"stage"`
	CurrentCohortKey    string     `json:"current_cohort_key,omitempty"`
	PlanID              string     `json:"plan_id"`
	PlanVersion         int        `json:"plan_version"`
	PlanDigest          string     `json:"plan_digest"`
	Blocked             bool       `json:"blocked"`
	FailureReason       string     `json:"failure_reason,omitempty"`
	CreatedBy           string     `json:"created_by"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	Revision            int64      `json:"revision"`
}

// Normalize gives the request's optional lists their contract default, so
// an omitted list is recorded and returned as [] rather than null.
func (r *Request) Normalize() {
	if r.ValidationChecks == nil {
		r.ValidationChecks = []Check{}
	}
}

// Terminal reports whether a stage ends the migration.
func Terminal(stage string) bool {
	return stage == StageComplete || stage == StageCancelled || stage == StageRolledBack
}
