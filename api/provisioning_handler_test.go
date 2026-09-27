// Target path: api/provisioning_handler_test.go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// provisioningAPIFixture seeds the minimum reference state a ZB-03.1 HTTP
// provisioning test needs, independently of internal/provisioning's own
// (unexported) seedZB02Fixture -- this package deliberately does not
// depend on internal/provisioning's test-only helpers.
type provisioningAPIFixture struct {
	TenantID, OtherTenantID string
	LegalEntityID           string
	CapabilityKey           string
	MarketUGID, MarketZAID  string
	CapabilityID            string
	EngineID, InstanceID    string
}

func seedProvisioningAPIFixture(t *testing.T, ctx context.Context, admin *pgxpool.Pool, repo *repository.PostgresRepository, suffix string) provisioningAPIFixture {
	t.Helper()
	f := provisioningAPIFixture{
		TenantID:      "tn_zb03api" + strings.ToLower(suffix),
		OtherTenantID: "tn_zb03apiother" + strings.ToLower(suffix),
		LegalEntityID: "ZB03API-LE-" + suffix,
		CapabilityKey: "trade.settlement-" + strings.ToLower(suffix),
		MarketUGID:    domain.NewUUIDv7(),
		MarketZAID:    domain.NewUUIDv7(),
		CapabilityID:  domain.NewUUIDv7(),
		EngineID:      domain.NewUUIDv7(),
		InstanceID:    domain.NewUUIDv7(),
	}

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM provisioning.readiness_snapshot WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.reconciliation_snapshot WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM messaging.outbox WHERE tenant_id IN ($1, $2)`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM market.trade_lane WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM market.market_participation_capability WHERE market_assignment_id IN (SELECT market_assignment_id FROM market.market_assignment WHERE tenant_id = $1)`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM market.market_assignment WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE engine_instance_id = $1::uuid`, f.InstanceID)
		admin.Exec(ctx, `DELETE FROM capability.capability_grant WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE tenant_id IN ($1, $2)`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id IN ($1, $2)`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = $1::uuid`, f.InstanceID)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = $1::uuid`, f.EngineID)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, f.CapabilityID)
		admin.Exec(ctx, `DELETE FROM market.market WHERE market_id IN ($1::uuid, $2::uuid)`, f.MarketUGID, f.MarketZAID)
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id IN ($1, $2)`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, f.LegalEntityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, f.LegalEntityID); err != nil {
		t.Fatalf("create legal entity: %v", err)
	}
	for _, tenantID := range []string{f.TenantID, f.OtherTenantID} {
		if _, err := admin.Exec(ctx, `INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference, tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region) VALUES ('BOOTSTRAP', 'Test fixture registered outside admission', 'test-fixture', $1, $2, 'ZB-03.1 API Fixture', 'row_level_security', 'af-south-1')`, tenantID, f.LegalEntityID); err != nil {
			t.Fatalf("create tenant %s: %v", tenantID, err)
		}
	}
	upper := strings.ToUpper(suffix)
	if err := repo.CreateMarket(ctx, domain.Market{ID: f.MarketUGID, Code: "UG-" + upper, Name: "Uganda", Currency: "UGX", Region: "af-east-1", IsActive: true}); err != nil {
		t.Fatalf("create market UG: %v", err)
	}
	if err := repo.CreateMarket(ctx, domain.Market{ID: f.MarketZAID, Code: "ZA-" + upper, Name: "South Africa", Currency: "ZAR", Region: "af-south-1", IsActive: true}); err != nil {
		t.Fatalf("create market ZA: %v", err)
	}
	if err := repo.CreateCapability(ctx, capabilitydomain.Capability{
		ID: f.CapabilityID, Key: f.CapabilityKey, Name: "Cross-Border Trade Settlement", DomainKey: "trade",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported,
	}); err != nil {
		t.Fatalf("create capability: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine(engine_id, code, name) VALUES ($1::uuid, $2, 'ZB-03.1 API Fixture Engine')`, f.EngineID, "zb03api-engine-"+suffix); err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status) VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE')`, f.InstanceID, f.EngineID); err != nil {
		t.Fatalf("create engine instance: %v", err)
	}
	return f
}

func (f provisioningAPIFixture) suffix() string {
	const prefix = "tn_zb03api"
	return f.TenantID[len(prefix):]
}

func newProvisioningTestHandler(t *testing.T) (http.Handler, *repository.PostgresRepository, *pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(admin.Close)

	tenantStore, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open tenant store: %v", err)
	}
	t.Cleanup(func() { tenantStore.Close() })
	if err := tenantStore.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	repo, err := repository.Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	handler := New(Dependencies{Store: tenantStore, AdminVerifier: fakeVerifier{principal: adminPrincipal()}, Provisioning: repo})
	return handler, repo, admin, url
}

func doJSON(t *testing.T, handler http.Handler, method, path, idempotencyKey string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer admin-token")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestProvisioningUnknownIDReturnsNotFound(t *testing.T) {
	handler, repo, admin, _ := newProvisioningTestHandler(t)
	ctx := context.Background()
	f := seedProvisioningAPIFixture(t, ctx, admin, repo, "unknownid")

	rec := doJSON(t, handler, http.MethodGet, "/v1/tenants/"+f.TenantID+"/provisioning/"+domain.NewUUIDv7(), "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown provisioning id, got %d: %s", rec.Code, rec.Body.String())
	}
}
