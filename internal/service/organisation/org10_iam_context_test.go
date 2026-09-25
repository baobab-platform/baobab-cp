package organisation

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestIamOrganisationContextResolution proves ADR-BCP-018 gate ORG-10
// through both context-resolution paths against real PostgreSQL: IAM
// organisation evidence resolves only through an active link to a canonical
// Organisation, which is then attested against the tenant exactly like an
// organisation_id (section 66). An IAM claim is never canonical truth: a
// linked organisation the tenant is not mapped to, an unlinked or retired
// IAM organisation, and a link in another realm all fail closed.
func TestIamOrganisationContextResolution(t *testing.T) {
	e := newEnv(t)
	tenants, err := postgres.Open(e.ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tenants.Close)

	acme, _, acmeReq := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	beta, _, betaReq := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	for _, id := range []string{acmeReq.TenantID, betaReq.TenantID} {
		if _, err := e.admin.Exec(e.ctx, `UPDATE tenants SET observed_state='active' WHERE tenant_id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	realm := "https://id.baobab-platform.test/realms/r" + token()
	kcAcme, kcBeta := domain.NewUUIDv7(), domain.NewUUIDv7()
	link := func(org, issuer, kc string) string {
		t.Helper()
		id, _, err := e.repo.LinkIamOrganisation(e.ctx, domain.IamOrganisationReference{OrganisationID: org, Provider: domain.IamProviderKeycloak,
			Issuer: issuer, ProviderOrganisationID: kc, Status: domain.IamReferenceActive, EffectiveFrom: e.at, SourceAuthority: "control-plane-admin"}, actor())
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	acmeLink := link(acme, realm, kcAcme)
	link(beta, realm, kcBeta)
	evidence := func(issuer, kc string) domain.IamOrganisationEvidence {
		return domain.IamOrganisationEvidence{Provider: domain.IamProviderKeycloak, Issuer: issuer, ProviderOrganisationID: kc}
	}

	svc := service.ContextResolutionService{
		Identity: service.IdentityService{Repository: repository.NewInMemoryRepository(), Provision: service.WorkloadOnlyProvisioningPolicy},
		Tenants:  tenants, Canonical: e.repo, Mappings: e.repo, IamOrganisations: e.repo,
	}
	resolvers := map[string]func(tenantID string, ev domain.IamOrganisationEvidence) (string, string, error){
		"provisioning resolver": func(tenantID string, ev domain.IamOrganisationEvidence) (string, string, error) {
			r := provisioning.NewAuthoritativeContextResolver(provisioning.ContextAuthorityAdapter{Tenants: tenants, Repo: e.repo})
			got, err := r.Resolve(e.ctx, provisioning.ContextResolutionRequest{PrincipalID: "principal-1", TenantID: tenantID,
				IamOrganization: &ev, CorrelationID: domain.NewUUIDv7()})
			return got.OrganisationID, got.Provenance["organisation_id"].Source, err
		},
		"context resolution service": func(tenantID string, ev domain.IamOrganisationEvidence) (string, string, error) {
			principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.baobab-platform.test/realms/baobab", ActorType: "workload",
				TenantID: tenantID, ClientID: "baobab-trade", TokenID: "token-" + token()}
			_, got, err := svc.ResolveWithIamOrganisation(e.ctx, principal, tenantID, ev, domain.NewUUIDv7(), time.Now().UTC())
			return got.OrganisationID, got.Provenance["organisation_id"].Source, err
		},
	}

	checks := []struct {
		name     string
		tenantID string
		ev       domain.IamOrganisationEvidence
		want     string
		setup    func(t *testing.T)
	}{
		{name: "own IAM organisation", tenantID: acmeReq.TenantID, ev: evidence(realm, kcAcme), want: acme},
		{name: "another tenant's linked IAM organisation", tenantID: acmeReq.TenantID, ev: evidence(realm, kcBeta)},
		{name: "unlinked IAM organisation", tenantID: acmeReq.TenantID, ev: evidence(realm, domain.NewUUIDv7())},
		{name: "own IAM organisation id under another realm", tenantID: acmeReq.TenantID, ev: evidence(realm+"-other", kcAcme)},
		{name: "canonical organisation id presented as IAM evidence", tenantID: acmeReq.TenantID, ev: evidence(realm, acme)},
		{name: "non-https issuer", tenantID: acmeReq.TenantID, ev: evidence(strings.Replace(realm, "https://", "http://", 1), kcAcme)},
		{
			name: "retired link", tenantID: acmeReq.TenantID, ev: evidence(realm, kcAcme),
			setup: func(t *testing.T) {
				if err := e.repo.RetireIamOrganisationReference(e.ctx, acmeLink, time.Now().UTC().Add(-time.Minute), "realm decommissioned", actor()); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, c := range checks {
		if c.setup != nil {
			c.setup(t)
		}
		for name, resolve := range resolvers {
			got, source, err := resolve(c.tenantID, c.ev)
			switch {
			case c.want != "" && (err != nil || got != c.want || source != "baobab-cp:iam-organisation-reference"):
				t.Errorf("%s / %s: got %q (source %q), %v; want %s from the IAM link", name, c.name, got, source, err, c.want)
			case c.want == "" && err == nil:
				t.Errorf("%s / %s: expected to fail closed, resolved %q", name, c.name, got)
			}
		}
	}

	// The API service reports unresolvable evidence distinctly, and never
	// resolves when no link repository is configured.
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.baobab-platform.test/realms/baobab", ActorType: "workload", ClientID: "baobab-trade", TokenID: "t"}
	if _, _, err := svc.ResolveWithIamOrganisation(e.ctx, principal, acmeReq.TenantID, evidence(realm, domain.NewUUIDv7()), domain.NewUUIDv7(), time.Now().UTC()); !errors.Is(err, service.ErrOrganisationNotResolved) {
		t.Fatalf("unlinked evidence: %v; want ErrOrganisationNotResolved", err)
	}
	unwired := svc
	unwired.IamOrganisations = nil
	if _, _, err := unwired.ResolveWithIamOrganisation(e.ctx, principal, betaReq.TenantID, evidence(realm, kcBeta), domain.NewUUIDv7(), time.Now().UTC()); !errors.Is(err, service.ErrOrganisationNotResolved) {
		t.Fatalf("no resolver configured: %v; want ErrOrganisationNotResolved", err)
	}
	// Both identifiers at once are refused rather than one silently winning.
	r := provisioning.NewAuthoritativeContextResolver(provisioning.ContextAuthorityAdapter{Tenants: tenants, Repo: e.repo})
	ev := evidence(realm, kcBeta)
	if _, err := r.Resolve(e.ctx, provisioning.ContextResolutionRequest{PrincipalID: "p", TenantID: betaReq.TenantID, OrganisationID: beta,
		IamOrganization: &ev, CorrelationID: domain.NewUUIDv7()}); err == nil {
		t.Fatal("supplying organisation_id and IAM evidence together must fail")
	}
}
