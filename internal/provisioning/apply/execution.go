// Package apply executes approved provisioning plans (ADR-SHARED-015) as
// durable operations through the existing provisioning pipeline.
package apply

import (
	"context"
	"fmt"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
)

// bindingPriority is the priority of the single PRIMARY binding a plan
// creates per capability.
const bindingPriority = 10

// ExecutionManifest is the internal manifest an approved plan executes as:
// exactly the plan's steps, never re-derived from desired state. A market's
// participation capabilities are those of the frozen desired state the
// plan's digest binds. Grants are
// attributed to the approval that authorised them. A plan that references a
// market or engine that no longer exists fails here, before anything is
// written.
func ExecutionManifest(ctx context.Context, registry convergence.ExecutionRegistry, plan convergence.Plan, desired convergence.DesiredState, legalEntityID, approvalID string) (provisioning.ResolvedManifest, error) {
	m := provisioning.ResolvedManifest{
		TenantID: plan.TenantID, LegalEntityID: legalEntityID,
		DesiredStateVersion: plan.DesiredStateVersion,
	}
	for _, step := range plan.Steps {
		r := step.Resources
		switch step.Operation {
		case convergence.OpCreateMarketParticipation:
			market, err := registry.GetMarketByCode(ctx, r.Market)
			if err != nil {
				return provisioning.ResolvedManifest{}, fmt.Errorf("step %s: market %s: %w", step.StepID, r.Market, err)
			}
			capabilities := []domain.MarketParticipationCapability{}
			for _, d := range desired.MarketParticipation {
				if d.Market == r.Market {
					for _, a := range d.Activities {
						capabilities = append(capabilities, domain.MarketParticipationCapability(a))
					}
				}
			}
			if len(capabilities) == 0 || desired.DesiredStateDigest != plan.DesiredStateDigest {
				return provisioning.ResolvedManifest{}, fmt.Errorf("step %s: market %s has no participation in the plan's desired state", step.StepID, r.Market)
			}
			m.Markets = append(m.Markets, provisioning.ResolvedMarket{Code: r.Market, MarketID: market.ID, Capabilities: capabilities})
		case convergence.OpCreateCapabilityGrant:
			m.CapabilityGrants = append(m.CapabilityGrants, provisioning.ResolvedCapabilityGrant{
				CapabilityKey: r.CapabilityKey, Source: capabilitydomain.GrantSourceManualApproval, SourceReference: approvalID})
		case convergence.OpCreateCapabilityBinding:
			engine, err := registry.EngineRowIDByCode(ctx, r.EngineID)
			if err != nil {
				return provisioning.ResolvedManifest{}, fmt.Errorf("step %s: engine %s: %w", step.StepID, r.EngineID, err)
			}
			instance, err := domain.ParseResourceID("ei", r.EngineInstanceID)
			if err != nil {
				return provisioning.ResolvedManifest{}, fmt.Errorf("step %s: %w", step.StepID, err)
			}
			m.CapabilityBindings = append(m.CapabilityBindings, provisioning.ResolvedCapabilityBinding{
				CapabilityKey: r.CapabilityKey, EngineID: engine, EngineInstanceID: instance,
				Mode: capabilitydomain.BindingMode(r.BindingMode), Priority: bindingPriority})
		case convergence.OpVerifySecurity, convergence.OpVerifyReadiness:
			// Verified by the pipeline's binding topology checks,
			// reconciliation and readiness phases.
		default:
			return provisioning.ResolvedManifest{}, fmt.Errorf("step %s: operation %s cannot be executed", step.StepID, step.Operation)
		}
	}
	return m, nil
}
