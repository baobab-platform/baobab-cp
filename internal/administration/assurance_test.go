package administration

import (
	"testing"
	"time"
)

func assuranceGrant(risk RiskClass, minimum string) Grant {
	g := Grant{GrantID: "agr_assure", PrincipalID: "prn_jane", Permission: "tenant.view", Scope: Scope{Level: LevelPlatform},
		GrantType: TypeStanding, Source: SourceDirect, RiskClass: risk, ValidFrom: time.Now().Add(-time.Hour), Status: StatusActive,
		GrantedBy: "prn_ops", Reason: "test", Version: 1}
	if minimum != "" {
		g.Conditions = &Conditions{MinimumACR: minimum}
	}
	return g
}

// TestAssurancePolicyIsTheOwnersLadder: the embedded policy is the ladder
// IAM issues today and the ADR-BCP-020 section 72 requirements per risk.
func TestAssurancePolicyIsTheOwnersLadder(t *testing.T) {
	p := MustDefaultAssurance()
	for risk, want := range map[RiskClass]string{
		RiskLow: "urn:baobab:acr:basic", RiskModerate: "urn:baobab:acr:basic",
		RiskHigh: "urn:baobab:acr:mfa", RiskCritical: "urn:baobab:acr:step-up",
	} {
		if got := p.Required(assuranceGrant(risk, "")).MinimumACR; got != want {
			t.Errorf("%s requires %s, want %s", risk, got, want)
		}
	}
	critical := p.Required(assuranceGrant(RiskCritical, ""))
	if critical.MaxAuthenticationAge != 300 || !critical.PhishingResistantRequired {
		t.Errorf("CRITICAL must demand fresh, phishing-resistant step-up: %+v", critical)
	}
}

