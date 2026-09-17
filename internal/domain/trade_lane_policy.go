// Target path: internal/domain/trade_lane_policy.go
//
// This file contains cross-market policy, deliberately separate from the
// TradeLane value object's structural validation.
//
// The key rule is that the lane does not grant market participation.
// It may only become operational when effective MarketAssignments already
// authorize the tenant at the origin and destination.
package domain

import (
	"errors"
	"fmt"
	"time"
)

func ValidateTradeLaneParticipation(
	lane TradeLane,
	origin MarketAssignment,
	destination MarketAssignment,
	at time.Time,
) error {
	if err := lane.Validate(); err != nil {
		return err
	}

	// Tenant checks prevent a valid market assignment belonging to another
	// tenant from accidentally authorizing this tenant's route.
	if origin.TenantID != lane.TenantID || destination.TenantID != lane.TenantID {
		return errors.New("trade lane participation belongs to another tenant")
	}
	if origin.MarketID != lane.OriginMarketID {
		return fmt.Errorf("origin participation market %q does not match lane origin %q",
			origin.MarketID, lane.OriginMarketID)
	}
	if destination.MarketID != lane.DestinationMarketID {
		return fmt.Errorf("destination participation market %q does not match lane destination %q",
			destination.MarketID, lane.DestinationMarketID)
	}
	if !marketAssignmentEffective(origin, at) {
		return errors.New("origin market participation is not effective")
	}
	if !marketAssignmentEffective(destination, at) {
		return errors.New("destination market participation is not effective")
	}

	// Direction is interpreted as the minimum participation authorization
	// needed to activate the lane. More specific business capabilities
	// (procurement.*, logistics.*, etc.) remain governed separately.
	switch lane.Direction {
	case TradeLaneExport:
		if !hasParticipationCapability(origin, MarketParticipationExporting) {
			return errors.New("origin market lacks EXPORTING participation")
		}
	case TradeLaneImport:
		if !hasParticipationCapability(destination, MarketParticipationImporting) {
			return errors.New("destination market lacks IMPORTING participation")
		}
	case TradeLaneCrossMarket:
		if !hasParticipationCapability(origin, MarketParticipationExporting) {
			return errors.New("origin market lacks EXPORTING participation")
		}
		if !hasParticipationCapability(destination, MarketParticipationImporting) {
			return errors.New("destination market lacks IMPORTING participation")
		}
	default:
		// Normally caught by lane.Validate(). Retaining the default makes this
		// policy safe if validation changes in the future.
		return fmt.Errorf("unsupported trade lane direction %q", lane.Direction)
	}
	return nil
}

func hasParticipationCapability(
	assignment MarketAssignment,
	required MarketParticipationCapability,
) bool {
	for _, capability := range assignment.Capabilities {
		if capability == required {
			return true
		}
	}
	return false
}

func marketAssignmentEffective(assignment MarketAssignment, at time.Time) bool {
	if at.Before(assignment.EffectiveFrom) {
		return false
	}
	// EffectiveTo is an exclusive boundary, matching the repository's
	// existing [effective_from, effective_to) temporal model.
	return assignment.EffectiveTo == nil || at.Before(*assignment.EffectiveTo)
}
