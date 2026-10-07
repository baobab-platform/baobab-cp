package erpprovisioning

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const (
	tenant      = "tn_01k4zuribeans"
	provisionID = "tp_01k4zuribeans"
	planDigest  = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	entity      = "ZURIBEANS-ZA"
	operationID = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b"
	issuer      = "https://iam.example.invalid/realms/baobab"
	subject     = "baobab-cp-provisioning-workload"
)

type token string

func (t token) Token(context.Context) (string, error) { return string(t), nil }

type erpFake struct {
	mu      sync.Mutex
	posts   []http.Header
	bodies  []map[string]any
	gets    int
	status  int
	problem string
	state   map[string]any
	retry   string
}

func newState(state string, revision int) map[string]any {
	return map[string]any{"operation_id": operationID, "tenant_id": tenant, "legal_entity_ids": []string{entity},
		"state": state, "revision": revision, "updated_at": "2026-10-07T07:00:00Z"}
}

func (f *erpFake) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.status >= 400 {
			if f.retry != "" {
				w.Header().Set("Retry-After", f.retry)
			}
			w.WriteHeader(f.status)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": f.problem, "status": f.status, "detail": "private detail must not travel"})
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/provisioning-operations":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			f.posts = append(f.posts, r.Header.Clone())
			f.bodies = append(f.bodies, body)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/provisioning-operations/"+operationID:
			f.gets++
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(f.state)
	})
}

type source struct {
	auth Authorised
	err  error
}

func (s source) Authorised(context.Context, string) (Authorised, error) { return s.auth, s.err }

func approved() Authorised {
	return Authorised{TenantID: tenant, LegalEntityIDs: []string{entity}, Countries: []string{"ZA"}, Currencies: []string{"ZAR"},
		Authority: Authority{TenantProvisioningID: provisionID, PlanID: "plan_01k4zuribeans", PlanVersion: 2, PlanDigest: planDigest}}
}

type identities struct {
	repository.IdentityRepository
	principal string
	err       error
	seen      [][2]string
}

func (i *identities) ResolveIdentity(_ context.Context, iss, sub string) (domain.Principal, error) {
	i.seen = append(i.seen, [2]string{iss, sub})
	if i.err != nil {
		return domain.Principal{}, i.err
	}
	return domain.Principal{ID: i.principal}, nil
}

type contexts struct {
	repository.ContextWriter
	created []domain.Context
	err     error
}

func (c *contexts) CreateContext(_ context.Context, ctx domain.Context) error {
	if c.err != nil {
		return c.err
	}
	// The real store validates before it inserts; a context the domain refuses must not reach a test as valid.
	if err := ctx.Validate(); err != nil {
		return err
	}
	c.created = append(c.created, ctx)
	return nil
}

type ledger struct {
	subs map[string]Submission
	last map[string]int64
}

func newLedger() *ledger { return &ledger{subs: map[string]Submission{}, last: map[string]int64{}} }
func (l *ledger) Submitted(_ context.Context, sub Submission, st State) error {
	l.subs[sub.OperationID] = sub
	if st.Revision > l.last[sub.OperationID] {
		l.last[sub.OperationID] = st.Revision
	}
	return nil
}
func (l *ledger) Lookup(_ context.Context, id string) (Submission, bool, error) {
	s, ok := l.subs[id]
	return s, ok, nil
}
func (l *ledger) Apply(_ context.Context, id string, st State) (bool, error) {
	if st.Revision <= l.last[id] {
		return false, nil
	}
	l.last[id] = st.Revision
	return true, nil
}

type rig struct {
	erp *erpFake
	ids *identities
	ctx *contexts
	led *ledger
	w   Worker
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{erp: &erpFake{state: newState("accepted", 1)}, ids: &identities{principal: "pr_provisioner"}, ctx: &contexts{}, led: newLedger()}
	srv := httptest.NewServer(r.erp.handler())
	t.Cleanup(srv.Close)
	r.w = Worker{
		Source: source{auth: approved()}, Ledger: r.led,
		Client: &Client{BaseURL: srv.URL + "/v1", Tokens: token("provisioner-token")},
		Context: ContextIssuer{Identities: r.ids, Contexts: r.ctx, Issuer: issuer, Subject: subject, TTL: 15 * time.Minute,
			Now: func() time.Time { return time.Date(2026, 10, 7, 7, 0, 0, 0, time.UTC) }},
	}
	return r
}

