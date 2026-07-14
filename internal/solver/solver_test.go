package solver

import (
	"math"
	"testing"
	"time"

	"ne-pulse/internal/detector"
)

func findTarget(t *testing.T, payload WarningBroadcastPayload, name string) TargetWarning {
	t.Helper()
	for _, tw := range payload.Targets {
		if tw.City.Name == name {
			return tw
		}
	}
	t.Fatalf("no target warning found for city %q", name)
	return TargetWarning{}
}

func expectedWarning(epicenterLat, epicenterLng float64, city City) (distanceKM, tWarning float64) {
	d := HaversineKM(epicenterLat, epicenterLng, city.Lat, city.Lng)
	return d, d/VsKmS - d/VpKmS
}

func newRuptureEvent(lat, lng float64) detector.RuptureEvent {
	return detector.RuptureEvent{
		CellID:        1,
		DeviceCount:   10,
		EpicenterLat:  lat,
		EpicenterLng:  lng,
		WindowStart:   time.Now(),
		WindowEnd:     time.Now(),
		TriggerDevice: "device-x",
	}
}

// TestEvaluate_MatchesHaversineFormulaExactlyForEveryCity is the ground-
// truth math test: it independently recomputes distance and T_warning via
// the same HaversineKM primitive and asserts Evaluate's output matches
// exactly (within float tolerance) for every registered city.
func TestEvaluate_MatchesHaversineFormulaExactlyForEveryCity(t *testing.T) {
	event := newRuptureEvent(40.3864, 71.7864) // deep Fergana Valley
	payload := Evaluate(event)

	if len(payload.Targets) != len(Cities()) {
		t.Fatalf("got %d targets, want %d", len(payload.Targets), len(Cities()))
	}
	for _, city := range Cities() {
		wantDist, wantWarning := expectedWarning(event.EpicenterLat, event.EpicenterLng, city)
		got := findTarget(t, payload, city.Name)
		if math.Abs(got.DistanceKM-wantDist) > 1e-9 {
			t.Errorf("%s: DistanceKM = %v, want %v", city.Name, got.DistanceKM, wantDist)
		}
		if math.Abs(got.TWarningSeconds-wantWarning) > 1e-9 {
			t.Errorf("%s: TWarningSeconds = %v, want %v", city.Name, got.TWarningSeconds, wantWarning)
		}
		if got.BlindZone != (wantWarning <= 0) {
			t.Errorf("%s: BlindZone = %v, want %v", city.Name, got.BlindZone, wantWarning <= 0)
		}
	}
}

// TestEvaluate_EpicenterNearChirchiqFlagsTashkentButSparesFarCities models
// a rupture just outside Tashkent (Chirchiq): Tashkent should get a sharply
// reduced warning window relative to Samarkand and Bukhara, which sit far
// enough away for ample lead time.
func TestEvaluate_EpicenterNearChirchiqFlagsTashkentButSparesFarCities(t *testing.T) {
	const chirchiqLat, chirchiqLng = 41.4682, 69.5822
	payload := Evaluate(newRuptureEvent(chirchiqLat, chirchiqLng))

	tashkent := findTarget(t, payload, "Tashkent")
	samarkand := findTarget(t, payload, "Samarkand")
	bukhara := findTarget(t, payload, "Bukhara")

	if tashkent.TWarningSeconds >= samarkand.TWarningSeconds {
		t.Errorf("Tashkent warning (%.2fs) should be far shorter than Samarkand's (%.2fs)", tashkent.TWarningSeconds, samarkand.TWarningSeconds)
	}
	if tashkent.TWarningSeconds >= bukhara.TWarningSeconds {
		t.Errorf("Tashkent warning (%.2fs) should be far shorter than Bukhara's (%.2fs)", tashkent.TWarningSeconds, bukhara.TWarningSeconds)
	}
	if tashkent.TWarningSeconds > 15 {
		t.Errorf("Tashkent warning = %.2fs, want a low window this close to the epicenter", tashkent.TWarningSeconds)
	}
	if samarkand.TWarningSeconds < 25 {
		t.Errorf("Samarkand warning = %.2fs, want ample lead time (>25s)", samarkand.TWarningSeconds)
	}
	if bukhara.TWarningSeconds < 25 {
		t.Errorf("Bukhara warning = %.2fs, want ample lead time (>25s)", bukhara.TWarningSeconds)
	}
}

// TestEvaluate_DeepFerganaValleyEpicenterAndijanCloserThanTashkent proves
// the differential math tracks true geography: a Fergana Valley epicenter
// sits far closer to Andijan than to Tashkent, so Andijan's countdown must
// tick down measurably faster (a shorter T_warning), while neither city is
// yet inside the blind zone.
func TestEvaluate_DeepFerganaValleyEpicenterAndijanCloserThanTashkent(t *testing.T) {
	const ferganaLat, ferganaLng = 40.3864, 71.7864
	payload := Evaluate(newRuptureEvent(ferganaLat, ferganaLng))

	andijan := findTarget(t, payload, "Andijan")
	tashkent := findTarget(t, payload, "Tashkent")

	if andijan.BlindZone || tashkent.BlindZone {
		t.Fatalf("neither city should be inside the blind zone for a Fergana Valley epicenter: andijan=%v tashkent=%v", andijan.BlindZone, tashkent.BlindZone)
	}
	if andijan.TWarningSeconds >= tashkent.TWarningSeconds {
		t.Errorf("Andijan warning (%.2fs) should be shorter than Tashkent's (%.2fs) for a Fergana Valley epicenter", andijan.TWarningSeconds, tashkent.TWarningSeconds)
	}

	wantDist, wantWarning := expectedWarning(ferganaLat, ferganaLng, City{Name: "Andijan", Lat: 40.7821, Lng: 72.3442})
	if math.Abs(andijan.DistanceKM-wantDist) > 1e-9 || math.Abs(andijan.TWarningSeconds-wantWarning) > 1e-9 {
		t.Errorf("Andijan distance/warning mismatch: got (%.6f, %.6f), want (%.6f, %.6f)", andijan.DistanceKM, andijan.TWarningSeconds, wantDist, wantWarning)
	}
}

// TestEvaluate_PropagatesMagnitudeFromRuptureEvent proves the radar's
// estimated Magnitude survives unchanged through to the broadcast payload
// — Evaluate must not drop, zero out, or recompute it.
func TestEvaluate_PropagatesMagnitudeFromRuptureEvent(t *testing.T) {
	event := newRuptureEvent(41.4682, 69.5822)
	event.Magnitude = 6.7

	payload := Evaluate(event)
	if payload.Magnitude != 6.7 {
		t.Errorf("Magnitude = %v, want 6.7", payload.Magnitude)
	}
}

// TestEvaluate_EpicenterAtCityCentreIsBlindZone proves the T_warning <= 0
// boundary is correctly flagged for a rupture directly under a city.
func TestEvaluate_EpicenterAtCityCentreIsBlindZone(t *testing.T) {
	payload := Evaluate(newRuptureEvent(41.2995, 69.2401)) // exactly Tashkent's coordinates
	tashkent := findTarget(t, payload, "Tashkent")
	if !tashkent.BlindZone {
		t.Errorf("epicenter at Tashkent's own coordinates should flag BlindZone=true, got TWarningSeconds=%v", tashkent.TWarningSeconds)
	}
	if tashkent.DistanceKM > 1e-6 {
		t.Errorf("DistanceKM = %v, want ~0 for a colocated epicenter", tashkent.DistanceKM)
	}
}
