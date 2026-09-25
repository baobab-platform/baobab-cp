package reconcile

import (
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestReconcileTenantState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		desired   domain.LifecycleStatus
		observed  domain.LifecycleStatus
		expected  domain.LifecycleStatus
		changed   bool
		wantError bool
	}{
		{name: "no-op active", desired: domain.LifecycleActive, observed: domain.LifecycleActive, expected: domain.LifecycleActive, changed: false},
		{name: "start provisioning", desired: domain.LifecycleProvisioning, observed: domain.LifecycleStatus(""), expected: domain.LifecycleProvisioning, changed: true},
		{name: "move active to suspended", desired: domain.LifecycleSuspended, observed: domain.LifecycleActive, expected: domain.LifecycleSuspended, changed: true},
		{name: "decommissioning to decommissioned", desired: domain.LifecycleDecommissioned, observed: domain.LifecycleDecommissioning, expected: domain.LifecycleDecommissioned, changed: true},
		{name: "illegal reactivation", desired: domain.LifecycleActive, observed: domain.LifecycleDecommissioned, expected: "", changed: false, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next, changed, err := ReconcileTenantState(tc.desired, tc.observed)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if next != tc.expected {
				t.Fatalf("expected next state %q, got %q", tc.expected, next)
			}
			if changed != tc.changed {
				t.Fatalf("expected changed=%v, got %v", tc.changed, changed)
			}
		})
	}
}
