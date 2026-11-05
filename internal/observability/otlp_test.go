package observability

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestDevelopmentExporterPreservesActualServerAndIssuerRequestLineage(t *testing.T) {
	requests := make(chan *collector.ExportTraceServiceRequest, 2)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		var batch collector.ExportTraceServiceRequest
		if err != nil || r.URL.Path != "/v1/traces" || proto.Unmarshal(payload, &batch) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- &batch
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer sink.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stop, err := StartExporter(ctx, "development", sink.URL+"/v1/traces")
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Store(nil)
	req := httptest.NewRequest(http.MethodPost, "/internal/identity/v1/principals/resolve", nil)
	req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	res := httptest.NewRecorder()
	var client Context
	Server(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call, end := StartIntrospection(r.Context())
		client, _ = Current(call)
		end(200)
		w.WriteHeader(http.StatusForbidden)
	})).ServeHTTP(res, req)
	server, ok := Parse(res.Header().Get("traceparent"))
	if !ok || client.ParentSpanID != server.SpanID || client.TraceID != server.TraceID {
		t.Fatal("outbound context does not descend from CP server span")
	}
	if err := stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-requests:
		scopes := batch.GetResourceSpans()[0].GetScopeSpans()
		spans := scopes[0].GetSpans()
		if len(spans) != 2 || spans[0].GetName() != "baobab.cp.iam.introspect" ||
			spans[1].GetName() != "baobab.cp.buyer.principal.resolve" ||
			spans[1].GetStatus().GetCode().String() != "STATUS_CODE_ERROR" {
			t.Fatal("exported CP request and issuer call do not match")
		}
	case <-ctx.Done():
		t.Fatal("OTLP sink received no export")
	}
}
