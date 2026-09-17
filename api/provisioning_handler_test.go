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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/provisioning"
	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
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
		if _, err := admin.Exec(ctx, `INSERT INTO tenants(tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region) VALUES ($1, $2, 'ZB-03.1 API Fixture', 'row_level_security', 'af-south-1')`, tenantID, f.LegalEntityID); err != nil {
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

func (f provisioningAPIFixture) manifest(desiredStateVersion int64) provisioning.TenantManifest {
	return provisioning.TenantManifest{
		APIVersion: "baobab.nabhold.com/v1", Kind: "TenantProvisioning",
		Metadata: provisioning.ManifestMetadata{Name: "zb03-api-fixture", TenantID: f.TenantID, DesiredStateVersion: desiredStateVersion},
		Spec: provisioning.TenantManifestSpec{
			LegalEntityID: f.LegalEntityID, DigitalEstate: "estate-" + f.TenantID,
			Markets: []provisioning.ManifestMarket{
				{MarketCode: "UG-" + strings.ToUpper(f.suffix()), Capabilities: []string{"EXPORTING", "SELLING"}},
				{MarketCode: "ZA-" + strings.ToUpper(f.suffix()), Capabilities: []string{"IMPORTING", "SELLING"}},
			},
			CapabilityGrants:   []provisioning.ManifestCapabilityGrant{{CapabilityKey: f.CapabilityKey, Source: "PLATFORM_BASELINE"}},
			CapabilityBindings: []provisioning.ManifestCapabilityBinding{{CapabilityKey: f.CapabilityKey, Engine: f.EngineID, EngineInstance: f.InstanceID, Mode: "PRIMARY", Priority: 10}},
			TradeLanes:         []provisioning.ManifestTradeLane{{OriginMarket: "UG-" + strings.ToUpper(f.suffix()), DestinationMarket: "ZA-" + strings.ToUpper(f.suffix()), Direction: "CROSS_MARKET", PermittedCapabilityKeys: []string{f.CapabilityKey}}},
		},
	}
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

func TestProvisioningCreateDrivesManifestToActive(t *testing.T) {
	handler, repo, admin, _ := newProvisioningTestHandler(t)
	ctx := context.Background()
	f := seedProvisioningAPIFixture(t, ctx, admin, repo, "createactive")

	rec := doJSON(t, handler, http.MethodPost, "/v1/tenants/"+f.TenantID+"/provisioning", strings.Repeat("a", 20), f.manifest(1))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var op provisioningdomain.TenantProvisioning
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if op.Status != provisioningdomain.ProvisioningStatusActive {
		t.Fatalf("expected ACTIVE, got %s (blocking_reasons=%v)", op.Status, op.BlockingReasons)
	}
	if loc := rec.Header().Get("Location"); loc != "/v1/tenants/"+f.TenantID+"/provisioning/"+op.ID {
		t.Fatalf("unexpected Location header: %q", loc)
	}

	// GET the same run.
	rec = doJSON(t, handler, http.MethodGet, "/v1/tenants/"+f.TenantID+"/provisioning/"+op.ID, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on get, got %d: %s", rec.Code, rec.Body.String())
	}

	// LIST for the tenant includes it.
	rec = doJSON(t, handler, http.MethodGet, "/v1/tenants/"+f.TenantID+"/provisioning", "", nil)
	var list []provisioningdomain.TenantProvisioning
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != op.ID {
		t.Fatalf("expected exactly the one run in the list, got %+v", list)
	}

	// READINESS evidence is queryable and shows why it's ready.
	rec = doJSON(t, handler, http.MethodGet, "/v1/tenants/"+f.TenantID+"/provisioning/"+op.ID+"/readiness", "", nil)
	var readiness []provisioningdomain.ReadinessSnapshotRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &readiness); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if len(readiness) == 0 || !readiness[0].OverallReady {
		t.Fatalf("expected at least one overall_ready readiness snapshot, got %+v", readiness)
	}

	// DRIFT evidence shows a converged, zero-drift snapshot.
	rec = doJSON(t, handler, http.MethodGet, "/v1/tenants/"+f.TenantID+"/provisioning/"+op.ID+"/drift", "", nil)
	var drift []provisioningdomain.ReconciliationSnapshotRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &drift); err != nil {
		t.Fatalf("decode drift: %v", err)
	}
	if len(drift) == 0 || !drift[0].Converged {
		t.Fatalf("expected at least one converged reconciliation snapshot, got %+v", drift)
	}

	// APPLY again on an already-ACTIVE run is a safe, idempotent no-op.
	rec = doJSON(t, handler, http.MethodPost, "/v1/tenants/"+f.TenantID+"/provisioning/"+op.ID+"/apply", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on re-apply, got %d: %s", rec.Code, rec.Body.String())
	}
	var reapplied provisioningdomain.TenantProvisioning
	if err := json.Unmarshal(rec.Body.Bytes(), &reapplied); err != nil {
		t.Fatalf("decode reapply response: %v", err)
	}
	if reapplied.Status != provisioningdomain.ProvisioningStatusActive {
		t.Fatalf("expected re-apply to remain ACTIVE, got %s", reapplied.Status)
	}

	// RETRY on a non-FAILED run is rejected, not silently accepted.
	rec = doJSON(t, handler, http.MethodPost, "/v1/tenants/"+f.TenantID+"/provisioning/"+op.ID+"/retry", "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 retrying a non-FAILED run, got %d: %s", rec.Code, rec.Body.String())
	}

	// Re-POSTing the identical manifest with the SAME Idempotency-Key
	// returns the same run rather than creating a duplicate.
	rec = doJSON(t, handler, http.MethodPost, "/v1/tenants/"+f.TenantID+"/provisioning", strings.Repeat("a", 20), f.manifest(1))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on idempotent replay, got %d: %s", rec.Code, rec.Body.String())
	}
	var replayed provisioningdomain.TenantProvisioning
	if err := json.Unmarshal(rec.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if replayed.ID != op.ID {
		t.Fatalf("expected the idempotent replay to reuse provisioning id %s, got %s", op.ID, replayed.ID)
	}

	// The SAME Idempotency-Key with a DIFFERENT body is rejected, not
	// silently applied as a new desired state under the old identity.
	other := f.manifest(1)
	other.Metadata.Name = "a-different-request"
	rec = doJSON(t, handler, http.MethodPost, "/v1/tenants/"+f.TenantID+"/provisioning", strings.Repeat("a", 20), other)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for idempotency key reused with a different request, got %d: %s", rec.Code, rec.Body.String())
	}

	// Cross-tenant access to the same provisioning ID under a different
	// tenant's path fails closed as 404, never 403 (never confirms
	// existence to a caller who isn't authorised for it).
	rec = doJSON(t, handler, http.MethodGet, "/v1/tenants/"+f.OtherTenantID+"/provisioning/"+op.ID, "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant provisioning access, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, handler, http.MethodGet, "/v1/tenants/"+f.OtherTenantID+"/provisioning/"+op.ID+"/readiness", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant readiness access, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProvisioningCancelTransitionsDirectly(t *testing.T) {
	handler, repo, admin, _ := newProvisioningTestHandler(t)
	ctx := context.Background()
	f := seedProvisioningAPIFixture(t, ctx, admin, repo, "cancel")

	now := time.Now().UTC()
	seed := provisioningdomain.TenantProvisioning{
		ID: domain.NewUUIDv7(), TenantID: f.TenantID, IdempotencyKey: "idem-cancel-http", RequestHash: "hash-cancel-http",
		Status: provisioningdomain.ProvisioningStatusReconcile, DesiredStateVersion: 1, ObservedStateVersion: 0,
		StartedAt: now, Version: 1,
	}
	if err := repo.CreateTenantProvisioning(ctx, seed); err != nil {
		t.Fatalf("seed tenant provisioning: %v", err)
	}

	rec := doJSON(t, handler, http.MethodPost, "/v1/tenants/"+f.TenantID+"/provisioning/"+seed.ID+"/cancel", "", map[string]string{"reason": "operator requested"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on cancel, got %d: %s", rec.Code, rec.Body.String())
	}
	var cancelled provisioningdomain.TenantProvisioning
	if err := json.Unmarshal(rec.Body.Bytes(), &cancelled); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if cancelled.Status != provisioningdomain.ProvisioningStatusCancelled {
		t.Fatalf("expected CANCELLED, got %s", cancelled.Status)
	}

	// Cancelling an already-CANCELLED run is rejected (no outbound edges
	// from a terminal state), not silently accepted a second time.
	rec = doJSON(t, handler, http.MethodPost, "/v1/tenants/"+f.TenantID+"/provisioning/"+seed.ID+"/cancel", "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 cancelling an already-CANCELLED run, got %d: %s", rec.Code, rec.Body.String())
	}
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
