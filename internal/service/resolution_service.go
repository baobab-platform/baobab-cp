package service

import (
	"context"
	"errors"
	"fmt"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
)

// ResolutionRequest is the service-level input for a control-plane resolution request.
//
// TenantID and CanonicalEntityID stay distinct end to end (ADR-0005 §2) --
// see resolver.ResolutionRequest's doc comment for why conflating them
// (docs/governance/gate-iam-0-discovery.md R-3) was a real bug, not a
// naming choice.
type ResolutionRequest struct {
	TenantID          string
	CanonicalEntityID string
	CapabilityKey     string
	Context           resolver.Context
	Mappings          []domain.Mapping
	Bindings          []resolver.CapabilityBinding
	EngineInstances   []resolver.EngineInstance
}

// ResolutionResult contains the fully resolved runtime decision for a tenant request.
type ResolutionResult struct {
	Context    resolver.Context
	Mapping    resolver.ResolvedMapping
	Capability resolver.ResolvedCapability
	Policy     resolver.PolicyDecision
	Topology   resolver.EngineInstance
	Trace      resolver.ResolutionTrace
}

// ResolutionService exposes the composed resolver pipeline as a service interface.
type ResolutionService struct {
	Pipeline   resolver.ResolutionPipeline
	Repository repository.ResolverRepository
	// EnforceEntitlement opts this service into the capability entitlement
	// and lifecycle-eligibility gates (ADR-BCP-003 §9/§6, resolver.
	// EntitlementResolverImpl and Capability.IsResolvable) for real
	// traffic. False by default: no rollout/backfill of
	// capability.capability_grant exists yet for every canonical capability,
	// so turning this on unconditionally would fail every
	// resolution rather than merely skip a check. Grants and capability
	// lifecycle state are only fetched when this rollout switch is enabled;
	// requested capability selection itself is always authoritative.
	// ResolutionRequest.Grants/.Capability are only populated when this is
	// explicitly true. Grants and Scopes are read separately from
	// Repository so enabling this doesn't also require reconstructing the
	// service's existing Repository wiring.
	EnforceEntitlement bool
	Grants             repository.CapabilityGrantRepository
	Scopes             repository.CapabilityScopeWriter
	CapabilityRegistry repository.CapabilityRegistryRepository
}

func (s ResolutionService) Resolve(ctx context.Context, req ResolutionRequest) (ResolutionResult, error) {
	if ctx == nil {
		return ResolutionResult{}, errors.New("context is required")
	}
	if req.TenantID == "" && req.Context.TenantID == "" {
		return ResolutionResult{}, errors.New("tenant_id is required")
	}
	if req.Context.TenantID == "" {
		req.Context.TenantID = req.TenantID
	}
	if req.CanonicalEntityID == "" {
		return ResolutionResult{}, errors.New("canonical_entity_id is required")
	}
	if !capabilitydomain.ValidCapabilityKey(req.CapabilityKey) {
		return ResolutionResult{}, errors.New("capability_key is invalid")
	}
	if s.Repository != nil {
		mappings, err := s.Repository.ListMappings(ctx, req.CanonicalEntityID)
		if err != nil {
			return ResolutionResult{}, fmt.Errorf("load mappings: %w", err)
		}
		bindings, err := s.Repository.ListBindings(ctx, req.CapabilityKey)
		if err != nil {
			return ResolutionResult{}, fmt.Errorf("load capability bindings: %w", err)
		}
		var instances []resolver.EngineInstance
		if len(bindings) > 0 {
			instances, err = s.Repository.ListActiveInstances(ctx, bindings[0].EngineID)
			if err != nil {
				return ResolutionResult{}, fmt.Errorf("load engine instances: %w", err)
			}
		}
		req.Mappings, req.Bindings, req.EngineInstances = mappings, bindings, instances
	}

	pipelineReq := resolver.ResolutionRequest{
		TenantID:          req.TenantID,
		CanonicalEntityID: req.CanonicalEntityID,
		CapabilityKey:     req.CapabilityKey,
		Context:           req.Context,
		Candidates:        req.Mappings,
		Bindings:          req.Bindings,
		EngineInstances:   req.EngineInstances,
	}

	if s.EnforceEntitlement {
		if s.Grants != nil {
			grants, err := s.Grants.ListGrants(ctx, req.Context.TenantID, req.CapabilityKey)
			if err != nil {
				return ResolutionResult{}, fmt.Errorf("load capability grants: %w", err)
			}
			// resolver.ResolutionRequest.Grants uses nil (not merely empty)
			// as its own opt-out signal, so a ListGrants result of no rows
			// -- which the fake and Postgres implementations both return as
			// a nil slice -- must not be forwarded as-is: that would
			// silently re-disable the very gate EnforceEntitlement just
			// turned on. Force it non-nil so "no grants found" is
			// unambiguously distinct from "Grants was never populated".
			if grants == nil {
				grants = []capabilitydomain.CapabilityGrant{}
			}
			pipelineReq.Grants = grants
			if s.Scopes != nil {
				scopes := make(map[string]capabilitydomain.CapabilityScope, len(grants))
				for _, grant := range grants {
					if _, alreadyLoaded := scopes[grant.ScopeID]; alreadyLoaded {
						continue
					}
					scope, err := s.Scopes.GetCapabilityScope(ctx, grant.ScopeID)
					if err != nil {
						continue
					}
					scopes[grant.ScopeID] = scope
				}
				pipelineReq.Scopes = scopes
			}
		}
		// A requested capability absent from the registry does not fail resolution
		// closed here -- capability registration (ADR-BCP-003 §4) is a
		// separate, still-incomplete rollout of its own; only a capability
		// the registry actually knows about gets its lifecycle checked.
		if s.CapabilityRegistry != nil {
			if capability, err := s.CapabilityRegistry.GetCapability(ctx, req.CapabilityKey); err == nil {
				pipelineReq.Capability = &capability
			}
		}
	}

	pipelineResult, err := s.Pipeline.Resolve(ctx, pipelineReq)
	if err != nil {
		return ResolutionResult{}, fmt.Errorf("resolution failed: %w", err)
	}

	return ResolutionResult{
		Context:    pipelineResult.Context,
		Mapping:    pipelineResult.Mapping,
		Capability: pipelineResult.Capability,
		Policy:     pipelineResult.Policy,
		Topology:   pipelineResult.Topology,
		Trace:      pipelineResult.Trace,
	}, nil
}
