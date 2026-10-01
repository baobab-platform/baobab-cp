package administration

import (
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

var now = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { t := now.Add(d); return &t }

func tenantGrant(id, principal, permission, tenant string) Grant {
	return Grant{GrantID: id, PrincipalID: principal, Permission: permission,
		Scope: Scope{Level: LevelTenant, TenantID: tenant}, GrantType: TypeStanding, Source: SourceDirect,
		RiskClass: MustDefaultCatalogue().permissions[permission].RiskClass, ValidFrom: now.Add(-time.Hour),
		Status: StatusActive, GrantedBy: "prn_platformops", Reason: "test", CreatedAt: now.Add(-time.Hour), Version: 1}
}

func TestCatalogueLoadsAndRefusesUnsafeVocabularies(t *testing.T) {
	c := MustDefaultCatalogue()
	if p, ok := c.Permission("tenant.suspend"); !ok || p.RiskClass != RiskHigh {
		t.Fatalf("tenant.suspend: %+v %v", p, ok)
	}
	if _, ok := c.Profile("tenant-administrator"); !ok {
		t.Fatal("tenant-administrator profile missing")
	}
	perms, _ := contracts.ReadEmbedded("administration/v1/permission-registry.yaml")
	profiles, _ := contracts.ReadEmbedded("administration/v1/profile-registry.yaml")
	unsafe := map[string][2]string{
		"a delegable CRITICAL permission": {
			"  - key: tenant.decommission\n    domain: TENANT\n    risk_class: CRITICAL\n    scope_levels: [PLATFORM]\n    delegable: false",
			"  - key: tenant.decommission\n    domain: TENANT\n    risk_class: CRITICAL\n    scope_levels: [PLATFORM]\n    delegable: true"},
	}
	for name, edit := range unsafe {
		if !strings.Contains(string(perms), edit[0]) {
			t.Fatalf("%s: the registry no longer contains %q", name, edit[0])
		}
		if _, err := ParseCatalogue([]byte(strings.Replace(string(perms), edit[0], edit[1], 1)), profiles); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	critical := strings.Replace(string(profiles), "      - application.decide\n", "      - application.decide\n      - tenant.decommission\n", 1)
	if _, err := ParseCatalogue(perms, []byte(critical)); err == nil {
		t.Error("a profile conferring standing CRITICAL authority was accepted")
	}
}

func TestGrantValidation(t *testing.T) {
	c := MustDefaultCatalogue()
	good := tenantGrant("agr_1", "prn_jane", "tenant.view", "tn_acmeug")
	if err := good.Validate(c); err != nil {
		t.Fatalf("a valid grant was refused: %v", err)
	}
	bad := map[string]func(g *Grant){
		"self-grant":                  func(g *Grant) { g.GrantedBy = g.PrincipalID },
		"email principal":             func(g *Grant) { g.PrincipalID = "jane@acme.example" },
		"unregistered permission":     func(g *Grant) { g.Permission = "tenant.own" },
		"level the permission lacks":  func(g *Grant) { g.Permission = "tenant.decommission"; g.RiskClass = RiskCritical },
		"risk below the permission's": func(g *Grant) { g.Permission = "tenant.suspend"; g.RiskClass = RiskLow },
		"foreign anchor":              func(g *Grant) { g.Scope.OrganisationID = "ORG-ACME" },
		"no anchor":                   func(g *Grant) { g.Scope.TenantID = "" },
		"standing with an end":        func(g *Grant) { g.ValidUntil = at(time.Hour) },
		"time-bound without an end":   func(g *Grant) { g.GrantType = TypeTimeBound },
		"profile without its key":     func(g *Grant) { g.Source = SourceProfile },
		"profile not conferring it":   func(g *Grant) { g.Source, g.ProfileKey = SourceProfile, "admission-reviewer" },
		"delegation without source":   func(g *Grant) { g.Source = SourceDelegation },
		"delegable non-delegable":     func(g *Grant) { g.Permission, g.RiskClass, g.DelegableDepth = "changeset.approve", RiskHigh, 1 },
		"tenant-scoped bootstrap":     func(g *Grant) { g.Source, g.GrantType, g.ValidUntil = SourceBootstrap, TypeTimeBound, at(time.Hour) },
		"standing bootstrap": func(g *Grant) {
			g.Source, g.Scope, g.Permission, g.RiskClass = SourceBootstrap, Scope{Level: LevelPlatform}, "administrator.grant", RiskHigh
		},
		"revoked without who":              func(g *Grant) { g.Status = StatusRevoked },
		"active with revocation":           func(g *Grant) { g.RevokedAt, g.RevokedBy, g.RevocationReason = at(0), "prn_sec", "x" },
		"no reason":                        func(g *Grant) { g.Reason = "" },
		"environment outside the contract": func(g *Grant) { g.Scope.Environment = "prod" },
		"group mode on a tenant":           func(g *Grant) { g.Scope.Mode = ModeDynamicGroupDescendants },
	}
	for name, mutate := range bad {
		g := good
		mutate(&g)
		if err := g.Validate(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	bootstrap := Grant{GrantID: "agr_2", PrincipalID: "prn_ops", Permission: "administrator.grant", Scope: Scope{Level: LevelPlatform},
		GrantType: TypeTimeBound, Source: SourceBootstrap, RiskClass: RiskCritical, ValidFrom: now, ValidUntil: at(24 * time.Hour),
		Status: StatusActive, GrantedBy: "cp-bootstrap", Reason: "initial authority", CreatedAt: now, Version: 1}
	if err := bootstrap.Validate(c); err != nil {
		t.Fatalf("a platform-scoped, time-bound bootstrap grant was refused: %v", err)
	}
}

func TestScopeCoverage(t *testing.T) {
	cases := []struct {
		name  string
		scope Scope
		res   Resource
		want  bool
	}{
		{"platform reaches everything", Scope{Level: LevelPlatform}, Resource{TenantID: "tn_a"}, true},
		{"environment qualifier", Scope{Level: LevelPlatform, Environment: "production"}, Resource{Environment: "staging"}, false},
		{"tenant exact", Scope{Level: LevelTenant, TenantID: "tn_a"}, Resource{TenantID: "tn_a"}, true},
		{"another tenant", Scope{Level: LevelTenant, TenantID: "tn_a"}, Resource{TenantID: "tn_b"}, false},
		{"tenant grant never reaches its organisation", Scope{Level: LevelTenant, TenantID: "tn_a"}, Resource{OrganisationID: "ORG-A"}, false},
		{"organisation reaches its tenant", Scope{Level: LevelOrganisation, OrganisationID: "ORG-A"}, Resource{OrganisationID: "ORG-A", TenantID: "tn_a"}, true},
		{"organisation never reaches a subsidiary", Scope{Level: LevelOrganisation, OrganisationID: "ORG-A"}, Resource{OrganisationID: "ORG-A-SUB", CorporateGroupIDs: []string{"cgrp_a"}}, false},
		{"exact group is the group only", Scope{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_a"}, Resource{OrganisationID: "ORG-A", CorporateGroupIDs: []string{"cgrp_a"}}, false},
		{"exact group on the group", Scope{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_a"}, Resource{CorporateGroupID: "cgrp_a"}, true},
		{"static membership lists its organisations", Scope{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_a", Mode: ModeStaticMembership, OrganisationIDs: []string{"ORG-A"}},
			Resource{OrganisationID: "ORG-A"}, true},
		{"static membership never grows", Scope{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_a", Mode: ModeStaticMembership, OrganisationIDs: []string{"ORG-A"}},
			Resource{OrganisationID: "ORG-NEWCO", CorporateGroupIDs: []string{"cgrp_a"}}, false},
		{"dynamic follows the current graph", Scope{Level: LevelCorporateGroup, CorporateGroupID: "cgrp_a", Mode: ModeDynamicGroupDescendants},
			Resource{OrganisationID: "ORG-NEWCO", CorporateGroupIDs: []string{"cgrp_a"}}, true},
		{"market with organisation", Scope{Level: LevelMarket, MarketID: "uganda_b2b", OrganisationID: "ORG-A"}, Resource{MarketID: "uganda_b2b", OrganisationID: "ORG-A"}, true},
		{"market never reaches another market", Scope{Level: LevelMarket, MarketID: "uganda_b2b", OrganisationID: "ORG-A"}, Resource{MarketID: "kenya_b2b", OrganisationID: "ORG-A"}, false},
		{"market qualifier holds", Scope{Level: LevelMarket, MarketID: "uganda_b2b", OrganisationID: "ORG-A"}, Resource{MarketID: "uganda_b2b", OrganisationID: "ORG-B"}, false},
	}
	for _, c := range cases {
		if got := c.scope.Covers(c.res); got != c.want {
			t.Errorf("%s: covers = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEvaluateDeniesByDefault(t *testing.T) {
	jane := "prn_jane"
	view := tenantGrant("agr_view", jane, "tenant.view", "tn_ug")
	org := Grant{GrantID: "agr_org", PrincipalID: jane, Permission: "organisation.view", Scope: Scope{Level: LevelOrganisation, OrganisationID: "ORG-ZA"},
		GrantType: TypeStanding, Source: SourceDirect, RiskClass: RiskLow, ValidFrom: now.Add(-time.Hour), Status: StatusActive, GrantedBy: "prn_ops", Reason: "x", Version: 1}
	decide := func(action string, res Resource, grants ...Grant) Decision {
		return Evaluate(Request{PrincipalActive: true, PrincipalID: jane, Action: action, Resource: res, Now: now, Grants: grants})
	}
	if d := decide("tenant.view", Resource{TenantID: "tn_ug"}, view); !d.Allowed() || d.MatchedGrants[0] != "agr_view" {
		t.Fatalf("a covering grant must allow: %+v", d)
	}
	cases := []struct {
		name   string
		d      Decision
		reason string
	}{
		{"no grant at all", decide("tenant.view", Resource{TenantID: "tn_ug"}), "NO_ADMINISTRATIVE_GRANT"},
		{"grant for another action", decide("tenant.suspend", Resource{TenantID: "tn_ug"}, view), "NO_ADMINISTRATIVE_GRANT"},
		{"grant for another tenant", decide("tenant.view", Resource{TenantID: "tn_za"}, view), "SCOPE_MISMATCH"},
		// Section 111: tenant.view on UG and organisation.view on ZA never make tenant.view on ZA.
		{"no union across scope", decide("tenant.view", Resource{TenantID: "tn_za", OrganisationID: "ORG-ZA"}, view, org), "SCOPE_MISMATCH"},
		{"another principal's grant", Evaluate(Request{PrincipalActive: true, PrincipalID: "prn_bob", Action: "tenant.view", Resource: Resource{TenantID: "tn_ug"}, Now: now, Grants: []Grant{view}}), "NO_ADMINISTRATIVE_GRANT"},
		{"suspended", decide("tenant.view", Resource{TenantID: "tn_ug"}, with(view, func(g *Grant) { g.Status = StatusSuspended })), "ADMINISTRATIVE_GRANT_SUSPENDED"},
		{"revoked", decide("tenant.view", Resource{TenantID: "tn_ug"}, with(view, func(g *Grant) { g.Status = StatusRevoked })), "ADMINISTRATIVE_GRANT_REVOKED"},
		{"pending", decide("tenant.view", Resource{TenantID: "tn_ug"}, with(view, func(g *Grant) { g.Status = StatusPending })), "ADMINISTRATIVE_GRANT_PENDING"},
		{"not yet valid", decide("tenant.view", Resource{TenantID: "tn_ug"}, with(view, func(g *Grant) { g.ValidFrom = now.Add(time.Minute) })), "ADMINISTRATIVE_GRANT_PENDING"},
		{"window ended though still ACTIVE", decide("tenant.view", Resource{TenantID: "tn_ug"}, with(view, func(g *Grant) {
			g.GrantType, g.ValidUntil = TypeTimeBound, at(0)
		})), "ADMINISTRATIVE_GRANT_EXPIRED"},
	}
	for _, c := range cases {
		if c.d.Allowed() || len(c.d.ReasonCodes) != 1 || c.d.ReasonCodes[0] != c.reason || len(c.d.MatchedGrants) != 0 {
			t.Errorf("%s: got %+v, want DENY %s", c.name, c.d, c.reason)
		}
	}
	if d := Evaluate(Request{PrincipalID: jane, Action: "tenant.view", Resource: Resource{TenantID: "tn_ug"}, Now: now, Grants: []Grant{view}}); d.Allowed() || d.ReasonCodes[0] != "PRINCIPAL_INACTIVE" {
		t.Errorf("an inactive principal must be denied whatever its grants: %+v", d)
	}
	stepUp := with(view, func(g *Grant) { g.Conditions = &Conditions{MinimumACR: "urn:baobab:acr:mfa"} })
	if d := decide("tenant.view", Resource{TenantID: "tn_ug"}, stepUp); d.Outcome != OutcomeStepUpRequired || d.Obligations[0] != "STEP_UP_AUTHENTICATION" {
		t.Errorf("an unmet assurance condition must ask for step-up: %+v", d)
	}
	if d := Evaluate(Request{PrincipalActive: true, PrincipalID: jane, Action: "tenant.view", Resource: Resource{TenantID: "tn_ug"}, Now: now,
		SessionACRs: []string{"urn:baobab:acr:mfa"}, Grants: []Grant{stepUp}}); !d.Allowed() {
		t.Errorf("a met assurance condition must allow: %+v", d)
	}
}

func with(g Grant, mutate func(*Grant)) Grant { mutate(&g); return g }

func TestDelegationNeverExceedsItsSource(t *testing.T) {
	source := with(tenantGrant("agr_src", "prn_jane", "tenant.view", "tn_ug"), func(g *Grant) {
		g.DelegableDepth, g.GrantType, g.ValidUntil = 1, TypeTimeBound, at(24*time.Hour)
	})
	delegated := Grant{GrantID: "agr_del", PrincipalID: "prn_carol", Permission: "tenant.view", Scope: source.Scope,
		GrantType: TypeTimeBound, Source: SourceDelegation, DelegatedFromGrantID: "agr_src", DelegationDepth: 1,
		RiskClass: RiskLow, ValidFrom: now.Add(-time.Minute), ValidUntil: at(12 * time.Hour), Status: StatusActive,
		GrantedBy: "prn_jane", Reason: "cover", Version: 1}
	decide := func(g Grant, sources ...Grant) Decision {
		m := map[string]Grant{}
		for _, s := range sources {
			m[s.GrantID] = s
		}
		return Evaluate(Request{PrincipalActive: true, PrincipalID: "prn_carol", Action: "tenant.view", Resource: Resource{TenantID: "tn_ug"}, Now: now, Grants: []Grant{g}, Sources: m})
	}
	if d := decide(delegated, source); !d.Allowed() {
		t.Fatalf("a valid delegation must allow: %+v", d)
	}
	invalid := map[string]Decision{
		"source unknown":                decide(delegated),
		"source revoked":                decide(delegated, with(source, func(g *Grant) { g.Status = StatusRevoked })),
		"source expired":                decide(delegated, with(source, func(g *Grant) { g.ValidUntil = at(0) })),
		"source forbids delegating":     decide(delegated, with(source, func(g *Grant) { g.DelegableDepth = 0 })),
		"outlives its source":           decide(with(delegated, func(g *Grant) { g.ValidUntil = at(48 * time.Hour) }), source),
		"delegated by someone else":     decide(with(delegated, func(g *Grant) { g.GrantedBy = "prn_mallory" }), source),
		"skips a depth":                 decide(with(delegated, func(g *Grant) { g.DelegationDepth = 2 }), source),
		"allows deeper than its source": decide(with(delegated, func(g *Grant) { g.DelegableDepth = 1 }), source),
	}
	for name, d := range invalid {
		if d.Allowed() || d.ReasonCodes[0] != "DELEGATION_INVALID" {
			t.Errorf("%s: got %+v, want DENY DELEGATION_INVALID", name, d)
		}
	}
	// A delegation cannot widen scope: a grant on another tenant simply
	// is not the delegation of this source.
	wider := with(delegated, func(g *Grant) { g.Scope = Scope{Level: LevelTenant, TenantID: "tn_za"} })
	if d := Evaluate(Request{PrincipalActive: true, PrincipalID: "prn_carol", Action: "tenant.view", Resource: Resource{TenantID: "tn_za"}, Now: now,
		Grants: []Grant{wider}, Sources: map[string]Grant{"agr_src": source}}); d.Allowed() {
		t.Error("a delegation widened scope beyond its source")
	}
}

func TestEffectiveAuthority(t *testing.T) {
	c := MustDefaultCatalogue()
	view := tenantGrant("agr_view", "prn_jane", "tenant.view", "tn_acmeug")
	jit := with(tenantGrant("agr_jitsuspend", "prn_jane", "tenant.suspend", "tn_acmeug"), func(g *Grant) {
		g.GrantType, g.ValidUntil, g.RiskClass = TypeJustInTime, at(30*time.Minute), RiskHigh
	})
	jit2 := with(jit, func(g *Grant) {
		g.GrantID, g.Permission, g.ValidUntil = "agr_jitreinstate", "tenant.reinstate", at(10*time.Minute)
	})
	suspended := with(tenantGrant("agr_suspended", "prn_jane", "market.view", "tn_acmeug"), func(g *Grant) { g.Status = StatusSuspended })
	other := tenantGrant("agr_bobview", "prn_bob", "tenant.view", "tn_acmeug")
	for _, g := range []Grant{view, jit, jit2, suspended, other} {
		if err := g.Validate(c); err != nil {
			t.Fatalf("fixture %s: %v", g.GrantID, err)
		}
	}
	e := Effective("prn_jane", []Grant{view, jit, jit2, suspended, other}, nil, now, Relations{})
	var ids []string
	for _, g := range e.Grants {
		ids = append(ids, g.GrantID)
	}
	if strings.Join(ids, ",") != "agr_jitreinstate,agr_jitsuspend,agr_view" {
		t.Fatalf("effective grants %v: want the usable grants of this principal, sorted by permission", ids)
	}
	if e.ElevatedUntil == nil || !e.ElevatedUntil.Equal(now.Add(10*time.Minute)) || !e.Grants[0].Elevated || e.Grants[2].Elevated {
		t.Fatalf("elevation: %+v", e)
	}
	if empty := Effective("prn_nobody", nil, nil, now, Relations{}); empty.Grants == nil || len(empty.Grants) != 0 {
		t.Fatal("a principal with no grants gets an empty list, not null")
	}
	schema := contracts.MustSchema("administration/v1/grant.schema.json#/$defs/EffectiveAuthority")
	if err := contracts.ValidateValue(schema, e); err != nil {
		t.Fatalf("effective authority does not conform: %v", err)
	}
	grantSchema := contracts.MustSchema("administration/v1/grant.schema.json#/$defs/AdministrativeGrant")
	for _, g := range []Grant{view, jit} {
		g.GrantID = "agr_" + strings.Repeat("0", 31) + "1"
		if err := contracts.ValidateValue(grantSchema, g); err != nil {
			t.Fatalf("grant %s does not conform: %v", g.Permission, err)
		}
	}
}
