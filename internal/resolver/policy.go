package resolver

import (
	"context"
	"errors"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
)

// PolicyDecision represents the result of checking a capability binding against policy constraints.
type PolicyDecision struct {
	Allowed bool
	Reason  string
}

// PolicyChecker validates a selected capability binding against residency and isolation constraints.
type PolicyChecker struct{}

func (PolicyChecker) Check(_ context.Context, ctx Context, binding CapabilityBinding) PolicyDecision {
	if binding.Status != "ACTIVE" {
		return PolicyDecision{Allowed: false, Reason: "binding inactive"}
	}
	if ctx.TenantID == "" {
		return PolicyDecision{Allowed: false, Reason: "tenant context required"}
	}
	if ctx.CountryCode == "" && ctx.MarketID == "" {
		return PolicyDecision{Allowed: false, Reason: "market or country context required"}
	}
	if binding.EngineInstanceID == "" {
		return PolicyDecision{Allowed: false, Reason: "engine instance missing"}
	}
	return PolicyDecision{Allowed: true, Reason: "policy ok"}
}

// EnforcePolicy resolves a capability using the binding resolver and then validates the selected binding.
func EnforcePolicy(ctx context.Context, q CapabilityResolutionQuery) (ResolvedCapability, error) {
	resolved, err := CapabilityResolverImpl{}.Resolve(ctx, q)
	if err != nil {
		return ResolvedCapability{}, err
	}
	checker := PolicyChecker{}
	decision := checker.Check(ctx, q.Context, CapabilityBinding{
		CapabilityKey:    resolved.CapabilityKey,
		EngineID:         resolved.EngineID,
		EngineInstanceID: resolved.EngineInstanceID,
		BindingMode:      capabilitydomain.BindingMode(resolved.BindingMode),
		Status:           "ACTIVE",
		ContractVersion:  resolved.ContractVersion,
	})
	if !decision.Allowed {
		return ResolvedCapability{}, errors.New(decision.Reason)
	}
	return resolved, nil
}
