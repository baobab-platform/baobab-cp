package administration

import (
	"errors"
	"testing"
	"time"
)

func TestSoDPolicySetLoads(t *testing.T) {
	s := MustDefaultSoD()
	if len(s.Policies) < 2 {
		t.Fatalf("the Shared policy set declares %d policies", len(s.Policies))
	}
	// HIGH and above keep an independence policy (section 39).
	if err := s.Independence(KindIssuance, RiskHigh, "req", "req", "gr"); err == nil {
		t.Fatal("a requester approving their own HIGH grant was accepted")
	}
}

func TestSoDIndependence(t *testing.T) {
	s := MustDefaultSoD()
	for name, tc := range map[string]struct {
		risk                         RiskClass
		requester, approver, grantee string
		code                         string
	}{
		"requester approves":         {RiskHigh, "a", "a", "c", CodeSelfApproval},
		"grantee approves":           {RiskHigh, "a", "c", "c", CodeSelfApproval},
		"requester is the grantee":   {RiskHigh, "a", "b", "a", CodeSelfApproval},
		"critical is held to it too": {RiskCritical, "a", "a", "c", CodeSelfApproval},
	} {
		err := s.Independence(KindIssuance, tc.risk, tc.requester, tc.approver, tc.grantee)
		var r *Refusal
		if !errors.As(err, &r) || r.Code != tc.code {
			t.Errorf("%s: want %s, got %v", name, tc.code, err)
		}
	}
	if err := s.Independence(KindIssuance, RiskHigh, "a", "b", "c"); err != nil {
		t.Errorf("three different principals were refused: %v", err)
	}
	// While only requester and grantee are known (planning), the rule still bites.
	if err := s.Independence(KindDelegation, RiskHigh, "a", "", "a"); err == nil {
		t.Error("a request for oneself was accepted while planning")
	}
	if err := s.Independence(KindDelegation, RiskHigh, "a", "", "c"); err != nil {
		t.Errorf("planning a request with no approver yet was refused: %v", err)
	}
	// Below the threshold the policy does not apply.
	if err := s.Independence(KindIssuance, RiskModerate, "a", "a", "c"); err != nil {
		t.Errorf("a MODERATE change was held to a HIGH policy: %v", err)
	}
}

func TestSoDGrantBound(t *testing.T) {
	s := MustDefaultSoD()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	within, beyond := from.Add(24*time.Hour), from.Add(24*time.Hour+time.Minute)
	if err := s.GrantBound(KindIssuance, RiskCritical, TypeStanding, from, nil); err == nil {
		t.Error("a STANDING CRITICAL grant was accepted")
	}
	if err := s.GrantBound(KindIssuance, RiskCritical, TypeTimeBound, from, &beyond); err == nil {
		t.Error("a CRITICAL grant beyond its bound was accepted")
	}
	if err := s.GrantBound(KindIssuance, RiskCritical, TypeTimeBound, from, &within); err != nil {
		t.Errorf("a bounded CRITICAL grant was refused: %v", err)
	}
	// Exactly 24 hours is allowed; a minute more is a new approval.
	// HIGH grants do not inherit the CRITICAL bound.
	long := from.Add(90 * 24 * time.Hour)
	if err := s.GrantBound(KindIssuance, RiskHigh, TypeTimeBound, from, &long); err != nil {
		t.Errorf("a long HIGH grant was held to the CRITICAL bound: %v", err)
	}
	if err := s.GrantBound(KindIssuance, RiskHigh, TypeStanding, from, nil); err != nil {
		t.Errorf("a STANDING HIGH grant was refused by a CRITICAL bound: %v", err)
	}
}

// The CRITICAL policy is the owner's decision (2026-10-01): never STANDING,
// at most 24 hours, a 1 hour JIT target, no inheritance by HIGH.
func TestCriticalPolicyIsTheOwnersDecision(t *testing.T) {
	var critical *SoDPolicy
	for i, p := range MustDefaultSoD().Policies {
		if p.RiskThreshold == RiskCritical && p.Status == "ACTIVE" && p.MaximumDurationHours > 0 {
			critical = &MustDefaultSoD().Policies[i]
		}
	}
	if critical == nil || critical.MaximumDurationHours != 24 || critical.JITTargetHours != 1 {
		t.Fatalf("the CRITICAL policy is %+v", critical)
	}
	for _, gt := range critical.AllowedGrantTypes {
		if gt == TypeStanding {
			t.Fatal("a CRITICAL grant may be STANDING")
		}
	}
}

