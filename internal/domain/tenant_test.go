package domain

import "testing"

func TestRegisterTenantValidate(t *testing.T) {
	valid := RegisterTenant{Basis: RegistrationOnboarding, TenantOnboardingRequestID: "tor_0190a1b2c3d4e5f60718293a4b5c6d7e", LegalEntityID: "THAMANI-GLOBAL", TenantID: NewTenantID(), DisplayName: "Zuri Beans", IsolationStrategy: "schema_per_tenant", ResidencyRegion: "af-south-1", RequestedProducts: []string{"baobab-trade"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}
	valid.RequestedProducts = []string{"baobab-trade", "baobab-trade"}
	if err := valid.Validate(); err == nil {
		t.Fatal("duplicate product accepted")
	}
}

func TestRegisterTenantValidateAcceptsLegacyLegalEntityAlias(t *testing.T) {
	// ADR-0003 §2.3: Control Plane v1 accepts the former lowercase alias at
	// this input boundary during the documented compatibility window.
	valid := RegisterTenant{Basis: RegistrationOnboarding, TenantOnboardingRequestID: "tor_0190a1b2c3d4e5f60718293a4b5c6d7e", LegalEntityID: "zuribeans_za", TenantID: NewTenantID(), DisplayName: "Zuri Beans", IsolationStrategy: "schema_per_tenant", ResidencyRegion: "af-south-1"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("legacy legal_entity_id alias rejected: %v", err)
	}
}

func TestRegisterTenantValidateRejectsMissingTenantID(t *testing.T) {
	// tenant_id is minted by the handler, not the caller; Validate still
	// guards against a programming error that skips minting it.
	invalid := RegisterTenant{Basis: RegistrationOnboarding, TenantOnboardingRequestID: "tor_0190a1b2c3d4e5f60718293a4b5c6d7e", LegalEntityID: "THAMANI-GLOBAL", DisplayName: "Zuri Beans", IsolationStrategy: "schema_per_tenant", ResidencyRegion: "af-south-1"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("missing tenant_id accepted")
	}
}

// TestRegisterTenantValidateRequiresABasis: there is no direct registration
// (ADR-BCP-017 sections 22-24). An onboarding registration names its
// request; a bootstrap registration names none and records its reason and
// evidence; a command with neither basis is refused.
func TestRegisterTenantValidateRequiresABasis(t *testing.T) {
	base := RegisterTenant{LegalEntityID: "NABHOLD", TenantID: NewTenantID(), DisplayName: "Nabhold", IsolationStrategy: "schema_per_tenant", ResidencyRegion: "af-south-1"}
	bootstrap := base
	bootstrap.Basis, bootstrap.BootstrapReason, bootstrap.BootstrapEvidenceReference = RegistrationBootstrap, "Pre-admission first-party tenant.", "migration-record/1"
	onboarding := base
	onboarding.Basis, onboarding.TenantOnboardingRequestID = RegistrationOnboarding, "tor_0190a1b2c3d4e5f60718293a4b5c6d7e"
	for name, c := range map[string]RegisterTenant{"onboarding": onboarding, "bootstrap": bootstrap} {
		if err := c.Validate(); err != nil {
			t.Errorf("%s registration rejected: %v", name, err)
		}
	}
	invalid := map[string]func(*RegisterTenant){
		"no basis":                            func(c *RegisterTenant) { c.Basis = "" },
		"onboarding without a request":        func(c *RegisterTenant) { c.TenantOnboardingRequestID = "" },
		"onboarding with a malformed request": func(c *RegisterTenant) { c.TenantOnboardingRequestID = "ACME-1" },
		"onboarding with a bootstrap reason":  func(c *RegisterTenant) { c.BootstrapReason = "Pre-admission first-party tenant." },
	}
	for name, mutate := range invalid {
		c := onboarding
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for name, mutate := range map[string]func(*RegisterTenant){
		"bootstrap naming a request":    func(c *RegisterTenant) { c.TenantOnboardingRequestID = "tor_0190a1b2c3d4e5f60718293a4b5c6d7e" },
		"bootstrap with a short reason": func(c *RegisterTenant) { c.BootstrapReason = "migrating" },
		"bootstrap without evidence":    func(c *RegisterTenant) { c.BootstrapEvidenceReference = " " },
	} {
		c := bootstrap
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestEntitlementQueryValidate(t *testing.T) {
	valid := EntitlementQuery{TenantID: NewTenantID(), ProductID: "baobab-trade"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid entitlement query rejected: %v", err)
	}
	invalid := EntitlementQuery{TenantID: "bad id", ProductID: "baobab-trade"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid tenant id accepted")
	}
}

func TestLifecycleStatusValidate(t *testing.T) {
	for _, status := range []LifecycleStatus{LifecycleProvisioning, LifecycleActive, LifecycleSuspended, LifecycleDecommissioning, LifecycleDecommissioned} {
		if !status.Valid() {
			t.Fatalf("status %q should be valid", status)
		}
	}
	if LifecycleStatus("unknown").Valid() {
		t.Fatal("unknown lifecycle status should be rejected")
	}
}

func TestLifecycleTransition(t *testing.T) {
	if next, ok := TransitionLifecycle(LifecycleProvisioning, LifecycleActive); !ok || next != LifecycleActive {
		t.Fatal("expected provisioning to become active")
	}
	if next, ok := TransitionLifecycle(LifecycleActive, LifecycleSuspended); !ok || next != LifecycleSuspended {
		t.Fatal("expected active to become suspended")
	}
	if _, ok := TransitionLifecycle(LifecycleDecommissioned, LifecycleActive); ok {
		t.Fatal("decommissioned tenant should not be re-activated without explicit reset")
	}
}

func TestLifecycleActionValidate(t *testing.T) {
	valid := LifecycleAction{TenantID: NewTenantID(), Action: "suspend"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid lifecycle action rejected: %v", err)
	}
	if err := (LifecycleAction{TenantID: "bad id", Action: "activate"}).Validate(); err == nil {
		t.Fatal("invalid lifecycle action accepted")
	}
}