func TestSubmitSendsTheApprovedTupleUnderTheProvisionersOwnContext(t *testing.T) {
	r := newRig(t)
	st, err := r.w.Submit(context.Background(), provisionID)
	if err != nil {
		t.Fatal(err)
	}
	if st.OperationID != operationID || st.State != "accepted" {
		t.Fatalf("state=%+v", st)
	}
	if len(r.ctx.created) != 1 {
		t.Fatalf("contexts=%d", len(r.ctx.created))
	}
	cx := r.ctx.created[0]
	if cx.PrincipalID != "pr_provisioner" || cx.TenantID != tenant || cx.ExpiresAt == nil || !cx.ExpiresAt.After(cx.ResolvedAt) {
		t.Fatalf("context must be tenant-bound, bounded and owned by the provisioner principal: %+v", cx)
	}
	// It is pre-activation authority, bound to exactly the approved plan, and never ordinary runtime authority.
	if cx.Purpose() != domain.ContextPurposeTenantProvisioning || cx.IsRuntime() || cx.ProvisioningAuthority == nil ||
		*cx.ProvisioningAuthority != (domain.ProvisioningAuthority{TenantProvisioningID: provisionID, PlanID: "plan_01k4zuribeans", PlanVersion: 2, PlanDigest: planDigest}) {
		t.Fatalf("context must be a TENANT_PROVISIONING context bound to the approved plan: %+v", cx)
	}
	if cx.ExpiresAt.Sub(cx.ResolvedAt) > domain.MaxProvisioningContextLifetime {
		t.Fatalf("a provisioning context lives at most 15 minutes: %v", cx.ExpiresAt.Sub(cx.ResolvedAt))
	}
	if got := r.ids.seen[0]; got != [2]string{issuer, subject} {
		t.Fatalf("principal resolved for %v", got)
	}
	h, body := r.erp.posts[0], r.erp.bodies[0]
	if h.Get("Authorization") != "Bearer provisioner-token" {
		t.Fatal("the provisioner's token must be the caller")
	}
	if k := h.Get("Idempotency-Key"); !strings.HasPrefix(k, "cp-erp-prov:") || len(k) < 16 {
		t.Fatalf("key=%q", k)
	}
	auth := body["control_plane_authority"].(map[string]any)
	if body["context_id"] != cx.ID || body["tenant_id"] != tenant || auth["plan_digest"] != planDigest ||
		auth["plan_id"] != "plan_01k4zuribeans" || auth["plan_version"] != float64(2) || auth["tenant_provisioning_id"] != provisionID {
		t.Fatalf("body=%v", body)
	}
	if sub := r.led.subs[operationID]; sub.TenantProvisioningID != provisionID || sub.Authority.PlanDigest != planDigest {
		t.Fatalf("submission not linked to its provisioning and plan: %+v", sub)
	}
}

func TestReplayUsesTheSameKeyEvenWithAFreshContextAndAReplanUsesAnother(t *testing.T) {
	r := newRig(t)
	for range 2 {
		if _, err := r.w.Submit(context.Background(), provisionID); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.ctx.created) != 2 || r.ctx.created[0].ID == r.ctx.created[1].ID {
		t.Fatal("each attempt issues its own bounded context")
	}
	if r.erp.posts[0].Get("Idempotency-Key") != r.erp.posts[1].Get("Idempotency-Key") {
		t.Fatal("a replay must reuse the idempotency key; context_id is not part of request identity")
	}
	a := approved()
	a.Authority.PlanDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	other := Request{TenantID: tenant, Authority: a.Authority, LegalEntityIDs: a.LegalEntityIDs}
	same := Request{TenantID: tenant, Authority: approved().Authority, LegalEntityIDs: a.LegalEntityIDs}
	if IdempotencyKey(other) == IdempotencyKey(same) {
		t.Fatal("a replanned digest is a different request")
	}
}

