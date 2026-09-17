package domain

import "testing"

func validPendingProjection() EntitlementProjection {
	return EntitlementProjection{
		SubscriptionID: "sub_01k4z9j3n2",
		TenantID:       "tn_01k4m7x9q2v6c8r3d5f1h0j4",
		CapabilityKey:  "commerce.order.create",
		Status:         EntitlementProjectionStatusPending,
	}
}

func TestEntitlementProjectionValidateAccepts(t *testing.T) {
	if err := validPendingProjection().Validate(); err != nil {
		t.Fatalf("expected valid entitlement projection, got: %v", err)
	}
}

func TestEntitlementProjectionValidateRequiresGrantIDWhenMaterialized(t *testing.T) {
	p := validPendingProjection()
	p.Status = EntitlementProjectionStatusMaterialized
	if err := p.Validate(); err == nil {
		t.Fatal("expected rejection of MATERIALIZED without grant_id")
	}
	p.GrantID = "grant-1"
	if err := p.Validate(); err != nil {
		t.Fatalf("expected MATERIALIZED with grant_id to be valid, got: %v", err)
	}
}

func TestEntitlementProjectionValidateRequiresFailureReasonWhenFailed(t *testing.T) {
	p := validPendingProjection()
	p.Status = EntitlementProjectionStatusFailed
	if err := p.Validate(); err == nil {
		t.Fatal("expected rejection of FAILED without failure_reason")
	}
	p.FailureReason = "capability not found"
	if err := p.Validate(); err != nil {
		t.Fatalf("expected FAILED with failure_reason to be valid, got: %v", err)
	}
}
