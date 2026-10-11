package organisation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type peo05CoreFixture struct {
	records       []repository.ConvergedProvisioning
	desired       convergence.DesiredState
	request       domain.TenantOnboardingRequest
	products      map[string]convergence.Product
	members       map[string][]convergence.Member
	subscriptions map[string]domain.ProductSubscriptionClassificationTarget
}

func (f *peo05CoreFixture) ListConvergedProvisionings(_ context.Context, _ string, _ int, _ string) ([]repository.ConvergedProvisioning, error) {
	return f.records, nil
}
func (f *peo05CoreFixture) GetDesiredState(_ context.Context, _ string, _ int64) (convergence.DesiredState, error) {
	return f.desired, nil
}
func (f *peo05CoreFixture) GetTenantOnboardingRequest(_ context.Context, _ string) (domain.TenantOnboardingRequest, error) {
	return f.request, nil
}
func (f *peo05CoreFixture) PlanningProduct(_ context.Context, id string) (convergence.Product, bool, error) {
	p, ok := f.products[id]
	return p, ok, nil
}
func (f *peo05CoreFixture) PlanningComposition(_ context.Context, key string) ([]convergence.Member, bool, error) {
	p, ok := f.members[key]
	return p, ok, nil
}
func (f *peo05CoreFixture) FindTenantProductSubscription(_ context.Context, _, id string) (domain.ProductSubscriptionClassificationTarget, error) {
	p, ok := f.subscriptions[id]
	if !ok {
		return p, repository.ErrProductSubscriptionNotFound
	}
	return p, nil
}

type peo05FreshnessFixture struct {
	current bool
	err     error
}

func (f peo05FreshnessFixture) CurrentPlan(_ context.Context, _ convergence.DesiredState, _ convergence.Plan) (bool, error) {
	return f.current, f.err
}

type peo05IAMFixture struct {
	proof PEO05IAMProof
	err   error
}

func (f *peo05IAMFixture) CurrentTenantIAM(_ context.Context, _, _ string, _ time.Time) (PEO05IAMProof, error) {
	return f.proof, f.err
}

type peo05ProviderFixture struct {
	proofs []PEO05ProviderProof
	err    error
}

