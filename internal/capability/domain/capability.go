// Package domain holds the capability module's aggregate model: Capability,
// CapabilityScope, CapabilityGrant, CapabilityProvider,
// ProviderCapabilitySupport and CapabilityBinding (ADR-BCP-003 SS3,
// ADR-BCP-010 SS41). This package owns these types; other modules reference
// them through explicit imports rather than duplicating or reaching into
// this module's persistence directly (ADR-BCP-010 SS39-40, "one owning
// module per table/aggregate").
//
// Moved here from the former flat internal/domain package (where PR #83
// first landed CapabilityProvider/ProviderCapabilitySupport/CapabilityBinding
// ahead of this package split) to establish the ADR-BCP-010 SS41 module
// layout for the capability module before further capability-centric
// domain types (CapabilityScope, CapabilityGrant) are added.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var capabilityKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`)

// ValidCapabilityKey reports whether key follows Shared's implementation-
// neutral, dot/hyphen-segmented capability grammar.
func ValidCapabilityKey(key string) bool {
	return capabilityKeyPattern.MatchString(key)
}

// CapabilityLifecycle is a Capability's (or CapabilityProvider's, or
// CapabilityBinding's) operational lifecycle -- distinct from maturity: a
// capability can be maturity=SUPPORTED and lifecycle=SUSPENDED
// simultaneously (ADR-BCP-003 §6, mirrored in nabhold/shared's
// contracts/capability/v1/domain.schema.json #/$defs/capabilityLifecycle).
type CapabilityLifecycle string

const (
	CapabilityLifecycleDraft      CapabilityLifecycle = "DRAFT"
	CapabilityLifecycleActive     CapabilityLifecycle = "ACTIVE"
	CapabilityLifecycleSuspended  CapabilityLifecycle = "SUSPENDED"
	CapabilityLifecycleDeprecated CapabilityLifecycle = "DEPRECATED"
	CapabilityLifecycleRetired    CapabilityLifecycle = "RETIRED"
)

func (l CapabilityLifecycle) Valid() bool {
	switch l {
	case CapabilityLifecycleDraft, CapabilityLifecycleActive, CapabilityLifecycleSuspended, CapabilityLifecycleDeprecated, CapabilityLifecycleRetired:
		return true
	default:
		return false
	}
}

// CapabilityMaturity is the confidence/support level of a Capability's
// contract and implementation -- distinct from lifecycle (ADR-BCP-003 §7).
type CapabilityMaturity string

const (
	CapabilityMaturityExperimental CapabilityMaturity = "EXPERIMENTAL"
	CapabilityMaturityPreview      CapabilityMaturity = "PREVIEW"
	CapabilityMaturitySupported    CapabilityMaturity = "SUPPORTED"
	CapabilityMaturityDeprecated   CapabilityMaturity = "DEPRECATED"
	CapabilityMaturityRetired      CapabilityMaturity = "RETIRED"
)

func (m CapabilityMaturity) Valid() bool {
	switch m {
	case CapabilityMaturityExperimental, CapabilityMaturityPreview, CapabilityMaturitySupported, CapabilityMaturityDeprecated, CapabilityMaturityRetired:
		return true
	default:
		return false
	}
}

