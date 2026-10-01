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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestProviderMigrationExecutionRoutes drives ADR-SHARED-016 over HTTP: a
// STATELESS_REBIND migration is approved by someone other than its
// creator and only with provider-migration:approve, then advanced one
// transition at a time to COMPLETE under provider-migration:execute, each
// advance a 202 PROVIDER_MIGRATION_ADVANCE operation. Every body conforms
// to the Shared contract.
func TestProviderMigrationExecutionRoutes(t *testing.T) {
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

	sourceEngine, targetEngine := domain.NewUUIDv7(), domain.NewUUIDv7()
	sourceInstance, targetInstance := domain.NewUUIDv7(), domain.NewUUIDv7()
	sourceProvider, targetProvider := domain.NewUUIDv7(), domain.NewUUIDv7()
	capability, scope, binding := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	tail := sourceEngine[len(sourceEngine)-8:]
	sourceKey, targetKey := "baobab-hsrc"+tail+".legacy", "baobab-htgt"+tail+".modern"
	capabilityKey := "finance.http" + tail + ".issue"
	var migrationID string
	t.Cleanup(func() {
		if migrationID != "" {
			admin.Exec(ctx, `DELETE FROM audit_events WHERE target = $1`, migrationID)
			admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE subject_id = $1`, migrationID)
			admin.Exec(ctx, `UPDATE topology.provider_migration SET approval_id = NULL WHERE provider_migration_id = $1`, migrationID)
			admin.Exec(ctx, `DELETE FROM topology.provider_migration_approval WHERE provider_migration_id = $1`, migrationID)
			admin.Exec(ctx, `DELETE FROM topology.provider_migration_binding WHERE provider_migration_id = $1`, migrationID)
			admin.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`)
			admin.Exec(ctx, `DELETE FROM topology.provider_migration_plan WHERE provider_migration_id = $1`, migrationID)
			admin.Exec(ctx, `DELETE FROM topology.provider_migration WHERE provider_migration_id = $1`, migrationID)
		}
		admin.Exec(ctx, `DELETE FROM topology.health_observation WHERE engine_instance_id = $1::uuid`, targetInstance)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE capability_id = $1::uuid`, capability)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE scope_id = $1::uuid`, scope)
		admin.Exec(ctx, `DELETE FROM capability.provider_capability_support WHERE provider_id = ANY($1::uuid[])`, []string{sourceProvider, targetProvider})
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_id = ANY($1::uuid[])`, []string{sourceProvider, targetProvider})
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, capability)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = ANY($1::uuid[])`, []string{sourceInstance, targetInstance})
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = ANY($1::uuid[])`, []string{sourceEngine, targetEngine})
	})
	mustNoError(t, repo.CreateCapability(ctx, capabilitydomain.Capability{ID: capability, Key: capabilityKey, Name: "HTTP execution test",
		DomainKey: "finance", Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}))
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO topology.engine (engine_id, code, name) VALUES ($1::uuid, $2, $2), ($3::uuid, $4, $4)`,
			[]any{sourceEngine, "baobab-hsrc" + tail, targetEngine, "baobab-htgt" + tail}},
		{`INSERT INTO topology.engine_instance (engine_instance_id, engine_id, region, environment, status)
			VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE'), ($3::uuid, $4::uuid, 'af-south-1', 'production', 'ACTIVE')`,
			[]any{sourceInstance, sourceEngine, targetInstance, targetEngine}},
		{`INSERT INTO capability.capability_provider (provider_id, provider_key, name, provider_type, engine_id, status)
			VALUES ($1::uuid, $2, 'Source', 'BAOBAB_ENGINE', $3::uuid, 'ACTIVE'), ($4::uuid, $5, 'Target', 'BAOBAB_ENGINE', $6::uuid, 'ACTIVE')`,
			[]any{sourceProvider, sourceKey, sourceEngine, targetProvider, targetKey, targetEngine}},
		{`INSERT INTO capability.provider_capability_support (provider_id, capability_id, contract_versions)
			VALUES ($1::uuid, $3::uuid, '{1}'), ($2::uuid, $3::uuid, '{1}')`, []any{sourceProvider, targetProvider, capability}},
		{`INSERT INTO capability.capability_scope (scope_id, tenant_id, market_id, deployment_region, environment)
			VALUES ($1::uuid, $2, 'KE', 'af-south-1', 'production')`, []any{scope, "tn_http" + tail}},
		{`INSERT INTO capability.capability_binding (id, capability_id, engine_instance_id, scope_id, binding_mode, status,
			contract_version, effective_from, provider_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '1 day', $5::uuid)`,
			[]any{binding, capability, sourceInstance, scope, sourceProvider}},
	} {
		mustNoError(t, execErr(admin.Exec(ctx, stmt.sql, stmt.args...)))
	}
	now := time.Now().UTC()
	mustNoError(t, repo.RecordHealthObservation(ctx, health.Observation{Subject: health.Subject{EngineInstanceID: targetInstance},
		Status: health.StatusHealthy, ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Hour), Source: health.SourceActiveProbe}))

	identities := repository.NewInMemoryRepository()
	for _, subject := range []string{"creator", "approver", "operator"} {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
	}
	human := func(subject string, scopes ...string) auth.Principal {
		granted := map[string]struct{}{}
		for _, s := range scopes {
			granted[s] = struct{}{}
		}
		return auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject + strings.Join(scopes, ""),
			Scopes: granted, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	handler := New(Dependencies{Store: &fakeStore{}, Identities: identities, ProviderMigrations: repo,
		AdminVerifier: tokenVerifier{
			"creator":          human("creator", "topology:read", "topology:write", "provider-migration:approve"),
			"approver":         human("approver", "topology:read", "provider-migration:approve"),
			"approver-noscope": human("approver", "topology:read"),
			"operator":         human("operator", "topology:read", "provider-migration:execute"),
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
	dir := contracttest.SharedDir(t)
	conforms := func(file, definition string, w *httptest.ResponseRecorder) {
		t.Helper()
		var body any
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "control-plane/v1/"+file+"#/$defs/"+definition), body)
	}
	expect := func(w *httptest.ResponseRecorder, status int, code, label string) {
		t.Helper()
		if w.Code != status || (code != "" && !strings.Contains(w.Body.String(), code)) {
			t.Fatalf("%s: want %d %s, got %d %s", label, status, code, w.Code, w.Body.String())
		}
	}

	created := call(http.MethodPost, "/v1/provider-migrations", "creator", map[string]string{"Idempotency-Key": "pm-http-create-" + tail},
		map[string]any{"source_provider_key": sourceKey, "target_provider_key": targetKey,
			"capabilities":   []map[string]any{{"capability_key": capabilityKey, "contract_version": 1}},
			"migration_mode": "STATELESS_REBIND", "data_strategy": "NONE", "rollback_strategy": "REBIND_SOURCE",
			"cohorts": []map[string]any{{"cohort_key": "all"}}, "owners": []string{"prn_0199a1b2c3d47e8f9a0b1c2d3e4f5a01"},
			"reason": "Replace the provider."})
	expect(created, http.StatusCreated, "", "create")
	var m struct {
		ID       string `json:"provider_migration_id"`
		PlanID   string `json:"plan_id"`
		Version  int    `json:"plan_version"`
		Digest   string `json:"plan_digest"`
		Blocked  bool   `json:"blocked"`
		Stage    string `json:"stage"`
		Approval string `json:"approval_id"`
	}
	mustNoError(t, json.Unmarshal(created.Body.Bytes(), &m))
	migrationID = m.ID
	if m.Blocked {
		t.Fatalf("a ready target produced a blocked migration: %s", created.Body.String())
	}
	path := "/v1/provider-migrations/" + m.ID
	decision := map[string]any{"plan_id": m.PlanID, "plan_version": m.Version, "plan_digest": m.Digest, "decision": "APPROVED"}
	ifMatch := func() map[string]string {
		w := call(http.MethodGet, path, "operator", nil, nil)
		expect(w, http.StatusOK, "", "read")
		conforms("provider-migration.schema.json", "ProviderMigration", w)
		return map[string]string{"If-Match": w.Header().Get("ETag")}
	}

	// Approval.
	expect(call(http.MethodPost, path+"/approve", "creator", ifMatch(), decision), http.StatusForbidden, "PROVIDER_MIGRATION_SELF_APPROVAL", "the creator approving")
	expect(call(http.MethodPost, path+"/approve", "approver-noscope", ifMatch(), decision), http.StatusForbidden, "AUTHORIZATION_DENIED", "approval without its scope")
	expect(call(http.MethodPost, path+"/approve", "approver", nil, decision), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "approval without If-Match")
	approved := call(http.MethodPost, path+"/approve", "approver", ifMatch(), decision)
	expect(approved, http.StatusOK, `"PROVIDER_MIGRATION"`, "approve")
	conforms("approval-decision.schema.json", "ApprovalDecision", approved)

	// Advancing.
	advance := func(transition string) *httptest.ResponseRecorder {
		t.Helper()
		headers := ifMatch()
		headers["Idempotency-Key"] = "pm-http-" + transition + "-" + domain.NewUUIDv7()
		return call(http.MethodPost, path+"/advance", "operator", headers, map[string]any{"transition": transition})
	}
	expect(call(http.MethodPost, path+"/advance", "approver", map[string]string{"If-Match": `"2"`, "Idempotency-Key": "pm-http-noscope-" + tail},
		map[string]any{"transition": "prepare"}), http.StatusForbidden, "AUTHORIZATION_DENIED", "advancing without its scope")
	expect(call(http.MethodPost, path+"/advance", "operator", ifMatch(), map[string]any{"transition": "prepare"}), http.StatusBadRequest, "", "advancing without a key")
	expect(call(http.MethodPost, path+"/advance", "operator", map[string]string{"If-Match": `"2"`, "Idempotency-Key": "pm-http-steps-" + tail},
		map[string]any{"transition": "prepare", "steps": []string{"verify-target"}}), http.StatusBadRequest, "VALIDATION_FAILED", "an advance naming steps")
	expect(advance("shift"), http.StatusConflict, "PROVIDER_MIGRATION_STAGE_CONFLICT", "shift from PLAN")
	for _, transition := range []string{"prepare", "canary", "validate", "retire_old", "complete"} {
		w := advance(transition)
		expect(w, http.StatusAccepted, `"SUCCEEDED"`, transition)
		conforms("execution-operation.schema.json", "ExecutionOperation", w)
		if !strings.HasPrefix(w.Header().Get("Location"), "/v1/admin/operations/op_") {
			t.Fatalf("%s: Location %q", transition, w.Header().Get("Location"))
		}
	}
	final := call(http.MethodGet, path, "operator", nil, nil)
	conforms("provider-migration.schema.json", "ProviderMigration", final)
	if !strings.Contains(final.Body.String(), `"stage":"COMPLETE"`) || !strings.Contains(final.Body.String(), `"shifted_cohort_keys":["all"]`) {
		t.Fatalf("final migration: %s", final.Body.String())
	}
	expect(advance("roll_back"), http.StatusBadRequest, "VALIDATION_FAILED", "a roll_back without a reason")
}
