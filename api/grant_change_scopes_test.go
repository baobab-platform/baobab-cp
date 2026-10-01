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
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestGrantChangeScopes: drafting a change to administrative authority
// needs administrator:write as well as changeset:write, and approving one
// needs administrator:approve as well as changeset:approve (ADR-BCP-020
// gate ADA-06). The approver is never the requester, and a grantee never
// approves their own grant. A plain changeset kind needs neither.
func TestGrantChangeScopes(t *testing.T) {
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
	mustNoError(t, store.ApplyMigrations(ctx))
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

	identities := repository.NewInMemoryRepository()
	ids := map[string]string{}
	for _, subject := range []string{"maker", "checker", "grantee"} {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
		ids[subject] = p.ID
		// The Postgres repository judges the grantee against its own identity table.
		mustNoError(t, repo.CreateIdentity(ctx, p))
	}
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM changeset.outcome WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE requested_by = $1)`, ids["maker"])
		admin.Exec(ctx, `DELETE FROM changeset.approval WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE requested_by = $1)`, ids["maker"])
		admin.Exec(ctx, `UPDATE changeset.changeset SET current_plan_id = NULL WHERE requested_by = $1`, ids["maker"])
		admin.Exec(ctx, `DELETE FROM changeset.plan WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE requested_by = $1)`, ids["maker"])
		admin.Exec(ctx, `DELETE FROM changeset.changeset WHERE requested_by = $1`, ids["maker"])
	})
	token := func(subject string, scopes ...string) auth.Principal {
		granted := map[string]struct{}{}
		for _, s := range scopes {
			granted[s] = struct{}{}
		}
		return auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject + strings.Join(scopes, ""),
			Scopes: granted, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	handler := New(Dependencies{Store: &fakeStore{}, Identities: identities, Changesets: repo, AdminVerifier: tokenVerifier{
		"maker":        token("maker", "changeset:read", "changeset:write", "administrator:write"),
		"maker-plain":  token("maker", "changeset:read", "changeset:write"),
		"checker":      token("checker", "changeset:read", "changeset:approve", "administrator:approve"),
		"checker-bare": token("checker", "changeset:read", "changeset:approve"),
		"grantee":      token("grantee", "changeset:read", "changeset:approve", "administrator:approve"),
	}})
	n := 0
	call := func(method, path, who string, headers map[string]string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+who)
		if method == http.MethodPost {
			n++
			r.Header.Set("Idempotency-Key", "grant-scope-test-"+strings.Repeat("0", 6)+string(rune('a'+n)))
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	request := map[string]any{"title": "Grant tenant.suspend", "reason": "On-call cover.", "desired_change": map[string]any{
		"kind": "ADMINISTRATIVE_GRANT_ISSUANCE", "principal_id": ids["grantee"], "permission": "tenant.suspend",
		"scope": map[string]any{"level": "TENANT", "tenant_id": "tn_scopetest"}, "grant_type": "TIME_BOUND", "valid_until": "2099-01-01T00:00:00Z"}}

	if w := call(http.MethodPost, "/v1/admin/changesets", "maker-plain", nil, request); w.Code != http.StatusForbidden {
		t.Fatalf("drafting a grant change without administrator:write: %d %s", w.Code, w.Body.String())
	}
	created := call(http.MethodPost, "/v1/admin/changesets", "maker", nil, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("drafting a grant change: %d %s", created.Code, created.Body.String())
	}
	var c struct {
		ChangesetID string `json:"changeset_id"`
		State       string `json:"state"`
	}
	mustNoError(t, json.Unmarshal(created.Body.Bytes(), &c))
	if os.Getenv("SHARED_CONTRACTS_DIR") != "" {
		var raw map[string]any
		mustNoError(t, json.Unmarshal(created.Body.Bytes(), &raw))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, os.Getenv("SHARED_CONTRACTS_DIR"), "control-plane/v1/changeset.schema.json#/$defs/Changeset"), raw)
	}
	path := "/v1/admin/changesets/" + c.ChangesetID
	submitted := call(http.MethodPost, path+"/submit", "maker", map[string]string{"If-Match": created.Header().Get("ETag")}, nil)
	if submitted.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", submitted.Code, submitted.Body.String())
	}
	etag := submitted.Header().Get("ETag")
	planned := call(http.MethodGet, path+"/plan", "maker", nil, nil)
	var plan struct {
		PlanID      string `json:"plan_id"`
		PlanVersion int    `json:"plan_version"`
		PlanDigest  string `json:"plan_digest"`
	}
	mustNoError(t, json.Unmarshal(planned.Body.Bytes(), &plan))
	decision := map[string]any{"plan_id": plan.PlanID, "plan_version": plan.PlanVersion, "plan_digest": plan.PlanDigest, "decision": "APPROVED"}

	if w := call(http.MethodPost, path+"/approve", "checker-bare", map[string]string{"If-Match": etag}, decision); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "administrator:approve") {
		t.Fatalf("approving without administrator:approve: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, path+"/approve", "grantee", map[string]string{"If-Match": etag}, decision); w.Code != http.StatusForbidden || problemCode(t, w) != "SELF_APPROVAL_PROHIBITED" {
		t.Fatalf("the grantee approving their own grant: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, path+"/approve", "checker", map[string]string{"If-Match": etag}, decision); w.Code != http.StatusOK {
		t.Fatalf("an independent approver: %d %s", w.Code, w.Body.String())
	}
}
