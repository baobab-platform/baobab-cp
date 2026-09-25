package resolver

import (
	"context"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// EngineInstance is the runtime engine instance selected by the topology resolver.
type EngineInstance = domain.EngineInstance

// TopologyResolutionQuery resolves an engine instance within the current trusted context.
type TopologyResolutionQuery struct {
	Context                  Context
	SelectedEngineInstanceID string
	EngineInstances          []EngineInstance
	At                       time.Time
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
		if instance.HealthStatus != "" && instance.HealthStatus != "UNKNOWN" && instance.HealthStatus != "HEALTHY" {
			return EngineInstance{}, errors.New("selected engine instance is not healthy")
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
