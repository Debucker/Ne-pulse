// Package catalog fetches authoritative earthquake catalogs (USGS ComCat,
// EMSC) for validation. Catalog data never drives detection: it is used
// after the fact to label NE-PULSE events as externally verified, to find
// catalog earthquakes the network missed, and to evaluate performance.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"

	"ne-pulse/internal/geo"
	"ne-pulse/internal/store"
)

// Region is a lat/lng bounding box.
type Region struct {
	MinLat, MaxLat, MinLng, MaxLng float64
}

// Client fetches catalogs.
type Client struct {
	HTTP    *http.Client
	USGSURL string // default https://earthquake.usgs.gov/fdsnws/event/1/query
	EMSCURL string // default https://www.seismicportal.eu/fdsnws/event/1/query
	UserAgt string
}

func (c *Client) get(ctx context.Context, base string, q url.Values, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	ua := c.UserAgt
	if ua == "" {
		ua = "ne-pulse-validation/2.0 (+https://ne-pulse.com)"
	}
	req.Header.Set("User-Agent", ua)
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := h.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: HTTP %d", base, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func query(r Region, start, end time.Time, minMag float64) url.Values {
	q := url.Values{}
	q.Set("starttime", start.UTC().Format("2006-01-02T15:04:05"))
	q.Set("endtime", end.UTC().Format("2006-01-02T15:04:05"))
	q.Set("minlatitude", fmt.Sprint(r.MinLat))
	q.Set("maxlatitude", fmt.Sprint(r.MaxLat))
	q.Set("minlongitude", fmt.Sprint(r.MinLng))
	q.Set("maxlongitude", fmt.Sprint(r.MaxLng))
	q.Set("minmagnitude", fmt.Sprint(minMag))
	q.Set("limit", "2000")
	return q
}

// USGS fetches ComCat events (GeoJSON).
func (c *Client) USGS(ctx context.Context, r Region, start, end time.Time, minMag float64) ([]store.CatalogEvent, error) {
	base := c.USGSURL
	if base == "" {
		base = "https://earthquake.usgs.gov/fdsnws/event/1/query"
	}
	q := query(r, start, end, minMag)
	q.Set("format", "geojson")
	var fc struct {
		Features []struct {
			ID         string `json:"id"`
			Properties struct {
				Mag     *float64 `json:"mag"`
				Place   string   `json:"place"`
				Time    int64    `json:"time"`
				URL     string   `json:"url"`
				MagType string   `json:"magType"`
				Type    string   `json:"type"`
			} `json:"properties"`
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := c.get(ctx, base, q, &fc); err != nil {
		return nil, err
	}
	var out []store.CatalogEvent
	for _, f := range fc.Features {
		if f.Properties.Type != "" && f.Properties.Type != "earthquake" {
			continue
		}
		if len(f.Geometry.Coordinates) < 3 || f.Properties.Mag == nil {
			continue
		}
		out = append(out, store.CatalogEvent{Source: "usgs", ID: f.ID, Time: float64(f.Properties.Time) / 1000,
			Lng: f.Geometry.Coordinates[0], Lat: f.Geometry.Coordinates[1], DepthKM: f.Geometry.Coordinates[2],
			Mag: *f.Properties.Mag, MagType: f.Properties.MagType, Place: f.Properties.Place, URL: f.Properties.URL})
	}
	return out, nil
}

// EMSC fetches events from the EMSC FDSN service (GeoJSON with EMSC
// property names; depth is taken from properties, since the geometry's
// third coordinate is negative-down).
func (c *Client) EMSC(ctx context.Context, r Region, start, end time.Time, minMag float64) ([]store.CatalogEvent, error) {
	base := c.EMSCURL
	if base == "" {
		base = "https://www.seismicportal.eu/fdsnws/event/1/query"
	}
	q := query(r, start, end, minMag)
	q.Set("format", "json")
	var fc struct {
		Features []struct {
			ID         string `json:"id"`
			Properties struct {
				Time    string   `json:"time"`
				Lat     float64  `json:"lat"`
				Lon     float64  `json:"lon"`
				Depth   float64  `json:"depth"`
				Mag     *float64 `json:"mag"`
				MagType string   `json:"magtype"`
				Region  string   `json:"flynn_region"`
				EvType  string   `json:"evtype"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := c.get(ctx, base, q, &fc); err != nil {
		return nil, err
	}
	var out []store.CatalogEvent
	for _, f := range fc.Features {
		if f.Properties.Mag == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, f.Properties.Time)
		if err != nil {
			continue
		}
		out = append(out, store.CatalogEvent{Source: "emsc", ID: f.ID, Time: float64(t.UnixNano()) / 1e9,
			Lat: f.Properties.Lat, Lng: f.Properties.Lon, DepthKM: f.Properties.Depth, Mag: *f.Properties.Mag,
			MagType: f.Properties.MagType, Place: f.Properties.Region,
			URL: "https://www.seismicportal.eu/eventdetails.html?unid=" + url.QueryEscape(f.ID)})
	}
	return out, nil
}

// Match is an association between a NE-PULSE event and a catalog event.
type Match struct {
	Source     string  `json:"source"`
	ID         string  `json:"id"`
	Mag        float64 `json:"mag"`
	MagType    string  `json:"magType"`
	Place      string  `json:"place"`
	URL        string  `json:"url"`
	DistanceKM float64 `json:"distanceKm"` // NE-PULSE epicentre -> catalog epicentre
	DtSec      float64 `json:"dtSec"`      // NE-PULSE origin - catalog origin
}

// MatchParams bounds an acceptable association.
type MatchParams struct {
	MaxDtSec   float64 // |origin difference|
	MaxDistKM  float64 // floor on the distance tolerance
	UncFactor  float64 // distance tolerance also grows with location uncertainty
	MaxMagDist float64 // catalog events must be felt-plausible: distance <= this x 10^(0.5 M) km (loose)
}

// DefaultMatchParams: NE-PULSE origin times carry location/phase ambiguity
// of seconds and epicentres tens of km for sparse networks.
func DefaultMatchParams() MatchParams {
	return MatchParams{MaxDtSec: 30, MaxDistKM: 50, UncFactor: 2, MaxMagDist: 3}
}

// Best returns the catalog event that best explains a NE-PULSE event, by
// normalised space-time distance, or nil.
func Best(lat, lng, originT, locUnc float64, cat []store.CatalogEvent, p MatchParams) *Match {
	var best *Match
	bestScore := math.Inf(1)
	tol := math.Max(p.MaxDistKM, p.UncFactor*locUnc)
	for _, c := range cat {
		dt := originT - c.Time
		if math.Abs(dt) > p.MaxDtSec {
			continue
		}
		d := geo.HaversineKM(lat, lng, c.Lat, c.Lng)
		if d > tol {
			continue
		}
		// Very distant from a small catalog event: NE-PULSE phones could
		// not have felt it, so this is a coincidence, not a detection.
		if d > p.MaxMagDist*math.Pow(10, 0.5*c.Mag) {
			continue
		}
		score := math.Pow(dt/p.MaxDtSec, 2) + math.Pow(d/tol, 2)
		if score < bestScore {
			bestScore = score
			best = &Match{Source: c.Source, ID: c.ID, Mag: c.Mag, MagType: c.MagType, Place: c.Place, URL: c.URL, DistanceKM: d, DtSec: dt}
		}
	}
	return best
}
