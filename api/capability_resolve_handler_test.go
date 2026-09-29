package api

import (
	"context"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
)

func seedCapabilityResolveFixture(t *testing.T, repo *repository.Repository, tenantID string) {
	t.Helper()
	repo.Mappings[tenantID] = []domain.Mapping{{
		ID:                      "mapping-tenant",
		MappingType:             "IDENTITY",
		TenantID:                tenantID,
		CanonicalEntityID:       tenantID,
		TargetCanonicalEntityID: "entity-tenant",
		ScopeID:                 tenantID,
		Direction:               "BIDIRECTIONAL",
		Cardinality:             "ONE_TO_ONE",
		Authority:               "baobab",
		Confidence:              "CONFIRMED",
		Status:                  "ACTIVE",
		ResolutionPriority:      50,
		EffectiveFrom:           "2025-01-01T00:00:00Z",
	}}
	repo.Bindings["commerce.order.create"] = []resolver.CapabilityBinding{{
		CapabilityKey:    "commerce.order.create",
		EngineID:         "engine-1",
		EngineInstanceID: "instance-1",
		BindingMode:      "PRIMARY",
		Priority:         100,
		Status:           "ACTIVE",
		ContractVersion:  "v1",
	}}
	repo.EngineInstances["engine-1"] = []resolver.EngineInstance{{
		ID:          "instance-1",
		EngineID:    "engine-1",
		Region:      "af-south-1",
		Environment: "production",
		Status:      "ACTIVE",
	}}
}

func seedResolvedContext(t *testing.T, repo *repository.Repository, id, tenantID string) {
	t.Helper()
	resolved := domain.Context{
		ID:            id,
		PrincipalID:   "principal-abc",
		TenantID:      tenantID,
		MarketID:      "market-789",
		CountryCode:   "ZA",
		CurrencyCode:  "ZAR",
		Locale:        "en-ZA",
		CorrelationID: "correlation-123",
		ResolvedAt:    time.Now().UTC(),
		Provenance: map[string]domain.ContextSource{
			"tenant_id": {Source: "verified_token", TrustLevel: domain.TrustVerified},
		},
	}
	if err := repo.CreateContext(context.Background(), resolved); err != nil {
		t.Fatalf("seed resolved context: %v", err)
	}
}
