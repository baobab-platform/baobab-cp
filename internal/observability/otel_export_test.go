package observability

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "testing"
 "time"
)

func TestExportedActualServerAndIssuerSpanLineage(t *testing.T) {
 type observation struct { Service string; Name string; Parent string; Span string; Trace string }
 reports := make([]observation, 0)
 collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  if r.Method != "POST" || r.URL.Path != "/v1/traces" { t.Errorf("unexpected OTLP endpoint"); w.WriteHeader(400); return }
  var doc map[string]any
  if json.NewDecoder(r.Body).Decode(&doc) != nil { t.Errorf("OTLP not JSON"); w.WriteHeader(400); return }
  data := doc["resourceSpans"].([]any)[0].(map[string]any)
  attrs := data["resource"].(map[string]any)["attributes"].([]any)
  service := attrs[0].(map[string]any)["value"].(map[string]any)["stringValue"].(string)
  item := data["scopeSpans"].([]any)[0].(map[string]any)["spans"].([]any)[0].(map[string]any)
  reports = append(reports, observation{Service:service,Name:item["name"].(string),Parent:item["parentSpanId"].(string),Span:item["spanId"].(string),Trace:item["traceId"].(string)})
  _ = w
 }))
 defer collector.Close()
 saved := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
 savedEnv := os.Getenv("BAOBAB_ENVIRONMENT")
 defer func(){ _ = os.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", saved); _ = os.Setenv("BAOBAB_ENVIRONMENT", savedEnv) }()
 _ = os.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
 _ = os.Setenv("BAOBAB_ENVIRONMENT", "test")
 handler := Server(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  child := Child(r.Context())
  ExportSpan(child, fakeStart(), "baobab.cp.iam.introspect")
  w.WriteHeader(http.StatusForbidden)
 }))
 req := httptest.NewRequest(http.MethodPost, "https://cp.example.invalid/protected?code=private", nil)
 req.Header.Set("Authorization", "Bearer very-private")
 handler.ServeHTTP(httptest.NewRecorder(), req)
 if len(reports) != 2 { t.Fatalf("want 2 independently exportable OTLP spans, got %d",len(reports)) }
 if reports[0].Service != "baobab-cp" || reports[0].Name != "baobab.cp.iam.introspect" ||
 reports[1].Name != "baobab.cp.http" || reports[0].Trace != reports[1].Trace ||
 reports[0].Parent != reports[1].Span {
  t.Fatal("actual exported CP client/server genealogy invalid")
 }
 if strings.Contains(reports[0].Service+reports[1].Name, "private") { t.Fatal("private data in exported attributes") }
}

func fakeStart() time.Time { return time.Now().Add(-time.Millisecond) }
