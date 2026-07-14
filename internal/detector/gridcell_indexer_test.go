//go:build !cgo

package detector

import "testing"

func TestGridCellIndexer_NearbyPointsShareCell(t *testing.T) {
	idx := NewGridCellIndexer(8)
	a := idx.CellID(41.311081, 69.240562)
	b := idx.CellID(41.311300, 69.240800) // ~30m away
	if a != b {
		t.Errorf("nearby points landed in different grid cells: %d vs %d", a, b)
	}
}

func TestGridCellIndexer_DistantPointsDifferentCells(t *testing.T) {
	idx := NewGridCellIndexer(8)
	tashkent := idx.CellID(41.311081, 69.240562)
	samarkand := idx.CellID(39.627001, 66.975006) // >300km away
	if tashkent == samarkand {
		t.Error("Tashkent and Samarkand landed in the same grid cell")
	}
}

func TestGridCellIndexer_CellCenterRoundTripsNearOriginalPoint(t *testing.T) {
	idx := NewGridCellIndexer(8)
	const lat, lng = 41.311081, 69.240562
	id := idx.CellID(lat, lng)
	centerLat, centerLng := idx.CellCenter(id)

	// The center must land back inside the same cell the original point
	// mapped to (within roughly one cell width of the original point).
	if abs(centerLat-lat) > idx.cellSizeDeg || abs(centerLng-lng) > idx.cellSizeDeg {
		t.Errorf("CellCenter(%d) = (%.6f, %.6f), too far from original point (%.6f, %.6f)", id, centerLat, centerLng, lat, lng)
	}
	if idx.CellID(centerLat, centerLng) != id {
		t.Errorf("CellID(CellCenter(id)) did not round-trip back to the same cell id")
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestNewGridCellIndexer_LowerResolutionProducesCoarserCells(t *testing.T) {
	fine := NewGridCellIndexer(8)
	coarse := NewGridCellIndexer(4)
	if coarse.cellSizeDeg <= fine.cellSizeDeg {
		t.Fatalf("resolution 4 cellSizeDeg = %v, want it larger than resolution 8's %v", coarse.cellSizeDeg, fine.cellSizeDeg)
	}

	// Two points ~5km apart should typically still share a coarse res-4
	// cell (~22km edge) but land in different fine res-8 cells (~461m
	// edge) — proving the resolution argument actually changes behavior,
	// not just the stored field.
	const lat1, lng1 = 41.30, 69.24
	const lat2, lng2 = 41.34, 69.24 // ~4.4km further north
	if fine.CellID(lat1, lng1) == fine.CellID(lat2, lng2) {
		t.Error("expected the fine (res 8) indexer to split these two ~4.4km-apart points into different cells")
	}
	if coarse.CellID(lat1, lng1) != coarse.CellID(lat2, lng2) {
		t.Error("expected the coarse (res 4) indexer to keep these two ~4.4km-apart points in the same cell")
	}
}

func TestNewDefaultIndexer_ReturnsGridCellIndexerWithoutCgo(t *testing.T) {
	idx := NewDefaultIndexer(8)
	if _, ok := idx.(*GridCellIndexer); !ok {
		t.Errorf("NewDefaultIndexer returned %T, want *GridCellIndexer in a cgo-less build", idx)
	}
}
