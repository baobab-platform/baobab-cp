package observability

import (
 "context"
 "io"
 "bytes"
 collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
 "google.golang.org/protobuf/proto"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

// These regression cases are retained from superseded CP #463. They exercise
// the single SDK exporter, not a second hand-written OTLP implementation.
func TestExporterProtectedEnvironmentsFailClosed(t *testing.T) {
 for _, env := range []string{"", "test", "sandbox", "staging", "production", "PRODUCTION", "unknown"} {
  t.Run(env,func(t *testing.T) {
   stop,err:=StartExporter(context.Background(),env,"http://127.0.0.1:4318/v1/traces")
   if err==nil || stop!=nil {t.Fatalf("environment %q enabled development exporter",env)}
  })
 }
 for _, endpoint:=range []string{"http://user:pass@localhost:4318/v1/traces","http://localhost:4318/v1/traces?token=secret","http://localhost:4318/other","file:///v1/traces","http://localhost:4318/v1/traces#fragment"} {
  if stop,err:=StartExporter(context.Background(),"development",endpoint);err==nil || stop!=nil {t.Fatalf("unsafe endpoint accepted: %q",endpoint)}
 }
}

func TestExporterDisabledPreservesW3CAndAuthority(t *testing.T) {
 provider.Store(nil)
 req:=httptest.NewRequest(http.MethodGet,"/private?secret=hidden",nil)
 req.Header.Set("traceparent","00-11111111111111111111111111111111-2222222222222222-03")
 req.Header.Set("Authorization","Bearer never-export")
 res:=httptest.NewRecorder()
 Server(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  child:=Child(r.Context())
  parent,ok:=Current(r.Context())
  outbound,valid:=Current(child)
  if !ok || !valid || parent.TraceID!="11111111111111111111111111111111" || parent.Flags!="03" || outbound.ParentSpanID!=parent.SpanID || outbound.TraceID!=parent.TraceID {t.Error("fallback W3C lineage lost")}
  w.WriteHeader(http.StatusForbidden)
 })).ServeHTTP(res,req)
 if res.Code!=http.StatusForbidden {t.Fatalf("telemetry changed refusal: %d",res.Code)}
}

func TestExporterCollectorFailureCannotChangeRefusal(t *testing.T) {
 sink:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  time.Sleep(250*time.Millisecond)
  w.WriteHeader(http.StatusServiceUnavailable)
 }))
 defer sink.Close()
 stop,err:=StartExporter(context.Background(),"integration",sink.URL+"/v1/traces")
 if err!=nil {t.Fatal(err)}
 defer func(){ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();_ = stop(ctx)}()
 req:=httptest.NewRequest(http.MethodPost,"/private?credential=secret",nil)
 req.Header.Set("traceparent","00-11111111111111111111111111111111-2222222222222222-03")
 req.Header.Set("Authorization","Bearer sensitive-token")
 res:=httptest.NewRecorder()
 start:=time.Now()
 Server(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  child,end:=StartIntrospection(r.Context())
  if trace,ok:=Current(child);!ok || trace.Flags!="03" {t.Error("sampled W3C flags lost")}
  end(http.StatusUnauthorized)
  w.WriteHeader(http.StatusForbidden)
 })).ServeHTTP(res,req)
 if res.Code!=http.StatusForbidden || time.Since(start)>200*time.Millisecond {t.Fatalf("collector changed authorization or delayed request: %d",res.Code)}
 if strings.Contains(res.Header().Get("traceparent"),"secret") {t.Fatal("sensitive data in trace context")}
}

func TestExporterRejectsInvalidIncomingTraceParent(t *testing.T) {
 provider.Store(nil)
 for _, raw:=range []string{"00-00000000000000000000000000000000-2222222222222222-01","00-11111111111111111111111111111111-0000000000000000-01","ff-11111111111111111111111111111111-2222222222222222-01"} {
  req:=httptest.NewRequest(http.MethodGet,"/",nil)
  req.Header.Set("traceparent",raw)
  Server(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
   v,ok:=Current(r.Context())
   if !ok || v.ParentSpanID!="" {t.Errorf("invalid parent accepted: %q",raw)}
   w.WriteHeader(http.StatusForbidden)
  })).ServeHTTP(httptest.NewRecorder(),req)
 }
}

func TestExporterWirePayloadPrivacyAndSampled03(t *testing.T) {
 reports:=make(chan *collector.ExportTraceServiceRequest,2)
 sink:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  raw,err:=io.ReadAll(r.Body)
  if err!=nil || r.URL.Path!="/v1/traces" || r.Header.Get("Content-Type")!="application/x-protobuf" {w.WriteHeader(http.StatusBadRequest);return}
  for _,secret:=range [][]byte{[]byte("private-secret-token"),[]byte("credential=private-secret"),[]byte("private-customer@example.invalid")} {
   if bytes.Contains(raw,secret) {t.Error("secret leaked to OTLP collector")}
  }
  var batch collector.ExportTraceServiceRequest
  if err:=proto.Unmarshal(raw,&batch);err!=nil {t.Error(err);w.WriteHeader(http.StatusBadRequest);return}
  reports<-&batch
  w.Header().Set("Content-Type","application/x-protobuf")
 }))
 defer sink.Close()
 stop,err:=StartExporter(context.Background(),"development",sink.URL+"/v1/traces")
 if err!=nil {t.Fatal(err)}
 req:=httptest.NewRequest(http.MethodPost,"/internal/identity/v1/principals/resolve?credential=private-secret",strings.NewReader("private-customer@example.invalid"))
 req.Header.Set("traceparent","00-11111111111111111111111111111111-2222222222222222-03")
 req.Header.Set("Authorization","Bearer private-secret-token")
 res:=httptest.NewRecorder()
 Server(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  call,end:=StartIntrospection(r.Context())
  if v,ok:=Current(call);!ok || v.Flags!="03" {t.Error("sampled trace flags lost")}
  end(http.StatusUnauthorized)
  w.WriteHeader(http.StatusForbidden)
 })).ServeHTTP(res,req)
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
 defer cancel()
 if err:=stop(ctx);err!=nil {t.Fatal(err)}
 select {
 case batch:=<-reports:
  spans:=0
  for _,rs:=range batch.GetResourceSpans() {for _,scope:=range rs.GetScopeSpans() {for _,span:=range scope.GetSpans() {
   spans++
   if len(span.GetTraceId())!=16 || len(span.GetSpanId())!=8 {t.Error("invalid OTLP span IDs")}
   if span.GetName()!="baobab.cp.buyer.principal.resolve" && span.GetName()!="baobab.cp.iam.introspect" {t.Errorf("unexpected span name %q",span.GetName())}
  }}}
  if spans!=2 {t.Fatalf("expected two actual exported spans, got %d",spans)}
 case <-ctx.Done():t.Fatal("no OTLP wire payload")
 }
}
