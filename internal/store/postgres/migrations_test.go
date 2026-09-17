package postgres

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalMigrationFilesExist(t *testing.T) {
	checks := []struct {
		name    string
		needle  string
		context string
	}{
		{name: "000001_extensions_and_schemas.sql", needle: "CREATE SCHEMA IF NOT EXISTS registry", context: "extensions and schemas migration"},
		{name: "000002_canonical_registry.sql", needle: "CREATE TABLE IF NOT EXISTS registry.canonical_entity", context: "canonical registry migration"},
		{name: "000003_canonical_relationships.sql", needle: "CREATE TABLE IF NOT EXISTS registry.canonical_relationship", context: "canonical relationships migration"},
		{name: "000004_isolation_profiles.sql", needle: "CREATE TABLE IF NOT EXISTS policy.isolation_profile", context: "isolation profiles migration"},
		{name: "000005_engine_topology.sql", needle: "CREATE TABLE IF NOT EXISTS topology.engine", context: "engine topology migration"},
		{name: "000006_markets.sql", needle: "CREATE TABLE IF NOT EXISTS market.market", context: "market migration"},
		{name: "000007_digital_estates.sql", needle: "CREATE TABLE IF NOT EXISTS estate.digital_estate", context: "digital estate migration"},
		{name: "000008_capabilities.sql", needle: "CREATE TABLE IF NOT EXISTS capability.capability", context: "capability migration"},
		{name: "000009_mapping_scopes.sql", needle: "CREATE TABLE IF NOT EXISTS mapping.mapping_scope", context: "mapping scope migration"},
		{name: "000010_external_references.sql", needle: "CREATE TABLE IF NOT EXISTS registry.external_reference", context: "external references migration"},
		{name: "000011_canonical_mappings.sql", needle: "CREATE TABLE IF NOT EXISTS mapping.canonical_mapping", context: "canonical mappings migration"},
		{name: "000012_capability_bindings.sql", needle: "CREATE TABLE IF NOT EXISTS capability.capability_binding", context: "capability bindings migration"},
		{name: "000013_context_snapshots.sql", needle: "CREATE TABLE IF NOT EXISTS audit.context_snapshot", context: "context snapshots migration"},
		{name: "000014_audit.sql", needle: "CREATE TABLE IF NOT EXISTS audit.audit_record", context: "audit migration"},
		{name: "000015_messaging.sql", needle: "CREATE TABLE IF NOT EXISTS messaging.outbox", context: "messaging migration"},
		{name: "000016_idempotency_and_revisions.sql", needle: "CREATE TABLE IF NOT EXISTS system.idempotency_record", context: "idempotency and revisions migration"},
		{name: "000017_indexes_and_integrity.sql", needle: "CREATE INDEX IF NOT EXISTS canonical_entity_type_status_idx", context: "indexes and integrity migration"},
		{name: "000018_canonical_entity_versions.sql", needle: "ADD COLUMN IF NOT EXISTS version bigint", context: "canonical entity version migration"},
		{name: "000019_tenant_lifecycle.sql", needle: "CREATE TABLE tenants", context: "tenant lifecycle migration"},
		{name: "000020_audit_identity_extensions.sql", needle: "ADD COLUMN actor_id varchar(255)", context: "audit identity extensions migration"},
		{name: "000021_context_resolution_audit_extensions.sql", needle: "ADD COLUMN target varchar(255)", context: "context resolution audit extensions migration"},
		{name: "000022_canonical_mapping_temporal_integrity.sql", needle: "canonical_mapping_source_type_active_excl", context: "canonical mapping temporal integrity migration"},
		{name: "000023_outbox_tenant_identity.sql", needle: "ALTER COLUMN aggregate_id TYPE text", context: "outbox tenant identity migration"},
		{name: "000024_resolution_spine_invariants.sql", needle: "tenant_isolation_profile_active_excl", context: "resolution spine database invariants"},
		{name: "000028_capability_providers_and_binding_mode.sql", needle: "CREATE TABLE IF NOT EXISTS capability.capability_provider", context: "capability provider and binding mode migration"},
		{name: "000029_capability_scope_and_grant.sql", needle: "CREATE TABLE IF NOT EXISTS capability.capability_grant", context: "capability scope and grant migration"},
		{name: "000030_capability_lifecycle_maturity_and_dependency.sql", needle: "CREATE TABLE IF NOT EXISTS capability.capability_dependency", context: "capability lifecycle, maturity and dependency migration"},
		{name: "000031_resolved_context_store.sql", needle: "CREATE TABLE IF NOT EXISTS context.resolved_context", context: "resolved context store migration"},
		{name: "000032_workforce_membership.sql", needle: "CREATE TABLE IF NOT EXISTS identity.workforce_membership", context: "workforce membership migration"},
		{name: "000033_product_registry_and_subscription.sql", needle: "CREATE TABLE IF NOT EXISTS product.product_subscription", context: "product registry and subscription migration"},
		{name: "000034_capability_binding_scope_rewire.sql", needle: "REFERENCES capability.capability_scope(scope_id)", context: "capability binding scope rewire migration"},
		{name: "000035_retire_tenant_capability.sql", needle: "DROP TABLE IF EXISTS capability.tenant_capability", context: "tenant capability retirement migration"},
		{name: "000036_capability_composition_and_entitlement_projection.sql", needle: "CREATE TABLE IF NOT EXISTS capability.capability_composition", context: "capability composition and entitlement projection migration"},
		{name: "000037_market_participation_capabilities.sql", needle: "CREATE TABLE IF NOT EXISTS market.market_participation_capability", context: "market participation capabilities migration"},
	}

	for _, tc := range checks {
		path := filepath.Join("migrations", tc.name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing %s: %v", tc.name, err)
		}
		if !strings.Contains(string(contents), tc.needle) {
			t.Fatalf("%s does not contain %q", tc.name, tc.needle)
		}
	}
}
