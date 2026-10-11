package server_test

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"ne-pulse/internal/client"
	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/server"
)

// A class phone that lies still for five minutes earns listening time for
// tonight; the admin research view lists it under its recording pseudonym
// with its code; deleting the phone removes the night.
func TestNightsListeningAndCodes(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 100, func(c *server.Config) {
		c.RecordDir = dir
		c.Cohorts = []string{"8A11UZ"}
		c.MaxSamplesPerSec = 1e6
		c.NightMinHours = 0.05 // 3 minutes, so the test earns a code
	})
	ctx := context.Background()
	creds, err := client.EnrollCohort(ctx, nil, h.http.URL, "android", "8a11uz", nil)
	if err != nil {
		t.Fatal(err)
	}
	cell := geo.Grid{CellKM: 1}.CellOf(41.31, 69.28)
	conn, err := client.Dial(ctx, h.wsURL("/ws/device"), creds, protocol.Location{GridKM: 1, Row: cell.Row, Col: cell.Col}, "android", nil)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(1))
	const secs, fs = 300, 60
	start := h.clock.Now()
	for s := 0; s < secs; s++ {
		for h.clock.Now() < start+float64(s)+1 { // stream in (fast-forwarded) real time
			time.Sleep(time.Millisecond)
		}
		var b []dsp.Sample3
		for i := 0; i < fs; i++ {
			tt := start + float64(s) + float64(i)/fs + rng.Float64()*0.002
			b = append(b, dsp.Sample3{T: tt, X: 0.004 * rng.NormFloat64(), Y: 0.004 * rng.NormFloat64(), Z: 9.81 + 0.004*rng.NormFloat64()})
		}
		if err := conn.SendSamples(b, 0, 40); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	conn.Close()

	get := func(path string, hdr map[string]string, out any) int {
		req, _ := http.NewRequest("GET", h.http.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(out)
		return resp.StatusCode
	}
	me := map[string]string{"X-Device-Id": creds.DeviceID, "Authorization": "Bearer " + creds.Secret}
	var mine struct {
		Nights []struct {
			Night    string
			Hours    float64
			Eligible bool
			Code     string
		}
	}
	for i := 0; i < 150; i++ { // the night is written when the session closes (async)
		get("/api/v1/devices/me/nights", me, &mine)
		if len(mine.Nights) > 0 && mine.Nights[0].Eligible {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(mine.Nights) != 1 || mine.Nights[0].Hours < 0.05 || !mine.Nights[0].Eligible || len(mine.Nights[0].Code) != 9 {
		ns, _ := h.st.NightsFor(ctx, "", "")
		t.Fatalf("my nights: %+v (store: %+v)", mine, ns)
	}
	if mine.Nights[0].Night != server.NightOf(start+secs) {
		t.Fatalf("night name %s, want %s", mine.Nights[0].Night, server.NightOf(start+secs))
	}

	var res struct {
		Joined int
		Rows   []struct {
			Device string
			Code   string
			Hours  float64
			Flags  []string
		}
	}
	if code := get("/api/admin/research?cohort=8A11UZ", nil, &res); code != 401 {
		t.Fatalf("research without token: %d", code)
	}
	get("/api/admin/research?cohort=8A11UZ", map[string]string{"Authorization": "Bearer " + "admin-test-token"}, &res)
	if res.Joined != 1 || len(res.Rows) != 1 || res.Rows[0].Code != mine.Nights[0].Code || len(res.Rows[0].Device) != 12 {
		t.Fatalf("research: %+v", res)
	}
	for _, f := range res.Rows[0].Flags {
		if f == "too-clean" || f[:3] == "not" {
			t.Fatalf("a normal phone was flagged: %v", res.Rows[0].Flags)
		}
	}

	// Leaving deletes the nights too.
	req, _ := http.NewRequest("DELETE", h.http.URL+"/api/v1/devices/me", nil)
	req.Header.Set("X-Device-Id", creds.DeviceID)
	req.Header.Set("Authorization", "Bearer "+creds.Secret)
	http.DefaultClient.Do(req)
	nights, _ := h.st.NightsFor(ctx, "8A11UZ", "")
	if len(nights) != 0 {
		t.Fatalf("nights left after deletion: %d", len(nights))
	}
}
