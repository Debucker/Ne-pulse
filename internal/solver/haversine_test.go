package solver

import (
	"math"
	"testing"
)

func TestHaversineKM_ZeroDistanceForIdenticalPoints(t *testing.T) {
	d := HaversineKM(41.2995, 69.2401, 41.2995, 69.2401)
	if math.Abs(d) > 1e-9 {
		t.Errorf("HaversineKM(same point) = %v, want 0", d)
	}
}

func TestHaversineKM_OneDegreeLongitudeAtEquator(t *testing.T) {
	d := HaversineKM(0, 0, 0, 1)
	const want = 111.19 // km, one degree of longitude at the equator
	if math.Abs(d-want) > 0.5 {
		t.Errorf("HaversineKM(1 deg lng at equator) = %v, want ~%v", d, want)
	}
}

func TestHaversineKM_SymmetricRegardlessOfArgumentOrder(t *testing.T) {
	a := HaversineKM(41.2995, 69.2401, 39.6542, 66.9597)
	b := HaversineKM(39.6542, 66.9597, 41.2995, 69.2401)
	if math.Abs(a-b) > 1e-9 {
		t.Errorf("HaversineKM not symmetric: %v vs %v", a, b)
	}
}

func TestHaversineKM_TashkentToSamarkandMatchesKnownDistance(t *testing.T) {
	d := HaversineKM(41.2995, 69.2401, 39.6542, 66.9597)
	// Known great-circle distance is ~280km; allow generous tolerance since
	// this only guards against a gross formula error, not precision.
	if d < 260 || d > 300 {
		t.Errorf("Tashkent-Samarkand distance = %.2f km, want ~280km", d)
	}
}
