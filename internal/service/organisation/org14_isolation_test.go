package organisation

import (
	"os"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/provisioning"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// TestOrganisationContextIsolation proves ADR-BCP-018 gate ORG-14 through
// both context-resolution paths against real PostgreSQL: a parent company,
// a sibling in the same CorporateGroup and a co-member of the same
// PlatformAccount gain no access to another organisation's context
// (sections 29, 41, 60, 89), and an IAM or external identifier is never
// accepted as an organisation_id (sections 67, 95). Only an ACTIVE, in-effect
// TenantOrganisationMapping attests a generic organisation.
func TestOrganisationContextIsolation(t *testing.T) {
	e := newEnv(t)
	tenants, err := postgres.Open(e.ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tenants.Close)
	now := time.Now().UTC()

	parent, parentRes, parentReq := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	sub, subRes, subReq := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	sibling, _, siblingReq := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	for _, id := range []string{parentReq.TenantID, subReq.TenantID, siblingReq.TenantID} {
		if _, err := e.admin.Exec(e.ctx, `UPDATE tenants SET observed_state='active' WHERE tenant_id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	if parentRes.TenantOrganisationMapping == "" || subRes.TenantOrganisationMapping == "" {
		t.Fatal("provisioning must map each tenant to its organisation")
	}

	// The parent verifiably owns both subsidiaries, which therefore form a
	// derived CorporateGroup, and both subsidiaries share a PlatformAccount.
	pct := 100.0
	for _, target := range []string{sub, sibling} {
		id, err := e.repo.EnsureCorporateRelationship(e.ctx, domain.CorporateRelationship{
			SourceOrganisationID: parent, TargetOrganisationID: target, RelationshipType: domain.CorpRelOwns,
			OwnershipPercentage: &pct, DirectOrDerived: domain.CorporateFactDirect, VerificationState: domain.VerificationPendingReview,
			Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "admission-review"}, actor())
		if err != nil {
			t.Fatal(err)
		}
		if err := e.repo.VerifyCorporateRelationship(e.ctx, id, e.evidence(), actor()); err != nil {
			t.Fatal(err)
		}
	}
	groupID := domain.NewResourceID(domain.CorporateGroupIDPrefix)
	if err := e.repo.CreateCorporateGroup(e.ctx, domain.CorporateGroup{ID: groupID, DisplayName: "Isolation Group", RootOrganisationID: parent,
		Status: "ACTIVE", GroupingPolicy: domain.GroupPolicyVerifiedControlMajority, EffectiveFrom: e.at}, actor()); err != nil {
		t.Fatal(err)
	}
	if report, err := (&CorporateGroupDeriver{Orgs: e.repo, Now: func() time.Time { return now }}).Derive(e.ctx, groupID, actor()); err != nil || len(report.Added) != 3 {
		t.Fatalf("derive group: %+v %v", report, err)
	}
	accountID := domain.NewResourceID(domain.PlatformAccountIDPrefix)
	if err := e.repo.CreatePlatformAccount(e.ctx, domain.PlatformAccount{ID: accountID, DisplayName: "Shared Account",
		PrimaryOrganisationID: sub, Status: "ACTIVE", EffectiveFrom: e.at}, actor()); err != nil {
		t.Fatal(err)
	}
	for org, role := range map[string]string{sub: domain.AccountRolePrimaryOrganisation, sibling: domain.AccountRoleContractingParty} {
		if _, err := e.repo.EnsurePlatformAccountMembership(e.ctx, domain.PlatformAccountMembership{PlatformAccountID: accountID,
			OrganisationID: org, AccountRole: role, Status: "ACTIVE", EffectiveFrom: e.at}, actor()); err != nil {
			t.Fatal(err)
		}
	}

	// A generic organisation registered by the parent's tenant but never
	// mapped to it: ownership of the row attests nothing.
	var unmapped string
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO registry.canonical_entity (entity_type, status, tenant_id) VALUES ('ORGANISATION','active',$1) RETURNING canonical_entity_id::text`,
		parentReq.TenantID).Scan(&unmapped); err != nil {
		t.Fatal(err)
	}

	resolvers := map[string]func(tenantID, organisationID string) (string, error){
		"provisioning resolver": func(tenantID, organisationID string) (string, error) {
			r := provisioning.NewAuthoritativeContextResolver(provisioning.ContextAuthorityAdapter{Tenants: tenants, Repo: e.repo})
			got, err := r.Resolve(e.ctx, provisioning.ContextResolutionRequest{PrincipalID: "principal-1", TenantID: tenantID,
				OrganisationID: organisationID, CorrelationID: domain.NewUUIDv7()})
			return got.OrganisationID, err
		},
		"context resolution service": func(tenantID, organisationID string) (string, error) {
			svc := service.ContextResolutionService{
				Identity: service.IdentityService{Repository: repository.NewInMemoryRepository(), Provision: service.WorkloadOnlyProvisioningPolicy},
				Tenants:  tenants, Canonical: e.repo, Mappings: e.repo,
			}
			principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.baobab-platform.test/realms/baobab", ActorType: "workload",
				TenantID: tenantID, ClientID: "baobab-trade", TokenID: "token-" + token()}
			_, got, err := svc.Resolve(e.ctx, principal, tenantID, organisationID, domain.NewUUIDv7(), time.Now().UTC())
			return got.OrganisationID, err
		},
	}

	type check struct {
		name           string
		tenantID, org  string
		allow          bool
		setup, cleanup func(t *testing.T)
	}
	var secondMapping string
	checks := []check{
		{name: "own mapped organisation", tenantID: subReq.TenantID, org: sub, allow: true},
		{name: "parent company's tenant requesting its subsidiary", tenantID: parentReq.TenantID, org: sub},
		{name: "subsidiary's tenant requesting its parent", tenantID: subReq.TenantID, org: parent},
		{name: "group sibling requesting another member", tenantID: siblingReq.TenantID, org: sub},
		{name: "platform account co-member requesting another member", tenantID: subReq.TenantID, org: sibling},
		{name: "registering tenant without a mapping", tenantID: parentReq.TenantID, org: unmapped},
		{name: "IAM subject as organisation_id", tenantID: subReq.TenantID, org: "baobab-trade"},
		{name: "legal entity identifier as organisation_id", tenantID: subReq.TenantID, org: subReq.LegalEntityID},
		{name: "mapping identifier as organisation_id", tenantID: subReq.TenantID, org: subRes.TenantOrganisationMapping},
		{name: "corporate group identifier as organisation_id", tenantID: subReq.TenantID, org: groupID},
		{name: "platform account identifier as organisation_id", tenantID: subReq.TenantID, org: accountID},
		{
			name: "second tenant with an explicit mapping", tenantID: siblingReq.TenantID, org: sub, allow: true,
			setup: func(t *testing.T) {
				var err error
				secondMapping, err = e.repo.EnsureTenantOrganisationMapping(e.ctx, domain.TenantOrganisationMapping{TenantID: siblingReq.TenantID,
					OrganisationID: sub, MappingRole: domain.TenantOrgRoleAdditional, Status: domain.RelationshipStatusActive,
					EffectiveFrom: e.at, Provenance: "governed-shared-services-agreement"}, actor())
				if err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "ended mapping, even for the organisation's own tenant", tenantID: subReq.TenantID, org: sub,
			setup: func(t *testing.T) {
				for _, id := range []string{secondMapping, subRes.TenantOrganisationMapping} {
					row, err := domain.ParseResourceID(domain.TenantOrganisationMappingIDPrefix, id)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := e.admin.Exec(e.ctx, `UPDATE registry.tenant_organisation_mapping SET status='ENDED', effective_to=$2 WHERE tenant_organisation_mapping_id=$1::uuid`,
						row, now.Add(-time.Minute)); err != nil {
						t.Fatal(err)
					}
				}
			},
		},
	}
	for _, c := range checks {
		if c.setup != nil {
			c.setup(t)
		}
		for name, resolve := range resolvers {
			got, err := resolve(c.tenantID, c.org)
			switch {
			case c.allow && (err != nil || got != c.org):
				t.Errorf("%s / %s: expected %s to be attested, got %q, %v", name, c.name, c.org, got, err)
			case !c.allow && err == nil:
				t.Errorf("%s / %s: expected organisation_id %s to fail closed, got context for %q", name, c.name, c.org, got)
			}
		}
	}
}
