package migration

import (
	"errors"
	"slices"
	"testing"
)

func executionPlan(mode string) Plan {
	cohorts := []Cohort{{CohortKey: "canary", Selector: &Selector{TenantIDs: []string{"tn_a"}}}, {CohortKey: "rest"}}
	plan := Plan{MigrationMode: mode, Request: Request{MigrationMode: mode, Cohorts: cohorts}}
	add := func(id, op, cohort string) {
		plan.Steps = append(plan.Steps, Step{StepID: id, Operation: op, Resources: StepResources{CohortKey: cohort}})
	}
	add("verify-target", OpVerifyTargetReadiness, "")
	add("bind", OpCreateMigrationBinding, "")
	for _, c := range cohorts {
		ops := []string{OpShiftCohort}
		if mode == ModeStatefulCutover {
			ops = statefulSequence
		}
		for _, op := range append(slices.Clone(ops), OpValidateCohort) {
			add(c.CohortKey+"-"+op, op, c.CohortKey)
		}
	}
	add("retire", OpRetireSourceBinding, "")
	return plan
}

func operationsOf(steps []Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Operation)
	}
	return out
}

// TestLifecycleExecution: the Shared lifecycle's execution rules load; each
// transition selects exactly the approved plan's steps its stage_steps
// name, for the right cohort; engine operations are known; and the stage
// graph refuses transitions it does not allow.
func TestLifecycleExecution(t *testing.T) {
	doc := Lifecycle()
	if doc.TaskLeaseSeconds <= 0 || len(doc.RollbackSteps) != 3 || !slices.Equal(doc.Compensation[TransitionCancel], []string{OpStopShadow, OpRemoveMigrationBinding}) {
		t.Fatalf("lifecycle: %+v", doc)
	}
	for _, op := range statefulSequence {
		if EngineOperation(op) != (op != OpShiftCohort) {
			t.Fatalf("%s: engine operation %v", op, EngineOperation(op))
		}
	}
	if next, err := Next(StagePlan, TransitionPrepare); err != nil || next != "PREPARE" {
		t.Fatalf("prepare: %s %v", next, err)
	}
	for _, tc := range []struct{ stage, transition string }{{StagePlan, TransitionCanary}, {"PREPARE", TransitionRetireOld},
		{StageComplete, TransitionRollBack}, {StagePlan, "replan"}, {"CANARY", TransitionShift}} {
		if _, err := Next(tc.stage, tc.transition); !errors.Is(err, ErrTransition) {
			t.Fatalf("%s from %s: %v", tc.transition, tc.stage, err)
		}
	}
	if Forward(TransitionCancel) || Forward(TransitionRollBack) || !Forward(TransitionPrepare) || !MovesAuthority(TransitionShift) || MovesAuthority(TransitionValidate) {
		t.Fatal("transition classification")
	}

	stateless := executionPlan(ModeStatelessRebind)
	prepare, err := Select(stateless, Migration{}, TransitionPrepare)
	if err != nil || !slices.Equal(operationsOf(prepare.Steps), []string{OpVerifyTargetReadiness, OpCreateMigrationBinding}) {
		t.Fatalf("prepare: %+v %v", prepare, err)
	}
	canary, err := Select(stateless, Migration{}, TransitionCanary)
	if err != nil || canary.CohortKey != "canary" || !slices.Equal(operationsOf(canary.Steps), []string{OpShiftCohort}) {
		t.Fatalf("canary: %+v %v", canary, err)
	}
	validate, err := Select(stateless, Migration{CurrentCohortKey: "canary"}, TransitionValidate)
	if err != nil || validate.CohortKey != "canary" || !slices.Equal(operationsOf(validate.Steps), []string{OpValidateCohort}) {
		t.Fatalf("validate: %+v %v", validate, err)
	}
	shift, err := Select(stateless, Migration{CurrentCohortKey: "canary"}, TransitionShift)
	if err != nil || shift.CohortKey != "rest" || shift.Steps[0].StepID != "rest-"+OpShiftCohort {
		t.Fatalf("shift: %+v %v", shift, err)
	}
	if _, err := Select(stateless, Migration{CurrentCohortKey: "rest"}, TransitionShift); !errors.Is(err, ErrTransition) {
		t.Fatalf("shift after the last cohort: %v", err)
	}
	retire, err := Select(stateless, Migration{CurrentCohortKey: "rest"}, TransitionRetireOld)
	if err != nil || !slices.Equal(operationsOf(retire.Steps), []string{OpRetireSourceBinding}) || !LastCohort(stateless, "rest") || LastCohort(stateless, "canary") {
		t.Fatalf("retire: %+v %v", retire, err)
	}
	if complete, err := Select(stateless, Migration{}, TransitionComplete); err != nil || len(complete.Steps) != 0 {
		t.Fatalf("complete: %+v %v", complete, err)
	}

	// A stateful cohort runs the whole single-writer sequence, in order.
	stateful := executionPlan(ModeStatefulCutover)
	canary, err = Select(stateful, Migration{}, TransitionCanary)
	if err != nil || !slices.Equal(operationsOf(canary.Steps), statefulSequence) {
		t.Fatalf("stateful canary: %+v %v", canary, err)
	}
}
