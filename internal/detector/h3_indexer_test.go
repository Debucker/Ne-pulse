//go:build cgo

package detector

import "testing"

func TestH3Indexer_NearbyPointsShareCell(t *testing.T) {
	idx := NewH3Indexer(8)
	// Two points ~30m apart in Tashkent — well within a single res-8 cell
	// (~461m edge length, ~0.74 km^2 area).
	a := idx.CellID(41.311081, 69.240562)
	b := idx.CellID(41.311300, 69.240800)
	if a != b {
		t.Errorf("nearby points landed in different H3 cells: %d vs %d", a, b)
	}
}

func TestH3Indexer_DistantPointsDifferentCells(t *testing.T) {
	idx := NewH3Indexer(8)
	tashkent := idx.CellID(41.311081, 69.240562)
	samarkand := idx.CellID(39.627001, 66.975006) // >300km away
	if tashkent == samarkand {
		t.Error("Tashkent and Samarkand landed in the same H3 cell")
	}
}

func TestCellIDString_ReturnsNonEmptyHex(t *testing.T) {
	idx := NewH3Indexer(8)
	id := idx.CellID(41.311081, 69.240562)
	s := CellIDString(id)
	if s == "" {
		t.Error("CellIDString returned an empty string")
	}
}

func TestH3Indexer_CellCenterLandsBackInTheSameCell(t *testing.T) {
	idx := NewH3Indexer(8)
	const lat, lng = 41.311081, 69.240562
	id := idx.CellID(lat, lng)
	centerLat, centerLng := idx.CellCenter(id)

	if idx.CellID(centerLat, centerLng) != id {
		t.Errorf("CellID(CellCenter(id)) did not round-trip back to the same cell id")
	}
}

func TestNewDefaultIndexer_ReturnsH3IndexerWithCgo(t *testing.T) {
	idx := NewDefaultIndexer(8)
	if _, ok := idx.(*H3Indexer); !ok {
		t.Errorf("NewDefaultIndexer returned %T, want *H3Indexer in a cgo build", idx)
	}
}
