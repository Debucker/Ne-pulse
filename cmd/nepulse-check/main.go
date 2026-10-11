// Command nepulse-check is a pre-flight test of a live server: it joins one
// simulated still phone to a research class, streams for a few minutes, checks
// that the server counts its listening time, then deletes it again.
//
//	go run ./cmd/nepulse-check -url https://xxxx.trycloudflare.com -class 8A11UZ
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ne-pulse/internal/client"
	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/sim"
)

func main() {
	base := flag.String("url", "http://localhost:8080", "server base URL")
	class := flag.String("class", "", "research class code")
	dur := flag.Duration("duration", 4*time.Minute, "how long to stream")
	keep := flag.Bool("keep", false, "do not delete the test phone afterwards")
	name := flag.String("name", "", "also set a contributor name and check it is saved")
	flag.Parse()
	ctx := context.Background()
	hc := &http.Client{Timeout: 30 * time.Second}

	c, err := client.EnrollCohort(ctx, hc, *base, "android", *class, nil)
	if err != nil {
		log.Fatalf("FAIL enrol: %v", err)
	}
	log.Printf("enrolled test phone %s… in class %q", c.DeviceID[:6], *class)
	api := func(method, path string) (map[string]any, error) {
		req, _ := http.NewRequest(method, *base+path, nil)
		req.Header.Set("X-Device-Id", c.DeviceID)
		req.Header.Set("Authorization", "Bearer "+c.Secret)
		resp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		if resp.StatusCode >= 300 {
			return m, fmt.Errorf("HTTP %d %v", resp.StatusCode, m["error"])
		}
		return m, nil
	}
	fail := false
	defer func() {
		if !*keep {
			if _, err := api("DELETE", "/api/v1/devices/me"); err != nil {
				log.Printf("FAIL delete: %v", err)
				fail = true
			} else {
				log.Print("test phone deleted")
			}
		}
		if fail {
			os.Exit(1)
		}
		log.Print("ALL OK")
	}()

	if *name != "" {
		req, _ := http.NewRequest("POST", *base+"/api/v1/devices/me/credit", strings.NewReader(`{"name":`+strconv.Quote(*name)+`}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Id", c.DeviceID)
		req.Header.Set("Authorization", "Bearer "+c.Secret)
		var cr map[string]any
		if resp, err := hc.Do(req); err != nil || resp.StatusCode != 200 {
			log.Printf("FAIL set name: %v", err)
			fail = true
			return
		} else {
			json.NewDecoder(resp.Body).Decode(&cr)
			resp.Body.Close()
		}
		if cr["name"] != *name || cr["ref"] == "" {
			log.Printf("FAIL credit: %v", cr)
			fail = true
			return
		}
		log.Printf("name saved: %v (invite code %v)", cr["name"], cr["ref"])
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	lat, lng := 41.3111, 69.2797
	cell := geo.Grid{CellKM: 1}.CellOf(lat, lng)
	ws := "ws" + strings.TrimPrefix(*base, "http") + "/ws/device"
	conn, err := client.Dial(ctx, ws, c, protocol.Location{GridKM: 1, Row: cell.Row, Col: cell.Col}, "android", nil)
	if err != nil {
		log.Printf("FAIL connect: %v", err)
		fail = true
		return
	}
	conn.Sync()
	dm := sim.TypicalPhone(rng.Float64() * 360)
	stop := time.Now().Add(*dur)
	last := float64(time.Now().UnixNano()) / 1e9
	for now := range time.Tick(time.Second) {
		if now.After(stop) {
			break
		}
		select {
		case <-conn.Done():
			log.Printf("FAIL connection closed by server: %+v", conn.LastErr.Load())
			fail = true
			return
		default:
		}
		t := float64(now.UnixNano()) / 1e9
		var batch []dsp.Sample3
		sim.Stream(rng, dm, nil, last, t, func(s dsp.Sample3) { batch = append(batch, s) })
		last = t
		if err := conn.SendSamples(batch, 0, 20); err != nil {
			log.Printf("FAIL send: %v", err)
			fail = true
			return
		}
		if st := conn.Status.Load(); st != nil && int(stop.Sub(now).Seconds())%30 == 0 {
			log.Printf("state %s", st.State)
		}
	}
	conn.Close()
	time.Sleep(6 * time.Second)
	me, err := api("GET", "/api/v1/devices/me")
	if err != nil {
		log.Printf("FAIL me: %v", err)
		fail = true
		return
	}
	log.Printf("me: cohort=%v telegramOn=%v", me["cohort"], me["telegramOn"])
	if *class != "" && me["cohort"] != *class {
		log.Printf("FAIL cohort not set")
		fail = true
	}
	n, err := api("GET", "/api/v1/devices/me/nights")
	if err != nil {
		log.Printf("FAIL nights: %v", err)
		fail = true
		return
	}
	var hours float64
	rows, _ := n["nights"].([]any)
	for _, r := range rows {
		if m, _ := r.(map[string]any); m != nil && m["night"] == n["tonight"] {
			hours, _ = m["hours"].(float64)
		}
	}
	log.Printf("tonight: %.3f h listening counted (minHours %v)", hours, n["minHours"])
	if hours <= 0 {
		log.Print("FAIL no listening time counted")
		fail = true
	}
}
