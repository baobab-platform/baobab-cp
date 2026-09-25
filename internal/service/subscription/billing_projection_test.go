package subscription_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/billing"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/application"
)

// ADR-BCP-018 gate ORG-11 end to end: a classified ProductSubscription is
// projected into baobab-subscriptions through the Shared subscriptions/v1
// contract. The engine is a contract-validating fake by default; set
// BILLING_E2E_URL and BILLING_E2E_TOKEN_FILE to run the same acceptance
// against a real baobab-subscriptions (see docs/certification).

const workloadToken = "test-workload-token"

// fakeEngine is baobab-subscriptions as the contract describes it: it
// validates every request against Shared subscriptions/v1, never regresses
// an authoritative revision, replays idempotent requests, and answers with
// documents the Control Plane validates in turn. It never calls payments:
// payments counts any call that reaches the payments fake.
type fakeEngine struct {
	mu          sync.Mutex
	projections map[string]map[string]any // by product_subscription_id
	replays     map[string][]byte
	requests    []map[string]any
	fail        atomic.Int32 // answer this many requests with 503
	lieInternal atomic.Bool  // answer INTERNAL with a charging policy
	payments    atomic.Int32
	seq         int
}

var (
	ensureSchema  = contracts.MustSchema("subscriptions/v1/billing.schema.json#/$defs/EnsureBillingProjectionRequest")
	commandSchema = contracts.MustSchema("subscriptions/v1/billing.schema.json#/$defs/BillingProjectionCommand")
)

func newFakeEngine(t *testing.T) (*fakeEngine, *httptest.Server, *httptest.Server) {
	f := &fakeEngine{projections: map[string]map[string]any{}, replays: map[string][]byte{}}
	engine := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(engine.Close)
	payments := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.payments.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(payments.Close)
	return f, engine, payments
}

func problemResponse(w http.ResponseWriter, status int, code string, retryable bool) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:baobab-platform:problem:" + strings.ToLower(code), "title": code,
		"status": status, "code": code, "retryable": retryable})
}

