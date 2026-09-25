package resolver

import (
	"context"
	"errors"
	"sort"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// Context represents a trusted runtime context produced by the resolver layer.
type Context = domain.Context

// TrustLevel identifies the trust level of the source evidence used to derive a context value.
type TrustLevel = domain.TrustLevel

const (
	TrustUntrusted  = domain.TrustUntrusted
	TrustVerified   = domain.TrustVerified
	TrustAuthorised = domain.TrustAuthorised
	TrustSystem     = domain.TrustSystem
)

type ContextSource = domain.ContextSource

// ResolutionEvidence is the input used by the resolver to derive runtime context.
type ResolutionEvidence struct {
	PrincipalID    string
	TenantID       string
	LegalEntityID  string
	OrganisationID string
	BusinessUnitID string
	MarketID       string
	CountryCode    string
	CurrencyCode   string
	Locale         string
	CorrelationID  string
	Provenance     map[string]ContextSource
	// TTL, when positive, bounds the resolved Context's lifetime
	// (ADR-BCP-004 §72). Zero (the default) leaves it unbounded, matching
	// this resolver's behavior before ExpiresAt existed.
	TTL time.Duration
}

// ContextResolverImpl resolves runtime context using supplied evidence and trust metadata.
type ContextResolverImpl struct{}

func (ContextResolverImpl) Resolve(ctx context.Context, evidence ResolutionEvidence) (Context, error) {
	if ctx == nil {
		return Context{}, errors.New("context is required")
	}
	if evidence.TenantID == "" {
		return Context{}, errors.New("tenant_id is required")
	}
	if evidence.PrincipalID == "" || evidence.CorrelationID == "" {
		return Context{}, errors.New("principal_id and correlation_id are required")
	}
	if evidence.Provenance == nil {
		evidence.Provenance = map[string]ContextSource{}
	}
	for key, value := range evidence.Provenance {
		if value.TrustLevel == "" {
			value.TrustLevel = TrustUntrusted
			evidence.Provenance[key] = value
		}
	}
	resolvedAt := time.Now().UTC()
	resolved := Context{
		// ADR-BCP-004 §70: every successfully resolved context SHALL
		// receive a context_id.
		ID:             domain.NewUUIDv7(),
		PrincipalID:    evidence.PrincipalID,
		TenantID:       evidence.TenantID,
		LegalEntityID:  evidence.LegalEntityID,
		OrganisationID: evidence.OrganisationID,
		BusinessUnitID: evidence.BusinessUnitID,
		MarketID:       evidence.MarketID,
		CountryCode:    evidence.CountryCode,
		CurrencyCode:   evidence.CurrencyCode,
		Locale:         evidence.Locale,
		CorrelationID:  evidence.CorrelationID,
		ResolvedAt:     resolvedAt,
		Provenance:     evidence.Provenance,
	}
	// §72: "Context MAY have bounded lifetime." TTL is opt-in per request
	// via evidence; zero (the default) leaves ExpiresAt unset, matching
	// this function's prior unbounded behavior exactly.
	if evidence.TTL > 0 {
		expiresAt := resolvedAt.Add(evidence.TTL)
		resolved.ExpiresAt = &expiresAt
	}
	if err := resolved.Validate(); err != nil {
		return Context{}, err
	}
	return resolved, nil
}

// ScopeMatch is the result of comparing a context against a scope.
type ScopeMatch struct {
	Compatible  bool
	Specificity int
	Matched     []string
	Inherited   []string
	RejectedBy  []string
}

// DefaultScopeMatcher is the default deterministic scope matcher.
type DefaultScopeMatcher struct{}

// Match compares a Context against a domain.MappingScope directly -- this
// used to take a package-local ScopeValues struct that duplicated
// domain.MappingScope's own dimension fields field-for-field, requiring a
// manual conversion at every call site (audit finding: "Runtime resolver
// types duplicate canonical concepts", docs/reconciliation/
// platform-resolution-spine-audit.md Gate 1). domain.MappingScope is the
// one canonical model for this concept; ID and EntityType (the two fields
// ScopeValues didn't carry) are simply unused by the matcher below.
func (DefaultScopeMatcher) Match(ctx Context, scope domain.MappingScope) ScopeMatch {
	matched := []string{}
	inherited := []string{}
	rejectedBy := []string{}
	specificity := 0

	matchChecks := []struct {
		name  string
		value string
		want  string
	}{
		{name: "tenant", value: ctx.TenantID, want: scope.TenantID},
		{name: "legal_entity", value: ctx.LegalEntityID, want: scope.LegalEntityID},
		{name: "organisation", value: ctx.OrganisationID, want: scope.OrganisationID},
		{name: "business_unit", value: ctx.BusinessUnitID, want: scope.BusinessUnitID},
		{name: "market", value: ctx.MarketID, want: scope.MarketID},
		{name: "country", value: ctx.CountryCode, want: scope.Country},
		{name: "digital_estate", value: ctx.DigitalEstateID, want: scope.EstateID},
		{name: "digital_property", value: ctx.DigitalPropertyID, want: scope.DigitalPropertyID},
		{name: "channel", value: ctx.ChannelID, want: scope.ChannelID},
		{name: "currency", value: ctx.CurrencyCode, want: scope.Currency},
		{name: "locale", value: ctx.Locale, want: scope.Locale},
		{name: "environment", value: ctx.Environment, want: scope.Environment},
	}

	for _, check := range matchChecks {
		if check.want == "" {
			continue
		}
		if check.value == check.want {
			matched = append(matched, check.name)
			specificity++
			continue
		}
		if check.value != "" && check.value != check.want {
			rejectedBy = append(rejectedBy, check.name)
			continue
		}
		inherited = append(inherited, check.name)
	}

	compatible := len(rejectedBy) == 0
	if compatible && len(matched) == 0 && len(inherited) == 0 {
		compatibility := ScopeMatch{Compatible: true, Specificity: 0, Matched: matched, Inherited: inherited, RejectedBy: rejectedBy}
		return compatibility
	}

	sort.Strings(matched)
	sort.Strings(inherited)
	sort.Strings(rejectedBy)

	return ScopeMatch{Compatible: compatible, Specificity: specificity, Matched: matched, Inherited: inherited, RejectedBy: rejectedBy}
}

// CapabilityScopeMatcher is the deterministic scope matcher for
// CapabilityBinding resolution. It is a distinct type from
// DefaultScopeMatcher, not an overload of it: capabilitydomain.CapabilityScope
// is deliberately not domain.MappingScope (ADR-SHARED-007 §25) -- the two
// share dimension vocabulary but are evaluated by different resolvers for
// different purposes, and merging them back into one matcher would
// reintroduce the conflation the ADR exists to prevent.
type CapabilityScopeMatcher struct{}

// Match compares a Context against a capabilitydomain.CapabilityScope.
// Dimensions CapabilityScope carries that Context has no corresponding
// field for yet (CustomerSegmentID, CatalogueID, OperatingRegionID,
// GeographicRegionID) are treated the same as any other value Context
// cannot currently supply: "inherited", not rejected -- consistent with
// every other unset-context-value case below, not a special case invented
// for this matcher.
func (CapabilityScopeMatcher) Match(ctx Context, scope capabilitydomain.CapabilityScope) ScopeMatch {
	matched := []string{}
	inherited := []string{}
	rejectedBy := []string{}
	specificity := 0

	matchChecks := []struct {
		name  string
		value string
		want  string
	}{
		{name: "tenant", value: ctx.TenantID, want: scope.TenantID},
		{name: "legal_entity", value: ctx.LegalEntityID, want: scope.LegalEntityID},
		{name: "organisation", value: ctx.OrganisationID, want: scope.OrganisationID},
		{name: "business_unit", value: ctx.BusinessUnitID, want: scope.BusinessUnitID},
		{name: "digital_estate", value: ctx.DigitalEstateID, want: scope.DigitalEstateID},
		{name: "digital_property", value: ctx.DigitalPropertyID, want: scope.DigitalPropertyID},
		{name: "channel", value: ctx.ChannelID, want: scope.ChannelID},
		{name: "market", value: ctx.MarketID, want: scope.MarketID},
		{name: "jurisdiction", value: ctx.Jurisdiction, want: scope.Jurisdiction},
		{name: "currency", value: ctx.CurrencyCode, want: scope.CurrencyCode},
		{name: "customer_segment", value: "", want: scope.CustomerSegmentID},
		{name: "catalogue", value: "", want: scope.CatalogueID},
		{name: "operating_region", value: "", want: scope.OperatingRegionID},
		{name: "geographic_region", value: "", want: scope.GeographicRegionID},
		{name: "deployment_region", value: ctx.DeploymentRegion, want: scope.DeploymentRegion},
		{name: "environment", value: ctx.Environment, want: scope.Environment},
		{name: "isolation_profile", value: ctx.IsolationProfileID, want: scope.IsolationProfileID},
	}

	for _, check := range matchChecks {
		if check.want == "" {
			continue
		}
		if check.value == check.want {
			matched = append(matched, check.name)
			specificity++
			continue
		}
		if check.value != "" && check.value != check.want {
			rejectedBy = append(rejectedBy, check.name)
			continue
		}
		inherited = append(inherited, check.name)
	}

	// §15 (mirrored in CapabilityScope.Validate): exclusion always takes
	// precedence over inclusion. A country Context does carry evidence for
	// (ctx.CountryCode set) is rejected if excluded, or if an inclusion
	// list exists and does not name it. An unset ctx.CountryCode cannot
	// violate either list -- there is nothing to check it against -- so it
	// is treated as "inherited", the same as any other dimension Context
	// has no value for.
	if ctx.CountryCode != "" {
		excluded := false
		for _, country := range scope.ExcludeCountries {
			if country == ctx.CountryCode {
				excluded = true
				break
			}
		}
		if excluded {
			rejectedBy = append(rejectedBy, "country")
		} else if len(scope.IncludeCountries) > 0 {
			included := false
			for _, country := range scope.IncludeCountries {
				if country == ctx.CountryCode {
					included = true
					break
				}
			}
			if included {
				matched = append(matched, "country")
				specificity++
			} else {
				rejectedBy = append(rejectedBy, "country")
			}
		}
	} else if len(scope.IncludeCountries) > 0 || len(scope.ExcludeCountries) > 0 {
		inherited = append(inherited, "country")
	}

	compatible := len(rejectedBy) == 0
	if compatible && len(matched) == 0 && len(inherited) == 0 {
		return ScopeMatch{Compatible: true, Specificity: 0, Matched: matched, Inherited: inherited, RejectedBy: rejectedBy}
	}

	sort.Strings(matched)
	sort.Strings(inherited)
	sort.Strings(rejectedBy)

	return ScopeMatch{Compatible: compatible, Specificity: specificity, Matched: matched, Inherited: inherited, RejectedBy: rejectedBy}
}
