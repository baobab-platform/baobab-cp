package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type documentaryRestrictionRegistry struct{ active bool }

func (r documentaryRestrictionRegistry) IsActive(client string) bool {
	return r.active && client == "baobab-trade"
}
func (r documentaryRestrictionRegistry) AllowsScope(client, scope string) bool {
	return r.IsActive(client) && scope == "documentary:restriction-assess"
}

type documentaryRestrictionFixture struct {
	calls   int
	blocked bool
	err     error
}
func (f *documentaryRestrictionFixture) DocumentaryScopeBlocked(_ context.Context, _, _, _ string) (bool, error) {
	f.calls++
	return f.blocked, f.err
}

func TestPEO02TDocumentaryRestrictionDefaultOffAndProductionDenied(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	for _, deps := range []Dependencies{
		{Store: &fakeStore{}, Environment: "integration", Contexts: repo, Identities: repo,
			DocumentaryRestriction: &documentaryRestrictionFixture{},
			WorkloadRegistry: documentaryRestrictionRegistry{active: true},
			DocumentaryRestrictionClientIDs: []string{"baobab-trade"}},
		{Store: &fakeStore{}, Environment: "production", Contexts: repo, Identities: repo,
			DocumentaryRestrictionEnabled: true, DocumentaryRestriction: &documentaryRestrictionFixture{},
			WorkloadRegistry: documentaryRestrictionRegistry{active: true},
			DocumentaryRestrictionClientIDs: []string{"baobab-trade"}},
		{Store: &fakeStore{}, Environment: "integration", Contexts: repo, Identities: repo,
			DocumentaryRestrictionEnabled: true, DocumentaryRestriction: &documentaryRestrictionFixture{},
			WorkloadRegistry: documentaryRestrictionRegistry{active: true}},
	} {
		r := httptest.NewRequest(http.MethodPost, "/internal/documentary/v1/restrictions/assess", nil)
		w := httptest.NewRecorder()
		New(deps).ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("restriction route mounted without complete nonproduction acceptance: %d", w.Code)
		}
	}
}

func TestPEO02TDocumentaryRestrictionContextOwnershipAndNegativeOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		blocked bool
		market  string
		owner   bool
		expired bool
		scope   bool
		outage  bool
		want    int
	}{
		{"current-blocked", true, "ZA", true, false, true, false, 200},
		{"no-active-restriction-is-not-authority", false, "ZA", true, false, true, false, 200},
		{"wrong-market", true, "UG", true, false, true, false, 403},
		{"unowned-context", true, "ZA", false, false, true, false, 404},
		{"expired-context", true, "ZA", true, true, true, false, 404},
		{"missing-scope", true, "ZA", true, false, false, false, 403},
		{"db-unavailable", true, "ZA", true, false, true, true, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := repository.NewInMemoryRepository()
			principal := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
			if err := repo.CreateIdentity(context.Background(), principal); err != nil {
				t.Fatal(err)
			}
			issuer, subject := "https://iam.example.test", "baobab-trade"
			if err := repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{
				ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID,
				Issuer: issuer, Subject: subject, Status: "ACTIVE",
			}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			expiry := now.Add(time.Minute)
			if tc.expired {
				expiry = now.Add(-time.Minute)
			}
			org := domain.NewUUIDv7()
			ctx := domain.Context{ID: domain.NewUUIDv7(), PrincipalID: principal.ID,
				TenantID: "tn_documentary", OrganisationID: org, CountryCode: "ZA",
				MarketID: "market-za", CorrelationID: domain.NewUUIDv7(),
				ResolvedAt: now.Add(-2 * time.Minute), ExpiresAt: &expiry}
			if !tc.owner {
				ctx.PrincipalID = domain.NewPrincipalID()
			}
			repo.Contexts[ctx.ID] = ctx
			reader := &documentaryRestrictionFixture{blocked: tc.blocked}
			if tc.outage {
				reader.err = errors.New("synthetic database outage")
			}
			scopes := map[string]struct{}{"context:resolve": {}}
			if tc.scope {
				scopes["documentary:restriction-assess"] = struct{}{}
			}
			handler := New(Dependencies{
				Store: &fakeStore{tenant: domain.Tenant{
					TenantID: ctx.TenantID, ObservedState: string(domain.LifecycleActive),
					PrimaryOrganisationID: org}},
				Environment: "integration",
				DocumentaryRestrictionEnabled: true, DocumentaryRestriction: reader,
				DocumentaryRestrictionClientIDs: []string{"baobab-trade"},
				WorkloadVerifier: fakeVerifier{principal: auth.Principal{
					Issuer: issuer, Subject: subject, ActorType: "workload",
					ClientID: "baobab-trade", TenantID: ctx.TenantID,
					TokenID: "token-documentary", Scopes: scopes}},
				WorkloadRegistry: documentaryRestrictionRegistry{active: true},
				Contexts: repo, Identities: repo,
			})
			body, _ := json.Marshal(documentaryRestrictionRequest{
				ContextID: ctx.ID, Capability: "trade.checkout",
				Market: tc.market, OperationReference: "synthetic/order-123"})
			request := httptest.NewRequest(http.MethodPost,
				"/internal/documentary/v1/restrictions/assess", bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer synthetic")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, request)
			if w.Code != tc.want {
				t.Fatalf("status=%d wanted %d, body=%s", w.Code, tc.want, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store, max-age=0" {
				t.Fatal("current restriction outcome must never be cached")
			}
			if tc.want != 200 {
				if reader.calls != 0 && !tc.outage {
					t.Fatal("untrusted context invoked authoritative restriction store")
				}
				return
			}
			var result documentaryRestrictionResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.AuthorityGranted || result.Blocked != tc.blocked ||
				result.ContextID != ctx.ID || result.OrganisationID != org ||
				!result.ExpiresAt.After(result.EvaluatedAt) ||
				result.ExpiresAt.After(result.EvaluatedAt.Add(2*time.Second)) ||
				reader.calls != 1 {
				t.Fatalf("incorrect negative-only response: %+v (reads %d)", result, reader.calls)
			}
		})
	}
}
