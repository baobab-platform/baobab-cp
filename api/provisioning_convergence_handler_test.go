package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	productdomain "github.com/baobab-platform/baobab-cp/internal/product/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/apply"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// onboardingRequests serves fixed onboarding requests over the real
// repository: their storage is the admission workflow's, not under test here.
type onboardingRequests struct {
	*repository.PostgresRepository
	requests map[string]domain.TenantOnboardingRequest
}

func (o onboardingRequests) GetTenantOnboardingRequest(_ context.Context, id string) (domain.TenantOnboardingRequest, error) {
	request, ok := o.requests[id]
	if !ok {
		return domain.TenantOnboardingRequest{}, repository.ErrOnboardingRequestNotFound
	}
	return request, nil
}

// convergenceFixture is authoritative state a provisioning is planned
// against: an active market XQ, a product packaging one mandatory
// capability, and a provider permitted in production on the fixture's
// af-south-1 production instance. Tenant a's onboarding request resides in
// af-south-1, tenant b's in eu-west-1, where no instance runs.
type convergenceFixture struct {
	repo                                           *repository.PostgresRepository
	admin                                          *pgxpool.Pool
	tenantStore                                    *postgres.Store
	f                                              provisioningAPIFixture
	suffix, capabilityKey, engineCode, providerKey string
	productID, instanceKey                         string
	call                                           func(token, method, path, key, ifMatch, body string) *httptest.ResponseRecorder
	decode                                         func(response *httptest.ResponseRecorder, want int, definition string) map[string]any
	refused                                        func(response *httptest.ResponseRecorder, status int, code string)
}