func TestNothingIsSentWithoutAnApprovedPlanOrARegisteredProvisionerPrincipal(t *testing.T) {
	r := newRig(t)
	r.w.Source = source{err: ErrNotAuthorised}
	if _, err := r.w.Submit(context.Background(), provisionID); !errors.Is(err, ErrNotAuthorised) {
		t.Fatalf("err=%v", err)
	}
	r = newRig(t)
	other := approved()
	other.Authority.TenantProvisioningID = "tp_other"
	r.w.Source = source{auth: other}
	if _, err := r.w.Submit(context.Background(), provisionID); !errors.Is(err, ErrNotAuthorised) {
		t.Fatalf("an authority for another provisioning must not be sent: %v", err)
	}
	r = newRig(t)
	r.ids.err = repository.ErrIdentityNotFound
	if _, err := r.w.Submit(context.Background(), provisionID); !errors.Is(err, ErrProvisionerIdentityNotRegistered) {
		t.Fatalf("err=%v", err)
	}
	if len(r.ctx.created) != 0 || len(r.erp.posts) != 0 {
		t.Fatal("no identity is created, no context recorded and nothing sent when the provisioner has no principal")
	}
}

func TestERPRefusalsKeepOnlyBoundedMeaning(t *testing.T) {
	for _, c := range []struct {
		status  int
		code    string
		retry   string
		is      error
		retryOK bool
	}{
		{409, "PLAN_AUTHORITY_MISMATCH", "", ErrPlanAuthorityMismatch, false},
		{403, "ERP_CONTEXT_REJECTED", "", nil, false},
		{503, "ERP_SERVICE_UNAVAILABLE", "7", nil, true},
	} {
		r := newRig(t)
		r.erp.status, r.erp.problem, r.erp.retry = c.status, c.code, c.retry
		_, err := r.w.Submit(context.Background(), provisionID)
		if err == nil {
			t.Fatalf("%d: want an error", c.status)
		}
		if c.is != nil && !errors.Is(err, c.is) {
			t.Fatalf("%d: %v", c.status, err)
		}
		if Retryable(err) != c.retryOK {
			t.Fatalf("%d: retryable=%v", c.status, Retryable(err))
		}
		if strings.Contains(err.Error(), "private detail") {
			t.Fatal("ERP free text must not propagate")
		}
		if len(r.led.subs) != 0 {
			t.Fatal("a refused request is never recorded as submitted")
		}
		var p *Problem
		if errors.As(err, &p) && c.retry != "" && p.RetryAfter != 7*time.Second {
			t.Fatalf("retry-after=%v", p.RetryAfter)
		}
	}
}

func TestAnAnswerThatDisagreesWithTheRequestIsNeverRecorded(t *testing.T) {
	r := newRig(t)
	r.erp.state["tenant_id"] = "tn_other123"
	if _, err := r.w.Submit(context.Background(), provisionID); !errors.Is(err, ErrStateDisagrees) {
		t.Fatalf("err=%v", err)
	}
	r = newRig(t)
	r.erp.state["legal_entity_ids"] = []string{"OTHER-ZA"}
	if _, err := r.w.Submit(context.Background(), provisionID); !errors.Is(err, ErrStateDisagrees) {
		t.Fatalf("err=%v", err)
	}
	r = newRig(t)
	r.erp.state["state"] = "invented"
	if _, err := r.w.Submit(context.Background(), provisionID); err == nil {
		t.Fatal("an answer outside erp/v1 must be refused")
	}
	if len(r.led.subs) != 0 {
		t.Fatal("nothing recorded")
	}
}

