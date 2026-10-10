package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

type inertLifecycleFoundingWriter struct { inertFoundingWriter }
func (inertLifecycleFoundingWriter) TransitionFoundingGovernance(
	context.Context, string, basestore.RequestMetadata, string, string, string,
	string, postgres.FoundingLifecycleInput,
) (postgres.FoundingLifecycleReceipt, error) {
	return postgres.FoundingLifecycleReceipt{}, errors.New("unexpected transition without authentication")
}

func TestPEO02LifecycleRoutesAreNonproductionAndAuthenticated(t *testing.T) {
	paths := []string{
		"/v2/founding-governance/sponsorships/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/suspend",
		"/v2/founding-governance/sponsorships/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/revoke",
		"/v2/founding-governance/documentary-deferrals/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/revoke",
	}
	cases := []struct {
		name string
		deps Dependencies
		want int
	}{
		{"default disabled", Dependencies{Store: &fakeStore{}}, http.StatusNotFound},
		{"production denied", Dependencies{
			Store: &fakeStore{}, Environment: "production",
			FoundingGovernanceEnabled: true, FoundingGovernance: inertLifecycleFoundingWriter{},
		}, http.StatusNotFound},
		{"unknown environment denied", Dependencies{
			Store: &fakeStore{}, Environment: "STAGING",
			FoundingGovernanceEnabled: true, FoundingGovernance: inertLifecycleFoundingWriter{},
		}, http.StatusNotFound},
		{"unimplemented lifecycle not mounted", Dependencies{
			Store: &fakeStore{}, Environment: "staging",
			FoundingGovernanceEnabled: true, FoundingGovernance: inertFoundingWriter{},
		}, http.StatusNotFound},
		{"staging rejects invalid bearer", Dependencies{
			Store: &fakeStore{}, Environment: "staging",
			AdminVerifier: fakeVerifier{err: errors.New("invalid issuer")},
			FoundingGovernanceEnabled: true, FoundingGovernance: inertLifecycleFoundingWriter{},
		}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := New(tc.deps)
			for _, path := range paths {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
				req.Header.Set("Authorization", "Bearer invalid")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != tc.want {
					t.Errorf("%s returned %d (want %d): %s",path,w.Code,tc.want,w.Body.String())
				}
			}
		})
	}
}
