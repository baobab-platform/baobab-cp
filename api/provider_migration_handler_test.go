package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

// migrationStore is an in-memory ProviderMigrationRepository over a ready
// target.
type migrationStore struct {
	mu        sync.Mutex
	byID      map[string]migration.Migration
	plans     map[string]migration.Plan
	hashes    map[string]string
	keys      map[string]string
	instances []migration.Instance
}

func newMigrationStore() *migrationStore {
	now := time.Now().UTC()
	return &migrationStore{byID: map[string]migration.Migration{}, plans: map[string]migration.Plan{}, hashes: map[string]string{},
		keys: map[string]string{}, instances: []migration.Instance{{EngineInstanceID: "ei_target1", Region: "af-south-1",
			Environment: "production", Status: "ACTIVE", Health: map[string]health.Levels{"finance.invoice.read": {
				EngineInstance: &health.Observation{Subject: health.Subject{EngineInstanceID: "ei_target1"}, Status: health.StatusHealthy,
					ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Source: health.SourceActiveProbe}}}}}}
}

func (s *migrationStore) SourceBindings(_ context.Context, provider string, _ []string) ([]migration.SourceBinding, error) {
	if provider != "baobab-erp.idempiere" {
		return nil, nil
	}
	return []migration.SourceBinding{{BindingID: "b1", CapabilityKey: "finance.invoice.read", ContractVersion: 1, TenantID: "tn_alpha",
		Markets: []string{"KE"}, Region: "af-south-1", Environment: "production"}}, nil
}

func (s *migrationStore) Provider(_ context.Context, key string) (migration.Provider, bool, error) {
	if key != "baobab-erp.nextledger" {
		return migration.Provider{}, false, nil
	}
	return migration.Provider{Status: "ACTIVE", Support: map[string][]int{"finance.invoice.read": {1}}}, true, nil
}

func (s *migrationStore) ProviderInstances(context.Context, string, []string) ([]migration.Instance, error) {
	return s.instances, nil
}

func (s *migrationStore) OpenMigrations(context.Context, string, []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for id, m := range s.byID {
		if !migration.Terminal(m.Stage) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *migrationStore) CreateProviderMigration(ctx context.Context, _ string, key, hash string,
	build func(context.Context) (migration.Migration, migration.Plan, error), _ repository.AuditActor) (migration.Migration, error) {
	m, plan, err := build(ctx)
	if err != nil {
		return m, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, used := s.keys[key]; used {
		return migration.Migration{}, repository.ErrProviderMigrationIdempotencyConflict
	}
	s.byID[m.ProviderMigrationID], s.plans[m.ProviderMigrationID] = m, plan
	s.keys[key], s.hashes[m.ProviderMigrationID] = m.ProviderMigrationID, hash
	return m, nil
}

func (s *migrationStore) GetProviderMigration(_ context.Context, id string) (migration.Migration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return m, repository.ErrProviderMigrationNotFound
	}
	return m, nil
}

func (s *migrationStore) GetProviderMigrationByIdempotencyKey(_ context.Context, key string) (migration.Migration, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.keys[key]
	if !ok {
		return migration.Migration{}, "", repository.ErrProviderMigrationNotFound
	}
	return s.byID[id], s.hashes[id], nil
}

func (s *migrationStore) CurrentProviderMigrationPlan(_ context.Context, id string) (migration.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	if !ok {
		return p, repository.ErrProviderMigrationNotFound
	}
	return p, nil
}

func migrationRequestBody(target string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"source_provider_key": "baobab-erp.idempiere", "target_provider_key": target,
		"capabilities":   []map[string]any{{"capability_key": "finance.invoice.read", "contract_version": 1}},
		"migration_mode": "STATELESS_REBIND", "data_strategy": "NONE", "rollback_strategy": "REBIND_SOURCE",
		"cohorts": []map[string]any{{"cohort_key": "canary", "selector": map[string]any{"tenant_ids": []string{"tn_alpha"}}}, {"cohort_key": "rest"}},
		"owners":  []string{"prn_owner1"}, "reason": "Replace the ledger provider.",
	})
	return raw
}

