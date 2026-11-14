package observability

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var provider atomic.Pointer[sdktrace.TracerProvider]

func StartExporter(ctx context.Context, environment, endpoint string) (func(context.Context) error, error) {
	if endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || (environment != "development" && environment != "integration") ||
		(u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/traces" {
		return nil, errors.New("invalid development OTLP configuration")
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint), otlptracehttp.WithTimeout(3*time.Second))
	if err != nil {
		return nil, err
	}
	p := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithMaxQueueSize(1024), sdktrace.WithBatchTimeout(200*time.Millisecond)),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "baobab-cp"))))
	provider.Store(p)
	return func(shutdownCtx context.Context) error {
		// Never let an old exporter shutdown clear a replacement provider.
		provider.CompareAndSwap(p, nil)
		return p.Shutdown(shutdownCtx)
	}, nil
}

func spanContext(ctx context.Context, sc trace.SpanContext, parent trace.SpanID) context.Context {
	parentID := ""
	if parent.IsValid() {
		parentID = parent.String()
	}
	return context.WithValue(ctx, key{}, Context{
		TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String(), Flags: sc.TraceFlags().String(),
		ParentSpanID: parentID,
	})
}

func serverSpan(r *http.Request) (context.Context, trace.Span) {
	p := provider.Load()
	if p == nil {
		return Begin(r.Context(), r.Header.Get("traceparent")), nil
	}
	// Span names come from a closed list. Arbitrary request paths, headers,
	// credentials, subject tokens, and request/response bodies stay private.
	name := "baobab.cp.http"
	switch r.URL.Path {
	case "/internal/identity/v1/principals/resolve":
		name = "baobab.cp.buyer.principal.resolve"
	case "/internal/trade/v1/order-authority/resolve":
		name = "baobab.cp.trade.order-authority.resolve"
	case "/internal/trade/v1/price-issuer/resolve":
		name = "baobab.cp.trade.price-issuer.resolve"
	case "/v1/context/resolve":
		name = "baobab.cp.context.resolve"
	default:
		if strings.HasPrefix(r.URL.Path, "/v1/markets/") && r.Method == http.MethodGet {
			name = "baobab.cp.market.read"
		}
	}
	// Reject unsupported W3C versions and zero IDs before handing context to the SDK.
	// The local W3C parser is also used by the non-exporting fallback.
	incoming := r.Header.Get("traceparent")
	if _, valid := Parse(incoming); !valid {
		incoming = ""
	}
	headers := r.Header.Clone()
	if incoming == "" {
		headers.Del("traceparent")
	}
	ctx := (propagation.TraceContext{}).Extract(r.Context(), propagation.HeaderCarrier(headers))
	parent := trace.SpanContextFromContext(ctx)
	ctx, span := p.Tracer("baobab.cp.http").Start(ctx, name, trace.WithSpanKind(trace.SpanKindServer))
	return spanContext(ctx, span.SpanContext(), parent.SpanID()), span
}

// StartIntrospection opens the client span that the existing CP W3C injector
// propagates to the actual issuer.
func StartIntrospection(ctx context.Context) (context.Context, func(int)) {
	p := provider.Load()
	parent := trace.SpanContextFromContext(ctx)
	if p == nil || !parent.IsValid() {
		return Child(ctx), func(int) {}
	}
	ctx, span := p.Tracer("baobab.cp.http").Start(ctx, "baobab.cp.iam.introspect", trace.WithSpanKind(trace.SpanKindClient))
	return spanContext(ctx, span.SpanContext(), parent.SpanID()), func(status int) {
		endHTTPSpan(span, status)
	}
}

func endHTTPSpan(span trace.Span, status int) {
	span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status == 0 || status >= 400 {
		span.SetStatus(codes.Error, "")
	}
	span.End()
}
