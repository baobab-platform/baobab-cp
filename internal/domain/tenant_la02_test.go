package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// LA-02 protects v1 contract pins while preparing the Organisation-first v2
// model. Validation alone confers no admission or legally binding mandate.
func TestRegisterTenantV2Compatibility(t *testing.T) {
	base := RegisterTenantV2{
		Basis: RegistrationOnboarding,
		TenantOnboardingRequestID: "tor_0190a1b2c3d4e5f60718293a4b5c6d7e",
		OrganisationID: NewUUIDv7(),
		TenantID: NewTenantID(),
		DisplayName: "ZuriBeans",
		IsolationStrategy: "schema_per_tenant",
		ResidencyRegion: "af-south-1",
		RequestedProducts: []string{"baobab-trade"},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("unincorporated v2 business rejected: %v", err)
	}
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "legal_entity_id") || strings.Contains(string(b), "tenant_id") {
		t.Fatalf("optional legal actor and server-minted tenant must not appear in v2 command: %s", b)
	}
	withNabhold := base
	withNabhold.LegalEntityID = "NABHOLD"
	if err := withNabhold.Validate(); err != nil {
		t.Fatalf("v2 canonical DEFAULT legal actor rejected: %v", err)
	}
	for name, change := range map[string]func(*RegisterTenantV2){
		"empty primary": func(c *RegisterTenantV2) { c.OrganisationID = "" },
		"legal alias as primary": func(c *RegisterTenantV2) { c.OrganisationID = "ZURIBEANS" },
		"legacy legal alias": func(c *RegisterTenantV2) { c.LegalEntityID = "zuribeans_za" },
		"missing authorised onboarding": func(c *RegisterTenantV2) { c.TenantOnboardingRequestID = "" },
		"invalid residency": func(c *RegisterTenantV2) { c.ResidencyRegion = "ZA" },
		"duplicate products": func(c *RegisterTenantV2) { c.RequestedProducts = []string{"baobab-trade", "baobab-trade"} },
	} {
		c := base
		change(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s wrongly accepted", name)
		}
	}
}

func TestRegisterTenantV1StillRequiresLegalEntity(t *testing.T) {
	base := RegisterTenant{
		Basis: RegistrationOnboarding,
		TenantOnboardingRequestID: "tor_0190a1b2c3d4e5f60718293a4b5c6d7e",
		TenantID: NewTenantID(), DisplayName: "ZuriBeans",
		IsolationStrategy: "schema_per_tenant", ResidencyRegion: "af-south-1",
	}
	if err := base.Validate(); err == nil {
		t.Fatal("v1 registration silently accepted a missing legal entity")
	}
	base.LegalEntityID = "zuribeans_za"
	if err := base.Validate(); err != nil {
		t.Fatalf("v1 legacy alias compatibility lost: %v", err)
	}
}
