package subscription

import (
	"context"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// CurrentInternalAuthority evaluates the current subscription and governed
// eligibility, not the classification explanation or its unbounded history.
// It performs no writes, caches no positive result and confers no capability.
func (c *Classifier) CurrentInternalAuthority(ctx context.Context, tenantID, subscriptionID, reference string, at time.Time) (bool, error) {
	if c == nil || c.Orgs == nil || c.Eligibility == nil || at.IsZero() {
		return false, errors.New("current INTERNAL authority dependencies unavailable")
	}
	reader, ok := c.Repo.(repository.CurrentProductSubscriptionReader)
	if !ok {
		return false, errors.New("current subscription reader unavailable")
	}
	row, err := reader.ReadCurrentProductSubscription(ctx, tenantID, subscriptionID)
	if err != nil {
		return false, err
	}
	if row.TenantID != tenantID || row.SubscriptionID != subscriptionID || row.Revision < 1 ||
		row.Status != "ACTIVE" || row.EffectiveFrom.IsZero() || row.EffectiveFrom.After(at) ||
		(row.EffectiveTo != nil && !at.Before(*row.EffectiveTo)) ||
		row.ClassificationID == "" || row.SubscriptionType != string(domain.SubscriptionInternal) ||
		row.ClassificationReference != reference || !referencePattern.MatchString(reference) ||
		row.InternalEligibility == nil || row.InternalEligibility.OrganisationID == "" ||
		row.InternalEligibility.EligibilityStatus != "ELIGIBLE" || len(row.InternalEligibility.BasisRelationshipIDs) == 0 ||
		row.InternalEligibility.EvaluatedAt.IsZero() || c.Eligibility.Platform() == "" ||
		row.InternalEligibility.PlatformID != c.Eligibility.Platform() {
		return false, nil
	}
	mappings, err := c.Orgs.ListLiveTenantOrganisationMappings(ctx, tenantID)
	if err != nil {
		return false, err
	}
	primary, count := "", 0
	for _, m := range mappings {
		if m.TenantID == tenantID && m.MappingRole == domain.TenantOrgRolePrimary &&
			m.Status == domain.RelationshipStatusActive && m.InEffect(at) {
			primary, count = m.OrganisationID, count+1
		}
	}
	if count != 1 || primary != row.InternalEligibility.OrganisationID {
		return false, nil
	}
	basis, err := c.basis(ctx, primary, at)
	return err == nil && len(basis) > 0, err
}
