package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

func TestFederationGovernanceTargetRegistrationRequiresCurrentTopologyAndScope(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := "tn_" + suffix
	legalEntityID := "LE-MP2C-" + strings.ToUpper(suffix[:8])
	orgID := domain.NewUUIDv7()
	estateID := "estate_" + suffix
	refID := "ref_" + suffix
	digest := "sha256:" + strings.Repeat("a", 64)

	if _, err := admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntityID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference,
			tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region)
		VALUES ('BOOTSTRAP','MP2-C integration fixture','test-fixture',$1,$2,'MP2-C governance','row_level_security','af-south-1')`,
		tenantID, legalEntityID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO registry.canonical_entity(canonical_entity_id, entity_type, status)
		VALUES ($1::uuid,'ORGANISATION','active')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO registry.tenant_organisation_mapping(
			tenant_id, organisation_id, mapping_role, status, effective_from, provenance)
		VALUES ($1,$2::uuid,'PRIMARY_ORGANISATION','ACTIVE',$3,'mp2c-test')`,
		tenantID, orgID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := admin.Exec(ctx, `
		INSERT INTO topology.engine(code,name) VALUES ('baobab-iam','baobab-iam')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var engineID string
	if err := admin.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code='baobab-iam'`).Scan(&engineID); err != nil {
		t.Fatal(err)
	}
	var instanceID, instanceKey string
	if err := admin.QueryRow(ctx, `
		INSERT INTO topology.engine_instance(engine_id,region,environment,status)
		VALUES ($1::uuid,'af-south-1','staging','ACTIVE')
		RETURNING engine_instance_id::text, engine_instance_key`, engineID).Scan(&instanceID, &instanceKey); err != nil {
		t.Fatal(err)
	}
	var providerRowID, providerID string
	if err := admin.QueryRow(ctx, `
		INSERT INTO capability.capability_provider(provider_key,name,provider_type,engine_id,status,ownership)
		VALUES ($1,'MP2-C governance target','ADAPTER',$2::uuid,'ACTIVE','baobab-platform/baobab-iam')
		RETURNING provider_id::text, canonical_provider_id`,
		"baobab-iam.gov-"+suffix, engineID).Scan(&providerRowID, &providerID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO capability.capability(code,name)
		VALUES ('identity.authentication.perform','Identity authentication')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var capabilityID string
	if err := admin.QueryRow(ctx, `SELECT capability_id::text FROM capability.capability WHERE code='identity.authentication.perform'`).Scan(&capabilityID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO capability.provider_capability_support(
			provider_id,capability_id,contract_versions,status,effective_from)
		VALUES ($1::uuid,$2::uuid,ARRAY[1],'ACTIVE',$3)`,
		providerRowID, capabilityID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	var scopeID string
	if err := admin.QueryRow(ctx, `
		INSERT INTO capability.capability_scope(
			tenant_id,organisation_id,digital_estate_id,environment)
		VALUES ($1,$2,$3,'staging') RETURNING scope_id::text`,
		tenantID, orgID, estateID).Scan(&scopeID); err != nil {
		t.Fatal(err)
	}
	var bindingID string
	if err := admin.QueryRow(ctx, `
		INSERT INTO capability.capability_binding(
			capability_id,engine_instance_id,scope_id,binding_mode,status,
			contract_version,effective_from,provider_id)
		VALUES ($1::uuid,$2::uuid,$3::uuid,'PRIMARY','ACTIVE','1',$4,$5::uuid)
		RETURNING id::text`,
		capabilityID, instanceID, scopeID, now.Add(-time.Minute), providerRowID).Scan(&bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO mapping.external_reference(
			external_reference_id,system_namespace,engine_id,engine_instance_id,environment,
			native_entity_type,native_id,source_authority,fingerprint,status,first_seen_at,last_verified_at)
		VALUES ($1,'baobab_iam','baobab-iam',$2,'staging','federation_configuration',$3,
			'reconciliation',$4,'active',$5,$5)`,
		refID, instanceKey, "native-"+suffix, digest, now); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		tx, err := admin.Begin(ctx)
		if err != nil {
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return
		}
		_, _ = tx.Exec(ctx, `DELETE FROM mapping.external_reference WHERE external_reference_id=$1`, refID)
		_, _ = tx.Exec(ctx, `DELETE FROM capability.capability_binding WHERE id=$1::uuid`, bindingID)
		_, _ = tx.Exec(ctx, `DELETE FROM capability.provider_capability_support WHERE provider_id=$1::uuid AND capability_id=$2::uuid`, providerRowID, capabilityID)
		_, _ = tx.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id=$1::uuid`, scopeID)
		_, _ = tx.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_id=$1::uuid`, providerRowID)
		_, _ = tx.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id=$1::uuid`, instanceID)
		_, _ = tx.Exec(ctx, `DELETE FROM registry.tenant_organisation_mapping WHERE tenant_id=$1`, tenantID)
		_, _ = tx.Exec(ctx, `DELETE FROM registry.canonical_entity WHERE canonical_entity_id=$1::uuid`, orgID)
		_, _ = tx.Exec(ctx, `DELETE FROM tenants WHERE tenant_id=$1`, tenantID)
		_, _ = tx.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id=$1`, legalEntityID)
		_ = tx.Commit(ctx)
	})

	query := FederationGovernanceTargetQuery{
		ReferenceID: refID, Kind: "federation_configuration",
		SystemNamespace: "baobab_iam", EngineCode: "baobab-iam",
		NativeEntityType: "federation_configuration",
		ProviderID: providerID, EngineInstanceID: instanceKey,
		OrganisationID: orgID, DigitalEstateID: estateID, Environment: "staging",
	}
	got, err := repo.ReadFederationGovernanceTargetRegistration(ctx, query, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != digest || got.ReferenceID != refID || got.Environment != "staging" || got.TenantID != tenantID {
		t.Fatalf("registration = %#v", got)
	}

	for name, mutate := range map[string]func(*FederationGovernanceTargetQuery){
		"wrong organisation": func(q *FederationGovernanceTargetQuery) { q.OrganisationID = domain.NewUUIDv7() },
		"wrong estate":       func(q *FederationGovernanceTargetQuery) { q.DigitalEstateID = "estate_other" },
		"wrong environment":  func(q *FederationGovernanceTargetQuery) { q.Environment = "production" },
		"wrong provider":     func(q *FederationGovernanceTargetQuery) { q.ProviderID = "provider_bbbbbbbb" },
		"wrong instance":     func(q *FederationGovernanceTargetQuery) { q.EngineInstanceID = "ei_bbbbbbbb" },
	} {
		t.Run(name, func(t *testing.T) {
			q := query
			mutate(&q)
			if _, err := repo.ReadFederationGovernanceTargetRegistration(ctx, q, now); !errors.Is(err, ErrFederationGovernanceTargetNotFound) {
				t.Fatalf("got %v, want ErrFederationGovernanceTargetNotFound", err)
			}
		})
	}

	if _, err := admin.Exec(ctx, `UPDATE mapping.external_reference SET source_authority='manual-import' WHERE external_reference_id=$1`, refID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadFederationGovernanceTargetRegistration(ctx, query, now); !errors.Is(err, ErrFederationGovernanceTargetNotFound) {
		t.Fatalf("manual-import target accepted: %v", err)
	}
}
