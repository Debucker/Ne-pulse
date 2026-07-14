package solver

import "math"

// EarthRadiusKM is the mean Earth radius used for great-circle distance.
const EarthRadiusKM = 6371.0088

// HaversineKM computes the great-circle distance in kilometers between two
// lat/lng points using the Haversine formula, using only primitive float64
// trig — no allocation, safe to call on every rupture evaluation.
func HaversineKM(lat1, lng1, lat2, lng2 float64) float64 {
	const degToRad = math.Pi / 180

	lat1Rad := lat1 * degToRad
	lat2Rad := lat2 * degToRad
	dLat := (lat2 - lat1) * degToRad
	dLng := (lng2 - lng1) * degToRad

	sinDLat2 := math.Sin(dLat / 2)
	sinDLng2 := math.Sin(dLng / 2)

	a := sinDLat2*sinDLat2 + math.Cos(lat1Rad)*math.Cos(lat2Rad)*sinDLng2*sinDLng2
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return EarthRadiusKM * c
}