func newConvergenceFixture(t *testing.T, name string) convergenceFixture {
	t.Helper()
	_, repo, admin, url := newProvisioningTestHandler(t)
	ctx := context.Background()
	f := seedProvisioningAPIFixture(t, ctx, admin, repo, name)
	suffix := strings.ToLower(f.suffix())
	capabilityKey := "trade.settlement" + suffix + ".execute"
	engineCode, providerKey := "zb"+name+"-"+suffix, "zb"+name+"-"+suffix+".medusa"
	productID, compositionKey := "zb"+name+"-"+suffix, "solution.zb"+name+"-"+suffix
	instanceKey, _ := domain.FormatResourceID("ei", f.InstanceID)

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE tenant_id IN ($1, $2)`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.tenant_manifest WHERE tenant_id IN ($1, $2)`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.plan_approval WHERE tenant_provisioning_id IN (SELECT tenant_provisioning_id FROM provisioning.tenant_provisioning WHERE tenant_id IN ($1, $2))`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.plan WHERE tenant_provisioning_id IN (SELECT tenant_provisioning_id FROM provisioning.tenant_provisioning WHERE tenant_id IN ($1, $2))`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM provisioning.desired_state WHERE tenant_provisioning_id IN (SELECT tenant_provisioning_id FROM provisioning.tenant_provisioning WHERE tenant_id IN ($1, $2))`, f.TenantID, f.OtherTenantID)
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE engine_instance_id = $1::uuid`, f.InstanceID)
		admin.Exec(ctx, `DELETE FROM capability.capability_grant WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM market.market_participation_capability WHERE market_assignment_id IN (SELECT market_assignment_id FROM market.market_assignment WHERE tenant_id = $1)`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM market.market_assignment WHERE tenant_id = $1`, f.TenantID)
		admin.Exec(ctx, `DELETE FROM market.market WHERE code = 'XQ'`)
		admin.Exec(ctx, `DELETE FROM capability.provider_capability_support WHERE provider_id IN (SELECT provider_id FROM capability.capability_provider WHERE provider_key = $1)`, providerKey)
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key = $1`, providerKey)
		admin.Exec(ctx, `DELETE FROM capability.capability_composition_member WHERE composition_id IN (SELECT composition_id FROM capability.capability_composition WHERE composition_key = $1)`, compositionKey)
		admin.Exec(ctx, `DELETE FROM capability.capability_composition WHERE composition_key = $1`, compositionKey)
		admin.Exec(ctx, `DELETE FROM product.product_version WHERE product_id = $1`, productID)
		admin.Exec(ctx, `DELETE FROM product.product WHERE product_id = $1`, productID)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
	}
	cleanup()
	t.Cleanup(cleanup)

	// Authoritative state: an active market, a product packaging one
	// mandatory capability, and a provider permitted in production running
	// on the fixture's af-south-1 production instance.
	mustNoError(t, repo.CreateMarket(ctx, domain.Market{ID: domain.NewUUIDv7(), Code: "XQ", Name: "Private use", Currency: "ZAR", Region: "af-south-1", IsActive: true}))
	capabilityID := domain.NewUUIDv7()
	mustNoError(t, repo.CreateCapability(ctx, capabilitydomain.Capability{ID: capabilityID, Key: capabilityKey, Name: "Settlement", DomainKey: "trade",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}))
	mustNoError(t, repo.CreateComposition(ctx, capabilitydomain.CapabilityComposition{CompositionKey: compositionKey, Name: "Fixture",
		CompositionType: capabilitydomain.CompositionTypeProduct, Version: "1.0.0", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Members: []capabilitydomain.CompositionMember{{CapabilityKey: capabilityKey, Criticality: capabilitydomain.MembershipCriticalityMandatory}}}))
	mustNoError(t, repo.CreateProduct(ctx, productdomain.Product{ID: productID, Name: "Fixture", Status: productdomain.ProductLifecycleActive}))
	released := time.Now().UTC()
	mustNoError(t, repo.CreateProductVersion(ctx, productdomain.ProductVersion{ProductID: productID, Version: "1.0.0",
		CompositionKey: compositionKey, Status: productdomain.ProductLifecycleActive, ReleasedAt: &released}))
	if _, err := admin.Exec(ctx, `UPDATE topology.engine SET code = $2 WHERE engine_id = $1::uuid`, f.EngineID, engineCode); err != nil {
		t.Fatal(err)
	}
	var providerID string
	if err := admin.QueryRow(ctx, `INSERT INTO capability.capability_provider (provider_key, name, provider_type, engine_id, status, metadata)
		VALUES ($1, 'Fixture', 'BAOBAB_ENGINE', $2::uuid, 'ACTIVE', '{"production_permitted": true}') RETURNING provider_id::text`,
		providerKey, f.EngineID).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO capability.provider_capability_support (provider_id, capability_id, contract_versions)
		VALUES ($1::uuid, $2::uuid, '{1}')`, providerID, capabilityID); err != nil {
		t.Fatal(err)
	}

	// Two registered principals: the requester, and the approver who is not.
	identities := repository.NewInMemoryRepository()
	principal := func(subject string, scopes ...string) auth.Principal {
		p := auth.Principal{Subject: subject, ActorType: "human", TokenID: "t-" + subject, Issuer: testRealm,
			Scopes: map[string]struct{}{}, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
		for _, s := range scopes {
			p.Scopes[s] = struct{}{}
		}
		registered := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, registered))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: registered.ID, Issuer: p.Issuer, Subject: p.Subject, Status: "ACTIVE"}))
		return p
	}
	requester := principal("requester-"+suffix, "tenant:write", "tenant:read", "provisioning:approve", "operation:read", "operation:control")
	approver := principal("approver-"+suffix, "tenant:read", "tenant:write", "provisioning:approve", "operation:read", "operation:control")
	authorised := time.Now().UTC()
	fulfilled := func(id, tenantID, residency string) domain.TenantOnboardingRequest {
		return domain.TenantOnboardingRequest{ID: id, AdmissionDecisionID: "adm_0199a1b2c3d47e8f", Status: domain.OnboardingFulfilled,
			AuthorisedBy: "prn_authoriser", AuthorisedAt: &authorised, TenantID: tenantID,
			DesiredState: domain.OnboardingDesiredState{DisplayName: "Converge Fixture", ResidencyRegion: residency,
				IsolationStrategy: "row_level_security", MarketScope: []string{"XQ"}, ProductRequirements: []string{productID},
				MarketParticipation: []domain.OnboardingMarketParticipation{{Market: "XQ", Activities: []string{"LEGAL_PRESENCE", "SELLING"}}}}}
	}
	store := onboardingRequests{PostgresRepository: repo, requests: map[string]domain.TenantOnboardingRequest{
		"tor_" + suffix + "a": fulfilled("tor_"+suffix+"a", f.TenantID, "af-south-1"),
		"tor_" + suffix + "b": fulfilled("tor_"+suffix+"b", f.OtherTenantID, "eu-west-1"),
	}}
	tenantStore, err := postgres.Open(ctx, url)
	mustNoError(t, err)
	t.Cleanup(func() { tenantStore.Close() })
	handler := New(Dependencies{Store: tenantStore, AdminVerifier: tokenVerifier{"requester": requester, "approver": approver},
		Identities: identities, Provisioning: store, Operations: repo})

	call := func(token, method, path, key, ifMatch, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		if ifMatch != "" {
			request.Header.Set("If-Match", ifMatch)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	decode := func(response *httptest.ResponseRecorder, want int, definition string) map[string]any {
		t.Helper()
		if response.Code != want {
			t.Fatalf("got %d, want %d: %s", response.Code, want, response.Body.String())
		}
		var body map[string]any
		mustNoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		if dir := os.Getenv("SHARED_CONTRACTS_DIR"); dir != "" && definition != "" {
			contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "control-plane/v1/"+definition), body)
		}
		return body
	}
	refused := func(response *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
			t.Fatalf("want %d %s, got %d: %s", status, code, response.Code, response.Body.String())
		}
	}
	return convergenceFixture{repo: repo, admin: admin, tenantStore: tenantStore, f: f, suffix: suffix,
		capabilityKey: capabilityKey, engineCode: engineCode, providerKey: providerKey, productID: productID,
		instanceKey: instanceKey, call: call, decode: decode, refused: refused}
}

// executor runs the fixture's operations.
func (x convergenceFixture) executor() apply.Executor {
	return apply.Executor{Store: x.repo, Registry: x.repo, Planner: convergence.Planner{Registry: x.repo},
		Pipeline: apply.StandardPipeline(provisioning.ZB02Dependencies{Tenants: x.tenantStore, Repo: x.repo, Provisioning: x.repo})}
}

// TestProvisioningIsPlannedApprovedAndApplied drives ADR-SHARED-015 end to
// end: plan from an authorised onboarding request, approve the plan's
// digest as another principal, apply it as an operation, execute it.
func TestProvisioningIsPlannedApprovedAndApplied(t *testing.T) {
	x := newConvergenceFixture(t, "converge")
	ctx := context.Background()
	repo, f, suffix := x.repo, x.f, x.suffix
	capabilityKey, engineCode, providerKey, instanceKey := x.capabilityKey, x.engineCode, x.providerKey, x.instanceKey
	call, decode, refused := x.call, x.decode, x.refused

	base := "/v1/tenants/" + f.TenantID + "/provisioning"
	create := `{"tenant_onboarding_request_id":"tor_` + suffix + `a"}`
	created := call("requester", http.MethodPost, base, "create-key-"+suffix, "", create)
	tp := decode(created, http.StatusCreated, "tenant-provisioning.schema.json#/$defs/TenantProvisioning")
	id := tp["tenant_provisioning_id"].(string)
	if tp["state"] != "PLANNED" || tp["legacy_state"] != "accepted" || created.Header().Get("ETag") != `"1"` ||
		created.Header().Get("Location") != base+"/"+id || tp["current_plan"].(map[string]any)["stale"] != false {
		t.Fatalf("created: %v", tp)
	}
	// Idempotent: the same request is the same provisioning; a different one under the key is refused.
	if again := decode(call("requester", http.MethodPost, base, "create-key-"+suffix, "", create), http.StatusCreated, ""); again["tenant_provisioning_id"] != id {
		t.Fatalf("replay created %v", again["tenant_provisioning_id"])
	}
	refused(call("requester", http.MethodPost, base, "create-key-"+suffix, "", `{"tenant_onboarding_request_id":"tor_`+suffix+`a","desired_state_version":1}`), http.StatusConflict, "IDEMPOTENCY_KEY_REUSED")
	refused(call("requester", http.MethodPost, base, "create-key2-"+suffix, "", create), http.StatusConflict, "PROVISIONING_IN_PROGRESS")
	refused(call("requester", http.MethodPost, base, "create-key3-"+suffix, "", `{"manifest":{}}`), http.StatusBadRequest, "INVALID_REQUEST")

	planResponse := call("requester", http.MethodGet, base+"/"+id+"/plan", "", "", "")
	plan := decode(planResponse, http.StatusOK, "provisioning-plan.schema.json#/$defs/ProvisioningPlan")
	digest := plan["plan_digest"].(string)
	if planResponse.Header().Get("ETag") != `"`+digest+`"` || plan["risk_class"] != "HIGH" {
		t.Fatalf("plan: %v", plan)
	}
	var bound bool
	for _, s := range plan["steps"].([]any) {
		r := s.(map[string]any)["resources"].(map[string]any)
		bound = bound || (r["engine_instance_id"] == instanceKey && r["engine_id"] == engineCode && r["provider_key"] == providerKey)
	}
	if !bound {
		t.Fatalf("the plan does not bind %s on %s: %v", capabilityKey, instanceKey, plan["steps"])
	}

	decision := func(d, reason string) string {
		body := `{"plan_id":"` + plan["plan_id"].(string) + `","plan_version":1,"plan_digest":"` + digest + `","decision":"` + d + `"`
		if reason != "" {
			body += `,"reason":"` + reason + `"`
		}
		return body + "}"
	}
	refused(call("requester", http.MethodPost, base+"/"+id+"/apply", "apply-key-"+suffix, `"1"`, ""), http.StatusConflict, "PLAN_NOT_APPROVED")
	refused(call("requester", http.MethodPost, base+"/"+id+"/approve", "", `"1"`, decision("APPROVED", "")), http.StatusForbidden, "PROVISIONING_SELF_APPROVAL")
	refused(call("approver", http.MethodPost, base+"/"+id+"/approve", "", "", decision("APPROVED", "")), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED")
	refused(call("approver", http.MethodPost, base+"/"+id+"/approve", "", `"1"`, strings.Replace(decision("APPROVED", ""), digest, "sha256:"+strings.Repeat("0", 64), 1)), http.StatusConflict, "PLAN_DIGEST_MISMATCH")
	refused(call("approver", http.MethodPost, base+"/"+id+"/approve", "", `"1"`, decision("REJECTED", "")), http.StatusBadRequest, "INVALID_REQUEST")
	refused(call("approver", http.MethodPost, base+"/"+id+"/approve", "", `"9"`, decision("APPROVED", "")), http.StatusPreconditionFailed, "PROVISIONING_REVISION_MISMATCH")
	approval := decode(call("approver", http.MethodPost, base+"/"+id+"/approve", "", `"1"`, decision("APPROVED", "")), http.StatusOK,
		"approval-decision.schema.json#/$defs/ApprovalDecision")
	if approval["plan_digest"] != digest || approval["subject_id"] != id {
		t.Fatalf("approval: %v", approval)
	}
	refused(call("approver", http.MethodPost, base+"/"+id+"/approve", "", `"2"`, decision("REJECTED", "Not now")), http.StatusConflict, "PLAN_ALREADY_DECIDED")

	accepted := call("requester", http.MethodPost, base+"/"+id+"/apply", "apply-key-"+suffix, `"2"`, "")
	op := decode(accepted, http.StatusAccepted, "execution-operation.schema.json#/$defs/ExecutionOperation")
	opID := op["operation_id"].(string)
	if op["status"] != "QUEUED" || op["plan_digest"] != digest || accepted.Header().Get("Location") != "/v1/admin/operations/"+opID {
		t.Fatalf("accepted: %v", op)
	}
	if replay := decode(call("requester", http.MethodPost, base+"/"+id+"/apply", "apply-key-"+suffix, `"2"`, ""), http.StatusAccepted, ""); replay["operation_id"] != opID {
		t.Fatalf("apply replay: %v", replay)
	}
	refused(call("requester", http.MethodPost, base+"/"+id+"/apply", "apply-key2-"+suffix, `"3"`, ""), http.StatusConflict, "OPERATION_IN_PROGRESS")

	executor := x.executor()
	if ran, err := executor.RunOnce(ctx); !ran || err != nil {
		t.Fatalf("executor: ran %v, %v", ran, err)
	}
	done, err := repo.GetOperation(ctx, opID)
	mustNoError(t, err)
	var result map[string]string
	if done.Status != "SUCCEEDED" || json.Unmarshal(done.Result, &result) != nil || result["resource_state"] != "ACTIVE" || result["resource_id"] != id {
		t.Fatalf("operation: %s %s %s", done.Status, done.Result, done.Problem)
	}
	active := decode(call("requester", http.MethodGet, base+"/"+id, "", "", ""), http.StatusOK, "tenant-provisioning.schema.json#/$defs/TenantProvisioning")
	if active["state"] != "ACTIVE" || active["operation_id"] != opID || active["approval"].(map[string]any)["decision"] != "APPROVED" {
		t.Fatalf("applied: %v", active)
	}
	refused(call("requester", http.MethodPost, base+"/"+id+"/apply", "apply-key3-"+suffix, `"`+jsonNumber(active["revision"])+`"`, ""), http.StatusConflict, "PROVISIONING_STATE_CONFLICT")
	if page := decode(call("requester", http.MethodGet, base, "", "", ""), http.StatusOK, "tenant-provisioning.schema.json#/$defs/TenantProvisioningPage"); len(page["items"].([]any)) != 1 {
		t.Fatalf("list: %v", page)
	}
	// Readiness and drift evidence of the applied provisioning, by its
	// public id: the verdict with the evaluation it derives from.
	readiness := decode(call("requester", http.MethodGet, base+"/"+id+"/readiness", "", "", ""), http.StatusOK,
		"tenant-provisioning.schema.json#/$defs/ProvisioningReadiness")
	if snapshots := readiness["snapshots"].([]any); readiness["status"] != "READY" || readiness["tenant_provisioning_id"] != id ||
		len(snapshots) != 1 || snapshots[0].(map[string]any)["level"] != "TENANT" || active["readiness_status"] != "READY" {
		t.Fatalf("readiness: %v (provisioning readiness_status %v)", readiness, active["readiness_status"])
	}
	if drift := decode(call("requester", http.MethodGet, base+"/"+id+"/drift", "", "", ""), http.StatusOK,
		"tenant-provisioning.schema.json#/$defs/ProvisioningDrift"); drift["tenant_provisioning_id"] != id || drift["observed_at"] == nil {
		t.Fatalf("drift: %v", drift)
	}
	refused(call("requester", http.MethodGet, "/v1/tenants/"+f.OtherTenantID+"/provisioning/"+id+"/readiness", "", "", ""), http.StatusNotFound, "PROVISIONING_NOT_FOUND")
	refused(call("requester", http.MethodGet, base+"/not-a-provisioning/drift", "", "", ""), http.StatusNotFound, "PROVISIONING_NOT_FOUND")
	refused(call("requester", http.MethodGet, "/v1/tenants/"+f.OtherTenantID+"/provisioning/"+id, "", "", ""), http.StatusNotFound, "PROVISIONING_NOT_FOUND")

	// No provider instance runs where the other tenant must reside: planned
	// BLOCKED, with the reason, and the plan cannot be approved.
	otherBase := "/v1/tenants/" + f.OtherTenantID + "/provisioning"
	blocked := decode(call("requester", http.MethodPost, otherBase, "create-key-other-"+suffix, "", `{"tenant_onboarding_request_id":"tor_`+suffix+`b"}`),
		http.StatusCreated, "tenant-provisioning.schema.json#/$defs/TenantProvisioning")
	reasons := blocked["blocking_reasons"].([]any)
	if blocked["state"] != "BLOCKED" || len(reasons) != 1 || reasons[0].(map[string]any)["code"] != "NO_RESIDENCY_COMPLIANT_PROVIDER" ||
		reasons[0].(map[string]any)["capability_key"] != capabilityKey {
		t.Fatalf("blocked: %v", blocked)
	}
	otherPlan := decode(call("requester", http.MethodGet, otherBase+"/"+blocked["tenant_provisioning_id"].(string)+"/plan", "", "", ""), http.StatusOK, "")
	refused(call("approver", http.MethodPost, otherBase+"/"+blocked["tenant_provisioning_id"].(string)+"/approve", "", `"1"`,
		`{"plan_id":"`+otherPlan["plan_id"].(string)+`","plan_version":1,"plan_digest":"`+otherPlan["plan_digest"].(string)+`","decision":"APPROVED"}`),
		http.StatusConflict, "PLAN_BLOCKED")
	// Never evaluated: readiness is UNKNOWN and no drift is observed.
	otherPath := otherBase + "/" + blocked["tenant_provisioning_id"].(string)
	if unknown := decode(call("requester", http.MethodGet, otherPath+"/readiness", "", "", ""), http.StatusOK,
		"tenant-provisioning.schema.json#/$defs/ProvisioningReadiness"); unknown["status"] != "UNKNOWN" || len(unknown["snapshots"].([]any)) != 0 {
		t.Fatalf("unevaluated readiness: %v", unknown)
	}
	if none := decode(call("requester", http.MethodGet, otherPath+"/drift", "", "", ""), http.StatusOK,
		"tenant-provisioning.schema.json#/$defs/ProvisioningDrift"); none["observed_at"] != nil || len(none["items"].([]any)) != 0 {
		t.Fatalf("unobserved drift: %v", none)
	}
}

func jsonNumber(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// TestProvisioningLifecycleCommands drives the lifecycle commands: an apply
// cancelled before it ran leaves the provisioning BLOCKED; remediation within
// the approved plan runs as an operation; a retryable operation is retried
// once per key; a plan with blockers is replanned when authoritative state
// changes, idempotently; and a provisioning not executing is withdrawn.
func TestProvisioningLifecycleCommands(t *testing.T) {
	x := newConvergenceFixture(t, "lifecycle")
	ctx := context.Background()
	call, decode, refused, suffix := x.call, x.decode, x.refused, x.suffix
	const provisioningSchema = "tenant-provisioning.schema.json#/$defs/TenantProvisioning"
	const operationSchema = "execution-operation.schema.json#/$defs/ExecutionOperation"
	revision := func(tp map[string]any) string { return `"` + jsonNumber(tp["revision"]) + `"` }
	reason := func(r string) string { return `{"reason":"` + r + `"}` }
	get := func(base, id string) map[string]any {
		return decode(call("requester", http.MethodGet, base+"/"+id, "", "", ""), http.StatusOK, provisioningSchema)
	}

	// Tenant a: planned, approved and applied, then cancelled before it ran.
	base := "/v1/tenants/" + x.f.TenantID + "/provisioning"
	tp := decode(call("requester", http.MethodPost, base, "create-a-"+suffix, "", `{"tenant_onboarding_request_id":"tor_`+suffix+`a"}`),
		http.StatusCreated, provisioningSchema)
	id := tp["tenant_provisioning_id"].(string)
	plan := decode(call("requester", http.MethodGet, base+"/"+id+"/plan", "", "", ""), http.StatusOK, "")
	decode(call("approver", http.MethodPost, base+"/"+id+"/approve", "", `"1"`, `{"plan_id":"`+plan["plan_id"].(string)+
		`","plan_version":1,"plan_digest":"`+plan["plan_digest"].(string)+`","decision":"APPROVED"}`), http.StatusOK, "")
	applied := decode(call("requester", http.MethodPost, base+"/"+id+"/apply", "apply-a-"+suffix, `"2"`, ""), http.StatusAccepted, operationSchema)
	ops := "/v1/admin/operations/" + applied["operation_id"].(string)

	// Abandoning or replanning an executing provisioning waits for its operation.
	refused(call("requester", http.MethodPost, base+"/"+id+"/withdraw", "", `"3"`, reason("not now")), http.StatusConflict, "OPERATION_IN_PROGRESS")
	cancelled := decode(call("requester", http.MethodPost, ops+"/cancel", "", "", reason("wrong window")), http.StatusAccepted, operationSchema)
	if cancelled["status"] != "CANCELLED" {
		t.Fatalf("a queued operation is cancelled at once: %v", cancelled)
	}
	refused(call("requester", http.MethodPost, ops+"/cancel", "", "", reason("again")), http.StatusConflict, "OPERATION_NOT_CANCELLABLE")
	refused(call("requester", http.MethodPost, ops+"/cancel", "", "", `{}`), http.StatusBadRequest, "INVALID_REQUEST")
	blocked := get(base, id)
	if blocked["state"] != "BLOCKED" || blocked["blocking_reasons"].([]any)[0].(map[string]any)["code"] != "EXECUTION_CANCELLED" {
		t.Fatalf("a cancelled apply leaves the provisioning BLOCKED: %v", blocked)
	}

	// Remediation runs the approved plan as an operation; the provisioning is
	// REMEDIATING, and nothing else runs until it ends.
	remediation := decode(call("requester", http.MethodPost, base+"/"+id+"/remediate", "remediate-a-"+suffix, revision(blocked), reason("window open")),
		http.StatusAccepted, operationSchema)
	if remediation["operation_type"] != "TENANT_PROVISIONING_REMEDIATE" || remediation["status"] != "QUEUED" {
		t.Fatalf("remediation: %v", remediation)
	}
	if again := decode(call("requester", http.MethodPost, base+"/"+id+"/remediate", "remediate-a-"+suffix, revision(blocked), reason("window open")),
		http.StatusAccepted, ""); again["operation_id"] != remediation["operation_id"] {
		t.Fatalf("a remediation replay created another operation: %v", again)
	}
	remediating := get(base, id)
	if remediating["state"] != "REMEDIATING" {
		t.Fatalf("remediation accepted: %v", remediating)
	}
	refused(call("requester", http.MethodPost, base+"/"+id+"/plan", "replan-a-"+suffix, revision(remediating), reason("change")), http.StatusConflict, "PROVISIONING_STATE_CONFLICT")

	// Retry: only a retryable FAILED or BLOCKED operation, once per key. The
	// executor's failure is set here directly.
	remediationOps := "/v1/admin/operations/" + remediation["operation_id"].(string)
	refused(call("requester", http.MethodPost, remediationOps+"/retry", "retry-a-"+suffix, "", reason("again")), http.StatusConflict, "OPERATION_NOT_RETRYABLE")
	if _, err := x.admin.Exec(ctx, `UPDATE operations.execution_operation SET status = 'FAILED', retryable = true, completed_at = now(),
		problem = '{"type":"https://docs.nabhold.com/problems/provisioning_unavailable","title":"Provisioning operation failed","status":503,"code":"PROVISIONING_UNAVAILABLE","correlation_id":"c","retryable":true}'
		WHERE operation_id = $1`, remediation["operation_id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := x.admin.Exec(ctx, `UPDATE provisioning.tenant_provisioning SET state = 'BLOCKED' WHERE tenant_provisioning_key = $1`, id); err != nil {
		t.Fatal(err)
	}
	retried := decode(call("requester", http.MethodPost, remediationOps+"/retry", "retry-a-"+suffix, "", reason("provider back")),
		http.StatusAccepted, operationSchema)
	if retried["status"] != "QUEUED" || retried["execution_attempt"] != float64(2) {
		t.Fatalf("retry: %v", retried)
	}
	if replay := decode(call("requester", http.MethodPost, remediationOps+"/retry", "retry-a-"+suffix, "", reason("provider back")),
		http.StatusAccepted, ""); replay["execution_attempt"] != float64(2) {
		t.Fatalf("a retry replay retried again: %v", replay)
	}
	refused(call("requester", http.MethodPost, remediationOps+"/retry", "retry-a2-"+suffix, "", reason("again")), http.StatusConflict, "OPERATION_NOT_RETRYABLE")
	// The retried attempt fails retryably; a late replay of its key does not
	// queue another attempt, a new key does.
	if _, err := x.admin.Exec(ctx, `UPDATE operations.execution_operation SET status = 'FAILED', retryable = true, completed_at = now(),
		problem = '{"type":"https://docs.nabhold.com/problems/provisioning_unavailable","title":"Provisioning operation failed","status":503,"code":"PROVISIONING_UNAVAILABLE","correlation_id":"c","retryable":true}'
		WHERE operation_id = $1`, remediation["operation_id"]); err != nil {
		t.Fatal(err)
	}
	if replay := decode(call("requester", http.MethodPost, remediationOps+"/retry", "retry-a-"+suffix, "", reason("provider back")),
		http.StatusAccepted, ""); replay["execution_attempt"] != float64(2) || replay["status"] != "FAILED" {
		t.Fatalf("a late retry replay retried again: %v", replay)
	}
	if again := decode(call("requester", http.MethodPost, remediationOps+"/retry", "retry-a3-"+suffix, "", reason("provider back again")),
		http.StatusAccepted, operationSchema); again["execution_attempt"] != float64(3) {
		t.Fatalf("second retry: %v", again)
	}

	if ran, err := x.executor().RunOnce(ctx); !ran || err != nil {
		t.Fatalf("executor: %v %v", ran, err)
	}
	done := decode(call("requester", http.MethodGet, remediationOps, "", "", ""), http.StatusOK, operationSchema)
	if done["status"] != "SUCCEEDED" || get(base, id)["state"] != "ACTIVE" {
		t.Fatalf("remediation did not complete the provisioning: %v", done)
	}

	// Tenant b: no instance runs where it must reside, so its plan is blocked;
	// remediation cannot help, another plan can.
	otherBase := "/v1/tenants/" + x.f.OtherTenantID + "/provisioning"
	other := decode(call("requester", http.MethodPost, otherBase, "create-b-"+suffix, "", `{"tenant_onboarding_request_id":"tor_`+suffix+`b"}`),
		http.StatusCreated, provisioningSchema)
	otherID := other["tenant_provisioning_id"].(string)
	refused(call("requester", http.MethodPost, otherBase+"/"+otherID+"/remediate", "remediate-b-"+suffix, `"1"`, reason("try")), http.StatusConflict, "PLAN_CHANGE_REQUIRED")
	refused(call("requester", http.MethodPost, otherBase+"/"+otherID+"/plan", "replan-b-"+suffix, "", reason("try")), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED")

	instance := domain.NewUUIDv7()
	t.Cleanup(func() {
		x.admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = $1::uuid`, instance)
	})
	if _, err := x.admin.Exec(ctx, `INSERT INTO topology.engine_instance (engine_instance_id, engine_id, region, environment, status)
		VALUES ($1::uuid, $2::uuid, 'eu-west-1', 'production', 'ACTIVE')`, instance, x.f.EngineID); err != nil {
		t.Fatal(err)
	}
	replanned := decode(call("requester", http.MethodPost, otherBase+"/"+otherID+"/plan", "replan-b-"+suffix, `"1"`, reason("eu instance live")),
		http.StatusOK, provisioningSchema)
	if replanned["state"] != "PLANNED" || replanned["current_plan"].(map[string]any)["plan_version"] != float64(2) || replanned["approval"] != nil {
		t.Fatalf("replan: %v", replanned)
	}
	if replay := decode(call("requester", http.MethodPost, otherBase+"/"+otherID+"/plan", "replan-b-"+suffix, `"1"`, reason("eu instance live")),
		http.StatusOK, ""); replay["current_plan"].(map[string]any)["plan_version"] != float64(2) {
		t.Fatalf("a replan replay replanned again: %v", replay)
	}
	refused(call("requester", http.MethodPost, otherBase+"/"+otherID+"/plan", "replan-b-"+suffix, `"2"`, reason("different")), http.StatusConflict, "IDEMPOTENCY_KEY_REUSED")
	refused(call("requester", http.MethodPost, otherBase+"/"+otherID+"/withdraw", "", `"1"`, reason("stale")), http.StatusPreconditionFailed, "PROVISIONING_REVISION_MISMATCH")
	withdrawn := decode(call("requester", http.MethodPost, otherBase+"/"+otherID+"/withdraw", "", revision(replanned), reason("not proceeding")),
		http.StatusOK, provisioningSchema)
	if withdrawn["state"] != "CANCELLED" || withdrawn["legacy_state"] != "cancelled" {
		t.Fatalf("withdraw: %v", withdrawn)
	}
	refused(call("requester", http.MethodPost, otherBase+"/"+otherID+"/withdraw", "", revision(withdrawn), reason("again")), http.StatusConflict, "PROVISIONING_STATE_CONFLICT")
}
