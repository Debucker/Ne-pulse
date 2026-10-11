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

func TestCohortRecordingAndWithdrawal(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 1, func(c *server.Config) {
		c.RecordDir = dir
		c.Cohorts = []string{"tash-9b"}
	})
	ctx := context.Background()

	if _, err := client.EnrollCohort(ctx, nil, h.http.URL, "android", "NOPE-1", nil); err == nil || !strings.Contains(err.Error(), "class code") {
		t.Fatalf("unknown class code accepted: %v", err)
	}
	creds, err := client.EnrollCohort(ctx, nil, h.http.URL, "android", "tash-9b", nil) // normalised to TASH-9B
	if err != nil {
		t.Fatal(err)
	}
	plain, err := client.Enroll(ctx, nil, h.http.URL, "android", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := geo.Grid{CellKM: 1}.CellOf(41.3, 69.2)
	send := func(cr client.Creds) {
		conn, err := client.Dial(ctx, h.wsURL("/ws/device"), cr, protocol.Location{GridKM: 1, Row: c.Row, Col: c.Col}, "android", nil)
		if err != nil {
			t.Fatal(err)
		}
		now := h.clock.Now()
		var s []dsp.Sample3
		for i := 0; i < 60; i++ {
			s = append(s, dsp.Sample3{T: now - 1 + float64(i)/60, X: 0.01, Z: 9.81})
		}
		if err := conn.SendSamples(s, 0, 40); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
		conn.Close()
	}
	send(creds)
	send(plain)

	// Recordings are flushed when idle or at shutdown; wait for the file.
	var files []string
	for i := 0; i < 100 && len(files) == 0; i++ {
		files, _ = filepath.Glob(filepath.Join(dir, "TASH-9B", "*", "*.csv.gz"))
		time.Sleep(20 * time.Millisecond)
	}
	if len(files) != 1 {
		t.Fatalf("want exactly one recording (cohort device only), got %v", files)
	}
	if strings.Contains(files[0], creds.DeviceID) {
		t.Fatal("device id in recording path")
	}

	// Admin can list it; the public cannot.
	req, _ := http.NewRequest("GET", h.http.URL+"/api/admin/recordings?cohort=TASH-9B", nil)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("recordings without token: %d", resp.StatusCode)
	}
	var list struct {
		Files []struct{ Path string } `json:"files"`
	}
	req.Header.Set("Authorization", "Bearer admin-test-token")
	getJSONReq(t, req, &list)
	if len(list.Files) != 1 {
		t.Fatalf("admin list: %+v", list)
	}
	req, _ = http.NewRequest("GET", h.http.URL+"/api/admin/recordings/file?path=../np.db", nil)
	req.Header.Set("Authorization", "Bearer admin-test-token")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 400 {
		t.Fatalf("path traversal: %d", resp.StatusCode)
	}

	// Withdrawal deletes the recordings.
	req, _ = http.NewRequest("DELETE", h.http.URL+"/api/v1/devices/me", nil)
	req.Header.Set("X-Device-Id", creds.DeviceID)
	req.Header.Set("Authorization", "Bearer "+creds.Secret)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 200 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		t.Fatalf("recording survived deletion: %v", err)
	}

	// A device can join a class later.
	req, _ = http.NewRequest("POST", h.http.URL+"/api/v1/devices/me/cohort", bytes.NewBufferString(`{"cohort":"TASH-9B"}`))
	req.Header.Set("X-Device-Id", plain.DeviceID)
	req.Header.Set("Authorization", "Bearer "+plain.Secret)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 200 {
		t.Fatalf("set cohort: %d", resp.StatusCode)
	}
}

func getJSONReq(t *testing.T, req *http.Request, v any) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%s: %d", req.URL, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}
