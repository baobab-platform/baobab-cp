package domain

import (
	"errors"
	"regexp"
)

var entitlementProjectionIDPattern = regexp.MustCompile(`^entproj_[a-z0-9]+$`)

// ValidEntitlementProjectionID reports whether v is a Control Plane-minted
// EntitlementProjection identifier (contracts/product/v1/domain.schema.json
// #/$defs/entitlementProjectionId).
func ValidEntitlementProjectionID(v string) bool {
	return len(v) >= 10 && len(v) <= 63 && entitlementProjectionIDPattern.MatchString(v)
}

// EntitlementProjectionStatus tracks whether one composition member's
// expansion into a CapabilityGrant has been materialized yet (mirrors
// baobab-platform/shared's contracts/product/v1/domain.schema.json
// #/$defs/entitlementProjectionStatus).
type EntitlementProjectionStatus string

const (
	EntitlementProjectionStatusPending      EntitlementProjectionStatus = "PENDING"
	EntitlementProjectionStatusMaterialized EntitlementProjectionStatus = "MATERIALIZED"
	EntitlementProjectionStatusFailed       EntitlementProjectionStatus = "FAILED"
)

func (s EntitlementProjectionStatus) Valid() bool {
	switch s {
	case EntitlementProjectionStatusPending, EntitlementProjectionStatusMaterialized, EntitlementProjectionStatusFailed:
		return true
	default:
		return false
	}
}

// EntitlementProjection is one row of the ProductSubscription ->
// CapabilityComposition -> CapabilityGrant expansion: it records, per
// composition member capability_key, whether that member has been turned
// into a real CapabilityGrant yet (Technical Specification §91). It is the
// audit trail CompositionExpansionService writes as it works, not a runtime
// authorization decision itself -- resolution still consults
// CapabilityGrant, never EntitlementProjection, directly.
type EntitlementProjection struct {
	ID             string                      `json:"entitlement_projection_id,omitempty"`
	SubscriptionID string                      `json:"subscription_id"`
	TenantID       string                      `json:"tenant_id"`
	CapabilityKey  string                      `json:"capability_key"`
	Status         EntitlementProjectionStatus `json:"status"`
	GrantID        string                      `json:"grant_id,omitempty"`
	FailureReason  string                      `json:"failure_reason,omitempty"`
}

func (p EntitlementProjection) Validate() error {
	if p.SubscriptionID == "" {
		return errors.New("subscription_id is required")
	}
	if p.TenantID == "" {
		return errors.New("tenant_id is required")
	}
	if p.CapabilityKey == "" {
		return errors.New("capability_key is required")
	}
	if !p.Status.Valid() {
		return errors.New("status must be one of PENDING, MATERIALIZED, FAILED")
	}
	if p.Status == EntitlementProjectionStatusMaterialized && p.GrantID == "" {
		return errors.New("grant_id is required once status is MATERIALIZED")
	}
	if p.Status == EntitlementProjectionStatusFailed && p.FailureReason == "" {
		return errors.New("failure_reason is required once status is FAILED")
	}
	return nil
}
