package solver

import (
	"strings"
	"testing"
	"time"
)

func TestFormatAlert_ListsMostUrgentCityFirstAndFlagsBlindZone(t *testing.T) {
	payload := WarningBroadcastPayload{
		CellID:       42,
		DeviceCount:  12,
		EpicenterLat: 41.30,
		EpicenterLng: 69.25,
		OriginTime:   time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC),
		Targets: []TargetWarning{
			{City: City{Name: "Samarkand"}, DistanceKM: 300.0, TWarningSeconds: 35.7, BlindZone: false},
			{City: City{Name: "Tashkent"}, DistanceKM: 1.0, TWarningSeconds: -0.5, BlindZone: true},
		},
	}

	out := FormatAlert(payload)

	if !strings.Contains(out, "Tashkent") || !strings.Contains(out, "Samarkand") {
		t.Fatalf("alert block missing a city name:\n%s", out)
	}
	if !strings.Contains(out, "BLIND ZONE") {
		t.Errorf("alert block did not flag the blind-zone city:\n%s", out)
	}

	tashkentIdx := strings.Index(out, "Tashkent")
	samarkandIdx := strings.Index(out, "Samarkand")
	if tashkentIdx == -1 || samarkandIdx == -1 || tashkentIdx > samarkandIdx {
		t.Errorf("expected the most urgent city (Tashkent, lowest T_warning) listed before Samarkand:\n%s", out)
	}
}
