package repository

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

// TestProviderMigrationStore covers migration 000068 (ADR-BCP-006 Gate 8):
// discovery reads the source provider's live bindings with their context,
// the target's support, instances and health; a migration and its plan
// round-trip; the idempotency key is unique; an open migration is found
// for the in-progress blocker; creation is audited; and the database
// refuses a migration from a provider to itself.
func TestProviderMigrationStore(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	sourceEngine, targetEngine := domain.NewUUIDv7(), domain.NewUUIDv7()
	sourceInstance, targetInstance := domain.NewUUIDv7(), domain.NewUUIDv7()
	sourceProvider, targetProvider := domain.NewUUIDv7(), domain.NewUUIDv7()
	capability, scope, binding := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	tail := sourceEngine[len(sourceEngine)-8:]
	sourceKey, targetKey := "baobab-src"+tail+".legacy", "baobab-tgt"+tail+".modern"
	capabilityKey := "finance.migrate" + tail + ".issue"
	tenant := "tn_migrate" + tail
	var migrationID string
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM audit_events WHERE target = $1`, migrationID)
		admin.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`)
		admin.Exec(ctx, `DELETE FROM topology.provider_migration_plan WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM topology.provider_migration WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM topology.health_observation WHERE engine_instance_id = $1::uuid`, targetInstance)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE id = $1::uuid`, binding)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id = $1::uuid`, scope)
		admin.Exec(ctx, `DELETE FROM capability.provider_capability_support WHERE provider_id = ANY($1::uuid[])`, []string{sourceProvider, targetProvider})
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_id = ANY($1::uuid[])`, []string{sourceProvider, targetProvider})
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, capability)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = ANY($1::uuid[])`, []string{sourceInstance, targetInstance})
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = ANY($1::uuid[])`, []string{sourceEngine, targetEngine})
	})
	if err := repo.CreateCapability(ctx, capabilitydomain.Capability{ID: capability, Key: capabilityKey, Name: "Migration test", DomainKey: "finance",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO topology.engine (engine_id, code, name) VALUES ($1::uuid, $2, $2), ($3::uuid, $4, $4)`,
			[]any{sourceEngine, "baobab-src" + tail, targetEngine, "baobab-tgt" + tail}},
		{`INSERT INTO topology.engine_instance (engine_instance_id, engine_id, region, environment, status)
			VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE'), ($3::uuid, $4::uuid, 'af-south-1', 'production', 'ACTIVE')`,
			[]any{sourceInstance, sourceEngine, targetInstance, targetEngine}},
		{`INSERT INTO capability.capability_provider (provider_id, provider_key, name, provider_type, engine_id, status)
			VALUES ($1::uuid, $2, 'Source', 'BAOBAB_ENGINE', $3::uuid, 'ACTIVE'), ($4::uuid, $5, 'Target', 'BAOBAB_ENGINE', $6::uuid, 'ACTIVE')`,
			[]any{sourceProvider, sourceKey, sourceEngine, targetProvider, targetKey, targetEngine}},
		{`INSERT INTO capability.provider_capability_support (provider_id, capability_id, contract_versions)
			VALUES ($1::uuid, $3::uuid, '{1}'), ($2::uuid, $3::uuid, '{1,2}')`, []any{sourceProvider, targetProvider, capability}},
		{`INSERT INTO capability.capability_scope (scope_id, tenant_id, market_id, deployment_region, environment)
			VALUES ($1::uuid, $2, 'KE', 'af-south-1', 'production')`, []any{scope, tenant}},
		{`INSERT INTO capability.capability_binding (id, capability_id, engine_instance_id, scope_id, binding_mode, status,
			contract_version, effective_from, provider_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '1 day', $5::uuid)`,
			[]any{binding, capability, sourceInstance, scope, sourceProvider}},
	} {
		if _, err := admin.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.RecordHealthObservation(ctx, health.Observation{Subject: health.Subject{EngineInstanceID: targetInstance},
		Status: health.StatusHealthy, ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Hour), Source: health.SourceActiveProbe}); err != nil {
		t.Fatal(err)
	}

	// Discovery.
	bindings, err := repo.SourceBindings(ctx, sourceKey, []string{capabilityKey})
	if err != nil || len(bindings) != 1 {
		t.Fatalf("source bindings: %+v %v", bindings, err)
	}
	if b := bindings[0]; b.BindingID != "bind_"+strings.ReplaceAll(binding, "-", "") || b.TenantID != tenant || b.ContractVersion != 1 || !slices.Equal(b.Markets, []string{"KE"}) ||
		b.Region != "af-south-1" || b.Environment != "production" {
		t.Fatalf("source binding context: %+v", b)
	}
	if got, _ := repo.SourceBindings(ctx, targetKey, []string{capabilityKey}); len(got) != 0 {
		t.Fatalf("the target's bindings were discovered as the source's: %+v", got)
	}
	target, found, err := repo.Provider(ctx, targetKey)
	if err != nil || !found || target.Status != "ACTIVE" || !slices.Equal(target.Support[capabilityKey], []int{1, 2}) {
		t.Fatalf("target provider: %+v %v %v", target, found, err)
	}
	if _, found, err := repo.Provider(ctx, "baobab-none.none"); found || err != nil {
		t.Fatalf("an unregistered provider was found: %v", err)
	}
	instances, err := repo.ProviderInstances(ctx, targetKey, []string{capabilityKey})
	if err != nil || len(instances) != 1 || instances[0].EngineInstanceID != domain.EngineInstanceKey(targetInstance) ||
		instances[0].Health[capabilityKey].EngineInstance == nil {
		t.Fatalf("target instances: %+v %v", instances, err)
	}

	// Plan and create.
	request := migration.Request{SourceProviderKey: sourceKey, TargetProviderKey: targetKey,
		Capabilities: []migration.Capability{{CapabilityKey: capabilityKey, ContractVersion: 1}}, MigrationMode: migration.ModeStatelessRebind,
		DataStrategy: migration.DataNone, RollbackStrategy: "REBIND_SOURCE", Cohorts: []migration.Cohort{{CohortKey: "all"}},
		Owners: []string{"prn_owner" + tail}, Reason: "Replace the provider."}
	migrationID = domain.NewResourceID("pmg")
	actor := AuditActor{ActorID: "prn_ops" + tail, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	key := "idem-" + domain.NewUUIDv7()
	build := func(ctx context.Context) (migration.Migration, migration.Plan, error) {
		plan, err := migration.Planner{Facts: repo, Policy: health.MustDefaultPolicy()}.Plan(ctx, migration.Input{Request: request,
			ProviderMigrationID: migrationID, PlanID: domain.NewResourceID("plan"), PlanVersion: 1, BaseRevision: 1, Now: now})
		if err != nil {
			return migration.Migration{}, migration.Plan{}, err
		}
		return migration.Migration{ProviderMigrationID: migrationID, Request: request, Stage: migration.StagePlan, PlanID: plan.PlanID,
			PlanVersion: 1, PlanDigest: plan.PlanDigest, Blocked: len(plan.Blockers) > 0, CreatedBy: actor.ActorID,
			CreatedAt: now, UpdatedAt: now, Revision: 1}, plan, nil
	}
	created, err := repo.CreateProviderMigration(ctx, sourceKey, key, "hash-1", build, actor)
	if err != nil {
		t.Fatal(err)
	}
	if created.Blocked {
		t.Fatal("a ready target produced a blocked migration")
	}
	got, err := repo.GetProviderMigration(ctx, migrationID)
	if err != nil || got.PlanDigest != created.PlanDigest || got.Stage != migration.StagePlan || got.Request.TargetProviderKey != targetKey ||
		!got.CreatedAt.Equal(now) {
		t.Fatalf("get: %+v %v", got, err)
	}
	plan, err := repo.CurrentProviderMigrationPlan(ctx, migrationID)
	if err != nil || plan.PlanDigest != created.PlanDigest || migration.PlanDigest(plan) != plan.PlanDigest ||
		plan.Discovery.BindingCount != 1 || plan.Steps[1].Resources.EngineInstanceID != domain.EngineInstanceKey(targetInstance) {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	byKey, hash, err := repo.GetProviderMigrationByIdempotencyKey(ctx, key)
	if err != nil || byKey.ProviderMigrationID != migrationID || hash != "hash-1" {
		t.Fatalf("by idempotency key: %v %q %v", byKey.ProviderMigrationID, hash, err)
	}
	if _, err := repo.GetProviderMigration(ctx, "pmg_missing"); !errors.Is(err, ErrProviderMigrationNotFound) {
		t.Fatalf("missing migration: %v", err)
	}
	// A second create with the same key, as a concurrent retry would make it
	// with its own freshly minted id, loses to the first.
	retry := func(ctx context.Context) (migration.Migration, migration.Plan, error) {
		m, plan, err := build(ctx)
		m.ProviderMigrationID = domain.NewResourceID("pmg")
		return m, plan, err
	}
	if _, err := repo.CreateProviderMigration(ctx, sourceKey, key, "hash-1", retry, actor); !errors.Is(err, ErrProviderMigrationIdempotencyConflict) {
		t.Fatalf("a reused idempotency key: %v", err)
	}
	open, err := repo.OpenMigrations(ctx, sourceKey, []string{capabilityKey})
	if err != nil || !slices.Equal(open, []string{migrationID}) {
		t.Fatalf("open migrations: %v %v", open, err)
	}
	var audited int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'provider_migration.created' AND target = $1`, migrationID).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("audit rows: %d %v", audited, err)
	}

	// The database refuses a migration from a provider to itself.
	if _, err := admin.Exec(ctx, `
		INSERT INTO topology.provider_migration (provider_migration_id, source_provider_key, target_provider_key, capability_keys,
			request, stage, plan_id, plan_version, plan_digest, idempotency_key, request_hash, created_by)
		VALUES ($1, $2, $2, $3, '{}', 'PLAN', 'plan_x', 1, $4, $5, 'h', 'prn_x')`,
		domain.NewResourceID("pmg"), sourceKey, []string{capabilityKey}, created.PlanDigest, "self-"+key); err == nil {
		t.Fatal("a migration from a provider to itself was recorded")
	}
}

// TestContractMajor: a binding's stored contract version is read as its
// major version, whatever form it was stored in.
func TestContractMajor(t *testing.T) {
	for stored, want := range map[string]int{"v1": 1, "1": 1, "1.0.0": 1, "V2": 2, " 3.1 ": 3, "": 0, "vx": 0, "0": 0, "-1": 0} {
		if got := contractMajor(stored); got != want {
			t.Errorf("contractMajor(%q) = %d, want %d", stored, got, want)
		}
	}
}
