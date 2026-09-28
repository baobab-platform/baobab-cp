package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestChangesetLifecycle covers migration 000069 and the changeset
// repository (ADR-BCP-021 CCM-02/03) end to end on one tenant: draft,
// idempotency, submit, every refusal on the decision and the apply, the
// applied suspension with its operation and outcome, a replayed apply, a
// blocked and a locked submit, staleness, cancellation, and an INVALID
// changeset for a tenant that does not exist. Every record conforms to
// the Shared contract.
func TestChangesetLifecycle(t *testing.T) {
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

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	tenant, legalEntity := "tn_cs"+suffix, "CS-TEST-"+strings.ToUpper(suffix)
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM changeset.outcome WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_tenant_id LIKE $1)`, "tn_cs"+suffix+"%")
		admin.Exec(ctx, `DELETE FROM changeset.approval WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_tenant_id LIKE $1)`, "tn_cs"+suffix+"%")
		admin.Exec(ctx, `UPDATE changeset.changeset SET current_plan_id = NULL WHERE target_tenant_id LIKE $1`, "tn_cs"+suffix+"%")
		admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM changeset.plan WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_tenant_id LIKE $1)`, "tn_cs"+suffix+"%")
		admin.Exec(ctx, `DELETE FROM changeset.changeset WHERE target_tenant_id LIKE $1`, "tn_cs"+suffix+"%")
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntity)
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntity); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference, tenant_id,
		legal_entity_id, display_name, isolation_strategy, residency_region, desired_state, observed_state)
		VALUES ('BOOTSTRAP', 'Test fixture registered outside admission', 'test-fixture', $1, $2, 'Changeset test', 'row_level_security',
		'af-south-1', 'active', 'active')`, tenant, legalEntity); err != nil {
		t.Fatal(err)
	}

	recordSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/Changeset")
	planSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangesetPlan")
	outcomeSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangeOutcome")
	approvalSchema := contracts.MustSchema("control-plane/v1/approval-decision.schema.json#/$defs/ApprovalDecision")
	conforms := func(label string, schema *contracts.Schema, v any) {
		t.Helper()
		if err := contracts.ValidateValue(schema, v); err != nil {
			t.Fatalf("%s does not conform: %v", label, err)
		}
	}
	requester, approver := "prn_requester"+suffix, "prn_approver"+suffix
	actor := AuditActor{ActorID: requester, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	draft := func(kind, tenantID, key string) changeset.Changeset {
		t.Helper()
		base, _, err := repo.TenantRevision(ctx, tenantID)
		if err != nil {
			t.Fatal(err)
		}
		c, err := changeset.Draft(changeset.CreateRequest{Title: "Change " + kind, Reason: "Test.",
			DesiredChange: changeset.DesiredChange{Kind: kind, TenantID: tenantID}}, domain.NewResourceID("cs"), requester, "API",
			actor.CorrelationID, base, now)
		if err != nil {
			t.Fatal(err)
		}
		created, err := repo.CreateChangeset(ctx, c, key, "hash-"+key, actor)
		if err != nil {
			t.Fatal(err)
		}
		return created
	}

	// Draft, and a replayed key.
	c := draft(changeset.KindTenantSuspension, tenant, "key-suspend-"+suffix)
	conforms("draft", recordSchema, c)
	retry := c
	retry.ChangesetID = domain.NewResourceID("cs") // a concurrent retry mints its own id
	if _, err := repo.CreateChangeset(ctx, retry, "key-suspend-"+suffix, "hash", actor); !errors.Is(err, ErrChangesetIdempotencyConflict) {
		t.Fatalf("a reused key: %v", err)
	}
	if got, hash, err := repo.GetChangesetByIdempotencyKey(ctx, requester, "key-suspend-"+suffix); err != nil || got.ChangesetID != c.ChangesetID || hash != "hash-key-suspend-"+suffix {
		t.Fatalf("by key: %v %q %v", got.ChangesetID, hash, err)
	}

	// Submit: planned and awaiting approval. A stale revision is refused.
	if _, err := repo.SubmitChangeset(ctx, c.ChangesetID, 7, domain.NewResourceID("plan"), now, actor); !errors.Is(err, ErrChangesetRevisionMismatch) {
		t.Fatalf("a stale If-Match: %v", err)
	}
	c, err = repo.SubmitChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("plan"), now, actor)
	if err != nil || c.State != changeset.StateAwaitingApproval || c.CurrentPlan == nil || c.RiskClass != "HIGH" {
		t.Fatalf("submit: %+v %v", c, err)
	}
	conforms("awaiting approval", recordSchema, c)
	plan, err := repo.CurrentChangesetPlan(ctx, c.ChangesetID)
	if err != nil || plan.PlanDigest != c.CurrentPlan.PlanDigest {
		t.Fatalf("plan: %v", err)
	}
	conforms("plan", planSchema, plan)

	// A second suspension of the same tenant is locked out while the first is open.
	other := draft(changeset.KindTenantSuspension, tenant, "key-other-"+suffix)
	other, err = repo.SubmitChangeset(ctx, other.ChangesetID, other.Revision, domain.NewResourceID("plan"), now, actor)
	if err != nil || other.State != changeset.StateBlocked || !hasCode(other.BlockingReasons, changeset.BlockTargetLocked) {
		t.Fatalf("an overlapping changeset: %+v %v", other, err)
	}
	conforms("blocked", recordSchema, other)

	// Decision refusals: not applicable yet, self-approval, another plan.
	decision := changeset.DecisionRequest{PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}
	if _, _, err := repo.ApplyChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("op"), "apply-early-"+suffix, "h", requester, now, actor); !errors.Is(err, ErrChangesetStateConflict) {
		t.Fatalf("apply before approval: %v", err)
	}
	if _, err := repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, decision, domain.NewResourceID("apd"), requester, now, actor); !errors.Is(err, ErrChangesetSelfApproval) {
		t.Fatalf("self-approval: %v", err)
	}
	wrong := decision
	wrong.PlanDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, wrong, domain.NewResourceID("apd"), approver, now, actor); !errors.Is(err, ErrChangesetPlanMismatch) {
		t.Fatalf("another digest: %v", err)
	}
	approval, err := repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, decision, domain.NewResourceID("apd"), approver, now, actor)
	if err != nil {
		t.Fatal(err)
	}
	conforms("approval", approvalSchema, approval)
	c, _ = repo.GetChangeset(ctx, c.ChangesetID)
	if c.State != changeset.StateApproved || c.ApprovalID != approval.ApprovalID {
		t.Fatalf("approved: %+v", c)
	}

	// A tenant changed underneath the approved plan makes it stale.
	if _, err := admin.Exec(ctx, `UPDATE tenants SET revision = revision + 1 WHERE tenant_id = $1`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ApplyChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("op"), "apply-stale-"+suffix, "h", requester, now, actor); !errors.Is(err, ErrChangesetPlanStale) {
		t.Fatalf("a stale plan applied: %v", err)
	}
	// Submitting again replans; the earlier approval lapses.
	c, err = repo.SubmitChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("plan"), now, actor)
	if err != nil || c.State != changeset.StateAwaitingApproval || c.CurrentPlan.PlanVersion != 2 || c.ApprovalID != "" {
		t.Fatalf("replan: %+v %v", c, err)
	}
	plan, _ = repo.CurrentChangesetPlan(ctx, c.ChangesetID)
	decision = changeset.DecisionRequest{PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}
	if _, err := repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, decision, domain.NewResourceID("apd"), approver, now, actor); err != nil {
		t.Fatal(err)
	}
	c, _ = repo.GetChangeset(ctx, c.ChangesetID)

	// Apply: the tenant is suspended, verified, and the outcome recorded.
	op, replayed, err := repo.ApplyChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("op"), "apply-"+suffix, "h-apply", requester, now, actor)
	if err != nil || replayed || op.Status != "SUCCEEDED" || op.Type != "CHANGESET_APPLY" || op.SubjectID != c.ChangesetID {
		t.Fatalf("apply: %+v %v %v", op, replayed, err)
	}
	var desired, observed string
	if err := admin.QueryRow(ctx, `SELECT desired_state, observed_state FROM tenants WHERE tenant_id = $1`, tenant).Scan(&desired, &observed); err != nil || desired != "suspended" || observed != "suspended" {
		t.Fatalf("tenant after apply: %s/%s %v", desired, observed, err)
	}
	c, _ = repo.GetChangeset(ctx, c.ChangesetID)
	if c.State != changeset.StateCompleted || c.OperationID != op.ID {
		t.Fatalf("completed: %+v", c)
	}
	conforms("completed", recordSchema, c)
	outcome, err := repo.GetChangeOutcome(ctx, c.ChangesetID)
	if err != nil || outcome.FinalState != changeset.StateCompleted || outcome.AppliedPlanDigest != plan.PlanDigest ||
		outcome.AffectedResources[0].Before != "active" || outcome.AffectedResources[0].After != "suspended" {
		t.Fatalf("outcome: %+v %v", outcome, err)
	}
	conforms("outcome", outcomeSchema, outcome)
	if again, replayed, err := repo.ApplyChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("op"), "apply-"+suffix, "h-apply", requester, now, actor); err != nil || !replayed || again.ID != op.ID {
		t.Fatalf("a replayed apply: %+v %v %v", again, replayed, err)
	}
	if _, _, err := repo.ApplyChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("op"), "apply-"+suffix, "other", requester, now, actor); !errors.Is(err, ErrOperationKeyReused) {
		t.Fatalf("a reused apply key: %v", err)
	}

	// The earlier blocked changeset: now the tenant is suspended, so a
	// resubmitted suspension conflicts with its state; it is cancelled.
	other, _ = repo.GetChangeset(ctx, other.ChangesetID)
	other, err = repo.SubmitChangeset(ctx, other.ChangesetID, other.Revision, domain.NewResourceID("plan"), now, actor)
	if err != nil || other.State != changeset.StateBlocked || !hasCode(other.BlockingReasons, changeset.BlockTargetStateConflict) {
		t.Fatalf("a suspension of a suspended tenant: %+v %v", other, err)
	}
	other, err = repo.CancelChangeset(ctx, other.ChangesetID, other.Revision, "Superseded.", now, actor)
	if err != nil || other.State != changeset.StateCancelled {
		t.Fatalf("cancel: %+v %v", other, err)
	}
	if _, err := repo.CancelChangeset(ctx, c.ChangesetID, c.Revision, "Too late.", now, actor); !errors.Is(err, ErrChangesetStateConflict) {
		t.Fatalf("cancelling a completed changeset: %v", err)
	}

	// A tenant that does not exist makes the changeset INVALID, with an outcome.
	missing := draft(changeset.KindTenantReinstatement, "tn_cs"+suffix+"none", "key-missing-"+suffix)
	missing, err = repo.SubmitChangeset(ctx, missing.ChangesetID, missing.Revision, domain.NewResourceID("plan"), now, actor)
	if err != nil || missing.State != changeset.StateInvalid || !hasCode(missing.BlockingReasons, changeset.BlockTargetNotFound) {
		t.Fatalf("a missing tenant: %+v %v", missing, err)
	}
	if o, err := repo.GetChangeOutcome(ctx, missing.ChangesetID); err != nil || o.FinalState != changeset.StateInvalid {
		t.Fatalf("invalid outcome: %+v %v", o, err)
	}

	// Listing filters by tenant, newest first, and pages.
	page, next, err := repo.ListChangesets(ctx, ChangesetFilter{TenantID: tenant, Limit: 1})
	if err != nil || len(page) != 1 || next == "" {
		t.Fatalf("list: %d %q %v", len(page), next, err)
	}
	rest, _, err := repo.ListChangesets(ctx, ChangesetFilter{TenantID: tenant, PageToken: next})
	if err != nil || len(rest) != 1 || rest[0].ChangesetID == page[0].ChangesetID {
		t.Fatalf("second page: %d %v", len(rest), err)
	}
}

func hasCode(findings []changeset.Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}
