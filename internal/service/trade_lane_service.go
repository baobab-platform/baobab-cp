// Target path: internal/service/trade_lane_service.go
//
// Service-level lifecycle orchestration for TradeLane.
//
// Keeping the interface narrow prevents the TradeLane service from depending
// on unrelated repository responsibilities. The existing PostgreSQL repository
// can satisfy this interface after the methods in the integration snippet are
// merged into it.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

type TradeLaneRepository interface {
	GetTradeLane(ctx context.Context, tenantID, tradeLaneID string) (domain.TradeLane, error)
	GetMarketAssignment(ctx context.Context, tenantID, marketID string, at time.Time) (domain.MarketAssignment, error)
	SaveTradeLane(ctx context.Context, lane domain.TradeLane) error
}

type TradeLaneService struct {
	repository TradeLaneRepository
	clock      func() time.Time
}

func NewTradeLaneService(repository TradeLaneRepository) *TradeLaneService {
	return &TradeLaneService{
		repository: repository,
		clock:      time.Now,
	}
}

// Activate is intentionally policy-gated. Direct arbitrary writes from
// SUSPENDED to ACTIVE would bypass MarketParticipation checks and should not
// be exposed as the normal application path.
func (s *TradeLaneService) Activate(
	ctx context.Context,
	tenantID string,
	tradeLaneID string,
) (domain.TradeLane, error) {
	now := s.clock().UTC()

	lane, err := s.repository.GetTradeLane(ctx, tenantID, tradeLaneID)
	if err != nil {
		return domain.TradeLane{}, err
	}
	if lane.TenantID != tenantID {
		return domain.TradeLane{}, errors.New("trade lane tenant mismatch")
	}
	if lane.Status == domain.TradeLaneRetired {
		return domain.TradeLane{}, errors.New("retired trade lane cannot be reactivated")
	}
	if lane.Status == domain.TradeLaneActive {
		// Idempotent activation: already-active is success.
		return lane, nil
	}

	origin, err := s.repository.GetMarketAssignment(ctx, tenantID, lane.OriginMarketID, now)
	if err != nil {
		return domain.TradeLane{}, fmt.Errorf("load origin participation: %w", err)
	}
	destination, err := s.repository.GetMarketAssignment(ctx, tenantID, lane.DestinationMarketID, now)
	if err != nil {
		return domain.TradeLane{}, fmt.Errorf("load destination participation: %w", err)
	}

	if err := domain.ValidateTradeLaneParticipation(lane, origin, destination, now); err != nil {
		return domain.TradeLane{}, fmt.Errorf("trade lane cannot be activated: %w", err)
	}

	lane.Status = domain.TradeLaneActive
	lane.UpdatedAt = now
	if err := s.repository.SaveTradeLane(ctx, lane); err != nil {
		return domain.TradeLane{}, fmt.Errorf("persist activated trade lane: %w", err)
	}
	return lane, nil
}

func (s *TradeLaneService) Suspend(
	ctx context.Context,
	tenantID string,
	tradeLaneID string,
) (domain.TradeLane, error) {
	now := s.clock().UTC()
	lane, err := s.repository.GetTradeLane(ctx, tenantID, tradeLaneID)
	if err != nil {
		return domain.TradeLane{}, err
	}
	if lane.Status == domain.TradeLaneRetired {
		return domain.TradeLane{}, errors.New("retired trade lane cannot be suspended")
	}
	lane.Status = domain.TradeLaneSuspended
	lane.UpdatedAt = now
	if err := s.repository.SaveTradeLane(ctx, lane); err != nil {
		return domain.TradeLane{}, err
	}
	return lane, nil
}

func (s *TradeLaneService) Retire(
	ctx context.Context,
	tenantID string,
	tradeLaneID string,
) (domain.TradeLane, error) {
	now := s.clock().UTC()
	lane, err := s.repository.GetTradeLane(ctx, tenantID, tradeLaneID)
	if err != nil {
		return domain.TradeLane{}, err
	}
	lane.Status = domain.TradeLaneRetired
	lane.UpdatedAt = now
	if err := s.repository.SaveTradeLane(ctx, lane); err != nil {
		return domain.TradeLane{}, err
	}
	return lane, nil
}
