// Target path: baobab-platform/baobab-cp/internal/service/organisation/eligibility.go
//
// ADR-BCP-018 CP-5 — INTERNAL subscription eligibility resolution.
//
// Purpose
// -------
// Resolve whether an Organisation may be treated as INTERNAL for product
// subscription eligibility (ADR-BCP-017), using only verified PlatformRelationship
// and CorporateRelationship evidence.
//
// This service MUST NOT:
//   - grant tenant data access
//   - grant administrative rights over other tenants
//   - treat CorporateGroup membership or PlatformAccount as authorization
//
// Fail closed when evidence is missing or conflicted.

package organisation

import (
	"context"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// EligibilityResolver derives INTERNAL eligibility from persisted relationships.
type EligibilityResolver struct {
	Orgs repository.OrganisationRepository
	// PlatformOwnerOrgIDs returns the set of organisation ids that currently
	// hold PLATFORM_OWNER (or equivalent). Prefer loading from verified
	// platform_relationship rows; a static seed is acceptable for Nabhold root
	// during transition.
	PlatformOwnerOrgIDs func(ctx context.Context, at time.Time) (map[string]struct{}, error)
}

// ResolveInternalEligibility evaluates INTERNAL eligibility for organisationID at time at.
// Returns (eligible, nil) or (false, nil) when evidence is insufficient (fail closed).
// Returns a non-nil error only for infrastructure failures (DB errors), not for deny.
func (r *EligibilityResolver) ResolveInternalEligibility(ctx context.Context, organisationID string, at time.Time) (bool, error) {
	if organisationID == "" {
		return false, nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	pr, err := r.Orgs.GetPlatformRelationship(ctx, organisationID, at)
	if err != nil {
		return false, fmt.Errorf("load platform relationship: %w", err)
	}
	if pr == nil {
		return false, nil // fail closed
	}

	corpRels, err := r.Orgs.ListCorporateRelationshipsByOrganisation(ctx, organisationID, at)
	if err != nil {
		return false, fmt.Errorf("load corporate relationships: %w", err)
	}

	var owners map[string]struct{}
	if r.PlatformOwnerOrgIDs != nil {
		owners, err = r.PlatformOwnerOrgIDs(ctx, at)
		if err != nil {
			return false, fmt.Errorf("load platform owners: %w", err)
		}
	}
	if owners == nil {
		owners = map[string]struct{}{}
	}

	ev := domain.InternalEligibilityEvidence{
		OrganisationID:         organisationID,
		PlatformRelationship:   pr,
		CorporateRelationships: corpRels,
		EvaluatedAt:            at,
	}
	return domain.DeriveInternalEligibility(ev, owners), nil
}

// LoadPlatformOwnersFromRepo builds the owner set from verified PLATFORM_OWNER rows.
// Wire as EligibilityResolver.PlatformOwnerOrgIDs when a full scan helper exists.
// Until then, callers may seed Nabhold root from configuration.
func LoadPlatformOwnersFromKnownIDs(ids ...string) func(ctx context.Context, at time.Time) (map[string]struct{}, error) {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			set[id] = struct{}{}
		}
	}
	return func(ctx context.Context, at time.Time) (map[string]struct{}, error) {
		return set, nil
	}
}
