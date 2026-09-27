package resolver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
)

// healthPolicy is capability/v1 health-policy.yaml at the pinned Shared
// commit. Resolution, relocation and provisioning planning all decide
// eligibility through it.
var healthPolicy = health.MustDefaultPolicy()

// EngineInstance is the runtime engine instance selected by the topology resolver.
type EngineInstance = domain.EngineInstance

// TopologyResolutionQuery resolves an engine instance within the current trusted context.
type TopologyResolutionQuery struct {
	Context                  Context
	SelectedEngineInstanceID string
	EngineInstances          []EngineInstance
	At                       time.Time
	// HealthCriticality is the capability's declared health criticality;
	// empty is STANDARD.
	HealthCriticality health.Criticality
	// Health is what the Control Plane holds for the selected instance and,
	// when the binding names one, its provider and the provider's
	// capability. A level with no current observation is UNKNOWN, which a
	// CRITICAL capability never accepts (ADR-BCP-006 sections 21-22).
	Health health.Levels
	// HealthAt is when health is judged; zero means At. A binding that takes
	// effect later is still judged on the health observed now.
	HealthAt time.Time
}

// TopologyResolverImpl validates the exact instance selected by CapabilityBinding.
type TopologyResolverImpl struct{}

func (TopologyResolverImpl) Resolve(_ context.Context, q TopologyResolutionQuery) (EngineInstance, error) {
	if len(q.EngineInstances) == 0 {
		return EngineInstance{}, errors.New("engine instance not found")
	}

	if q.SelectedEngineInstanceID == "" {
		return EngineInstance{}, errors.New("selected engine instance is required")
	}
	at := q.At
	if at.IsZero() {
		at = q.Context.ResolvedAt
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	for _, instance := range q.EngineInstances {
		if instance.ID != q.SelectedEngineInstanceID {
			continue
		}
		if instance.Status != "ACTIVE" {
			return EngineInstance{}, errors.New("selected engine instance is not active")
		}
		healthAt := q.HealthAt
		if healthAt.IsZero() {
			healthAt = at
		}
		if err := healthPolicy.Check(q.HealthCriticality, q.Health, healthAt); err != nil {
			return EngineInstance{}, fmt.Errorf("selected engine instance is not eligible: %w", err)
		}
		if !instance.EffectiveFrom.IsZero() && at.Before(instance.EffectiveFrom) {
			return EngineInstance{}, errors.New("selected engine instance is not yet effective")
		}
		if instance.EffectiveTo != nil && !at.Before(*instance.EffectiveTo) {
			return EngineInstance{}, errors.New("selected engine instance is expired")
		}
		if q.Context.Environment != "" && instance.Environment != q.Context.Environment {
			return EngineInstance{}, errors.New("engine instance environment mismatch")
		}
		if q.Context.DeploymentRegion != "" && instance.Region != q.Context.DeploymentRegion {
			return EngineInstance{}, errors.New("engine instance region mismatch")
		}
		if q.Context.IsolationProfileID != "" && instance.IsolationProfileID != q.Context.IsolationProfileID {
			return EngineInstance{}, errors.New("engine instance isolation profile mismatch")
		}
		if q.Context.DeploymentRegion != "" && instance.ResidencyRegion != "" && instance.ResidencyRegion != q.Context.DeploymentRegion {
			return EngineInstance{}, errors.New("engine instance residency mismatch")
		}
		return instance, nil
	}
	return EngineInstance{}, errors.New("selected engine instance not found")
}
