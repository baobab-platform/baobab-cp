package convergence

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
)

type fakeRegistry struct {
	products   map[string]Product
	members    map[string][]Member
	markets    map[string]bool
	capability map[string]bool
	candidates map[string][]Candidate
}

func (f fakeRegistry) PlanningProduct(_ context.Context, id string) (Product, bool, error) {
	p, ok := f.products[id]
	return p, ok, nil
}
func (f fakeRegistry) PlanningComposition(_ context.Context, key string) ([]Member, bool, error) {
	m, ok := f.members[key]
	return m, ok, nil
}
func (f fakeRegistry) PlanningMarket(_ context.Context, code string) (bool, error) {
	return f.markets[code], nil
}
func (f fakeRegistry) PlanningCapability(_ context.Context, key string) (bool, error) {
	return f.capability[key], nil
}
func (f fakeRegistry) PlanningCandidates(_ context.Context, key string) ([]Candidate, error) {
	return f.candidates[key], nil
}

func registry() fakeRegistry {
	return fakeRegistry{
		products: map[string]Product{"baobab-xbt": {ProductID: "baobab-xbt", CompositionKey: "solution.baobab-xbt"}},
		members: map[string][]Member{"solution.baobab-xbt": {
			{CapabilityKey: "commerce.order.manage", Criticality: CriticalityMandatory},
			{CapabilityKey: "insight.report.view", Criticality: CriticalityOptional},
		}},
		markets:    map[string]bool{"UG": true, "ZA": true},
		capability: map[string]bool{"commerce.order.manage": true, "insight.report.view": true},
		candidates: map[string][]Candidate{
			"commerce.order.manage": {
				{ProviderKey: "baobab-trade.medusa", EngineID: "baobab-trade", EngineInstanceID: "ei_0000000000000000000000000000000b", Region: "af-south-1", Environment: "production", ProductionPermitted: true},
				{ProviderKey: "baobab-trade.medusa", EngineID: "baobab-trade", EngineInstanceID: "ei_0000000000000000000000000000000a", Region: "af-south-1", Environment: "production", ProductionPermitted: true},
				{ProviderKey: "baobab-trade.medusa", EngineID: "baobab-trade", EngineInstanceID: "ei_0000000000000000000000000000000c", Region: "eu-west-1", Environment: "production", ProductionPermitted: true},
			},
		},
	}
}

