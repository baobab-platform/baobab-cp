package organisation

import (
	"context"

	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
)

// PEO05PlanFreshness re-evaluates a frozen plan against currently authoritative
// registry state. The stored digest alone cannot prove provider eligibility.
type PEO05PlanFreshness interface {
	CurrentPlan(context.Context, convergence.DesiredState, convergence.Plan) (bool, error)
}

// PEO05CanonicalPlanFreshness uses exactly the planner and Material rule that
// the CP provisioning API uses to invalidate superseded approvals. Environment
// must come from trusted CP deployment configuration, not tenant input.
type PEO05CanonicalPlanFreshness struct {
	Registry    convergence.Registry
	Environment string
}

func (f PEO05CanonicalPlanFreshness) CurrentPlan(ctx context.Context, desired convergence.DesiredState, approved convergence.Plan) (bool, error) {
	if ctx == nil || ctx.Err() != nil || f.Registry == nil || f.Environment == "" {
		return false, ErrPEO05Integrity
	}
	again, err := (convergence.Planner{Registry: f.Registry, Environment: f.Environment}).Plan(ctx, convergence.Input{
		Desired: desired, TenantProvisioningID: approved.TenantProvisioningID,
		PlanID: approved.PlanID, PlanVersion: approved.PlanVersion,
		BaseRevision: approved.BaseRevision, Now: approved.GeneratedAt,
	})
	if err != nil {
		return false, err
	}
	return convergence.Material(again) == convergence.Material(approved), nil
}
