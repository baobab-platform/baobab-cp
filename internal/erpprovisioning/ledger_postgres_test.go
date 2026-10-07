package erpprovisioning

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// The ledger is proved against real PostgreSQL (migration 000095): its
// guarantees (one operation per approved plan, immutable link, forward-only
// progress) are the database's. Set TEST_DATABASE_URL to run it.
func TestPostgresLedger(t *testing.T) {
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
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	const (
		tenantID = "tn_ledgertest"
		opA      = "0199a1b2-c3d4-7e8f-9a0b-aaaaaaaaaaa1"
		opB      = "0199a1b2-c3d4-7e8f-9a0b-aaaaaaaaaaa2"
	)
	var rowID string
	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM provisioning.erp_submission WHERE tenant_id = $1`, tenantID)
		pool.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id = $1`, tenantID)
	}
	cleanup()
	t.Cleanup(cleanup)
	if err := pool.QueryRow(ctx, `INSERT INTO provisioning.tenant_provisioning (tenant_id, idempotency_key, request_hash)
		VALUES ($1, 'k', 'h') RETURNING tenant_provisioning_id::text`, tenantID).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	key := "tp_" + rowID[0:8] + rowID[9:13] + rowID[14:18] + rowID[19:23] + rowID[24:]

	l := PostgresLedger{DB: pool}
	ref := func(entityID string, version int) FinanceBaselineReference {
		r := FinanceBaselineReference{BaselineID: "fb_" + strings.ToLower(entityID[:3]), LegalEntityID: entityID, Version: version,
			Digest: "sha256:" + strings.Repeat("9f", 32), EffectiveFrom: "2026-04-01"}
		r.Authority.EngineID, r.Authority.SystemOfRecord = "baobab-erp", "FINANCE_BASELINE"
		return r
	}
	refs := []FinanceBaselineReference{ref("LE-B", 2), ref("LE-A", 1)}
	sub := Submission{FinanceBaselines: refs, TenantProvisioningID: key, TenantID: tenantID, LegalEntityIDs: []string{"LE-B", "LE-A"},
		Authority: Authority{TenantProvisioningID: key, PlanID: "plan-1", PlanVersion: 2, PlanDigest: planDigest}}
	st := func(op, state string, rev int64) State {
		return State{OperationID: op, TenantID: tenantID, LegalEntityIDs: []string{"LE-A", "LE-B"}, State: state, Revision: rev}
	}

	if err := l.Submitted(ctx, sub, st(opA, "accepted", 1)); err != nil {
		t.Fatal(err)
	}
	got, found, err := l.Lookup(ctx, opA)
	if err != nil || !found {
		t.Fatalf("lookup: %v %v", found, err)
	}
	// The complete reference set round-trips, in legal entity order, exactly as submitted.
	if len(got.FinanceBaselines) != 2 || got.FinanceBaselines[0] != ref("LE-A", 1) || got.FinanceBaselines[1] != ref("LE-B", 2) {
		t.Fatalf("finance baselines: %+v", got.FinanceBaselines)
	}
	if got.TenantProvisioningID != key || got.Authority != sub.Authority || got.TenantID != tenantID || got.LastRevision != 1 ||
		got.LastState != "accepted" || got.LegalEntityIDs[0] != "LE-A" {
		t.Fatalf("round trip: %+v", got)
	}
	if _, found, _ := l.Lookup(ctx, opB); found {
		t.Fatal("an unknown operation must not be found")
	}
	// The recorded submission is found by its exact approved plan tuple, and only by it.
	if byPlan, found, err := l.ForPlan(ctx, key, sub.Authority); err != nil || !found || byPlan.OperationID != opA {
		t.Fatalf("for plan: %+v %v %v", byPlan, found, err)
	}
	replanned := sub.Authority
	replanned.PlanDigest = "sha256:" + strings.Repeat("1", 64)
	if _, found, err := l.ForPlan(ctx, key, replanned); found || err != nil {
		t.Fatalf("another plan digest has no submission: %v %v", found, err)
	}
	if _, found, err := l.Lookup(ctx, "not-an-id"); found || err != nil {
		t.Fatalf("a malformed id is absent, not an error: %v %v", found, err)
	}

	// An idempotent replay, even one carrying older progress, changes nothing.
	if err := l.Submitted(ctx, sub, st(opA, "accepted", 1)); err != nil {
		t.Fatalf("replay: %v", err)
	}
	// Progress moves forward only.
	if applied, err := l.Apply(ctx, opA, st(opA, "provisioning", 3)); err != nil || !applied {
		t.Fatalf("newer: %v %v", applied, err)
	}
	if applied, _ := l.Apply(ctx, opA, st(opA, "validating", 2)); applied {
		t.Fatal("an older state overwrote a newer one")
	}
	if applied, _ := l.Apply(ctx, opA, st(opA, "validating", 3)); applied {
		t.Fatal("an equal revision is not newer")
	}
	if err := l.Submitted(ctx, sub, st(opA, "accepted", 1)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := l.Lookup(ctx, opA); got.LastRevision != 3 || got.LastState != "provisioning" {
		t.Fatalf("a replay rewound progress: %+v", got)
	}

	// The same operation naming other references is not a replay: it never rewrites what was relied on.
	altered := sub
	altered.FinanceBaselines = []FinanceBaselineReference{ref("LE-B", 3), ref("LE-A", 1)}
	if err := l.Submitted(ctx, altered, st(opA, "accepted", 9)); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("altered references: %v", err)
	}
	// The same references in another order are the same set.
	reordered := sub
	reordered.FinanceBaselines = []FinanceBaselineReference{ref("LE-A", 1), ref("LE-B", 2)}
	if err := l.Submitted(ctx, reordered, st(opA, "accepted", 1)); err != nil {
		t.Fatalf("reordered: %v", err)
	}
	if got, _, _ := l.Lookup(ctx, opA); got.FinanceBaselines[1] != ref("LE-B", 2) || got.LastRevision != 3 {
		t.Fatalf("a conflicting claim changed the record: %+v", got)
	}

	// A second, different operation for the same approved plan is a conflict, never recorded over.
	if err := l.Submitted(ctx, sub, st(opB, "accepted", 1)); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("got %v", err)
	}
	// The same operation id claimed under another plan is a conflict too.
	other := sub
	other.Authority.PlanDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	if err := l.Submitted(ctx, other, st(opA, "accepted", 9)); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("got %v", err)
	}
	if got, _, _ := l.Lookup(ctx, opA); got.LastRevision != 3 || got.Authority.PlanDigest != planDigest {
		t.Fatalf("a conflicting claim changed the record: %+v", got)
	}

	// A replanned (new digest) approval gets its own operation; readiness follows the latest.
	next := sub
	next.Authority.PlanDigest = other.Authority.PlanDigest
	if err := l.Submitted(ctx, next, st(opB, "accepted", 1)); err != nil {
		t.Fatal(err)
	}
	latest, found, err := l.LatestForTenant(ctx, tenantID)
	if err != nil || !found || latest.OperationID != opB {
		t.Fatalf("latest: %+v %v %v", latest, found, err)
	}
	if _, found, _ := l.LatestForTenant(ctx, "tn_nobody"); found {
		t.Fatal("a tenant with no submission has no latest")
	}

	if _, err := pool.Exec(ctx, `UPDATE provisioning.erp_submission SET finance_baselines = '[]'::jsonb WHERE operation_id = $1::uuid`, opA); err == nil {
		t.Fatal("the finance baselines of a submission were rewritten")
	}
	// The link itself is fixed by the database, not by the code path.
	if _, err := pool.Exec(ctx, `UPDATE provisioning.erp_submission SET plan_digest = 'x' WHERE operation_id = $1::uuid`, opA); err == nil {
		t.Fatal("the plan tuple of a submission was rewritten")
	}
	if _, err := pool.Exec(ctx, `UPDATE provisioning.erp_submission SET last_revision = 0 WHERE operation_id = $1::uuid`, opA); err == nil {
		t.Fatal("progress moved backwards")
	}
	if _, err := pool.Exec(ctx, `UPDATE provisioning.erp_submission SET last_state = 'failed' WHERE operation_id = $1::uuid`, opA); err == nil {
		t.Fatal("a state changed without a newer revision")
	}
	if _, err := pool.Exec(ctx, `UPDATE provisioning.erp_submission SET last_state = 'done' WHERE operation_id = $1::uuid`, opA); err == nil {
		t.Fatal("a state outside erp/v1 was accepted")
	}
}
