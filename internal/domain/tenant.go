package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var regionID = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]+$`)

type NotFoundError string

func (e NotFoundError) Error() string { return string(e) }

// RegisterTenant is the register-tenant command. tenant_id is intentionally
// absent from the JSON wire shape: per contracts/control-plane/v1/
// tenant-registration.schema.json, tenant_id is minted by the Control Plane
// (see NewTenantID), never supplied by the caller. TenantID is populated by
// the HTTP handler after decoding and before Validate/persistence.
// Registration bases (ADR-BCP-017 sections 22-24): a tenant is registered
// for an AUTHORISED TenantOnboardingRequest, or, only for a tenant that
// predates the admission workflow, as a bootstrap tenant.
const (
	RegistrationOnboarding = "ONBOARDING"
	RegistrationBootstrap  = "BOOTSTRAP"
)

type RegisterTenant struct {
	// TenantOnboardingRequestID names the AUTHORISED request an ONBOARDING
	// registration fulfils (Shared tenant-registration.schema.json).
	TenantOnboardingRequestID string `json:"tenant_onboarding_request_id,omitempty"`
	// Basis, BootstrapReason and BootstrapEvidenceReference are set by the
	// route that accepted the command, never decoded from it.
	Basis                      string            `json:"-"`
	BootstrapReason            string            `json:"-"`
	BootstrapEvidenceReference string            `json:"-"`
	LegalEntityID              string            `json:"legal_entity_id"`
	TenantID                   string            `json:"-"`
	DisplayName                string            `json:"display_name"`
	IsolationStrategy          string            `json:"isolation_strategy"`
	ResidencyRegion            string            `json:"residency_region"`
	RequestedProducts          []string          `json:"requested_products,omitempty"`
	Metadata                   map[string]string `json:"metadata,omitempty"`
}

type Tenant struct {
	TenantID          string            `json:"tenant_id"`
	LegalEntityID     string            `json:"legal_entity_id"`
	DisplayName       string            `json:"display_name"`
	IsolationStrategy string            `json:"isolation_strategy"`
	ResidencyRegion   string            `json:"residency_region"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	DesiredState      string            `json:"desired_state"`
	ObservedState     string            `json:"observed_state"`
	Revision          int64             `json:"revision"`
}

type EntitlementQuery struct {
	TenantID  string
	ProductID string
}

func (q EntitlementQuery) Validate() error {
	if !ValidTenantID(q.TenantID) {
		return errors.New("tenant_id must be a canonical Control Plane tenant identifier")
	}
	if !ValidProductID(q.ProductID) {
		return errors.New("product_id must be a canonical product identifier")
	}
	return nil
}

type Entitlement struct {
	TenantID  string `json:"tenant_id"`
	ProductID string `json:"product_id"`
	Status    string `json:"status"`
	Tier      string `json:"tier,omitempty"`
}

type LifecycleAction struct {
	TenantID string `json:"tenant_id"`
	Action   string `json:"action"`
}

func (a LifecycleAction) Validate() error {
	if !ValidTenantID(a.TenantID) {
		return errors.New("tenant_id must be a canonical Control Plane tenant identifier")
	}
	switch a.Action {
	case "activate", "suspend", "decommission":
		return nil
	default:
		return errors.New("action must be one of activate, suspend, or decommission")
	}
}

type LifecycleStatus string

const (
	LifecycleProvisioning    LifecycleStatus = "provisioning"
	LifecycleActive          LifecycleStatus = "active"
	LifecycleSuspended       LifecycleStatus = "suspended"
	LifecycleDecommissioning LifecycleStatus = "decommissioning"
	LifecycleDecommissioned  LifecycleStatus = "decommissioned"
)

func (s LifecycleStatus) Valid() bool {
	switch s {
	case LifecycleProvisioning, LifecycleActive, LifecycleSuspended, LifecycleDecommissioning, LifecycleDecommissioned:
		return true
	default:
		return false
	}
}

func TransitionLifecycle(from, to LifecycleStatus) (LifecycleStatus, bool) {
	if !from.Valid() || !to.Valid() {
		return "", false
	}
	if from == LifecycleDecommissioned && to != LifecycleDecommissioned {
		return "", false
	}
	allowed := map[LifecycleStatus][]LifecycleStatus{
		LifecycleProvisioning:    {LifecycleActive, LifecycleSuspended},
		LifecycleActive:          {LifecycleSuspended, LifecycleDecommissioning},
		LifecycleSuspended:       {LifecycleActive, LifecycleDecommissioning},
		LifecycleDecommissioning: {LifecycleDecommissioned},
		LifecycleDecommissioned:  {LifecycleDecommissioned},
	}
	for _, next := range allowed[from] {
		if next == to {
			return to, true
		}
	}
	return "", false
}

func (c RegisterTenant) Validate() error {
	switch c.Basis {
	case RegistrationOnboarding:
		if _, err := ParseResourceID(TenantOnboardingRequestIDPrefix, c.TenantOnboardingRequestID); err != nil {
			return errors.New("tenant_onboarding_request_id must name an AUTHORISED tenant onboarding request")
		}
		if c.BootstrapReason != "" || c.BootstrapEvidenceReference != "" {
			return errors.New("an onboarding registration carries no bootstrap reason or evidence")
		}
	case RegistrationBootstrap:
		if c.TenantOnboardingRequestID != "" {
			return errors.New("a bootstrap registration names no onboarding request")
		}
		if n := len(strings.TrimSpace(c.BootstrapReason)); n < 20 || n > 1000 {
			return errors.New("bootstrap_reason must contain 20 to 1000 characters")
		}
		if n := len(strings.TrimSpace(c.BootstrapEvidenceReference)); n < 1 || n > 255 {
			return errors.New("evidence_reference must contain 1 to 255 characters")
		}
	default:
		return errors.New("a tenant is registered only for an onboarding request or as a bootstrap tenant")
	}
	if !ValidLegalEntityID(c.LegalEntityID) {
		return errors.New("legal_entity_id must be a canonical legal-entity identifier (e.g. THAMANI-GLOBAL) or an accepted legacy alias")
	}
	if !ValidTenantID(c.TenantID) {
		// TenantID is minted by NewTenantID before Validate runs, so this
		// guards a programming error, not caller input.
		return errors.New("tenant_id must be a canonical Control Plane tenant identifier")
	}
	if n := len(strings.TrimSpace(c.DisplayName)); n < 1 || n > 255 {
		return errors.New("display_name must contain 1 to 255 characters")
	}
	if c.IsolationStrategy != "schema_per_tenant" && c.IsolationStrategy != "row_level_security" {
		return errors.New("isolation_strategy is invalid")
	}
	if !regionID.MatchString(c.ResidencyRegion) {
		return errors.New("residency_region is invalid")
	}
	if len(c.Metadata) > 20 {
		return errors.New("metadata may contain at most 20 entries")
	}
	seen := map[string]struct{}{}
	for _, product := range c.RequestedProducts {
		if !ValidProductID(product) {
			return errors.New("requested_products contains an invalid identifier")
		}
		if _, ok := seen[product]; ok {
			return errors.New("requested_products must be unique")
		}
		seen[product] = struct{}{}
	}
	for _, value := range c.Metadata {
		if len(value) > 255 {
			return errors.New("metadata values may contain at most 255 characters")
		}
	}
	return nil
}

type Operation struct {
	OperationID string    `json:"operation_id"`
	TenantID    string    `json:"tenant_id"`
	State       string    `json:"state"`
	Revision    int       `json:"revision"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
