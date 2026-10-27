package development

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/api"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/billing"
	"github.com/baobab-platform/baobab-cp/internal/capability/catalogue"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
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
	cfg.ExternalAPI = true

	// A failure after entity/tenant writes must roll back the whole bootstrap.
	if _, err = pool.Exec(ctx, `CREATE FUNCTION development.reject_fixture_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$; CREATE TRIGGER reject_fixture_test BEFORE INSERT ON development.mandate_scenario FOR EACH ROW EXECUTE FUNCTION development.reject_fixture_test()`); err != nil {
		t.Fatal(err)
	}
	if _, err = Bootstrap(ctx, pool, cfg, raw); err == nil {
		t.Fatal("injected fixture failure accepted")
	}
	var partial int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenants)+(SELECT count(*) FROM development.bootstrap_run)+(SELECT count(*) FROM registry.organisation_profile)`).Scan(&partial); err != nil || partial != 0 {
		t.Fatalf("partial transaction survived: %d %v", partial, err)
	}
	if _, err = pool.Exec(ctx, `DROP TRIGGER reject_fixture_test ON development.mandate_scenario; DROP FUNCTION development.reject_fixture_test()`); err != nil {
		t.Fatal(err)
	}
	result, err := Bootstrap(ctx, pool, cfg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SyntheticOnly || result.ProtectedAcceptance || len(result.Enterprises) != 5 || result.MakerID == result.CheckerID {
		t.Fatalf("invalid bootstrap result: %+v", result)
	}
	facts, err := CheckExternalAPI(ctx, pool, cfg)
	if err != nil || !facts.ZeroEstates || !facts.OwnAssignment || !facts.ValidatorUnassigned || !facts.IndependentTenant || facts.ProtectedAcceptance {
		t.Fatalf("external facts: %+v %v", facts, err)
	}
	sub, err := SubscriptionFixtures(ctx, pool, cfg, "prepare")
	if err != nil || sub.Status != "ACTIVE" || sub.Revision != 1 || sub.TenantID == sub.OtherTenantID {
		t.Fatalf("subscription fixture: %+v %v", sub, err)
	}
	replay, err := SubscriptionFixtures(ctx, pool, cfg, "prepare")
	if err != nil || replay.SubscriptionID != sub.SubscriptionID {
		t.Fatal("subscription fixture replay", err)
	}
	if _, err = SubscriptionFixtures(ctx, pool, cfg, "suspend"); err == nil {
		t.Fatal("out of order subscription transition")
	}
	advanced, err := SubscriptionFixtures(ctx, pool, cfg, "advance")
	if err != nil || advanced.Revision != 2 {
		t.Fatal("subscription revision", err)
	}
	suspended, err := SubscriptionFixtures(ctx, pool, cfg, "suspend")
	if err != nil || suspended.Revision != 3 || suspended.Status != "SUSPENDED" {
		t.Fatal("subscription suspension", err)
	}
	bad := cfg
	bad.Environment = "production"
	if _, err = SubscriptionFixtures(ctx, pool, bad, "prepare"); err == nil {
		t.Fatal("protected subscription fixture accepted")
	}
	bad = cfg
	bad.Now = result.ExpiresAt.Add(time.Second)
	if _, err = SubscriptionFixtures(ctx, pool, bad, "read"); err == nil {
		t.Fatal("expired subscription fixture accepted")
	}
	again, err := Bootstrap(ctx, pool, cfg, raw)
	if err != nil || again.Enterprises[0].TenantID != result.Enterprises[0].TenantID {
		t.Fatalf("idempotent replay: %v", err)
	}
	withoutAPI := cfg
	withoutAPI.ExternalAPI = false
	if _, err = Bootstrap(ctx, pool, withoutAPI, raw); err == nil {
		t.Fatal("API fixture flag drift accepted")
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
	if _, err := CheckExternalAPI(ctx, pool, expired); err == nil {
		t.Fatal("expired external facts accepted")
	}
	protected := cfg
	protected.Environment = "staging"
	if _, err = Bootstrap(ctx, pool, protected, raw); err == nil {
		t.Fatal("staging bootstrap accepted")
	}

	report, err := CheckFixtures(ctx, pool, cfg)
	if err != nil || len(report.Observations) != 24 || report.OfficialActorVerified || report.ProviderPermissionsGranted || report.ProtectedAcceptance {
		t.Fatalf("fixture checks: %+v %v", report, err)
	}
	if _, err := CheckFixtures(ctx, pool, expired); err == nil {
		t.Fatal("expired fixture check accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE development.mandate_scenario SET status='PENDING' WHERE case_key='valid_scoped'`); err == nil {
		t.Fatal("fixture history mutable")
	}
	var legalCount, canonicalMandates int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM registry.legal_entity_profile`).Scan(&legalCount); err != nil || legalCount != 3 {
		t.Fatalf("invented legal entity: %d %v", legalCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM registry.operating_legal_actor_mandate`).Scan(&canonicalMandates); err != nil || canonicalMandates != 0 {
		t.Fatalf("fixture fabricated legal authority: %v", err)
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
		foreign := result.Enterprises[0]
		if foreign.TenantID == e.TenantID {
			foreign = result.Enterprises[len(result.Enterprises)-1]
		}
		var primary, verified int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM registry.tenant_organisation_mapping WHERE tenant_id=$1 AND organisation_id=$2 AND mapping_role='PRIMARY_ORGANISATION' AND status='ACTIVE'`, e.TenantID, e.OrganisationID).Scan(&primary); err != nil || primary != 1 {
			t.Fatalf("PRIMARY mapping: %v", err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM registry.legal_entity_profile WHERE legal_entity_id=$1 AND verification_state='VERIFIED'`, e.LegalEntityID).Scan(&verified); err != nil || verified != 0 {
			t.Fatal("synthetic company misrepresented as verified")
		}

		if e.Key == "zuribeans" || e.Key == "equator-estate" {
			if e.LegalEntityID != "" || e.SyntheticParentLegalEntityID != result.Enterprises[0].LegalEntityID {
				t.Fatal("incorrect unincorporated parent model")
			}
			var defaultCount int
			var hasDefault bool
			if err := pool.QueryRow(ctx, `SELECT legal_entity_id IS NOT NULL FROM tenants WHERE tenant_id=$1`, e.TenantID).Scan(&hasDefault); err != nil || hasDefault {
				t.Fatalf("invented default legal actor: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM registry.tenant_legal_entity_mapping WHERE tenant_id=$1`, e.TenantID).Scan(&defaultCount); err != nil || defaultCount != 0 {
				t.Fatalf("parent inherited as default: %v", err)
			}
			resolution, err := store.ResolveOperatingLegalActor(ctx, legalactor.Request{TenantID: e.TenantID, OperatingOrganisationID: e.OrganisationID, Role: "CONTRACTING_PARTY", Activity: fixtureActivity, Market: "ZA", Capability: fixtureCapability, EffectiveAt: cfg.Now})
			if err != nil || resolution.Outcome != legalactor.NoApplicableMandate || resolution.ResponsibleLegalEntityID != "" {
				t.Fatalf("production resolver consumed scenario: %+v %v", resolution, err)
			}
		}
		if e.Key == "external-client" {
			if e.EstateID != "" || e.PrincipalID == "" || e.ClientID != ExternalAPIClient {
				t.Fatal("external client forced to have estate")
			}
			var estates int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM estate.digital_estate WHERE tenant_id=$1`, e.TenantID).Scan(&estates); err != nil || estates != 0 {
				t.Fatal("API-only tenant has an estate")
			}
			if ok, err := repo.DevelopmentTenantAllowed(ctx, result.ProofPrincipals[PulseValidatorClient], PulseValidatorClient, e.TenantID, cfg.Now); err != nil || ok {
				t.Fatal("validator obtained tenant assignment")
			}
		}
		allowed, err := repo.DevelopmentTenantAllowed(ctx, e.PrincipalID, e.ClientID, e.TenantID, cfg.Now)
		if err != nil || !allowed {
			t.Fatalf("development own-tenant assignment: %v", err)
		}
		if allowed, err = repo.DevelopmentTenantAllowed(ctx, e.PrincipalID, e.ClientID, foreign.TenantID, cfg.Now); err != nil || allowed {
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

		// Component API checks use an explicitly synthetic authenticated context;
		// real IAM qualification remains DEV-03. Persistence and policy are real PG.
		handler := api.New(api.Dependencies{Store: store, Environment: "development",
			WorkloadVerifier: fixtureTokenVerifier{principal}, Identity: service.IdentityService{Repository: repo},
			Identities: repo, Contexts: repo, Canonical: service.CanonicalEntityService{Repository: repo},
			OrganisationMappings: repo, MarketParticipations: repo})
		for _, tc := range []struct {
			name, tenant, org, market string
			authenticated             bool
			want                      int
		}{
			{"own", e.TenantID, e.OrganisationID, e.Markets[0], true, http.StatusOK},
			{"other tenant", foreign.TenantID, foreign.OrganisationID, "ZA", true, http.StatusForbidden},
			{"other organisation", e.TenantID, foreign.OrganisationID, e.Markets[0], true, http.StatusForbidden},
			{"other market", e.TenantID, e.OrganisationID, "GB", true, http.StatusForbidden},
			{"anonymous", e.TenantID, e.OrganisationID, e.Markets[0], false, http.StatusUnauthorized},
		} {
			body, _ := json.Marshal(map[string]string{"tenant_id": tc.tenant, "organisation_id": tc.org, "country_code": tc.market})
			req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", strings.NewReader(string(body)))
			if tc.authenticated {
				req.Header.Set("Authorization", "Bearer synthetic-component-token")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("%s %s HTTP %d wanted %d: %s", e.Key, tc.name, response.Code, tc.want, response.Body.String())
			}
		}
		lease, err := repo.DevelopmentTenantLease(ctx, e.PrincipalID, e.ClientID, e.TenantID, cfg.Now)
		if err != nil || lease.Sub(result.ExpiresAt).Abs() > time.Microsecond {
			t.Fatalf("authoritative tenant lease: expiry=%s err=%v", lease, err)
		}
		if _, err = repo.DevelopmentTenantLease(ctx, e.PrincipalID, e.ClientID, e.TenantID, result.ExpiresAt); err == nil {
			t.Fatal("expired run still grants a development tenant lease")
		}
		if _, err = repo.DevelopmentTenantLease(ctx, e.PrincipalID, e.ClientID, foreign.TenantID, cfg.Now); err == nil {
			t.Fatal("sibling tenant granted a development lease")
		}
		if _, _, err = svc.WithCountry("ZA").Resolve(ctx, principal, foreign.TenantID, foreign.OrganisationID, domain.NewUUIDv7(), cfg.Now); err == nil {
			t.Fatal("CP service accepted neighbour context")
		}
		own, err := repo.GetWorkforceMembership(ctx, e.PrincipalID, e.TenantID)
		if e.Key == "external-client" {
			// An unrelated API workload is not an employee/estate service.
			// Its CP-owned lease above supplies only its own tenant scope.
			if err == nil {
				t.Fatal("external workload fabricated workforce membership")
			}
		} else if err != nil || own.Status != "ACTIVE" {
			t.Fatalf("own tenant relationship: %v", err)
		}
		other := foreign.TenantID
		if _, err = repo.GetWorkforceMembership(ctx, e.PrincipalID, other); err == nil {
			t.Fatal("workload inherited external tenant membership")
		}
	}
	for _, environment := range []string{"development", "staging", "production"} {
		handler := api.New(api.Dependencies{Store: store, Environment: environment})
		for _, route := range []string{"/v1/development/bootstrap", "/internal/legal-actor/v1/assess"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{}`)))
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s exposed guarded route %s: %d", environment, route, response.Code)
			}
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
	proof := ExternalCapabilitySource{SourceSHA: strings.Repeat("a", 40), ManifestDigest: "sha256:" + strings.Repeat("b", 64), DeclarationDigest: "sha256:" + strings.Repeat("c", 64)}
	registered, err := ExternalCapabilityFixtures(ctx, pool, cfg, "register", proof)
	if err != nil || registered.Phase != "REGISTERED" || registered.TenantID == "" || registered.ProviderID == "" {
		t.Fatalf("owned registration: %+v %v", registered, err)
	}
	externalAgain, err := ExternalCapabilityFixtures(ctx, pool, cfg, "register", proof)
	if err != nil || externalAgain.ProviderID != registered.ProviderID {
		t.Fatalf("registration replay: %v", err)
	}
	if _, err = ExternalCapabilityFixtures(ctx, pool, cfg, "absent", proof); err == nil {
		t.Fatal("out-of-order withdrawal accepted")
	}
	if _, err = ExternalCapabilityFixtures(ctx, pool, protected, "read", proof); err == nil {
		t.Fatal("protected fixture accepted")
	}
	if _, err = ExternalCapabilityFixtures(ctx, pool, expired, "read", proof); err == nil {
		t.Fatal("expired fixture accepted")
	}
	cms, err := CMSCapabilityFixtures(ctx, pool, cfg, "register", proof)
	if err != nil || cms.Phase != "REGISTERED" || cms.ProviderID == "" || cms.TenantID == registered.TenantID || cms.InstanceID == registered.InstanceID {
		t.Fatalf("CMS owner registration did not preserve profile/tenant isolation: %+v %v", cms, err)
	}
	if cms.SourcePresent || cms.GrantActive || cms.BindingActive || cms.ProviderActive {
		t.Fatal("CMS registration invented active authority")
	}
	cmsReplay, err := CMSCapabilityFixtures(ctx, pool, cfg, "register", proof)
	if err != nil || cmsReplay.ProviderID != cms.ProviderID {
		t.Fatal("CMS registration replay failed")
	}
	changedProof := proof
	changedProof.SourceSHA = strings.Repeat("d", 40)
	if _, err = CMSCapabilityFixtures(ctx, pool, cfg, "register", changedProof); err == nil {
		t.Fatal("CMS registration adopted changed source proof")
	}
	for _, denied := range []Config{protected, expired} {
		if _, err = CMSCapabilityFixtures(ctx, pool, denied, "read", proof); err == nil {
			t.Fatal("CMS protected/expired fixture accepted")
		}
	}
	if _, err = CMSCapabilityFixtures(ctx, pool, cfg, "unentitled", proof); err == nil {
		t.Fatal("CMS withdrew absent grant")
	}
	var providerState string
	if err = pool.QueryRow(ctx, `SELECT status FROM capability.capability_provider WHERE provider_id=$1::uuid`, registered.ProviderID).Scan(&providerState); err != nil || providerState != "DRAFT" {
		t.Fatal("registration self-activated provider")
	}
	var invented int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM capability.capability_grant WHERE tenant_id=$1)+(SELECT count(*) FROM estate.digital_estate WHERE tenant_id=$1)`, registered.TenantID).Scan(&invented); err != nil || invented != 0 {
		t.Fatal("registration invented a grant or estate")
	}

	t.Run("DEV07BuyerIdentityOwnerFixture", func(t *testing.T) {
		buyer, e := BuyerIdentityFixtures(ctx, pool, cfg, "prepare")
		if e != nil || buyer.Status != "ACTIVE" || buyer.Revision != 1 || !buyer.SyntheticOnly || buyer.ProtectedAcceptance {
			t.Fatalf("buyer prepare: %+v %v", buyer, e)
		}
		var own Enterprise
		for _, candidate := range result.Enterprises {
			if candidate.Key == "zuribeans" {
				own = candidate
			}
		}
		if buyer.EstateID != own.EstateID || buyer.EngineInstanceID != result.EngineInstances["baobab-trade"] ||
			buyer.TenantID != own.TenantID || buyer.OrganisationID != own.OrganisationID {
			t.Fatal("fixture invented topology identifiers")
		}
		replay, e := BuyerIdentityFixtures(ctx, pool, cfg, "prepare")
		if e != nil || replay.BindingID != buyer.BindingID || replay.PrincipalID != buyer.PrincipalID {
			t.Fatal("buyer replay", e)
		}
		for _, bad := range []Config{protected, expired} {
			if _, e = BuyerIdentityFixtures(ctx, pool, bad, "read"); e == nil {
				t.Fatal("protected/expired buyer fixture accepted")
			}
		}
		changed := cfg
		changed.MakerSubject = "other-subject"
		if _, e = BuyerIdentityFixtures(ctx, pool, changed, "read"); e == nil {
			t.Fatal("wrong subject adopted run")
		}
		for _, phase := range []string{"suspend", "restore", "revoke"} {
			current, e := BuyerIdentityFixtures(ctx, pool, cfg, phase)
			if e != nil || current.Revision <= buyer.Revision {
				t.Fatalf("buyer %s: %+v %v", phase, current, e)
			}
			buyer = current
		}
		if _, e = BuyerIdentityFixtures(ctx, pool, cfg, "restore"); e == nil {
			t.Fatal("revoked buyer binding restored")
		}
		if _, e = BuyerIdentityFixtures(ctx, pool, cfg, "unknown"); e == nil {
			t.Fatal("unknown buyer phase")
		}
		current, e := BuyerIdentityFixtures(ctx, pool, cfg, "read")
		if e != nil || current.Status != "REVOKED" {
			t.Fatal("terminal buyer readback", e)
		}
	})

}

// This verifier is only a component seam; it makes no signed-token claim.
type fixtureTokenVerifier struct{ principal auth.Principal }

func (v fixtureTokenVerifier) Verify(_ context.Context, token string) (auth.Principal, error) {
	if token != "synthetic-component-token" {
		return auth.Principal{}, errors.New("synthetic component token required")
	}
	return v.principal, nil
}

// Separate disposable stores prove descriptor-driven identity/tenant assignment.
// This PG component test does not claim actual issuer or engine execution.
func TestEngineProfileBootstrapPostgres(t *testing.T) {
	for _, profileName := range []string{"pulse-intelligence", "payments-sandbox"} {
		t.Run(profileName, func(t *testing.T) {
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
			profileRaw, err := contracts.ReadEmbedded("development/v1/examples/engines/" + profileName + ".json")
			if err != nil {
				t.Fatal(err)
			}
			profile, err := ParseEngineIntegrationProfile(profileRaw)
			if err != nil {
				t.Fatal(err)
			}
			cfg := fixtureConfig()
			cfg.IntegrationProfile = profile
			result, err := Bootstrap(ctx, pool, cfg, raw)
			if err != nil {
				t.Fatal(err)
			}
			if result.FixtureVersion != 4 || result.ProtectedAcceptance {
				t.Fatal("incorrect engine profile result")
			}
			report, err := CheckFixtures(ctx, pool, cfg)
			if err != nil || len(report.Observations) != 24 || report.OfficialActorVerified || report.ProviderPermissionsGranted {
				t.Fatalf("engine bootstrap must retain non-authorising enterprise fixture checks: %+v, %v", report, err)
			}
			repo, err := repository.Open(ctx, u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			if profileName == "payments-sandbox" {
				if _, err := catalogue.SyncEmbedded(ctx, repo); err != nil {
					t.Fatal(err)
				}
				registrations, err := billing.EmbeddedRegistrations()
				if err != nil {
					t.Fatal(err)
				}
				for _, registration := range registrations {
					if registration.Provider.ProviderKey != "baobab-payments.sandbox" {
						continue
					}
					// Real owner registration precedes CP startup replay. The
					// complete embedded bundle must preserve its route/metadata.
					registration.Provider.EngineKey = "sandbox"
					registration.Provider.Invocation = &repository.ProviderInvocation{ServiceReference: "service://baobab-payments/payments", Protocol: "http"}
					if err := repo.RegisterEngine(ctx, registration); err != nil {
						t.Fatal(err)
					}
				}
				var provider, status string
				var before, after int64
				if err := pool.QueryRow(ctx, `SELECT provider_id::text,version FROM capability.capability_provider WHERE provider_key='baobab-payments.sandbox'`).Scan(&provider, &before); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					if _, err := billing.RegisterEmbeddedEngines(ctx, repo, "development", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
						t.Fatal(err)
					}
					invocation, exists, err := repo.ProviderInvocationByID(ctx, provider)
					if err != nil || !exists || invocation.ServiceReference != "service://baobab-payments/payments" || invocation.Protocol != "http" {
						t.Fatalf("startup removed native invocation: %+v %v %v", invocation, exists, err)
					}
					if err := pool.QueryRow(ctx, `SELECT version,status FROM capability.capability_provider WHERE provider_id::text=$1`, provider).Scan(&after, &status); err != nil || after != before || status != "DRAFT" {
						t.Fatalf("startup changed owner registration or authority: %d -> %d, %s, %v", before, after, status, err)
					}
				}
			}
			for _, e := range result.Enterprises {
				for _, role := range []string{"caller", "neighbour", "validator"} {
					id := profile.IAM[role].ClientID
					allowed, err := repo.DevelopmentTenantAllowed(ctx, result.ProofPrincipals[id], id, e.TenantID, cfg.Now)
					expected := role == "caller" && e.Key == profile.EnterpriseSeed || role == "neighbour" && e.Key == profile.NeighbourSeed
					if err != nil || allowed != expected {
						t.Fatalf("%s/%s assignment: %v %v", role, e.Key, allowed, err)
					}
				}
			}
			if _, err = Bootstrap(ctx, pool, cfg, raw); err != nil {
				t.Fatal("identical replay refused", err)
			}
			cfg.IntegrationProfile = nil
			if _, err = Bootstrap(ctx, pool, cfg, raw); err == nil {
				t.Fatal("different descriptor adopted old run")
			}
		})
	}
}
