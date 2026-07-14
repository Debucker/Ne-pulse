//go:build cgo

package detector

import h3 "github.com/uber/h3-go/v3"

// H3Indexer indexes coordinates into real H3 hexagonal cells via
// uber/h3-go's cgo bindings to the official H3 C library. This file only
// builds when cgo is available — H3's reference implementation is a C
// library, so github.com/uber/h3-go cannot compile without a C toolchain.
// See gridcell_indexer.go for the pure-Go fallback used otherwise (e.g. in
// a cgo-less sandbox); NewDefaultIndexer picks whichever this build
// actually has.
type H3Indexer struct {
	Resolution int
}

// NewH3Indexer builds an indexer at the given H3 resolution (8 isolates
// geographic regions down to roughly 0.7 km^2, per the H3 spec).
func NewH3Indexer(resolution int) *H3Indexer {
	if resolution <= 0 {
		resolution = 8
	}
	return &H3Indexer{Resolution: resolution}
}

// CellID converts lat/lng into an H3 index and returns it as a raw uint64
// — the same bits h3.ToString would render as hex, just without the
// allocation, so the ingestion hot path never allocates purely to compute a
// map key.
func (idx *H3Indexer) CellID(lat, lng float64) uint64 {
	cell := h3.FromGeo(h3.GeoCoord{Latitude: lat, Longitude: lng}, idx.Resolution)
	return uint64(cell)
}

// CellIDString renders a cell's canonical hex form (e.g. for logging a
// confirmed RuptureEvent). This is the cold path — ruptures are rare — so
// the allocation here is acceptable.
func CellIDString(cellID uint64) string {
	return h3.ToString(h3.H3Index(cellID))
}

// CellCenter reverses a cell index back into its hexagon's centroid
// lat/lng. Used by the dashboard aggregator so the frontend can place a
// density marker without needing any H3 logic of its own.
func (idx *H3Indexer) CellCenter(cellID uint64) (lat, lng float64) {
	geo := h3.ToGeo(h3.H3Index(cellID))
	return geo.Latitude, geo.Longitude
}

// NewDefaultIndexer returns the production CellIndexer for this build: real
// H3 resolution-8 cells.
func NewDefaultIndexer(resolution int) CellIndexer {
	return NewH3Indexer(resolution)
}
