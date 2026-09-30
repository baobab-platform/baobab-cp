package domain

import "testing"

func TestPrincipalValidateRejectsUnknownActorType(t *testing.T) {
	p := Principal{ActorType: "admin", Status: "ACTIVE"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected unknown actor_type to be rejected")
	}
	for _, actorType := range []string{"human", "workload", "external"} {
		if err := (Principal{ActorType: actorType, Status: "ACTIVE"}).Validate(); err != nil {
			t.Fatalf("valid actor_type %q rejected: %v", actorType, err)
		}
	}
}

func TestPrincipalValidateRejectsUnknownStatus(t *testing.T) {
	p := Principal{ActorType: "human", Status: "active"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected lowercase status to be rejected (this codebase's lifecycle vocabulary is uppercase)")
	}
	for _, status := range []string{"ACTIVE", "SUSPENDED", "DISABLED", "ARCHIVED"} {
		if err := (Principal{ActorType: "human", Status: status}).Validate(); err != nil {
			t.Fatalf("valid status %q rejected: %v", status, err)
		}
	}
}

func TestNewPrincipalIDIsUniqueAndUUIDShaped(t *testing.T) {
	a, b := NewPrincipalID(), NewPrincipalID()
	if a == b {
		t.Fatal("expected distinct principal IDs")
	}
	if len(a) != 36 {
		t.Fatalf("expected a UUID-shaped (36-char) principal ID, got %q", a)
	}
}

func TestExternalIdentityValidateIsProviderNeutral(t *testing.T) {
	for _, providerType := range []string{"keycloak", "ory", "google", ""} {
		e := ExternalIdentity{
			PrincipalID:  "p-1",
			Issuer:       "https://identity.example",
			Subject:      "sub-1",
			ProviderType: providerType,
			Status:       "ACTIVE",
		}
		if err := e.Validate(); err != nil {
			t.Fatalf("provider_type %q should not change canonical identity validity: %v", providerType, err)
		}
	}
}

func TestExternalIdentityValidateRequiresPrincipalIssuerAndSubject(t *testing.T) {
	cases := []struct {
		name string
		e    ExternalIdentity
	}{
		{"missing principal_id", ExternalIdentity{Issuer: "https://iam.nabhold.com", Subject: "sub-1", Status: "ACTIVE"}},
		{"missing issuer", ExternalIdentity{PrincipalID: "p-1", Subject: "sub-1", Status: "ACTIVE"}},
		{"missing subject", ExternalIdentity{PrincipalID: "p-1", Issuer: "https://iam.nabhold.com", Status: "ACTIVE"}},
		{"invalid status", ExternalIdentity{PrincipalID: "p-1", Issuer: "https://iam.nabhold.com", Subject: "sub-1", Status: "PENDING"}},
	}
	for _, c := range cases {
		if err := c.e.Validate(); err == nil {
			t.Fatalf("%s: expected rejection", c.name)
		}
	}

	valid := ExternalIdentity{PrincipalID: "p-1", Issuer: "https://iam.nabhold.com", Subject: "sub-1", Status: "ACTIVE"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid external identity rejected: %v", err)
	}
}

func TestIdentityReferenceValidateRequiresPrincipalEngineAndExternalActor(t *testing.T) {
	cases := []struct {
		name string
		r    IdentityReference
	}{
		{"missing principal_id", IdentityReference{Engine: "baobab-trade", ExternalType: "customer", ExternalID: "C-100", Status: "ACTIVE"}},
		{"unknown engine", IdentityReference{PrincipalID: "p-1", Engine: "not-a-real-engine", ExternalType: "customer", ExternalID: "C-100", Status: "ACTIVE"}},
		{"missing external_type", IdentityReference{PrincipalID: "p-1", Engine: "baobab-trade", ExternalID: "C-100", Status: "ACTIVE"}},
		{"missing external_id", IdentityReference{PrincipalID: "p-1", Engine: "baobab-trade", ExternalType: "customer", Status: "ACTIVE"}},
		{"invalid status", IdentityReference{PrincipalID: "p-1", Engine: "baobab-trade", ExternalType: "customer", ExternalID: "C-100", Status: "REVOKED"}},
	}
	for _, c := range cases {
		if err := c.r.Validate(); err == nil {
			t.Fatalf("%s: expected rejection", c.name)
		}
	}

	for _, engine := range []string{"baobab-trade", "baobab-erp", "baobab-cms", "baobab-pulse"} {
		valid := IdentityReference{PrincipalID: "p-1", Engine: engine, ExternalType: "customer", ExternalID: "C-100", Status: "ACTIVE"}
		if err := valid.Validate(); err != nil {
			t.Fatalf("valid identity reference for engine %q rejected: %v", engine, err)
		}
	}
	for _, status := range []string{"ACTIVE", "INACTIVE", "HISTORICAL"} {
		valid := IdentityReference{PrincipalID: "p-1", Engine: "baobab-trade", ExternalType: "customer", ExternalID: "C-100", Status: status}
		if err := valid.Validate(); err != nil {
			t.Fatalf("valid identity reference for status %q rejected: %v", status, err)
		}
	}
}

func TestNewIdentityReferenceIDIsUniqueAndUUIDShaped(t *testing.T) {
	a, b := NewIdentityReferenceID(), NewIdentityReferenceID()
	if a == b {
		t.Fatal("expected distinct identity reference IDs")
	}
	if len(a) != 36 {
		t.Fatalf("expected a UUID-shaped (36-char) identity reference ID, got %q", a)
	}
}
