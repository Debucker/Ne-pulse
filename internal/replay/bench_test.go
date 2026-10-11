package replay

import (
	"math/rand"
	"testing"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/station"
)

func BenchmarkStationPush(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	var s []dsp.Sample3
	sim.Stream(rng, sim.TypicalPhone(0), nil, 0, 400, func(x dsp.Sample3) { s = append(s, x) })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := station.New(station.DefaultConfig())
		for _, x := range s {
			p.Push(x, func(station.Output) {})
		}
	}
	b.ReportMetric(float64(len(s)), "samples/op")
}
