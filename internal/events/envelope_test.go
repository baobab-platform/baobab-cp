package events

import (
	"testing"
	"time"
)

func validParams() Params {
	return Params{
		Type:          "com.baobab-platform.control-plane.tenant.provisioning-started.v1",
		Source:        "urn:baobab-platform:service:baobab-cp",
		Subject:       "tn_01k4example",
		DataSchema:    "https://contracts.baobab-platform.com/control-plane/v1/provisioning-started.schema.json",
		CorrelationID: "9f8b6e2a-0000-4000-8000-000000000001",
		TenantID:      "tn_01k4example",
		Data:          map[string]any{"tenant_id": "tn_01k4example"},
	}
}

func TestNewProducesTenantScopedEnvelope(t *testing.T) {
	env, err := New(validParams())
	if err != nil {
		t.Fatalf("valid params rejected: %v", err)
	}
	if env.SpecVersion != "1.0" {
		t.Fatalf("expected specversion 1.0, got %q", env.SpecVersion)
	}
	if env.BaobabScope != ScopeTenant {
		t.Fatalf("expected tenant scope when TenantID is set, got %q", env.BaobabScope)
	}
	if !uuidPattern.MatchString(env.ID) {
		t.Fatalf("expected a minted uuid id, got %q", env.ID)
	}
	if env.DataContentType != "application/json" {
		t.Fatalf("expected datacontenttype application/json, got %q", env.DataContentType)
	}
}

func TestNewProducesPlatformScopedEnvelopeWithoutTenantID(t *testing.T) {
	p := validParams()
	p.TenantID = ""
	env, err := New(p)
	if err != nil {
		t.Fatalf("valid platform-scoped params rejected: %v", err)
	}
	if env.BaobabScope != ScopePlatform {
		t.Fatalf("expected platform scope when TenantID is empty, got %q", env.BaobabScope)
	}
	if env.TenantID != "" {
		t.Fatalf("expected tenantid to be absent for a platform event, got %q", env.TenantID)
	}
}

func TestNewRejectsInvalidType(t *testing.T) {
	p := validParams()
	p.Type = "tenant.provisioning.started" // legacy shape ADR-0004 retired
	if _, err := New(p); err == nil {
		t.Fatal("expected an invalid event type to be rejected")
	}
}

func TestNewRejectsInvalidTenantID(t *testing.T) {
	p := validParams()
	p.TenantID = "zuribeans_za" // legacy alias shape, not a minted tenant_id
	if _, err := New(p); err == nil {
		t.Fatal("expected an invalid tenantid to be rejected")
	}
}

func TestNewRejectsMissingCorrelationID(t *testing.T) {
	p := validParams()
	p.CorrelationID = ""
	if _, err := New(p); err == nil {
		t.Fatal("expected a missing correlationid to be rejected")
	}
}

func TestNewIsDeterministicGivenFixedIDAndClock(t *testing.T) {
	fixedTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	p := validParams()
	p.idGenerator = func() string { return "9f8b6e2a-0000-4000-8000-0000000000ff" }
	p.now = func() time.Time { return fixedTime }
	env, err := New(p)
	if err != nil {
		t.Fatalf("valid params rejected: %v", err)
	}
	if env.ID != "9f8b6e2a-0000-4000-8000-0000000000ff" {
		t.Fatalf("expected the injected id generator to be used, got %q", env.ID)
	}
	if !env.Time.Equal(fixedTime) {
		t.Fatalf("expected the injected clock to be used, got %v", env.Time)
	}
}

func TestLegacyNamespaceIsRejected(t *testing.T) {
	_, err := New(Params{
		Type:          "com.nabhold.control-plane.tenant-provisioning-started.v1",
		Source:        "urn:baobab-platform:service:baobab-cp",
		Subject:       "tn_01k4example",
		DataSchema:    "https://contracts.baobab-platform.com/control-plane/v1/provisioning-started.schema.json",
		CorrelationID: "9f8b6e2a-0000-4000-8000-000000000001",
		Data:          map[string]any{"tenant_id": "tn_01k4example"},
	})
	if err == nil {
		t.Fatal("the legacy com.nabhold event namespace must be rejected; every event is com.baobab-platform.* (ADR-SHARED-008)")
	}
}
