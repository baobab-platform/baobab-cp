// ZB-02 manifest resolution. TenantManifest (manifest.go) is desired state
// expressed with human-readable symbolic references (market codes,
// capability keys); ResolveManifest turns those into the canonical CP
// identifiers APPLY materializers and reconciliation actually operate on.
// It never mints canonical IDs itself -- every reference must already exist
// and be resolvable in the authoritative registries, or resolution fails
// closed.
package provisioning

import (
	"context"
	"fmt"
	"strings"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
)

// ManifestRegistry is the minimum read surface ResolveManifest needs.
// *repository.Repository and *repository.PostgresRepository both satisfy it
// already.
type ManifestRegistry interface {
	GetMarketByCode(ctx context.Context, code string) (domain.Market, error)
	GetCapability(ctx context.Context, capabilityKey string) (capabilitydomain.Capability, error)
}

// ResolvedMarket is a manifest market with its symbolic code resolved to a
// canonical Market.ID.
type ResolvedMarket struct {
	Code         string
	MarketID     string
	Capabilities []domain.MarketParticipationCapability
}

// ResolvedCapabilityGrant is a manifest capability grant ready for
// CapabilityGrantProvisioner. Scope is deliberately not carried here -- the
// composition root assigns the provisioning-run's scope, since the manifest
// format has no concept of CapabilityScope yet (tracked as a follow-up,
// mirroring this session's established "basics" scoping precedent).
type ResolvedCapabilityGrant struct {
	CapabilityKey   string
	MarketCode      string
	Source          capabilitydomain.GrantSource
	SourceReference string
}

// ResolvedCapabilityBinding is a manifest capability binding ready for
// CapabilityBindingProvisioner. Engine/EngineInstance are accepted as
// already-canonical topology.engine / topology.engine_instance identifiers
// -- resolving an engine *slug* vocabulary is out of this pass's scope (see
// manifest_loader's original design note in the ZB-02 scaffold).
type ResolvedCapabilityBinding struct {
	CapabilityKey    string
	MarketCode       string
	EngineID         string
	EngineInstanceID string
	Mode             capabilitydomain.BindingMode
	Priority         int
}

// ResolvedTradeLane is a manifest trade lane with origin/destination market
// codes resolved to canonical Market.IDs.
type ResolvedTradeLane struct {
	OriginMarketID          string
	DestinationMarketID     string
	Direction               domain.TradeLaneDirection
	PermittedCapabilityKeys []string
}

// ResolvedManifest is TenantManifest after every symbolic reference has been
// verified against authoritative CP registries. It is what APPLY
// materializers, reconciliation's desired readers, and readiness probes
// actually consume.
type ResolvedManifest struct {
	TenantID             string
	LegalEntityID        string
	DigitalEstate        string
	DesiredStateVersion  int64
	Markets              []ResolvedMarket
	CapabilityGrants     []ResolvedCapabilityGrant
	CapabilityBindings   []ResolvedCapabilityBinding
	TradeLanes           []ResolvedTradeLane
	IsolationRequirement string
	ResidencyRequirement string
}

