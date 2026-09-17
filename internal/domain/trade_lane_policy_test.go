// Target path: internal/domain/trade_lane_policy_test.go
package domain

import (
	"testing"
	"time"
)

func TestCrossMarketLaneRequiresExportAndImport(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	lane := validTradeLane()

	origin := MarketAssignment{
		TenantID: "tenant-zuribeans", MarketID: "market-ug",
		Capabilities: []MarketParticipationCapability{MarketParticipationExporting},
		EffectiveFrom: now.Add(-time.Hour),
	}
	destination := MarketAssignment{
		TenantID: "tenant-zuribeans", MarketID: "market-za",
		Capabilities: []MarketParticipationCapability{MarketParticipationImporting},
		EffectiveFrom: now.Add(-time.Hour),
	}

	if err := ValidateTradeLaneParticipation(lane, origin, destination, now); err != nil {
		t.Fatalf("expected participation to authorize lane: %v", err)
	}

	destination.Capabilities = []MarketParticipationCapability{MarketParticipationSelling}
	if err := ValidateTradeLaneParticipation(lane, origin, destination, now); err == nil {
		t.Fatal("expected missing IMPORTING capability to reject activation")
	}
}

func TestTradeLaneRejectsCrossTenantParticipation(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	lane := validTradeLane()
	origin := MarketAssignment{
		TenantID: "another-tenant", MarketID: lane.OriginMarketID,
		Capabilities: []MarketParticipationCapability{MarketParticipationExporting},
		EffectiveFrom: now.Add(-time.Hour),
	}
	destination := MarketAssignment{
		TenantID: lane.TenantID, MarketID: lane.DestinationMarketID,
		Capabilities: []MarketParticipationCapability{MarketParticipationImporting},
		EffectiveFrom: now.Add(-time.Hour),
	}
	if err := ValidateTradeLaneParticipation(lane, origin, destination, now); err == nil {
		t.Fatal("expected cross-tenant participation to be rejected")
	}
}
