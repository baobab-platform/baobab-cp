package domain_test

import (
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// These tests validate payloads this repository's own domain types produce
// against the actual JSON Schemas in a local baobab-platform/shared checkout, per
// the automation called for in
// docs/reconciliation/shared-control-plane-audit.md §6/§10.5. Set
// SHARED_CONTRACTS_DIR to a checkout of baobab-platform/shared at (or above) the
// commit pinned in contracts.lock.yaml; they are skipped otherwise.

func TestRegisterTenantMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "control-plane/v1/tenant-registration.schema.json")

	command := domain.RegisterTenant{
		LegalEntityID:     "THAMANI-GLOBAL",
		TenantID:          domain.NewTenantID(), // json:"-": must not appear in the encoded payload
		DisplayName:       "Thamani Global",
		IsolationStrategy: "schema_per_tenant",
		ResidencyRegion:   "af-south-1",
		RequestedProducts: []string{"baobab-trade"},
	}
	if err := command.Validate(); err != nil {
		t.Fatalf("command should be valid: %v", err)
	}
	contracttest.ValidateJSON(t, schema, command)
}

func TestResolvedContextMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "control-plane/v1/context-resolution.schema.json#/$defs/response")

	tier := "standard"
	resolved := domain.ResolvedContext{
		TenantID:        "tn_01k4example",
		EntityID:        "THAMANI-GLOBAL",
		LifecycleStatus: "active",
		ProductID:       "baobab-trade",
		Entitled:        true,
		EntitlementTier: &tier,
		CacheTTLSeconds: 15,
		ResolvedAt:      time.Now().UTC(),
		CorrelationID:   "9f8b6e2a-0000-4000-8000-000000000001",
	}
	contracttest.ValidateJSON(t, schema, resolved)
}

// TestMappingMatchesSharedSchema and TestMappingScopeMatchesSharedSchema
// validate against contracts/control-plane/v1/canonical-mapping.schema.json
// per docs/reconciliation/platform-resolution-spine-audit.md Gate 1
// ("consolidate canonical Go model and shared wire schemas"). Both use a
// hand-built example, like every other test in this file: the IDs this
// wire schema requires (map_..., scope_..., tn_...) are a different opaque
// scheme than what internal/repository/postgres.go's real mapping.
// canonical_mapping/mapping_scope tables actually mint (gen_random_uuid()
// primary keys) -- so no example loadable from this repository's own
// Postgres implementation today would pass this schema's mapping_id/
// scope_id pattern regardless of field-shape reconciliation. That's a
// separate, ID-generation-scheme gap, not something this pass's field-level
// reconciliation fixes; flagged rather than silently worked around.

func TestMappingMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "control-plane/v1/canonical-mapping.schema.json#/$defs/mapping")

	mapping := domain.Mapping{
		ID:                  "map_zuribeansproduct001",
		MappingType:         "IDENTITY",
		TenantID:            "tn_zuribeans",
		LegalEntityID:       "ZURIBEANS-ZA",
		CanonicalEntityID:   "product:zuribeans:arabica-60kg",
		ExternalReferenceID: "ref_medusa00123",
		ScopeID:             "scope_zuribeansprod",
		Direction:           "CANONICAL_TO_EXTERNAL",
		Cardinality:         "ONE_TO_ONE",
		Authority:           "trade",
		Confidence:          "CONFIRMED",
		ResolutionPriority:  10,
		Status:              "ACTIVE",
		EffectiveFrom:       "2026-01-01T00:00:00Z",
		Revision:            1,
		CreatedAt:           "2026-01-01T00:00:00Z",
		CreatedBy:           "system:baobab-cp-migration",
	}
	if err := mapping.Validate(); err != nil {
		t.Fatalf("mapping should be valid: %v", err)
	}
	contracttest.ValidateJSON(t, schema, mapping)
}

func TestMappingScopeMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "control-plane/v1/canonical-mapping.schema.json#/$defs/mappingScope")

	scope := domain.MappingScope{
		ScopeID:           "scope_zuribeansprod",
		TenantID:          "tn_zuribeans",
		LegalEntityID:     "ZURIBEANS-ZA",
		MarketID:          "kenya_b2b",
		Country:           "KE",
		EstateID:          "zuribeans_estate",
		DigitalPropertyID: "thamani_co_ke",
		ChannelID:         "web",
		Currency:          "KES",
		Locale:            "en-KE",
		CatalogueID:       "default_catalogue",
		CustomerSegmentID: "wholesale",
		EngineID:          "medusa",
		EngineInstanceID:  "medusa_prod_af",
		Environment:       "production",
		DeploymentRegion:  "af_south_1",
		IncludeCountries:  []string{"KE", "UG"},
		ExcludeCountries:  []string{"TZ"},
		CreatedAt:         "2026-01-01T00:00:00Z",
		UpdatedAt:         "2026-01-01T00:00:00Z",
	}
	contracttest.ValidateJSON(t, schema, scope)
}

// TestPrincipalMatchesSharedSchema and TestExternalIdentityMatchesSharedSchema
// validate domain.Principal/domain.ExternalIdentity (Gate IAM-3 phase 1,
// docs/governance/gate-iam-3-canonical-identity-scope.md) against
// contracts/identity/v1/principal.schema.json and external-identity.schema.json
// -- the two shared contracts ADR-0004 §75's suggested CanonicalIdentity/
// ExternalIdentity data model already matched field-for-field before this
// phase's Go types existed.

func TestPrincipalMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "identity/v1/principal.schema.json")

	principal := domain.Principal{
		ID:        domain.NewPrincipalID(),
		ActorType: "human",
		Status:    "ACTIVE",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := principal.Validate(); err != nil {
		t.Fatalf("principal should be valid: %v", err)
	}
	contracttest.ValidateJSON(t, schema, principal)
}

func TestExternalIdentityMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "identity/v1/external-identity.schema.json")

	lastSeen := time.Now().UTC()
	external := domain.ExternalIdentity{
		ID:           domain.NewExternalIdentityID(),
		PrincipalID:  domain.NewPrincipalID(),
		Issuer:       "https://iam.nabhold.com/realms/baobab",
		Subject:      "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		ProviderType: "keycloak",
		Status:       "ACTIVE",
		CreatedAt:    time.Now().UTC(),
		LastSeenAt:   &lastSeen,
	}
	if err := external.Validate(); err != nil {
		t.Fatalf("external identity should be valid: %v", err)
	}
	contracttest.ValidateJSON(t, schema, external)
}

// TestIdentityReferenceMatchesSharedSchema validates domain.IdentityReference
// (Gate IAM-3 phase 5) against contracts/identity/v1/external-reference.
// schema.json -- another shared contract (ADR-0004 §23-27's "engine-native
// identity" concept) that already matched field-for-field before this
// phase's Go type existed.
func TestIdentityReferenceMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "identity/v1/external-reference.schema.json")

	reference := domain.IdentityReference{
		ID:               domain.NewIdentityReferenceID(),
		PrincipalID:      domain.NewPrincipalID(),
		Engine:           "baobab-trade",
		EngineInstanceID: "af-south-1-production",
		ExternalType:     "customer",
		ExternalID:       "C-100",
		Status:           "ACTIVE",
		CreatedAt:        time.Now().UTC(),
	}
	if err := reference.Validate(); err != nil {
		t.Fatalf("identity reference should be valid: %v", err)
	}
	contracttest.ValidateJSON(t, schema, reference)
}
