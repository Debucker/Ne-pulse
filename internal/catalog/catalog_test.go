package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ne-pulse/internal/store"
)

// Response shapes captured from the live services on 2026-09-29.
const usgsBody = `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"mag":3.4,"place":"7 km SE of Almaty, Kazakhstan","time":1790570290605,"url":"https://earthquake.usgs.gov/earthquakes/eventpage/us6000ty55","magType":"ml","type":"earthquake"},"geometry":{"type":"Point","coordinates":[76.9838,43.2073,10]},"id":"us6000ty55"}]}`
const emscBody = `{"type":"FeatureCollection","metadata":{"count":1},"features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[69.69,40.75,-33.0]},"id":"20260928_0000104","properties":{"time":"2026-09-28T04:03:04.7Z","flynn_region":"EASTERN UZBEKISTAN","lat":40.75,"lon":69.69,"depth":33.0,"mag":4.1,"magtype":"mb","unid":"20260928_0000104"}}]}`

func TestParsers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") == "geojson" {
			w.Write([]byte(usgsBody))
		} else {
			w.Write([]byte(emscBody))
		}
	}))
	defer srv.Close()
	c := &Client{USGSURL: srv.URL, EMSCURL: srv.URL}
	reg := Region{35, 47, 55, 77}
	u, err := c.USGS(context.Background(), reg, time.Now().Add(-time.Hour), time.Now(), 2)
	if err != nil || len(u) != 1 || u[0].Lat != 43.2073 || u[0].DepthKM != 10 || u[0].Time != 1790570290.605 {
		t.Fatalf("usgs %+v %v", u, err)
	}
	e, err := c.EMSC(context.Background(), reg, time.Now().Add(-time.Hour), time.Now(), 2)
	if err != nil || len(e) != 1 || e[0].DepthKM != 33 || e[0].Place != "EASTERN UZBEKISTAN" {
		t.Fatalf("emsc %+v %v", e, err)
	}
	if want := time.Date(2026, 9, 28, 4, 3, 4, 700e6, time.UTC); e[0].Time != float64(want.UnixNano())/1e9 {
		t.Fatalf("emsc time %v", e[0].Time)
	}
}

func TestBestMatch(t *testing.T) {
	cat := []store.CatalogEvent{
		{Source: "emsc", ID: "a", Time: 1000, Lat: 40.75, Lng: 69.69, Mag: 4.1},
		{Source: "usgs", ID: "far", Time: 1002, Lat: 43.2, Lng: 76.98, Mag: 3.4},    // 600 km away
		{Source: "usgs", ID: "late", Time: 1200, Lat: 40.75, Lng: 69.69, Mag: 5.0}, // 200 s later
	}
	m := Best(40.80, 69.60, 1004, 5, cat, DefaultMatchParams())
	if m == nil || m.ID != "a" || m.DistanceKM > 10 {
		t.Fatalf("match %+v", m)
	}
	if Best(20, 20, 1000, 5, cat, DefaultMatchParams()) != nil {
		t.Fatal("matched a distant event")
	}
}
