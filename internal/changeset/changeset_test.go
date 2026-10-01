package changeset

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

var (
	now          = time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	planSchema   = contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangesetPlan")
	recordSchema = contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/Changeset")
)

const tenant = "tn_0199a1b2c3d47e8f9a0b1c2d3e4f5a6b"

func suspension(t *testing.T) Changeset {
	t.Helper()
	c, err := Draft(CreateRequest{Title: "Suspend", Reason: "Past due.", DesiredChange: DesiredChange{Kind: KindTenantSuspension, TenantID: tenant}},
		"cs_0199a1b2c3d47e8f", "prn_requester1", "API", "", 4, now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestDraftDerivesWhatTheCallerMustNotSupply: type, scope, source,
// requester and base revision come from the kind, the target and the
// verified caller, and the draft conforms to the contract.
func TestDraftDerivesWhatTheCallerMustNotSupply(t *testing.T) {
	c := suspension(t)
	if c.ChangesetType != "SUSPEND" || c.TargetScope.TenantID != tenant || c.State != StateDraft || c.BaseRevision != 4 || c.Revision != 1 {
		t.Fatalf("draft: %+v", c)
	}
	if err := contracts.ValidateValue(recordSchema, c); err != nil {
		t.Fatalf("the draft does not conform: %v", err)
	}
	if _, err := Draft(CreateRequest{DesiredChange: DesiredChange{Kind: "TENANT_DELETION", TenantID: tenant}}, "cs_x1", "p", "API", "", 1, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unsupported kind: %v", err)
	}
	if _, err := Draft(CreateRequest{DesiredChange: DesiredChange{Kind: KindTenantSuspension, TenantID: "acme"}}, "cs_x1", "p", "API", "", 1, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a malformed tenant: %v", err)
	}
}

// TestLifecycleIsSharedAndEnforced: transitions come from the embedded
// Shared lifecycle; an illegal one is refused (section 211).
func TestLifecycleIsSharedAndEnforced(t *testing.T) {
	for _, tc := range []struct{ from, transition, to string }{
		{StateDraft, "submit", StateValidating},
		{StateAwaitingApproval, "approve", StateApproved},
		{StateAwaitingApproval, "request_changes", StateChangesRequested},
		{StateApproved, "apply", StateApplying},
		{StateVerifying, "complete", StateCompleted},
	} {
		if got, err := Next(tc.from, tc.transition); err != nil || got != tc.to {
			t.Errorf("%s --%s--> %s, %v; want %s", tc.from, tc.transition, got, err, tc.to)
		}
	}
	for _, tc := range []struct{ from, transition string }{
		{StateDraft, "complete"}, {StateDraft, "approve"}, {StateApplying, "cancel"}, {StateCompleted, "submit"},
	} {
		if _, err := Next(tc.from, tc.transition); !errors.Is(err, ErrTransition) {
			t.Errorf("%s --%s--> allowed", tc.from, tc.transition)
		}
	}
	if !Terminal(StateCompleted) || !Terminal(StateInvalid) || Terminal(StateApproved) {
		t.Fatal("terminal states differ from the contract")
	}
	if k := Kinds()[KindTenantReinstatement]; k.ChangesetType != "REINSTATE" || k.ToStatus != "active" {
		t.Fatalf("reinstatement kind: %+v", k)
	}
}

// TestValidationSeparatesInvalidFromBlocked: a missing tenant can never be
// changed (INVALID); a tenant in the wrong state or locked by another open
// changeset may be later (BLOCKED).
func TestValidationSeparatesInvalidFromBlocked(t *testing.T) {
	c := suspension(t)
	if v := Validate(c, Target{}); len(v.Invalid) != 1 || v.Invalid[0].Code != BlockTargetNotFound || len(v.Blockers) != 0 {
		t.Fatalf("missing tenant: %+v", v)
	}
	if v := Validate(c, Target{Found: true, Status: "suspended", Revision: 4}); len(v.Invalid) != 0 || v.Blockers[0].Code != BlockTargetStateConflict {
		t.Fatalf("already suspended: %+v", v)
	}
	v := Validate(c, Target{Found: true, Status: "active", Revision: 4, LockedBy: []string{"cs_other1", c.ChangesetID}})
	if len(v.Blockers) != 1 || v.Blockers[0].Code != BlockTargetLocked {
		t.Fatalf("locked: %+v", v)
	}
	if v := Validate(c, Target{Found: true, Status: "active", Revision: 4}); len(v.Invalid)+len(v.Blockers) != 0 {
		t.Fatalf("a ready tenant: %+v", v)
	}
}

// TestPlanConformsAndDetectsStaleness: the plan runs the kind's operations
// in order, conforms to the contract, is deterministic, and goes stale when
// the tenant changes underneath it.
func TestPlanConformsAndDetectsStaleness(t *testing.T) {
	c := suspension(t)
	target := Target{Found: true, Status: "active", Revision: 4}
	p, err := Generate(PlanInput{Changeset: c, Target: target, PlanID: "plan_0199a1b2c3d4", PlanVersion: 1, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.ValidateValue(planSchema, p); err != nil {
		t.Fatalf("the plan does not conform: %v", err)
	}
	var ops []string
	for _, s := range p.Steps {
		ops = append(ops, s.Operation)
	}
	if !slices.Equal(ops, []string{OpSuspendTenant, OpVerifyTenantState}) || !slices.Equal(p.Steps[1].DependsOn, []string{"suspend-tenant"}) ||
		p.RiskClass != "HIGH" || len(p.Blockers) != 0 || p.Steps[0].Resources.FromStatus != "active" {
		t.Fatalf("plan: %+v", p)
	}
	again, _ := Generate(PlanInput{Changeset: c, Target: target, PlanID: "plan_other00001", PlanVersion: 2, Now: now.Add(time.Hour)})
	if Material(again) != Material(p) || Stale(p, again, now.Add(time.Hour)) {
		t.Fatal("replanning unchanged state changed the material")
	}
	moved, _ := Generate(PlanInput{Changeset: c, Target: Target{Found: true, Status: "active", Revision: 5}, PlanID: "plan_other00001", PlanVersion: 2, Now: now})
	if !Stale(p, moved, now) {
		t.Fatal("a tenant that changed underneath the plan did not make it stale")
	}
	if !Stale(p, again, p.ExpiresAt) {
		t.Fatal("an expired plan is not stale")
	}
}

// TestActivationKinds: market and mapping activation derive a MODIFY
// changeset at PLATFORM scope, name only their own target, and plan the
// kind's two operations with the reviewed revision bound to the change
// step. The kinds' approval scopes come from the Shared lifecycle.
func TestActivationKinds(t *testing.T) {
	for _, tc := range []struct {
		kind, scope string
		desired     DesiredChange
		ops         []string
	}{
		{KindMarketActivation, "market:approve", DesiredChange{Kind: KindMarketActivation, MarketID: "mkt_01k9za7b2b"}, []string{OpActivateMarket, OpVerifyMarketState}},
		{KindMappingActivation, "mapping:approve", DesiredChange{Kind: KindMappingActivation, MappingID: "map_01k9za7b2c"}, []string{OpActivateMapping, OpVerifyMapping}},
	} {
		if got := Kinds()[tc.kind].ApprovalScope; got != tc.scope {
			t.Fatalf("%s approval scope: %q", tc.kind, got)
		}
		c, err := Draft(CreateRequest{Title: "Activate", Reason: "Reviewed.", DesiredChange: tc.desired}, "cs_0199a1b2c3d47e90", "prn_requester1", "API", "", 3, now)
		if err != nil {
			t.Fatal(err)
		}
		if c.ChangesetType != "MODIFY" || c.TargetScope.Level != "PLATFORM" || c.TargetScope.TenantID != "" {
			t.Fatalf("%s draft: %+v", tc.kind, c)
		}
		if err := contracts.ValidateValue(recordSchema, c); err != nil {
			t.Fatalf("%s draft does not conform: %v", tc.kind, err)
		}
		crossed := tc.desired
		crossed.TenantID = tenant
		if _, err := Draft(CreateRequest{Title: "x", Reason: "y", DesiredChange: crossed}, "cs_x1", "prn_r", "API", "", 1, now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s naming a tenant too: %v", tc.kind, err)
		}
		p, err := Generate(PlanInput{Changeset: c, Target: Target{Found: true, Status: "VALIDATED", Revision: 3}, PlanID: "plan_0199a1b2c3d47ea1", PlanVersion: 1, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Blockers) != 0 || len(p.Steps) != 2 || p.Steps[0].Operation != tc.ops[0] || p.Steps[1].Operation != tc.ops[1] ||
			p.Steps[0].Resources.TargetRevision != 3 || p.Steps[0].Resources.FromStatus != "VALIDATED" || p.Steps[1].Resources.TargetRevision != 0 {
			t.Fatalf("%s plan: %+v", tc.kind, p)
		}
		if err := contracts.ValidateValue(planSchema, p); err != nil {
			t.Fatalf("%s plan does not conform: %v", tc.kind, err)
		}
		moved, _ := Generate(PlanInput{Changeset: c, Target: Target{Found: true, Status: "VALIDATED", Revision: 4}, PlanID: p.PlanID, PlanVersion: 1, Now: now})
		if !Stale(p, moved, now) {
			t.Fatalf("%s: a moved revision does not make the plan stale", tc.kind)
		}
		if active := Validate(c, Target{Found: true, Status: "ACTIVE", Revision: 4}); len(active.Blockers) != 1 || active.Blockers[0].Code != BlockTargetStateConflict {
			t.Fatalf("%s from ACTIVE: %+v", tc.kind, active)
		}
	}
}

// TestProviderActivation (EA-02D): a provider activation derives a MODIFY
// changeset at PLATFORM scope naming only a canonical provider id, plans
// ACTIVATE_PROVIDER then VERIFY_PROVIDER_STATE from DRAFT, reports every
// plan check as a readiness requirement, and turns each failed check into
// a blocker with its registered code, in the lifecycle's order.
func TestProviderActivation(t *testing.T) {
	const provider = "provider_0199a1b2c3d47e8f9a0b1c2d3e4f5a6b"
	kind := Kinds()[KindProviderActivation]
	if kind.Target != TargetProvider || kind.ApprovalScope != "provider:approve" || !slices.Equal(kind.FromStatus, []string{"DRAFT"}) ||
		len(kind.PlanChecks) != 9 {
		t.Fatalf("pinned PROVIDER_ACTIVATION: %+v", kind)
	}
	desired := DesiredChange{Kind: KindProviderActivation, ProviderID: provider}
	c, err := Draft(CreateRequest{Title: "Activate", Reason: "Reviewed.", DesiredChange: desired}, "cs_0199a1b2c3d47e92", "prn_requester1", "API", "", 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.ChangesetType != "MODIFY" || c.TargetScope.Level != "PLATFORM" || c.DesiredChange.TargetID() != provider {
		t.Fatalf("draft: %+v", c)
	}
	if err := contracts.ValidateValue(recordSchema, c); err != nil {
		t.Fatalf("draft does not conform: %v", err)
	}
	for name, bad := range map[string]DesiredChange{
		"a UUID provider id":    {Kind: KindProviderActivation, ProviderID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b"},
		"a provider key":        {Kind: KindProviderActivation, ProviderID: "baobab-payments.sandbox"},
		"no provider":           {Kind: KindProviderActivation},
		"a provider and tenant": {Kind: KindProviderActivation, ProviderID: provider, TenantID: tenant},
	} {
		if _, err := Draft(CreateRequest{Title: "x", Reason: "y", DesiredChange: bad}, "cs_x1", "prn_r", "API", "", 1, now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	ready := Target{Found: true, Status: "DRAFT", Revision: 2}
	p, err := Generate(PlanInput{Changeset: c, Target: ready, PlanID: "plan_0199a1b2c3d47ea4", PlanVersion: 1, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.ValidateValue(planSchema, p); err != nil {
		t.Fatalf("plan does not conform: %v", err)
	}
	if len(p.Blockers) != 0 || len(p.Steps) != 2 || p.Steps[0].Operation != OpActivateProvider || p.Steps[1].Operation != OpVerifyProvider ||
		p.Steps[0].Resources.ProviderID != provider || p.Steps[0].Resources.FromStatus != "DRAFT" || p.Steps[0].Resources.TargetRevision != 2 ||
		p.RiskClass != "HIGH" {
		t.Fatalf("plan: %+v", p)
	}
	var checks []string
	for _, r := range p.ReadinessRequirements {
		checks = append(checks, r.Check)
	}
	var want []string
	for _, pc := range kind.PlanChecks {
		want = append(want, pc.Check)
	}
	if !slices.Equal(checks, want) {
		t.Fatalf("readiness requirements %v, want every plan check %v", checks, want)
	}

	failing := ready
	failing.CheckFailures = map[string]string{"HEALTH": "No instance is healthy.", "ENGINE_RELEASE": "No release is recorded."}
	blocked, err := Generate(PlanInput{Changeset: c, Target: failing, PlanID: "plan_0199a1b2c3d47ea4", PlanVersion: 1, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, b := range blocked.Blockers {
		codes = append(codes, b.Code)
	}
	if !slices.Equal(codes, []string{"PROVIDER_NO_ELIGIBLE_RELEASE", "PROVIDER_HEALTH_NOT_ELIGIBLE"}) {
		t.Fatalf("blockers %v", codes)
	}
	if err := contracts.ValidateValue(planSchema, blocked); err != nil {
		t.Fatalf("blocked plan does not conform: %v", err)
	}
	if !Stale(p, blocked, now) {
		t.Fatal("a check that started failing does not make the plan stale")
	}
	if active := Validate(c, Target{Found: true, Status: "ACTIVE", Revision: 3}); len(active.Blockers) != 1 || active.Blockers[0].Code != BlockTargetStateConflict {
		t.Fatalf("from ACTIVE: %+v", active)
	}
}
