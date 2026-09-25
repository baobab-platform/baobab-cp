// Target path: internal/service/trade_lane_service_test.go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

type fakeTradeLaneRepo struct {
	lane        domain.TradeLane
	assignments map[string]domain.MarketAssignment
	saveErr     error
}

func (r *fakeTradeLaneRepo) GetTradeLane(context.Context, string, string) (domain.TradeLane, error) {
	return r.lane, nil
}
func (r *fakeTradeLaneRepo) GetMarketAssignment(_ context.Context, _ string, marketID string, _ time.Time) (domain.MarketAssignment, error) {
	a, ok := r.assignments[marketID]
	if !ok {
		return domain.MarketAssignment{}, errors.New("not found")
	}
	return a, nil
}
func (r *fakeTradeLaneRepo) SaveTradeLane(_ context.Context, lane domain.TradeLane) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.lane = lane
	return nil
}

func TestTradeLaneServiceActivate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	lane := domain.TradeLane{
		ID: "tlane_ugza", TenantID: "tenant-zuribeans",
		OriginMarketID: "market-ug", DestinationMarketID: "market-za",
		Direction: domain.TradeLaneCrossMarket, Status: domain.TradeLaneSuspended,
	}
	repo := &fakeTradeLaneRepo{
		lane: lane,
		assignments: map[string]domain.MarketAssignment{
			"market-ug": {
				TenantID: lane.TenantID, MarketID: "market-ug",
				Capabilities:  []domain.MarketParticipationCapability{domain.MarketParticipationExporting},
				EffectiveFrom: now.Add(-time.Hour),
			},
			"market-za": {
				TenantID: lane.TenantID, MarketID: "market-za",
				Capabilities:  []domain.MarketParticipationCapability{domain.MarketParticipationImporting},
				EffectiveFrom: now.Add(-time.Hour),
			},
		},
	}
	svc := NewTradeLaneService(repo)
	svc.clock = func() time.Time { return now }

	got, err := svc.Activate(context.Background(), lane.TenantID, lane.ID)
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if got.Status != domain.TradeLaneActive {
		t.Fatalf("status = %s, want ACTIVE", got.Status)
	}
}
