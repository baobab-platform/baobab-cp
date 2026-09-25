package reconcile

import (
	"fmt"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// ReconcileTenantState resolves the desired lifecycle state against the current observed state.
// It returns the next state, whether a change was required, and an error if the transition is invalid.
func ReconcileTenantState(desired, observed domain.LifecycleStatus) (domain.LifecycleStatus, bool, error) {
	if desired == "" {
		return observed, false, nil
	}
	if !desired.Valid() {
		return "", false, fmt.Errorf("desired lifecycle status %q is invalid", desired)
	}
	if observed == "" {
		return desired, true, nil
	}
	if !observed.Valid() {
		return "", false, fmt.Errorf("observed lifecycle status %q is invalid", observed)
	}
	if desired == observed {
		return observed, false, nil
	}
	if next, ok := domain.TransitionLifecycle(observed, desired); ok {
		return next, true, nil
	}
	return "", false, fmt.Errorf("cannot reconcile %q to %q", observed, desired)
}
