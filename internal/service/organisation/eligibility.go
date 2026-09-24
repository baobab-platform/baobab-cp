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
	// PlatformID selects the platform whose owners qualify; DefaultPlatformID when empty.
	PlatformID string
}

// ResolveInternalEligibility returns (eligible, nil), or (false, nil) when
// denied. Infrastructure errors are returned as a non-nil error and never
// read as eligibility.
func (r *EligibilityResolver) ResolveInternalEligibility(ctx context.Context, organisationID string, at time.Time) (bool, error) {
	if organisationID == "" {
		return false, nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	platformID := r.PlatformID
	if platformID == "" {
		platformID = DefaultPlatformID
	}
	prs, err := r.Orgs.ListPlatformRelationships(ctx, organisationID, at)
	if err != nil {
		return false, fmt.Errorf("list platform relationships: %w", err)
	}
	var onPlatform []domain.PlatformRelationship
	for _, pr := range prs {
		if pr.PlatformID == platformID {
			onPlatform = append(onPlatform, pr)
		}
	}
	if len(onPlatform) == 0 {
		return false, nil
	}
	ancestry, err := r.Orgs.ListCorporateControlAncestry(ctx, organisationID, at)
	if err != nil {
		return false, fmt.Errorf("list corporate control ancestry: %w", err)
	}
	owners, err := r.Orgs.ListPlatformOwnerOrganisations(ctx, platformID, at)
	if err != nil {
		return false, fmt.Errorf("list platform owners: %w", err)
	}
	return domain.DeriveInternalEligibility(domain.InternalEligibilityEvidence{
		OrganisationID:         organisationID,
		PlatformRelationships:  onPlatform,
		CorporateRelationships: ancestry,
		EvaluatedAt:            at,
	}, owners), nil
}
