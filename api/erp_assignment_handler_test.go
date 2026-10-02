package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const (
	erpTenant    = "tn_01k4zuribeans"
	erpUUID      = "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b"
	erpKey       = "tp_0199a1b2c3d47e8f9a0b1c2d3e4f5a6b"
	erpLegal     = "ZURIBEANS-ZA"
	erpInstance  = "ei_0199a1b2c3d47e8f"
	erpDigestHex = "sha256:b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
)

type erpSources struct {
	c       repository.ConvergedProvisioning
	desired convergence.DesiredState
	err     error
	profile *domain.LegalEntityProfile
}

func (s *erpSources) GetConvergedProvisioning(_ context.Context, id string) (repository.ConvergedProvisioning, error) {
	if s.err != nil {
		return repository.ConvergedProvisioning{}, s.err
	}
	if id != s.c.ID {
		return repository.ConvergedProvisioning{}, repository.ErrProvisioningNotFound
	}
	return s.c, nil
}

func (s *erpSources) GetDesiredState(context.Context, string, int64) (convergence.DesiredState, error) {
	return s.desired, nil
}

func (s *erpSources) GetLegalEntityProfile(_ context.Context, id string) (*domain.LegalEntityProfile, error) {
	if s.profile == nil || s.profile.LegalEntityID != id {
		return nil, nil
	}
	return s.profile, nil
}

func erpStep(id, op, engine, instance, capability string) convergence.Step {
	return convergence.Step{StepID: id, Operation: op, Resources: convergence.StepResources{
		EngineID: engine, EngineInstanceID: instance, CapabilityKey: capability}}
}

// newErpSources is an approved, executable, consistent provisioning of two legal entities.
func newErpSources() *erpSources {
	plan := &convergence.Plan{
		PlanID: "plan_1", PlanVersion: 1, PlanDigest: erpDigestHex, TenantProvisioningID: erpKey, TenantID: erpTenant,
		DesiredStateDigest: "sha256:a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1",
		Steps: []convergence.Step{
			erpStep("grant", convergence.OpCreateCapabilityGrant, "", "", "finance.order-consequence.process"),
			// Out of order, duplicated and mixed with another engine's step: only ERP's, deduplicated and sorted.
			erpStep("b2", convergence.OpCreateCapabilityBinding, "baobab-erp", erpInstance, "finance.order-consequence.process"),
			erpStep("b1", convergence.OpCreateCapabilityBinding, "baobab-erp", erpInstance, "finance.ledger.post"),
			erpStep("b3", convergence.OpCreateCapabilityBinding, "baobab-erp", erpInstance, "finance.order-consequence.process"),
			erpStep("t1", convergence.OpCreateCapabilityBinding, "baobab-trade", "ei_trade", "commerce.order.manage"),
		},
	}
	return &erpSources{
		c: repository.ConvergedProvisioning{ID: erpUUID, Key: erpKey, TenantID: erpTenant, State: "PLANNED",
			DesiredStateVersion: 1, DesiredStateDigest: plan.DesiredStateDigest, Plan: plan,
			Decision: &repository.PlanDecision{Decision: "APPROVED", PlanID: "plan_1", PlanDigest: erpDigestHex}},
		desired: convergence.DesiredState{
			Tenant: convergence.DesiredTenant{TenantID: erpTenant, DisplayName: "Zuribeans"}, LegalEntities: []string{erpLegal, "ZURIBEANS-UG"},
			MarketParticipation:  []convergence.DesiredMarket{{Market: "ZA", Activities: []string{"SELLING", "IMPORTING"}}},
			IsolationRequirement: "row_level_security", DesiredStateDigest: plan.DesiredStateDigest,
		},
		profile: &domain.LegalEntityProfile{LegalEntityID: erpLegal, LegalName: "Zuribeans South Africa (Pty) Ltd",
			JurisdictionOfIncorporation: "ZA", VerificationState: domain.VerificationVerified,
			RegistrationIdentifiers: []domain.OrganisationIdentifier{{Type: "COMPANY_REGISTRATION", Value: "2026/000000/07", Verified: true}}},
	}
}

func erpRequest(t *testing.T, s *erpSources, principal *auth.Principal, tenant, key, legal string, stale bool) *httptest.ResponseRecorder {
	t.Helper()
	h := erpAssignmentHandler{provisionings: s, profiles: s, now: func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC) },
		stale: func(context.Context, repository.ConvergedProvisioning) (bool, error) { return stale, nil }}
	router := chi.NewRouter()
	router.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if principal != nil {
				r = r.WithContext(auth.WithPrincipal(r.Context(), *principal))
			}
			next.ServeHTTP(w, r)
		})
	}).Get("/v1/tenants/{tenantID}/provisioning/{provisioningID}/erp-assignments/{legalEntityID}", h.get)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tenants/"+tenant+"/provisioning/"+key+"/erp-assignments/"+legal, nil))
	return rec
}

