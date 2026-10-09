package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The LA-03 v2 intake must remain unavailable until the reviewed rollout
// enables the feature. No request can bypass authenticated platform admin.
func TestOrganisationFirstV2RoutesDefaultOff(t *testing.T) {
	handler := New(Dependencies{Store: &fakeStore{}})
	for _, url := range []string{
		"/v2/tenants",
		"/v2/tenant-onboarding/tor_0190a1b2c3d4e5f60718293a4b5c6d7e/primary-organisation",
	} {
		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("disabled route %s returned %d", url, recorder.Code)
		}
	}
}

func TestOrganisationFirstV2RequiresPrivilegedIdentity(t *testing.T) {
	handler := New(Dependencies{
		Store: &fakeStore{}, OrganisationFirstV2: true,
		AdminVerifier: fakeVerifier{err: errors.New("invalid bearer")},
	})
	for _, url := range []string{
		"/v2/tenants",
		"/v2/tenant-onboarding/tor_0190a1b2c3d4e5f60718293a4b5c6d7e/primary-organisation",
	} {
		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer invalid")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("v2 route %s admitted an unauthenticated principal: %d", url, recorder.Code)
		}
	}
}
