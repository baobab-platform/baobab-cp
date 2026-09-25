package provisioning

import (
	"context"
	"os"
	"testing"
	"time"

	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestZB02OrchestrationRecoversAfterSimulatedCrash proves spec §65: an
// interrupted provisioning run can be resumed by a completely independent
// Orchestrator instance -- as a fresh process restart would build -- and
// converge to ACTIVE without duplicating any resource APPLY already
// materialised before the "crash".
//
// The interruption is simulated by invoking the first Orchestrator's own
// APPLY phase worker directly (mirroring exactly what Orchestrator.Run
// would have done for the APPLY status) without letting Run drive the
// TenantProvisioning row past APPLY -- i.e. the durable row is left
// sitting at APPLY, exactly as it would be found after a process died
// right after APPLY's writes committed but before the transition to
// RECONCILE was recorded. A brand new Orchestrator (simulating the
// restarted process, sharing no in-memory state with the first) is then
// built against the same PostgreSQL connection and asked to finish the
// job from that durable state alone.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestZB02OrchestrationRecoversAfterSimulatedCrash(t *testing.T) {
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

	f := seedZB02Fixture(t, ctx, admin, repo, "crash")

	resolved, err := ResolveManifest(ctx, repo, f.manifest())
	if err != nil {
		t.Fatalf("resolve manifest: %v", err)
	}
	scopeID, err := EnsureDefaultCapabilityScope(ctx, repo, f.TenantID)
	if err != nil {
		t.Fatalf("ensure default capability scope: %v", err)
	}

	now := time.Now().UTC()
	deps := ZB02Dependencies{
		Tenants: tenantStore, Repo: repo, Provisioning: repo,
		Now: func() time.Time { return now },
	}

	svc := service.TenantProvisioningService{Repository: repo, Now: func() time.Time { return now }}
	planned, err := svc.Plan(ctx, f.TenantID, "zb02-crash-idem-1", "zb02-crash-hash-1", nil, []string{"UG", "ZA"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// -- "process 1": build an orchestrator, drive PLAN -> APPLY, then run
	// the APPLY worker's own materialisation once -- then stop. Nothing
	// past this point advances the durable TenantProvisioning row: no
	// save, no transition to RECONCILE. This is the crash.
	crashedOrchestrator, err := BuildZB02Pipeline(deps, resolved, scopeID)
	if err != nil {
		t.Fatalf("build first orchestrator: %v", err)
	}
	afterApplyTransition, err := svc.Apply(ctx, planned.ID)
	if err != nil {
		t.Fatalf("transition PLAN -> APPLY: %v", err)
	}
	// White-box: reach into the unexported apply worker directly (this
	// test lives in package provisioning) so only the APPLY phase's
	// materialisation runs, without Orchestrator.Run also saving progress
	// or transitioning the row to RECONCILE -- exactly the partial state a
	// process death right after APPLY's writes committed would leave.
	if _, err := crashedOrchestrator.apply.Run(ctx, afterApplyTransition); err != nil {
		t.Fatalf("simulate first APPLY attempt: %v", err)
	}

	preCrash, err := repo.GetTenantProvisioning(ctx, planned.ID)
	if err != nil {
		t.Fatalf("get pre-crash state: %v", err)
	}
	if preCrash.Status != provisioningdomain.ProvisioningStatusApply {
		t.Fatalf("expected the durable row to still be at APPLY after the simulated crash, got %s", preCrash.Status)
	}

	// Prove APPLY actually materialised something real before "dying", so
	// resuming genuinely has to cope with partial progress rather than an
	// empty database.
	ugAssignmentBeforeRestart, err := repo.GetEffectiveMarketAssignment(ctx, f.TenantID, f.MarketUGID, now)
	if err != nil || !ugAssignmentBeforeRestart.IsOperationalAt(now) {
		t.Fatalf("expected the crashed APPLY attempt to have already materialised UG participation, got %+v, err=%v", ugAssignmentBeforeRestart, err)
	}

	// -- "process 2": a completely independent Orchestrator, sharing no
	// Go-level state with the first, built the same way a restarted
	// process would build one. It only knows what PostgreSQL knows.
	restartedOrchestrator, err := BuildZB02Pipeline(deps, resolved, scopeID)
	if err != nil {
		t.Fatalf("build restarted orchestrator: %v", err)
	}
	final, err := restartedOrchestrator.Run(ctx, planned.ID)
	if err != nil {
		t.Fatalf("resume after simulated crash: %v", err)
	}
	if final.Status != provisioningdomain.ProvisioningStatusActive {
		t.Fatalf("expected recovery to converge to ACTIVE, got %s (blocking_reasons=%v)", final.Status, final.BlockingReasons)
	}
	if final.AttemptCount != 0 {
		t.Fatalf("expected recovery via re-running idempotent APPLY, not a FAILED/Retry cycle, got attempt_count=%d", final.AttemptCount)
	}

	// -- no duplication: exactly one MarketAssignment per market, despite
	// APPLY having run twice (once pre-crash, once on resume).
	assignments, err := repo.ListMarketAssignmentsForTenant(ctx, f.TenantID)
	if err != nil {
		t.Fatalf("list market assignments: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("expected exactly one MarketAssignment per market (2 total) after APPLY ran twice, got %d: %+v", len(assignments), assignments)
	}

	grants, err := repo.ListGrants(ctx, f.TenantID, f.CapabilityKey)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected exactly one CapabilityGrant despite APPLY running twice, got %d: %+v", len(grants), grants)
	}

	bindings, err := repo.ListBindings(ctx, f.CapabilityKey)
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected exactly one CapabilityBinding despite APPLY running twice, got %d: %+v", len(bindings), bindings)
	}
}
