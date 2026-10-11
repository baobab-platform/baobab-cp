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

type inertReleaseWriter struct{}

func (inertReleaseWriter) ProposeDocumentaryRelease(context.Context, string,
	basestore.RequestMetadata, string, string, postgres.DocumentaryReleaseProposal) (postgres.ReviewReceipt, error) {
	return postgres.ReviewReceipt{}, errors.New("unauthorised release proposal executed")
}
func (inertReleaseWriter) DecideDocumentaryRelease(context.Context, string,
	basestore.RequestMetadata, string, string, postgres.DocumentaryReleaseDecision) (postgres.ReviewReceipt, error) {
	return postgres.ReviewReceipt{}, errors.New("unauthorised release approval executed")
}

func TestPEO02UReleaseRoutesRequireHumanIdentityAndNeverRunInProduction(t *testing.T) {
	paths := []string{
		"/v2/documentary-restrictions/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/release-proposals",
		"/v2/documentary-restrictions/release-proposals/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/decision",
	}
	cases := []struct {
		name string
		dep  Dependencies
		want int
	}{
		{"default-off", Dependencies{Store: &fakeStore{}}, http.StatusNotFound},
		{"production-no-mount", Dependencies{Store: &fakeStore{}, Environment: "production",
			DocumentaryReleaseEnabled: true, DocumentaryRelease: inertReleaseWriter{}}, http.StatusNotFound},
		{"mixed-case-staging-no-mount", Dependencies{Store: &fakeStore{}, Environment: "STAGING",
			DocumentaryReleaseEnabled: true, DocumentaryRelease: inertReleaseWriter{}}, http.StatusNotFound},
		{"synthetic-staging-denies-invalid-iam", Dependencies{Store: &fakeStore{}, Environment: "staging",
			DocumentaryReleaseEnabled: true, DocumentaryRelease: inertReleaseWriter{},
			AdminVerifier: fakeVerifier{err: errors.New("invalid IAM")}}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := New(tc.dep)
			for _, path := range paths {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
				req.Header.Set("Authorization", "Bearer invalid")
				req.Header.Set("Idempotency-Key", "synthetic-release-deny-001")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != tc.want {
					t.Errorf("%s returned %d, want %d: %s", path, w.Code, tc.want, w.Body.String())
				}
			}
		})
	}
}
