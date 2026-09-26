package resolver

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

var ErrMappingAmbiguous = errors.New("mapping is ambiguous")
var ErrReverseMappingUnresolved = errors.New("reverse mapping unresolved")

type MappingResolutionQuery struct {
	CanonicalEntityID string
	Context           Context
	Candidates        []domain.Mapping
	Scopes            map[string]domain.MappingScope
	At                time.Time
}

type ReverseMappingResolutionQuery struct {
	ExternalReferenceID string
	Context             Context
	Candidates          []domain.Mapping
	Scopes              map[string]domain.MappingScope
	At                  time.Time
}

type ResolvedMapping struct {
	Mapping     domain.Mapping
	Specificity int
}

type QuarantineDecision struct {
	Quarantined         bool
	Reason              string
	ExternalReferenceID string
	CorrelationID       string
}

type MappingResolverImpl struct{}

func (MappingResolverImpl) Resolve(_ context.Context, q MappingResolutionQuery) (ResolvedMapping, error) {
	if q.CanonicalEntityID == "" {
		return ResolvedMapping{}, errors.New("canonical_entity_id is required")
	}
	return resolveMapping(q.Context, q.Candidates, q.Scopes, q.At, func(mapping domain.Mapping) bool {
		return mapping.CanonicalEntityID == q.CanonicalEntityID &&
			(mapping.Direction == "BIDIRECTIONAL" || mapping.Direction == "CANONICAL_TO_EXTERNAL" || mapping.Direction == "SOURCE_TO_TARGET")
	})
}

func (MappingResolverImpl) ResolveReverse(_ context.Context, q ReverseMappingResolutionQuery) (ResolvedMapping, QuarantineDecision, error) {
	if q.ExternalReferenceID == "" {
		return ResolvedMapping{}, QuarantineDecision{}, errors.New("external_reference_id is required")
	}
	resolved, err := resolveMapping(q.Context, q.Candidates, q.Scopes, q.At, func(mapping domain.Mapping) bool {
		return mapping.ExternalReferenceID == q.ExternalReferenceID &&
			(mapping.Direction == "BIDIRECTIONAL" || mapping.Direction == "EXTERNAL_TO_CANONICAL")
	})
	if err != nil {
		return ResolvedMapping{}, QuarantineDecision{Quarantined: true, Reason: err.Error(), ExternalReferenceID: q.ExternalReferenceID, CorrelationID: q.Context.CorrelationID}, ErrReverseMappingUnresolved
	}
	return resolved, QuarantineDecision{}, nil
}

type rankedMapping struct {
	mapping     domain.Mapping
	specificity int
}

func resolveMapping(ctx Context, candidates []domain.Mapping, scopes map[string]domain.MappingScope, at time.Time, matches func(domain.Mapping) bool) (ResolvedMapping, error) {
	if len(candidates) == 0 {
		return ResolvedMapping{}, errors.New("mapping not found")
	}
	if at.IsZero() {
		at = ctx.ResolvedAt
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	eligible := make([]rankedMapping, 0, len(candidates))
	for _, mapping := range candidates {
		if !matches(mapping) || mapping.Status != "ACTIVE" || mapping.Confidence == "REJECTED" {
			continue
		}
		if err := mapping.Validate(); err != nil {
			continue
		}
		from, _ := time.Parse(time.RFC3339, mapping.EffectiveFrom)
		if at.Before(from) {
			continue
		}
		if mapping.EffectiveTo != "" {
			to, _ := time.Parse(time.RFC3339, mapping.EffectiveTo)
			if !at.Before(to) {
				continue
			}
		}
		specificity := legacyScopeSpecificity(ctx, mapping.ScopeID)
		if len(scopes) > 0 {
			scope, ok := scopes[mapping.ScopeID]
			if !ok {
				continue
			}
			match := DefaultScopeMatcher{}.Match(ctx, scope)
			if !match.Compatible {
				continue
			}
			specificity = match.Specificity
		}
		eligible = append(eligible, rankedMapping{mapping: mapping, specificity: specificity})
	}
	if len(eligible) == 0 {
		return ResolvedMapping{}, errors.New("mapping not found")
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].specificity != eligible[j].specificity {
			return eligible[i].specificity > eligible[j].specificity
		}
		if eligible[i].mapping.ResolutionPriority != eligible[j].mapping.ResolutionPriority {
			return eligible[i].mapping.ResolutionPriority > eligible[j].mapping.ResolutionPriority
		}
		if confidenceRank(eligible[i].mapping.Confidence) != confidenceRank(eligible[j].mapping.Confidence) {
			return confidenceRank(eligible[i].mapping.Confidence) > confidenceRank(eligible[j].mapping.Confidence)
		}
		return eligible[i].mapping.ID < eligible[j].mapping.ID
	})
	if len(eligible) > 1 && sameMappingRank(eligible[0], eligible[1]) {
		return ResolvedMapping{}, ErrMappingAmbiguous
	}
	return ResolvedMapping{Mapping: eligible[0].mapping, Specificity: eligible[0].specificity}, nil
}

func sameMappingRank(left, right rankedMapping) bool {
	return left.specificity == right.specificity && left.mapping.ResolutionPriority == right.mapping.ResolutionPriority && confidenceRank(left.mapping.Confidence) == confidenceRank(right.mapping.Confidence)
}

func legacyScopeSpecificity(ctx Context, scopeID string) int {
	if scopeID == "" {
		return 0
	}
	switch scopeID {
	case ctx.TenantID:
		return 20
	case ctx.LegalEntityID:
		return 15
	case ctx.MarketID:
		return 10
	case ctx.CountryCode:
		return 5
	default:
		return 0
	}
}

func confidenceRank(confidence string) int {
	switch confidence {
	case "", "CONFIRMED":
		// A mapping recorded without a confidence ranks as confirmed, as
		// external reference resolution ranks it.
		return 4
	case "PROBABLE":
		return 3
	case "CANDIDATE":
		return 2
	case "REJECTED":
		return 1
	default:
		return 0
	}
}
