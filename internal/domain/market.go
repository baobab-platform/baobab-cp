package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var marketCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Market models market.market (migration 000006) -- a market a tenant may
// participate in, per ADR-BCP-004 §4/§55. The table has existed since early
// in this codebase's history; this is the first Go code to read or write it.
type Market struct {
	ID       string `json:"id,omitempty"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
	Region   string `json:"region"`
	IsActive bool   `json:"is_active"`
}

func (m Market) Validate() error {
	if strings.TrimSpace(m.Code) == "" {
		return errors.New("code is required")
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("name is required")
	}
	if !marketCurrencyPattern.MatchString(m.Currency) {
		return errors.New("currency must be a 3-letter uppercase ISO code")
	}
	if strings.TrimSpace(m.Region) == "" {
		return errors.New("region is required")
	}
	return nil
}

// MarketParticipationCapability is what an organisational context is
// authorised to do in a market -- deliberately independent of the market
// itself (ADR-BCP-011 §6: "a market SHALL NOT be assigned a permanent
// architectural role such as SOURCE MARKET/EXPORT MARKET"; the same market
// may independently hold any subset of these). Mirrors ADR-BCP-011 §53's
// proposed canonical MarketParticipationCapability enum.
type MarketParticipationCapability string

const (
	MarketParticipationLegalPresence MarketParticipationCapability = "LEGAL_PRESENCE"
	MarketParticipationSourcing      MarketParticipationCapability = "SOURCING"
	MarketParticipationProcurement   MarketParticipationCapability = "PROCUREMENT"
	MarketParticipationSelling       MarketParticipationCapability = "SELLING"
	MarketParticipationImporting     MarketParticipationCapability = "IMPORTING"
	MarketParticipationExporting     MarketParticipationCapability = "EXPORTING"
	MarketParticipationWarehousing   MarketParticipationCapability = "WAREHOUSING"
	MarketParticipationDistribution  MarketParticipationCapability = "DISTRIBUTION"
	MarketParticipationFulfilment    MarketParticipationCapability = "FULFILMENT"
	MarketParticipationProcessing    MarketParticipationCapability = "PROCESSING"
	MarketParticipationTransit       MarketParticipationCapability = "TRANSIT"
)

func (c MarketParticipationCapability) Valid() bool {
	switch c {
	case MarketParticipationLegalPresence, MarketParticipationSourcing, MarketParticipationProcurement,
		MarketParticipationSelling, MarketParticipationImporting, MarketParticipationExporting,
		MarketParticipationWarehousing, MarketParticipationDistribution, MarketParticipationFulfilment,
		MarketParticipationProcessing, MarketParticipationTransit:
		return true
	default:
		return false
	}
}

// MarketAssignment models market.market_assignment (migration 000006,
// extended by migration 000024 with a generated valid_period column and a
// market_assignment_active_excl exclusion constraint, and by migration
// 000037 with LegalEntityID and Capabilities). That exclusion constraint
// SHALL reject two assignments for the same (tenant_id, market_id) pair
// with overlapping [effective_from, effective_to) periods -- a tenant MAY
// have concurrent assignments to different markets, just not two
// overlapping assignments to the same one.
//
// This is ADR-BCP-011 §5's canonical MarketParticipation concept
// ("Tenant + Legal Entity where applicable + Market + Participation
// Capability + Effective Period"), kept under its existing Go/table name
// per Gate P0's classification (docs/reconciliation/phase-0-architecture-
// inventory-and-lock.md) rather than renamed, since #61/#74-class renames
// are reserved for cases with an actual naming conflict. §5's `status`,
// `source` and `policy_version` fields are deliberately not modelled here
// yet -- those are Programme Gate P7 (Tenant Provisioning Engine)
// governance-lifecycle concerns, not the capability-flag gap this remodel
// closes (Gate P0: "market_assignment proves a tenant may participate in a
// market at all, but cannot yet represent what it may do there").
type MarketAssignment struct {
	ID            string                          `json:"id,omitempty"`
	TenantID      string                          `json:"tenant_id"`
	LegalEntityID string                          `json:"legal_entity_id,omitempty"`
	MarketID      string                          `json:"market_id"`
	Capabilities  []MarketParticipationCapability `json:"capabilities"`
	EffectiveFrom time.Time                       `json:"effective_from"`
	EffectiveTo   *time.Time                      `json:"effective_to,omitempty"`
}

func (a MarketAssignment) Validate() error {
	if strings.TrimSpace(a.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	if strings.TrimSpace(a.MarketID) == "" {
		return errors.New("market_id is required")
	}
	if len(a.Capabilities) == 0 {
		return errors.New("at least one participation capability is required")
	}
	seen := make(map[MarketParticipationCapability]bool, len(a.Capabilities))
	for _, c := range a.Capabilities {
		if !c.Valid() {
			return fmt.Errorf("invalid participation capability %q", c)
		}
		if seen[c] {
			return fmt.Errorf("duplicate participation capability %q", c)
		}
		seen[c] = true
	}
	if a.EffectiveFrom.IsZero() {
		return errors.New("effective_from is required")
	}
	if a.EffectiveTo != nil && !a.EffectiveTo.After(a.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}
	return nil
}
