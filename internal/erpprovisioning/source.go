package erpprovisioning

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

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
//   - functional currencies are not here at all: neither the plan nor this
//     repository's markets carry them, and Finance owns them. The worker takes
//     them from ERP's effective Finance baseline (Worker.Submit).
//
// Countries are intent only: ERP refuses requested_countries that differ from
// the markets of the approved plan.
type PlanSource struct {
	Provisionings ConvergedSource
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
	entities := sorted(desired.LegalEntities)
	countries := make([]string, 0, len(desired.MarketParticipation))
	for _, m := range desired.MarketParticipation {
		countries = append(countries, m.Market)
	}
	sort.Strings(countries)
	return Authorised{
		TenantID:       c.TenantID,
		Authority:      Authority{TenantProvisioningID: c.Key, PlanID: c.Plan.PlanID, PlanVersion: c.Plan.PlanVersion, PlanDigest: c.Plan.PlanDigest},
		LegalEntityIDs: entities, Countries: countries,
	}, nil
}

var _ Source = PlanSource{}
