package administration

import (
	"errors"
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

func refusedWith(t *testing.T, err error, code string) {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) || r.Code != code {
		t.Fatalf("want refusal %s, got %v", code, err)
	}
}

func issue(permission string, scope Scope) IssueRequest {
	return IssueRequest{PrincipalID: "prn_jane", Permission: permission, Scope: scope, GrantType: TypeStanding, Reason: "needed"}
}

func TestPlanIssueIssuesLowAndModerateOnly(t *testing.T) {
	c := MustDefaultCatalogue()
	tenant := Scope{Level: LevelTenant, TenantID: "tn_acmeug"}
	g, err := PlanIssue(c, "prn_ops", issue("tenant.view", tenant), now)
	if err != nil {
		t.Fatalf("a LOW grant was refused: %v", err)
	}
	if g.Source != SourceDirect || g.Status != StatusActive || g.GrantedBy != "prn_ops" || g.Version != 1 || g.RiskClass != RiskLow {
		t.Fatalf("unexpected grant %+v", g)
	}
	future := now.Add(time.Hour)
	q := issue("tenant.view", tenant)
	q.ValidFrom = &future
	if g, err = PlanIssue(c, "prn_ops", q, now); err != nil || g.Status != StatusPending {
		t.Fatalf("a future grant must start PENDING: %+v %v", g, err)
	}

	// HIGH and CRITICAL authority needs a second person (section 38).
	_, err = PlanIssue(c, "prn_ops", issue("tenant.suspend", tenant), now)
	refusedWith(t, err, CodeApprovalRequired)
	_, err = PlanIssue(c, "prn_ops", issue("administrator.grant", Scope{Level: LevelPlatform}), now)
	refusedWith(t, err, CodeApprovalRequired)
}

func TestPlanIssueRefusals(t *testing.T) {
	c := MustDefaultCatalogue()
	tenant := Scope{Level: LevelTenant, TenantID: "tn_acmeug"}
	until := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	cases := map[string]struct {
		caller string
		edit   func(*IssueRequest)
		code   string
	}{
		"self grant":              {"prn_jane", func(*IssueRequest) {}, CodeSelfApproval},
		"unregistered permission": {"prn_ops", func(q *IssueRequest) { q.Permission = "tenant.own" }, CodeInvalidGrant},
		"permission at a level it does not allow": {"prn_ops", func(q *IssueRequest) {
			q.Scope = Scope{Level: LevelMarket, MarketID: "mkt_x"}
			q.Permission = "tenant.view"
		}, CodeInvalidGrant},
		"time bound without an end": {"prn_ops", func(q *IssueRequest) { q.GrantType = TypeTimeBound }, CodeInvalidGrant},
		"standing with an end":      {"prn_ops", func(q *IssueRequest) { q.ValidUntil = &until }, CodeInvalidGrant},
		"start in the past":         {"prn_ops", func(q *IssueRequest) { q.ValidFrom = &past }, CodeInvalidGrant},
		"no reason":                 {"prn_ops", func(q *IssueRequest) { q.Reason = " " }, CodeInvalidGrant},
		"email as principal":        {"prn_ops", func(q *IssueRequest) { q.PrincipalID = "jane@acme.example" }, CodeInvalidGrant},
		"delegable what is not delegable": {"prn_ops", func(q *IssueRequest) {
			q.Permission = "administrator.view"
			q.Scope = Scope{Level: LevelTenant, TenantID: "tn_acmeug"}
			q.DelegableDepth = 3
		}, CodeInvalidGrant},
	}
	for name, tc := range cases {
		q := issue("tenant.view", tenant)
		tc.edit(&q)
		_, err := PlanIssue(c, tc.caller, q, now)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		var r *Refusal
		if !errors.As(err, &r) || r.Code != tc.code {
			t.Errorf("%s: want %s, got %v", name, tc.code, err)
		}
	}
}

func delegableSource(until *time.Time) Grant {
	g := tenantGrant("agr_source", "prn_jane", "tenant.view", "tn_acmeug")
	g.DelegableDepth = 2
	g.ValidUntil = until
	if until != nil {
		g.GrantType = TypeTimeBound
	}
	return g
}

