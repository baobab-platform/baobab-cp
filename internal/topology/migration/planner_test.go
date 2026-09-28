package migration

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/health"
)

var (
	now        = time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	planSchema = contracts.MustSchema("control-plane/v1/provider-migration.schema.json#/$defs/ProviderMigrationPlan")
)

type fakeFacts struct {
	bindings  []SourceBinding
	providers map[string]Provider
	instances map[string][]Instance
	open      []string
}

func (f *fakeFacts) SourceBindings(_ context.Context, provider string, keys []string) ([]SourceBinding, error) {
	var out []SourceBinding
	for _, b := range f.bindings {
		if provider == "baobab-erp.idempiere" && slices.Contains(keys, b.CapabilityKey) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeFacts) Provider(_ context.Context, key string) (Provider, bool, error) {
	p, ok := f.providers[key]
	return p, ok, nil
}

func (f *fakeFacts) ProviderInstances(_ context.Context, key string, _ []string) ([]Instance, error) {
	return f.instances[key], nil
}

func (f *fakeFacts) OpenMigrations(context.Context, string, []string) ([]string, error) {
	return f.open, nil
}

func healthy(capabilities ...string) map[string]health.Levels {
	levels := map[string]health.Levels{}
	for _, c := range capabilities {
		levels[c] = health.Levels{EngineInstance: &health.Observation{Subject: health.Subject{EngineInstanceID: "ei_target1"},
			Status: health.StatusHealthy, ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Source: health.SourceActiveProbe}}
	}
	return levels
}

// world is a stateless two-tenant migration whose target is ready.
func world() (*fakeFacts, Request) {
	facts := &fakeFacts{
		bindings: []SourceBinding{
			{BindingID: "bind_beta0001", CapabilityKey: "finance.invoice.read", ContractVersion: 1, TenantID: "tn_beta", Markets: []string{"ZA"}, Region: "af-south-1", Environment: "production"},
			{BindingID: "bind_alpha001", CapabilityKey: "finance.invoice.read", ContractVersion: 1, TenantID: "tn_alpha", EstateID: "alpha_web", Markets: []string{"KE"}, Region: "af-south-1", Environment: "production"},
		},
		providers: map[string]Provider{"baobab-erp.nextledger": {Status: "ACTIVE", Support: map[string][]int{"finance.invoice.read": {1, 2}}}},
		instances: map[string][]Instance{"baobab-erp.nextledger": {{EngineInstanceID: "ei_target1", Region: "af-south-1", Environment: "production",
			Status: "ACTIVE", Health: healthy("finance.invoice.read")}}},
	}
	request := Request{
		SourceProviderKey: "baobab-erp.idempiere", TargetProviderKey: "baobab-erp.nextledger",
		Capabilities:  []Capability{{CapabilityKey: "finance.invoice.read", ContractVersion: 1}},
		MigrationMode: ModeStatelessRebind, DataStrategy: DataNone, RollbackStrategy: "REBIND_SOURCE",
		Cohorts: []Cohort{{CohortKey: "canary", Selector: &Selector{TenantIDs: []string{"tn_alpha"}}}, {CohortKey: "rest"}},
		Owners:  []string{"prn_owner1"}, Reason: "Replace the ledger provider.",
	}
	return facts, request
}

func plan(t *testing.T, facts *fakeFacts, r Request) Plan {
	t.Helper()
	p, err := Planner{Facts: facts, Policy: health.MustDefaultPolicy()}.Plan(context.Background(),
		Input{Request: r, ProviderMigrationID: "pmg_test1", PlanID: "plan_test1", PlanVersion: 1, BaseRevision: 1, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.ValidateValue(planSchema, p); err != nil {
		t.Fatalf("the plan does not conform to the Shared contract: %v", err)
	}
	return p
}

func codes(findings []Finding) []string {
	out := []string{}
	for _, f := range findings {
		out = append(out, f.Code)
	}
	return out
}

func operations(p Plan) []string {
	out := []string{}
	for _, s := range p.Steps {
		out = append(out, s.StepID+":"+s.Operation)
	}
	return out
}

// TestPlanDiscoversAndAssignsEveryContext: discovery comes from
// authoritative state, each context lands in exactly one cohort, and a
// ready target yields an unblocked plan with a single chain of steps.
func TestPlanDiscoversAndAssignsEveryContext(t *testing.T) {
	facts, request := world()
	p := plan(t, facts, request)
	if len(p.Blockers) != 0 {
		t.Fatalf("blockers: %v", p.Blockers)
	}
	if p.Discovery.BindingCount != 2 || !slices.Equal(p.Discovery.TenantIDs, []string{"tn_alpha", "tn_beta"}) ||
		!slices.Equal(p.Discovery.Markets, []string{"KE", "ZA"}) || !slices.Equal(p.Discovery.EstateIDs, []string{"alpha_web"}) {
		t.Fatalf("discovery: %+v", p.Discovery)
	}
	if got := p.Discovery.Cohorts; got[0].BindingCount != 1 || got[0].TenantCount != 1 || got[1].BindingCount != 1 {
		t.Fatalf("cohorts: %+v", got)
	}
	want := []string{"verify-target:VERIFY_TARGET_READINESS", "bind-finance-invoice-read-ei-target1:CREATE_MIGRATION_BINDING",
		"canary-shift-cohort:SHIFT_COHORT", "canary-validate-cohort:VALIDATE_COHORT",
		"rest-shift-cohort:SHIFT_COHORT", "rest-validate-cohort:VALIDATE_COHORT",
		"retire-finance-invoice-read:RETIRE_SOURCE_BINDING"}
	if !slices.Equal(operations(p), want) {
		t.Fatalf("steps:\n%v\nwant\n%v", operations(p), want)
	}
	for i, s := range p.Steps[1:] {
		if !slices.Equal(s.DependsOn, []string{p.Steps[i].StepID}) {
			t.Fatalf("step %s depends on %v", s.StepID, s.DependsOn)
		}
	}
	if r := p.Steps[1].Resources; r.EngineInstanceID != "ei_target1" || !slices.Equal(r.BindingIDs, []string{"bind_alpha001", "bind_beta0001"}) || p.RiskClass != "HIGH" {
		t.Fatalf("bind step %+v, risk %s", p.Steps[1].Resources, p.RiskClass)
	}
}

// TestPlanFixesEachBindingsTargetInstance: bindings that need different
// target instances each get a step naming exactly them and their instance,
// so nothing is re-resolved after approval.
func TestPlanFixesEachBindingsTargetInstance(t *testing.T) {
	facts, request := world()
	facts.bindings[0].Region = "eu-west-1" // bind_beta0001
	facts.instances["baobab-erp.nextledger"] = append(facts.instances["baobab-erp.nextledger"], Instance{EngineInstanceID: "ei_target2",
		Region: "eu-west-1", Environment: "production", Status: "ACTIVE", Health: healthy("finance.invoice.read")})
	p := plan(t, facts, request)
	var got []string
	for _, s := range p.Steps {
		if s.Operation == OpCreateMigrationBinding {
			got = append(got, s.Resources.EngineInstanceID+"="+strings.Join(s.Resources.BindingIDs, ","))
		}
	}
	if want := []string{"ei_target1=bind_alpha001", "ei_target2=bind_beta0001"}; !slices.Equal(got, want) || len(p.Blockers) != 0 {
		t.Fatalf("binding assignments %v, want %v; blockers %v", got, want, p.Blockers)
	}
}

// TestPlanChecksEveryBindingsContractVersion: the versions discovered
// bindings actually require decide compatibility, not only the version
// the request names.
func TestPlanChecksEveryBindingsContractVersion(t *testing.T) {
	facts, request := world()
	facts.bindings[0].ContractVersion = 2
	if got := codes(plan(t, facts, request).Blockers); !slices.Equal(got, []string{BlockTargetContractIncompatible}) {
		t.Fatalf("a v2 binding under a v1 request: %v", got)
	}
	facts.providers["baobab-erp.nextledger"] = Provider{Status: "ACTIVE", Support: map[string][]int{"finance.invoice.read": {1}}}
	p := plan(t, facts, request)
	messages := []string{}
	for _, b := range p.Blockers {
		messages = append(messages, b.Message)
	}
	if !strings.Contains(strings.Join(messages, " "), "does not support finance.invoice.read contract version 2") {
		t.Fatalf("a v1-only target for a v2 binding: %v", messages)
	}
	facts.bindings[0].ContractVersion = 0
	if got := codes(plan(t, facts, request).Blockers); !slices.Contains(got, BlockTargetContractIncompatible) {
		t.Fatalf("an unreadable binding version: %v", got)
	}
}

// TestStatefulCohortsKeepOneWriter: every stateful cohort freezes, moves
// and reconciles its data before its authority shifts (sections 55-56).
func TestStatefulCohortsKeepOneWriter(t *testing.T) {
	facts, request := world()
	request.MigrationMode, request.DataStrategy = ModeStatefulCutover, "BULK_MIGRATE_THEN_CUTOVER"
	request.CutoverWindow = &CutoverWindow{StartsAt: now.Add(24 * time.Hour), EndsAt: now.Add(30 * time.Hour)}
	p := plan(t, facts, request)
	for _, cohort := range []string{"canary", "rest"} {
		var ops []string
		for _, s := range p.Steps {
			if s.Resources.CohortKey == cohort {
				ops = append(ops, s.Operation)
			}
		}
		if want := append(slices.Clone(statefulSequence), OpValidateCohort); !slices.Equal(ops, want) {
			t.Fatalf("cohort %s runs %v, want %v", cohort, ops, want)
		}
	}
	if p.RiskClass != "CRITICAL" || p.ImpactAnalysis.AvailabilityImpact == "" {
		t.Fatalf("a stateful cutover is CRITICAL and states its availability impact: %s %q", p.RiskClass, p.ImpactAnalysis.AvailabilityImpact)
	}
}

// TestPlanBlocksEveryUnmetPrecondition: each section 48 precondition and
// cohort rule that fails is a registered blocker, never an error.
func TestPlanBlocksEveryUnmetPrecondition(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*fakeFacts, *Request)
		want   string
	}{
		"unregistered target": {func(f *fakeFacts, _ *Request) { delete(f.providers, "baobab-erp.nextledger") }, BlockTargetNotRegistered},
		"suspended target": {func(f *fakeFacts, _ *Request) {
			f.providers["baobab-erp.nextledger"] = Provider{Status: "SUSPENDED", Support: f.providers["baobab-erp.nextledger"].Support}
		}, BlockTargetNotRegistered},
		"unsupported capability": {func(f *fakeFacts, _ *Request) {
			f.providers["baobab-erp.nextledger"] = Provider{Status: "ACTIVE", Support: map[string][]int{}}
		}, BlockTargetNotSupported},
		"incompatible contract": {func(_ *fakeFacts, r *Request) { r.Capabilities[0].ContractVersion = 3 }, BlockTargetContractIncompatible},
		"unobserved target": {func(f *fakeFacts, _ *Request) {
			f.instances["baobab-erp.nextledger"][0].Health = nil
		}, BlockTargetUnhealthy},
		"target in another region": {func(f *fakeFacts, _ *Request) {
			f.instances["baobab-erp.nextledger"][0].Region = "eu-west-1"
		}, BlockTargetNotEligible},
		"unassigned context": {func(_ *fakeFacts, r *Request) { r.Cohorts = r.Cohorts[:1] }, BlockContextUnassigned},
		"overlapping cohorts": {func(_ *fakeFacts, r *Request) {
			r.Cohorts = []Cohort{{CohortKey: "a", Selector: &Selector{Regions: []string{"af-south-1"}}}, {CohortKey: "b", Selector: &Selector{Markets: []string{"KE"}}}, {CohortKey: "rest"}}
		}, BlockCohortOverlap},
		"shadow":          {func(_ *fakeFacts, r *Request) { r.Shadow = true }, BlockShadowUnsafe},
		"open migration":  {func(f *fakeFacts, _ *Request) { f.open = []string{"pmg_other1"} }, BlockAlreadyInProgress},
		"nothing to move": {func(f *fakeFacts, _ *Request) { f.bindings = nil }, BlockSourceNotBound},
	} {
		t.Run(name, func(t *testing.T) {
			facts, request := world()
			tc.change(facts, &request)
			if got := codes(plan(t, facts, request).Blockers); !slices.Contains(got, tc.want) {
				t.Fatalf("blockers %v, want %s", got, tc.want)
			}
		})
	}
	// The migration's own id is not a competing migration.
	facts, request := world()
	facts.open = []string{"pmg_test1"}
	if got := codes(plan(t, facts, request).Blockers); len(got) != 0 {
		t.Fatalf("a migration blocked itself: %v", got)
	}
}

// TestPlanWarnsWithoutBlocking: an empty cohort and an irreversible
// rollback are warnings, never blockers.
func TestPlanWarnsWithoutBlocking(t *testing.T) {
	facts, request := world()
	request.RollbackStrategy = RollbackForwardFixOnly
	request.Cohorts = []Cohort{{CohortKey: "nobody", Selector: &Selector{TenantIDs: []string{"tn_nobody"}}}, {CohortKey: "rest"}}
	p := plan(t, facts, request)
	if got := codes(p.Warnings); !slices.Equal(got, []string{WarnCohortEmpty, WarnNotReversible}) || len(p.Blockers) != 0 {
		t.Fatalf("warnings %v blockers %v", got, p.Blockers)
	}
	for _, s := range p.Steps {
		if s.Irreversible != (s.Operation == OpShiftCohort) {
			t.Fatalf("step %s irreversible=%v under FORWARD_FIX_ONLY", s.StepID, s.Irreversible)
		}
	}
}

// TestPlanningIsDeterministic: the same request against the same state
// yields the same material and digest, whatever order state is read in.
func TestPlanningIsDeterministic(t *testing.T) {
	facts, request := world()
	first := plan(t, facts, request)
	slices.Reverse(facts.bindings)
	second := plan(t, facts, request)
	if Material(first) != Material(second) || first.PlanDigest != second.PlanDigest {
		t.Fatal("reordered authoritative state changed the plan")
	}
	// The request is part of the material: an approval covers every
	// strategy the plan executes, not only its steps.
	if first.Request.RollbackStrategy != request.RollbackStrategy {
		t.Fatal("the plan does not embed its request")
	}
	changed := request
	changed.RollbackStrategy = "RESTORE_AND_REBIND_SOURCE"
	if Material(plan(t, facts, changed)) == Material(first) {
		t.Fatal("a different rollback strategy produced the same material")
	}
	facts.bindings = facts.bindings[:1]
	if Material(plan(t, facts, request)) == Material(first) {
		t.Fatal("a different binding set produced the same material")
	}
}

// TestValidateRefusesContradictions: rules the schema cannot express.
func TestValidateRefusesContradictions(t *testing.T) {
	_, base := world()
	for name, change := range map[string]func(*Request){
		"same provider": func(r *Request) { r.TargetProviderKey = r.SourceProviderKey },
		"open cohort not last": func(r *Request) {
			r.Cohorts = []Cohort{{CohortKey: "rest"}, {CohortKey: "late", Selector: &Selector{TenantIDs: []string{"tn_x"}}}}
		},
		"repeated cohort": func(r *Request) {
			r.Cohorts = []Cohort{{CohortKey: "a", Selector: &Selector{TenantIDs: []string{"tn_x"}}}, {CohortKey: "a"}}
		},
		"repeated capability": func(r *Request) { r.Capabilities = append(r.Capabilities, r.Capabilities[0]) },
		"inverted window": func(r *Request) {
			r.CutoverWindow = &CutoverWindow{StartsAt: now, EndsAt: now.Add(-time.Hour)}
		},
	} {
		r := base
		r.Cohorts, r.Capabilities = slices.Clone(base.Cohorts), slices.Clone(base.Capabilities)
		change(&r)
		if err := Validate(r); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// TestLongCapabilityKeysKeepContractStepIDs: step ids stay within the
// contract's 64 characters and unique.
func TestLongCapabilityKeysKeepContractStepIDs(t *testing.T) {
	long := "finance." + strings.Repeat("ledger", 12) + ".issue"
	a, b := stepID("bind", long), stepID("bind", long+"x")
	if len(a) > 64 || a == b || !strings.HasPrefix(a, "bind-finance-") {
		t.Fatalf("step ids %q and %q", a, b)
	}
}
