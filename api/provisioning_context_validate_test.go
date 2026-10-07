package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// Pre-activation provisioning authority (docs/architecture/context-authority-for-workloads.md section 13): the negative
// tests pinned there, in the order they are listed.

var platformContextValidationSchema = contracts.MustSchema("control-plane/v1/platform-context.schema.json#/$defs/PlatformContextValidation")

const (
	provisionerToken = "provisioner-token.header.signature-01"
	otherWorkloadTok = "other-workload-token.header.sig-0001"
	suspendedToken   = "suspended-provisioner-token.sig-0001"
	provCtxOK        = "00000000-0000-4000-8000-0000000000b1"
)

type provisioningFixture struct {
	*validateFixture
	sources     *erpSources
	provisioner string // canonical principal of the provisioner workload
}

const provisioningRegistry = `schema: {name: baobab-platform-workload-registry, version: "1.0"}
workloads:
  baobab-erp-workload:
    status: ACTIVE
    allowed_scopes: ["context:validate", "context:resolve"]
    validates_audiences: ["baobab-erp"]
  baobab-trade-workload:
    status: ACTIVE
    allowed_scopes: ["context:resolve"]
  baobab-cp-provisioning-workload:
    status: ACTIVE
    allowed_scopes: ["erp:provision"]
    context_purposes: ["TENANT_PROVISIONING"]
  baobab-suspended-provisioner:
    status: SUSPENDED
    allowed_scopes: ["erp:provision"]
    context_purposes: ["TENANT_PROVISIONING"]
  baobab-other-workload:
    status: ACTIVE
    allowed_scopes: ["context:resolve"]
`

func newProvisioningFixture(t *testing.T, withJudge bool) *provisioningFixture {
	t.Helper()
	sources := newErpSources()
	sources.c.State = "PROVISIONING_PROVIDERS"
	pf := &provisioningFixture{sources: sources}
	pf.validateFixture = newValidateFixture(t, func(d *Dependencies) {
		file := filepath.Join(t.TempDir(), "registry.yaml")
		if err := os.WriteFile(file, []byte(provisioningRegistry), 0o600); err != nil {
			t.Fatal(err)
		}
		registry, err := auth.LoadWorkloadRegistryFile(file)
		if err != nil {
			t.Fatal(err)
		}
		d.WorkloadRegistry = registry
		if withJudge {
			d.Provisioning = erpRouterProvisioning{src: sources}
		}
		subject := func(client, sub string) auth.Principal {
			return auth.Principal{Subject: sub, Issuer: validateIssuer, ActorType: "workload", ClientID: client, TokenID: "t-" + client,
				Scopes: map[string]struct{}{"erp:provision": {}}}
		}
		tokens := d.SubjectVerifiers.(audienceTokens).byAudience["baobab-erp"]
		tokens[provisionerToken] = subject("baobab-cp-provisioning-workload", "prov-sub")
		tokens[otherWorkloadTok] = subject("baobab-other-workload", "other-workload-sub")
		tokens[suspendedToken] = subject("baobab-suspended-provisioner", "suspended-sub")
	})
	pf.provisioner = resolutionIdentity(t, pf.contexts, "prov-sub")
	pf.store.tenant = domain.Tenant{TenantID: erpTenant, ObservedState: "pending", DesiredState: "active", Revision: 1}
	return pf
}

func (f *provisioningFixture) authority() *domain.ProvisioningAuthority {
	return &domain.ProvisioningAuthority{TenantProvisioningID: erpKey, PlanID: erpPlanID, PlanVersion: 3, PlanDigest: erpDigestHex}
}