func event(t *testing.T, m map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEventsAreAppliedOnlyWhenNewerAndOnlyForOurOperations(t *testing.T) {
	r := newRig(t)
	if _, err := r.w.Submit(context.Background(), provisionID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, applied, err := r.w.OnProvisioningChanged(ctx, event(t, newState("provisioning", 3)))
	if err != nil || !applied || st.State != "provisioning" {
		t.Fatalf("%+v %v %v", st, applied, err)
	}
	if _, applied, err = r.w.OnProvisioningChanged(ctx, event(t, newState("validating", 2))); err != nil || applied {
		t.Fatalf("a late older event must be ignored: applied=%v err=%v", applied, err)
	}
	if _, applied, err = r.w.OnProvisioningChanged(ctx, event(t, newState("provisioning", 3))); err != nil || applied {
		t.Fatalf("a duplicate must be ignored: applied=%v err=%v", applied, err)
	}
	foreign := newState("active", 9)
	foreign["operation_id"] = "0199a1b2-c3d4-7e8f-9a0b-ffffffffffff"
	if _, _, err = r.w.OnProvisioningChanged(ctx, event(t, foreign)); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("err=%v", err)
	}
	wrongTenant := newState("active", 9)
	wrongTenant["tenant_id"] = "tn_other123"
	if _, _, err = r.w.OnProvisioningChanged(ctx, event(t, wrongTenant)); !errors.Is(err, ErrStateDisagrees) {
		t.Fatalf("err=%v", err)
	}
	if _, _, err = r.w.OnProvisioningChanged(ctx, []byte(`{"operation_id":"x"}`)); err == nil {
		t.Fatal("an event outside erp/v1 must be refused")
	}
}

func TestReadIsOnlyForReconciliationOfKnownOperations(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, _, err := r.w.Reconcile(ctx, operationID); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("err=%v", err)
	}
	if r.erp.gets != 0 {
		t.Fatal("an unknown operation must not be read from ERP")
	}
	if _, err := r.w.Submit(ctx, provisionID); err != nil {
		t.Fatal(err)
	}
	r.erp.state = newState("active", 4)
	st, applied, err := r.w.Reconcile(ctx, operationID)
	if err != nil || !applied || st.State != "active" || !st.Terminal() || r.erp.gets != 1 {
		t.Fatalf("%+v %v %v gets=%d", st, applied, err, r.erp.gets)
	}
	if _, _, err := r.w.Reconcile(ctx, "not-an-operation"); err == nil || r.erp.gets != 1 {
		t.Fatal("a malformed operation id is refused before any call")
	}
}

func TestUnreachableERPIsRetryable(t *testing.T) {
	r := newRig(t)
	r.w.Client.BaseURL = "http://127.0.0.1:1/v1"
	_, err := r.w.Submit(context.Background(), provisionID)
	if err == nil || !Retryable(err) {
		t.Fatalf("err=%v retryable=%v", err, Retryable(err))
	}
}

func TestKeyIdentityIsTheEntitiesNotTheirOrderAndStatusDecidesMismatch(t *testing.T) {
	base := Request{TenantID: tenant, Authority: approved().Authority, LegalEntityIDs: []string{"A-ZA", "B-ZA"}}
	reordered := base
	reordered.LegalEntityIDs = []string{"B-ZA", "A-ZA"}
	fewer := base
	fewer.LegalEntityIDs = []string{"A-ZA"}
	if IdempotencyKey(base) != IdempotencyKey(reordered) {
		t.Fatal("order of legal entities is not request identity")
	}
	if IdempotencyKey(base) == IdempotencyKey(fewer) {
		t.Fatal("a different set of legal entities is a different request")
	}
	if (&Problem{Status: 403, Code: "PLAN_AUTHORITY_MISMATCH"}).PlanAuthorityMismatch() {
		t.Fatal("the code alone, with another status, is not ERP's plan-authority answer")
	}
	if (&Problem{Status: 409, Code: "ERP_CONTEXT_REJECTED"}).ContextRejected() {
		t.Fatal("the code alone, with another status, is not a rejected context")
	}
}

func TestACancelledCallerIsNotERPBeingDown(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.w.Client.Operation(ctx, operationID, "c")
	if !errors.Is(err, context.Canceled) || Retryable(err) {
		t.Fatalf("a cancelled caller must stay detectable and must not be rescheduled: err=%v retryable=%v", err, Retryable(err))
	}
	short, stop := context.WithTimeout(context.Background(), time.Nanosecond)
	defer stop()
	time.Sleep(time.Millisecond)
	if _, err = r.w.Client.Operation(short, operationID, "c"); !errors.Is(err, context.DeadlineExceeded) || Retryable(err) {
		t.Fatalf("err=%v retryable=%v", err, Retryable(err))
	}
}

func TestAProvisioningContextCannotOutliveFifteenMinutes(t *testing.T) {
	r := newRig(t)
	r.w.Context.TTL = 16 * time.Minute
	if _, err := r.w.Submit(context.Background(), provisionID); err == nil || len(r.erp.posts) != 0 || len(r.ctx.created) != 0 {
		t.Fatalf("a longer lifetime is a configuration error: nothing is recorded or sent (err=%v)", err)
	}
	r.w.Context.TTL = domain.MaxProvisioningContextLifetime
	if _, err := r.w.Submit(context.Background(), provisionID); err != nil {
		t.Fatalf("exactly 15 minutes is allowed: %v", err)
	}
}