// ResolveManifest validates m and resolves every symbolic reference it
// carries. It rejects unknown or inactive/non-resolvable references rather
// than silently dropping them (manifest_loader's original design note:
// "Reject unknown, inactive or cross-tenant references").
func ResolveManifest(ctx context.Context, registry ManifestRegistry, m TenantManifest) (ResolvedManifest, error) {
	if registry == nil {
		return ResolvedManifest{}, fmt.Errorf("manifest registry is required")
	}
	if err := m.Validate(); err != nil {
		return ResolvedManifest{}, err
	}

	resolved := ResolvedManifest{
		TenantID: m.Metadata.TenantID, LegalEntityID: m.Spec.LegalEntityID,
		DigitalEstate: m.Spec.DigitalEstate, DesiredStateVersion: m.Metadata.DesiredStateVersion,
		IsolationRequirement: m.Spec.IsolationRequirement, ResidencyRequirement: m.Spec.ResidencyRequirement,
	}

	marketIDByCode := make(map[string]string, len(m.Spec.Markets))
	for _, mk := range m.Spec.Markets {
		code := strings.ToUpper(strings.TrimSpace(mk.MarketCode))
		market, err := registry.GetMarketByCode(ctx, code)
		if err != nil {
			return ResolvedManifest{}, fmt.Errorf("resolve market %s: %w", code, err)
		}
		if !market.IsActive {
			return ResolvedManifest{}, fmt.Errorf("market %s is not active", code)
		}
		capabilities := make([]domain.MarketParticipationCapability, 0, len(mk.Capabilities))
		for _, raw := range mk.Capabilities {
			capability := domain.MarketParticipationCapability(raw)
			if !capability.Valid() {
				return ResolvedManifest{}, fmt.Errorf("market %s: unknown participation capability %q", code, raw)
			}
			capabilities = append(capabilities, capability)
		}
		marketIDByCode[code] = market.ID
		resolved.Markets = append(resolved.Markets, ResolvedMarket{Code: code, MarketID: market.ID, Capabilities: capabilities})
	}

	for _, g := range m.Spec.CapabilityGrants {
		capability, err := registry.GetCapability(ctx, g.CapabilityKey)
		if err != nil {
			return ResolvedManifest{}, fmt.Errorf("resolve capability %s: %w", g.CapabilityKey, err)
		}
		if !capability.IsResolvable() {
			return ResolvedManifest{}, fmt.Errorf("capability %s is not ACTIVE/resolvable", g.CapabilityKey)
		}
		source := capabilitydomain.GrantSource(g.Source)
		if !source.Valid() {
			return ResolvedManifest{}, fmt.Errorf("capability %s: unknown grant source %q", g.CapabilityKey, g.Source)
		}
		resolved.CapabilityGrants = append(resolved.CapabilityGrants, ResolvedCapabilityGrant{
			CapabilityKey: g.CapabilityKey, MarketCode: strings.ToUpper(strings.TrimSpace(g.MarketCode)),
			Source: source, SourceReference: g.SourceReference,
		})
	}

	for _, b := range m.Spec.CapabilityBindings {
		capability, err := registry.GetCapability(ctx, b.CapabilityKey)
		if err != nil {
			return ResolvedManifest{}, fmt.Errorf("resolve capability %s: %w", b.CapabilityKey, err)
		}
		if !capability.IsResolvable() {
			return ResolvedManifest{}, fmt.Errorf("capability %s is not ACTIVE/resolvable", b.CapabilityKey)
		}
		if strings.TrimSpace(b.Engine) == "" || strings.TrimSpace(b.EngineInstance) == "" {
			return ResolvedManifest{}, fmt.Errorf("capability %s: engine and engine_instance are required", b.CapabilityKey)
		}
		mode := capabilitydomain.BindingMode(b.Mode)
		if !mode.Valid() {
			return ResolvedManifest{}, fmt.Errorf("capability %s: unknown binding mode %q", b.CapabilityKey, b.Mode)
		}
		resolved.CapabilityBindings = append(resolved.CapabilityBindings, ResolvedCapabilityBinding{
			CapabilityKey: b.CapabilityKey, MarketCode: strings.ToUpper(strings.TrimSpace(b.MarketCode)),
			EngineID: b.Engine, EngineInstanceID: b.EngineInstance, Mode: mode, Priority: b.Priority,
		})
	}

	for _, l := range m.Spec.TradeLanes {
		originID, ok := marketIDByCode[strings.ToUpper(l.OriginMarket)]
		if !ok {
			return ResolvedManifest{}, fmt.Errorf("trade lane origin market %s is not declared in spec.markets", l.OriginMarket)
		}
		destinationID, ok := marketIDByCode[strings.ToUpper(l.DestinationMarket)]
		if !ok {
			return ResolvedManifest{}, fmt.Errorf("trade lane destination market %s is not declared in spec.markets", l.DestinationMarket)
		}
		direction := domain.TradeLaneDirection(l.Direction)
		if !direction.Valid() {
			return ResolvedManifest{}, fmt.Errorf("unknown trade lane direction %q", l.Direction)
		}
		resolved.TradeLanes = append(resolved.TradeLanes, ResolvedTradeLane{
			OriginMarketID: originID, DestinationMarketID: destinationID,
			Direction: direction, PermittedCapabilityKeys: l.PermittedCapabilityKeys,
		})
	}

	return resolved, nil
}
