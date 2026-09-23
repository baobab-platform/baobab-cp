// Target path: baobab-platform/baobab-cp/internal/domain/tenant_mapping_compat_test.go

package domain

import (
	"testing"
	"time"
)

func TestDefaultLegalEntityIDFromMappings_ActiveDefault(t *testing.T) {
	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	mappings := []TenantLegalEntityMapping{
		{
			ID: "m1", TenantID: "tn_x", LegalEntityID: "LE-A",
			IsDefault: true, Status: "ACTIVE", EffectiveFrom: at.Add(-24 * time.Hour),
		},
		{
			ID: "m2", TenantID: "tn_x", LegalEntityID: "LE-B",
			IsDefault: false, Status: "ACTIVE", EffectiveFrom: at.Add(-24 * time.Hour),
		},
	}
	if got := DefaultLegalEntityIDFromMappings(mappings, at); got != "LE-A" {
		t.Fatalf("got %q want LE-A", got)
	}
}

func TestDefaultLegalEntityIDFromMappings_ExpiredDefaultIgnored(t *testing.T) {
	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	end := at.Add(-time.Hour)
	mappings := []TenantLegalEntityMapping{
		{
			ID: "m1", TenantID: "tn_x", LegalEntityID: "LE-OLD",
			IsDefault: true, Status: "ACTIVE",
			EffectiveFrom: at.Add(-48 * time.Hour), EffectiveTo: &end,
		},
	}
	if got := DefaultLegalEntityIDFromMappings(mappings, at); got != "" {
		t.Fatalf("expired default must not project, got %q", got)
	}
}

func TestAssertDefaultLegalEntityProjection_Conflict(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	mappings := []TenantLegalEntityMapping{
		{
			ID: "m1", TenantID: "tn_x", LegalEntityID: "LE-A",
			IsDefault: true, Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour),
		},
	}
	if err := AssertDefaultLegalEntityProjection("LE-B", mappings, at); err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestAssertDefaultLegalEntityProjection_Consistent(t *testing.T) {
	at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	mappings := []TenantLegalEntityMapping{
		{
			ID: "m1", TenantID: "tn_x", LegalEntityID: "LE-A",
			IsDefault: true, Status: "ACTIVE", EffectiveFrom: at.Add(-time.Hour),
		},
	}
	if err := AssertDefaultLegalEntityProjection("LE-A", mappings, at); err != nil {
		t.Fatal(err)
	}
}

func TestNewDefaultTenantLegalEntityMapping(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := NewDefaultTenantLegalEntityMapping("id1", "tn_x", "LE-A", from)
	if !m.IsDefault || m.Status != "ACTIVE" || m.LegalEntityID != "LE-A" {
		t.Fatalf("unexpected mapping: %+v", m)
	}
}
