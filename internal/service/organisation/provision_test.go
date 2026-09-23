// Target path: baobab-platform/baobab-cp/internal/service/organisation/provision_test.go

package organisation

import (
	"context"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// memoryOrgRepo is a minimal in-memory OrganisationRepository for unit tests.
type memoryOrgRepo struct {
	orgs     map[string]domain.Organisation
	leps     map[string]domain.LegalEntityProfile
	tlems    []domain.TenantLegalEntityMapping
	toms     []domain.TenantOrganisationMapping
	platform []domain.PlatformRelationship
}

func newMemoryOrgRepo() *memoryOrgRepo {
	return &memoryOrgRepo{
		orgs: map[string]domain.Organisation{},
		leps: map[string]domain.LegalEntityProfile{},
	}
}

func (m *memoryOrgRepo) UpsertOrganisation(ctx context.Context, org domain.Organisation) error {
	m.orgs[org.CanonicalEntityID] = org
	return nil
}
func (m *memoryOrgRepo) GetOrganisation(ctx context.Context, id string) (*domain.Organisation, error) {
	o, ok := m.orgs[id]
	if !ok {
		return nil, nil
	}
	return &o, nil
}
func (m *memoryOrgRepo) UpsertLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile) error {
	m.leps[lep.LegalEntityID] = lep
	return nil
}
func (m *memoryOrgRepo) GetLegalEntityProfile(ctx context.Context, id string) (*domain.LegalEntityProfile, error) {
	l, ok := m.leps[id]
	if !ok {
		return nil, nil
	}
	return &l, nil
}
func (m *memoryOrgRepo) ListLegalEntityProfilesByOrganisation(ctx context.Context, organisationID string) ([]domain.LegalEntityProfile, error) {
	return nil, nil
}
func (m *memoryOrgRepo) UpsertCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship) error {
	return nil
}
func (m *memoryOrgRepo) ListCorporateRelationshipsByOrganisation(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error) {
	return nil, nil
}
func (m *memoryOrgRepo) UpsertCorporateGroup(ctx context.Context, g domain.CorporateGroup) error {
	return nil
}
func (m *memoryOrgRepo) UpsertCorporateGroupMembership(ctx context.Context, mem domain.CorporateGroupMembership) error {
	return nil
}
func (m *memoryOrgRepo) ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error) {
	return nil, nil
}
func (m *memoryOrgRepo) UpsertPlatformRelationship(ctx context.Context, rel domain.PlatformRelationship) error {
	m.platform = append(m.platform, rel)
	return nil
}
func (m *memoryOrgRepo) GetPlatformRelationship(ctx context.Context, organisationID string, at time.Time) (*domain.PlatformRelationship, error) {
	for i := range m.platform {
		if m.platform[i].OrganisationID == organisationID {
			return &m.platform[i], nil
		}
	}
	return nil, nil
}
func (m *memoryOrgRepo) UpsertPlatformAccount(ctx context.Context, acct domain.PlatformAccount) error {
	return nil
}
func (m *memoryOrgRepo) UpsertPlatformAccountMembership(ctx context.Context, mem domain.PlatformAccountMembership) error {
	return nil
}
func (m *memoryOrgRepo) ListPlatformAccountMemberships(ctx context.Context, accountID string, at time.Time) ([]domain.PlatformAccountMembership, error) {
	return nil, nil
}
func (m *memoryOrgRepo) UpsertTenantOrganisationMapping(ctx context.Context, mapping domain.TenantOrganisationMapping) error {
	m.toms = append(m.toms, mapping)
	return nil
}
func (m *memoryOrgRepo) ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error) {
	return m.toms, nil
}
func (m *memoryOrgRepo) UpsertTenantLegalEntityMapping(ctx context.Context, mapping domain.TenantLegalEntityMapping) error {
	m.tlems = append(m.tlems, mapping)
	return nil
}
func (m *memoryOrgRepo) ListTenantLegalEntityMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantLegalEntityMapping, error) {
	return m.tlems, nil
}
func (m *memoryOrgRepo) SetDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, mappingID string) error {
	return nil
}

func TestProvisionTenantOrganisation_ExternalClient(t *testing.T) {
	repo := newMemoryOrgRepo()
	n := 0
	p := &Provisioner{
		Orgs: repo,
		NewID: func() string {
			n++
			return "id-" + string(rune('0'+n))
		},
	}
	at := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	err := p.ProvisionTenantOrganisation(context.Background(), ProvisionRequest{
		TenantID:             "tn_acme_foods",
		CanonicalEntityID:    "ce_acme_foods",
		LegalEntityID:        "ACME-FOODS",
		DisplayName:          "Acme Foods",
		SourceAuthority:      "control-plane-admission",
		PlatformRelType:      domain.PlatformRelExternalClient,
		PlatformRelAuthority: "control-plane-admission",
		EffectiveFrom:        at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.orgs["ce_acme_foods"]; !ok {
		t.Fatal("expected organisation profile")
	}
	if _, ok := repo.leps["ACME-FOODS"]; !ok {
		t.Fatal("expected legal entity profile")
	}
	if len(repo.tlems) != 1 || !repo.tlems[0].IsDefault {
		t.Fatal("expected default tenant legal entity mapping")
	}
	if len(repo.platform) != 1 || repo.platform[0].RelationshipType != domain.PlatformRelExternalClient {
		t.Fatal("expected EXTERNAL_CLIENT platform relationship")
	}
}

func TestProvisionRejectsPrivilegedPlatformRelFromApplicant(t *testing.T) {
	repo := newMemoryOrgRepo()
	p := &Provisioner{Orgs: repo, NewID: func() string { return "x" }}
	err := p.ProvisionTenantOrganisation(context.Background(), ProvisionRequest{
		TenantID:             "tn_x",
		CanonicalEntityID:    "ce_x",
		LegalEntityID:        "LE-X",
		DisplayName:          "X",
		SourceAuthority:      "applicant-submission",
		PlatformRelType:      domain.PlatformRelOwner,
		PlatformRelAuthority: "applicant-submission",
		EffectiveFrom:        time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected reject of PLATFORM_OWNER with applicant authority")
	}
}

func TestIsFirstPartyLegalEntity(t *testing.T) {
	if !IsFirstPartyLegalEntity("ZURIBEANS") {
		t.Fatal("expected ZURIBEANS first-party")
	}
	if IsFirstPartyLegalEntity("ACME-FOODS") {
		t.Fatal("ACME-FOODS must not be first-party")
	}
}
