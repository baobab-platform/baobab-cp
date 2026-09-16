package postgres

import "testing"

func TestContextPolicyDecision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		found              bool
		desired            string
		observed           string
		subscription       string
		wantResult         string
		wantPolicyDecision string
	}{
		// subscription uses product.product_subscription's uppercase
		// subscriptionStatus enum (migration 000033); desired/observed
		// remain tenants.desired_state/observed_state's lowercase values,
		// which that migration does not touch.
		{name: "active and entitled", found: true, desired: "active", observed: "active", subscription: "ACTIVE", wantResult: "allowed", wantPolicyDecision: "context_allowed"},
		{name: "unknown tenant", found: false, wantResult: "denied", wantPolicyDecision: "tenant_unknown"},
		{name: "suspended tenant", found: true, desired: "suspended", observed: "active", subscription: "ACTIVE", wantResult: "denied", wantPolicyDecision: "tenant_not_active"},
		{name: "unreconciled tenant", found: true, desired: "active", observed: "pending", subscription: "ACTIVE", wantResult: "denied", wantPolicyDecision: "tenant_not_active"},
		{name: "missing entitlement", found: true, desired: "active", observed: "active", wantResult: "denied", wantPolicyDecision: "product_not_entitled"},
		{name: "pending entitlement", found: true, desired: "active", observed: "active", subscription: "PENDING", wantResult: "denied", wantPolicyDecision: "product_not_entitled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, policyDecision := contextPolicyDecision(test.found, test.desired, test.observed, test.subscription)
			if result != test.wantResult || policyDecision != test.wantPolicyDecision {
				t.Fatalf("got (%q,%q), want (%q,%q)", result, policyDecision, test.wantResult, test.wantPolicyDecision)
			}
		})
	}
}
