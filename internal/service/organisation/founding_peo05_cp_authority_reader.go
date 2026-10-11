package organisation

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// PEO05CoreRepository is the existing authoritative CP persistence interface.
// The production PostgresRepository implements it; no new parallel state model.
type PEO05CoreRepository interface {
	ListConvergedProvisionings(context.Context, string, int, string) ([]repository.ConvergedProvisioning, error)
	GetDesiredState(context.Context, string, int64) (convergence.DesiredState, error)
	GetTenantOnboardingRequest(context.Context, string) (domain.TenantOnboardingRequest, error)
	PlanningProduct(context.Context, string) (convergence.Product, bool, error)
	PlanningComposition(context.Context, string) ([]convergence.Member, bool, error)
	FindTenantProductSubscription(context.Context, string, string) (domain.ProductSubscriptionClassificationTarget, error)
}

var _ PEO05CoreRepository = (*repository.PostgresRepository)(nil)

// IAM and provider attestations may only be returned by separately
// authenticated authority adapters, with exact scope, TTL and revocation.
// They are never copied from applicant or browser data.
type PEO05CurrentIAM interface {
	CurrentTenantIAM(context.Context, string, string, time.Time) (PEO05IAMProof, error)
}
type PEO05CurrentProviders interface {
	CurrentMandatoryProviders(context.Context, string, string, convergence.Plan, []string, time.Time) ([]PEO05ProviderProof, error)
}

// AuthoritativePEO05Reader derives the CP half from durable, current state.
// It requires live IAM and provider readers by construction and fails closed
// when they are absent, stale or unavailable. It is non-mutating.
type AuthoritativePEO05Reader struct {
	Core      PEO05CoreRepository
	Freshness PEO05PlanFreshness
	IAM       PEO05CurrentIAM
	Providers PEO05CurrentProviders
}

