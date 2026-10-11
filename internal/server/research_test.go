package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ne-pulse/internal/client"
	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/server"
)

func TestPlacementLabelsAndTelegramLink(t *testing.T) {
	dir := t.TempDir()
	var srv *server.Server
	h := newHarness(t, 1, func(c *server.Config) {
		c.RecordDir = dir
		c.Cohorts = []string{"TASH-9B"}
		c.TelegramBot = "nepulse_test_bot"
	})
	srv = h.srv
	ctx := context.Background()
	creds, err := client.EnrollCohort(ctx, nil, h.http.URL, "android", "TASH-9B", nil)
	if err != nil {
		t.Fatal(err)
	}
	post := func(path string, body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", h.http.URL+path, bytes.NewReader(b))
		req.Header.Set("X-Device-Id", creds.DeviceID)
		req.Header.Set("Authorization", "Bearer "+creds.Secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// No area yet: a Telegram link needs one.
	if code, _ := post("/api/v1/devices/me/telegram", nil); code != 409 {
		t.Fatalf("telegram link before streaming: %d", code)
	}
	cell := geo.Grid{CellKM: 1}.CellOf(41.31, 69.28)
	conn, err := client.Dial(ctx, h.wsURL("/ws/device"), creds, protocol.Location{GridKM: 1, Row: cell.Row, Col: cell.Col}, "android", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := h.clock.Now()
	var s []dsp.Sample3
	for i := 0; i < 60; i++ {
		s = append(s, dsp.Sample3{T: now - 1 + float64(i)/60, Z: 9.81})
	}
	conn.SendSamples(s, 0, 40)
	time.Sleep(150 * time.Millisecond)
	if live, _, ok := srv.DeviceSeen(creds.DeviceID); !live || !ok {
		t.Fatal("connected phone not seen as live")
	}
	// During the very first session the area is not stored yet; the link
	// must still work (from the live session).
	if code, _ := post("/api/v1/devices/me/telegram", nil); code != 200 {
		t.Fatalf("telegram link during first session: %d", code)
	}
	conn.Close()

	if code, _ := post("/api/v1/devices/me/placement", map[string]string{"floor": "4-9", "surface": "table", "building": "apartment"}); code != 200 {
		t.Fatalf("placement: %d", code)
	}
	if code, _ := post("/api/v1/devices/me/placement", map[string]string{"floor": "99", "surface": "table", "building": "apartment"}); code != 400 {
		t.Fatalf("bad placement accepted: %d", code)
	}
	if code, _ := post("/api/v1/devices/me/label", map[string]string{"kind": "door"}); code != 200 {
		t.Fatalf("label: %d", code)
	}
	if code, _ := post("/api/v1/devices/me/label", map[string]string{"kind": "door"}); code != 429 {
		t.Fatalf("label rate limit: %d", code)
	}
	if code, _ := post("/api/v1/devices/me/label", map[string]string{"kind": "earthquake!"}); code != 400 {
		t.Fatalf("unknown label accepted: %d", code)
	}

	// Labels and the placement land next to the recordings, under the pseudonym.
	var files []string
	for i := 0; i < 100 && len(files) == 0; i++ {
		files, _ = filepath.Glob(filepath.Join(dir, "TASH-9B", "*", "*_labels.csv"))
		time.Sleep(20 * time.Millisecond)
	}
	if len(files) != 1 || strings.Contains(files[0], creds.DeviceID) {
		t.Fatalf("labels file: %v", files)
	}
	var b []byte
	for i := 0; i < 100; i++ { // the label is written just after the placement
		b, _ = os.ReadFile(files[0])
		if strings.Contains(string(b), ",door,") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(string(b), ",placement,floor=4-9;surface=table;building=apartment") || !strings.Contains(string(b), ",door,") {
		t.Fatalf("labels content:\n%s", b)
	}

	// A one-time Telegram link resolves to this phone's area, once. (After the
	// session the area comes from the store, written asynchronously.)
	code, out := post("/api/v1/devices/me/telegram", nil)
	for i := 0; i < 100 && code == 409; i++ {
		time.Sleep(20 * time.Millisecond)
		code, out = post("/api/v1/devices/me/telegram", nil)
	}
	url, _ := out["url"].(string)
	if code != 200 || !strings.HasPrefix(url, "https://t.me/nepulse_test_bot?start=d") {
		t.Fatalf("telegram link: %d %v", code, out)
	}
	tok := strings.TrimPrefix(url, "https://t.me/nepulse_test_bot?start=d")
	dev, lat, lng, ok := srv.ClaimTelegramLink(tok)
	if !ok || dev != creds.DeviceID || lat < 41.2 || lat > 41.4 || lng < 69.2 || lng > 69.4 {
		t.Fatalf("claim: %v %v %v %v", dev, lat, lng, ok)
	}
	if _, _, _, ok := srv.ClaimTelegramLink(tok); ok {
		t.Fatal("link usable twice")
	}
}
