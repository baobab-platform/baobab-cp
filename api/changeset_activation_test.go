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
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// activationHarness drives the changeset routes for the activation kinds
// (ADR-BCP-021 adoption): the optional governed path beside the direct
// activate routes.
type activationHarness struct {
	t       *testing.T
	handler http.Handler
	dir     string
}

func (h activationHarness) call(method, path, who string, headers map[string]string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
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
	h.handler.ServeHTTP(w, r)
	return w
}

func (h activationHarness) conforms(file, definition string, w *httptest.ResponseRecorder) {
	h.t.Helper()
	var body any
	mustNoError(h.t, json.Unmarshal(w.Body.Bytes(), &body))
	contracttest.ValidateJSON(h.t, contracttest.CompileSchema(h.t, h.dir, "control-plane/v1/"+file+"#/$defs/"+definition), body)
}

func (h activationHarness) expect(w *httptest.ResponseRecorder, status int, code, label string) {
	h.t.Helper()
	if w.Code != status || (code != "" && !strings.Contains(w.Body.String(), code)) {
		h.t.Fatalf("%s: want %d %s, got %d %s", label, status, code, w.Code, w.Body.String())
	}
}

// submitted drafts and submits a changeset for desired, returning it.
func (h activationHarness) submitted(requester, key string, desired map[string]any) (changeset.Changeset, *httptest.ResponseRecorder) {
	h.t.Helper()
	created := h.call(http.MethodPost, "/v1/admin/changesets", requester, map[string]string{"Idempotency-Key": key},
		map[string]any{"title": "Activate", "reason": "Reviewed for launch.", "desired_change": desired})
	h.expect(created, http.StatusCreated, "", "create "+key)
	h.conforms("changeset.schema.json", "Changeset", created)
	var c changeset.Changeset
	mustNoError(h.t, json.Unmarshal(created.Body.Bytes(), &c))
	submitted := h.call(http.MethodPost, "/v1/admin/changesets/"+c.ChangesetID+"/submit", requester,
		map[string]string{"If-Match": created.Header().Get("ETag")}, nil)
	h.expect(submitted, http.StatusOK, "", "submit "+key)
	h.conforms("changeset.schema.json", "Changeset", submitted)
	mustNoError(h.t, json.Unmarshal(submitted.Body.Bytes(), &c))
	return c, submitted
}

func (h activationHarness) decide(c changeset.Changeset, submitted *httptest.ResponseRecorder, who string) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.call(http.MethodPost, "/v1/admin/changesets/"+c.ChangesetID+"/approve", who, map[string]string{"If-Match": submitted.Header().Get("ETag")},
		map[string]any{"plan_id": c.CurrentPlan.PlanID, "plan_version": c.CurrentPlan.PlanVersion, "plan_digest": c.CurrentPlan.PlanDigest, "decision": "APPROVED"})
}

// apply applies an approved changeset and returns its outcome.
func (h activationHarness) apply(c changeset.Changeset, who, key string) changeset.Outcome {
	h.t.Helper()
	path := "/v1/admin/changesets/" + c.ChangesetID
	current := h.call(http.MethodGet, path, who, nil, nil)
	applied := h.call(http.MethodPost, path+"/apply", who, map[string]string{"If-Match": current.Header().Get("ETag"), "Idempotency-Key": key}, nil)
	h.expect(applied, http.StatusAccepted, "SUCCEEDED", "apply "+key)
	h.conforms("execution-operation.schema.json", "ExecutionOperation", applied)
	outcome := h.call(http.MethodGet, path+"/outcome", who, nil, nil)
	h.expect(outcome, http.StatusOK, `"COMPLETED"`, "outcome "+key)
	h.conforms("changeset.schema.json", "ChangeOutcome", outcome)
	var o changeset.Outcome
	mustNoError(h.t, json.Unmarshal(outcome.Body.Bytes(), &o))
	return o
}

func activationDatabase(t *testing.T) (context.Context, *pgxpool.Pool, *repository.PostgresRepository) {
	t.Helper()
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
	return ctx, admin, repo
}

// cleanupChangesets removes the changesets that target ids, with their
// plans, decisions, operations and outcomes.
func cleanupChangesets(ctx context.Context, admin *pgxpool.Pool, targetIDs func() []string) {
	ids := targetIDs()
	for _, stmt := range []string{
		`DELETE FROM changeset.outcome WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = ANY($1))`,
		`DELETE FROM changeset.approval WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = ANY($1))`,
		`UPDATE changeset.changeset SET current_plan_id = NULL WHERE target_id = ANY($1)`,
		`DELETE FROM operations.execution_operation WHERE subject_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = ANY($1))`,
		`DELETE FROM changeset.plan WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = ANY($1))`,
		`DELETE FROM changeset.changeset WHERE target_id = ANY($1)`,
	} {
		admin.Exec(ctx, stmt, ids)
	}
}

