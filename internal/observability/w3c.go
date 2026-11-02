// Package observability propagates W3C Trace Context across CP HTTP boundaries.
// Trace metadata is diagnostics only, never a source of tenant or actor authority.
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

// Context is a local span with its verified parent, not a trusted identity.
type Context struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Flags        string
}

var parentFormat = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$`)

func Parse(value string) (Context, bool) {
	m := parentFormat.FindStringSubmatch(value)
	if m == nil || strings.Trim(m[1], "0") == "" || strings.Trim(m[2], "0") == "" {
		return Context{}, false
	}
	return Context{TraceID: m[1], SpanID: m[2], Flags: m[3]}, true
}

func fresh(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Begin creates a server child of a valid W3C parent, or a new trace root.
// Telemetry entropy failure never changes whether a business request proceeds.
func Begin(ctx context.Context, incoming string) context.Context {
	remote, valid := Parse(incoming)
	traceID := remote.TraceID
	flags := "01"
	parent := ""
	if valid {
		flags = remote.Flags
		parent = remote.SpanID
	}
	if traceID == "" {
		id, err := fresh(16)
		if err != nil {
			return ctx
		}
		traceID = id
	}
	spanID, err := fresh(8)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, key{}, Context{
		TraceID: traceID, SpanID: spanID, ParentSpanID: parent, Flags: flags,
	})
}

// Current exposes only internally constructed diagnostic context.
func Current(ctx context.Context) (Context, bool) {
	v, ok := ctx.Value(key{}).(Context)
	return v, ok && v.TraceID != "" && v.SpanID != ""
}

func (v Context) Traceparent() string {
	if v.TraceID == "" || v.SpanID == "" {
		return ""
	}
	return "00-" + v.TraceID + "-" + v.SpanID + "-" + v.Flags
}

// Child creates a distinct client span for an actual outbound request.
func Child(ctx context.Context) context.Context {
	v, ok := Current(ctx)
	if !ok {
		return ctx
	}
	span, err := fresh(8)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, key{}, Context{
		TraceID: v.TraceID, SpanID: span, ParentSpanID: v.SpanID, Flags: v.Flags,
	})
}

// Inject propagates only a generated span; no caller baggage or identity.
func Inject(ctx context.Context, req *http.Request) error {
	if req == nil {
		return errors.New("nil request")
	}
	if v, ok := Current(ctx); ok {
		req.Header.Set("traceparent", v.Traceparent())
	}
	return nil
}

// Server instruments actual HTTP requests, including rejected requests.
func Server(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := Begin(r.Context(), r.Header.Get("traceparent"))
		if v, ok := Current(ctx); ok {
			w.Header().Set("traceparent", v.Traceparent())
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
