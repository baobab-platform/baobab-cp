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
	HealthStatus       string     `json:"health_status,omitempty"`
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

type ExternalReference struct {
	ID                string         `json:"id,omitempty"`
	CanonicalEntityID string         `json:"canonical_entity_id"`
	EngineID          string         `json:"engine_id"`
	EngineInstanceID  string         `json:"engine_instance_id,omitempty"`
	NativeType        string         `json:"native_type"`
	NativeID          string         `json:"native_id"`
	ExternalURL       string         `json:"external_url,omitempty"`
	Status            string         `json:"status"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

func (r ExternalReference) Validate() error {
	if r.CanonicalEntityID == "" || r.EngineID == "" || r.NativeType == "" || r.NativeID == "" {
		return errors.New("canonical_entity_id, engine_id, native_type and native_id are required")
	}
	return nil
}
