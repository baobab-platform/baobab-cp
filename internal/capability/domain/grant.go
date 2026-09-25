package domain

import (
	"errors"
	"strings"
	"time"
)

// GrantSource is the provenance of a CapabilityGrant (ADR-BCP-003 §10,
// mirrored in baobab-platform/shared's contracts/capability/v1/domain.schema.json
// #/$defs/capabilityGrantSource). Multiple independent grants from
// different sources may coexist for the same tenant/capability/scope;
// revoking one source's grant never revokes another source's grant for the
// same capability (§35).
type GrantSource string

const (
	GrantSourcePlatformBaseline    GrantSource = "PLATFORM_BASELINE"
	GrantSourceProductSubscription GrantSource = "PRODUCT_SUBSCRIPTION"
	GrantSourceContract            GrantSource = "CONTRACT"
	GrantSourceTrial               GrantSource = "TRIAL"
	GrantSourceManualApproval      GrantSource = "MANUAL_APPROVAL"
	GrantSourceInternalPolicy      GrantSource = "INTERNAL_POLICY"
	GrantSourceMigration           GrantSource = "MIGRATION"
)

func (s GrantSource) Valid() bool {
	switch s {
	case GrantSourcePlatformBaseline, GrantSourceProductSubscription, GrantSourceContract,
		GrantSourceTrial, GrantSourceManualApproval, GrantSourceInternalPolicy, GrantSourceMigration:
		return true
	default:
		return false
	}
}

// GrantStatus is the lifecycle of a CapabilityGrant (ADR-BCP-003 §11).
type GrantStatus string

const (
	GrantStatusPending   GrantStatus = "PENDING"
	GrantStatusActive    GrantStatus = "ACTIVE"
	GrantStatusSuspended GrantStatus = "SUSPENDED"
	GrantStatusRevoked   GrantStatus = "REVOKED"
	GrantStatusExpired   GrantStatus = "EXPIRED"
)

func (s GrantStatus) Valid() bool {
	switch s {
	case GrantStatusPending, GrantStatusActive, GrantStatusSuspended, GrantStatusRevoked, GrantStatusExpired:
		return true
	default:
		return false
	}
}

// CapabilityGrant answers exactly one question: may this tenant, in this
// scope, consume this capability? It never determines which provider or
// engine instance serves the request -- that is CapabilityBinding's job
// (ADR-BCP-003 §9). Mirrors baobab-platform/shared's
// contracts/capability/v1/grant.schema.json field-for-field.
type CapabilityGrant struct {
	ID               string         `json:"id,omitempty"`
	TenantID         string         `json:"tenant_id"`
	CapabilityID     string         `json:"capability_id,omitempty"`
	CapabilityKey    string         `json:"capability_key,omitempty"`
	ScopeID          string         `json:"scope_id"`
	Source           GrantSource    `json:"source"`
	SourceReference  string         `json:"source_reference,omitempty"`
	Status           GrantStatus    `json:"status"`
	EffectiveFrom    time.Time      `json:"effective_from"`
	EffectiveTo      *time.Time     `json:"effective_to,omitempty"`
	Constraints      map[string]any `json:"constraints,omitempty"`
	GrantedBy        string         `json:"granted_by,omitempty"`
	RevokedAt        *time.Time     `json:"revoked_at,omitempty"`
	RevokedBy        string         `json:"revoked_by,omitempty"`
	RevocationReason string         `json:"revocation_reason,omitempty"`
	Version          int64          `json:"version,omitempty"`
}

func (g CapabilityGrant) Validate() error {
	if strings.TrimSpace(g.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	if g.CapabilityID == "" && g.CapabilityKey == "" {
		return errors.New("capability identity is required")
	}
	if strings.TrimSpace(g.ScopeID) == "" {
		return errors.New("scope_id is required")
	}
	if !g.Source.Valid() {
		return errors.New("source must be one of PLATFORM_BASELINE, PRODUCT_SUBSCRIPTION, CONTRACT, TRIAL, MANUAL_APPROVAL, INTERNAL_POLICY, MIGRATION")
	}
	// Provenance SHALL be retained back to its source (§10); a grant
	// whose source is not the platform's own baseline entitlement must
	// name the record it came from (e.g. a subscription_id), matching
	// baobab-platform/shared's grant.schema.json conditional requirement.
	if g.Source != GrantSourcePlatformBaseline && strings.TrimSpace(g.SourceReference) == "" {
		return errors.New("source_reference is required unless source is PLATFORM_BASELINE")
	}
	if !g.Status.Valid() {
		return errors.New("status must be one of PENDING, ACTIVE, SUSPENDED, REVOKED, EXPIRED")
	}
	if g.EffectiveTo != nil && !g.EffectiveTo.After(g.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}
	return nil
}

// IsEffective reports whether the grant satisfies resolution at the given
// instant: status=ACTIVE and the instant falls within
// [effective_from, effective_to) (ADR-BCP-003 §11's effectiveness formula).
// Only effective grants SHALL satisfy resolution -- a PENDING, SUSPENDED,
// REVOKED or EXPIRED grant never does, regardless of its temporal window.
func (g CapabilityGrant) IsEffective(at time.Time) bool {
	if g.Status != GrantStatusActive {
		return false
	}
	if at.Before(g.EffectiveFrom) {
		return false
	}
	if g.EffectiveTo != nil && !at.Before(*g.EffectiveTo) {
		return false
	}
	return true
}
