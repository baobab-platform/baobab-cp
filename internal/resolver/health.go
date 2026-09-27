package resolver

import (
	"context"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/health"
)

// ResolveHealthyCapability resolves a capability among only the bindings
// whose health the policy accepts at now (ADR-BCP-006 section 22), so an
// unhealthy PRIMARY yields to a healthy FALLBACK instead of failing the
// resolution. levels holds each binding's health by binding ID; a binding
// absent from it has no observation, so its instance is UNKNOWN.
//
// When no health-eligible binding resolves but one the policy refused would
// have, the refusal is returned (a *health.IneligibleError naming the
// registered denial code), not a bare "capability not found".
func ResolveHealthyCapability(ctx context.Context, q CapabilityResolutionQuery, criticality health.Criticality,
	levels map[string]health.Levels, now time.Time) (ResolvedCapability, error) {
	var eligible []CapabilityBinding
	refused := false
	for _, b := range q.Bindings {
		if healthPolicy.Evaluate(criticality, levels[b.ID], now).Eligible {
			eligible = append(eligible, b)
		} else {
			refused = true
		}
	}
	healthy := q
	healthy.Bindings = eligible
	resolved, err := CapabilityResolverImpl{}.Resolve(ctx, healthy)
	if err == nil || !refused {
		return resolved, err
	}
	wouldHave, allErr := CapabilityResolverImpl{}.Resolve(ctx, q)
	if allErr != nil {
		return resolved, err
	}
	if healthErr := healthPolicy.Check(criticality, levels[wouldHave.BindingID], now); healthErr != nil {
		return ResolvedCapability{}, healthErr
	}
	return resolved, err
}

// HealthValidUntil is the earliest expires_at among the held observations:
// after it, the health a decision relied on is no longer current, so the
// decision must not outlive it. Zero when no observation is held.
func HealthValidUntil(levels health.Levels) time.Time {
	var until time.Time
	for _, o := range []*health.Observation{levels.EngineInstance, levels.Provider, levels.ProviderCapability} {
		if o != nil && (until.IsZero() || o.ExpiresAt.Before(until)) {
			until = o.ExpiresAt
		}
	}
	return until
}
