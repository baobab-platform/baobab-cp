package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

type privateEvidenceFixture struct {
	value  repository.FederationIdentityEvidence
	calls  int
	onRead func()
}

func (f *privateEvidenceFixture) ReadFederationIdentityEvidence(context.Context, string, string, string, string) (repository.FederationIdentityEvidence, error) {
	f.calls++
	if f.onRead != nil {
		f.onRead()
	}
	return f.value, nil
}

type privateWorkloadRegistry bool

func (r privateWorkloadRegistry) IsActive(string) bool { return bool(r) }

func TestPrivateCanonicalSourceRequiresGrantsAndNeverProvisions(t *testing.T) {
	for _, mode := range []string{"valid", "role-only", "no-registry", "inactive-workload", "revoked-caller", "wrong-scope", "duplicate-json", "unknown-json", "trailing-json", "grant-revoked-during-read"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC()
			repo := repository.NewInMemoryRepository()
			caller := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
			mustNoError(t, repo.CreateIdentity(ctx, caller))
			mustNoError(t, repo.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: caller.ID, Issuer: testRealm, Subject: "authority-reader", Status: "ACTIVE"}))
			human := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
			external := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: human.ID, Issuer: "https://upstream.example", Subject: "stable-human", Status: "ACTIVE"}
			identity := repository.FederationIdentity{Principal: human, ExternalIdentity: external}
			fixture := &privateEvidenceFixture{value: repository.FederationIdentityEvidence{Identity: identity, Reference: domain.ExternalReference{ID: "ref_testcanonical", SystemNamespace: "baobab_cp", EngineID: "baobab-cp", EngineInstanceID: "ei_testcanonical", Environment: "development", NativeEntityType: "canonical_identity_mapping", NativeID: external.ID, SourceAuthority: "reconciliation", Fingerprint: repository.FederationIdentityDigest(identity), Status: "active", LastVerifiedAt: &now}}}
			grant := administration.Grant{GrantID: "agr_testcanonical", PrincipalID: caller.ID, Permission: "security.federation.view", Scope: administration.Scope{Level: administration.LevelPlatform}, GrantType: administration.TypeStanding, Source: administration.SourceDirect, RiskClass: administration.RiskLow, ValidFrom: now.Add(-time.Minute), Status: administration.StatusActive, Version: 1}
			grants := grantsFake{caller.ID: {grant}}
			principal := auth.Principal{Issuer: testRealm, Subject: "authority-reader", ActorType: "workload", ClientID: "iam-authority-reader", Scopes: map[string]struct{}{"federation-authority:read": {}}, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
			var registry auth.WorkloadRegistry = privateWorkloadRegistry(true)
			body := `{"Issuer":"https://upstream.example","Subject":"stable-human"}`
			switch mode {
			case "role-only":
				delete(grants, caller.ID)
			case "no-registry":
				registry = nil
			case "inactive-workload":
				registry = privateWorkloadRegistry(false)
			case "revoked-caller":
				caller.Status = "REVOKED"
				repo.Principals[caller.ID] = caller
			case "wrong-scope":
				grant.Scope = administration.Scope{Level: administration.LevelOrganisation, OrganisationID: "org_unrelated"}
				grants[caller.ID] = []administration.Grant{grant}
			case "duplicate-json":
				body = `{"Issuer":"https://attacker.example","Issuer":"https://upstream.example","Subject":"stable-human"}`
			case "unknown-json":
				body = `{"Issuer":"https://upstream.example","Subject":"stable-human","Approved":true}`
			case "trailing-json":
				body += ` {}`
			case "grant-revoked-during-read":
				fixture.onRead = func() { delete(grants, caller.ID) }
			}
			source := &service.FederationIdentityEvidenceService{Repository: fixture, Environment: "development", EngineInstanceID: "ei_testcanonical", Now: func() time.Time { return now }}
			h := New(Dependencies{Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": principal}, WorkloadRegistry: registry, Identities: repo, AdministrativeGrants: grants, Environment: "development", FederationCanonical: source})
			request := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/identity", strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer caller")
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, request)
			if mode == "valid" {
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), human.ID) || !strings.Contains(w.Body.String(), `"MappingReference":"ref_testcanonical"`) {
					t.Fatal(w.Code, w.Body.String())
				}
			} else if w.Code == http.StatusOK || strings.Contains(w.Body.String(), human.ID) {
				t.Fatal("authority leaked", w.Code, w.Body.String())
			}
			if len(repo.Principals) != 1 || len(repo.ExternalIdentities) != 1 {
				t.Fatal("source provisioned identity")
			}
		})
	}
}
