// Target path: internal/provisioning/manifest.go
//
// Versioned declarative tenant provisioning input. This is desired state,
// not a second canonical domain model: symbolic references are resolved by
// the Control Plane before canonical resources are written.
package provisioning

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type TenantManifest struct {
	APIVersion string `json:"api_version" yaml:"api_version"`
	Kind string `json:"kind" yaml:"kind"`
	Metadata ManifestMetadata `json:"metadata" yaml:"metadata"`
	Spec TenantManifestSpec `json:"spec" yaml:"spec"`
}

type ManifestMetadata struct {
	Name string `json:"name" yaml:"name"`
	TenantID string `json:"tenant_id" yaml:"tenant_id"`
	DesiredStateVersion int64 `json:"desired_state_version" yaml:"desired_state_version"`
}

type TenantManifestSpec struct {
	LegalEntityID string `json:"legal_entity_id" yaml:"legal_entity_id"`
	DigitalEstate string `json:"digital_estate" yaml:"digital_estate"`
	Markets []ManifestMarket `json:"markets" yaml:"markets"`
	CapabilityGrants []ManifestCapabilityGrant `json:"capability_grants" yaml:"capability_grants"`
	CapabilityBindings []ManifestCapabilityBinding `json:"capability_bindings" yaml:"capability_bindings"`
	TradeLanes []ManifestTradeLane `json:"trade_lanes" yaml:"trade_lanes"`
	IsolationRequirement string `json:"isolation_requirement,omitempty" yaml:"isolation_requirement,omitempty"`
	ResidencyRequirement string `json:"residency_requirement,omitempty" yaml:"residency_requirement,omitempty"`
}

type ManifestMarket struct {
	MarketCode string `json:"market_code" yaml:"market_code"`
	Capabilities []string `json:"capabilities" yaml:"capabilities"`
}
type ManifestCapabilityGrant struct {
	CapabilityKey string `json:"capability_key" yaml:"capability_key"`
	MarketCode string `json:"market_code,omitempty" yaml:"market_code,omitempty"`
	Source string `json:"source" yaml:"source"`
	SourceReference string `json:"source_reference" yaml:"source_reference"`
}
type ManifestCapabilityBinding struct {
	CapabilityKey string `json:"capability_key" yaml:"capability_key"`
	MarketCode string `json:"market_code,omitempty" yaml:"market_code,omitempty"`
	Engine string `json:"engine" yaml:"engine"`
	EngineInstance string `json:"engine_instance" yaml:"engine_instance"`
	Mode string `json:"mode" yaml:"mode"`
	Priority int `json:"priority" yaml:"priority"`
}
type ManifestTradeLane struct {
	OriginMarket string `json:"origin_market" yaml:"origin_market"`
	DestinationMarket string `json:"destination_market" yaml:"destination_market"`
	Direction string `json:"direction" yaml:"direction"`
	PermittedCapabilityKeys []string `json:"permitted_capability_keys,omitempty" yaml:"permitted_capability_keys,omitempty"`
}

func (m TenantManifest) Validate() error {
	if m.APIVersion!="baobab.nabhold.com/v1" { return errors.New("unsupported api_version") }
	if m.Kind!="TenantProvisioning" { return errors.New("kind must be TenantProvisioning") }
	if strings.TrimSpace(m.Metadata.Name)=="" || strings.TrimSpace(m.Metadata.TenantID)=="" {
		return errors.New("metadata.name and metadata.tenant_id are required")
	}
	if m.Metadata.DesiredStateVersion < 1 { return errors.New("desired_state_version must be >= 1") }
	if strings.TrimSpace(m.Spec.LegalEntityID)=="" || strings.TrimSpace(m.Spec.DigitalEstate)=="" {
		return errors.New("legal_entity_id and digital_estate are required")
	}
	markets:=map[string]struct{}{}
	for _, market:=range m.Spec.Markets {
		code:=strings.ToUpper(strings.TrimSpace(market.MarketCode))
		if code=="" { return errors.New("market_code is required") }
		if _,exists:=markets[code];exists { return fmt.Errorf("duplicate market %s",code) }
		markets[code]=struct{}{}
	}
	for _, lane:=range m.Spec.TradeLanes {
		if lane.OriginMarket==lane.DestinationMarket { return errors.New("trade lane origin and destination must differ") }
		if _,ok:=markets[strings.ToUpper(lane.OriginMarket)];!ok { return fmt.Errorf("unknown origin market %s",lane.OriginMarket) }
		if _,ok:=markets[strings.ToUpper(lane.DestinationMarket)];!ok { return fmt.Errorf("unknown destination market %s",lane.DestinationMarket) }
	}
	return nil
}

func (m TenantManifest) StableResourceKeys() []string {
	var keys []string
	for _,v:=range m.Spec.Markets { keys=append(keys,"market:"+strings.ToUpper(v.MarketCode)) }
	for _,v:=range m.Spec.CapabilityGrants { keys=append(keys,"grant:"+v.CapabilityKey+":"+strings.ToUpper(v.MarketCode)) }
	for _,v:=range m.Spec.CapabilityBindings { keys=append(keys,"binding:"+v.CapabilityKey+":"+strings.ToUpper(v.MarketCode)) }
	for _,v:=range m.Spec.TradeLanes { keys=append(keys,"lane:"+strings.ToUpper(v.OriginMarket)+":"+strings.ToUpper(v.DestinationMarket)) }
	sort.Strings(keys)
	return keys
}
