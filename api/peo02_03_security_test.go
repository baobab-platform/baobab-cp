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

type inertProgressiveWriter struct{}

func (inertProgressiveWriter) CreateProgressiveApplication(context.Context, basestore.RequestMetadata, string, string, []byte) (postgres.ProgressiveApplicantDraft, error) {
	return postgres.ProgressiveApplicantDraft{}, errors.New("unexpected direct storage invocation")
}
func (inertProgressiveWriter) GetProgressiveApplication(context.Context, string, string) (postgres.ProgressiveApplicantDraft, error) {
	return postgres.ProgressiveApplicantDraft{}, errors.New("unexpected direct storage invocation")
}
func (inertProgressiveWriter) ChangeProgressiveApplication(context.Context, basestore.RequestMetadata, string, string, []byte, bool) (postgres.ProgressiveApplicantDraft, error) {
	return postgres.ProgressiveApplicantDraft{}, errors.New("unexpected direct storage invocation")
}

type inertFoundingWriter struct{}

func (inertFoundingWriter) ProposeFoundingGovernance(context.Context, string, basestore.RequestMetadata, string, string, []byte) (postgres.FoundingCommandReceipt, error) {
	return postgres.FoundingCommandReceipt{}, errors.New("unexpected direct founding grant")
}
func (inertFoundingWriter) DecideFoundingGovernance(context.Context, string, basestore.RequestMetadata, string, string, postgres.FoundingDecisionInput) (postgres.FoundingCommandReceipt, error) {
	return postgres.FoundingCommandReceipt{}, errors.New("unexpected direct founding checker grant")
}
func TestPEO02AndPEO03FeatureGatesAndAuthentication(t *testing.T) {
	endpoints := []struct{ method, path string }{
		{"POST", "/v2/client-applications"},
		{"GET", "/v2/client-applications/capp_0190a1b2c3d4e5f60718293a4b5c6d7e"},
		{"PATCH", "/v2/client-applications/capp_0190a1b2c3d4e5f60718293a4b5c6d7e"},
		{"POST", "/v2/client-applications/capp_0190a1b2c3d4e5f60718293a4b5c6d7e/submit"},
		{"POST", "/v2/founding-governance/sponsorship-proposals"},
		{"POST", "/v2/founding-governance/documentary-deferral-proposals"},
		{"POST", "/v2/founding-governance/intents/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/decision"},
	}
	for _, tc := range []struct {
		name     string
		deps     Dependencies
		expected int
	}{
		{"default off", Dependencies{Store: &fakeStore{}}, 404},
		{"production disabled even if flag enabled", Dependencies{
			Store: &fakeStore{}, Environment: "production",
			ProgressiveApplicationsEnabled: true, ProgressiveApplications: inertProgressiveWriter{},
			FoundingGovernanceEnabled: true, FoundingGovernance: inertFoundingWriter{},
		}, 404},
		{"staging routes require verified bearer", Dependencies{
			Store: &fakeStore{}, Environment: "staging",
			AdminVerifier:                  fakeVerifier{err: errors.New("invalid issuer")},
			ProgressiveApplicationsEnabled: true, ProgressiveApplications: inertProgressiveWriter{},
			FoundingGovernanceEnabled: true, FoundingGovernance: inertFoundingWriter{},
		}, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(tc.deps)
			for _, ep := range endpoints {
				req := httptest.NewRequest(ep.method, ep.path, strings.NewReader("{}"))
				if tc.expected == 401 {
					req.Header.Set("Authorization", "Bearer invalid")
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != tc.expected {
					t.Errorf("%s %s got %d want %d", ep.method, ep.path, w.Code, tc.expected)
				}
			}
		})
	}
}
