// Package metrics exposes Control Plane metrics in the Prometheus text
// exposition format (version 0.0.4) without an external client library.
//
// Counters count events in this process; collectors report current state
// read on scrape. Label names are fixed per metric and label values come
// from bounded vocabularies: identifiers, names and other high-cardinality
// values are never labels (ADR-BCP-008 section 44, ADR-BCP-018 section 130).
package metrics

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Kind is a metric type.
type Kind string

const (
	Counter Kind = "counter"
	Gauge   Kind = "gauge"
)

// Sample is one labelled value of a metric.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// Family is one metric with its samples.
type Family struct {
	Name    string
	Help    string
	Kind    Kind
	Samples []Sample
}

// Collector reports metric families read at scrape time.
type Collector interface {
	Collect(ctx context.Context) ([]Family, error)
}

// Registry holds counters and collectors and serves their exposition.
type Registry struct {
	mu         sync.Mutex
	counters   []*CounterVec
	collectors []Collector
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Default is the process-wide registry the Control Plane serves.
var Default = NewRegistry()

// CounterVec is a counter with a fixed set of label names.
type CounterVec struct {
	name, help string
	labels     []string
	mu         sync.RWMutex
	values     map[string]*atomic.Uint64
}

// NewCounterVec registers a counter. It panics on an invalid or duplicate
// name, which is a programming error caught at start-up.
func (r *Registry) NewCounterVec(name, help string, labels ...string) *CounterVec {
	if !validName(name) {
		panic(fmt.Sprintf("metrics: invalid metric name %q", name))
	}
	for _, l := range labels {
		if !validName(l) {
			panic(fmt.Sprintf("metrics: invalid label name %q", l))
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.counters {
		if c.name == name {
			panic(fmt.Sprintf("metrics: duplicate metric %q", name))
		}
	}
	c := &CounterVec{name: name, help: help, labels: labels, values: map[string]*atomic.Uint64{}}
	r.counters = append(r.counters, c)
	return c
}

// Add adds n to the counter for the given label values, in label order.
// A call with the wrong number of values is ignored rather than recorded
// under a malformed series.
func (c *CounterVec) Add(n uint64, values ...string) {
	if len(values) != len(c.labels) {
		return
	}
	key := strings.Join(values, "\xff")
	c.mu.RLock()
	v, ok := c.values[key]
	c.mu.RUnlock()
	if !ok {
		c.mu.Lock()
		if v, ok = c.values[key]; !ok {
			v = &atomic.Uint64{}
			c.values[key] = v
		}
		c.mu.Unlock()
	}
	v.Add(n)
}

// Inc adds one.
func (c *CounterVec) Inc(values ...string) { c.Add(1, values...) }

// Value returns the current count for the label values (for tests).
func (c *CounterVec) Value(values ...string) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.values[strings.Join(values, "\xff")]; ok {
		return v.Load()
	}
	return 0
}

func (c *CounterVec) family() Family {
	f := Family{Name: c.name, Help: c.help, Kind: Counter}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for key, v := range c.values {
		labels := map[string]string{}
		if len(c.labels) > 0 {
			for i, value := range strings.Split(key, "\xff") {
				labels[c.labels[i]] = value
			}
		}
		f.Samples = append(f.Samples, Sample{Labels: labels, Value: float64(v.Load())})
	}
	if len(c.labels) == 0 && len(f.Samples) == 0 {
		f.Samples = []Sample{{Value: 0}}
	}
	return f
}

// Register adds a collector.
func (r *Registry) Register(c Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors = append(r.collectors, c)
}

// Gather returns every family, sorted by name, samples sorted by labels.
// A failing collector's families are omitted and its error returned, so a
// scrape still serves the counters.
func (r *Registry) Gather(ctx context.Context) ([]Family, error) {
	r.mu.Lock()
	counters := append([]*CounterVec(nil), r.counters...)
	collectors := append([]Collector(nil), r.collectors...)
	r.mu.Unlock()
	var families []Family
	for _, c := range counters {
		families = append(families, c.family())
	}
	var firstErr error
	for _, c := range collectors {
		fs, err := c.Collect(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		families = append(families, fs...)
	}
	sort.Slice(families, func(i, j int) bool { return families[i].Name < families[j].Name })
	for i := range families {
		sort.Slice(families[i].Samples, func(a, b int) bool {
			return labelKey(families[i].Samples[a].Labels) < labelKey(families[i].Samples[b].Labels)
		})
	}
	return families, firstErr
}

// Handler serves the exposition. A collector failure is reported as a
// scrape error metric rather than failing the whole scrape.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		families, err := r.Gather(req.Context())
		failed := 0.0
		if err != nil {
			failed = 1
		}
		families = append(families, Family{Name: "metrics_collection_failed", Help: "1 when a state collector failed during this scrape.",
			Kind: Gauge, Samples: []Sample{{Value: failed}}})
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		out := bufio.NewWriter(w)
		for _, f := range families {
			writeFamily(out, f)
		}
		_ = out.Flush()
	})
}

func writeFamily(w *bufio.Writer, f Family) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", f.Name, escapeHelp(f.Help), f.Name, f.Kind)
	for _, s := range f.Samples {
		w.WriteString(f.Name)
		if len(s.Labels) > 0 {
			names := make([]string, 0, len(s.Labels))
			for n := range s.Labels {
				names = append(names, n)
			}
			sort.Strings(names)
			w.WriteByte('{')
			for i, n := range names {
				if i > 0 {
					w.WriteByte(',')
				}
				fmt.Fprintf(w, `%s="%s"`, n, escapeLabel(s.Labels[n]))
			}
			w.WriteByte('}')
		}
		w.WriteByte(' ')
		w.WriteString(strconv.FormatFloat(s.Value, 'g', -1, 64))
		w.WriteByte('\n')
	}
}

func labelKey(labels map[string]string) string {
	names := make([]string, 0, len(labels))
	for n := range labels {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n + "=" + labels[n] + "\xff")
	}
	return b.String()
}

func escapeLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(s)
}

func escapeHelp(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`).Replace(s)
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// CachedCollector serves a collector's last result for TTL, so frequent
// scrapes do not turn into frequent database reads. Concurrent scrapes
// share one refresh.
type CachedCollector struct {
	Collector Collector
	TTL       time.Duration
	Now       func() time.Time

	mu       sync.Mutex
	at       time.Time
	families []Family
}

func (c *CachedCollector) Collect(ctx context.Context) ([]Family, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.families != nil && now().Sub(c.at) < c.TTL {
		return c.families, nil
	}
	families, err := c.Collector.Collect(ctx)
	if err != nil {
		return nil, err
	}
	c.families, c.at = families, now()
	return families, nil
}
