// Package geo holds the spherical-earth geometry and the deterministic
// equal-size grid NE-PULSE uses for (a) coarsening device locations on the
// device before they are ever transmitted, (b) grouping co-located sensors
// into statistically independent "units", and (c) aggregating public maps.
//
// All distances are kilometres, all angles are decimal degrees (WGS84
// latitude/longitude treated on a sphere of mean radius EarthRadiusKM). The
// spherical approximation introduces < 0.5 % distance error, far below the
// ~1 km location quantisation the rest of the system deliberately applies.
package geo

import (
	"fmt"
	"math"
)

// EarthRadiusKM is the IUGG mean Earth radius R1.
const EarthRadiusKM = 6371.0088

// KMPerDegLat is the length of one degree of latitude on that sphere.
const KMPerDegLat = 2 * math.Pi * EarthRadiusKM / 360

const degToRad = math.Pi / 180

// HaversineKM returns the great-circle distance between two points.
func HaversineKM(lat1, lng1, lat2, lng2 float64) float64 {
	p1 := lat1 * degToRad
	p2 := lat2 * degToRad
	dp := (lat2 - lat1) * degToRad
	dl := (lng2 - lng1) * degToRad
	s1 := math.Sin(dp / 2)
	s2 := math.Sin(dl / 2)
	a := s1*s1 + math.Cos(p1)*math.Cos(p2)*s2*s2
	if a > 1 {
		a = 1
	}
	return 2 * EarthRadiusKM * math.Asin(math.Sqrt(a))
}

// Destination returns the point reached by travelling distKM from (lat,lng)
// along the initial bearing bearingDeg (clockwise from north).
func Destination(lat, lng, bearingDeg, distKM float64) (float64, float64) {
	d := distKM / EarthRadiusKM
	b := bearingDeg * degToRad
	p1 := lat * degToRad
	l1 := lng * degToRad
	p2 := math.Asin(math.Sin(p1)*math.Cos(d) + math.Cos(p1)*math.Sin(d)*math.Cos(b))
	l2 := l1 + math.Atan2(math.Sin(b)*math.Sin(d)*math.Cos(p1), math.Cos(d)-math.Sin(p1)*math.Sin(p2))
	lng2 := math.Mod(l2/degToRad+540, 360) - 180
	return p2 / degToRad, lng2
}

// LocalXY projects (lat,lng) onto a local tangent plane centred on
// (lat0,lng0) using an equirectangular approximation, returning east/north
// offsets in km. Accurate to well under 1 % inside a few hundred km, which is
// the scale of the associator's grid search.
func LocalXY(lat0, lng0, lat, lng float64) (x, y float64) {
	dl := lng - lng0
	if dl > 180 {
		dl -= 360
	} else if dl < -180 {
		dl += 360
	}
	x = dl * KMPerDegLat * math.Cos(lat0*degToRad)
	y = (lat - lat0) * KMPerDegLat
	return x, y
}

// FromLocalXY inverts LocalXY.
func FromLocalXY(lat0, lng0, x, y float64) (lat, lng float64) {
	lat = lat0 + y/KMPerDegLat
	c := math.Cos(lat0 * degToRad)
	if c < 1e-6 {
		c = 1e-6
	}
	lng = lng0 + x/(KMPerDegLat*c)
	lng = math.Mod(lng+540, 360) - 180
	return lat, lng
}

// Grid is a deterministic partition of the sphere into cells of roughly
// CellKM x CellKM. Rows are bands of constant latitude height; each row is
// divided into an integer number of equal-longitude columns chosen so a
// column is as close as possible to CellKM wide at the row's centre latitude.
//
// The same arithmetic is implemented in web/lib/sensor/grid.ts so a phone
// can compute its own cell and transmit only (row, col) — the server never
// receives a coordinate finer than one cell.
type Grid struct {
	CellKM float64
}

// Cell identifies one grid cell. Row 0 starts at the south pole.
type Cell struct {
	Row int32 `json:"row"`
	Col int32 `json:"col"`
}

func (g Grid) dLat() float64 { return g.CellKM / KMPerDegLat }

// Rows is the number of latitude bands.
func (g Grid) Rows() int32 { return int32(math.Ceil(180 / g.dLat())) }

// Cols is the number of cells in the given row.
func (g Grid) Cols(row int32) int32 {
	latC := -90 + (float64(row)+0.5)*g.dLat()
	c := math.Cos(latC * degToRad)
	if c < 1e-3 {
		c = 1e-3
	}
	n := math.Round(360 * KMPerDegLat * c / g.CellKM)
	if n < 1 {
		n = 1
	}
	return int32(n)
}

// CellOf returns the cell containing (lat, lng).
func (g Grid) CellOf(lat, lng float64) Cell {
	if lat >= 90 {
		lat = 89.999999
	}
	if lat < -90 {
		lat = -90
	}
	row := int32(math.Floor((lat + 90) / g.dLat()))
	if max := g.Rows() - 1; row > max {
		row = max
	}
	n := g.Cols(row)
	lng = math.Mod(lng+540, 360) - 180
	col := int32(math.Floor((lng + 180) / (360 / float64(n))))
	if col >= n {
		col = n - 1
	}
	if col < 0 {
		col = 0
	}
	return Cell{Row: row, Col: col}
}

// Valid reports whether c exists in this grid.
func (g Grid) Valid(c Cell) bool {
	return c.Row >= 0 && c.Row < g.Rows() && c.Col >= 0 && c.Col < g.Cols(c.Row)
}

// Center returns the centroid of a cell.
func (g Grid) Center(c Cell) (lat, lng float64) {
	lat = -90 + (float64(c.Row)+0.5)*g.dLat()
	n := g.Cols(c.Row)
	lng = -180 + (float64(c.Col)+0.5)*(360/float64(n))
	return lat, lng
}

// Coarsen maps a cell of g onto the cell of coarse that contains its centre.
func (g Grid) Coarsen(c Cell, coarse Grid) Cell {
	lat, lng := g.Center(c)
	return coarse.CellOf(lat, lng)
}

// Key packs a cell into a comparable integer.
func (c Cell) Key() uint64 { return uint64(uint32(c.Row))<<32 | uint64(uint32(c.Col)) }

// String renders "row:col".
func (c Cell) String() string { return fmt.Sprintf("%d:%d", c.Row, c.Col) }

// CellFromKey inverts Key.
func CellFromKey(k uint64) Cell { return Cell{Row: int32(uint32(k >> 32)), Col: int32(uint32(k))} }
