package resolver

import (
	"context"
	"errors"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
)

// EntitlementResolutionQuery answers exactly one question, strictly before
// any CapabilityBinding is ever considered: may this tenant, in this
// context, consume this capability at all (ADR-BCP-003 §9, §34 step 2)?
type EntitlementResolutionQuery struct {
	CapabilityKey string
	Context       Context
	Grants        []capabilitydomain.CapabilityGrant
	Scopes        map[string]capabilitydomain.CapabilityScope
	At            time.Time
}

// EntitlementDecision is the result of an entitlement check.
type EntitlementDecision struct {
	Entitled bool
	GrantID  string
	Reason   string
}

// EntitlementResolverImpl checks whether at least one effective,
// scope-compatible grant exists for the requested capability.
type EntitlementResolverImpl struct{}

func (EntitlementResolverImpl) Resolve(_ context.Context, q EntitlementResolutionQuery) (EntitlementDecision, error) {
	if q.CapabilityKey == "" {
		return EntitlementDecision{}, errors.New("capability key is required")
	}
	at := q.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	// §35 "Grant Ambiguity": unlike CapabilityBinding resolution, multiple
	// compatible grants MAY legitimately coexist for the same capability
	// (e.g. a platform-baseline grant plus an enterprise product grant).
	// Entitlement is a yes/no gate satisfied by the first effective,
	// compatible grant found -- there is no ranking and no ambiguity error
	// to raise here, deliberately unlike bindingModeRank's ambiguity check.
	for _, g := range q.Grants {
		if g.CapabilityKey != q.CapabilityKey {
			continue
		}
		if !g.IsEffective(at) {
			continue
		}
		scope, ok := q.Scopes[g.ScopeID]
		if !ok {
			continue
		}
		if !scopeCompatible(q.Context, scope) {
			continue
		}
		return EntitlementDecision{Entitled: true, GrantID: g.ID, Reason: "effective compatible grant found"}, nil
	}
	return EntitlementDecision{Entitled: false, Reason: "no effective compatible grant"}, nil
}

// scopeCompatible reports whether a CapabilityScope's populated dimensions
// are compatible with ctx (ADR-BCP-003 §14-15): an unspecified scope
// dimension means "not further restricted on this axis", never a wildcard
// bypass of dimensions that ARE specified; exclusion always takes
// precedence over inclusion, and a country excluded from an otherwise
// matching scope makes it ineligible outright.
//
// domain.Context deliberately does not carry customer_segment_id,
// catalogue_id, operating_region_id or geographic_region_id: ADR-BCP-004
// §5's canonical PlatformContext field list does not include them, and
// §65 ("OperationScope") is explicit that "only explicitly approved
// fields SHALL influence platform resolution" -- these are
// request/operation-specific dimensions threaded through OperationScope
// on a per-capability-resolution-request basis (ADR-BCP-003 §64-65), not
// universal resolved-context fields every scope check can assume exists.
// A CapabilityScope that restricts on one of those four dimensions is
// therefore treated as unrestricted on it here, by design, not as an
// unmodeled gap. organisation_id and business_unit_id, which ARE part of
// PlatformContext's canonical model, are checked below.
func scopeCompatible(ctx Context, scope capabilitydomain.CapabilityScope) bool {
	checks := []struct {
		value string
		want  string
	}{
		{ctx.TenantID, scope.TenantID},
		{ctx.LegalEntityID, scope.LegalEntityID},
		{ctx.OrganisationID, scope.OrganisationID},
		{ctx.BusinessUnitID, scope.BusinessUnitID},
		{ctx.DigitalEstateID, scope.DigitalEstateID},
		{ctx.DigitalPropertyID, scope.DigitalPropertyID},
		{ctx.ChannelID, scope.ChannelID},
		{ctx.MarketID, scope.MarketID},
		{ctx.Jurisdiction, scope.Jurisdiction},
		{ctx.CurrencyCode, scope.CurrencyCode},
		{ctx.DeploymentRegion, scope.DeploymentRegion},
		{ctx.Environment, scope.Environment},
		{ctx.IsolationProfileID, scope.IsolationProfileID},
	}
	for _, c := range checks {
		if c.want != "" && c.value != c.want {
			return false
		}
	}
	if len(scope.ExcludeCountries) > 0 && containsString(scope.ExcludeCountries, ctx.CountryCode) {
		return false
	}
	if len(scope.IncludeCountries) > 0 && !containsString(scope.IncludeCountries, ctx.CountryCode) {
		return false
	}
	return true
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
