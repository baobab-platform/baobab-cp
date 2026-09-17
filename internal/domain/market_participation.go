// Target path: internal/domain/market_participation.go
//
// Governance/lifecycle helpers for ADR-BCP-011 MarketParticipation.
//
// IMPORTANT ARCHITECTURAL DECISION:
// MarketAssignment in market.go IS the existing implementation of the
// MarketParticipation aggregate. This file intentionally does not define a
// second MarketParticipation struct. It adds vocabulary and behaviour used
// by the fields merged into MarketAssignment via integration/market.go.patch.txt.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// MarketParticipationStatus is lifecycle state, not an operational role.
// Operational permissions remain in MarketAssignment.Capabilities.
type MarketParticipationStatus string

const (
	MarketParticipationPending   MarketParticipationStatus = "PENDING"
	MarketParticipationActive    MarketParticipationStatus = "ACTIVE"
	MarketParticipationSuspended MarketParticipationStatus = "SUSPENDED"
	MarketParticipationRetired   MarketParticipationStatus = "RETIRED"
)

func (s MarketParticipationStatus) Valid() bool {
	switch s {
	case MarketParticipationPending, MarketParticipationActive,
		MarketParticipationSuspended, MarketParticipationRetired:
		return true
	default:
		return false
	}
}

// MarketParticipationSource records why/how the desired participation exists.
// This is governance provenance; it must not be inferred from capabilities.
type MarketParticipationSource string

const (
	MarketParticipationSourceProvisioning MarketParticipationSource = "PROVISIONING"
	MarketParticipationSourcePolicy       MarketParticipationSource = "POLICY"
	MarketParticipationSourceAdmin        MarketParticipationSource = "ADMIN"
	MarketParticipationSourceMigration    MarketParticipationSource = "MIGRATION"
)

func (s MarketParticipationSource) Valid() bool {
	switch s {
	case MarketParticipationSourceProvisioning, MarketParticipationSourcePolicy,
		MarketParticipationSourceAdmin, MarketParticipationSourceMigration:
		return true
	default:
		return false
	}
}

// ValidateMarketParticipationGovernance validates the ZB-02/P7 governance
// fields added to MarketAssignment. Structural/capability/effective-period
// validation remains MarketAssignment.Validate()'s responsibility.
func ValidateMarketParticipationGovernance(a MarketAssignment) error {
	if !a.Status.Valid() {
		return fmt.Errorf("invalid market participation status %q", a.Status)
	}
	if !a.Source.Valid() {
		return fmt.Errorf("invalid market participation source %q", a.Source)
	}
	if strings.TrimSpace(a.PolicyVersion) == "" {
		return errors.New("market participation policy_version is required")
	}
	return nil
}

// IsEffectiveAt answers the temporal part of participation eligibility.
// EffectiveTo is exclusive: [effective_from, effective_to).
func (a MarketAssignment) IsEffectiveAt(at time.Time) bool {
	if at.Before(a.EffectiveFrom) {
		return false
	}
	return a.EffectiveTo == nil || at.Before(*a.EffectiveTo)
}

// IsOperationalAt deliberately requires BOTH lifecycle ACTIVE and an
// effective period. A PENDING/SUSPENDED/RETIRED assignment never authorizes
// runtime operations merely because its dates cover `at`.
func (a MarketAssignment) IsOperationalAt(at time.Time) bool {
	return a.Status == MarketParticipationActive && a.IsEffectiveAt(at)
}

func (a MarketAssignment) HasParticipationCapability(required MarketParticipationCapability) bool {
	for _, capability := range a.Capabilities {
		if capability == required {
			return true
		}
	}
	return false
}

// CanTransitionMarketParticipation centralizes lifecycle transitions so
// handlers/services do not each invent their own rules.
//
// RETIRED is terminal. Re-creation, if ever required, must be represented by
// a new effective-period record rather than resurrecting historical state.
func CanTransitionMarketParticipation(from, to MarketParticipationStatus) bool {
	if from == to {
		return true // idempotent reconciliation
	}
	switch from {
	case MarketParticipationPending:
		return to == MarketParticipationActive ||
			to == MarketParticipationSuspended ||
			to == MarketParticipationRetired
	case MarketParticipationActive:
		return to == MarketParticipationSuspended ||
			to == MarketParticipationRetired
	case MarketParticipationSuspended:
		return to == MarketParticipationActive ||
			to == MarketParticipationRetired
	case MarketParticipationRetired:
		return false
	default:
		return false
	}
}
