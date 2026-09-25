package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func iamLink(org, issuer, providerOrgID string, from time.Time) domain.IamOrganisationReference {
	return domain.IamOrganisationReference{OrganisationID: org, Provider: domain.IamProviderKeycloak, Issuer: issuer,
		ProviderOrganisationID: providerOrgID, Status: domain.IamReferenceActive, EffectiveFrom: from, SourceAuthority: "control-plane-admin"}
}

// TestIamOrganisationLinks proves ADR-BCP-018 gate ORG-10's link rules
// against real PostgreSQL: many IAM organisations per canonical
// Organisation (section 65), exactly one Organisation per issuer and provider
// organisation, idempotent linking, retire-not-delete, and fail-closed
// resolution (section 66).
func TestIamOrganisationLinks(t *testing.T) {
	f := newOrgFixture(t)
	acme, other := f.organisation(t, "Acme"), f.organisation(t, "Other")
	realm := "https://id.baobab-platform.test/realms/r" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12]
	euRealm := realm + "-eu"
	kcOrg := domain.NewUUIDv7()
	now := f.at.Add(time.Hour)

	first := f.actor()
	id, created, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(acme, realm, kcOrg, f.at), first)
	if err != nil || !created || !strings.HasPrefix(id, "iamorg_") {
		t.Fatalf("link: id=%q created=%v err=%v", id, created, err)
	}
	if audits, events, _ := f.recorded(t, first); audits != 1 || events != 0 {
		t.Fatalf("link recorded audits=%d events=%d; want one audit row and no event", audits, events)
	}
	replay := f.actor()
	again, created, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(acme, realm, kcOrg, f.at), replay)
	if err != nil || created || again != id {
		t.Fatalf("replayed link: id=%q created=%v err=%v; want %s, not created", again, created, err, id)
	}
	if audits, _, _ := f.recorded(t, replay); audits != 0 {
		t.Fatalf("a replayed link wrote %d audit rows", audits)
	}

	// Section 65: the same Organisation in a second realm is a second link;
	// the same provider id under another issuer is a different IAM organisation.
	eu, _, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(acme, euRealm, kcOrg, f.at), f.actor())
	if err != nil || eu == id {
		t.Fatalf("second realm link: %q %v", eu, err)
	}
	if _, _, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(other, realm, kcOrg, f.at), f.actor()); !errors.Is(err, ErrIamOrganisationAlreadyLinked) {
		t.Fatalf("linking an IAM organisation held by another Organisation: %v", err)
	}

	resolve := func(issuer, provider string, at time.Time) (string, error) {
		return f.repo.ResolveIamOrganisation(f.ctx, domain.IamOrganisationEvidence{Provider: domain.IamProviderKeycloak, Issuer: issuer, ProviderOrganisationID: provider}, at)
	}
	for _, issuer := range []string{realm, euRealm} {
		if got, err := resolve(issuer, kcOrg, now); err != nil || got != acme {
			t.Fatalf("resolve %s: %q %v", issuer, got, err)
		}
	}
	for name, try := range map[string]func() (string, error){
		"unknown provider organisation": func() (string, error) { return resolve(realm, domain.NewUUIDv7(), now) },
		"unknown issuer":                func() (string, error) { return resolve(realm+"-other", kcOrg, now) },
		"before the link took effect":   func() (string, error) { return resolve(realm, kcOrg, f.at.Add(-time.Minute)) },
	} {
		if got, err := try(); !errors.Is(err, ErrIamOrganisationNotLinked) {
			t.Errorf("%s: resolved %q, err=%v; want ErrIamOrganisationNotLinked", name, got, err)
		}
	}
	if _, err := resolve("http://id.baobab-platform.test/realms/x", kcOrg, now); err == nil {
		t.Error("evidence with a non-https issuer must be rejected")
	}

	// Retire, never delete: the retired link stays listed and stops
	// resolving, and the IAM organisation may then be linked elsewhere.
	if err := f.repo.RetireIamOrganisationReference(f.ctx, id, now, "realm decommissioned", f.actor()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(realm, kcOrg, now.Add(time.Minute)); !errors.Is(err, ErrIamOrganisationNotLinked) {
		t.Fatalf("a retired link must not resolve: %v", err)
	}
	if err := f.repo.RetireIamOrganisationReference(f.ctx, id, now, "again", f.actor()); err == nil {
		t.Fatal("retiring twice must fail")
	}
	refs, err := f.repo.ListIamOrganisationReferences(f.ctx, acme)
	if err != nil || len(refs) != 2 || refs[0].Status != domain.IamReferenceRetired || refs[0].EffectiveTo == nil || refs[1].Status != domain.IamReferenceActive {
		t.Fatalf("links after retirement: %+v %v", refs, err)
	}
	if _, created, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(other, realm, kcOrg, now), f.actor()); err != nil || !created {
		t.Fatalf("relinking a retired IAM organisation elsewhere: created=%v %v", created, err)
	}

	// Only organisations can be linked.
	var product string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO registry.canonical_entity (entity_type, status) VALUES ($1, 'active') RETURNING canonical_entity_id::text`,
		domain.EntityTypeProduct).Scan(&product); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(product, realm, domain.NewUUIDv7(), f.at), f.actor()); !errors.Is(err, ErrNotAnOrganisation) {
		t.Fatalf("linking a product: %v", err)
	}
	if _, _, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(domain.NewUUIDv7(), realm, domain.NewUUIDv7(), f.at), f.actor()); !errors.Is(err, ErrCanonicalEntityNotFound) {
		t.Fatalf("linking an unknown entity: %v", err)
	}

	// The database itself refuses a second active link for one IAM organisation.
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO registry.iam_organisation_reference (organisation_id, provider, issuer,
		provider_organisation_id, effective_from, source_authority) VALUES ($1::uuid, 'keycloak', $2, $3, $4, 'test')`,
		acme, euRealm, kcOrg, f.at); err == nil {
		t.Fatal("a second ACTIVE link for the same issuer and provider organisation must violate the unique index")
	}
}