func TestPlanDelegationNeverExceedsTheSource(t *testing.T) {
	c := MustDefaultCatalogue()
	end := now.Add(48 * time.Hour)
	source := delegableSource(&end)
	base := DelegationRequest{PrincipalID: "prn_bob", Permission: "tenant.view", Scope: source.Scope,
		ValidUntil: now.Add(24 * time.Hour), DelegableDepth: 1, Reason: "cover"}
	g, err := PlanDelegation(c, "prn_jane", source, nil, base, now)
	if err != nil {
		t.Fatalf("a valid delegation was refused: %v", err)
	}
	if g.Source != SourceDelegation || g.DelegatedFromGrantID != "agr_source" || g.DelegationDepth != 1 ||
		g.GrantedBy != "prn_jane" || g.GrantType != TypeTimeBound {
		t.Fatalf("unexpected delegation %+v", g)
	}
	// The result is what evaluation accepts.
	if !Usable(g, map[string]Grant{source.GrantID: source}, now) {
		t.Fatal("a freshly planned delegation is not usable")
	}

	bad := map[string]struct {
		caller string
		edit   func(*DelegationRequest)
		code   string
	}{
		"someone else's grant":    {"prn_mallory", func(*DelegationRequest) {}, CodeDelegationInvalid},
		"to oneself":              {"prn_jane", func(q *DelegationRequest) { q.PrincipalID = "prn_jane" }, CodeSelfApproval},
		"another permission":      {"prn_jane", func(q *DelegationRequest) { q.Permission = "tenant.suspend" }, CodeDelegationInvalid},
		"a wider scope":           {"prn_jane", func(q *DelegationRequest) { q.Scope = Scope{Level: LevelPlatform} }, CodeDelegationInvalid},
		"an unrelated tenant":     {"prn_jane", func(q *DelegationRequest) { q.Scope = Scope{Level: LevelTenant, TenantID: "tn_other"} }, CodeDelegationInvalid},
		"beyond the source's end": {"prn_jane", func(q *DelegationRequest) { q.ValidUntil = end.Add(time.Hour) }, CodeDelegationInvalid},
		"more hops than are left": {"prn_jane", func(q *DelegationRequest) { q.DelegableDepth = 2 }, CodeDelegationInvalid},
		"already ended":           {"prn_jane", func(q *DelegationRequest) { q.ValidUntil = now.Add(-time.Minute) }, CodeInvalidGrant},
		"without a reason":        {"prn_jane", func(q *DelegationRequest) { q.Reason = "" }, CodeInvalidGrant},
	}
	for name, tc := range bad {
		q := base
		tc.edit(&q)
		_, err := PlanDelegation(c, tc.caller, source, nil, q, now)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		var r *Refusal
		if !errors.As(err, &r) || r.Code != tc.code {
			t.Errorf("%s: want %s, got %v", name, tc.code, err)
		}
	}

	// A source that allows no further hop, one that is not usable, and a
	// permission that is not delegable.
	none := delegableSource(&end)
	none.DelegableDepth = 0
	_, err = PlanDelegation(c, "prn_jane", none, nil, base, now)
	refusedWith(t, err, CodeDelegationInvalid)
	suspended := delegableSource(&end)
	suspended.Status = StatusSuspended
	_, err = PlanDelegation(c, "prn_jane", suspended, nil, base, now)
	refusedWith(t, err, CodeDelegationInvalid)
	notDelegable := tenantGrant("agr_nd", "prn_jane", "administrator.grant", "tn_acmeug")
	notDelegable.DelegableDepth = 1
	q := base
	q.Permission = "administrator.grant"
	_, err = PlanDelegation(c, "prn_jane", notDelegable, nil, q, now)
	refusedWith(t, err, CodeDelegationInvalid)
	// A delegable HIGH permission still waits for a second person's approval.
	highDelegable := tenantGrant("agr_hd", "prn_jane", "tenant.suspend", "tn_acmeug")
	highDelegable.DelegableDepth = 2
	q.Permission = "tenant.suspend"
	q.DelegableDepth = 0
	_, err = PlanDelegation(c, "prn_jane", highDelegable, nil, q, now)
	refusedWith(t, err, CodeApprovalRequired)

	// A delegation from a delegation is one hop deeper, and the third hop is the last.
	second := g
	second.GrantID = "agr_second"
	second.PrincipalID = "prn_bob"
	second.DelegableDepth = 1
	chain := map[string]Grant{source.GrantID: source}
	q2 := DelegationRequest{PrincipalID: "prn_carol", Permission: "tenant.view", Scope: source.Scope,
		ValidUntil: now.Add(12 * time.Hour), Reason: "cover"}
	third, err := PlanDelegation(c, "prn_bob", second, chain, q2, now)
	if err != nil || third.DelegationDepth != 2 {
		t.Fatalf("a second hop: %+v %v", third, err)
	}
}

