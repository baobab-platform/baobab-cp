// Package domain holds the provisioning module's aggregate model:
// TenantProvisioning (Technical Specification §21-23, ADR-BCP-010 §41).
// It tracks tenant onboarding orchestration -- it does not replace Tenant
// itself (internal/domain.Tenant), which remains the durable resource
// record. This package owns TenantProvisioning; other modules reference it
// through explicit imports rather than duplicating or reaching into this
// module's persistence directly, matching internal/capability/domain and
// internal/product/domain's existing module layout.
//
// Scope note -- this is Programme Gate P7's "basics" cut, not the full
// Tenant Provisioning Engine: the Technical Specification's own §22 state
// machine has thirteen states (DRAFT/VALIDATING/PLANNED/REGISTERING/
// CONFIGURING_CONTEXT/PROVISIONING_ENTITLEMENTS/PROVISIONING_PROVIDERS/
// VALIDATING_SECURITY/VERIFYING_READINESS/READY/ACTIVE, plus BLOCKED/
// REMEDIATING/FAILED/CANCELLED/DEPROVISIONED) with each phase driving real
// business logic (context resolution, entitlement materialisation,
// provider binding, security validation). This package instead implements
// Gate ZB-02's own simplified five-stage exit lifecycle (PLAN -> APPLY ->
// RECONCILE -> READY -> ACTIVE, plus FAILED/CANCELLED terminals) as a real,
// persisted, idempotent state machine with retry and failure handling --
// the orchestration *mechanism* -- without wiring the per-phase business
// logic (creating CapabilityGrants, resolving Context, binding providers)
// that later gates (P8 Provider Reconciliation onward) are expected to
// plug into it. A caller drives phase transitions explicitly; this package
// does not decide *when* a tenant is ready, only whether a requested
// transition is legal and records it durably.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ProvisioningStatus is TenantProvisioning's lifecycle state (Gate ZB-02's
// PLAN -> APPLY -> RECONCILE -> READY -> ACTIVE exit lifecycle).
type ProvisioningStatus string

const (
	ProvisioningStatusPlan      ProvisioningStatus = "PLAN"
	ProvisioningStatusApply     ProvisioningStatus = "APPLY"
	ProvisioningStatusReconcile ProvisioningStatus = "RECONCILE"
	ProvisioningStatusReady     ProvisioningStatus = "READY"
	ProvisioningStatusActive    ProvisioningStatus = "ACTIVE"
	// ProvisioningStatusFailed and ProvisioningStatusCancelled are terminal
	// unless explicitly retried (Retry transitions FAILED back to APPLY --
	// see TransitionProvisioning).
	ProvisioningStatusFailed    ProvisioningStatus = "FAILED"
	ProvisioningStatusCancelled ProvisioningStatus = "CANCELLED"
)