func TestSoDConflictingPermissions(t *testing.T) {
	s := &SoD{}
	s.Conflicting = append(s.Conflicting, struct {
		Permissions []string `yaml:"permissions"`
	}{Permissions: []string{"audit.view", "audit.manage"}})
	if got := s.Conflict("audit.manage", []string{"tenant.view", "audit.view"}); got != "audit.view" {
		t.Errorf("conflict = %q", got)
	}
	if got := s.Conflict("audit.manage", []string{"tenant.view"}); got != "" {
		t.Errorf("a non-conflicting holder reported %q", got)
	}
	if got := MustDefaultSoD().Conflict("tenant.suspend", []string{"tenant.view"}); got != "" {
		t.Errorf("no sets are declared, yet %q conflicts", got)
	}
}

func TestIssuanceFailures(t *testing.T) {
	c, s := MustDefaultCatalogue(), MustDefaultSoD()
	tenant := Scope{Level: LevelTenant, TenantID: "tn_acmeug"}
	until := now.Add(48 * time.Hour)
	good := IssuanceFacts{Catalogue: c, SoD: s, Requester: "prn_maker", GranteeActive: true, Now: now,
		Request: IssueRequest{PrincipalID: "prn_jane", Permission: "tenant.suspend", Scope: tenant, GrantType: TypeTimeBound, ValidUntil: &until}}
	if f := IssuanceFailures(good); len(f) != 0 {
		t.Fatalf("a sound request failed: %v", f)
	}
	for name, tc := range map[string]struct {
		edit  func(*IssuanceFacts)
		check string
	}{
		"inactive grantee":    {func(f *IssuanceFacts) { f.GranteeActive = false }, CheckGranteeActive},
		"request for oneself": {func(f *IssuanceFacts) { f.Requester = "prn_jane" }, CheckRequesterNotGrantee},
		"unregistered":        {func(f *IssuanceFacts) { f.Request.Permission = "tenant.own" }, CheckPermissionGrantable},
		"wrong level":         {func(f *IssuanceFacts) { f.Request.Scope = Scope{Level: LevelMarket, MarketID: "mkt_x"} }, CheckPermissionGrantable},
		"malformed scope":     {func(f *IssuanceFacts) { f.Request.Scope = Scope{Level: LevelTenant} }, CheckPermissionGrantable},
		"not delegable":       {func(f *IssuanceFacts) { f.Request.Permission = "administrator.grant"; f.Request.DelegableDepth = 1 }, CheckPermissionGrantable},
		"end before start":    {func(f *IssuanceFacts) { past := now.Add(-time.Hour); f.Request.ValidUntil = &past }, CheckValidityWindow},
		"standing with end":   {func(f *IssuanceFacts) { f.Request.GrantType = TypeStanding }, CheckValidityWindow},
		"timed without end":   {func(f *IssuanceFacts) { f.Request.ValidUntil = nil }, CheckValidityWindow},
		"critical standing": {func(f *IssuanceFacts) {
			f.Request = IssueRequest{PrincipalID: "prn_jane", Permission: "administrator.grant", Scope: Scope{Level: LevelPlatform}, GrantType: TypeStanding}
		}, CheckValidityWindow},
		"already held": {func(f *IssuanceFacts) {
			g := tenantGrant("agr_held", "prn_jane", "tenant.suspend", "tn_acmeug")
			f.Held = []Grant{g}
		}, CheckNotAlreadyHeld},
	} {
		f := good
		tc.edit(&f)
		if got := IssuanceFailures(f); got[tc.check] == "" {
			t.Errorf("%s: %s did not fail (%v)", name, tc.check, got)
		}
	}
	// A revoked or expired grant is not "held".
	revoked := tenantGrant("agr_gone", "prn_jane", "tenant.suspend", "tn_acmeug")
	revoked.Status = StatusRevoked
	f := good
	f.Held = []Grant{revoked}
	if got := IssuanceFailures(f); len(got) != 0 {
		t.Errorf("a revoked grant blocked a new one: %v", got)
	}
}

