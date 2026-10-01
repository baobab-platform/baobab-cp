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
		if got := Contains(tc.outer, tc.inner, Relations{}); got != tc.want {
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
			if !Contains(outer, inner, Relations{}) {
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
		ValidUntil: now.Add(24 * time.Hour), Reason: "Production cover."}, now, Relations{})
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

// An effective TenantOrganisationMapping is the one cross-level relation
// (ADR-BCP-018 section 50): an organisation contains the tenants mapped to
// it, as it covers their resources, and nothing else follows from ownership,
// naming, parentage or PlatformAccount membership.
func TestOrganisationContainsMappedTenants(t *testing.T) {
	acme, other := "0a1b2c3d-0000-4000-8000-000000000001", "0a1b2c3d-0000-4000-8000-000000000002"
	rel := Relations{TenantOrganisations: map[string][]string{"tn_acmeug": {acme}, "tn_other": {other}, "tn_shared": {acme, other}}}
	org := func(id string) Scope { return Scope{Level: LevelOrganisation, OrganisationID: id} }
	tenant := func(id, env string) Scope { return Scope{Level: LevelTenant, TenantID: id, Environment: env} }
	for name, tc := range map[string]struct {
		outer, inner Scope
		want         bool
	}{
		"a mapped tenant":                    {org(acme), tenant("tn_acmeug", ""), true},
		"the id in another case":             {org("0A1B2C3D-0000-4000-8000-000000000001"), tenant("tn_acmeug", ""), true},
		"an environment of a mapped tenant":  {org(acme), tenant("tn_acmeug", "production"), true},
		"an unmapped tenant":                 {org(acme), tenant("tn_other", ""), false},
		"a tenant mapped to two":             {org(acme), tenant("tn_shared", ""), true},
		"a tenant nobody maps":               {org(acme), tenant("tn_unknown", ""), false},
		"the tenant's other organisation":    {org(other), tenant("tn_acmeug", ""), false},
		"never the reverse":                  {tenant("tn_acmeug", ""), org(acme), false},
		"an environment the org lacks":       {Scope{Level: LevelOrganisation, OrganisationID: acme, Environment: "staging"}, tenant("tn_acmeug", "production"), false},
		"a named environment, no tenant env": {Scope{Level: LevelOrganisation, OrganisationID: acme, Environment: "production"}, tenant("tn_acmeug", ""), false},
	} {
		if got := Contains(tc.outer, tc.inner, rel); got != tc.want {
			t.Errorf("%s: Contains = %v, want %v", name, got, tc.want)
		}
	}
	// With no relation supplied nothing across levels is provable.
	if Contains(org(acme), tenant("tn_acmeug", ""), Relations{}) {
		t.Error("an organisation contained a tenant with no mapping supplied")
	}
}

// Coverage reads the same relation: an organisation grant reaches a mapped
// tenant's resources, and containment is its converse over a population.
func TestMappedOrganisationCoverageAgreesWithContainment(t *testing.T) {
	acme, other := "org-acme", "org-other"
	rel := Relations{TenantOrganisations: map[string][]string{"tn_a": {acme}, "tn_b": {other}, "tn_ab": {acme, other}}}
	scopes := []Scope{
		{Level: LevelOrganisation, OrganisationID: acme}, {Level: LevelOrganisation, OrganisationID: other},
		{Level: LevelTenant, TenantID: "tn_a"}, {Level: LevelTenant, TenantID: "tn_b"}, {Level: LevelTenant, TenantID: "tn_ab"},
		{Level: LevelTenant, TenantID: "tn_a", Environment: "production"}, {Level: LevelOrganisation, OrganisationID: acme, Environment: "production"},
	}
	var population []Resource
	for _, env := range []string{"", "production", "staging"} {
		for _, tenantID := range []string{"", "tn_a", "tn_b", "tn_ab", "tn_x"} {
			for _, orgID := range []string{"", acme, other} {
				population = append(population, rel.ResolveResource(Resource{Environment: env, TenantID: tenantID, OrganisationID: orgID}))
			}
		}
	}
	proven := 0
	for _, outer := range scopes {
		for _, inner := range scopes {
			if !Contains(outer, inner, rel) {
				continue
			}
			proven++
			for _, r := range population {
				if inner.Covers(r) && !outer.Covers(r) {
					t.Fatalf("Contains(%+v, %+v) holds but the container does not reach %+v", outer, inner, r)
				}
			}
		}
	}
	if proven < len(scopes) {
		t.Fatalf("only %d containments proven; the population exercises nothing", proven)
	}
	// An organisation grant reaches a mapped tenant's resources and no other's.
	acmeOrg := Scope{Level: LevelOrganisation, OrganisationID: acme}
	if !acmeOrg.Covers(rel.ResolveResource(Resource{TenantID: "tn_a"})) || acmeOrg.Covers(rel.ResolveResource(Resource{TenantID: "tn_b"})) {
		t.Fatal("organisation coverage does not follow the mapping")
	}
	// A tenant's scope never reaches an organisation's own resources.
	if (Scope{Level: LevelTenant, TenantID: "tn_a"}).Covers(Resource{OrganisationID: acme}) {
		t.Fatal("a tenant scope reached an organisation resource")
	}
	if !(Resource{TenantID: "tn_a"}).Anchors(LevelTenant) || rel.ResolveResource(Resource{TenantID: "tn_a"}).Anchors(LevelOrganisation) != true ||
		(Resource{TenantID: "tn_a"}).Anchors(LevelOrganisation) {
		t.Fatal("organisation anchoring must require the mapping to have been resolved")
	}
}

