// Package solver evaluates confirmed tectonic RuptureEvents into a
// per-city S-wave early-warning countdown, using the classic P-wave/S-wave
// velocity differential that underlies every real earthquake early-warning
// system: the sensor grid trips on the faster (but harmless) P-wave, buying
// a lead window before the slower, destructive S-wave physically arrives.
package solver

import (
	"time"

	"ne-pulse/internal/detector"
)

const (
	// VpKmS is the primary (compression) wave velocity in km/s — the wave
	// that trips the sensor grid.
	VpKmS = 6.0

	// VsKmS is the secondary (shear) wave velocity in km/s — the slower,
	// destructive wave the early-warning countdown is timed against.
	VsKmS = 3.5
)

// TargetWarning is one city's early-warning countdown for a single
// confirmed rupture.
type TargetWarning struct {
	City            City    `json:"city"`
	DistanceKM      float64 `json:"distanceKm"`
	TWarningSeconds float64 `json:"tWarningSeconds"` // remaining S-wave arrival buffer; <= 0 means already inside the blind zone
	BlindZone       bool    `json:"blindZone"`
}

// WarningBroadcastPayload is the fully computed early-warning fan-out for
// one confirmed RuptureEvent. JSON field names are camelCase (rather than
// bare Go field names) since this struct is broadcast directly to the
// Next.js dashboard over the telemetry websocket; CellID is serialized as a
// string (`,string` tag) so a uint64 wider than 2^53 doesn't silently lose
// precision going through JavaScript's float64 numbers.
type WarningBroadcastPayload struct {
	EpicenterLat float64 `json:"epicenterLat"`
	EpicenterLng float64 `json:"epicenterLng"`
	CellID       uint64  `json:"cellId,string"`
	DeviceCount  int     `json:"deviceCount"`

	// Magnitude is the radar's empirical estimate derived from peak
	// acceleration norm during the confirming coincidence (see
	// detector.estimateMagnitude), clamped to [4.0, 8.5].
	Magnitude float64 `json:"magnitude"`

	// OriginTime is treated as the rupture's detection time (the radar's
	// WindowEnd — when the triggering reading arrived). Given ne-pulse's
	// distributed edge layout, detection latency is negligible next to
	// city-scale wave propagation delay, so t_detect ≈ t_origin and
	// T_warning collapses to the pure velocity-differential buffer below.
	OriginTime time.Time `json:"originTime"`

	Targets []TargetWarning `json:"targets"`
}

// Evaluate computes the S-wave arrival countdown for every registered city
// given a confirmed rupture:
//
//	T_warning = D/Vs - D/Vp
//
// where D is the great-circle distance from the epicenter to the city. A
// city with T_warning <= 0 is already inside the Zone of Immediate Impact
// (the "blind zone") — the destructive wave arrives before any warning
// could possibly help.
func Evaluate(event detector.RuptureEvent) WarningBroadcastPayload {
	cities := Cities()
	payload := WarningBroadcastPayload{
		EpicenterLat: event.EpicenterLat,
		EpicenterLng: event.EpicenterLng,
		CellID:       event.CellID,
		DeviceCount:  event.DeviceCount,
		Magnitude:    event.Magnitude,
		OriginTime:   event.WindowEnd,
		Targets:      make([]TargetWarning, 0, len(cities)),
	}

	for _, city := range cities {
		distance := HaversineKM(event.EpicenterLat, event.EpicenterLng, city.Lat, city.Lng)
		tWarning := distance/VsKmS - distance/VpKmS
		payload.Targets = append(payload.Targets, TargetWarning{
			City:            city,
			DistanceKM:      distance,
			TWarningSeconds: tWarning,
			BlindZone:       tWarning <= 0,
		})
	}
	return payload
}
