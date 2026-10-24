package api

import (
	"context"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"net/http"
	"net/http/httptest"
	"testing"
)

type unavailableCurrentVerifier struct{}

func (unavailableCurrentVerifier) Verify(context.Context, string) (auth.Principal, error) {
	return auth.Principal{}, auth.ErrIntrospectionUnavailable
}
func TestMixedAuthenticationPreservesCurrentStatusOutage(t *testing.T) {
	a := &API{adminVerifier: unavailableCurrentVerifier{}}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("outage reached resource") })
	req := httptest.NewRequest("GET", "/v1/markets/selected", nil)
	req.Header.Set("Authorization", "Bearer private")
	response := httptest.NewRecorder()
	a.adminOrWorkload("market:read", "context:resolve")(next).ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable || problemCode(t, response) != "AUTH_VERIFIER_UNAVAILABLE" {
		t.Fatal(response.Code, response.Body.String())
	}
}
