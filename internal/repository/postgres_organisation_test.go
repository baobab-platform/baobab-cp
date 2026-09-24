package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// ADR-BCP-018 persistence invariants (§117-119) against real PostgreSQL.
// Set TEST_DATABASE_URL to run; skipped otherwise.

type orgFixture struct {
	ctx   context.Context
	repo  *PostgresRepository
	admin *pgxpool.Pool
	at    time.Time
}

func newOrgFixture(t *testing.T) *orgFixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(admin.Close)
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(repo.Close)
	return &orgFixture{ctx: ctx, repo: repo, admin: admin, at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// organisation creates an ORGANISATION canonical entity plus an unverified profile.
func (f *orgFixture) organisation(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO registry.canonical_entity (entity_type, status) VALUES ($1, 'active') RETURNING canonical_entity_id::text`,
		domain.EntityTypeOrganisation).Scan(&id); err != nil {
		t.Fatalf("canonical entity: %v", err)
	}
	if _, err := f.repo.EnsureOrganisation(f.ctx, domain.Organisation{
		CanonicalEntityID: id, DisplayName: name, VerificationState: domain.VerificationUnverified,
		SourceAuthority: "test", Status: "ACTIVE", EffectiveFrom: f.at,
	}); err != nil {
		t.Fatalf("ensure organisation: %v", err)
	}
	return id
}

func (f *orgFixture) tenant(t *testing.T, legalEntityID string) string {
	t.Helper()
	tenantID := "tn_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:24]
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1) ON CONFLICT DO NOTHING`, legalEntityID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO tenants(tenant_id,legal_entity_id,display_name,isolation_strategy,residency_region) VALUES ($1,$2,'T','row_level_security','af-south-1')`,
		tenantID, legalEntityID); err != nil {
		t.Fatal(err)
	}
	return tenantID
}

func (f *orgFixture) evidence() Evidence {
	return Evidence{References: []string{"evd_share_register"}, VerifiedBy: "principal:reviewer", VerifiedAt: f.at}
}

func (f *orgFixture) edge(t *testing.T, source, target string) string {
	t.Helper()
	id, err := f.repo.EnsureCorporateRelationship(f.ctx, domain.CorporateRelationship{
		SourceOrganisationID: source, TargetOrganisationID: target, RelationshipType: domain.CorpRelOwns,
		DirectOrDerived: domain.CorporateFactDirect, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: f.at, SourceAuthority: "admission-review",
	})
	if err != nil {
		t.Fatalf("ensure edge: %v", err)
	}
	return id
}

func uniqueLE() string {
	return "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:16])
}

func TestPostgresOrganisationRoundTripsContractFields(t *testing.T) {
	f := newOrgFixture(t)
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO registry.canonical_entity (entity_type, status) VALUES ('ORGANISATION','active') RETURNING canonical_entity_id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	want := domain.Organisation{
		CanonicalEntityID: id, DisplayName: "Acme Foods Ltd", OfficialName: "Acme Foods Limited",
		TradingNames: []string{"Acme Fresh"}, OrganisationForm: domain.OrgFormCompany, Jurisdiction: "KE",
		VerificationState: domain.VerificationUnverified, SourceAuthority: "applicant-submission", Status: "ACTIVE",
		EffectiveFrom: f.at,
		Identifiers:   []domain.OrganisationIdentifier{{Type: "COMPANY_REGISTRATION", Value: "PVT-1", IssuingJurisdiction: "KE"}},
		Addresses:     []domain.OrganisationAddress{{AddressType: "REGISTERED", Lines: []string{"1 Road"}, CountryCode: "KE"}},
		Metadata:      map[string]any{"k": "v"},
	}
	if created, err := f.repo.EnsureOrganisation(f.ctx, want); err != nil || !created {
		t.Fatalf("ensure: created=%v err=%v", created, err)
	}
	// A replay that tries to claim VERIFIED is ignored: identity is never overwritten.
	replay := want
	replay.VerificationState = domain.VerificationPendingReview
	replay.DisplayName = "Renamed"
	if created, err := f.repo.EnsureOrganisation(f.ctx, replay); err != nil || created {
		t.Fatalf("replay: created=%v err=%v", created, err)
	}
	got, err := f.repo.GetOrganisation(f.ctx, id)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != want.DisplayName || got.VerificationState != domain.VerificationUnverified ||
		got.OfficialName != want.OfficialName || got.OrganisationForm != want.OrganisationForm ||
		got.Jurisdiction != "KE" || len(got.TradingNames) != 1 || len(got.Identifiers) != 1 ||
		got.Identifiers[0].Value != "PVT-1" || len(got.Addresses) != 1 || got.Metadata["k"] != "v" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestPostgresVerifiedRequiresEvidenceAtTheDatabase(t *testing.T) {
	f := newOrgFixture(t)
	a, b := f.organisation(t, "A"), f.organisation(t, "B")
	_, err := f.admin.Exec(f.ctx, `INSERT INTO registry.corporate_relationship
		(source_organisation_id, target_organisation_id, relationship_type, verification_state, status, effective_from, source_authority)
		VALUES ($1::uuid, $2::uuid, 'OWNS', 'VERIFIED', 'ACTIVE', now(), 'applicant-submission')`, a, b)
	if err == nil || !strings.Contains(err.Error(), "corporate_relationship_verified_has_evidence") {
		t.Fatalf("VERIFIED corporate relationship without evidence must be rejected, got %v", err)
	}
	_, err = f.admin.Exec(f.ctx, `INSERT INTO registry.platform_relationship
		(platform_id, organisation_id, relationship_type, verification_state, status, effective_from, source_authority, basis_relationship_id)
		VALUES ('baobab-platform', $1::uuid, 'PLATFORM_GROUP_AFFILIATE', 'UNVERIFIED', 'PENDING', now(), 'x', NULL)`, a)
	if err == nil || !strings.Contains(err.Error(), "platform_relationship_affiliate_has_basis") {
		t.Fatalf("affiliate without basis must be rejected, got %v", err)
	}
}

func TestPostgresEnsureCorporateRelationshipConvergesAndVerifies(t *testing.T) {
	f := newOrgFixture(t)
	a, b := f.organisation(t, "A"), f.organisation(t, "B")
	first := f.edge(t, a, b)
	if second := f.edge(t, a, b); second != first {
		t.Fatalf("replay minted a second relationship: %s vs %s", first, second)
	}
	if !strings.HasPrefix(first, "crel_") {
		t.Fatalf("id %q is not a contract id", first)
	}
	if err := f.repo.VerifyCorporateRelationship(f.ctx, first, Evidence{VerifiedBy: "principal:x", VerifiedAt: f.at}); err == nil {
		t.Fatal("verification without evidence references must fail")
	}
	if err := f.repo.VerifyCorporateRelationship(f.ctx, first, f.evidence()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := f.repo.VerifyCorporateRelationship(f.ctx, first, f.evidence()); err == nil {
		t.Fatal("verifying an already verified relationship must fail rather than silently overwrite evidence")
	}
	rels, err := f.repo.ListCorporateRelationshipsByOrganisation(f.ctx, b, f.at.Add(time.Hour))
	if err != nil || len(rels) != 1 {
		t.Fatalf("list: %v %v", rels, err)
	}
	r := rels[0]
	if r.ID != first || r.VerificationState != domain.VerificationVerified || r.Status != domain.RelationshipStatusActive ||
		r.VerifiedBy != "principal:reviewer" || len(r.EvidenceReferences) != 1 || r.DirectOrDerived != domain.CorporateFactDirect {
		t.Fatalf("unexpected relationship after verification: %+v", r)
	}
}

func TestPostgresControlAncestryIsDirectedMultiHopAndCycleSafe(t *testing.T) {
	f := newOrgFixture(t)
	owner, mid, leaf, stranger := f.organisation(t, "Owner"), f.organisation(t, "Mid"), f.organisation(t, "Leaf"), f.organisation(t, "Stranger")
	e1, e2 := f.edge(t, owner, mid), f.edge(t, mid, leaf)
	back := f.edge(t, leaf, owner)          // cycle
	unverified := f.edge(t, stranger, leaf) // stays PENDING_REVIEW
	for _, id := range []string{e1, e2, back} {
		if err := f.repo.VerifyCorporateRelationship(f.ctx, id, f.evidence()); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.repo.ListCorporateControlAncestry(f.ctx, leaf, f.at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range got {
		ids[r.ID] = true
	}
	if !ids[e1] || !ids[e2] || !ids[back] || ids[unverified] || len(ids) != 3 {
		t.Fatalf("ancestry = %v; want exactly the verified edges into leaf (e1, e2 and the cycle edge)", ids)
	}
	// Outbound-only organisation has no ancestry except via the cycle.
	if got, err := f.repo.ListCorporateControlAncestry(f.ctx, stranger, f.at.Add(time.Hour)); err != nil || len(got) != 0 {
		t.Fatalf("stranger ancestry = %v, %v; want none", got, err)
	}
}

func TestPostgresLegalEntityProfileIsNeverRepointed(t *testing.T) {
	f := newOrgFixture(t)
	a, b := f.organisation(t, "A"), f.organisation(t, "B")
	le := uniqueLE()
	lep := domain.LegalEntityProfile{LegalEntityID: le, OrganisationID: a, LegalName: "A Ltd", LegalStatus: domain.LegalStatusUnknown,
		SourceAuthority: "test", VerificationState: domain.VerificationUnverified, EffectiveFrom: f.at}
	if created, err := f.repo.EnsureLegalEntityProfile(f.ctx, lep); err != nil || !created {
		t.Fatalf("ensure: %v %v", created, err)
	}
	if created, err := f.repo.EnsureLegalEntityProfile(f.ctx, lep); err != nil || created {
		t.Fatalf("replay: %v %v", created, err)
	}
	lep.OrganisationID = b
	if _, err := f.repo.EnsureLegalEntityProfile(f.ctx, lep); !errors.Is(err, ErrOrganisationConflict) {
		t.Fatalf("re-pointing a legal entity must conflict, got %v", err)
	}
	if err := f.repo.VerifyLegalEntityProfile(f.ctx, le, f.evidence()); err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.GetLegalEntityProfile(f.ctx, le)
	if err != nil || got.OrganisationID != a || got.VerificationState != domain.VerificationVerified || got.VerifiedAt == nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestPostgresDefaultLegalEntityMappingReplayAndSwitch(t *testing.T) {
	f := newOrgFixture(t)
	le1, le2 := uniqueLE(), uniqueLE()
	tenantID := f.tenant(t, le1)
	id1, err := f.repo.EnsureDefaultTenantLegalEntityMapping(f.ctx, tenantID, le1, "test", f.at)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := f.repo.EnsureDefaultTenantLegalEntityMapping(f.ctx, tenantID, le1, "test", f.at); err != nil || again != id1 {
		t.Fatalf("replay: %q %v; want %q", again, err, id1)
	}
	id2, err := f.repo.EnsureDefaultTenantLegalEntityMapping(f.ctx, tenantID, le2, "test", f.at.Add(time.Hour))
	if err != nil || id2 == id1 {
		t.Fatalf("switch: %q %v", id2, err)
	}
	var projected string
	if err := f.admin.QueryRow(f.ctx, `SELECT legal_entity_id FROM tenants WHERE tenant_id=$1`, tenantID).Scan(&projected); err != nil || projected != le2 {
		t.Fatalf("tenants.legal_entity_id = %q, %v; want %q", projected, err, le2)
	}
	later, err := f.repo.ListTenantLegalEntityMappings(f.ctx, tenantID, f.at.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := domain.ProjectTenantLegalEntityID(later, f.at.Add(2*time.Hour)); got != le2 {
		t.Fatalf("projection = %q, want %q (mappings %+v)", got, le2, later)
	}
	var history int
	if err := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM registry.tenant_legal_entity_mapping WHERE tenant_id=$1 AND status='ENDED'`, tenantID).Scan(&history); err != nil || history != 1 {
		t.Fatalf("previous default must be kept as ENDED history, got %d %v", history, err)
	}
}

