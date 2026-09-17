package domain

import (
	"errors"
	"regexp"
	"strings"
)

var compositionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*\.[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// ValidCompositionKey reports whether key follows Shared's "<type>.<name>"
// composition-key grammar (contracts/capability/v1/domain.schema.json
// #/$defs/capabilityCompositionKey), e.g. "platform.core", "solution.baobab-xbt".
func ValidCompositionKey(key string) bool {
	return compositionKeyPattern.MatchString(key)
}

// CapabilityCompositionType classifies a CapabilityComposition -- governs how
// it may be combined with others, not runtime behaviour (mirrors
// nabhold/shared's contracts/capability/v1/domain.schema.json
// #/$defs/capabilityCompositionType).
type CapabilityCompositionType string

const (
	CompositionTypePlatform CapabilityCompositionType = "PLATFORM"
	CompositionTypeProduct  CapabilityCompositionType = "PRODUCT"
	CompositionTypeProfile  CapabilityCompositionType = "PROFILE"
	CompositionTypeAddOn    CapabilityCompositionType = "ADD_ON"
	CompositionTypeInternal CapabilityCompositionType = "INTERNAL"
)

func (t CapabilityCompositionType) Valid() bool {
	switch t {
	case CompositionTypePlatform, CompositionTypeProduct, CompositionTypeProfile, CompositionTypeAddOn, CompositionTypeInternal:
		return true
	default:
		return false
	}
}

// MembershipCriticality is the criticality of one capability's membership
// within one specific composition -- the same capability can be MANDATORY in
// one composition and OPTIONAL in another (mirrors nabhold/shared's
// #/$defs/capabilityMembershipCriticality).
type MembershipCriticality string

const (
	MembershipCriticalityMandatory MembershipCriticality = "MANDATORY"
	MembershipCriticalityImportant MembershipCriticality = "IMPORTANT"
	MembershipCriticalityOptional  MembershipCriticality = "OPTIONAL"
)

func (c MembershipCriticality) Valid() bool {
	switch c {
	case MembershipCriticalityMandatory, MembershipCriticalityImportant, MembershipCriticalityOptional:
		return true
	default:
		return false
	}
}

// CompositionMember names one capability's participation in a
// CapabilityComposition. Mirrors nabhold/shared's
// contracts/capability/v1/composition.schema.json #/$defs/compositionMember.
type CompositionMember struct {
	CapabilityKey       string                `json:"capability_key"`
	Criticality         MembershipCriticality `json:"criticality"`
	VersionConstraint   string                `json:"version_constraint,omitempty"`
	ActivationCondition string                `json:"activation_condition,omitempty"`
}

func (m CompositionMember) Validate() error {
	if !ValidCapabilityKey(m.CapabilityKey) {
		return errors.New("composition member requires an implementation-neutral capability_key")
	}
	if !m.Criticality.Valid() {
		return errors.New("composition member criticality must be one of MANDATORY, IMPORTANT, OPTIONAL")
	}
	return nil
}

// CapabilityComposition is a declarative grouping of capabilities into
// platform/product/profile/add-on packages. It carries no business logic and
// mints no runtime service; expanding a ProductSubscription's ProductVersion
// composition_key into real CapabilityGrant records is
// CompositionExpansionService's job (ADR-BCP-005, Programme Gate P4). Mirrors
// nabhold/shared's contracts/capability/v1/composition.schema.json
// #/$defs/composition.
type CapabilityComposition struct {
	ID              string                    `json:"id,omitempty"`
	CompositionKey  string                    `json:"composition_key"`
	Name            string                    `json:"name,omitempty"`
	CompositionType CapabilityCompositionType `json:"composition_type"`
	Version         string                    `json:"version"`
	Members         []CompositionMember       `json:"members"`
	// IncludesCompositions and IncompatibleWith are carried for forward
	// compatibility with nabhold/shared's schema but are not resolved by
	// CompositionExpansionService yet -- graph inclusion/exclusion is
	// explicitly out of scope for Programme Gate P4's "basics" cut (see the
	// package doc on composition_service.go). A composition using either
	// field expands only its own direct Members.
	IncludesCompositions []string            `json:"includes_compositions,omitempty"`
	IncompatibleWith     []string            `json:"incompatible_with,omitempty"`
	Lifecycle            CapabilityLifecycle `json:"lifecycle"`
	Metadata             map[string]any      `json:"metadata,omitempty"`
}

func (c CapabilityComposition) Validate() error {
	if !ValidCompositionKey(c.CompositionKey) {
		return errors.New("composition requires a <type>.<name> composition_key")
	}
	if !c.CompositionType.Valid() {
		return errors.New("composition_type must be one of PLATFORM, PRODUCT, PROFILE, ADD_ON, INTERNAL")
	}
	if strings.TrimSpace(c.Version) == "" {
		return errors.New("version is required")
	}
	if len(c.Members) == 0 {
		return errors.New("composition requires at least one member")
	}
	for _, m := range c.Members {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	if !c.Lifecycle.Valid() {
		return errors.New("lifecycle must be one of DRAFT, ACTIVE, SUSPENDED, DEPRECATED, RETIRED")
	}
	return nil
}

// IsResolvable reports whether this composition is eligible for expansion --
// same default-eligibility rule as Capability.IsResolvable (ADR-BCP-003 §6):
// only ACTIVE compositions expand.
func (c CapabilityComposition) IsResolvable() bool {
	return c.Lifecycle == CapabilityLifecycleActive
}
