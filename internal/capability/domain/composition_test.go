package domain

import "testing"

func validComposition() CapabilityComposition {
	return CapabilityComposition{
		CompositionKey:  "solution.baobab-xbt",
		CompositionType: CompositionTypeProduct,
		Version:         "1.0.0",
		Members: []CompositionMember{
			{CapabilityKey: "commerce.order.create", Criticality: MembershipCriticalityMandatory},
		},
		Lifecycle: CapabilityLifecycleActive,
	}
}

func TestCapabilityCompositionValidateAccepts(t *testing.T) {
	if err := validComposition().Validate(); err != nil {
		t.Fatalf("expected valid composition, got: %v", err)
	}
}

func TestCapabilityCompositionValidateRejectsBadKey(t *testing.T) {
	c := validComposition()
	c.CompositionKey = "not-a-composition-key"
	if err := c.Validate(); err == nil {
		t.Fatal("expected rejection of a non <type>.<name> composition_key")
	}
}

func TestCapabilityCompositionValidateRejectsInvalidType(t *testing.T) {
	c := validComposition()
	c.CompositionType = "BOGUS"
	if err := c.Validate(); err == nil {
		t.Fatal("expected rejection of an invalid composition_type")
	}
}

func TestCapabilityCompositionValidateRequiresAtLeastOneMember(t *testing.T) {
	c := validComposition()
	c.Members = nil
	if err := c.Validate(); err == nil {
		t.Fatal("expected rejection of a composition with no members")
	}
}

func TestCapabilityCompositionValidateRejectsInvalidMember(t *testing.T) {
	c := validComposition()
	c.Members = []CompositionMember{{CapabilityKey: "bad key", Criticality: MembershipCriticalityMandatory}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected rejection of a member with an invalid capability_key")
	}
}

func TestCapabilityCompositionValidateRejectsInvalidLifecycle(t *testing.T) {
	c := validComposition()
	c.Lifecycle = "BOGUS"
	if err := c.Validate(); err == nil {
		t.Fatal("expected rejection of an invalid lifecycle")
	}
}

func TestCapabilityCompositionIsResolvable(t *testing.T) {
	active := validComposition()
	if !active.IsResolvable() {
		t.Fatal("expected an ACTIVE composition to be resolvable")
	}
	draft := validComposition()
	draft.Lifecycle = CapabilityLifecycleDraft
	if draft.IsResolvable() {
		t.Fatal("expected a DRAFT composition to not be resolvable")
	}
}

func TestValidCompositionKey(t *testing.T) {
	cases := map[string]bool{
		"platform.core":               true,
		"solution.baobab-xbt":         true,
		"addon.advanced-intelligence": true,
		"platform":                    false,
		"Platform.Core":               false,
		"":                            false,
	}
	for key, want := range cases {
		if got := ValidCompositionKey(key); got != want {
			t.Errorf("ValidCompositionKey(%q) = %v, want %v", key, got, want)
		}
	}
}
