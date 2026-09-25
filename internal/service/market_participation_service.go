// Target path: internal/service/market_participation_service.go
//
// Application service for declarative MarketParticipation reconciliation.
// It is intentionally repository-interface driven so provisioning can call it
// without depending on PostgreSQL implementation details.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// MarketParticipationStore is deliberately narrow. The existing PostgreSQL
// repository can satisfy it after integration/postgres.go.patch.txt is merged.
type MarketParticipationStore interface {
	GetMarketAssignment(ctx context.Context, assignmentID string) (domain.MarketAssignment, error)
	GetEffectiveMarketAssignment(ctx context.Context, tenantID, marketID string, at time.Time) (domain.MarketAssignment, error)
	CreateMarketAssignment(ctx context.Context, assignment domain.MarketAssignment) error
	UpdateMarketAssignmentGovernance(ctx context.Context, assignment domain.MarketAssignment) error
}

type MarketParticipationService struct {
	store MarketParticipationStore
	clock func() time.Time
}

func NewMarketParticipationService(store MarketParticipationStore) *MarketParticipationService {
	return &MarketParticipationService{store: store, clock: time.Now}
}

// CreateDesired is the provisioning entry point for a new desired
// participation. New records should normally begin PENDING and become ACTIVE
// only after provisioning/reconciliation validates dependencies.
func (s *MarketParticipationService) CreateDesired(
	ctx context.Context,
	assignment domain.MarketAssignment,
) error {
	if err := assignment.Validate(); err != nil {
		return fmt.Errorf("validate market participation: %w", err)
	}
	if err := domain.ValidateMarketParticipationGovernance(assignment); err != nil {
		return err
	}
	if assignment.Status == domain.MarketParticipationRetired {
		return errors.New("cannot create desired market participation already RETIRED")
	}
	return s.store.CreateMarketAssignment(ctx, assignment)
}

func (s *MarketParticipationService) Transition(
	ctx context.Context,
	assignmentID string,
	to domain.MarketParticipationStatus,
) (domain.MarketAssignment, error) {
	if !to.Valid() {
		return domain.MarketAssignment{}, fmt.Errorf("invalid target status %q", to)
	}
	current, err := s.store.GetMarketAssignment(ctx, assignmentID)
	if err != nil {
		return domain.MarketAssignment{}, err
	}
	if !domain.CanTransitionMarketParticipation(current.Status, to) {
		return domain.MarketAssignment{}, fmt.Errorf(
			"market participation transition %s -> %s is not allowed", current.Status, to,
		)
	}
	current.Status = to
	if err := s.store.UpdateMarketAssignmentGovernance(ctx, current); err != nil {
		return domain.MarketAssignment{}, err
	}
	return current, nil
}

// RequireOperational is a reusable fail-closed guard for TradeLane,
// Context resolution and later provisioning readiness checks.
func (s *MarketParticipationService) RequireOperational(
	ctx context.Context,
	tenantID string,
	marketID string,
	required ...domain.MarketParticipationCapability,
) (domain.MarketAssignment, error) {
	at := s.clock().UTC()
	a, err := s.store.GetEffectiveMarketAssignment(ctx, tenantID, marketID, at)
	if err != nil {
		return domain.MarketAssignment{}, err
	}
	if !a.IsOperationalAt(at) {
		return domain.MarketAssignment{}, errors.New("market participation is not operational")
	}
	for _, capability := range required {
		if !a.HasParticipationCapability(capability) {
			return domain.MarketAssignment{}, fmt.Errorf(
				"market participation lacks required capability %s", capability,
			)
		}
	}
	return a, nil
}