type Capability struct {
	ID          string              `json:"id,omitempty"`
	Key         string              `json:"capability_key"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	DomainKey   string              `json:"domain"`
	Lifecycle   CapabilityLifecycle `json:"lifecycle"`
	Maturity    CapabilityMaturity  `json:"maturity"`
	Version     int64               `json:"version,omitempty"`
}

func (c Capability) Validate() error {
	if !ValidCapabilityKey(c.Key) || strings.TrimSpace(c.Name) == "" {
		return errors.New("capability requires an implementation-neutral key and name")
	}
	if strings.TrimSpace(c.DomainKey) == "" {
		return errors.New("domain is required")
	}
	if !c.Lifecycle.Valid() {
		return errors.New("lifecycle must be one of DRAFT, ACTIVE, SUSPENDED, DEPRECATED, RETIRED")
	}
	if !c.Maturity.Valid() {
		return errors.New("maturity must be one of EXPERIMENTAL, PREVIEW, SUPPORTED, DEPRECATED, RETIRED")
	}
	return nil
}

// IsResolvable reports whether a capability in this lifecycle state SHALL
// resolve at all (ADR-BCP-003 §6's default eligibility matrix): ACTIVE
// always does, DRAFT/SUSPENDED/RETIRED never do. §6 allows DEPRECATED
// "where policy allows", but no governed platform policy mechanism exists
// yet to grant that allowance -- so DEPRECATED fails closed (denied) here,
// consistent with this codebase's standing default of failing closed
// wherever a policy hook is not yet implemented (e.g. cache fail-safe,
// ambiguous binding resolution). This is expected to become configurable,
// not to stay hardcoded, once governed platform policy exists.
func (c Capability) IsResolvable() bool {
	return c.Lifecycle == CapabilityLifecycleActive
}

// DependencyType is whether a depended-upon capability must, may, or
// conditionally must be satisfied (ADR-BCP-003 §8, mirrored in
// nabhold/shared's domain.schema.json #/$defs/capabilityDependencyType).
type DependencyType string

const (
	DependencyTypeRequired    DependencyType = "REQUIRED"
	DependencyTypeOptional    DependencyType = "OPTIONAL"
	DependencyTypeConditional DependencyType = "CONDITIONAL"
)

func (t DependencyType) Valid() bool {
	switch t {
	case DependencyTypeRequired, DependencyTypeOptional, DependencyTypeConditional:
		return true
	default:
		return false
	}
}

// CapabilityDependency declares that one capability depends on another.
// Mirrors nabhold/shared's contracts/capability/v1/capability.schema.json
// #/$defs/capabilityDependency (CapabilityKey/DependencyType/
// VersionConstraint/Condition), plus the owning-capability identity a
// persisted runtime edge needs that the wire-embedded contract shape does
// not (ADR-BCP-003 §51 lists capability.capability_dependency as its own
// persisted table, not merely a nested array on the wire document).
type CapabilityDependency struct {
	ID                  string         `json:"id,omitempty"`
	CapabilityID        string         `json:"capability_id,omitempty"`
	CapabilityKey       string         `json:"capability_key,omitempty"`
	DependsOnCapability string         `json:"depends_on_capability_key"`
	DependencyType      DependencyType `json:"dependency_type"`
	VersionConstraint   string         `json:"version_constraint,omitempty"`
	Condition           string         `json:"condition,omitempty"`
	Status              string         `json:"status,omitempty"`
}

func (d CapabilityDependency) Validate() error {
	if d.CapabilityID == "" && d.CapabilityKey == "" {
		return errors.New("owning capability identity is required")
	}
	if strings.TrimSpace(d.DependsOnCapability) == "" {
		return errors.New("depends_on_capability_key is required")
	}
	if d.CapabilityKey != "" && d.CapabilityKey == d.DependsOnCapability {
		return errors.New("a capability cannot depend on itself")
	}
	if !d.DependencyType.Valid() {
		return errors.New("dependency_type must be one of REQUIRED, OPTIONAL, CONDITIONAL")
	}
	// Mirrors shared's if/then: a CONDITIONAL dependency must always carry
	// a machine-evaluable condition; it is never expressed only in prose.
	if d.DependencyType == DependencyTypeConditional && strings.TrimSpace(d.Condition) == "" {
		return errors.New("condition is required when dependency_type is CONDITIONAL")
	}
	return nil
}

// HasCapabilityDependencyCycle reports whether the REQUIRED-only subgraph
// of dependencies contains a cycle. ADR-BCP-003 §8: "Required dependency
// graphs SHALL be acyclic." OPTIONAL and CONDITIONAL edges are excluded
// deliberately -- the ADR's acyclic requirement is scoped to REQUIRED
// dependencies, since an optional or conditional edge can never force an
// unsatisfiable resolution the way a required cycle would.
func HasCapabilityDependencyCycle(dependencies []CapabilityDependency) bool {
	edges := make(map[string][]string)
	for _, d := range dependencies {
		if d.DependencyType != DependencyTypeRequired || d.CapabilityKey == "" {
			continue
		}
		edges[d.CapabilityKey] = append(edges[d.CapabilityKey], d.DependsOnCapability)
	}

	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int)

	var visit func(node string) bool
	visit = func(node string) bool {
		switch state[node] {
		case visiting:
			return true
		case done:
			return false
		}
		state[node] = visiting
		for _, next := range edges[node] {
			if visit(next) {
				return true
			}
		}
		state[node] = done
		return false
	}

	for node := range edges {
		if state[node] == unvisited && visit(node) {
			return true
		}
	}
	return false
}

// BindingMode is the canonical, closed set of CapabilityBinding modes
// (BCP-TS-ONBOARDING-001 CR-002, mirrored in nabhold/shared's
// contracts/capability/v1/domain.schema.json). A superseded seven-value
// set (including SECONDARY, READ_ONLY, MIGRATION_SOURCE, MIGRATION_TARGET)
// existed in this repository's earlier migrations and must not be
// reintroduced -- see migration 000028's comment for the rationale and
// the remapping applied to any pre-existing rows.
type BindingMode string

const (
	// BindingModePrimary is the normal authoritative binding.
	BindingModePrimary BindingMode = "PRIMARY"
	// BindingModeFallback is eligible only under approved failover
	// semantics; it never bypasses entitlement, scope, contract,
	// residency or isolation checks.
	BindingModeFallback BindingMode = "FALLBACK"
	// BindingModeShadow is non-authoritative mirrored/validation
	// execution. A resolution SHALL NOT treat a SHADOW binding as a
	// valid authoritative result even if it wins ranking; a
	// SHADOW-only candidate set resolves as if no eligible binding
	// existed (nabhold/shared's scope-specificity.yaml, "binding mode
	// preference").
	BindingModeShadow BindingMode = "SHADOW"
	// BindingModeMigration is a temporary binding participating in an
	// explicit ProviderMigration plan.
	BindingModeMigration BindingMode = "MIGRATION"
	// BindingModeDisabled is retained but ineligible; it SHALL be
	// excluded from resolution candidates entirely, not merely
	// deprioritised.
	BindingModeDisabled BindingMode = "DISABLED"
)

// Valid reports whether m is one of the five canonical binding modes.
func (m BindingMode) Valid() bool {
	switch m {
	case BindingModePrimary, BindingModeFallback, BindingModeShadow, BindingModeMigration, BindingModeDisabled:
		return true
	default:
		return false
	}
}

// CapabilityProvider is a Baobab-recognised implementation of one or more
// capabilities. It is distinct from Engine (technology family) and
// EngineInstance (concrete deployment): a provider declares what it can
// implement, a binding declares where it does so
// (ADR-BCP-002 §5.3, ADR-BCP-006 §5-6, ADR-SHARED-007 §31-33). Confirmed
// entirely absent from this codebase during the Capability Platform
// Phase-0 audit -- this is net-new, not a remodel of an existing type.
type CapabilityProvider struct {
	ID           string         `json:"id,omitempty"`
	ProviderKey  string         `json:"provider_key"`
	Name         string         `json:"name"`
	ProviderType string         `json:"provider_type"`
	EngineID     string         `json:"engine_id"`
	Status       string         `json:"status"`
	Ownership    string         `json:"ownership,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Version      int64          `json:"version,omitempty"`
}

var providerKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$`)

func (p CapabilityProvider) Validate() error {
	if !providerKeyPattern.MatchString(p.ProviderKey) {
		return errors.New("provider_key must be in <owning-repository>.<engine> form, e.g. baobab-trade.medusa")
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.EngineID) == "" {
		return errors.New("provider requires a name and engine_id")
	}
	switch p.ProviderType {
	case "BAOBAB_ENGINE", "EXTERNAL_SERVICE", "PLATFORM_NATIVE", "ADAPTER":
	default:
		return errors.New("provider_type must be one of BAOBAB_ENGINE, EXTERNAL_SERVICE, PLATFORM_NATIVE, ADAPTER")
	}
	return nil
}

// ProviderCapabilitySupport is a provider's explicit declaration that it
// implements a specific capability at specific contract versions. Support
// is never inferred merely from engine association (ADR-SHARED-007 §31).
type ProviderCapabilitySupport struct {
	ProviderID       string     `json:"provider_id"`
	CapabilityID     string     `json:"capability_id,omitempty"`
	CapabilityKey    string     `json:"capability_key,omitempty"`
	ContractVersions []int      `json:"contract_versions"`
	Status           string     `json:"status"`
	EffectiveFrom    time.Time  `json:"effective_from,omitempty"`
	EffectiveTo      *time.Time `json:"effective_to,omitempty"`
}

func (s ProviderCapabilitySupport) Validate() error {
	if s.ProviderID == "" {
		return errors.New("provider_id is required")
	}
	if s.CapabilityID == "" && s.CapabilityKey == "" {
		return errors.New("capability identity is required")
	}
	if len(s.ContractVersions) == 0 {
		return errors.New("at least one contract_version is required")
	}
	if s.EffectiveTo != nil && !s.EffectiveTo.After(s.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}
	return nil
}

type CapabilityBinding struct {
	ID               string         `json:"id,omitempty"`
	CapabilityID     string         `json:"capability_id,omitempty"`
	CapabilityKey    string         `json:"capability_key,omitempty"`
	ProviderID       string         `json:"provider_id,omitempty"`
	EngineID         string         `json:"engine_id,omitempty"`
	EngineInstanceID string         `json:"engine_instance_id"`
	ScopeID          string         `json:"scope_id"`
	BindingMode      BindingMode    `json:"binding_mode"`
	Priority         int            `json:"priority"`
	Status           string         `json:"status"`
	ContractVersion  string         `json:"contract_version"`
	EffectiveFrom    time.Time      `json:"effective_from,omitempty"`
	EffectiveTo      *time.Time     `json:"effective_to,omitempty"`
	Configuration    map[string]any `json:"configuration,omitempty"`
	Version          int64          `json:"version,omitempty"`
}

func (b CapabilityBinding) Validate() error {
	if b.CapabilityID == "" && b.CapabilityKey == "" {
		return errors.New("capability identity is required")
	}
	if b.EngineInstanceID == "" || b.ScopeID == "" {
		return errors.New("engine_instance_id and scope_id are required")
	}
	if b.BindingMode != "" && !b.BindingMode.Valid() {
		return errors.New("binding_mode must be one of PRIMARY, FALLBACK, SHADOW, MIGRATION, DISABLED")
	}
	if b.EffectiveTo != nil && !b.EffectiveTo.After(b.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}
	return nil
}
