package resolver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestMappingResolverSelectsMostSpecificCandidate(t *testing.T) {
	resolver := MappingResolverImpl{}
	q := MappingResolutionQuery{
		CanonicalEntityID: "tenant-123",
		Context: Context{
			TenantID:      "tenant-123",
			LegalEntityID: "legal-456",
			MarketID:      "market-789",
			CountryCode:   "ZA",
			CurrencyCode:  "ZAR",
			Locale:        "en-ZA",
		},
		Candidates: []domain.Mapping{
			{
				ID:                      "mapping-market",
				MappingType:             "IDENTITY",
				TenantID:                "tenant-123",
				CanonicalEntityID:       "tenant-123",
				TargetCanonicalEntityID: "entity-market",
				ScopeID:                 "market-789",
				Direction:               "BIDIRECTIONAL",
				Cardinality:             "ONE_TO_ONE",
				Authority:               "baobab",
				Confidence:              "CONFIRMED",
				Status:                  "ACTIVE",
				ResolutionPriority:      20,
				EffectiveFrom:           "2025-01-01T00:00:00Z",
			},
			{
				ID:                      "mapping-tenant",
				MappingType:             "IDENTITY",
				TenantID:                "tenant-123",
				CanonicalEntityID:       "tenant-123",
				TargetCanonicalEntityID: "entity-tenant",
				ScopeID:                 "tenant-123",
				Direction:               "BIDIRECTIONAL",
				Cardinality:             "ONE_TO_ONE",
				Authority:               "baobab",
				Confidence:              "CONFIRMED",
				Status:                  "ACTIVE",
				ResolutionPriority:      50,
				EffectiveFrom:           "2025-01-01T00:00:00Z",
			},
		},
	}

	resolved, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.Mapping.ID != "mapping-tenant" {
		t.Fatalf("expected tenant-scoped mapping, got %q", resolved.Mapping.ID)
	}
	if resolved.Specificity <= 0 {
		t.Fatal("expected positive specificity")
	}
}

func TestMappingResolverEnforcesScopeAndTemporalWindow(t *testing.T) {
	at := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	base := validMapping("wrong-tenant", "canonical-1", "external-wrong")
	base.ResolutionPriority = 100
	valid := validMapping("right-tenant", "canonical-1", "external-right")
	valid.ResolutionPriority = 10
	expired := validMapping("expired", "canonical-1", "external-expired")
	expired.EffectiveTo = at.Format(time.RFC3339)

	resolved, err := (MappingResolverImpl{}).Resolve(context.Background(), MappingResolutionQuery{
		CanonicalEntityID: "canonical-1",
		Context:           Context{TenantID: "tenant-a"},
		Candidates:        []domain.Mapping{base, valid, expired},
		Scopes: map[string]domain.MappingScope{
			"wrong-tenant": {ScopeID: "wrong-tenant", TenantID: "tenant-b"},
			"right-tenant": {ScopeID: "right-tenant", TenantID: "tenant-a"},
			"expired":      {ScopeID: "expired", TenantID: "tenant-a"},
		},
		At: at,
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.Mapping.ExternalReferenceID != "external-right" {
		t.Fatalf("expected scope-compatible active mapping, got %q", resolved.Mapping.ExternalReferenceID)
	}
}

func TestMappingResolverFailsClosedOnAmbiguity(t *testing.T) {
	first := validMapping("tenant-scope", "canonical-1", "external-1")
	second := validMapping("tenant-scope", "canonical-1", "external-2")
	first.ID, second.ID = "a", "b"

	_, err := (MappingResolverImpl{}).Resolve(context.Background(), MappingResolutionQuery{
		CanonicalEntityID: "canonical-1",
		Context:           Context{TenantID: "tenant-a"},
		Candidates:        []domain.Mapping{first, second},
		Scopes: map[string]domain.MappingScope{
			"tenant-scope": {ScopeID: "tenant-scope", TenantID: "tenant-a"},
		},
	})
	if !errors.Is(err, ErrMappingAmbiguous) {
		t.Fatalf("expected ambiguity failure, got %v", err)
	}
}

func TestMappingResolverReversePreservesCanonicalIdentity(t *testing.T) {
	mapping := validMapping("tenant-scope", "canonical-stable", "external-1")
	mapping.Direction = "EXTERNAL_TO_CANONICAL"
	resolved, quarantine, err := (MappingResolverImpl{}).ResolveReverse(context.Background(), ReverseMappingResolutionQuery{
		ExternalReferenceID: "external-1",
		Context:             Context{TenantID: "tenant-a", CorrelationID: "corr-1"},
		Candidates:          []domain.Mapping{mapping},
		Scopes: map[string]domain.MappingScope{
			"tenant-scope": {ScopeID: "tenant-scope", TenantID: "tenant-a"},
		},
	})
	if err != nil {
		t.Fatalf("reverse resolve failed: %v", err)
	}
	if quarantine.Quarantined {
		t.Fatal("resolved mapping must not be quarantined")
	}
	if resolved.Mapping.CanonicalEntityID != "canonical-stable" {
		t.Fatalf("canonical identity changed: %q", resolved.Mapping.CanonicalEntityID)
	}
}

func TestMappingResolverQuarantinesUnresolvedReverseMapping(t *testing.T) {
	_, quarantine, err := (MappingResolverImpl{}).ResolveReverse(context.Background(), ReverseMappingResolutionQuery{
		ExternalReferenceID: "unknown-external",
		Context:             Context{TenantID: "tenant-a", CorrelationID: "corr-1"},
	})
	if !errors.Is(err, ErrReverseMappingUnresolved) {
		t.Fatalf("expected unresolved reverse mapping, got %v", err)
	}
	if !quarantine.Quarantined || quarantine.ExternalReferenceID != "unknown-external" || quarantine.CorrelationID != "corr-1" {
		t.Fatalf("unexpected quarantine decision: %#v", quarantine)
	}
}

func validMapping(scopeID, canonicalID, externalID string) domain.Mapping {
	return domain.Mapping{
		ID:                  externalID,
		MappingType:         "IDENTITY",
		TenantID:            "tenant-a",
		CanonicalEntityID:   canonicalID,
		ExternalReferenceID: externalID,
		ScopeID:             scopeID,
		Direction:           "BIDIRECTIONAL",
		Cardinality:         "ONE_TO_ONE",
		Authority:           "baobab",
		Confidence:          "CONFIRMED",
		Status:              "ACTIVE",
		EffectiveFrom:       "2025-01-01T00:00:00Z",
	}
}

func TestMappingResolverRejectsInactiveMappings(t *testing.T) {
	resolver := MappingResolverImpl{}
	_, err := resolver.Resolve(context.Background(), MappingResolutionQuery{
		CanonicalEntityID: "tenant-123",
		Context:           Context{TenantID: "tenant-123"},
		Candidates: []domain.Mapping{{
			ID:                      "mapping-inactive",
			MappingType:             "IDENTITY",
			TenantID:                "tenant-123",
			CanonicalEntityID:       "tenant-123",
			TargetCanonicalEntityID: "entity-inactive",
			ScopeID:                 "tenant-123",
			Direction:               "BIDIRECTIONAL",
			Cardinality:             "ONE_TO_ONE",
			Authority:               "baobab",
			Confidence:              "CONFIRMED",
			Status:                  "INACTIVE",
			EffectiveFrom:           "2025-01-01T00:00:00Z",
		}},
	})
	if err == nil {
		t.Fatal("expected inactive mapping rejection")
	}
}
