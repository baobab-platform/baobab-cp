// ADR-BCP-018 / ADR-BCP-017 — INTERNAL subscription eligibility (fail closed).

package organisation

import (
	"context"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// FoundingSponsorshipPolicy is the authoritative current-time gate for
// first-party operating businesses. A missing sponsor never becomes permission.
type FoundingSponsorshipPolicy interface {
	FoundingOperatingEligibility(context.Context, string, time.Time) (bool, bool, error)
}

// EligibilityResolver derives INTERNAL eligibility from persisted relationships.
type EligibilityResolver struct {
	Orgs repository.OrganisationRepository
	Sponsorship FoundingSponsorshipPolicy
	// PlatformID selects the platform whose owners qualify; DefaultPlatformID when empty.
	PlatformID string
}

// ResolveInternalEligibility returns (eligible, nil), or (false, nil) when
// denied. Infrastructure errors are returned as a non-nil error and never
// read as eligibility.
func (r *EligibilityResolver) ResolveInternalEligibility(ctx context.Context, organisationID string, at time.Time) (bool, error) {
	basis, err := r.InternalEligibilityBasis(ctx, organisationID, at)
	return len(basis) > 0, err
}

// InternalEligibilityBasis returns the platform relationships that make
// organisationID INTERNAL-eligible at at, or none when it is not. It is the
// evidence an INTERNAL subscription classification records (ADR-BCP-017
// section 13). Infrastructure errors are never read as eligibility.
func (r *EligibilityResolver) InternalEligibilityBasis(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error) {
	if organisationID == "" {
		return nil, nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	platformID := r.Platform()
	prs, err := r.Orgs.ListPlatformRelationships(ctx, organisationID, at)
	if err != nil {
		return nil, fmt.Errorf("list platform relationships: %w", err)
	}
	var onPlatform []domain.PlatformRelationship
	for _, pr := range prs {
		if pr.PlatformID == platformID {
			onPlatform = append(onPlatform, pr)
		}
	}
	// A platform relationship is necessary but not sufficient for founding
	// operating businesses. Re-evaluate the sponsor's VERIFIED authority at
	// every classification and explanation read; do not trust event projections.
	if r.Sponsorship != nil {
		applies, eligible, err := r.Sponsorship.FoundingOperatingEligibility(ctx, organisationID, at)
		if err != nil {
			return nil, fmt.Errorf("check current founding sponsorship: %w", err)
		}
		if applies && !eligible {
			return nil, nil
		}
	}
	if len(onPlatform) == 0 {
		return nil, nil
	}
	ancestry, err := r.Orgs.ListCorporateControlAncestry(ctx, organisationID, at)
	if err != nil {
		return nil, fmt.Errorf("list corporate control ancestry: %w", err)
	}
	owners, err := r.Orgs.ListPlatformOwnerOrganisations(ctx, platformID, at)
	if err != nil {
		return nil, fmt.Errorf("list platform owners: %w", err)
	}
	return domain.QualifyingPlatformRelationships(domain.InternalEligibilityEvidence{
		OrganisationID:         organisationID,
		PlatformRelationships:  onPlatform,
		CorporateRelationships: ancestry,
		EvaluatedAt:            at,
	}, owners), nil
}

// Platform is the platform whose owners qualify.
func (r *EligibilityResolver) Platform() string {
	if r.PlatformID == "" {
		return DefaultPlatformID
	}
	return r.PlatformID
}
