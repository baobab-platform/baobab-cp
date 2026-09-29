package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
)

// Capability resolution decisions (Shared capability/v1 domain.schema.json
// capabilityResolutionDecision).
const (
	DecisionResolved     = "RESOLVED"
	DecisionDenied       = "DENIED"
	DecisionUnavailable  = "UNAVAILABLE"
	DecisionAmbiguous    = "AMBIGUOUS"
	DecisionIncompatible = "INCOMPATIBLE"
)

// CapabilityResolutionStore is what capability resolution reads, and where
// every decision is recorded.
type CapabilityResolutionStore interface {
	GetCapability(ctx context.Context, capabilityKey string) (capabilitydomain.Capability, error)
	ListGrants(ctx context.Context, tenantID, capabilityKey string) ([]capabilitydomain.CapabilityGrant, error)
	GetCapabilityScope(ctx context.Context, scopeID string) (capabilitydomain.CapabilityScope, error)
	ListBindings(ctx context.Context, capabilityKey string) ([]resolver.CapabilityBinding, error)
	ListActiveInstances(ctx context.Context, engineID string) ([]resolver.EngineInstance, error)
	HealthLevels(ctx context.Context, engineInstanceID, providerID, capabilityKey string) (health.Levels, error)
	ProviderInvocationByID(ctx context.Context, providerID string) (repository.ProviderInvocation, bool, error)
	RecordCapabilityResolution(ctx context.Context, r repository.CapabilityResolutionRecord) error
}

// CapabilityResolutionRequest is one capability to decide for a resolved,
// tenant-validated context.
type CapabilityResolutionRequest struct {
	Context                 domain.Context
	CapabilityKey           string
	RequiredContractVersion int
	CorrelationID           string
}

// CapabilityResolutionService decides capability/v1 resolutions: the
// capability must be registered and resolvable, an effective grant must
// cover the context, one binding must win by scope, mode, priority and
// health, the resolution policy must hold, and the binding's provider must
// have registered how it is invoked. Every decision is recorded.
type CapabilityResolutionService struct {
	Store CapabilityResolutionStore
	Now   func() time.Time
}

func (s CapabilityResolutionService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Resolve decides one capability and records the decision. An error means
// the Control Plane could not decide or record, never a denial.
func (s CapabilityResolutionService) Resolve(ctx context.Context, req CapabilityResolutionRequest) (repository.CapabilityResolutionRecord, error) {
	now := s.now()
	rec := repository.CapabilityResolutionRecord{
		ResolutionID: "res_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", ""), ContextID: req.Context.ID,
		TenantID: req.Context.TenantID, CapabilityKey: req.CapabilityKey, CorrelationID: req.CorrelationID, ResolvedAt: now,
	}
	if err := s.decide(ctx, req, now, &rec); err != nil {
		return repository.CapabilityResolutionRecord{}, err
	}
	if req.Context.ExpiresAt != nil && (rec.ExpiresAt == nil || req.Context.ExpiresAt.Before(*rec.ExpiresAt)) {
		expires := *req.Context.ExpiresAt
		rec.ExpiresAt = &expires
	}
	if err := s.Store.RecordCapabilityResolution(ctx, rec); err != nil {
		return repository.CapabilityResolutionRecord{}, fmt.Errorf("record capability resolution: %w", err)
	}
	return rec, nil
}

func decideAs(rec *repository.CapabilityResolutionRecord, decision, code string) error {
	rec.Decision, rec.ReasonCode = decision, code
	return nil
}

