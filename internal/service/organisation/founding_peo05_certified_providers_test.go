package organisation

import (
	"context"
	"errors"
	"testing"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/capability/certification"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

type peo05ProviderSource struct {
	grants []capabilitydomain.CapabilityGrant
	scope capabilitydomain.CapabilityScope
	binding repository.PEO05BoundProvider
	observed release.ObservedRelease
	engineRelease release.Release
	certs []certification.Certification
	health health.Levels
	err error
}

func (s *peo05ProviderSource) ListGrants(context.Context, string, string) ([]capabilitydomain.CapabilityGrant, error) {
	return s.grants, s.err
}
func (s *peo05ProviderSource) GetCapabilityScope(context.Context, string) (capabilitydomain.CapabilityScope, error) {
	return s.scope, s.err
}
func (s *peo05ProviderSource) ReadPEO05BoundProvider(context.Context, string, string, string, string, time.Time) (repository.PEO05BoundProvider, error) {
	return s.binding, s.err
}
func (s *peo05ProviderSource) GetObservedRelease(context.Context, string, time.Time) (release.ObservedRelease, error) {
	return s.observed, s.err
}
func (s *peo05ProviderSource) GetEngineRelease(context.Context, string) (release.Release, error) {
	return s.engineRelease, s.err
}
func (s *peo05ProviderSource) ListProviderCapabilityCertifications(context.Context, repository.ProviderCapabilityCertificationFilter) ([]certification.Certification, string, error) {
	return s.certs, "", s.err
}
func (s *peo05ProviderSource) HealthLevels(context.Context, string, string, string) (health.Levels, error) {
	return s.health, s.err
}

func peo05ProviderCase() (*peo05ProviderSource, convergence.Plan, time.Time) {
	now := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	key, tenant := "commerce.order.manage", "tn_testa"
	obs := func(subject health.Subject) *health.Observation {
		return &health.Observation{Subject: subject, Status: health.StatusHealthy,
			ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Source: health.SourceActiveProbe}
	}
	p := &peo05ProviderSource{
		grants: []capabilitydomain.CapabilityGrant{{ID: "grant1", TenantID: tenant, CapabilityKey: key,
			ScopeID: "scope1", Status: capabilitydomain.GrantStatusActive, EffectiveFrom: now.Add(-time.Hour)}},
		scope: capabilitydomain.CapabilityScope{TenantID: tenant},
		binding: repository.PEO05BoundProvider{
			ProviderKey: "trade.medusa", CanonicalProviderID: "cp_trade_medusa",
			ProviderUUID: "10000000-0000-7000-8000-000000000001",
			EngineInstanceKey: "ei_trade", EngineInstanceUUID: "20000000-0000-7000-8000-000000000002",
			EngineID: "baobab-trade", ContractMajor: 1, Region: "af-south-1", Environment: "staging",
		},
		observed: release.ObservedRelease{
			State: release.StateRelease, ReleaseID: "erl_trade", EngineInstanceID: "ei_trade", ObservationID: "dob_live",
		},
		engineRelease: release.Release{
			ReleaseID: "erl_trade", EngineID: "baobab-trade", Status: release.StatusApproved,
			ProviderSupport: []release.ProviderSupport{{
				ProviderKey: "trade.medusa", CapabilityKey: key, ContractVersions: []int{1},
			}},
		},
		certs: []certification.Certification{{
			CertificationID: "cert_live", ProviderID: "cp_trade_medusa",
			ProviderKey: "trade.medusa", CapabilityKey: key,
			ContractVersion: 1, ReleaseID: "erl_trade", Status: certification.StatusCertified,
		}},
		health: health.Levels{
			EngineInstance: obs(health.Subject{EngineInstanceID: "20000000-0000-7000-8000-000000000002"}),
			Provider: obs(health.Subject{ProviderID: "10000000-0000-7000-8000-000000000001"}),
			ProviderCapability: obs(health.Subject{ProviderID: "10000000-0000-7000-8000-000000000001", CapabilityKey: key}),
		},
	}
	plan := convergence.Plan{
		TenantID: tenant, ExpiresAt: now.Add(time.Hour),
		Steps: []convergence.Step{
			{Operation: convergence.OpCreateCapabilityGrant, Resources: convergence.StepResources{TenantID: tenant, CapabilityKey: key}},
			{Operation: convergence.OpCreateCapabilityBinding, Resources: convergence.StepResources{
				CapabilityKey: key, ProviderKey: "trade.medusa", EngineInstanceID: "ei_trade", BindingMode: "PRIMARY",
			}},
		},
	}
	return p, plan, now
}

func TestPEO05CProviderNeedsActiveTenantGrantCertifiedObservedReleaseAndHealth(t *testing.T) {
	store, plan, now := peo05ProviderCase()
	reader := PEO05CertifiedProviders{Store: store}
	proofs, err := reader.CurrentMandatoryProviders(context.Background(), "tn_testa", "org_test",
		plan, []string{"commerce.order.manage"}, now)
	if err != nil || len(proofs) != 1 || proofs[0].CertificationReference != "cert_live" ||
		proofs[0].ResidencyRegion != "af-south-1" || proofs[0].TenantID != "tn_testa" {
		t.Fatalf("real authority composition failed: %+v %v", proofs, err)
	}
}

func TestPEO05CProviderFailsClosedWhenAuthorityChanges(t *testing.T) {
	cases := map[string]func(*peo05ProviderSource, *convergence.Plan){
		"revoked grant": func(s *peo05ProviderSource, _ *convergence.Plan) { s.grants[0].Status = capabilitydomain.GrantStatusRevoked },
		"foreign grant": func(s *peo05ProviderSource, _ *convergence.Plan) { s.grants[0].TenantID = "tn_sibling" },
		"foreign scope": func(s *peo05ProviderSource, _ *convergence.Plan) { s.scope.TenantID = "tn_sibling" },
		"provider unknown": func(s *peo05ProviderSource, _ *convergence.Plan) { s.binding.CanonicalProviderID = "" },
		"instance mismatch": func(s *peo05ProviderSource, _ *convergence.Plan) { s.binding.EngineInstanceKey = "ei_other" },
		"drifted release": func(s *peo05ProviderSource, _ *convergence.Plan) { s.observed.State = release.StateMixed },
		"release revoked": func(s *peo05ProviderSource, _ *convergence.Plan) { s.engineRelease.Status = release.StatusRevoked },
		"release contract unsupported": func(s *peo05ProviderSource, _ *convergence.Plan) { s.engineRelease.ProviderSupport[0].ContractVersions = []int{2} },
		"certification revoked": func(s *peo05ProviderSource, _ *convergence.Plan) { s.certs[0].Status = certification.StatusRevoked },
		"certification wrong major": func(s *peo05ProviderSource, _ *convergence.Plan) { s.certs[0].ContractVersion = 2 },
		"health expired": func(s *peo05ProviderSource, _ *convergence.Plan) { s.health.ProviderCapability.ExpiresAt = time.Time{} },
		"provider health absent": func(s *peo05ProviderSource, _ *convergence.Plan) { s.health.Provider = nil },
		"provider registry offline": func(s *peo05ProviderSource, _ *convergence.Plan) { s.err = errors.New("offline") },
		"plan provider substituted": func(_ *peo05ProviderSource, p *convergence.Plan) { p.Steps[1].Resources.ProviderKey = "sibling.provider" },
		"missing plan grant": func(_ *peo05ProviderSource, p *convergence.Plan) { p.Steps = p.Steps[1:] },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			store, plan, now := peo05ProviderCase()
			mutate(store, &plan)
			if _, err := (PEO05CertifiedProviders{Store: store}).CurrentMandatoryProviders(
				context.Background(), "tn_testa", "org_test", plan, []string{"commerce.order.manage"}, now);
				!errors.Is(err, ErrPEO05Integrity) {
				t.Fatalf("stale or foreign provider evidence accepted: %v", err)
			}
		})
	}
}
