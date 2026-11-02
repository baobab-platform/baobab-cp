// Package observability carries W3C Trace Context through native HTTP
// boundaries. Trace metadata is diagnostics only; it never grants authority.
// This package deliberately does not change CP's token or tenant validation.
package observability

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "errors"
 "net/http"
 "regexp"
 "strings"
)

type key struct{}

// Context is the current server/client span and its verified parent.
type Context struct {
 TraceID string
 SpanID string
 ParentSpanID string
 Flags string
}

var parentFormat = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$`)

func Parse(value string) (Context, bool) {
 m := parentFormat.FindStringSubmatch(value)
 if m == nil || strings.Trim(m[1], "0") == "" || strings.Trim(m[2], "0") == "" { return Context{}, false }
 return Context{TraceID:m[1],SpanID:m[2],Flags:m[3]},true
}

func fresh(n int) (string,error) {
 b:=make([]byte,n)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return hex.EncodeToString(b),nil
}

// Begin starts a real local server span, linking a valid incoming W3C parent.
// Invalid/untrusted upstream context creates a new root, not a new privilege.
// Failure of the diagnostic random source must not block business operations.
func Begin(ctx context.Context, incoming string) context.Context {
 remote,valid:=Parse(incoming)
 traceID:=remote.TraceID
 flags:="01"
 parent:=""
 if valid {flags=remote.Flags;parent=remote.SpanID}
 if traceID=="" {
   id,err:=fresh(16);if err!=nil{return ctx};traceID=id
 }
 spanID,err:=fresh(8);if err!=nil{return ctx}
 return context.WithValue(ctx,key{},Context{TraceID:traceID,SpanID:spanID,ParentSpanID:parent,Flags:flags})
}

// Current returns a copy of the span metadata, never caller supplied authority.
func Current(ctx context.Context) (Context,bool) {
 v,ok:=ctx.Value(key{}).(Context)
 return v,ok && v.TraceID!="" && v.SpanID!=""
}
func (v Context) Traceparent() string {
 if v.TraceID=="" || v.SpanID=="" {return ""}
 return "00-"+v.TraceID+"-"+v.SpanID+"-"+v.Flags
}

// Child represents one actual outgoing operation within the request trace.
// Separate call spans avoid misrepresenting the CP server span as the issuer's.
func Child(ctx context.Context) context.Context {
 v,ok:=Current(ctx);if !ok{return ctx}
 span,err:=fresh(8);if err!=nil{return ctx}
 return context.WithValue(ctx,key{},Context{TraceID:v.TraceID,SpanID:span,ParentSpanID:v.SpanID,Flags:v.Flags})
}

// Inject propagates only a validated, internally issued span. Never propagate
// arbitrary incoming tracestate/baggage/identity claims as CP authority.
func Inject(ctx context.Context, req *http.Request) error {
 if req==nil{return errors.New("nil request")}
 if v,ok:=Current(ctx);ok{req.Header.Set("traceparent",v.Traceparent())}
 return nil
}

// Server wraps a real HTTP handler; handlers and outbound clients receive
// the native context via r.Context(), including HTTP 401/403 cases.
func Server(next http.Handler) http.Handler {
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   ctx:=Begin(r.Context(),r.Header.Get("traceparent"))
   if v,ok:=Current(ctx);ok{w.Header().Set("traceparent",v.Traceparent())}
   next.ServeHTTP(w,r.WithContext(ctx))
 })
}