// TestMarketActivationChangeset activates a VALIDATED market through a
// MARKET_ACTIVATION changeset: the market's maker cannot approve it, an
// approver without market:approve cannot either, a second administrator
// approves, and apply activates exactly the reviewed revision with the
// approver recorded as activator. A market edited after planning makes
// the plan stale; an ACTIVE market blocks; an unknown market is INVALID.
// The direct activate route is unchanged (TestMarketRoutes).
func TestMarketActivationChangeset(t *testing.T) {
	ctx, admin, repo := activationDatabase(t)
	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	tenant, legalEntity, key := "tn_mca"+suffix, "MCA-API-"+strings.ToUpper(suffix), "ke.b2b"+suffix
	var markets []string
	cleanup := func() {
		cleanupChangesets(ctx, admin, func() []string { return append(markets, "mkt_nosuchmarket"+suffix) })
		admin.Exec(ctx, `DELETE FROM market.market WHERE registry_market_id IN (SELECT market_id FROM market.registry WHERE owner_tenant_id = $1)`, tenant)
		admin.Exec(ctx, `DELETE FROM market.registry WHERE owner_tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntity)
	}
	cleanup()
	t.Cleanup(cleanup)
	mustNoError(t, execErr(admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntity)))
	mustNoError(t, execErr(admin.Exec(ctx, `INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference, tenant_id,
		legal_entity_id, display_name, isolation_strategy, residency_region, desired_state, observed_state)
		VALUES ('BOOTSTRAP', 'Test fixture registered outside admission', 'test-fixture', $1, $2, 'Market changeset test', 'row_level_security',
		'af-south-1', 'active', 'active')`, tenant, legalEntity)))

	identities := repository.NewInMemoryRepository()
	principals := map[string]string{}
	for _, subject := range []string{"maker", "checker", "requester"} {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject, Status: "ACTIVE"}))
		principals[subject] = p.ID
	}
	human := func(subject string, scopes ...string) auth.Principal {
		granted := map[string]struct{}{}
		for _, s := range scopes {
			granted[s] = struct{}{}
		}
		return auth.Principal{Subject: subject, Issuer: testRealm, ActorType: "human", TokenID: "t-" + subject + strings.Join(scopes, ""),
			Scopes: granted, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	h := activationHarness{t: t, dir: contracttest.SharedDir(t), handler: New(Dependencies{Store: &fakeStore{}, Identities: identities,
		Markets: repo, Changesets: repo, Operations: repo, AdminVerifier: tokenVerifier{
			"maker":            human("maker", "market:write", "market:approve", "changeset:read", "changeset:approve"),
			"checker":          human("checker", "market:read", "market:approve", "changeset:read", "changeset:approve"),
			"checker-no-scope": human("checker", "market:read", "changeset:read", "changeset:approve"),
			"requester":        human("requester", "changeset:read", "changeset:write", "operation:read"),
		}})}

	register := func(canonical string) (string, int64) {
		t.Helper()
		body := map[string]any{"canonical_key": canonical, "name": "Kenya B2B", "owner_tenant_id": tenant, "market_type": "B2B",
			"default_country": "XN", "countries": []string{"XN"}, "default_currency": "KES", "allowed_currencies": []string{"KES"},
			"supported_locales": []string{"en-KE"}, "default_locale": "en-KE", "timezone": "Africa/Nairobi", "effective_from": "2026-11-01T00:00:00Z"}
		w := h.call(http.MethodPost, "/v1/markets", "maker", map[string]string{"Idempotency-Key": "mca-" + canonical}, body)
		h.expect(w, http.StatusCreated, `"VALIDATED"`, "register "+canonical)
		var m struct {
			MarketID string `json:"market_id"`
			Revision int64  `json:"revision"`
		}
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &m))
		markets = append(markets, m.MarketID)
		return m.MarketID, m.Revision
	}
	marketStatus := func(id string) (status, activatedBy string) {
		t.Helper()
		mustNoError(t, admin.QueryRow(ctx, `SELECT status, COALESCE(activated_by, '') FROM market.registry WHERE market_id = $1`, id).Scan(&status, &activatedBy))
		return
	}

	id, revision := register(key)
	c, submitted := h.submitted("requester", "mca-cs-"+suffix, map[string]any{"kind": "MARKET_ACTIVATION", "market_id": id})
	if c.State != changeset.StateAwaitingApproval || c.ChangesetType != "MODIFY" || c.TargetScope.Level != "PLATFORM" {
		t.Fatalf("submitted: %+v", c)
	}
	plan := h.call(http.MethodGet, "/v1/admin/changesets/"+c.ChangesetID+"/plan", "checker", nil, nil)
	h.expect(plan, http.StatusOK, "", "plan")
	h.conforms("changeset.schema.json", "ChangesetPlan", plan)
	var p changeset.Plan
	mustNoError(t, json.Unmarshal(plan.Body.Bytes(), &p))
	if len(p.Steps) != 2 || p.Steps[0].Operation != changeset.OpActivateMarket || p.Steps[0].Resources.MarketID != id ||
		p.Steps[0].Resources.TargetRevision != revision || p.Steps[1].Operation != changeset.OpVerifyMarketState {
		t.Fatalf("plan steps: %+v", p.Steps)
	}

	// Maker-checker as on the direct route, plus the kind's approval scope.
	h.expect(h.decide(c, submitted, "maker"), http.StatusForbidden, "MARKET_SELF_ACTIVATION", "the market's maker approving")
	h.expect(h.decide(c, submitted, "checker-no-scope"), http.StatusForbidden, "AUTHORIZATION_DENIED", "approving without market:approve")
	if status, _ := marketStatus(id); status != "VALIDATED" {
		t.Fatalf("a refused approval changed the market: %s", status)
	}
	approved := h.decide(c, submitted, "checker")
	h.expect(approved, http.StatusOK, `"APPROVED"`, "approve")
	h.conforms("approval-decision.schema.json", "ApprovalDecision", approved)
	o := h.apply(c, "requester", "mca-apply-"+suffix)
	if len(o.AffectedResources) != 1 || o.AffectedResources[0].ResourceType != "MARKET" || o.AffectedResources[0].ResourceID != id ||
		o.AffectedResources[0].Before != "VALIDATED" || o.AffectedResources[0].After != "ACTIVE" || o.VerificationResult.Checks[0].Check != "MARKET_STATUS_MATCHES" {
		t.Fatalf("outcome: %+v", o)
	}
	if status, by := marketStatus(id); status != "ACTIVE" || by != principals["checker"] {
		t.Fatalf("market after apply: %s by %s", status, by)
	}

	// An ACTIVE market blocks a second activation.
	blocked, _ := h.submitted("requester", "mca-again-"+suffix, map[string]any{"kind": "MARKET_ACTIVATION", "market_id": id})
	if blocked.State != changeset.StateBlocked || blocked.BlockingReasons[0].Code != changeset.BlockTargetStateConflict {
		t.Fatalf("activating an active market: %+v", blocked)
	}
	// An unknown market is INVALID.
	invalid, _ := h.submitted("requester", "mca-unknown-"+suffix, map[string]any{"kind": "MARKET_ACTIVATION", "market_id": "mkt_nosuchmarket" + suffix})
	if invalid.State != changeset.StateInvalid || invalid.BlockingReasons[0].Code != changeset.BlockTargetNotFound {
		t.Fatalf("an unknown market: %+v", invalid)
	}

	// A market edited after planning: the reviewed plan is stale.
	other, otherRevision := register(key + ".retail")
	stale, staleSubmitted := h.submitted("requester", "mca-stale-"+suffix, map[string]any{"kind": "MARKET_ACTIVATION", "market_id": other})
	edited := h.call(http.MethodPatch, "/v1/markets/"+other, "maker", map[string]string{"If-Match": entityTag(otherRevision)}, map[string]any{"name": "Kenya B2B Retail"})
	h.expect(edited, http.StatusOK, `"VALIDATED"`, "editing after planning")
	h.expect(h.decide(stale, staleSubmitted, "checker"), http.StatusConflict, "PLAN_STALE", "approving a plan for an older revision")
}

// TestMappingActivationChangeset activates a VALIDATED mapping through a
// MAPPING_ACTIVATION changeset: its creator cannot approve, a second
// administrator approves, and apply records that approver as the mapping's
// approver, exactly as the direct route does.
func TestMappingActivationChangeset(t *testing.T) {
	f := newMappingFixture(t)
	ctx, admin, repo := activationDatabase(t)
	short := f.tenant[3:11]
	native := "cus_" + f.tenant[3:15]
	var mappings []string
	t.Cleanup(func() { cleanupChangesets(ctx, admin, func() []string { return mappings }) })

	reference := `{"system_namespace":"medusa","engine_id":"baobab-trade","engine_instance_id":"` + f.instance +
		`","environment":"production","native_entity_type":"customer","native_id":"` + native + `"}`
	refID := f.ok(f.do("creator", http.MethodPost, "/v1/external-references", "", reference), http.StatusCreated, "externalReference")["external_reference_id"].(string)
	id := f.ok(f.do("creator", http.MethodPost, "/v1/mappings", "", f.proposal(f.entity, refID)), http.StatusCreated, "mapping")["mapping_id"].(string)
	mappings = append(mappings, id)
	f.ok(f.do("creator", http.MethodPost, "/v1/mappings/"+id+"/validate", `"1"`, ""), http.StatusOK, "mapping")

	// The same subjects the mapping routes saw, now as registered principals.
	identities := repository.NewInMemoryRepository()
	principal := func(subject string, scopes ...string) auth.Principal {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject + "-" + short, Status: "ACTIVE"}))
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: subject + "-" + short, Issuer: testRealm, ActorType: "human", TokenID: "cs-" + subject, Scopes: set,
			Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	h := activationHarness{t: t, dir: contracttest.SharedDir(t), handler: New(Dependencies{Store: &fakeStore{}, Identities: identities,
		Changesets: repo, Operations: repo, AdminVerifier: tokenVerifier{
			"creator":   principal("creator", "mapping:approve", "changeset:read", "changeset:approve"),
			"approver":  principal("approver", "mapping:approve", "changeset:read", "changeset:approve"),
			"requester": principal("requester", "changeset:read", "changeset:write", "operation:read"),
		}})}

	c, submitted := h.submitted("requester", "mapping-activation-"+short, map[string]any{"kind": "MAPPING_ACTIVATION", "mapping_id": id})
	if c.State != changeset.StateAwaitingApproval || c.ChangesetType != "MODIFY" {
		t.Fatalf("submitted: %+v", c)
	}
	h.expect(h.decide(c, submitted, "creator"), http.StatusForbidden, "MAPPING_SELF_APPROVAL", "the mapping's creator approving")
	h.expect(h.decide(c, submitted, "approver"), http.StatusOK, `"APPROVED"`, "approve")
	o := h.apply(c, "requester", "mapping-activation-apply-"+short)
	if o.AffectedResources[0].ResourceType != "MAPPING" || o.AffectedResources[0].After != "ACTIVE" || o.VerificationResult.Checks[0].Check != "MAPPING_STATUS_MATCHES" {
		t.Fatalf("outcome: %+v", o)
	}
	var status, approvedBy string
	mustNoError(t, admin.QueryRow(ctx, `SELECT status, COALESCE(approved_by, '') FROM mapping.mapping WHERE mapping_id = $1`, id).Scan(&status, &approvedBy))
	if status != "ACTIVE" || approvedBy != "approver-"+short {
		t.Fatalf("mapping after apply: %s approved by %q", status, approvedBy)
	}
}

