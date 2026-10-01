package administration

import (
	"testing"
	"time"
)

func TestContains(t *testing.T) {
	tenant := func(id, env string) Scope { return Scope{Level: LevelTenant, TenantID: id, Environment: env} }
	org := func(id string) Scope { return Scope{Level: LevelOrganisation, OrganisationID: id} }
	group := func(mode ScopeMode, orgs ...string) Scope {
		return Scope{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_acme", Mode: mode, OrganisationIDs: orgs}
	}
	market := func(org, tenant string) Scope {
		return Scope{Level: LevelMarket, MarketID: "mkt_ug", OrganisationID: org, TenantID: tenant}
	}
	for name, tc := range map[string]struct {
		outer, inner Scope
		want         bool
	}{
		"the same tenant":                         {tenant("tn_a", ""), tenant("tn_a", ""), true},
		"another tenant":                          {tenant("tn_a", ""), tenant("tn_b", ""), false},
		"an environment inside no environment":    {tenant("tn_a", ""), tenant("tn_a", "production"), true},
		"no environment inside an environment":    {tenant("tn_a", "production"), tenant("tn_a", ""), false},
		"another environment":                     {tenant("tn_a", "staging"), tenant("tn_a", "production"), false},
		"anything inside the platform":            {Scope{Level: LevelPlatform}, tenant("tn_a", "production"), true},
		"the platform inside a tenant":            {tenant("tn_a", ""), Scope{Level: LevelPlatform}, false},
		"a platform environment inside platform":  {Scope{Level: LevelPlatform}, Scope{Level: LevelPlatform, Environment: "staging"}, true},
		"a tenant inside its organisation":        {org("ORG-ACME"), tenant("tn_a", ""), false}, // no organisation-to-tenant relation is followed
		"an organisation inside a tenant":         {tenant("tn_a", ""), org("ORG-ACME"), false},
		"the same organisation":                   {org("ORG-ACME"), org("ORG-ACME"), true},
		"another organisation":                    {org("ORG-ACME"), org("ORG-OTHER"), false},
		"a listed organisation in a static group": {group(ModeStaticMembership, "ORG-A", "ORG-B"), org("ORG-A"), true},
		"an unlisted organisation":                {group(ModeStaticMembership, "ORG-A"), org("ORG-Z"), false},
		"a sub-list in a static group":            {group(ModeStaticMembership, "ORG-A", "ORG-B"), group(ModeStaticMembership, "ORG-A"), true},
		"a longer list":                           {group(ModeStaticMembership, "ORG-A"), group(ModeStaticMembership, "ORG-A", "ORG-B"), false},
		"another group":                           {group(ModeStaticMembership, "ORG-A"), Scope{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_other", Mode: ModeStaticMembership, OrganisationIDs: []string{"ORG-A"}}, false},
		"the group itself in a dynamic group":     {group(ModeDynamicGroupDescendants), group(ModeExact), true},
		"a dynamic group in itself":               {group(ModeDynamicGroupDescendants), group(ModeDynamicGroupDescendants), true},
		"an organisation in a dynamic group":      {group(ModeDynamicGroupDescendants), org("ORG-A"), false}, // membership is not known to a scope
		"a dynamic group in an exact group":       {group(ModeExact), group(ModeDynamicGroupDescendants), false},
		"an exact group in itself":                {group(""), group(ModeExact), true},
		"a static group in an exact group":        {group(ModeExact), group(ModeStaticMembership, "ORG-A"), false},
		"a qualified market inside the market":    {market("", ""), market("ORG-A", "tn_a"), true},
		"the market inside a qualified market":    {market("ORG-A", ""), market("", ""), false},
		"another organisation's market":           {market("ORG-A", ""), market("ORG-B", ""), false},
		"the same qualified market":               {market("ORG-A", "tn_a"), market("ORG-A", "tn_a"), true},
		"a resource inside itself":                {Scope{Level: LevelResource, ResourceType: "report", ResourceID: "r1"}, Scope{Level: LevelResource, ResourceType: "report", ResourceID: "r1"}, true},
		"another resource":                        {Scope{Level: LevelResource, ResourceType: "report", ResourceID: "r1"}, Scope{Level: LevelResource, ResourceType: "report", ResourceID: "r2"}, false},
		"a digital estate inside itself":          {Scope{Level: LevelDigitalEstate, DigitalEstateID: "de_1"}, Scope{Level: LevelDigitalEstate, DigitalEstateID: "de_1"}, true},
	} {
		if got := Contains(tc.outer, tc.inner); got != tc.want {
			t.Errorf("%s: Contains = %v, want %v", name, got, tc.want)
		}
	}
}

// Containment is the converse of coverage: whatever a contained scope
// reaches, its container reaches. Checked against Covers on a population of
// resources, so the two cannot drift apart.
func TestContainsAgreesWithCovers(t *testing.T) {
	scopes := []Scope{
		{Level: LevelPlatform}, {Level: LevelPlatform, Environment: "production"},
		{Level: LevelTenant, TenantID: "tn_a"}, {Level: LevelTenant, TenantID: "tn_a", Environment: "production"}, {Level: LevelTenant, TenantID: "tn_b"},
		{Level: LevelOrganisation, OrganisationID: "ORG-A"}, {Level: LevelOrganisation, OrganisationID: "ORG-A", Environment: "staging"},
		{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_x", Mode: ModeStaticMembership, OrganisationIDs: []string{"ORG-A", "ORG-B"}},
		{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_x", Mode: ModeStaticMembership, OrganisationIDs: []string{"ORG-A"}},
		{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_x", Mode: ModeDynamicGroupDescendants},
		{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_x"},
		{Level: LevelMarket, MarketID: "mkt_ug"}, {Level: LevelMarket, MarketID: "mkt_ug", OrganisationID: "ORG-A"},
		{Level: LevelMarket, MarketID: "mkt_ug", OrganisationID: "ORG-A", TenantID: "tn_a"},
	}
	var population []Resource
	for _, env := range []string{"", "production", "staging"} {
		for _, tenantID := range []string{"", "tn_a", "tn_b"} {
			for _, orgID := range []string{"", "ORG-A", "ORG-B", "ORG-Z"} {
				for _, market := range []string{"", "mkt_ug", "mkt_ke"} {
					for _, groups := range [][]string{nil, {"cgrp_x"}} {
						for _, self := range []string{"", "cgrp_x"} {
							population = append(population, Resource{Environment: env, TenantID: tenantID, OrganisationID: orgID,
								MarketID: market, CorporateGroupIDs: groups, CorporateGroupID: self})
						}
					}
				}
			}
		}
	}
	for _, outer := range scopes {
		for _, inner := range scopes {
			if !Contains(outer, inner) {
				continue
			}
			for _, r := range population {
				if inner.Covers(r) && !outer.Covers(r) {
					t.Fatalf("Contains(%+v, %+v) is true but the container does not reach %+v", outer, inner, r)
				}
			}
		}
	}
}

// A delegation inside its source is usable by the evaluator, at any
// distance from the source's scope, and stops being usable when the source
// does (sections 43-48).
func TestNarrowerDelegationIsEvaluated(t *testing.T) {
	c := MustDefaultCatalogue()
	end := now.Add(72 * time.Hour)
	source := tenantGrant("agr_source", "prn_jane", "tenant.view", "tn_acmeug")
	source.DelegableDepth, source.GrantType, source.ValidUntil = 1, TypeTimeBound, &end
	narrower := source.Scope
	narrower.Environment = "production"
	g, err := PlanDelegation(c, "prn_jane", source, nil, DelegationRequest{PrincipalID: "prn_bob", Permission: "tenant.view", Scope: narrower,
		ValidUntil: now.Add(24 * time.Hour), Reason: "Production cover."}, now)
	if err != nil {
		t.Fatalf("a narrower delegation was refused: %v", err)
	}
	if g.Scope.Environment != "production" {
		t.Fatalf("the delegation did not keep its narrower scope: %+v", g.Scope)
	}
	sources := map[string]Grant{source.GrantID: source}
	prod := Resource{Environment: "production", TenantID: "tn_acmeug"}
	staging := Resource{Environment: "staging", TenantID: "tn_acmeug"}
	allow := Evaluate(Request{PrincipalID: "prn_bob", PrincipalActive: true, Action: "tenant.view", Resource: prod, Now: now, Grants: []Grant{g}, Sources: sources})
	if !allow.Allowed() {
		t.Fatalf("a narrower delegation was not usable where it applies: %+v", allow)
	}
	if got := Evaluate(Request{PrincipalID: "prn_bob", PrincipalActive: true, Action: "tenant.view", Resource: staging, Now: now, Grants: []Grant{g}, Sources: sources}); got.Allowed() {
		t.Fatalf("a delegation reached beyond its narrower scope: %+v", got)
	}
	// A delegation whose scope is not inside its source's is invalid at evaluation too.
	escaped := g
	escaped.Scope = Scope{Level: LevelTenant, TenantID: "tn_other"}
	if got := Evaluate(Request{PrincipalID: "prn_bob", PrincipalActive: true, Action: "tenant.view", Resource: Resource{TenantID: "tn_other"}, Now: now,
		Grants: []Grant{escaped}, Sources: sources}); got.Allowed() || got.ReasonCodes[0] != "DELEGATION_INVALID" {
		t.Fatalf("a delegation outside its source was accepted: %+v", got)
	}
	// And when the source is revoked, so is the delegation.
	revoked := source
	revoked.Status = StatusRevoked
	if got := Evaluate(Request{PrincipalID: "prn_bob", PrincipalActive: true, Action: "tenant.view", Resource: prod, Now: now, Grants: []Grant{g},
		Sources: map[string]Grant{source.GrantID: revoked}}); got.Allowed() {
		t.Fatalf("a delegation outlived its revoked source: %+v", got)
	}
}
