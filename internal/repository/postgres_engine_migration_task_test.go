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
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

// TestStatefulMigrationEngineTasks drives a STATEFUL_CUTOVER migration
// through engine migration tasks (ADR-SHARED-016 sections 3-5):
//   - the canary issues FREEZE (source), MIGRATE (target, with the source
//     as counterpart), RECONCILE (both sides) and UNFREEZE (target) in turn,
//     each only after the previous step's reports, with the shift between;
//   - only the attested workload of a task's instance sees, claims and
//     reports it, under a lease, and a report is final and replayable;
//   - an unreported task past its deadline fails the next cohort's
//     advance with MIGRATION_TASK_TIMEOUT, leaving that cohort frozen;
//   - roll_back releases the frozen cohort, restores the canary with
//     RESTORE_AND_REBIND_SOURCE through reverse tasks, and ends ROLLED_BACK
//     with the source authoritative again.
func TestStatefulMigrationEngineTasks(t *testing.T) {
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
	scopeA, scopeB := domain.NewUUIDv7(), domain.NewUUIDv7()
	bindingA, bindingB := domain.NewUUIDv7(), domain.NewUUIDv7()
	tail := sourceEngine[len(sourceEngine)-8:]
	sourceKey, targetKey := "baobab-ssrc"+tail+".ledger", "baobab-stgt"+tail+".ledger"
	capabilityKey := "finance.stateful" + tail + ".issue"
	tenantA, tenantB := "tn_stfa"+tail, "tn_stfb"+tail
	sourceClient, targetClient := "src-workload-"+tail, "tgt-workload-"+tail
	migrationID := domain.NewResourceID("pmg")
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM audit_events WHERE target = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM topology.engine_migration_task WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM topology.provider_migration_run WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE subject_id = $1`, migrationID)
		admin.Exec(ctx, `UPDATE topology.provider_migration SET approval_id = NULL WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM topology.provider_migration_approval WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM topology.provider_migration_binding WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`)
		admin.Exec(ctx, `DELETE FROM topology.provider_migration_plan WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM topology.provider_migration WHERE provider_migration_id = $1`, migrationID)
		admin.Exec(ctx, `DELETE FROM topology.health_observation WHERE engine_instance_id = $1::uuid`, targetInstance)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE capability_id = $1::uuid`, capability)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id = ANY($1::uuid[])`, []string{scopeA, scopeB})
		admin.Exec(ctx, `DELETE FROM capability.provider_capability_support WHERE provider_id = ANY($1::uuid[])`, []string{sourceProvider, targetProvider})
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_id = ANY($1::uuid[])`, []string{sourceProvider, targetProvider})
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, capability)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = ANY($1::uuid[])`, []string{sourceInstance, targetInstance})
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = ANY($1::uuid[])`, []string{sourceEngine, targetEngine})
	})
	if err := repo.CreateCapability(ctx, capabilitydomain.Capability{ID: capability, Key: capabilityKey, Name: "Stateful test", DomainKey: "finance",
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
		sourceEngine, "baobab-ssrc"+tail, targetEngine, "baobab-stgt"+tail)
	// Each instance is attested to its own workload client.
	exec(`INSERT INTO topology.engine_instance (engine_instance_id, engine_id, region, environment, status, workload_client_id)
		VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE', $5), ($3::uuid, $4::uuid, 'af-south-1', 'production', 'ACTIVE', $6)`,
		sourceInstance, sourceEngine, targetInstance, targetEngine, sourceClient, targetClient)
	exec(`INSERT INTO capability.capability_provider (provider_id, provider_key, name, provider_type, engine_id, status)
		VALUES ($1::uuid, $2, 'Source', 'BAOBAB_ENGINE', $3::uuid, 'ACTIVE'), ($4::uuid, $5, 'Target', 'BAOBAB_ENGINE', $6::uuid, 'ACTIVE')`,
		sourceProvider, sourceKey, sourceEngine, targetProvider, targetKey, targetEngine)
	exec(`INSERT INTO capability.provider_capability_support (provider_id, capability_id, contract_versions)
		VALUES ($1::uuid, $3::uuid, '{1}'), ($2::uuid, $3::uuid, '{1}')`, sourceProvider, targetProvider, capability)
	exec(`INSERT INTO capability.capability_scope (scope_id, tenant_id, market_id, deployment_region, environment)
		VALUES ($1::uuid, $2, 'KE', 'af-south-1', 'production'), ($3::uuid, $4, 'ZA', 'af-south-1', 'production')`,
		scopeA, tenantA, scopeB, tenantB)
	exec(`INSERT INTO capability.capability_binding (id, capability_id, engine_instance_id, scope_id, binding_mode, status,
		contract_version, effective_from, provider_id)
		VALUES ($1::uuid, $3::uuid, $4::uuid, $5::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '1 day', $7::uuid),
			($2::uuid, $3::uuid, $4::uuid, $6::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '1 day', $7::uuid)`,
		bindingA, bindingB, capability, sourceInstance, scopeA, scopeB, sourceProvider)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.RecordHealthObservation(ctx, health.Observation{Subject: health.Subject{EngineInstanceID: targetInstance},
		Status: health.StatusHealthy, ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(2 * time.Hour), Source: health.SourceActiveProbe}); err != nil {
		t.Fatal(err)
	}

	policy := health.MustDefaultPolicy()
	planner := migration.Planner{Policy: policy}
	creator := AuditActor{ActorID: "prn_creator" + tail, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	operator := AuditActor{ActorID: "prn_operator" + tail, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	request := migration.Request{SourceProviderKey: sourceKey, TargetProviderKey: targetKey,
		Capabilities: []migration.Capability{{CapabilityKey: capabilityKey, ContractVersion: 1}}, MigrationMode: migration.ModeStatefulCutover,
		DataStrategy: "BULK_MIGRATE_THEN_CUTOVER", RollbackStrategy: "RESTORE_AND_REBIND_SOURCE",
		Cohorts:       []migration.Cohort{{CohortKey: "canary", Selector: &migration.Selector{TenantIDs: []string{tenantA}}}, {CohortKey: "rest"}},
		CutoverWindow: &migration.CutoverWindow{StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)},
		Owners:        []string{"prn_owner" + tail}, Reason: "Replace the ledger provider."}
	m, err := repo.CreateProviderMigration(ctx, sourceKey, "idem-"+domain.NewUUIDv7(), "hash", func(ctx context.Context) (migration.Migration, migration.Plan, error) {
		p := planner
		p.Facts = repo
		plan, err := p.Plan(ctx, migration.Input{Request: request, ProviderMigrationID: migrationID, PlanID: domain.NewResourceID("plan"),
			PlanVersion: 1, BaseRevision: 1, Now: now})
		if err != nil {
			return migration.Migration{}, migration.Plan{}, err
		}
		return migration.Migration{ProviderMigrationID: migrationID, Request: request, Stage: migration.StagePlan, PlanID: plan.PlanID,
			PlanVersion: 1, PlanDigest: plan.PlanDigest, Blocked: len(plan.Blockers) > 0, CreatedBy: creator.ActorID,
			CreatedAt: now, UpdatedAt: now, Revision: 1}, plan, nil
	}, creator)
	if err != nil || m.Blocked {
		t.Fatalf("create: %+v %v", m, err)
	}
	plan, err := repo.CurrentProviderMigrationPlan(ctx, migrationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DecideProviderMigration(ctx, migrationID, m.Revision, changeset.DecisionRequest{PlanID: plan.PlanID,
		PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}, domain.NewResourceID("apd"),
		"prn_approver"+tail, planner, now, AuditActor{ActorID: "prn_approver" + tail, ActorType: "human", CorrelationID: domain.NewUUIDv7()}); err != nil {
		t.Fatal(err)
	}

	current := func() migration.Migration {
		t.Helper()
		got, err := repo.GetProviderMigration(ctx, migrationID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	advance := func(transition string) operations.Operation {
		t.Helper()
		reason := ""
		if !migration.Forward(transition) {
			reason = "Test " + transition + "."
		}
		op, err := repo.AdvanceProviderMigration(ctx, MigrationAdvance{ProviderMigrationID: migrationID, ExpectedRevision: current().Revision,
			Transition: transition, Reason: reason, OperationID: domain.NewResourceID("op"), IdempotencyKey: "adv-" + domain.NewUUIDv7(),
			RequestHash: "h", RequestedBy: operator.ActorID, Planner: planner, Now: time.Now().UTC(), Actor: operator})
		if err != nil {
			t.Fatalf("%s: %v", transition, err)
		}
		return op
	}
	operation := func(id string) operations.Operation {
		t.Helper()
		op, err := repo.GetOperation(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return op
	}
	workload := func(client string) AuditActor {
		return AuditActor{ActorID: client, ActorType: "workload", ClientID: client, CorrelationID: domain.NewUUIDv7()}
	}
	taskSchema := contracts.MustSchema("control-plane/v1/engine-migration-task.schema.json#/$defs/EngineMigrationTask")
	conforms := func(label string, v any) {
		t.Helper()
		if err := contracts.ValidateValue(taskSchema, v); err != nil {
			t.Fatalf("%s does not conform: %v", label, err)
		}
	}
	list := func(client string) []migration.Task {
		t.Helper()
		tasks, _, err := repo.ListEngineMigrationTasks(ctx, client, "", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tasks {
			conforms("listed "+task.Operation, task)
		}
		return tasks
	}
	count := func(n int64) *int64 { return &n }
	digest := "sha256:" + strings.Repeat("ab", 32)
	// work claims and reports every open task of client as succeeded, and
	// returns the operations they had.
	work := func(client string) []string {
		t.Helper()
		var ops []string
		for _, task := range list(client) {
			claimed, err := repo.ClaimEngineMigrationTask(ctx, task.TaskID, task.Revision, client, policy, time.Now().UTC(), workload(client))
			if err != nil || claimed.Status != migration.TaskClaimed || claimed.ClaimedBy != client || claimed.LeaseExpiresAt == nil {
				t.Fatalf("claim %s: %+v %v", task.Operation, claimed, err)
			}
			conforms("claimed "+task.Operation, claimed)
			report := migration.TaskReport{Outcome: migration.TaskSucceeded}
			if task.Operation == migration.OpReconcileCohortData {
				report.RecordCount, report.ContentDigest = count(42), digest
			}
			reported, err := repo.ReportEngineMigrationTask(ctx, task.TaskID, claimed.Revision, client, report, "hash-"+task.TaskID, policy,
				time.Now().UTC(), workload(client))
			if err != nil {
				t.Fatalf("report %s: %v", task.Operation, err)
			}
			conforms("reported "+task.Operation, reported)
			ops = append(ops, task.Operation+"/"+task.Role+"/"+task.Direction)
		}
		return ops
	}
	modes := func() []string {
		t.Helper()
		var out []string
		for _, id := range []string{bindingA, bindingB} {
			var mode, status string
			if err := admin.QueryRow(ctx, `SELECT binding_mode, status FROM capability.capability_binding WHERE id = $1::uuid`, id).Scan(&mode, &status); err != nil {
				t.Fatal(err)
			}
			out = append(out, mode+"/"+status)
		}
		return out
	}

	if op := advance(migration.TransitionPrepare); op.Status != operations.StatusSucceeded {
		t.Fatalf("prepare: %+v", op)
	}

	// The canary waits on each engine step in turn.
	canary := advance(migration.TransitionCanary)
	if canary.Status != operations.StatusRunning || current().Stage != "CANARY" || canary.CurrentStep != "canary-freeze-cohort-writes" {
		t.Fatalf("canary accepted: %+v %+v", canary, current())
	}
	if got := list(targetClient); len(got) != 0 {
		t.Fatalf("the target sees the source's freeze: %+v", got)
	}
	freeze := list(sourceClient)
	if len(freeze) != 1 || freeze[0].Operation != migration.OpFreezeCohortWrites || freeze[0].Role != migration.RoleSource ||
		!slices.Equal(freeze[0].Contexts.TenantIDs, []string{tenantA}) || !slices.Equal(freeze[0].Capabilities, []string{capabilityKey}) {
		t.Fatalf("freeze task: %+v", freeze)
	}
	// Only the instance's attested workload reaches its task.
	if _, err := repo.ClaimEngineMigrationTask(ctx, freeze[0].TaskID, freeze[0].Revision, targetClient, policy, time.Now().UTC(),
		workload(targetClient)); !errors.Is(err, ErrEngineMigrationTaskNotFound) {
		t.Fatalf("another instance's workload claiming: %v", err)
	}
	if _, err := repo.ReportEngineMigrationTask(ctx, freeze[0].TaskID, freeze[0].Revision, sourceClient,
		migration.TaskReport{Outcome: migration.TaskSucceeded}, "h", policy, time.Now().UTC(), workload(sourceClient)); !errors.Is(err, ErrEngineMigrationTaskLeaseExpired) {
		t.Fatalf("reporting without a claim: %v", err)
	}
	if _, err := repo.ClaimEngineMigrationTask(ctx, freeze[0].TaskID, freeze[0].Revision+5, sourceClient, policy, time.Now().UTC(),
		workload(sourceClient)); !errors.Is(err, ErrEngineMigrationTaskRevision) {
		t.Fatalf("a stale claim: %v", err)
	}
	if got := work(sourceClient); !slices.Equal(got, []string{"FREEZE_COHORT_WRITES/SOURCE/FORWARD"}) {
		t.Fatalf("source work: %v", got)
	}
	// A report is final; the same one replays, another is refused.
	replayed, err := repo.ReportEngineMigrationTask(ctx, freeze[0].TaskID, 1, sourceClient, migration.TaskReport{Outcome: migration.TaskSucceeded},
		"hash-"+freeze[0].TaskID, policy, time.Now().UTC(), workload(sourceClient))
	if err != nil || replayed.Status != migration.TaskSucceeded {
		t.Fatalf("a replayed report: %+v %v", replayed, err)
	}
	if _, err := repo.ReportEngineMigrationTask(ctx, freeze[0].TaskID, replayed.Revision, sourceClient,
		migration.TaskReport{Outcome: migration.TaskFailed, ReasonCode: "MIGRATION_TASK_REJECTED"}, "other", policy, time.Now().UTC(),
		workload(sourceClient)); !errors.Is(err, ErrEngineMigrationTaskFinal) {
		t.Fatalf("a second, different report: %v", err)
	}

	migrate := list(targetClient)
	if len(migrate) != 1 || migrate[0].Operation != migration.OpMigrateCohortData || migrate[0].Counterpart == nil ||
		migrate[0].Counterpart.ProviderKey != sourceKey || !slices.Equal(migrate[0].Counterpart.EngineInstanceIDs, []string{domain.EngineInstanceKey(sourceInstance)}) {
		t.Fatalf("migrate task: %+v", migrate)
	}
	// A reconciliation must carry its count and digest.
	work(targetClient)
	reconcile := list(targetClient)
	if len(reconcile) != 1 || reconcile[0].Operation != migration.OpReconcileCohortData {
		t.Fatalf("target reconcile task: %+v", reconcile)
	}
	claimed, err := repo.ClaimEngineMigrationTask(ctx, reconcile[0].TaskID, reconcile[0].Revision, targetClient, policy, time.Now().UTC(), workload(targetClient))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReportEngineMigrationTask(ctx, reconcile[0].TaskID, claimed.Revision, targetClient,
		migration.TaskReport{Outcome: migration.TaskSucceeded}, "h", policy, time.Now().UTC(), workload(targetClient)); !errors.Is(err, ErrEngineMigrationTaskReportInvalid) {
		t.Fatalf("a reconciliation without its digest: %v", err)
	}
	if _, err := repo.ReportEngineMigrationTask(ctx, reconcile[0].TaskID, claimed.Revision, targetClient,
		migration.TaskReport{Outcome: migration.TaskSucceeded, RecordCount: count(42), ContentDigest: digest}, "h2", policy, time.Now().UTC(),
		workload(targetClient)); err != nil {
		t.Fatal(err)
	}
	if got := work(sourceClient); !slices.Equal(got, []string{"RECONCILE_COHORT_DATA/SOURCE/FORWARD"}) {
		t.Fatalf("source reconcile: %v", got)
	}
	// Reconciled: the canary shifted, and the target unfreezes.
	if got := modes(); got[0] != "MIGRATION/ACTIVE" || got[1] != "PRIMARY/ACTIVE" {
		t.Fatalf("after the shift: %v", got)
	}
	if got := work(targetClient); !slices.Equal(got, []string{"UNFREEZE_COHORT_WRITES/TARGET/FORWARD"}) {
		t.Fatalf("unfreeze: %v", got)
	}
	if op := operation(canary.ID); op.Status != operations.StatusSucceeded || !slices.Equal(current().ShiftedCohortKeys, []string{"canary"}) {
		t.Fatalf("canary done: %+v %+v", op, current())
	}
	if op := advance(migration.TransitionValidate); op.Status != operations.StatusSucceeded {
		t.Fatalf("validate: %+v %s", op, op.Problem)
	}

	// The next cohort's freeze is never reported: past its deadline it
	// fails the advance, and the cohort stays frozen.
	shift := advance(migration.TransitionShift)
	if shift.Status != operations.StatusRunning || current().CurrentCohortKey != "rest" {
		t.Fatalf("shift accepted: %+v", shift)
	}
	pending := list(sourceClient)
	if len(pending) != 1 || pending[0].Operation != migration.OpFreezeCohortWrites {
		t.Fatalf("rest freeze: %+v", pending)
	}
	exec(`UPDATE topology.engine_migration_task SET deadline_at = now() - interval '1 second' WHERE task_id = $1`, pending[0].TaskID)
	if _, err := repo.ClaimEngineMigrationTask(ctx, pending[0].TaskID, pending[0].Revision, sourceClient, policy, time.Now().UTC(),
		workload(sourceClient)); !errors.Is(err, ErrEngineMigrationTaskFinal) {
		t.Fatalf("claiming an overdue task: %v", err)
	}
	if op := operation(shift.ID); op.Status != operations.StatusFailed || !strings.Contains(string(op.Problem), "MIGRATION_TASK_TIMEOUT") {
		t.Fatalf("timed-out shift: %+v %s", op, op.Problem)
	}
	if c := current(); c.Stage != "SHIFT" || c.FailureReason == "" || !slices.Equal(c.ShiftedCohortKeys, []string{"canary"}) {
		t.Fatalf("after the timeout: %+v", c)
	}

	// Roll back: the timed-out freeze never succeeded, so nothing of "rest"
	// is frozen; the canary returns to the source through reverse tasks.
	rollback := advance(migration.TransitionRollBack)
	if rollback.Status != operations.StatusRunning || current().Stage != "SHIFT" {
		t.Fatalf("rollback accepted: %+v %+v", rollback, current())
	}
	// Asking the rollback to stop is honoured when the migration is next
	// settled: its open tasks are cancelled and it ends CANCELLED, the
	// migration still in SHIFT; roll_back runs again afresh.
	if _, err := repo.CancelOperation(ctx, rollback.ID, "Pause to investigate.", operator); err != nil {
		t.Fatal(err)
	}
	stale := current().Revision
	if err := repo.settleProviderMigration(ctx, migrationID, policy, time.Now().UTC(), operator); err != nil || current().Revision == stale {
		t.Fatalf("settling a cancel request: %v", err)
	}
	if op := operation(rollback.ID); op.Status != operations.StatusCancelled || len(list(targetClient)) != 0 || current().Stage != "SHIFT" {
		t.Fatalf("cancelled rollback: %+v %+v", op, current())
	}
	rollback = advance(migration.TransitionRollBack)
	if rollback.Status != operations.StatusRunning {
		t.Fatalf("rollback again: %+v", rollback)
	}
	var reverse []string
	for i := 0; i < 6 && operation(rollback.ID).Status == operations.StatusRunning; i++ {
		reverse = append(reverse, work(targetClient)...)
		reverse = append(reverse, work(sourceClient)...)
	}
	for _, want := range []string{"FREEZE_COHORT_WRITES/TARGET/REVERSE", "MIGRATE_COHORT_DATA/SOURCE/REVERSE",
		"RECONCILE_COHORT_DATA/TARGET/REVERSE", "RECONCILE_COHORT_DATA/SOURCE/REVERSE", "UNFREEZE_COHORT_WRITES/SOURCE/REVERSE"} {
		if !slices.Contains(reverse, want) {
			t.Fatalf("rollback work %v lacks %s", reverse, want)
		}
	}
	if op := operation(rollback.ID); op.Status != operations.StatusSucceeded {
		t.Fatalf("rollback: %+v %s", op, op.Problem)
	}
	if c := current(); c.Stage != "ROLLED_BACK" || len(c.ShiftedCohortKeys) != 0 || c.CompletedAt == nil {
		t.Fatalf("rolled back: %+v", c)
	}
	if got := modes(); !slices.Equal(got, []string{"PRIMARY/ACTIVE", "PRIMARY/ACTIVE"}) {
		t.Fatalf("source authority after rollback: %v", got)
	}
}