// TestExternalReferenceLookupFailsClosedOnAmbiguity: the generic
// registry.external_reference table is unique only per canonical entity, so
// one provider key linked to two entities must not resolve to either.
func TestExternalReferenceLookupFailsClosedOnAmbiguity(t *testing.T) {
	f := newOrgFixture(t)
	a, b := f.organisation(t, "A"), f.organisation(t, "B")
	native := "kc-org-" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:16]
	link := func(entity string) {
		t.Helper()
		if _, err := f.repo.CreateExternalReference(f.ctx, domain.ExternalReference{CanonicalEntityID: entity, EngineID: "baobab-iam",
			NativeType: "keycloak_organization", NativeID: native}); err != nil {
			t.Fatal(err)
		}
	}
	link(a)
	if got, err := f.repo.GetCanonicalEntityByExternalReference(f.ctx, "baobab-iam", "keycloak_organization", native); err != nil || got.ID != a {
		t.Fatalf("single link: %+v %v", got, err)
	}
	link(b)
	if got, err := f.repo.GetCanonicalEntityByExternalReference(f.ctx, "baobab-iam", "keycloak_organization", native); !errors.Is(err, ErrExternalReferenceAmbiguous) {
		t.Fatalf("ambiguous link resolved to %+v, err=%v", got, err)
	}
}

// TestIamOrganisationReferencesConformToSharedContract validates the links
// and evidence the Control Plane produces against baobab-platform/shared
// contracts/organisation/v1/iam.schema.json. It skips while the pinned Shared
// revision predates that file.
func TestIamOrganisationReferencesConformToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	if _, err := os.Stat(filepath.Join(dir, "contracts", "organisation", "v1", "iam.schema.json")); err != nil {
		t.Skip("pinned baobab-platform/shared revision has no contracts/organisation/v1/iam.schema.json yet")
	}
	f := newOrgFixture(t)
	org := f.organisation(t, "Contract")
	realm := "https://id.baobab-platform.test/realms/c" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12]
	id, _, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(org, realm, domain.NewUUIDv7(), f.at), f.actor())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.RetireIamOrganisationReference(f.ctx, id, f.at.Add(time.Hour), "contract test", f.actor()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.repo.LinkIamOrganisation(f.ctx, iamLink(org, realm+"-eu", domain.NewUUIDv7(), f.at), f.actor()); err != nil {
		t.Fatal(err)
	}
	refs, err := f.repo.ListIamOrganisationReferences(f.ctx, org)
	if err != nil || len(refs) != 2 {
		t.Fatalf("links: %+v %v", refs, err)
	}
	schema := contracttest.CompileSchema(t, dir, "organisation/v1/iam.schema.json#/$defs/IamOrganisationReference")
	for _, ref := range refs {
		contracttest.ValidateJSON(t, schema, ref)
	}
	contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "organisation/v1/iam.schema.json#/$defs/IamOrganisationEvidence"), refs[0].Evidence())
}