// TestProviderActivationChangeset drives PROVIDER_ACTIVATION (EA-02D)
// through the routes: a registered DRAFT provider is named by its
// canonical provider_ id, its plan reports every plan check, and without a
// recorded engine release (ADR-BCP-025 ER-02) the plan is BLOCKED by that
// check. Approving needs provider:approve as well as changeset:approve.
// The repository test covers the approved, applied path.
func TestProviderActivationChangeset(t *testing.T) {
	ctx, admin, repo := activationDatabase(t)
	short := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	engine := "baobab-apiactivation" + short
	providerKey, capabilityKey := engine+".engine", "test.apiactivation"+short+".perform"
	var canonical string
	cleanup := func() {
		cleanupChangesets(ctx, admin, func() []string { return []string{canonical} })
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key = $1`, providerKey)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)
	definition := capabilitydomain.Capability{Key: capabilityKey, Name: "API activation test", DomainKey: "test",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}
	_, err := repo.SyncCapabilityCatalogue(ctx, []repository.CatalogueCapability{{Capability: definition, ContractVersions: []int{1},
		DataClassification: "INTERNAL", Owner: engine, Source: "fixtures/api-activation-test", Digest: "sha256:" + strings.Repeat("4", 64)}})
	mustNoError(t, err)
	mustNoError(t, repo.RegisterEngine(ctx, repository.EngineRegistrationRecord{Repository: engine,
		Capabilities: []capabilitydomain.Capability{definition},
		Provider: repository.EngineRegistrationProvider{ProviderKey: providerKey, Name: "API activation test", ProviderType: "BAOBAB_ENGINE",
			EngineKey: "engine", Lifecycle: "DRAFT", Ownership: engine},
		Support: []repository.EngineRegistrationSupport{{CapabilityKey: capabilityKey, ContractVersions: []int{1}}}}))
	var engineID string
	mustNoError(t, admin.QueryRow(ctx, `SELECT canonical_provider_id, engine_id::text FROM capability.capability_provider WHERE provider_key = $1`,
		providerKey).Scan(&canonical, &engineID))
	_, err = admin.Exec(ctx, `INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status)
		VALUES ($1, $2, 'af-south-1', 'staging', 'ACTIVE')`, domain.NewUUIDv7(), engineID)
	mustNoError(t, err)

	identities := repository.NewInMemoryRepository()
	principal := func(subject string, scopes ...string) auth.Principal {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject + "-" + short, Status: "ACTIVE"}))
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: subject + "-" + short, Issuer: testRealm, ActorType: "human", TokenID: "pa-" + subject, Scopes: set,
			Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	h := activationHarness{t: t, dir: contracttest.SharedDir(t), handler: New(Dependencies{Store: &fakeStore{}, Identities: identities,
		Changesets: repo, Operations: repo, AdminVerifier: tokenVerifier{
			"approver":   principal("approver", "changeset:read", "changeset:approve"),
			"provider":   principal("provider", "provider:approve", "changeset:read", "changeset:approve"),
			"requester":  principal("requester", "changeset:read", "changeset:write", "operation:read"),
			"uuid-asker": principal("uuid-asker", "changeset:read", "changeset:write"),
		}})}

	// Only the canonical identifier names a provider.
	uuid := h.call(http.MethodPost, "/v1/admin/changesets", "uuid-asker", map[string]string{"Idempotency-Key": "provider-uuid-" + short},
		map[string]any{"title": "Activate", "reason": "Reviewed.", "desired_change": map[string]any{"kind": "PROVIDER_ACTIVATION", "provider_id": engineID}})
	if uuid.Code != http.StatusBadRequest && uuid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a UUID provider id: %d %s", uuid.Code, uuid.Body.String())
	}

	c, submitted := h.submitted("requester", "provider-activation-"+short, map[string]any{"kind": "PROVIDER_ACTIVATION", "provider_id": canonical})
	if c.State != changeset.StateBlocked || c.ChangesetType != "MODIFY" || len(c.BlockingReasons) != 1 ||
		c.BlockingReasons[0].Code != "PROVIDER_NO_ELIGIBLE_RELEASE" {
		t.Fatalf("submitted: %+v", c)
	}
	plan := h.call(http.MethodGet, "/v1/admin/changesets/"+c.ChangesetID+"/plan", "requester", nil, nil)
	h.expect(plan, http.StatusOK, `"ENGINE_RELEASE"`, "plan")
	h.conforms("changeset.schema.json", "ChangesetPlan", plan)
	h.expect(h.decide(c, submitted, "approver"), http.StatusForbidden, "provider:approve", "an approver without provider:approve")
	h.expect(h.decide(c, submitted, "provider"), http.StatusConflict, "", "approving a blocked plan")
	var status string
	mustNoError(t, admin.QueryRow(ctx, `SELECT status FROM capability.capability_provider WHERE provider_key = $1`, providerKey).Scan(&status))
	if status != "DRAFT" {
		t.Fatalf("a blocked activation changed the provider to %s", status)
	}
}

// TestEngineReleaseApprovalChangeset drives ENGINE_RELEASE_APPROVAL
// (ADR-BCP-025 section 2.4) through the routes: an administrator records a
// CANDIDATE release, and only an approval changeset makes it APPROVED.
// Approving needs engine-release:approve as well as changeset:approve; the
// administrator who recorded the release never approves it
// (RELEASE_SELF_APPROVAL); another does, and apply approves the release
// under that approver's principal. Every response conforms.
func TestEngineReleaseApprovalChangeset(t *testing.T) {
	ctx, admin, repo := activationDatabase(t)
	short := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	engine := "baobab-apiapproval" + short
	capabilityKey := "test.apiapproval" + short + ".perform"
	var releaseID string
	cleanup := func() {
		cleanupChangesets(ctx, admin, func() []string { return []string{releaseID} })
		tx, err := admin.Begin(ctx)
		if err == nil {
			// Releases are never deleted in operation; this test-only
			// cleanup turns the immutability triggers off for itself.
			tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
			for _, stmt := range []string{
				`DELETE FROM topology.engine_release_artifact WHERE engine_release_id IN (SELECT r.engine_release_id FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id WHERE e.code = $1)`,
				`DELETE FROM topology.engine_release_provider_support WHERE engine_release_id IN (SELECT r.engine_release_id FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id WHERE e.code = $1)`,
				`DELETE FROM topology.engine_release WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`,
			} {
				tx.Exec(ctx, stmt, engine)
			}
			tx.Commit(ctx)
		}
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)
	var engineID string
	mustNoError(t, admin.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1) RETURNING engine_id::text`, engine).Scan(&engineID))
	_, err := admin.Exec(ctx, `INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status)
		VALUES ($1, $2, 'af-south-1', 'staging', 'ACTIVE')`, domain.NewUUIDv7(), engineID)
	mustNoError(t, err)
	_, err = repo.SyncCapabilityCatalogue(ctx, []repository.CatalogueCapability{{Capability: capabilitydomain.Capability{Key: capabilityKey,
		Name: "API approval test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL",
		Owner: engine, Source: "fixtures/api-approval-test", Digest: "sha256:" + strings.Repeat("8", 64)}})
	mustNoError(t, err)

	identities := repository.NewInMemoryRepository()
	principals := map[string]string{}
	principal := func(subject string, scopes ...string) auth.Principal {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject + "-" + short, Status: "ACTIVE"}))
		principals[subject] = p.ID
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: subject + "-" + short, Issuer: testRealm, ActorType: "human", TokenID: "ra-" + subject, Scopes: set,
			Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	h := activationHarness{t: t, dir: contracttest.SharedDir(t), handler: New(Dependencies{Store: &fakeStore{}, Identities: identities,
		Changesets: repo, Operations: repo, EngineReleases: repo, AdminVerifier: tokenVerifier{
			"recorder":  principal("recorder", "topology:write", "topology:read", "engine-release:approve", "changeset:read", "changeset:approve"),
			"approver":  principal("approver", "changeset:read", "changeset:approve"),
			"releaser":  principal("releaser", "engine-release:approve", "changeset:read", "changeset:approve", "topology:read"),
			"requester": principal("requester", "changeset:read", "changeset:write", "operation:read"),
		}})}

	recorded := h.call(http.MethodPost, "/v1/engine-releases", "recorder", nil, map[string]any{"engine_id": engine, "release_version": "3.0.0",
		"artifacts":                              []map[string]any{{"artifact_type": "OCI_IMAGE", "repository": "ghcr.io/baobab-platform/" + engine, "digest": "sha256:" + strings.Repeat("c", 52) + short}},
		"provider_support":                       []map[string]any{{"provider_key": engine + ".engine", "capability_key": capabilityKey, "contract_versions": []int{1}}},
		"capability_provider_declaration_digest": "sha256:" + strings.Repeat("d", 64), "source_revision": strings.Repeat("a", 40),
		"reason": "Built from main."})
	h.expect(recorded, http.StatusCreated, `"CANDIDATE"`, "record")
	var rel struct {
		ReleaseID       string `json:"release_id"`
		Status          string `json:"status"`
		StatusChangedBy string `json:"status_changed_by"`
	}
	mustNoError(t, json.Unmarshal(recorded.Body.Bytes(), &rel))
	releaseID = rel.ReleaseID

	// Only an engine release identifier names a release.
	h.expect(h.call(http.MethodPost, "/v1/admin/changesets", "requester", map[string]string{"Idempotency-Key": "release-uuid-" + short},
		map[string]any{"title": "Approve", "reason": "Reviewed.", "desired_change": map[string]any{"kind": "ENGINE_RELEASE_APPROVAL", "release_id": engineID}}),
		http.StatusBadRequest, "", "a UUID release id")

	c, submitted := h.submitted("requester", "release-approval-"+short, map[string]any{"kind": "ENGINE_RELEASE_APPROVAL", "release_id": releaseID})
	if c.State != changeset.StateAwaitingApproval || c.ChangesetType != "MODIFY" {
		t.Fatalf("submitted: %+v", c)
	}
	plan := h.call(http.MethodGet, "/v1/admin/changesets/"+c.ChangesetID+"/plan", "requester", nil, nil)
	h.expect(plan, http.StatusOK, `"APPROVE_ENGINE_RELEASE"`, "plan")
	h.conforms("changeset.schema.json", "ChangesetPlan", plan)
	h.expect(h.decide(c, submitted, "approver"), http.StatusForbidden, "engine-release:approve", "an approver without engine-release:approve")
	h.expect(h.decide(c, submitted, "recorder"), http.StatusForbidden, "RELEASE_SELF_APPROVAL", "the release's recorder approving")
	h.expect(h.decide(c, submitted, "releaser"), http.StatusOK, `"APPROVED"`, "approve")
	o := h.apply(c, "requester", "release-approval-apply-"+short)
	if o.AffectedResources[0].ResourceType != "ENGINE_RELEASE" || o.AffectedResources[0].After != "APPROVED" ||
		o.VerificationResult.Checks[0].Check != "ENGINE_RELEASE_STATUS_MATCHES" {
		t.Fatalf("outcome: %+v", o)
	}
	got := h.call(http.MethodGet, "/v1/engine-releases/"+releaseID, "releaser", nil, nil)
	h.expect(got, http.StatusOK, `"APPROVED"`, "read the approved release")
	mustNoError(t, json.Unmarshal(got.Body.Bytes(), &rel))
	if rel.StatusChangedBy != principals["releaser"] {
		t.Fatalf("approved by %q, want the approver's principal %q", rel.StatusChangedBy, principals["releaser"])
	}
}

