// ADR-BCP-018 sections 74-75 and 167 — corporate change and divestiture review.

package organisation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/metrics"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// CorporateChangeReviewer carries a corporate change through the review the
// ADR requires (section 75): a corporate fact ending never silently rewrites
// PlatformRelationships, subscriptions or tenants. It reports the
// PLATFORM_GROUP_AFFILIATE relationships that rested on the fact, with the
// INTERNAL eligibility their organisations have now, and applies the
// governance decision taken on each. Canonical organisation, legal entity and
// tenant identities never change here (section 74).
type CorporateChangeReviewer struct {
	Orgs        repository.OrganisationRepository
	Eligibility *EligibilityResolver
}

// AffiliateUnderReview is one PLATFORM_GROUP_AFFILIATE relationship whose
// basis corporate relationship has changed.
type AffiliateUnderReview struct {
	PlatformRelationshipID string `json:"platform_relationship_id"`
	PlatformID             string `json:"platform_id"`
	OrganisationID         string `json:"organisation_id"`
	// InternalEligible is the organisation's ADR-BCP-017 INTERNAL eligibility
	// at the review time. It is false once the basis is no longer
	// consequential, unless another qualifying relationship still holds.
	InternalEligible bool `json:"internal_eligible"`
}

// CorporateChangeReview lists what a corporate change leaves for governance.
type CorporateChangeReview struct {
	CorporateRelationshipID string                 `json:"corporate_relationship_id"`
	ReviewedAt              time.Time              `json:"reviewed_at"`
	Affiliates              []AffiliateUnderReview `json:"affiliates,omitempty"`
}

// EndCorporateRelationship ends the corporate fact and returns the review it
// triggers.
func (r *CorporateChangeReviewer) EndCorporateRelationship(ctx context.Context, id string, at time.Time, reason string, actor repository.AuditActor) (CorporateChangeReview, error) {
	if err := r.Orgs.EndCorporateRelationship(ctx, id, at, reason, actor); err != nil {
		return CorporateChangeReview{}, err
	}
	return r.Review(ctx, id, at)
}

// Review reports the live affiliates resting on corporateRelationshipID and
// their organisations' INTERNAL eligibility at at. It changes nothing.
func (r *CorporateChangeReviewer) Review(ctx context.Context, corporateRelationshipID string, at time.Time) (CorporateChangeReview, error) {
	review := CorporateChangeReview{CorporateRelationshipID: corporateRelationshipID, ReviewedAt: at}
	if r.Eligibility == nil {
		return review, errors.New("corporate change review requires an eligibility resolver")
	}
	affiliates, err := r.Orgs.ListLivePlatformRelationshipsByBasis(ctx, corporateRelationshipID)
	if err != nil {
		return review, fmt.Errorf("list affiliates resting on %s: %w", corporateRelationshipID, err)
	}
	for _, pr := range affiliates {
		eligible, err := r.Eligibility.ResolveInternalEligibility(ctx, pr.OrganisationID, at)
		if err != nil {
			return review, fmt.Errorf("resolve eligibility of %s: %w", pr.OrganisationID, err)
		}
		review.Affiliates = append(review.Affiliates, AffiliateUnderReview{
			PlatformRelationshipID: pr.ID, PlatformID: pr.PlatformID, OrganisationID: pr.OrganisationID, InternalEligible: eligible,
		})
	}
	metrics.InternalEligibilityReviews.Add(uint64(len(review.Affiliates)))
	return review, nil
}

// AffiliateTermination is a governance decision to terminate a
// PLATFORM_GROUP_AFFILIATE relationship after a corporate change.
type AffiliateTermination struct {
	PlatformRelationshipID string
	At                     time.Time
	Reason                 string
	// DecisionReference cites the governance decision (for example a
	// commercial reclassification approval). It is required.
	DecisionReference string
	// ReclassifyAs optionally records the organisation's new relationship
	// with the platform, e.g. EXTERNAL_CLIENT. It is recorded PENDING and
	// UNVERIFIED: it confers nothing until governance verifies it.
	ReclassifyAs domain.PlatformRelationshipType
}

// reclassificationTypes are the relationships a divested affiliate may be
// reclassified into. Owner, operator and affiliate need their own governed
// basis and are never produced by a divestiture.
var reclassificationTypes = map[domain.PlatformRelationshipType]bool{
	domain.PlatformRelExternalClient: true,
	domain.PlatformRelPartner:        true,
	domain.PlatformRelManagedEntity:  true,
}

// TerminateAffiliate ends the affiliate relationship and, when requested,
// records the pending reclassification. It returns the reclassified
// relationship's id, or "" when none was requested.
func (r *CorporateChangeReviewer) TerminateAffiliate(ctx context.Context, t AffiliateTermination, actor repository.AuditActor) (string, error) {
	if strings.TrimSpace(t.DecisionReference) == "" {
		return "", errors.New("terminating an affiliate requires a governance decision reference")
	}
	if t.ReclassifyAs != "" && !reclassificationTypes[t.ReclassifyAs] {
		return "", fmt.Errorf("a divested affiliate cannot be reclassified as %s", t.ReclassifyAs)
	}
	affiliate, err := r.liveAffiliate(ctx, t.PlatformRelationshipID)
	if err != nil {
		return "", err
	}
	reason := t.Reason + " (decision " + t.DecisionReference + ")"
	if err := r.Orgs.EndPlatformRelationship(ctx, affiliate.ID, t.At, reason, actor); err != nil {
		return "", err
	}
	if t.ReclassifyAs == "" {
		metrics.PlatformRelationshipReclassifications.Inc(metrics.OutcomeAffiliateEnded)
		return "", nil
	}
	id, err := r.Orgs.EnsurePlatformRelationship(ctx, domain.PlatformRelationship{
		PlatformID: affiliate.PlatformID, OrganisationID: affiliate.OrganisationID, RelationshipType: t.ReclassifyAs,
		VerificationState: domain.VerificationPendingReview, Status: domain.RelationshipStatusPending,
		EffectiveFrom: t.At, AdmissionDecisionID: t.DecisionReference, SourceAuthority: "platform-governance",
		Metadata: map[string]any{"reclassified_from_platform_relationship_id": affiliate.ID},
	}, actor)
	if err != nil {
		// The affiliate is ended even though the reclassification failed.
		metrics.PlatformRelationshipReclassifications.Inc(metrics.OutcomeAffiliateEnded)
		return "", err
	}
	metrics.PlatformRelationshipReclassifications.Inc(metrics.OutcomeAffiliateMovedTo)
	return id, nil
}

// liveAffiliate finds the live PLATFORM_GROUP_AFFILIATE relationship id.
func (r *CorporateChangeReviewer) liveAffiliate(ctx context.Context, id string) (domain.PlatformRelationship, error) {
	pr, err := r.Orgs.GetPlatformRelationship(ctx, id)
	if err != nil {
		return domain.PlatformRelationship{}, err
	}
	if pr == nil {
		return domain.PlatformRelationship{}, fmt.Errorf("platform relationship %s not found", id)
	}
	if pr.RelationshipType != domain.PlatformRelGroupAffiliate {
		return domain.PlatformRelationship{}, fmt.Errorf("platform relationship %s is %s, not PLATFORM_GROUP_AFFILIATE", id, pr.RelationshipType)
	}
	return *pr, nil
}
