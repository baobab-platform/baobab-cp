package migration

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

const lifecyclePath = "control-plane/v1/provider-migration-lifecycle.yaml"

// Transitions a migration is advanced by (migrationTransition).
const (
	TransitionPrepare   = "prepare"
	TransitionShadow    = "shadow"
	TransitionCanary    = "canary"
	TransitionValidate  = "validate"
	TransitionShift     = "shift"
	TransitionRetireOld = "retire_old"
	TransitionComplete  = "complete"
	TransitionCancel    = "cancel"
	TransitionRollBack  = "roll_back"
)

// Step scopes (stage_steps scope).
const (
	ScopeMigration     = "MIGRATION"
	ScopeFirstCohort   = "FIRST_COHORT"
	ScopeNextCohort    = "NEXT_COHORT"
	ScopeCurrentCohort = "CURRENT_COHORT"
)

// StageSteps is one stage_steps entry: which of the approved plan's
// operations a transition runs.
type StageSteps struct {
	Scope      string   `yaml:"scope"`
	Operations []string `yaml:"operations"`
}

// EngineStep is one engine_steps entry: the roles an engine operation is
// assigned to going forward and in rollback.
type EngineStep struct {
	Forward []string `yaml:"forward"`
	Reverse []string `yaml:"reverse"`
}

// LifecycleDocument is provider-migration-lifecycle.yaml.
type LifecycleDocument struct {
	InitialState   string   `yaml:"initial_state"`
	TerminalStates []string `yaml:"terminal_states"`
	States         map[string]struct {
		Transitions map[string]string `yaml:"transitions"`
	} `yaml:"states"`
	StatefulCohortSequence []string              `yaml:"stateful_cohort_sequence"`
	StageSteps             map[string]StageSteps `yaml:"stage_steps"`
	EngineSteps            map[string]EngineStep `yaml:"engine_steps"`
	RollbackRelease        []string              `yaml:"rollback_release"`
	RollbackSteps          map[string][]string   `yaml:"rollback_steps"`
	Compensation           map[string][]string   `yaml:"compensation"`
	TaskLeaseSeconds       int                   `yaml:"task_lease_seconds"`
	TaskDefaultDeadline    int                   `yaml:"task_default_deadline_hours"`
}

var (
	lifecycleOnce sync.Once
	lifecycle     *LifecycleDocument
	lifecycleErr  error
)

// Lifecycle is the pinned Shared provider-migration-lifecycle.yaml. It is
// embedded, so a document that does not load is a build defect.
func Lifecycle() *LifecycleDocument {
	lifecycleOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(lifecyclePath)
		if err != nil {
			lifecycleErr = err
			return
		}
		var doc LifecycleDocument
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			lifecycleErr = fmt.Errorf("parse %s: %w", lifecyclePath, err)
			return
		}
		if doc.InitialState != StagePlan || len(doc.StageSteps) == 0 || len(doc.EngineSteps) == 0 {
			lifecycleErr = fmt.Errorf("%s does not describe the execution this package implements", lifecyclePath)
			return
		}
		lifecycle = &doc
	})
	if lifecycleErr != nil {
		panic(lifecycleErr)
	}
	return lifecycle
}

// ErrTransition is a transition the lifecycle does not allow from a stage.
var ErrTransition = errors.New("provider migration transition not allowed")

// Next is the stage a transition leads to from stage, or ErrTransition.
func Next(stage, transition string) (string, error) {
	spec, ok := Lifecycle().States[stage]
	if !ok {
		return "", fmt.Errorf("%w: %s has ended", ErrTransition, stage)
	}
	next, ok := spec.Transitions[transition]
	if !ok || transition == "replan" {
		return "", fmt.Errorf("%w: %s from %s", ErrTransition, transition, stage)
	}
	return next, nil
}

// Forward reports whether a transition moves the migration on and so needs
// the plan's approval; cancel and roll_back leave or undo it.
func Forward(transition string) bool {
	return transition != TransitionCancel && transition != TransitionRollBack
}

// MovesAuthority reports whether a transition shifts a cohort's authority,
// and so runs only inside an open cutover window.
func MovesAuthority(transition string) bool {
	return transition == TransitionCanary || transition == TransitionShift
}

// EngineOperation reports whether an operation is performed by engines as
// engine migration tasks (engine_steps) rather than by the Control Plane.
func EngineOperation(op string) bool {
	_, ok := Lifecycle().EngineSteps[op]
	return ok
}

// ErrNotExecutable is a step the Control Plane cannot run (yet).
var ErrNotExecutable = errors.New("provider migration step not executable")

// Selection is what one transition runs: the approved plan's steps in plan
// order and, for a cohort transition, the cohort they move.
type Selection struct {
	CohortKey string
	Steps     []Step
}

// Select picks the approved plan's steps a forward transition runs, by the
// lifecycle's stage_steps: MIGRATION-wide steps, or the first, next or
// current cohort's. It never invents a step the plan does not contain.
func Select(plan Plan, m Migration, transition string) (Selection, error) {
	spec, ok := Lifecycle().StageSteps[transition]
	if !ok {
		return Selection{}, fmt.Errorf("%w: %s runs no plan steps", ErrTransition, transition)
	}
	cohorts := plan.Request.Cohorts
	var cohort string
	switch spec.Scope {
	case ScopeFirstCohort:
		if len(cohorts) == 0 {
			return Selection{}, fmt.Errorf("%w: the plan has no cohort", ErrTransition)
		}
		cohort = cohorts[0].CohortKey
	case ScopeCurrentCohort:
		if m.CurrentCohortKey == "" {
			return Selection{}, fmt.Errorf("%w: no cohort is being moved", ErrTransition)
		}
		cohort = m.CurrentCohortKey
	case ScopeNextCohort:
		i := slices.IndexFunc(cohorts, func(c Cohort) bool { return c.CohortKey == m.CurrentCohortKey })
		if i < 0 || i+1 >= len(cohorts) {
			return Selection{}, fmt.Errorf("%w: no cohort follows %s", ErrTransition, m.CurrentCohortKey)
		}
		cohort = cohorts[i+1].CohortKey
	}
	out := Selection{CohortKey: cohort}
	for _, step := range plan.Steps {
		if !slices.Contains(spec.Operations, step.Operation) {
			continue
		}
		if spec.Scope != ScopeMigration {
			if step.Resources.CohortKey != cohort && step.Operation != OpStopShadow {
				continue
			}
		} else if step.Resources.CohortKey != "" {
			continue
		}
		out.Steps = append(out.Steps, step)
	}
	return out, nil
}

// LastCohort reports whether key is the plan's last cohort.
func LastCohort(plan Plan, key string) bool {
	cohorts := plan.Request.Cohorts
	return len(cohorts) > 0 && cohorts[len(cohorts)-1].CohortKey == key
}
