package convergence_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/api"
	"github.com/baobab-platform/baobab-cp/internal/erpprovisioning"
	"github.com/baobab-platform/baobab-cp/internal/eventingress"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// FB-04e. The producer is baobab-erp's real code (testdata/erp_driver.py runs its command store, outbox and signed dispatcher);
// everything on this side is the production ingress handler, inbox, processor, worker and recovery sweep over PostgreSQL.
//
// Inputs (all required; the test is skipped when they are absent unless CONVERGENCE_REQUIRED=1, which CI sets so a missing
// dependency fails instead of silently passing):
//
//	TEST_DATABASE_URL               Control Plane database
//	CONVERGENCE_ERP_DATABASE_URL    an empty ERP database (db/migrate.sh is applied to it)
//	CONVERGENCE_ERP_DIR             a baobab-erp checkout at the pinned revision
//	CONVERGENCE_SHARED_DIR          a Shared checkout (ERP validates its events against it)
const (
	tenant  = "tn_convergence"
	keyID   = "erp-delivery-2026-10"
	source  = "urn:baobab-platform:service:baobab-erp"
	entity  = "LE-A"
	timeout = 3 * time.Minute
)

var secret = bytes.Repeat([]byte{7}, 32)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

type rig struct {
	t        *testing.T
	ctx      context.Context
	cp       *pgxpool.Pool
	erpDir   string
	sharedDB string
	erpDB    string
	ledger   erpprovisioning.PostgresLedger
	worker   erpprovisioning.Worker
	policy   eventingress.Policy
	ingress  *httptest.Server
	erpRead  *httptest.Server
	clock    time.Time // the processor's and sweep's notion of now; advanced by tests to make back-offs due
	// intercept lets a test misbehave on the way in: it sees the delivered revision and says what to do with it.
	intercept func(revision int) action
	proc      *eventingress.Processor
}

type action int

const (
	pass         action = iota // forward to the real ingress and relay its answer
	loseResponse               // forward, then answer 503: the sender cannot know the event was recorded
	refuse                     // do not forward; answer 503: the event has not arrived
)

func need(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		if os.Getenv("CONVERGENCE_REQUIRED") == "1" {
			t.Fatalf("%s is required for the convergence test (CONVERGENCE_REQUIRED=1)", name)
		}
		t.Skipf("%s not set; skipping the cross-repository convergence test", name)
	}
	return v
}

