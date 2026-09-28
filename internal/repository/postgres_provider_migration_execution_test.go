package repository

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

// TestProviderMigrationExecution covers migration 000075 and ADR-SHARED-016
// for a STATELESS_REBIND migration of two cohorts:
//   - the creator cannot approve, and a decision must name the current plan;
//   - nothing advances before approval, and a stale revision is refused;
//   - prepare binds the target in MIGRATION mode;
//   - the canary shifts authority to the target, validate checks it, and a
//     replayed key returns the same operation;
//   - roll_back returns the cohort to the source and removes the target
//     bindings;
//   - a second migration runs through to COMPLETE, and a change outside the
//     migration makes its plan stale while the migration's own effects never do.
func TestProviderMigrationExecution(t *testing.T) {
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
	capability := domain.NewUUIDv7()
	scopeA, scopeB, scopeC := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	bindingA, bindingB, bindingC := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	tail := sourceEngine[len(sourceEngine)-8:]
	sourceKey, targetKey := "baobab-xsrc"+tail+".legacy", "baobab-xtgt"+tail+".modern"
	capabilityKey := "finance.execute" + tail + ".issue"
	tenantA, tenantB, tenantC := "tn_execa"+tail, "tn_execb"+tail, "tn_execc"+tail
	var migrations []string
	t.Cleanup(func() {
		for _, id := range migrations {
			admin.Exec(ctx, `DELETE FROM audit_events WHERE target = $1`, id)
			admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE subject_id = $1`, id)
			admin.Exec(ctx, `UPDATE topology.provider_migration SET approval_id = NULL WHERE provider_migration_id = $1`, id)
			admin.Exec(ctx, `DELETE FROM topology.provider_migration_approval WHERE provider_migration_id = $1`, id)
			admin.Exec(ctx, `DELETE FROM topology.provider_migration_binding WHERE provider_migration_id = $1`, id)
			admin.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`)
			admin.Exec(ctx, `DELETE FROM topology.provider_migration_plan WHERE provider_migration_id = $1`, id)
			admin.Exec(ctx, `DELETE FROM topology.provider_migration WHERE provider_migration_id = $1`, id)
		}
		admin.Exec(ctx, `DELETE FROM topology.health_observation WHERE engine_instance_id = $1::uuid`, targetInstance)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE capability_id = $1::uuid`, capability)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id = ANY($1::uuid[])`, []string{scopeA, scopeB, scopeC})
		admin.Exec(ctx, `DELETE FROM capability.provider_capability_support WHERE provider_id = ANY($1::uuid[])`, []string{sourceProvider, targetProvider})
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_id = ANY($1::uuid[])`, []string{sourceProvider, targetProvider})
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, capability)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = ANY($1::uuid[])`, []string{sourceInstance, targetInstance})
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = ANY($1::uuid[])`, []string{sourceEngine, targetEngine})
	})
	if err := repo.CreateCapability(ctx, capabilitydomain.Capability{ID: capability, Key: capabilityKey, Name: "Execution test", DomainKey: "finance",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO topology.engine (engine_id, code, name) VALUES ($1::uuid, $2, $2), ($3::uuid, $4, $4)`,
		sourceEngine, "baobab-xsrc"+tail, targetEngine, "baobab-xtgt"+tail)
	exec(`INSERT INTO topology.engine_instance (engine_instance_id, engine_id, region, environment, status)
		VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE'), ($3::uuid, $4::uuid, 'af-south-1', 'production', 'ACTIVE')`,
		sourceInstance, sourceEngine, targetInstance, targetEngine)
	exec(`INSERT INTO capability.capability_provider (provider_id, provider_key, name, provider_type, engine_id, status)
		VALUES ($1::uuid, $2, 'Source', 'BAOBAB_ENGINE', $3::uuid, 'ACTIVE'), ($4::uuid, $5, 'Target', 'BAOBAB_ENGINE', $6::uuid, 'ACTIVE')`,
		sourceProvider, sourceKey, sourceEngine, targetProvider, targetKey, targetEngine)
	exec(`INSERT INTO capability.provider_capability_support (provider_id, capability_id, contract_versions)
		VALUES ($1::uuid, $3::uuid, '{1}'), ($2::uuid, $3::uuid, '{1}')`, sourceProvider, targetProvider, capability)
	exec(`INSERT INTO capability.capability_scope (scope_id, tenant_id, market_id, deployment_region, environment)
		VALUES ($1::uuid, $2, 'KE', 'af-south-1', 'production'), ($3::uuid, $4, 'ZA', 'af-south-1', 'production'),
			($5::uuid, $6, 'UG', 'af-south-1', 'production')`, scopeA, tenantA, scopeB, tenantB, scopeC, tenantC)
	exec(`INSERT INTO capability.capability_binding (id, capability_id, engine_instance_id, scope_id, binding_mode, status,
		contract_version, effective_from, provider_id)
		VALUES ($1::uuid, $3::uuid, $4::uuid, $5::uuid, 'PRIMARY', 'ACTIVE', 'v1', now() - interval '1 day', $7::uuid),
			($2::uuid, $3::uuid, $4::uuid, $6::uuid, 'PRIMARY', 'ACTIVE', 'v1', now() - interval '1 day', $7::uuid)`,
		bindingA, bindingB, capability, sourceInstance, scopeA, scopeB, sourceProvider)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.RecordHealthObservation(ctx, health.Observation{Subject: health.Subject{EngineInstanceID: targetInstance},
		Status: health.StatusHealthy, ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Hour), Source: health.SourceActiveProbe}); err != nil {
		t.Fatal(err)
	}

	planner := migration.Planner{Policy: health.MustDefaultPolicy()}
	creator := AuditActor{ActorID: "prn_creator" + tail, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	approver, operator := "prn_approver"+tail, "prn_operator"+tail
	operatorActor := AuditActor{ActorID: operator, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	create := func() (migration.Migration, migration.Plan) {
		t.Helper()
		request := migration.Request{SourceProviderKey: sourceKey, TargetProviderKey: targetKey,
			Capabilities: []migration.Capability{{CapabilityKey: capabilityKey, ContractVersion: 1}}, MigrationMode: migration.ModeStatelessRebind,
			DataStrategy: migration.DataNone, RollbackStrategy: "REBIND_SOURCE",
			Cohorts: []migration.Cohort{{CohortKey: "canary", Selector: &migration.Selector{TenantIDs: []string{tenantA}}}, {CohortKey: "rest"}},
			Owners:  []string{"prn_owner" + tail}, Reason: "Replace the provider."}
		id := domain.NewResourceID("pmg")
		migrations = append(migrations, id)
		m, err := repo.CreateProviderMigration(ctx, sourceKey, "idem-"+domain.NewUUIDv7(), "hash", func(ctx context.Context) (migration.Migration, migration.Plan, error) {
			p := planner
			p.Facts = repo
			plan, err := p.Plan(ctx, migration.Input{Request: request, ProviderMigrationID: id, PlanID: domain.NewResourceID("plan"),
				PlanVersion: 1, BaseRevision: 1, Now: now})
			if err != nil {
				return migration.Migration{}, migration.Plan{}, err
			}
			return migration.Migration{ProviderMigrationID: id, Request: request, Stage: migration.StagePlan, PlanID: plan.PlanID,
				PlanVersion: 1, PlanDigest: plan.PlanDigest, Blocked: len(plan.Blockers) > 0, CreatedBy: creator.ActorID,
				CreatedAt: now, UpdatedAt: now, Revision: 1}, plan, nil
		}, creator)
		if err != nil || m.Blocked {
			t.Fatalf("create: %+v %v", m, err)
		}
		plan, err := repo.CurrentProviderMigrationPlan(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return m, plan
	}
	decide := func(m migration.Migration, plan migration.Plan, who string) (changeset.Approval, error) {
		return repo.DecideProviderMigration(ctx, m.ProviderMigrationID, m.Revision, changeset.DecisionRequest{PlanID: plan.PlanID,
			PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}, domain.NewResourceID("apd"),
			who, planner, now, AuditActor{ActorID: who, ActorType: "human", CorrelationID: domain.NewUUIDv7()})
	}
	current := func(id string) migration.Migration {
		t.Helper()
		m, err := repo.GetProviderMigration(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	advanceKeyed := func(id, transition, key string) (operations.Operation, error) {
		m := current(id)
		reason := ""
		if !migration.Forward(transition) {
			reason = "Test " + transition + "."
		}
		return repo.AdvanceProviderMigration(ctx, MigrationAdvance{ProviderMigrationID: id, ExpectedRevision: m.Revision,
			Transition: transition, Reason: reason, OperationID: domain.NewResourceID("op"), IdempotencyKey: key,
			RequestHash: "hash-" + key, RequestedBy: operator, Planner: planner, Now: time.Now().UTC(), Actor: operatorActor})
	}
	advance := func(id, transition string) operations.Operation {
		t.Helper()
		op, err := advanceKeyed(id, transition, "adv-"+domain.NewUUIDv7())
		if err != nil {
			t.Fatalf("%s: %v", transition, err)
		}
		if op.Status != operations.StatusSucceeded || op.Type != "PROVIDER_MIGRATION_ADVANCE" || op.SubjectType != "PROVIDER_MIGRATION" {
			t.Fatalf("%s operation: %+v %s", transition, op, op.Problem)
		}
		return op
	}
	modes := func(ids ...string) []string {
		t.Helper()
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			var mode, status string
			if err := admin.QueryRow(ctx, `SELECT binding_mode, status FROM capability.capability_binding WHERE id = $1::uuid`, id).Scan(&mode, &status); err != nil {
				t.Fatal(err)
			}
			out = append(out, mode+"/"+status)
		}
		return out
	}
	targets := func(id string) map[string]string {
		t.Helper()
		rows, err := admin.Query(ctx, `SELECT source_binding_id::text, target_binding_id::text FROM topology.provider_migration_binding
			WHERE provider_migration_id = $1`, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var s, tg string
			if err := rows.Scan(&s, &tg); err != nil {
				t.Fatal(err)
			}
			out[s] = tg
		}
		return out
	}

	// Approval.
	m, plan := create()
	if _, err := advanceKeyed(m.ProviderMigrationID, migration.TransitionPrepare, "adv-"+domain.NewUUIDv7()); !errors.Is(err, ErrProviderMigrationNotApproved) {
		t.Fatalf("advancing an unapproved migration: %v", err)
	}
	if _, err := decide(m, plan, creator.ActorID); !errors.Is(err, ErrProviderMigrationSelfApproval) {
		t.Fatalf("self-approval: %v", err)
	}
	wrong := plan
	wrong.PlanDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := decide(m, wrong, approver); !errors.Is(err, ErrProviderMigrationPlanMismatch) {
		t.Fatalf("a decision on another digest: %v", err)
	}
	a, err := decide(m, plan, approver)
	if err != nil || a.SubjectType != "PROVIDER_MIGRATION" || a.DecidedBy != approver {
		t.Fatalf("approve: %+v %v", a, err)
	}
	if _, err := decide(current(m.ProviderMigrationID), plan, approver); !errors.Is(err, ErrProviderMigrationStageConflict) {
		t.Fatalf("a second decision: %v", err)
	}
	// The approver never advances the migration it approved.
	if _, err := repo.AdvanceProviderMigration(ctx, MigrationAdvance{ProviderMigrationID: m.ProviderMigrationID,
		ExpectedRevision: current(m.ProviderMigrationID).Revision, Transition: migration.TransitionPrepare, OperationID: domain.NewResourceID("op"),
		IdempotencyKey: "adv-" + domain.NewUUIDv7(), RequestHash: "h", RequestedBy: approver, Planner: planner, Now: now,
		Actor: AuditActor{ActorID: approver, ActorType: "human", CorrelationID: domain.NewUUIDv7()}}); !errors.Is(err, ErrProviderMigrationSelfExecution) {
		t.Fatalf("the approver advancing: %v", err)
	}
	if _, err := repo.AdvanceProviderMigration(ctx, MigrationAdvance{ProviderMigrationID: m.ProviderMigrationID, ExpectedRevision: 1,
		Transition: migration.TransitionPrepare, OperationID: domain.NewResourceID("op"), IdempotencyKey: "adv-" + domain.NewUUIDv7(),
		RequestHash: "h", RequestedBy: operator, Planner: planner, Now: now, Actor: operatorActor}); !errors.Is(err, ErrProviderMigrationRevisionMismatch) {
		t.Fatalf("a stale revision: %v", err)
	}

	// Prepare binds the target in MIGRATION mode; the source keeps authority.
	advance(m.ProviderMigrationID, migration.TransitionPrepare)
	pairs := targets(m.ProviderMigrationID)
	if len(pairs) != 2 || current(m.ProviderMigrationID).Stage != "PREPARE" {
		t.Fatalf("prepare: %v %+v", pairs, current(m.ProviderMigrationID))
	}
	if got := modes(bindingA, pairs[bindingA], bindingB, pairs[bindingB]); !slices.Equal(got, []string{"PRIMARY/ACTIVE", "MIGRATION/ACTIVE", "PRIMARY/ACTIVE", "MIGRATION/ACTIVE"}) {
		t.Fatalf("after prepare: %v", got)
	}
	// The migration's own bindings never make its plan stale; validate is
	// not a transition from PREPARE.
	if _, err := advanceKeyed(m.ProviderMigrationID, migration.TransitionValidate, "adv-"+domain.NewUUIDv7()); !errors.Is(err, ErrProviderMigrationStageConflict) {
		t.Fatalf("validate from PREPARE: %v", err)
	}

	// The canary shifts tenant A only, and validates.
	key := "adv-" + domain.NewUUIDv7()
	canary, err := advanceKeyed(m.ProviderMigrationID, migration.TransitionCanary, key)
	if err != nil || canary.Status != operations.StatusSucceeded {
		t.Fatalf("canary: %+v %v", canary, err)
	}
	if replay, err := advanceKeyed(m.ProviderMigrationID, migration.TransitionCanary, key); err != nil || replay.ID != canary.ID {
		t.Fatalf("a replayed advance: %+v %v", replay, err)
	}
	if got := modes(bindingA, pairs[bindingA], bindingB, pairs[bindingB]); !slices.Equal(got, []string{"MIGRATION/ACTIVE", "PRIMARY/ACTIVE", "PRIMARY/ACTIVE", "MIGRATION/ACTIVE"}) {
		t.Fatalf("after canary: %v", got)
	}
	if c := current(m.ProviderMigrationID); c.Stage != "CANARY" || c.CurrentCohortKey != "canary" || !slices.Equal(c.ShiftedCohortKeys, []string{"canary"}) || c.OperationID != canary.ID {
		t.Fatalf("canary migration: %+v", c)
	}
	advance(m.ProviderMigrationID, migration.TransitionValidate)
	if _, err := advanceKeyed(m.ProviderMigrationID, migration.TransitionRetireOld, "adv-"+domain.NewUUIDv7()); !errors.Is(err, ErrProviderMigrationStageConflict) {
		t.Fatalf("retiring before the last cohort moved: %v", err)
	}

	// Roll back: the canary returns to the source; the target bindings go.
	advance(m.ProviderMigrationID, migration.TransitionRollBack)
	if got := modes(bindingA, pairs[bindingA], bindingB, pairs[bindingB]); !slices.Equal(got, []string{"PRIMARY/ACTIVE", "MIGRATION/RETIRED", "PRIMARY/ACTIVE", "MIGRATION/RETIRED"}) {
		t.Fatalf("after roll_back: %v", got)
	}
	if c := current(m.ProviderMigrationID); c.Stage != "ROLLED_BACK" || c.CompletedAt == nil || len(c.ShiftedCohortKeys) != 0 {
		t.Fatalf("rolled back: %+v", c)
	}
	if _, err := advanceKeyed(m.ProviderMigrationID, migration.TransitionPrepare, "adv-"+domain.NewUUIDv7()); !errors.Is(err, ErrProviderMigrationStageConflict) {
		t.Fatalf("advancing an ended migration: %v", err)
	}

	// A second migration: a change outside it makes the approved plan stale.
	m, plan = create()
	if _, err := decide(m, plan, approver); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO capability.capability_binding (id, capability_id, engine_instance_id, scope_id, binding_mode, status,
		contract_version, effective_from, provider_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'PRIMARY', 'ACTIVE', 'v1', now() - interval '1 hour', $5::uuid)`,
		bindingC, capability, sourceInstance, scopeC, sourceProvider)
	if _, err := advanceKeyed(m.ProviderMigrationID, migration.TransitionPrepare, "adv-"+domain.NewUUIDv7()); !errors.Is(err, ErrProviderMigrationPlanStale) {
		t.Fatalf("a plan made stale outside the migration: %v", err)
	}
	exec(`DELETE FROM capability.capability_binding WHERE id = $1::uuid`, bindingC)

	// Through to COMPLETE.
	for _, transition := range []string{migration.TransitionPrepare, migration.TransitionCanary, migration.TransitionValidate,
		migration.TransitionShift, migration.TransitionValidate, migration.TransitionRetireOld, migration.TransitionComplete} {
		advance(m.ProviderMigrationID, transition)
	}
	pairs = targets(m.ProviderMigrationID)
	if got := modes(bindingA, pairs[bindingA], bindingB, pairs[bindingB]); !slices.Equal(got, []string{"MIGRATION/RETIRED", "PRIMARY/ACTIVE", "MIGRATION/RETIRED", "PRIMARY/ACTIVE"}) {
		t.Fatalf("after complete: %v", got)
	}
	if c := current(m.ProviderMigrationID); c.Stage != "COMPLETE" || c.CompletedAt == nil || c.StartedAt == nil ||
		!slices.Equal(c.ShiftedCohortKeys, []string{"canary", "rest"}) {
		t.Fatalf("complete: %+v", c)
	}
	var audited int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'provider_migration.advanced' AND target = $1`,
		m.ProviderMigrationID).Scan(&audited); err != nil || audited != 7 {
		t.Fatalf("advance audits: %d %v", audited, err)
	}
}
