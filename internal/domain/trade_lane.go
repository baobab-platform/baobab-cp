// Target path: internal/domain/trade_lane.go
//
// TradeLane is the Control Plane implementation of the canonical
// `shared/contracts/trade-lane/v1` resource.
//
// Architectural boundary:
//   - Shared owns the cross-repository contract.
//   - baobab-cp owns lifecycle, validation and persistence.
//   - MarketAssignment remains baobab-cp's current implementation of
//     ADR-BCP-011 MarketParticipation.
//   - A TradeLane NEVER implies legal presence in either market.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type TradeLaneStatus string

const (
	TradeLaneActive    TradeLaneStatus = "ACTIVE"
	TradeLaneSuspended TradeLaneStatus = "SUSPENDED"
	TradeLaneRetired   TradeLaneStatus = "RETIRED"
)

func (s TradeLaneStatus) Valid() bool {
	switch s {
	case TradeLaneActive, TradeLaneSuspended, TradeLaneRetired:
		return true
	default:
		return false
	}
}

type TradeLaneDirection string

const (
	TradeLaneImport      TradeLaneDirection = "IMPORT"
	TradeLaneExport      TradeLaneDirection = "EXPORT"
	TradeLaneCrossMarket TradeLaneDirection = "CROSS_MARKET"
)

func (d TradeLaneDirection) Valid() bool {
	switch d {
	case TradeLaneImport, TradeLaneExport, TradeLaneCrossMarket:
		return true
	default:
		return false
	}
}

// This pattern intentionally mirrors Shared v1. Do not loosen it locally;
// changing an identifier contract is a Shared contract-versioning decision.
var tradeLaneIDPattern = regexp.MustCompile(`^tlane_[a-z0-9]+$`)

type TradeLane struct {
	ID                      string             `json:"trade_lane_id"`
	TenantID                string             `json:"tenant_id"`
	OriginMarketID          string             `json:"origin_market_id"`
	DestinationMarketID     string             `json:"destination_market_id"`
	Direction               TradeLaneDirection `json:"direction"`
	Status                  TradeLaneStatus    `json:"status"`
	PermittedCapabilityKeys []string           `json:"permitted_capability_keys,omitempty"`
	CreatedAt               time.Time          `json:"created_at,omitempty"`
	UpdatedAt               time.Time          `json:"updated_at,omitempty"`
}

func (l TradeLane) Validate() error {
	if !tradeLaneIDPattern.MatchString(l.ID) {
		return errors.New("trade_lane_id must match ^tlane_[a-z0-9]+$")
	}
	if strings.TrimSpace(l.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	if strings.TrimSpace(l.OriginMarketID) == "" {
		return errors.New("origin_market_id is required")
	}
	if strings.TrimSpace(l.DestinationMarketID) == "" {
		return errors.New("destination_market_id is required")
	}
	if l.OriginMarketID == l.DestinationMarketID {
		return errors.New("origin_market_id and destination_market_id must differ")
	}
	if !l.Direction.Valid() {
		return fmt.Errorf("invalid trade lane direction %q", l.Direction)
	}
	if !l.Status.Valid() {
		return fmt.Errorf("invalid trade lane status %q", l.Status)
	}

	// Duplicate lane capabilities are rejected to keep policy evaluation
	// deterministic and serialized representations canonical.
	seen := make(map[string]struct{}, len(l.PermittedCapabilityKeys))
	for _, capability := range l.PermittedCapabilityKeys {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			return errors.New("permitted capability key cannot be empty")
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("duplicate permitted capability key %q", capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}

func (l TradeLane) AllowsCapability(key string) bool {
	for _, permitted := range l.PermittedCapabilityKeys {
		if permitted == key {
			return true
		}
	}
	return false
}

func (l TradeLane) IsUsable() bool {
	return l.Status == TradeLaneActive
}