func (f *fakeEngine) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+workloadToken {
		problemResponse(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", false)
		return
	}
	if f.fail.Load() > 0 {
		f.fail.Add(-1)
		problemResponse(w, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", true)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	key := r.Header.Get("Idempotency-Key")
	f.mu.Lock()
	defer f.mu.Unlock()
	if prior, ok := f.replays[key]; ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(prior)
		return
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.requests = append(f.requests, body)
	var (
		proj map[string]any
		code string
	)
	switch {
	case r.URL.Path == "/v1/billing-projections":
		if err := contracts.Validate(ensureSchema, raw); err != nil {
			problemResponse(w, http.StatusBadRequest, "VALIDATION_FAILED", false)
			return
		}
		proj, code = f.ensure(body)
	case strings.HasPrefix(r.URL.Path, "/v1/billing-projections/"):
		if err := contracts.Validate(commandSchema, raw); err != nil {
			problemResponse(w, http.StatusBadRequest, "VALIDATION_FAILED", false)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/billing-projections/"), "/")
		proj, code = f.command(parts[0], parts[1], body)
	default:
		problemResponse(w, http.StatusNotFound, "NOT_FOUND", false)
		return
	}
	if code != "" {
		problemResponse(w, http.StatusConflict, code, false)
		return
	}
	out, _ := json.Marshal(proj)
	f.replays[key] = out
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

func revision(v any) int64 { n, _ := v.(float64); return int64(n) }

func (f *fakeEngine) ensure(req map[string]any) (map[string]any, string) {
	id := req["product_subscription_id"].(string)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p, ok := f.projections[id]
	if ok {
		if revision(req["authoritative_revision"]) < revision(p["authoritative_revision"]) {
			return nil, "STALE_AUTHORITATIVE_REVISION"
		}
	} else {
		f.seq++
		p = map[string]any{"billing_subscription_id": fmt.Sprintf("bsub_%032d", f.seq), "tenant_id": req["tenant_id"],
			"product_subscription_id": id, "product_id": req["product_id"], "version": float64(0), "created_at": now,
			"provider": map[string]any{"kind": "TEMPORARY", "simulated": true}, "operational_condition": "HEALTHY"}
		f.projections[id] = p
	}
	p["authoritative_revision"] = req["authoritative_revision"]
	p["subscription_type"] = req["subscription_type"]
	p["classification"] = req["classification"]
	p["version"] = p["version"].(float64) + 1
	p["updated_at"] = now
	if req["subscription_type"] == "INTERNAL" && !f.lieInternal.Load() {
		p["billing_policy"] = map[string]any{"monetary_charge": "ZERO", "billing_required": false, "usage_metering": true, "payment_execution": "NEVER"}
		p["billing_state"] = "ACTIVE"
		p["readiness"] = map[string]any{"status": "READY", "blockers": []string{}, "facts": map[string]any{"classification_valid": true,
			"projection_valid": true, "metering_available": true, "billing_configuration_complete": false, "provider_ready": false,
			"payment_path_ready": false}}
	} else {
		p["billing_policy"] = map[string]any{"monetary_charge": "PRICED", "billing_required": true, "usage_metering": true, "payment_execution": "REQUIRED"}
		p["billing_state"] = "PENDING_CONFIGURATION"
		p["readiness"] = map[string]any{"status": "BLOCKED", "blockers": []string{"PRICING_CONFIGURATION_MISSING", "BILLING_ACCOUNT_MISSING",
			"BILLING_PROVIDER_NOT_CONFIGURED", "PAYMENT_PATH_NOT_READY"}, "facts": map[string]any{"classification_valid": true,
			"projection_valid": true, "metering_available": true, "billing_configuration_complete": false, "provider_ready": false,
			"payment_path_ready": false}}
	}
	return p, ""
}

func (f *fakeEngine) command(billingID, action string, req map[string]any) (map[string]any, string) {
	for _, p := range f.projections {
		if p["billing_subscription_id"] != billingID || p["tenant_id"] != req["tenant_id"] {
			continue
		}
		if revision(req["authoritative_revision"]) < revision(p["authoritative_revision"]) {
			return nil, "STALE_AUTHORITATIVE_REVISION"
		}
		if p["billing_state"] == "TERMINATED" {
			return nil, "BILLING_PROJECTION_TERMINATED"
		}
		readiness := p["readiness"].(map[string]any)
		switch action {
		case "suspend":
			p["billing_state"] = "SUSPENDED"
			readiness["status"], readiness["blockers"] = "BLOCKED", []string{"PROJECTION_SUSPENDED"}
		case "terminate":
			p["billing_state"] = "TERMINATED"
			readiness["status"], readiness["blockers"] = "BLOCKED", []string{"PROJECTION_TERMINATED"}
		}
		p["version"] = p["version"].(float64) + 1
		return p, ""
	}
	return nil, "BILLING_PROJECTION_NOT_FOUND"
}

func (f *fakeEngine) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// projection is the engine a test drives: the fake, or a real
// baobab-subscriptions when BILLING_E2E_URL is set.
type projection struct {
	fake      *fakeEngine
	client    *billing.Client
	projector *billing.Projector
	now       time.Time
}

func (e *env) projection(t *testing.T, allowReal bool) *projection {
	t.Helper()
	p := &projection{now: time.Now().UTC()}
	if url := os.Getenv("BILLING_E2E_URL"); url != "" && allowReal {
		p.client = &billing.Client{BaseURL: url, Tokens: billing.FileTokenSource{Path: os.Getenv("BILLING_E2E_TOKEN_FILE")}}
	} else {
		f, engine, _ := newFakeEngine(t)
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/token", []byte(workloadToken+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		p.fake = f
		p.client = &billing.Client{BaseURL: engine.URL, Tokens: billing.FileTokenSource{Path: dir + "/token"}}
	}
	p.projector = &billing.Projector{Repo: e.repo, Engine: p.client, Now: func() time.Time { return p.now }, Batch: 1000}
	return p
}

// converge runs passes until the subscription's projection is recorded at
// its current revision. Other tests' subscriptions may be projected too.
func (p *projection) converge(t *testing.T, e *env, sub string) repository.BillingProjectionSyncState {
	t.Helper()
	target, err := e.repo.GetSubscriptionClassificationTarget(e.ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := p.projector.SyncPending(e.ctx); err != nil {
			t.Fatal(err)
		}
		s, err := e.repo.GetBillingProjectionSync(e.ctx, sub)
		if err == nil && s.Revision == target.Version && s.LastErrorCode == "" {
			return s
		}
	}
	s, err := e.repo.GetBillingProjectionSync(e.ctx, sub)
	t.Fatalf("projection of %s did not converge to revision %d: %v %+v", sub, target.Version, err, s)
	return s
}

func (e *env) internalSubscription(t *testing.T, g group) (tenantID, sub string) {
	t.Helper()
	apps := &application.Service{Repo: e.repo, Eligibility: g.resolver, Now: e.clock}
	applicant, reviewer, decider, approver := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	tenantID, sub = e.tenant(t, g.sub, g.platform, "", "")
	decision := e.decision(t, apps, applicant, reviewer, decider, internalDecision(g.sub))
	if _, _, err := e.classifier(g).ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID)); err != nil {
		t.Fatal(err)
	}
	return tenantID, sub
}

// TestInternalBillingProjection: the ZuriBeans chain ends in an INTERNAL
// projection that bills nothing, needs no payment path, and is READY; the
// payments engine is never called.
func TestInternalBillingProjection(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	p := e.projection(t, true)
	tenantID, sub := e.internalSubscription(t, g)

	s := p.converge(t, e, sub)
	if s.BillingState != "ACTIVE" || s.ReadinessStatus != "READY" || !strings.HasPrefix(s.BillingSubscriptionID, "bsub_") {
		t.Fatalf("INTERNAL projection: %+v", s)
	}
	target, _ := e.repo.GetSubscriptionClassificationTarget(e.ctx, sub)
	proj, err := p.client.Ensure(e.ctx, billing.EnsureRequest{TenantID: tenantID, ProductSubscriptionID: sub,
		AuthoritativeRevision: target.Version, ProductID: target.ProductID, SubscriptionType: "INTERNAL",
		Classification: e.classification(t, sub)}, "cp-test-"+token(), "")
	if err != nil {
		t.Fatal(err)
	}
	if proj.BillingSubscriptionID != s.BillingSubscriptionID || proj.BillingPolicy.BillingRequired ||
		proj.BillingPolicy.PaymentExecution != "NEVER" || proj.BillingPolicy.MonetaryCharge != "ZERO" || !proj.BillingPolicy.UsageMetering ||
		len(proj.Readiness.Blockers) != 0 {
		t.Fatalf("INTERNAL is zero charge, metered, never payments: %+v", proj)
	}
	if p.fake != nil {
		if n := p.fake.payments.Load(); n != 0 {
			t.Fatalf("payments must never be called for INTERNAL, got %d calls", n)
		}
		before := p.fake.requestCount()
		if _, err := p.projector.SyncPending(e.ctx); err != nil {
			t.Fatal(err)
		}
		if after := p.fake.requestCount(); after != before {
			t.Fatalf("a converged projection is not re-sent: %d -> %d requests", before, after)
		}
	}
}

// TestCommercialBillingProjectionIsBlocked: Acme, an EXTERNAL_CLIENT, is
// projected COMMERCIAL and stays BLOCKED on the temporary provider; nothing
// fakes commercial readiness.
func TestCommercialBillingProjectionIsBlocked(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	p := e.projection(t, true)
	apps := &application.Service{Repo: e.repo, Eligibility: g.resolver, Now: e.clock}
	applicant, reviewer, decider, approver := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	acme := e.organisation(t)
	decision := e.decision(t, apps, applicant, reviewer, decider, commercialDecision)
	_, sub := e.tenant(t, acme, g.platform, domain.PlatformRelExternalClient, decision.ID)
	if _, _, err := e.classifier(g).ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID)); err != nil {
		t.Fatal(err)
	}
	s := p.converge(t, e, sub)
	if s.ReadinessStatus != "BLOCKED" || s.BillingState == "ACTIVE" {
		t.Fatalf("COMMERCIAL on a temporary provider is never ready: %+v", s)
	}
}

