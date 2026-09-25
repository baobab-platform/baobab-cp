package organisation

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// End-to-end ADR-BCP-018 provisioning and INTERNAL eligibility against real
// PostgreSQL through the production repository. Set TEST_DATABASE_URL to run.

type env struct {
	ctx   context.Context
	repo  *repository.PostgresRepository
	admin *pgxpool.Pool
	at    time.Time
}

func newEnv(t *testing.T) *env {
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
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	repo, err := repository.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)
	return &env{ctx: ctx, repo: repo, admin: admin, at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func token() string { return strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:20] }

func (e *env) canonicalOrganisation(t *testing.T) string {
	t.Helper()
	var id string
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO registry.canonical_entity (entity_type, status) VALUES ('ORGANISATION','active') RETURNING canonical_entity_id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// tenantWithOrganisation provisions a tenant + organisation as admission would.
func (e *env) tenantWithOrganisation(t *testing.T, relType domain.PlatformRelationshipType, basis string) (org string, res ProvisionResult, req ProvisionRequest) {
	t.Helper()
	le := "LE-" + strings.ToUpper(token())
	tenantID := "tn_" + token()
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, le); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO tenants(tenant_id,legal_entity_id,display_name,isolation_strategy,residency_region) VALUES ($1,$2,'T','row_level_security','af-south-1')`, tenantID, le); err != nil {
		t.Fatal(err)
	}
	org = e.canonicalOrganisation(t)
	req = ProvisionRequest{
		TenantID: tenantID, CanonicalEntityID: org, LegalEntityID: le, DisplayName: "Org " + org[:8],
		SourceAuthority: "control-plane-admission", PlatformRelType: relType, PlatformRelAuthority: "platform-governance",
		BasisRelationshipID: basis, AdmissionDecisionID: "adm_" + token(), EffectiveFrom: e.at, Actor: actor(),
	}
	res, err := (&Provisioner{Orgs: e.repo}).ProvisionTenantOrganisation(e.ctx, req)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	return org, res, req
}

// tenantFor creates a tenant whose singular legal entity is legalEntityID.
func (e *env) tenantFor(t *testing.T, legalEntityID string) string {
	t.Helper()
	tenantID := "tn_" + token()
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1) ON CONFLICT DO NOTHING`, legalEntityID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO tenants(tenant_id,legal_entity_id,display_name,isolation_strategy,residency_region) VALUES ($1,$2,'T','row_level_security','af-south-1')`, tenantID, legalEntityID); err != nil {
		t.Fatal(err)
	}
	return tenantID
}

func (e *env) evidence() repository.Evidence {
	return repository.Evidence{References: []string{"evd_" + token()}, VerifiedAt: e.at, Reason: "reviewed"}
}

func actor() repository.AuditActor {
	return repository.AuditActor{ActorID: "principal:reviewer", ActorType: "human", CorrelationID: domain.NewUUIDv7()}
}

func (e *env) outboxTypes(t *testing.T, correlationID string) []string {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT event_type FROM messaging.outbox WHERE correlation_id=$1::uuid ORDER BY occurred_at, event_type`, correlationID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var typ string
		if err := rows.Scan(&typ); err != nil {
			t.Fatal(err)
		}
		out = append(out, typ)
	}
	return out
}