// The ACME to ACME Uganda case end to end: an organisation grant delegates a
// tenant slice, the delegation works while the mapping is effective and
// stops when it ends or when the source's permission is not valid at both
// levels (ADR-BCP-020 sections 43-44; owner decision, 2026-10-01).
func TestOrganisationGrantDelegatesAMappedTenant(t *testing.T) {
	c := MustDefaultCatalogue()
	acme := "0a1b2c3d-0000-4000-8000-000000000001"
	rel := Relations{TenantOrganisations: map[string][]string{"tn_acmeug": {acme}}}
	end := now.Add(72 * time.Hour)
	source := Grant{GrantID: "agr_orgsource", PrincipalID: "prn_jane", Permission: "tenant.view", Scope: Scope{Level: LevelOrganisation, OrganisationID: acme},
		GrantType: TypeTimeBound, Source: SourceDirect, DelegableDepth: 1, RiskClass: RiskLow, ValidFrom: now.Add(-time.Hour), ValidUntil: &end,
		Status: StatusActive, GrantedBy: "prn_ops", Reason: "seed", CreatedAt: now, Version: 1}
	if p, _ := c.Permission("tenant.view"); !(contains(p.ScopeLevels, LevelOrganisation) && contains(p.ScopeLevels, LevelTenant)) {
		t.Fatal("tenant.view must be valid at ORGANISATION and TENANT for this case")
	}
	q := DelegationRequest{PrincipalID: "prn_bob", Permission: "tenant.view", Scope: Scope{Level: LevelTenant, TenantID: "tn_acmeug"},
		ValidUntil: now.Add(24 * time.Hour), Reason: "Cover ACME Uganda."}
	g, err := PlanDelegation(c, "prn_jane", source, nil, q, now, rel)
	if err != nil {
		t.Fatalf("delegating a mapped tenant was refused: %v", err)
	}
	sources := map[string]Grant{source.GrantID: source}
	use := func(rel Relations) Decision {
		return Evaluate(Request{PrincipalID: "prn_bob", PrincipalActive: true, Action: "tenant.view", Resource: rel.ResolveResource(Resource{TenantID: "tn_acmeug"}),
			Now: now, Grants: []Grant{g}, Sources: sources, Relations: rel})
	}
	if d := use(rel); !d.Allowed() {
		t.Fatalf("the delegation is not usable while the mapping is effective: %+v", d)
	}
	// The mapping ends: the delegator's own authority no longer reaches the tenant, so neither does the delegation.
	if d := use(Relations{}); d.Allowed() || d.ReasonCodes[0] != "DELEGATION_INVALID" {
		t.Fatalf("the delegation outlived its mapping: %+v", d)
	}
	// The delegator themselves reach the mapped tenant through the organisation grant.
	if d := Evaluate(Request{PrincipalID: "prn_jane", PrincipalActive: true, Action: "tenant.view", Resource: rel.ResolveResource(Resource{TenantID: "tn_acmeug"}),
		Now: now, Grants: []Grant{source}, Relations: rel}); !d.Allowed() {
		t.Fatalf("an organisation grant does not reach its mapped tenant: %+v", d)
	}
	// A tenant mapped to a different organisation, ownership or naming: refused.
	for name, r := range map[string]Relations{"unmapped": {}, "another organisation": {TenantOrganisations: map[string][]string{"tn_acmeug": {"someone-else"}}}} {
		if _, err := PlanDelegation(c, "prn_jane", source, nil, q, now, r); err == nil {
			t.Errorf("%s: a delegation was planned without an effective mapping", name)
		}
	}
	// A permission that is valid at ORGANISATION but not at TENANT is not
	// delegated across the levels.
	notTenant := source
	notTenant.Permission = "organisation.manage"
	if p, ok := c.Permission("organisation.manage"); ok && contains(p.ScopeLevels, LevelTenant) {
		t.Skip("organisation.manage is valid at TENANT in this registry")
	}
	crossed := q
	crossed.Permission = "organisation.manage"
	if _, err := PlanDelegation(c, "prn_jane", notTenant, nil, crossed, now, rel); err == nil {
		t.Error("a permission invalid at TENANT was delegated at a tenant")
	}
}

func contains(levels []ScopeLevel, l ScopeLevel) bool {
	for _, x := range levels {
		if x == l {
			return true
		}
	}
	return false
}
