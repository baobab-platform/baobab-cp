package administration

import "slices"

// Contains reports whether outer reaches every resource inner reaches
// (ADR-BCP-020 sections 43-44: delegated authority is a subset of the
// delegator's). It is the exact converse of how Evaluate decides coverage:
// it asks, for each kind of scope, whether outer.Covers(r) holds for every
// resource r that inner.Covers(r) holds for.
//
// Only containment the scopes themselves state is proven, from their own
// anchors, environment, market qualifiers and corporate-group membership
// lists. Nothing is inferred from identifiers (no prefix or naming guess)
// and no organisation-to-tenant or group-descendant relation is assumed,
// because Evaluate does not follow one: an ORGANISATION scope reaches
// resources that name that organisation, not the tenants it owns, and a
// DYNAMIC_GROUP_DESCENDANTS scope's members are not known to a scope on
// its own. Where containment cannot be proven the answer is false, so a
// delegation is refused rather than widened.
func Contains(outer, inner Scope) bool {
	// An environment-less scope reaches every environment; a named one only
	// itself.
	if outer.Environment != "" && outer.Environment != inner.Environment {
		return false
	}
	if outer.Level == LevelPlatform {
		return true
	}
	switch outer.Level {
	case LevelCorporateGroup:
		return containsGroup(outer, inner)
	case LevelMarket:
		return inner.Level == LevelMarket && inner.MarketID == outer.MarketID &&
			(outer.OrganisationID == "" || inner.OrganisationID == outer.OrganisationID) &&
			(outer.TenantID == "" || inner.TenantID == outer.TenantID)
	}
	if inner.Level != outer.Level {
		return false
	}
	switch outer.Level {
	case LevelPlatformAccount:
		return inner.PlatformAccountID == outer.PlatformAccountID
	case LevelOrganisation:
		return inner.OrganisationID == outer.OrganisationID
	case LevelTenant:
		return inner.TenantID == outer.TenantID
	case LevelLegalEntity:
		return inner.LegalEntityID == outer.LegalEntityID
	case LevelDigitalEstate:
		return inner.DigitalEstateID == outer.DigitalEstateID
	case LevelResource:
		return inner.ResourceType == outer.ResourceType && inner.ResourceID == outer.ResourceID
	}
	return false
}

func containsGroup(outer, inner Scope) bool {
	mode := func(s Scope) ScopeMode {
		if s.Mode == "" {
			return ModeExact
		}
		return s.Mode
	}
	switch mode(outer) {
	case ModeStaticMembership:
		switch {
		case inner.Level == LevelOrganisation:
			return slices.Contains(outer.OrganisationIDs, inner.OrganisationID)
		case inner.Level == LevelCorporateGroup && mode(inner) == ModeStaticMembership && inner.CorporateGroupID == outer.CorporateGroupID:
			for _, o := range inner.OrganisationIDs {
				if !slices.Contains(outer.OrganisationIDs, o) {
					return false
				}
			}
			return true
		}
	case ModeDynamicGroupDescendants:
		// Reaches the group itself and everything that currently belongs to it.
		return inner.Level == LevelCorporateGroup && inner.CorporateGroupID == outer.CorporateGroupID &&
			(mode(inner) == ModeExact || mode(inner) == ModeDynamicGroupDescendants)
	default:
		return inner.Level == LevelCorporateGroup && mode(inner) == ModeExact && inner.CorporateGroupID == outer.CorporateGroupID
	}
	return false
}