// The transition table is lifecycle.yaml's person-issued transitions and
// nothing else (sections 57-64).
func TestTransitionTableMatchesTheSharedLifecycle(t *testing.T) {
	raw, err := contracts.ReadEmbedded("administration/v1/lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Transitions []struct {
			Command string `yaml:"command"`
			From    Status `yaml:"from"`
			To      Status `yaml:"to"`
			Actor   string `yaml:"actor"`
		} `yaml:"transitions"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, tr := range doc.Transitions {
		if tr.Actor == "PLATFORM" {
			if Command(tr.Command).Valid() {
				t.Errorf("%s is the platform's alone", tr.Command)
			}
			continue
		}
		want++
		if got := transitionTable[Command(tr.Command)][tr.From]; got != tr.To {
			t.Errorf("%s from %s: lifecycle.yaml says %s, the table %s", tr.Command, tr.From, tr.To, got)
		}
	}
	have := 0
	for _, m := range transitionTable {
		have += len(m)
	}
	if have != want {
		t.Errorf("the table has %d person transitions, lifecycle.yaml %d", have, want)
	}
	_ = os.Getenv
}

func TestTransitionTarget(t *testing.T) {
	active := tenantGrant("agr_a", "prn_jane", "tenant.view", "tn_acmeug")
	if to, err := TransitionTarget(active, CommandSuspend, now); err != nil || to != StatusSuspended {
		t.Fatalf("suspend: %v %v", to, err)
	}
	for _, c := range []Command{CommandResume, CommandWithdraw} {
		_, err := TransitionTarget(active, c, now)
		refusedWith(t, err, CodeTransitionInvalid)
	}
	revoked := active
	revoked.Status = StatusRevoked
	for _, c := range []Command{CommandSuspend, CommandResume, CommandRevoke, CommandWithdraw} {
		_, err := TransitionTarget(revoked, c, now)
		refusedWith(t, err, CodeTransitionInvalid)
	}
	ended := now.Add(-time.Minute)
	lapsed := active
	lapsed.Status, lapsed.ValidUntil, lapsed.GrantType = StatusSuspended, &ended, TypeTimeBound
	_, err := TransitionTarget(lapsed, CommandResume, now)
	refusedWith(t, err, CodeTransitionInvalid)
	if to, err := TransitionTarget(lapsed, CommandRevoke, now); err != nil || to != StatusRevoked {
		t.Fatalf("a lapsed suspended grant may still be revoked: %v %v", to, err)
	}
}

func TestEffectiveRiskRaisesPlatformAdministration(t *testing.T) {
	c := MustDefaultCatalogue()
	p, _ := c.Permission("administrator.grant")
	if EffectiveRisk(p, Scope{Level: LevelPlatform}) != RiskCritical {
		t.Error("administrator.grant at PLATFORM must be CRITICAL")
	}
	if EffectiveRisk(p, Scope{Level: LevelTenant, TenantID: "tn_acmeug"}) != RiskHigh {
		t.Error("administrator.grant at a tenant is HIGH")
	}
	v, _ := c.Permission("administrator.view")
	if EffectiveRisk(v, Scope{Level: LevelPlatform}) != RiskLow {
		t.Error("a read-only permission is not raised")
	}
}

func TestAddsAuthority(t *testing.T) {
	end := now.Add(48 * time.Hour)
	later := end.Add(time.Hour)
	earlier := end.Add(-time.Hour)
	base := tenantGrant("agr_old", "prn_jane", "tenant.suspend", "tn_acmeug")
	base.GrantType, base.ValidUntil = TypeTimeBound, &end
	same := func(edit func(*Grant)) Grant { g := base; g.GrantID = "agr_new"; edit(&g); return g }
	for name, tc := range map[string]struct {
		next Grant
		adds bool
	}{
		"identical":              {same(func(*Grant) {}), false},
		"an earlier end":         {same(func(g *Grant) { g.ValidUntil = &earlier }), false},
		"a later end":            {same(func(g *Grant) { g.ValidUntil = &later }), true},
		"no end at all":          {same(func(g *Grant) { g.GrantType, g.ValidUntil = TypeStanding, nil }), true},
		"another permission":     {same(func(g *Grant) { g.Permission = "tenant.activate" }), true},
		"another scope":          {same(func(g *Grant) { g.Scope = Scope{Level: LevelTenant, TenantID: "tn_other"} }), true},
		"a narrower environment": {same(func(g *Grant) { g.Scope.Environment = "production" }), false},
		"a wider environment":    {func() Grant { g := same(func(*Grant) {}); return g }(), false},
		"more delegation":        {same(func(g *Grant) { g.DelegableDepth = 1 }), true},
	} {
		if got := AddsAuthority(base, tc.next); got != tc.adds {
			t.Errorf("%s: AddsAuthority = %v, want %v", name, got, tc.adds)
		}
	}
	guarded := base
	guarded.Conditions = &Conditions{MinimumACR: "urn:baobab:acr:mfa"}
	weaker := same(func(g *Grant) {})
	if !AddsAuthority(guarded, weaker) {
		t.Error("dropping the assurance condition is a weaker grant")
	}
	kept := same(func(g *Grant) { g.Conditions = &Conditions{MinimumACR: "urn:baobab:acr:mfa"} })
	if AddsAuthority(guarded, kept) {
		t.Error("keeping the assurance condition added authority")
	}
}

func TestPlanReplacement(t *testing.T) {
	c := MustDefaultCatalogue()
	end := now.Add(48 * time.Hour)
	old := tenantGrant("agr_old", "prn_jane", "tenant.suspend", "tn_acmeug")
	old.GrantType, old.ValidUntil = TypeTimeBound, &end
	shorter := now.Add(24 * time.Hour)
	q := ReplaceRequest{Permission: "tenant.suspend", Scope: old.Scope, GrantType: TypeTimeBound, ValidUntil: &shorter, Reason: "Shorten."}
	g, err := PlanReplacement(c, "prn_ops", old, q, now)
	if err != nil {
		t.Fatalf("a non-escalating HIGH replacement was refused: %v", err)
	}
	if g.SupersedesGrantID != "agr_old" || g.PrincipalID != "prn_jane" || g.GrantedBy != "prn_ops" || g.Source != SourceDirect || g.Status != StatusActive {
		t.Fatalf("unexpected replacement %+v", g)
	}
	// Adding HIGH authority is a changeset.
	wider := q
	long := end.Add(24 * time.Hour)
	wider.ValidUntil = &long
	_, err = PlanReplacement(c, "prn_ops", old, wider, now)
	refusedWith(t, err, CodeApprovalRequired)
	// LOW and MODERATE replacements are direct whatever they add.
	view := tenantGrant("agr_view", "prn_jane", "tenant.view", "tn_acmeug")
	if _, err := PlanReplacement(c, "prn_ops", view, ReplaceRequest{Permission: "tenant.view", Scope: Scope{Level: LevelTenant, TenantID: "tn_other"},
		GrantType: TypeStanding, Reason: "Move."}, now); err != nil {
		t.Fatalf("a LOW replacement was refused: %v", err)
	}
	// The holder never replaces their own grant.
	_, err = PlanReplacement(c, "prn_jane", old, q, now)
	refusedWith(t, err, CodeSelfApproval)
	// Only a live DIRECT grant is replaceable.
	for name, mutate := range map[string]func(*Grant){
		"a delegation":        func(g *Grant) { g.Source, g.DelegatedFromGrantID, g.DelegationDepth = SourceDelegation, "agr_src", 1 },
		"a revoked grant":     func(g *Grant) { g.Status = StatusRevoked },
		"an expired grant":    func(g *Grant) { past := now.Add(-time.Hour); g.ValidUntil = &past },
		"bootstrap authority": func(g *Grant) { g.Source = SourceBootstrap },
	} {
		bad := old
		mutate(&bad)
		_, err := PlanReplacement(c, "prn_ops", bad, q, now)
		refusedWith(t, err, CodeReplacementInvalid)
		_ = name
	}
	// A replacement takes effect at once: a later start would leave a gap.
	future := now.Add(time.Hour)
	later := q
	later.ValidFrom = &future
	_, err = PlanReplacement(c, "prn_ops", old, later, now)
	refusedWith(t, err, CodeInvalidGrant)
}
