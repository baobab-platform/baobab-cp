// Target path: internal/domain/trade_lane_test.go
package domain

import "testing"

func validTradeLane() TradeLane {
	return TradeLane{
		ID:                      "tlane_ugza",
		TenantID:                "tenant-zuribeans",
		OriginMarketID:          "market-ug",
		DestinationMarketID:     "market-za",
		Direction:               TradeLaneCrossMarket,
		Status:                  TradeLaneSuspended,
		PermittedCapabilityKeys: []string{"procurement.order", "logistics.shipment"},
	}
}

func TestTradeLaneValidate(t *testing.T) {
	t.Parallel()
	lane := validTradeLane()
	if err := lane.Validate(); err != nil {
		t.Fatalf("expected valid lane, got %v", err)
	}
}

func TestTradeLaneRejectsSameMarket(t *testing.T) {
	t.Parallel()
	lane := validTradeLane()
	lane.DestinationMarketID = lane.OriginMarketID
	if err := lane.Validate(); err == nil {
		t.Fatal("expected same-market lane to be rejected")
	}
}

func TestTradeLaneRejectsDuplicateCapability(t *testing.T) {
	t.Parallel()
	lane := validTradeLane()
	lane.PermittedCapabilityKeys = []string{"logistics.shipment", "logistics.shipment"}
	if err := lane.Validate(); err == nil {
		t.Fatal("expected duplicate capability to be rejected")
	}
}

func TestTradeLaneAllowsCapability(t *testing.T) {
	t.Parallel()
	lane := validTradeLane()
	if !lane.AllowsCapability("procurement.order") {
		t.Fatal("expected procurement.order to be permitted")
	}
	if lane.AllowsCapability("pricing.override") {
		t.Fatal("unexpected capability permission")
	}
}
