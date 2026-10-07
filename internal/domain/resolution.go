package domain

import (
	"errors"
	"strings"
	"time"
)

type TrustLevel string

const (
	TrustUntrusted  TrustLevel = "UNTRUSTED"
	TrustVerified   TrustLevel = "VERIFIED"
	TrustAuthorised TrustLevel = "AUTHORISED"
	TrustSystem     TrustLevel = "SYSTEM"
)

type ContextSource struct {
	Source     string     `json:"source"`
	TrustLevel TrustLevel `json:"trust_level"`
	Evidence   string     `json:"evidence,omitempty"`
}

// Context is an immutable, server-resolved operation scope. Distinct IDs are
// deliberately separate: none is an alias for another.
type Context struct {
	ID                 string    `json:"id,omitempty"`
	PrincipalID        string    `json:"principal_id"`
	TenantID           string    `json:"tenant_id"`
	LegalEntityID      string    `json:"legal_entity_id,omitempty"`
	OrganisationID     string    `json:"organisation_id,omitempty"`
	BusinessUnitID     string    `json:"business_unit_id,omitempty"`
	DigitalEstateID    string    `json:"digital_estate_id,omitempty"`
	DigitalPropertyID  string    `json:"digital_property_id,omitempty"`
	ChannelID          string    `json:"channel_id,omitempty"`
	MarketID           string    `json:"market_id,omitempty"`
	Jurisdiction       string    `json:"jurisdiction,omitempty"`
	CountryCode        string    `json:"country_code,omitempty"`
	CurrencyCode       string    `json:"currency_code,omitempty"`
	Locale             string    `json:"locale,omitempty"`
	DeploymentRegion   string    `json:"deployment_region,omitempty"`
	Environment        string    `json:"environment,omitempty"`
	IsolationProfileID string    `json:"isolation_profile_id,omitempty"`
	CorrelationID      string    `json:"correlation_id"`
	ResolvedAt         time.Time `json:"resolved_at"`
	// ExpiresAt is optional (ADR-BCP-004 §72, "Context Lifetime": "Context
	// MAY have bounded lifetime"). Nil means no bound is imposed; a
	// long-running caller SHALL NOT assume an unbounded context remains
	// valid forever regardless, but this package does not itself impose a
	// default TTL.
	ExpiresAt  *time.Time               `json:"expires_at,omitempty"`
	Provenance map[string]ContextSource `json:"provenance"`
	// AuthorityPurpose says what the context is authority for (Shared
	// control-plane/v1 ContextAuthorityPurpose). The zero value is RUNTIME, the
	// only purpose every pre-existing context and every context resolved through
	// POST /v1/platform-context/resolve has. Like the rest of a context it is
	// fixed at creation (ADR-BCP-004 section 71).
	AuthorityPurpose ContextAuthorityPurpose `json:"authority_purpose,omitempty"`
	// ProvisioningAuthority is the approved plan a TENANT_PROVISIONING context
	// is bound to, and is present exactly for that purpose.
	ProvisioningAuthority *ProvisioningAuthority `json:"provisioning_authority,omitempty"`
}

// ContextAuthorityPurpose is control-plane/v1 ContextAuthorityPurpose.
type ContextAuthorityPurpose string

const (
	// ContextPurposeRuntime acts for an ACTIVE tenant.
	ContextPurposeRuntime ContextAuthorityPurpose = "RUNTIME"
	// ContextPurposeTenantProvisioning is the pre-activation provisioning
	// authority: created only by the Control Plane's own provisioning execution,
	// for the dedicated provisioner workload, bound to one approved plan, and never
	// RUNTIME authority.
	ContextPurposeTenantProvisioning ContextAuthorityPurpose = "TENANT_PROVISIONING"
)

// MaxProvisioningContextLifetime bounds a TENANT_PROVISIONING context
// (control-plane/v1: at most 15 minutes).
const MaxProvisioningContextLifetime = 15 * time.Minute

// ProvisioningAuthority is control-plane/v1 ProvisioningContextAuthority: the
// approved plan tuple (ADR-BCP-021 section 24: an approval binds plan id, version
// and digest together).
type ProvisioningAuthority struct {
	TenantProvisioningID string `json:"tenant_provisioning_id"`
	PlanID               string `json:"plan_id"`
	PlanVersion          int    `json:"plan_version"`
	PlanDigest           string `json:"plan_digest"`
}

// Purpose returns the context's purpose, RUNTIME when none was recorded.
func (c Context) Purpose() ContextAuthorityPurpose {
	if c.AuthorityPurpose == "" {
		return ContextPurposeRuntime
	}
	return c.AuthorityPurpose
}

// IsRuntime reports whether the context may be redeemed as ordinary runtime
// authority. A provisioning context never may.
func (c Context) IsRuntime() bool { return c.Purpose() == ContextPurposeRuntime }

func (a ProvisioningAuthority) validate() error {
	switch {
	case !strings.HasPrefix(a.TenantProvisioningID, "tp_") || len(a.TenantProvisioningID) < 6:
		return errors.New("provisioning authority needs a tenant provisioning id")
	case !strings.HasPrefix(a.PlanID, "plan_") || len(a.PlanID) < 8:
		return errors.New("provisioning authority needs a plan id")
	case a.PlanVersion < 1:
		return errors.New("provisioning authority needs a plan version")
	case !strings.HasPrefix(a.PlanDigest, "sha256:") || len(a.PlanDigest) != len("sha256:")+64:
		return errors.New("provisioning authority needs a plan digest")
	}
	return nil
}