// seed stores a TENANT_PROVISIONING context for the provisioner unless the caller overrides it.
func (f *provisioningFixture) seed(id string, mutate func(*domain.Context)) string {
	f.t.Helper()
	now := time.Now().UTC()
	expires := now.Add(10 * time.Minute)
	c := domain.Context{ID: id, PrincipalID: f.provisioner, TenantID: erpTenant, CorrelationID: erpKey, ResolvedAt: now, ExpiresAt: &expires,
		Provenance:       map[string]domain.ContextSource{"tenant_id": {Source: "tenant_provisioning", TrustLevel: domain.TrustSystem}},
		AuthorityPurpose: domain.ContextPurposeTenantProvisioning, ProvisioningAuthority: f.authority()}
	if mutate != nil {
		mutate(&c)
	}
	if err := f.contexts.CreateContext(context.Background(), c); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *provisioningFixture) validate(id string) *httptest.ResponseRecorder {
	return f.post(validatorBearer, vbody(id, provisionerToken))
}

func TestRuntimeContextOfAPendingTenantIsStillRefused(t *testing.T) {
	f := newProvisioningFixture(t, true)
	runtime := f.seed("00000000-0000-4000-8000-0000000000b2", func(c *domain.Context) {
		c.AuthorityPurpose, c.ProvisioningAuthority = "", nil
	})
	f.expect(f.post(validatorBearer, vbody(runtime, provisionerToken)), http.StatusForbidden, "TENANT_NOT_ACTIVE")
}

func TestAProvisioningContextOfAPendingTenantValidatesWhileTheApprovedPlanIsExecuting(t *testing.T) {
	f := newProvisioningFixture(t, true)
	id := f.seed(provCtxOK, nil)
	w := f.validate(id)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(w.Body.String()), &raw); err != nil {
		t.Fatal(err)
	}
	authority, _ := raw["provisioning_authority"].(map[string]any)
	if raw["authority_purpose"] != "TENANT_PROVISIONING" || raw["tenant_id"] != erpTenant || authority["plan_digest"] != erpDigestHex ||
		authority["plan_id"] != erpPlanID || authority["plan_version"] != float64(3) || authority["tenant_provisioning_id"] != erpKey {
		t.Fatalf("response: %v", raw)
	}
	for _, forbidden := range []string{"market_id", "organisation_id", "legal_entity_id", "principal_id"} {
		if _, ok := raw[forbidden]; ok {
			t.Fatalf("a provisioning validation must carry no %s: %v", forbidden, raw)
		}
	}
	if len(raw) != 6 {
		t.Fatalf("response: %v", raw)
	}
	// The answer is exactly the pinned Shared contract, including the purpose/tuple coupling.
	if err := contracts.Validate(platformContextValidationSchema, []byte(w.Body.String())); err != nil {
		t.Fatalf("response does not conform to control-plane/v1 PlatformContextValidation: %v", err)
	}
}

func TestAProvisioningContextIsAuthorityOnlyForARegisteredProvisioner(t *testing.T) {
	f := newProvisioningFixture(t, true)
	reference := f.post(validatorBearer, vbody("00000000-0000-4000-8000-0000000000ff", provisionerToken))
	// Owned by a workload whose registry entry lists no TENANT_PROVISIONING purpose.
	other := resolutionIdentity(t, f.contexts, "other-workload-sub")
	notRegistered := f.seed("00000000-0000-4000-8000-0000000000b3", func(c *domain.Context) { c.PrincipalID = other })
	w := f.post(validatorBearer, vbody(notRegistered, otherWorkloadTok))
	f.expect(w, http.StatusNotFound, "CONTEXT_NOT_FOUND")
	if _, d1 := problemOf(t, w); d1 != func() string { _, d := problemOf(t, reference); return d }() {
		t.Fatal("a refused purpose is distinguishable from an unknown context")
	}
	// A registered provisioner that is no longer ACTIVE gains nothing from the purpose.
	suspended := resolutionIdentity(t, f.contexts, "suspended-sub")
	asSuspended := f.seed("00000000-0000-4000-8000-0000000000b4", func(c *domain.Context) { c.PrincipalID = suspended })
	f.expect(f.post(validatorBearer, vbody(asSuspended, suspendedToken)), http.StatusNotFound, "CONTEXT_NOT_FOUND")
	// Another principal's token cannot validate the provisioner's context.
	f.expect(f.post(validatorBearer, vbody(f.seed("00000000-0000-4000-8000-0000000000b5", nil), otherWorkloadTok)), http.StatusNotFound, "CONTEXT_NOT_FOUND")
}