func TestDelegationFailures(t *testing.T) {
	c, s := MustDefaultCatalogue(), MustDefaultSoD()
	end := now.Add(72 * time.Hour)
	source := delegableSource(&end)
	source.Permission = "tenant.suspend"
	source.RiskClass = RiskHigh
	good := DelegationFacts{Catalogue: c, SoD: s, Requester: "prn_jane", Source: source, GranteeActive: true, Now: now,
		Request: DelegationRequest{PrincipalID: "prn_bob", Permission: "tenant.suspend", Scope: source.Scope, ValidUntil: now.Add(24 * time.Hour), Reason: "cover"}}
	if f := DelegationFailures(good); len(f) != 0 {
		t.Fatalf("a sound delegation failed: %v", f)
	}
	for name, tc := range map[string]struct {
		edit  func(*DelegationFacts)
		check string
	}{
		"inactive delegate": {func(f *DelegationFacts) { f.GranteeActive = false }, CheckGranteeActive},
		"to oneself":        {func(f *DelegationFacts) { f.Request.PrincipalID = "prn_jane" }, CheckRequesterNotGrantee},
		"not the holder":    {func(f *DelegationFacts) { f.Requester = "prn_mallory" }, CheckRequesterHolds},
		"beyond the source": {func(f *DelegationFacts) { f.Request.ValidUntil = end.Add(time.Hour) }, CheckWithinSource},
		"another scope":     {func(f *DelegationFacts) { f.Request.Scope = Scope{Level: LevelPlatform} }, CheckWithinSource},
		"source not usable": {func(f *DelegationFacts) { f.Source.Status = StatusSuspended }, CheckWithinSource},
		"no hops left":      {func(f *DelegationFacts) { f.Source.DelegableDepth = 0 }, CheckWithinSource},
	} {
		f := good
		tc.edit(&f)
		if got := DelegationFailures(f); got[tc.check] == "" {
			t.Errorf("%s: %s did not fail (%v)", name, tc.check, got)
		}
	}
}

func TestApprovedPlansNeedTheirApproval(t *testing.T) {
	c := MustDefaultCatalogue()
	tenant := Scope{Level: LevelTenant, TenantID: "tn_acmeug"}
	until := now.Add(48 * time.Hour)
	q := IssueRequest{PrincipalID: "prn_jane", Permission: "tenant.suspend", Scope: tenant, GrantType: TypeTimeBound, ValidUntil: &until, Reason: "cover"}
	if _, err := PlanApprovedIssue(c, "prn_maker", q, now, ""); err == nil {
		t.Fatal("an approved issue without an approval was accepted")
	}
	g, err := PlanApprovedIssue(c, "prn_maker", q, now, "apd_01abc")
	if err != nil || g.ApprovalReference != "apd_01abc" || g.Source != SourceDirect || g.GrantedBy != "prn_maker" {
		t.Fatalf("approved issue: %+v %v", g, err)
	}
	// A start that has passed since planning begins now; authority is never backdated.
	passed := now.Add(-30 * time.Minute)
	q.ValidFrom = &passed
	if g, err = PlanApprovedIssue(c, "prn_maker", q, now, "apd_01abc"); err != nil || !g.ValidFrom.Equal(now) {
		t.Fatalf("a passed start was not clamped to now: %+v %v", g, err)
	}
	// The direct route still refuses HIGH, and says where to go.
	q.ValidFrom = nil
	if _, err := PlanIssue(c, "prn_maker", q, now); err == nil {
		t.Fatal("the direct route issued HIGH authority")
	} else {
		var r *Refusal
		if !errors.As(err, &r) || r.Code != CodeApprovalRequired {
			t.Fatalf("unexpected refusal %v", err)
		}
	}
}
