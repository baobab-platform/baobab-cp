package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// ExternalReference records that a native object exists in a registered
// external system: Shared control-plane/v1 canonical-mapping.schema.json
// #/$defs/externalReference (ADR-SHARED-013). It never names a canonical
// entity; a Mapping relates the two.
type ExternalReference struct {
	ID               string
	SystemNamespace  string
	EngineID         string
	EngineInstanceID string
	Environment      string
	NativeEntityType string
	NativeID         string
	NativeKey        string
	NativeURI        string
	SourceAuthority  string
	Fingerprint      string
	Status           string
	FirstSeenAt      time.Time
	LastVerifiedAt   *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NativeIdentity identifies one native object: the ExternalReference's
// natural key (Canonical Mapping Model section 8.3). Empty optional fields
// mean "not recorded".
type NativeIdentity struct {
	SystemNamespace  string
	EngineID         string
	EngineInstanceID string
	Environment      string
	NativeEntityType string
	NativeID         string
}

// The initial status and source of an administratively registered reference.
const (
	ExternalReferenceUnverified   = "unverified"
	ExternalReferenceManualImport = "manual-import"
)

var (
	systemNamespacePattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	nativeEntityTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
)

var environments = map[string]bool{"local": true, "development": true, "staging": true, "production": true}

// Validate checks the native identity's grammar (the schema's structural
// rules). Whether the system is registered is ExternalSystems' concern.
// ValidSystemNamespace reports whether namespace is an ExternalReference
// system_namespace in Shared's grammar.
func ValidSystemNamespace(namespace string) bool {
	return len(namespace) <= 128 && systemNamespacePattern.MatchString(namespace)
}

func (n NativeIdentity) Validate() error {
	switch {
	case !ValidSystemNamespace(n.SystemNamespace):
		return errors.New("system_namespace must be a lower-case snake_case namespace")
	case !ValidEngineID(n.EngineID):
		return errors.New("engine_id must be an engine id such as baobab-trade")
	case n.EngineInstanceID != "" && !ValidEngineInstanceID(n.EngineInstanceID):
		return errors.New("engine_instance_id must be a canonical ei_ identifier")
	case n.Environment != "" && !environments[n.Environment]:
		return errors.New("environment must be local, development, staging or production")
	case len(n.NativeEntityType) > 128 || !nativeEntityTypePattern.MatchString(n.NativeEntityType):
		return errors.New("native_entity_type must be a lower-case snake_case type")
	case strings.TrimSpace(n.NativeID) == "" || len(n.NativeID) > 256:
		return errors.New("native_id must be 1 to 256 characters")
	}
	return nil
}

// Identity is the reference's native identity.
func (r ExternalReference) Identity() NativeIdentity {
	return NativeIdentity{SystemNamespace: r.SystemNamespace, EngineID: r.EngineID, EngineInstanceID: r.EngineInstanceID,
		Environment: r.Environment, NativeEntityType: r.NativeEntityType, NativeID: r.NativeID}
}

// ExternalSystems is the registry of external systems and the engines that
// may hold each (Shared control-plane/v1 external-systems.yaml,
// ADR-SHARED-012). A namespace is never inferred from an engine.
type ExternalSystems map[string]map[string]bool

// Registered reports whether engineID may hold objects of systemNamespace.
func (s ExternalSystems) Registered(systemNamespace, engineID string) bool {
	return s[systemNamespace][engineID]
}