func erpWorkload(tenant string) *auth.Principal {
	return &auth.Principal{Subject: "erp", ActorType: "workload", TenantID: tenant, ClientID: "erp-client",
		Scopes: map[string]struct{}{"erp-assignment:read": {}}}
}

func TestErpAssignmentProjectsOnlyContractFacts(t *testing.T) {
	rec := erpRequest(t, newErpSources(), erpWorkload(erpTenant), erpTenant, erpKey, erpLegal, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if err := contracts.Validate(erpAssignmentSchema, rec.Body.Bytes()); err != nil {
		t.Fatalf("response does not satisfy the pinned Shared schema: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["tenant_id"] != erpTenant || got["tenant_provisioning_id"] != erpKey || got["plan_digest"] != erpDigestHex ||
		got["engine_id"] != "baobab-erp" || got["engine_instance_id"] != erpInstance || got["isolation_requirement"] != "row_level_security" {
		t.Errorf("unexpected projection: %v", got)
	}
	// Derived from the approved plan's ERP steps: deduplicated, sorted, and not the other engine's.
	if caps, _ := json.Marshal(got["capabilities"]); string(caps) != `["finance.ledger.post","finance.order-consequence.process"]` {
		t.Errorf("capabilities = %s", caps)
	}
	entity := got["legal_entity"].(map[string]any)
	if entity["legal_entity_id"] != erpLegal || entity["jurisdiction_code"] != "ZA" || entity["verification_state"] != "VERIFIED" {
		t.Errorf("legal entity = %v", entity)
	}
	// 15 minutes from the clock; the issue time is the only moving part.
	if got["issued_at"] != "2026-10-02T09:00:00Z" || got["expires_at"] != "2026-10-02T09:15:00Z" {
		t.Errorf("window = %v .. %v", got["issued_at"], got["expires_at"])
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("an assignment must not be cached")
	}
}

// The removed concepts stay removed: no registry identifier, and no UUID standing in for a canonical id.
func TestErpAssignmentNeverEmitsRegistryIdentifiers(t *testing.T) {
	rec := erpRequest(t, newErpSources(), erpWorkload(erpTenant), erpTenant, erpKey, erpLegal, false)
	body := rec.Body.String()
	for _, forbidden := range []string{"capability_binding_id", "capability_bindings", "isolation_profile_id", "native_client", "ad_client", "legal_entity_code"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response carries %q: %s", forbidden, body)
		}
	}
	if regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`).MatchString(body) {
		t.Errorf("response carries a UUID: %s", body)
	}
}

func TestErpAssignmentForbidsAnotherTenantsToken(t *testing.T) {
	cases := map[string]*auth.Principal{
		"no principal":          nil,
		"no tenant claim":       erpWorkload(""),
		"another tenant":        erpWorkload("tn_01k4other"),
		"a human administrator": {Subject: "admin", ActorType: "human", TenantID: erpTenant},
	}
	for name, principal := range cases {
		t.Run(name, func(t *testing.T) {
			rec := erpRequest(t, newErpSources(), principal, erpTenant, erpKey, erpLegal, false)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestErpAssignmentAbsenceIsNotFound(t *testing.T) {
	other := newErpSources()
	other.c.TenantID = "tn_01k4other"
	cases := map[string]struct {
		sources    *erpSources
		key, legal string
	}{
		"unknown provisioning":                    {newErpSources(), "tp_ffffffffffffffffffffffffffffffff", erpLegal},
		"malformed provisioning id":               {newErpSources(), "not-an-id", erpLegal},
		"a provisioning of another tenant":        {other, erpKey, erpLegal},
		"a legal entity outside the provisioning": {newErpSources(), erpKey, "ACME-KE"},
		"a malformed legal entity id":             {newErpSources(), erpKey, "zuribeans-za"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := erpRequest(t, c.sources, erpWorkload(erpTenant), erpTenant, c.key, c.legal, false)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d: %s", rec.Code, rec.Body)
			}
		})
	}
}

// Inconsistency is a conflict, never repaired or defaulted.
func TestErpAssignmentConflictsAreNeverRepaired(t *testing.T) {
	cases := map[string]struct {
		mutate func(*erpSources)
		stale  bool
	}{
		"plan has no decision":                 {func(s *erpSources) { s.c.Decision = nil }, false},
		"plan was rejected":                    {func(s *erpSources) { s.c.Decision.Decision = "REJECTED" }, false},
		"approval is for another digest":       {func(s *erpSources) { s.c.Decision.PlanDigest = "sha256:" + strings.Repeat("c", 64) }, false},
		"approval is for another plan":         {func(s *erpSources) { s.c.Decision.PlanID = "plan_2" }, false},
		"provisioning was withdrawn":           {func(s *erpSources) { s.c.State = "CANCELLED" }, false},
		"provisioning has no plan":             {func(s *erpSources) { s.c.Plan = nil }, false},
		"plan is stale":                        {func(*erpSources) {}, true},
		"desired state names another tenant":   {func(s *erpSources) { s.desired.Tenant.TenantID = "tn_01k4other" }, false},
		"desired state digest disagrees":       {func(s *erpSources) { s.desired.DesiredStateDigest = "sha256:" + strings.Repeat("d", 64) }, false},
		"plan belongs to another provisioning": {func(s *erpSources) { s.c.Plan.TenantProvisioningID = "tp_ffffffffffffffffffffffffffffffff" }, false},
		"isolation requirement is missing":     {func(s *erpSources) { s.desired.IsolationRequirement = "" }, false},
		"no ERP steps":                         {func(s *erpSources) { s.c.Plan.Steps = s.c.Plan.Steps[:1] }, false},
		"ERP steps resolve to two instances":   {func(s *erpSources) { s.c.Plan.Steps[1].Resources.EngineInstanceID = "ei_0199a1b2c3d47e90" }, false},
		"legal entity has no profile":          {func(s *erpSources) { s.profile = nil }, false},
		"legal entity is not verified":         {func(s *erpSources) { s.profile.VerificationState = domain.VerificationPendingReview }, false},
		"legal entity has no jurisdiction":     {func(s *erpSources) { s.profile.JurisdictionOfIncorporation = "" }, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newErpSources()
			c.mutate(s)
			rec := erpRequest(t, s, erpWorkload(erpTenant), erpTenant, erpKey, erpLegal, c.stale)
			if rec.Code != http.StatusConflict {
				t.Errorf("status = %d: %s", rec.Code, rec.Body)
			}
		})
	}
}

// Once execution has begun the registry has moved on, so staleness is judged only before apply.
func TestErpAssignmentIsServedWhileExecutionIsUnderway(t *testing.T) {
	s := newErpSources()
	s.c.State = "PROVISIONING_PROVIDERS"
	if rec := erpRequest(t, s, erpWorkload(erpTenant), erpTenant, erpKey, erpLegal, true); rec.Code != http.StatusOK {
		t.Errorf("status = %d: %s", rec.Code, rec.Body)
	}
}

func TestErpAssignmentSourceOutageIsUnavailableNotAbsent(t *testing.T) {
	s := newErpSources()
	s.err = errors.New("database down")
	if rec := erpRequest(t, s, erpWorkload(erpTenant), erpTenant, erpKey, erpLegal, false); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d: %s", rec.Code, rec.Body)
	}
}

// Through the real router, the scope is required and a human token does not substitute for it.
func TestErpAssignmentRouteRequiresTheWorkloadScope(t *testing.T) {
	s := newErpSources()
	s.c.State = "ACTIVE"
	prov := erpRouterProvisioning{ProvisioningRepository: nil, src: s}
	orgs := erpRouterOrganisations{src: s}
	call := func(principal auth.Principal) int {
		handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: adminPrincipal()},
			WorkloadVerifier: fakeVerifier{principal: principal}, Provisioning: prov, OrganisationAdmission: orgs})
		req := httptest.NewRequest(http.MethodGet, "/v1/tenants/"+erpTenant+"/provisioning/"+erpKey+"/erp-assignments/"+erpLegal, nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	withScope := *erpWorkload(erpTenant)
	if code := call(withScope); code != http.StatusOK {
		t.Errorf("with the scope: status = %d", code)
	}
	withoutScope := withScope
	withoutScope.Scopes = map[string]struct{}{"mapping:read": {}, "tenant:read": {}}
	if code := call(withoutScope); code != http.StatusForbidden {
		t.Errorf("without the scope: status = %d, want 403", code)
	}
}

// erpRouterProvisioning and erpRouterOrganisations satisfy the router's large repository interfaces with only the
// reads the projection uses; anything else would panic, which is the point of a narrow test double.
type erpRouterProvisioning struct {
	ProvisioningRepository
	src *erpSources
}

func (p erpRouterProvisioning) GetConvergedProvisioning(ctx context.Context, id string) (repository.ConvergedProvisioning, error) {
	return p.src.GetConvergedProvisioning(ctx, id)
}

func (p erpRouterProvisioning) GetDesiredState(ctx context.Context, id string, version int64) (convergence.DesiredState, error) {
	return p.src.GetDesiredState(ctx, id, version)
}

type erpRouterOrganisations struct {
	repository.OrganisationAdmissionRepository
	src *erpSources
}

func (o erpRouterOrganisations) GetLegalEntityProfile(ctx context.Context, id string) (*domain.LegalEntityProfile, error) {
	return o.src.GetLegalEntityProfile(ctx, id)
}
