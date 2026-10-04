package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type federationReader struct {
	result repository.FederationIdentity
	err    error
}

func (f *federationReader) ReadFederationIdentity(context.Context, string, string) (repository.FederationIdentity, error) {
	return f.result, f.err
}
func seedFederation(t *testing.T, repo *repository.Repository, issuer, subject string) repository.FederationIdentity {
	t.Helper()
	p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	e := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: issuer, Subject: subject, Status: "ACTIVE", CreatedAt: time.Now().UTC()}
	if err := repo.CreateIdentity(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkExternalIdentity(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return repository.FederationIdentity{Principal: p, ExternalIdentity: e}
}
func TestFederationReadDoesNotProvisionOrLinkByEmail(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	subject := "same@example.test"
	a := seedFederation(t, repo, "https://one.example.test", subject)
	b := seedFederation(t, repo, "urn:example:saml:two", subject)
	s := FederationIdentityService{repo}
	for _, want := range []repository.FederationIdentity{a, b} {
		got, err := s.Resolve(context.Background(), want.ExternalIdentity.Issuer, subject)
		if err != nil || got.Principal.ID != want.Principal.ID {
			t.Fatal("issuer+subject resolution failed")
		}
	}
	if a.Principal.ID == b.Principal.ID {
		t.Fatal("distinct issuers were linked")
	}
	before := len(repo.Principals)
	if _, err := s.Resolve(context.Background(), "https://unknown.example.test", subject); !errors.Is(err, ErrFederationIdentityUnresolved) {
		t.Fatal("unknown identity not unresolved")
	}
	if len(repo.Principals) != before || len(repo.ExternalIdentities) != 2 {
		t.Fatal("resolution provisioned identity")
	}
}
func TestFederationReadRejectsNonactiveOrInconsistentRecords(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	mapping := seedFederation(t, repo, "https://idp.example.test", "subject")
	mutations := []func(*repository.FederationIdentity){
		func(m *repository.FederationIdentity) { m.Principal.Status = "SUSPENDED" }, func(m *repository.FederationIdentity) { m.ExternalIdentity.Status = "REVOKED" },
		func(m *repository.FederationIdentity) { m.Principal.ActorType = "workload" }, func(m *repository.FederationIdentity) { m.ExternalIdentity.PrincipalID = domain.NewPrincipalID() },
		func(m *repository.FederationIdentity) { m.ExternalIdentity.Issuer = "https://other.example.test" }, func(m *repository.FederationIdentity) { m.ExternalIdentity.Subject = "other" },
		func(m *repository.FederationIdentity) { m.Principal.ID = "orphan:pending-review" },
	}
	for _, mutate := range mutations {
		m := mapping
		mutate(&m)
		s := FederationIdentityService{&federationReader{result: m}}
		got, err := s.Resolve(context.Background(), mapping.ExternalIdentity.Issuer, mapping.ExternalIdentity.Subject)
		if !errors.Is(err, ErrFederationIdentityDenied) || got.Principal.ID != "" {
			t.Fatal("inconsistent mapping accepted")
		}
	}
}
func TestFederationReadRedactsStorageFailureAndRejectsNil(t *testing.T) {
	for _, s := range []FederationIdentityService{{}, {Repository: (*federationReader)(nil)}, {Repository: &federationReader{err: errors.New("database password-sensitive-error")}}} {
		_, err := s.Resolve(context.Background(), "https://idp.example.test", "subject")
		if !errors.Is(err, ErrFederationIdentityUnavailable) || strings.Contains(err.Error(), "password") {
			t.Fatal("storage failure misclassified or leaked")
		}
	}
	repo := repository.NewInMemoryRepository()
	s := FederationIdentityService{repo}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Resolve(ctx, "https://idp.example.test", "subject"); err == nil {
		t.Fatal("cancelled request accepted")
	}
}
func TestFederationReadMatchesPinnedIdentityContracts(t *testing.T) {
	shared := contracttest.SharedDir(t)
	repo := repository.NewInMemoryRepository()
	m := seedFederation(t, repo, "urn:example:saml:idp", "stable-subject")
	s := FederationIdentityService{repo}
	got, err := s.Resolve(context.Background(), m.ExternalIdentity.Issuer, m.ExternalIdentity.Subject)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.ValidateJSON(t, contracttest.CompileSchema(t, shared, "identity/v1/principal.schema.json"), got.Principal)
	contracttest.ValidateJSON(t, contracttest.CompileSchema(t, shared, "identity/v1/external-identity.schema.json"), got.ExternalIdentity)
}

func TestFederationMP2BoundaryReusesCPIdentifierContracts(t *testing.T) {
	shared := contracttest.SharedDir(t)
	for _, protocol := range []string{"oidc", "saml2"} {
		data, err := os.ReadFile(filepath.Join(shared, "contracts/identity/v1/examples/federation-"+protocol+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var bundle map[string]any
		if err := json.Unmarshal(data, &bundle); err != nil {
			t.Fatal(err)
		}
		for field, name := range map[string]string{"trust": "federation-trust.schema.json", "external_principal": "external-principal.schema.json", "assurance": "federated-assurance.schema.json"} {
			contracttest.ValidateJSON(t, contracttest.CompileSchema(t, shared, "identity/v1/"+name), bundle[field])
		}
		trust := bundle["trust"].(map[string]any)
		binding := trust["provider_binding"].(map[string]any)
		if !domain.ValidProviderID(binding["provider_id"].(string)) || !domain.ValidEngineInstanceID(binding["engine_instance_id"].(string)) {
			t.Fatal("federation binding diverges from existing CP IDs")
		}
	}
}
