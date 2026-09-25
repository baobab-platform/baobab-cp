package metrics

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type staticCollector struct {
	families []Family
	err      error
	calls    int
}

func (s *staticCollector) Collect(context.Context) ([]Family, error) {
	s.calls++
	return s.families, s.err
}

func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("content type %q", ct)
	}
	return w.Body.String()
}

func TestExpositionIsDeterministicAndEscaped(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounterVec("b_total", "Counts b.", "outcome")
	c.Inc("x")
	c.Add(2, "a\"b\\c\nd")
	c.Inc() // wrong arity: ignored, never a malformed series
	plain := r.NewCounterVec("a_total", "Line one\nline two.")
	r.Register(&staticCollector{families: []Family{{Name: "c_gauge", Help: "Gauge.", Kind: Gauge,
		Samples: []Sample{{Labels: map[string]string{"status": "ACTIVE", "relationship_type": "OWNS"}, Value: 3}}}}})
	_ = plain

	want := strings.Join([]string{
		"# HELP a_total Line one\\nline two.",
		"# TYPE a_total counter",
		"a_total 0",
		"# HELP b_total Counts b.",
		"# TYPE b_total counter",
		`b_total{outcome="a\"b\\c\nd"} 2`,
		`b_total{outcome="x"} 1`,
		"# HELP c_gauge Gauge.",
		"# TYPE c_gauge gauge",
		`c_gauge{relationship_type="OWNS",status="ACTIVE"} 3`,
		"# HELP metrics_collection_failed 1 when a state collector failed during this scrape.",
		"# TYPE metrics_collection_failed gauge",
		"metrics_collection_failed 0",
	}, "\n") + "\n"
	if got := scrape(t, r); got != want {
		t.Fatalf("exposition:\n%s\nwant:\n%s", got, want)
	}
	if c.Value("x") != 1 || c.Value() != 0 {
		t.Fatalf("values: x=%d none=%d", c.Value("x"), c.Value())
	}
}

func TestFailingCollectorKeepsCountersAndReportsFailure(t *testing.T) {
	r := NewRegistry()
	r.NewCounterVec("events_total", "Events.").Inc()
	r.Register(&staticCollector{err: errors.New("db down")})
	got := scrape(t, r)
	if !strings.Contains(got, "events_total 1\n") || !strings.Contains(got, "metrics_collection_failed 1\n") {
		t.Fatalf("a failing collector must not hide counters and must be reported:\n%s", got)
	}
}

func TestCachedCollectorRefreshesAfterTTL(t *testing.T) {
	now := time.Unix(0, 0)
	inner := &staticCollector{families: []Family{{Name: "g", Kind: Gauge}}}
	c := &CachedCollector{Collector: inner, TTL: 30 * time.Second, Now: func() time.Time { return now }}
	for i := 0; i < 3; i++ {
		if _, err := c.Collect(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if inner.calls != 1 {
		t.Fatalf("within the TTL the collector must be read once, was %d", inner.calls)
	}
	now = now.Add(31 * time.Second)
	if _, err := c.Collect(context.Background()); err != nil || inner.calls != 2 {
		t.Fatalf("after the TTL it must refresh: calls=%d %v", inner.calls, err)
	}
	inner.err = errors.New("db down")
	now = now.Add(31 * time.Second)
	if _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("a refresh failure must surface, not serve stale data silently")
	}
}

func TestInvalidOrDuplicateNamesPanic(t *testing.T) {
	for name, register := range map[string]func(r *Registry){
		"invalid name":  func(r *Registry) { r.NewCounterVec("bad-name", "x") },
		"invalid label": func(r *Registry) { r.NewCounterVec("ok_total", "x", "tenant-id") },
		"duplicate":     func(r *Registry) { r.NewCounterVec("dup_total", "x"); r.NewCounterVec("dup_total", "x") },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			register(NewRegistry())
		})
	}
}
