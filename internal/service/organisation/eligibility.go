// Target path: internal/service/organisation/eligibility.go
//
// ADR-BCP-018 / ADR-BCP-017 — INTERNAL subscription eligibility (fail closed).

package organisation

import (
	"context"
	"fmt"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// EligibilityResolver derives INTERNAL eligibility from persisted relationships.
type EligibilityResolver struct {
	Orgs repository.OrganisationRepository
	// PlatformOwnerOrgIDs returns organisation ids with verified PLATFORM_OWNER.
	PlatformOwnerOrgIDs func(ctx context.Context, at time.Time) (map[string]struct{}, error)
}

// ResolveInternalEligibility returns (eligible, nil) or (false, nil) when denied.
// Infrastructure errors are returned as non-nil error.
func (r *EligibilityResolver) ResolveInternalEligibility(ctx context.Context, organisationID string, at time.Time) (bool, error) {
	if organisationID == "" {
		return false, nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	prs, err := r.Orgs.ListPlatformRelationships(ctx, organisationID, at)
	if err != nil {
		return false, fmt.Errorf("list platform relationships: %w", err)
	}
	if len(prs) == 0 {
		return false, nil
	}
	corp, err := r.Orgs.ListCorporateRelationshipsByOrganisation(ctx, organisationID, at)
	if err != nil {
		return false, fmt.Errorf("list corporate relationships: %w", err)
	}
	owners := map[string]struct{}{}
	if r.PlatformOwnerOrgIDs != nil {
		owners, err = r.PlatformOwnerOrgIDs(ctx, at)
		if err != nil {
			return false, fmt.Errorf("load platform owners: %w", err)
		}
	}
	return domain.DeriveInternalEligibility(domain.InternalEligibilityEvidence{
		OrganisationID:         organisationID,
		PlatformRelationships:  prs,
		CorporateRelationships: corp,
		EvaluatedAt:            at,
	}, owners), nil
}
