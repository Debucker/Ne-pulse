package metrics

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestHistogramQuantilesTrackTrueQuantiles(t *testing.T) {
	bounds := []float64{}
	for b := 0.1; b <= 10; b += 0.1 {
		bounds = append(bounds, math.Round(b*10)/10)
	}
	h := NewHistogram(bounds...)
	rng := rand.New(rand.NewSource(1))
	var xs []float64
	for i := 0; i < 20000; i++ {
		x := rng.ExpFloat64()
		xs = append(xs, x)
		h.Observe(x)
	}
	sort.Float64s(xs)
	for _, q := range []float64{0.5, 0.95} {
		want := xs[int(q*float64(len(xs)))]
		if got := h.Quantile(q); math.Abs(got-want) > 0.1 {
			t.Fatalf("q%.2f: got %.3f want %.3f", q, got, want)
		}
	}
	if h.Count() != 20000 {
		t.Fatal("count")
	}
}

func TestPrometheusExposition(t *testing.T) {
	r := NewRegistry()
	r.Counter("a_total", "a").Add(3)
	r.Gauge("b", "b").Set(1.5)
	r.Histogram("c_seconds", "c", 1, 2).Observe(1.5)
	var sb strings.Builder
	r.WritePrometheus(&sb)
	out := sb.String()
	for _, want := range []string{"a_total 3", "b 1.5", `c_seconds_bucket{le="1"} 0`, `c_seconds_bucket{le="2"} 1`, `c_seconds_bucket{le="+Inf"} 1`, "c_seconds_count 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
}
