package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nabhold/baobab-cp/internal/domain"
	basestore "github.com/nabhold/baobab-cp/internal/store"
)

// TestRegisterTenantRecordsUnverifiedOrganisationClaims proves ADR-BCP-018
// section 69 on the real registration path: registration input becomes
// UNVERIFIED claims, a second tenant for the same legal entity reuses the
// Organisation without rewriting it, and both tenants get explicit mappings.
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestRegisterTenantRecordsUnverifiedOrganisationClaims(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	legalEntityID := "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:16])
	register := func(displayName string) string {
		t.Helper()
		tenantID := domain.NewTenantID()
		command := domain.RegisterTenant{LegalEntityID: legalEntityID, TenantID: tenantID, DisplayName: displayName,
			IsolationStrategy: "row_level_security", ResidencyRegion: "af-south-1"}
		metadata := basestore.RequestMetadata{ActorID: "integration-test", ActorType: "workload", CorrelationID: "9f8b6e2a-0000-4000-8000-000000000045"}
		if _, err := store.RegisterTenant(ctx, "org-register-"+tenantID, metadata, command); err != nil {
			t.Fatalf("register tenant: %v", err)
		}
		return tenantID
	}
	first := register("Applicant-supplied name")
	second := register("Different name from a second registration")

	var orgID, orgState, orgName, orgSource, lepState, lepStatus, lepOrg string
	if err := store.pool.QueryRow(ctx, `
		SELECT p.canonical_entity_id::text, p.verification_state, p.display_name, p.source_authority,
		       l.verification_state, l.legal_status, l.organisation_id::text
		FROM registry.legal_entity_profile l
		JOIN registry.organisation_profile p ON p.canonical_entity_id = l.organisation_id
		WHERE l.legal_entity_id = $1`, legalEntityID).Scan(&orgID, &orgState, &orgName, &orgSource, &lepState, &lepStatus, &lepOrg); err != nil {
		t.Fatalf("read organisation structure: %v", err)
	}
	if orgState != "UNVERIFIED" || lepState != "UNVERIFIED" || lepStatus != "UNKNOWN" || orgSource != registrationSourceAuthority {
		t.Fatalf("registration must record unverified claims, got org=%s lep=%s legal_status=%s source=%s", orgState, lepState, lepStatus, orgSource)
	}
	if orgName != "Applicant-supplied name" || lepOrg != orgID {
		t.Fatalf("second registration rewrote the existing organisation: name=%q lep org=%s org=%s", orgName, lepOrg, orgID)
	}
	for _, tenantID := range []string{first, second} {
		var defaults, primaries int
		if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM registry.tenant_legal_entity_mapping WHERE tenant_id=$1 AND legal_entity_id=$2 AND mapping_role='DEFAULT' AND status='ACTIVE'`, tenantID, legalEntityID).Scan(&defaults); err != nil {
			t.Fatal(err)
		}
		if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM registry.tenant_organisation_mapping WHERE tenant_id=$1 AND organisation_id=$2::uuid AND mapping_role='PRIMARY_ORGANISATION'`, tenantID, orgID).Scan(&primaries); err != nil {
			t.Fatal(err)
		}
		if defaults != 1 || primaries != 1 {
			t.Fatalf("tenant %s: default legal-entity mappings=%d primary organisation mappings=%d; want 1 and 1", tenantID, defaults, primaries)
		}
	}
	var owner string
	if err := store.pool.QueryRow(ctx, `SELECT tenant_id FROM registry.canonical_entity WHERE canonical_entity_id=$1::uuid`, orgID).Scan(&owner); err != nil || owner != first {
		t.Fatalf("organisation owner tenant = %q, %v; want the first registering tenant %q (ADR-BCP-016 attestation)", owner, err, first)
	}
}