func (s CapabilityResolutionService) decide(ctx context.Context, req CapabilityResolutionRequest, now time.Time, rec *repository.CapabilityResolutionRecord) error {
	capability, err := s.Store.GetCapability(ctx, req.CapabilityKey)
	if errors.Is(err, repository.ErrCapabilityNotFound) {
		return decideAs(rec, DecisionDenied, "CAPABILITY_UNKNOWN")
	}
	if err != nil {
		return fmt.Errorf("load capability: %w", err)
	}
	if !capability.IsResolvable() {
		return decideAs(rec, DecisionUnavailable, "CAPABILITY_INACTIVE")
	}
	scopes := map[string]capabilitydomain.CapabilityScope{}
	scope := func(id string) error {
		if _, ok := scopes[id]; ok || id == "" {
			return nil
		}
		sc, err := s.Store.GetCapabilityScope(ctx, id)
		if err != nil {
			return fmt.Errorf("load capability scope: %w", err)
		}
		scopes[id] = sc
		return nil
	}

	// Entitlement: an effective grant whose scope covers the context.
	grants, err := s.Store.ListGrants(ctx, req.Context.TenantID, req.CapabilityKey)
	if err != nil {
		return fmt.Errorf("load capability grants: %w", err)
	}
	for _, g := range grants {
		if err := scope(g.ScopeID); err != nil {
			return err
		}
	}
	entitlement, err := resolver.EntitlementResolverImpl{}.Resolve(ctx, resolver.EntitlementResolutionQuery{
		CapabilityKey: req.CapabilityKey, Context: req.Context, Grants: grants, Scopes: scopes, At: now})
	if err != nil {
		return fmt.Errorf("resolve entitlement: %w", err)
	}
	if !entitlement.Entitled {
		return decideAs(rec, DecisionDenied, grantDenial(grants, scopes, req.Context, now))
	}
	rec.GrantID = entitlement.GrantID

	// Bindings the caller's contract version can use, judged by scope,
	// mode, priority and health.
	bindings, err := s.Store.ListBindings(ctx, req.CapabilityKey)
	if err != nil {
		return fmt.Errorf("load capability bindings: %w", err)
	}
	usable := bindings[:0:0]
	for _, b := range bindings {
		if req.RequiredContractVersion == 0 || contractMajor(b.ContractVersion) == req.RequiredContractVersion {
			usable = append(usable, b)
		}
	}
	if len(bindings) > 0 && len(usable) == 0 {
		return decideAs(rec, DecisionIncompatible, "CONTRACT_VERSION_UNSUPPORTED")
	}
	bindingScopes := map[string]capabilitydomain.CapabilityScope{}
	levels := make(map[string]health.Levels, len(usable))
	for _, b := range usable {
		if err := scope(b.ScopeID); err != nil {
			return err
		}
		if sc, ok := scopes[b.ScopeID]; ok {
			bindingScopes[b.ScopeID] = sc
		}
		l, err := s.Store.HealthLevels(ctx, b.EngineInstanceID, b.ProviderID, req.CapabilityKey)
		if err != nil {
			return fmt.Errorf("load binding health: %w", err)
		}
		levels[b.ID] = l
	}
	criticality := capability.HealthCriticality.OrDefault()
	chosen, err := resolver.ResolveHealthyCapability(ctx, resolver.CapabilityResolutionQuery{
		CapabilityKey: req.CapabilityKey, Context: req.Context, Bindings: usable, Scopes: bindingScopes, At: now,
	}, criticality, levels, now)
	var ineligible *health.IneligibleError
	switch {
	case errors.As(err, &ineligible):
		return decideAs(rec, DecisionUnavailable, ineligible.Decision.ReasonCode)
	case errors.Is(err, resolver.ErrBindingAmbiguous):
		return decideAs(rec, DecisionAmbiguous, "BINDING_AMBIGUOUS")
	case errors.Is(err, resolver.ErrBindingNotFound):
		return decideAs(rec, DecisionUnavailable, "BINDING_NOT_FOUND")
	case err != nil:
		return fmt.Errorf("resolve capability binding: %w", err)
	}
	rec.BindingID, rec.ProviderID, rec.EngineInstanceID = chosen.BindingID, chosen.ProviderID, chosen.EngineInstanceID
	rec.ContractVersion = contractMajor(chosen.ContractVersion)
	if rec.ContractVersion == 0 {
		// A binding with no readable contract version cannot tell the caller
		// which contract it serves.
		return decideAs(rec, DecisionIncompatible, "CONTRACT_VERSION_UNSUPPORTED")
	}

	// The selected engine instance is eligible for this context.
	instances, err := s.Store.ListActiveInstances(ctx, chosen.EngineID)
	if err != nil {
		return fmt.Errorf("load engine instances: %w", err)
	}
	if _, err := (resolver.TopologyResolverImpl{}).Resolve(ctx, resolver.TopologyResolutionQuery{
		Context: req.Context, SelectedEngineInstanceID: chosen.EngineInstanceID, EngineInstances: instances, At: now,
		HealthCriticality: criticality, Health: levels[chosen.BindingID], HealthAt: now,
	}); err != nil {
		if errors.As(err, &ineligible) {
			return decideAs(rec, DecisionUnavailable, ineligible.Decision.ReasonCode)
		}
		return decideAs(rec, DecisionUnavailable, "PROVIDER_UNAVAILABLE")
	}
	// Residency: the context must name the market it resolves for.
	if policy := (resolver.PolicyChecker{}).Check(ctx, req.Context, resolver.CapabilityBinding{
		CapabilityKey: chosen.CapabilityKey, EngineID: chosen.EngineID, EngineInstanceID: chosen.EngineInstanceID,
		BindingMode: capabilitydomain.BindingMode(chosen.BindingMode), Status: "ACTIVE", ContractVersion: chosen.ContractVersion,
	}); !policy.Allowed {
		return decideAs(rec, DecisionDenied, "RESIDENCY_POLICY_MISMATCH")
	}

	invocation, ok, err := s.Store.ProviderInvocationByID(ctx, chosen.ProviderID)
	if err != nil {
		return fmt.Errorf("load provider invocation: %w", err)
	}
	if !ok {
		return decideAs(rec, DecisionUnavailable, "PROVIDER_INVOCATION_UNDECLARED")
	}
	rec.Decision = DecisionResolved
	rec.ServiceReference, rec.Protocol = invocation.ServiceReference, invocation.Protocol
	if until := resolver.HealthValidUntil(levels[chosen.BindingID]); !until.IsZero() {
		rec.ExpiresAt = &until
	}
	return nil
}

// grantDenial names why no grant entitles the context: a grant covering it
// that is suspended, revoked or expired says so; otherwise there is none.
func grantDenial(grants []capabilitydomain.CapabilityGrant, scopes map[string]capabilitydomain.CapabilityScope, ctx domain.Context, now time.Time) string {
	code := "GRANT_NOT_FOUND"
	for _, g := range grants {
		sc, ok := scopes[g.ScopeID]
		if !ok || !resolver.ScopeCompatible(ctx, sc) {
			continue
		}
		switch {
		case g.Status == capabilitydomain.GrantStatusSuspended:
			return "GRANT_SUSPENDED"
		case g.Status == capabilitydomain.GrantStatusRevoked:
			code = "GRANT_REVOKED"
		case g.Status == capabilitydomain.GrantStatusExpired || (g.EffectiveTo != nil && !now.Before(*g.EffectiveTo)):
			if code == "GRANT_NOT_FOUND" {
				code = "GRANT_EXPIRED"
			}
		}
	}
	return code
}

// contractMajor reads a binding's contract version ("1", "v1", "1.2") as
// its major version; 0 when it has none.
func contractMajor(version string) int {
	v := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(version)), "v")
	if i := strings.IndexByte(v, '.'); i >= 0 {
		v = v[:i]
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0
	}
	return n
}
