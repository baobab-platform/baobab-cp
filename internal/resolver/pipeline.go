package resolver

import (
	"context"
	"errors"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// ResolutionRequest is the combined request used by the full resolver pipeline.
//
// TenantID and CanonicalEntityID are deliberately separate fields (ADR-0005
// §2, "Realm/Org/Tenant/LegalEntity Model"): a tenant is the platform
// boundary a request executes within, a canonical entity is the specific
// business object (product, customer, order, ...) a mapping resolves for.
// Conflating them here (docs/governance/gate-iam-0-discovery.md R-3) meant
// /v1/resolve could only ever "resolve the tenant itself" instead of an
// actual entity within it -- see Resolve's mapping-lookup call below.
type ResolutionRequest struct {
	TenantID          string
	CanonicalEntityID string
	CapabilityKey     string
	Context           Context
	Candidates        []domain.Mapping
	Bindings          []CapabilityBinding
	EngineInstances   []EngineInstance
	// Grants and Scopes feed the entitlement gate (ADR-BCP-003 §9, §34 step
	// 2), checked before any CapabilityBinding is considered. Grants is nil
	// by default and the gate is skipped entirely when it is nil: no
	// backfill of capability.capability_grant exists yet for any
	// tenant/capability, so unconditionally enforcing entitlement today
	// would fail every resolution in a real deployment. A caller passes a
	// non-nil Grants slice (even an empty one, meaning "none found") to
	// opt into real enforcement once grant data exists for it.
	Grants []capabilitydomain.CapabilityGrant
	Scopes map[string]capabilitydomain.CapabilityScope
	// Capability, when supplied, gates resolution on the capability's own
	// lifecycle eligibility (ADR-BCP-003 §6). Nil by default and skipped
	// when nil, mirroring Grants' opt-in rollout mechanism.
	Capability *capabilitydomain.Capability
	// MappingScopes holds the governed MappingScopes the candidates name, by
	// scope_id. A candidate naming a governed scope that is absent here never
	// applies.
	MappingScopes map[string]domain.MappingScope
}

// ResolutionResult is the final output from the composed resolver pipeline.
type ResolutionResult struct {
	Context    Context
	Mapping    ResolvedMapping
	Capability ResolvedCapability
	Policy     PolicyDecision
	Topology   EngineInstance
	Trace      ResolutionTrace
}

// ResolutionPipeline composes the resolution stack into a single deterministic decision process.
type ResolutionPipeline struct{}

func (ResolutionPipeline) Resolve(ctx context.Context, req ResolutionRequest) (ResolutionResult, error) {
	trace := ResolutionTrace{CorrelationID: req.Context.CorrelationID, TenantID: req.Context.TenantID, CapabilityKey: req.CapabilityKey}
	if req.TenantID == "" && req.Context.TenantID == "" {
		return ResolutionResult{}, resolutionFailure(trace, errors.New("tenant context required"))
	}
	if req.Context.TenantID == "" {
		req.Context.TenantID = req.TenantID
	}
	trace.TenantID = req.Context.TenantID

	if req.CanonicalEntityID == "" {
		return ResolutionResult{}, resolutionFailure(trace, errors.New("canonical_entity_id is required"))
	}
	if req.CapabilityKey == "" {
		return ResolutionResult{}, resolutionFailure(trace, errors.New("capability_key is required"))
	}

	if req.Context.Provenance == nil {
		req.Context.Provenance = map[string]ContextSource{}
	}

	// Only the request tenant's mappings are candidates: another tenant's
	// mapping of the same canonical entity must neither win nor tie
	// (Canonical Mapping Model section 47).
	candidates := make([]domain.Mapping, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		if candidate.TenantID == req.Context.TenantID {
			candidates = append(candidates, candidate)
		}
	}
	mappingResult, err := MappingResolverImpl{}.Resolve(ctx, MappingResolutionQuery{
		CanonicalEntityID: req.CanonicalEntityID,
		Context:           req.Context,
		Candidates:        candidates,
		GovernedScopes:    req.MappingScopes,
	})
	if err != nil {
		return ResolutionResult{}, resolutionFailure(trace, err)
	}
	// domain.Mapping.Validate() (called inside resolveMapping's eligibility
	// filter) already requires TenantID to be non-empty, so this is a
	// straight equality check, not a nil-guarded one: a resolved mapping
	// whose own tenant doesn't match the requesting tenant is exactly the
	// cross-tenant leakage ADR-0005's separation exists to prevent, and
	// must fail closed rather than silently route through.
	if mappingResult.Mapping.TenantID != req.Context.TenantID {
		return ResolutionResult{}, resolutionFailure(trace, errors.New("mapping tenant does not match request tenant"))
	}
	trace.MappingID = mappingResult.Mapping.ID

	if req.Grants != nil {
		entitlementResult, err := EntitlementResolverImpl{}.Resolve(ctx, EntitlementResolutionQuery{
			CapabilityKey: req.CapabilityKey,
			Context:       req.Context,
			Grants:        req.Grants,
			Scopes:        req.Scopes,
		})
		if err != nil {
			return ResolutionResult{}, resolutionFailure(trace, err)
		}
		if !entitlementResult.Entitled {
			return ResolutionResult{}, resolutionFailure(trace, errors.New("capability not granted"))
		}
		trace.GrantID = entitlementResult.GrantID
	}

	capabilityResult, err := CapabilityResolverImpl{}.Resolve(ctx, CapabilityResolutionQuery{
		CapabilityKey: req.CapabilityKey,
		Context:       req.Context,
		Bindings:      req.Bindings,
		Scopes:        req.Scopes,
		Capability:    req.Capability,
	})
	if err != nil {
		return ResolutionResult{}, resolutionFailure(trace, err)
	}
	trace.BindingID = capabilityResult.BindingID
	trace.EngineInstanceID = capabilityResult.EngineInstanceID

	topologyResult, err := TopologyResolverImpl{}.Resolve(ctx, TopologyResolutionQuery{
		Context:                  req.Context,
		SelectedEngineInstanceID: capabilityResult.EngineInstanceID,
		EngineInstances:          req.EngineInstances,
		At:                       req.Context.ResolvedAt,
	})
	if err != nil {
		return ResolutionResult{}, resolutionFailure(trace, err)
	}

	policyResult := PolicyChecker{}.Check(ctx, req.Context, CapabilityBinding{
		CapabilityKey:    capabilityResult.CapabilityKey,
		EngineID:         capabilityResult.EngineID,
		EngineInstanceID: capabilityResult.EngineInstanceID,
		BindingMode:      capabilitydomain.BindingMode(capabilityResult.BindingMode),
		Status:           "ACTIVE",
		ContractVersion:  capabilityResult.ContractVersion,
	})
	if !policyResult.Allowed {
		return ResolutionResult{}, resolutionFailure(trace, errors.New(policyResult.Reason))
	}
	trace.Outcome = "ROUTED"
	trace.Reason = "active mapping and binding selected; policy allowed; engine instance eligible"

	return ResolutionResult{
		Context:    req.Context,
		Mapping:    mappingResult,
		Capability: capabilityResult,
		Policy:     policyResult,
		Topology:   topologyResult,
		Trace:      trace,
	}, nil
}
