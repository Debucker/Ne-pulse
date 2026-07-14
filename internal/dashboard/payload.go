// Package dashboard aggregates the live telemetry stream into a
// lightweight, browser-friendly summary — H3 cell density weights plus
// confirmed rupture alerts — and pushes it out over a websocket hub. It
// exists specifically to avoid flooding the frontend with one message per
// device tick: raw per-frame updates for thousands of concurrent devices
// would blow well past a browser's paint budget, so ticks are aggregated
// into cell weights over a short window (100-200ms) before anything is
// sent down the wire.
package dashboard

import (
	"time"

	"ne-pulse/internal/solver"
)

// CellWeight is one geographic cell's activity count for the current
// aggregation window, with its centroid already resolved server-side so
// the frontend needs no H3/geodesic logic of its own to place a marker.
type CellWeight struct {
	CellID string  `json:"cellId"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Weight int     `json:"weight"`
}

// TelemetrySnapshot is the periodic, aggregated broadcast: active cell
// density across the whole fleet since the last snapshot.
type TelemetrySnapshot struct {
	Type      string       `json:"type"`
	Cells     []CellWeight `json:"cells"`
	Timestamp time.Time    `json:"timestamp"`
}

// RuptureAlert wraps a confirmed rupture's early-warning broadcast for
// delivery to dashboard clients.
type RuptureAlert struct {
	Type    string                         `json:"type"`
	Payload solver.WarningBroadcastPayload `json:"payload"`
}
