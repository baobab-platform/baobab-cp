package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestCapabilityResolutionRecordsAndGrantGaps: every decision is recorded
// (migration 000079), a RESOLVED record must name what it resolved to and
// any other must name why not; and capability.tenant_binding_without_grant
// lists an ACTIVE binding until its tenant holds an effective grant.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestCapabilityResolutionRecordsAndGrantGaps(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)

	const (
		tenant     = "tn_resolutionrecord"
		capability = "82000000-0000-0000-0000-000000000001"
		engine     = "82000000-0000-0000-0000-000000000002"
		instance   = "82000000-0000-0000-0000-000000000003"
		scope      = "82000000-0000-0000-0000-000000000004"
		binding    = "82000000-0000-0000-0000-000000000005"
		grant      = "82000000-0000-0000-0000-000000000006"
	)
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM capability.capability_resolution WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM capability.capability_grant WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE capability_id = $1`, capability)
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE engine_id = $1`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id = $1`, scope)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id = $1`, engine)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = $1`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1`, capability)
	}
	cleanup()
	t.Cleanup(cleanup)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO capability.capability(capability_id, code, name) VALUES ($1, 'test.resolutionrecord.execute', 'Resolution record')`, capability)
	exec(`INSERT INTO topology.engine(engine_id, code, name) VALUES ($1, 'resolutionrecord-engine', 'Engine')`, engine)
	exec(`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status) VALUES ($1, $2, 'af-south-1', 'production', 'ACTIVE')`, instance, engine)
	exec(`INSERT INTO capability.capability_scope(scope_id, tenant_id) VALUES ($1, $2)`, scope, tenant)
	var provider string
	if err := admin.QueryRow(ctx, `WITH p AS (INSERT INTO capability.capability_provider(provider_key, name, provider_type, engine_id, status)
		VALUES ('resolutionrecord-engine.main', 'Provider', 'BAOBAB_ENGINE', $1, 'ACTIVE') RETURNING provider_id)
		SELECT provider_id::text FROM p`, engine).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO capability.capability_binding(id, capability_id, engine_instance_id, scope_id, binding_mode, priority, status, contract_version, effective_from, provider_id)
		VALUES ($1, $2, $3, $4, 'PRIMARY', 1, 'ACTIVE', '1', now(), $5)`, binding, capability, instance, scope, provider)

	gaps := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM capability.tenant_binding_without_grant WHERE tenant_id = $1`, tenant).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if gaps() != 1 {
		t.Fatal("an ACTIVE binding without a grant is not reported")
	}
	exec(`INSERT INTO capability.capability_grant(grant_id, tenant_id, capability_id, scope_id, source, status, effective_from)
		VALUES ($1, $2, $3, $4, 'PLATFORM_BASELINE', 'ACTIVE', now() - interval '1 hour')`, grant, tenant, capability, scope)
	if gaps() != 0 {
		t.Fatal("a granted binding is still reported")
	}

	resolutionID := func() string { return "res_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "") }
	now := time.Now().UTC()
	resolved := CapabilityResolutionRecord{ResolutionID: resolutionID(), ContextID: "ctx-1", TenantID: tenant, CapabilityKey: "test.resolutionrecord.execute",
		Decision: "RESOLVED", GrantID: grant, BindingID: binding, ProviderID: provider, EngineInstanceID: instance, ContractVersion: 1,
		ServiceReference: "service://resolutionrecord/main", Protocol: "http", CorrelationID: domain.NewUUIDv7(), ResolvedAt: now}
	if err := repo.RecordCapabilityResolution(ctx, resolved); err != nil {
		t.Fatal(err)
	}
	denied := CapabilityResolutionRecord{ResolutionID: resolutionID(), ContextID: "ctx-1", TenantID: tenant, CapabilityKey: "test.resolutionrecord.execute",
		Decision: "DENIED", ReasonCode: "GRANT_NOT_FOUND", CorrelationID: domain.NewUUIDv7(), ResolvedAt: now}
	if err := repo.RecordCapabilityResolution(ctx, denied); err != nil {
		t.Fatal(err)
	}
	var recorded int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM capability.capability_resolution WHERE tenant_id = $1`, tenant).Scan(&recorded); err != nil || recorded != 2 {
		t.Fatalf("recorded %d decisions: %v", recorded, err)
	}
	// A RESOLVED decision always names what it resolved to, and only a
	// non-RESOLVED one carries a reason.
	incomplete := resolved
	incomplete.ResolutionID, incomplete.GrantID = resolutionID(), ""
	if err := repo.RecordCapabilityResolution(ctx, incomplete); err == nil {
		t.Fatal("a RESOLVED decision without its grant was recorded")
	}
	reasonless := denied
	reasonless.ResolutionID, reasonless.ReasonCode = resolutionID(), ""
	if err := repo.RecordCapabilityResolution(ctx, reasonless); err == nil {
		t.Fatal("a DENIED decision without a reason was recorded")
	}
}
