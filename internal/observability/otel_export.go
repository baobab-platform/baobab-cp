package observability

// DEV-11 development-only exporter at actual CP request and issuer boundaries.
// Fixed attributes never include identity, tokens, URLs, payload or SQL.
// Trace context is diagnostics only and cannot grant CP authority.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

func otlpTarget() string {
	raw := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/traces" {
		return ""
	}
	// Unset/unknown is protected, as in internal/config.Config.Production.
	// A configured OTLP URL is NEVER an implicit development opt-in.
	env := os.Getenv("BAOBAB_ENVIRONMENT")
	// Protected traffic must use separately reviewed batched exporter sources.
	if env != "development" && env != "test" && env != "integration" {
		return ""
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	return u.String()
}

// OTLP/HTTP JSON special-cases traceId/spanId/parentSpanId as lowercase
// hexadecimal, unlike generic protobuf bytes fields. Invalid IDs are omitted.
func otlpId(value string, bytes int) string {
	if len(value) != bytes*2 {
		return ""
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != bytes {
		return ""
	}
	return hex.EncodeToString(decoded)
}

// ExportSpan reports an actual request or issuer call through the Collector.
// The owning caller creates the span and records its genuine service duration.
// Collector failures do not change an IAM/CP decision.
func ExportSpan(ctx context.Context, begin time.Time, name string) {
	endpoint := otlpTarget()
	span, ok := Current(ctx)
	if endpoint == "" || !ok || span.Flags != "01" {
		return
	}
	if name != "baobab.cp.http" && name != "baobab.cp.iam.introspect" {
		return
	}
	// Never emit malformed/non-W3C ids if context is changed by another
	// component; absence of a parent is allowed for a root server span only.
	if otlpId(span.TraceID, 16) == "" || otlpId(span.SpanID, 8) == "" ||
		(span.ParentSpanID != "" && otlpId(span.ParentSpanID, 8) == "") {
		return
	}
	kind := 2
	if name == "baobab.cp.iam.introspect" {
		kind = 3
	}
	attributes := []any{
		map[string]any{
			"key":   "baobab.operation",
			"value": map[string]any{"stringValue": name},
		},
	}
	data := map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "baobab-cp"}},
						map[string]any{"key": "service.namespace", "value": map[string]any{"stringValue": "baobab"}},
					},
				},
				"scopeSpans": []any{
					map[string]any{
						"scope": map[string]any{"name": "baobab.cp.http", "version": "dev11"},
						"spans": []any{
							map[string]any{
								"traceId":           otlpId(span.TraceID, 16),
								"spanId":            otlpId(span.SpanID, 8),
								"parentSpanId":      otlpId(span.ParentSpanID, 8),
								"name":              name,
								"kind":              kind,
								"attributes":        attributes,
								"startTimeUnixNano": strconv.FormatInt(begin.UnixNano(), 10),
								"endTimeUnixNano":   strconv.FormatInt(time.Now().UnixNano(), 10),
							},
						},
					},
				},
			},
		},
	}
	body, err := json.Marshal(data)
	if err != nil {
		return
	}
	reqCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout:       time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Do(req)
	if err == nil {
		_ = res.Body.Close()
	}
}
