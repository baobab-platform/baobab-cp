// Target path: internal/domain/market_participation_test.go
package domain

import (
	"testing"
	"time"
)

func TestMarketParticipationOperationalRequiresActiveAndEffective(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	a := MarketAssignment{
		TenantID: "tenant-zuribeans",
		MarketID: "market-ug",
		Capabilities: []MarketParticipationCapability{MarketParticipationExporting},
		EffectiveFrom: now.Add(-time.Hour),
		Status: MarketParticipationActive,
		Source: MarketParticipationSourceProvisioning,
		PolicyVersion: "1",
	}
	if !a.IsOperationalAt(now) {
		t.Fatal("expected ACTIVE effective participation to be operational")
	}
	a.Status = MarketParticipationSuspended
	if a.IsOperationalAt(now) {
		t.Fatal("SUSPENDED participation must not be operational")
	}
}

func TestMarketParticipationEffectiveToIsExclusive(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	a := MarketAssignment{EffectiveFrom: from, EffectiveTo: &to}
	if a.IsEffectiveAt(to) {
		t.Fatal("effective_to must be exclusive")
	}
}

func TestRetiredMarketParticipationIsTerminal(t *testing.T) {
	t.Parallel()
	if CanTransitionMarketParticipation(MarketParticipationRetired, MarketParticipationActive) {
		t.Fatal("RETIRED participation must not reactivate")
	}
}

func TestMarketParticipationGovernanceValidation(t *testing.T) {
	t.Parallel()
	a := MarketAssignment{
		Status: MarketParticipationActive,
		Source: MarketParticipationSourceProvisioning,
		PolicyVersion: "1",
	}
	if err := ValidateMarketParticipationGovernance(a); err != nil {
		t.Fatalf("unexpected governance validation error: %v", err)
	}
	a.PolicyVersion = ""
	if err := ValidateMarketParticipationGovernance(a); err == nil {
		t.Fatal("expected missing policy_version to fail")
	}
}
