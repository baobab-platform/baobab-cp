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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
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
	KindMarketActivation    = "MARKET_ACTIVATION"
	KindMappingActivation   = "MAPPING_ACTIVATION"
	KindProviderActivation  = "PROVIDER_ACTIVATION"
	KindReleaseApproval     = "ENGINE_RELEASE_APPROVAL"
	KindDesiredRelease      = "ENGINE_INSTANCE_DESIRED_RELEASE"
	KindGrantIssuance       = administration.KindIssuance
	KindGrantDelegation     = administration.KindDelegation
)

// Change kind targets.
const (
	TargetTenant   = "TENANT"
	TargetMarket   = "MARKET"
	TargetMapping  = "MAPPING"
	TargetProvider = "PROVIDER"
	TargetRelease  = "ENGINE_RELEASE"
	TargetInstance = "ENGINE_INSTANCE"
	// TargetAdminPrincipal is the grantee of a grant issuance; TargetAdminGrant
	// the source grant of a delegation.
	TargetAdminPrincipal = "ADMINISTRATIVE_PRINCIPAL"
	TargetAdminGrant     = "ADMINISTRATIVE_GRANT"
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
	OpActivateMarket    = "ACTIVATE_MARKET"
	OpVerifyMarketState = "VERIFY_MARKET_STATE"
	OpActivateMapping   = "ACTIVATE_MAPPING"
	OpVerifyMapping     = "VERIFY_MAPPING_STATE"
	OpActivateProvider  = "ACTIVATE_PROVIDER"
	OpVerifyProvider    = "VERIFY_PROVIDER_STATE"
	OpApproveRelease    = "APPROVE_ENGINE_RELEASE"
	OpVerifyRelease     = "VERIFY_ENGINE_RELEASE_STATE"
	OpSetDesired        = "SET_DESIRED_RELEASE"
	OpVerifyDesired     = "VERIFY_DESIRED_RELEASE"
	OpIssueGrant        = "ISSUE_ADMINISTRATIVE_GRANT"
	OpDelegateGrant     = "DELEGATE_ADMINISTRATIVE_GRANT"
	OpVerifyGrant       = "VERIFY_ADMINISTRATIVE_GRANT"
)

// DesiredChange is one of the desiredChange kinds. Exactly one of the
// target identifiers is set, as the kind's schema branch requires. An
// ENGINE_INSTANCE_DESIRED_RELEASE names its engine instance and, in
// ReleaseID, the release it desires; an empty ReleaseID clears the desired
// release and is written as release_id null.
type DesiredChange struct {
	Kind             string `json:"kind"`
	TenantID         string `json:"tenant_id,omitempty"`
	MarketID         string `json:"market_id,omitempty"`
	MappingID        string `json:"mapping_id,omitempty"`
	ProviderID       string `json:"provider_id,omitempty"`
	EngineInstanceID string `json:"engine_instance_id,omitempty"`
	ReleaseID        string `json:"release_id,omitempty"`
	// Administrative grant changes (ADMINISTRATIVE_GRANT_ISSUANCE and
	// ADMINISTRATIVE_GRANT_DELEGATION): the grantee or delegate, the
	// authority wanted and, for a delegation, the grant it rests on.
	PrincipalID    string                     `json:"principal_id,omitempty"`
	Permission     string                     `json:"permission,omitempty"`
	Scope          *administration.Scope      `json:"scope,omitempty"`
	GrantType      administration.GrantType   `json:"grant_type,omitempty"`
	ValidFrom      *time.Time                 `json:"valid_from,omitempty"`
	ValidUntil     *time.Time                 `json:"valid_until,omitempty"`
	DelegableDepth int                        `json:"delegable_depth,omitempty"`
	Conditions     *administration.Conditions `json:"conditions,omitempty"`
	SourceGrantID  string                     `json:"source_grant_id,omitempty"`
}

