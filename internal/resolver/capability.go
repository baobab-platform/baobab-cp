package resolver

import (
	"context"
	"errors"
	"sort"
	"time"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
)

// CapabilityBinding represents the effective binding between a capability and a runtime engine instance.
type CapabilityBinding = capabilitydomain.CapabilityBinding

// CapabilityResolutionQuery resolves a capability in the current trusted context.
type CapabilityResolutionQuery struct {
	CapabilityKey string
	Context       Context
	Bindings      []CapabilityBinding
	// Scopes is keyed by CapabilityBinding.ScopeID and evaluated by
	// CapabilityScopeMatcher, not DefaultScopeMatcher -- CapabilityScope is
	// deliberately distinct from domain.MappingScope (ADR-SHARED-007 §25,
	// mirrored in capabilitydomain.CapabilityScope's own doc comment): the
	// two share dimension vocabulary but are evaluated by different
	// resolvers for different purposes. Gate P0's classification
	// (docs/reconciliation/phase-0-architecture-inventory-and-lock.md,
	// tracked as #74) found capability_binding.scope_id still pointing at
	// mapping.mapping_scope in the database and this query still typed
	// against domain.MappingScope in Go -- both corrected together.
	Scopes map[string]capabilitydomain.CapabilityScope
	At     time.Time
	// Capability, when supplied, gates resolution on the capability's own
	// lifecycle eligibility (ADR-BCP-003 §6: only ACTIVE capabilities
	// SHALL resolve by default). Nil by default and skipped when nil --
	// no caller populates the Capability registry from real callers yet,
	// so unconditionally enforcing this would fail every resolution today.
	// Mirrors EntitlementResolutionQuery's nil-means-skip Grants field.
	Capability *capabilitydomain.Capability
}

// ResolvedCapability is the selected capability binding and target engine.
type ResolvedCapability struct {
	BindingID        string
	CapabilityKey    string
	BindingMode      string
	EngineID         string
	EngineInstanceID string
	ContractVersion  string
	Specificity      int
}

// CapabilityResolverImpl resolves a capability to the highest-priority active binding.
type CapabilityResolverImpl struct{}

func (CapabilityResolverImpl) Resolve(_ context.Context, q CapabilityResolutionQuery) (ResolvedCapability, error) {
	if q.CapabilityKey == "" {
		return ResolvedCapability{}, errors.New("capability key is required")
	}
	if q.Capability != nil && !q.Capability.IsResolvable() {
		return ResolvedCapability{}, errors.New("capability is not in a resolvable lifecycle state")
	}
	if len(q.Bindings) == 0 {
		return ResolvedCapability{}, errors.New("capability not found")
	}

	at := q.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	type rankedBinding struct {
		binding     CapabilityBinding
		specificity int
	}
	active := make([]rankedBinding, 0, len(q.Bindings))
	for _, b := range q.Bindings {
		if b.CapabilityKey != q.CapabilityKey {
			continue
		}
		if b.Status != "ACTIVE" {
			continue
		}
		// DISABLED bindings are excluded from resolution candidates
		// entirely, not merely deprioritised (capabilitydomain.BindingModeDisabled).
		if b.BindingMode == capabilitydomain.BindingModeDisabled {
			continue
		}
		if !b.EffectiveFrom.IsZero() && at.Before(b.EffectiveFrom) {
			continue
		}
		if b.EffectiveTo != nil && !at.Before(*b.EffectiveTo) {
			continue
		}
		specificity := 0
		if len(q.Scopes) > 0 {
			scope, ok := q.Scopes[b.ScopeID]
			if !ok {
				continue
			}
			match := CapabilityScopeMatcher{}.Match(q.Context, scope)
			if !match.Compatible {
				continue
			}
			specificity = match.Specificity
		}
		active = append(active, rankedBinding{binding: b, specificity: specificity})
	}
	if len(active) == 0 {
		return ResolvedCapability{}, errors.New("capability not found")
	}

	// Tie-break order follows nabhold/shared's canonical
	// contracts/capability/v1/scope-specificity.yaml exactly: specificity,
	// then binding_mode preference, then explicit priority (ADR-BCP-003
	// SS16-19's "Priority SHALL NOT casually override scope specificity" and
	// its recommended evaluation sequence place binding mode ahead of
	// priority -- priority is an administrative override of last resort
	// among candidates still tied after mode, never a substitute for it).
	sort.Slice(active, func(i, j int) bool {
		if active[i].specificity != active[j].specificity {
			return active[i].specificity > active[j].specificity
		}
		if rankI, rankJ := bindingModeRank(active[i].binding.BindingMode), bindingModeRank(active[j].binding.BindingMode); rankI != rankJ {
			return rankI > rankJ
		}
		if active[i].binding.Priority != active[j].binding.Priority {
			return active[i].binding.Priority > active[j].binding.Priority
		}
		return active[i].binding.ID < active[j].binding.ID
	})

	if len(active) > 1 && active[0].specificity == active[1].specificity &&
		bindingModeRank(active[0].binding.BindingMode) == bindingModeRank(active[1].binding.BindingMode) &&
		active[0].binding.Priority == active[1].binding.Priority {
		return ResolvedCapability{}, errors.New("capability binding is ambiguous")
	}
	chosen := active[0]
	// A SHADOW binding is non-authoritative: even when it wins ranking (no
	// PRIMARY/FALLBACK/MIGRATION candidate is eligible), it SHALL NOT be
	// returned as a resolution result -- resolve as if no eligible binding
	// existed (capabilitydomain.BindingModeShadow; nabhold/shared's
	// scope-specificity.yaml "binding mode preference").
	if chosen.binding.BindingMode == capabilitydomain.BindingModeShadow {
		return ResolvedCapability{}, errors.New("capability not found")
	}
	return ResolvedCapability{
		BindingID:        chosen.binding.ID,
		CapabilityKey:    chosen.binding.CapabilityKey,
		BindingMode:      string(chosen.binding.BindingMode),
		EngineID:         chosen.binding.EngineID,
		EngineInstanceID: chosen.binding.EngineInstanceID,
		ContractVersion:  chosen.binding.ContractVersion,
		Specificity:      chosen.specificity,
	}, nil
}

// bindingModeRank orders binding modes by resolution preference (highest
// first): PRIMARY, FALLBACK, SHADOW, MIGRATION -- mirroring nabhold/shared's
// contracts/capability/v1/scope-specificity.yaml "binding_mode_preference".
// DISABLED is never ranked: it is filtered out of candidates before this is
// ever consulted.
func bindingModeRank(mode capabilitydomain.BindingMode) int {
	switch mode {
	case capabilitydomain.BindingModePrimary:
		return 4
	case capabilitydomain.BindingModeFallback:
		return 3
	case capabilitydomain.BindingModeShadow:
		return 2
	case capabilitydomain.BindingModeMigration:
		return 1
	default:
		return 0
	}
}