func TestAssuranceSessionsMeetRequirementsByRank(t *testing.T) {
	p := MustDefaultAssurance()
	now := time.Now()
	for _, c := range []struct {
		name    string
		risk    RiskClass
		minimum string
		session Session
		want    bool
	}{
		{"password session, LOW", RiskLow, "", Session{ACR: "1"}, true},
		{"no acr at all, LOW: any verified authentication", RiskLow, "", Session{}, true},
		{"unlisted acr, LOW", RiskLow, "", Session{ACR: "weird"}, true},
		{"password session, HIGH", RiskHigh, "", Session{ACR: "1"}, false},
		{"reused SSO session, HIGH", RiskHigh, "", Session{ACR: "0"}, false},
		{"no acr, HIGH", RiskHigh, "", Session{}, false},
		{"unlisted acr, HIGH fails closed", RiskHigh, "", Session{ACR: "weird"}, false},
		{"OTP step-up (gold), HIGH", RiskHigh, "", Session{ACR: "2"}, true},
		{"OTP step-up by name, HIGH", RiskHigh, "", Session{ACR: "gold"}, true},
		{"grant condition above the risk class", RiskLow, "urn:baobab:acr:mfa", Session{ACR: "1"}, false},
		{"grant condition above the risk class, met", RiskLow, "urn:baobab:acr:mfa", Session{ACR: "2"}, true},
		{"grant condition below the risk class does not relax it", RiskHigh, "urn:baobab:acr:basic", Session{ACR: "1"}, false},
		{"a level the ladder does not list is never met", RiskLow, "urn:other:acr", Session{ACR: "2"}, false},
		{"step-up level IAM does not issue is met by nobody", RiskLow, "urn:baobab:acr:step-up", Session{ACR: "2", AMR: []string{"hwk"}, AuthenticatedAt: now}, false},
		{"CRITICAL even with the best session today", RiskCritical, "", Session{ACR: "2", AMR: []string{"hwk"}, AuthenticatedAt: now}, false},
	} {
		if got := p.Met(p.Required(assuranceGrant(c.risk, c.minimum)), c.session, now); got != c.want {
			t.Errorf("%s: met = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAssuranceFreshnessAndMethods exercises the freshness and
// phishing-resistant rules on a requirement a future IAM level could meet.
func TestAssuranceFreshnessAndMethods(t *testing.T) {
	p := MustDefaultAssurance()
	now := time.Now()
	req := AssuranceRequirement{MinimumACR: "urn:baobab:acr:mfa", MaxAuthenticationAge: 300, PhishingResistantRequired: true}
	good := Session{ACR: "2", AMR: []string{"pwd", "webauthn"}, AuthenticatedAt: now.Add(-time.Minute)}
	if !p.Met(req, good, now) {
		t.Fatal("a fresh, phishing-resistant, step-up session must meet it")
	}
	for name, s := range map[string]Session{
		"stale":                         {ACR: "2", AMR: good.AMR, AuthenticatedAt: now.Add(-6 * time.Minute)},
		"authenticated in the future":   {ACR: "2", AMR: good.AMR, AuthenticatedAt: now.Add(time.Minute)},
		"no authentication time":        {ACR: "2", AMR: good.AMR},
		"OTP is not phishing-resistant": {ACR: "2", AMR: []string{"pwd", "otp"}, AuthenticatedAt: now},
		"no methods":                    {ACR: "2", AuthenticatedAt: now},
	} {
		if p.Met(req, s, now) {
			t.Errorf("%s must not meet the requirement", name)
		}
	}
	// A step-up moves freshness forward without a new login.
	stepped := Session{ACR: "2", AMR: good.AMR, AuthenticatedAt: now.Add(-time.Hour), StepUpAt: now.Add(-time.Minute)}
	if !p.Met(req, stepped, now) {
		t.Error("a recent step-up must satisfy freshness, whatever the login age")
	}
}

func TestEvaluateAsksForStepUpWhenAssuranceIsInsufficient(t *testing.T) {
	now := time.Now()
	high := assuranceGrant(RiskHigh, "")
	request := func(s Session) Decision {
		return Evaluate(Request{PrincipalID: "prn_jane", PrincipalActive: true, Action: "tenant.view", Resource: Resource{TenantID: "tn_ug"},
			Now: now, Grants: []Grant{high}, Session: s})
	}
	if d := request(Session{ACR: "1"}); d.Outcome != OutcomeStepUpRequired || d.ReasonCodes[0] != "AUTHENTICATION_ASSURANCE_INSUFFICIENT" {
		t.Errorf("a password session on a HIGH grant must ask for step-up: %+v", d)
	}
	if d := request(Session{ACR: "2"}); !d.Allowed() {
		t.Errorf("a step-up session must allow: %+v", d)
	}
	// Assurance is never permission (section 74): the best session alone
	// grants nothing.
	if d := Evaluate(Request{PrincipalID: "prn_jane", PrincipalActive: true, Action: "tenant.view", Resource: Resource{TenantID: "tn_ug"},
		Now: now, Session: Session{ACR: "2"}}); d.Allowed() || d.ReasonCodes[0] != "NO_ADMINISTRATIVE_GRANT" {
		t.Errorf("assurance must not create authority: %+v", d)
	}
}

func TestIssuanceRefusesAnUnknownAssuranceLevel(t *testing.T) {
	c := MustDefaultCatalogue()
	now := time.Now()
	issue := func(minimum string) error {
		_, err := PlanIssue(c, "prn_ops", IssueRequest{PrincipalID: "prn_jane", Permission: "tenant.view", Scope: Scope{Level: LevelPlatform},
			GrantType: TypeStanding, Conditions: &Conditions{MinimumACR: minimum}, Reason: "test"}, now)
		return err
	}
	if err := issue("urn:baobab:acr:mfa"); err != nil {
		t.Errorf("a ladder level must be accepted: %v", err)
	}
	if err := issue("gold"); err == nil {
		t.Error("a raw IAM acr is not a ladder level and must be refused")
	}
	if err := issue("urn:nope"); err == nil {
		t.Error("an unlisted level must be refused")
	}
}

func TestWeakerAssuranceIsComparedByWhatIsRequired(t *testing.T) {
	low := func(min string) Grant { return assuranceGrant(RiskLow, min) }
	for _, c := range []struct {
		name      string
		old, next Grant
		want      bool
	}{
		{"same condition", low("urn:baobab:acr:mfa"), low("urn:baobab:acr:mfa"), false},
		{"dropped condition", low("urn:baobab:acr:mfa"), low(""), true},
		{"lowered", low("urn:baobab:acr:mfa"), low("urn:baobab:acr:basic"), true},
		{"raised", low("urn:baobab:acr:basic"), low("urn:baobab:acr:mfa"), false},
		{"added", low(""), low("urn:baobab:acr:mfa"), false},
		{"unlisted old, changed", low("urn:legacy"), low("urn:baobab:acr:mfa"), true},
		{"risk-implied mfa spelt out is no change", assuranceGrant(RiskHigh, ""), assuranceGrant(RiskHigh, "urn:baobab:acr:mfa"), false},
	} {
		if got := weakerAssurance(c.old, c.next); got != c.want {
			t.Errorf("%s: weaker = %v, want %v", c.name, got, c.want)
		}
	}
}