// TestProviderMigrationRoutes: the preview writes nothing; a create is
// recorded in PLAN with its plan and replays idempotently; and every
// response conforms to the Shared contract.
func TestProviderMigrationRoutes(t *testing.T) {
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()
	p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, p))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
		PrincipalID: p.ID, Issuer: testRealm, Subject: "ops", Status: "ACTIVE"}))
	admin := func(subject string, scopes ...string) auth.Principal {
		granted := map[string]struct{}{}
		for _, s := range scopes {
			granted[s] = struct{}{}
		}
		return auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject,
			Scopes: granted, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	store := newMigrationStore()
	handler := New(Dependencies{Store: &fakeStore{}, Identities: identities, ProviderMigrations: store, AdminVerifier: tokenVerifier{
		"writer": admin("ops", "topology:read", "topology:write"), "reader": admin("ops", "topology:read"),
	}})
	call := func(method, path, token, key string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	conforms := func(definition string, w *httptest.ResponseRecorder) {
		t.Helper()
		schema := contracts.MustSchema("control-plane/v1/provider-migration.schema.json#/$defs/" + definition)
		if err := contracts.Validate(schema, w.Body.Bytes()); err != nil {
			t.Fatalf("%s response does not conform: %v\n%s", definition, err, w.Body.String())
		}
	}

	preview := call(http.MethodPost, "/v1/provider-migrations/plan", "reader", "", migrationRequestBody("baobab-erp.nextledger"))
	if preview.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	conforms("ProviderMigrationPlan", preview)
	var previewed migration.Plan
	mustNoError(t, json.Unmarshal(preview.Body.Bytes(), &previewed))
	if previewed.ProviderMigrationID != "" || len(previewed.Blockers) != 0 || len(store.byID) != 0 {
		t.Fatalf("the preview wrote or was blocked: %+v, %d stored", previewed.Blockers, len(store.byID))
	}

	// A reader may preview but not create.
	if w := call(http.MethodPost, "/v1/provider-migrations", "reader", "migration-key-000001", migrationRequestBody("baobab-erp.nextledger")); w.Code != http.StatusForbidden {
		t.Fatalf("create without topology:write: %d", w.Code)
	}
	created := call(http.MethodPost, "/v1/provider-migrations", "writer", "migration-key-000001", migrationRequestBody("baobab-erp.nextledger"))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	conforms("ProviderMigration", created)
	var m migration.Migration
	mustNoError(t, json.Unmarshal(created.Body.Bytes(), &m))
	if m.Stage != migration.StagePlan || m.Blocked || m.CreatedBy != p.ID || created.Header().Get("Location") != "/v1/provider-migrations/"+m.ProviderMigrationID {
		t.Fatalf("created: %+v, Location %q", m, created.Header().Get("Location"))
	}

	replay := call(http.MethodPost, "/v1/provider-migrations", "writer", "migration-key-000001", migrationRequestBody("baobab-erp.nextledger"))
	var replayed migration.Migration
	mustNoError(t, json.Unmarshal(replay.Body.Bytes(), &replayed))
	if replay.Code != http.StatusCreated || replayed.ProviderMigrationID != m.ProviderMigrationID || len(store.byID) != 1 {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	if w := call(http.MethodPost, "/v1/provider-migrations", "writer", "migration-key-000001", migrationRequestBody("baobab-erp.otherledger")); w.Code != http.StatusConflict {
		t.Fatalf("a reused key with another body: %d", w.Code)
	}

	got := call(http.MethodGet, "/v1/provider-migrations/"+m.ProviderMigrationID, "reader", "", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("get: %d", got.Code)
	}
	conforms("ProviderMigration", got)
	plan := call(http.MethodGet, "/v1/provider-migrations/"+m.ProviderMigrationID+"/plan", "reader", "", nil)
	if plan.Code != http.StatusOK || plan.Header().Get("ETag") != `"`+m.PlanDigest+`"` {
		t.Fatalf("plan: %d ETag %q", plan.Code, plan.Header().Get("ETag"))
	}
	conforms("ProviderMigrationPlan", plan)

	// A second migration of the same capability away from the same source
	// is created, but blocked.
	second := call(http.MethodPost, "/v1/provider-migrations", "writer", "migration-key-000002", migrationRequestBody("baobab-erp.nextledger"))
	var blocked migration.Migration
	mustNoError(t, json.Unmarshal(second.Body.Bytes(), &blocked))
	if second.Code != http.StatusCreated || !blocked.Blocked {
		t.Fatalf("a competing migration: %d blocked=%v", second.Code, blocked.Blocked)
	}

	for name, tc := range map[string]struct {
		method, path, key string
		body              []byte
		want              int
	}{
		"same provider":      {http.MethodPost, "/v1/provider-migrations/plan", "", migrationRequestBody("baobab-erp.idempiere"), http.StatusUnprocessableEntity},
		"percentage cohort":  {http.MethodPost, "/v1/provider-migrations/plan", "", []byte(strings.Replace(string(migrationRequestBody("baobab-erp.nextledger")), `"tenant_ids":["tn_alpha"]`, `"percentage":10`, 1)), http.StatusBadRequest},
		"no idempotency key": {http.MethodPost, "/v1/provider-migrations", "", migrationRequestBody("baobab-erp.nextledger"), http.StatusBadRequest},
		"unknown migration":  {http.MethodGet, "/v1/provider-migrations/pmg_missing", "", nil, http.StatusNotFound},
		"malformed id":       {http.MethodGet, "/v1/provider-migrations/not-an-id/plan", "", nil, http.StatusNotFound},
	} {
		if w := call(tc.method, tc.path, "writer", tc.key, tc.body); w.Code != tc.want {
			t.Errorf("%s: %d, want %d: %s", name, w.Code, tc.want, w.Body.String())
		}
	}
}