func TestPostgresMappingsAndPlatformRelationshipsConverge(t *testing.T) {
	f := newOrgFixture(t)
	org := f.organisation(t, "Org")
	tenantID := f.tenant(t, uniqueLE())
	m := domain.TenantOrganisationMapping{TenantID: tenantID, OrganisationID: org, MappingRole: domain.TenantOrgRolePrimary,
		Status: domain.RelationshipStatusActive, EffectiveFrom: f.at, Provenance: "test"}
	a, err := f.repo.EnsureTenantOrganisationMapping(f.ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := f.repo.EnsureTenantOrganisationMapping(f.ctx, m); err != nil || b != a {
		t.Fatalf("mapping replay: %q %v; want %q", b, err, a)
	}
	pr := domain.PlatformRelationship{PlatformID: "baobab-platform", OrganisationID: org, RelationshipType: domain.PlatformRelExternalClient,
		VerificationState: domain.VerificationPendingReview, Status: domain.RelationshipStatusPending, EffectiveFrom: f.at, SourceAuthority: "admission"}
	p1, err := f.repo.EnsurePlatformRelationship(f.ctx, pr)
	if err != nil {
		t.Fatal(err)
	}
	if p2, err := f.repo.EnsurePlatformRelationship(f.ctx, pr); err != nil || p2 != p1 {
		t.Fatalf("platform relationship replay: %q %v; want %q", p2, err, p1)
	}
	pr.RelationshipType = domain.PlatformRelPartner
	if p3, err := f.repo.EnsurePlatformRelationship(f.ctx, pr); err != nil || p3 == p1 {
		t.Fatalf("concurrent relationship types must coexist: %q %v", p3, err)
	}
}
