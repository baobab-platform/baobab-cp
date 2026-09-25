// Target path: internal/service/market_participation_service_test.go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

type fakeMarketParticipationStore struct {
	assignment domain.MarketAssignment
}

func (f *fakeMarketParticipationStore) GetMarketAssignment(context.Context, string) (domain.MarketAssignment, error) {
	return f.assignment, nil
}
func (f *fakeMarketParticipationStore) GetEffectiveMarketAssignment(context.Context, string, string, time.Time) (domain.MarketAssignment, error) {
	if f.assignment.ID == "" {
		return domain.MarketAssignment{}, errors.New("not found")
	}
	return f.assignment, nil
}
func (f *fakeMarketParticipationStore) CreateMarketAssignment(_ context.Context, a domain.MarketAssignment) error {
	f.assignment = a
	return nil
}
func (f *fakeMarketParticipationStore) UpdateMarketAssignmentGovernance(_ context.Context, a domain.MarketAssignment) error {
	f.assignment = a
	return nil
}

func TestRequireOperationalMarketParticipation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	store := &fakeMarketParticipationStore{assignment: domain.MarketAssignment{
		ID:       "11111111-1111-7111-8111-111111111111",
		TenantID: "zuribeans",
		MarketID: "market-ug",
		Capabilities: []domain.MarketParticipationCapability{
			domain.MarketParticipationSourcing,
			domain.MarketParticipationExporting,
		},
		EffectiveFrom: now.Add(-time.Hour),
		Status:        domain.MarketParticipationActive,
		Source:        domain.MarketParticipationSourceProvisioning,
		PolicyVersion: "1",
	}}
	svc := NewMarketParticipationService(store)
	svc.clock = func() time.Time { return now }

	if _, err := svc.RequireOperational(
		context.Background(), "zuribeans", "market-ug",
		domain.MarketParticipationExporting,
	); err != nil {
		t.Fatalf("expected operational participation: %v", err)
	}

	if _, err := svc.RequireOperational(
		context.Background(), "zuribeans", "market-ug",
		domain.MarketParticipationImporting,
	); err == nil {
		t.Fatal("expected missing IMPORTING capability to fail closed")
	}
}
