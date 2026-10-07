package domain

import (
	"strings"
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

func provisioningContextFixture() Context {
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	expires := now.Add(10 * time.Minute)
	return Context{ID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a70", PrincipalID: "prn_provisioner", TenantID: "tn_01k4zuribeans", CorrelationID: "tp_1",
		ResolvedAt: now, ExpiresAt: &expires,
		Provenance:       map[string]ContextSource{"tenant_id": {Source: "tenant_provisioning", TrustLevel: TrustSystem}},
		AuthorityPurpose: ContextPurposeTenantProvisioning,
		ProvisioningAuthority: &ProvisioningAuthority{TenantProvisioningID: "tp_0199a1b2c3d4", PlanID: "plan_0199a1b2c3d4", PlanVersion: 2,
			PlanDigest: "sha256:" + strings.Repeat("a", 64)}}
}

// A context with no recorded purpose is RUNTIME, and only a provisioning context carries a plan tuple.
func TestContextAuthorityPurposeRules(t *testing.T) {
	runtime := provisioningContextFixture()
	runtime.AuthorityPurpose, runtime.ProvisioningAuthority = "", nil
	if runtime.Purpose() != ContextPurposeRuntime || !runtime.IsRuntime() || runtime.Validate() != nil {
		t.Fatalf("the zero purpose is RUNTIME: %v", runtime.Validate())
	}
	if c := provisioningContextFixture(); c.Validate() != nil || c.IsRuntime() {
		t.Fatalf("a bound, bounded provisioning context is valid and not runtime: %v", c.Validate())
	}
	expires := func(c *Context, d time.Duration) { e := c.ResolvedAt.Add(d); c.ExpiresAt = &e }
	for name, mutate := range map[string]func(*Context){
		"runtime carrying a plan tuple": func(c *Context) { c.AuthorityPurpose = ContextPurposeRuntime },
		"an unknown purpose":            func(c *Context) { c.AuthorityPurpose = "PENDING_TENANT" },
		"no tuple":                      func(c *Context) { c.ProvisioningAuthority = nil },
		"no provisioning id":            func(c *Context) { c.ProvisioningAuthority.TenantProvisioningID = "" },
		"a malformed plan id":           func(c *Context) { c.ProvisioningAuthority.PlanID = "x" },
		"plan version 0":                func(c *Context) { c.ProvisioningAuthority.PlanVersion = 0 },
		"a malformed digest":            func(c *Context) { c.ProvisioningAuthority.PlanDigest = "sha256:short" },
		"unbounded":                     func(c *Context) { c.ExpiresAt = nil },
		"16 minutes":                    func(c *Context) { expires(c, 16*time.Minute) },
		"a legal entity":                func(c *Context) { c.LegalEntityID = "ZURIBEANS-ZA" },
		"an organisation":               func(c *Context) { c.OrganisationID = "org_1" },
		"a market":                      func(c *Context) { c.MarketID = "mkt_1" },
		"a country":                     func(c *Context) { c.CountryCode = "ZA" },
		"a digital estate":              func(c *Context) { c.DigitalEstateID = "estate_1" },
	} {
		c := provisioningContextFixture()
		mutate(&c)
		if c.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	at := provisioningContextFixture()
	expires(&at, MaxProvisioningContextLifetime)
	if at.Validate() != nil {
		t.Fatalf("exactly 15 minutes is allowed: %v", at.Validate())
	}
}
