// Package changeset is the generic governed unit of change (ADR-BCP-021,
// gates CCM-02 and CCM-03): a typed desired change is validated, planned
// side-effect free into an immutable, digest-bound plan, approved by
// someone other than its requester, applied as exactly that plan, and ends
// with an outcome record.
//
// The lifecycle and the change kinds are Shared's
// (control-plane/v1/changeset-lifecycle.yaml), loaded from the embedded
// contracts rather than restated here. The types mirror
// control-plane/v1/changeset.schema.json field for field.
package changeset

import (
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// States (changesetState).
const (
	StateDraft              = "DRAFT"
	StateValidating         = "VALIDATING"
	StateInvalid            = "INVALID"
	StatePlanning           = "PLANNING"
	StateBlocked            = "BLOCKED"
	StatePlanned            = "PLANNED"
	StateAwaitingApproval   = "AWAITING_APPROVAL"
	StateRejected           = "REJECTED"
	StateChangesRequested   = "CHANGES_REQUESTED"
	StateApproved           = "APPROVED"
	StateApplying           = "APPLYING"
	StateFailed             = "FAILED"
	StateVerifying          = "VERIFYING"
	StateVerificationFailed = "VERIFICATION_FAILED"
	StateCompleted          = "COMPLETED"
	StateCancelled          = "CANCELLED"
)

// Change kinds.
const (
	KindTenantSuspension    = "TENANT_SUSPENSION"
	KindTenantReinstatement = "TENANT_REINSTATEMENT"
)

// Blocking codes (changeset_blocker).
const (
	BlockTargetNotFound      = "CHANGESET_TARGET_NOT_FOUND"
	BlockTargetStateConflict = "CHANGESET_TARGET_STATE_CONFLICT"
	BlockTargetLocked        = "CHANGESET_TARGET_LOCKED"
)

// Canonical changeset operations.
const (
	OpSuspendTenant     = "SUSPEND_TENANT"
	OpReinstateTenant   = "REINSTATE_TENANT"
	OpVerifyTenantState = "VERIFY_TENANT_STATE"
)

// DesiredChange is one of the desiredChange kinds.
type DesiredChange struct {
	Kind     string `json:"kind"`
	TenantID string `json:"tenant_id"`
}

// CreateRequest is ChangesetCreateRequest.
type CreateRequest struct {
	Title                 string        `json:"title"`
	Description           string        `json:"description,omitempty"`
	Reason                string        `json:"reason"`
	BusinessJustification string        `json:"business_justification,omitempty"`
	DesiredChange         DesiredChange `json:"desired_change"`
}

// PlanReference names one exact plan.
type PlanReference struct {
	PlanID      string `json:"plan_id"`
	PlanVersion int    `json:"plan_version"`
	PlanDigest  string `json:"plan_digest"`
}

// Finding is planFinding.
type Finding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	StepID  string `json:"step_id,omitempty"`
}