// MarshalJSON writes release_id null for a desired-release change that
// clears the desired release: the contract requires release_id on that
// kind, as a release or null.
func (d DesiredChange) MarshalJSON() ([]byte, error) {
	type plain DesiredChange
	if d.Kind != KindDesiredRelease || d.ReleaseID != "" {
		return json.Marshal(plain(d))
	}
	return json.Marshal(struct {
		plain
		ReleaseID *string `json:"release_id"`
	}{plain: plain(d)})
}

// TargetID is the identifier of the resource the change names.
func (d DesiredChange) TargetID() string {
	switch Kinds()[d.Kind].Target {
	case TargetMarket:
		return d.MarketID
	case TargetMapping:
		return d.MappingID
	case TargetProvider:
		return d.ProviderID
	case TargetRelease:
		return d.ReleaseID
	case TargetInstance:
		return d.EngineInstanceID
	case TargetAdminPrincipal:
		return d.PrincipalID
	case TargetAdminGrant:
		return d.SourceGrantID
	}
	return d.TenantID
}

// TargetType is the resource type the change names (TENANT, MARKET,
// MAPPING, PROVIDER, ENGINE_RELEASE or ENGINE_INSTANCE), or "" for an
// unsupported kind.
func (d DesiredChange) TargetType() string { return Kinds()[d.Kind].Target }

