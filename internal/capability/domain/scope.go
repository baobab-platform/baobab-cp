package domain

import (
	"errors"
	"strings"
)

// CapabilityScope represents the applicability dimensions of one
// CapabilityGrant or CapabilityBinding (ADR-BCP-003 §13). It mirrors
// baobab-platform/shared's contracts/capability/v1/scope.schema.json field-for-field
// and is deliberately distinct from MappingScope (internal/domain):
// the two share dimension vocabulary but are evaluated by different
// resolvers for different purposes (ADR-SHARED-007 §25). An unspecified
// dimension means "not further restricted on this axis" -- it is never a
// wildcard bypass of dimensions that ARE specified elsewhere (§14).
type CapabilityScope struct {
	ScopeID            string         `json:"scope_id,omitempty"`
	TenantID           string         `json:"tenant_id"`
	LegalEntityID      string         `json:"legal_entity_id,omitempty"`
	OrganisationID     string         `json:"organisation_id,omitempty"`
	BusinessUnitID     string         `json:"business_unit_id,omitempty"`
	DigitalEstateID    string         `json:"digital_estate_id,omitempty"`
	DigitalPropertyID  string         `json:"digital_property_id,omitempty"`
	ChannelID          string         `json:"channel_id,omitempty"`
	MarketID           string         `json:"market_id,omitempty"`
	Jurisdiction       string         `json:"jurisdiction,omitempty"`
	CurrencyCode       string         `json:"currency_code,omitempty"`
	CustomerSegmentID  string         `json:"customer_segment_id,omitempty"`
	CatalogueID        string         `json:"catalogue_id,omitempty"`
	OperatingRegionID  string         `json:"operating_region_id,omitempty"`
	GeographicRegionID string         `json:"geographic_region_id,omitempty"`
	DeploymentRegion   string         `json:"deployment_region,omitempty"`
	Environment        string         `json:"environment,omitempty"`
	IsolationProfileID string         `json:"isolation_profile_id,omitempty"`
	IncludeCountries   []string       `json:"include_countries,omitempty"`
	ExcludeCountries   []string       `json:"exclude_countries,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CreatedAt          string         `json:"created_at,omitempty"`
	UpdatedAt          string         `json:"updated_at,omitempty"`
}

func (s CapabilityScope) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	// §15: exclusion always takes precedence over inclusion, but a scope
	// whose exclude_countries fully cancels out every entry in a non-empty
	// include_countries leaves no country ever eligible -- that is not a
	// restrictive-but-valid scope, it is a scope that can never match, and
	// §15 requires rejecting such contradictory configuration before
	// activation rather than allowing it to silently resolve to "always
	// ineligible".
	if len(s.IncludeCountries) > 0 {
		excluded := make(map[string]bool, len(s.ExcludeCountries))
		for _, country := range s.ExcludeCountries {
			excluded[country] = true
		}
		anyEligible := false
		for _, country := range s.IncludeCountries {
			if !excluded[country] {
				anyEligible = true
				break
			}
		}
		if !anyEligible {
			return errors.New("include_countries and exclude_countries are contradictory: no country would ever be eligible")
		}
	}
	return nil
}
