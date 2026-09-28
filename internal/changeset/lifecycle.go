package changeset

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

const lifecyclePath = "control-plane/v1/changeset-lifecycle.yaml"

// Kind is one change kind of changeset-lifecycle.yaml: the changeset type
// it derives, the tenant statuses it starts from and produces, and its
// canonical operations in order.
type Kind struct {
	ChangesetType string   `yaml:"changeset_type"`
	FromStatus    []string `yaml:"from_status"`
	ToStatus      string   `yaml:"to_status"`
	Operations    []string `yaml:"operations"`
}

type lifecycleDocument struct {
	InitialState   string   `yaml:"initial_state"`
	TerminalStates []string `yaml:"terminal_states"`
	States         map[string]struct {
		Transitions map[string]string `yaml:"transitions"`
	} `yaml:"states"`
	ChangeKinds   map[string]Kind `yaml:"change_kinds"`
	BlockingCodes []string        `yaml:"blocking_codes"`
}

var (
	lifecycleOnce sync.Once
	lifecycle     *lifecycleDocument
	lifecycleErr  error
)

func load() (*lifecycleDocument, error) {
	lifecycleOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(lifecyclePath)
		if err != nil {
			lifecycleErr = err
			return
		}
		var doc lifecycleDocument
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			lifecycleErr = fmt.Errorf("parse %s: %w", lifecyclePath, err)
			return
		}
		if doc.InitialState != StateDraft || len(doc.States) == 0 || len(doc.ChangeKinds) == 0 {
			lifecycleErr = fmt.Errorf("%s does not describe the changeset lifecycle this package implements", lifecyclePath)
			return
		}
		lifecycle = &doc
	})
	return lifecycle, lifecycleErr
}

func mustLoad() *lifecycleDocument {
	doc, err := load()
	if err != nil {
		// The lifecycle is embedded; one that does not load is a build
		// defect, found by any test of this package.
		panic(err)
	}
	return doc
}

// Kinds are the change kinds the pinned contract supports.
func Kinds() map[string]Kind { return mustLoad().ChangeKinds }

// Terminal reports whether a state ends the changeset.
func Terminal(state string) bool { return slices.Contains(mustLoad().TerminalStates, state) }

// ErrTransition is a transition the lifecycle does not allow from a state.
var ErrTransition = errors.New("changeset transition not allowed")

// Next is the state a transition leads to from state, or ErrTransition.
// The backend enforces legal transitions (section 211): DRAFT never jumps
// to COMPLETED.
func Next(state, transition string) (string, error) {
	spec, ok := mustLoad().States[state]
	if !ok {
		return "", fmt.Errorf("%w: %s is terminal", ErrTransition, state)
	}
	next, ok := spec.Transitions[transition]
	if !ok {
		return "", fmt.Errorf("%w: %s from %s", ErrTransition, transition, state)
	}
	return next, nil
}

// Allows reports whether transition is legal from state.
func Allows(state, transition string) bool {
	_, err := Next(state, transition)
	return err == nil
}
