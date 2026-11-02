package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/observability"
)

// This executes the production HTTPIntrospector against an ephemeral HTTPS
// issuer listener. It tests real transport, NOT live Keycloak qualification.
func TestDEV11LiveHTTPIntrospectionPreservesTraceWithoutGrantingAuthority(t *testing.T) {
	var mu sync.Mutex
	headers := map[string]string{}
	var calls int
	issuer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		headers[r.Header.Get("traceparent")] = r.Header.Get("Authorization")
		mu.Unlock()
		if r.URL.Path != "/introspect" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 2048))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(b), "revoked") {
			_, _ = w.Write([]byte(`{"active":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"active":true,"iss":"https://issuer.example","sub":"synthetic","client_id":"cp","scope":"cp:read","exp":4102444800}`))
	}))
	defer issuer.Close()
	introspector, err := NewHTTPIntrospector(issuer.URL, issuer.URL+"/introspect", "dev11-cp", "synthetic-client-secret-12345")
	if err != nil {
		t.Fatal(err)
	}
	introspector.Client = issuer.Client()
	server := httptest.NewServer(observability.Server(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, err := introspector.Introspect(r.Context(), r.URL.Query().Get("mode"))
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !result.Active {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	defer server.Close()
	parent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	for _, tc := range []struct {
		mode   string
		status int
	}{{"current", 204}, {"revoked", 403}} {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/?mode="+tc.mode, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("traceparent", parent)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("status=%d wanted=%d", res.StatusCode, tc.status)
		}
		out, ok := observability.Parse(res.Header.Get("traceparent"))
		if !ok || out.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || out.SpanID == "00f067aa0ba902b7" {
			t.Fatal("CP must create a W3C child for both accepted and rejected operations")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 || len(headers) != 2 {
		t.Fatalf("issuer calls=%d distinct contexts=%d", calls, len(headers))
	}
	for raw, authorization := range headers {
		span, ok := observability.Parse(raw)
		if !ok || span.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatal("issuer request lost W3C trace")
		}
		if !strings.HasPrefix(authorization, "Basic ") {
			t.Fatal("issuer request lacks the configured private client credential")
		}
	}
}
