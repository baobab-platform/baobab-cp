package erpprovisioning

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

func TestSweepPolicyBackoff(t *testing.T) {
	p := DefaultSweepPolicy()
	for n, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 6: 32 * time.Minute, 7: time.Hour, 40: time.Hour} {
		if got := p.Backoff(n); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", n, got, want)
		}
	}
}

type fakeClaims struct {
	ops   []string
	calls int
	err   error
}

func (f *fakeClaims) ClaimOverdue(context.Context, time.Time, SweepPolicy) (string, int, bool, error) {
	if f.err != nil {
		return "", 0, false, f.err
	}
	if f.calls >= len(f.ops) {
		return "", 0, false, nil
	}
	f.calls++
	return f.ops[f.calls-1], 1, true, nil
}

type fakeReconciler struct {
	results map[string]error
	read    []string
}

func (f *fakeReconciler) Reconcile(_ context.Context, op string) (State, bool, error) {
	f.read = append(f.read, op)
	if err := f.results[op]; err != nil {
		return State{}, false, err
	}
	return State{OperationID: op, State: "active", Revision: 3}, op != "same", nil
}

func TestSweeperPass(t *testing.T) {
	claims := &fakeClaims{ops: []string{"ok", "same", "down", "bad", "ok2"}}
	rec := &fakeReconciler{results: map[string]error{"down": errors.New("ERP unavailable"), "bad": ErrStateDisagrees}}
	s := &Sweeper{Claims: claims, Worker: rec, Policy: SweepPolicy{Batch: 4}}
	res, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// One pass is bounded by the batch; a failed read does not stop it; the fifth is left for the next pass.
	if res != (SweepResult{Read: 4, Advanced: 1, Failed: 2}) || len(rec.read) != 4 {
		t.Fatalf("result %+v, read %v", res, rec.read)
	}
	claims.err = errors.New("db down")
	if _, err := s.RunOnce(context.Background()); err == nil {
		t.Fatal("a claim failure ends the pass with an error")
	}
}

// The claim is proved against real PostgreSQL (migration 000100). Set TEST_DATABASE_URL to run it.
func TestClaimOverdue(t *testing.T) {
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

	const tenantID = "tn_sweeptest"
	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM provisioning.erp_submission WHERE tenant_id = $1`, tenantID)
		pool.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id = $1`, tenantID)
	}
	cleanup()
	t.Cleanup(cleanup)
	var rowID string
	if err := pool.QueryRow(ctx, `INSERT INTO provisioning.tenant_provisioning (tenant_id, idempotency_key, request_hash)
		VALUES ($1, 'k', 'h') RETURNING tenant_provisioning_id::text`, tenantID).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	ops := map[string]string{ // plan version -> operation
		"1": "0199a1b2-c3d4-7e8f-9a0b-bbbbbbbbbbb1", // open and quiet: due
		"2": "0199a1b2-c3d4-7e8f-9a0b-bbbbbbbbbbb2", // open but updated a moment ago: inside the grace
		"3": "0199a1b2-c3d4-7e8f-9a0b-bbbbbbbbbbb3", // terminal: never read
		"4": "0199a1b2-c3d4-7e8f-9a0b-bbbbbbbbbbb4", // open but older than the maximum age: given up
	}
	state := map[string]string{"1": "provisioning", "2": "validating", "3": "active", "4": "accepted"}
	for v, op := range ops {
		if _, err := pool.Exec(ctx, `INSERT INTO provisioning.erp_submission (operation_id, tenant_provisioning_id, tenant_id, plan_id,
			plan_version, plan_digest, legal_entity_ids, finance_baselines, last_revision, last_state)
			VALUES ($1::uuid, $2::uuid, $3, 'plan-s', $4::int, 'sha256:d', '{LE-A}', '[]'::jsonb, 1, $5)`, op, rowID, tenantID, v, state[v]); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	age := func(op string, submitted, updated time.Duration) {
		if _, err := pool.Exec(ctx, `UPDATE provisioning.erp_submission SET submitted_at = $2, updated_at = $3 WHERE operation_id = $1::uuid`,
			op, now.Add(-submitted), now.Add(-updated)); err != nil {
			t.Fatal(err)
		}
	}
	age(ops["1"], time.Hour, 30*time.Minute)
	age(ops["2"], time.Hour, time.Minute)
	age(ops["3"], time.Hour, 30*time.Minute)
	age(ops["4"], 100*time.Hour, 99*time.Hour)

	p := DefaultSweepPolicy()
	l := PostgresLedger{DB: pool}
	op, attempt, found, err := l.ClaimOverdue(ctx, now, p)
	if err != nil || !found || op != ops["1"] || attempt != 1 {
		t.Fatalf("claim: %q %d %v %v", op, attempt, found, err)
	}
	// The claim is the lease: the same operation is not offered again until its back-off has passed, and nothing else is due.
	if _, _, found, _ := l.ClaimOverdue(ctx, now.Add(30*time.Second), p); found {
		t.Fatal("a claimed operation is not offered again inside its back-off")
	}
	// After the back-off it is due again, and the back-off doubles with each read that found nothing newer.
	if _, attempt, found, _ := l.ClaimOverdue(ctx, now.Add(61*time.Second), p); !found || attempt != 2 {
		t.Fatalf("due again after the first back-off: %d %v", attempt, found)
	}
	if _, _, found, _ := l.ClaimOverdue(ctx, now.Add(61*time.Second+90*time.Second), p); found {
		t.Fatal("the second back-off is two minutes")
	}
	if _, attempt, found, _ := l.ClaimOverdue(ctx, now.Add(61*time.Second+121*time.Second), p); !found || attempt != 3 {
		t.Fatalf("due again after the second back-off: %d %v", attempt, found)
	}
	// Once the grace has passed the quiet-but-recent operation is due too; the terminal and the over-age ones never are.
	later := now.Add(10 * time.Minute)
	due := map[string]bool{}
	for i := 0; i < 50; i++ { // rows other tests leave in a shared database are claimed too; only this test's own are judged
		op, _, found, _ := l.ClaimOverdue(ctx, later, p)
		if !found {
			break
		}
		for _, mine := range ops {
			if op == mine {
				due[op] = true
			}
		}
	}
	if len(due) != 2 || !due[ops["1"]] || !due[ops["2"]] {
		t.Fatalf("the operation past its grace is due as well, and only the two open ones of this test: %v", due)
	}
	for i := 0; i < 20; i++ {
		if op, _, found, _ := l.ClaimOverdue(ctx, now.Add(time.Duration(i+1)*2*time.Hour), p); found && (op == ops["3"] || op == ops["4"]) {
			t.Fatalf("operation %s must never be swept", op)
		}
	}
	// Recording a newer ERP state resets the back-off.
	if applied, err := l.Apply(ctx, ops["1"], State{OperationID: ops["1"], State: "reconciling", Revision: 2}); err != nil || !applied {
		t.Fatalf("apply: %v %v", applied, err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT sweep_attempts FROM provisioning.erp_submission WHERE operation_id = $1::uuid`, ops["1"]).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("progress resets the back-off: %d %v", attempts, err)
	}
}