// Changeset is the Changeset aggregate.
type Changeset struct {
	ChangesetID           string               `json:"changeset_id"`
	ChangesetType         string               `json:"changeset_type"`
	Title                 string               `json:"title"`
	Description           string               `json:"description,omitempty"`
	Reason                string               `json:"reason"`
	BusinessJustification string               `json:"business_justification,omitempty"`
	Source                string               `json:"source"`
	RequestedBy           string               `json:"requested_by"`
	RequestedAt           time.Time            `json:"requested_at"`
	TargetScope           administration.Scope `json:"target_scope"`
	BaseRevision          int64                `json:"base_revision"`
	DesiredChange         DesiredChange        `json:"desired_change"`
	RiskClass             string               `json:"risk_class,omitempty"`
	State                 string               `json:"state"`
	CurrentPlan           *PlanReference       `json:"current_plan,omitempty"`
	ApprovalID            string               `json:"approval_id,omitempty"`
	OperationID           string               `json:"operation_id,omitempty"`
	BlockingReasons       []Finding            `json:"blocking_reasons,omitempty"`
	CorrelationID         string               `json:"correlation_id,omitempty"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
	Revision              int64                `json:"revision"`
}

// StepResources is changesetStepResources.
type StepResources struct {
	TenantID   string `json:"tenant_id,omitempty"`
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// Step is changesetStep.
type Step struct {
	StepID       string        `json:"step_id"`
	Operation    string        `json:"operation"`
	DependsOn    []string      `json:"depends_on"`
	Resources    StepResources `json:"resources"`
	Irreversible bool          `json:"irreversible"`
}

// Check is planCheck.
type Check struct {
	Check       string `json:"check"`
	Description string `json:"description"`
}

// ImpactAnalysis is impactAnalysis.
type ImpactAnalysis struct {
	Summary            string `json:"summary"`
	ResourcesCreated   int    `json:"resources_created"`
	ResourcesChanged   int    `json:"resources_changed"`
	ResourcesRemoved   int    `json:"resources_removed"`
	SecurityImpact     string `json:"security_impact,omitempty"`
	AvailabilityImpact string `json:"availability_impact,omitempty"`
}

// Plan is ChangesetPlan.
type Plan struct {
	PlanID                string         `json:"plan_id"`
	PlanVersion           int            `json:"plan_version"`
	PlanDigest            string         `json:"plan_digest"`
	BaseRevision          int64          `json:"base_revision"`
	GeneratedAt           time.Time      `json:"generated_at"`
	ExpiresAt             time.Time      `json:"expires_at"`
	RiskClass             string         `json:"risk_class"`
	ChangesetID           string         `json:"changeset_id"`
	ChangesetType         string         `json:"changeset_type"`
	DesiredChange         DesiredChange  `json:"desired_change"`
	Steps                 []Step         `json:"steps"`
	SecurityChecks        []Check        `json:"security_checks"`
	ReadinessRequirements []Check        `json:"readiness_requirements"`
	Blockers              []Finding      `json:"blockers"`
	Warnings              []Finding      `json:"warnings"`
	ImpactAnalysis        ImpactAnalysis `json:"impact_analysis"`
	VerificationStrategy  string         `json:"verification_strategy"`
	CompensationStrategy  string         `json:"compensation_strategy"`
}

// Reference is the plan's identity and digest.
func (p Plan) Reference() PlanReference {
	return PlanReference{PlanID: p.PlanID, PlanVersion: p.PlanVersion, PlanDigest: p.PlanDigest}
}

// AffectedResource is affectedResource.
type AffectedResource struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Before       string `json:"before,omitempty"`
	After        string `json:"after,omitempty"`
}

// VerificationCheck is one verificationResult check.
type VerificationCheck struct {
	Check  string `json:"check"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// VerificationResult is verificationResult.
type VerificationResult struct {
	Status string              `json:"status"`
	Checks []VerificationCheck `json:"checks"`
}

// Outcome is ChangeOutcome.
type Outcome struct {
	ChangesetID        string              `json:"changeset_id"`
	OperationID        string              `json:"operation_id,omitempty"`
	FinalState         string              `json:"final_state"`
	AppliedPlanDigest  string              `json:"applied_plan_digest,omitempty"`
	StartedAt          *time.Time          `json:"started_at,omitempty"`
	CompletedAt        *time.Time          `json:"completed_at,omitempty"`
	AffectedResources  []AffectedResource  `json:"affected_resources"`
	VerificationResult *VerificationResult `json:"verification_result,omitempty"`
	ResidualRisks      []string            `json:"residual_risks"`
	CorrelationID      string              `json:"correlation_id,omitempty"`
	RecordedAt         time.Time           `json:"recorded_at"`
}

// ErrInvalid wraps a create request the schema accepts but whose intent
// the Control Plane cannot represent (422).
var ErrInvalid = errors.New("invalid changeset")

// Draft derives a DRAFT changeset from a create request: its type from the
// change kind, its scope from the target, and its source, requester and
// base revision from the verified caller and authoritative state.
func Draft(req CreateRequest, id, requester, source, correlationID string, baseRevision int64, now time.Time) (Changeset, error) {
	kind, ok := Kinds()[req.DesiredChange.Kind]
	if !ok {
		return Changeset{}, fmt.Errorf("%w: change kind %q is not supported", ErrInvalid, req.DesiredChange.Kind)
	}
	if !domain.ValidTenantID(req.DesiredChange.TenantID) {
		return Changeset{}, fmt.Errorf("%w: tenant_id is not a Control Plane tenant identifier", ErrInvalid)
	}
	if baseRevision < 1 {
		baseRevision = 1
	}
	return Changeset{ChangesetID: id, ChangesetType: kind.ChangesetType, Title: req.Title, Description: req.Description,
		Reason: req.Reason, BusinessJustification: req.BusinessJustification, Source: source, RequestedBy: requester,
		RequestedAt: now, TargetScope: administration.Scope{Level: administration.LevelTenant, TenantID: req.DesiredChange.TenantID},
		BaseRevision: baseRevision, DesiredChange: req.DesiredChange, State: StateDraft, CorrelationID: correlationID,
		CreatedAt: now, UpdatedAt: now, Revision: 1}, nil
}

// Approval outcomes (approvalOutcome).
const (
	DecisionApproved         = "APPROVED"
	DecisionRejected         = "REJECTED"
	DecisionChangesRequested = "CHANGES_REQUESTED"
)

// DecisionRequest is ApprovalDecisionRequest.
type DecisionRequest struct {
	PlanID      string `json:"plan_id"`
	PlanVersion int    `json:"plan_version"`
	PlanDigest  string `json:"plan_digest"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason,omitempty"`
}

// Approval is ApprovalDecision with subject CHANGESET.
type Approval struct {
	ApprovalID    string    `json:"approval_id"`
	SubjectType   string    `json:"subject_type"`
	SubjectID     string    `json:"subject_id"`
	PlanID        string    `json:"plan_id"`
	PlanVersion   int       `json:"plan_version"`
	PlanDigest    string    `json:"plan_digest"`
	Decision      string    `json:"decision"`
	Reason        string    `json:"reason,omitempty"`
	DecidedBy     string    `json:"decided_by"`
	DecidedAt     time.Time `json:"decided_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

// DecisionTransition is the lifecycle transition a decision takes.
func DecisionTransition(decision string) string {
	switch decision {
	case DecisionApproved:
		return "approve"
	case DecisionRejected:
		return "reject"
	}
	return "request_changes"
}