func (f *peo05ProviderFixture) CurrentMandatoryProviders(_ context.Context, _, _ string, _ convergence.Plan, _ []string, _ time.Time) ([]PEO05ProviderProof, error) {
	return f.proofs, f.err
}
func canonicalCPFixture(t *testing.T) (AuthoritativePEO05Reader, time.Time) {
	t.Helper()
	at := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	const tenant = "tn_synthetic_one"
	const org = "org_synthetic_one"
	req := domain.TenantOnboardingRequest{
		ID: "tor_0199a1b2c3d47e8f", AdmissionDecisionID: "adm_0199a1b2c3d47e8f",
		Status: domain.OnboardingFulfilled, TenantID: tenant,
		RequestedBy: "prn_maker", AuthorisedBy: "prn_onboarding_checker",
	}
	desired := convergence.DesiredState{
		Tenant:               convergence.DesiredTenant{TenantID: tenant, DisplayName: "Synthetic Business"},
		Products:             []convergence.DesiredProduct{{ProductID: "xbt"}, {ProductID: "erp"}},
		ResidencyRequirement: "af-south-1",
		IsolationRequirement: "row_level_security",
		FrozenAt:             at.Add(-time.Minute),
		Provenance: convergence.DesiredStateProvenance{
			TenantOnboardingRequestID: req.ID, AdmissionDecisionID: req.AdmissionDecisionID,
			DesiredStateVersion: 1,
		},
	}
	desired.DesiredStateDigest = convergence.DesiredStateDigest(desired)
	plan := convergence.Plan{
		TenantProvisioningID: "tp_synthetic", TenantID: tenant,
		DesiredStateDigest: desired.DesiredStateDigest,
		PlanID:             "plan_synthetic", PlanVersion: 1, BaseRevision: 1,
		GeneratedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour),
	}
	plan.PlanDigest = convergence.PlanDigest(plan)
	core := &peo05CoreFixture{
		records: []repository.ConvergedProvisioning{{
			ID:  "01800000-0000-7000-8000-000000000001",
			Key: "tp_synthetic", TenantID: tenant, State: "READY", ReadinessStatus: "READY",
			TenantOnboardingRequestID: req.ID, AdmissionDecisionID: req.AdmissionDecisionID,
			DesiredStateVersion: 1, DesiredStateDigest: desired.DesiredStateDigest,
			Plan: &plan, Decision: &repository.PlanDecision{
				ApprovalID: "apd_other", PlanID: plan.PlanID, PlanVersion: 1,
				PlanDigest: plan.PlanDigest, Decision: "APPROVED",
				DecidedBy: "prn_plan_checker", DecidedAt: at.Add(-time.Second),
			},
		}},
		desired: desired, request: req,
		products: map[string]convergence.Product{
			"xbt": {ProductID: "xbt", CompositionKey: "solution.xbt"},
			"erp": {ProductID: "erp", CompositionKey: "solution.erp"},
		},
		members: map[string][]convergence.Member{
			"solution.xbt": {{CapabilityKey: "commerce.order.manage", Criticality: convergence.CriticalityMandatory}},
			"solution.erp": {{CapabilityKey: "erp.invoice.post", Criticality: convergence.CriticalityMandatory}},
		},
		subscriptions: map[string]domain.ProductSubscriptionClassificationTarget{
			"xbt": {ID: "sub_xbt", TenantID: tenant, ProductID: "xbt", Status: "ACTIVE", ClassificationID: "psc_xbt"},
			"erp": {ID: "sub_erp", TenantID: tenant, ProductID: "erp", Status: "ACTIVE", ClassificationID: "psc_erp"},
		},
	}
	iam := &peo05IAMFixture{proof: PEO05IAMProof{
		TenantID: tenant, OrganisationID: org, PrincipalID: "prn_member",
		IssuerID: "iss_staging", Audience: "baobab-control-plane",
		MembershipStatus: "ACTIVE", EvidenceReference: "revocation_verified",
		RevocationCheckedAt: at.Add(-time.Second), ExpiresAt: at.Add(time.Minute),
	}}
	certified := func(key string) PEO05ProviderProof {
		return PEO05ProviderProof{
			TenantID: tenant, CapabilityKey: key, EngineInstanceID: "ei_staging",
			ProviderKey: "engine.provider", ResidencyRegion: "af-south-1",
			SupportStatus: "CERTIFIED", CertificationReference: "cert_reviewed",
			CertificationExpiresAt: at.Add(time.Hour), HealthStatus: "HEALTHY",
			HealthObservedAt: at.Add(-time.Second), HealthExpiresAt: at.Add(time.Minute),
		}
	}
	providers := &peo05ProviderFixture{proofs: []PEO05ProviderProof{
		certified("commerce.order.manage"), certified("erp.invoice.post"),
	}}
	return AuthoritativePEO05Reader{Core: core, Freshness: peo05FreshnessFixture{current: true}, IAM: iam, Providers: providers}, at
}

