package observability

// DEV-11: an opt-in OTLP/HTTP JSON emitter for ACTUAL CP server and issuer-client
// boundaries. No trace or collector response grants a Principal, tenant, market,
// mandate, or posting authority. All attributes are fixed and privacy-bounded.
import (
 "bytes"
 "context"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "net/http"
 "net/url"
 "os"
 "strings"
 "time"
)

func otlpTarget() string {
 raw := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
 if raw == "" { return "" }
 u, err := url.Parse(raw)
 if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/traces" { return "" }
 env := os.Getenv("BAOBAB_ENVIRONMENT")
 if env == "" { env = "development" }
 if u.Scheme != "https" && !(u.Scheme == "http" && (env == "development" || env == "test" || env == "integration")) { return "" }
 return u.String()
}

func otlpId(value string) string {
 b, err := hex.DecodeString(value)
 if err != nil { return "" }
 return base64.StdEncoding.EncodeToString(b)
}

func otlpSpan(ctx context.Context, begin time.Time, name string) {
 endpoint := otlpTarget()
 span, ok := Current(ctx)
 if endpoint == "" || !ok || span.Flags != "01" { return }
 if name != "baobab.cp.http" && name != "baobab.cp.iam.introspect" { return }
 kind := 2 // SERVER
 if name == "baobab.cp.iam.introspect" { kind = 3 } // CLIENT
 attributes := []any{map[string]any{"key":"baobab.operation", "value":map[string]any{"stringValue":name}}}
 data := map[string]any{"resourceSpans":[]any{map[string]any{
  "resource":map[string]any{"attributes":[]any{
   map[string]any{"key":"service.name","value":map[string]any{"stringValue":"baobab-cp"}},
   map[string]any{"key":"service.namespace","value":map[string]any{"stringValue":"baobab"}},
  }},
  "scopeSpans":[]any{map[string]any{
   "scope":map[string]any{"name":"baobab.cp.http","version":"dev11"},
   "spans":[]any{map[string]any{
    "traceId":otlpId(span.TraceID),"spanId":otlpId(span.SpanID),
    "parentSpanId":otlpId(span.ParentSpanID),
    "name":name,"kind":kind,"attributes":attributes,
    "startTimeUnixNano":begin.UnixNano(),
    "endTimeUnixNano":time.Now().UnixNano(),
   }},
  }},
 }}}
 // OTLP JSON requires uint64 nanotimes as JSON decimal strings, not float64.
 body, err := json.Marshal(data)
 if err != nil { return }
 body = bytes.ReplaceAll(body, []byte("\"startTimeUnixNano\":"+int64Text(begin.UnixNano())),
  []byte("\"startTimeUnixNano\":\""+int64Text(begin.UnixNano())+"\""))
 end := time.Now().UnixNano()
 // endTimeUnixNano is represented as a string in the actual export payload;
 // never fabricate another span or alter its business source.
 // Replace the numeric field found in this one bounded object.
 var message map[string]any
 if json.Unmarshal(body, &message) != nil { return }
 resource := message["resourceSpans"].([]any)[0].(map[string]any)
 scopes := resource["scopeSpans"].([]any)[0].(map[string]any)
 emitted := scopes["spans"].([]any)[0].(map[string]any)
 emitted["startTimeUnixNano"] = int64Text(begin.UnixNano())
 emitted["endTimeUnixNano"] = int64Text(end)
 body, err = json.Marshal(message)
 if err != nil { return }
 reqCtx, cancel := context.WithTimeout(context.Background(), time.Second)
 defer cancel()
 req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
 if err != nil { return }
 req.Header.Set("Content-Type", "application/json")
 client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
 res, err := client.Do(req)
 if err == nil { _ = res.Body.Close() }
}
func int64Text(n int64) string {
 // strconv.FormatInt preserves exact uint64 JSON representation.
 return strings.TrimSpace(formatInt(n))
}
