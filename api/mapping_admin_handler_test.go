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
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// tokenVerifier maps bearer tokens to principals, for tests with several
// callers.
type tokenVerifier map[string]auth.Principal

func (v tokenVerifier) Verify(_ context.Context, raw string) (auth.Principal, error) {
	principal, ok := v[raw]
	if !ok {
		return auth.Principal{}, context.DeadlineExceeded
	}
	return principal, nil
}

type mappingFixture struct {
	t        *testing.T
	ctx      context.Context
	admin    *pgxpool.Pool
	handler  http.Handler
	tenant   string
	entity   string
	sibling  string
	foreign  string
	instance string
	schemas  map[string]*jsonschema.Schema
}

func newMappingFixture(t *testing.T) *mappingFixture {
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

	// The random tail of a UUIDv7, not its time-ordered head.
	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[12:] + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[12:]
	f := &mappingFixture{t: t, ctx: ctx, admin: admin, tenant: "tn_" + suffix[:20], schemas: map[string]*jsonschema.Schema{}}
	entity := func(tenant string) string {
		id := domain.NewUUIDv7()
		if _, err := admin.Exec(ctx, `INSERT INTO registry.canonical_entity (canonical_entity_id, tenant_id, entity_type, external_key, status)
			VALUES ($1::uuid, $2, 'PRODUCT', $3, 'active')`, id, tenant, "product:"+id[len(id)-12:]); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.entity, f.sibling, f.foreign = entity(f.tenant), entity(f.tenant), entity("tn_"+suffix[20:40])
	engine, instance := domain.NewUUIDv7(), domain.NewUUIDv7()
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine (engine_id, code, name) VALUES ($1::uuid, $2, 'mapping test')`, engine, "baobab-mapping-test-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO topology.engine_instance (engine_instance_id, engine_id, region, environment, status)
		VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE')`, instance, engine); err != nil {
		t.Fatal(err)
	}
	f.instance = domain.EngineInstanceKey(instance)

	principal := func(subject string, scopes ...string) auth.Principal {
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: subject + "-" + suffix[:8], ActorType: "human", TokenID: "token-" + subject, Scopes: set,
			Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	admins := tokenVerifier{
		// The creator could approve, were it not the creator (four eyes).
		"creator":  principal("creator", "mapping:write", "mapping:approve", "canonical:read"),
		"approver": principal("approver", "mapping:approve", "mapping:write", "canonical:read"),
	}
	workloads := tokenVerifier{
		"workload":       {Subject: "trade", ActorType: "workload", TenantID: f.tenant, ClientID: "baobab-trade", TokenID: "w1", Scopes: map[string]struct{}{"mapping:read": {}, "mapping:resolve": {}}},
		"other-workload": {Subject: "trade", ActorType: "workload", TenantID: "tn_elsewhere", ClientID: "baobab-trade", TokenID: "w2", Scopes: map[string]struct{}{"mapping:read": {}, "mapping:resolve": {}}},
	}
	f.handler = New(Dependencies{Store: &fakeStore{}, AdminVerifier: admins, WorkloadVerifier: workloads, Mappings: repo})
	if dir := os.Getenv("SHARED_CONTRACTS_DIR"); dir != "" {
		for _, def := range []string{"externalReference", "externalReferenceList", "mapping", "externalReferenceResolutionResponse"} {
			f.schemas[def] = contracttest.CompileSchema(t, dir, "control-plane/v1/canonical-mapping.schema.json#/$defs/"+def)
		}
	}
	return f
}

func (f *mappingFixture) do(token, method, path, ifMatch, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	key := ""
	if method == http.MethodPost && path == "/v1/mappings" {
		key = "test-" + domain.NewUUIDv7()
	}
	return f.doKeyed(token, method, path, ifMatch, key, body)
}

// doKeyed sends a request with an explicit Idempotency-Key ("" for none).
func (f *mappingFixture) doKeyed(token, method, path, ifMatch, key, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if ifMatch != "" {
		request.Header.Set("If-Match", ifMatch)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

// ok asserts a status and a schema-conformant body.
func (f *mappingFixture) ok(response *httptest.ResponseRecorder, status int, definition string) map[string]any {
	f.t.Helper()
	if response.Code != status {
		f.t.Fatalf("got %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		f.t.Fatal(err)
	}
	if schema := f.schemas[definition]; schema != nil {
		contracttest.ValidateJSON(f.t, schema, body)
	}
	return body
}

func (f *mappingFixture) refused(response *httptest.ResponseRecorder, status int, code string) {
	f.t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	if response.Code != status || body.Code != code {
		f.t.Fatalf("got %d %s, want %d %s: %s", response.Code, body.Code, status, code, response.Body.String())
	}
}

func (f *mappingFixture) proposal(entity, reference string) string {
	return `{"tenant_id":"` + f.tenant + `","mapping_type":"COMMERCE","canonical_entity_id":"` + entity +
		`","external_reference_id":"` + reference + `","direction":"BIDIRECTIONAL","cardinality":"ONE_TO_ONE",` +
		`"authority":"trade","confidence":"CONFIRMED","effective_from":"2026-01-01T00:00:00Z"}`
}

// activate proposes, validates and (as a second administrator) activates.
func (f *mappingFixture) activate(body string) string {
	f.t.Helper()
	created := f.ok(f.do("creator", http.MethodPost, "/v1/mappings", "", body), http.StatusCreated, "mapping")
	id := created["mapping_id"].(string)
	f.ok(f.do("creator", http.MethodPost, "/v1/mappings/"+id+"/validate", `"1"`, ""), http.StatusOK, "mapping")
	f.ok(f.do("approver", http.MethodPost, "/v1/mappings/"+id+"/activate", `"2"`, ""), http.StatusOK, "mapping")
	return id
}

// TestExternalReferencesAndMappingsFollowTheContract drives ADR-SHARED-013
// end to end against PostgreSQL: a native identity is recorded once, for a
// registered system only; a mapping is proposed, validated and activated by
// a second administrator, each step under If-Match; resolution follows only
// active mappings and fails closed on ambiguity; and a workload sees only
// its own tenant.
func TestExternalReferencesAndMappingsFollowTheContract(t *testing.T) {
	f := newMappingFixture(t)
	native := "cus_" + f.tenant[3:15]
	reference := `{"system_namespace":"medusa","engine_id":"baobab-trade","engine_instance_id":"` + f.instance +
		`","environment":"production","native_entity_type":"customer","native_id":"` + native + `"}`

	// Recording native identities.
	f.refused(f.do("creator", http.MethodPost, "/v1/external-references", "", strings.Replace(reference, `"medusa"`, `"payload"`, 1)),
		http.StatusUnprocessableEntity, "EXTERNAL_SYSTEM_NOT_REGISTERED")
	f.refused(f.do("creator", http.MethodPost, "/v1/external-references", "", strings.Replace(reference, `"baobab-trade"`, `"baobab_trade"`, 1)),
		http.StatusBadRequest, "VALIDATION_FAILED")
	f.refused(f.do("creator", http.MethodPost, "/v1/external-references", "", strings.Replace(reference, f.instance, "ei_notregistered", 1)),
		http.StatusUnprocessableEntity, "ENGINE_INSTANCE_NOT_REGISTERED")
	f.refused(f.do("creator", http.MethodPost, "/v1/external-references", "", strings.Replace(reference, `"native_id"`, `"canonical_entity_id":"x","native_id"`, 1)),
		http.StatusBadRequest, "INVALID_REQUEST")
	recorded := f.do("creator", http.MethodPost, "/v1/external-references", "", reference)
	ref := f.ok(recorded, http.StatusCreated, "externalReference")
	refID := ref["external_reference_id"].(string)
	if ref["status"] != "unverified" || ref["source_authority"] != "manual-import" || recorded.Header().Get("Location") != "/v1/external-references/"+refID {
		t.Fatalf("a registered reference starts unverified, from manual import: %v", ref)
	}
	f.refused(f.do("creator", http.MethodPost, "/v1/external-references", "", reference), http.StatusConflict, "EXTERNAL_REFERENCE_EXISTS")
	query := "/v1/external-references?system_namespace=medusa&engine_id=baobab-trade&native_entity_type=customer&native_id=" + native +
		"&engine_instance_id=" + f.instance + "&environment=production"
	if list := f.ok(f.do("creator", http.MethodGet, query, "", ""), http.StatusOK, "externalReferenceList"); len(list["items"].([]any)) != 1 {
		t.Fatalf("lookup by native identity: %v", list)
	}
	if list := f.ok(f.do("creator", http.MethodGet, strings.Replace(query, native, "cus_absent", 1), "", ""), http.StatusOK, "externalReferenceList"); len(list["items"].([]any)) != 0 {
		t.Fatalf("an unknown native identity finds nothing: %v", list)
	}
	f.ok(f.do("creator", http.MethodGet, "/v1/external-references/"+refID, "", ""), http.StatusOK, "externalReference")
	f.refused(f.do("creator", http.MethodGet, "/v1/external-references/ref_absent01", "", ""), http.StatusNotFound, "EXTERNAL_REFERENCE_NOT_FOUND")

	// Proposing a mapping.
	f.refused(f.do("creator", http.MethodPost, "/v1/mappings", "", strings.Replace(f.proposal(f.entity, refID), `"tenant_id"`, `"status":"ACTIVE","tenant_id"`, 1)),
		http.StatusBadRequest, "INVALID_REQUEST")
	f.refused(f.do("creator", http.MethodPost, "/v1/mappings", "", f.proposal(f.foreign, refID)), http.StatusUnprocessableEntity, "CROSS_TENANT_MAPPING")
	f.refused(f.do("creator", http.MethodPost, "/v1/mappings", "", f.proposal(f.entity, "ref_absent01")), http.StatusNotFound, "MAPPING_SUBJECT_NOT_FOUND")
	proposed := f.do("creator", http.MethodPost, "/v1/mappings", "", f.proposal(f.entity, refID))
	mapping := f.ok(proposed, http.StatusCreated, "mapping")
	id := mapping["mapping_id"].(string)
	if mapping["status"] != "DRAFT" || mapping["revision"] != float64(1) || proposed.Header().Get("ETag") != `"1"` {
		t.Fatalf("a proposal starts DRAFT at revision 1: %v", mapping)
	}
	path := "/v1/mappings/" + id

	// Changing it while DRAFT, under If-Match.
	f.refused(f.do("creator", http.MethodPatch, path, "", `{"resolution_priority":10}`), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED")
	f.refused(f.do("creator", http.MethodPatch, path, `"9"`, `{"resolution_priority":10}`), http.StatusPreconditionFailed, "MAPPING_REVISION_MISMATCH")
	f.refused(f.do("creator", http.MethodPatch, path, `"1"`, `{"tenant_id":"`+f.tenant+`"}`), http.StatusBadRequest, "INVALID_REQUEST")
	if changed := f.ok(f.do("creator", http.MethodPatch, path, `"1"`, `{"resolution_priority":10}`), http.StatusOK, "mapping"); changed["revision"] != float64(2) {
		t.Fatalf("a change bumps the revision: %v", changed)
	}

	// Governance: DRAFT -> VALIDATED -> ACTIVE, four eyes.
	f.refused(f.do("approver", http.MethodPost, path+"/activate", `"2"`, ""), http.StatusConflict, "MAPPING_LIFECYCLE_CONFLICT")
	f.ok(f.do("creator", http.MethodPost, path+"/validate", `"2"`, ""), http.StatusOK, "mapping")
	f.refused(f.do("creator", http.MethodPost, path+"/activate", `"3"`, ""), http.StatusForbidden, "MAPPING_SELF_APPROVAL")
	creatorApprover := f.do("approver", http.MethodPost, path+"/validate", `"3"`, "")
	f.refused(creatorApprover, http.StatusConflict, "MAPPING_LIFECYCLE_CONFLICT")
	active := f.ok(f.do("approver", http.MethodPost, path+"/activate", `"3"`, ""), http.StatusOK, "mapping")
	if active["status"] != "ACTIVE" || active["approved_by"] == active["created_by"] {
		t.Fatalf("activation is approved by someone other than the creator: %v", active)
	}
	f.refused(f.do("creator", http.MethodPatch, path, `"4"`, `{"resolution_priority":1}`), http.StatusConflict, "MAPPING_NOT_DRAFT")

	// Reading: administrators, and workloads within their tenant only.
	if got := f.do("creator", http.MethodGet, path, "", ""); got.Header().Get("ETag") != `"4"` {
		t.Fatalf("read ETag %q", got.Header().Get("ETag"))
	}
	f.ok(f.do("workload", http.MethodGet, path, "", ""), http.StatusOK, "mapping")
	f.refused(f.do("other-workload", http.MethodGet, path, "", ""), http.StatusNotFound, "MAPPING_NOT_FOUND")

	// Resolving the native object.
	resolution := `{"tenant_id":"` + f.tenant + `","system_namespace":"medusa","engine_id":"baobab-trade","native_entity_type":"customer","native_id":"` + native + `"}`
	resolved := f.ok(f.do("workload", http.MethodPost, "/v1/resolution/external-references", "", resolution), http.StatusOK, "externalReferenceResolutionResponse")
	if resolved["canonical_entity_id"] != f.entity || resolved["mapping_id"] != id {
		t.Fatalf("resolution: %v", resolved)
	}
	f.ok(f.do("creator", http.MethodPost, "/v1/resolution/external-references", "", resolution), http.StatusOK, "externalReferenceResolutionResponse")
	f.refused(f.do("other-workload", http.MethodPost, "/v1/resolution/external-references", "", resolution), http.StatusNotFound, "MAPPING_NOT_FOUND")
	f.refused(f.do("workload", http.MethodPost, "/v1/resolution/external-references", "", strings.Replace(resolution, native, "cus_absent", 1)),
		http.StatusNotFound, "MAPPING_NOT_FOUND")

	// An identical mapping cannot be ACTIVE twice; an equally authoritative
	// mapping to another entity makes resolution ambiguous, not a guess.
	duplicate := f.ok(f.do("creator", http.MethodPost, "/v1/mappings", "", f.proposal(f.entity, refID)), http.StatusCreated, "mapping")["mapping_id"].(string)
	f.ok(f.do("creator", http.MethodPost, "/v1/mappings/"+duplicate+"/validate", `"1"`, ""), http.StatusOK, "mapping")
	f.refused(f.do("approver", http.MethodPost, "/v1/mappings/"+duplicate+"/activate", `"2"`, ""), http.StatusConflict, "MAPPING_OVERLAP")
	rival := f.activate(strings.Replace(f.proposal(f.sibling, refID), `"confidence"`, `"resolution_priority":10,"confidence"`, 1))
	f.refused(f.do("workload", http.MethodPost, "/v1/resolution/external-references", "", resolution), http.StatusConflict, "MAPPING_AMBIGUOUS")

	// Retiring one leaves the other; a retirement needs a reason.
	f.refused(f.do("creator", http.MethodPost, "/v1/mappings/"+rival+"/retire", `"3"`, `{}`), http.StatusBadRequest, "VALIDATION_FAILED")
	f.refused(f.do("creator", http.MethodPost, "/v1/mappings/"+rival+"/retire", `"3"`, `{"reason":"duplicate","retired_by":"x"}`), http.StatusBadRequest, "INVALID_REQUEST")
	retired := f.ok(f.do("creator", http.MethodPost, "/v1/mappings/"+rival+"/retire", `"3"`, `{"reason":"duplicate of `+id+`"}`), http.StatusOK, "mapping")
	if retired["status"] != "RETIRED" {
		t.Fatalf("retire: %v", retired)
	}
	if again := f.ok(f.do("workload", http.MethodPost, "/v1/resolution/external-references", "", resolution), http.StatusOK, "externalReferenceResolutionResponse"); again["mapping_id"] != id {
		t.Fatalf("after retirement the remaining mapping resolves: %v", again)
	}

	var audited int
	if err := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE target = $1 AND action LIKE 'mapping.%'`, "mapping/"+id).Scan(&audited); err != nil || audited != 4 {
		t.Fatalf("proposal, change, validation and activation are each audited: %d %v", audited, err)
	}
}

// TestLegacyExternalReferencesAreFrozenAndReported: the legacy table takes no
// writes, and each of its rows is reported with a disposition rather than
// converted by guesswork (migration 000059).
func TestLegacyExternalReferencesAreFrozenAndReported(t *testing.T) {
	f := newMappingFixture(t)
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO registry.external_reference (canonical_entity_id, provider, provider_key)
		VALUES ($1::uuid, 'baobab-trade', 'customer:c1')`, f.entity); err == nil {
		t.Fatal("the legacy external reference table must refuse writes")
	}
	// Rows that predate the freeze.
	tx, err := f.admin.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx) //nolint:errcheck
	if _, err := tx.Exec(f.ctx, `ALTER TABLE registry.external_reference DISABLE TRIGGER legacy_external_reference_frozen`); err != nil {
		t.Fatal(err)
	}
	insert := func(provider, key string) string {
		var id string
		if err := tx.QueryRow(f.ctx, `INSERT INTO registry.external_reference (canonical_entity_id, provider, provider_key)
			VALUES ($1::uuid, $2, $3) RETURNING external_reference_id::text`, f.entity, provider, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	iam := insert("baobab-iam", "keycloak_organization:kc-"+f.tenant[3:15])
	engine := insert("baobab-trade", "customer:"+f.tenant[3:15])
	if _, err := tx.Exec(f.ctx, `ALTER TABLE registry.external_reference ENABLE TRIGGER legacy_external_reference_frozen`); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{iam: "IAM_ORGANISATION_UNLINKED", engine: "SYSTEM_NAMESPACE_UNKNOWN"} {
		var disposition string
		if err := tx.QueryRow(f.ctx, `SELECT disposition FROM mapping.legacy_external_reference_report WHERE legacy_row_id = $1::uuid`, id).Scan(&disposition); err != nil || disposition != want {
			t.Fatalf("legacy row %s reported %q, want %q (%v)", id, disposition, want, err)
		}
	}
}

// TestMappingAdministrationReviewFixes covers the review of #185: a mapping
// proposal is idempotent by key, If-Match must be a strong entity tag, a
// retirement's successor must supersede the retired mapping, and a retired
// mapping still resolves at times before its retirement.
func TestMappingAdministrationReviewFixes(t *testing.T) {
	f := newMappingFixture(t)
	native := "cus_" + f.tenant[3:15] + "r"
	reference := `{"system_namespace":"medusa","engine_id":"baobab-trade","engine_instance_id":"` + f.instance +
		`","environment":"production","native_entity_type":"customer","native_id":"` + native + `"}`
	refID := f.ok(f.do("creator", http.MethodPost, "/v1/external-references", "", reference), http.StatusCreated, "externalReference")["external_reference_id"].(string)
	proposal := f.proposal(f.entity, refID)

	// Idempotency-Key is required; a replay returns the same mapping; the
	// same key with another body is refused.
	f.refused(f.doKeyed("creator", http.MethodPost, "/v1/mappings", "", "", proposal), http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY")
	f.refused(f.doKeyed("creator", http.MethodPost, "/v1/mappings", "", "short", proposal), http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY")
	key := "replay-" + domain.NewUUIDv7()
	first := f.ok(f.doKeyed("creator", http.MethodPost, "/v1/mappings", "", key, proposal), http.StatusCreated, "mapping")
	again := f.ok(f.doKeyed("creator", http.MethodPost, "/v1/mappings", "", key, proposal), http.StatusCreated, "mapping")
	if first["mapping_id"] != again["mapping_id"] {
		t.Fatalf("a replay created a second mapping: %v then %v", first["mapping_id"], again["mapping_id"])
	}
	f.refused(f.doKeyed("creator", http.MethodPost, "/v1/mappings", "", key, strings.Replace(proposal, `"COMMERCE"`, `"IDENTITY"`, 1)),
		http.StatusConflict, "IDEMPOTENCY_KEY_REUSED")
	id := first["mapping_id"].(string)
	path := "/v1/mappings/" + id

	// If-Match is a strong entity tag, nothing looser.
	for _, malformed := range []string{`1`, `"1`, `1"`, `W/"1"`, `"01"`} {
		f.refused(f.do("creator", http.MethodPost, path+"/validate", malformed, ""), http.StatusBadRequest, "INVALID_IF_MATCH")
	}
	f.ok(f.do("creator", http.MethodPost, path+"/validate", `"1"`, ""), http.StatusOK, "mapping")
	activated := f.ok(f.do("approver", http.MethodPost, path+"/activate", `"2"`, ""), http.StatusOK, "mapping")
	approvedAt := activated["approved_at"].(string)

	// A successor must record the retired mapping as the one it supersedes.
	unrelated := f.ok(f.do("creator", http.MethodPost, "/v1/mappings", "", f.proposal(f.sibling, refID)), http.StatusCreated, "mapping")["mapping_id"].(string)
	f.refused(f.do("creator", http.MethodPost, path+"/retire", `"3"`, `{"reason":"replaced","successor_mapping_id":"`+unrelated+`"}`),
		http.StatusUnprocessableEntity, "MAPPING_SUCCESSOR_MISMATCH")
	successor := f.ok(f.do("creator", http.MethodPost, "/v1/mappings", "",
		strings.Replace(f.proposal(f.sibling, refID), `"confidence"`, `"supersedes_mapping_id":"`+id+`","confidence"`, 1)), http.StatusCreated, "mapping")["mapping_id"].(string)
	f.ok(f.do("creator", http.MethodPost, path+"/retire", `"3"`, `{"reason":"replaced","successor_mapping_id":"`+successor+`"}`), http.StatusOK, "mapping")
	var recorded string
	if err := f.admin.QueryRow(f.ctx, `SELECT payload->>'successor_mapping_id' FROM audit_events WHERE target = $1 AND action = 'mapping.retired'`,
		"mapping/"+id).Scan(&recorded); err != nil || recorded != successor {
		t.Fatalf("the retirement records its successor: %q %v", recorded, err)
	}

	// The retired mapping resolves at times before its retirement, not now.
	resolution := `{"tenant_id":"` + f.tenant + `","system_namespace":"medusa","engine_id":"baobab-trade","native_entity_type":"customer","native_id":"` + native + `"`
	historical := f.ok(f.do("workload", http.MethodPost, "/v1/resolution/external-references", "", resolution+`,"effective_timestamp":"`+approvedAt+`"}`),
		http.StatusOK, "externalReferenceResolutionResponse")
	if historical["mapping_id"] != id || historical["status"] != "RETIRED" {
		t.Fatalf("historical resolution of a retired mapping: %v", historical)
	}
	f.refused(f.do("workload", http.MethodPost, "/v1/resolution/external-references", "", resolution+`}`), http.StatusNotFound, "MAPPING_NOT_FOUND")
}