func (c Context) Validate() error {
	if strings.TrimSpace(c.PrincipalID) == "" || strings.TrimSpace(c.TenantID) == "" {
		return errors.New("principal_id and tenant_id are required")
	}
	if strings.TrimSpace(c.CorrelationID) == "" || c.ResolvedAt.IsZero() {
		return errors.New("correlation_id and resolved_at are required")
	}
	if c.ExpiresAt != nil && !c.ExpiresAt.After(c.ResolvedAt) {
		return errors.New("expires_at must be after resolved_at")
	}
	for field, source := range c.Provenance {
		if field == "" || source.Source == "" || source.TrustLevel == "" || source.TrustLevel == TrustUntrusted {
			return errors.New("context provenance must identify a trusted source")
		}
	}
	switch c.Purpose() {
	case ContextPurposeRuntime:
		if c.ProvisioningAuthority != nil {
			return errors.New("only a TENANT_PROVISIONING context carries a provisioning authority")
		}
	case ContextPurposeTenantProvisioning:
		// Docs/architecture/context-authority-for-workloads.md section 13: bound to one approved plan, short, and
		// carrying no business dimension, so it cannot be mistaken for the context of an operating tenant.
		if c.ProvisioningAuthority == nil {
			return errors.New("a TENANT_PROVISIONING context must carry the approved plan it is bound to")
		}
		if err := c.ProvisioningAuthority.validate(); err != nil {
			return err
		}
		if c.ExpiresAt == nil || c.ExpiresAt.Sub(c.ResolvedAt) > MaxProvisioningContextLifetime {
			return errors.New("a TENANT_PROVISIONING context must expire within 15 minutes")
		}
		if c.LegalEntityID != "" || c.OrganisationID != "" || c.BusinessUnitID != "" || c.DigitalEstateID != "" || c.DigitalPropertyID != "" ||
			c.ChannelID != "" || c.MarketID != "" || c.Jurisdiction != "" || c.CountryCode != "" || c.CurrencyCode != "" {
			return errors.New("a TENANT_PROVISIONING context carries no business dimension")
		}
	default:
		return errors.New("unknown context authority purpose")
	}
	return nil
}

// IsExpired reports whether the context's optional lifetime bound has
// passed as of at (ADR-BCP-004 §72). A context with no ExpiresAt never
// expires by this check alone.
func (c Context) IsExpired(at time.Time) bool {
	return c.ExpiresAt != nil && !at.Before(*c.ExpiresAt)
}

type IsolationProfile struct {
	ID               string `json:"id,omitempty"`
	Name             string `json:"name"`
	Strategy         string `json:"strategy"`
	TenantScope      string `json:"tenant_scope"`
	DataPartitioning string `json:"data_partitioning"`
	IsDefault        bool   `json:"is_default"`
}

type Engine struct {
	ID          string `json:"id,omitempty"`
	Key         string `json:"engine_key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
}

type EngineInstance struct {
	ID                 string     `json:"id,omitempty"`
	EngineID           string     `json:"engine_id"`
	Key                string     `json:"engine_instance_key,omitempty"`
	Region             string     `json:"region"`
	Environment        string     `json:"environment"`
	IsolationProfileID string     `json:"isolation_profile_id,omitempty"`
	ResidencyRegion    string     `json:"residency_region,omitempty"`
	Status             string     `json:"status"`
	EffectiveFrom      time.Time  `json:"effective_from,omitempty"`
	EffectiveTo        *time.Time `json:"effective_to,omitempty"`
}

// MappingScope's fields mirror baobab-platform/shared's contracts/control-plane/v1/
// canonical-mapping.schema.json #/$defs/mappingScope field-for-field (names,
// required-ness) per docs/reconciliation/platform-resolution-spine-audit.md
// Gate 1; see internal/domain/contract_compatibility_test.go. Gate 2
// completed mapping.mapping_scope's schema and added real Postgres-backed
// persistence (internal/repository/postgres.go's CreateMappingScope/
// GetMappingScope/ListMappingScopes) -- MarketID, DigitalPropertyID,
// EngineID and EngineInstanceID still read back as this database's own
// uuid identifiers rather than the wire schema's slug pattern; see
// migration 000025_mapping_scope_dimensions.sql for why that particular
// gap is left open rather than converted unilaterally.
type MappingScope struct {
	ScopeID            string   `json:"scope_id,omitempty"`
	TenantID           string   `json:"tenant_id,omitempty"`
	LegalEntityID      string   `json:"legal_entity_id,omitempty"`
	OrganisationID     string   `json:"organisation_id,omitempty"`
	BusinessUnitID     string   `json:"business_unit_id,omitempty"`
	OperatingRegionID  string   `json:"operating_region_id,omitempty"`
	GeographicRegionID string   `json:"geographic_region_id,omitempty"`
	MarketID           string   `json:"market_id,omitempty"`
	Country            string   `json:"country,omitempty"`
	EstateID           string   `json:"estate_id,omitempty"`
	DigitalPropertyID  string   `json:"digital_property_id,omitempty"`
	ChannelID          string   `json:"channel_id,omitempty"`
	Currency           string   `json:"currency,omitempty"`
	Locale             string   `json:"locale,omitempty"`
	CatalogueID        string   `json:"catalogue_id,omitempty"`
	CustomerSegmentID  string   `json:"customer_segment_id,omitempty"`
	EngineID           string   `json:"engine_id,omitempty"`
	EngineInstanceID   string   `json:"engine_instance_id,omitempty"`
	Environment        string   `json:"environment,omitempty"`
	DeploymentRegion   string   `json:"deployment_region,omitempty"`
	IncludeCountries   []string `json:"include_countries,omitempty"`
	ExcludeCountries   []string `json:"exclude_countries,omitempty"`
	CreatedAt          string   `json:"created_at,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
}

func (s MappingScope) Validate() error {
	if strings.TrimSpace(s.ScopeID) == "" {
		return errors.New("scope_id is required")
	}
	if strings.TrimSpace(s.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	return nil
}