// label names the target in findings and summaries.
func (d DesiredChange) label() string {
	switch d.TargetType() {
	case TargetMarket:
		return "Market " + d.MarketID
	case TargetMapping:
		return "Mapping " + d.MappingID
	case TargetProvider:
		return "Provider " + d.ProviderID
	case TargetRelease:
		return "Engine release " + d.ReleaseID
	case TargetInstance:
		return "Engine instance " + d.EngineInstanceID
	case TargetAdminPrincipal:
		return "Principal " + d.PrincipalID
	case TargetAdminGrant:
		return "Grant " + d.SourceGrantID
	}
	return "Tenant " + d.TenantID
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

// StepResources is changesetStepResources. An engine instance step names
// the DesiredReleaseID it sets; empty clears it, written as null.
type StepResources struct {
	TenantID         string `json:"tenant_id,omitempty"`
	MarketID         string `json:"market_id,omitempty"`
	MappingID        string `json:"mapping_id,omitempty"`
	ProviderID       string `json:"provider_id,omitempty"`
	ReleaseID        string `json:"release_id,omitempty"`
	EngineInstanceID string `json:"engine_instance_id,omitempty"`
	DesiredReleaseID string `json:"desired_release_id,omitempty"`
	// GranteePrincipalID is a grant issuance step's target; SourceGrantID a
	// delegation step's.
	GranteePrincipalID string `json:"grantee_principal_id,omitempty"`
	SourceGrantID      string `json:"source_grant_id,omitempty"`
	FromStatus         string `json:"from_status,omitempty"`
	ToStatus           string `json:"to_status,omitempty"`
	TargetRevision     int64  `json:"target_revision,omitempty"`
}

// MarshalJSON writes desired_release_id null on an engine instance step
// that clears the desired release.
func (r StepResources) MarshalJSON() ([]byte, error) {
	type plain StepResources
	if r.EngineInstanceID == "" || r.DesiredReleaseID != "" {
		return json.Marshal(plain(r))
	}
	return json.Marshal(struct {
		plain
		DesiredReleaseID *string `json:"desired_release_id"`
	}{plain: plain(r)})
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
	scope := administration.Scope{Level: administration.LevelPlatform}
	switch d := req.DesiredChange; kind.Target {
	case TargetTenant:
		if !domain.ValidTenantID(d.TenantID) || d.MarketID != "" || d.MappingID != "" || d.ProviderID != "" || d.ReleaseID != "" || d.EngineInstanceID != "" {
			return Changeset{}, fmt.Errorf("%w: tenant_id is not a Control Plane tenant identifier", ErrInvalid)
		}
		scope = administration.Scope{Level: administration.LevelTenant, TenantID: d.TenantID}
	case TargetMarket:
		if d.MarketID == "" || d.TenantID != "" || d.MappingID != "" || d.ProviderID != "" || d.ReleaseID != "" || d.EngineInstanceID != "" {
			return Changeset{}, fmt.Errorf("%w: a market activation names exactly its market_id", ErrInvalid)
		}
	case TargetMapping:
		if !domain.ValidMappingID(d.MappingID) || d.TenantID != "" || d.MarketID != "" || d.ProviderID != "" || d.ReleaseID != "" || d.EngineInstanceID != "" {
			return Changeset{}, fmt.Errorf("%w: mapping_id is not a Control Plane mapping identifier", ErrInvalid)
		}
	case TargetProvider:
		if !domain.ValidProviderID(d.ProviderID) || d.TenantID != "" || d.MarketID != "" || d.MappingID != "" || d.ReleaseID != "" || d.EngineInstanceID != "" {
			return Changeset{}, fmt.Errorf("%w: provider_id is not a canonical capability provider identifier", ErrInvalid)
		}
	case TargetRelease:
		if !release.ValidID(d.ReleaseID) || d.TenantID != "" || d.MarketID != "" || d.MappingID != "" || d.ProviderID != "" || d.EngineInstanceID != "" {
			return Changeset{}, fmt.Errorf("%w: release_id is not an engine release identifier", ErrInvalid)
		}
	case TargetInstance:
		if !domain.ValidEngineInstanceID(d.EngineInstanceID) || (d.ReleaseID != "" && !release.ValidID(d.ReleaseID)) ||
			d.TenantID != "" || d.MarketID != "" || d.MappingID != "" || d.ProviderID != "" {
			return Changeset{}, fmt.Errorf("%w: a desired release change names an engine instance and a release or none", ErrInvalid)
		}
	case TargetAdminPrincipal, TargetAdminGrant:
		sc, err := grantChangeScope(req.DesiredChange, kind.Target)
		if err != nil {
			return Changeset{}, err
		}
		scope = sc
	default:
		return Changeset{}, fmt.Errorf("%w: change kind %q names no supported target", ErrInvalid, req.DesiredChange.Kind)
	}
	if baseRevision < 1 {
		baseRevision = 1
	}
	return Changeset{ChangesetID: id, ChangesetType: kind.ChangesetType, Title: req.Title, Description: req.Description,
		Reason: req.Reason, BusinessJustification: req.BusinessJustification, Source: source, RequestedBy: requester,
		RequestedAt: now, TargetScope: scope,
		BaseRevision: baseRevision, DesiredChange: req.DesiredChange, State: StateDraft, CorrelationID: correlationID,
		CreatedAt: now, UpdatedAt: now, Revision: 1}, nil
}

// grantChangeScope validates the shape of an administrative grant change and
// returns its target scope: the scope of the authority it concerns. Whether
// the authority is grantable is a plan check, not a shape error.
func grantChangeScope(d DesiredChange, target string) (administration.Scope, error) {
	if d.TenantID != "" || d.MarketID != "" || d.MappingID != "" || d.ProviderID != "" || d.ReleaseID != "" || d.EngineInstanceID != "" {
		return administration.Scope{}, fmt.Errorf("%w: a grant change names no tenant, market, mapping, provider, release or instance", ErrInvalid)
	}
	if !domain.IsUUID(d.PrincipalID) || d.Permission == "" || d.Scope == nil {
		return administration.Scope{}, fmt.Errorf("%w: a grant change names a principal, a permission and a scope", ErrInvalid)
	}
	if target == TargetAdminGrant && (d.SourceGrantID == "" || d.GrantType != "") {
		return administration.Scope{}, fmt.Errorf("%w: a delegation names its source_grant_id and no grant_type", ErrInvalid)
	}
	if target == TargetAdminPrincipal && (d.SourceGrantID != "" || d.GrantType == "") {
		return administration.Scope{}, fmt.Errorf("%w: an issuance names its grant_type and no source_grant_id", ErrInvalid)
	}
	if err := d.Scope.Validate(); err != nil {
		return administration.Scope{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return *d.Scope, nil
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