// TestDivestitureReprojectsAndStaleRevisionIsRefused: reclassifying the
// divested ZuriBeans subscription to COMMERCIAL re-projects the same billing
// projection at the new revision, and the old revision cannot undo it.
func TestDivestitureReprojectsAndStaleRevisionIsRefused(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	p := e.projection(t, true)
	tenantID, sub := e.internalSubscription(t, g)
	internal := p.converge(t, e, sub)
	oldClassification := e.classification(t, sub)

	if _, _, err := e.classifier(g).Reclassify(e.ctx, e.principal(t), sub, []byte(`{"subscription_type":"COMMERCIAL",
		"classification_reference":"chg_divested","reason":"Divested: no longer a group affiliate."}`)); err != nil {
		t.Fatal(err)
	}
	commercial := p.converge(t, e, sub)
	if commercial.Revision != internal.Revision+1 || commercial.BillingSubscriptionID != internal.BillingSubscriptionID ||
		commercial.ReadinessStatus != "BLOCKED" {
		t.Fatalf("reclassification converges the same projection at the next revision: %+v -> %+v", internal, commercial)
	}
	target, _ := e.repo.GetSubscriptionClassificationTarget(e.ctx, sub)
	_, err := p.client.Ensure(e.ctx, billing.EnsureRequest{TenantID: tenantID, ProductSubscriptionID: sub,
		AuthoritativeRevision: internal.Revision, ProductID: target.ProductID, SubscriptionType: "INTERNAL",
		Classification: oldClassification}, "cp-test-"+token(), "")
	var problem *billing.Problem
	if !errors.As(err, &problem) || problem.Status != http.StatusConflict || problem.Code != "STALE_AUTHORITATIVE_REVISION" {
		t.Fatalf("an older revision never regresses the projection: %v", err)
	}
}

