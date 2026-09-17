package provisioning

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// TestZuriBeansUGZAManifestReachesActive proves Gate ZB-02's whole point:
// starting from nothing but seeded reference data (markets, a capability,
// an engine/instance, a legal entity and a tenant -- no hand-written
// MarketAssignment/CapabilityGrant/CapabilityBinding/TradeLane rows), a
// declarative ZuriBeans UG/ZA TenantProvisioning manifest can be resolved,
// planned and driven through PLAN -> APPLY -> RECONCILE -> READY -> ACTIVE
// by BuildZB02Pipeline's orchestrator alone, against a real PostgreSQL
// database, with no manual database manipulation once the fixture is
// seeded.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestZuriBeansUGZAManifestReachesActive(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()

	tenantStore, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open tenant store: %v", err)
	}
	defer tenantStore.Close()
	if err := tenantStore.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	repo, err := repository.Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	const (
		// tn_[a-z0-9]+ is the canonical tenant ID pattern
		// (domain.ValidTenantID, ADR-0004) -- no underscore after the
		// prefix, enforced by the outbox event envelope this test's writes
		// now produce.
		tenantID      = "tn_zuribeanszb02e2e"
		legalEntityID = "ZURIBEANS-EA-ZB02-E2E"
		capabilityKey = "trade.settlement"
	)
	marketUGID := domain.NewUUIDv7()
	marketZAID := domain.NewUUIDv7()
	capabilityID := domain.NewUUIDv7()
	engineID := domain.NewUUIDv7()
	instanceID := domain.NewUUIDv7()

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM messaging.outbox WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM market.trade_lane WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM market.market_participation_capability WHERE market_assignment_id IN (SELECT market_assignment_id FROM market.market_assignment WHERE tenant_id = $1)`, tenantID)
		admin.Exec(ctx, `DELETE FROM market.market_assignment WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE engine_instance_id = $1::uuid`, instanceID)
		admin.Exec(ctx, `DELETE FROM capability.capability_grant WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = $1::uuid`, instanceID)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = $1::uuid`, engineID)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, capabilityID)
		admin.Exec(ctx, `DELETE FROM market.market WHERE market_id IN ($1::uuid, $2::uuid)`, marketUGID, marketZAID)
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// -- seed known reference/reference-adjacent state (no provisioning
	// resources: no MarketAssignment, CapabilityGrant, CapabilityBinding or
	// TradeLane row exists yet) --
	if _, err := admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntityID); err != nil {
		t.Fatalf("fixture: create legal entity: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO tenants(tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region) VALUES ($1, $2, 'ZuriBeans ZB-02 E2E Test', 'row_level_security', 'af-south-1')`, tenantID, legalEntityID); err != nil {
		t.Fatalf("fixture: create tenant: %v", err)
	}
	if err := repo.CreateMarket(ctx, domain.Market{ID: marketUGID, Code: "UG", Name: "Uganda", Currency: "UGX", Region: "af-east-1", IsActive: true}); err != nil {
		t.Fatalf("fixture: create market UG: %v", err)
	}
	if err := repo.CreateMarket(ctx, domain.Market{ID: marketZAID, Code: "ZA", Name: "South Africa", Currency: "ZAR", Region: "af-south-1", IsActive: true}); err != nil {
		t.Fatalf("fixture: create market ZA: %v", err)
	}
	if err := repo.CreateCapability(ctx, capabilitydomain.Capability{
		ID: capabilityID, Key: capabilityKey, Name: "Cross-Border Trade Settlement", DomainKey: "trade",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported,
	}); err != nil {
		t.Fatalf("fixture: create capability: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine(engine_id, code, name) VALUES ($1::uuid, 'zb02-e2e-engine', 'ZB-02 E2E Settlement Engine')`, engineID); err != nil {
		t.Fatalf("fixture: create engine: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status) VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE')`, instanceID, engineID); err != nil {
		t.Fatalf("fixture: create engine instance: %v", err)
	}

	// -- desired state: a declarative ZuriBeans UG/ZA manifest, symbolic
	// references only (market codes, a capability key) --
	manifest := TenantManifest{
		APIVersion: "baobab.nabhold.com/v1",
		Kind:       "TenantProvisioning",
		Metadata:   ManifestMetadata{Name: "zuribeans-ug-za", TenantID: tenantID, DesiredStateVersion: 1},
		Spec: TenantManifestSpec{
			LegalEntityID: legalEntityID,
			DigitalEstate: "estate-zuribeans",
			// Both markets carry EXPORTING and IMPORTING (not just one
			// direction each) to prove ZB-02 does not hard-code UG as a
			// permanent "export market" or ZA as a permanent "import
			// market" (spec §41): a market's capabilities are an
			// authorised set, independent of any other market's.
			Markets: []ManifestMarket{
				{MarketCode: "UG", Capabilities: []string{"EXPORTING", "IMPORTING", "SELLING"}},
				{MarketCode: "ZA", Capabilities: []string{"EXPORTING", "IMPORTING", "SELLING"}},
			},
			CapabilityGrants: []ManifestCapabilityGrant{
				{CapabilityKey: capabilityKey, Source: "PLATFORM_BASELINE"},
			},
			CapabilityBindings: []ManifestCapabilityBinding{
				{CapabilityKey: capabilityKey, Engine: engineID, EngineInstance: instanceID, Mode: "PRIMARY", Priority: 10},
			},
			TradeLanes: []ManifestTradeLane{
				{OriginMarket: "UG", DestinationMarket: "ZA", Direction: "CROSS_MARKET", PermittedCapabilityKeys: []string{capabilityKey}},
				{OriginMarket: "ZA", DestinationMarket: "UG", Direction: "CROSS_MARKET", PermittedCapabilityKeys: []string{capabilityKey}},
			},
		},
	}

	resolved, err := ResolveManifest(ctx, repo, manifest)
	if err != nil {
		t.Fatalf("resolve manifest: %v", err)
	}
	scopeID, err := EnsureDefaultCapabilityScope(ctx, repo, tenantID)
	if err != nil {
		t.Fatalf("ensure default capability scope: %v", err)
	}

	now := time.Now().UTC()
	deps := ZB02Dependencies{
		Tenants: tenantStore, Repo: repo, Provisioning: repo,
		Now: func() time.Time { return now },
	}
	orchestrator, err := BuildZB02Pipeline(deps, resolved, scopeID)
	if err != nil {
		t.Fatalf("build ZB-02 pipeline: %v", err)
	}

	svc := service.TenantProvisioningService{Repository: repo, Now: func() time.Time { return now }}
	planned, err := svc.Plan(ctx, tenantID, "zb02-e2e-idem-1", "zb02-e2e-hash-1", []string{"solution.baobab-xbt"}, []string{"UG", "ZA"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if planned.Status != provisioningdomain.ProvisioningStatusPlan {
		t.Fatalf("expected PLAN status after Plan, got %s", planned.Status)
	}

	final, err := orchestrator.Run(ctx, planned.ID)
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}
	if final.Status != provisioningdomain.ProvisioningStatusActive {
		t.Fatalf("expected ACTIVE, got %s (blocking_reasons=%v)", final.Status, final.BlockingReasons)
	}
	if final.ObservedStateVersion != final.DesiredStateVersion {
		t.Fatalf("expected observed_state_version to converge to desired_state_version, got observed=%d desired=%d", final.ObservedStateVersion, final.DesiredStateVersion)
	}
	if final.CompletedAt == nil {
		t.Fatal("expected completed_at to be set for ACTIVE")
	}
	if len(final.BlockingReasons) != 0 {
		t.Fatalf("expected no blocking reasons once ACTIVE, got %v", final.BlockingReasons)
	}

	// Re-fetch independently to prove the ACTIVE state is durable, not just
	// the in-memory return value of Run.
	stored, err := repo.GetTenantProvisioning(ctx, planned.ID)
	if err != nil {
		t.Fatalf("re-get tenant provisioning: %v", err)
	}
	if stored.Status != provisioningdomain.ProvisioningStatusActive {
		t.Fatalf("expected persisted status ACTIVE, got %s", stored.Status)
	}

	// And prove the underlying resources this manifest declared are
	// actually there, not merely that the state machine says so.
	ugAssignment, err := repo.GetEffectiveMarketAssignment(ctx, tenantID, marketUGID, now)
	if err != nil || !ugAssignment.IsOperationalAt(now) {
		t.Fatalf("expected an operational UG market participation, got %+v, err=%v", ugAssignment, err)
	}
	zaAssignment, err := repo.GetEffectiveMarketAssignment(ctx, tenantID, marketZAID, now)
	if err != nil || !zaAssignment.IsOperationalAt(now) {
		t.Fatalf("expected an operational ZA market participation, got %+v, err=%v", zaAssignment, err)
	}

	// Both directions must be provisioned and usable -- neither market is
	// hard-coded as a permanent exporter or importer (spec §41).
	ugToZALaneID := deterministicTradeLaneID(tenantID, marketUGID, marketZAID, domain.TradeLaneCrossMarket)
	ugToZALane, err := repo.GetTradeLane(ctx, tenantID, ugToZALaneID)
	if err != nil || !ugToZALane.IsUsable() {
		t.Fatalf("expected an ACTIVE UG->ZA trade lane, got %+v, err=%v", ugToZALane, err)
	}
	zaToUGLaneID := deterministicTradeLaneID(tenantID, marketZAID, marketUGID, domain.TradeLaneCrossMarket)
	zaToUGLane, err := repo.GetTradeLane(ctx, tenantID, zaToUGLaneID)
	if err != nil || !zaToUGLane.IsUsable() {
		t.Fatalf("expected an ACTIVE ZA->UG trade lane, got %+v, err=%v", zaToUGLane, err)
	}
	grants, err := repo.ListGrants(ctx, tenantID, capabilityKey)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	foundActiveGrant := false
	for _, g := range grants {
		if g.Status == capabilitydomain.GrantStatusActive && g.ScopeID == scopeID {
			foundActiveGrant = true
		}
	}
	if !foundActiveGrant {
		t.Fatalf("expected an ACTIVE capability grant for %s, got %+v", capabilityKey, grants)
	}

	// Outbox events for this run's meaningful domain transitions
	// (market-participation-created x2, trade-lane-activated x2,
	// tenant-provisioning-ready, tenant-provisioning-active) must have
	// committed atomically with the writes that produced them.
	rows, err := admin.Query(ctx, `SELECT event_type FROM messaging.outbox WHERE tenant_id = $1 ORDER BY occurred_at`, tenantID)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	defer rows.Close()
	var eventTypes []string
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scan outbox event_type: %v", err)
		}
		eventTypes = append(eventTypes, eventType)
	}
	expected := map[string]int{
		"com.nabhold.control-plane.market-participation-created.v1": 2,
		"com.nabhold.control-plane.trade-lane-activated.v1":         2,
		"com.nabhold.control-plane.tenant-provisioning-ready.v1":    1,
		"com.nabhold.control-plane.tenant-provisioning-active.v1":   1,
	}
	got := map[string]int{}
	for _, eventType := range eventTypes {
		got[eventType]++
	}
	for eventType, count := range expected {
		if got[eventType] != count {
			t.Fatalf("expected %d %s outbox event(s), got %d (all events: %v)", count, eventType, got[eventType], eventTypes)
		}
	}
}
