package development

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This runs against the real PG17 CI service, but creates a separate disposable
// database. It cannot seed or adopt the other packages' test data.
func TestDevelopmentBootstrapPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var exists bool
	if err = admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='baobab_dev')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		if _, err = admin.Exec(ctx, `CREATE ROLE baobab_dev LOGIN SUPERUSER`); err != nil {
			t.Fatal(err)
		}
	}
	database := "baobab_dev_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")
	name := pgx.Identifier{database}.Sanitize()
	if _, err = admin.Exec(ctx, `CREATE DATABASE `+name+` OWNER baobab_dev`); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, `DROP DATABASE `+name+` WITH (FORCE)`)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("baobab_dev")
	u.Path = "/" + database
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if CheckDatabase(ctx, pool) == nil {
		t.Fatal("unmarked development database accepted")
	}
	if _, err = admin.Exec(ctx, `COMMENT ON DATABASE `+name+` IS '`+DatabaseMarker+`'`); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := contracts.ReadEmbedded("development/v1/examples/nabhold-group.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg := fixtureConfig()
	result, err := Bootstrap(ctx, pool, cfg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SyntheticOnly || result.ProtectedAcceptance || len(result.Enterprises) != 5 || result.MakerID == result.CheckerID {
		t.Fatalf("invalid bootstrap result: %+v", result)
	}
	again, err := Bootstrap(ctx, pool, cfg, raw)
	if err != nil || again.Enterprises[0].TenantID != result.Enterprises[0].TenantID {
		t.Fatalf("idempotent replay: %v", err)
	}
	drift := cfg
	drift.CheckerSubject = "different-synthetic-checker"
	if _, err = Bootstrap(ctx, pool, drift, raw); err == nil {
		t.Fatal("changed replay accepted")
	}
	expired := cfg
	expired.Now = result.ExpiresAt.Add(time.Second)
	if _, err = Bootstrap(ctx, pool, expired, raw); err == nil {
		t.Fatal("expired lease renewed")
	}
	protected := cfg
	protected.Environment = "staging"
	if _, err = Bootstrap(ctx, pool, protected, raw); err == nil {
		t.Fatal("staging bootstrap accepted")
	}
	seen := map[string]bool{}
	repo, err := repository.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for _, e := range result.Enterprises {
		if seen[e.TenantID] || !domain.ValidTenantID(e.TenantID) {
			t.Fatal("tenant identity reused/invalid")
		}
		seen[e.TenantID] = true
		var primary, verified int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM registry.tenant_organisation_mapping WHERE tenant_id=$1 AND organisation_id=$2 AND mapping_role='PRIMARY_ORGANISATION' AND status='ACTIVE'`, e.TenantID, e.OrganisationID).Scan(&primary); err != nil || primary != 1 {
			t.Fatalf("PRIMARY mapping: %v", err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM registry.legal_entity_profile WHERE legal_entity_id=$1 AND verification_state='VERIFIED'`, e.LegalEntityID).Scan(&verified); err != nil || verified != 0 {
			t.Fatal("synthetic company misrepresented as verified")
		}
		if e.Key == "external-client" {
			if e.EstateID != "" || e.PrincipalID != "" {
				t.Fatal("external client forced to have estate")
			}
			continue
		}
		allowed, err := repo.DevelopmentTenantAllowed(ctx, e.PrincipalID, e.ClientID, e.TenantID, cfg.Now)
		if err != nil || !allowed {
			t.Fatalf("development own-tenant assignment: %v", err)
		}
		if allowed, err = repo.DevelopmentTenantAllowed(ctx, e.PrincipalID, e.ClientID, result.Enterprises[4].TenantID, cfg.Now); err != nil || allowed {
			t.Fatal("development cross-tenant assignment accepted")
		}
		if allowed, err = repo.DevelopmentTenantAllowed(ctx, e.PrincipalID, e.ClientID, e.TenantID, result.ExpiresAt); err != nil || allowed {
			t.Fatal("expired development assignment accepted")
		}
		svc := service.ContextResolutionService{Identity: service.IdentityService{Repository: repo}, Tenants: store, Canonical: repo, Mappings: repo, Markets: repo}
		principal := auth.Principal{Issuer: cfg.Issuer, Subject: e.ClientID, ClientID: e.ClientID, ActorType: "workload", TokenID: "synthetic-provider-evidence", Scopes: map[string]struct{}{"context:resolve": {}}}
		_, boundedContext, err := svc.WithCountry(e.Markets[0]).Resolve(ctx, principal, e.TenantID, e.OrganisationID, domain.NewUUIDv7(), cfg.Now)
		if err != nil {
			t.Fatalf("real CP own-context service: %v", err)
		}
		if boundedContext.ExpiresAt == nil || boundedContext.ExpiresAt.Sub(result.ExpiresAt).Abs() > time.Microsecond {
			t.Fatalf("development context must expire with bootstrap lease: %+v", boundedContext.ExpiresAt)
		}
		lease, err := repo.DevelopmentTenantLease(ctx, e.PrincipalID, e.ClientID, e.TenantID, cfg.Now)
		if err != nil || lease.Sub(result.ExpiresAt).Abs() > time.Microsecond {
			t.Fatalf("authoritative tenant lease: expiry=%s err=%v", lease, err)
		}
		if _, err = repo.DevelopmentTenantLease(ctx, e.PrincipalID, e.ClientID, e.TenantID, result.ExpiresAt); err == nil {
			t.Fatal("expired run still grants a development tenant lease")
		}
		if _, err = repo.DevelopmentTenantLease(ctx, e.PrincipalID, e.ClientID, result.Enterprises[4].TenantID, cfg.Now); err == nil {
			t.Fatal("sibling tenant granted a development lease")
		}
		if _, _, err = svc.WithCountry("ZA").Resolve(ctx, principal, result.Enterprises[4].TenantID, result.Enterprises[4].OrganisationID, domain.NewUUIDv7(), cfg.Now); err == nil {
			t.Fatal("CP service accepted neighbour context")
		}
		own, err := repo.GetWorkforceMembership(ctx, e.PrincipalID, e.TenantID)
		if err != nil || own.Status != "ACTIVE" {
			t.Fatalf("own tenant relationship: %v", err)
		}
		other := result.Enterprises[4].TenantID
		if _, err = repo.GetWorkforceMembership(ctx, e.PrincipalID, other); err == nil {
			t.Fatal("workload inherited external tenant membership")
		}
	}
	// The topology table records lifecycle status, not a health_status column.
	// A fixture may register PROVISIONING instances but must never seed an
	// ACTIVE or otherwise purportedly qualified runtime provider.
	var notProvisioning int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM topology.engine_instance WHERE status <> 'PROVISIONING'`).Scan(&notProvisioning); err != nil {
		t.Fatalf("query fixture provider lifecycle: %v", err)
	} else if notProvisioning != 0 {
		t.Fatalf("bootstrap invented qualified providers: %d non-PROVISIONING instances", notProvisioning)
	}
	// Exercise CP's real existing maker/checker mechanism using synthetic actors.
	// Bootstrap did not weaken its SQL enforcement or substitute a PASS receipt.
	target := result.ProofPrincipals["baobab-int03c-issuer-owner"]
	digest := "sha256:" + strings.Repeat("c", 64)
	provenance, err := repo.ProposeInt03CProvenance(ctx, target, "https://hydra.int03.test", "baobab-int03c-issuer-owner", cfg.RunID, strings.Repeat("b", 40), digest, result.MakerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ApproveInt03CProvenance(ctx, provenance.ProvenanceID, result.MakerID, digest); err == nil {
		t.Fatal("same synthetic Principal self-approved")
	}
	if _, err = repo.ApproveInt03CProvenance(ctx, provenance.ProvenanceID, result.CheckerID, digest); err != nil {
		t.Fatal(err)
	}
}