// TestBillingProjectionFailsClosed: an unavailable engine is retried with
// backoff, and an engine answer that contradicts the INTERNAL policy is
// never recorded.
func TestBillingProjectionFailsClosed(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	p := e.projection(t, false)
	_, sub := e.internalSubscription(t, g)

	p.fake.fail.Store(1000)
	if _, err := p.projector.SyncPending(e.ctx); err != nil {
		t.Fatal(err)
	}
	s, err := e.repo.GetBillingProjectionSync(e.ctx, sub)
	if err != nil || s.LastErrorCode != "BILLING_UNAVAILABLE" || s.Attempts != 1 || s.Revision != 0 {
		t.Fatalf("an unavailable engine is recorded as a failed attempt: %v %+v", err, s)
	}
	if work, _ := e.repo.ListBillingProjectionWork(e.ctx, p.now, 1000); containsWork(work, sub) {
		t.Fatal("a failed projection waits for its backoff")
	}
	p.fake.fail.Store(0)
	p.fake.lieInternal.Store(true)
	p.now = p.now.Add(time.Minute)
	if _, err := p.projector.SyncPending(e.ctx); err != nil {
		t.Fatal(err)
	}
	if s, _ := e.repo.GetBillingProjectionSync(e.ctx, sub); s.Revision != 0 || s.LastErrorCode != "BILLING_CONTRACT_VIOLATION" {
		t.Fatalf("an engine answer that charges INTERNAL violates the contract and is never recorded: %+v", s)
	}
	p.fake.lieInternal.Store(false)
	p.fake.mu.Lock()
	clear(p.fake.replays) // a fixed engine no longer replays its wrong answer
	p.fake.mu.Unlock()
	p.now = p.now.Add(2 * time.Hour)
	if s := p.converge(t, e, sub); s.ReadinessStatus != "READY" || s.Attempts != 0 {
		t.Fatalf("the projection converges once the engine answers correctly: %+v", s)
	}
}

func containsWork(work []repository.BillingProjectionWork, sub string) bool {
	for _, w := range work {
		if w.SubscriptionID == sub {
			return true
		}
	}
	return false
}

func (e *env) classification(t *testing.T, sub string) billing.Classification {
	t.Helper()
	records, err := e.repo.ListSubscriptionClassifications(e.ctx, sub)
	if err != nil || len(records) == 0 {
		t.Fatalf("classification of %s: %v", sub, err)
	}
	c := records[0]
	return billing.Classification{ClassificationID: c.ID, ClassificationSource: string(c.Source),
		ClassificationReference: c.Reference, ClassifiedAt: c.ClassifiedAt}
}

// TestEnginesRegisterFromSharedRegistrations: every embedded Shared
// EngineRegistration registers through the same path; re-registering
// converges; in production, providers not permitted there are refused.
func TestEnginesRegisterFromSharedRegistrations(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key IN
		('baobab-subscriptions.temporary-billing', 'baobab-payments.sandbox')`); err != nil {
		t.Fatal(err)
	}
	registered, err := billing.RegisterEmbeddedEngines(ctx, e.repo, "", discard())
	if err != nil || len(registered) != 0 {
		t.Fatalf("production registers no simulated provider: %v %v", err, registered)
	}
	for range 2 {
		registered, err = billing.RegisterEmbeddedEngines(ctx, e.repo, "integration", discard())
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(registered) != 2 {
		t.Fatalf("both engines register: %v", registered)
	}
	for provider, engine := range map[string]string{"baobab-subscriptions.temporary-billing": "baobab-subscriptions",
		"baobab-payments.sandbox": "baobab-payments"} {
		var engineCode string
		var supports int
		var simulated bool
		if err := e.admin.QueryRow(ctx, `SELECT e.code, (p.metadata->>'simulated')::boolean,
				(SELECT count(*) FROM capability.provider_capability_support s WHERE s.provider_id = p.provider_id)
			FROM capability.capability_provider p JOIN topology.engine e ON e.engine_id = p.engine_id WHERE p.provider_key = $1`,
			provider).Scan(&engineCode, &simulated, &supports); err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if engineCode != engine || !simulated || supports == 0 {
			t.Fatalf("%s: engine %s simulated %v supports %d", provider, engineCode, simulated, supports)
		}
	}
	if _, err := e.repo.GetCapability(ctx, "billing.subscription.manage"); err != nil {
		t.Fatal(err)
	}
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