func TestPEO05BCanonicalMultipleProductsAndIndependentEvidence(t *testing.T) {
	reader, now := canonicalCPFixture(t)
	got, err := reader.ReadPEO05Tenant(context.Background(), "tn_synthetic_one", "org_synthetic_one", now)
	if err != nil || len(got.RequiredProducts) != 2 || len(got.Subscriptions) != 2 ||
		len(got.RequiredCapabilities) != 2 {
		t.Fatalf("canonical two-product qualification failed: %+v %v", got, err)
	}
}
func TestPEO05BDeniesAbsentIAMOrProviderAdapters(t *testing.T) {
	reader, now := canonicalCPFixture(t)
	reader.IAM = nil
	if _, err := reader.ReadPEO05Tenant(context.Background(), "tn_synthetic_one", "org_synthetic_one", now); !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("missing IAM accepted: %v", err)
	}
	reader, now = canonicalCPFixture(t)
	reader.Providers = nil
	if _, err := reader.ReadPEO05Tenant(context.Background(), "tn_synthetic_one", "org_synthetic_one", now); !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("missing certification source accepted: %v", err)
	}
}
func TestPEO05BDeniesStalePlanDifferentTenantAndMissingProduct(t *testing.T) {
	cases := map[string]func(*peo05CoreFixture){
		"stale plan digest":                    func(c *peo05CoreFixture) { c.records[0].Decision.PlanDigest = "sha256:stale" },
		"unapproved plan":                      func(c *peo05CoreFixture) { c.records[0].Decision.Decision = "REJECTED" },
		"stale desired state":                  func(c *peo05CoreFixture) { c.desired.ResidencyRequirement = "eu-west-1" },
		"sibling target":                       func(c *peo05CoreFixture) { c.records[0].TenantID = "tn_sibling" },
		"self approved plan":                   func(c *peo05CoreFixture) { c.records[0].Decision.DecidedBy = c.request.RequestedBy },
		"unready tenant":                       func(c *peo05CoreFixture) { c.records[0].ReadinessStatus = "BLOCKED" },
		"superseded or duplicate provisioning": func(c *peo05CoreFixture) { c.records = append(c.records, c.records[0]) },
		"missing second subscription":          func(c *peo05CoreFixture) { delete(c.subscriptions, "erp") },
		"missing classification": func(c *peo05CoreFixture) {
			s := c.subscriptions["erp"]
			s.ClassificationID = ""
			c.subscriptions["erp"] = s
		},
		"missing canonical composition": func(c *peo05CoreFixture) { delete(c.members, "solution.erp") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			reader, now := canonicalCPFixture(t)
			change(reader.Core.(*peo05CoreFixture))
			if _, err := reader.ReadPEO05Tenant(context.Background(), "tn_synthetic_one", "org_synthetic_one", now); !errors.Is(err, ErrPEO05Integrity) {
				t.Fatalf("bad source accepted: %v", err)
			}
		})
	}
}
func TestPEO05BDeniesMateriallyStalePlan(t *testing.T) {
	reader, now := canonicalCPFixture(t)
	reader.Freshness = peo05FreshnessFixture{current: false}
	if _, err := reader.ReadPEO05Tenant(context.Background(), "tn_synthetic_one", "org_synthetic_one", now); !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("stale provider or market material accepted: %v", err)
	}
	reader.Freshness = peo05FreshnessFixture{err: errors.New("registry unavailable")}
	if _, err := reader.ReadPEO05Tenant(context.Background(), "tn_synthetic_one", "org_synthetic_one", now); !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("failed replanning accepted: %v", err)
	}
	reader.Freshness = nil
	if _, err := reader.ReadPEO05Tenant(context.Background(), "tn_synthetic_one", "org_synthetic_one", now); !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("missing replanner accepted: %v", err)
	}
}

func TestPEO05BDeniesRevokedIAMAndProviderProof(t *testing.T) {
	for _, choice := range []string{"iam", "provider"} {
		t.Run(choice, func(t *testing.T) {
			reader, now := canonicalCPFixture(t)
			if choice == "iam" {
				reader.IAM.(*peo05IAMFixture).proof.MembershipStatus = "REVOKED"
			} else {
				reader.Providers.(*peo05ProviderFixture).proofs[1].SupportStatus = "REVOKED"
			}
			if _, err := reader.ReadPEO05Tenant(context.Background(), "tn_synthetic_one", "org_synthetic_one", now); !errors.Is(err, ErrPEO05Integrity) {
				t.Fatalf("revoked source accepted: %v", err)
			}
		})
	}
}
