package api

import (
    "bytes"
    "encoding/json"
    "log/slog"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/go-chi/chi/v5"
)

// DEV-11: a raw path can carry customer identifiers even when the request is
// denied. This verifies the actual middleware log, not a duplicated helper.
func TestDEV11RequestLogUsesOnlyRouteTemplate(t *testing.T) {
    oldLogger := slog.Default()
    var output bytes.Buffer
    slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
    t.Cleanup(func() { slog.SetDefault(oldLogger) })

    a := &API{}
    router := chi.NewRouter()
    router.Use(a.requestLog)
    router.Get("/v1/users/{subject}", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusForbidden)
    })

    request := httptest.NewRequest(http.MethodGet,
        "/v1/users/customer-private-123?token=private-query-value", nil)
    request.Header.Set("Authorization", "Bearer private-bearer-token")
    response := httptest.NewRecorder()
    router.ServeHTTP(response, request)
    if response.Code != http.StatusForbidden {
        t.Fatalf("unexpected status: %d", response.Code)
    }
    var fields map[string]any
    if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
        t.Fatalf("invalid structured log: %v", err)
    }
    if fields["route"] != "/v1/users/{subject}" {
        t.Fatalf("missing server-owned route template: %v", fields["route"])
    }
    if fields["status"] != float64(http.StatusForbidden) {
        t.Fatalf("incorrect status: %v", fields["status"])
    }
    for _, forbiddenKey := range []string{"path", "actor_id", "client_id", "tenant_id"} {
        if _, exists := fields[forbiddenKey]; exists {
            t.Errorf("request log contains forbidden identifier field: %s", forbiddenKey)
        }
    }
    for _, value := range []string{"customer-private-123", "private-query-value", "private-bearer-token"} {
        if strings.Contains(output.String(), value) {
            t.Error("private request data leaked into structured log")
        }
    }
}

func TestDEV11RequestLogUnmatchedRouteIsClosed(t *testing.T) {
    oldLogger := slog.Default()
    var output bytes.Buffer
    slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
    t.Cleanup(func() { slog.SetDefault(oldLogger) })

    router := chi.NewRouter()
    a := &API{}
    router.Use(a.requestLog)
    request := httptest.NewRequest(http.MethodGet, "/not-found/customer-private-123", nil)
    router.ServeHTTP(httptest.NewRecorder(), request)
    var fields map[string]any
    if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
        t.Fatalf("invalid structured log: %v", err)
    }
    if fields["route"] != "UNMATCHED" {
        t.Fatalf("unknown route must not log the request path: %v", fields["route"])
    }
    if strings.Contains(output.String(), "customer-private-123") {
        t.Fatal("unmatched route revealed private URL path")
    }
}
