package solver

import (
	"fmt"
	"sort"
	"strings"
)

const alertWidth = 66

// FormatAlert renders a WarningBroadcastPayload as a human-readable
// terminal alert block, most-urgent city (shortest T_warning) first.
func FormatAlert(payload WarningBroadcastPayload) string {
	targets := make([]TargetWarning, len(payload.Targets))
	copy(targets, payload.Targets)
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].TWarningSeconds < targets[j].TWarningSeconds
	})

	line := strings.Repeat("=", alertWidth)
	rule := strings.Repeat("-", alertWidth)

	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", line)
	fmt.Fprintf(&b, " EARTHQUAKE EARLY WARNING — RUPTURE CONFIRMED\n")
	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, " cell        : %d\n", payload.CellID)
	fmt.Fprintf(&b, " devices     : %d unique nodes\n", payload.DeviceCount)
	fmt.Fprintf(&b, " epicenter   : %.4f, %.4f\n", payload.EpicenterLat, payload.EpicenterLng)
	fmt.Fprintf(&b, " detected at : %s\n", payload.OriginTime.Format("2006-01-02 15:04:05.000"))
	fmt.Fprintf(&b, "%s\n", rule)
	fmt.Fprintf(&b, " %-12s %10s %12s   %s\n", "CITY", "DIST(km)", "S-ETA(s)", "STATUS")
	fmt.Fprintf(&b, "%s\n", rule)
	for _, tw := range targets {
		status := fmt.Sprintf("%.2fs to impact", tw.TWarningSeconds)
		if tw.BlindZone {
			status = "*** BLIND ZONE — NO WARNING ***"
		}
		fmt.Fprintf(&b, " %-12s %10.2f %12.2f   %s\n", tw.City.Name, tw.DistanceKM, tw.TWarningSeconds, status)
	}
	fmt.Fprintf(&b, "%s\n", line)
	return b.String()
}
