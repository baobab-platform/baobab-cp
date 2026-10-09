package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

// LA-04A intentionally permits mandate drafts but cannot activate them.
// PostgreSQL is the actual enforcement boundary, not a UI feature flag.
func TestOperatingLegalActorMandateActivationGate(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}

	tenantID := domain.NewTenantID()
	legalEntityID := "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:16])
	_, err = db.RegisterTenant(ctx, "la04-"+tenantID,
		basestore.RequestMetadata{
			ActorID: "la04-test", ActorType: "workload",
			CorrelationID: domain.NewUUIDv7(),
		}, domain.RegisterTenant{
			TenantID: tenantID,
			LegalEntityID: legalEntityID,
			Basis: domain.RegistrationBootstrap,
			BootstrapReason: "synthetic LA-04 mandate table test",
			BootstrapEvidenceReference: "test-fixture",
			DisplayName: "LA-04 synthetic operating organisation",
			IsolationStrategy: "row_level_security",
			ResidencyRegion: "af-south-1",
		}, nil)
	if err != nil {
		t.Fatalf("create test tenant: %v", err)
	}

	var operatingOrganisation, maker, checker string
	if err := db.pool.QueryRow(ctx, `SELECT organisation_id::text
		FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION'
		AND status='ACTIVE'`, tenantID).Scan(&operatingOrganisation); err != nil {
		t.Fatal(err)
	}
	for _, into := range []*string{&maker, &checker} {
		if err := db.pool.QueryRow(ctx,
			`INSERT INTO identity.principal(actor_type) VALUES('human')
			RETURNING principal_id::text`).Scan(into); err != nil {
			t.Fatal(err)
		}
	}
	params := []any{tenantID, operatingOrganisation, legalEntityID, maker, checker}
	const insert = `INSERT INTO registry.operating_legal_actor_mandate (
		tenant_id, operating_organisation_id, responsible_legal_entity_id,
		roles, activity_scope, market_scope, status,
		authority_basis_reference, evidence_references,
		legal_actor_verification_reference, effective_from,
		created_by, approved_by, approved_at, provenance
	) VALUES (
		$1, $2::uuid, $3,
		ARRAY['SELLER_OF_RECORD'], ARRAY['B2B_COFFEE_SALE'], ARRAY['ZA'], $6,
		'governance/independent-decision', ARRAY['evidence/case-1'],
		'registry/verified-legal-actor', now() - interval '1 hour',
		$4::uuid, $5::uuid, now(), 'synthetic-la04-test'
	) RETURNING mandate_id::text`
	var mandateID string
	activeArgs := append(append([]any(nil), params...), "ACTIVE")
	if err := db.pool.QueryRow(ctx, insert, activeArgs...).Scan(&mandateID); err == nil ||
		!strings.Contains(err.Error(), "operating_legal_actor_mandate_activation_gate") {
		t.Fatalf("ACTIVE without governed activation route was not blocked by dedicated gate: %v", err)
	}

	pendingArgs := append(append([]any(nil), params...), "PENDING")
	if err := db.pool.QueryRow(ctx, insert, pendingArgs...).Scan(&mandateID); err != nil {
		t.Fatalf("record inert mandate history: %v", err)
	}

	if _, err := db.pool.Exec(ctx, `UPDATE registry.operating_legal_actor_mandate
		SET market_scope=ARRAY['UG'] WHERE mandate_id=$1::uuid`, mandateID); err == nil {
		t.Fatal("mandate scope mutated without a new reviewed record")
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM registry.operating_legal_actor_mandate
		WHERE mandate_id=$1::uuid`, mandateID); err == nil {
		t.Fatal("mandate history was deleted")
	}

	if _, err := db.pool.Exec(ctx, `UPDATE registry.operating_legal_actor_mandate
		SET status='REVOKED', revoked_at=$2
		WHERE mandate_id=$1::uuid`, mandateID, time.Now().UTC()); err != nil {
		t.Fatalf("revocation of inert mandate failed: %v", err)
	}
	var status string
	if err := db.pool.QueryRow(ctx, `SELECT status FROM
		registry.operating_legal_actor_mandate WHERE mandate_id=$1::uuid`,
		mandateID).Scan(&status); err != nil || status != "REVOKED" {
		t.Fatalf("mandate did not preserve revoked history: %s %v", status, err)
	}
}