func TestProvisioningIsIdempotentAndNeverVerifies(t *testing.T) {
	e := newEnv(t)
	org, first, req := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	if !first.OrganisationCreated || !first.LegalEntityProfileCreated {
		t.Fatalf("first provision should create profiles: %+v", first)
	}
	if got := e.outboxTypes(t, req.Actor.CorrelationID); len(got) != 3 {
		// organisation.created + tenant-legal-entity-mapping.activated +
		// tenant-organisation-mapping.activated; the PENDING platform
		// relationship is audited but publishes nothing.
		t.Fatalf("first provision published %v; want exactly three activation events", got)
	}
	p := &Provisioner{Orgs: e.repo}
	for i := 0; i < 3; i++ {
		req.Actor = actor()
		again, err := p.ProvisionTenantOrganisation(e.ctx, req)
		if err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
		if again.OrganisationCreated || again.LegalEntityProfileCreated ||
			again.TenantLegalEntityMappingID != first.TenantLegalEntityMappingID ||
			again.TenantOrganisationMapping != first.TenantOrganisationMapping ||
			again.PlatformRelationshipID != first.PlatformRelationshipID {
			t.Fatalf("replay %d diverged: %+v vs %+v", i, again, first)
		}
		if got := e.outboxTypes(t, req.Actor.CorrelationID); len(got) != 0 {
			t.Fatalf("replay %d published %v; a replay must publish nothing", i, got)
		}
	}
	for table, where := range map[string]string{
		"registry.tenant_legal_entity_mapping": "tenant_id='" + req.TenantID + "'",
		"registry.tenant_organisation_mapping": "tenant_id='" + req.TenantID + "'",
		"registry.platform_relationship":       "organisation_id='" + org + "'",
	} {
		var n int
		if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM `+table+` WHERE `+where).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s rows = %d, %v; want exactly 1 after replays", table, n, err)
		}
	}
	o, _ := e.repo.GetOrganisation(e.ctx, org)
	l, _ := e.repo.GetLegalEntityProfile(e.ctx, req.LegalEntityID)
	prs, _ := e.repo.ListPlatformRelationships(e.ctx, org, e.at.Add(time.Hour))
	if o.VerificationState == domain.VerificationVerified || l.VerificationState == domain.VerificationVerified ||
		len(prs) != 1 || prs[0].VerificationState == domain.VerificationVerified || prs[0].Status != domain.RelationshipStatusPending {
		t.Fatalf("provisioning manufactured verification: org=%s lep=%s prs=%+v", o.VerificationState, l.VerificationState, prs)
	}
}

func TestPrivilegedRelationshipsNeedServerAuthority(t *testing.T) {
	e := newEnv(t)
	p := &Provisioner{Orgs: e.repo}
	for _, typ := range []domain.PlatformRelationshipType{domain.PlatformRelOwner, domain.PlatformRelOperator, domain.PlatformRelGroupAffiliate} {
		_, err := p.ProvisionTenantOrganisation(e.ctx, ProvisionRequest{
			TenantID: "tn_x", CanonicalEntityID: "00000000-0000-0000-0000-000000000000", LegalEntityID: "LE-X",
			DisplayName: "X", SourceAuthority: "applicant-submission", PlatformRelType: typ, PlatformRelAuthority: "applicant-submission",
			Actor: actor(),
		})
		if err == nil || !strings.Contains(err.Error(), "server-side source_authority") {
			t.Errorf("%s from an applicant must be refused before any write, got %v", typ, err)
		}
	}
}

func TestInternalEligibilityFollowsVerifiedDirectedOwnership(t *testing.T) {
	e := newEnv(t)
	later := e.at.Add(time.Hour)
	// Unique platform per test run so owners from other tests don't leak in.
	platform := "test-platform-" + token()
	resolver := &EligibilityResolver{Orgs: e.repo, PlatformID: platform}

	owner := e.canonicalOrganisation(t)
	holding := e.canonicalOrganisation(t)
	sub := e.canonicalOrganisation(t)
	ownerRel := domain.PlatformRelationship{PlatformID: platform, OrganisationID: owner, RelationshipType: domain.PlatformRelOwner,
		VerificationState: domain.VerificationPendingReview, Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "platform-governance"}
	ownerRelID, err := e.repo.EnsurePlatformRelationship(e.ctx, ownerRel, actor())
	if err != nil {
		t.Fatal(err)
	}
	edge := func(src, dst string) string {
		id, err := e.repo.EnsureCorporateRelationship(e.ctx, domain.CorporateRelationship{
			SourceOrganisationID: src, TargetOrganisationID: dst, RelationshipType: domain.CorpRelOwns,
			DirectOrDerived: domain.CorporateFactDirect, VerificationState: domain.VerificationPendingReview,
			Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "shared-governance"}, actor())
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	e1, e2 := edge(owner, holding), edge(holding, sub)
	affiliateID, err := e.repo.EnsurePlatformRelationship(e.ctx, domain.PlatformRelationship{PlatformID: platform, OrganisationID: sub,
		RelationshipType: domain.PlatformRelGroupAffiliate, BasisRelationshipID: e2, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "platform-governance"}, actor())
	if err != nil {
		t.Fatal(err)
	}

	check := func(stage string, want bool) {
		t.Helper()
		got, err := resolver.ResolveInternalEligibility(e.ctx, sub, later)
		if err != nil || got != want {
			t.Fatalf("%s: eligible=%v err=%v; want %v", stage, got, err, want)
		}
	}
	check("nothing verified", false)
	for _, id := range []string{e1, e2} {
		if err := e.repo.VerifyCorporateRelationship(e.ctx, id, e.evidence(), actor()); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.repo.VerifyPlatformRelationship(e.ctx, affiliateID, e.evidence(), actor()); err != nil {
		t.Fatal(err)
	}
	check("owner's platform relationship still unverified", false)
	if err := e.repo.VerifyPlatformRelationship(e.ctx, ownerRelID, e.evidence(), actor()); err != nil {
		t.Fatal(err)
	}
	check("two-hop verified chain from verified owner", true)
	// The basis an INTERNAL classification records (ADR-BCP-017 section 13)
	// is exactly the qualifying relationship.
	basis, err := resolver.InternalEligibilityBasis(e.ctx, sub, later)
	if err != nil || len(basis) != 1 || basis[0].ID != affiliateID {
		t.Fatalf("eligibility basis = %+v, %v; want the affiliate relationship %s", basis, err, affiliateID)
	}
	if basis, err := resolver.InternalEligibilityBasis(e.ctx, sub, e.at.Add(-time.Hour)); err != nil || len(basis) != 0 {
		t.Fatalf("before the facts took effect there is no basis: %+v %v", basis, err)
	}

	// Direction matters: an organisation that owns the platform owner is not
	// thereby first-party.
	parentOfOwner := e.canonicalOrganisation(t)
	up := edge(parentOfOwner, owner)
	if err := e.repo.VerifyCorporateRelationship(e.ctx, up, e.evidence(), actor()); err != nil {
		t.Fatal(err)
	}
	fake, err := e.repo.EnsurePlatformRelationship(e.ctx, domain.PlatformRelationship{PlatformID: platform, OrganisationID: parentOfOwner,
		RelationshipType: domain.PlatformRelGroupAffiliate, BasisRelationshipID: up, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "platform-governance"}, actor())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.repo.VerifyPlatformRelationship(e.ctx, fake, e.evidence(), actor()); err != nil {
		t.Fatal(err)
	}
	if got, err := resolver.ResolveInternalEligibility(e.ctx, parentOfOwner, later); err != nil || got {
		t.Fatalf("owner's parent must not be INTERNAL-eligible via an upward edge: %v %v", got, err)
	}
}
