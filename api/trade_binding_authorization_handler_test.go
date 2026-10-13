package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

func TestTradeBindingIntakeAdmission(t *testing.T) {
	for _, scenario := range []string{"allowed", "allowed-test", "allowed-sandbox", "allowed-development", "allowed-staging", "scope-only", "workload", "sibling", "delegation", "revoked", "weak-assurance", "inactive-human", "expired-token", "foreign-issuer", "production", "off", "unknown-field"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now().UTC()
			ctx := context.Background()
			repo := repository.NewInMemoryRepository()
			human := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
			if scenario == "inactive-human" {
				human.Status = "SUSPENDED"
			}
			mustNoError(t, repo.CreateIdentity(ctx, human))
			mustNoError(t, repo.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: human.ID, Issuer: testRealm, Subject: "maker", Status: "ACTIVE"}))
			org := domain.NewUUIDv7()
			p := auth.Principal{Issuer: testRealm, Subject: "maker", ActorType: "human", TokenID: "token-binding", ExpiresAt: now.Add(time.Minute), Scopes: map[string]struct{}{"trade-binding:propose": {}}, Assurance: auth.Assurance{ACR: "2", AuthenticatedAt: now}}
			grant := administration.Grant{GrantID: domain.NewUUIDv7(), PrincipalID: human.ID, Permission: "organisation.trade-binding.propose", Scope: administration.Scope{Level: administration.LevelOrganisation, OrganisationID: org}, RiskClass: administration.RiskHigh, Status: administration.StatusActive, ValidFrom: now.Add(-time.Minute), Source: administration.SourceDirect, GrantType: administration.TypeStanding}
			grants := []administration.Grant{grant}
			env := "integration"
			enabled := true
			switch scenario {
			case "allowed-test":
				env = "test"
			case "allowed-sandbox":
				env = "sandbox"
			case "allowed-development":
				env = "development"
			case "allowed-staging":
				env = "staging"
			case "scope-only":
				grants = nil
			case "workload":
				p.ActorType = "workload"
			case "sibling":
				grants[0].Scope.OrganisationID = domain.NewUUIDv7()
			case "delegation":
				grants[0].Source = administration.SourceDelegation
			case "revoked":
				grants[0].Status = administration.StatusRevoked
			case "weak-assurance":
				p.Assurance.ACR = "1"
			case "expired-token":
				p.ExpiresAt = now.Add(-time.Second)
			case "foreign-issuer":
				p.Issuer = "https://foreign.example.test"
			case "production":
				env = "production"
			case "off":
				enabled = false
			}
			h := New(Dependencies{Store: &fakeStore{}, Environment: env, TradeBindingIntakeEnabled: enabled, AdminVerifier: fakeVerifier{principal: p}, Identities: repo, AdministrativeGrants: governanceGrantReader{grants: map[string][]administration.Grant{human.ID: grants}}})
			body := map[string]any{"scope": tradeBindingScope{CartID: "cart-test", TenantID: "tn_test01", OrganisationID: org, ResponsibleLegalEntityID: "le_test", MarketCode: "ZA", CurrencyCode: "ZAR", SalesChannelID: "channel-test", RegionID: "region-test"}, "command_digest": strings.Repeat("a", 64)}
			if scenario == "unknown-field" {
				body["actor"] = "forged"
			}
			data, _ := json.Marshal(body)
			r := httptest.NewRequest(http.MethodPost, "/internal/trade-binding/v1/propose/authorize", bytes.NewReader(data))
			r.Header.Set("Authorization", "Bearer fixture")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if !strings.HasPrefix(scenario, "allowed") {
				if w.Code < 400 {
					t.Fatalf("denial bypassed: %d %s", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != 200 {
				t.Fatalf("admission failed: %d %s", w.Code, w.Body.String())
			}
			var result tradeBindingAuthorizationResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.PrincipalID != human.ID || result.Issuer != p.Issuer || result.TokenID != p.TokenID || result.ProviderPermissionsGranted || result.ValidUntil.Sub(result.EvaluatedAt) > 5*time.Second || result.Scope != body["scope"] {
				t.Fatalf("invalid decision: %+v", result)
			}
			// A second request must read CURRENT grants; no successful token cache.
			// The reader shares the slice, matching removal/suspension in the source.
			grants[0].Status = administration.StatusRevoked
			r = httptest.NewRequest(http.MethodPost, "/internal/trade-binding/v1/propose/authorize", bytes.NewReader(data))
			r.Header.Set("Authorization", "Bearer fixture")
			r.Header.Set("Content-Type", "application/json")
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("revoked grant cached: %d", w.Code)
			}
		})
	}
}
