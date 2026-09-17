// Target path: internal/provisioning/context_resolver.go
//
// ZB-02 authoritative Context composition. This layer gathers only trusted,
// server-side control-plane evidence and delegates immutable context creation
// to resolver.ContextResolverImpl. Leaf digital estates must not reconstruct
// tenant/legal-entity/market authority themselves.
package provisioning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/resolver"
)

// ContextAuthority is deliberately read-only. Context resolution consumes
// authoritative CP state; it does not mutate tenancy or market participation.
type ContextAuthority interface {
	GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error)
	GetMarket(ctx context.Context, marketID string) (domain.Market, error)
	ListMarketAssignments(ctx context.Context, tenantID string) ([]domain.MarketAssignment, error)
	GetDigitalEstate(ctx context.Context, estateID string) (domain.DigitalEstate, error)
	GetIsolationProfile(ctx context.Context, profileID string) (domain.IsolationProfile, error)
}

// ContextResolutionRequest carries caller identity plus identifiers whose
// authority must be verified by CP before entering Platform Context.
type ContextResolutionRequest struct {
	PrincipalID       string
	TenantID          string
	LegalEntityID     string
	MarketID          string
	DigitalEstateID   string
	DeploymentRegion  string
	Environment       string
	IsolationProfileID string
	CorrelationID     string
	TTL               time.Duration
}

// AuthoritativeContextResolver composes a trusted Context from CP resources.
type AuthoritativeContextResolver struct {
	authority ContextAuthority
	now       func() time.Time
}

func NewAuthoritativeContextResolver(authority ContextAuthority) *AuthoritativeContextResolver {
	return &AuthoritativeContextResolver{
		authority: authority,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (r *AuthoritativeContextResolver) Resolve(ctx context.Context, req ContextResolutionRequest) (resolver.Context, error) {
	if r == nil || r.authority == nil {
		return resolver.Context{}, errors.New("context authority is not initialized")
	}
	if req.PrincipalID == "" || req.TenantID == "" || req.CorrelationID == "" {
		return resolver.Context{}, errors.New("principal_id, tenant_id and correlation_id are required")
	}

	tenant, err := r.authority.GetTenant(ctx, req.TenantID)
	if err != nil {
		return resolver.Context{}, fmt.Errorf("resolve tenant: %w", err)
	}
	if tenant.ID != req.TenantID {
		return resolver.Context{}, errors.New("tenant authority returned a different tenant")
	}

	evidence := resolver.ResolutionEvidence{
		PrincipalID:   req.PrincipalID,
		TenantID:      req.TenantID,
		LegalEntityID: req.LegalEntityID,
		CorrelationID: req.CorrelationID,
		TTL:           req.TTL,
		Provenance: map[string]resolver.ContextSource{
			"tenant_id": {
				Source: "baobab-cp:tenant-registry", TrustLevel: resolver.TrustSystem,
				Evidence: req.TenantID,
			},
			"principal_id": {
				Source: "baobab-iam:verified-principal", TrustLevel: resolver.TrustAuthorised,
				Evidence: req.PrincipalID,
			},
		},
	}

	if req.LegalEntityID != "" {
		if tenant.LegalEntityID != req.LegalEntityID {
			return resolver.Context{}, errors.New("requested legal entity is not the tenant legal entity")
		}
		evidence.Provenance["legal_entity_id"] = resolver.ContextSource{
			Source: "baobab-cp:tenant-registry", TrustLevel: resolver.TrustSystem,
			Evidence: req.LegalEntityID,
		}
	}

	if req.MarketID != "" {
		market, err := r.authority.GetMarket(ctx, req.MarketID)
		if err != nil {
			return resolver.Context{}, fmt.Errorf("resolve market: %w", err)
		}
		if !market.IsActive {
			return resolver.Context{}, errors.New("requested market is inactive")
		}
		if err := r.requireMarketParticipation(ctx, req, r.now()); err != nil {
			return resolver.Context{}, err
		}
		evidence.MarketID = market.ID
		evidence.CurrencyCode = market.Currency
		evidence.Provenance["market_id"] = resolver.ContextSource{
			Source: "baobab-cp:market-registry", TrustLevel: resolver.TrustSystem,
			Evidence: market.ID,
		}
		evidence.Provenance["currency_code"] = resolver.ContextSource{
			Source: "baobab-cp:market-registry", TrustLevel: resolver.TrustSystem,
			Evidence: market.Currency,
		}
	}

	resolved, err := (resolver.ContextResolverImpl{}).Resolve(ctx, evidence)
	if err != nil {
		return resolver.Context{}, err
	}

	// These dimensions are verified below but are not accepted by the current
	// ResolutionEvidence DTO. Populate them only after authoritative checks.
	if req.DigitalEstateID != "" {
		estate, err := r.authority.GetDigitalEstate(ctx, req.DigitalEstateID)
		if err != nil {
			return resolver.Context{}, fmt.Errorf("resolve digital estate: %w", err)
		}
		if estate.TenantID != req.TenantID {
			return resolver.Context{}, errors.New("digital estate tenant mismatch")
		}
		resolved.DigitalEstateID = estate.ID
		resolved.Provenance["digital_estate_id"] = resolver.ContextSource{
			Source: "baobab-cp:digital-estate-registry", TrustLevel: resolver.TrustSystem,
			Evidence: estate.ID,
		}
	}

	if req.IsolationProfileID != "" {
		profile, err := r.authority.GetIsolationProfile(ctx, req.IsolationProfileID)
		if err != nil {
			return resolver.Context{}, fmt.Errorf("resolve isolation profile: %w", err)
		}
		resolved.IsolationProfileID = profile.ID
		resolved.Provenance["isolation_profile_id"] = resolver.ContextSource{
			Source: "baobab-cp:isolation-registry", TrustLevel: resolver.TrustSystem,
			Evidence: profile.ID,
		}
	}

	resolved.DeploymentRegion = req.DeploymentRegion
	resolved.Environment = req.Environment
	if req.DeploymentRegion != "" {
		resolved.Provenance["deployment_region"] = resolver.ContextSource{
			Source: "baobab-cp:runtime-policy", TrustLevel: resolver.TrustSystem,
			Evidence: req.DeploymentRegion,
		}
	}
	if req.Environment != "" {
		resolved.Provenance["environment"] = resolver.ContextSource{
			Source: "baobab-cp:runtime-policy", TrustLevel: resolver.TrustSystem,
			Evidence: req.Environment,
		}
	}
	if err := resolved.Validate(); err != nil {
		return resolver.Context{}, err
	}
	return resolved, nil
}

func (r *AuthoritativeContextResolver) requireMarketParticipation(
	ctx context.Context,
	req ContextResolutionRequest,
	at time.Time,
) error {
	assignments, err := r.authority.ListMarketAssignments(ctx, req.TenantID)
	if err != nil {
		return fmt.Errorf("list market participation: %w", err)
	}
	for _, a := range assignments {
		if a.TenantID != req.TenantID || a.MarketID != req.MarketID {
			continue
		}
		if req.LegalEntityID != "" && a.LegalEntityID != req.LegalEntityID {
			continue
		}
		if at.Before(a.EffectiveFrom) {
			continue
		}
		if a.EffectiveTo != nil && !at.Before(*a.EffectiveTo) {
			continue
		}
		return nil
	}
	return errors.New("no effective market participation authorises requested context")
}
