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
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
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

// TestProvisioningIsPlannedApprovedAndApplied drives ADR-SHARED-015 end to
// end: plan from an authorised onboarding request, approve the plan's
// digest as another principal, apply it as an operation, execute it.
func TestProvisioningIsPlannedApprovedAndApplied(t *testing.T) {
	_, repo, admin, url := newProvisioningTestHandler(t)
	ctx := context.Background()
	f := seedProvisioningAPIFixture(t, ctx, admin, repo, "converge")
	suffix := strings.ToLower(f.suffix())
	capabilityKey := "trade.settlement" + suffix + ".execute"
	engineCode, providerKey := "zbconv-"+suffix, "zbconv-"+suffix+".medusa"
	productID, compositionKey := "zbconv-"+suffix, "solution.zbconv-"+suffix
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
	requester := principal("requester-"+suffix, "tenant:write", "tenant:read", "provisioning:approve")
	approver := principal("approver-"+suffix, "tenant:read", "tenant:write", "provisioning:approve")
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
		Identities: identities, Provisioning: store})

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

	executor := apply.Executor{Store: repo, Registry: repo, Pipeline: apply.StandardPipeline(provisioning.ZB02Dependencies{Tenants: tenantStore, Repo: repo, Provisioning: repo})}
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
	// Readiness evidence of the applied provisioning, by its public id.
	if readiness := call("requester", http.MethodGet, base+"/"+id+"/readiness", "", "", ""); readiness.Code != http.StatusOK {
		t.Fatalf("readiness: %d %s", readiness.Code, readiness.Body.String())
	}
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
}

func jsonNumber(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
