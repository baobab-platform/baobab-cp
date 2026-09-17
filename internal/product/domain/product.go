// Package domain holds the product module's aggregate model: Product,
// ProductVersion and EntitlementProjection (Technical Specification §31,
// §88, §91; Programme Gate P4 "Product and Composition Engine"). Mirrors
// nabhold/shared's contracts/product/v1 package field-for-field. This
// package owns these types; other modules reference them through explicit
// imports rather than duplicating or reaching into this module's
// persistence directly (ADR-BCP-010 §39-40, "one owning module per
// table/aggregate") -- the same layout internal/capability/domain
// establishes for the capability module.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	productIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)
	productVersionIDPattern = regexp.MustCompile(`^prodver_[a-z0-9]+$`)
	productVersionSemver    = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// ValidProductID reports whether v satisfies Shared's implementation-neutral
// productId grammar (contracts/control-plane/v1/domain.schema.json
// #/$defs/productId).
func ValidProductID(v string) bool {
	return len(v) >= 3 && len(v) <= 63 && productIDPattern.MatchString(v)
}

// ProductLifecycle is the operational lifecycle of a Product or
// ProductVersion (mirrors nabhold/shared's contracts/product/v1/domain.schema.json
// #/$defs/productLifecycle -- deliberately the same five values as
// capability/v1's capabilityLifecycle, declared locally since Product is not
// itself a Capability).
type ProductLifecycle string

const (
	ProductLifecycleDraft      ProductLifecycle = "DRAFT"
	ProductLifecycleActive     ProductLifecycle = "ACTIVE"
	ProductLifecycleSuspended  ProductLifecycle = "SUSPENDED"
	ProductLifecycleDeprecated ProductLifecycle = "DEPRECATED"
	ProductLifecycleRetired    ProductLifecycle = "RETIRED"
)

func (l ProductLifecycle) Valid() bool {
	switch l {
	case ProductLifecycleDraft, ProductLifecycleActive, ProductLifecycleSuspended, ProductLifecycleDeprecated, ProductLifecycleRetired:
		return true
	default:
		return false
	}
}

// Product is a sellable platform offering (e.g. "baobab-xbt"). It carries no
// business logic and mints no runtime service.
type Product struct {
	ID          string           `json:"product_id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Status      ProductLifecycle `json:"status"`
	Owner       string           `json:"owner,omitempty"`
}

func (p Product) Validate() error {
	if !ValidProductID(p.ID) {
		return errors.New("product requires an implementation-neutral product_id")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name is required")
	}
	if !p.Status.Valid() {
		return errors.New("status must be one of DRAFT, ACTIVE, SUSPENDED, DEPRECATED, RETIRED")
	}
	return nil
}

// ProductVersion is one immutable, versioned packaging of a Product as a
// capabilitydomain.CapabilityComposition. Published versions are immutable:
// a change is a new version, never an in-place edit (mirrors ADR-BCP-005
// §114). ProductSubscription -> ProductVersion -> CapabilityComposition ->
// CapabilityGrant expansion is CompositionExpansionService's responsibility
// (Technical Specification §31).
type ProductVersion struct {
	ID             string           `json:"product_version_id,omitempty"`
	ProductID      string           `json:"product_id"`
	Version        string           `json:"version"`
	CompositionKey string           `json:"composition_key"`
	Status         ProductLifecycle `json:"status"`
	ReleasedAt     *time.Time       `json:"released_at,omitempty"`
	DeprecatedAt   *time.Time       `json:"deprecated_at,omitempty"`
	RetiredAt      *time.Time       `json:"retired_at,omitempty"`
}

func (v ProductVersion) Validate() error {
	if !ValidProductID(v.ProductID) {
		return errors.New("product_version requires an implementation-neutral product_id")
	}
	if !productVersionSemver.MatchString(v.Version) {
		return errors.New("version must be a semantic version (major.minor.patch)")
	}
	if strings.TrimSpace(v.CompositionKey) == "" {
		return errors.New("composition_key is required")
	}
	if !v.Status.Valid() {
		return errors.New("status must be one of DRAFT, ACTIVE, SUSPENDED, DEPRECATED, RETIRED")
	}
	if v.Status == ProductLifecycleDeprecated && v.DeprecatedAt == nil {
		return errors.New("deprecated_at is required once status is DEPRECATED")
	}
	return nil
}

// ValidProductVersionID reports whether v is a Control Plane-minted
// ProductVersion identifier (contracts/product/v1/domain.schema.json
// #/$defs/productVersionId).
func ValidProductVersionID(v string) bool {
	return len(v) >= 10 && len(v) <= 63 && productVersionIDPattern.MatchString(v)
}