func TestAProvisioningContextIsNeverValidWithoutTheMeansToJudgeIt(t *testing.T) {
	f := newProvisioningFixture(t, false) // no provisioning sources configured
	f.expect(f.validate(f.seed(provCtxOK, nil)), http.StatusNotFound, "CONTEXT_NOT_FOUND")
}

func TestProvisioningAuthorityEndsWhenTheProvisioningLeavesTheAdmissibleStates(t *testing.T) {
	for state, ok := range map[string]bool{
		"PROVISIONING_PROVIDERS": true, "VERIFYING_READINESS": true, "REMEDIATING": true,
		"DRAFT": false, "VALIDATING": false, "PLANNED": false, "REGISTERING": false, "CONFIGURING_CONTEXT": false,
		"PROVISIONING_ENTITLEMENTS": false, "VALIDATING_SECURITY": false, "BLOCKED": false, "READY": false, "ACTIVE": false,
		"FAILED": false, "CANCELLED": false, "DEPROVISIONED": false, "": false, "SOMETHING_NEW": false,
	} {
		f := newProvisioningFixture(t, true)
		f.sources.c.State = state
		w := f.validate(f.seed(provCtxOK, nil))
		if ok {
			if w.Code != http.StatusOK {
				t.Errorf("%s: %d %s", state, w.Code, w.Body.String())
			}
			continue
		}
		f.expect(w, http.StatusForbidden, "PROVISIONING_AUTHORITY_NOT_CURRENT")
	}
}

