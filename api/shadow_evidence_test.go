package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// evidenceFake keeps shadow observations in memory, as the repository sums them.
type evidenceFake struct {
	mu       sync.Mutex
	rows     map[string]*administration.PermissionEvidence
	failNext bool
	batches  int
}

func newEvidenceFake() *evidenceFake {
	return &evidenceFake{rows: map[string]*administration.PermissionEvidence{}}
}

func (f *evidenceFake) RecordShadowObservations(_ context.Context, obs []repository.ShadowObservation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return errors.New("store down")
	}
	f.batches++
	for _, o := range obs {
		ev := f.rows[o.Permission]
		if ev == nil {
			first, last := o.FirstAt, o.LastAt
			ev = &administration.PermissionEvidence{Permission: o.Permission, FirstObservedAt: &first, LastObservedAt: &last, ObservedDays: 1}
			f.rows[o.Permission] = ev
		}
		ev.Decisions += o.Decisions
		switch o.Agreement {
		case metrics.ShadowAgree:
			ev.Agree += o.Decisions
		case metrics.ShadowGrantsBroader:
			ev.GrantsBroader += o.Decisions
		case metrics.ShadowGrantsNarrower:
			ev.GrantsNarrower += o.Decisions
		case metrics.ShadowNotEvaluated:
			ev.NotEvaluated += o.Decisions
		}
	}
	return nil
}

func (f *evidenceFake) ShadowEvidence(context.Context) ([]administration.PermissionEvidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []administration.PermissionEvidence
	for _, ev := range f.rows {
		out = append(out, *ev)
	}
	return out, nil
}

func TestShadowRecorderAggregatesAndKeepsCountsAcrossAFailedFlush(t *testing.T) {
	store := newEvidenceFake()
	rec := newShadowRecorder(store)
	now := time.Now()
	for i := 0; i < 3; i++ {
		rec.add("tenant.view", metrics.ShadowAllow, metrics.ShadowAllow, metrics.ShadowAgree, now)
	}
	rec.add("tenant.view", metrics.ShadowAllow, metrics.ShadowDeny, metrics.ShadowGrantsNarrower, now)
	// Values outside the closed sets the table accepts are dropped.
	rec.add("Tenant View", metrics.ShadowAllow, metrics.ShadowAllow, metrics.ShadowAgree, now)
	rec.add("tenant.view", "maybe", metrics.ShadowAllow, metrics.ShadowAgree, now)
	rec.add("tenant.view", metrics.ShadowAllow, "sure", metrics.ShadowAgree, now)
	rec.add("tenant.view", metrics.ShadowAllow, metrics.ShadowAllow, "same", now)
	store.failNext = true
	if err := rec.Flush(context.Background()); err == nil {
		t.Fatal("a failing store must report the failure")
	}
	if err := rec.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := store.ShadowEvidence(context.Background())
	if len(got) != 1 || got[0].Decisions != 4 || got[0].Agree != 3 || got[0].GrantsNarrower != 1 {
		t.Fatalf("a failed flush must lose nothing and invalid values must not count: %+v", got)
	}
	var none *shadowRecorder
	none.add("tenant.view", metrics.ShadowAllow, metrics.ShadowAllow, metrics.ShadowAgree, now)
	if newShadowRecorder(nil) != nil || none.Flush(context.Background()) != nil {
		t.Fatal("no store records nothing and fails nothing")
	}
}

