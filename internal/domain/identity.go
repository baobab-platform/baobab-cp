package domain

import (
	"errors"
	"time"
)

// Principal is Baobab's canonical identity, per ADR-0004 ("Canonical
// Identity and External Identity Mapping") -- a durable actor reference
// that outlives any individual identity provider, engine, email address,
// tenant relationship or Digital Estate. It is named Principal, not
// CanonicalIdentity, to match contracts/identity/v1/principal.schema.json
// exactly (docs/governance/gate-iam-3-canonical-identity-scope.md,
// Decision 1) -- and to avoid sitting next to this package's unrelated
// CanonicalEntity (a Mapping-domain business entity, e.g. a product; see
// canonical.go) under a confusingly similar name.
//
// Deliberately minimal per ADR-0004 §3: "It SHALL NOT contain the complete
// user profile." Human/workload-specific attributes
// (contracts/identity/v1/human-identity.schema.json,
// workload-identity.schema.json) are a later phase, not fields here.
type Principal struct {
	ID        string    `json:"id,omitempty"`
	ActorType string    `json:"actor_type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

var validPrincipalActorTypes = map[string]bool{"human": true, "workload": true, "external": true}
var validPrincipalStatuses = map[string]bool{"ACTIVE": true, "SUSPENDED": true, "DISABLED": true, "ARCHIVED": true}

func (p Principal) Validate() error {
	if !validPrincipalActorTypes[p.ActorType] {
		return errors.New("actor_type must be human, workload or external")
	}
	if !validPrincipalStatuses[p.Status] {
		return errors.New("status must be ACTIVE, SUSPENDED, DISABLED or ARCHIVED")
	}
	return nil
}

// NewPrincipalID mints a new canonical identity identifier. Per ADR-0004 §5
// ("If baobab-platform/shared defines a UUID or UUIDv7 convention for canonical
// entities, Canonical Identity SHALL follow that convention") this follows
// contracts/identity/v1/principal.schema.json's own established convention
// -- a bare UUID (format: uuid), not this package's map_/scope_/ref_-style
// opaque-prefixed tokens, which are a control-plane/v1 wire convention
// identity/v1 does not use anywhere (Session.id, ExternalIdentity.id are
// bare uuid too). NewUUIDv7, not Postgres's gen_random_uuid() (UUIDv4): see
// identifier.go's BCP-GO-001 citation.
func NewPrincipalID() string { return NewUUIDv7() }

// ExternalIdentity is an identity asserted by an external identity
// provider, mapped to a Principal -- ADR-0004 §6-9: the durable provider
// key is (issuer, subject), never email or username, because both are
// mutable, reassignable and federated inconsistently. Matches
// contracts/identity/v1/external-identity.schema.json field-for-field;
// ProviderType and LastSeenAt are optional there (and here) since that
// schema's own required list omits them.
type ExternalIdentity struct {
	ID           string     `json:"id,omitempty"`
	PrincipalID  string     `json:"principal_id"`
	Issuer       string     `json:"issuer"`
	Subject      string     `json:"subject"`
	ProviderType string     `json:"provider_type,omitempty"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at,omitempty"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
}

var validExternalIdentityStatuses = map[string]bool{"ACTIVE": true, "UNLINKED": true, "DISABLED": true, "REVOKED": true}

func (e ExternalIdentity) Validate() error {
	if e.PrincipalID == "" {
		return errors.New("principal_id is required")
	}
	if e.Issuer == "" || e.Subject == "" {
		return errors.New("issuer and subject are required")
	}
	if !validExternalIdentityStatuses[e.Status] {
		return errors.New("status must be ACTIVE, UNLINKED, DISABLED or REVOKED")
	}
	return nil
}

// NewExternalIdentityID mints a new external-identity identifier, following
// the same bare-UUID convention as NewPrincipalID (identity/v1's own
// convention, not this package's opaque-prefixed control-plane/v1 tokens).
func NewExternalIdentityID() string { return NewUUIDv7() }

// IdentityReference maps a Principal to an engine-native actor -- ADR-0004
// §23-27: Medusa customer, iDempiere AD_User, Payload user, etc. "SHALL be
// represented as mappings rather than canonical identities" (§23). Matches
// contracts/identity/v1/external-reference.schema.json field-for-field.
//
// Deliberately not this package's own ExternalReference (resolution.go),
// which is keyed to CanonicalEntityID -- a Mapping-domain business-entity
// concept (see canonical.go), not an actor. Gate IAM-3's scope doc
// (Decision 2) records why identity mappings live in a new, parallel
// identity.identity_reference table instead.
type IdentityReference struct {
	ID               string    `json:"id,omitempty"`
	PrincipalID      string    `json:"principal_id"`
	Engine           string    `json:"engine"`
	EngineInstanceID string    `json:"engine_instance_id,omitempty"`
	ExternalType     string    `json:"external_type"`
	ExternalID       string    `json:"external_id"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
}

// validIdentityReferenceEngines matches external-reference.schema.json's
// own "engine" enum exactly.
var validIdentityReferenceEngines = map[string]bool{"baobab-trade": true, "baobab-erp": true, "baobab-cms": true, "baobab-pulse": true}
var validIdentityReferenceStatuses = map[string]bool{"ACTIVE": true, "INACTIVE": true, "HISTORICAL": true}

func (r IdentityReference) Validate() error {
	if r.PrincipalID == "" {
		return errors.New("principal_id is required")
	}
	if !validIdentityReferenceEngines[r.Engine] {
		return errors.New("engine must be baobab-trade, baobab-erp, baobab-cms or baobab-pulse")
	}
	if r.ExternalType == "" || r.ExternalID == "" {
		return errors.New("external_type and external_id are required")
	}
	if !validIdentityReferenceStatuses[r.Status] {
		return errors.New("status must be ACTIVE, INACTIVE or HISTORICAL")
	}
	return nil
}

// NewIdentityReferenceID mints a new identity-reference identifier,
// following the same bare-UUID convention as NewPrincipalID.
func NewIdentityReferenceID() string { return NewUUIDv7() }
