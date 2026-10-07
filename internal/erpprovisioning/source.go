package erpprovisioning

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// ErrFinanceBaselineUnavailable means the Finance-authoritative functional
// currencies of the legal entities could not be resolved, so nothing is sent to
// ERP.
var ErrFinanceBaselineUnavailable = errors.New("the Finance baseline of the legal entities is not available")

// FinanceBaselines resolves the functional currencies of legal entities from
// their Finance-authoritative baseline.
//
// The Control Plane does not own accounting facts. A market's registry
// currency is a market fact, not an entity's functional currency, and neither
// the approved plan nor the desired state carries one, so the Source never
// derives it. There is no resolver until Shared defines a versioned Finance
// baseline reference and ERP exposes it; until one is configured every
// submission stops here, which is the fail-closed outcome (ERP would refuse a
// guess anyway: erp/v1 answers 409 PLAN_AUTHORITY_MISMATCH).
type FinanceBaselines interface {
	FunctionalCurrencies(ctx context.Context, tenantID string, legalEntityIDs []string) ([]string, error)
}

// ConvergedSource is what the Source reads; PostgresRepository satisfies it.
type ConvergedSource interface {
	GetConvergedProvisioning(ctx context.Context, id string) (repository.ConvergedProvisioning, error)
	GetDesiredState(ctx context.Context, id string, version int64) (convergence.DesiredState, error)
}

// PlanSource assembles what an approved plan authorises ERP to provision.
//
//   - tenant, plan tuple, legal entities and countries come from the approved
//     plan and the desired state it froze (the single definition of "approved
//     plan" is repository.ApprovedPlanProblem, shared with ERP's assignment read
//     and with context validation, so the three can never disagree);
//   - functional currencies come from the Finance baseline, never from this
//     repository's markets.
//
// Countries are intent only: ERP refuses requested_countries that differ from
// the markets of the approved plan.
type PlanSource struct {
	Provisionings ConvergedSource
	Finance       FinanceBaselines
}

func (s PlanSource) Authorised(ctx context.Context, tenantProvisioningID string) (Authorised, error) {
	if s.Provisionings == nil {
		return Authorised{}, errors.New("plan source is not configured")
	}
	id, err := repository.ProvisioningUUID(tenantProvisioningID)
	if err != nil {
		return Authorised{}, fmt.Errorf("%w: %v", ErrNotAuthorised, err)
	}
	c, err := s.Provisionings.GetConvergedProvisioning(ctx, id)
	if errors.Is(err, repository.ErrProvisioningNotFound) {
		return Authorised{}, fmt.Errorf("%w: no such provisioning", ErrNotAuthorised)
	}
	if err != nil {
		return Authorised{}, fmt.Errorf("read provisioning: %w", err)
	}
	desired, err := s.Provisionings.GetDesiredState(ctx, c.ID, c.DesiredStateVersion)
	if errors.Is(err, repository.ErrProvisioningNotFound) {
		return Authorised{}, fmt.Errorf("%w: the frozen desired state is missing", ErrNotAuthorised)
	}
	if err != nil {
		return Authorised{}, fmt.Errorf("read desired state: %w", err)
	}
	if reason := repository.ApprovedPlanProblem(c, desired); reason != "" {
		return Authorised{}, fmt.Errorf("%w: %s", ErrNotAuthorised, reason)
	}
	if len(desired.LegalEntities) == 0 || len(desired.MarketParticipation) == 0 {
		return Authorised{}, fmt.Errorf("%w: the desired state names no legal entity or market", ErrNotAuthorised)
	}
	if s.Finance == nil {
		return Authorised{}, ErrFinanceBaselineUnavailable
	}
	entities := sorted(desired.LegalEntities)
	currencies, err := s.Finance.FunctionalCurrencies(ctx, c.TenantID, entities)
	if err != nil {
		return Authorised{}, fmt.Errorf("%w: %v", ErrFinanceBaselineUnavailable, err)
	}
	if len(currencies) == 0 {
		return Authorised{}, fmt.Errorf("%w: no functional currency", ErrFinanceBaselineUnavailable)
	}
	countries := make([]string, 0, len(desired.MarketParticipation))
	for _, m := range desired.MarketParticipation {
		countries = append(countries, m.Market)
	}
	sort.Strings(countries)
	return Authorised{
		TenantID:       c.TenantID,
		Authority:      Authority{TenantProvisioningID: c.Key, PlanID: c.Plan.PlanID, PlanVersion: c.Plan.PlanVersion, PlanDigest: c.Plan.PlanDigest},
		LegalEntityIDs: entities, Countries: countries, Currencies: sorted(currencies),
	}, nil
}

var _ Source = PlanSource{}