func TestReadinessRouteReportsEvidenceAndEnforcesNothing(t *testing.T) {
	store := newEvidenceFake()
	handler := New(Dependencies{Store: &fakeStore{}, ShadowEvidence: store, AdminVerifier: tokenVerifier{
		"admin": {Subject: "admin", Issuer: testRealm, ActorType: "human", TokenID: "t-admin",
			Scopes: map[string]struct{}{"administrator:read": {}}, Roles: map[string]struct{}{RolePlatformAdmin: {}}},
		"noscope": {Subject: "admin", Issuer: testRealm, ActorType: "human", TokenID: "t-noscope",
			Scopes: map[string]struct{}{"tenant:read": {}}, Roles: map[string]struct{}{RolePlatformAdmin: {}}},
		"norole": {Subject: "other", Issuer: testRealm, ActorType: "human", TokenID: "t-norole",
			Scopes: map[string]struct{}{"administrator:read": {}}, Roles: map[string]struct{}{}},
	}})
	get := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/v1/admin/authority-migration/readiness", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := get("noscope"); w.Code != http.StatusForbidden {
		t.Errorf("the scope is necessary: %d", w.Code)
	}
	if w := get("norole"); w.Code != http.StatusForbidden {
		t.Errorf("the role is necessary: %d", w.Code)
	}
	// Plenty of clean evidence still reports nothing ready: the criteria are
	// not approved, and CRITICAL is prohibited.
	now := time.Now()
	for _, key := range []string{"tenant.view", "tenant.decommission"} {
		first := now.AddDate(0, 0, -30)
		store.rows[key] = &administration.PermissionEvidence{Permission: key, Decisions: 9000, Agree: 9000, ObservedDays: 30, FirstObservedAt: &first, LastObservedAt: &now}
	}
	w := get("admin")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("readiness: %d %v", w.Code, w.Header())
	}
	var report administration.MigrationReadiness
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.CriteriaStatus == "APPROVED" || len(report.Permissions) < 50 {
		t.Fatalf("unexpected report header: %s, %d permissions", report.CriteriaStatus, len(report.Permissions))
	}
	for _, p := range report.Permissions {
		if p.Ready || p.Enforcement != administration.ModeRoleAuthoritative || len(p.Blockers) == 0 {
			t.Fatalf("nothing is ready or enforced while the criteria are not approved: %+v", p)
		}
		if p.Permission == "tenant.decommission" && p.Wave != 4 {
			t.Fatalf("CRITICAL is the last wave: %+v", p)
		}
	}
	if dir := os.Getenv("SHARED_CONTRACTS_DIR"); dir != "" {
		var raw map[string]any
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "administration/v1/authority-migration.schema.json#/$defs/MigrationReadiness"), raw)
	}
}

// TestOperationRoutesAreShadowed: the durable-operation routes authorise
// inside their handler, so they were a blind spot; the comparison is made
// there, for the operation's tenant, and never changes the decision.
func TestOperationRoutesAreShadowed(t *testing.T) {
	var seen []struct {
		allowed bool
		tenant  string
	}
	h := operationHandler{
		tenantAdminOf: func(*http.Request, auth.Principal, string) adminAuthority { return adminDenied },
		decide: func(_ *http.Request, _ auth.Principal, allowed bool, tenant string) adminVerdict {
			seen = append(seen, struct {
				allowed bool
				tenant  string
			}{allowed, tenant})
			return adminVerdict{Allowed: allowed}
		},
	}
	call := func(roles ...string) (adminAuthority, bool) {
		p := auth.Principal{Subject: "admin", Issuer: testRealm, ActorType: "human", Roles: map[string]struct{}{}}
		for _, role := range roles {
			p.Roles[role] = struct{}{}
		}
		r := httptest.NewRequest(http.MethodGet, "/v1/admin/operations/op_1", nil).WithContext(auth.WithPrincipal(context.Background(), p))
		return h.authorised(r, operations.Operation{ID: "op_1", TenantID: "tn_ug"})
	}
	if _, ok := call(RolePlatformAdmin); !ok {
		t.Fatal("a platform administrator reads every operation")
	}
	if _, ok := call(); ok {
		t.Fatal("no role reads nothing")
	}
	if len(seen) != 2 || !seen[0].allowed || seen[1].allowed || seen[0].tenant != "tn_ug" {
		t.Fatalf("the comparison must see each legacy decision and the operation's tenant: %+v", seen)
	}
	for _, key := range []string{"GET /v1/admin/operations/{operationID}", "POST /v1/admin/operations/{operationID}/retry", "POST /v1/admin/operations/{operationID}/cancel"} {
		if adminRoutePermissions[key] == "" {
			t.Errorf("%s maps to no permission", key)
		}
	}
}
