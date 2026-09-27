package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestOperationsArePolledAsTheirOwnResource: an operation is read by id with
// its revision as ETag, polling with that ETag is 304, a failed operation
// carries its problem, and only platform administrators read operations until
// tenant-scoped authority exists (ADR-BCP-022 sections 58-67).
func TestOperationsArePolledAsTheirOwnResource(t *testing.T) {
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

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[12:]
	running, failed := "op_"+suffix+"a", "op_"+suffix+"b"
	digest := "sha256:" + strings.Repeat("ab", 32)
	if _, err := admin.Exec(ctx, `INSERT INTO operations.execution_operation (operation_id, operation_type, status, subject_type, subject_id,
		tenant_id, plan_id, plan_digest, requested_by, current_phase, completed_steps, total_steps, current_step, revision)
		VALUES ($1, 'TENANT_PROVISIONING_APPLY', 'RUNNING', 'TENANT_PROVISIONING', $2, $3, $4, $5, 'prn_requester', 'REGISTERING', 1, 3, 'bind', 2)`,
		running, "tp_"+suffix, "tn_"+suffix[:20], "plan_"+suffix, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO operations.execution_operation (operation_id, operation_type, status, subject_type, subject_id,
		requested_by, retryable, problem) VALUES ($1, 'TENANT_PROVISIONING_APPLY', 'FAILED', 'TENANT_PROVISIONING', $2, 'prn_requester', true, $3)`,
		failed, "tp_"+suffix, `{"type":"https://docs.nabhold.com/problems/provider_unavailable","title":"Bad Gateway","status":502,"code":"PROVIDER_UNAVAILABLE","correlation_id":"c","retryable":true}`); err != nil {
		t.Fatal(err)
	}
	succeeded := "op_" + suffix + "s"
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE operation_id = ANY($1)`, []string{running, failed, succeeded})
	})
	// Success names the resource's state, which is not the operation's.
	if _, err := admin.Exec(ctx, `INSERT INTO operations.execution_operation (operation_id, operation_type, status, subject_type, subject_id,
		requested_by, result, completed_at) VALUES ($1, 'TENANT_PROVISIONING_APPLY', 'SUCCEEDED', 'TENANT_PROVISIONING', 'tp_x', 'prn_requester',
		'{"resource_type":"TENANT_PROVISIONING","resource_id":"tp_x"}', now())`, succeeded); err == nil {
		t.Fatal("a SUCCEEDED operation without the resource's state was stored")
	}
	if _, err := admin.Exec(ctx, `INSERT INTO operations.execution_operation (operation_id, operation_type, status, subject_type, subject_id,
		requested_by, result, completed_at) VALUES ($1, 'TENANT_PROVISIONING_APPLY', 'SUCCEEDED', 'TENANT_PROVISIONING', $2, 'prn_requester',
		$3, now())`, succeeded, "tp_"+suffix, `{"resource_type":"TENANT_PROVISIONING","resource_id":"tp_`+suffix+`","resource_state":"READY"}`); err != nil {
		t.Fatal(err)
	}
	// Without a status problem, a FAILED operation is refused by the store.
	if _, err := admin.Exec(ctx, `INSERT INTO operations.execution_operation (operation_id, operation_type, status, subject_type, subject_id,
		requested_by) VALUES ($1, 'TENANT_PROVISIONING_APPLY', 'FAILED', 'TENANT_PROVISIONING', 'tp_x', 'prn_requester')`, "op_"+suffix+"c"); err == nil {
		t.Fatal("a FAILED operation without its problem was stored")
	}

	principal := func(role string) auth.Principal {
		return auth.Principal{Subject: "admin-" + suffix, ActorType: "human", TokenID: "t-" + role,
			Scopes: map[string]struct{}{"operation:read": {}}, Roles: map[string]struct{}{role: {}}}
	}
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: tokenVerifier{
		"platform": principal(RolePlatformAdmin), "tenant": principal(RoleTenantAdmin),
	}, Operations: repo})
	get := func(token, id, ifNoneMatch string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/v1/admin/operations/"+id, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		if ifNoneMatch != "" {
			request.Header.Set("If-None-Match", ifNoneMatch)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	validate := func(response *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		if response.Code != http.StatusOK {
			t.Fatalf("got %d: %s", response.Code, response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if dir := os.Getenv("SHARED_CONTRACTS_DIR"); dir != "" {
			contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "control-plane/v1/execution-operation.schema.json#/$defs/ExecutionOperation"), body)
		}
		return body
	}

	got := get("platform", running, "")
	body := validate(got)
	if body["status"] != "RUNNING" || got.Header().Get("ETag") != `"2"` || body["progress"].(map[string]any)["total_steps"] != float64(3) {
		t.Fatalf("running operation: %v (ETag %s)", body, got.Header().Get("ETag"))
	}
	if unchanged := get("platform", running, `"2"`); unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("polling an unchanged operation: %d %s", unchanged.Code, unchanged.Body.String())
	}
	for _, header := range []string{`"1", W/"2"`, `*`} {
		if unchanged := get("platform", running, header); unchanged.Code != http.StatusNotModified {
			t.Fatalf("If-None-Match %s: %d", header, unchanged.Code)
		}
	}
	if changed := get("platform", running, `"1"`); changed.Code != http.StatusOK {
		t.Fatalf("polling with a stale ETag: %d", changed.Code)
	}
	if body := validate(get("platform", failed, "")); body["problem"].(map[string]any)["code"] != "PROVIDER_UNAVAILABLE" || body["retryable"] != true {
		t.Fatalf("failed operation: %v", body)
	}
	if body := validate(get("platform", succeeded, "")); body["result"].(map[string]any)["resource_state"] != "READY" {
		t.Fatalf("succeeded operation: %v", body)
	}
	if missing := get("platform", "op_absent1", ""); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown operation: %d", missing.Code)
	}
	if tenantAdmin := get("tenant", running, ""); tenantAdmin.Code != http.StatusForbidden {
		t.Fatalf("a tenant administrator read an operation: %d", tenantAdmin.Code)
	}
}
