package repository

import (
	"errors"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

// TestRollbackSteps: roll_back releases a cohort frozen mid-step first,
// returns shifted cohorts most recent first through their strategy (engine
// steps only for STATEFUL_CUTOVER), always ends by removing the target
// bindings, and FORWARD_FIX_ONLY refuses once a cohort shifted.
func TestRollbackSteps(t *testing.T) {
	ops := func(steps []migration.Step) []string {
		out := []string{}
		for _, s := range steps {
			out = append(out, s.Resources.CohortKey+":"+s.Operation)
		}
		return out
	}
	stateful := migration.Plan{MigrationMode: migration.ModeStatefulCutover, Request: migration.Request{RollbackStrategy: "REBIND_SOURCE"}}
	steps, err := rollbackSteps(migration.Migration{ShiftedCohortKeys: []string{"a", "b"}}, stateful, "c")
	want := []string{"c:UNFREEZE_COHORT_WRITES", "b:FREEZE_COHORT_WRITES", "b:SHIFT_COHORT", "b:UNFREEZE_COHORT_WRITES",
		"a:FREEZE_COHORT_WRITES", "a:SHIFT_COHORT", "a:UNFREEZE_COHORT_WRITES", ":REMOVE_MIGRATION_BINDING"}
	if got := ops(steps); err != nil || len(got) != len(want) {
		t.Fatalf("stateful rollback: %v %v", got, err)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("stateful rollback: %v, want %v", got, want)
			}
		}
	}
	stateless := migration.Plan{MigrationMode: migration.ModeStatelessRebind, Request: migration.Request{RollbackStrategy: "RESTORE_AND_REBIND_SOURCE"}}
	if got := ops(must(rollbackSteps(migration.Migration{ShiftedCohortKeys: []string{"a"}}, stateless, ""))); len(got) != 2 ||
		got[0] != "a:SHIFT_COHORT" || got[1] != ":REMOVE_MIGRATION_BINDING" {
		t.Fatalf("stateless rollback: %v", got)
	}
	forward := migration.Plan{MigrationMode: migration.ModeStatefulCutover, Request: migration.Request{RollbackStrategy: migration.RollbackForwardFixOnly}}
	if _, err := rollbackSteps(migration.Migration{ShiftedCohortKeys: []string{"a"}}, forward, ""); !errors.Is(err, ErrProviderMigrationNotReversible) {
		t.Fatalf("forward-fix-only after a shift: %v", err)
	}
	if got := ops(must(rollbackSteps(migration.Migration{}, forward, "a"))); len(got) != 2 || got[0] != "a:UNFREEZE_COHORT_WRITES" {
		t.Fatalf("forward-fix-only releasing a frozen cohort: %v", got)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
