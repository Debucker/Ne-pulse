package geo

import (
	"math"
	"math/rand"
	"testing"
)

func TestHaversineKnownDistances(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lng1, lat2, lng2 float64
		want, tol              float64
	}{
		{"same point", 41.3, 69.24, 41.3, 69.24, 0, 1e-9},
		// One degree of latitude along a meridian = 2*pi*R/360.
		{"1 deg meridian", 0, 0, 1, 0, KMPerDegLat, 1e-6},
		// Quarter great circle: pole to equator.
		{"pole to equator", 90, 0, 0, 0, math.Pi * EarthRadiusKM / 2, 1e-6},
		// Antipodes = half circumference.
		{"antipodal", 0, 0, 0, 180, math.Pi * EarthRadiusKM, 1e-6},
		// Tashkent -> Samarkand, ~ 266 km great-circle (independent check
		// with a Vincenty calculator gives 266.4 km on WGS84).
		{"Tashkent-Samarkand", 41.2995, 69.2401, 39.6270, 66.9750, 266.0, 2.0},
	}
	for _, c := range cases {
		got := HaversineKM(c.lat1, c.lng1, c.lat2, c.lng2)
		if math.Abs(got-c.want) > c.tol {
			t.Errorf("%s: got %.6f km, want %.6f ± %.6f", c.name, got, c.want, c.tol)
		}
	}
}

func TestDestinationRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 500; i++ {
		lat := rng.Float64()*140 - 70
		lng := rng.Float64()*360 - 180
		d := rng.Float64() * 500
		b := rng.Float64() * 360
		lat2, lng2 := Destination(lat, lng, b, d)
		if got := HaversineKM(lat, lng, lat2, lng2); math.Abs(got-d) > 1e-6*math.Max(1, d) {
			t.Fatalf("destination distance %v want %v", got, d)
		}
	}
}

func TestLocalXYAccuracy(t *testing.T) {
	// Inside 100 km the equirectangular projection must agree with the
	// great-circle distance to better than 0.5 %.
	rng := rand.New(rand.NewSource(4))
	for i := 0; i < 1000; i++ {
		lat0 := rng.Float64()*120 - 60
		lng0 := rng.Float64()*360 - 180
		lat, lng := Destination(lat0, lng0, rng.Float64()*360, rng.Float64()*100)
		x, y := LocalXY(lat0, lng0, lat, lng)
		d := HaversineKM(lat0, lng0, lat, lng)
		if d < 1 {
			continue
		}
		if e := math.Abs(math.Hypot(x, y)-d) / d; e > 0.005 {
			t.Fatalf("local projection error %.4f at lat0=%.2f d=%.1f", e, lat0, d)
		}
		la, lo := FromLocalXY(lat0, lng0, x, y)
		if HaversineKM(la, lo, lat, lng) > 1e-6 {
			t.Fatalf("FromLocalXY does not invert LocalXY")
		}
	}
}

func TestGridCellsContainTheirPointsAndHaveExpectedSize(t *testing.T) {
	g := Grid{CellKM: 1}
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 20000; i++ {
		lat := rng.Float64()*150 - 75
		lng := rng.Float64()*360 - 180
		c := g.CellOf(lat, lng)
		if !g.Valid(c) {
			t.Fatalf("invalid cell %v", c)
		}
		clat, clng := g.Center(c)
		// A point is at most half a cell diagonal from its centre.
		if d := HaversineKM(lat, lng, clat, clng); d > 0.5*math.Sqrt2*1.02 {
			t.Fatalf("point %.5f,%.5f is %.3f km from its cell centre", lat, lng, d)
		}
		if got := g.CellOf(clat, clng); got != c {
			t.Fatalf("centre of %v maps to %v", c, got)
		}
		if CellFromKey(c.Key()) != c {
			t.Fatalf("key round trip failed for %v", c)
		}
	}
}

func TestGridCoarsenNests(t *testing.T) {
	fine, coarse := Grid{CellKM: 1}, Grid{CellKM: 10}
	lat, lng := 41.31, 69.28
	c := fine.CellOf(lat, lng)
	cc := fine.Coarsen(c, coarse)
	clat, clng := coarse.Center(cc)
	if HaversineKM(lat, lng, clat, clng) > 10 {
		t.Fatalf("coarse cell centre too far from original point")
	}
}

// Golden values shared with web/lib/sensor/grid.test.ts so the browser and
// server agree on cell arithmetic.
func TestGridGolden(t *testing.T) {
	g := Grid{CellKM: 1}
	golden := []struct {
		lat, lng float64
		want     Cell
	}{
		{41.2995, 69.2401, Cell{Row: 14599, Col: 20821}},
		{39.6270, 66.9750, Cell{Row: 14413, Col: 21152}},
		{-33.8688, 151.2093, Cell{Row: 6241, Col: 30579}},
		{0, 0, Cell{Row: 10007, Col: 20015}},
	}
	for _, c := range golden {
		if got := g.CellOf(c.lat, c.lng); got != c.want {
			t.Errorf("CellOf(%v,%v) = %v, want %v", c.lat, c.lng, got, c.want)
		}
	}
}
