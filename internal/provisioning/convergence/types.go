// Package convergence plans tenant provisioning as desired-state
// convergence (ADR-SHARED-015). Business intent is frozen from an
// AUTHORISED TenantOnboardingRequest into a DesiredState; planning derives
// provider selection, grants and bindings from it and from authoritative
// registry state into an immutable, digest-bound Plan. Planning is side-effect
// free (ADR-BCP-021 section 20): nothing here writes.
//
// The types mirror Shared control-plane/v1 provisioning-desired-state,
// provisioning-plan and change-plan schemas field for field.
package convergence

import (
	"context"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// DesiredStateProvenance is the unbroken lineage AdmissionDecision ->
// TenantOnboardingRequest -> desired state.
type DesiredStateProvenance struct {
	TenantOnboardingRequestID string `json:"tenant_onboarding_request_id"`
	AdmissionDecisionID       string `json:"admission_decision_id"`
	DesiredStateVersion       int64  `json:"desired_state_version"`
}

type DesiredTenant struct {
	TenantID    string `json:"tenant_id"`
	DisplayName string `json:"display_name"`
}

type DesiredProduct struct {
	ProductID string   `json:"product_id"`
	Profiles  []string `json:"profiles"`
}

type DesiredMarket struct {
	Market     string   `json:"market"`
	Activities []string `json:"activities"`
}

type DesiredEstate struct {
	EstateKey string `json:"estate_key"`
	Channel   string `json:"channel"`
}

// DesiredState is the business intent a tenant is provisioned to. It never
// names engines, providers, grants or bindings.
type DesiredState struct {
	Tenant               DesiredTenant          `json:"tenant"`
	LegalEntities        []string               `json:"legal_entities"`
	Products             []DesiredProduct       `json:"products"`
	MarketParticipation  []DesiredMarket        `json:"market_participation"`
	DigitalEstates       []DesiredEstate        `json:"digital_estates"`
	IsolationRequirement string                 `json:"isolation_requirement"`
	ResidencyRequirement string                 `json:"residency_requirement"`
	Provenance           DesiredStateProvenance `json:"provenance"`
	DesiredStateDigest   string                 `json:"desired_state_digest"`
	FrozenAt             time.Time              `json:"frozen_at"`
}

// Canonical provisioning operation types (provisioning-plan.schema.json).
const (
	OpCreateMarketParticipation = "CREATE_MARKET_PARTICIPATION"
	OpCreateCapabilityGrant     = "CREATE_CAPABILITY_GRANT"
	OpCreateCapabilityBinding   = "CREATE_CAPABILITY_BINDING"
	OpVerifySecurity            = "VERIFY_SECURITY"
	OpVerifyReadiness           = "VERIFY_READINESS"
)

// StepResources are the resolved resources a step acts on, named only in
// ADR-SHARED-012 identifiers.
type StepResources struct {
	TenantID         string `json:"tenant_id,omitempty"`
	Market           string `json:"market,omitempty"`
	ProductID        string `json:"product_id,omitempty"`
	CapabilityKey    string `json:"capability_key,omitempty"`
	ProviderKey      string `json:"provider_key,omitempty"`
	BindingMode      string `json:"binding_mode,omitempty"`
	EngineID         string `json:"engine_id,omitempty"`
	SystemNamespace  string `json:"system_namespace,omitempty"`
	EngineInstanceID string `json:"engine_instance_id,omitempty"`
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

type Check struct {
	Check       string `json:"check"`
	Description string `json:"description"`
}

type ImpactAnalysis struct {
	Summary          string `json:"summary"`
	ResourcesCreated int    `json:"resources_created"`
	ResourcesChanged int    `json:"resources_changed"`
	ResourcesRemoved int    `json:"resources_removed"`
	SecurityImpact   string `json:"security_impact,omitempty"`
	IsolationImpact  string `json:"isolation_impact,omitempty"`
	ResidencyImpact  string `json:"residency_impact,omitempty"`
}

// Plan is a ProvisioningPlan: an immutable, reviewable artifact whose digest
// an approval binds.
type Plan struct {
	PlanID                string         `json:"plan_id"`
	PlanVersion           int            `json:"plan_version"`
	PlanDigest            string         `json:"plan_digest"`
	BaseRevision          int64          `json:"base_revision"`
	TenantProvisioningID  string         `json:"tenant_provisioning_id"`
	TenantID              string         `json:"tenant_id"`
	DesiredStateVersion   int64          `json:"desired_state_version"`
	DesiredStateDigest    string         `json:"desired_state_digest"`
	GeneratedAt           time.Time      `json:"generated_at"`
	ExpiresAt             time.Time      `json:"expires_at"`
	RiskClass             string         `json:"risk_class"`
	Steps                 []Step         `json:"steps"`
	SecurityChecks        []Check        `json:"security_checks"`
	ReadinessRequirements []Check        `json:"readiness_requirements"`
	Blockers              []Finding      `json:"blockers"`
	Warnings              []Finding      `json:"warnings"`
	ImpactAnalysis        ImpactAnalysis `json:"impact_analysis"`
	VerificationStrategy  string         `json:"verification_strategy"`
	CompensationStrategy  string         `json:"compensation_strategy"`
}

// Expired reports whether the plan's validity window has closed
// (ADR-BCP-021 section 28). A plan is also stale when authoritative state
// changed so that planning again yields other Material (section 29).
func (p Plan) Expired(now time.Time) bool {
	return !now.Before(p.ExpiresAt)
}

// ExecutionRegistry translates a plan's public identifiers back to the
// registry rows the existing provisioners write against.
type ExecutionRegistry interface {
	GetMarketByCode(ctx context.Context, code string) (domain.Market, error)
	EngineRowIDByCode(ctx context.Context, engineID string) (string, error)
}

// ExecutedProvisioning is what executing an operation needs of the
// provisioning it applies.
type ExecutedProvisioning struct {
	ID, Key, TenantID string
	Plan              *Plan
	Desired           *DesiredState
	ApprovalID        string
	ApprovedDigest    string
	Approved          bool
}