// TestDesiredReleaseRoutes drives ADR-BCP-025 gate ER-03 through the
// routes. A release approved through its changeset becomes an engine
// instance's desired release through ENGINE_INSTANCE_DESIRED_RELEASE, which
// needs desired-release:approve. Infrastructure tooling reads it under
// desired-release:read. An administrator deprecates it and revokes it; the
// revocation must dispose of the instance. Every response conforms.
func TestDesiredReleaseRoutes(t *testing.T) {
	ctx, admin, repo := activationDatabase(t)
	short := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	engine := "baobab-apidesired" + short
	capabilityKey := "test.apidesired" + short + ".perform"
	var releaseID, instanceKey string
	cleanup := func() {
		cleanupChangesets(ctx, admin, func() []string { return []string{releaseID, instanceKey} })
		admin.Exec(ctx, `UPDATE topology.engine_instance SET desired_release_id = NULL WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
		tx, err := admin.Begin(ctx)
		if err == nil {
			// Releases are never deleted in operation; this test-only
			// cleanup turns the immutability triggers off for itself.
			tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
			for _, stmt := range []string{
				`DELETE FROM topology.engine_release_artifact WHERE engine_release_id IN (SELECT r.engine_release_id FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id WHERE e.code = $1)`,
				`DELETE FROM topology.engine_release_provider_support WHERE engine_release_id IN (SELECT r.engine_release_id FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id WHERE e.code = $1)`,
				`DELETE FROM topology.engine_release WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`,
			} {
				tx.Exec(ctx, stmt, engine)
			}
			tx.Commit(ctx)
		}
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)
	var engineID string
	mustNoError(t, admin.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1) RETURNING engine_id::text`, engine).Scan(&engineID))
	mustNoError(t, admin.QueryRow(ctx, `INSERT INTO topology.engine_instance(engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE') RETURNING engine_instance_key`, engineID).Scan(&instanceKey))
	_, err := repo.SyncCapabilityCatalogue(ctx, []repository.CatalogueCapability{{Capability: capabilitydomain.Capability{Key: capabilityKey,
		Name: "API desired test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL",
		Owner: engine, Source: "fixtures/api-desired-test", Digest: "sha256:" + strings.Repeat("5", 64)}})
	mustNoError(t, err)

	identities := repository.NewInMemoryRepository()
	principal := func(subject string, scopes ...string) auth.Principal {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject + "-" + short, Status: "ACTIVE"}))
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: subject + "-" + short, Issuer: testRealm, ActorType: "human", TokenID: "dr-" + subject, Scopes: set,
			Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	workload := func(client string, scopes ...string) auth.Principal {
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: client, Issuer: testRealm, ActorType: "workload", ClientID: client, TokenID: "dr-" + client, Scopes: set}
	}
	h := activationHarness{t: t, dir: contracttest.SharedDir(t), handler: New(Dependencies{Store: &fakeStore{}, Identities: identities,
		Changesets: repo, Operations: repo, EngineReleases: repo, DesiredReleases: repo,
		AdminVerifier: tokenVerifier{
			"requester": principal("requester", "changeset:read", "changeset:write", "operation:read"),
			"releaser":  principal("releaser", "engine-release:approve", "changeset:read", "changeset:approve"),
			"approver":  principal("approver", "changeset:read", "changeset:approve"),
			"deployer":  principal("deployer", "desired-release:approve", "changeset:read", "changeset:approve"),
			"operator":  principal("operator", "topology:write", "topology:read"),
			"reader":    principal("reader", "topology:read"),
		},
		WorkloadVerifier: tokenVerifier{
			"tooling":  workload("deploy-controller", "desired-release:read"),
			"stranger": workload("other-workload", "context:resolve"),
		}})}
	topology := func(definition string, w *httptest.ResponseRecorder) {
		t.Helper()
		var v any
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &v))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, h.dir, "topology/v1/release.schema.json#/$defs/"+definition), v)
	}

	rel, _, err := repo.RecordEngineRelease(ctx, release.RecordRequest{EngineID: engine, ReleaseVersion: "4.0.0",
		Artifacts: []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engine,
			Digest: "sha256:" + strings.Repeat("e", 52) + short}},
		ProviderSupport:                     []release.ProviderSupport{{ProviderKey: engine + ".engine", CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
		CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64), SourceRevision: strings.Repeat("a", 40),
		Reason: "Built from main."}, "workload:release-tooling", time.Now().UTC().Truncate(time.Microsecond),
		repository.AuditActor{ActorID: "workload:release-tooling", ActorType: "workload", CorrelationID: domain.NewUUIDv7()})
	mustNoError(t, err)
	releaseID = rel.ReleaseID
	c, submitted := h.submitted("requester", "approve-"+short, map[string]any{"kind": "ENGINE_RELEASE_APPROVAL", "release_id": releaseID})
	h.expect(h.decide(c, submitted, "releaser"), http.StatusOK, `"APPROVED"`, "approve the release")
	h.apply(c, "requester", "approve-apply-"+short)

	// Desiring it is a changeset approved under desired-release:approve.
	c, submitted = h.submitted("requester", "desire-"+short,
		map[string]any{"kind": "ENGINE_INSTANCE_DESIRED_RELEASE", "engine_instance_id": instanceKey, "release_id": releaseID})
	if c.State != changeset.StateAwaitingApproval || c.ChangesetType != "MODIFY" {
		t.Fatalf("submitted: %+v", c)
	}
	h.expect(h.decide(c, submitted, "approver"), http.StatusForbidden, "desired-release:approve", "an approver without desired-release:approve")
	h.expect(h.decide(c, submitted, "deployer"), http.StatusOK, `"APPROVED"`, "approve the desired release")
	o := h.apply(c, "requester", "desire-apply-"+short)
	if o.AffectedResources[0].ResourceType != "ENGINE_INSTANCE" || o.AffectedResources[0].After != releaseID {
		t.Fatalf("outcome: %+v", o)
	}

	// Tooling reads it; so does an administrator.
	path := "/v1/engine-instances/" + instanceKey + "/desired-release"
	read := h.call(http.MethodGet, path, "tooling", nil, nil)
	h.expect(read, http.StatusOK, releaseID, "tooling reads the desired release")
	topology("EngineInstanceDesiredRelease", read)
	h.expect(h.call(http.MethodGet, path, "reader", nil, nil), http.StatusOK, releaseID, "an administrator reads it")
	h.expect(h.call(http.MethodGet, path, "stranger", nil, nil), http.StatusForbidden, "", "a workload without desired-release:read")
	h.expect(h.call(http.MethodGet, "/v1/engine-instances/ei_00000000000000000000000000000000/desired-release", "tooling", nil, nil),
		http.StatusNotFound, "ENGINE_INSTANCE_NOT_FOUND", "an unknown instance")

	// Deprecation and revocation.
	statusPath := "/v1/engine-releases/" + releaseID + "/status-changes"
	h.expect(h.call(http.MethodPost, statusPath, "reader", nil, map[string]any{"target_status": "DEPRECATED", "reason": "Superseded."}),
		http.StatusForbidden, "", "an administrator without topology:write")
	h.expect(h.call(http.MethodPost, statusPath, "operator", nil, map[string]any{"target_status": "APPROVED", "reason": "Qualified."}),
		http.StatusBadRequest, "VALIDATION_FAILED", "an approval as a status change")
	deprecated := h.call(http.MethodPost, statusPath, "operator", nil, map[string]any{"target_status": "DEPRECATED", "reason": "Superseded."})
	h.expect(deprecated, http.StatusOK, `"DEPRECATED"`, "deprecate")
	topology("EngineRelease", deprecated)
	h.expect(h.call(http.MethodPost, statusPath, "operator", nil, map[string]any{"target_status": "REVOKED", "reason": "Withdrawn.",
		"desired_release_dispositions": []map[string]any{}}), http.StatusUnprocessableEntity, "RELEASE_REVOCATION_UNCOVERED", "an uncovered revocation")
	revoked := h.call(http.MethodPost, statusPath, "operator", nil, map[string]any{"target_status": "REVOKED", "reason": "Withdrawn.",
		"desired_release_dispositions": []map[string]any{{"engine_instance_id": instanceKey, "action": "CLEAR"}}})
	h.expect(revoked, http.StatusOK, `"REVOKED"`, "revoke, clearing the instance")
	topology("EngineRelease", revoked)
	cleared := h.call(http.MethodGet, path, "tooling", nil, nil)
	h.expect(cleared, http.StatusOK, `"desired_release_id":null`, "the cleared desired release")
	topology("EngineInstanceDesiredRelease", cleared)
	h.expect(h.call(http.MethodPost, statusPath, "operator", nil, map[string]any{"target_status": "DEPRECATED", "reason": "Again."}),
		http.StatusConflict, "RELEASE_STATUS_TRANSITION_INVALID", "deprecating a revoked release")
}