func desired(t *testing.T) DesiredState {
	t.Helper()
	at := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	d, err := FreezeDesiredState(domain.TenantOnboardingRequest{
		ID: "tor_0199a1b2c3d47e8f", AdmissionDecisionID: "adm_0199a1b2c3d47e8f", Status: domain.OnboardingFulfilled,
		AuthorisedBy: "prn_authoriser", TenantID: "tn_acme",
		DesiredState: domain.OnboardingDesiredState{DisplayName: "Acme Uganda", ResidencyRegion: "af-south-1",
			IsolationStrategy: "row_level_security", MarketScope: []string{"UG", "za", "UG"}, ProductRequirements: []string{"baobab-xbt"},
			MarketParticipation: []domain.OnboardingMarketParticipation{
				{Market: "UG", Activities: []string{"LEGAL_PRESENCE", "SOURCING", "EXPORTING"}},
				{Market: "za", Activities: []string{"SELLING", "IMPORTING"}}}},
	}, "tn_acme", 1, at)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func plan(t *testing.T, r Registry, environment string, d DesiredState) Plan {
	t.Helper()
	p, err := Planner{Registry: r, Environment: environment}.Plan(context.Background(), Input{Desired: d,
		TenantProvisioningID: "tp_0199a1b2c3d47e8f", PlanID: "plan_0199a1b2c3d47e8f", PlanVersion: 1, BaseRevision: 1,
		Now: time.Date(2026, 9, 27, 9, 0, 1, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func step(p Plan, id string) (Step, bool) {
	for _, s := range p.Steps {
		if s.StepID == id {
			return s, true
		}
	}
	return Step{}, false
}

func TestDesiredStateIsFrozenFromTheAuthorisedRequestOnly(t *testing.T) {
	d := desired(t)
	if len(d.MarketParticipation) != 2 || d.MarketParticipation[1].Market != "ZA" || len(d.MarketParticipation[1].Activities) != 2 ||
		d.DesiredStateDigest != DesiredStateDigest(d) {
		t.Fatalf("desired state: %+v", d)
	}
	for label, request := range map[string]domain.TenantOnboardingRequest{
		"authorised but not fulfilled": {Status: domain.OnboardingAuthorised, AuthorisedBy: "prn_a", TenantID: "tn_acme"},
		"fulfilled for another tenant": {Status: domain.OnboardingFulfilled, AuthorisedBy: "prn_a", TenantID: "tn_other"},
		"never authorised":             {Status: domain.OnboardingFulfilled, TenantID: "tn_acme"},
	} {
		if _, err := FreezeDesiredState(request, "tn_acme", 1, time.Now()); err != ErrOnboardingNotAuthorised {
			t.Errorf("%s: %v", label, err)
		}
	}
}

func TestPlanGrantsBindsAndVerifies(t *testing.T) {
	p := plan(t, registry(), "production", desired(t))
	if len(p.Blockers) != 0 || p.RiskClass != "HIGH" || !p.ExpiresAt.Equal(p.GeneratedAt.Add(24*time.Hour)) {
		t.Fatalf("plan: blockers %v risk %s expires %s", p.Blockers, p.RiskClass, p.ExpiresAt)
	}
	bind, ok := step(p, "bind-commerce-order-manage")
	// The residency-compatible instance with the lowest id wins, whatever order the registry returns.
	if !ok || bind.Resources.EngineInstanceID != "ei_0000000000000000000000000000000a" || bind.Resources.SystemNamespace != "medusa" ||
		bind.DependsOn[0] != "grant-commerce-order-manage" {
		t.Fatalf("binding step: %+v", bind)
	}
	if _, ok := step(p, "market-ug"); !ok {
		t.Fatal("no market participation step for UG")
	}
	// The optional capability has no provider: granted, not bound, and a warning, not a blocker.
	if _, ok := step(p, "grant-insight-report-view"); !ok || len(p.Warnings) != 1 || p.Warnings[0].Code != "NO_PROVIDER" {
		t.Fatalf("optional capability: warnings %v", p.Warnings)
	}
	if p.PlanDigest != PlanDigest(p) || !strings.HasPrefix(p.PlanDigest, "sha256:") {
		t.Fatalf("digest %s", p.PlanDigest)
	}
}

func TestPlanningIsDeterministicAndDigestBound(t *testing.T) {
	d := desired(t)
	a, b := plan(t, registry(), "production", d), plan(t, registry(), "production", d)
	if Material(a) != Material(b) || a.PlanDigest != b.PlanDigest {
		t.Fatal("the same desired state against the same state planned differently")
	}
	next := a
	next.PlanVersion = 2
	if PlanDigest(next) == a.PlanDigest || Material(next) != Material(a) {
		t.Fatal("a new plan version must have a new digest over the same material")
	}
	// Authoritative state changing (the chosen instance goes away) changes the material: the plan is stale.
	changed := registry()
	changed.candidates["commerce.order.manage"] = changed.candidates["commerce.order.manage"][:1]
	if Material(plan(t, changed, "production", d)) == Material(a) {
		t.Fatal("changed topology did not change the plan's material")
	}
}

func TestEligibilityFailsClosed(t *testing.T) {
	d := desired(t)
	notPermitted := registry()
	for i := range notPermitted.candidates["commerce.order.manage"] {
		notPermitted.candidates["commerce.order.manage"][i].ProductionPermitted = false
	}
	for label, c := range map[string]struct {
		registry    fakeRegistry
		environment string
		code        string
	}{
		"production, provider not permitted": {notPermitted, "", "NO_PRODUCTION_PERMITTED_PROVIDER"},
		"no instance in the residency region": {func() fakeRegistry {
			r := registry()
			r.candidates["commerce.order.manage"] = r.candidates["commerce.order.manage"][2:]
			return r
		}(), "production", "NO_RESIDENCY_COMPLIANT_PROVIDER"},
		"market not active":                 {func() fakeRegistry { r := registry(); delete(r.markets, "ZA"); return r }(), "production", "MARKET_NOT_AVAILABLE"},
		"product without an active version": {func() fakeRegistry { r := registry(); delete(r.products, "baobab-xbt"); return r }(), "production", "PRODUCT_NOT_AVAILABLE"},
	} {
		p := plan(t, c.registry, c.environment, d)
		if len(p.Blockers) != 1 || p.Blockers[0].Code != c.code {
			t.Errorf("%s: blockers %v", label, p.Blockers)
		}
	}
	// Outside production, a provider not permitted in production is eligible, and risk is lower.
	if p := plan(t, notPermitted, "development", d); len(p.Blockers) != 0 || p.RiskClass != "MEDIUM" {
		t.Errorf("development: blockers %v risk %s", p.Blockers, p.RiskClass)
	}
}

func TestStepIDsStayWithinTheContract(t *testing.T) {
	id := stepID("bind", "averyveryverylongdomainname.averyveryverylongresource.averyveryverylongaction")
	if len(id) > 64 || !strings.HasPrefix(id, "bind-averyvery") {
		t.Fatalf("step id %q", id)
	}
}

func TestUndeclaredActivitiesBlockTheMarket(t *testing.T) {
	d := desired(t)
	d.MarketParticipation[1].Activities = []string{}
	p := plan(t, registry(), "production", d)
	if len(p.Blockers) != 1 || p.Blockers[0].Code != "MARKET_ACTIVITIES_UNDECLARED" {
		t.Fatalf("blockers %v", p.Blockers)
	}
	if _, ok := step(p, "market-za"); ok {
		t.Fatal("a market without declared activities was planned")
	}
}

// TestPlanningAppliesTheHealthPolicy: the planner judges candidates' health
// as resolution does (ADR-BCP-006 section 22), so it never plans a binding
// resolution would refuse.
func TestPlanningAppliesTheHealthPolicy(t *testing.T) {
	d := desired(t)
	at := time.Date(2026, 9, 27, 9, 0, 1, 0, time.UTC)
	observed := func(instance string, status health.Status) *health.Observation {
		o := &health.Observation{Subject: health.Subject{EngineInstanceID: instance}, Status: status,
			ObservedAt: at.Add(-time.Second), ExpiresAt: at.Add(time.Minute), Source: health.SourceActiveProbe}
		if status != health.StatusHealthy {
			o.Reasons = []string{"HEALTH_PROBE_FAILED"}
		}
		return o
	}
	const first, second = "ei_0000000000000000000000000000000a", "ei_0000000000000000000000000000000b"
	with := func(criticality health.Criticality, levels map[string]*health.Observation) fakeRegistry {
		r := registry()
		cs := r.candidates["commerce.order.manage"]
		for i := range cs {
			cs[i].HealthCriticality = criticality
			cs[i].Health = health.Levels{EngineInstance: levels[cs[i].EngineInstanceID]}
		}
		return r
	}
	bound := func(p Plan) string {
		s, ok := step(p, "bind-commerce-order-manage")
		if !ok {
			return ""
		}
		return s.Resources.EngineInstanceID
	}

	// Unobserved instances still serve a STANDARD capability: no change from today.
	if p := plan(t, with(health.CriticalityStandard, nil), "production", d); len(p.Blockers) != 0 || bound(p) != first {
		t.Errorf("standard, unobserved: blockers %v bound %s", p.Blockers, bound(p))
	}
	// A DEGRADED instance is passed over for a healthy one.
	p := plan(t, with(health.CriticalityStandard, map[string]*health.Observation{
		first: observed(first, health.StatusDegraded), second: observed(second, health.StatusHealthy)}), "production", d)
	if len(p.Blockers) != 0 || bound(p) != second {
		t.Errorf("standard, first degraded: blockers %v bound %s", p.Blockers, bound(p))
	}
	// A CRITICAL capability takes only an instance observed HEALTHY.
	p = plan(t, with(health.CriticalityCritical, map[string]*health.Observation{second: observed(second, health.StatusHealthy)}), "production", d)
	if len(p.Blockers) != 0 || bound(p) != second {
		t.Errorf("critical, only second observed: blockers %v bound %s", p.Blockers, bound(p))
	}
	// With none observed, a mandatory CRITICAL capability blocks the plan.
	p = plan(t, with(health.CriticalityCritical, nil), "production", d)
	if len(p.Blockers) != 1 || p.Blockers[0].Code != "NO_HEALTHY_PROVIDER" || !strings.Contains(p.Blockers[0].Message, "healthy enough") {
		t.Errorf("critical, unobserved: blockers %v", p.Blockers)
	}
}
