// Package metrics provides lock-free counters, gauges and fixed-bucket
// histograms with Prometheus text exposition and a JSON snapshot.
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"
)

// Counter is monotonically increasing.
type Counter struct{ v atomic.Int64 }

func (c *Counter) Add(n int64)  { c.v.Add(n) }
func (c *Counter) Inc()         { c.v.Add(1) }
func (c *Counter) Value() int64 { return c.v.Load() }

// Gauge holds a current value.
type Gauge struct{ v atomic.Uint64 }

func (g *Gauge) Set(x float64)  { g.v.Store(math.Float64bits(x)) }
func (g *Gauge) Value() float64 { return math.Float64frombits(g.v.Load()) }

// Histogram counts observations into fixed upper-bound buckets.
type Histogram struct {
	bounds []float64
	counts []atomic.Int64
	sum    atomic.Uint64
	n      atomic.Int64
}

// NewHistogram builds a histogram with the given ascending bucket bounds.
func NewHistogram(bounds ...float64) *Histogram {
	return &Histogram{bounds: bounds, counts: make([]atomic.Int64, len(bounds)+1)}
}

// Observe records x.
func (h *Histogram) Observe(x float64) {
	i := sort.SearchFloat64s(h.bounds, x)
	h.counts[i].Add(1)
	h.n.Add(1)
	for {
		old := h.sum.Load()
		if h.sum.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+x)) {
			return
		}
	}
}

// Quantile estimates the q-quantile by linear interpolation within buckets.
func (h *Histogram) Quantile(q float64) float64 {
	total := h.n.Load()
	if total == 0 {
		return 0
	}
	target := q * float64(total)
	var cum float64
	for i := range h.counts {
		c := float64(h.counts[i].Load())
		if cum+c >= target {
			lo := 0.0
			if i > 0 {
				lo = h.bounds[i-1]
			}
			hi := lo
			if i < len(h.bounds) {
				hi = h.bounds[i]
			}
			if c == 0 {
				return hi
			}
			return lo + (hi-lo)*(target-cum)/c
		}
		cum += c
	}
	return h.bounds[len(h.bounds)-1]
}

// Count returns the number of observations.
func (h *Histogram) Count() int64 { return h.n.Load() }

// Registry names metrics.
type Registry struct {
	mu     sync.Mutex
	counts map[string]*Counter
	gauges map[string]*Gauge
	hists  map[string]*Histogram
	help   map[string]string
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{counts: map[string]*Counter{}, gauges: map[string]*Gauge{}, hists: map[string]*Histogram{}, help: map[string]string{}}
}

// Counter returns (creating) a counter.
func (r *Registry) Counter(name, help string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counts[name]; ok {
		return c
	}
	c := &Counter{}
	r.counts[name], r.help[name] = c, help
	return c
}

// Gauge returns (creating) a gauge.
func (r *Registry) Gauge(name, help string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &Gauge{}
	r.gauges[name], r.help[name] = g, help
	return g
}

// Histogram returns (creating) a histogram.
func (r *Registry) Histogram(name, help string, bounds ...float64) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.hists[name]; ok {
		return h
	}
	h := NewHistogram(bounds...)
	r.hists[name], r.help[name] = h, help
	return h
}

// Snapshot returns every metric as a flat map (histograms as p50/p95/count).
func (r *Registry) Snapshot() map[string]float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]float64{}
	for k, c := range r.counts {
		out[k] = float64(c.Value())
	}
	for k, g := range r.gauges {
		out[k] = g.Value()
	}
	for k, h := range r.hists {
		out[k+"_p50"] = h.Quantile(0.5)
		out[k+"_p95"] = h.Quantile(0.95)
		out[k+"_count"] = float64(h.Count())
	}
	return out
}

// WritePrometheus writes the text exposition format.
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := func(m map[string]bool) []string {
		var s []string
		for k := range m {
			s = append(s, k)
		}
		sort.Strings(s)
		return s
	}
	cs, gs, hs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for k := range r.counts {
		cs[k] = true
	}
	for k := range r.gauges {
		gs[k] = true
	}
	for k := range r.hists {
		hs[k] = true
	}
	for _, k := range names(cs) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", k, r.help[k], k, k, r.counts[k].Value())
	}
	for _, k := range names(gs) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %g\n", k, r.help[k], k, k, r.gauges[k].Value())
	}
	for _, k := range names(hs) {
		h := r.hists[k]
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", k, r.help[k], k)
		var cum int64
		for i, b := range h.bounds {
			cum += h.counts[i].Load()
			fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", k, b, cum)
		}
		cum += h.counts[len(h.bounds)].Load()
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n%s_sum %g\n%s_count %d\n", k, cum, k, math.Float64frombits(h.sum.Load()), k, cum)
	}
}
