package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	productdomain "github.com/baobab-platform/baobab-cp/internal/product/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// CompositionExpansionService turns a ProductSubscription's ProductVersion
// into real CapabilityGrant records, one per eligible CapabilityComposition
// member, recording each attempt as an EntitlementProjection (Technical
// Specification §31/§91; Programme Gate P4 "Product and Composition
// Engine").
//
// Scope note -- this is Gate P4's "basics" cut, not the full engine:
//
//   - Only MANDATORY and IMPORTANT members with an empty ActivationCondition
//     expand. OPTIONAL members (selected via ProductSubscription's
//     subscription_profiles) and any member with a non-empty
//     ActivationCondition are skipped -- no profile-selection or
//     condition-evaluation logic exists yet, and silently granting a
//     conditional or opt-in capability would violate the fail-closed
//     posture this codebase holds everywhere else (ADR-BCP-003 §6).
//   - CapabilityComposition.IncludesCompositions/IncompatibleWith are not
//     resolved -- only the composition's own direct Members expand.
//   - VersionConstraint is not evaluated -- a member expands regardless of
//     what it names.
//
// Each of these is recorded on CapabilityComposition/CompositionMember
// (internal/capability/domain/composition.go) as deferred scope, not
// silently dropped.
type CompositionExpansionService struct {
	Compositions repository.CompositionRepository
	Grants       interface {
		repository.CapabilityGrantWriter
		repository.CapabilityScopeWriter
	}
	Projections repository.EntitlementProjectionRepository
}

// Expand resolves version.CompositionKey's latest ACTIVE
// CapabilityComposition and, for each eligible member, creates a
// CapabilityGrant sourced from this subscription and an EntitlementProjection
// recording the outcome. A failure resolving the subscription's tenant
// scope or the composition itself is a subscription-level failure and is
// returned directly; a failure creating one member's grant is per-member --
// it is recorded as a FAILED EntitlementProjection and processing continues
// with the remaining members, so one bad member never blocks the rest of
// the subscription's entitlements from materializing.
func (s CompositionExpansionService) Expand(ctx context.Context, subscriptionID, tenantID string, version productdomain.ProductVersion) ([]productdomain.EntitlementProjection, error) {
	if subscriptionID == "" || tenantID == "" {
		return nil, errors.New("subscription_id and tenant_id are required")
	}
	if version.Status != productdomain.ProductLifecycleActive {
		return nil, fmt.Errorf("product version %s is not ACTIVE", version.ID)
	}

	composition, err := s.Compositions.GetActiveComposition(ctx, version.CompositionKey)
	if err != nil {
		return nil, fmt.Errorf("resolve composition %q: %w", version.CompositionKey, err)
	}

	scopeID, err := s.resolveTenantBaselineScope(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("resolve tenant baseline scope: %w", err)
	}

	now := time.Now().UTC()
	var projections []productdomain.EntitlementProjection
	for _, member := range composition.Members {
		if !eligibleForExpansion(member) {
			continue
		}

		projection := productdomain.EntitlementProjection{
			SubscriptionID: subscriptionID,
			TenantID:       tenantID,
			CapabilityKey:  member.CapabilityKey,
		}

		grant := capabilitydomain.CapabilityGrant{
			ID:              domain.NewUUIDv7(),
			TenantID:        tenantID,
			CapabilityKey:   member.CapabilityKey,
			ScopeID:         scopeID,
			Source:          capabilitydomain.GrantSourceProductSubscription,
			SourceReference: subscriptionID,
			Status:          capabilitydomain.GrantStatusActive,
			EffectiveFrom:   now,
		}

		if err := s.Grants.CreateGrant(ctx, grant); err != nil {
			projection.Status = productdomain.EntitlementProjectionStatusFailed
			projection.FailureReason = err.Error()
		} else {
			projection.Status = productdomain.EntitlementProjectionStatusMaterialized
			projection.GrantID = grant.ID
		}

		if err := s.Projections.CreateEntitlementProjection(ctx, projection); err != nil {
			return projections, fmt.Errorf("record entitlement projection for %q: %w", member.CapabilityKey, err)
		}
		projections = append(projections, projection)
	}
	return projections, nil
}

// eligibleForExpansion reports whether member is in this Gate's "basics"
// scope -- see CompositionExpansionService's doc comment.
func eligibleForExpansion(member capabilitydomain.CompositionMember) bool {
	if member.ActivationCondition != "" {
		return false
	}
	switch member.Criticality {
	case capabilitydomain.MembershipCriticalityMandatory, capabilitydomain.MembershipCriticalityImportant:
		return true
	default:
		return false
	}
}

// resolveTenantBaselineScope returns the tenant's own unscoped
// CapabilityScope (every dimension empty except TenantID) -- the correct
// scope for a platform-wide product entitlement, which is not restricted to
// one legal entity, market or channel. It creates one if the tenant does
// not have one yet.
func (s CompositionExpansionService) resolveTenantBaselineScope(ctx context.Context, tenantID string) (string, error) {
	scopes, err := s.Grants.ListCapabilityScopes(ctx, tenantID)
	if err != nil {
		return "", err
	}
	for _, scope := range scopes {
		if isTenantOnlyScope(scope) {
			return scope.ScopeID, nil
		}
	}
	scope := capabilitydomain.CapabilityScope{ScopeID: domain.NewUUIDv7(), TenantID: tenantID}
	if err := s.Grants.CreateCapabilityScope(ctx, scope); err != nil {
		return "", err
	}
	return scope.ScopeID, nil
}

func isTenantOnlyScope(s capabilitydomain.CapabilityScope) bool {
	return s.LegalEntityID == "" && s.OrganisationID == "" && s.BusinessUnitID == "" &&
		s.DigitalEstateID == "" && s.DigitalPropertyID == "" && s.ChannelID == "" &&
		s.MarketID == "" && s.Jurisdiction == "" && s.CurrencyCode == "" &&
		s.CustomerSegmentID == "" && s.CatalogueID == "" && s.OperatingRegionID == "" &&
		s.GeographicRegionID == "" && s.DeploymentRegion == "" && s.Environment == "" &&
		s.IsolationProfileID == "" && len(s.IncludeCountries) == 0 && len(s.ExcludeCountries) == 0
}