func newRig(t *testing.T) *rig {
	t.Helper()
	cpURL, erpURL := need(t, "TEST_DATABASE_URL"), need(t, "CONVERGENCE_ERP_DATABASE_URL")
	r := &rig{t: t, erpDir: need(t, "CONVERGENCE_ERP_DIR"), sharedDB: need(t, "CONVERGENCE_SHARED_DIR"), erpDB: erpURL}
	var cancel context.CancelFunc
	r.ctx, cancel = context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	store, err := postgres.Open(r.ctx, cpURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(r.ctx); err != nil {
		t.Fatal(err)
	}
	if r.cp, err = pgxpool.New(r.ctx, cpURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.cp.Close)

	migrate := exec.CommandContext(r.ctx, "bash", filepath.Join(r.erpDir, "db", "migrate.sh"))
	migrate.Env = append(os.Environ(), "DATABASE_URL="+erpURL)
	if out, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("ERP migrations: %v\n%s", err, out)
	}
	erp, err := pgxpool.New(r.ctx, erpURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(erp.Close)
	r.clean(erp)
	t.Cleanup(func() { r.clean(erp) })

	r.policy, err = eventingress.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	r.ledger = erpprovisioning.PostgresLedger{DB: r.cp}
	r.clock = time.Now()

	// ERP's authoritative read: the operation read answers exactly the document ERP's own code builds for the stored command.
	r.erpRead = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		op := strings.TrimPrefix(req.URL.Path, "/provisioning-operations/")
		out := r.erp("state", tenant, op)
		if strings.TrimSpace(string(out)) == "null" {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	t.Cleanup(r.erpRead.Close)
	r.worker = erpprovisioning.Worker{Ledger: r.ledger,
		Client: &erpprovisioning.Client{BaseURL: r.erpRead.URL, HTTP: http.DefaultClient, Tokens: staticToken("test")}}
	r.startIngress()
	return r
}

func (r *rig) clean(erp *pgxpool.Pool) {
	ctx := context.Background()
	_, _ = erp.Exec(ctx, `TRUNCATE baobab.event_outbox, baobab.erp_provisioning_command, baobab.erp_provisioning_command_entity CASCADE`)
	_, _ = r.cp.Exec(ctx, `DELETE FROM messaging.event_receipt WHERE source = $1`, source)
	_, _ = r.cp.Exec(ctx, `DELETE FROM provisioning.erp_submission WHERE tenant_id = $1`, tenant)
	_, _ = r.cp.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_id = $1`, tenant)
}

// startIngress starts (or restarts) the Control Plane's HTTP ingress and processor over the same database: everything durable is in
// PostgreSQL, so a restart is a new set of objects over the same rows.
func (r *rig) startIngress() {
	if r.ingress != nil {
		r.ingress.Close()
	}
	key := eventingress.Key{ID: keyID, Sender: "baobab-erp", Secret: secret}
	inbox := eventingress.PostgresInbox{DB: r.cp}
	real := api.New(api.Dependencies{EventIngress: &eventingress.Receiver{Policy: r.policy, Inbox: inbox,
		Keys: func(id string) (eventingress.Key, bool) { return key, id == keyID }}})
	r.ingress = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
		act := pass
		if r.intercept != nil {
			var env struct {
				Data struct {
					Revision int `json:"revision"`
				} `json:"data"`
			}
			_ = json.Unmarshal(body, &env)
			act = r.intercept(env.Data.Revision)
		}
		if act == refuse {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		rec := httptest.NewRecorder()
		real.ServeHTTP(rec, req)
		if act == loseResponse {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	r.t.Cleanup(r.ingress.Close)
	r.newProcessor()
}

// newProcessor is a Control Plane restart as far as processing goes: no state is carried but the database.
func (r *rig) newProcessor() {
	r.proc = &eventingress.Processor{Inbox: eventingress.PostgresInbox{DB: r.cp}, Policy: r.policy,
		Handlers: map[string]eventingress.Apply{erpprovisioning.ProvisioningChangedEvent: r.worker.ApplyEvent},
		Now:      func() time.Time { return r.clock }}
}

func (r *rig) env(extra ...string) []string {
	env := append(os.Environ(), "DATABASE_URL="+r.erpDB, "PYTHONPATH="+filepath.Join(r.erpDir, "modules"),
		"BAOBAB_SHARED_PATH="+r.sharedDB)
	return append(env, extra...)
}

// erp runs the ERP driver (the real ERP code) and returns its stdout.
func (r *rig) erp(args ...string) []byte {
	r.t.Helper()
	out, err := r.erpWith(nil, args...)
	if err != nil {
		r.t.Fatalf("erp_driver %v: %v", args, err)
	}
	return out
}

func (r *rig) erpWith(extra []string, args ...string) ([]byte, error) {
	// The working directory is this package, so the driver is found at testdata/. PYTHONPATH (from env) puts ERP's modules on the path.
	cmd := exec.CommandContext(r.ctx, "python3", append([]string{"-X", "utf8", "testdata/erp_driver.py"}, args...)...)
	cmd.Env = r.env(extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		r.t.Logf("stderr: %s", stderr.String())
	}
	return out, err
}

// dispatch runs one pass of ERP's real dispatcher against the ingress URL, signed with key (the correct one unless a test says otherwise).
// What happened is read from the outbox rows, the dispatcher's durable record, not from its report line (whose per-pass counts are
// shadowed by the backlog totals; see the FB-04e note in docs/runbooks/event-ingress.md).
func (r *rig) dispatch(key []byte) {
	r.t.Helper()
	out, err := r.erpWith([]string{"BAOBAB_CP_EVENT_INGRESS_URL=" + r.ingress.URL + "/v1/integration/events",
		"BAOBAB_CP_EVENT_KEY_ID=" + keyID, "BAOBAB_CP_EVENT_SECRET_B64=" + base64.StdEncoding.EncodeToString(key)}, "dispatch")
	if err != nil || !strings.Contains(string(out), "control-plane-ingress") {
		r.t.Fatalf("dispatch: %v %q", err, out)
	}
}

// outboxIs asserts how many of the operation's outbox rows are delivered, awaiting retry and dead-lettered.
func (r *rig) outboxIs(op string, delivered, retry, dead int) {
	r.t.Helper()
	var d, rt, dl int
	for _, row := range r.outbox(op) {
		switch row.Status {
		case "delivered":
			d++
		case "retry":
			rt++
		case "dead_letter":
			dl++
		}
	}
	if d != delivered || rt != retry || dl != dead {
		r.t.Fatalf("outbox: delivered %d retry %d dead_letter %d, want %d, %d, %d (%+v)", d, rt, dl, delivered, retry, dead, r.outbox(op))
	}
}

type state struct {
	OperationID string `json:"operation_id"`
	State       string `json:"state"`
	Revision    int64  `json:"revision"`
}

type outboxRow struct {
	EventID  string `json:"event_id"`
	Revision int    `json:"revision"`
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
}

// accept creates an ERP command (revision 1, announced) and records the Control Plane's matching submission, as the worker does
// after ERP's 202. withSubmission=false leaves the Control Plane not yet knowing the operation.
func (r *rig) accept(withSubmission bool) string {
	r.t.Helper()
	var acc struct {
		OperationID string          `json:"operation_id"`
		State       json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(r.erp("accept", tenant, entity), &acc); err != nil {
		r.t.Fatal(err)
	}
	if withSubmission {
		r.submit(acc.OperationID, acc.State)
	}
	return acc.OperationID
}

func (r *rig) submit(op string, stateDoc json.RawMessage) {
	r.t.Helper()
	var rowID string
	if err := r.cp.QueryRow(r.ctx, `INSERT INTO provisioning.tenant_provisioning (tenant_id, idempotency_key, request_hash)
		VALUES ($1, $2, 'h') RETURNING tenant_provisioning_id::text`, tenant, "conv-"+op).Scan(&rowID); err != nil {
		r.t.Fatal(err)
	}
	key := "tp_" + rowID[0:8] + rowID[9:13] + rowID[14:18] + rowID[19:23] + rowID[24:]
	st, err := erpprovisioning.ParseState(stateDoc)
	if err != nil {
		r.t.Fatal(err)
	}
	sub := erpprovisioning.Submission{TenantProvisioningID: key, TenantID: tenant, LegalEntityIDs: []string{entity},
		Authority: erpprovisioning.Authority{TenantProvisioningID: key, PlanID: "plan-conv", PlanVersion: 1, PlanDigest: "sha256:" + strings.Repeat("0", 64)}}
	if err := r.ledger.Submitted(r.ctx, sub, st); err != nil {
		r.t.Fatal(err)
	}
}

// advanceAll moves the ERP command through the remaining states, one committed revision each, and returns the final revision.
func (r *rig) advanceAll(op string) int64 {
	r.t.Helper()
	var last int64 = 1
	for _, s := range []string{"provisioning", "reconciling", "active"} {
		var doc state
		if err := json.Unmarshal(r.erp("advance", op, s), &doc); err != nil {
			r.t.Fatal(err)
		}
		last = doc.Revision
	}
	return last
}

func (r *rig) outbox(op string) []outboxRow {
	r.t.Helper()
	var rows []outboxRow
	if err := json.Unmarshal(r.erp("outbox", op), &rows); err != nil {
		r.t.Fatal(err)
	}
	return rows
}

func (r *rig) cpState(op string) (int64, string) {
	r.t.Helper()
	sub, found, err := r.ledger.Lookup(r.ctx, op)
	if err != nil || !found {
		r.t.Fatalf("control plane submission %s: %v %v", op, found, err)
	}
	return sub.LastRevision, sub.LastState
}

// run applies every due recorded event.
func (r *rig) run() eventingress.Summary {
	r.t.Helper()
	s, err := r.proc.RunOnce(r.ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

// receipts counts recorded receipts and how many are applied, for the operation's events.
func (r *rig) receipts(op string) (total, applied int) {
	r.t.Helper()
	ids := []string{}
	for _, row := range r.outbox(op) {
		ids = append(ids, row.EventID)
	}
	if err := r.cp.QueryRow(r.ctx, `SELECT count(*), count(*) FILTER (WHERE status = 'applied') FROM messaging.event_receipt
		WHERE source = $1 AND event_id::text = ANY($2::text[])`, source, ids).Scan(&total, &applied); err != nil {
		r.t.Fatal(err)
	}
	return
}

// converged is the acceptance: the Control Plane holds exactly the revision and state ERP's authoritative read answers.
func (r *rig) converged(op string) {
	r.t.Helper()
	var erp state
	if err := json.Unmarshal(r.erp("state", tenant, op), &erp); err != nil {
		r.t.Fatal(err)
	}
	rev, st := r.cpState(op)
	if rev != erp.Revision || st != erp.State {
		r.t.Fatalf("not converged: control plane has %s r%d, ERP says %s r%d", st, rev, erp.State, erp.Revision)
	}
}

func TestEventToControlPlaneConvergence(t *testing.T) {
	t.Run("an operation's every revision is delivered, recorded once and applied", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		final := r.advanceAll(op)
		r.dispatch(secret)
		r.outboxIs(op, int(final), 0, 0)
		if total, applied := r.receipts(op); total != int(final) || applied != 0 {
			t.Fatalf("receipts %d applied %d: a 2xx means recorded, not applied", total, applied)
		}
		if s := r.run(); s.Applied != int(final) {
			t.Fatalf("processor: %+v", s)
		}
		r.converged(op)
		if rev, st := r.cpState(op); rev != 4 || st != "active" {
			t.Fatalf("final %s r%d", st, rev)
		}
		for _, row := range r.outbox(op) {
			if row.Status != "delivered" {
				t.Fatalf("outbox row r%d is %s", row.Revision, row.Status)
			}
		}
	})

	t.Run("a lost response is retried and the redelivery is a duplicate, not a second application", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		r.advanceAll(op)
		r.intercept = func(rev int) action {
			if rev == 2 {
				return loseResponse
			}
			return pass
		}
		r.dispatch(secret)
		r.outboxIs(op, 3, 1, 0)
		// The CP recorded revision 2 although ERP could not know; the retry gets a duplicate receipt and is delivered.
		r.intercept = nil
		r.erp("due", op)
		r.dispatch(secret)
		r.outboxIs(op, 4, 0, 0)
		if total, _ := r.receipts(op); total != 4 {
			t.Fatalf("one receipt per revision expected, got %d", total)
		}
		r.run()
		r.converged(op)
	})

	t.Run("revisions arriving out of order never move the state back", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		r.advanceAll(op)
		r.intercept = func(rev int) action {
			if rev == 2 {
				return refuse
			}
			return pass
		}
		r.dispatch(secret)
		r.run()
		if rev, _ := r.cpState(op); rev != 4 {
			t.Fatalf("revisions 1, 3 and 4 applied, want r4, got r%d", rev)
		}
		r.intercept = nil
		r.erp("due", op)
		r.dispatch(secret)
		if s := r.run(); s.Applied != 1 {
			t.Fatalf("the late revision is applied (and ignored as older): %+v", s)
		}
		if rev, st := r.cpState(op); rev != 4 || st != "active" {
			t.Fatalf("late revision 2 moved the state to %s r%d", st, rev)
		}
		r.converged(op)
	})

	t.Run("events that arrive before the Control Plane has recorded the operation wait and then apply", func(t *testing.T) {
		r := newRig(t)
		var acc struct {
			OperationID string          `json:"operation_id"`
			State       json.RawMessage `json:"state"`
		}
		_ = json.Unmarshal(r.erp("accept", tenant, entity), &acc)
		r.advanceAll(acc.OperationID)
		r.dispatch(secret)
		if s := r.run(); s.Applied != 0 || s.Retried != 4 {
			t.Fatalf("with no submission recorded every event waits: %+v", s)
		}
		if total, applied := r.receipts(acc.OperationID); total != 4 || applied != 0 {
			t.Fatalf("receipts %d applied %d: nothing is lost while waiting", total, applied)
		}
		r.submit(acc.OperationID, acc.State) // the worker records its submission after ERP's 202
		r.clock = r.clock.Add(time.Hour)     // the back-off has passed
		if s := r.run(); s.Applied != 4 {
			t.Fatalf("after the submission exists: %+v", s)
		}
		r.converged(acc.OperationID)
	})

	t.Run("a Control Plane restart between receipt and processing loses nothing", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		r.advanceAll(op)
		r.dispatch(secret)
		r.startIngress() // the process is gone: new ingress, new processor, same database
		if s := r.run(); s.Applied != 4 {
			t.Fatalf("after restart: %+v", s)
		}
		r.converged(op)
	})

	t.Run("an ERP outage then recovery delivers the backlog", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		r.advanceAll(op)
		r.intercept = func(int) action { return refuse }
		r.dispatch(secret)
		r.outboxIs(op, 0, 4, 0)
		if total, _ := r.receipts(op); total != 0 {
			t.Fatalf("nothing was recorded during the outage, got %d", total)
		}
		r.intercept = nil
		r.erp("due", op)
		r.dispatch(secret)
		r.outboxIs(op, 4, 0, 0)
		r.run()
		r.converged(op)
	})

	t.Run("a wrongly signed delivery is refused and recorded nowhere", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		r.advanceAll(op)
		r.dispatch(bytes.Repeat([]byte{9}, 32))
		r.outboxIs(op, 0, 4, 0) // a 401 is retried, never accepted
		if total, _ := r.receipts(op); total != 0 {
			t.Fatalf("a refused delivery was recorded: %d receipts", total)
		}
		r.erp("due", op)
		r.dispatch(secret)
		r.run()
		r.converged(op)
	})

	t.Run("a conflicting event is dead-lettered by the sender and the rest still converge", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		r.advanceAll(op)
		rows := r.outbox(op)
		// Someone delivered different bytes under revision 2's identity first.
		r.postConflicting(op, rows[1].EventID)
		r.dispatch(secret)
		r.outboxIs(op, 3, 0, 1) // the 409 is dead-lettered at once, not retried
		if got := r.outbox(op)[1]; got.Status != "dead_letter" {
			t.Fatalf("revision 2 is %s", got.Status)
		}
		r.run()
		r.converged(op)
	})

	t.Run("the recovery sweep repairs events that were never delivered", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		r.advanceAll(op) // ERP committed four revisions; nothing was ever sent
		if rev, _ := r.cpState(op); rev != 1 {
			t.Fatalf("setup: control plane at r%d", rev)
		}
		sweep := &erpprovisioning.Sweeper{Claims: r.ledger, Worker: r.worker, Now: func() time.Time { return r.clock.Add(time.Hour) },
			Policy: erpprovisioning.SweepPolicy{Grace: time.Minute, BackoffBase: time.Minute, BackoffMax: time.Hour, MaxAge: 72 * time.Hour, Batch: 10}}
		res, err := sweep.RunOnce(r.ctx)
		if err != nil || res.Read != 1 || res.Advanced != 1 {
			t.Fatalf("sweep: %+v %v", res, err)
		}
		r.converged(op)
		// Terminal now: it is never read again.
		if res, _ := sweep.RunOnce(r.ctx); res.Read != 0 {
			t.Fatalf("a terminal operation was swept again: %+v", res)
		}
	})

	t.Run("the sweep and a late event agree and neither rewinds the other", func(t *testing.T) {
		r := newRig(t)
		op := r.accept(true)
		r.advanceAll(op)
		sweep := &erpprovisioning.Sweeper{Claims: r.ledger, Worker: r.worker, Now: func() time.Time { return r.clock.Add(time.Hour) },
			Policy: erpprovisioning.SweepPolicy{Grace: time.Minute, BackoffBase: time.Minute, BackoffMax: time.Hour, MaxAge: 72 * time.Hour, Batch: 10}}
		if _, err := sweep.RunOnce(r.ctx); err != nil {
			t.Fatal(err)
		}
		r.dispatch(secret) // the events arrive after the sweep already repaired the state
		r.run()
		r.converged(op)
	})
}

// postConflicting delivers a signed event under a recorded identity with different content.
func (r *rig) postConflicting(op, eventID string) {
	r.t.Helper()
	// A well-formed, correctly signed event with revision 2's (source, id) but other content.
	body, _ := json.Marshal(map[string]any{
		"specversion": "1.0", "id": eventID, "type": erpprovisioning.ProvisioningChangedEvent, "source": source,
		"subject": "provisioning:" + op, "time": "2026-10-07T17:30:00Z", "datacontenttype": "application/json",
		"dataschema": "https://contracts.baobab-platform.com/erp/v1/provisioning-state.schema.json", "baobabscope": "tenant",
		"correlationid": "e7a5b216-c90d-4a36-99f3-e1d81276638f", "tenantid": tenant, "idempotencykey": "conflict-" + eventID,
		"data": map[string]any{"operation_id": op, "tenant_id": tenant, "legal_entity_ids": []string{entity}, "state": "validating",
			"revision": 2, "updated_at": "2026-10-07T17:30:00Z"},
	})
	post := func(b []byte) int {
		ts := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		req, _ := http.NewRequestWithContext(r.ctx, http.MethodPost, r.ingress.URL+"/v1/integration/events", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/cloudevents+json")
		req.Header.Set("Baobab-Key-Id", keyID)
		req.Header.Set("Baobab-Timestamp", ts)
		req.Header.Set("Baobab-Signature", eventingress.Sign(eventingress.Key{ID: keyID, Secret: secret}, ts, b))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			r.t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(body); code != http.StatusAccepted {
		r.t.Fatalf("seeding the identity: %d", code)
	}
}