func TestProvisioningAuthorityIsBoundToTheApprovedCurrentPlan(t *testing.T) {
	mutations := map[string]func(*provisioningFixture){
		"plan id superseded":       func(f *provisioningFixture) { f.sources.c.Plan.PlanID = "plan_0199a1b2c3d4ffff" },
		"plan version superseded":  func(f *provisioningFixture) { f.sources.c.Plan.PlanVersion = 4 },
		"plan digest superseded":   func(f *provisioningFixture) { f.sources.c.Plan.PlanDigest = "sha256:" + strings.Repeat("c", 64) },
		"no plan":                  func(f *provisioningFixture) { f.sources.c.Plan = nil },
		"no approval":              func(f *provisioningFixture) { f.sources.c.Decision = nil },
		"approval rejected":        func(f *provisioningFixture) { f.sources.c.Decision.Decision = "REJECTED" },
		"approval of another plan": func(f *provisioningFixture) { f.sources.c.Decision.PlanDigest = "sha256:" + strings.Repeat("d", 64) },
		"provisioning of a tenant": func(f *provisioningFixture) { f.sources.c.TenantID = "tn_someoneelse" },
		"unknown provisioning":     func(f *provisioningFixture) { f.sources.c.ID = "00000000-0000-4000-8000-00000000dead" },
		"desired state disagrees": func(f *provisioningFixture) {
			f.sources.desired.DesiredStateDigest = "sha256:" + strings.Repeat("e", 64)
		},
		"desired state of a tenant": func(f *provisioningFixture) { f.sources.desired.Tenant.TenantID = "tn_someoneelse" },
	}
	for name, mutate := range mutations {
		f := newProvisioningFixture(t, true)
		mutate(f)
		w := f.validate(f.seed(provCtxOK, nil))
		if got, _ := problemOf(t, w); w.Code != http.StatusForbidden || got != "PROVISIONING_AUTHORITY_NOT_CURRENT" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestAProvisioningContextBoundToAnotherProvisioningIsNotCurrent(t *testing.T) {
	f := newProvisioningFixture(t, true)
	other := f.seed(provCtxOK, func(c *domain.Context) {
		a := *c.ProvisioningAuthority
		a.TenantProvisioningID = "tp_0199a1b2c3d47e8f9a0b1c2d3e4f5aaa"
		c.ProvisioningAuthority = &a
	})
	f.expect(f.validate(other), http.StatusForbidden, "PROVISIONING_AUTHORITY_NOT_CURRENT")
}

func TestATenantThatCannotBeProvisionedIsNotActiveToAProvisioningContext(t *testing.T) {
	for state, ok := range map[string]bool{
		"pending": true, "provisioning": true, "active": true,
		"suspended": false, "decommissioning": false, "decommissioned": false, "": false, "unknown_state": false,
	} {
		f := newProvisioningFixture(t, true)
		f.store.tenant.ObservedState = state
		w := f.validate(f.seed(provCtxOK, nil))
		if ok {
			if w.Code != http.StatusOK {
				t.Errorf("%q: %d %s", state, w.Code, w.Body.String())
			}
			continue
		}
		f.expect(w, http.StatusForbidden, "TENANT_NOT_ACTIVE")
	}
	f := newProvisioningFixture(t, true)
	f.store.tenantErr = domain.NotFoundError("tenant not found")
	f.expect(f.validate(f.seed(provCtxOK, nil)), http.StatusForbidden, "TENANT_NOT_ACTIVE")
}

func TestAnUnreadableProvisioningSourceIsUnavailableNotAnAnswerAboutTheContext(t *testing.T) {
	f := newProvisioningFixture(t, true)
	f.sources.err = errors.New("database down")
	f.expect(f.validate(f.seed(provCtxOK, nil)), http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE")
}

// A provisioning context is never ordinary runtime authority (section 13.2 P6): to capability resolution it is as
// absent as an unknown context, even for the principal that owns it and holds context:resolve.
func TestAProvisioningContextIsNotRedeemableAsRuntimeAuthority(t *testing.T) {
	f := newProvisioningFixture(t, true)
	erpPrincipal := resolutionIdentity(t, f.contexts, "erp-sub")
	owned := f.seed("00000000-0000-4000-8000-0000000000b6", func(c *domain.Context) { c.PrincipalID = erpPrincipal })
	for _, path := range []string{"/v1/capabilities/resolve", "/v1/capabilities/resolve-batch"} {
		body := `{"context_id":"` + owned + `","capability_key":"commerce.order.create","correlation_id":"0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a99"}`
		if strings.HasSuffix(path, "batch") {
			body = `{"context_id":"` + owned + `","capabilities":["commerce.order.create"],"correlation_id":"0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a98"}`
		}
		w := f.postTo(path, "resolve-only", body)
		f.expect(w, http.StatusNotFound, "CONTEXT_NOT_FOUND")
		// Control: the same request for the same principal's RUNTIME context is not refused as an unknown context, so the
		// refusal above is the purpose and nothing else.
		runtimeBody := strings.Replace(body, owned, "00000000-0000-4000-8000-0000000000a4", 1)
		if got, _ := problemOf(t, f.postTo(path, "resolve-only", runtimeBody)); got == "CONTEXT_NOT_FOUND" {
			t.Fatalf("%s: the control request was refused as an unknown context", path)
		}
	}
}

func TestRuntimeContextsHidesEveryNonRuntimePurpose(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	now := time.Now().UTC()
	expires := now.Add(5 * time.Minute)
	for id, c := range map[string]domain.Context{
		"00000000-0000-4000-8000-0000000000c1": {PrincipalID: "p", TenantID: "tn_runtimectx", CorrelationID: "c", ResolvedAt: now, ExpiresAt: &expires},
		"00000000-0000-4000-8000-0000000000c2": {PrincipalID: "p", TenantID: "tn_runtimectx", CorrelationID: "c", ResolvedAt: now, ExpiresAt: &expires,
			AuthorityPurpose: domain.ContextPurposeTenantProvisioning, ProvisioningAuthority: &domain.ProvisioningAuthority{TenantProvisioningID: erpKey,
				PlanID: erpPlanID, PlanVersion: 1, PlanDigest: erpDigestHex}},
	} {
		c.ID = id
		if err := repo.CreateContext(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	view := runtimeOnly(repo)
	if _, err := view.GetContext(context.Background(), "00000000-0000-4000-8000-0000000000c1"); err != nil {
		t.Fatalf("a RUNTIME context must stay redeemable: %v", err)
	}
	if _, err := view.GetContext(context.Background(), "00000000-0000-4000-8000-0000000000c2"); !errors.Is(err, repository.ErrContextNotFound) {
		t.Fatalf("a provisioning context must be absent to runtime consumers: %v", err)
	}
	if runtimeOnly(nil) != nil {
		t.Fatal("a store that is not configured must stay not configured")
	}
}

// Every consumer of a stored context other than the validator goes through the RUNTIME view, so a new route cannot read a
// provisioning context by accident (the single-view rule of context-authority-for-workloads.md I4).
func TestOnlyTheValidatorReadsTheUnfilteredContextStore(t *testing.T) {
	raw, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatal(err)
	}
	reading := regexp.MustCompile(`(?i)\bcontexts?:\s*dependencies\.Contexts\b`)
	for _, line := range strings.Split(string(raw), "\n") {
		if !reading.MatchString(line) {
			continue
		}
		if strings.Contains(line, "ContextValidationHandler{") || strings.Contains(line, "PlatformContextHandler{") ||
			strings.Contains(line, "validation := ContextValidationHandler{") {
			continue
		}
		t.Errorf("a context consumer reads the unfiltered store; use runtimeOnly: %s", strings.TrimSpace(line))
	}
}

// A replan followed by a fresh approval is a different approved plan: a context minted for the earlier one is no longer
// authority, even though the provisioning now has a perfectly consistent approved plan (ADR-BCP-021 section 24: an approval
// binds id, version and digest together; section 27: an approved plan is never edited).
func TestAContextForASupersededPlanIsNotCurrentEvenWhenTheNewPlanIsApproved(t *testing.T) {
	for name, mutate := range map[string]func(*provisioningFixture){
		"new digest": func(f *provisioningFixture) {
			d := "sha256:" + strings.Repeat("f", 64)
			f.sources.c.Plan.PlanDigest, f.sources.c.Decision.PlanDigest = d, d
		},
		"new version": func(f *provisioningFixture) { f.sources.c.Plan.PlanVersion, f.sources.c.Decision.PlanVersion = 4, 4 },
		"new plan id": func(f *provisioningFixture) {
			f.sources.c.Plan.PlanID, f.sources.c.Decision.PlanID = "plan_0199a1b2c3d4eeee", "plan_0199a1b2c3d4eeee"
		},
	} {
		f := newProvisioningFixture(t, true)
		mutate(f)
		if got, _ := problemOf(t, f.validate(f.seed(provCtxOK, nil))); got != "PROVISIONING_AUTHORITY_NOT_CURRENT" {
			t.Errorf("%s: %s", name, got)
		}
	}
}

// The context names a tenant and a provisioning; the provisioning that answers must be that provisioning of that tenant,
// whatever else is consistent.
func TestAContextIsOnlyAuthorityForTheTenantAndProvisioningItNames(t *testing.T) {
	f := newProvisioningFixture(t, true)
	elsewhere := f.seed(provCtxOK, func(c *domain.Context) { c.TenantID = "tn_anothertenant" })
	if got, _ := problemOf(t, f.validate(elsewhere)); got != "PROVISIONING_AUTHORITY_NOT_CURRENT" {
		t.Errorf("a context of another tenant than the provisioning's: %s", got)
	}
	f = newProvisioningFixture(t, true)
	f.sources.c.Key, f.sources.c.Plan.TenantProvisioningID = "tp_0199a1b2c3d47e8f9a0b1c2d3e4f5bbb", "tp_0199a1b2c3d47e8f9a0b1c2d3e4f5bbb"
	if got, _ := problemOf(t, f.validate(f.seed("00000000-0000-4000-8000-0000000000b7", nil))); got != "PROVISIONING_AUTHORITY_NOT_CURRENT" {
		t.Errorf("a provisioning answering under another key: %s", got)
	}
}
