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

// TestMarketRoutes drives a market through the four registry routes: it
// is registered DRAFT with findings, updated to VALIDATED, refused
// activation by its maker, activated by a second administrator, and then
// refused further edits. Workloads read only active markets. Every body
// conforms to the Shared contract.
func TestMarketRoutes(t *testing.T) {
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
	tenant, legalEntity, key := "tn_mkt"+suffix, "MKT-API-"+strings.ToUpper(suffix), "za.b2b"+suffix
	cleanup := func() {
		admin.Exec(ctx, `UPDATE market.registry SET parent_market_id = NULL WHERE owner_tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM market.registry WHERE owner_tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntity)
	}
	cleanup()
	t.Cleanup(cleanup)
	mustNoError(t, execErr(admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntity)))
	mustNoError(t, execErr(admin.Exec(ctx, `INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference, tenant_id,
		legal_entity_id, display_name, isolation_strategy, residency_region, desired_state, observed_state)
		VALUES ('BOOTSTRAP', 'Test fixture registered outside admission', 'test-fixture', $1, $2, 'Market API test', 'row_level_security',
		'af-south-1', 'active', 'active')`, tenant, legalEntity)))

	identities := repository.NewInMemoryRepository()
	for _, subject := range []string{"maker", "checker"} {
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
	handler := New(Dependencies{Store: &fakeStore{}, Identities: identities, Markets: repo,
		AdminVerifier: tokenVerifier{
			"maker":            human("maker", "market:write", "market:approve"),
			"checker":          human("checker", "market:write", "market:approve"),
			"checker-no-scope": human("checker", "market:write"),
			"approver-only":    human("checker", "market:approve"),
		},
		WorkloadVerifier: tokenVerifier{
			"estate": {Subject: "svc-estate", Issuer: testRealm, ActorType: "workload", ClientID: "estate-client", TokenID: "t-estate",
				Scopes: map[string]struct{}{"market:read": {}}},
			"estate-owner": {Subject: "svc-owner", Issuer: testRealm, ActorType: "workload", ClientID: "owner-client", TokenID: "t-owner",
				TenantID: tenant, Scopes: map[string]struct{}{"market:read": {}}},
			"estate-other": {Subject: "svc-other", Issuer: testRealm, ActorType: "workload", ClientID: "other-client", TokenID: "t-other",
				TenantID: "tn_someoneelse", Scopes: map[string]struct{}{"market:read": {}}},
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
	marketSchema := contracttest.CompileSchema(t, dir, "control-plane/v1/market.schema.json#/$defs/market")
	type view struct {
		MarketID string `json:"market_id"`
		Status   string `json:"status"`
		Revision int64  `json:"revision"`
		Findings []struct {
			Code string `json:"code"`
		} `json:"validation_findings"`
		ActivatedBy string `json:"activated_by"`
	}
	read := func(w *httptest.ResponseRecorder, status int, label string) view {
		t.Helper()
		if w.Code != status {
			t.Fatalf("%s: %d %s", label, w.Code, w.Body.String())
		}
		var body any
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		contracttest.ValidateJSON(t, marketSchema, body)
		var v view
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &v))
		if got := w.Header().Get("ETag"); got != entityTag(v.Revision) {
			t.Fatalf("%s: ETag %q for revision %d", label, got, v.Revision)
		}
		return v
	}
	codes := func(v view) []string {
		var out []string
		for _, f := range v.Findings {
			out = append(out, f.Code)
		}
		return out
	}
	expectProblem := func(w *httptest.ResponseRecorder, status int, code, label string) {
		t.Helper()
		if w.Code != status || !strings.Contains(w.Body.String(), code) {
			t.Fatalf("%s: want %d %s, got %d %s", label, status, code, w.Code, w.Body.String())
		}
	}

	create := map[string]any{"canonical_key": key, "name": "South Africa B2B", "owner_tenant_id": tenant, "market_type": "B2B",
		"default_country": "ZA", "countries": []string{"ZA"}, "default_currency": "ZAR", "allowed_currencies": []string{"ZAR", "USD"},
		"supported_locales": []string{"en-ZA"}, "timezone": "Africa/Johannesburg"}
	idem := map[string]string{"Idempotency-Key": "market-create-" + suffix}

	// The caller never states status, identity or the approver.
	expectProblem(call(http.MethodPost, "/v1/markets", "maker", idem, map[string]any{"status": "ACTIVE", "canonical_key": key}),
		http.StatusBadRequest, "VALIDATION_FAILED", "a create naming its status")
	expectProblem(call(http.MethodPost, "/v1/markets", "maker", nil, create), http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "a create without a key")
	unknownOwner := map[string]any{}
	for k, v := range create {
		unknownOwner[k] = v
	}
	unknownOwner["owner_tenant_id"], unknownOwner["canonical_key"] = "tn_nosuchtenant", key+".other"
	expectProblem(call(http.MethodPost, "/v1/markets", "maker", map[string]string{"Idempotency-Key": "market-owner-" + suffix}, unknownOwner),
		http.StatusUnprocessableEntity, "MARKET_OWNER_UNKNOWN", "an unknown owner")

	// Registered DRAFT: no locale and no effective_from yet.
	w := call(http.MethodPost, "/v1/markets", "maker", idem, create)
	draft := read(w, http.StatusCreated, "create")
	if draft.Status != "DRAFT" || strings.Join(codes(draft), ",") != "MARKET_LOCALE_REQUIRED,MARKET_EFFECTIVE_FROM_REQUIRED" ||
		w.Header().Get("Location") != "/v1/markets/"+draft.MarketID {
		t.Fatalf("draft: %+v %s", draft, w.Header().Get("Location"))
	}
	if again := read(call(http.MethodPost, "/v1/markets", "maker", idem, create), http.StatusCreated, "replay"); again.MarketID != draft.MarketID {
		t.Fatalf("a replayed create registered %s", again.MarketID)
	}
	changed := map[string]any{}
	for k, v := range create {
		changed[k] = v
	}
	changed["name"] = "Another"
	expectProblem(call(http.MethodPost, "/v1/markets", "maker", idem, changed), http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "a reused key")
	expectProblem(call(http.MethodPost, "/v1/markets", "maker", map[string]string{"Idempotency-Key": "market-dup-" + suffix}, create),
		http.StatusConflict, "MARKET_CANONICAL_KEY_TAKEN", "a duplicate canonical key")

	path := "/v1/markets/" + draft.MarketID
	// Workloads never see a draft; administrators do.
	expectProblem(call(http.MethodGet, path, "estate", nil, nil), http.StatusNotFound, "MARKET_NOT_FOUND", "a workload reading a draft")
	read(call(http.MethodGet, path, "maker", nil, nil), http.StatusOK, "an administrator reading a draft")
	expectProblem(call(http.MethodGet, "/v1/markets/mkt_nosuchmarket", "maker", nil, nil), http.StatusNotFound, "MARKET_NOT_FOUND", "an unknown market")

	// Updates need the current revision and never change identity.
	patch := map[string]any{"default_locale": "en-ZA", "effective_from": "2026-11-01T00:00:00Z"}
	expectProblem(call(http.MethodPatch, path, "maker", nil, patch), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "an update without If-Match")
	expectProblem(call(http.MethodPatch, path, "maker", map[string]string{"If-Match": `"9"`}, patch),
		http.StatusPreconditionFailed, "MARKET_REVISION_MISMATCH", "a stale update")
	expectProblem(call(http.MethodPatch, path, "maker", map[string]string{"If-Match": `"1"`}, map[string]any{"canonical_key": "x.y"}),
		http.StatusBadRequest, "VALIDATION_FAILED", "an update changing the canonical key")

	// An unknown parent is a finding, not a failure.
	withParent := read(call(http.MethodPatch, path, "maker", map[string]string{"If-Match": `"1"`},
		map[string]any{"default_locale": "en-ZA", "effective_from": "2026-11-01T00:00:00Z", "parent_market_id": "mkt_nosuchparent"}),
		http.StatusOK, "an update naming an unknown parent")
	if withParent.Status != "DRAFT" || strings.Join(codes(withParent), ",") != "MARKET_PARENT_UNKNOWN" {
		t.Fatalf("unknown parent: %+v", withParent)
	}
	// Clearing the parent with null validates the market.
	validated := read(call(http.MethodPatch, path, "maker", map[string]string{"If-Match": `"2"`}, map[string]any{"parent_market_id": nil}),
		http.StatusOK, "an update clearing the parent")
	if validated.Status != "VALIDATED" || len(validated.Findings) != 0 || validated.Revision != 3 {
		t.Fatalf("validated: %+v", validated)
	}

	// Activation: never by the maker, only with the approve scope, only at
	// the reviewed revision.
	expectProblem(call(http.MethodPost, path+"/activate", "maker", map[string]string{"If-Match": `"3"`}, map[string]any{}),
		http.StatusForbidden, "MARKET_SELF_ACTIVATION", "the maker activating")
	expectProblem(call(http.MethodPost, path+"/activate", "checker-no-scope", map[string]string{"If-Match": `"3"`}, map[string]any{}),
		http.StatusForbidden, "AUTHORIZATION_DENIED", "activation without market:approve")
	expectProblem(call(http.MethodPost, path+"/activate", "checker", map[string]string{"If-Match": `"3"`}, map[string]any{"approved_by": "prn_x"}),
		http.StatusBadRequest, "VALIDATION_FAILED", "an activation naming its approver")
	// A least-privilege approver reads the revision to name.
	if v := read(call(http.MethodGet, path, "approver-only", nil, nil), http.StatusOK, "an approver reading"); v.Revision != 3 {
		t.Fatalf("approver read: %+v", v)
	}
	expectProblem(call(http.MethodPost, path+"/activate", "checker", map[string]string{"If-Match": `"2"`}, nil),
		http.StatusPreconditionFailed, "MARKET_REVISION_MISMATCH", "activating an older revision")
	active := read(call(http.MethodPost, path+"/activate", "checker", map[string]string{"If-Match": `"3"`},
		map[string]any{"reason": "Reviewed against the launch plan."}), http.StatusOK, "activation")
	if active.Status != "ACTIVE" || active.ActivatedBy == "" || active.Revision != 4 {
		t.Fatalf("active: %+v", active)
	}

	// An active market is read by workloads and changed only by a governed change.
	if v := read(call(http.MethodGet, path, "estate", nil, nil), http.StatusOK, "a workload reading an active market"); v.Status != "ACTIVE" {
		t.Fatalf("workload read: %+v", v)
	}
	// A workload of another tenant never sees the market; its own does.
	expectProblem(call(http.MethodGet, path, "estate-other", nil, nil), http.StatusNotFound, "MARKET_NOT_FOUND", "another tenant's workload")
	read(call(http.MethodGet, path, "estate-owner", nil, nil), http.StatusOK, "the owner's workload")
	expectProblem(call(http.MethodPatch, path, "checker", map[string]string{"If-Match": `"4"`}, map[string]any{"name": "Renamed"}),
		http.StatusConflict, "MARKET_NOT_EDITABLE", "editing an active market")
	expectProblem(call(http.MethodPost, path+"/activate", "checker", map[string]string{"If-Match": `"4"`}, nil),
		http.StatusConflict, "MARKET_NOT_VALIDATED", "activating twice")

	// A second market may name the first as its parent.
	child := map[string]any{}
	for k, v := range create {
		child[k] = v
	}
	child["canonical_key"], child["parent_market_id"], child["default_locale"], child["effective_from"] =
		key+".retail", draft.MarketID, "en-ZA", "2026-11-01T00:00:00Z"
	if v := read(call(http.MethodPost, "/v1/markets", "checker", map[string]string{"Idempotency-Key": "market-child-" + suffix}, child),
		http.StatusCreated, "a child market"); v.Status != "VALIDATED" {
		t.Fatalf("child: %+v", v)
	}

	// Every change is audited.
	var audited int
	mustNoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target = $1`, "market/"+draft.MarketID).Scan(&audited))
	if audited != 4 {
		t.Fatalf("expected 4 audit events (registered, two updates, activated), got %d", audited)
	}
}