func (s ProvisioningStatus) Valid() bool {
	switch s {
	case ProvisioningStatusPlan, ProvisioningStatusApply, ProvisioningStatusReconcile,
		ProvisioningStatusReady, ProvisioningStatusActive, ProvisioningStatusFailed, ProvisioningStatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s is a terminal state: ACTIVE (successful
// completion) or CANCELLED. FAILED is deliberately not terminal here --
// unlike CANCELLED, a failed provisioning attempt SHALL remain retryable
// (Programme Gate P7: "Retry" is a required capability, not a dead end).
func (s ProvisioningStatus) IsTerminal() bool {
	return s == ProvisioningStatusActive || s == ProvisioningStatusCancelled
}

// provisioningTransitions is the only edge set TransitionProvisioning will
// accept, mirroring domain.TransitionLifecycle's table-driven pattern.
var provisioningTransitions = map[ProvisioningStatus][]ProvisioningStatus{
	ProvisioningStatusPlan:      {ProvisioningStatusApply, ProvisioningStatusCancelled, ProvisioningStatusFailed},
	ProvisioningStatusApply:     {ProvisioningStatusReconcile, ProvisioningStatusCancelled, ProvisioningStatusFailed},
	ProvisioningStatusReconcile: {ProvisioningStatusReady, ProvisioningStatusApply, ProvisioningStatusCancelled, ProvisioningStatusFailed},
	ProvisioningStatusReady:     {ProvisioningStatusActive, ProvisioningStatusCancelled, ProvisioningStatusFailed},
	// A FAILED attempt may only be retried back into APPLY (re-running the
	// apply phase against the same plan) or explicitly cancelled. It may
	// never be retried directly into RECONCILE/READY/ACTIVE -- a failure
	// invalidates any progress claimed past APPLY, per the fail-closed
	// posture this codebase holds everywhere else (ADR-BCP-003 §6).
	ProvisioningStatusFailed: {ProvisioningStatusApply, ProvisioningStatusCancelled},
	// ACTIVE and CANCELLED are terminal: no outbound edges.
}

// TransitionProvisioning reports whether the requested from -> to edge is
// legal, returning (to, true) on success. It never mutates a
// TenantProvisioning itself -- callers apply the returned status
// themselves, matching domain.TransitionLifecycle's calling convention.
func TransitionProvisioning(from, to ProvisioningStatus) (ProvisioningStatus, bool) {
	if !from.Valid() || !to.Valid() {
		return "", false
	}
	for _, next := range provisioningTransitions[from] {
		if next == to {
			return to, true
		}
	}
	return "", false
}

// TenantProvisioning is the tenant onboarding process aggregate (Technical
// Specification §21). It tracks orchestration; it does not replace Tenant.
type TenantProvisioning struct {
	ID                   string             `json:"id,omitempty"`
	TenantID             string             `json:"tenant_id"`
	IdempotencyKey       string             `json:"idempotency_key"`
	RequestHash          string             `json:"request_hash"`
	Status               ProvisioningStatus `json:"status"`
	DesiredStateVersion  int64              `json:"desired_state_version"`
	ObservedStateVersion int64              `json:"observed_state_version"`
	ProductRequests      []string           `json:"product_requests,omitempty"`
	MarketRequests       []string           `json:"market_requests,omitempty"`
	IsolationRequirement string             `json:"isolation_requirement,omitempty"`
	ResidencyRequirement string             `json:"residency_requirement,omitempty"`
	BlockingReasons      []string           `json:"blocking_reasons,omitempty"`
	AttemptCount         int                `json:"attempt_count"`
	LastError            string             `json:"last_error,omitempty"`
	StartedAt            time.Time          `json:"started_at,omitempty"`
	CompletedAt          *time.Time         `json:"completed_at,omitempty"`
	Version              int64              `json:"version,omitempty"`
	Metadata             map[string]string  `json:"metadata,omitempty"`
}

func (p TenantProvisioning) Validate() error {
	if strings.TrimSpace(p.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return errors.New("idempotency_key is required")
	}
	if strings.TrimSpace(p.RequestHash) == "" {
		return errors.New("request_hash is required")
	}
	if !p.Status.Valid() {
		return errors.New("status must be one of PLAN, APPLY, RECONCILE, READY, ACTIVE, FAILED, CANCELLED")
	}
	if p.StartedAt.IsZero() {
		return errors.New("started_at is required")
	}
	if p.Status == ProvisioningStatusActive && p.CompletedAt == nil {
		return errors.New("completed_at is required once status is ACTIVE")
	}
	if p.Status == ProvisioningStatusFailed && strings.TrimSpace(p.LastError) == "" {
		return errors.New("last_error is required once status is FAILED")
	}
	return nil
}

// Advance validates and applies from p.Status -> to, returning a new
// TenantProvisioning value (this type is used by value throughout this
// codebase's other aggregates, e.g. capabilitydomain.CapabilityGrant) with
// Status updated and Version incremented. It does not itself persist
// anything -- callers pass the result to their repository's optimistic-
// locked update, mirroring RevokeGrant's expectedVersion pattern.
func (p TenantProvisioning) Advance(to ProvisioningStatus, reason string) (TenantProvisioning, error) {
	next, ok := TransitionProvisioning(p.Status, to)
	if !ok {
		return TenantProvisioning{}, fmt.Errorf("illegal provisioning transition %s -> %s", p.Status, to)
	}
	p.Status = next
	p.Version++
	switch next {
	case ProvisioningStatusFailed:
		p.AttemptCount++
		p.LastError = reason
	case ProvisioningStatusApply:
		// Retrying FAILED -> APPLY clears the previous failure reason but
		// keeps AttemptCount, so total attempts remain auditable.
		p.LastError = ""
	case ProvisioningStatusCancelled:
		if reason != "" {
			p.BlockingReasons = append(p.BlockingReasons, reason)
		}
	case ProvisioningStatusActive:
		now := time.Now().UTC()
		p.CompletedAt = &now
	}
	return p, nil
}
