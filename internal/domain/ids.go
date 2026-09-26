package domain

import (
	"regexp"
	"strings"
)

// These patterns are copied verbatim from
// baobab-platform/shared's contracts/control-plane/v1/domain.schema.json so that every
// layer of this repository checks identifier shape exactly one way instead of
// re-deriving a slightly different regex per file (see
// docs/reconciliation/shared-control-plane-audit.md §3).
var (
	tenantIDPattern            = regexp.MustCompile(`^tn_[a-z0-9]+$`)
	canonicalLegalEntityID     = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$`)
	legacyLegalEntityAliasID   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	productIDPattern           = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)
	mappingIDPattern           = regexp.MustCompile(`^map_[a-z0-9]+$`)
	externalReferenceIDPattern = regexp.MustCompile(`^ref_[a-z0-9]+$`)
	mappingScopeIDPattern      = regexp.MustCompile(`^scope_[a-z0-9]+$`)
	engineIDPattern            = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	engineInstanceIDPattern    = regexp.MustCompile(`^ei_[a-z0-9]+$`)
)

// ValidEngineID reports whether v satisfies $defs.engineId: a registered
// engine named as its repository, e.g. "baobab-trade" (ADR-SHARED-012).
func ValidEngineID(v string) bool {
	return len(v) >= 3 && len(v) <= 63 && engineIDPattern.MatchString(v)
}

// ValidEngineInstanceID reports whether v satisfies $defs.engineInstanceId,
// the canonical "ei_" engine instance identifier (ADR-SHARED-012).
func ValidEngineInstanceID(v string) bool {
	return len(v) >= 6 && len(v) <= 63 && engineInstanceIDPattern.MatchString(v)
}

// EngineInstanceKey is the canonical identifier of the engine instance whose
// internal surrogate is id: "ei_" and id's lowercase letters and digits.
// topology.engine_instance.engine_instance_key (migration 000058) derives
// the same value from the UUID, so the surrogate never reaches the wire. An
// id already in canonical form is returned unchanged.
func EngineInstanceKey(id string) string {
	if ValidEngineInstanceID(id) {
		return id
	}
	var b strings.Builder
	b.WriteString("ei_")
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidTenantID reports whether v satisfies the Control Plane-minted tenant
// identifier grammar ($defs.tenantId): "tn_" followed by an opaque lowercase
// alphanumeric token. tenant_id is never client-supplied; see NewTenantID.
func ValidTenantID(v string) bool {
	return len(v) >= 6 && len(v) <= 63 && tenantIDPattern.MatchString(v)
}

// ValidLegalEntityID reports whether v is a canonical legal-entity identifier
// (uppercase kebab-case, e.g. "THAMANI-GLOBAL", from
// contracts/legal-entity/registry.yaml) or an accepted v1-compatibility
// lowercase alias, per domain.schema.json's legalEntityIdInput union. New
// tenant registrations SHOULD supply the canonical form; the legacy alias is
// accepted only at this input boundary during the documented v1 compatibility
// window (ADR-0003 §2.3) — callers must not persist or emit it as canonical.
func ValidLegalEntityID(v string) bool {
	if len(v) < 3 || len(v) > 63 {
		return false
	}
	return canonicalLegalEntityID.MatchString(v) || legacyLegalEntityAliasID.MatchString(v)
}

// IsCanonicalLegalEntityID reports whether v is already in the canonical
// uppercase-kebab form, as opposed to a legacy lowercase alias.
func IsCanonicalLegalEntityID(v string) bool {
	return canonicalLegalEntityID.MatchString(v)
}

// ValidProductID reports whether v satisfies the shared productId grammar.
func ValidProductID(v string) bool {
	return len(v) >= 3 && len(v) <= 63 && productIDPattern.MatchString(v)
}

// ValidMappingID reports whether v satisfies the Control Plane-minted mapping
// identifier grammar ($defs.mappingId).
func ValidMappingID(v string) bool {
	return len(v) >= 8 && len(v) <= 63 && mappingIDPattern.MatchString(v)
}

// ValidExternalReferenceID reports whether v satisfies the Control
// Plane-minted external-reference identifier grammar ($defs.externalReferenceId).
func ValidExternalReferenceID(v string) bool {
	return len(v) >= 8 && len(v) <= 63 && externalReferenceIDPattern.MatchString(v)
}

// ValidMappingScopeID reports whether v satisfies the Control Plane-minted
// mapping-scope identifier grammar ($defs.mappingScopeId).
func ValidMappingScopeID(v string) bool {
	return len(v) >= 8 && len(v) <= 63 && mappingScopeIDPattern.MatchString(v)
}

// NewTenantID mints a new opaque, Control Plane-owned tenant identifier. Per
// tenancy.yaml, a newly minted tenant_id must not embed a legal-entity name,
// country or market, so the caller-supplied legal_entity_id is never used
// here — this is why tenant_id is not an accepted field on the wire (see
// RegisterTenant).
func NewTenantID() string { return newOpaqueID("tn_") }

// NewMappingID mints a new opaque mapping identifier.
func NewMappingID() string { return newOpaqueID("map_") }

// NewExternalReferenceID mints a new opaque external-reference identifier.
func NewExternalReferenceID() string { return newOpaqueID("ref_") }

// NewMappingScopeID mints a new opaque mapping-scope identifier.
func NewMappingScopeID() string { return newOpaqueID("scope_") }

// newOpaqueID mints a prefixed opaque token from a UUIDv7's lowercase hex
// representation (time-ordered and collision-resistant, matching the
// identifier guidance in the Canonical Mapping Model §6) rather than
// embedding any semantic content in the identifier itself.
func newOpaqueID(prefix string) string {
	return prefix + strings.ReplaceAll(NewUUIDv7(), "-", "")
}
