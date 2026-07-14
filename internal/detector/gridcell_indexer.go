//go:build !cgo

package detector

import "math"

// GridCellIndexer is a pure-Go stand-in for H3Indexer, used automatically
// when cgo is unavailable (see h3_indexer.go — H3's reference
// implementation is a C library, so github.com/uber/h3-go cannot build
// without a C toolchain). It quantizes lat/lng onto a fixed equirectangular
// grid sized to approximate the requested H3 resolution's cell footprint,
// which is all SpatialRadar actually needs: a stable, collision-resistant
// bucket key for nearby coordinates — not true geodesic hexagons. Deploy
// behind a cgo-enabled build (e.g. a normal Linux container with gcc) to
// get real H3Indexer instead.
type GridCellIndexer struct {
	cellSizeDeg float64
}

// res8CellSizeDeg is H3 resolution 8's approximate edge length (~461m)
// expressed in degrees of latitude at the equator (111.32 km/degree).
const res8CellSizeDeg = 0.00414

// NewGridCellIndexer builds the fallback indexer, sized to approximate the
// given H3 resolution. Each H3 resolution step subdivides a cell into
// roughly 7 children, so cell area scales by ~7x and edge length (and thus
// this grid's degree-sized step) scales by ~sqrt(7)x per resolution level
// away from 8, the resolution H3Indexer defaults to elsewhere in this
// package.
func NewGridCellIndexer(resolution int) *GridCellIndexer {
	if resolution <= 0 {
		resolution = 8
	}
	scale := math.Pow(math.Sqrt(7), float64(8-resolution))
	return &GridCellIndexer{cellSizeDeg: res8CellSizeDeg * scale}
}

// CellID quantizes lat/lng onto the grid and packs the two cell coordinates
// into a single uint64 — no string formatting, no heap allocation.
func (idx *GridCellIndexer) CellID(lat, lng float64) uint64 {
	latCell := int32(math.Floor(lat / idx.cellSizeDeg))
	lngCell := int32(math.Floor(lng / idx.cellSizeDeg))
	return uint64(uint32(latCell))<<32 | uint64(uint32(lngCell))
}

// CellCenter reverses a packed cell key back into the centroid of its grid
// square. Used by the dashboard aggregator so the frontend can place a
// density marker without needing any indexing logic of its own.
func (idx *GridCellIndexer) CellCenter(cellID uint64) (lat, lng float64) {
	latCell := int32(uint32(cellID >> 32))
	lngCell := int32(uint32(cellID))
	lat = (float64(latCell) + 0.5) * idx.cellSizeDeg
	lng = (float64(lngCell) + 0.5) * idx.cellSizeDeg
	return lat, lng
}

// NewDefaultIndexer returns the production CellIndexer for this build. Note
// this is the pure-Go approximation, not real H3 — see the type doc above.
func NewDefaultIndexer(resolution int) CellIndexer {
	return NewGridCellIndexer(resolution)
}
