package observability

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExportedActualServerAndIssuerSpanLineage(t *testing.T) {
	type observation struct {
		Service string
		Name    string
		Parent  string
		Span    string
		Trace   string
	}
	reports := make([]observation, 0)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/traces" {
			t.Error("unexpected OTLP endpoint")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var doc map[string]any
		if json.NewDecoder(r.Body).Decode(&doc) != nil {
			t.Error("OTLP not JSON")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data := doc["resourceSpans"].([]any)[0].(map[string]any)
		attrs := data["resource"].(map[string]any)["attributes"].([]any)
		service := attrs[0].(map[string]any)["value"].(map[string]any)["stringValue"].(string)
		item := data["scopeSpans"].([]any)[0].(map[string]any)["spans"].([]any)[0].(map[string]any)
		reports = append(reports, observation{
			Service: service,
			Name:    item["name"].(string),
			Parent:  item["parentSpanId"].(string),
			Span:    item["spanId"].(string),
			Trace:   item["traceId"].(string),
		})
	}))
	defer collector.Close()

	saved := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	savedEnv := os.Getenv("BAOBAB_ENVIRONMENT")
	defer func() {
		_ = os.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", saved)
		_ = os.Setenv("BAOBAB_ENVIRONMENT", savedEnv)
	}()
	_ = os.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
	_ = os.Setenv("BAOBAB_ENVIRONMENT", "test")
	handler := Server(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		child := Child(r.Context())
		ExportSpan(child, time.Now().Add(-time.Millisecond), "baobab.cp.iam.introspect")
		w.WriteHeader(http.StatusForbidden)
	}))
	req := httptest.NewRequest(http.MethodPost, "https://cp.example.invalid/protected?code=private", nil)
	req.Header.Set("Authorization", "Bearer very-private")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if len(reports) != 2 {
		t.Fatalf("want 2 independently exportable OTLP spans, got %d", len(reports))
	}
	// Validate the wire JSON against OTLP's exact hex ID representation,
	// not merely the mocked Collector's assumption that both IDs match.
	for _, received := range reports {
		for _, item := range []struct {
			value string
			bytes int
		}{{received.Trace, 16}, {received.Span, 8}} {
			decoded, err := hex.DecodeString(item.value)
			if err != nil || len(decoded) != item.bytes || hex.EncodeToString(decoded) != item.value {
				t.Fatalf("OTLP JSON ID must be lowercase hex of %d bytes, got %q", item.bytes, item.value)
			}
		}
	}
	parent, err := hex.DecodeString(reports[0].Parent)
	if err != nil || len(parent) != 8 {
		t.Fatalf("OTLP client parent id must be exact 8-byte hex: %q", reports[0].Parent)
	}
	if reports[1].Parent != "" {
		t.Fatalf("root CP span must have no parent, got %q", reports[1].Parent)
	}
	if reports[0].Service != "baobab-cp" ||
		reports[0].Name != "baobab.cp.iam.introspect" ||
		reports[1].Name != "baobab.cp.http" ||
		reports[0].Trace != reports[1].Trace ||
		reports[0].Parent != reports[1].Span {
		t.Fatal("actual exported CP client/server genealogy invalid")
	}
	if strings.Contains(reports[0].Service+reports[1].Name, "private") {
		t.Fatal("private data in exported attributes")
	}
}

func TestOTLPExplicitEnvironmentAndInvalidIDsFailClosed(t *testing.T) {
	// The endpoint alone must not activate a development-only exporter in
	// an unset, sandbox, staging, production or unknown deployment.
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://127.0.0.1:4318/v1/traces")
	for _, env := range []string{"", "sandbox", "staging", "production", "PRODUCTION", "other"} {
		t.Setenv("BAOBAB_ENVIRONMENT", env)
		if endpoint := otlpTarget(); endpoint != "" {
			t.Fatalf("environment %q unexpectedly enabled exporter %q", env, endpoint)
		}
	}
	for _, env := range []string{"development", "test", "integration"} {
		t.Setenv("BAOBAB_ENVIRONMENT", env)
		if endpoint := otlpTarget(); endpoint == "" {
			t.Fatalf("explicit nonprotected environment %q must permit opt-in", env)
		}
	}
	for _, item := range []struct {
		value string
		bytes int
	}{{"", 8}, {"zzzzzzzzzzzzzzzz", 8}, {"fedcba987654321", 8},
		{"ABCDEF0123456789", 8}, {"0123456789abcdef", 16}} {
		if encoded := otlpId(item.value, item.bytes); encoded != "" {
			t.Fatalf("invalid OTLP id %q was exported: %q", item.value, encoded)
		}
	}
	if id := otlpId("0123456789abcdef", 8); id != "0123456789abcdef" {
		t.Fatalf("OTLP ID changed wire representation: %q", id)
	}
}
