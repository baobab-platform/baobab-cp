package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestChangesetRoutes drives a tenant suspension through the HTTP routes
// (ADR-BCP-021, ADR-BCP-022 section 44): draft, submit, plan, a refused
// self-approval and a refused scope, approval, apply as a 202 operation that
// is then polled, and the outcome. Every body conforms to the Shared
// contract.
func TestChangesetRoutes(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	repo, err := repository.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	tenant, legalEntity := "tn_api"+suffix, "CS-API-"+strings.ToUpper(suffix)
	cleanup := func() {
		for _, stmt := range []string{
			`DELETE FROM changeset.outcome WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_tenant_id = $1)`,
			`DELETE FROM changeset.approval WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_tenant_id = $1)`,
			`UPDATE changeset.changeset SET current_plan_id = NULL WHERE target_tenant_id = $1`,
			`DELETE FROM operations.execution_operation WHERE tenant_id = $1`,
			`DELETE FROM changeset.plan WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_tenant_id = $1)`,
			`DELETE FROM changeset.changeset WHERE target_tenant_id = $1`,
			`DELETE FROM tenants WHERE tenant_id = $1`,
		} {
			admin.Exec(ctx, stmt, tenant)
		}
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntity)
	}
	cleanup()
	t.Cleanup(cleanup)
	mustNoError(t, execErr(admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntity)))
	mustNoError(t, execErr(admin.Exec(ctx, `INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference, tenant_id,
		legal_entity_id, display_name, isolation_strategy, residency_region, desired_state, observed_state)
		VALUES ('BOOTSTRAP', 'Test fixture registered outside admission', 'test-fixture', $1, $2, 'Changeset API test', 'row_level_security',
		'af-south-1', 'active', 'active')`, tenant, legalEntity)))

	identities := repository.NewInMemoryRepository()
	for _, subject := range []string{"requester", "approver"} {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
	}
	token := func(subject string, scopes ...string) auth.Principal {
		granted := map[string]struct{}{}
		for _, s := range scopes {
			granted[s] = struct{}{}
		}
		return auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject + strings.Join(scopes, ""),
			Scopes: granted, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	handler := New(Dependencies{Store: &fakeStore{}, Identities: identities, Changesets: repo, Operations: repo, AdminVerifier: tokenVerifier{
		"requester":         token("requester", "changeset:read", "changeset:write", "changeset:approve", "operation:read"),
		"approver":          token("approver", "changeset:read", "changeset:approve"),
		"approver-no-scope": token("approver", "changeset:read", "changeset:write"),
	}})
	call := func(method, path, who string, headers map[string]string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+who)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	// contracttest compiles every Shared schema, including those whose
	// patterns RE2 cannot express (ExecutionOperation's trace_id).
	dir := contracttest.SharedDir(t)
	conforms := func(file, definition string, w *httptest.ResponseRecorder) {
		t.Helper()
		var body any
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "control-plane/v1/"+file+"#/$defs/"+definition), body)
	}
	decode := func(w *httptest.ResponseRecorder, into any) { mustNoError(t, json.Unmarshal(w.Body.Bytes(), into)) }

	body := map[string]any{"title": "Suspend for non-payment", "reason": "Past due.",
		"desired_change": map[string]any{"kind": "TENANT_SUSPENSION", "tenant_id": tenant}}
	if w := call(http.MethodPost, "/v1/admin/changesets", "requester", map[string]string{"Idempotency-Key": "changeset-key-" + suffix},
		map[string]any{"title": "x", "reason": "y", "changeset_type": "SUSPEND", "desired_change": body["desired_change"]}); w.Code != http.StatusBadRequest {
		t.Fatalf("a caller-supplied type: %d", w.Code)
	}
	created := call(http.MethodPost, "/v1/admin/changesets", "requester", map[string]string{"Idempotency-Key": "changeset-key-" + suffix}, body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	conforms("changeset.schema.json", "Changeset", created)
	var c changeset.Changeset
	decode(created, &c)
	path := "/v1/admin/changesets/" + c.ChangesetID
	if replay := call(http.MethodPost, "/v1/admin/changesets", "requester", map[string]string{"Idempotency-Key": "changeset-key-" + suffix}, body); replay.Code != http.StatusCreated || !strings.Contains(replay.Body.String(), c.ChangesetID) {
		t.Fatalf("replay: %d", replay.Code)
	}

	if w := call(http.MethodPost, path+"/submit", "requester", nil, nil); w.Code != http.StatusPreconditionRequired {
		t.Fatalf("submit without If-Match: %d", w.Code)
	}
	submitted := call(http.MethodPost, path+"/submit", "requester", map[string]string{"If-Match": created.Header().Get("ETag")}, nil)
	if submitted.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", submitted.Code, submitted.Body.String())
	}
	conforms("changeset.schema.json", "Changeset", submitted)
	decode(submitted, &c)
	plan := call(http.MethodGet, path+"/plan", "approver", nil, nil)
	if plan.Code != http.StatusOK || plan.Header().Get("ETag") != `"`+c.CurrentPlan.PlanDigest+`"` {
		t.Fatalf("plan: %d %q", plan.Code, plan.Header().Get("ETag"))
	}
	conforms("changeset.schema.json", "ChangesetPlan", plan)

	decision := map[string]any{"plan_id": c.CurrentPlan.PlanID, "plan_version": c.CurrentPlan.PlanVersion,
		"plan_digest": c.CurrentPlan.PlanDigest, "decision": "APPROVED"}
	ifMatch := map[string]string{"If-Match": submitted.Header().Get("ETag")}
	if w := call(http.MethodPost, path+"/approve", "requester", ifMatch, decision); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "CHANGESET_SELF_APPROVAL") {
		t.Fatalf("self-approval: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, path+"/approve", "approver-no-scope", ifMatch, decision); w.Code != http.StatusForbidden {
		t.Fatalf("approval without changeset:approve: %d", w.Code)
	}
	approved := call(http.MethodPost, path+"/approve", "approver", ifMatch, decision)
	if approved.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	conforms("approval-decision.schema.json", "ApprovalDecision", approved)

	current := call(http.MethodGet, path, "requester", nil, nil)
	applied := call(http.MethodPost, path+"/apply", "requester", map[string]string{"If-Match": current.Header().Get("ETag"),
		"Idempotency-Key": "changeset-apply-" + suffix}, nil)
	if applied.Code != http.StatusAccepted || !strings.HasPrefix(applied.Header().Get("Location"), "/v1/admin/operations/op_") {
		t.Fatalf("apply: %d %s", applied.Code, applied.Body.String())
	}
	conforms("execution-operation.schema.json", "ExecutionOperation", applied)
	if polled := call(http.MethodGet, applied.Header().Get("Location"), "requester", nil, nil); polled.Code != http.StatusOK || !strings.Contains(polled.Body.String(), `"SUCCEEDED"`) {
		t.Fatalf("poll: %d %s", polled.Code, polled.Body.String())
	}
	outcome := call(http.MethodGet, path+"/outcome", "approver", nil, nil)
	if outcome.Code != http.StatusOK || !strings.Contains(outcome.Body.String(), `"COMPLETED"`) {
		t.Fatalf("outcome: %d %s", outcome.Code, outcome.Body.String())
	}
	conforms("changeset.schema.json", "ChangeOutcome", outcome)
	var status string
	mustNoError(t, admin.QueryRow(ctx, `SELECT desired_state FROM tenants WHERE tenant_id = $1`, tenant).Scan(&status))
	if status != "suspended" {
		t.Fatalf("tenant after apply: %s", status)
	}

	final := call(http.MethodGet, path, "requester", nil, nil)
	for name, tc := range map[string]struct {
		method, path string
		headers      map[string]string
		body         any
		want         int
	}{
		"cancel a completed changeset": {http.MethodPost, path + "/cancel", map[string]string{"If-Match": final.Header().Get("ETag")}, map[string]any{"reason": "late"}, http.StatusConflict},
		"a stale If-Match":             {http.MethodPost, path + "/cancel", map[string]string{"If-Match": `"1"`}, map[string]any{"reason": "late"}, http.StatusPreconditionFailed},
		"a malformed id":               {http.MethodGet, "/v1/admin/changesets/not-an-id", nil, nil, http.StatusNotFound},
		"an unknown changeset":         {http.MethodGet, "/v1/admin/changesets/cs_missing0", nil, nil, http.StatusNotFound},
		"a bad state filter":           {http.MethodGet, "/v1/admin/changesets?state=done!", nil, nil, http.StatusBadRequest},
	} {
		if w := call(tc.method, tc.path, "requester", tc.headers, tc.body); w.Code != tc.want {
			t.Errorf("%s: %d, want %d: %s", name, w.Code, tc.want, w.Body.String())
		}
	}
	list := call(http.MethodGet, "/v1/admin/changesets?tenant_id="+tenant, "approver", nil, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), c.ChangesetID) {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	conforms("changeset.schema.json", "ChangesetPage", list)
}

func execErr(_ any, err error) error { return err }
