package sim_test

import (
	"testing"

	"ne-pulse/internal/network"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/station"
)

// TestScenarioSuite runs every scenario through the production station and
// network code and checks the physically correct outcome.
func TestScenarioSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("scenario suite is long; run without -short")
	}
	for _, sc := range sim.Suite() {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			r := sim.Run(sc, station.DefaultConfig(), network.DefaultConfig())
			t.Logf("%s: %d devices, %d units, %d eligible at quake; detected=%v false=%v picks=%v (%.1fs)",
				sc.Name, r.Devices, r.Units, r.EligibleAtQuake, r.Detected, r.FalseEvents, r.PickClasses, r.WallSec)
			for _, e := range r.Events {
				t.Logf("  %s %-12s units=%d log10p=%.1f cons=%.2f quake=%d locErr=%.1fkm latConf=%.1fs failed=%v",
					e.ID, e.Level, e.Units, e.Log10PAdj, e.Consistency, e.Quake, e.LocErrKM, e.LatConfirmed, e.FailedChecks)
			}
			if !r.Pass {
				t.Errorf("scenario expectation not met: %+v", sc.Expect)
			}
		})
	}
}
