package provisioning

import (
	"context"
	"strings"
	"testing"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

// zb02Fixture is the minimum reference/reference-adjacent state every
// ZB-02 Postgres integration test needs: a legal entity, a tenant, two
// markets, one capability and one engine/engine_instance. It never
// creates a provisioning resource itself (MarketAssignment/
// CapabilityGrant/CapabilityBinding/TradeLane) -- those are what the
// orchestrator under test is responsible for materialising.
type zb02Fixture struct {
	TenantID      string
	LegalEntityID string
	CapabilityKey string
	MarketUGID    string
	MarketZAID    string
	CapabilityID  string
	EngineID      string
	InstanceID    string
}

// seedZB02Fixture seeds and registers cleanup for one independent ZB-02
// fixture. tenantSuffix must be unique per call within a test binary run
// (market.market.code is global, not tenant-scoped, so two fixtures in the
// same run need distinct market codes).
func seedZB02Fixture(t *testing.T, ctx context.Context, admin *pgxpool.Pool, repo *repository.PostgresRepository, tenantSuffix string) zb02Fixture {
	t.Helper()
	f := zb02Fixture{
		// tn_[a-z0-9]+ is the canonical tenant ID pattern (domain.ValidTenantID,
		// enforced by the outbox event envelope this fixture's writes now
		// produce, ADR-0004) -- no underscore is allowed after the prefix,
		// so tenantSuffix is appended directly rather than joined with "_".
		TenantID:      "tn_zb02" + strings.ToLower(tenantSuffix),
		LegalEntityID: "ZB02-LE-" + tenantSuffix,
		// capability.capability.code is globally unique (like
		// market.market.code), so this must be suffixed the same way.
		CapabilityKey: "trade.settlement-" + strings.ToLower(tenantSuffix),
		MarketUGID:    domain.NewUUIDv7(),
		MarketZAID:    domain.NewUUIDv7(),
		CapabilityID:  domain.NewUUIDv7(),
		EngineID:      domain.NewUUIDv7(),
		InstanceID:    domain.NewUUIDv7(),
	}

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM messaging.outbox WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM market.trade_lane WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM market.market_participation_capability WHERE market_assignment_id IN (SELECT market_assignment_id FROM market.market_assignment WHERE tenant_id = $1)`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM market.market_assignment WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE engine_instance_id = $1::uuid`, f.InstanceID)
		admin.Exec(ctx, `DELETE FROM capability.capability_grant WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = $1::uuid`, f.InstanceID)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = $1::uuid`, f.EngineID)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, f.CapabilityID)
		admin.Exec(ctx, `DELETE FROM market.market WHERE market_id IN ($1::uuid, $2::uuid)`, f.MarketUGID, f.MarketZAID)
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, f.LegalEntityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, f.LegalEntityID); err != nil {
		t.Fatalf("fixture %s: create legal entity: %v", tenantSuffix, err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference, tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region) VALUES ('BOOTSTRAP', 'Test fixture registered outside admission', 'test-fixture', $1, $2, 'ZB-02 Fixture Test', 'row_level_security', 'af-south-1')`, f.TenantID, f.LegalEntityID); err != nil {
		t.Fatalf("fixture %s: create tenant: %v", tenantSuffix, err)
	}
	// ResolveManifest uppercases market codes before lookup, so the seeded
	// code must already be uppercase or GetMarketByCode won't find it.
	upperSuffix := strings.ToUpper(tenantSuffix)
	if err := repo.CreateMarket(ctx, domain.Market{ID: f.MarketUGID, Code: "UG-" + upperSuffix, Name: "Uganda", Currency: "UGX", Region: "af-east-1", IsActive: true}); err != nil {
		t.Fatalf("fixture %s: create market UG: %v", tenantSuffix, err)
	}
	if err := repo.CreateMarket(ctx, domain.Market{ID: f.MarketZAID, Code: "ZA-" + upperSuffix, Name: "South Africa", Currency: "ZAR", Region: "af-south-1", IsActive: true}); err != nil {
		t.Fatalf("fixture %s: create market ZA: %v", tenantSuffix, err)
	}
	if err := repo.CreateCapability(ctx, capabilitydomain.Capability{
		ID: f.CapabilityID, Key: f.CapabilityKey, Name: "Cross-Border Trade Settlement", DomainKey: "trade",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported,
	}); err != nil {
		t.Fatalf("fixture %s: create capability: %v", tenantSuffix, err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine(engine_id, code, name) VALUES ($1::uuid, $2, 'ZB-02 Fixture Settlement Engine')`, f.EngineID, "zb02-engine-"+tenantSuffix); err != nil {
		t.Fatalf("fixture %s: create engine: %v", tenantSuffix, err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status) VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE')`, f.InstanceID, f.EngineID); err != nil {
		t.Fatalf("fixture %s: create engine instance: %v", tenantSuffix, err)
	}
	return f
}

// manifest builds the standard UG/ZA CROSS_MARKET desired-state manifest
// for this fixture's tenant.
func (f zb02Fixture) manifest() TenantManifest {
	return TenantManifest{
		APIVersion: "baobab.nabhold.com/v1",
		Kind:       "TenantProvisioning",
		Metadata:   ManifestMetadata{Name: "zb02-fixture", TenantID: f.TenantID, DesiredStateVersion: 1},
		Spec: TenantManifestSpec{
			LegalEntityID: f.LegalEntityID,
			DigitalEstate: "estate-" + f.TenantID,
			Markets: []ManifestMarket{
				{MarketCode: "UG-" + f.suffix(), Capabilities: []string{"EXPORTING", "SELLING"}},
				{MarketCode: "ZA-" + f.suffix(), Capabilities: []string{"IMPORTING", "SELLING"}},
			},
			CapabilityGrants: []ManifestCapabilityGrant{
				{CapabilityKey: f.CapabilityKey, Source: "PLATFORM_BASELINE"},
			},
			CapabilityBindings: []ManifestCapabilityBinding{
				{CapabilityKey: f.CapabilityKey, Engine: f.EngineID, EngineInstance: f.InstanceID, Mode: "PRIMARY", Priority: 10},
			},
			TradeLanes: []ManifestTradeLane{
				{OriginMarket: "UG-" + f.suffix(), DestinationMarket: "ZA-" + f.suffix(), Direction: "CROSS_MARKET", PermittedCapabilityKeys: []string{f.CapabilityKey}},
			},
		},
	}
}

// suffix recovers the tenantSuffix seedZB02Fixture was called with, since
// TenantID is "tn_zb02"+suffix and market codes need the same suffix.
func (f zb02Fixture) suffix() string {
	const prefix = "tn_zb02"
	if len(f.TenantID) > len(prefix) {
		return f.TenantID[len(prefix):]
	}
	return f.TenantID
}
