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
	reports := make(chan observation, 2)
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
		reports <- observation{
			Service: service,
			Name:    item["name"].(string),
			Parent:  item["parentSpanId"].(string),
			Span:    item["spanId"].(string),
			Trace:   item["traceId"].(string),
		}
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

	// Export completion is asynchronous: collect the real receiver's two
	// requests rather than inspecting an unsynchronised server goroutine.
	received := make([]observation, 0, 2)
	for len(received) < 2 {
		select {
		case item := <-reports:
			received = append(received, item)
		case <-time.After(5 * time.Second):
			t.Fatalf("want 2 independently exportable OTLP spans, got %d", len(received))
		}
	}
	// Validate the wire JSON against OTLP's exact hex ID representation,
	// not merely the mocked Collector's assumption that both IDs match.
	for _, received := range received {
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
	parent, err := hex.DecodeString(received[0].Parent)
	if err != nil || len(parent) != 8 {
		t.Fatalf("OTLP client parent id must be exact 8-byte hex: %q", received[0].Parent)
	}
	if received[1].Parent != "" {
		t.Fatalf("root CP span must have no parent, got %q", received[1].Parent)
	}
	if received[0].Service != "baobab-cp" ||
		received[0].Name != "baobab.cp.iam.introspect" ||
		received[1].Name != "baobab.cp.http" ||
		received[0].Trace != received[1].Trace ||
		received[0].Parent != received[1].Span {
		t.Fatal("actual exported CP client/server genealogy invalid")
	}
	if strings.Contains(received[0].Service+received[1].Name, "private") {
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

// TestCollectorFailureDoesNotDelayAuthorization exercises the real server
// middleware against a Collector that stalls beyond request completion.
// The trace is deliberately sampled. No connector failure may add latency to
// the CP request or change its status.
func TestCollectorFailureDoesNotDelayAuthorization(t *testing.T) {
	stalled := make(chan struct{})
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-stalled
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer collector.Close()
	defer close(stalled)
	t.Setenv("BAOBAB_ENVIRONMENT", "test")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
	handler := Server(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	start := time.Now()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/protected", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("collector changed authority result: %d", w.Code)
	}
	if duration := time.Since(start); duration > 200*time.Millisecond {
		t.Fatalf("collector blocked authorization for %v", duration)
	}
}

// W3C sampled is a bit, and a valid upstream 0x03 flags value must not
// disappear from the exported BFF -> IAM -> CP lineage.
func TestSampledTraceFlagsPreserveW3CBitfield(t *testing.T) {
	for flags, want := range map[string]bool{
		"00": false, "01": true, "02": false, "03": true,
		"ff": true, "fe": false, "zz": false, "": false,
	} {
		if got := sampledTraceFlags(flags); got != want {
			t.Errorf("trace-flags %q sampled=%t, want %t", flags, got, want)
		}
	}
}