func (r AuthoritativePEO05Reader) ReadPEO05Tenant(ctx context.Context, tenantID, orgID string, at time.Time) (PEO05TenantEvidence, error) {
	empty := PEO05TenantEvidence{}
	if ctx == nil || ctx.Err() != nil || r.Core == nil || r.Freshness == nil || r.IAM == nil || r.Providers == nil ||
		tenantID == "" || orgID == "" || at.IsZero() {
		return empty, ErrPEO05Integrity
	}

	rows, err := r.Core.ListConvergedProvisionings(ctx, tenantID, 2, "")
	if err != nil || len(rows) != 1 {
		return empty, ErrPEO05Integrity
	}
	current := rows[0]
	if current.TenantID != tenantID || current.State != "READY" ||
		current.ReadinessStatus != "READY" || current.Plan == nil || current.Decision == nil ||
		current.TenantOnboardingRequestID == "" || current.AdmissionDecisionID == "" ||
		current.OperationActive {
		return empty, ErrPEO05Integrity
	}

	desired, err := r.Core.GetDesiredState(ctx, current.ID, current.DesiredStateVersion)
	if err != nil || desired.Tenant.TenantID != tenantID ||
		!validPEO05Digest(desired.DesiredStateDigest) ||
		desired.DesiredStateDigest != current.DesiredStateDigest ||
		convergence.DesiredStateDigest(desired) != desired.DesiredStateDigest ||
		desired.Provenance.TenantOnboardingRequestID != current.TenantOnboardingRequestID ||
		desired.Provenance.AdmissionDecisionID != current.AdmissionDecisionID {
		return empty, ErrPEO05Integrity
	}
	plan, approval := *current.Plan, *current.Decision
	if plan.TenantID != tenantID || plan.TenantProvisioningID != current.Key ||
		plan.DesiredStateDigest != desired.DesiredStateDigest ||
		plan.PlanDigest != convergence.PlanDigest(plan) ||
		plan.PlanVersion != approval.PlanVersion || plan.PlanID != approval.PlanID ||
		plan.PlanDigest != approval.PlanDigest || approval.Decision != "APPROVED" ||
		approval.DecidedBy == "" || !plan.ExpiresAt.After(at) || len(plan.Blockers) > 0 {
		return empty, ErrPEO05Integrity
	}
	// An unchanged stored digest is insufficient: an approved plan becomes
	// stale whenever current market, catalogue, provider or topology facts
	// produce different execution material. Use the normal planner's rule.
	fresh, err := r.Freshness.CurrentPlan(ctx, desired, plan)
	if err != nil || !fresh {
		return empty, ErrPEO05Integrity
	}
	request, err := r.Core.GetTenantOnboardingRequest(ctx, current.TenantOnboardingRequestID)
	if err != nil || request.Status != domain.OnboardingFulfilled || request.TenantID != tenantID ||
		request.ID != current.TenantOnboardingRequestID ||
		request.AdmissionDecisionID != current.AdmissionDecisionID ||
		request.RequestedBy == "" || request.AuthorisedBy == "" ||
		request.RequestedBy == request.AuthorisedBy || request.RequestedBy == approval.DecidedBy ||
		request.AuthorisedBy == approval.DecidedBy {
		return empty, ErrPEO05Integrity
	}
	if desired.ResidencyRequirement == "" || len(desired.Products) == 0 {
		return empty, ErrPEO05Integrity
	}
	evidence := PEO05TenantEvidence{
		TenantID: tenantID, OrganisationID: orgID,
		AdmissionDecisionID: request.AdmissionDecisionID,
		OnboardingRequestID: request.ID,
		RequesterID:         request.RequestedBy, AuthoriserID: request.AuthorisedBy,
		ProvisioningState:      current.State,
		DesiredStateDigest:     desired.DesiredStateDigest,
		PlanDesiredStateDigest: plan.DesiredStateDigest,
		PlanDigest:             plan.PlanDigest, ApprovedPlanDigest: approval.PlanDigest,
		PlanApprovalID: approval.ApprovalID, PlanApproverID: approval.DecidedBy,
		PlanExpiresAt: plan.ExpiresAt, ResidencyRequirement: desired.ResidencyRequirement,
		RequiredProducts: []string{}, Subscriptions: []PEO05SubscriptionProof{},
		RequiredCapabilities: []string{},
	}

	// Refuse absent, ambiguous and unclassified subscriptions; never infer
	// product permission from a sponsoring group or another product.
	products := map[string]bool{}
	mandatory := map[string]bool{}
	for _, entry := range desired.Products {
		id := strings.TrimSpace(entry.ProductID)
		if id == "" || products[id] {
			return empty, ErrPEO05Integrity
		}
		products[id] = true
		product, found, err := r.Core.PlanningProduct(ctx, id)
		if err != nil || !found || product.ProductID != id || product.CompositionKey == "" {
			return empty, ErrPEO05Integrity
		}
		members, found, err := r.Core.PlanningComposition(ctx, product.CompositionKey)
		if err != nil || !found || len(members) == 0 {
			return empty, ErrPEO05Integrity
		}
		for _, member := range members {
			if member.CapabilityKey == "" {
				return empty, ErrPEO05Integrity
			}
			if member.Criticality == convergence.CriticalityMandatory {
				mandatory[member.CapabilityKey] = true
			}
		}
		sub, err := r.Core.FindTenantProductSubscription(ctx, tenantID, id)
		if err != nil || sub.TenantID != tenantID || sub.ProductID != id ||
			sub.ID == "" || sub.Status != "ACTIVE" || sub.ClassificationID == "" {
			return empty, ErrPEO05Integrity
		}
		evidence.RequiredProducts = append(evidence.RequiredProducts, id)
		evidence.Subscriptions = append(evidence.Subscriptions, PEO05SubscriptionProof{
			TenantID: sub.TenantID, ProductID: sub.ProductID, SubscriptionID: sub.ID, Status: sub.Status,
		})
	}
	if len(mandatory) == 0 {
		return empty, ErrPEO05Integrity
	}
	for key := range mandatory {
		evidence.RequiredCapabilities = append(evidence.RequiredCapabilities, key)
	}
	slices.Sort(evidence.RequiredCapabilities)
	slices.Sort(evidence.RequiredProducts)

	// Independent, authenticated calls are deliberately last. An error
	// leaves no candidate: neither a CP role nor provider metadata grants IAM.
	iam, err := r.IAM.CurrentTenantIAM(ctx, tenantID, orgID, at)
	if err != nil {
		return empty, ErrPEO05Integrity
	}
	providers, err := r.Providers.CurrentMandatoryProviders(ctx, tenantID, orgID, plan, evidence.RequiredCapabilities, at)
	if err != nil {
		return empty, ErrPEO05Integrity
	}
	evidence.IAM = iam
	evidence.Providers = providers
	if len(assessPEO05Tenant(evidence, FoundingAcceptanceEntry{OrganisationID: orgID, TenantID: tenantID}, at)) > 0 {
		return empty, ErrPEO05Integrity
	}
	return evidence, nil
}
