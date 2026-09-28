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
