package domain

import (
	"testing"
	"time"
)

func TestContextRequiresTrustedAuthoritativeIdentity(t *testing.T) {
	now := time.Now().UTC()
	valid := validContextFixture(now)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Context rejected: %v", err)
	}

	spoofed := valid
	spoofed.Provenance = map[string]ContextSource{
		"tenant_id": {Source: "request_header", TrustLevel: TrustUntrusted},
	}
	if err := spoofed.Validate(); err == nil {
		t.Fatal("untrusted tenant provenance must fail closed")
	}
}

func validContextFixture(now time.Time) Context {
	return Context{
		PrincipalID:   "principal:workload:zuribeans",
		TenantID:      "tn_zuribeans",
		CorrelationID: "01K4N5M6P7Q8R9S0T1V2W3X4Y5",
		ResolvedAt:    now,
		Provenance: map[string]ContextSource{
			"tenant_id": {Source: "verified_token", TrustLevel: TrustVerified},
		},
	}
}

func TestContextRejectsExpiresAtNotAfterResolvedAt(t *testing.T) {
	now := time.Now().UTC()
	ctx := validContextFixture(now)
	before := now.Add(-time.Minute)
	ctx.ExpiresAt = &before
	if err := ctx.Validate(); err == nil {
		t.Fatal("expires_at at or before resolved_at accepted")
	}

	after := now.Add(time.Minute)
	ctx.ExpiresAt = &after
	if err := ctx.Validate(); err != nil {
		t.Fatalf("valid expires_at rejected: %v", err)
	}
}

func TestContextIsExpired(t *testing.T) {
	now := time.Now().UTC()
	unbounded := validContextFixture(now)
	if unbounded.IsExpired(now.Add(24 * time.Hour)) {
		t.Fatal("a Context with no ExpiresAt must never report itself expired")
	}

	future := now.Add(time.Hour)
	bounded := unbounded
	bounded.ExpiresAt = &future
	if bounded.IsExpired(now) {
		t.Fatal("a Context before its ExpiresAt reported itself expired")
	}
	if !bounded.IsExpired(future) {
		t.Fatal("a Context at its ExpiresAt did not report itself expired")
	}
	if !bounded.IsExpired(future.Add(time.Second)) {
		t.Fatal("a Context past its ExpiresAt did not report itself expired")
	}
}

func TestNativeIdentityFollowsTheSharedGrammar(t *testing.T) {
	identity := NativeIdentity{SystemNamespace: "idempiere", EngineID: "baobab-erp", NativeEntityType: "c_bpartner", NativeID: "10043"}
	if err := identity.Validate(); err != nil {
		t.Fatalf("valid native identity rejected: %v", err)
	}
	for name, bad := range map[string]NativeIdentity{
		"no native id":         {SystemNamespace: "idempiere", EngineID: "baobab-erp", NativeEntityType: "c_bpartner"},
		"snake_case engine":    {SystemNamespace: "idempiere", EngineID: "baobab_erp", NativeEntityType: "c_bpartner", NativeID: "1"},
		"upper-case type":      {SystemNamespace: "idempiere", EngineID: "baobab-erp", NativeEntityType: "C_BPartner", NativeID: "1"},
		"uuid engine instance": {SystemNamespace: "idempiere", EngineID: "baobab-erp", EngineInstanceID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b", NativeEntityType: "c_bpartner", NativeID: "1"},
		"unknown environment":  {SystemNamespace: "idempiere", EngineID: "baobab-erp", Environment: "prod", NativeEntityType: "c_bpartner", NativeID: "1"},
		"hyphenated namespace": {SystemNamespace: "i-dempiere", EngineID: "baobab-erp", NativeEntityType: "c_bpartner", NativeID: "1"},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
